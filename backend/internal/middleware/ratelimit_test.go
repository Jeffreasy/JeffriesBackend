package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		hops       int
		want       string
	}{
		{"no trust uses peer", "203.0.113.9:5555", "1.2.3.4, 5.6.7.8", 0, "203.0.113.9"},
		{"spoof ignored without trust", "203.0.113.9:5555", "9.9.9.9", 0, "203.0.113.9"},
		{"one hop takes last xff", "10.0.0.1:5555", "1.2.3.4, 5.6.7.8", 1, "5.6.7.8"},
		{"two hops takes second-to-last", "10.0.0.1:5555", "1.2.3.4, 5.6.7.8", 2, "1.2.3.4"},
		{"hops exceed list clamps to first", "10.0.0.1:5555", "1.2.3.4", 3, "1.2.3.4"},
		{"trust but no xff falls back to peer", "203.0.113.9:5555", "", 1, "203.0.113.9"},
		{"peer without port", "203.0.113.9", "", 0, "203.0.113.9"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := http.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := clientIP(r, tc.hops); got != tc.want {
				t.Fatalf("clientIP(hops=%d) = %q, want %q", tc.hops, got, tc.want)
			}
		})
	}
}
func TestRateLimiterWithLimitsUsesConfiguredBurst(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mw := RateLimiterWithLimits(0, RateLimits{
		APIRequestsPerSecond: 0.000001, APIBurst: 1,
		BridgeRequestsPerSecond: 0.000001, BridgeBurst: 1,
	})(next)
	request := func() int {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/rooms", nil)
		r.RemoteAddr = "198.51.100.77:4567"
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, r)
		return w.Code
	}
	if got := request(); got != http.StatusOK {
		t.Fatalf("first request status = %d", got)
	}
	if got := request(); got != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429 from configured burst", got)
	}
}

func TestSensitiveRateLimiterHasIndependentSmallBurst(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := SensitiveRateLimiter(0)(next)
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/laventecare/mailbox/ai-suggest", nil)
		req.RemoteAddr = "198.51.100.249:443"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if i < 3 && rec.Code != http.StatusNoContent {
			t.Fatalf("request %d got %d, want 204", i+1, rec.Code)
		}
		if i == 3 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("fourth request got %d, want 429", rec.Code)
		}
	}
}
