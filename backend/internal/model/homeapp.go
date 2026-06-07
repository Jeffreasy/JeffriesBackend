package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ─── Schedule (Werkdiensten) ─────────────────────────────────────────────────

type Schedule struct {
	ID           uuid.UUID `json:"id" db:"id"`
	UserID       string    `json:"user_id" db:"user_id"`
	EventID      string    `json:"event_id" db:"event_id"`
	Titel        string    `json:"titel" db:"titel"`
	StartDatum   string    `json:"start_datum" db:"start_datum"`
	StartTijd    string    `json:"start_tijd" db:"start_tijd"`
	EindDatum    string    `json:"eind_datum" db:"eind_datum"`
	EindTijd     string    `json:"eind_tijd" db:"eind_tijd"`
	Werktijd     string    `json:"werktijd" db:"werktijd"`
	Locatie      string    `json:"locatie" db:"locatie"`
	Team         string    `json:"team" db:"team"`
	ShiftType    string    `json:"shift_type" db:"shift_type"`
	Prioriteit   int       `json:"prioriteit" db:"prioriteit"`
	Duur         float64   `json:"duur" db:"duur"`
	Weeknr       string    `json:"weeknr" db:"weeknr"`
	Dag          string    `json:"dag" db:"dag"`
	Status       string    `json:"status" db:"status"`
	Beschrijving string    `json:"beschrijving" db:"beschrijving"`
	Heledag      bool      `json:"heledag" db:"heledag"`
}

type ScheduleMeta struct {
	ID         uuid.UUID `json:"id" db:"id"`
	UserID     string    `json:"user_id" db:"user_id"`
	ImportedAt time.Time `json:"imported_at" db:"imported_at"`
	FileName   string    `json:"file_name" db:"file_name"`
	TotalRows  int       `json:"total_rows" db:"total_rows"`
}

type ScheduleImport struct {
	EventID      string  `json:"eventId"`
	Titel        string  `json:"titel"`
	StartDatum   string  `json:"startDatum"`
	StartTijd    string  `json:"startTijd"`
	EindDatum    string  `json:"eindDatum"`
	EindTijd     string  `json:"eindTijd"`
	Werktijd     string  `json:"werktijd"`
	Locatie      string  `json:"locatie"`
	Team         string  `json:"team"`
	ShiftType    string  `json:"shiftType"`
	Prioriteit   int     `json:"prioriteit"`
	Duur         float64 `json:"duur"`
	Weeknr       string  `json:"weeknr"`
	Dag          string  `json:"dag"`
	Status       string  `json:"status"`
	Beschrijving string  `json:"beschrijving"`
	Heledag      bool    `json:"heledag"`
}

// ─── Loonstrook ──────────────────────────────────────────────────────────────

type Loonstrook struct {
	ID               string          `json:"id" db:"id"`
	UserID           string          `json:"user_id" db:"user_id"`
	Jaar             int             `json:"jaar" db:"jaar"`
	Periode          int             `json:"periode" db:"periode"`
	PeriodeLabel     string          `json:"periode_label" db:"periode_label"`
	Type             string          `json:"type" db:"type"`
	Netto            float64         `json:"netto" db:"netto"`
	BrutoBetaling    float64         `json:"bruto_betaling" db:"bruto_betaling"`
	BrutoInhouding   float64         `json:"bruto_inhouding" db:"bruto_inhouding"`
	SalarisBasis     float64         `json:"salaris_basis" db:"salaris_basis"`
	OrtTotaal        float64         `json:"ort_totaal" db:"ort_totaal"`
	OrtDetail        json.RawMessage `json:"ort_detail" db:"ort_detail" swaggertype:"object"`
	AmtZeerintensief *float64        `json:"amt_zeerintensief" db:"amt_zeerintensief"`
	Pensioenpremie   *float64        `json:"pensioenpremie" db:"pensioenpremie"`
	Loonheffing      *float64        `json:"loonheffing" db:"loonheffing"`
	Reiskosten       *float64        `json:"reiskosten" db:"reiskosten"`
	Vakantietoeslag  *float64        `json:"vakantietoeslag" db:"vakantietoeslag"`
	EjuBedrag        *float64        `json:"eju_bedrag" db:"eju_bedrag"`
	ToeslagBalansvlf *float64        `json:"toeslag_balansvlf" db:"toeslag_balansvlf"`
	ExtraUrenBedrag  *float64        `json:"extra_uren_bedrag" db:"extra_uren_bedrag"`
	Schaalnummer     string          `json:"schaalnummer" db:"schaalnummer"`
	Trede            string          `json:"trede" db:"trede"`
	ParttimeFactor   float64         `json:"parttime_factor" db:"parttime_factor"`
	Uurloon          *float64        `json:"uurloon" db:"uurloon"`
	Componenten      json.RawMessage `json:"componenten" db:"componenten" swaggertype:"object"`
	Cumulatieven     json.RawMessage `json:"cumulatieven" db:"cumulatieven" swaggertype:"object"`
	GeimporteerdOp   string          `json:"geimporteerd_op" db:"geimporteerd_op"`
}

