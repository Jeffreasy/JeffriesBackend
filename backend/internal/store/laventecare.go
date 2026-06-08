package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// LaventeCareStore handles all LaventeCare CRM database operations.
type LaventeCareStore struct {
	db *DB
}

var (
	ErrQuoteNotAccepted = errors.New("quote must be accepted before invoice conversion")
	ErrQuoteHasNoLines  = errors.New("quote has no lines to invoice")
)

// NewLaventeCareStore creates a new LaventeCareStore.
func NewLaventeCareStore(db *DB) *LaventeCareStore {
	return &LaventeCareStore{db: db}
}

// isOpenStatus returns true if the status is considered active/open.
func isOpenStatus(status string) bool {
	switch status {
	case "nieuw", "intake", "discovery", "voorstel", "actief",
		"wacht_op_klant", "geblokkeerd", "open", "bezig",
		"in_behandeling", "voorgesteld", "beoordeeld", "analyse",
		"uitvoering", "review":
		return true
	}
	return false
}

func isClosedStatus(status string) bool {
	switch status {
	case "afgerond", "done", "gesloten", "gearchiveerd", "omgezet_project":
		return true
	}
	return false
}

// ─── Companies & contacts ───────────────────────────────────────────────────

func (s *LaventeCareStore) ListCompanies(ctx context.Context, userID string, limit int, query string) ([]model.LCCompany, error) {
	if limit <= 0 {
		limit = 30
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	rows, err := s.db.Pool.Query(ctx,
		`SELECT c.id, c.user_id, c.naam, c.website, c.sector, c.status, c.relatie_type,
		        c.notities, c.laatste_contact, c.volgende_actie, c.created_at, c.updated_at,
		        (SELECT COUNT(*)::int FROM lc_contacts ct WHERE ct.user_id = c.user_id AND ct.company_id = c.id),
		        (SELECT COUNT(*)::int FROM lc_leads l WHERE l.user_id = c.user_id AND l.company_id = c.id),
		        (SELECT COUNT(*)::int FROM lc_workstreams w WHERE w.user_id = c.user_id AND w.company_id = c.id),
		        (SELECT COUNT(*)::int FROM lc_projects p WHERE p.user_id = c.user_id AND p.company_id = c.id),
		        (SELECT COUNT(*)::int FROM lc_action_items a WHERE a.user_id = c.user_id AND a.linked_company_id = c.id AND a.status IN ('open','bezig','wacht_op_klant')),
		        (SELECT COUNT(*)::int FROM lc_dossier_documents d WHERE d.user_id = c.user_id AND d.company_id = c.id)
		 FROM lc_companies c
		 WHERE c.user_id = $1
		   AND ($2 = '' OR LOWER(c.naam) LIKE '%' || $2 || '%' OR LOWER(COALESCE(c.website, '')) LIKE '%' || $2 || '%')
		 ORDER BY c.updated_at DESC
		 LIMIT $3`,
		userID, needle, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanCompany)
}

func (s *LaventeCareStore) GetCompany(ctx context.Context, userID string, id uuid.UUID) (*model.LCCompany, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT c.id, c.user_id, c.naam, c.website, c.sector, c.status, c.relatie_type,
		        c.notities, c.laatste_contact, c.volgende_actie, c.created_at, c.updated_at,
		        (SELECT COUNT(*)::int FROM lc_contacts ct WHERE ct.user_id = c.user_id AND ct.company_id = c.id),
		        (SELECT COUNT(*)::int FROM lc_leads l WHERE l.user_id = c.user_id AND l.company_id = c.id),
		        (SELECT COUNT(*)::int FROM lc_workstreams w WHERE w.user_id = c.user_id AND w.company_id = c.id),
		        (SELECT COUNT(*)::int FROM lc_projects p WHERE p.user_id = c.user_id AND p.company_id = c.id),
		        (SELECT COUNT(*)::int FROM lc_action_items a WHERE a.user_id = c.user_id AND a.linked_company_id = c.id AND a.status IN ('open','bezig','wacht_op_klant')),
		        (SELECT COUNT(*)::int FROM lc_dossier_documents d WHERE d.user_id = c.user_id AND d.company_id = c.id)
		 FROM lc_companies c
		 WHERE c.user_id = $1 AND c.id = $2
		 LIMIT 1`,
		userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	companies, err := pgx.CollectRows(rows, scanCompany)
	if err != nil {
		return nil, err
	}
	if len(companies) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &companies[0], nil
}

func (s *LaventeCareStore) CreateCompany(ctx context.Context, userID string, input model.LCCompanyCreate) (*model.LCCompany, error) {
	name := strings.TrimSpace(input.Naam)
	if name == "" {
		return nil, pgx.ErrNoRows
	}
	now := time.Now().UTC()
	id := uuid.New()
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "actief"
	}
	relatieType := strings.TrimSpace(input.RelatieType)
	if relatieType == "" {
		relatieType = "prospect"
	}
	laatsteContact := parseDateTimePtr(input.LaatsteContact)

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO lc_companies (id, user_id, naam, website, sector, status, relatie_type,
		        notities, laatste_contact, volgende_actie, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)`,
		id, userID, name, cleanStringPtr(input.Website), cleanStringPtr(input.Sector),
		status, relatieType, cleanStringPtr(input.Notities), laatsteContact,
		cleanStringPtr(input.VolgendeActie), now)
	if err != nil {
		return nil, err
	}
	return s.GetCompany(ctx, userID, id)
}

func (s *LaventeCareStore) UpdateCompany(ctx context.Context, userID string, id uuid.UUID, input model.LCCompanyUpdate) error {
	now := time.Now().UTC()
	latestContact := parseDateTimePtr(input.LaatsteContact)
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_companies SET
			naam = COALESCE($3, naam),
			website = COALESCE($4, website),
			sector = COALESCE($5, sector),
			status = COALESCE($6, status),
			relatie_type = COALESCE($7, relatie_type),
			notities = COALESCE($8, notities),
			laatste_contact = COALESCE($9, laatste_contact),
			volgende_actie = COALESCE($10, volgende_actie),
			updated_at = $11
		 WHERE id = $1 AND user_id = $2`,
		id, userID, cleanStringPtr(input.Naam), cleanStringPtr(input.Website),
		cleanStringPtr(input.Sector), cleanStringPtr(input.Status), cleanStringPtr(input.RelatieType),
		cleanStringPtr(input.Notities), latestContact, cleanStringPtr(input.VolgendeActie), now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) ListContacts(ctx context.Context, userID string, companyID *uuid.UUID, limit int) ([]model.LCContact, error) {
	if limit <= 0 {
		limit = 30
	}
	if companyID != nil {
		rows, err := s.db.Pool.Query(ctx,
			`SELECT id, user_id, company_id, naam, email, telefoon, rol, is_primary,
			        notities, created_at, updated_at
			 FROM lc_contacts WHERE user_id = $1 AND company_id = $2
			 ORDER BY is_primary DESC, updated_at DESC LIMIT $3`,
			userID, *companyID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return pgx.CollectRows(rows, scanContact)
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, company_id, naam, email, telefoon, rol, is_primary,
		        notities, created_at, updated_at
		 FROM lc_contacts WHERE user_id = $1
		 ORDER BY updated_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanContact)
}

func (s *LaventeCareStore) CreateContact(ctx context.Context, userID string, input model.LCContactCreate) (*model.LCContact, error) {
	name := strings.TrimSpace(input.Naam)
	if name == "" {
		return nil, pgx.ErrNoRows
	}
	if input.CompanyID != nil {
		if _, err := s.GetCompany(ctx, userID, *input.CompanyID); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	id := uuid.New()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO lc_contacts (id, user_id, company_id, naam, email, telefoon, rol,
		        is_primary, notities, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`,
		id, userID, input.CompanyID, name, cleanStringPtr(input.Email), cleanStringPtr(input.Telefoon),
		cleanStringPtr(input.Rol), input.IsPrimary, cleanStringPtr(input.Notities), now)
	if err != nil {
		return nil, err
	}
	if input.IsPrimary && input.CompanyID != nil {
		_ = s.clearOtherPrimaryContacts(ctx, userID, id, *input.CompanyID)
	}
	return s.GetContact(ctx, userID, id)
}

func (s *LaventeCareStore) GetContact(ctx context.Context, userID string, id uuid.UUID) (*model.LCContact, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, company_id, naam, email, telefoon, rol, is_primary,
		        notities, created_at, updated_at
		 FROM lc_contacts WHERE user_id = $1 AND id = $2 LIMIT 1`,
		userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	contacts, err := pgx.CollectRows(rows, scanContact)
	if err != nil {
		return nil, err
	}
	if len(contacts) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &contacts[0], nil
}

func (s *LaventeCareStore) UpdateContact(ctx context.Context, userID string, id uuid.UUID, input model.LCContactUpdate) error {
	if input.CompanyID != nil {
		if _, err := s.GetCompany(ctx, userID, *input.CompanyID); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_contacts SET
			company_id = COALESCE($3, company_id),
			naam = COALESCE($4, naam),
			email = COALESCE($5, email),
			telefoon = COALESCE($6, telefoon),
			rol = COALESCE($7, rol),
			is_primary = COALESCE($8, is_primary),
			notities = COALESCE($9, notities),
			updated_at = $10
		 WHERE id = $1 AND user_id = $2`,
		id, userID, input.CompanyID, cleanStringPtr(input.Naam), cleanStringPtr(input.Email),
		cleanStringPtr(input.Telefoon), cleanStringPtr(input.Rol), input.IsPrimary,
		cleanStringPtr(input.Notities), now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if input.IsPrimary != nil && *input.IsPrimary && input.CompanyID != nil {
		_ = s.clearOtherPrimaryContacts(ctx, userID, id, *input.CompanyID)
	}
	return nil
}

func (s *LaventeCareStore) ResolveCompanyReference(ctx context.Context, userID string, companyID *uuid.UUID, companyName, website *string) (*uuid.UUID, *string, error) {
	if companyID != nil {
		company, err := s.GetCompany(ctx, userID, *companyID)
		if err != nil {
			return nil, nil, err
		}
		return &company.ID, &company.Naam, nil
	}

	name := strings.TrimSpace(deref(companyName))
	if name == "" {
		return nil, nil, nil
	}

	web := cleanStringPtr(website)
	var existing uuid.UUID
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id FROM lc_companies
		 WHERE user_id = $1
		   AND (LOWER(TRIM(naam)) = LOWER(TRIM($2))
		        OR ($3::text IS NOT NULL AND website IS NOT NULL AND LOWER(TRIM(website)) = LOWER(TRIM($3))))
		 ORDER BY updated_at DESC
		 LIMIT 1`,
		userID, name, web).Scan(&existing)
	if err == nil {
		now := time.Now().UTC()
		_, _ = s.db.Pool.Exec(ctx,
			`UPDATE lc_companies SET
				website = COALESCE($3, website),
				updated_at = $4
			 WHERE user_id = $1 AND id = $2`,
			userID, existing, web, now)
		return &existing, &name, nil
	}
	if err != pgx.ErrNoRows {
		return nil, nil, err
	}

	company, err := s.CreateCompany(ctx, userID, model.LCCompanyCreate{
		Naam:        name,
		Website:     web,
		Status:      "actief",
		RelatieType: "prospect",
	})
	if err != nil {
		return nil, nil, err
	}
	return &company.ID, &company.Naam, nil
}

func (s *LaventeCareStore) clearOtherPrimaryContacts(ctx context.Context, userID string, keepID, companyID uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_contacts SET is_primary = false
		 WHERE user_id = $1 AND company_id = $2 AND id <> $3`,
		userID, companyID, keepID)
	return err
}

// ─── Leads ───────────────────────────────────────────────────────────────────

