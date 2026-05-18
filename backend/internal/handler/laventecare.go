package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

// LaventeCareHandler handles LaventeCare CRM endpoints.
type LaventeCareHandler struct {
	store  *store.LaventeCareStore
	userID string
}

// NewLaventeCareHandler creates a new LaventeCareHandler.
func NewLaventeCareHandler(s *store.LaventeCareStore, userID string) *LaventeCareHandler {
	return &LaventeCareHandler{store: s, userID: userID}
}

// Cockpit returns the aggregated LaventeCare dashboard.
// @Summary Get LaventeCare Cockpit
// @Description Returns the aggregated CRM dashboard data
// @Tags LaventeCare
// @Produce json
// @Success 200 {object} model.LCCockpit
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/cockpit [get]
func (h *LaventeCareHandler) Cockpit(w http.ResponseWriter, r *http.Request) {
	cockpit, err := h.store.GetCockpit(r.Context(), h.userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, cockpit)
}

// ListLeads returns all active leads.
// @Summary List Leads
// @Description Returns all active CRM leads
// @Tags LaventeCare
// @Produce json
// @Param limit query int false "Limit count" default(30)
// @Success 200 {array} model.LCLead
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/leads [get]
func (h *LaventeCareHandler) ListLeads(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 30)
	leads, err := h.store.ListLeads(r.Context(), h.userID, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, leads)
}

// CreateLead creates a new lead.
// @Summary Create Lead
// @Description Creates a new CRM lead
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCLeadCreate true "Lead Details"
// @Success 201 {object} model.LCLead
// @Failure 400 {string} string "Invalid request body or missing title"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/leads [post]
func (h *LaventeCareHandler) CreateLead(w http.ResponseWriter, r *http.Request) {
	var input model.LCLeadCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Titel == "" {
		Error(w, http.StatusBadRequest, "Titel is verplicht")
		return
	}

	lead, err := h.store.CreateLead(r.Context(), h.userID, input)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, lead)
}

// UpdateLead modifies lead fields.
// @Summary Update Lead
// @Description Modifies an existing CRM lead
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Lead ID (UUID)"
// @Param request body model.LCLeadUpdate true "Updated Lead Details"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Lead not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/leads/{id} [patch]
func (h *LaventeCareHandler) UpdateLead(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid lead ID")
		return
	}

	var input model.LCLeadUpdate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.store.UpdateLead(r.Context(), h.userID, id, input); err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Lead niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ConvertLeadToProject converts a lead to a project.
// @Summary Convert Lead to Project
// @Description Converts an existing CRM lead into a project
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Lead ID (UUID)"
// @Param request body model.LCConvertLeadToProject true "Conversion Details"
// @Success 201 {object} model.LCProject
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Lead not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/leads/{id}/convert [post]
func (h *LaventeCareHandler) ConvertLeadToProject(w http.ResponseWriter, r *http.Request) {
	leadID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid lead ID")
		return
	}

	var input model.LCConvertLeadToProject
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	input.LeadID = leadID

	project, err := h.store.ConvertLeadToProject(r.Context(), h.userID, input)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Lead niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, project)
}

// ListProjects returns all projects.
// @Summary List Projects
// @Description Returns all CRM projects
// @Tags LaventeCare
// @Produce json
// @Param limit query int false "Limit count" default(30)
// @Success 200 {array} model.LCProject
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/projects [get]
func (h *LaventeCareHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 30)
	projects, err := h.store.ListProjects(r.Context(), h.userID, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, projects)
}

// UpdateProject modifies project fields.
// @Summary Update Project
// @Description Modifies an existing CRM project
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Project ID (UUID)"
// @Param request body model.LCProjectUpdate true "Updated Project Details"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Project not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/projects/{id} [patch]
func (h *LaventeCareHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid project ID")
		return
	}

	var input model.LCProjectUpdate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.store.UpdateProject(r.Context(), h.userID, id, input); err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Project niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListActions returns open action items.
// @Summary List Action Items
// @Description Returns all open CRM action items
// @Tags LaventeCare
// @Produce json
// @Param limit query int false "Limit count" default(8)
// @Success 200 {array} model.LCActionItem
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/actions [get]
func (h *LaventeCareHandler) ListActions(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 8)
	actions, err := h.store.ListActions(r.Context(), h.userID, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, actions)
}

// CreateAction creates a new action item.
// @Summary Create Action Item
// @Description Creates a new CRM action item
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCActionCreate true "Action Details"
// @Success 201 {object} model.LCActionItem
// @Failure 400 {string} string "Invalid request body or missing title"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/actions [post]
func (h *LaventeCareHandler) CreateAction(w http.ResponseWriter, r *http.Request) {
	var input model.LCActionCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Title == "" {
		Error(w, http.StatusBadRequest, "Title is verplicht")
		return
	}

	action, err := h.store.CreateAction(r.Context(), h.userID, input)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, action)
}

// UpdateActionStatus changes an action item's status.
// @Summary Update Action Status
// @Description Changes the status of an action item
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Action ID (UUID)"
// @Param request body map[string]string true "Status Details (e.g. {status: 'done'})"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Action not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/actions/{id}/status [patch]
func (h *LaventeCareHandler) UpdateActionStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid action ID")
		return
	}

	var input struct {
		Status string `json:"status"`
	}
	if err := DecodeJSON(r, &input); err != nil || input.Status == "" {
		Error(w, http.StatusBadRequest, "Status is verplicht")
		return
	}

	if err := h.store.UpdateActionStatus(r.Context(), h.userID, id, input.Status); err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Actie niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListDocuments returns the document catalog.
// @Summary List Documents
// @Description Returns all CRM documents in the catalog
// @Tags LaventeCare
// @Produce json
// @Success 200 {array} model.LCDocument
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/documents [get]
func (h *LaventeCareHandler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := h.store.ListDocuments(r.Context(), h.userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, docs)
}

// ConvertSignalToLead creates a lead from a business signal.
// @Summary Convert Signal to Lead
// @Description Converts a business signal into a CRM lead
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCConvertSignalToLead true "Signal Details"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {string} string "Invalid request body"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/signals/convert-lead [post]
func (h *LaventeCareHandler) ConvertSignalToLead(w http.ResponseWriter, r *http.Request) {
	var input model.LCConvertSignalToLead
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	pijnpunt := input.Subtitle + "\n" + input.ActionHint
	prioriteit := input.Urgency

	lead, err := h.store.CreateLead(r.Context(), h.userID, model.LCLeadCreate{
		Titel:      input.Title,
		Bron:       input.Source,
		SourceID:   &input.SourceID,
		Pijnpunt:   &pijnpunt,
		Prioriteit: &prioriteit,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, map[string]any{
		"lead":  lead,
		"reused": false,
	})
}

// SeedDocuments upserts the full LaventeCare knowledge document catalog.
// @Summary Seed Documents
// @Description Bulk upserts knowledge documents into the catalog
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body []model.LCDocument true "Documents"
// @Success 200 {object} model.LCSeedResult
// @Failure 400 {string} string "Invalid request body or empty"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/documents/seed [post]
func (h *LaventeCareHandler) SeedDocuments(w http.ResponseWriter, r *http.Request) {
	var docs []model.LCDocument
	if err := DecodeJSON(r, &docs); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if len(docs) == 0 {
		Error(w, http.StatusBadRequest, "Geen documenten ontvangen")
		return
	}

	inserted, updated, err := h.store.SeedDocuments(r.Context(), h.userID, docs)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, model.LCSeedResult{
		Total:    len(docs),
		Inserted: inserted,
		Updated:  updated,
	})
}
