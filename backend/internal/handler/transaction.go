package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type TransactionHandler struct {
	store       *store.TransactionStore
	ownerUserID string
}

func NewTransactionHandler(s *store.TransactionStore, ownerUserID string) *TransactionHandler {
	return &TransactionHandler{store: s, ownerUserID: ownerUserID}
}

func validateTransactionDateRange(year, from, to string) string {
	if year != "" {
		if len(year) != 4 {
			return "Ongeldig jaarFilter (verwacht YYYY)."
		}
		for _, r := range year {
			if r < '0' || r > '9' {
				return "Ongeldig jaarFilter (verwacht YYYY)."
			}
		}
	}
	for name, value := range map[string]string{"datumVan": from, "datumTot": to} {
		if value == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return "Ongeldige " + name + " (verwacht YYYY-MM-DD)."
		}
	}
	if from != "" && to != "" && from > to {
		return "datumVan mag niet na datumTot liggen."
	}
	return ""
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
	// This is a single-owner service. Never let a caller select a tenant through
	// the query string, even though userId remains accepted for old clients.
	userID := h.ownerUserID
	q := r.URL.Query()
	if message := validateTransactionDateRange(q.Get("jaarFilter"), q.Get("datumVan"), q.Get("datumTot")); message != "" {
		Error(w, http.StatusBadRequest, message)
		return
	}
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
		InternalError(w, r, err)
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
		RespondDecodeError(w, err)
		return
	}
	if len(body.Transactions) == 0 {
		Error(w, http.StatusBadRequest, "transactions verplicht")
		return
	}

	// Ignore the legacy body.userId and always import into the configured owner.
	inserted, err := h.store.ImportBatch(r.Context(), h.ownerUserID, body.Transactions)
	if err != nil {
		if errors.Is(err, store.ErrInvalidTransactionImport) {
			Error(w, http.StatusBadRequest, err.Error())
			return
		}
		InternalError(w, r, err)
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
	// Scope the mutation to the configured owner; query parameters are untrusted.
	userID := h.ownerUserID
	var body struct {
		Categorie string `json:"categorie"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	rows, err := h.store.UpdateCategorie(r.Context(), userID, id, body.Categorie)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if rows == 0 {
		Error(w, http.StatusNotFound, "Transactie niet gevonden")
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
	userID := h.ownerUserID
	if message := validateTransactionDateRange(r.URL.Query().Get("jaarFilter"), r.URL.Query().Get("datumVan"), r.URL.Query().Get("datumTot")); message != "" {
		Error(w, http.StatusBadRequest, message)
		return
	}

	var ibanFilter, jaarFilter, datumVan, datumTot *string
	if v := r.URL.Query().Get("ibanFilter"); v != "" {
		ibanFilter = &v
	}
	if v := r.URL.Query().Get("jaarFilter"); v != "" {
		jaarFilter = &v
	}
	// Optional period bounds (YYYY-MM-DD) — same params the transaction list
	// uses, so "huidige selectie" in the stats copy is finally true.
	if v := r.URL.Query().Get("datumVan"); v != "" {
		if _, err := time.Parse("2006-01-02", v); err != nil {
			Error(w, http.StatusBadRequest, "Ongeldige datumVan (verwacht YYYY-MM-DD).")
			return
		}
		datumVan = &v
	}
	if v := r.URL.Query().Get("datumTot"); v != "" {
		if _, err := time.Parse("2006-01-02", v); err != nil {
			Error(w, http.StatusBadRequest, "Ongeldige datumTot (verwacht YYYY-MM-DD).")
			return
		}
		datumTot = &v
	}

	stats, err := h.store.GetFullStats(r.Context(), userID, ibanFilter, jaarFilter, datumVan, datumTot)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, stats)
}