func (s *LaventeCareStore) GetLead(ctx context.Context, userID string, id uuid.UUID) (*model.LCLead, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, company_id, contact_id, titel, bron, source_id, status,
		        fit_score, pijnpunt, prioriteit, volgende_stap, volgende_actie_datum,
		        created_at, updated_at
		 FROM lc_leads WHERE user_id = $1 AND id = $2
		 LIMIT 1`, userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	leads, err := pgx.CollectRows(rows, scanLead)
	if err != nil {
		return nil, err
	}
	if len(leads) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &leads[0], nil
}

func (s *LaventeCareStore) ListLeads(ctx context.Context, userID string, limit int) ([]model.LCLead, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, company_id, contact_id, titel, bron, source_id, status,
		        fit_score, pijnpunt, prioriteit, volgende_stap, volgende_actie_datum,
		        created_at, updated_at
		 FROM lc_leads WHERE user_id = $1
		 ORDER BY updated_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanLead)
}

func (s *LaventeCareStore) CreateLead(ctx context.Context, userID string, input model.LCLeadCreate) (*model.LCLead, error) {
	id := uuid.New()
	now := time.Now().UTC()
	bron := input.Bron
	if bron == "" {
		bron = "cockpit"
	}
	companyID, _, err := s.ResolveCompanyReference(ctx, userID, input.CompanyID, input.CompanyName, input.Website)
	if err != nil {
		return nil, err
	}
	if input.ContactID != nil {
		if _, err := s.GetContact(ctx, userID, *input.ContactID); err != nil {
			return nil, err
		}
	}

	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO lc_leads (id, user_id, company_id, contact_id, titel, bron, source_id, status, fit_score,
		        pijnpunt, prioriteit, volgende_stap, volgende_actie_datum, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'nieuw',$8,$9,$10,$11,$12,$13,$13)`,
		id, userID, companyID, input.ContactID, input.Titel, bron, input.SourceID, input.FitScore,
		input.Pijnpunt, input.Prioriteit, input.VolgendeStap, input.VolgendeActieDatum, now)
	if err != nil {
		return nil, err
	}

	return &model.LCLead{
		ID: id, UserID: userID, CompanyID: companyID, ContactID: input.ContactID, Titel: input.Titel, Bron: bron,
		SourceID: input.SourceID, Status: "nieuw", FitScore: input.FitScore,
		Pijnpunt: input.Pijnpunt, Prioriteit: input.Prioriteit,
		VolgendeStap: input.VolgendeStap, VolgendeActieDatum: input.VolgendeActieDatum,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *LaventeCareStore) UpdateLead(ctx context.Context, userID string, id uuid.UUID, input model.LCLeadUpdate) error {
	now := time.Now().UTC()
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_leads SET
			company_id = COALESCE($3, company_id),
			contact_id = COALESCE($4, contact_id),
			status = COALESCE($5, status),
			fit_score = COALESCE($6, fit_score),
			pijnpunt = COALESCE($7, pijnpunt),
			prioriteit = COALESCE($8, prioriteit),
			volgende_stap = COALESCE($9, volgende_stap),
			volgende_actie_datum = COALESCE($10, volgende_actie_datum),
			updated_at = $11
		 WHERE id = $1 AND user_id = $2`,
		id, userID, input.CompanyID, input.ContactID, input.Status, input.FitScore, input.Pijnpunt,
		input.Prioriteit, input.VolgendeStap, input.VolgendeActieDatum, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ─── Projects ────────────────────────────────────────────────────────────────

func (s *LaventeCareStore) ListProjects(ctx context.Context, userID string, limit int) ([]model.LCProject, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, company_id, lead_id, naam, fase, status,
		        waarde_indicatie, start_datum, deadline, samenvatting,
		        created_at, updated_at
		 FROM lc_projects WHERE user_id = $1
		 ORDER BY updated_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanProject)
}

func (s *LaventeCareStore) CreateProject(ctx context.Context, userID string, p model.LCProject) (*model.LCProject, error) {
	p.ID = uuid.New()
	p.UserID = userID
	p.CreatedAt = time.Now().UTC()
	p.UpdatedAt = p.CreatedAt
	if p.Fase == "" {
		p.Fase = "intake"
	}
	if p.Status == "" {
		p.Status = "actief"
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO lc_projects (id, user_id, company_id, lead_id, naam, fase, status,
		        waarde_indicatie, start_datum, deadline, samenvatting, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)`,
		p.ID, p.UserID, p.CompanyID, p.LeadID, p.Naam, p.Fase, p.Status,
		p.WaardeIndicatie, p.StartDatum, p.Deadline, p.Samenvatting, p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *LaventeCareStore) UpdateProject(ctx context.Context, userID string, id uuid.UUID, input model.LCProjectUpdate) error {
	now := time.Now().UTC()
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_projects SET
			company_id = COALESCE($3, company_id),
			fase = COALESCE($4, fase),
			status = COALESCE($5, status),
			waarde_indicatie = COALESCE($6, waarde_indicatie),
			start_datum = COALESCE($7, start_datum),
			deadline = COALESCE($8, deadline),
			samenvatting = COALESCE($9, samenvatting),
			updated_at = $10
		 WHERE id = $1 AND user_id = $2`,
		id, userID, input.CompanyID, input.Fase, input.Status, input.WaardeIndicatie,
		input.StartDatum, input.Deadline, input.Samenvatting, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ConvertLeadToProject creates a project from a lead and marks the lead as won.
func (s *LaventeCareStore) ConvertLeadToProject(ctx context.Context, userID string, input model.LCConvertLeadToProject) (*model.LCProject, error) {
	lead, err := s.GetLead(ctx, userID, input.LeadID)
	if err != nil {
		return nil, err
	}
	// Mark lead as won
	won := "gewonnen"
	if err := s.UpdateLead(ctx, userID, input.LeadID, model.LCLeadUpdate{Status: &won}); err != nil {
		return nil, err
	}

	fase := "intake"
	if input.Fase != nil {
		fase = *input.Fase
	}
	status := "actief"
	if input.Status != nil {
		status = *input.Status
	}

	return s.CreateProject(ctx, userID, model.LCProject{
		CompanyID:    lead.CompanyID,
		LeadID:       &input.LeadID,
		Naam:         input.Naam,
		Fase:         fase,
		Status:       status,
		Samenvatting: input.Samenvatting,
	})
}

// ─── Workstreams / Opdrachten ───────────────────────────────────────────────

func (s *LaventeCareStore) ListWorkstreams(ctx context.Context, userID string, limit int, includeClosed bool) ([]model.LCWorkstream, error) {
	statusClause := ``
	if !includeClosed {
		statusClause = ` AND status NOT IN ('afgerond','done','gesloten','gearchiveerd','omgezet_project')`
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, company_id, lead_id, project_id, titel, type, status,
		        prioriteit, klant_naam, bron, source_id, doel, scope, deliverable,
		        bevindingen, volgende_stap, deadline, geschatte_minuten,
		        waarde_indicatie, stack_tags, tags, completed_at, created_at, updated_at
		 FROM lc_workstreams WHERE user_id = $1`+statusClause+`
		 ORDER BY CASE WHEN deadline IS NULL THEN 1 ELSE 0 END, deadline ASC, updated_at DESC
		 LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanWorkstream)
}

func (s *LaventeCareStore) CreateWorkstream(ctx context.Context, userID string, input model.LCWorkstreamCreate) (*model.LCWorkstream, error) {
	id := uuid.New()
	now := time.Now().UTC()
	companyID, companyName, err := s.ResolveCompanyReference(ctx, userID, input.CompanyID, input.KlantNaam, nil)
	if err != nil {
		return nil, err
	}
	if input.KlantNaam == nil && companyName != nil {
		input.KlantNaam = companyName
	}
	workstreamType := strings.TrimSpace(input.Type)
	if workstreamType == "" {
		workstreamType = "advies"
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "nieuw"
	}
	priority := strings.TrimSpace(input.Prioriteit)
	if priority == "" {
		priority = "normaal"
	}
	bron := strings.TrimSpace(input.Bron)
	if bron == "" {
		bron = "cockpit"
	}
	stackTags := cleanTags(input.StackTags)
	tags := cleanTags(input.Tags)

	var completedAt *time.Time
	if isClosedStatus(status) {
		completedAt = &now
	}

	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO lc_workstreams (id, user_id, company_id, lead_id, project_id, titel, type, status,
		        prioriteit, klant_naam, bron, source_id, doel, scope, deliverable,
		        bevindingen, volgende_stap, deadline, geschatte_minuten, waarde_indicatie,
		        stack_tags, tags, completed_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$24)`,
		id, userID, companyID, input.LeadID, input.ProjectID, input.Titel, workstreamType, status,
		priority, input.KlantNaam, bron, input.SourceID, input.Doel, input.Scope,
		input.Deliverable, input.Bevindingen, input.VolgendeStap, input.Deadline,
		input.GeschatteMinuten, input.WaardeIndicatie, stackTags, tags, completedAt, now)
	if err != nil {
		return nil, err
	}

	return &model.LCWorkstream{
		ID:               id,
		UserID:           userID,
		CompanyID:        companyID,
		LeadID:           input.LeadID,
		ProjectID:        input.ProjectID,
		Titel:            input.Titel,
		Type:             workstreamType,
		Status:           status,
		Prioriteit:       priority,
		KlantNaam:        input.KlantNaam,
		Bron:             bron,
		SourceID:         input.SourceID,
		Doel:             input.Doel,
		Scope:            input.Scope,
		Deliverable:      input.Deliverable,
		Bevindingen:      input.Bevindingen,
		VolgendeStap:     input.VolgendeStap,
		Deadline:         input.Deadline,
		GeschatteMinuten: input.GeschatteMinuten,
		WaardeIndicatie:  input.WaardeIndicatie,
		StackTags:        stackTags,
		Tags:             tags,
		CompletedAt:      completedAt,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func (s *LaventeCareStore) UpdateWorkstream(ctx context.Context, userID string, id uuid.UUID, input model.LCWorkstreamUpdate) error {
	now := time.Now().UTC()
	stackTags := cleanTags(input.StackTags)
	tags := cleanTags(input.Tags)
	var stackTagsParam any
	var tagsParam any
	if input.StackTags != nil {
		stackTagsParam = stackTags
	}
	if input.Tags != nil {
		tagsParam = tags
	}

	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_workstreams SET
			company_id = COALESCE($3, company_id),
			type = COALESCE($4, type),
			status = COALESCE($5, status),
			prioriteit = COALESCE($6, prioriteit),
			klant_naam = COALESCE($7, klant_naam),
			doel = COALESCE($8, doel),
			scope = COALESCE($9, scope),
			deliverable = COALESCE($10, deliverable),
			bevindingen = COALESCE($11, bevindingen),
			volgende_stap = COALESCE($12, volgende_stap),
			deadline = COALESCE($13, deadline),
			geschatte_minuten = COALESCE($14, geschatte_minuten),
			waarde_indicatie = COALESCE($15, waarde_indicatie),
			stack_tags = COALESCE($16, stack_tags),
			tags = COALESCE($17, tags),
			completed_at = CASE
				WHEN $5 IN ('afgerond','done','gesloten','gearchiveerd','omgezet_project') THEN COALESCE(completed_at, $18)
				WHEN $5 IS NOT NULL THEN NULL
				ELSE completed_at
			END,
			updated_at = $18
		 WHERE id = $1 AND user_id = $2`,
		id, userID, input.CompanyID, input.Type, input.Status, input.Prioriteit, input.KlantNaam,
		input.Doel, input.Scope, input.Deliverable, input.Bevindingen,
		input.VolgendeStap, input.Deadline, input.GeschatteMinuten,
		input.WaardeIndicatie, stackTagsParam, tagsParam, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) ConvertWorkstreamToProject(ctx context.Context, userID string, input model.LCConvertWorkstreamToProject) (*model.LCProject, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, company_id, lead_id, project_id, titel, type, status,
		        prioriteit, klant_naam, bron, source_id, doel, scope, deliverable,
		        bevindingen, volgende_stap, deadline, geschatte_minuten,
		        waarde_indicatie, stack_tags, tags, completed_at, created_at, updated_at
		 FROM lc_workstreams WHERE id = $1 AND user_id = $2`,
		input.WorkstreamID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	workstreams, err := pgx.CollectRows(rows, scanWorkstream)
	if err != nil {
		return nil, err
	}
	if len(workstreams) == 0 {
		return nil, pgx.ErrNoRows
	}
	workstream := workstreams[0]

	fase := "intake"
	if input.Fase != nil {
		fase = *input.Fase
	}
	status := "actief"
	if input.Status != nil {
		status = *input.Status
	}
	name := strings.TrimSpace(input.Naam)
	if name == "" {
		name = workstream.Titel
	}
	summary := input.Samenvatting
	if summary == nil {
		parts := []string{deref(workstream.Doel), deref(workstream.Scope), deref(workstream.Bevindingen)}
		joined := strings.TrimSpace(strings.Join(nonEmpty(parts), "\n\n"))
		if joined != "" {
			summary = &joined
		}
	}

	project, err := s.CreateProject(ctx, userID, model.LCProject{
		CompanyID:       workstream.CompanyID,
		LeadID:          workstream.LeadID,
		Naam:            name,
		Fase:            fase,
		Status:          status,
		WaardeIndicatie: workstream.WaardeIndicatie,
		Deadline:        workstream.Deadline,
		Samenvatting:    summary,
	})
	if err != nil {
		return nil, err
	}
	done := "omgezet_project"
	if err := s.UpdateWorkstream(ctx, userID, input.WorkstreamID, model.LCWorkstreamUpdate{Status: &done}); err != nil {
		return nil, err
	}
	return project, nil
}

// ─── Action Items ────────────────────────────────────────────────────────────

func (s *LaventeCareStore) ListActions(ctx context.Context, userID string, limit int) ([]model.LCActionItem, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, source, source_id, title, summary, action_type,
		        status, priority, due_date, linked_lead_id, linked_project_id, linked_workstream_id,
		        linked_company_id, created_at, updated_at
		 FROM lc_action_items WHERE user_id = $1 AND status IN ('open','bezig','wacht_op_klant')
		 ORDER BY COALESCE(due_date, '9999-12-31'), updated_at DESC
		 LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanAction)
}

