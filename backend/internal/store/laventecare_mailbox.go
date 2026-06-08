package store

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// GetMailbox returns the LaventeCare outbound mailbox workspace.
func (s *LaventeCareStore) GetMailbox(ctx context.Context, userID string, limit int, configured bool, senderEmail string) (*model.LCMailbox, error) {
	if err := s.SeedDefaultMailTemplates(ctx, userID); err != nil {
		return nil, err
	}
	templates, err := s.ListMailTemplates(ctx, userID, 80)
	if err != nil {
		return nil, err
	}
	outbox, err := s.ListMailOutbox(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	summary, err := s.GetMailboxSummary(ctx, userID, templates, configured, senderEmail)
	if err != nil {
		return nil, err
	}
	return &model.LCMailbox{
		Summary:   summary,
		Templates: templates,
		Outbox:    outbox,
	}, nil
}

func (s *LaventeCareStore) GetMailboxSummary(ctx context.Context, userID string, templates []model.LCMailTemplate, configured bool, senderEmail string) (model.LCMailboxSummary, error) {
	var activeTemplates int
	for _, template := range templates {
		if template.Status == "active" {
			activeTemplates++
		}
	}

	var totalOutbox, drafts, sent, failed int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT
		    COUNT(*)::int,
		    COUNT(*) FILTER (WHERE status = 'concept')::int,
		    COUNT(*) FILTER (WHERE status = 'sent')::int,
		    COUNT(*) FILTER (WHERE status = 'failed')::int
		   FROM lc_mail_outbox
		  WHERE user_id = $1`,
		userID,
	).Scan(&totalOutbox, &drafts, &sent, &failed)
	if err != nil {
		return model.LCMailboxSummary{}, err
	}

	nextStep := "Maak een templateconcept of verstuur de eerste klantmail."
	if !configured {
		nextStep = "Vul Microsoft Graph env in en zet LAVENTECARE_MAIL_ENABLED=true."
	} else if sent > 0 {
		nextStep = "Mailbox is klaar. Gebruik templates per klant, offerte of factuur."
	} else if drafts > 0 {
		nextStep = "Controleer concepten en verstuur de eerste LaventeCare-mail."
	}

	return model.LCMailboxSummary{
		Templates:       len(templates),
		ActiveTemplates: activeTemplates,
		Outbox:          totalOutbox,
		Drafts:          drafts,
		Sent:            sent,
		Failed:          failed,
		Provider:        "microsoft_graph",
		SenderEmail:     senderEmail,
		Configured:      configured,
		NextStep:        nextStep,
	}, nil
}

func (s *LaventeCareStore) SeedDefaultMailTemplates(ctx context.Context, userID string) error {
	now := time.Now().UTC()
	defaults := []model.LCMailTemplateCreate{
		{
			TemplateKey:     "intake_followup",
			Name:            "Intake opvolging",
			Category:        "sales",
			Status:          "active",
			SubjectTemplate: "Vervolg LaventeCare intake - {{company.naam}}",
			BodyHTML:        defaultMailHTML("Intake opvolging", "Beste {{contact.naam}},", "Dank voor je bericht en de context rond {{company.naam}}. Ik heb de belangrijkste punten vastgelegd en kijk graag met je mee naar de meest praktische vervolgstap.", "Voorstel voor vervolg: {{next_step}}"),
			BodyText:        mailStrPtr("Beste {{contact.naam}},\n\nDank voor je bericht en de context rond {{company.naam}}. Ik heb de belangrijkste punten vastgelegd en kijk graag met je mee naar de meest praktische vervolgstap.\n\nVoorstel voor vervolg: {{next_step}}\n\nMet vriendelijke groet,\nLaventeCare"),
		},
		{
			TemplateKey:     "quote_send",
			Name:            "Offerte versturen",
			Category:        "commerce",
			Status:          "active",
			SubjectTemplate: "Offerte {{quote.number}} - {{company.naam}}",
			BodyHTML:        defaultMailHTML("Offerte", "Beste {{contact.naam}},", "In de bijlage/omgeving staat de offerte voor {{company.naam}} klaar. De kern: {{quote.summary}}", "Laat me weten of je akkoord bent, dan plan ik de uitvoering in."),
			BodyText:        mailStrPtr("Beste {{contact.naam}},\n\nDe offerte voor {{company.naam}} staat klaar. De kern: {{quote.summary}}\n\nLaat me weten of je akkoord bent, dan plan ik de uitvoering in.\n\nMet vriendelijke groet,\nLaventeCare"),
		},
		{
			TemplateKey:     "invoice_send",
			Name:            "Factuur en betaalverzoek",
			Category:        "commerce",
			Status:          "active",
			SubjectTemplate: "Factuur {{invoice.number}} - {{company.naam}}",
			BodyHTML:        defaultMailHTML("Factuur", "Beste {{contact.naam}},", "De factuur voor de uitgevoerde werkzaamheden staat klaar. Je kunt betalen via: {{invoice.payment_url}}", "Als er iets niet klopt, hoor ik het graag direct."),
			BodyText:        mailStrPtr("Beste {{contact.naam}},\n\nDe factuur voor de uitgevoerde werkzaamheden staat klaar. Je kunt betalen via: {{invoice.payment_url}}\n\nAls er iets niet klopt, hoor ik het graag direct.\n\nMet vriendelijke groet,\nLaventeCare"),
		},
		{
			TemplateKey:     "project_update",
			Name:            "Projectupdate",
			Category:        "delivery",
			Status:          "active",
			SubjectTemplate: "Projectupdate - {{project.naam}}",
			BodyHTML:        defaultMailHTML("Projectupdate", "Beste {{contact.naam}},", "Hierbij een korte update over {{project.naam}}: {{project.update}}", "Volgende stap: {{next_step}}"),
			BodyText:        mailStrPtr("Beste {{contact.naam}},\n\nHierbij een korte update over {{project.naam}}: {{project.update}}\n\nVolgende stap: {{next_step}}\n\nMet vriendelijke groet,\nLaventeCare"),
		},
		{
			TemplateKey:     "meeting_recap",
			Name:            "Gespreksverslag",
			Category:        "dossier",
			Status:          "active",
			SubjectTemplate: "Samenvatting gesprek - {{company.naam}}",
			BodyHTML:        defaultMailHTML("Gespreksverslag", "Beste {{contact.naam}},", "Hierbij de korte samenvatting van ons gesprek: {{meeting.summary}}", "Acties: {{meeting.actions}}"),
			BodyText:        mailStrPtr("Beste {{contact.naam}},\n\nHierbij de korte samenvatting van ons gesprek: {{meeting.summary}}\n\nActies: {{meeting.actions}}\n\nMet vriendelijke groet,\nLaventeCare"),
		},
	}

	for _, template := range defaults {
		_, err := s.db.Pool.Exec(ctx,
			`INSERT INTO lc_mail_templates (id, user_id, template_key, name, category, status,
			        subject_template, body_html, body_text, default_cc, default_bcc, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)
			 ON CONFLICT (user_id, template_key) DO NOTHING`,
			uuid.New(), userID, template.TemplateKey, template.Name, template.Category, template.Status,
			template.SubjectTemplate, template.BodyHTML, template.BodyText, cleanEmails(template.DefaultCC),
			cleanEmails(template.DefaultBCC), now)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *LaventeCareStore) ListMailTemplates(ctx context.Context, userID string, limit int) ([]model.LCMailTemplate, error) {
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, template_key, name, category, status, subject_template,
		        body_html, body_text, default_cc, default_bcc, created_at, updated_at
		   FROM lc_mail_templates
		  WHERE user_id = $1
		  ORDER BY category ASC, name ASC
		  LIMIT $2`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanMailTemplate)
}

