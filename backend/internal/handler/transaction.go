package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type TransactionHandler struct {
	store *store.TransactionStore
}

func NewTransactionHandler(s *store.TransactionStore) *TransactionHandler {
	return &TransactionHandler{store: s}
}

// TransactionListResponse represents the paginated response for transactions
type TransactionListResponse struct {
	Page       []model.Transaction `json:"page"`
	TotalCount int                 `json:"totalCount"`
	IsDone     bool                `json:"isDone"`
}

// List returns transactions with full filter support.
// @Summary List transactions
// @Description Returns transactions for a user with extensive filtering options
// @Tags Transactions
// @Produce json
// @Param userId query string true "User ID"
// @Param excludeIntern query boolean false "Exclude internal transactions"
// @Param onlyStorneringen query boolean false "Only show reversed transactions"
// @Param codeFilter query string false "Transaction code"
// @Param ibanFilter query string false "IBAN filter"
// @Param categorieFilter query string false "Category filter"
// @Param richting query string false "Direction (in/uit)"
// @Param datumVan query string false "Start date (YYYY-MM-DD)"
// @Param datumTot query string false "End date (YYYY-MM-DD)"
// @Param zoekterm query string false "Search query"
// @Param minBedrag query number false "Minimum amount"
// @Param maxBedrag query number false "Maximum amount"
// @Param jaarFilter query string false "Year filter"
// @Param limit query int false "Limit count" default(50)
// @Param offset query int false "Offset count" default(0)
// @Success 200 {object} TransactionListResponse
// @Failure 400 {string} string "userId verplicht"
// @Failure 500 {string} string "Internal Server Error"
// @Router /transactions [get]
func (h *TransactionHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId verplicht")
		return
	}

	q := r.URL.Query()
	f := store.TransactionFilter{
		ExcludeIntern:    q.Get("excludeIntern") == "true",
		OnlyStorneringen: q.Get("onlyStorneringen") == "true",
		Code:             q.Get("codeFilter"),
		Iban:             q.Get("ibanFilter"),
		Categorie:        q.Get("categorieFilter"),
		Richting:         q.Get("richting"),
		DatumVan:         q.Get("datumVan"),
		DatumTot:         q.Get("datumTot"),
		Zoekterm:         q.Get("zoekterm"),
	}
	if v := q.Get("minBedrag"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			f.MinBedrag = &n
		}
	}
	if v := q.Get("maxBedrag"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			f.MaxBedrag = &n
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Offset = n
		}
	}

	// Handle jaarFilter shortcut
	if j := q.Get("jaarFilter"); j != "" {
		if f.DatumVan == "" {
			f.DatumVan = j + "-01-01"
		}
		if f.DatumTot == "" {
			f.DatumTot = j + "-12-31"
		}
	}

	txs, totalCount, err := h.store.ListFiltered(r.Context(), userID, f)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"page":       txs,
		"totalCount": totalCount,
		"isDone":     f.Offset+len(txs) >= totalCount,
	})
}

// TransactionImportResponse represents the import result
type TransactionImportResponse struct {
	OK       bool `json:"ok"`
	Inserted int  `json:"inserted"`
	Total    int  `json:"total"`
	Skipped  int  `json:"skipped"`
}

// Import inserts a batch of transactions (CSV parse result from frontend).
// @Summary Import transactions
// @Description Bulk imports parsed transactions from CSV
// @Tags Transactions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body map[string]interface{} true "Import Details"
// @Success 200 {object} TransactionImportResponse
// @Failure 400 {string} string "Ongeldige JSON of ontbrekende velden"
// @Failure 500 {string} string "Internal Server Error"
// @Router /transactions/import [post]
func (h *TransactionHandler) Import(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID       string                    `json:"userId"`
		Transactions []model.TransactionImport `json:"transactions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "Ongeldige JSON")
		return
	}
	if body.UserID == "" || len(body.Transactions) == 0 {
		Error(w, http.StatusBadRequest, "userId en transactions verplicht")
		return
	}

	inserted, err := h.store.ImportBatch(r.Context(), body.UserID, body.Transactions)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"inserted": inserted,
		"total":    len(body.Transactions),
		"skipped":  len(body.Transactions) - inserted,
	})
}

// UpdateCategorie changes the category of a single transaction.
// @Summary Update transaction category
// @Description Updates the category of a specific transaction
// @Tags Transactions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param txID path string true "Transaction ID (UUID)"
// @Param request body map[string]string true "Category Details"
// @Success 200 {object} map[string]bool "ok: true"
// @Failure 400 {string} string "Ongeldige JSON of ID"
// @Failure 500 {string} string "Internal Server Error"
// @Router /transactions/{txID} [patch]
func (h *TransactionHandler) UpdateCategorie(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "txID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig transaction ID")
		return
	}
	var body struct {
		Categorie string `json:"categorie"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "Ongeldige JSON")
		return
	}
	if err := h.store.UpdateCategorie(r.Context(), id, body.Categorie); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Stats returns aggregate transaction stats (full dashboard data).
// @Summary Get transaction statistics
// @Description Returns aggregate statistics for the dashboard
// @Tags Transactions
// @Produce json
// @Param userId query string true "User ID"
// @Param ibanFilter query string false "IBAN filter"
// @Param jaarFilter query string false "Year filter"
// @Success 200 {object} store.TransactionStats
// @Failure 400 {string} string "userId verplicht"
// @Failure 500 {string} string "Internal Server Error"
// @Router /transactions/stats [get]
func (h *TransactionHandler) Stats(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId verplicht")
		return
	}

	var ibanFilter, jaarFilter *string
	if v := r.URL.Query().Get("ibanFilter"); v != "" {
		ibanFilter = &v
	}
	if v := r.URL.Query().Get("jaarFilter"); v != "" {
		jaarFilter = &v
	}

	stats, err := h.store.GetFullStats(r.Context(), userID, ibanFilter, jaarFilter)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, stats)
}