func (s *LaventeCareStore) CreateAction(ctx context.Context, userID string, input model.LCActionCreate) (*model.LCActionItem, error) {
	id := uuid.New()
	now := time.Now().UTC()
	source := input.Source
	if source == "" {
		source = "handmatig"
	}
	actionType := input.ActionType
	if actionType == "" {
		actionType = "opvolgen"
	}
	priority := input.Priority
	if priority == "" {
		priority = "normaal"
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO lc_action_items (id, user_id, source, source_id, title, summary,
		        action_type, status, priority, due_date, linked_lead_id, linked_project_id, linked_workstream_id,
		        linked_company_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'open',$8,$9,$10,$11,$12,$13,$14,$14)`,
		id, userID, source, input.SourceID, input.Title, input.Summary,
		actionType, priority, input.DueDate, input.LinkedLeadID, input.LinkedProjectID,
		input.LinkedWorkstreamID, input.LinkedCompanyID, now)
	if err != nil {
		return nil, err
	}

	return &model.LCActionItem{
		ID: id, UserID: userID, Source: source, SourceID: input.SourceID,
		Title: input.Title, Summary: input.Summary, ActionType: actionType,
		Status: "open", Priority: priority, DueDate: input.DueDate,
		LinkedLeadID: input.LinkedLeadID, LinkedProjectID: input.LinkedProjectID,
		LinkedWorkstreamID: input.LinkedWorkstreamID,
		LinkedCompanyID:    input.LinkedCompanyID,
		CreatedAt:          now, UpdatedAt: now,
	}, nil
}

func (s *LaventeCareStore) UpdateActionStatus(ctx context.Context, userID string, id uuid.UUID, status string) error {
	now := time.Now().UTC()
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_action_items SET status = $3, updated_at = $4
		 WHERE id = $1 AND user_id = $2`, id, userID, status, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ─── Documents ───────────────────────────────────────────────────────────────

func (s *LaventeCareStore) ListDocuments(ctx context.Context, userID string) ([]model.LCDocument, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, document_key, titel, categorie, fase, versie,
		        source_path, samenvatting, tags, created_at, updated_at
		 FROM lc_documents WHERE user_id = $1
		 ORDER BY categorie, titel`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanDocument)
}

func (s *LaventeCareStore) SearchDocuments(ctx context.Context, userID string, query string, limit int) ([]model.LCDocument, error) {
	if query == "" {
		rows, err := s.db.Pool.Query(ctx,
			`SELECT id, user_id, document_key, titel, categorie, fase, versie,
			        source_path, samenvatting, tags, created_at, updated_at
			 FROM lc_documents WHERE user_id = $1
			 ORDER BY categorie, titel LIMIT $2`, userID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return pgx.CollectRows(rows, scanDocument)
	}

	search := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, document_key, titel, categorie, fase, versie,
		        source_path, samenvatting, tags, created_at, updated_at
		 FROM lc_documents 
		 WHERE user_id = $1 AND (LOWER(titel) LIKE $2 OR LOWER(samenvatting) LIKE $2 OR LOWER(categorie) LIKE $2)
		 ORDER BY categorie, titel LIMIT $3`, userID, search, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanDocument)
}

func (s *LaventeCareStore) SeedDocuments(ctx context.Context, userID string, docs []model.LCDocument) (inserted, updated int, err error) {
	now := time.Now().UTC()
	for _, doc := range docs {
		var wasInserted bool
		uErr := s.db.Pool.QueryRow(ctx,
			`INSERT INTO lc_documents (id, user_id, document_key, titel, categorie, fase, versie,
			        source_path, samenvatting, tags, created_at, updated_at)
			 VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
			 ON CONFLICT (user_id, document_key) DO UPDATE SET
			        titel = EXCLUDED.titel, categorie = EXCLUDED.categorie,
			        fase = EXCLUDED.fase, versie = EXCLUDED.versie,
			        source_path = EXCLUDED.source_path, samenvatting = EXCLUDED.samenvatting,
			        tags = EXCLUDED.tags, updated_at = EXCLUDED.updated_at
			 RETURNING xmax = 0`,
			userID, doc.DocumentKey, doc.Titel, doc.Categorie, doc.Fase, doc.Versie,
			doc.SourcePath, doc.Samenvatting, doc.Tags, now).Scan(&wasInserted)
		if uErr != nil {
			return inserted, updated, uErr
		}
		if wasInserted {
			inserted++
		} else {
			updated++
		}
	}
	return inserted, updated, nil
}

// ─── Dossier Documents ──────────────────────────────────────────────────────

func (s *LaventeCareStore) ListDossierDocuments(ctx context.Context, userID string, limit int, leadID, projectID, workstreamID, companyID *uuid.UUID) ([]model.LCDossierDocument, error) {
	base := `SELECT id, user_id, document_key, titel, template_label, context_type,
		        context_id, context_title, lead_id, project_id, workstream_id, company_id, pdf_url, theme,
		        delivery, notes, generated_at, created_at
		 FROM lc_dossier_documents
		 WHERE user_id = $1`

	if leadID != nil {
		rows, err := s.db.Pool.Query(ctx, base+` AND lead_id = $2 ORDER BY created_at DESC LIMIT $3`, userID, *leadID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return pgx.CollectRows(rows, scanDossierDocument)
	}

	if projectID != nil {
		rows, err := s.db.Pool.Query(ctx, base+` AND project_id = $2 ORDER BY created_at DESC LIMIT $3`, userID, *projectID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return pgx.CollectRows(rows, scanDossierDocument)
	}

	if workstreamID != nil {
		rows, err := s.db.Pool.Query(ctx, base+` AND workstream_id = $2 ORDER BY created_at DESC LIMIT $3`, userID, *workstreamID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return pgx.CollectRows(rows, scanDossierDocument)
	}

	if companyID != nil {
		rows, err := s.db.Pool.Query(ctx, base+` AND company_id = $2 ORDER BY created_at DESC LIMIT $3`, userID, *companyID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return pgx.CollectRows(rows, scanDossierDocument)
	}

	rows, err := s.db.Pool.Query(ctx, base+` ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanDossierDocument)
}

func (s *LaventeCareStore) CountDossierDocuments(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM lc_dossier_documents WHERE user_id = $1`,
		userID,
	).Scan(&count)
	return count, err
}

func (s *LaventeCareStore) ListActivityEvents(ctx context.Context, userID string, limit int, companyID *uuid.UUID) ([]model.LCActivityEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}

	query := `SELECT e.id, e.user_id, e.company_id, e.contact_id, e.lead_id,
		        e.project_id, e.workstream_id, e.action_item_id, e.event_type, e.channel,
		        e.title, e.body, e.occurred_at, e.created_at, e.updated_at,
		        c.naam, ct.naam, p.naam, w.titel
		   FROM lc_activity_events e
		   JOIN lc_companies c ON c.id = e.company_id AND c.user_id = e.user_id
		   LEFT JOIN lc_contacts ct ON ct.id = e.contact_id AND ct.user_id = e.user_id
		   LEFT JOIN lc_projects p ON p.id = e.project_id AND p.user_id = e.user_id
		   LEFT JOIN lc_workstreams w ON w.id = e.workstream_id AND w.user_id = e.user_id
		  WHERE e.user_id = $1`
	args := []any{userID}
	if companyID != nil {
		args = append(args, *companyID)
		query += ` AND e.company_id = $2`
	}
	args = append(args, limit)
	query += ` ORDER BY e.occurred_at DESC, e.created_at DESC LIMIT $` + strconv.Itoa(len(args))

	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanActivityEvent)
}

func (s *LaventeCareStore) CountActivityEvents(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM lc_activity_events WHERE user_id = $1`,
		userID,
	).Scan(&count)
	return count, err
}

func (s *LaventeCareStore) CreateActivityEvent(ctx context.Context, userID string, input model.LCActivityEventCreate) (*model.LCActivityEvent, error) {
	if err := s.validateActivityEventTarget(ctx, userID, input); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	occurredAt := now
	if parsed := parseDateTimePtr(input.OccurredAt); parsed != nil {
		occurredAt = parsed.UTC()
	}
	eventType := strings.TrimSpace(input.EventType)
	if eventType == "" {
		eventType = "notitie"
	}
	channel := strings.TrimSpace(input.Channel)
	if channel == "" {
		channel = "manual"
	}
	body := cleanStringPtr(input.Body)
	id := uuid.New()

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO lc_activity_events (id, user_id, company_id, contact_id, lead_id,
		        project_id, workstream_id, action_item_id, event_type, channel, title, body,
		        occurred_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$14)`,
		id, userID, input.CompanyID, input.ContactID, input.LeadID, input.ProjectID,
		input.WorkstreamID, input.ActionItemID, eventType, channel, strings.TrimSpace(input.Title),
		body, occurredAt, now)
	if err != nil {
		return nil, err
	}

	if shouldActivityUpdateLastContact(eventType) {
		_, _ = s.db.Pool.Exec(ctx,
			`UPDATE lc_companies
			    SET laatste_contact = $1, updated_at = $2
			  WHERE user_id = $3 AND id = $4`,
			occurredAt, now, userID, input.CompanyID)
	}

	return &model.LCActivityEvent{
		ID:           id,
		UserID:       userID,
		CompanyID:    input.CompanyID,
		ContactID:    input.ContactID,
		LeadID:       input.LeadID,
		ProjectID:    input.ProjectID,
		WorkstreamID: input.WorkstreamID,
		ActionItemID: input.ActionItemID,
		EventType:    eventType,
		Channel:      channel,
		Title:        strings.TrimSpace(input.Title),
		Body:         body,
		OccurredAt:   occurredAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (s *LaventeCareStore) CreateDossierDocument(ctx context.Context, userID string, input model.LCDossierDocumentCreate) (*model.LCDossierDocument, error) {
	companyID, err := s.resolveDossierCompanyID(ctx, userID, input.CompanyID, input.LeadID, input.ProjectID, input.WorkstreamID)
	if err != nil {
		return nil, err
	}
	if err := s.validateDossierDocumentTarget(ctx, userID, companyID, input.LeadID, input.ProjectID, input.WorkstreamID); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	id := uuid.New()
	contextType := strings.TrimSpace(input.ContextType)
	if contextType == "" {
		contextType = "manual"
	}
	theme := strings.TrimSpace(input.Theme)
	if theme == "" {
		theme = "screen"
	}
	delivery := strings.TrimSpace(input.Delivery)
	if delivery == "" {
		delivery = "inline"
	}

	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO lc_dossier_documents (id, user_id, document_key, titel, template_label,
		        context_type, context_id, context_title, lead_id, project_id, workstream_id, company_id, pdf_url,
		        theme, delivery, notes, generated_at, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17)`,
		id, userID, input.DocumentKey, input.Titel, input.TemplateLabel,
		contextType, input.ContextID, input.ContextTitle, input.LeadID, input.ProjectID,
		input.WorkstreamID, companyID, input.PDFURL, theme, delivery, input.Notes, now)
	if err != nil {
		return nil, err
	}

	return &model.LCDossierDocument{
		ID:            id,
		UserID:        userID,
		DocumentKey:   input.DocumentKey,
		Titel:         input.Titel,
		TemplateLabel: input.TemplateLabel,
		ContextType:   contextType,
		ContextID:     input.ContextID,
		ContextTitle:  input.ContextTitle,
		LeadID:        input.LeadID,
		ProjectID:     input.ProjectID,
		WorkstreamID:  input.WorkstreamID,
		CompanyID:     companyID,
		PDFURL:        input.PDFURL,
		Theme:         theme,
		Delivery:      delivery,
		Notes:         input.Notes,
		GeneratedAt:   now,
		CreatedAt:     now,
	}, nil
}

func (s *LaventeCareStore) validateDossierDocumentTarget(ctx context.Context, userID string, companyID, leadID, projectID, workstreamID *uuid.UUID) error {
	if companyID != nil {
		var exists bool
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM lc_companies WHERE user_id = $1 AND id = $2)`,
			userID, *companyID,
		).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
	}

	if leadID != nil {
		var exists bool
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM lc_leads WHERE user_id = $1 AND id = $2)`,
			userID, *leadID,
		).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
	}

	if projectID != nil {
		var exists bool
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM lc_projects WHERE user_id = $1 AND id = $2)`,
			userID, *projectID,
		).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
	}

	if workstreamID != nil {
		var exists bool
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM lc_workstreams WHERE user_id = $1 AND id = $2)`,
			userID, *workstreamID,
		).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
	}

	return nil
}

func (s *LaventeCareStore) validateActivityEventTarget(ctx context.Context, userID string, input model.LCActivityEventCreate) error {
	var companyExists bool
	if err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM lc_companies WHERE user_id = $1 AND id = $2)`,
		userID, input.CompanyID,
	).Scan(&companyExists); err != nil {
		return err
	}
	if !companyExists {
		return pgx.ErrNoRows
	}

	check := func(query string, id *uuid.UUID) error {
		if id == nil {
			return nil
		}
		var exists bool
		if err := s.db.Pool.QueryRow(ctx, query, userID, *id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
		return nil
	}

	if err := check(`SELECT EXISTS (SELECT 1 FROM lc_contacts WHERE user_id = $1 AND id = $2)`, input.ContactID); err != nil {
		return err
	}
	if err := check(`SELECT EXISTS (SELECT 1 FROM lc_leads WHERE user_id = $1 AND id = $2)`, input.LeadID); err != nil {
		return err
	}
	if err := check(`SELECT EXISTS (SELECT 1 FROM lc_projects WHERE user_id = $1 AND id = $2)`, input.ProjectID); err != nil {
		return err
	}
	if err := check(`SELECT EXISTS (SELECT 1 FROM lc_workstreams WHERE user_id = $1 AND id = $2)`, input.WorkstreamID); err != nil {
		return err
	}
	if err := check(`SELECT EXISTS (SELECT 1 FROM lc_action_items WHERE user_id = $1 AND id = $2)`, input.ActionItemID); err != nil {
		return err
	}
	return nil
}

