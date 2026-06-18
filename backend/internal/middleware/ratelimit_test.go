package middleware

import (
	"net/http"
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
