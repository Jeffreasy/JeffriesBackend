package model

import (
	"time"

	"github.com/google/uuid"
)

// ─── LaventeCare CRM ────────────────────────────────────────────────────────

type LCLead struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	UserID           string     `json:"user_id" db:"user_id"`
	CompanyID        *uuid.UUID `json:"company_id" db:"company_id"`
	ContactID        *uuid.UUID `json:"contact_id" db:"contact_id"`
	Titel            string     `json:"titel" db:"titel"`
	Bron             string     `json:"bron" db:"bron"`
	SourceID         *string    `json:"source_id" db:"source_id"`
	Status           string     `json:"status" db:"status"`
	FitScore         *int       `json:"fit_score" db:"fit_score"`
	Pijnpunt         *string    `json:"pijnpunt" db:"pijnpunt"`
	Prioriteit       *string    `json:"prioriteit" db:"prioriteit"`
	VolgendeStap     *string    `json:"volgende_stap" db:"volgende_stap"`
	VolgendeActieDatum *string  `json:"volgende_actie_datum" db:"volgende_actie_datum"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

type LCLeadCreate struct {
	Titel            string  `json:"titel"`
	CompanyName      *string `json:"company_name"`
	Website          *string `json:"website"`
	Bron             string  `json:"bron"`
	SourceID         *string `json:"source_id"`
	Pijnpunt         *string `json:"pijnpunt"`
	Prioriteit       *string `json:"prioriteit"`
	FitScore         *int    `json:"fit_score"`
	VolgendeStap     *string `json:"volgende_stap"`
	VolgendeActieDatum *string `json:"volgende_actie_datum"`
}

type LCLeadUpdate struct {
	Status           *string `json:"status,omitempty"`
	FitScore         *int    `json:"fit_score,omitempty"`
	Pijnpunt         *string `json:"pijnpunt,omitempty"`
	Prioriteit       *string `json:"prioriteit,omitempty"`
	VolgendeStap     *string `json:"volgende_stap,omitempty"`
	VolgendeActieDatum *string `json:"volgende_actie_datum,omitempty"`
}

type LCProject struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	UserID          string     `json:"user_id" db:"user_id"`
	CompanyID       *uuid.UUID `json:"company_id" db:"company_id"`
	LeadID          *uuid.UUID `json:"lead_id" db:"lead_id"`
	Naam            string     `json:"naam" db:"naam"`
	Fase            string     `json:"fase" db:"fase"`
	Status          string     `json:"status" db:"status"`
	WaardeIndicatie *int       `json:"waarde_indicatie" db:"waarde_indicatie"`
	StartDatum      *string    `json:"start_datum" db:"start_datum"`
	Deadline        *string    `json:"deadline" db:"deadline"`
	Samenvatting    *string    `json:"samenvatting" db:"samenvatting"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

type LCProjectUpdate struct {
	Fase            *string `json:"fase,omitempty"`
	Status          *string `json:"status,omitempty"`
	WaardeIndicatie *int    `json:"waarde_indicatie,omitempty"`
	StartDatum      *string `json:"start_datum,omitempty"`
	Deadline        *string `json:"deadline,omitempty"`
	Samenvatting    *string `json:"samenvatting,omitempty"`
}

