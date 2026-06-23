package model

import (
	"time"

	"github.com/google/uuid"
)

// ─── LaventeCare CRM ────────────────────────────────────────────────────────

type LCCompany struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	UserID            string     `json:"user_id" db:"user_id"`
	Naam              string     `json:"naam" db:"naam"`
	Website           *string    `json:"website" db:"website"`
	Sector            *string    `json:"sector" db:"sector"`
	Status            string     `json:"status" db:"status"`
	RelatieType       string     `json:"relatie_type" db:"relatie_type"`
	Notities          *string    `json:"notities" db:"notities"`
	LaatsteContact    *time.Time `json:"laatste_contact" db:"laatste_contact"`
	VolgendeActie     *string    `json:"volgende_actie" db:"volgende_actie"`
	KVKNumber         *string    `json:"kvk_number" db:"kvk_number"`
	VATNumber         *string    `json:"vat_number" db:"vat_number"`
	BillingEmail      *string    `json:"billing_email" db:"billing_email"`
	BillingAddress    *string    `json:"billing_address" db:"billing_address"`
	BillingReference  *string    `json:"billing_reference" db:"billing_reference"`
	PaymentTermsDays  int        `json:"payment_terms_days" db:"payment_terms_days"`
	ContractStatus    string     `json:"contract_status" db:"contract_status"`
	ServiceLevel      string     `json:"service_level" db:"service_level"`
	PreferredChannel  *string    `json:"preferred_channel" db:"preferred_channel"`
	PortalURL         *string    `json:"portal_url" db:"portal_url"`
	DefaultLoginURL   *string    `json:"default_login_url" db:"default_login_url"`
	OnboardingStatus  string     `json:"onboarding_status" db:"onboarding_status"`
	DataProcessStatus string     `json:"data_processing_status" db:"data_processing_status"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`
	Contacts          int        `json:"contacts"`
	Leads             int        `json:"leads"`
	Workstreams       int        `json:"workstreams"`
	Projects          int        `json:"projects"`
	ActionItems       int        `json:"actionItems"`
	DossierDocuments  int        `json:"dossierDocuments"`
}

type LCCompanyCreate struct {
	Naam              string  `json:"naam"`
	Website           *string `json:"website"`
	Sector            *string `json:"sector"`
	Status            string  `json:"status"`
	RelatieType       string  `json:"relatie_type"`
	Notities          *string `json:"notities"`
	LaatsteContact    *string `json:"laatste_contact"`
	VolgendeActie     *string `json:"volgende_actie"`
	KVKNumber         *string `json:"kvk_number"`
	VATNumber         *string `json:"vat_number"`
	BillingEmail      *string `json:"billing_email"`
	BillingAddress    *string `json:"billing_address"`
	BillingReference  *string `json:"billing_reference"`
	PaymentTermsDays  *int    `json:"payment_terms_days"`
	ContractStatus    *string `json:"contract_status"`
	ServiceLevel      *string `json:"service_level"`
	PreferredChannel  *string `json:"preferred_channel"`
	PortalURL         *string `json:"portal_url"`
	DefaultLoginURL   *string `json:"default_login_url"`
	OnboardingStatus  *string `json:"onboarding_status"`
	DataProcessStatus *string `json:"data_processing_status"`
}

type LCCompanyUpdate struct {
	Naam              *string `json:"naam,omitempty"`
	Website           *string `json:"website,omitempty"`
	Sector            *string `json:"sector,omitempty"`
	Status            *string `json:"status,omitempty"`
	RelatieType       *string `json:"relatie_type,omitempty"`
	Notities          *string `json:"notities,omitempty"`
	LaatsteContact    *string `json:"laatste_contact,omitempty"`
	VolgendeActie     *string `json:"volgende_actie,omitempty"`
	KVKNumber         *string `json:"kvk_number,omitempty"`
	VATNumber         *string `json:"vat_number,omitempty"`
	BillingEmail      *string `json:"billing_email,omitempty"`
	BillingAddress    *string `json:"billing_address,omitempty"`
	BillingReference  *string `json:"billing_reference,omitempty"`
	PaymentTermsDays  *int    `json:"payment_terms_days,omitempty"`
	ContractStatus    *string `json:"contract_status,omitempty"`
	ServiceLevel      *string `json:"service_level,omitempty"`
	PreferredChannel  *string `json:"preferred_channel,omitempty"`
	PortalURL         *string `json:"portal_url,omitempty"`
	DefaultLoginURL   *string `json:"default_login_url,omitempty"`
	OnboardingStatus  *string `json:"onboarding_status,omitempty"`
	DataProcessStatus *string `json:"data_processing_status,omitempty"`
}

