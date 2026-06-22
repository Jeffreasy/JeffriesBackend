package middleware

import "net/http"

// DefaultMaxRequestBytes is a generous global ceiling on request body size.
// It sits well above any legitimate payload here (JSON is KBs; the largest
// uploads — voice audio for Whisper, scanned PDF dossiers, CSV imports — are a
// few MB), so it never trips normal use; it only stops a client from streaming
// unbounded gigabytes into a handler. A route that genuinely needs more can wrap
// its own http.MaxBytesReader with a higher limit.
const DefaultMaxRequestBytes int64 = 50 << 20 // 50 MiB

// MaxBytes caps each request body to n bytes. When the limit is exceeded during
// a Read, http.MaxBytesReader makes the handler's decode fail and the server
// responds 413, so oversized bodies are rejected instead of being buffered whole.
func MaxBytes(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}