func (s *LaventeCareStore) resolveDossierCompanyID(ctx context.Context, userID string, companyID, leadID, projectID, workstreamID *uuid.UUID) (*uuid.UUID, error) {
	if companyID != nil {
		return companyID, nil
	}
	if leadID != nil {
		var id *uuid.UUID
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT company_id FROM lc_leads WHERE user_id = $1 AND id = $2`,
			userID, *leadID,
		).Scan(&id); err != nil && err != pgx.ErrNoRows {
			return nil, err
		} else if id != nil {
			return id, nil
		}
	}
	if workstreamID != nil {
		var id *uuid.UUID
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT company_id FROM lc_workstreams WHERE user_id = $1 AND id = $2`,
			userID, *workstreamID,
		).Scan(&id); err != nil && err != pgx.ErrNoRows {
			return nil, err
		} else if id != nil {
			return id, nil
		}
	}
	if projectID != nil {
		var id *uuid.UUID
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT company_id FROM lc_projects WHERE user_id = $1 AND id = $2`,
			userID, *projectID,
		).Scan(&id); err != nil && err != pgx.ErrNoRows {
			return nil, err
		} else if id != nil {
			return id, nil
		}
	}
	return nil, nil
}

// ─── Billing: quotes, hours and invoices ────────────────────────────────────

func (s *LaventeCareStore) GetBilling(ctx context.Context, userID string, limit int, companyID *uuid.UUID) (*model.LCBilling, error) {
	if limit <= 0 {
		limit = 40
	}
	quotes, err := s.ListQuotes(ctx, userID, limit, companyID)
	if err != nil {
		return nil, err
	}
	quoteLines, err := s.ListQuoteLines(ctx, userID, companyID)
	if err != nil {
		return nil, err
	}
	timeEntries, err := s.ListTimeEntries(ctx, userID, limit, companyID)
	if err != nil {
		return nil, err
	}
	invoices, err := s.ListInvoices(ctx, userID, limit, companyID)
	if err != nil {
		return nil, err
	}
	invoiceLines, err := s.ListInvoiceLines(ctx, userID, companyID)
	if err != nil {
		return nil, err
	}

	summary := model.LCBillingSummary{
		Quotes:              len(quotes),
		TimeEntries:         len(timeEntries),
		Invoices:            len(invoices),
		DefaultProvider:     "bunq",
		BunqReady:           bunqBillingConfigured(),
		NextStepDescription: "Maak een conceptfactuur vanuit uren en activeer daarna bunq betaalverzoeken met bevestiging.",
	}
	for _, quote := range quotes {
		if quote.Status != "vervallen" && quote.Status != "geweigerd" && quote.Status != "geaccepteerd" {
			summary.OpenQuotes++
		}
	}
	for _, entry := range timeEntries {
		if entry.Billable {
			summary.BillableMinutes += entry.Minutes
		}
		if entry.Billable && entry.InvoiceID == nil && entry.Status != "afgeschreven" {
			summary.UninvoicedMinutes += entry.Minutes
		}
	}
	for _, invoice := range invoices {
		if invoice.Status != "betaald" && invoice.Status != "geannuleerd" {
			summary.OpenInvoices++
			summary.OutstandingCents += maxInt(invoice.TotalCents-invoice.PaidCents, 0)
		}
		summary.PaidCents += invoice.PaidCents
	}

	return &model.LCBilling{
		Summary:      summary,
		Quotes:       quotes,
		QuoteLines:   quoteLines,
		TimeEntries:  timeEntries,
		Invoices:     invoices,
		InvoiceLines: invoiceLines,
	}, nil
}

func bunqBillingConfigured() bool {
	return strings.TrimSpace(os.Getenv("BUNQ_API_KEY")) != "" &&
		strings.TrimSpace(os.Getenv("BUNQ_USER_ID")) != "" &&
		strings.TrimSpace(os.Getenv("BUNQ_MONETARY_ACCOUNT_ID")) != ""
}

func (s *LaventeCareStore) ListQuotes(ctx context.Context, userID string, limit int, companyID *uuid.UUID) ([]model.LCQuote, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT q.id, q.user_id, q.company_id, q.project_id, q.workstream_id,
		        q.quote_number, q.titel, q.status, q.issue_date::text, q.valid_until::text,
		        q.currency, q.subtotal_cents, q.vat_rate_bps, q.vat_cents, q.total_cents,
		        q.accepted_at, q.notes, q.created_at, q.updated_at,
		        c.naam, p.naam, w.titel
		   FROM lc_quotes q
		   LEFT JOIN lc_companies c ON c.id = q.company_id
		   LEFT JOIN lc_projects p ON p.id = q.project_id
		   LEFT JOIN lc_workstreams w ON w.id = q.workstream_id
		  WHERE q.user_id = $1
		    AND ($2::uuid IS NULL OR q.company_id = $2)
		  ORDER BY q.created_at DESC
		  LIMIT $3`,
		userID, companyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanQuote)
}

func (s *LaventeCareStore) ListQuoteLines(ctx context.Context, userID string, companyID *uuid.UUID) ([]model.LCQuoteLine, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT l.id, l.quote_id, l.user_id, l.description, l.quantity,
		        l.unit_amount_cents, l.total_cents, l.sort_order
		   FROM lc_quote_lines l
		   JOIN lc_quotes q ON q.id = l.quote_id
		  WHERE l.user_id = $1
		    AND ($2::uuid IS NULL OR q.company_id = $2)
		  ORDER BY q.created_at DESC, l.sort_order ASC
		  LIMIT 200`,
		userID, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanQuoteLine)
}

func (s *LaventeCareStore) ListQuoteLinesByQuote(ctx context.Context, userID string, quoteID uuid.UUID) ([]model.LCQuoteLine, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT l.id, l.quote_id, l.user_id, l.description, l.quantity,
		        l.unit_amount_cents, l.total_cents, l.sort_order
		   FROM lc_quote_lines l
		   JOIN lc_quotes q ON q.id = l.quote_id
		  WHERE l.user_id = $1 AND q.user_id = $1 AND l.quote_id = $2
		  ORDER BY l.sort_order ASC`,
		userID, quoteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanQuoteLine)
}

