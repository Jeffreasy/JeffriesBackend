package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// client represents a single API client's rate limiter.
type client struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	mu      sync.Mutex
	clients = make(map[string]*client)
)

func init() {
	// Cleanup background routine to remove stale IPs from memory
	go func() {
		for {
			time.Sleep(1 * time.Minute)
			mu.Lock()
			for ip, c := range clients {
				if time.Since(c.lastSeen) > 3*time.Minute {
					delete(clients, ip)
				}
			}
			mu.Unlock()
		}
	}()
}

func getClient(key string, limit rate.Limit, burst int) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()

	c, exists := clients[key]
	if !exists {
		limiter := rate.NewLimiter(limit, burst)
		clients[key] = &client{
			limiter:  limiter,
			lastSeen: time.Now(),
		}
		return limiter
	}

	c.lastSeen = time.Now()
	return c.limiter
}

// clientIP returns the best-guess client IP. X-Forwarded-For is honoured only for
// the number of trusted reverse-proxy hops in front of the app (trustedHops);
// with trustedHops <= 0 the real TCP peer is used, which cannot be spoofed.
func clientIP(r *http.Request, trustedHops int) string {
	peer := r.RemoteAddr
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}
	if trustedHops <= 0 {
		return peer
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	parts := strings.Split(xff, ",")
	// Proxies append on the right, so the client seen by the outermost trusted
	// proxy is trustedHops entries from the end.
	idx := len(parts) - trustedHops
	if idx < 0 {
		idx = 0
	}
	ip := strings.TrimSpace(parts[idx])
	if ip == "" {
		return peer
	}
	return ip
}

// RateLimiter returns an HTTP middleware that limits requests per client IP.
// trustedHops is the number of reverse-proxy hops whose X-Forwarded-For entries
// may be trusted (e.g. 1 behind Render's edge; 0 to trust nothing).
func RateLimiter(trustedHops int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r, trustedHops)

			limit := rate.Limit(30)
			burst := 60
			key := ip
			if strings.HasPrefix(r.URL.Path, "/api/v1/bridge/") {
				limit = rate.Limit(20)
				burst = 80
				key = ip + "|bridge"
			}

			limiter := getClient(key, limit, burst)
			if !limiter.Allow() {
				slog.Warn("Rate limit exceeded", "ip", ip, "path", r.URL.Path)
				// Standard error envelope ({"detail": ...}, Dutch) + Retry-After so
				// clients can back off instead of parsing a one-off shape.
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"detail":"Te veel verzoeken — probeer het over een paar seconden opnieuw."}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