// ─── Salary ──────────────────────────────────────────────────────────────────

type Salary struct {
	ID                 uuid.UUID `json:"id" db:"id"`
	UserID             string    `json:"user_id" db:"user_id"`
	Periode            string    `json:"periode" db:"periode"`
	Jaar               int       `json:"jaar" db:"jaar"`
	Maand              int       `json:"maand" db:"maand"`
	AantalDiensten     int       `json:"aantal_diensten" db:"aantal_diensten"`
	UurloonORT         float64   `json:"uurloon_ort" db:"uurloon_ort"`
	BasisLoon          float64   `json:"basis_loon" db:"basis_loon"`
	AmtZeerintensief   float64   `json:"amt_zeerintensief" db:"amt_zeerintensief"`
	ToeslagBalansvlf   float64   `json:"toeslag_balansvlf" db:"toeslag_balansvlf"`
	OrtTotaal          float64   `json:"ort_totaal" db:"ort_totaal"`
	ExtraUrenBedrag    float64   `json:"extra_uren_bedrag" db:"extra_uren_bedrag"`
	ToeslagVakatieUren float64   `json:"toeslag_vakatie_uren" db:"toeslag_vakatie_uren"`
	Reiskosten         float64   `json:"reiskosten" db:"reiskosten"`
	EenmaligTotaal     float64   `json:"eenmalig_totaal" db:"eenmalig_totaal"`
	BrutoBetaling      float64   `json:"bruto_betaling" db:"bruto_betaling"`
	Pensioenpremie     float64   `json:"pensioenpremie" db:"pensioenpremie"`
	LoonheffingSchat   float64   `json:"loonheffing_schat" db:"loonheffing_schat"`
	NettoPrognose      float64   `json:"netto_prognose" db:"netto_prognose"`
	BerekendOp         time.Time `json:"berekend_op" db:"berekend_op"`
}

// ─── Transaction ─────────────────────────────────────────────────────────────

type Transaction struct {
	ID                   uuid.UUID `json:"id" db:"id"`
	UserID               string    `json:"user_id" db:"user_id"`
	RekeningIban         string    `json:"rekening_iban" db:"rekening_iban"`
	Volgnr               string    `json:"volgnr" db:"volgnr"`
	Datum                string    `json:"datum" db:"datum"`
	Bedrag               float64   `json:"bedrag" db:"bedrag"`
	SaldoNaTrn           float64   `json:"saldo_na_trn" db:"saldo_na_trn"`
	Code                 string    `json:"code" db:"code"`
	TegenrekeningIban    *string   `json:"tegenrekening_iban" db:"tegenrekening_iban"`
	TegenpartijNaam      *string   `json:"tegenpartij_naam" db:"tegenpartij_naam"`
	Omschrijving         string    `json:"omschrijving" db:"omschrijving"`
	Referentie           *string   `json:"referentie" db:"referentie"`
	RedenRetour          *string   `json:"reden_retour" db:"reden_retour"`
	OorspBedrag          *float64  `json:"oorsp_bedrag" db:"oorsp_bedrag"`
	OorspMunt            *string   `json:"oorsp_munt" db:"oorsp_munt"`
	IsInterneOverboeking bool      `json:"is_interne_overboeking" db:"is_interne_overboeking"`
	Categorie            *string   `json:"categorie" db:"categorie"`
}