func (s *LaventeCareStore) CreateQuote(ctx context.Context, userID string, input model.LCQuoteCreate) (*model.LCQuote, error) {
	if strings.TrimSpace(input.Titel) == "" {
		return nil, pgx.ErrNoRows
	}
	companyID, err := s.resolveBillingCompanyID(ctx, userID, input.CompanyID, input.ProjectID, input.WorkstreamID, nil)
	if err != nil {
		return nil, err
	}
	if err := s.validateBillingTarget(ctx, userID, companyID, input.ProjectID, input.WorkstreamID, nil); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	status := cleanStatus(input.Status, "concept")
	issueDate := cleanDateValue(input.IssueDate, now.Format("2006-01-02"))
	currency := cleanCurrency(input.Currency)
	vatRate := 2100
	if input.VatRateBps != nil {
		vatRate = *input.VatRateBps
	}
	subtotal := 0
	cleanLines := cleanQuoteLines(input.Lines)
	for _, line := range cleanLines {
		subtotal += line.TotalCents
	}
	vat := vatCents(subtotal, vatRate)
	total := subtotal + vat
	number, err := s.nextLCNumber(ctx, userID, "lc_quotes", "quote_number", "LC-OFF")
	if err != nil {
		return nil, err
	}
	id := uuid.New()

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`INSERT INTO lc_quotes (id, user_id, company_id, project_id, workstream_id,
		        quote_number, titel, status, issue_date, valid_until, currency,
		        subtotal_cents, vat_rate_bps, vat_cents, total_cents, notes, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17)`,
		id, userID, companyID, input.ProjectID, input.WorkstreamID, number, strings.TrimSpace(input.Titel),
		status, issueDate, cleanDatePtr(input.ValidUntil), currency, subtotal, vatRate, vat, total,
		cleanStringPtr(input.Notes), now)
	if err != nil {
		return nil, err
	}
	for idx, line := range cleanLines {
		sortOrder := line.SortOrder
		if sortOrder == 0 {
			sortOrder = idx + 1
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO lc_quote_lines (id, quote_id, user_id, description, quantity,
			        unit_amount_cents, total_cents, sort_order)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			uuid.New(), id, userID, line.Description, line.Quantity, line.UnitAmountCents,
			line.TotalCents, sortOrder); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetQuote(ctx, userID, id)
}

func (s *LaventeCareStore) GetQuote(ctx context.Context, userID string, id uuid.UUID) (*model.LCQuote, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT q.id, q.user_id, q.company_id, q.project_id, q.workstream_id,
		        q.quote_number, q.titel, q.status, q.issue_date::text, q.valid_until::text,
		        q.currency, q.subtotal_cents, q.vat_rate_bps, q.vat_cents, q.total_cents,
		        q.accepted_at, q.notes, q.created_at, q.updated_at,
		        c.naam, p.naam, w.titel
		   FROM lc_quotes q
		   LEFT JOIN lc_companies c ON c.id = q.company_id
		   LEFT JOIN lc_projects p ON p.id = q.project_id
		   LEFT JOIN lc_workstreams w ON w.id = q.workstream_id
		  WHERE q.user_id = $1 AND q.id = $2
		  LIMIT 1`,
		userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	quotes, err := pgx.CollectRows(rows, scanQuote)
	if err != nil {
		return nil, err
	}
	if len(quotes) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &quotes[0], nil
}

func (s *LaventeCareStore) UpdateQuoteStatus(ctx context.Context, userID string, id uuid.UUID, status string) error {
	status = cleanStatus(status, "")
	if status == "" {
		return pgx.ErrNoRows
	}
	now := time.Now().UTC()
	var acceptedAt *time.Time
	if status == "geaccepteerd" {
		acceptedAt = &now
	}
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_quotes
		    SET status = $3,
		        accepted_at = COALESCE($4, accepted_at),
		        updated_at = $5
		  WHERE id = $1 AND user_id = $2`,
		id, userID, status, acceptedAt, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) ListTimeEntries(ctx context.Context, userID string, limit int, companyID *uuid.UUID) ([]model.LCTimeEntry, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT t.id, t.user_id, t.company_id, t.project_id, t.workstream_id,
		        t.activity_event_id, t.invoice_id, t.source_type, t.source_id,
		        t.description, t.entry_date::text, t.minutes, t.hourly_rate_cents,
		        t.billable, t.status, t.created_at, t.updated_at,
		        c.naam, p.naam, w.titel
		   FROM lc_time_entries t
		   LEFT JOIN lc_companies c ON c.id = t.company_id
		   LEFT JOIN lc_projects p ON p.id = t.project_id
		   LEFT JOIN lc_workstreams w ON w.id = t.workstream_id
		  WHERE t.user_id = $1
		    AND ($2::uuid IS NULL OR t.company_id = $2)
		  ORDER BY t.entry_date DESC, t.created_at DESC
		  LIMIT $3`,
		userID, companyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanTimeEntry)
}

func (s *LaventeCareStore) CreateTimeEntry(ctx context.Context, userID string, input model.LCTimeEntryCreate) (*model.LCTimeEntry, error) {
	if strings.TrimSpace(input.Description) == "" || input.Minutes <= 0 {
		return nil, pgx.ErrNoRows
	}
	companyID, err := s.resolveBillingCompanyID(ctx, userID, input.CompanyID, input.ProjectID, input.WorkstreamID, input.ActivityEventID)
	if err != nil {
		return nil, err
	}
	if err := s.validateBillingTarget(ctx, userID, companyID, input.ProjectID, input.WorkstreamID, input.ActivityEventID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	id := uuid.New()
	rate := 7500
	if input.HourlyRateCents != nil && *input.HourlyRateCents >= 0 {
		rate = *input.HourlyRateCents
	}
	billable := true
	if input.Billable != nil {
		billable = *input.Billable
	}
	sourceType := cleanStatus(input.SourceType, "manual")
	status := cleanStatus(input.Status, "concept")
	entryDate := cleanDateValue(input.EntryDate, now.Format("2006-01-02"))

	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO lc_time_entries (id, user_id, company_id, project_id, workstream_id,
		        activity_event_id, source_type, source_id, description, entry_date, minutes,
		        hourly_rate_cents, billable, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$15)`,
		id, userID, companyID, input.ProjectID, input.WorkstreamID, input.ActivityEventID,
		sourceType, cleanStringPtr(input.SourceID), strings.TrimSpace(input.Description), entryDate,
		input.Minutes, rate, billable, status, now)
	if err != nil {
		return nil, err
	}
	return s.GetTimeEntry(ctx, userID, id)
}

func (s *LaventeCareStore) GetTimeEntry(ctx context.Context, userID string, id uuid.UUID) (*model.LCTimeEntry, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT t.id, t.user_id, t.company_id, t.project_id, t.workstream_id,
		        t.activity_event_id, t.invoice_id, t.source_type, t.source_id,
		        t.description, t.entry_date::text, t.minutes, t.hourly_rate_cents,
		        t.billable, t.status, t.created_at, t.updated_at,
		        c.naam, p.naam, w.titel
		   FROM lc_time_entries t
		   LEFT JOIN lc_companies c ON c.id = t.company_id
		   LEFT JOIN lc_projects p ON p.id = t.project_id
		   LEFT JOIN lc_workstreams w ON w.id = t.workstream_id
		  WHERE t.user_id = $1 AND t.id = $2
		  LIMIT 1`,
		userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries, err := pgx.CollectRows(rows, scanTimeEntry)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &entries[0], nil
}

func (s *LaventeCareStore) ListInvoices(ctx context.Context, userID string, limit int, companyID *uuid.UUID) ([]model.LCInvoice, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT i.id, i.user_id, i.company_id, i.project_id, i.workstream_id, i.quote_id,
		        i.invoice_number, i.status, i.issue_date::text, i.due_date::text,
		        i.currency, i.subtotal_cents, i.vat_rate_bps, i.vat_cents, i.total_cents,
		        i.paid_cents, i.payment_provider, i.provider_request_id, i.merchant_reference,
		        i.payment_url, i.sent_at, i.paid_at, i.notes, i.created_at, i.updated_at,
		        c.naam, p.naam, w.titel
		   FROM lc_invoices i
		   LEFT JOIN lc_companies c ON c.id = i.company_id
		   LEFT JOIN lc_projects p ON p.id = i.project_id
		   LEFT JOIN lc_workstreams w ON w.id = i.workstream_id
		  WHERE i.user_id = $1
		    AND ($2::uuid IS NULL OR i.company_id = $2)
		  ORDER BY i.created_at DESC
		  LIMIT $3`,
		userID, companyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanInvoice)
}

func (s *LaventeCareStore) ListInvoiceLines(ctx context.Context, userID string, companyID *uuid.UUID) ([]model.LCInvoiceLine, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT l.id, l.invoice_id, l.user_id, l.source_time_entry_id, l.description,
		        l.quantity_minutes, l.unit_amount_cents, l.vat_rate_bps, l.total_cents, l.sort_order
		   FROM lc_invoice_lines l
		   JOIN lc_invoices i ON i.id = l.invoice_id
		  WHERE l.user_id = $1
		    AND ($2::uuid IS NULL OR i.company_id = $2)
		  ORDER BY i.created_at DESC, l.sort_order ASC
		  LIMIT 200`,
		userID, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanInvoiceLine)
}

func (s *LaventeCareStore) CreateInvoice(ctx context.Context, userID string, input model.LCInvoiceCreate) (*model.LCInvoice, error) {
	if input.QuoteID != nil {
		existing, err := s.GetInvoiceByQuote(ctx, userID, *input.QuoteID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
		quote, err := s.GetQuote(ctx, userID, *input.QuoteID)
		if err != nil {
			return nil, err
		}
		if input.CompanyID == nil {
			input.CompanyID = quote.CompanyID
		}
		if input.ProjectID == nil {
			input.ProjectID = quote.ProjectID
		}
		if input.WorkstreamID == nil {
			input.WorkstreamID = quote.WorkstreamID
		}
	}

	companyID, err := s.resolveBillingCompanyID(ctx, userID, input.CompanyID, input.ProjectID, input.WorkstreamID, nil)
	if err != nil {
		return nil, err
	}
	if err := s.validateBillingTarget(ctx, userID, companyID, input.ProjectID, input.WorkstreamID, nil); err != nil {
		return nil, err
	}

	lines := cleanInvoiceLines(input.Lines)
	if len(input.TimeEntryIDs) > 0 {
		timeLines, resolvedCompanyID, err := s.invoiceLinesFromTimeEntries(ctx, userID, input.TimeEntryIDs)
		if err != nil {
			return nil, err
		}
		if companyID == nil {
			companyID = resolvedCompanyID
		}
		lines = append(lines, timeLines...)
	}
	if len(lines) == 0 {
		return nil, pgx.ErrNoRows
	}

	now := time.Now().UTC()
	issueDate := cleanDateValue(input.IssueDate, now.Format("2006-01-02"))
	dueDate := cleanDatePtr(input.DueDate)
	if dueDate == nil {
		dueDate = addDaysDatePtr(issueDate, 14)
	}
	status := cleanStatus(input.Status, "concept")
	currency := cleanCurrency(input.Currency)
	vatRate := 2100
	if input.VatRateBps != nil {
		vatRate = *input.VatRateBps
	}
	subtotal := 0
	for _, line := range lines {
		subtotal += line.TotalCents
	}
	vat := vatCents(subtotal, vatRate)
	total := subtotal + vat
	number, err := s.nextLCNumber(ctx, userID, "lc_invoices", "invoice_number", "LC-FAC")
	if err != nil {
		return nil, err
	}
	merchantReference := number
	id := uuid.New()

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`INSERT INTO lc_invoices (id, user_id, company_id, project_id, workstream_id, quote_id,
		        invoice_number, status, issue_date, due_date, currency, subtotal_cents,
		        vat_rate_bps, vat_cents, total_cents, paid_cents, payment_provider,
		        merchant_reference, notes, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,0,'bunq',$16,$17,$18,$18)`,
		id, userID, companyID, input.ProjectID, input.WorkstreamID, input.QuoteID, number, status,
		issueDate, dueDate, currency, subtotal, vatRate, vat, total, merchantReference,
		cleanStringPtr(input.Notes), now)
	if err != nil {
		return nil, err
	}

	for idx, line := range lines {
		sortOrder := line.SortOrder
		if sortOrder == 0 {
			sortOrder = idx + 1
		}
		lineVat := vatRate
		if line.VatRateBps != nil {
			lineVat = *line.VatRateBps
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO lc_invoice_lines (id, invoice_id, user_id, source_time_entry_id,
			        description, quantity_minutes, unit_amount_cents, vat_rate_bps, total_cents, sort_order)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			uuid.New(), id, userID, line.SourceTimeEntryID, strings.TrimSpace(line.Description),
			line.QuantityMinutes, line.UnitAmountCents, lineVat, line.TotalCents, sortOrder); err != nil {
			return nil, err
		}
	}

	if len(input.TimeEntryIDs) > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE lc_time_entries
			    SET invoice_id = $3, status = 'gefactureerd', updated_at = $4
			  WHERE user_id = $1 AND id = ANY($2)`,
			userID, input.TimeEntryIDs, id, now); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetInvoice(ctx, userID, id)
}

