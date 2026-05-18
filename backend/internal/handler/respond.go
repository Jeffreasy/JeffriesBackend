package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

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
	JSON(w, status, map[string]string{"detail": message})
}

// DecodeJSON reads and decodes the request body into v.
func DecodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
