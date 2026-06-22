package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMaxBytes(t *testing.T) {
	const limit = 1024

	newHandler := func(captured *error, readN *int) http.Handler {
		return MaxBytes(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, err := io.ReadAll(r.Body)
			*captured = err
			*readN = len(b)
			w.WriteHeader(http.StatusOK)
		}))
	}

	t.Run("body under the limit reads fully", func(t *testing.T) {
		var err error
		var n int
		r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(make([]byte, 512)))
		newHandler(&err, &n).ServeHTTP(httptest.NewRecorder(), r)
		if err != nil {
			t.Fatalf("unexpected read error under limit: %v", err)
		}
		if n != 512 {
			t.Fatalf("read %d bytes, want 512", n)
		}
	})

	t.Run("body over the limit is rejected mid-read", func(t *testing.T) {
		var err error
		var n int
		r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(make([]byte, limit+1)))
		newHandler(&err, &n).ServeHTTP(httptest.NewRecorder(), r)
		if err == nil {
			t.Fatal("expected an error reading an oversized body, got nil")
		}
	})

	t.Run("nil body passes through without panic", func(t *testing.T) {
		// The middleware must not wrap a nil Body; a handler that doesn't touch
		// the body should run normally.
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Body = nil
		MaxBytes(limit)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", w.Code)
		}
	})
}