type LCContact struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	UserID           string     `json:"user_id" db:"user_id"`
	CompanyID        *uuid.UUID `json:"company_id" db:"company_id"`
	Naam             string     `json:"naam" db:"naam"`
	Email            *string    `json:"email" db:"email"`
	Telefoon         *string    `json:"telefoon" db:"telefoon"`
	Rol              *string    `json:"rol" db:"rol"`
	IsPrimary        bool       `json:"is_primary" db:"is_primary"`
	Notities         *string    `json:"notities" db:"notities"`
	PreferredChannel *string    `json:"preferred_channel" db:"preferred_channel"`
	DecisionRole     *string    `json:"decision_role" db:"decision_role"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

type LCContactCreate struct {
	CompanyID        *uuid.UUID `json:"company_id"`
	Naam             string     `json:"naam"`
	Email            *string    `json:"email"`
	Telefoon         *string    `json:"telefoon"`
	Rol              *string    `json:"rol"`
	IsPrimary        bool       `json:"is_primary"`
	Notities         *string    `json:"notities"`
	PreferredChannel *string    `json:"preferred_channel"`
	DecisionRole     *string    `json:"decision_role"`
}

type LCContactUpdate struct {
	CompanyID        *uuid.UUID `json:"company_id,omitempty"`
	Naam             *string    `json:"naam,omitempty"`
	Email            *string    `json:"email,omitempty"`
	Telefoon         *string    `json:"telefoon,omitempty"`
	Rol              *string    `json:"rol,omitempty"`
	IsPrimary        *bool      `json:"is_primary,omitempty"`
	Notities         *string    `json:"notities,omitempty"`
	PreferredChannel *string    `json:"preferred_channel,omitempty"`
	DecisionRole     *string    `json:"decision_role,omitempty"`
}

type LCAccessCredential struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	UserID           string     `json:"user_id" db:"user_id"`
	CompanyID        uuid.UUID  `json:"company_id" db:"company_id"`
	ContactID        *uuid.UUID `json:"contact_id" db:"contact_id"`
	ProjectID        *uuid.UUID `json:"project_id" db:"project_id"`
	WorkstreamID     *uuid.UUID `json:"workstream_id" db:"workstream_id"`
	Title            string     `json:"title" db:"title"`
	LoginURL         *string    `json:"login_url" db:"login_url"`
	Username         *string    `json:"username" db:"username"`
	Role             *string    `json:"role" db:"role"`
	Environment      string     `json:"environment" db:"environment"`
	Status           string     `json:"status" db:"status"`
	OwnerContact     *string    `json:"owner_contact" db:"owner_contact"`
	SecretLabel      string     `json:"secret_label" db:"secret_label"`
	SecretConfigured bool       `json:"secret_configured"`
	SecretHint       *string    `json:"secret_hint" db:"secret_hint"`
	SharingPolicy    string     `json:"sharing_policy" db:"sharing_policy"`
	LastCheckedAt    *time.Time `json:"last_checked_at" db:"last_checked_at"`
	ExpiresAt        *time.Time `json:"expires_at" db:"expires_at"`
	RevokedAt        *time.Time `json:"revoked_at" db:"revoked_at"`
	Notes            *string    `json:"notes" db:"notes"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
	CompanyName      *string    `json:"company_name,omitempty"`
	ContactName      *string    `json:"contact_name,omitempty"`
	ProjectName      *string    `json:"project_name,omitempty"`
	WorkstreamTitle  *string    `json:"workstream_title,omitempty"`
}

type LCAccessCredentialCreate struct {
	CompanyID     uuid.UUID  `json:"company_id"`
	ContactID     *uuid.UUID `json:"contact_id"`
	ProjectID     *uuid.UUID `json:"project_id"`
	WorkstreamID  *uuid.UUID `json:"workstream_id"`
	Title         string     `json:"title"`
	LoginURL      *string    `json:"login_url"`
	Username      *string    `json:"username"`
	Role          *string    `json:"role"`
	Environment   string     `json:"environment"`
	Status        string     `json:"status"`
	OwnerContact  *string    `json:"owner_contact"`
	SecretLabel   *string    `json:"secret_label"`
	SecretValue   *string    `json:"secret_value"`
	SecretHint    *string    `json:"secret_hint"`
	SharingPolicy *string    `json:"sharing_policy"`
	LastCheckedAt *string    `json:"last_checked_at"`
	ExpiresAt     *string    `json:"expires_at"`
	Notes         *string    `json:"notes"`
}

type LCAccessCredentialUpdate struct {
	ContactID     *uuid.UUID `json:"contact_id,omitempty"`
	ProjectID     *uuid.UUID `json:"project_id,omitempty"`
	WorkstreamID  *uuid.UUID `json:"workstream_id,omitempty"`
	Title         *string    `json:"title,omitempty"`
	LoginURL      *string    `json:"login_url,omitempty"`
	Username      *string    `json:"username,omitempty"`
	Role          *string    `json:"role,omitempty"`
	Environment   *string    `json:"environment,omitempty"`
	Status        *string    `json:"status,omitempty"`
	OwnerContact  *string    `json:"owner_contact,omitempty"`
	SecretLabel   *string    `json:"secret_label,omitempty"`
	SecretValue   *string    `json:"secret_value,omitempty"`
	SecretHint    *string    `json:"secret_hint,omitempty"`
	SharingPolicy *string    `json:"sharing_policy,omitempty"`
	LastCheckedAt *string    `json:"last_checked_at,omitempty"`
	ExpiresAt     *string    `json:"expires_at,omitempty"`
	RevokedAt     *string    `json:"revoked_at,omitempty"`
	Notes         *string    `json:"notes,omitempty"`
}

type LCLead struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	UserID             string     `json:"user_id" db:"user_id"`
	CompanyID          *uuid.UUID `json:"company_id" db:"company_id"`
	ContactID          *uuid.UUID `json:"contact_id" db:"contact_id"`
	Titel              string     `json:"titel" db:"titel"`
	Bron               string     `json:"bron" db:"bron"`
	SourceID           *string    `json:"source_id" db:"source_id"`
	Status             string     `json:"status" db:"status"`
	FitScore           *int       `json:"fit_score" db:"fit_score"`
	Pijnpunt           *string    `json:"pijnpunt" db:"pijnpunt"`
	Prioriteit         *string    `json:"prioriteit" db:"prioriteit"`
	VolgendeStap       *string    `json:"volgende_stap" db:"volgende_stap"`
	VolgendeActieDatum *string    `json:"volgende_actie_datum" db:"volgende_actie_datum"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
}

type LCLeadCreate struct {
	Titel              string     `json:"titel"`
	CompanyID          *uuid.UUID `json:"company_id"`
	ContactID          *uuid.UUID `json:"contact_id"`
	CompanyName        *string    `json:"company_name"`
	Website            *string    `json:"website"`
	Bron               string     `json:"bron"`
	SourceID           *string    `json:"source_id"`
	Pijnpunt           *string    `json:"pijnpunt"`
	Prioriteit         *string    `json:"prioriteit"`
	FitScore           *int       `json:"fit_score"`
	VolgendeStap       *string    `json:"volgende_stap"`
	VolgendeActieDatum *string    `json:"volgende_actie_datum"`
}

type LCLeadUpdate struct {
	CompanyID          *uuid.UUID `json:"company_id,omitempty"`
	ContactID          *uuid.UUID `json:"contact_id,omitempty"`
	Status             *string    `json:"status,omitempty"`
	FitScore           *int       `json:"fit_score,omitempty"`
	Pijnpunt           *string    `json:"pijnpunt,omitempty"`
	Prioriteit         *string    `json:"prioriteit,omitempty"`
	VolgendeStap       *string    `json:"volgende_stap,omitempty"`
	VolgendeActieDatum *string    `json:"volgende_actie_datum,omitempty"`
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

type LCProjectCreate struct {
	Naam            string     `json:"naam"`
	CompanyID       *uuid.UUID `json:"company_id"`
	CompanyName     *string    `json:"company_name"`
	Website         *string    `json:"website"`
	Fase            string     `json:"fase"`
	Status          string     `json:"status"`
	WaardeIndicatie *int       `json:"waarde_indicatie"`
	StartDatum      *string    `json:"start_datum"`
	Deadline        *string    `json:"deadline"`
	Samenvatting    *string    `json:"samenvatting"`
}

type LCProjectUpdate struct {
	CompanyID       *uuid.UUID `json:"company_id,omitempty"`
	Fase            *string    `json:"fase,omitempty"`
	Status          *string    `json:"status,omitempty"`
	WaardeIndicatie *int       `json:"waarde_indicatie,omitempty"`
	StartDatum      *string    `json:"start_datum,omitempty"`
	Deadline        *string    `json:"deadline,omitempty"`
	Samenvatting    *string    `json:"samenvatting,omitempty"`
}

type LCWorkstream struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	UserID           string     `json:"user_id" db:"user_id"`
	CompanyID        *uuid.UUID `json:"company_id" db:"company_id"`
	LeadID           *uuid.UUID `json:"lead_id" db:"lead_id"`
	ProjectID        *uuid.UUID `json:"project_id" db:"project_id"`
	Titel            string     `json:"titel" db:"titel"`
	Type             string     `json:"type" db:"type"`
	Status           string     `json:"status" db:"status"`
	Prioriteit       string     `json:"prioriteit" db:"prioriteit"`
	KlantNaam        *string    `json:"klant_naam" db:"klant_naam"`
	Bron             string     `json:"bron" db:"bron"`
	SourceID         *string    `json:"source_id" db:"source_id"`
	Doel             *string    `json:"doel" db:"doel"`
	Scope            *string    `json:"scope" db:"scope"`
	Deliverable      *string    `json:"deliverable" db:"deliverable"`
	Bevindingen      *string    `json:"bevindingen" db:"bevindingen"`
	VolgendeStap     *string    `json:"volgende_stap" db:"volgende_stap"`
	Deadline         *string    `json:"deadline" db:"deadline"`
	GeschatteMinuten *int       `json:"geschatte_minuten" db:"geschatte_minuten"`
	WaardeIndicatie  *int       `json:"waarde_indicatie" db:"waarde_indicatie"`
	StackTags        []string   `json:"stack_tags" db:"stack_tags"`
	Tags             []string   `json:"tags" db:"tags"`
	CompletedAt      *time.Time `json:"completed_at" db:"completed_at"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