func (s *LaventeCareStore) CreateInvoiceFromQuote(ctx context.Context, userID string, quoteID uuid.UUID) (*model.LCInvoice, error) {
	existing, err := s.GetInvoiceByQuote(ctx, userID, quoteID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	quote, err := s.GetQuote(ctx, userID, quoteID)
	if err != nil {
		return nil, err
	}
	if quote.Status != "geaccepteerd" {
		return nil, ErrQuoteNotAccepted
	}

	quoteLines, err := s.ListQuoteLinesByQuote(ctx, userID, quoteID)
	if err != nil {
		return nil, err
	}
	if len(quoteLines) == 0 {
		return nil, ErrQuoteHasNoLines
	}

	lines := make([]model.LCInvoiceLineCreate, 0, len(quoteLines))
	for idx, line := range quoteLines {
		sortOrder := line.SortOrder
		if sortOrder == 0 {
			sortOrder = idx + 1
		}
		lines = append(lines, model.LCInvoiceLineCreate{
			Description:     line.Description,
			QuantityMinutes: maxInt(line.Quantity, 1) * 60,
			UnitAmountCents: line.UnitAmountCents,
			VatRateBps:      &quote.VatRateBps,
			TotalCents:      line.TotalCents,
			SortOrder:       sortOrder,
		})
	}

	notes := fmt.Sprintf("Aangemaakt vanuit offerte %s - %s.", quote.QuoteNumber, quote.Titel)
	if quote.Notes != nil && strings.TrimSpace(*quote.Notes) != "" {
		notes = notes + "\n\nOffertenotitie:\n" + strings.TrimSpace(*quote.Notes)
	}

	return s.CreateInvoice(ctx, userID, model.LCInvoiceCreate{
		CompanyID:    quote.CompanyID,
		ProjectID:    quote.ProjectID,
		WorkstreamID: quote.WorkstreamID,
		QuoteID:      &quote.ID,
		Status:       "concept",
		Currency:     quote.Currency,
		VatRateBps:   &quote.VatRateBps,
		Notes:        &notes,
		Lines:        lines,
	})
}

func (s *LaventeCareStore) GetInvoice(ctx context.Context, userID string, id uuid.UUID) (*model.LCInvoice, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT i.id, i.user_id, i.company_id, i.project_id, i.workstream_id, i.quote_id,
		        i.invoice_number, i.status, i.issue_date::text, i.due_date::text,
		        i.currency, i.subtotal_cents, i.vat_rate_bps, i.vat_cents, i.total_cents,
		        i.paid_cents, i.payment_provider, i.provider_request_id, i.merchant_reference,
		        i.payment_url, i.sent_at, i.paid_at, i.notes, i.created_at, i.updated_at,
		        c.naam, p.naam, w.titel
		   FROM lc_invoices i
		   LEFT JOIN lc_companies c ON c.id = i.company_id
		   LEFT JOIN lc_projects p ON p.id = i.project_id
		   LEFT JOIN lc_workstreams w ON w.id = i.workstream_id
		  WHERE i.user_id = $1 AND i.id = $2
		  LIMIT 1`,
		userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	invoices, err := pgx.CollectRows(rows, scanInvoice)
	if err != nil {
		return nil, err
	}
	if len(invoices) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &invoices[0], nil
}

func (s *LaventeCareStore) GetInvoiceByQuote(ctx context.Context, userID string, quoteID uuid.UUID) (*model.LCInvoice, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT i.id, i.user_id, i.company_id, i.project_id, i.workstream_id, i.quote_id,
		        i.invoice_number, i.status, i.issue_date::text, i.due_date::text,
		        i.currency, i.subtotal_cents, i.vat_rate_bps, i.vat_cents, i.total_cents,
		        i.paid_cents, i.payment_provider, i.provider_request_id, i.merchant_reference,
		        i.payment_url, i.sent_at, i.paid_at, i.notes, i.created_at, i.updated_at,
		        c.naam, p.naam, w.titel
		   FROM lc_invoices i
		   LEFT JOIN lc_companies c ON c.id = i.company_id
		   LEFT JOIN lc_projects p ON p.id = i.project_id
		   LEFT JOIN lc_workstreams w ON w.id = i.workstream_id
		  WHERE i.user_id = $1 AND i.quote_id = $2 AND i.status <> 'geannuleerd'
		  ORDER BY i.created_at DESC
		  LIMIT 1`,
		userID, quoteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	invoices, err := pgx.CollectRows(rows, scanInvoice)
	if err != nil {
		return nil, err
	}
	if len(invoices) == 0 {
		return nil, nil
	}
	return &invoices[0], nil
}

func (s *LaventeCareStore) UpdateInvoiceStatus(ctx context.Context, userID string, id uuid.UUID, input model.LCInvoiceStatusUpdate) error {
	invoice, err := s.GetInvoice(ctx, userID, id)
	if err != nil {
		return err
	}
	status := cleanStatus(input.Status, "")
	if status == "" {
		return pgx.ErrNoRows
	}
	now := time.Now().UTC()
	paidAt := parseDateTimePtr(input.PaidAt)
	sentAt := parseDateTimePtr(input.SentAt)
	paidCents := input.PaidCents
	if status == "betaald" {
		if paidAt == nil {
			paidAt = &now
		}
		if paidCents == nil {
			value := invoice.TotalCents
			paidCents = &value
		}
	}
	if status == "verstuurd" && sentAt == nil {
		sentAt = &now
	}
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_invoices
		    SET status = $3,
		        paid_cents = COALESCE($4, paid_cents),
		        payment_provider = COALESCE($5, payment_provider),
		        provider_request_id = COALESCE($6, provider_request_id),
		        merchant_reference = COALESCE($7, merchant_reference),
		        payment_url = COALESCE($8, payment_url),
		        sent_at = COALESCE($9, sent_at),
		        paid_at = COALESCE($10, paid_at),
		        updated_at = $11
		  WHERE id = $1 AND user_id = $2`,
		id, userID, status, paidCents, cleanStringPtr(input.PaymentProvider),
		cleanStringPtr(input.ProviderRequestID), cleanStringPtr(input.MerchantReference),
		cleanStringPtr(input.PaymentURL), sentAt, paidAt, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) invoiceLinesFromTimeEntries(ctx context.Context, userID string, ids []uuid.UUID) ([]model.LCInvoiceLineCreate, *uuid.UUID, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, company_id, description, minutes, hourly_rate_cents
		   FROM lc_time_entries
		  WHERE user_id = $1
		    AND id = ANY($2)
		    AND billable = true
		    AND invoice_id IS NULL
		  ORDER BY entry_date ASC, created_at ASC`,
		userID, ids)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	lines := make([]model.LCInvoiceLineCreate, 0, len(ids))
	var companyID *uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		var entryCompanyID *uuid.UUID
		var description string
		var minutes int
		var rate int
		if err := rows.Scan(&id, &entryCompanyID, &description, &minutes, &rate); err != nil {
			return nil, nil, err
		}
		if companyID == nil && entryCompanyID != nil {
			companyID = entryCompanyID
		}
		entryID := id
		lines = append(lines, model.LCInvoiceLineCreate{
			SourceTimeEntryID: &entryID,
			Description:       description,
			QuantityMinutes:   minutes,
			UnitAmountCents:   rate,
			TotalCents:        centsFromMinutes(minutes, rate),
		})
	}
	if rows.Err() != nil {
		return nil, nil, rows.Err()
	}
	if len(lines) != len(ids) {
		return nil, nil, pgx.ErrNoRows
	}
	return lines, companyID, nil
}

func (s *LaventeCareStore) resolveBillingCompanyID(ctx context.Context, userID string, companyID, projectID, workstreamID, activityEventID *uuid.UUID) (*uuid.UUID, error) {
	resolved, err := s.resolveDossierCompanyID(ctx, userID, companyID, nil, projectID, workstreamID)
	if err != nil || resolved != nil {
		return resolved, err
	}
	if activityEventID != nil {
		var id *uuid.UUID
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT company_id FROM lc_activity_events WHERE user_id = $1 AND id = $2`,
			userID, *activityEventID).Scan(&id); err != nil && err != pgx.ErrNoRows {
			return nil, err
		} else if id != nil {
			return id, nil
		}
	}
	return nil, nil
}

func (s *LaventeCareStore) validateBillingTarget(ctx context.Context, userID string, companyID, projectID, workstreamID, activityEventID *uuid.UUID) error {
	check := func(table string, id *uuid.UUID) error {
		if id == nil {
			return nil
		}
		var exists bool
		query := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE user_id = $1 AND id = $2)`, table)
		if err := s.db.Pool.QueryRow(ctx, query, userID, *id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
		return nil
	}
	if err := check("lc_companies", companyID); err != nil {
		return err
	}
	if err := check("lc_projects", projectID); err != nil {
		return err
	}
	if err := check("lc_workstreams", workstreamID); err != nil {
		return err
	}
	if err := check("lc_activity_events", activityEventID); err != nil {
		return err
	}
	return nil
}

func (s *LaventeCareStore) nextLCNumber(ctx context.Context, userID, table, column, prefix string) (string, error) {
	year := time.Now().UTC().Format("2006")
	needle := prefix + "-" + year + "-%"
	query := fmt.Sprintf(`SELECT COUNT(*)::int FROM %s WHERE user_id = $1 AND %s LIKE $2`, table, column)
	var count int
	if err := s.db.Pool.QueryRow(ctx, query, userID, needle).Scan(&count); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%04d", prefix, year, count+1), nil
}

// ─── Cockpit (aggregated dashboard) ──────────────────────────────────────────