type TransactionImport struct {
	RekeningIban         string   `json:"rekeningIban"`
	Volgnr               string   `json:"volgnr"`
	Datum                string   `json:"datum"`
	Bedrag               float64  `json:"bedrag"`
	SaldoNaTrn           float64  `json:"saldoNaTrn"`
	Code                 string   `json:"code"`
	TegenrekeningIban    *string  `json:"tegenrekeningIban"`
	TegenpartijNaam      *string  `json:"tegenpartijNaam"`
	Omschrijving         string   `json:"omschrijving"`
	Referentie           *string  `json:"referentie"`
	RedenRetour          *string  `json:"redenRetour"`
	OorspBedrag          *float64 `json:"oorspBedrag"`
	OorspMunt            *string  `json:"oorspMunt"`
	IsInterneOverboeking bool     `json:"isInterneOverboeking"`
	Categorie            *string  `json:"categorie"`
}

// ─── PersonalEvent ───────────────────────────────────────────────────────────

type PersonalEvent struct {
	ID                   uuid.UUID `json:"id" db:"id"`
	UserID               string    `json:"user_id" db:"user_id"`
	EventID              string    `json:"event_id" db:"event_id"`
	Titel                string    `json:"titel" db:"titel"`
	StartDatum           string    `json:"start_datum" db:"start_datum"`
	StartTijd            *string   `json:"start_tijd" db:"start_tijd"`
	EindDatum            string    `json:"eind_datum" db:"eind_datum"`
	EindTijd             *string   `json:"eind_tijd" db:"eind_tijd"`
	Heledag              bool      `json:"heledag" db:"heledag"`
	Locatie              *string   `json:"locatie" db:"locatie"`
	Beschrijving         *string   `json:"beschrijving" db:"beschrijving"`
	ConflictMetDienst    *string   `json:"conflict_met_dienst" db:"conflict_met_dienst"`
	Symbol               *string   `json:"symbol" db:"symbol"`
	BusinessContextType  *string   `json:"business_context_type" db:"business_context_type"`
	BusinessContextID    *string   `json:"business_context_id" db:"business_context_id"`
	BusinessContextTitle *string   `json:"business_context_title" db:"business_context_title"`
	Status               string    `json:"status" db:"status"`
	Kalender             string    `json:"kalender" db:"kalender"`
}

// ─── AuditLog ────────────────────────────────────────────────────────────────

type AuditLog struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    *string   `json:"user_id" db:"user_id"`
	Actor     string    `json:"actor" db:"actor"`
	Source    string    `json:"source" db:"source"`
	Action    string    `json:"action" db:"action"`
	Entity    string    `json:"entity" db:"entity"`
	EntityID  *string   `json:"entity_id" db:"entity_id"`
	Status    string    `json:"status" db:"status"`
	Summary   string    `json:"summary" db:"summary"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ─── Email (Gmail sync) ─────────────────────────────────────────────────────

type Email struct {
	ID            uuid.UUID `json:"id" db:"id"`
	UserID        string    `json:"user_id" db:"user_id"`
	GmailID       string    `json:"gmail_id" db:"gmail_id"`
	ThreadID      string    `json:"thread_id" db:"thread_id"`
	FromAddr      string    `json:"from_addr" db:"from_addr"`
	ToAddr        string    `json:"to_addr" db:"to_addr"`
	CC            *string   `json:"cc" db:"cc"`
	BCC           *string   `json:"bcc" db:"bcc"`
	Subject       string    `json:"subject" db:"subject"`
	Snippet       string    `json:"snippet" db:"snippet"`
	Datum         string    `json:"datum" db:"datum"`
	Ontvangen     int64     `json:"ontvangen" db:"ontvangen"`
	IsGelezen     bool      `json:"is_gelezen" db:"is_gelezen"`
	IsSter        bool      `json:"is_ster" db:"is_ster"`
	IsVerwijderd  bool      `json:"is_verwijderd" db:"is_verwijderd"`
	IsDraft       bool      `json:"is_draft" db:"is_draft"`
	LabelIDs      []string  `json:"label_ids" db:"label_ids"`
	Categorie     *string   `json:"categorie" db:"categorie"`
	HeeftBijlagen bool      `json:"heeft_bijlagen" db:"heeft_bijlagen"`
	BijlagenCount int       `json:"bijlagen_count" db:"bijlagen_count"`
	SearchText    string    `json:"search_text" db:"search_text"`
	SyncedAt      time.Time `json:"synced_at" db:"synced_at"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