type LCActionItem struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	UserID          string     `json:"user_id" db:"user_id"`
	Source          string     `json:"source" db:"source"`
	SourceID        *string    `json:"source_id" db:"source_id"`
	Title           string     `json:"title" db:"title"`
	Summary         *string    `json:"summary" db:"summary"`
	ActionType      string     `json:"action_type" db:"action_type"`
	Status          string     `json:"status" db:"status"`
	Priority        string     `json:"priority" db:"priority"`
	DueDate         *string    `json:"due_date" db:"due_date"`
	LinkedLeadID    *uuid.UUID `json:"linked_lead_id" db:"linked_lead_id"`
	LinkedProjectID *uuid.UUID `json:"linked_project_id" db:"linked_project_id"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

type LCActionCreate struct {
	Source          string     `json:"source"`
	SourceID        *string    `json:"source_id"`
	Title           string     `json:"title"`
	Summary         *string    `json:"summary"`
	ActionType      string     `json:"action_type"`
	Priority        string     `json:"priority"`
	DueDate         *string    `json:"due_date"`
	LinkedLeadID    *uuid.UUID `json:"linked_lead_id"`
	LinkedProjectID *uuid.UUID `json:"linked_project_id"`
}

type LCDocument struct {
	ID          uuid.UUID `json:"id" db:"id"`
	UserID      string    `json:"user_id" db:"user_id"`
	DocumentKey string    `json:"document_key" db:"document_key"`
	Titel       string    `json:"titel" db:"titel"`
	Categorie   string    `json:"categorie" db:"categorie"`
	Fase        *string   `json:"fase" db:"fase"`
	Versie      string    `json:"versie" db:"versie"`
	SourcePath  *string   `json:"source_path" db:"source_path"`
	Samenvatting string   `json:"samenvatting" db:"samenvatting"`
	Tags        []string  `json:"tags" db:"tags"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type LCDecision struct {
	ID        uuid.UUID  `json:"id" db:"id"`
	UserID    string     `json:"user_id" db:"user_id"`
	ProjectID *uuid.UUID `json:"project_id" db:"project_id"`
	Titel     string     `json:"titel" db:"titel"`
	Besluit   string     `json:"besluit" db:"besluit"`
	Reden     string     `json:"reden" db:"reden"`
	Impact    *string    `json:"impact" db:"impact"`
	Status    string     `json:"status" db:"status"`
	Datum     string     `json:"datum" db:"datum"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

type LCChangeRequest struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	UserID         string     `json:"user_id" db:"user_id"`
	ProjectID      *uuid.UUID `json:"project_id" db:"project_id"`
	Titel          string     `json:"titel" db:"titel"`
	Impact         string     `json:"impact" db:"impact"`
	PlanningImpact *string    `json:"planning_impact" db:"planning_impact"`
	BudgetImpact   *string    `json:"budget_impact" db:"budget_impact"`
	Status         string     `json:"status" db:"status"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

type LCSlaIncident struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	UserID          string     `json:"user_id" db:"user_id"`
	ProjectID       *uuid.UUID `json:"project_id" db:"project_id"`
	Titel           string     `json:"titel" db:"titel"`
	Prioriteit      string     `json:"prioriteit" db:"prioriteit"`
	Status          string     `json:"status" db:"status"`
	Kanaal          string     `json:"kanaal" db:"kanaal"`
	GemeldOp        time.Time  `json:"gemeld_op" db:"gemeld_op"`
	ReactieDeadline *time.Time `json:"reactie_deadline" db:"reactie_deadline"`
	Samenvatting    *string    `json:"samenvatting" db:"samenvatting"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

// LCCockpit is the aggregated dashboard response.
type LCCockpit struct {
	Summary         LCCockpitSummary   `json:"summary"`
	ActiveLeads     []LCLead           `json:"activeLeads"`
	ActiveProjects  []LCProject        `json:"activeProjects"`
	ActionItems     []LCActionItem     `json:"actionItems"`
	OpenIncidents   []LCSlaIncident    `json:"openIncidents"`
	OpenChanges     []LCChangeRequest  `json:"openChanges"`
	RecentDecisions []LCDecision       `json:"recentDecisions"`
	DocumentCatalog []LCDocument       `json:"documentCatalog"`
	BusinessSignals []LCBusinessSignal `json:"businessSignals"`
	FollowUps       []LCFollowUpSignal `json:"followUps"`
}

type LCCockpitSummary struct {
	Leads           int  `json:"leads"`
	ActiveLeads     int  `json:"activeLeads"`
	Projects        int  `json:"projects"`
	ActiveProjects  int  `json:"activeProjects"`
	Documents       int  `json:"documents"`
	OpenIncidents   int  `json:"openIncidents"`
	OpenChanges     int  `json:"openChanges"`
	Decisions       int  `json:"decisions"`
	ActionItems     int  `json:"actionItems"`
	DocumentsSeeded bool `json:"documentsSeeded"`
	BusinessSignals int  `json:"businessSignals"`
	FollowUps       int  `json:"followUps"`
}

// LCConvertLeadToProject is the request body for converting a lead to a project.
type LCConvertLeadToProject struct {
	LeadID       uuid.UUID `json:"lead_id"`
	Naam         string    `json:"naam"`
	Fase         *string   `json:"fase"`
	Status       *string   `json:"status"`
	Samenvatting *string   `json:"samenvatting"`
}

// LCConvertSignalToLead is the request body for converting a business signal to a lead.
type LCConvertSignalToLead struct {
	Source      string `json:"source"`
	SourceID    string `json:"source_id"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	Date        string `json:"date"`
	MatchedTerm string `json:"matched_term"`
	Urgency     string `json:"urgency"`
	ActionHint  string `json:"action_hint"`
}

// LCBusinessSignal is a detected business-relevant signal from emails/events/notes.
type LCBusinessSignal struct {
	Source      string `json:"source"`
	ID          string `json:"id"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	Date        string `json:"date"`
	MatchedTerm string `json:"matched_term"`
	Urgency     string `json:"urgency"`
	ActionHint  string `json:"action_hint"`
}

// LCFollowUpSignal is a lead/project that needs follow-up action.
type LCFollowUpSignal struct {
	Source     string `json:"source"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	Date       string `json:"date"`
	Status     string `json:"status"`
	Priority   string `json:"priority"`
	ActionHint string `json:"action_hint"`
}

// LCSeedResult is the response from the document seed endpoint.
type LCSeedResult struct {
	Total    int `json:"total"`
	Inserted int `json:"inserted"`
	Updated  int `json:"updated"`
}