type LCWorkstreamCreate struct {
	Titel            string     `json:"titel"`
	CompanyID        *uuid.UUID `json:"company_id"`
	Type             string     `json:"type"`
	Status           string     `json:"status"`
	Prioriteit       string     `json:"prioriteit"`
	KlantNaam        *string    `json:"klant_naam"`
	Bron             string     `json:"bron"`
	SourceID         *string    `json:"source_id"`
	LeadID           *uuid.UUID `json:"lead_id"`
	ProjectID        *uuid.UUID `json:"project_id"`
	Doel             *string    `json:"doel"`
	Scope            *string    `json:"scope"`
	Deliverable      *string    `json:"deliverable"`
	Bevindingen      *string    `json:"bevindingen"`
	VolgendeStap     *string    `json:"volgende_stap"`
	Deadline         *string    `json:"deadline"`
	GeschatteMinuten *int       `json:"geschatte_minuten"`
	WaardeIndicatie  *int       `json:"waarde_indicatie"`
	StackTags        []string   `json:"stack_tags"`
	Tags             []string   `json:"tags"`
}

type LCWorkstreamUpdate struct {
	CompanyID        *uuid.UUID `json:"company_id,omitempty"`
	ProjectID        *uuid.UUID `json:"project_id,omitempty"`
	Type             *string    `json:"type,omitempty"`
	Status           *string    `json:"status,omitempty"`
	Prioriteit       *string    `json:"prioriteit,omitempty"`
	KlantNaam        *string    `json:"klant_naam,omitempty"`
	Doel             *string    `json:"doel,omitempty"`
	Scope            *string    `json:"scope,omitempty"`
	Deliverable      *string    `json:"deliverable,omitempty"`
	Bevindingen      *string    `json:"bevindingen,omitempty"`
	VolgendeStap     *string    `json:"volgende_stap,omitempty"`
	Deadline         *string    `json:"deadline,omitempty"`
	GeschatteMinuten *int       `json:"geschatte_minuten,omitempty"`
	WaardeIndicatie  *int       `json:"waarde_indicatie,omitempty"`
	StackTags        []string   `json:"stack_tags,omitempty"`
	Tags             []string   `json:"tags,omitempty"`
}

type LCActionItem struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	UserID             string     `json:"user_id" db:"user_id"`
	Source             string     `json:"source" db:"source"`
	SourceID           *string    `json:"source_id" db:"source_id"`
	Title              string     `json:"title" db:"title"`
	Summary            *string    `json:"summary" db:"summary"`
	ActionType         string     `json:"action_type" db:"action_type"`
	Status             string     `json:"status" db:"status"`
	Priority           string     `json:"priority" db:"priority"`
	DueDate            *string    `json:"due_date" db:"due_date"`
	LinkedLeadID       *uuid.UUID `json:"linked_lead_id" db:"linked_lead_id"`
	LinkedProjectID    *uuid.UUID `json:"linked_project_id" db:"linked_project_id"`
	LinkedWorkstreamID *uuid.UUID `json:"linked_workstream_id" db:"linked_workstream_id"`
	LinkedCompanyID    *uuid.UUID `json:"linked_company_id" db:"linked_company_id"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
}

type LCActionCreate struct {
	Source             string     `json:"source"`
	SourceID           *string    `json:"source_id"`
	Title              string     `json:"title"`
	Summary            *string    `json:"summary"`
	ActionType         string     `json:"action_type"`
	Priority           string     `json:"priority"`
	DueDate            *string    `json:"due_date"`
	LinkedLeadID       *uuid.UUID `json:"linked_lead_id"`
	LinkedProjectID    *uuid.UUID `json:"linked_project_id"`
	LinkedWorkstreamID *uuid.UUID `json:"linked_workstream_id"`
	LinkedCompanyID    *uuid.UUID `json:"linked_company_id"`
}

type LCDocument struct {
	ID           uuid.UUID `json:"id" db:"id"`
	UserID       string    `json:"user_id" db:"user_id"`
	DocumentKey  string    `json:"document_key" db:"document_key"`
	Titel        string    `json:"titel" db:"titel"`
	Categorie    string    `json:"categorie" db:"categorie"`
	Fase         *string   `json:"fase" db:"fase"`
	Versie       string    `json:"versie" db:"versie"`
	SourcePath   *string   `json:"source_path" db:"source_path"`
	Samenvatting string    `json:"samenvatting" db:"samenvatting"`
	Tags         []string  `json:"tags" db:"tags"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