func (s *LaventeCareStore) GetCockpit(ctx context.Context, userID string) (*model.LCCockpit, error) {
	companies, err := s.ListCompanies(ctx, userID, 30, "")
	if err != nil {
		return nil, err
	}
	contacts, err := s.ListContacts(ctx, userID, nil, 30)
	if err != nil {
		return nil, err
	}
	leads, err := s.ListLeads(ctx, userID, 30)
	if err != nil {
		return nil, err
	}
	projects, err := s.ListProjects(ctx, userID, 30)
	if err != nil {
		return nil, err
	}
	workstreams, err := s.ListWorkstreams(ctx, userID, 30, false)
	if err != nil {
		return nil, err
	}
	actions, err := s.ListActions(ctx, userID, 8)
	if err != nil {
		return nil, err
	}
	documents, err := s.ListDocuments(ctx, userID)
	if err != nil {
		return nil, err
	}
	dossierDocuments, err := s.ListDossierDocuments(ctx, userID, 8, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	dossierDocumentCount, err := s.CountDossierDocuments(ctx, userID)
	if err != nil {
		return nil, err
	}
	activityEvents, err := s.ListActivityEvents(ctx, userID, 30, nil)
	if err != nil {
		return nil, err
	}
	activityEventCount, err := s.CountActivityEvents(ctx, userID)
	if err != nil {
		return nil, err
	}

	activeLeads := filterOpen(leads, func(l model.LCLead) string { return l.Status })
	activeWorkstreams := filterOpen(workstreams, func(w model.LCWorkstream) string { return w.Status })
	activeProjects := filterOpen(projects, func(p model.LCProject) string { return p.Status })

	incidents, _ := s.listSlaIncidents(ctx, userID, 5)
	changes, _ := s.listChangeRequests(ctx, userID, 5)
	decisions, _ := s.listDecisions(ctx, userID, 5)

	// Business signals: scan emails for business-term matches
	signals := s.buildBusinessSignals(ctx, userID, companies, leads, projects, workstreams)
	// Follow-ups: leads, opdrachten and projects with upcoming deadlines
	followUps := s.buildFollowUps(companies, activeLeads, activeProjects, activeWorkstreams)

	return &model.LCCockpit{
		Summary: model.LCCockpitSummary{
			Companies:         len(companies),
			Contacts:          len(contacts),
			Leads:             len(leads),
			ActiveLeads:       len(activeLeads),
			Workstreams:       len(workstreams),
			ActiveWorkstreams: len(activeWorkstreams),
			Projects:          len(projects),
			ActiveProjects:    len(activeProjects),
			Documents:         len(documents),
			OpenIncidents:     len(incidents),
			OpenChanges:       len(changes),
			Decisions:         len(decisions),
			ActionItems:       len(actions),
			DossierDocuments:  dossierDocumentCount,
			ActivityEvents:    activityEventCount,
			DocumentsSeeded:   len(documents) > 0,
			BusinessSignals:   len(signals),
			FollowUps:         len(followUps),
		},
		Companies:         take(companies, 12),
		Contacts:          take(contacts, 12),
		ActiveLeads:       take(activeLeads, 8),
		ActiveWorkstreams: take(activeWorkstreams, 8),
		ActiveProjects:    take(activeProjects, 8),
		ActionItems:       actions,
		OpenIncidents:     incidents,
		OpenChanges:       changes,
		RecentDecisions:   decisions,
		DocumentCatalog:   documents,
		DossierDocuments:  dossierDocuments,
		ActivityEvents:    activityEvents,
		BusinessSignals:   signals,
		FollowUps:         followUps,
	}, nil
}

// buildBusinessSignals matches emails against CRM entity names.
func (s *LaventeCareStore) buildBusinessSignals(ctx context.Context, userID string, companies []model.LCCompany, leads []model.LCLead, projects []model.LCProject, workstreams []model.LCWorkstream) []model.LCBusinessSignal {
	terms := buildSignalTerms(companies, leads, projects, workstreams)
	if len(terms) == 0 {
		return nil
	}

	// Query recent emails
	rows, err := s.db.Pool.Query(ctx,
		`SELECT gmail_id, "from", subject, snippet, datum, is_gelezen
		 FROM emails WHERE user_id = $1 AND is_verwijderd = false
		 ORDER BY datum DESC LIMIT 80`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var signals []model.LCBusinessSignal
	for rows.Next() {
		var gmailID, from, subject, snippet, datum string
		var isGelezen bool
		if err := rows.Scan(&gmailID, &from, &subject, &snippet, &datum, &isGelezen); err != nil {
			continue
		}
		haystack := normalize(subject + " " + snippet + " " + from)
		matched := matchTerm(haystack, terms)
		if matched == "" {
			continue
		}
		urgency := "normaal"
		hint := "Review of dit bij een lead/project hoort."
		if !isGelezen {
			urgency = "hoog"
			hint = "Ongelezen zakelijke email opvolgen."
		}
		signals = append(signals, model.LCBusinessSignal{
			Source:      "email",
			ID:          gmailID,
			Title:       subject,
			Subtitle:    from,
			Date:        datum,
			MatchedTerm: matched,
			Urgency:     urgency,
			ActionHint:  hint,
		})
		if len(signals) >= 12 {
			break
		}
	}
	return signals
}

// buildFollowUps creates follow-up signals from companies, leads, opdrachten and projects with pending deadlines.
func (s *LaventeCareStore) buildFollowUps(companies []model.LCCompany, leads []model.LCLead, projects []model.LCProject, workstreams []model.LCWorkstream) []model.LCFollowUpSignal {
	today := time.Now().Format("2006-01-02")
	var followUps []model.LCFollowUpSignal

	for _, company := range companies {
		if company.VolgendeActie == nil {
			continue
		}
		priority := "normaal"
		if *company.VolgendeActie <= today {
			priority = "hoog"
		}
		followUps = append(followUps, model.LCFollowUpSignal{
			Source:     "company",
			ID:         company.ID.String(),
			Title:      company.Naam,
			Date:       *company.VolgendeActie,
			Status:     company.RelatieType,
			Priority:   priority,
			ActionHint: "Klantrelatie opvolgen en volgende stap vastleggen.",
		})
	}

	for _, lead := range leads {
		if lead.VolgendeActieDatum == nil {
			continue
		}
		priority := "normaal"
		if (lead.Prioriteit != nil && *lead.Prioriteit == "hoog") || *lead.VolgendeActieDatum <= today {
			priority = "hoog"
		}
		hint := "Bepaal de volgende concrete opvolgstap."
		if lead.VolgendeStap != nil {
			hint = *lead.VolgendeStap
		}
		followUps = append(followUps, model.LCFollowUpSignal{
			Source:     "lead",
			ID:         lead.ID.String(),
			Title:      lead.Titel,
			Date:       *lead.VolgendeActieDatum,
			Status:     lead.Status,
			Priority:   priority,
			ActionHint: hint,
		})
	}

	for _, project := range projects {
		if project.Deadline == nil {
			continue
		}
		priority := "normaal"
		if *project.Deadline <= today {
			priority = "hoog"
		}
		followUps = append(followUps, model.LCFollowUpSignal{
			Source:     "project",
			ID:         project.ID.String(),
			Title:      project.Naam,
			Date:       *project.Deadline,
			Status:     project.Fase,
			Priority:   priority,
			ActionHint: "Controleer scope, voortgang en eventuele blockers.",
		})
	}

	for _, workstream := range workstreams {
		if workstream.Deadline == nil {
			continue
		}
		priority := "normaal"
		if workstream.Prioriteit == "hoog" || *workstream.Deadline <= today {
			priority = "hoog"
		}
		hint := "Controleer bevindingen, scope en volgende stap."
		if workstream.VolgendeStap != nil {
			hint = *workstream.VolgendeStap
		}
		followUps = append(followUps, model.LCFollowUpSignal{
			Source:     "workstream",
			ID:         workstream.ID.String(),
			Title:      workstream.Titel,
			Date:       *workstream.Deadline,
			Status:     workstream.Status,
			Priority:   priority,
			ActionHint: hint,
		})
	}

	// Sort by date ascending, take first 10
	sortFollowUps(followUps)
	return take(followUps, 10)
}

// ─── Signal helpers ──────────────────────────────────────────────────────────

var staticSignalTerms = []string{
	"laventecare", "lavente care", "discovery", "blueprint", "voorstel",
	"proposal", "scope", "deliverables", "sla", "change request",
	"decision log", "verwerkersovereenkomst", "algemene voorwaarden",
	"privacyverklaring", "security one pager", "systeemanalyse",
}

func buildSignalTerms(companies []model.LCCompany, leads []model.LCLead, projects []model.LCProject, workstreams []model.LCWorkstream) []string {
	seen := make(map[string]bool)
	var terms []string
	add := func(t string) {
		n := normalize(t)
		if len(n) < 4 || seen[n] {
			return
		}
		seen[n] = true
		terms = append(terms, n)
	}
	for _, t := range staticSignalTerms {
		add(t)
	}
	for _, c := range companies {
		add(c.Naam)
		add(deref(c.Website))
	}
	for _, l := range leads {
		add(l.Titel)
	}
	for _, p := range projects {
		add(p.Naam)
	}
	for _, w := range workstreams {
		add(w.Titel)
		add(deref(w.KlantNaam))
		for _, tag := range w.StackTags {
			add(tag)
		}
		for _, tag := range w.Tags {
			add(tag)
		}
	}
	return terms
}

func matchTerm(haystack string, terms []string) string {
	for _, t := range terms {
		if strings.Contains(haystack, t) {
			return t
		}
	}
	return ""
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func sortFollowUps(items []model.LCFollowUpSignal) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Date < items[j].Date
	})
}

// ─── Supporting queries ──────────────────────────────────────────────────────

func (s *LaventeCareStore) listSlaIncidents(ctx context.Context, userID string, limit int) ([]model.LCSlaIncident, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, project_id, titel, prioriteit, status, kanaal,
		        gemeld_op, reactie_deadline, samenvatting, created_at, updated_at
		 FROM lc_sla_incidents WHERE user_id = $1 AND status IN ('open','in_behandeling','wacht_op_klant')
		 ORDER BY updated_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.LCSlaIncident, error) {
		var i model.LCSlaIncident
		err := row.Scan(&i.ID, &i.UserID, &i.ProjectID, &i.Titel, &i.Prioriteit,
			&i.Status, &i.Kanaal, &i.GemeldOp, &i.ReactieDeadline, &i.Samenvatting,
			&i.CreatedAt, &i.UpdatedAt)
		return i, err
	})
}

func (s *LaventeCareStore) CreateSlaIncident(ctx context.Context, userID string, input model.LCSlaIncident) (*model.LCSlaIncident, error) {
	now := time.Now().UTC()
	input.ID = uuid.New()
	input.UserID = userID
	input.CreatedAt = now
	input.UpdatedAt = now
	if input.GemeldOp.IsZero() {
		input.GemeldOp = now
	}
	if strings.TrimSpace(input.Prioriteit) == "" {
		input.Prioriteit = "P3"
	}
	if strings.TrimSpace(input.Status) == "" {
		input.Status = "open"
	}
	if strings.TrimSpace(input.Kanaal) == "" {
		input.Kanaal = "telegram"
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO lc_sla_incidents (id, user_id, project_id, titel, prioriteit,
		        status, kanaal, gemeld_op, reactie_deadline, samenvatting,
		        created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)`,
		input.ID, input.UserID, input.ProjectID, input.Titel, input.Prioriteit,
		input.Status, input.Kanaal, input.GemeldOp, input.ReactieDeadline,
		input.Samenvatting, input.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &input, nil
}

func (s *LaventeCareStore) listChangeRequests(ctx context.Context, userID string, limit int) ([]model.LCChangeRequest, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, project_id, titel, impact, planning_impact,
		        budget_impact, status, created_at, updated_at
		 FROM lc_change_requests WHERE user_id = $1 AND status IN ('nieuw','beoordeeld')
		 ORDER BY updated_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.LCChangeRequest, error) {
		var c model.LCChangeRequest
		err := row.Scan(&c.ID, &c.UserID, &c.ProjectID, &c.Titel, &c.Impact,
			&c.PlanningImpact, &c.BudgetImpact, &c.Status, &c.CreatedAt, &c.UpdatedAt)
		return c, err
	})
}

func (s *LaventeCareStore) CreateChangeRequest(ctx context.Context, userID string, input model.LCChangeRequest) (*model.LCChangeRequest, error) {
	now := time.Now().UTC()
	input.ID = uuid.New()
	input.UserID = userID
	input.CreatedAt = now
	input.UpdatedAt = now
	if strings.TrimSpace(input.Status) == "" {
		input.Status = "nieuw"
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO lc_change_requests (id, user_id, project_id, titel, impact,
		        planning_impact, budget_impact, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)`,
		input.ID, input.UserID, input.ProjectID, input.Titel, input.Impact,
		input.PlanningImpact, input.BudgetImpact, input.Status, input.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &input, nil
}

func (s *LaventeCareStore) listDecisions(ctx context.Context, userID string, limit int) ([]model.LCDecision, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, project_id, titel, besluit, reden, impact, status, datum, created_at
		 FROM lc_decisions WHERE user_id = $1
		 ORDER BY datum DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.LCDecision, error) {
		var d model.LCDecision
		err := row.Scan(&d.ID, &d.UserID, &d.ProjectID, &d.Titel, &d.Besluit,
			&d.Reden, &d.Impact, &d.Status, &d.Datum, &d.CreatedAt)
		return d, err
	})
}

