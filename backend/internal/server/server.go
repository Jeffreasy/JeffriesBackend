package server

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/handler"
	customMiddleware "github.com/Jeffreasy/JeffriesBackend/internal/middleware"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/Jeffreasy/JeffriesBackend/internal/telegram"
	"github.com/Jeffreasy/JeffriesBackend/internal/wiz"
)

// Server wraps the HTTP server with graceful shutdown.
type Server struct {
	cfg    *config.Config
	router *chi.Mux
	db     *store.DB
}

// New creates and configures the HTTP server.
func New(cfg *config.Config, db *store.DB) *Server {
	r := chi.NewRouter()

	// Global middleware.
	// NOTE: chi's middleware.RealIP is intentionally NOT used — it trusts
	// X-Forwarded-For/X-Real-IP unconditionally, which lets a client spoof its IP
	// and bypass the rate limiter. The limiter derives the client IP itself,
	// honouring forwarded headers only for cfg.TrustedProxyCount proxy hops.
	r.Use(middleware.RequestID)
	r.Use(slogMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware(cfg.CORSOrigins))
	r.Use(customMiddleware.RateLimiter(cfg.TrustedProxyCount))
	r.Use(customMiddleware.MaxBytes(customMiddleware.DefaultMaxRequestBytes))

	// Handlers
	wizClient := wiz.NewClient()
	deviceStore := store.NewDeviceStore(db)
	commandStore := store.NewDeviceCommandStore(db)

	healthH := handler.NewHealthHandler(db)
	roomH := handler.NewRoomHandler(store.NewRoomStore(db))
	deviceH := handler.NewDeviceHandler(deviceStore, commandStore, wizClient, cfg.HomeappUserID, cfg.LightCommandMode)
	bridgeH := handler.NewBridgeHandler(deviceStore, commandStore)
	sceneH := handler.NewSceneHandler(store.NewSceneStore(db), deviceStore, commandStore, wizClient, cfg.HomeappUserID, cfg.LightCommandMode)
	autoH := handler.NewAutomationHandler(store.NewAutomationStore(db))
	scheduleH := handler.NewScheduleHandler(store.NewScheduleStore(db))
	transactionH := handler.NewTransactionHandler(store.NewTransactionStore(db), cfg.HomeappUserID)
	salaryH := handler.NewSalaryHandler(store.NewSalaryStore(db))
	loonstrookH := handler.NewLoonstrookHandler(store.NewLoonstrookStore(db))
	personalEventH := handler.NewPersonalEventHandler(store.NewPersonalEventStore(db), cfg)
	emailH := handler.NewEmailHandler(store.NewEmailStore(db))
	privacyH := handler.NewPrivacyHandler(store.NewPrivacyStore(db))
	noteH := handler.NewNoteHandler(store.NewNoteStore(db))
	habitH := handler.NewHabitHandler(store.NewHabitStore(db))
	pendingH := handler.NewPendingActionHandler(db, cfg)
	lcH := handler.NewLaventeCareHandler(store.NewLaventeCareStore(db), store.NewPendingStore(db.Pool), cfg.HomeappUserID, cfg)
	focusH := handler.NewFocusHandler(db, cfg)

	var telegramClient *telegram.Client
	if cfg.TelegramBotToken != "" {
		telegramClient = telegram.NewClient(cfg.TelegramBotToken)
	}
	settingsH := handler.NewSettingsHandler(db, telegramClient, cfg)
	syncH := handler.NewSyncHandler(db, cfg)

	registerRoutes(r, cfg, healthH, roomH, deviceH, bridgeH, sceneH, autoH,
		scheduleH, transactionH, salaryH, loonstrookH, personalEventH, emailH,
		privacyH, noteH, habitH, lcH, settingsH, syncH, pendingH, focusH)

	return &Server{cfg: cfg, router: r, db: db}
}

// ListenAndServe starts the HTTP server with graceful shutdown.
func (s *Server) ListenAndServe() {
	srv := &http.Server{
		Addr:         s.cfg.Addr(),
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		slog.Info("HTTP server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	s.db.Close()
	slog.Info("server stopped cleanly")
}

// ─── Middleware ───────────────────────────────────────────────────────────────

// slogMiddleware logs each request using slog.
func slogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration", time.Since(start).String(),
		)
	})
}

// corsMiddleware adds CORS headers.
func corsMiddleware(origins []string) func(http.Handler) http.Handler {
	originSet := make(map[string]bool, len(origins))
	for _, o := range origins {
		originSet[strings.TrimSpace(o)] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			// Only reflect an explicitly allow-listed origin. An empty allow-list
			// denies CORS rather than turning into allow-all-with-credentials.
			if origin != "" && originSet[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// apiKeyMiddleware validates the X-API-Key header.
func apiKeyMiddleware(secretKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		expected := []byte(secretKey)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-API-Key")
			// Constant-time compare to avoid leaking the secret via timing.
			if subtle.ConstantTimeCompare([]byte(key), expected) != 1 {
				handler.Error(w, http.StatusForbidden,
					"Ongeldige of ontbrekende API key. Stuur X-API-Key header.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