type LCDossierDocument struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	UserID        string     `json:"user_id" db:"user_id"`
	DocumentKey   string     `json:"document_key" db:"document_key"`
	Titel         string     `json:"titel" db:"titel"`
	TemplateLabel *string    `json:"template_label" db:"template_label"`
	ContextType   string     `json:"context_type" db:"context_type"`
	ContextID     *string    `json:"context_id" db:"context_id"`
	ContextTitle  *string    `json:"context_title" db:"context_title"`
	LeadID        *uuid.UUID `json:"lead_id" db:"lead_id"`
	ProjectID     *uuid.UUID `json:"project_id" db:"project_id"`
	WorkstreamID  *uuid.UUID `json:"workstream_id" db:"workstream_id"`
	CompanyID     *uuid.UUID `json:"company_id" db:"company_id"`
	PDFURL        string     `json:"pdf_url" db:"pdf_url"`
	Theme         string     `json:"theme" db:"theme"`
	Delivery      string     `json:"delivery" db:"delivery"`
	Notes         *string    `json:"notes" db:"notes"`
	GeneratedAt   time.Time  `json:"generated_at" db:"generated_at"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
}

type LCDossierDocumentCreate struct {
	DocumentKey   string     `json:"document_key"`
	Titel         string     `json:"titel"`
	TemplateLabel *string    `json:"template_label"`
	ContextType   string     `json:"context_type"`
	ContextID     *string    `json:"context_id"`
	ContextTitle  *string    `json:"context_title"`
	LeadID        *uuid.UUID `json:"lead_id"`
	ProjectID     *uuid.UUID `json:"project_id"`
	WorkstreamID  *uuid.UUID `json:"workstream_id"`
	CompanyID     *uuid.UUID `json:"company_id"`
	PDFURL        string     `json:"pdf_url"`
	Theme         string     `json:"theme"`
	Delivery      string     `json:"delivery"`
	Notes         *string    `json:"notes"`
}

// LCDossierAdviceRequest is a read-only request for AI dossier guidance.
type LCDossierAdviceRequest struct {
	CompanyID    *uuid.UUID `json:"company_id,omitempty"`
	LeadID       *uuid.UUID `json:"lead_id,omitempty"`
	ProjectID    *uuid.UUID `json:"project_id,omitempty"`
	WorkstreamID *uuid.UUID `json:"workstream_id,omitempty"`
	Query        string     `json:"query,omitempty"`
	Limit        int        `json:"limit,omitempty"`
}

// LCDossierAdviceTarget describes the CRM entity used as dossier context.
type LCDossierAdviceTarget struct {
	Kind        string     `json:"kind"`
	ID          *uuid.UUID `json:"id,omitempty"`
	Title       string     `json:"title"`
	Subtitle    string     `json:"subtitle,omitempty"`
	CompanyID   *uuid.UUID `json:"company_id,omitempty"`
	CompanyName string     `json:"company_name,omitempty"`
	Phase       string     `json:"phase,omitempty"`
	Status      string     `json:"status,omitempty"`
	Priority    string     `json:"priority,omitempty"`
	Query       string     `json:"query,omitempty"`
}

// LCDocumentRecommendation is a deterministic document recommendation for a dossier.
type LCDocumentRecommendation struct {
	Document          LCDocument `json:"document"`
	Score             int        `json:"score"`
	Priority          string     `json:"priority"`
	Usage             string     `json:"usage"`
	Reasons           []string   `json:"reasons"`
	AlreadyInDossier  bool       `json:"alreadyInDossier"`
	DossierDocumentID *uuid.UUID `json:"dossierDocumentId,omitempty"`
	DossierCreatedAt  *time.Time `json:"dossierCreatedAt,omitempty"`
}

// LCDossierRequirement tracks a professional dossier building block.
type LCDossierRequirement struct {
	Key                     string   `json:"key"`
	Label                   string   `json:"label"`
	Status                  string   `json:"status"`
	Reason                  string   `json:"reason"`
	RecommendedDocumentKeys []string `json:"recommendedDocumentKeys"`
}

// LCDossierAdvice is the read-only AI contract used by Telegram, Grok and UI.
type LCDossierAdvice struct {
	GeneratedAt             time.Time                  `json:"generatedAt"`
	Target                  LCDossierAdviceTarget      `json:"target"`
	Status                  string                     `json:"status"`
	Coverage                int                        `json:"coverage"`
	Requirements            []LCDossierRequirement     `json:"requirements"`
	Recommendations         []LCDocumentRecommendation `json:"recommendations"`
	PresentDocuments        []LCDossierDocument        `json:"presentDocuments"`
	TotalDossierDocuments   int                        `json:"totalDossierDocuments"`
	MatchedDossierDocuments int                        `json:"matchedDossierDocuments"`
	NextActions             []string                   `json:"nextActions"`
	Evidence                []string                   `json:"evidence"`
}

type LCActivityEvent struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	UserID         string     `json:"user_id" db:"user_id"`
	CompanyID      uuid.UUID  `json:"company_id" db:"company_id"`
	ContactID      *uuid.UUID `json:"contact_id" db:"contact_id"`
	LeadID         *uuid.UUID `json:"lead_id" db:"lead_id"`
	ProjectID      *uuid.UUID `json:"project_id" db:"project_id"`
	WorkstreamID   *uuid.UUID `json:"workstream_id" db:"workstream_id"`
	ActionItemID   *uuid.UUID `json:"action_item_id" db:"action_item_id"`
	EventType      string     `json:"event_type" db:"event_type"`
	Channel        string     `json:"channel" db:"channel"`
	Title          string     `json:"title" db:"title"`
	Body           *string    `json:"body" db:"body"`
	OccurredAt     time.Time  `json:"occurred_at" db:"occurred_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
	CompanyName    *string    `json:"company_name,omitempty"`
	ContactName    *string    `json:"contact_name,omitempty"`
	ProjectName    *string    `json:"project_name,omitempty"`
	WorkstreamName *string    `json:"workstream_name,omitempty"`
}

