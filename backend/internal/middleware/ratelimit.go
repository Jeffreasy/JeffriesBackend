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

// RateLimiter returns an HTTP middleware that limits requests by IP.
func RateLimiter() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}

			limit := rate.Limit(5)
			burst := 10
			key := ip
			if strings.HasPrefix(r.URL.Path, "/api/v1/bridge/") {
				limit = rate.Limit(20)
				burst = 80
				key = ip + "|bridge"
			}

			limiter := getClient(key, limit, burst)
			if !limiter.Allow() {
				slog.Warn("Rate limit exceeded", "ip", ip, "path", r.URL.Path)
				http.Error(w, `{"error": "Too Many Requests"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