func (s *LaventeCareStore) GetMailTemplate(ctx context.Context, userID string, id uuid.UUID) (*model.LCMailTemplate, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, template_key, name, category, status, subject_template,
		        body_html, body_text, default_cc, default_bcc, created_at, updated_at
		   FROM lc_mail_templates
		  WHERE user_id = $1 AND id = $2
		  LIMIT 1`,
		userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	templates, err := pgx.CollectRows(rows, scanMailTemplate)
	if err != nil {
		return nil, err
	}
	if len(templates) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &templates[0], nil
}

func (s *LaventeCareStore) CreateMailTemplate(ctx context.Context, userID string, input model.LCMailTemplateCreate) (*model.LCMailTemplate, error) {
	now := time.Now().UTC()
	id := uuid.New()
	key := strings.ToLower(strings.TrimSpace(input.TemplateKey))
	if key == "" {
		key = "template_" + id.String()[:8]
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "Nieuwe template"
	}
	category := cleanStatus(input.Category, "general")
	status := cleanStatus(input.Status, "active")
	subject := strings.TrimSpace(input.SubjectTemplate)
	bodyHTML := strings.TrimSpace(input.BodyHTML)
	if subject == "" || bodyHTML == "" {
		return nil, pgx.ErrNoRows
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO lc_mail_templates (id, user_id, template_key, name, category, status,
		        subject_template, body_html, body_text, default_cc, default_bcc, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)`,
		id, userID, key, name, category, status, subject, bodyHTML, cleanStringPtr(input.BodyText),
		cleanEmails(input.DefaultCC), cleanEmails(input.DefaultBCC), now)
	if err != nil {
		return nil, err
	}
	return s.GetMailTemplate(ctx, userID, id)
}