type LCActivityEventCreate struct {
	CompanyID    uuid.UUID  `json:"company_id"`
	ContactID    *uuid.UUID `json:"contact_id"`
	LeadID       *uuid.UUID `json:"lead_id"`
	ProjectID    *uuid.UUID `json:"project_id"`
	WorkstreamID *uuid.UUID `json:"workstream_id"`
	ActionItemID *uuid.UUID `json:"action_item_id"`
	EventType    string     `json:"event_type"`
	Channel      string     `json:"channel"`
	Title        string     `json:"title"`
	Body         *string    `json:"body"`
	OccurredAt   *string    `json:"occurred_at"`
}

type LCBilling struct {
	Summary      LCBillingSummary `json:"summary"`
	Quotes       []LCQuote        `json:"quotes"`
	QuoteLines   []LCQuoteLine    `json:"quoteLines"`
	TimeEntries  []LCTimeEntry    `json:"timeEntries"`
	Invoices     []LCInvoice      `json:"invoices"`
	InvoiceLines []LCInvoiceLine  `json:"invoiceLines"`
}

type LCMailbox struct {
	Summary   LCMailboxSummary   `json:"summary"`
	Templates []LCMailTemplate   `json:"templates"`
	Outbox    []LCMailOutboxItem `json:"outbox"`
	Inbox     []LCMailInboxItem  `json:"inbox"`
}

// LCMailInboxItem is a received email (ingested from Microsoft Graph), threaded
// to the outbox by conversation_id and linked to a company by sender address.
type LCMailInboxItem struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	UserID         string     `json:"user_id" db:"user_id"`
	MessageID      string     `json:"message_id" db:"message_id"`
	ConversationID *string    `json:"conversation_id" db:"conversation_id"`
	CompanyID      *uuid.UUID `json:"company_id" db:"company_id"`
	ContactID      *uuid.UUID `json:"contact_id" db:"contact_id"`
	FromEmail      string     `json:"from_email" db:"from_email"`
	FromName       *string    `json:"from_name" db:"from_name"`
	Subject        *string    `json:"subject" db:"subject"`
	BodyPreview    *string    `json:"body_preview" db:"body_preview"`
	WebLink        *string    `json:"web_link" db:"web_link"`
	HasAttachments bool       `json:"has_attachments" db:"has_attachments"`
	IsRead         bool       `json:"is_read" db:"is_read"`
	ReceivedAt     time.Time  `json:"received_at" db:"received_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
	CompanyName    *string    `json:"company_name,omitempty"`
}

type LCMailboxSummary struct {
	Templates       int    `json:"templates"`
	ActiveTemplates int    `json:"activeTemplates"`
	Outbox          int    `json:"outbox"`
	Drafts          int    `json:"drafts"`
	Sent            int    `json:"sent"`
	Failed          int    `json:"failed"`
	Provider        string `json:"provider"`
	SenderEmail     string `json:"senderEmail"`
	Configured      bool   `json:"configured"`
	NextStep        string `json:"nextStep"`
}

type LCMailTemplate struct {
	ID              uuid.UUID `json:"id" db:"id"`
	UserID          string    `json:"user_id" db:"user_id"`
	TemplateKey     string    `json:"template_key" db:"template_key"`
	Name            string    `json:"name" db:"name"`
	Category        string    `json:"category" db:"category"`
	Status          string    `json:"status" db:"status"`
	SubjectTemplate string    `json:"subject_template" db:"subject_template"`
	BodyHTML        string    `json:"body_html" db:"body_html"`
	BodyText        *string   `json:"body_text" db:"body_text"`
	DefaultCC       []string  `json:"default_cc" db:"default_cc"`
	DefaultBCC      []string  `json:"default_bcc" db:"default_bcc"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

type LCMailTemplateCreate struct {
	TemplateKey     string   `json:"template_key"`
	Name            string   `json:"name"`
	Category        string   `json:"category"`
	Status          string   `json:"status"`
	SubjectTemplate string   `json:"subject_template"`
	BodyHTML        string   `json:"body_html"`
	BodyText        *string  `json:"body_text"`
	DefaultCC       []string `json:"default_cc"`
	DefaultBCC      []string `json:"default_bcc"`
}

type LCMailTemplateUpdate struct {
	Name            *string  `json:"name,omitempty"`
	Category        *string  `json:"category,omitempty"`
	Status          *string  `json:"status,omitempty"`
	SubjectTemplate *string  `json:"subject_template,omitempty"`
	BodyHTML        *string  `json:"body_html,omitempty"`
	BodyText        *string  `json:"body_text,omitempty"`
	DefaultCC       []string `json:"default_cc,omitempty"`
	DefaultBCC      []string `json:"default_bcc,omitempty"`
}

type LCMailOutboxItem struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	UserID            string     `json:"user_id" db:"user_id"`
	TemplateID        *uuid.UUID `json:"template_id" db:"template_id"`
	CompanyID         *uuid.UUID `json:"company_id" db:"company_id"`
	ContactID         *uuid.UUID `json:"contact_id" db:"contact_id"`
	ProjectID         *uuid.UUID `json:"project_id" db:"project_id"`
	WorkstreamID      *uuid.UUID `json:"workstream_id" db:"workstream_id"`
	QuoteID           *uuid.UUID `json:"quote_id" db:"quote_id"`
	InvoiceID         *uuid.UUID `json:"invoice_id" db:"invoice_id"`
	ToEmail           string     `json:"to_email" db:"to_email"`
	ToName            *string    `json:"to_name" db:"to_name"`
	CC                []string   `json:"cc" db:"cc"`
	BCC               []string   `json:"bcc" db:"bcc"`
	Subject           string     `json:"subject" db:"subject"`
	BodyHTML          string     `json:"body_html" db:"body_html"`
	BodyText          *string    `json:"body_text" db:"body_text"`
	Status            string     `json:"status" db:"status"`
	Provider          string     `json:"provider" db:"provider"`
	ProviderMessageID *string    `json:"provider_message_id" db:"provider_message_id"`
	ErrorMessage      *string    `json:"error_message" db:"error_message"`
	SentAt            *time.Time `json:"sent_at" db:"sent_at"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`
	TemplateName      *string    `json:"template_name,omitempty"`
	CompanyName       *string    `json:"company_name,omitempty"`
	ContactName       *string    `json:"contact_name,omitempty"`
}

