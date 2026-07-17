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

type RateLimits struct {
	APIRequestsPerSecond    float64
	APIBurst                int
	BridgeRequestsPerSecond float64
	BridgeBurst             int
}

func DefaultRateLimits() RateLimits {
	return RateLimits{APIRequestsPerSecond: 30, APIBurst: 60, BridgeRequestsPerSecond: 20, BridgeBurst: 80}
}

// RateLimiter preserves the default policy for callers/tests that do not need
// runtime configuration.
func RateLimiter(trustedHops int) func(http.Handler) http.Handler {
	return RateLimiterWithLimits(trustedHops, DefaultRateLimits())
}

// RateLimiterWithLimits limits per trusted client IP using deployment-configured
// buckets. Invalid/non-positive values fail safely to the documented defaults.
// SensitiveRateLimiter adds a much smaller independent bucket around routes that
// trigger paid/limited third-party work (AI, Microsoft Graph, bunq). It is layered
// on top of the coarse API limiter and keyed separately, so ordinary reads cannot
// consume or refill this budget.
func SensitiveRateLimiter(trustedHops int) func(http.Handler) http.Handler {
	const requestsPerMinute = 6
	const burst = 3
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r, trustedHops)
			limiter := getClient(ip+"|sensitive", rate.Limit(float64(requestsPerMinute)/60.0), burst)
			if !limiter.Allow() {
				slog.Warn("Sensitive route rate limit exceeded", "ip", ip, "path", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "10")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"detail":"Te veel kostbare verzoeken — probeer het over enkele seconden opnieuw."}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RateLimiterWithLimits(trustedHops int, limits RateLimits) func(http.Handler) http.Handler {
	defaults := DefaultRateLimits()
	if limits.APIRequestsPerSecond <= 0 {
		limits.APIRequestsPerSecond = defaults.APIRequestsPerSecond
	}
	if limits.APIBurst <= 0 {
		limits.APIBurst = defaults.APIBurst
	}
	if limits.BridgeRequestsPerSecond <= 0 {
		limits.BridgeRequestsPerSecond = defaults.BridgeRequestsPerSecond
	}
	if limits.BridgeBurst <= 0 {
		limits.BridgeBurst = defaults.BridgeBurst
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r, trustedHops)
			limit := rate.Limit(limits.APIRequestsPerSecond)
			burst := limits.APIBurst
			key := ip
			if strings.HasPrefix(r.URL.Path, "/api/v1/bridge/") {
				limit = rate.Limit(limits.BridgeRequestsPerSecond)
				burst = limits.BridgeBurst
				key = ip + "|bridge"
			}

			limiter := getClient(key, limit, burst)
			if !limiter.Allow() {
				slog.Warn("Rate limit exceeded", "ip", ip, "path", r.URL.Path)
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