// EmailSyncMeta tracks Gmail incremental sync state.
type EmailSyncMeta struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	UserID       string     `json:"user_id" db:"user_id"`
	HistoryID    string     `json:"history_id" db:"history_id"`
	LastFullSync *time.Time `json:"last_full_sync" db:"last_full_sync"`
	TotalSynced  int        `json:"total_synced" db:"total_synced"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

// ─── Privacy Settings ───────────────────────────────────────────────────────

type PrivacySettings struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    string    `json:"user_id" db:"user_id"`
	Finance   bool      `json:"finance" db:"finance"`
	Habits    bool      `json:"habits" db:"habits"`
	Notes     bool      `json:"notes" db:"notes"`
	Email     bool      `json:"email" db:"email"`
	Account   bool      `json:"account" db:"account"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// ─── Notes ──────────────────────────────────────────────────────────────────

type Note struct {
	ID                   uuid.UUID  `json:"id" db:"id"`
	UserID               string     `json:"user_id" db:"user_id"`
	Titel                *string    `json:"titel" db:"titel"`
	Inhoud               string     `json:"inhoud" db:"inhoud"`
	Tags                 []string   `json:"tags" db:"tags"`
	Kleur                *string    `json:"kleur" db:"kleur"`
	IsPinned             bool       `json:"is_pinned" db:"is_pinned"`
	IsArchived           bool       `json:"is_archived" db:"is_archived"`
	IsCompleted          bool       `json:"is_completed" db:"is_completed"`
	CompletedAt          *time.Time `json:"completed_at" db:"completed_at"`
	Deadline             *time.Time `json:"deadline" db:"deadline"`
	LinkedEventID        *string    `json:"linked_event_id" db:"linked_event_id"`
	Prioriteit           *string    `json:"prioriteit" db:"prioriteit"`
	Symbol               *string    `json:"symbol" db:"symbol"`
	BusinessContextType  *string    `json:"business_context_type" db:"business_context_type"`
	BusinessContextID    *string    `json:"business_context_id" db:"business_context_id"`
	BusinessContextTitle *string    `json:"business_context_title" db:"business_context_title"`
	TriageFlag           *bool      `json:"triage_flag" db:"triage_flag"`
	Aangemaakt           time.Time  `json:"aangemaakt" db:"aangemaakt"`
	Gewijzigd            time.Time  `json:"gewijzigd" db:"gewijzigd"`
}

type NoteRevision struct {
	ID                   uuid.UUID  `json:"id" db:"id"`
	NoteID               uuid.UUID  `json:"note_id" db:"note_id"`
	UserID               string     `json:"user_id" db:"user_id"`
	Titel                *string    `json:"titel" db:"titel"`
	Inhoud               string     `json:"inhoud" db:"inhoud"`
	Tags                 []string   `json:"tags" db:"tags"`
	Kleur                *string    `json:"kleur" db:"kleur"`
	Deadline             *time.Time `json:"deadline" db:"deadline"`
	LinkedEventID        *string    `json:"linked_event_id" db:"linked_event_id"`
	Prioriteit           *string    `json:"prioriteit" db:"prioriteit"`
	Symbol               *string    `json:"symbol" db:"symbol"`
	BusinessContextType  *string    `json:"business_context_type" db:"business_context_type"`
	BusinessContextID    *string    `json:"business_context_id" db:"business_context_id"`
	BusinessContextTitle *string    `json:"business_context_title" db:"business_context_title"`
	Aangemaakt           time.Time  `json:"aangemaakt" db:"aangemaakt"`
}

type NoteLink struct {
	ID         uuid.UUID `json:"id" db:"id"`
	UserID     string    `json:"user_id" db:"user_id"`
	SourceID   uuid.UUID `json:"source_id" db:"source_id"`
	TargetID   uuid.UUID `json:"target_id" db:"target_id"`
	Aangemaakt time.Time `json:"aangemaakt" db:"aangemaakt"`
}

// ─── Habits ─────────────────────────────────────────────────────────────────

