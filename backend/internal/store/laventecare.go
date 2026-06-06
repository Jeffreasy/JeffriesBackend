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
		"in_behandeling", "voorgesteld", "beoordeeld":
		return true
	}
	return false
}

// ─── Leads ───────────────────────────────────────────────────────────────────

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

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO lc_leads (id, user_id, titel, bron, source_id, status, fit_score,
		        pijnpunt, prioriteit, volgende_stap, volgende_actie_datum, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,'nieuw',$6,$7,$8,$9,$10,$11,$11)`,
		id, userID, input.Titel, bron, input.SourceID, input.FitScore,
		input.Pijnpunt, input.Prioriteit, input.VolgendeStap, input.VolgendeActieDatum, now)
	if err != nil {
		return nil, err
	}

	return &model.LCLead{
		ID: id, UserID: userID, Titel: input.Titel, Bron: bron,
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
			status = COALESCE($3, status),
			fit_score = COALESCE($4, fit_score),
			pijnpunt = COALESCE($5, pijnpunt),
			prioriteit = COALESCE($6, prioriteit),
			volgende_stap = COALESCE($7, volgende_stap),
			volgende_actie_datum = COALESCE($8, volgende_actie_datum),
			updated_at = $9
		 WHERE id = $1 AND user_id = $2`,
		id, userID, input.Status, input.FitScore, input.Pijnpunt,
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
			fase = COALESCE($3, fase),
			status = COALESCE($4, status),
			waarde_indicatie = COALESCE($5, waarde_indicatie),
			start_datum = COALESCE($6, start_datum),
			deadline = COALESCE($7, deadline),
			samenvatting = COALESCE($8, samenvatting),
			updated_at = $9
		 WHERE id = $1 AND user_id = $2`,
		id, userID, input.Fase, input.Status, input.WaardeIndicatie,
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
		LeadID:       &input.LeadID,
		Naam:         input.Naam,
		Fase:         fase,
		Status:       status,
		Samenvatting: input.Samenvatting,
	})
}

// ─── Action Items ────────────────────────────────────────────────────────────

func (s *LaventeCareStore) ListActions(ctx context.Context, userID string, limit int) ([]model.LCActionItem, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, source, source_id, title, summary, action_type,
		        status, priority, due_date, linked_lead_id, linked_project_id,
		        created_at, updated_at
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
		        action_type, status, priority, due_date, linked_lead_id, linked_project_id,
		        created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'open',$8,$9,$10,$11,$12,$12)`,
		id, userID, source, input.SourceID, input.Title, input.Summary,
		actionType, priority, input.DueDate, input.LinkedLeadID, input.LinkedProjectID, now)
	if err != nil {
		return nil, err
	}

	return &model.LCActionItem{
		ID: id, UserID: userID, Source: source, SourceID: input.SourceID,
		Title: input.Title, Summary: input.Summary, ActionType: actionType,
		Status: "open", Priority: priority, DueDate: input.DueDate,
		LinkedLeadID: input.LinkedLeadID, LinkedProjectID: input.LinkedProjectID,
		CreatedAt: now, UpdatedAt: now,
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
		tag, uErr := s.db.Pool.Exec(ctx,
			`INSERT INTO lc_documents (id, user_id, document_key, titel, categorie, fase, versie,
			        source_path, samenvatting, tags, created_at, updated_at)
			 VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
			 ON CONFLICT (user_id, document_key) DO UPDATE SET
			        titel = EXCLUDED.titel, categorie = EXCLUDED.categorie,
			        fase = EXCLUDED.fase, versie = EXCLUDED.versie,
			        source_path = EXCLUDED.source_path, samenvatting = EXCLUDED.samenvatting,
			        tags = EXCLUDED.tags, updated_at = EXCLUDED.updated_at`,
			userID, doc.DocumentKey, doc.Titel, doc.Categorie, doc.Fase, doc.Versie,
			doc.SourcePath, doc.Samenvatting, doc.Tags, now)
		if uErr != nil {
			return inserted, updated, uErr
		}
		if tag.RowsAffected() > 0 {
			// ON CONFLICT DO UPDATE always reports 1 row; distinguish by checking creation
			updated++
		}
	}
	return inserted, updated, nil
}