func (s *LaventeCareStore) CreateDecision(ctx context.Context, userID string, input model.LCDecision) (*model.LCDecision, error) {
	now := time.Now().UTC()
	input.ID = uuid.New()
	input.UserID = userID
	input.CreatedAt = now
	if strings.TrimSpace(input.Status) == "" {
		input.Status = "genomen"
	}
	if strings.TrimSpace(input.Datum) == "" {
		input.Datum = now.Format("2006-01-02")
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO lc_decisions (id, user_id, project_id, titel, besluit, reden,
		        impact, status, datum, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		input.ID, input.UserID, input.ProjectID, input.Titel, input.Besluit,
		input.Reden, input.Impact, input.Status, input.Datum, input.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &input, nil
}

// ─── Row scanners ────────────────────────────────────────────────────────────

func scanCompany(row pgx.CollectableRow) (model.LCCompany, error) {
	var c model.LCCompany
	err := row.Scan(&c.ID, &c.UserID, &c.Naam, &c.Website, &c.Sector, &c.Status,
		&c.RelatieType, &c.Notities, &c.LaatsteContact, &c.VolgendeActie,
		&c.CreatedAt, &c.UpdatedAt, &c.Contacts, &c.Leads, &c.Workstreams,
		&c.Projects, &c.ActionItems, &c.DossierDocuments)
	return c, err
}

func scanContact(row pgx.CollectableRow) (model.LCContact, error) {
	var c model.LCContact
	err := row.Scan(&c.ID, &c.UserID, &c.CompanyID, &c.Naam, &c.Email, &c.Telefoon,
		&c.Rol, &c.IsPrimary, &c.Notities, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func scanLead(row pgx.CollectableRow) (model.LCLead, error) {
	var l model.LCLead
	err := row.Scan(&l.ID, &l.UserID, &l.CompanyID, &l.ContactID, &l.Titel,
		&l.Bron, &l.SourceID, &l.Status, &l.FitScore, &l.Pijnpunt,
		&l.Prioriteit, &l.VolgendeStap, &l.VolgendeActieDatum, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

func scanProject(row pgx.CollectableRow) (model.LCProject, error) {
	var p model.LCProject
	err := row.Scan(&p.ID, &p.UserID, &p.CompanyID, &p.LeadID, &p.Naam,
		&p.Fase, &p.Status, &p.WaardeIndicatie, &p.StartDatum, &p.Deadline,
		&p.Samenvatting, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func scanWorkstream(row pgx.CollectableRow) (model.LCWorkstream, error) {
	var w model.LCWorkstream
	err := row.Scan(&w.ID, &w.UserID, &w.CompanyID, &w.LeadID, &w.ProjectID,
		&w.Titel, &w.Type, &w.Status, &w.Prioriteit, &w.KlantNaam, &w.Bron,
		&w.SourceID, &w.Doel, &w.Scope, &w.Deliverable, &w.Bevindingen,
		&w.VolgendeStap, &w.Deadline, &w.GeschatteMinuten, &w.WaardeIndicatie,
		&w.StackTags, &w.Tags, &w.CompletedAt, &w.CreatedAt, &w.UpdatedAt)
	return w, err
}

func scanAction(row pgx.CollectableRow) (model.LCActionItem, error) {
	var a model.LCActionItem
	err := row.Scan(&a.ID, &a.UserID, &a.Source, &a.SourceID, &a.Title,
		&a.Summary, &a.ActionType, &a.Status, &a.Priority, &a.DueDate,
		&a.LinkedLeadID, &a.LinkedProjectID, &a.LinkedWorkstreamID, &a.LinkedCompanyID,
		&a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func scanDocument(row pgx.CollectableRow) (model.LCDocument, error) {
	var d model.LCDocument
	err := row.Scan(&d.ID, &d.UserID, &d.DocumentKey, &d.Titel, &d.Categorie,
		&d.Fase, &d.Versie, &d.SourcePath, &d.Samenvatting, &d.Tags,
		&d.CreatedAt, &d.UpdatedAt)
	return d, err
}

func scanDossierDocument(row pgx.CollectableRow) (model.LCDossierDocument, error) {
	var d model.LCDossierDocument
	err := row.Scan(&d.ID, &d.UserID, &d.DocumentKey, &d.Titel, &d.TemplateLabel,
		&d.ContextType, &d.ContextID, &d.ContextTitle, &d.LeadID, &d.ProjectID,
		&d.WorkstreamID, &d.CompanyID, &d.PDFURL, &d.Theme, &d.Delivery, &d.Notes, &d.GeneratedAt, &d.CreatedAt)
	return d, err
}

func scanActivityEvent(row pgx.CollectableRow) (model.LCActivityEvent, error) {
	var e model.LCActivityEvent
	err := row.Scan(&e.ID, &e.UserID, &e.CompanyID, &e.ContactID, &e.LeadID,
		&e.ProjectID, &e.WorkstreamID, &e.ActionItemID, &e.EventType, &e.Channel,
		&e.Title, &e.Body, &e.OccurredAt, &e.CreatedAt, &e.UpdatedAt,
		&e.CompanyName, &e.ContactName, &e.ProjectName, &e.WorkstreamName)
	return e, err
}

func scanQuote(row pgx.CollectableRow) (model.LCQuote, error) {
	var q model.LCQuote
	err := row.Scan(&q.ID, &q.UserID, &q.CompanyID, &q.ProjectID, &q.WorkstreamID,
		&q.QuoteNumber, &q.Titel, &q.Status, &q.IssueDate, &q.ValidUntil,
		&q.Currency, &q.SubtotalCents, &q.VatRateBps, &q.VatCents, &q.TotalCents,
		&q.AcceptedAt, &q.Notes, &q.CreatedAt, &q.UpdatedAt,
		&q.CompanyName, &q.ProjectName, &q.WorkstreamTitle)
	return q, err
}

func scanQuoteLine(row pgx.CollectableRow) (model.LCQuoteLine, error) {
	var l model.LCQuoteLine
	err := row.Scan(&l.ID, &l.QuoteID, &l.UserID, &l.Description, &l.Quantity,
		&l.UnitAmountCents, &l.TotalCents, &l.SortOrder)
	return l, err
}

func scanTimeEntry(row pgx.CollectableRow) (model.LCTimeEntry, error) {
	var t model.LCTimeEntry
	err := row.Scan(&t.ID, &t.UserID, &t.CompanyID, &t.ProjectID, &t.WorkstreamID,
		&t.ActivityEventID, &t.InvoiceID, &t.SourceType, &t.SourceID,
		&t.Description, &t.EntryDate, &t.Minutes, &t.HourlyRateCents,
		&t.Billable, &t.Status, &t.CreatedAt, &t.UpdatedAt,
		&t.CompanyName, &t.ProjectName, &t.WorkstreamTitle)
	return t, err
}

func scanInvoice(row pgx.CollectableRow) (model.LCInvoice, error) {
	var i model.LCInvoice
	err := row.Scan(&i.ID, &i.UserID, &i.CompanyID, &i.ProjectID, &i.WorkstreamID,
		&i.QuoteID, &i.InvoiceNumber, &i.Status, &i.IssueDate, &i.DueDate, &i.Currency,
		&i.SubtotalCents, &i.VatRateBps, &i.VatCents, &i.TotalCents, &i.PaidCents,
		&i.PaymentProvider, &i.ProviderRequestID, &i.MerchantReference,
		&i.PaymentURL, &i.SentAt, &i.PaidAt, &i.Notes, &i.CreatedAt, &i.UpdatedAt,
		&i.CompanyName, &i.ProjectName, &i.WorkstreamTitle)
	return i, err
}

func scanInvoiceLine(row pgx.CollectableRow) (model.LCInvoiceLine, error) {
	var l model.LCInvoiceLine
	err := row.Scan(&l.ID, &l.InvoiceID, &l.UserID, &l.SourceTimeEntryID,
		&l.Description, &l.QuantityMinutes, &l.UnitAmountCents, &l.VatRateBps,
		&l.TotalCents, &l.SortOrder)
	return l, err
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func filterOpen[T any](items []T, statusFn func(T) string) []T {
	var result []T
	for _, item := range items {
		if isOpenStatus(statusFn(item)) {
			result = append(result, item)
		}
	}
	return result
}

func take[T any](items []T, n int) []T {
	if len(items) <= n {
		return items
	}
	return items[:n]
}

func cleanTags(values []string) []string {
	if values == nil {
		return nil
	}
	seen := make(map[string]bool)
	tags := make([]string, 0, len(values))
	for _, value := range values {
		tag := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "#")))
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func cleanStatus(value, fallback string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func cleanCurrency(value string) string {
	currency := strings.ToUpper(strings.TrimSpace(value))
	if currency == "" {
		return "EUR"
	}
	return currency
}

func cleanDateValue(value *string, fallback string) string {
	raw := strings.TrimSpace(deref(value))
	if raw == "" {
		return fallback
	}
	parsed := parseDate(raw)
	if parsed == "" {
		return fallback
	}
	return parsed
}

func cleanDatePtr(value *string) *string {
	raw := strings.TrimSpace(deref(value))
	if raw == "" {
		return nil
	}
	parsed := parseDate(raw)
	if parsed == "" {
		return nil
	}
	return &parsed
}

func parseDate(raw string) string {
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02 15:04"} {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return ""
}

func addDaysDatePtr(date string, days int) *string {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil
	}
	value := parsed.AddDate(0, 0, days).Format("2006-01-02")
	return &value
}

func cleanQuoteLines(lines []model.LCQuoteLineCreate) []model.LCQuoteLineCreate {
	result := make([]model.LCQuoteLineCreate, 0, len(lines))
	for _, line := range lines {
		description := strings.TrimSpace(line.Description)
		if description == "" {
			continue
		}
		quantity := line.Quantity
		if quantity <= 0 {
			quantity = 1
		}
		unit := maxInt(line.UnitAmountCents, 0)
		result = append(result, model.LCQuoteLineCreate{
			Description:     description,
			Quantity:        quantity,
			UnitAmountCents: unit,
			TotalCents:      quantity * unit,
			SortOrder:       line.SortOrder,
		})
	}
	return result
}

func cleanInvoiceLines(lines []model.LCInvoiceLineCreate) []model.LCInvoiceLineCreate {
	result := make([]model.LCInvoiceLineCreate, 0, len(lines))
	for _, line := range lines {
		description := strings.TrimSpace(line.Description)
		if description == "" {
			continue
		}
		minutes := line.QuantityMinutes
		if minutes < 0 {
			minutes = 0
		}
		unit := maxInt(line.UnitAmountCents, 0)
		total := line.TotalCents
		if total <= 0 {
			total = centsFromMinutes(minutes, unit)
		}
		result = append(result, model.LCInvoiceLineCreate{
			SourceTimeEntryID: line.SourceTimeEntryID,
			Description:       description,
			QuantityMinutes:   minutes,
			UnitAmountCents:   unit,
			VatRateBps:        line.VatRateBps,
			TotalCents:        total,
			SortOrder:         line.SortOrder,
		})
	}
	return result
}

func centsFromMinutes(minutes, hourlyRateCents int) int {
	if minutes <= 0 || hourlyRateCents <= 0 {
		return 0
	}
	return (minutes*hourlyRateCents + 30) / 60
}

func vatCents(subtotalCents, vatRateBps int) int {
	if subtotalCents <= 0 || vatRateBps <= 0 {
		return 0
	}
	return (subtotalCents*vatRateBps + 5000) / 10000
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func parseDateTimePtr(value *string) *time.Time {
	raw := strings.TrimSpace(deref(value))
	if raw == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04"} {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func shouldActivityUpdateLastContact(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "contact", "gesprek", "call", "meeting", "afspraak", "email":
		return true
	default:
		return false
	}
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func nonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