type LCMailSendRequest struct {
	TemplateID   uuid.UUID          `json:"template_id"`
	CompanyID    *uuid.UUID         `json:"company_id"`
	ContactID    *uuid.UUID         `json:"contact_id"`
	ProjectID    *uuid.UUID         `json:"project_id"`
	WorkstreamID *uuid.UUID         `json:"workstream_id"`
	QuoteID      *uuid.UUID         `json:"quote_id"`
	InvoiceID    *uuid.UUID         `json:"invoice_id"`
	ToEmail      *string            `json:"to_email"`
	ToName       *string            `json:"to_name"`
	CC           []string           `json:"cc"`
	BCC          []string           `json:"bcc"`
	Variables    map[string]string  `json:"variables"`
	Send         bool               `json:"send"`
	Attachments  []LCMailAttachment `json:"attachments,omitempty"`
}

type LCMailAttachment struct {
	Name         string `json:"name"`
	ContentType  string `json:"content_type"`
	ContentBytes string `json:"content_bytes"`
}

type LCMailAISuggestionRequest struct {
	TemplateID   uuid.UUID                   `json:"template_id"`
	CompanyID    *uuid.UUID                  `json:"company_id"`
	ContactID    *uuid.UUID                  `json:"contact_id"`
	ProjectID    *uuid.UUID                  `json:"project_id"`
	WorkstreamID *uuid.UUID                  `json:"workstream_id"`
	QuoteID      *uuid.UUID                  `json:"quote_id"`
	InvoiceID    *uuid.UUID                  `json:"invoice_id"`
	ToEmail      *string                     `json:"to_email"`
	ToName       *string                     `json:"to_name"`
	Intent       string                      `json:"intent"`
	Tone         string                      `json:"tone"`
	Variables    map[string]string           `json:"variables"`
	Attachments  []LCMailAIContextAttachment `json:"attachments,omitempty"`
}

type LCMailAISuggestion struct {
	Variables   map[string]string `json:"variables"`
	SubjectHint *string           `json:"subject_hint,omitempty"`
	Briefing    string            `json:"briefing"`
	Sources     []LCMailAISource  `json:"sources"`
	Confidence  string            `json:"confidence"`
	GeneratedAt time.Time         `json:"generated_at"`
}

type LCMailAISource struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Date    string `json:"date,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type LCMailAIContext struct {
	Template     *LCMailTemplate             `json:"template,omitempty"`
	Company      *LCCompany                  `json:"company,omitempty"`
	Contact      *LCContact                  `json:"contact,omitempty"`
	Project      map[string]any              `json:"project,omitempty"`
	Workstream   map[string]any              `json:"workstream,omitempty"`
	Quote        map[string]any              `json:"quote,omitempty"`
	Invoice      map[string]any              `json:"invoice,omitempty"`
	Notes        []LCMailAIContextItem       `json:"notes"`
	Agenda       []LCMailAIContextItem       `json:"agenda"`
	Schedule     []LCMailAIContextItem       `json:"schedule"`
	Actions      []LCMailAIContextItem       `json:"actions"`
	Activity     []LCMailAIContextItem       `json:"activity"`
	Billing      []LCMailAIContextItem       `json:"billing"`
	Dossier      []LCMailAIContextItem       `json:"dossier"`
	Attachments  []LCMailAIContextAttachment `json:"attachments"`
	ExistingVars map[string]string           `json:"existing_vars"`
	Today        string                      `json:"today"`
}

type LCMailAIContextAttachment struct {
	Name             string `json:"name"`
	ContentType      string `json:"content_type"`
	Size             int    `json:"size"`
	Pages            int    `json:"pages"`
	ExtractedText    string `json:"extracted_text"`
	Summary          string `json:"summary"`
	ExtractionStatus string `json:"extraction_status"`
}

type LCMailAIContextItem struct {
	Type     string `json:"type"`
	ID       string `json:"id,omitempty"`
	Title    string `json:"title"`
	Date     string `json:"date,omitempty"`
	Status   string `json:"status,omitempty"`
	Priority string `json:"priority,omitempty"`
	Summary  string `json:"summary,omitempty"`
}

type LCBillingSummary struct {
	Quotes              int    `json:"quotes"`
	OpenQuotes          int    `json:"openQuotes"`
	TimeEntries         int    `json:"timeEntries"`
	BillableMinutes     int    `json:"billableMinutes"`
	UninvoicedMinutes   int    `json:"uninvoicedMinutes"`
	Invoices            int    `json:"invoices"`
	OpenInvoices        int    `json:"openInvoices"`
	OutstandingCents    int    `json:"outstandingCents"`
	PaidCents           int    `json:"paidCents"`
	DefaultProvider     string `json:"defaultProvider"`
	BunqReady           bool   `json:"bunqReady"`
	NextStepDescription string `json:"nextStepDescription"`
}

type LCQuote struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	UserID          string     `json:"user_id" db:"user_id"`
	CompanyID       *uuid.UUID `json:"company_id" db:"company_id"`
	ProjectID       *uuid.UUID `json:"project_id" db:"project_id"`
	WorkstreamID    *uuid.UUID `json:"workstream_id" db:"workstream_id"`
	QuoteNumber     string     `json:"quote_number" db:"quote_number"`
	Titel           string     `json:"titel" db:"titel"`
	Status          string     `json:"status" db:"status"`
	IssueDate       string     `json:"issue_date" db:"issue_date"`
	ValidUntil      *string    `json:"valid_until" db:"valid_until"`
	Currency        string     `json:"currency" db:"currency"`
	SubtotalCents   int        `json:"subtotal_cents" db:"subtotal_cents"`
	VatRateBps      int        `json:"vat_rate_bps" db:"vat_rate_bps"`
	VatCents        int        `json:"vat_cents" db:"vat_cents"`
	TotalCents      int        `json:"total_cents" db:"total_cents"`
	AcceptedAt      *time.Time `json:"accepted_at" db:"accepted_at"`
	Notes           *string    `json:"notes" db:"notes"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
	CompanyName     *string    `json:"company_name,omitempty"`
	ProjectName     *string    `json:"project_name,omitempty"`
	WorkstreamTitle *string    `json:"workstream_title,omitempty"`
}

