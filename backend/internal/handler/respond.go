package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// internalErrorDetail is the fixed Dutch message every 500 returns — the real
// error never leaves the server (it is logged with the request ID instead).
const internalErrorDetail = "Er ging iets mis aan de serverkant. Probeer het opnieuw."

// JSON writes a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			slog.Error("json encode failed", "error", err)
		}
	}
}

// Error writes a JSON error response.
func Error(w http.ResponseWriter, status int, message string) {
	if status == http.StatusInternalServerError {
		// Safety net: even a direct 500 through Error never leaks raw error text.
		slog.Error("Internal server error", "raw_error", message)
		message = internalErrorDetail
	}
	JSON(w, status, map[string]string{"detail": message})
}

// InternalError logs the real error server-side (with the chi request ID so a
// user report can be matched to the log line) and responds with a fixed Dutch
// 500 — raw pgx/SQL/API error text never reaches the client.
func InternalError(w http.ResponseWriter, r *http.Request, err error) {
	args := []any{"error", err, "method", r.Method, "path", r.URL.Path}
	if reqID := chimiddleware.GetReqID(r.Context()); reqID != "" {
		args = append(args, "request_id", reqID)
	}
	slog.Error("internal server error", args...)
	JSON(w, http.StatusInternalServerError, map[string]string{"detail": internalErrorDetail})
}

// DecodeJSON reads and decodes the request body into v.
func DecodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// RespondDecodeError writes the right status for a request-body decode failure:
// 413 when the global request-size cap was hit (http.MaxBytesReader), otherwise
// a Dutch 400 — so an oversized upload no longer reports itself as "invalid JSON".
func RespondDecodeError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		Error(w, http.StatusRequestEntityTooLarge, "Verzoek is te groot (max 50 MB).")
		return
	}
	Error(w, http.StatusBadRequest, "Ongeldige aanvraag (JSON kon niet worden gelezen).")
}
