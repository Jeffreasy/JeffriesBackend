package store

import (
	"context"
	"sort"
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