func (s *LaventeCareStore) UpdateMailTemplate(ctx context.Context, userID string, id uuid.UUID, input model.LCMailTemplateUpdate) error {
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_mail_templates SET
			name = COALESCE($3, name),
			category = COALESCE($4, category),
			status = COALESCE($5, status),
			subject_template = COALESCE($6, subject_template),
			body_html = COALESCE($7, body_html),
			body_text = COALESCE($8, body_text),
			default_cc = CASE WHEN $9::text[] IS NULL THEN default_cc ELSE $9 END,
			default_bcc = CASE WHEN $10::text[] IS NULL THEN default_bcc ELSE $10 END,
			updated_at = $11
		 WHERE user_id = $1 AND id = $2`,
		userID, id, cleanStringPtr(input.Name), cleanStringPtr(input.Category), cleanStringPtr(input.Status),
		cleanStringPtr(input.SubjectTemplate), cleanStringPtr(input.BodyHTML), cleanStringPtr(input.BodyText),
		nullableEmails(input.DefaultCC), nullableEmails(input.DefaultBCC), time.Now().UTC())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) ListMailOutbox(ctx context.Context, userID string, limit int) ([]model.LCMailOutboxItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	rows, err := s.db.Pool.Query(ctx, mailOutboxSelectSQL()+` WHERE m.user_id = $1 ORDER BY m.created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanMailOutboxItem)
}

