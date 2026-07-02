package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type LoonstrookHandler struct{ store *store.LoonstrookStore }

func NewLoonstrookHandler(s *store.LoonstrookStore) *LoonstrookHandler {
	return &LoonstrookHandler{store: s}
}

// List returns all loonstroken.
// @Summary List loonstroken
// @Description Returns all payslips for the user
// @Tags Loonstroken
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {array} model.Loonstrook
// @Failure 400 {string} string "userId verplicht"
// @Failure 500 {string} string "Internal Server Error"
// @Router /loonstroken [get]
func (h *LoonstrookHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId verplicht")
		return
	}
	list, err := h.store.List(r.Context(), userID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, list)
}

// LoonstrokenImportResponse represents the import result.
// Total counts every upserted row (inserted + updated); Inserted is the
// genuinely-new count and Updated is the number of overwritten existing rows.
// The frontend derives "bijgewerkt" from Updated (or Total - Inserted).
type LoonstrokenImportResponse struct {
	OK       bool `json:"ok"`
	Inserted int  `json:"inserted"`
	Updated  int  `json:"updated"`
	Total    int  `json:"total"`
}

// LoonstrokenImportRequest represents the import payload
type LoonstrokenImportRequest struct {
	UserID string           `json:"userId"`
	Items  []map[string]any `json:"items"`
}

// Import bulk upserts loonstroken.
// @Summary Import loonstroken
// @Description Bulk upserts payslip data extracted from PDFs
// @Tags Loonstroken
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body LoonstrokenImportRequest true "Import payload containing UserID and Items"
// @Success 200 {object} LoonstrokenImportResponse
// @Failure 400 {string} string "Ongeldige JSON of ontbrekende velden"
// @Failure 500 {string} string "Internal Server Error"
// @Router /loonstroken/import [post]
func (h *LoonstrookHandler) Import(w http.ResponseWriter, r *http.Request) {
	var body LoonstrokenImportRequest
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if body.UserID == "" || len(body.Items) == 0 {
		Error(w, http.StatusBadRequest, "userId en items verplicht")
		return
	}
	res, err := h.store.ImportBatch(r.Context(), body.UserID, body.Items)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	// total = every upserted row so re-uploads are truthfully reported; the
	// frontend's "bijgewerkt = total - inserted" now equals res.Updated.
	JSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"inserted": res.Inserted,
		"updated":  res.Updated,
		"total":    res.Inserted + res.Updated,
	})
}