type Habit struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	UserID            string     `json:"user_id" db:"user_id"`
	Naam              string     `json:"naam" db:"naam"`
	Emoji             string     `json:"emoji" db:"emoji"`
	Type              string     `json:"type" db:"type"`
	Beschrijving      *string    `json:"beschrijving" db:"beschrijving"`
	Frequentie        string     `json:"frequentie" db:"frequentie"`
	AangepasteDagen   []int32    `json:"aangepaste_dagen" db:"aangepaste_dagen"`
	DoelAantal        *int       `json:"doel_aantal" db:"doel_aantal"`
	RoosterFilter     *string    `json:"rooster_filter" db:"rooster_filter"`
	IsKwantitatief    bool       `json:"is_kwantitatief" db:"is_kwantitatief"`
	DoelWaarde        *float64   `json:"doel_waarde" db:"doel_waarde"`
	Eenheid           *string    `json:"eenheid" db:"eenheid"`
	DoelTijd          *string    `json:"doel_tijd" db:"doel_tijd"`
	XPPerVoltooiing   int        `json:"xp_per_voltooiing" db:"xp_per_voltooiing"`
	Moeilijkheid      string     `json:"moeilijkheid" db:"moeilijkheid"`
	FinancieCategorie *string    `json:"financie_categorie" db:"financie_categorie"`
	HuidigeStreak     int        `json:"huidige_streak" db:"huidige_streak"`
	LangsteStreak     int        `json:"langste_streak" db:"langste_streak"`
	TotaalVoltooid    int        `json:"totaal_voltooid" db:"totaal_voltooid"`
	TotaalXP          int        `json:"totaal_xp" db:"totaal_xp"`
	Kleur             *string    `json:"kleur" db:"kleur"`
	Volgorde          int        `json:"volgorde" db:"volgorde"`
	IsActief          bool       `json:"is_actief" db:"is_actief"`
	IsPauze           bool       `json:"is_pauze" db:"is_pauze"`
	GepauzeerOm       *time.Time `json:"gepauzeer_om" db:"gepauzeer_om"`
	Aangemaakt        time.Time  `json:"aangemaakt" db:"aangemaakt"`
	Gewijzigd         time.Time  `json:"gewijzigd" db:"gewijzigd"`
}

type HabitLog struct {
	ID         uuid.UUID `json:"id" db:"id"`
	UserID     string    `json:"user_id" db:"user_id"`
	HabitID    uuid.UUID `json:"habit_id" db:"habit_id"`
	Datum      string    `json:"datum" db:"datum"`
	Voltooid   bool      `json:"voltooid" db:"voltooid"`
	Waarde     *float64  `json:"waarde" db:"waarde"`
	IsIncident bool      `json:"is_incident" db:"is_incident"`
	TriggerCat *string   `json:"trigger_cat" db:"trigger_cat"`
	Notitie    *string   `json:"notitie" db:"notitie"`
	Bron       string    `json:"bron" db:"bron"`
	XPVerdiend int       `json:"xp_verdiend" db:"xp_verdiend"`
	Aangemaakt time.Time `json:"aangemaakt" db:"aangemaakt"`
}

type HabitBadge struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	UserID       string     `json:"user_id" db:"user_id"`
	BadgeID      string     `json:"badge_id" db:"badge_id"`
	HabitID      *uuid.UUID `json:"habit_id" db:"habit_id"`
	Naam         string     `json:"naam" db:"naam"`
	Emoji        string     `json:"emoji" db:"emoji"`
	Beschrijving string     `json:"beschrijving" db:"beschrijving"`
	XPBonus      int        `json:"xp_bonus" db:"xp_bonus"`
	BehaaldOp    time.Time  `json:"behaald_op" db:"behaald_op"`
}

// ─── Automations ────────────────────────────────────────────────────────────

type AutomationRow struct {
	ID            uuid.UUID       `json:"id" db:"id"`
	UserID        string          `json:"user_id" db:"user_id"`
	Name          string          `json:"name" db:"name"`
	Enabled       bool            `json:"enabled" db:"enabled"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	LastFiredAt   *time.Time      `json:"last_fired_at" db:"last_fired_at"`
	GroupName     *string         `json:"group_name" db:"group_name"`
	TriggerConfig json.RawMessage `json:"trigger_config" db:"trigger_config" swaggertype:"object"`
	ActionConfig  json.RawMessage `json:"action_config" db:"action_config" swaggertype:"object"`
}
