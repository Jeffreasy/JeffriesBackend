package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIKeyMiddleware(t *testing.T) {
	const secret = "s3cr3t-key-value"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := apiKeyMiddleware(secret)(next)

	tests := []struct {
		name     string
		key      string
		wantCode int
	}{
		{"correct key passes", secret, http.StatusOK},
		{"wrong key rejected", "wrong", http.StatusForbidden},
		{"empty key rejected", "", http.StatusForbidden},
		{"prefix of key rejected", secret[:4], http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/rooms", nil)
			if tc.key != "" {
				r.Header.Set("X-API-Key", tc.key)
			}
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, r)
			if w.Code != tc.wantCode {
				t.Fatalf("key=%q got %d, want %d", tc.key, w.Code, tc.wantCode)
			}
		})
	}
}

func TestCORSMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	t.Run("allowed origin is reflected", func(t *testing.T) {
		mw := corsMiddleware([]string{"https://app.example.com"})(next)
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Origin", "https://app.example.com")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, r)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
			t.Fatalf("ACAO = %q, want the allowed origin", got)
		}
	})

	t.Run("unlisted origin is not reflected", func(t *testing.T) {
		mw := corsMiddleware([]string{"https://app.example.com"})(next)
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Origin", "https://evil.example.com")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, r)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("ACAO = %q, want empty for unlisted origin", got)
		}
	})

	t.Run("empty allow-list denies all", func(t *testing.T) {
		mw := corsMiddleware(nil)(next)
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Origin", "https://anything.example.com")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, r)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("ACAO = %q, want empty when allow-list is empty (deny)", got)
		}
	})
}