type LCQuoteLine struct {
	ID              uuid.UUID `json:"id" db:"id"`
	QuoteID         uuid.UUID `json:"quote_id" db:"quote_id"`
	UserID          string    `json:"user_id" db:"user_id"`
	Description     string    `json:"description" db:"description"`
	Quantity        int       `json:"quantity" db:"quantity"`
	UnitAmountCents int       `json:"unit_amount_cents" db:"unit_amount_cents"`
	TotalCents      int       `json:"total_cents" db:"total_cents"`
	SortOrder       int       `json:"sort_order" db:"sort_order"`
}

type LCQuoteLineCreate struct {
	Description     string `json:"description"`
	Quantity        int    `json:"quantity"`
	UnitAmountCents int    `json:"unit_amount_cents"`
	TotalCents      int    `json:"total_cents"`
	SortOrder       int    `json:"sort_order"`
}

type LCQuoteCreate struct {
	CompanyID    *uuid.UUID          `json:"company_id"`
	ProjectID    *uuid.UUID          `json:"project_id"`
	WorkstreamID *uuid.UUID          `json:"workstream_id"`
	Titel        string              `json:"titel"`
	Status       string              `json:"status"`
	IssueDate    *string             `json:"issue_date"`
	ValidUntil   *string             `json:"valid_until"`
	Currency     string              `json:"currency"`
	VatRateBps   *int                `json:"vat_rate_bps"`
	Notes        *string             `json:"notes"`
	Lines        []LCQuoteLineCreate `json:"lines"`
}