func (s *LaventeCareStore) CreateMailFromTemplate(ctx context.Context, userID string, input model.LCMailSendRequest) (*model.LCMailOutboxItem, error) {
	template, err := s.GetMailTemplate(ctx, userID, input.TemplateID)
	if err != nil {
		return nil, err
	}
	if template.Status != "active" {
		return nil, errors.New("mail template is not active")
	}

	contextValues, companyID, contactID, toEmail, toName, err := s.buildMailRenderContext(ctx, userID, input)
	if err != nil {
		return nil, err
	}
	subject := renderTemplate(template.SubjectTemplate, contextValues)
	bodyHTML := renderTemplate(template.BodyHTML, contextValues)
	bodyText := cleanStringPtr(template.BodyText)
	if bodyText != nil {
		rendered := renderTemplate(*bodyText, contextValues)
		bodyText = &rendered
	}

	id := uuid.New()
	now := time.Now().UTC()
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO lc_mail_outbox (id, user_id, template_id, company_id, contact_id,
		        project_id, workstream_id, quote_id, invoice_id, to_email, to_name, cc, bcc,
		        subject, body_html, body_text, status, provider, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,'concept','microsoft_graph',$17,$17)`,
		id, userID, template.ID, companyID, contactID, input.ProjectID, input.WorkstreamID,
		input.QuoteID, input.InvoiceID, toEmail, toName, mergeEmails(template.DefaultCC, input.CC),
		mergeEmails(template.DefaultBCC, input.BCC), subject, bodyHTML, bodyText, now)
	if err != nil {
		return nil, err
	}
	return s.GetMailOutboxItem(ctx, userID, id)
}

func (s *LaventeCareStore) GetMailOutboxItem(ctx context.Context, userID string, id uuid.UUID) (*model.LCMailOutboxItem, error) {
	rows, err := s.db.Pool.Query(ctx, mailOutboxSelectSQL()+` WHERE m.user_id = $1 AND m.id = $2 LIMIT 1`, userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, scanMailOutboxItem)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &items[0], nil
}

func (s *LaventeCareStore) MarkMailOutboxSent(ctx context.Context, userID string, id uuid.UUID, providerMessageID string) error {
	now := time.Now().UTC()
	msgID := cleanStringPtr(&providerMessageID)
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_mail_outbox
		    SET status = 'sent', provider_message_id = $3, error_message = NULL, sent_at = $4, updated_at = $4
		  WHERE user_id = $1 AND id = $2`,
		userID, id, msgID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) MarkMailOutboxFailed(ctx context.Context, userID string, id uuid.UUID, message string) error {
	now := time.Now().UTC()
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_mail_outbox
		    SET status = 'failed', error_message = $3, updated_at = $4
		  WHERE user_id = $1 AND id = $2`,
		userID, id, strings.TrimSpace(message), now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) buildMailRenderContext(ctx context.Context, userID string, input model.LCMailSendRequest) (map[string]string, *uuid.UUID, *uuid.UUID, string, *string, error) {
	values := map[string]string{
		"laventecare.name":  "LaventeCare",
		"laventecare.email": strings.TrimSpace(os.Getenv("MICROSOFT_SENDER_EMAIL")),
		"next_step":         valueOr(input.Variables["next_step"], "Ik hoor graag wat voor jou het beste moment is om dit op te pakken."),
	}
	for key, value := range input.Variables {
		values[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}

	var company *model.LCCompany
	var contact *model.LCContact
	companyID := input.CompanyID
	contactID := input.ContactID

	if contactID != nil {
		c, err := s.GetContact(ctx, userID, *contactID)
		if err != nil {
			return nil, nil, nil, "", nil, err
		}
		contact = c
		if companyID == nil && c.CompanyID != nil {
			companyID = c.CompanyID
		}
		values["contact.naam"] = c.Naam
		values["contact.email"] = deref(c.Email)
		values["contact.rol"] = deref(c.Rol)
	}
	if companyID != nil {
		c, err := s.GetCompany(ctx, userID, *companyID)
		if err != nil {
			return nil, nil, nil, "", nil, err
		}
		company = c
		values["company.naam"] = c.Naam
		values["company.website"] = deref(c.Website)
		values["company.sector"] = deref(c.Sector)
		values["company.volgende_actie"] = deref(c.VolgendeActie)
	}

	toEmail := strings.TrimSpace(deref(input.ToEmail))
	if toEmail == "" && contact != nil {
		toEmail = strings.TrimSpace(deref(contact.Email))
	}
	if toEmail == "" {
		return nil, nil, nil, "", nil, errors.New("recipient email is required")
	}
	toName := cleanStringPtr(input.ToName)
	if toName == nil && contact != nil {
		toName = &contact.Naam
	}
	if _, ok := values["contact.naam"]; !ok {
		if toName != nil {
			values["contact.naam"] = *toName
		} else {
			values["contact.naam"] = "relatie"
		}
	}
	if _, ok := values["company.naam"]; !ok {
		if company != nil {
			values["company.naam"] = company.Naam
		} else {
			values["company.naam"] = "je organisatie"
		}
	}

	return values, companyID, contactID, toEmail, toName, nil
}

func mailOutboxSelectSQL() string {
	return `SELECT m.id, m.user_id, m.template_id, m.company_id, m.contact_id, m.project_id,
		        m.workstream_id, m.quote_id, m.invoice_id, m.to_email, m.to_name, m.cc, m.bcc,
		        m.subject, m.body_html, m.body_text, m.status, m.provider, m.provider_message_id,
		        m.error_message, m.sent_at, m.created_at, m.updated_at,
		        t.name, c.naam, ct.naam
		   FROM lc_mail_outbox m
		   LEFT JOIN lc_mail_templates t ON t.id = m.template_id AND t.user_id = m.user_id
		   LEFT JOIN lc_companies c ON c.id = m.company_id AND c.user_id = m.user_id
		   LEFT JOIN lc_contacts ct ON ct.id = m.contact_id AND ct.user_id = m.user_id`
}

func scanMailTemplate(row pgx.CollectableRow) (model.LCMailTemplate, error) {
	var t model.LCMailTemplate
	err := row.Scan(&t.ID, &t.UserID, &t.TemplateKey, &t.Name, &t.Category,
		&t.Status, &t.SubjectTemplate, &t.BodyHTML, &t.BodyText, &t.DefaultCC,
		&t.DefaultBCC, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func scanMailOutboxItem(row pgx.CollectableRow) (model.LCMailOutboxItem, error) {
	var m model.LCMailOutboxItem
	err := row.Scan(&m.ID, &m.UserID, &m.TemplateID, &m.CompanyID, &m.ContactID,
		&m.ProjectID, &m.WorkstreamID, &m.QuoteID, &m.InvoiceID, &m.ToEmail,
		&m.ToName, &m.CC, &m.BCC, &m.Subject, &m.BodyHTML, &m.BodyText,
		&m.Status, &m.Provider, &m.ProviderMessageID, &m.ErrorMessage,
		&m.SentAt, &m.CreatedAt, &m.UpdatedAt, &m.TemplateName,
		&m.CompanyName, &m.ContactName)
	return m, err
}

func defaultMailHTML(title, greeting, body, next string) string {
	return fmt.Sprintf(
		`<div style="font-family:Inter,Arial,sans-serif;font-size:15px;line-height:1.65;color:#0f172a;background:#ffffff;">
  <p style="margin:0 0 16px;font-size:13px;font-weight:700;letter-spacing:.04em;text-transform:uppercase;color:#0f766e;">%s</p>
  <p>%s</p>
  <p>%s</p>
  <p>%s</p>
  <p style="margin-top:24px;">Met vriendelijke groet,<br><strong>LaventeCare</strong></p>
</div>`,
		html.EscapeString(title),
		html.EscapeString(greeting),
		html.EscapeString(body),
		html.EscapeString(next),
	)
}

func renderTemplate(input string, values map[string]string) string {
	result := input
	for key, value := range values {
		result = strings.ReplaceAll(result, "{{"+key+"}}", value)
		result = strings.ReplaceAll(result, "{{ "+key+" }}", value)
	}
	return result
}

func cleanEmails(values []string) []string {
	seen := map[string]bool{}
	emails := make([]string, 0, len(values))
	for _, value := range values {
		email := strings.ToLower(strings.TrimSpace(value))
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true
		emails = append(emails, email)
	}
	return emails
}

func nullableEmails(values []string) any {
	if values == nil {
		return nil
	}
	return cleanEmails(values)
}

func mergeEmails(a, b []string) []string {
	return cleanEmails(append(append([]string{}, a...), b...))
}

func mailStrPtr(value string) *string {
	return &value
}

func valueOr(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func laventeCareMailConfiguredFromEnv() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("LAVENTECARE_MAIL_ENABLED")), "true") &&
		strings.TrimSpace(os.Getenv("MICROSOFT_TENANT_ID")) != "" &&
		strings.TrimSpace(os.Getenv("MICROSOFT_CLIENT_ID")) != "" &&
		strings.TrimSpace(os.Getenv("MICROSOFT_CLIENT_SECRET")) != "" &&
		strings.TrimSpace(os.Getenv("MICROSOFT_SENDER_EMAIL")) != ""
}