// ─── Cockpit (aggregated dashboard) ──────────────────────────────────────────

func (s *LaventeCareStore) GetCockpit(ctx context.Context, userID string) (*model.LCCockpit, error) {
	leads, err := s.ListLeads(ctx, userID, 30)
	if err != nil {
		return nil, err
	}
	projects, err := s.ListProjects(ctx, userID, 30)
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

	activeLeads := filterOpen(leads, func(l model.LCLead) string { return l.Status })
	activeProjects := filterOpen(projects, func(p model.LCProject) string { return p.Status })

	incidents, _ := s.listSlaIncidents(ctx, userID, 5)
	changes, _ := s.listChangeRequests(ctx, userID, 5)
	decisions, _ := s.listDecisions(ctx, userID, 5)

	// Business signals: scan emails for business-term matches
	signals := s.buildBusinessSignals(ctx, userID, leads, projects)
	// Follow-ups: leads and projects with upcoming deadlines
	followUps := s.buildFollowUps(activeLeads, activeProjects)

	return &model.LCCockpit{
		Summary: model.LCCockpitSummary{
			Leads:           len(leads),
			ActiveLeads:     len(activeLeads),
			Projects:        len(projects),
			ActiveProjects:  len(activeProjects),
			Documents:       len(documents),
			OpenIncidents:   len(incidents),
			OpenChanges:     len(changes),
			Decisions:       len(decisions),
			ActionItems:     len(actions),
			DocumentsSeeded: len(documents) > 0,
			BusinessSignals: len(signals),
			FollowUps:       len(followUps),
		},
		ActiveLeads:     take(activeLeads, 8),
		ActiveProjects:  take(activeProjects, 8),
		ActionItems:     actions,
		OpenIncidents:   incidents,
		OpenChanges:     changes,
		RecentDecisions: decisions,
		DocumentCatalog: documents,
		BusinessSignals: signals,
		FollowUps:       followUps,
	}, nil
}

// buildBusinessSignals matches emails against CRM entity names.
func (s *LaventeCareStore) buildBusinessSignals(ctx context.Context, userID string, leads []model.LCLead, projects []model.LCProject) []model.LCBusinessSignal {
	terms := buildSignalTerms(leads, projects)
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

// buildFollowUps creates follow-up signals from leads and projects with pending deadlines.
func (s *LaventeCareStore) buildFollowUps(leads []model.LCLead, projects []model.LCProject) []model.LCFollowUpSignal {
	today := time.Now().Format("2006-01-02")
	var followUps []model.LCFollowUpSignal

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

func buildSignalTerms(leads []model.LCLead, projects []model.LCProject) []string {
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
	for _, l := range leads {
		add(l.Titel)
	}
	for _, p := range projects {
		add(p.Naam)
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

func scanAction(row pgx.CollectableRow) (model.LCActionItem, error) {
	var a model.LCActionItem
	err := row.Scan(&a.ID, &a.UserID, &a.Source, &a.SourceID, &a.Title,
		&a.Summary, &a.ActionType, &a.Status, &a.Priority, &a.DueDate,
		&a.LinkedLeadID, &a.LinkedProjectID, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func scanDocument(row pgx.CollectableRow) (model.LCDocument, error) {
	var d model.LCDocument
	err := row.Scan(&d.ID, &d.UserID, &d.DocumentKey, &d.Titel, &d.Categorie,
		&d.Fase, &d.Versie, &d.SourcePath, &d.Samenvatting, &d.Tags,
		&d.CreatedAt, &d.UpdatedAt)
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