type LCTimeEntry struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	UserID          string     `json:"user_id" db:"user_id"`
	CompanyID       *uuid.UUID `json:"company_id" db:"company_id"`
	ProjectID       *uuid.UUID `json:"project_id" db:"project_id"`
	WorkstreamID    *uuid.UUID `json:"workstream_id" db:"workstream_id"`
	ActivityEventID *uuid.UUID `json:"activity_event_id" db:"activity_event_id"`
	InvoiceID       *uuid.UUID `json:"invoice_id" db:"invoice_id"`
	SourceType      string     `json:"source_type" db:"source_type"`
	SourceID        *string    `json:"source_id" db:"source_id"`
	Description     string     `json:"description" db:"description"`
	EntryDate       string     `json:"entry_date" db:"entry_date"`
	Minutes         int        `json:"minutes" db:"minutes"`
	HourlyRateCents int        `json:"hourly_rate_cents" db:"hourly_rate_cents"`
	Billable        bool       `json:"billable" db:"billable"`
	Status          string     `json:"status" db:"status"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
	CompanyName     *string    `json:"company_name,omitempty"`
	ProjectName     *string    `json:"project_name,omitempty"`
	WorkstreamTitle *string    `json:"workstream_title,omitempty"`
}

type LCTimeEntryCreate struct {
	CompanyID       *uuid.UUID `json:"company_id"`
	ProjectID       *uuid.UUID `json:"project_id"`
	WorkstreamID    *uuid.UUID `json:"workstream_id"`
	ActivityEventID *uuid.UUID `json:"activity_event_id"`
	SourceType      string     `json:"source_type"`
	SourceID        *string    `json:"source_id"`
	Description     string     `json:"description"`
	EntryDate       *string    `json:"entry_date"`
	Minutes         int        `json:"minutes"`
	HourlyRateCents *int       `json:"hourly_rate_cents"`
	Billable        *bool      `json:"billable"`
	Status          string     `json:"status"`
}

type LCInvoice struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	UserID            string     `json:"user_id" db:"user_id"`
	CompanyID         *uuid.UUID `json:"company_id" db:"company_id"`
	ProjectID         *uuid.UUID `json:"project_id" db:"project_id"`
	WorkstreamID      *uuid.UUID `json:"workstream_id" db:"workstream_id"`
	QuoteID           *uuid.UUID `json:"quote_id" db:"quote_id"`
	InvoiceNumber     string     `json:"invoice_number" db:"invoice_number"`
	Status            string     `json:"status" db:"status"`
	IssueDate         string     `json:"issue_date" db:"issue_date"`
	DueDate           *string    `json:"due_date" db:"due_date"`
	Currency          string     `json:"currency" db:"currency"`
	SubtotalCents     int        `json:"subtotal_cents" db:"subtotal_cents"`
	VatRateBps        int        `json:"vat_rate_bps" db:"vat_rate_bps"`
	VatCents          int        `json:"vat_cents" db:"vat_cents"`
	TotalCents        int        `json:"total_cents" db:"total_cents"`
	PaidCents         int        `json:"paid_cents" db:"paid_cents"`
	PaymentProvider   string     `json:"payment_provider" db:"payment_provider"`
	ProviderRequestID *string    `json:"provider_request_id" db:"provider_request_id"`
	MerchantReference *string    `json:"merchant_reference" db:"merchant_reference"`
	PaymentURL        *string    `json:"payment_url" db:"payment_url"`
	DocumentURL       *string    `json:"document_url" db:"document_url"`
	DocumentGenerated *time.Time `json:"document_generated_at" db:"document_generated_at"`
	UBLXML            *string    `json:"-" db:"ubl_xml"`
	UBLGeneratedAt    *time.Time `json:"ubl_generated_at" db:"ubl_generated_at"`
	PaymentCheckedAt  *time.Time `json:"payment_checked_at" db:"payment_checked_at"`
	PaymentStatus     *string    `json:"payment_status" db:"payment_status"`
	PaymentLastError  *string    `json:"payment_last_error" db:"payment_last_error"`
	ReminderCount     int        `json:"reminder_count" db:"reminder_count"`
	LastReminderAt    *time.Time `json:"last_reminder_at" db:"last_reminder_at"`
	SentAt            *time.Time `json:"sent_at" db:"sent_at"`
	PaidAt            *time.Time `json:"paid_at" db:"paid_at"`
	Notes             *string    `json:"notes" db:"notes"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`
	CompanyName       *string    `json:"company_name,omitempty"`
	ProjectName       *string    `json:"project_name,omitempty"`
	WorkstreamTitle   *string    `json:"workstream_title,omitempty"`
}

type LCInvoiceDocument struct {
	Invoice      LCInvoice       `json:"invoice"`
	Lines        []LCInvoiceLine `json:"lines"`
	Company      *LCCompany      `json:"company,omitempty"`
	HTML         string          `json:"html"`
	Text         string          `json:"text"`
	UBLXML       string          `json:"ubl_xml"`
	DownloadName string          `json:"download_name"`
	GeneratedAt  time.Time       `json:"generated_at"`
}

type LCInvoicePaymentRefresh struct {
	Invoice        LCInvoice `json:"invoice"`
	ProviderStatus string    `json:"provider_status"`
	Changed        bool      `json:"changed"`
	Message        string    `json:"message"`
	CheckedAt      time.Time `json:"checked_at"`
}

type LCInvoiceLine struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	InvoiceID         uuid.UUID  `json:"invoice_id" db:"invoice_id"`
	UserID            string     `json:"user_id" db:"user_id"`
	SourceTimeEntryID *uuid.UUID `json:"source_time_entry_id" db:"source_time_entry_id"`
	Description       string     `json:"description" db:"description"`
	QuantityMinutes   int        `json:"quantity_minutes" db:"quantity_minutes"`
	UnitAmountCents   int        `json:"unit_amount_cents" db:"unit_amount_cents"`
	VatRateBps        int        `json:"vat_rate_bps" db:"vat_rate_bps"`
	TotalCents        int        `json:"total_cents" db:"total_cents"`
	SortOrder         int        `json:"sort_order" db:"sort_order"`
}

type LCInvoiceLineCreate struct {
	SourceTimeEntryID *uuid.UUID `json:"source_time_entry_id"`
	Description       string     `json:"description"`
	QuantityMinutes   int        `json:"quantity_minutes"`
	UnitAmountCents   int        `json:"unit_amount_cents"`
	VatRateBps        *int       `json:"vat_rate_bps"`
	TotalCents        int        `json:"total_cents"`
	SortOrder         int        `json:"sort_order"`
}

type LCInvoiceCreate struct {
	CompanyID    *uuid.UUID            `json:"company_id"`
	ProjectID    *uuid.UUID            `json:"project_id"`
	WorkstreamID *uuid.UUID            `json:"workstream_id"`
	QuoteID      *uuid.UUID            `json:"quote_id"`
	Status       string                `json:"status"`
	IssueDate    *string               `json:"issue_date"`
	DueDate      *string               `json:"due_date"`
	Currency     string                `json:"currency"`
	VatRateBps   *int                  `json:"vat_rate_bps"`
	Notes        *string               `json:"notes"`
	TimeEntryIDs []uuid.UUID           `json:"time_entry_ids"`
	Lines        []LCInvoiceLineCreate `json:"lines"`
}

type LCInvoiceStatusUpdate struct {
	Status            string  `json:"status"`
	PaidCents         *int    `json:"paid_cents"`
	PaymentProvider   *string `json:"payment_provider"`
	ProviderRequestID *string `json:"provider_request_id"`
	MerchantReference *string `json:"merchant_reference"`
	PaymentURL        *string `json:"payment_url"`
	PaymentStatus     *string `json:"payment_status"`
	PaymentLastError  *string `json:"payment_last_error"`
	PaymentCheckedAt  *string `json:"payment_checked_at"`
	PaidAt            *string `json:"paid_at"`
	SentAt            *string `json:"sent_at"`
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
	Summary           LCCockpitSummary     `json:"summary"`
	Companies         []LCCompany          `json:"companies"`
	Contacts          []LCContact          `json:"contacts"`
	AccessCredentials []LCAccessCredential `json:"accessCredentials"`
	ActiveLeads       []LCLead             `json:"activeLeads"`
	ActiveWorkstreams []LCWorkstream       `json:"activeWorkstreams"`
	ActiveProjects    []LCProject          `json:"activeProjects"`
	ActionItems       []LCActionItem       `json:"actionItems"`
	OpenIncidents     []LCSlaIncident      `json:"openIncidents"`
	OpenChanges       []LCChangeRequest    `json:"openChanges"`
	RecentDecisions   []LCDecision         `json:"recentDecisions"`
	DocumentCatalog   []LCDocument         `json:"documentCatalog"`
	DossierDocuments  []LCDossierDocument  `json:"dossierDocuments"`
	ActivityEvents    []LCActivityEvent    `json:"activityEvents"`
	Mailbox           *LCMailboxSummary    `json:"mailbox,omitempty"`
	BusinessSignals   []LCBusinessSignal   `json:"businessSignals"`
	FollowUps         []LCFollowUpSignal   `json:"followUps"`
}

type LCCockpitSummary struct {
	Companies         int  `json:"companies"`
	Contacts          int  `json:"contacts"`
	AccessCredentials int  `json:"accessCredentials"`
	Leads             int  `json:"leads"`
	ActiveLeads       int  `json:"activeLeads"`
	Workstreams       int  `json:"workstreams"`
	ActiveWorkstreams int  `json:"activeWorkstreams"`
	Projects          int  `json:"projects"`
	ActiveProjects    int  `json:"activeProjects"`
	Documents         int  `json:"documents"`
	OpenIncidents     int  `json:"openIncidents"`
	OpenChanges       int  `json:"openChanges"`
	Decisions         int  `json:"decisions"`
	ActionItems       int  `json:"actionItems"`
	DossierDocuments  int  `json:"dossierDocuments"`
	ActivityEvents    int  `json:"activityEvents"`
	MailTemplates     int  `json:"mailTemplates"`
	MailOutbox        int  `json:"mailOutbox"`
	MailConfigured    bool `json:"mailConfigured"`
	DocumentsSeeded   bool `json:"documentsSeeded"`
	BusinessSignals   int  `json:"businessSignals"`
	FollowUps         int  `json:"followUps"`
}

// LCConvertLeadToProject is the request body for converting a lead to a project.
type LCConvertLeadToProject struct {
	LeadID       uuid.UUID `json:"lead_id"`
	Naam         string    `json:"naam"`
	Fase         *string   `json:"fase"`
	Status       *string   `json:"status"`
	Samenvatting *string   `json:"samenvatting"`
}

// LCConvertWorkstreamToProject is the request body for promoting an opdracht to a project.
type LCConvertWorkstreamToProject struct {
	WorkstreamID uuid.UUID  `json:"workstream_id"`
	ProjectID    *uuid.UUID `json:"project_id,omitempty"`
	Naam         string     `json:"naam"`
	Fase         *string    `json:"fase"`
	Status       *string    `json:"status"`
	Samenvatting *string    `json:"samenvatting"`
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
