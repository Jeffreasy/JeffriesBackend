package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
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

func TestAPIKeyMiddlewareEmptyExpectedFailsClosed(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r := httptest.NewRequest(http.MethodGet, "/api/v1/rooms", nil)
	w := httptest.NewRecorder()
	apiKeyMiddleware("")(next).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("empty expected key returned %d, want %d", w.Code, http.StatusForbidden)
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

func TestSecurityHeadersMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	t.Run("production hardens and enables HSTS", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		w := httptest.NewRecorder()
		securityHeadersMiddleware(false)(next).ServeHTTP(w, r)
		for header, want := range map[string]string{
			"Cache-Control":             "no-store",
			"Content-Security-Policy":   "default-src 'none'; base-uri 'none'; frame-ancestors 'none'",
			"Permissions-Policy":        "camera=(), geolocation=(), microphone=()",
			"Referrer-Policy":           "no-referrer",
			"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
			"X-Content-Type-Options":    "nosniff",
			"X-Frame-Options":           "DENY",
		} {
			if got := w.Header().Get(header); got != want {
				t.Errorf("%s = %q, want %q", header, got, want)
			}
		}
	})

	t.Run("development omits HSTS and permits Swagger assets", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/swagger/index.html", nil)
		w := httptest.NewRecorder()
		securityHeadersMiddleware(true)(next).ServeHTTP(w, r)
		if got := w.Header().Get("Strict-Transport-Security"); got != "" {
			t.Fatalf("development HSTS = %q, want empty", got)
		}
		if got := w.Header().Get("Content-Security-Policy"); got == "default-src 'none'; base-uri 'none'; frame-ancestors 'none'" {
			t.Fatalf("Swagger unexpectedly received the deny-all CSP")
		}
	})
}

func TestSwaggerEnabledOnlyInDevelopment(t *testing.T) {
	if !swaggerEnabled(&config.Config{AppEnv: "development"}) {
		t.Fatal("development must expose Swagger")
	}
	if swaggerEnabled(&config.Config{AppEnv: "production"}) || swaggerEnabled(nil) {
		t.Fatal("production and nil config must not expose Swagger")
	}
}
