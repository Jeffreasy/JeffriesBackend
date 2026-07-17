package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
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
	r.Use(securityHeadersMiddleware(cfg.IsDevelopment()))
	r.Use(corsMiddleware(cfg.CORSOrigins))
	r.Use(customMiddleware.RateLimiterWithLimits(cfg.TrustedProxyCount, customMiddleware.RateLimits{
		APIRequestsPerSecond: cfg.APIRateLimitRPS, APIBurst: cfg.APIRateLimitBurst,
		BridgeRequestsPerSecond: cfg.BridgeRateLimitRPS, BridgeBurst: cfg.BridgeRateLimitBurst,
	}))
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
	autoH := handler.NewAutomationHandler(store.NewAutomationStore(db), cfg.HomeappUserID)
	scheduleH := handler.NewScheduleHandler(store.NewScheduleStore(db), cfg.HomeappUserID)
	transactionH := handler.NewTransactionHandler(store.NewTransactionStore(db), cfg.HomeappUserID)
	salaryH := handler.NewSalaryHandler(store.NewSalaryStore(db), cfg.HomeappUserID)
	loonstrookH := handler.NewLoonstrookHandler(store.NewLoonstrookStore(db), cfg.HomeappUserID)
	personalEventH := handler.NewPersonalEventHandler(store.NewPersonalEventStore(db), cfg)
	emailH := handler.NewEmailHandler(store.NewEmailStore(db), cfg.HomeappUserID)
	privacyH := handler.NewPrivacyHandler(store.NewPrivacyStore(db), cfg.HomeappUserID)
	noteH := handler.NewNoteHandler(store.NewNoteStore(db), cfg.HomeappUserID)
	habitH := handler.NewHabitHandler(store.NewHabitStore(db), cfg.HomeappUserID)
	pendingH := handler.NewPendingActionHandler(db, cfg)
	laventeCareStore := store.NewLaventeCareStore(db)
	lcH := handler.NewLaventeCareHandler(laventeCareStore, store.NewPendingStore(db.Pool), cfg.HomeappUserID, cfg)
	intakeH := handler.NewPublicIntakeHandler(laventeCareStore, cfg.HomeappUserID)
	focusH := handler.NewFocusHandler(db, cfg)
	contactH := handler.NewContactHandler(store.NewContactStore(db), cfg.HomeappUserID)

	var telegramClient *telegram.Client
	if cfg.TelegramBotToken != "" {
		telegramClient = telegram.NewClient(cfg.TelegramBotToken)
	}
	settingsH := handler.NewSettingsHandler(db, telegramClient, cfg)
	syncH := handler.NewSyncHandler(db, cfg)
	// After "Rooster wissen" reconcile Todoist so shift tasks for deleted diensten
	// don't orphan (H10). No-op when Todoist is unconfigured.
	scheduleH.SetTodoistCleanup(syncH.ReconcileTodoist)

	registerRoutes(r, cfg, healthH, roomH, deviceH, bridgeH, sceneH, autoH,
		scheduleH, transactionH, salaryH, loonstrookH, personalEventH, emailH,
		privacyH, noteH, habitH, lcH, intakeH, settingsH, syncH, pendingH, focusH, contactH)

	return &Server{cfg: cfg, router: r, db: db}
}

// ListenAndServe starts the HTTP server and shuts it down when ctx is cancelled.
// Database/background-worker ownership stays with main so workers always stop
// before the pool is closed.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:        s.cfg.Addr(),
		Handler:     s.router,
		ReadTimeout: 15 * time.Second,
		// Must exceed the longest handler budget (gmail sync 90s, calendar/todoist
		// 60s), otherwise clients may retry work that succeeded server-side.
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("HTTP server starting", "addr", srv.Addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	slog.Info("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdownErr := srv.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		_ = srv.Close()
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	if shutdownErr != nil {
		return fmt.Errorf("http server shutdown: %w", shutdownErr)
	}
	slog.Info("server stopped cleanly")
	return nil
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

// securityHeadersMiddleware applies browser hardening to every response. The
// development-only Swagger UI needs a narrow exception for its own assets; the
// production API never serves Swagger and therefore uses a deny-by-default CSP.
func securityHeadersMiddleware(development bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("Cache-Control", "no-store")
			h.Set("X-XSS-Protection", "0")
			if development && strings.HasPrefix(r.URL.Path, "/api/v1/swagger/") {
				h.Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; img-src 'self' data:; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net")
			} else {
				h.Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
			}
			if !development {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
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

// bridgeKeyMiddleware accepts only the bridge-scoped key. An empty expected key
// always rejects, so a missing env var can never accidentally open the route.
func bridgeKeyMiddleware(bridgeKey string) func(http.Handler) http.Handler {
	expected := []byte(bridgeKey)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := []byte(r.Header.Get("X-API-Key"))
			if len(expected) == 0 || subtle.ConstantTimeCompare(key, expected) != 1 {
				handler.Error(w, http.StatusForbidden,
					"Ongeldige of ontbrekende bridge API key. Stuur X-API-Key header.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// scopedBearerMiddleware protects a narrow server-to-server endpoint without
// granting the caller the owner API key. Empty secrets always reject.
func scopedBearerMiddleware(secret string) func(http.Handler) http.Handler {
	expected := []byte(strings.TrimSpace(secret))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Fields(r.Header.Get("Authorization"))
			valid := len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") &&
				len(expected) > 0 && subtle.ConstantTimeCompare([]byte(parts[1]), expected) == 1
			if !valid {
				handler.Error(w, http.StatusUnauthorized, "Ongeldige of ontbrekende intake-authorisatie.")
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
			// Constant-time compare to avoid leaking the secret via timing. An empty
			// expected value is always disabled/fail-closed, even in development.
			if len(expected) == 0 || subtle.ConstantTimeCompare([]byte(key), expected) != 1 {
				handler.Error(w, http.StatusForbidden,
					"Ongeldige of ontbrekende API key. Stuur X-API-Key header.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
