package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
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
	ErrQuoteNotAccepted             = errors.New("quote must be accepted before invoice conversion")
	ErrQuoteHasNoLines              = errors.New("quote has no lines to invoice")
	ErrInvalidDossierAdviceTarget   = errors.New("choose exactly one dossier context")
	ErrInvalidStatus                = errors.New("unknown status")
	ErrInvalidStatusTransition      = errors.New("illegal status transition for a finalized record")
	dossierAdviceResponseSampleSize = 25
)

// Allowed lifecycle states. A paid invoice and an accepted/declined quote are
// treated as financially final and may not be silently reverted or re-amounted.
var (
	invoiceStatuses = map[string]bool{"concept": true, "verstuurd": true, "betaald": true, "geannuleerd": true}
	quoteStatuses   = map[string]bool{"concept": true, "verstuurd": true, "geaccepteerd": true, "afgewezen": true, "verlopen": true}
)

// validateInvoiceStatusTransition rejects unknown statuses, reverting a paid
// invoice back to concept/verstuurd, and re-amounting paid_cents on an
// already-paid invoice (legally-final VAT records).
func validateInvoiceStatusTransition(current, next string, newPaidCents *int, currentPaidCents int) error {
	if !invoiceStatuses[next] {
		return ErrInvalidStatus
	}
	if current == "betaald" {
		if next == "concept" || next == "verstuurd" {
			return ErrInvalidStatusTransition
		}
		if newPaidCents != nil && *newPaidCents != currentPaidCents {
			return ErrInvalidStatusTransition
		}
	}
	return nil
}

// validateQuoteStatusTransition rejects unknown statuses and un-accepting a quote
// that was already accepted (it may have been converted to an invoice).
func validateQuoteStatusTransition(current, next string) error {
	if !quoteStatuses[next] {
		return ErrInvalidStatus
	}
	if current == "geaccepteerd" && (next == "concept" || next == "verstuurd") {
		return ErrInvalidStatusTransition
	}
	return nil
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
	case "afgerond", "done", "gesloten", "gearchiveerd", "omgezet_project",
		"gewonnen", "verloren", "gediskwalificeerd", "geannuleerd":
		return true
	}
	return false
}

// lcKnownStatus is the union of every recognised lead/project/workstream status
// (the open + closed/terminal vocabularies). Update paths validate against it so
// a typo'd status is rejected, without over-constraining the deliberately
// flexible free-text status model (any genuinely-used value is in one of the two
// predicates above).
func lcKnownStatus(status string) bool {
	return isOpenStatus(status) || isClosedStatus(status)
}

// validateLCStatus returns ErrInvalidStatus when an explicitly-set status is not
// a recognised value. nil/empty means "unchanged" and always passes.
func validateLCStatus(status *string) error {
	if status == nil {
		return nil
	}
	st := strings.TrimSpace(*status)
	if st == "" || lcKnownStatus(st) {
		return nil
	}
	return ErrInvalidStatus
}

// ─── Companies & contacts ───────────────────────────────────────────────────

func (s *LaventeCareStore) ListCompanies(ctx context.Context, userID string, limit int, query string) ([]model.LCCompany, error) {
	if limit <= 0 {
		limit = 30
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	rows, err := s.db.Pool.Query(ctx,
		`SELECT c.id, c.user_id, c.naam, c.website, c.sector, c.status, c.relatie_type,
		        c.notities, c.laatste_contact, c.volgende_actie,
		        c.kvk_number, c.vat_number, c.billing_email, c.billing_address, c.billing_reference,
		        c.payment_terms_days, c.contract_status, c.service_level, c.preferred_channel,
		        c.portal_url, c.default_login_url, c.onboarding_status, c.data_processing_status,
		        c.created_at, c.updated_at,
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
		        c.notities, c.laatste_contact, c.volgende_actie,
		        c.kvk_number, c.vat_number, c.billing_email, c.billing_address, c.billing_reference,
		        c.payment_terms_days, c.contract_status, c.service_level, c.preferred_channel,
		        c.portal_url, c.default_login_url, c.onboarding_status, c.data_processing_status,
		        c.created_at, c.updated_at,
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
		        notities, laatste_contact, volgende_actie, kvk_number, vat_number, billing_email,
		        billing_address, billing_reference, payment_terms_days, contract_status, service_level,
		        preferred_channel, portal_url, default_login_url, onboarding_status, data_processing_status,
		        created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$24)`,
		id, userID, name, cleanStringPtr(input.Website), cleanStringPtr(input.Sector),
		status, relatieType, cleanStringPtr(input.Notities), laatsteContact,
		cleanStringPtr(input.VolgendeActie), cleanStringPtr(input.KVKNumber),
		cleanStringPtr(input.VATNumber), cleanStringPtr(input.BillingEmail),
		cleanStringPtr(input.BillingAddress), cleanStringPtr(input.BillingReference),
		positiveIntOr(input.PaymentTermsDays, 14), cleanStatusPtr(input.ContractStatus, "geen_contract"),
		cleanStatusPtr(input.ServiceLevel, "basis"), cleanStringPtr(input.PreferredChannel),
		cleanStringPtr(input.PortalURL), cleanStringPtr(input.DefaultLoginURL),
		cleanStatusPtr(input.OnboardingStatus, "niet_gestart"),
		cleanStatusPtr(input.DataProcessStatus, "niet_nodig"), now)
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
			kvk_number = COALESCE($11, kvk_number),
			vat_number = COALESCE($12, vat_number),
			billing_email = COALESCE($13, billing_email),
			billing_address = COALESCE($14, billing_address),
			billing_reference = COALESCE($15, billing_reference),
			payment_terms_days = COALESCE($16, payment_terms_days),
			contract_status = COALESCE($17, contract_status),
			service_level = COALESCE($18, service_level),
			preferred_channel = COALESCE($19, preferred_channel),
			portal_url = COALESCE($20, portal_url),
			default_login_url = COALESCE($21, default_login_url),
			onboarding_status = COALESCE($22, onboarding_status),
			data_processing_status = COALESCE($23, data_processing_status),
			updated_at = $24
		 WHERE id = $1 AND user_id = $2`,
		id, userID, cleanStringPtr(input.Naam), cleanStringPtr(input.Website),
		cleanStringPtr(input.Sector), cleanStringPtr(input.Status), cleanStringPtr(input.RelatieType),
		cleanStringPtr(input.Notities), latestContact, cleanStringPtr(input.VolgendeActie),
		cleanStringPtr(input.KVKNumber), cleanStringPtr(input.VATNumber), cleanStringPtr(input.BillingEmail),
		cleanStringPtr(input.BillingAddress), cleanStringPtr(input.BillingReference),
		positiveIntPtr(input.PaymentTermsDays), cleanStringPtr(input.ContractStatus),
		cleanStringPtr(input.ServiceLevel), cleanStringPtr(input.PreferredChannel),
		cleanStringPtr(input.PortalURL), cleanStringPtr(input.DefaultLoginURL),
		cleanStringPtr(input.OnboardingStatus), cleanStringPtr(input.DataProcessStatus), now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	// Keep the denormalized business_context_title on notes/personal-events in
	// sync when the company is renamed, so reads don't show the old name.
	if input.Naam != nil {
		if newName := strings.TrimSpace(*input.Naam); newName != "" {
			idStr := id.String()
			if _, err := s.db.Pool.Exec(ctx, `
				UPDATE notes SET business_context_title = $3
				 WHERE user_id = $1 AND business_context_id = $2`, userID, idStr, newName); err != nil {
				return err
			}
			if _, err := s.db.Pool.Exec(ctx, `
				UPDATE personal_events SET business_context_title = $3
				 WHERE user_id = $1 AND business_context_id = $2`, userID, idStr, newName); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteCompany erases a customer and their personal data (GDPR Art.17). It
// removes the company's contacts (PII) explicitly, then deletes the company —
// which cascades the access credentials and the activity timeline. Leads,
// projects and documents are retained but their company/contact references are
// nulled by FK, so no orphaned PII remains.
func (s *LaventeCareStore) DeleteCompany(ctx context.Context, userID string, id uuid.UUID) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM lc_contacts WHERE user_id = $1 AND company_id = $2`, userID, id); err != nil {
		return err
	}
	// Notes/personal-events carry a denormalized business_context pointing at this
	// company by id (free TEXT, no FK). Clear it in the same tx so a deleted
	// company can't leave a dangling id + stale cached title behind.
	idStr := id.String()
	if _, err := tx.Exec(ctx, `
		UPDATE notes SET business_context_id = NULL, business_context_type = NULL, business_context_title = NULL
		 WHERE user_id = $1 AND business_context_id = $2`, userID, idStr); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE personal_events SET business_context_id = NULL, business_context_type = NULL, business_context_title = NULL
		 WHERE user_id = $1 AND business_context_id = $2`, userID, idStr); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM lc_companies WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return tx.Commit(ctx)
}

func (s *LaventeCareStore) ListContacts(ctx context.Context, userID string, companyID *uuid.UUID, limit int) ([]model.LCContact, error) {
	if limit <= 0 {
		limit = 30
	}
	if companyID != nil {
		rows, err := s.db.Pool.Query(ctx,
			`SELECT id, user_id, company_id, naam, email, telefoon, rol, is_primary,
			        notities, preferred_channel, decision_role, created_at, updated_at
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
		        notities, preferred_channel, decision_role, created_at, updated_at
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
		        is_primary, notities, preferred_channel, decision_role, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)`,
		id, userID, input.CompanyID, name, cleanStringPtr(input.Email), cleanStringPtr(input.Telefoon),
		cleanStringPtr(input.Rol), input.IsPrimary, cleanStringPtr(input.Notities),
		cleanStringPtr(input.PreferredChannel), cleanStringPtr(input.DecisionRole), now)
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
		        notities, preferred_channel, decision_role, created_at, updated_at
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
			preferred_channel = COALESCE($10, preferred_channel),
			decision_role = COALESCE($11, decision_role),
			updated_at = $12
		 WHERE id = $1 AND user_id = $2`,
		id, userID, input.CompanyID, cleanStringPtr(input.Naam), cleanStringPtr(input.Email),
		cleanStringPtr(input.Telefoon), cleanStringPtr(input.Rol), input.IsPrimary,
		cleanStringPtr(input.Notities), cleanStringPtr(input.PreferredChannel),
		cleanStringPtr(input.DecisionRole), now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if input.IsPrimary != nil && *input.IsPrimary {
		companyID := input.CompanyID
		if companyID == nil {
			companyID, err = s.scopedCompanyID(ctx, userID, &id,
				`SELECT company_id FROM lc_contacts WHERE user_id = $1 AND id = $2`)
			if err != nil {
				return err
			}
		}
		if companyID != nil {
			_ = s.clearOtherPrimaryContacts(ctx, userID, id, *companyID)
		}
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

func (s *LaventeCareStore) ListAccessCredentials(ctx context.Context, userID string, limit int, companyID *uuid.UUID) ([]model.LCAccessCredential, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT a.id, a.user_id, a.company_id, a.contact_id, a.project_id, a.workstream_id,
		        a.title, a.login_url, a.username, a.role, a.environment, a.status, a.owner_contact,
		        a.secret_label, (a.secret_value_encrypted IS NOT NULL), a.secret_hint, a.sharing_policy,
		        a.last_checked_at, a.expires_at, a.revoked_at, a.notes, a.created_at, a.updated_at,
		        c.naam, ct.naam, p.naam, w.titel
		   FROM lc_access_credentials a
		   JOIN lc_companies c ON c.id = a.company_id AND c.user_id = a.user_id
		   LEFT JOIN lc_contacts ct ON ct.id = a.contact_id AND ct.user_id = a.user_id
		   LEFT JOIN lc_projects p ON p.id = a.project_id AND p.user_id = a.user_id
		   LEFT JOIN lc_workstreams w ON w.id = a.workstream_id AND w.user_id = a.user_id
		  WHERE a.user_id = $1
		    AND ($2::uuid IS NULL OR a.company_id = $2)
		  ORDER BY
		    CASE a.status WHEN 'actief' THEN 0 WHEN 'tijdelijk' THEN 1 WHEN 'te_controleren' THEN 2 ELSE 3 END,
		    COALESCE(a.expires_at, a.updated_at) ASC
		  LIMIT $3`,
		userID, companyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanAccessCredential)
}

func (s *LaventeCareStore) CountAccessCredentials(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*)::int FROM lc_access_credentials WHERE user_id = $1 AND status <> 'verwijderd'`,
		userID).Scan(&count)
	return count, err
}

func (s *LaventeCareStore) CreateAccessCredential(ctx context.Context, userID string, input model.LCAccessCredentialCreate) (*model.LCAccessCredential, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, pgx.ErrNoRows
	}
	if err := s.validateAccessCredentialTarget(ctx, userID, input.CompanyID, input.ContactID, input.ProjectID, input.WorkstreamID); err != nil {
		return nil, err
	}

	secret, err := encryptLaventeCareSecret(input.SecretValue)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	id := uuid.New()
	status := cleanStatus(input.Status, "actief")
	environment := cleanStatus(input.Environment, "pilot")
	secretLabel := cleanStatus(deref(input.SecretLabel), "wachtwoord")
	sharingPolicy := cleanStatus(deref(input.SharingPolicy), "veilig_kanaal")

	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO lc_access_credentials (
		    id, user_id, company_id, contact_id, project_id, workstream_id, title, login_url,
		    username, role, environment, status, owner_contact, secret_label, secret_value_encrypted,
		    secret_hint, sharing_policy, last_checked_at, expires_at, notes, created_at, updated_at
		 ) VALUES (
		    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$21
		 )`,
		id, userID, input.CompanyID, input.ContactID, input.ProjectID, input.WorkstreamID,
		title, cleanStringPtr(input.LoginURL), cleanStringPtr(input.Username), cleanStringPtr(input.Role),
		environment, status, cleanStringPtr(input.OwnerContact), secretLabel, secret,
		cleanStringPtr(input.SecretHint), sharingPolicy, parseDateTimePtr(input.LastCheckedAt),
		parseDateTimePtr(input.ExpiresAt), cleanStringPtr(input.Notes), now)
	if err != nil {
		return nil, err
	}
	return s.GetAccessCredential(ctx, userID, id)
}

func (s *LaventeCareStore) GetAccessCredential(ctx context.Context, userID string, id uuid.UUID) (*model.LCAccessCredential, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT a.id, a.user_id, a.company_id, a.contact_id, a.project_id, a.workstream_id,
		        a.title, a.login_url, a.username, a.role, a.environment, a.status, a.owner_contact,
		        a.secret_label, (a.secret_value_encrypted IS NOT NULL), a.secret_hint, a.sharing_policy,
		        a.last_checked_at, a.expires_at, a.revoked_at, a.notes, a.created_at, a.updated_at,
		        c.naam, ct.naam, p.naam, w.titel
		   FROM lc_access_credentials a
		   JOIN lc_companies c ON c.id = a.company_id AND c.user_id = a.user_id
		   LEFT JOIN lc_contacts ct ON ct.id = a.contact_id AND ct.user_id = a.user_id
		   LEFT JOIN lc_projects p ON p.id = a.project_id AND p.user_id = a.user_id
		   LEFT JOIN lc_workstreams w ON w.id = a.workstream_id AND w.user_id = a.user_id
		  WHERE a.user_id = $1 AND a.id = $2
		  LIMIT 1`,
		userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, scanAccessCredential)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &items[0], nil
}

func (s *LaventeCareStore) UpdateAccessCredential(ctx context.Context, userID string, id uuid.UUID, input model.LCAccessCredentialUpdate) error {
	current, err := s.GetAccessCredential(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := s.validateAccessCredentialTarget(ctx, userID, current.CompanyID, input.ContactID, input.ProjectID, input.WorkstreamID); err != nil {
		return err
	}

	// setSecret distinguishes "change the secret" (SecretValue provided, possibly
	// empty → clear it) from "leave it" (nil). An empty value encrypts to nil and
	// then clears the column, instead of being silently kept by COALESCE.
	setSecret := input.SecretValue != nil
	var secret *string
	if setSecret {
		secret, err = encryptLaventeCareSecret(input.SecretValue)
		if err != nil {
			return err
		}
	}

	now := time.Now().UTC()
	status := cleanStringPtr(input.Status)
	revokedAt := parseDateTimePtr(input.RevokedAt)
	if status != nil && (*status == "ingetrokken" || *status == "verlopen") && revokedAt == nil {
		revokedAt = &now
	}
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_access_credentials SET
		    contact_id = COALESCE($3, contact_id),
		    project_id = COALESCE($4, project_id),
		    workstream_id = COALESCE($5, workstream_id),
		    title = COALESCE($6, title),
		    login_url = COALESCE($7, login_url),
		    username = COALESCE($8, username),
		    role = COALESCE($9, role),
		    environment = COALESCE($10, environment),
		    status = COALESCE($11, status),
		    owner_contact = COALESCE($12, owner_contact),
		    secret_label = COALESCE($13, secret_label),
		    secret_value_encrypted = CASE WHEN $22 THEN $14 ELSE secret_value_encrypted END,
		    secret_hint = COALESCE($15, secret_hint),
		    sharing_policy = COALESCE($16, sharing_policy),
		    last_checked_at = COALESCE($17, last_checked_at),
		    expires_at = COALESCE($18, expires_at),
		    revoked_at = COALESCE($19, revoked_at),
		    notes = COALESCE($20, notes),
		    updated_at = $21
		  WHERE user_id = $1 AND id = $2`,
		userID, id, input.ContactID, input.ProjectID, input.WorkstreamID,
		cleanStringPtr(input.Title), cleanStringPtr(input.LoginURL), cleanStringPtr(input.Username),
		cleanStringPtr(input.Role), cleanStringPtr(input.Environment), status,
		cleanStringPtr(input.OwnerContact), cleanStringPtr(input.SecretLabel), secret,
		cleanStringPtr(input.SecretHint), cleanStringPtr(input.SharingPolicy),
		parseDateTimePtr(input.LastCheckedAt), parseDateTimePtr(input.ExpiresAt),
		revokedAt, cleanStringPtr(input.Notes), now, setSecret)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) validateAccessCredentialTarget(ctx context.Context, userID string, companyID uuid.UUID, contactID, projectID, workstreamID *uuid.UUID) error {
	if _, err := s.GetCompany(ctx, userID, companyID); err != nil {
		return err
	}
	scope := &companyID
	var err error
	if _, err = s.validateScopedCompanyObject(ctx, userID, scope, contactID,
		`SELECT company_id FROM lc_contacts WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	if _, err = s.validateScopedCompanyObject(ctx, userID, scope, projectID,
		`SELECT company_id FROM lc_projects WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	if _, err = s.validateScopedCompanyObject(ctx, userID, scope, workstreamID,
		`SELECT company_id FROM lc_workstreams WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	return nil
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

// GetLeadBySource returns an existing lead matching (user, bron, source_id), or
// (nil, nil) when none exists. It de-duplicates signal→lead conversion so
// converting the same signal twice doesn't create duplicate leads.
func (s *LaventeCareStore) GetLeadBySource(ctx context.Context, userID, bron, sourceID string) (*model.LCLead, error) {
	if strings.TrimSpace(sourceID) == "" {
		return nil, nil
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, company_id, contact_id, titel, bron, source_id, status,
		        fit_score, pijnpunt, prioriteit, volgende_stap, volgende_actie_datum,
		        created_at, updated_at
		 FROM lc_leads WHERE user_id = $1 AND bron = $2 AND source_id = $3
		 ORDER BY created_at DESC
		 LIMIT 1`, userID, bron, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	leads, err := pgx.CollectRows(rows, scanLead)
	if err != nil {
		return nil, err
	}
	if len(leads) == 0 {
		return nil, nil
	}
	return &leads[0], nil
}

func (s *LaventeCareStore) ListLeads(ctx context.Context, userID string, limit int) ([]model.LCLead, error) {
	query := `SELECT id, user_id, company_id, contact_id, titel, bron, source_id, status,
		        fit_score, pijnpunt, prioriteit, volgende_stap, volgende_actie_datum,
		        created_at, updated_at
		 FROM lc_leads WHERE user_id = $1
		 ORDER BY updated_at DESC`
	args := []any{userID}
	if limit > 0 {
		args = append(args, limit)
		query += ` LIMIT $2`
	}
	rows, err := s.db.Pool.Query(ctx, query, args...)
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
	if err := validateLCStatus(input.Status); err != nil {
		return err
	}
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
			bron = COALESCE($11, bron),
			updated_at = $12
		 WHERE id = $1 AND user_id = $2`,
		id, userID, input.CompanyID, input.ContactID, input.Status, input.FitScore, input.Pijnpunt,
		input.Prioriteit, input.VolgendeStap, input.VolgendeActieDatum, input.Bron, now)
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
	query := `SELECT id, user_id, company_id, lead_id, naam, fase, status,
		        waarde_indicatie, start_datum, deadline, samenvatting,
		        created_at, updated_at
		 FROM lc_projects WHERE user_id = $1
		 ORDER BY updated_at DESC`
	args := []any{userID}
	if limit > 0 {
		args = append(args, limit)
		query += ` LIMIT $2`
	}
	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanProject)
}

func (s *LaventeCareStore) GetProject(ctx context.Context, userID string, id uuid.UUID) (*model.LCProject, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, company_id, lead_id, naam, fase, status,
		        waarde_indicatie, start_datum, deadline, samenvatting,
		        created_at, updated_at
		 FROM lc_projects WHERE user_id = $1 AND id = $2`,
		userID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects, err := pgx.CollectRows(rows, scanProject)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &projects[0], nil
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
	if err := validateLCStatus(input.Status); err != nil {
		return err
	}
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
	fase := "intake"
	if input.Fase != nil {
		fase = *input.Fase
	}
	status := "actief"
	if input.Status != nil {
		status = *input.Status
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Mark the lead won and create the project atomically, so a failure can't
	// strand a 'gewonnen' lead with no project or duplicate the project on retry.
	if _, err := tx.Exec(ctx,
		`UPDATE lc_leads SET status = 'gewonnen', updated_at = now() WHERE user_id = $1 AND id = $2`,
		userID, input.LeadID); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	proj := model.LCProject{
		ID: uuid.New(), UserID: userID, CompanyID: lead.CompanyID, LeadID: &input.LeadID,
		Naam: input.Naam, Fase: fase, Status: status, Samenvatting: input.Samenvatting,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO lc_projects (id, user_id, company_id, lead_id, naam, fase, status,
		        waarde_indicatie, start_datum, deadline, samenvatting, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)`,
		proj.ID, proj.UserID, proj.CompanyID, proj.LeadID, proj.Naam, proj.Fase, proj.Status,
		proj.WaardeIndicatie, proj.StartDatum, proj.Deadline, proj.Samenvatting, proj.CreatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &proj, nil
}

// ─── Workstreams / Opdrachten ───────────────────────────────────────────────

func (s *LaventeCareStore) ListWorkstreams(ctx context.Context, userID string, limit int, includeClosed bool) ([]model.LCWorkstream, error) {
	statusClause := ``
	if !includeClosed {
		statusClause = ` AND status NOT IN ('afgerond','done','gesloten','gearchiveerd','omgezet_project')`
	}
	query := `SELECT id, user_id, company_id, lead_id, project_id, titel, type, status,
		        prioriteit, klant_naam, bron, source_id, doel, scope, deliverable,
		        bevindingen, volgende_stap, deadline, geschatte_minuten,
		        waarde_indicatie, stack_tags, tags, completed_at, created_at, updated_at
		 FROM lc_workstreams WHERE user_id = $1` + statusClause + `
		 ORDER BY CASE WHEN deadline IS NULL THEN 1 ELSE 0 END, deadline ASC, updated_at DESC
		`
	args := []any{userID}
	if limit > 0 {
		args = append(args, limit)
		query += ` LIMIT $2`
	}
	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanWorkstream)
}

func (s *LaventeCareStore) GetWorkstream(ctx context.Context, userID string, id uuid.UUID) (*model.LCWorkstream, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, company_id, lead_id, project_id, titel, type, status,
		        prioriteit, klant_naam, bron, source_id, doel, scope, deliverable,
		        bevindingen, volgende_stap, deadline, geschatte_minuten,
		        waarde_indicatie, stack_tags, tags, completed_at, created_at, updated_at
		 FROM lc_workstreams WHERE user_id = $1 AND id = $2
		 LIMIT 1`, userID, id)
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
	return &workstreams[0], nil
}

func (s *LaventeCareStore) CreateWorkstream(ctx context.Context, userID string, input model.LCWorkstreamCreate) (*model.LCWorkstream, error) {
	id := uuid.New()
	now := time.Now().UTC()
	if input.ProjectID != nil {
		project, err := s.GetProject(ctx, userID, *input.ProjectID)
		if err != nil {
			return nil, err
		}
		if input.CompanyID != nil && project.CompanyID != nil && *input.CompanyID != *project.CompanyID {
			return nil, fmt.Errorf("opdracht en project horen niet bij dezelfde klant")
		}
		if input.CompanyID == nil {
			input.CompanyID = project.CompanyID
		}
	}
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
	if err := validateLCStatus(input.Status); err != nil {
		return err
	}
	now := time.Now().UTC()
	if input.ProjectID != nil {
		project, err := s.GetProject(ctx, userID, *input.ProjectID)
		if err != nil {
			return err
		}
		if input.CompanyID != nil && project.CompanyID != nil && *input.CompanyID != *project.CompanyID {
			return fmt.Errorf("opdracht en project horen niet bij dezelfde klant")
		}
		if input.CompanyID == nil {
			input.CompanyID = project.CompanyID
		}
	}
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
			project_id = COALESCE($4, project_id),
			type = COALESCE($5, type),
			status = COALESCE($6, status),
			prioriteit = COALESCE($7, prioriteit),
			klant_naam = COALESCE($8, klant_naam),
			doel = COALESCE($9, doel),
			scope = COALESCE($10, scope),
			deliverable = COALESCE($11, deliverable),
			bevindingen = COALESCE($12, bevindingen),
			volgende_stap = COALESCE($13, volgende_stap),
			deadline = COALESCE($14, deadline),
			geschatte_minuten = COALESCE($15, geschatte_minuten),
			waarde_indicatie = COALESCE($16, waarde_indicatie),
			stack_tags = COALESCE($17, stack_tags),
			tags = COALESCE($18, tags),
			completed_at = CASE
				WHEN $6 IN ('afgerond','done','gesloten','gearchiveerd','omgezet_project') THEN COALESCE(completed_at, $19)
				WHEN $6 IS NOT NULL THEN NULL
				ELSE completed_at
			END,
			updated_at = $19
		 WHERE id = $1 AND user_id = $2`,
		id, userID, input.CompanyID, input.ProjectID, input.Type, input.Status, input.Prioriteit, input.KlantNaam,
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

	targetProjectID := input.ProjectID
	if targetProjectID == nil {
		targetProjectID = workstream.ProjectID
	}
	if targetProjectID != nil {
		project, err := s.GetProject(ctx, userID, *targetProjectID)
		if err != nil {
			return nil, err
		}
		done := "omgezet_project"
		if err := s.UpdateWorkstream(ctx, userID, input.WorkstreamID, model.LCWorkstreamUpdate{
			ProjectID: &project.ID,
			Status:    &done,
		}); err != nil {
			return nil, err
		}
		return project, nil
	}

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

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Create the project and mark the workstream converted atomically, so a
	// partial failure can't leave a project without its workstream link (or
	// duplicate the project on retry).
	now := time.Now().UTC()
	proj := model.LCProject{
		ID: uuid.New(), UserID: userID, CompanyID: workstream.CompanyID, LeadID: workstream.LeadID,
		Naam: name, Fase: fase, Status: status, WaardeIndicatie: workstream.WaardeIndicatie,
		Deadline: workstream.Deadline, Samenvatting: summary, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO lc_projects (id, user_id, company_id, lead_id, naam, fase, status,
		        waarde_indicatie, start_datum, deadline, samenvatting, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)`,
		proj.ID, proj.UserID, proj.CompanyID, proj.LeadID, proj.Naam, proj.Fase, proj.Status,
		proj.WaardeIndicatie, proj.StartDatum, proj.Deadline, proj.Samenvatting, proj.CreatedAt); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE lc_workstreams SET project_id = $3, status = 'omgezet_project', updated_at = $4
		  WHERE user_id = $1 AND id = $2`,
		userID, input.WorkstreamID, proj.ID, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &proj, nil
}

// ─── Action Items ────────────────────────────────────────────────────────────

func (s *LaventeCareStore) ListActions(ctx context.Context, userID string, limit int) ([]model.LCActionItem, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT a.id, a.user_id, a.source, a.source_id, a.title, a.summary, a.action_type,
		        a.status, a.priority, a.due_date, a.due_time, a.linked_lead_id, a.linked_project_id,
		        a.linked_workstream_id, a.linked_company_id, a.created_at, a.updated_at,
		        co.naam, pr.naam, ws.titel, ld.titel,
		        act.id, act.title, act.occurred_at
		   FROM lc_action_items a
		   LEFT JOIN lc_companies co ON co.id = a.linked_company_id AND co.user_id = a.user_id
		   LEFT JOIN lc_projects pr ON pr.id = a.linked_project_id AND pr.user_id = a.user_id
		   LEFT JOIN lc_workstreams ws ON ws.id = a.linked_workstream_id AND ws.user_id = a.user_id
		   LEFT JOIN lc_leads ld ON ld.id = a.linked_lead_id AND ld.user_id = a.user_id
		   LEFT JOIN LATERAL (
		       SELECT e.id, e.title, e.occurred_at
		         FROM lc_activity_events e
		        WHERE e.action_item_id = a.id AND e.user_id = a.user_id
		        ORDER BY e.occurred_at DESC
		        LIMIT 1
		   ) act ON true
		  WHERE a.user_id = $1 AND a.status IN ('open','bezig','wacht_op_klant')
		  ORDER BY COALESCE(a.due_date, '9999-12-31'), a.updated_at DESC
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
		        action_type, status, priority, due_date, due_time, linked_lead_id, linked_project_id,
		        linked_workstream_id, linked_company_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'open',$8,$9,$10,$11,$12,$13,$14,$15,$15)`,
		id, userID, source, input.SourceID, input.Title, input.Summary,
		actionType, priority, input.DueDate, input.DueTime, input.LinkedLeadID, input.LinkedProjectID,
		input.LinkedWorkstreamID, input.LinkedCompanyID, now)
	if err != nil {
		return nil, err
	}

	return &model.LCActionItem{
		ID: id, UserID: userID, Source: source, SourceID: input.SourceID,
		Title: input.Title, Summary: input.Summary, ActionType: actionType,
		Status: "open", Priority: priority, DueDate: input.DueDate, DueTime: input.DueTime,
		LinkedLeadID: input.LinkedLeadID, LinkedProjectID: input.LinkedProjectID,
		LinkedWorkstreamID: input.LinkedWorkstreamID,
		LinkedCompanyID:    input.LinkedCompanyID,
		CreatedAt:          now, UpdatedAt: now,
	}, nil
}

func (s *LaventeCareStore) UpdateActionStatus(ctx context.Context, userID string, id uuid.UUID, status string) error {
	now := time.Now().UTC()
	var title string
	var summary *string
	var linkedLeadID, linkedProjectID, linkedWorkstreamID, linkedCompanyID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx,
		`UPDATE lc_action_items SET status = $3, updated_at = $4
		 WHERE id = $1 AND user_id = $2
		 RETURNING title, summary, linked_lead_id, linked_project_id, linked_workstream_id, linked_company_id`,
		id, userID, status, now,
	).Scan(&title, &summary, &linkedLeadID, &linkedProjectID, &linkedWorkstreamID, &linkedCompanyID)
	if err != nil {
		return err
	}

	// Close the loop with fix #4: completing an action logs a timeline "moment"
	// on the linked customer dossier, so the dossier history stays complete
	// without manual double-entry. Best-effort — a company-less action (no
	// dossier to attach to) or a validation hiccup must not fail the status
	// update itself.
	if isActionCompletionStatus(status) && linkedCompanyID != nil {
		occurredAt := now.Format(time.RFC3339)
		actionID := id
		_, _ = s.CreateActivityEvent(ctx, userID, model.LCActivityEventCreate{
			CompanyID:    *linkedCompanyID,
			LeadID:       linkedLeadID,
			ProjectID:    linkedProjectID,
			WorkstreamID: linkedWorkstreamID,
			ActionItemID: &actionID,
			EventType:    "actie_afgerond",
			Channel:      "systeem",
			Title:        "Actie afgerond: " + title,
			Body:         summary,
			OccurredAt:   &occurredAt,
		})
	}
	return nil
}

func isActionCompletionStatus(status string) bool {
	return status == "done" || status == "afgerond"
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
		 WHERE user_id = $1 AND (
		        LOWER(document_key) LIKE $2
		        OR LOWER(titel) LIKE $2
		        OR LOWER(COALESCE(fase, '')) LIKE $2
		        OR LOWER(COALESCE(source_path, '')) LIKE $2
		        OR LOWER(samenvatting) LIKE $2
		        OR LOWER(categorie) LIKE $2
		        OR EXISTS (SELECT 1 FROM unnest(tags) AS tag WHERE LOWER(tag) LIKE $2)
		     )
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

type lcDossierAdviceRelations struct {
	CompanyID     *uuid.UUID
	CompanyName   string
	LeadIDs       map[uuid.UUID]bool
	ProjectIDs    map[uuid.UUID]bool
	WorkstreamIDs map[uuid.UUID]bool
	HasActiveWork bool
}

// BuildDossierAdvice returns deterministic, read-only AI guidance for a LaventeCare dossier.
func (s *LaventeCareStore) BuildDossierAdvice(ctx context.Context, userID string, input model.LCDossierAdviceRequest) (*model.LCDossierAdvice, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 8
	}
	if limit > 20 {
		limit = 20
	}

	target, relations, contextText, err := s.resolveDossierAdviceTarget(ctx, userID, input)
	if err != nil {
		return nil, err
	}

	documents, err := s.ListDocuments(ctx, userID)
	if err != nil {
		return nil, err
	}
	dossierDocuments, err := s.listDossierDocumentsForAdvice(ctx, userID, target, relations)
	if err != nil {
		return nil, err
	}
	presentDocuments := filterDossierDocumentsForAdvice(dossierDocuments, target, relations)
	presentByKey := indexDossierDocumentsByKey(presentDocuments)
	totalDossierDocuments, err := s.CountDossierDocuments(ctx, userID)
	if err != nil {
		return nil, err
	}

	requirements := buildDossierRequirements(documents, presentByKey, target, relations)
	recommendations := rankDossierDocumentRecommendations(documents, presentByKey, target, contextText, limit)
	coverage := dossierRequirementCoverage(requirements)
	missingRequirements, attentionRequirements := dossierRequirementIssueCounts(requirements)
	status := "klaar"
	if len(documents) == 0 {
		status = "documentbasis_leeg"
	} else if missingRequirements > 0 {
		status = "onvolledig"
	} else if attentionRequirements > 0 {
		status = "aandacht"
	}

	return &model.LCDossierAdvice{
		GeneratedAt:             time.Now().UTC(),
		Target:                  target,
		Status:                  status,
		Coverage:                coverage,
		Requirements:            requirements,
		Recommendations:         recommendations,
		PresentDocuments:        take(presentDocuments, dossierAdviceResponseSampleSize),
		TotalDossierDocuments:   totalDossierDocuments,
		MatchedDossierDocuments: len(presentDocuments),
		NextActions:             buildDossierAdviceNextActions(requirements, recommendations, documents, target),
		Evidence: []string{
			fmt.Sprintf("%d kennisdocument(en) geindexeerd", len(documents)),
			fmt.Sprintf("%d van %d dossierstuk(ken) passend bij deze context", len(presentDocuments), totalDossierDocuments),
			fmt.Sprintf("Target: %s - %s", target.Kind, target.Title),
		},
	}, nil
}

func (s *LaventeCareStore) resolveDossierAdviceTarget(ctx context.Context, userID string, input model.LCDossierAdviceRequest) (model.LCDossierAdviceTarget, lcDossierAdviceRelations, string, error) {
	targetCount := 0
	for _, id := range []*uuid.UUID{input.CompanyID, input.LeadID, input.ProjectID, input.WorkstreamID} {
		if id != nil {
			targetCount++
		}
	}
	if targetCount > 1 {
		return model.LCDossierAdviceTarget{}, lcDossierAdviceRelations{}, "", fmt.Errorf("%w: kies company_id, lead_id, project_id of workstream_id", ErrInvalidDossierAdviceTarget)
	}

	relations := lcDossierAdviceRelations{
		LeadIDs:       make(map[uuid.UUID]bool),
		ProjectIDs:    make(map[uuid.UUID]bool),
		WorkstreamIDs: make(map[uuid.UUID]bool),
	}
	query := strings.TrimSpace(input.Query)
	if input.CompanyID != nil {
		company, err := s.GetCompany(ctx, userID, *input.CompanyID)
		if err != nil {
			return model.LCDossierAdviceTarget{}, relations, "", err
		}
		relations.CompanyID = &company.ID
		relations.CompanyName = company.Naam
		relations, err = s.expandDossierAdviceRelations(ctx, userID, relations)
		if err != nil {
			return model.LCDossierAdviceTarget{}, relations, "", err
		}
		target := model.LCDossierAdviceTarget{
			Kind:        "company",
			ID:          &company.ID,
			Title:       company.Naam,
			Subtitle:    "klantdossier",
			CompanyID:   &company.ID,
			CompanyName: company.Naam,
			Status:      company.Status,
			Query:       query,
		}
		context := strings.Join([]string{company.Naam, deref(company.Website), deref(company.Sector), company.Status, company.RelatieType, deref(company.Notities), query}, " ")
		return target, relations, context, nil
	}
	if input.ProjectID != nil {
		project, err := s.GetProject(ctx, userID, *input.ProjectID)
		if err != nil {
			return model.LCDossierAdviceTarget{}, relations, "", err
		}
		relations.ProjectIDs[project.ID] = true
		relations.CompanyID = project.CompanyID
		companyName := ""
		if project.CompanyID != nil {
			if company, err := s.GetCompany(ctx, userID, *project.CompanyID); err == nil {
				companyName = company.Naam
			}
		}
		relations.CompanyName = companyName
		relations, err = s.expandDossierAdviceRelations(ctx, userID, relations)
		if err != nil {
			return model.LCDossierAdviceTarget{}, relations, "", err
		}
		target := model.LCDossierAdviceTarget{
			Kind:        "project",
			ID:          &project.ID,
			Title:       project.Naam,
			Subtitle:    "project",
			CompanyID:   project.CompanyID,
			CompanyName: companyName,
			Phase:       project.Fase,
			Status:      project.Status,
			Query:       query,
		}
		context := strings.Join([]string{project.Naam, project.Fase, project.Status, deref(project.Samenvatting), companyName, query}, " ")
		return target, relations, context, nil
	}
	if input.WorkstreamID != nil {
		workstream, err := s.GetWorkstream(ctx, userID, *input.WorkstreamID)
		if err != nil {
			return model.LCDossierAdviceTarget{}, relations, "", err
		}
		relations.WorkstreamIDs[workstream.ID] = true
		if workstream.ProjectID != nil {
			relations.ProjectIDs[*workstream.ProjectID] = true
		}
		if workstream.LeadID != nil {
			relations.LeadIDs[*workstream.LeadID] = true
		}
		relations.CompanyID = workstream.CompanyID
		companyName := deref(workstream.KlantNaam)
		if companyName == "" && workstream.CompanyID != nil {
			if company, err := s.GetCompany(ctx, userID, *workstream.CompanyID); err == nil {
				companyName = company.Naam
			}
		}
		relations.CompanyName = companyName
		relations, err = s.expandDossierAdviceRelations(ctx, userID, relations)
		if err != nil {
			return model.LCDossierAdviceTarget{}, relations, "", err
		}
		target := model.LCDossierAdviceTarget{
			Kind:        "workstream",
			ID:          &workstream.ID,
			Title:       workstream.Titel,
			Subtitle:    "opdracht",
			CompanyID:   workstream.CompanyID,
			CompanyName: companyName,
			Phase:       workstream.Type,
			Status:      workstream.Status,
			Priority:    workstream.Prioriteit,
			Query:       query,
		}
		context := strings.Join([]string{
			workstream.Titel, workstream.Type, workstream.Status, workstream.Prioriteit,
			deref(workstream.KlantNaam), deref(workstream.Doel), deref(workstream.Scope),
			deref(workstream.Deliverable), deref(workstream.Bevindingen), deref(workstream.VolgendeStap),
			strings.Join(workstream.StackTags, " "), strings.Join(workstream.Tags, " "), query,
		}, " ")
		return target, relations, context, nil
	}
	if input.LeadID != nil {
		lead, err := s.GetLead(ctx, userID, *input.LeadID)
		if err != nil {
			return model.LCDossierAdviceTarget{}, relations, "", err
		}
		relations.LeadIDs[lead.ID] = true
		relations.CompanyID = lead.CompanyID
		companyName := ""
		if lead.CompanyID != nil {
			if company, err := s.GetCompany(ctx, userID, *lead.CompanyID); err == nil {
				companyName = company.Naam
			}
		}
		relations.CompanyName = companyName
		relations, err = s.expandDossierAdviceRelations(ctx, userID, relations)
		if err != nil {
			return model.LCDossierAdviceTarget{}, relations, "", err
		}
		target := model.LCDossierAdviceTarget{
			Kind:        "lead",
			ID:          &lead.ID,
			Title:       lead.Titel,
			Subtitle:    "lead",
			CompanyID:   lead.CompanyID,
			CompanyName: companyName,
			Status:      lead.Status,
			Priority:    deref(lead.Prioriteit),
			Query:       query,
		}
		context := strings.Join([]string{lead.Titel, lead.Bron, lead.Status, deref(lead.Pijnpunt), deref(lead.VolgendeStap), deref(lead.Prioriteit), companyName, query}, " ")
		return target, relations, context, nil
	}

	title := "LaventeCare"
	kind := "laventecare"
	subtitle := "algemene bedrijfscontext"
	if query != "" && !isGenericDossierQuery(query) {
		title = query
		kind = "query"
		subtitle = "zoekcontext"
	}
	target := model.LCDossierAdviceTarget{Kind: kind, Title: title, Subtitle: subtitle, Query: query}
	return target, relations, query, nil
}

func (s *LaventeCareStore) expandDossierAdviceRelations(ctx context.Context, userID string, relations lcDossierAdviceRelations) (lcDossierAdviceRelations, error) {
	leads, err := s.ListLeads(ctx, userID, 0)
	if err != nil {
		return relations, err
	}
	projects, err := s.ListProjects(ctx, userID, 0)
	if err != nil {
		return relations, err
	}
	workstreams, err := s.ListWorkstreams(ctx, userID, 0, true)
	if err != nil {
		return relations, err
	}
	for _, lead := range leads {
		if relations.CompanyID != nil && lead.CompanyID != nil && *lead.CompanyID == *relations.CompanyID {
			relations.LeadIDs[lead.ID] = true
			if isOpenStatus(lead.Status) {
				relations.HasActiveWork = true
			}
		}
	}
	for _, project := range projects {
		if relations.CompanyID != nil && project.CompanyID != nil && *project.CompanyID == *relations.CompanyID {
			relations.ProjectIDs[project.ID] = true
		}
		if relations.ProjectIDs[project.ID] && isOpenStatus(project.Status) {
			relations.HasActiveWork = true
		}
	}
	for _, workstream := range workstreams {
		if relations.CompanyID != nil && workstream.CompanyID != nil && *workstream.CompanyID == *relations.CompanyID {
			relations.WorkstreamIDs[workstream.ID] = true
		}
		if workstream.ProjectID != nil && relations.ProjectIDs[*workstream.ProjectID] {
			relations.WorkstreamIDs[workstream.ID] = true
		}
		if workstream.LeadID != nil && relations.LeadIDs[*workstream.LeadID] {
			relations.WorkstreamIDs[workstream.ID] = true
		}
		if relations.WorkstreamIDs[workstream.ID] && isOpenStatus(workstream.Status) {
			relations.HasActiveWork = true
		}
	}
	return relations, nil
}

func (s *LaventeCareStore) listDossierDocumentsForAdvice(ctx context.Context, userID string, target model.LCDossierAdviceTarget, relations lcDossierAdviceRelations) ([]model.LCDossierDocument, error) {
	base := `SELECT id, user_id, document_key, titel, template_label, context_type,
		        context_id, context_title, lead_id, project_id, workstream_id, company_id, pdf_url, theme,
		        delivery, notes, generated_at, created_at
		 FROM lc_dossier_documents
		 WHERE user_id = $1`
	args := []any{userID}
	conditions := make([]string, 0, 8)

	addTextCondition := func(sql string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(sql, len(args)))
	}
	addSearchCondition := func(value string) {
		args = append(args, value)
		idx := len(args)
		conditions = append(conditions, fmt.Sprintf(`(
			LOWER(document_key) LIKE $%[1]d
			OR LOWER(titel) LIKE $%[1]d
			OR LOWER(COALESCE(template_label, '')) LIKE $%[1]d
			OR LOWER(context_type) LIKE $%[1]d
			OR LOWER(COALESCE(context_title, '')) LIKE $%[1]d
			OR LOWER(COALESCE(notes, '')) LIKE $%[1]d
		)`, idx))
	}
	addUUIDSetCondition := func(column string, ids []uuid.UUID) {
		if len(ids) == 0 {
			return
		}
		addTextCondition(column+`::text = ANY($%d)`, uuidStrings(ids))
	}

	if target.ID != nil {
		addTextCondition(`context_id = $%d`, target.ID.String())
	}
	if relations.CompanyID != nil {
		addUUIDSetCondition("company_id", []uuid.UUID{*relations.CompanyID})
	}
	addUUIDSetCondition("lead_id", uuidKeys(relations.LeadIDs))
	addUUIDSetCondition("project_id", uuidKeys(relations.ProjectIDs))
	addUUIDSetCondition("workstream_id", uuidKeys(relations.WorkstreamIDs))
	if relations.CompanyName != "" {
		addTextCondition(`LOWER(COALESCE(context_title, '')) LIKE $%d`, "%"+normalize(relations.CompanyName)+"%")
	}

	query := strings.TrimSpace(target.Query)
	if target.Kind == "query" && query != "" {
		addSearchCondition("%" + normalize(query) + "%")
	}

	sql := base
	if len(conditions) > 0 {
		sql += ` AND (` + strings.Join(conditions, ` OR `) + `)`
	}
	sql += ` ORDER BY created_at DESC`

	rows, err := s.db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanDossierDocument)
}

func filterDossierDocumentsForAdvice(docs []model.LCDossierDocument, target model.LCDossierAdviceTarget, relations lcDossierAdviceRelations) []model.LCDossierDocument {
	if isGlobalDossierAdviceTarget(target) || target.Kind == "query" {
		return docs
	}
	matches := make([]model.LCDossierDocument, 0)
	for _, doc := range docs {
		if target.ID != nil && doc.ContextID != nil && strings.EqualFold(strings.TrimSpace(*doc.ContextID), target.ID.String()) {
			matches = append(matches, doc)
			continue
		}
		if relations.CompanyID != nil && doc.CompanyID != nil && *doc.CompanyID == *relations.CompanyID {
			matches = append(matches, doc)
			continue
		}
		if doc.LeadID != nil && relations.LeadIDs[*doc.LeadID] {
			matches = append(matches, doc)
			continue
		}
		if doc.ProjectID != nil && relations.ProjectIDs[*doc.ProjectID] {
			matches = append(matches, doc)
			continue
		}
		if doc.WorkstreamID != nil && relations.WorkstreamIDs[*doc.WorkstreamID] {
			matches = append(matches, doc)
			continue
		}
		if relations.CompanyName != "" && doc.ContextTitle != nil && strings.Contains(normalize(*doc.ContextTitle), normalize(relations.CompanyName)) {
			matches = append(matches, doc)
		}
	}
	return matches
}

func indexDossierDocumentsByKey(docs []model.LCDossierDocument) map[string]model.LCDossierDocument {
	byKey := make(map[string]model.LCDossierDocument, len(docs))
	for _, doc := range docs {
		key := normalize(doc.DocumentKey)
		if key == "" {
			continue
		}
		if existing, ok := byKey[key]; ok && existing.CreatedAt.After(doc.CreatedAt) {
			continue
		}
		byKey[key] = doc
	}
	return byKey
}

func rankDossierDocumentRecommendations(documents []model.LCDocument, presentByKey map[string]model.LCDossierDocument, target model.LCDossierAdviceTarget, contextText string, limit int) []model.LCDocumentRecommendation {
	recommendations := make([]model.LCDocumentRecommendation, 0, len(documents))
	for _, doc := range documents {
		score, reasons := scoreDossierDocument(doc, target, contextText)
		key := normalize(doc.DocumentKey)
		present, alreadyInDossier := presentByKey[key]
		if alreadyInDossier {
			score -= 12
			reasons = append(reasons, "staat al in dit dossier")
		} else {
			score += 6
			reasons = append(reasons, "nog niet vastgelegd in dit dossier")
		}
		if score < 12 && strings.TrimSpace(contextText) != "" {
			continue
		}
		priority := "normaal"
		if score >= 70 && !alreadyInDossier {
			priority = "hoog"
		} else if score < 35 {
			priority = "laag"
		}
		usage := "interne referentie"
		if doc.Categorie == "commercieel" {
			usage = "klantcommunicatie"
		} else if doc.Categorie == "governance" {
			usage = "controle en afspraken"
		} else if doc.Categorie == "proces" {
			usage = "delivery-dossier"
		}
		var dossierID *uuid.UUID
		var dossierCreatedAt *time.Time
		if alreadyInDossier {
			id := present.ID
			createdAt := present.CreatedAt
			dossierID = &id
			dossierCreatedAt = &createdAt
		}
		recommendations = append(recommendations, model.LCDocumentRecommendation{
			Document:          doc,
			Score:             score,
			Priority:          priority,
			Usage:             usage,
			Reasons:           dedupeStrings(reasons),
			AlreadyInDossier:  alreadyInDossier,
			DossierDocumentID: dossierID,
			DossierCreatedAt:  dossierCreatedAt,
		})
	}
	sort.SliceStable(recommendations, func(i, j int) bool {
		if recommendations[i].AlreadyInDossier != recommendations[j].AlreadyInDossier {
			return !recommendations[i].AlreadyInDossier
		}
		if recommendations[i].Score == recommendations[j].Score {
			return recommendations[i].Document.Titel < recommendations[j].Document.Titel
		}
		return recommendations[i].Score > recommendations[j].Score
	})
	return take(recommendations, limit)
}

func scoreDossierDocument(doc model.LCDocument, target model.LCDossierAdviceTarget, contextText string) (int, []string) {
	text := normalize(strings.Join([]string{
		doc.DocumentKey,
		doc.Titel,
		doc.Categorie,
		deref(doc.Fase),
		doc.Samenvatting,
		strings.Join(doc.Tags, " "),
	}, " "))
	context := normalize(strings.Join([]string{contextText, target.Kind, target.Title, target.Subtitle, target.Phase, target.Status, target.Priority, target.CompanyName, target.Query}, " "))
	score := 0
	var reasons []string

	if doc.Categorie == "commercieel" && (target.Kind == "lead" || strings.Contains(context, "voorstel") || strings.Contains(context, "offerte") || strings.Contains(context, "scope")) {
		score += 30
		reasons = append(reasons, "commercieel document past bij intake/voorstel")
	}
	if doc.Categorie == "proces" && (target.Kind == "project" || target.Kind == "workstream" || strings.Contains(context, "pilot") || strings.Contains(context, "uitvoering") || strings.Contains(context, "realisatie")) {
		score += 30
		reasons = append(reasons, "procesdocument past bij delivery of pilot")
	}
	if doc.Categorie == "governance" && (strings.Contains(context, "sla") || strings.Contains(context, "privacy") || strings.Contains(context, "security") || strings.Contains(context, "productie") || strings.Contains(context, "live")) {
		score += 30
		reasons = append(reasons, "governance relevant voor afspraken, privacy of beheer")
	}
	if target.Phase != "" && strings.Contains(text, normalize(target.Phase)) {
		score += 25
		reasons = append(reasons, "fase/type matcht met context")
	}
	if target.Status != "" && strings.Contains(text, normalize(target.Status)) {
		score += 10
		reasons = append(reasons, "status komt terug in documentcontext")
	}
	for _, marker := range []string{"pilot", "test", "scope", "analyse", "website", "integratie", "automatisering", "ai", "crm", "lead", "support", "sla", "factuur", "offerte"} {
		if strings.Contains(context, marker) && strings.Contains(text, marker) {
			score += 14
			reasons = append(reasons, fmt.Sprintf("%s sluit aan op context", marker))
		}
	}
	for _, token := range dossierAdviceTokens(context) {
		if strings.Contains(text, token) {
			score += 4
		}
	}
	if score == 0 && isGlobalDossierAdviceTarget(target) {
		score = 15
		reasons = append(reasons, "algemene LaventeCare documentbasis")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "beperkte match op metadata")
	}
	return score, reasons
}

func buildDossierRequirements(documents []model.LCDocument, presentByKey map[string]model.LCDossierDocument, target model.LCDossierAdviceTarget, relations lcDossierAdviceRelations) []model.LCDossierRequirement {
	if isGlobalDossierAdviceTarget(target) {
		return buildGlobalDossierRequirements(documents, presentByKey, target)
	}

	hasDocs := len(documents) > 0
	hasPresent := len(presentByKey) > 0
	context := normalize(strings.Join([]string{target.Kind, target.Title, target.Phase, target.Status, target.Priority, target.Query}, " "))
	requiresDelivery := relations.HasActiveWork || target.Kind == "project" || target.Kind == "workstream" || strings.Contains(context, "pilot") || strings.Contains(context, "uitvoering") || strings.Contains(context, "realisatie")
	requiresGovernance := strings.Contains(context, "live") || strings.Contains(context, "productie") || strings.Contains(context, "sla") || strings.Contains(context, "privacy") || strings.Contains(context, "security")

	requirements := []model.LCDossierRequirement{
		{
			Key:    "customer_context",
			Label:  "Klantcontext",
			Status: boolStatus(target.CompanyID != nil, "ok", "attention"),
			Reason: boolReason(target.CompanyID != nil, "Klantdossier is gekoppeld.", "Koppel dit advies bij voorkeur aan een bestaand klantdossier/company_id."),
		},
		{
			Key:                     "knowledge_catalog",
			Label:                   "Kennisbank",
			Status:                  boolStatus(hasDocs, "ok", "missing"),
			Reason:                  boolReason(hasDocs, "Documentbasis is beschikbaar voor AI-advies.", "Documentbasis is leeg; initialiseer de LaventeCare kennisbank."),
			RecommendedDocumentKeys: documentKeysByCategory(documents, "commercieel", 3),
		},
		{
			Key:                     "commercial_scope",
			Label:                   "Scope en voorstel",
			Status:                  boolStatus(hasPresentCategory(documents, presentByKey, "commercieel"), "ok", "attention"),
			Reason:                  boolReason(hasPresentCategory(documents, presentByKey, "commercieel"), "Commerciele scope/voorstel is al in het dossier vastgelegd.", "Leg minimaal een scope-, analyse- of voorsteldocument vast."),
			RecommendedDocumentKeys: documentKeysByCategory(documents, "commercieel", 4),
		},
		{
			Key:                     "delivery_plan",
			Label:                   "Aanpak en delivery",
			Status:                  deliveryRequirementStatus(requiresDelivery, hasPresentCategory(documents, presentByKey, "proces")),
			Reason:                  deliveryRequirementReason(requiresDelivery, hasPresentCategory(documents, presentByKey, "proces")),
			RecommendedDocumentKeys: documentKeysByCategory(documents, "proces", 4),
		},
		{
			Key:                     "governance",
			Label:                   "Afspraken en governance",
			Status:                  governanceRequirementStatus(requiresGovernance, hasPresentCategory(documents, presentByKey, "governance")),
			Reason:                  governanceRequirementReason(requiresGovernance, hasPresentCategory(documents, presentByKey, "governance")),
			RecommendedDocumentKeys: documentKeysByCategory(documents, "governance", 4),
		},
		{
			Key:    "dossier_history",
			Label:  "PDF dossierhistorie",
			Status: boolStatus(hasPresent, "ok", "attention"),
			Reason: boolReason(hasPresent, "Er zijn dossierstukken aan deze context gekoppeld.", "Er is nog geen PDF dossierhistorie voor deze context."),
		},
	}
	return requirements
}

func buildGlobalDossierRequirements(documents []model.LCDocument, presentByKey map[string]model.LCDossierDocument, target model.LCDossierAdviceTarget) []model.LCDossierRequirement {
	hasDocs := len(documents) > 0
	hasPresent := len(presentByKey) > 0
	hasCommercial := len(documentKeysByCategory(documents, "commercieel", 1)) > 0
	hasProcess := len(documentKeysByCategory(documents, "proces", 1)) > 0
	hasGovernance := len(documentKeysByCategory(documents, "governance", 1)) > 0
	contextStatus := "ok"
	contextReason := "Algemene LaventeCare-scan actief; gebruik klant-, project- of opdrachtcontext voor definitieve dossierchecks."
	if target.Kind == "query" {
		contextStatus = "attention"
		contextReason = "Zoekcontext zonder vaste klant; koppel aan een klant/project zodra dit advies besluitvorming of klantcommunicatie raakt."
	}

	return []model.LCDossierRequirement{
		{
			Key:    "business_context",
			Label:  "Bedrijfscontext",
			Status: contextStatus,
			Reason: contextReason,
		},
		{
			Key:                     "knowledge_catalog",
			Label:                   "Kennisbank",
			Status:                  boolStatus(hasDocs, "ok", "missing"),
			Reason:                  boolReason(hasDocs, "Documentbasis is beschikbaar voor AI-advies.", "Documentbasis is leeg; initialiseer de LaventeCare kennisbank."),
			RecommendedDocumentKeys: documentKeysByCategory(documents, "commercieel", 3),
		},
		{
			Key:                     "commercial_templates",
			Label:                   "Commercie",
			Status:                  boolStatus(hasCommercial, "ok", "missing"),
			Reason:                  boolReason(hasCommercial, "Commerciele templates zijn beschikbaar voor voorstellen, pilots en offertes.", "Voeg commerciele templates toe voor intake, analyse, voorstel en pilot."),
			RecommendedDocumentKeys: documentKeysByCategory(documents, "commercieel", 4),
		},
		{
			Key:                     "delivery_templates",
			Label:                   "Delivery",
			Status:                  boolStatus(hasProcess, "ok", "attention"),
			Reason:                  boolReason(hasProcess, "Proces- en deliverydocumenten zijn beschikbaar.", "Voeg procesdocumenten toe voor projectaanpak, pilot en oplevering."),
			RecommendedDocumentKeys: documentKeysByCategory(documents, "proces", 4),
		},
		{
			Key:                     "governance_templates",
			Label:                   "Governance",
			Status:                  boolStatus(hasGovernance, "ok", "attention"),
			Reason:                  boolReason(hasGovernance, "Governance-, privacy- en beheerafspraken zijn beschikbaar.", "Voeg governance-documenten toe voor privacy, SLA, beheer en changes."),
			RecommendedDocumentKeys: documentKeysByCategory(documents, "governance", 4),
		},
		{
			Key:    "dossier_history",
			Label:  "Dossierhistorie",
			Status: boolStatus(hasPresent, "ok", "attention"),
			Reason: boolReason(hasPresent, "Er zijn al PDF dossierstukken gegenereerd.", "Er zijn nog geen PDF dossierstukken vastgelegd."),
		},
	}
}

func dossierRequirementCoverage(requirements []model.LCDossierRequirement) int {
	if len(requirements) == 0 {
		return 0
	}
	score := 0
	for _, req := range requirements {
		switch req.Status {
		case "ok":
			score += 100
		case "attention":
			score += 50
		}
	}
	return score / len(requirements)
}

func dossierRequirementIssueCounts(requirements []model.LCDossierRequirement) (missing int, attention int) {
	for _, req := range requirements {
		switch req.Status {
		case "missing":
			missing++
		case "attention":
			attention++
		}
	}
	return missing, attention
}

func buildDossierAdviceNextActions(requirements []model.LCDossierRequirement, recommendations []model.LCDocumentRecommendation, documents []model.LCDocument, target model.LCDossierAdviceTarget) []string {
	actions := make([]string, 0, 4)
	if len(documents) == 0 {
		return []string{"Initialiseer de LaventeCare documentbasis voordat AI-advies betrouwbaar is."}
	}
	if target.CompanyID == nil && target.Kind != "query" && target.Kind != "laventecare" {
		actions = append(actions, "Koppel deze context aan een klantdossier zodat notities, agenda, documenten en facturen samenkomen.")
	}
	for _, req := range requirements {
		if req.Status == "missing" || req.Status == "attention" {
			actions = append(actions, req.Reason)
		}
		if len(actions) >= 3 {
			break
		}
	}
	for _, rec := range recommendations {
		if !rec.AlreadyInDossier {
			actions = append(actions, fmt.Sprintf("Maak of controleer '%s' als eerstvolgend dossierstuk.", rec.Document.Titel))
			break
		}
	}
	if len(actions) == 0 {
		actions = append(actions, "Dossier is op hoofdlijnen bruikbaar; blijf nieuwe klantmomenten en documenten consequent koppelen.")
	}
	return dedupeStrings(actions)
}

func hasPresentCategory(documents []model.LCDocument, presentByKey map[string]model.LCDossierDocument, category string) bool {
	for _, doc := range documents {
		if doc.Categorie == category && presentByKey[normalize(doc.DocumentKey)].ID != uuid.Nil {
			return true
		}
	}
	return false
}

func documentKeysByCategory(documents []model.LCDocument, category string, limit int) []string {
	keys := make([]string, 0, limit)
	for _, doc := range documents {
		if doc.Categorie != category {
			continue
		}
		keys = append(keys, doc.DocumentKey)
		if len(keys) >= limit {
			break
		}
	}
	return keys
}

func isGlobalDossierAdviceTarget(target model.LCDossierAdviceTarget) bool {
	return target.Kind == "laventecare"
}

func isGenericDossierQuery(query string) bool {
	switch normalize(query) {
	case "", "laventecare", "lc", "kennisbank", "dossier", "documenten", "templates":
		return true
	default:
		return false
	}
}

func uuidKeys(values map[uuid.UUID]bool) []uuid.UUID {
	if len(values) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(values))
	for id, ok := range values {
		if ok {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i].String() < ids[j].String()
	})
	return ids
}

func uuidStrings(values []uuid.UUID) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	return out
}

func dossierAdviceTokens(value string) []string {
	fields := strings.FieldsFunc(normalize(value), func(r rune) bool {
		return r < '0' || (r > '9' && r < 'a') || r > 'z'
	})
	tokens := make([]string, 0, len(fields))
	seen := make(map[string]bool)
	for _, field := range fields {
		if len(field) < 4 || seen[field] {
			continue
		}
		seen[field] = true
		tokens = append(tokens, field)
		if len(tokens) >= 20 {
			break
		}
	}
	return tokens
}

func boolStatus(ok bool, okStatus, fallback string) string {
	if ok {
		return okStatus
	}
	return fallback
}

func boolReason(ok bool, okReason, fallback string) string {
	if ok {
		return okReason
	}
	return fallback
}

func deliveryRequirementStatus(required, present bool) string {
	if present {
		return "ok"
	}
	if required {
		return "missing"
	}
	return "attention"
}

func deliveryRequirementReason(required, present bool) string {
	if present {
		return "Aanpak, pilot of delivery-document is al vastgelegd."
	}
	if required {
		return "Actieve opdracht/project vraagt om een aanpak-, pilot- of delivery-document."
	}
	return "Nog geen delivery-document vastgelegd; nuttig zodra er uitvoering start."
}

func governanceRequirementStatus(required, present bool) string {
	if present {
		return "ok"
	}
	if required {
		return "missing"
	}
	return "attention"
}

func governanceRequirementReason(required, present bool) string {
	if present {
		return "Governance-, privacy- of beheerafspraken zijn vastgelegd."
	}
	if required {
		return "Live/productie/privacy/SLA-context vraagt om governance-documentatie."
	}
	return "Governance-documentatie is nog niet gekoppeld; relevant bij livegang, SLA of privacygevoelige data."
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func (s *LaventeCareStore) ListActivityEvents(ctx context.Context, userID string, limit int, companyID *uuid.UUID) ([]model.LCActivityEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}

	query := `SELECT e.id, e.user_id, e.company_id, e.contact_id, e.lead_id,
		        e.project_id, e.workstream_id, e.action_item_id, e.event_type, e.channel,
		        e.title, e.body, e.occurred_at, e.created_at, e.updated_at,
		        c.naam, ct.naam, p.naam, w.titel, ai.title, ai.status
		   FROM lc_activity_events e
		   JOIN lc_companies c ON c.id = e.company_id AND c.user_id = e.user_id
		   LEFT JOIN lc_contacts ct ON ct.id = e.contact_id AND ct.user_id = e.user_id
		   LEFT JOIN lc_projects p ON p.id = e.project_id AND p.user_id = e.user_id
		   LEFT JOIN lc_workstreams w ON w.id = e.workstream_id AND w.user_id = e.user_id
		   LEFT JOIN lc_action_items ai ON ai.id = e.action_item_id AND ai.user_id = e.user_id
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
		// Backdating a moment (logging historical contact history) must not
		// regress laatste_contact past a genuinely more recent one already
		// on record — only advance it, never rewind it.
		_, _ = s.db.Pool.Exec(ctx,
			`UPDATE lc_companies
			    SET laatste_contact = $1, updated_at = $2
			  WHERE user_id = $3 AND id = $4
			    AND (laatste_contact IS NULL OR laatste_contact < $1)`,
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

func (s *LaventeCareStore) ensureCompanyExists(ctx context.Context, userID string, companyID *uuid.UUID) error {
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
	return nil
}

func (s *LaventeCareStore) scopedCompanyID(ctx context.Context, userID string, id *uuid.UUID, query string) (*uuid.UUID, error) {
	if id == nil {
		return nil, nil
	}
	var companyID *uuid.UUID
	if err := s.db.Pool.QueryRow(ctx, query, userID, *id).Scan(&companyID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, pgx.ErrNoRows
		}
		return nil, err
	}
	return companyID, nil
}

func mergeCompanyScope(expected, actual *uuid.UUID) (*uuid.UUID, error) {
	if actual == nil {
		return expected, nil
	}
	if expected == nil {
		id := *actual
		return &id, nil
	}
	if *expected != *actual {
		return nil, pgx.ErrNoRows
	}
	return expected, nil
}

func (s *LaventeCareStore) validateScopedCompanyObject(ctx context.Context, userID string, expectedCompanyID *uuid.UUID, id *uuid.UUID, query string) (*uuid.UUID, error) {
	companyID, err := s.scopedCompanyID(ctx, userID, id, query)
	if err != nil {
		return nil, err
	}
	return mergeCompanyScope(expectedCompanyID, companyID)
}

func (s *LaventeCareStore) validateDossierDocumentTarget(ctx context.Context, userID string, companyID, leadID, projectID, workstreamID *uuid.UUID) error {
	if err := s.ensureCompanyExists(ctx, userID, companyID); err != nil {
		return err
	}
	scope := companyID
	var err error
	if scope, err = s.validateScopedCompanyObject(ctx, userID, scope, leadID,
		`SELECT company_id FROM lc_leads WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	if scope, err = s.validateScopedCompanyObject(ctx, userID, scope, projectID,
		`SELECT company_id FROM lc_projects WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	if _, err = s.validateScopedCompanyObject(ctx, userID, scope, workstreamID,
		`SELECT company_id FROM lc_workstreams WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	return nil
}

func (s *LaventeCareStore) validateActivityEventTarget(ctx context.Context, userID string, input model.LCActivityEventCreate) error {
	scope := &input.CompanyID
	if err := s.ensureCompanyExists(ctx, userID, scope); err != nil {
		return err
	}
	var err error
	if scope, err = s.validateScopedCompanyObject(ctx, userID, scope, input.ContactID,
		`SELECT company_id FROM lc_contacts WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	if scope, err = s.validateScopedCompanyObject(ctx, userID, scope, input.LeadID,
		`SELECT company_id FROM lc_leads WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	if scope, err = s.validateScopedCompanyObject(ctx, userID, scope, input.ProjectID,
		`SELECT company_id FROM lc_projects WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	if scope, err = s.validateScopedCompanyObject(ctx, userID, scope, input.WorkstreamID,
		`SELECT company_id FROM lc_workstreams WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	if _, err = s.validateScopedCompanyObject(ctx, userID, scope, input.ActionItemID,
		`SELECT COALESCE(a.linked_company_id, l.company_id, p.company_id, w.company_id)
		   FROM lc_action_items a
		   LEFT JOIN lc_leads l ON l.user_id = a.user_id AND l.id = a.linked_lead_id
		   LEFT JOIN lc_projects p ON p.user_id = a.user_id AND p.id = a.linked_project_id
		   LEFT JOIN lc_workstreams w ON w.user_id = a.user_id AND w.id = a.linked_workstream_id
		  WHERE a.user_id = $1 AND a.id = $2`); err != nil {
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

	summary, err := s.getBillingSummary(ctx, userID, companyID)
	if err != nil {
		return nil, err
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

func (s *LaventeCareStore) getBillingSummary(ctx context.Context, userID string, companyID *uuid.UUID) (model.LCBillingSummary, error) {
	var quotes, openQuotes, timeEntries, invoices, openInvoices int64
	var billableMinutes, uninvoicedMinutes, outstandingCents, paidCents int64
	err := s.db.Pool.QueryRow(ctx,
		`SELECT
		    (SELECT COUNT(*) FROM lc_quotes q WHERE q.user_id = $1 AND ($2::uuid IS NULL OR q.company_id = $2)),
		    (SELECT COUNT(*) FROM lc_quotes q WHERE q.user_id = $1 AND ($2::uuid IS NULL OR q.company_id = $2) AND q.status NOT IN ('vervallen','geweigerd','geaccepteerd')),
		    (SELECT COUNT(*) FROM lc_time_entries t WHERE t.user_id = $1 AND ($2::uuid IS NULL OR t.company_id = $2)),
		    (SELECT COALESCE(SUM(t.minutes) FILTER (WHERE t.billable), 0) FROM lc_time_entries t WHERE t.user_id = $1 AND ($2::uuid IS NULL OR t.company_id = $2)),
		    (SELECT COALESCE(SUM(t.minutes) FILTER (WHERE t.billable AND t.invoice_id IS NULL AND t.status <> 'afgeschreven'), 0) FROM lc_time_entries t WHERE t.user_id = $1 AND ($2::uuid IS NULL OR t.company_id = $2)),
		    (SELECT COUNT(*) FROM lc_invoices i WHERE i.user_id = $1 AND ($2::uuid IS NULL OR i.company_id = $2)),
		    (SELECT COUNT(*) FROM lc_invoices i WHERE i.user_id = $1 AND ($2::uuid IS NULL OR i.company_id = $2) AND i.status NOT IN ('betaald','geannuleerd')),
		    (SELECT COALESCE(SUM(GREATEST(i.total_cents - i.paid_cents, 0)) FILTER (WHERE i.status NOT IN ('betaald','geannuleerd')), 0) FROM lc_invoices i WHERE i.user_id = $1 AND ($2::uuid IS NULL OR i.company_id = $2)),
		    (SELECT COALESCE(SUM(i.paid_cents), 0) FROM lc_invoices i WHERE i.user_id = $1 AND ($2::uuid IS NULL OR i.company_id = $2))`,
		userID, companyID,
	).Scan(&quotes, &openQuotes, &timeEntries, &billableMinutes, &uninvoicedMinutes, &invoices, &openInvoices, &outstandingCents, &paidCents)
	if err != nil {
		return model.LCBillingSummary{}, err
	}
	return model.LCBillingSummary{
		Quotes:              int(quotes),
		OpenQuotes:          int(openQuotes),
		TimeEntries:         int(timeEntries),
		BillableMinutes:     int(billableMinutes),
		UninvoicedMinutes:   int(uninvoicedMinutes),
		Invoices:            int(invoices),
		OpenInvoices:        int(openInvoices),
		OutstandingCents:    int(outstandingCents),
		PaidCents:           int(paidCents),
		DefaultProvider:     "bunq",
		BunqReady:           bunqBillingConfigured(),
		NextStepDescription: "Maak een conceptfactuur vanuit uren en activeer daarna bunq betaalverzoeken met bevestiging.",
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
	id := uuid.New()

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Allocate the quote number inside the tx so concurrent creates can't collide.
	number, err := s.nextLCNumber(ctx, tx, userID, "lc_quotes", "quote_number", "LC-OFF")
	if err != nil {
		return nil, err
	}

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
	quote, err := s.GetQuote(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := validateQuoteStatusTransition(quote.Status, status); err != nil {
		return err
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
		        i.payment_url, i.document_url, i.document_generated_at, i.ubl_xml, i.ubl_generated_at,
		        i.payment_checked_at, i.payment_status, i.payment_last_error, i.reminder_count,
		        i.last_reminder_at, i.sent_at, i.paid_at, i.notes, i.created_at, i.updated_at,
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

func (s *LaventeCareStore) ListInvoiceLinesByInvoice(ctx context.Context, userID string, invoiceID uuid.UUID) ([]model.LCInvoiceLine, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT l.id, l.invoice_id, l.user_id, l.source_time_entry_id, l.description,
		        l.quantity_minutes, l.unit_amount_cents, l.vat_rate_bps, l.total_cents, l.sort_order
		   FROM lc_invoice_lines l
		   JOIN lc_invoices i ON i.id = l.invoice_id AND i.user_id = l.user_id
		  WHERE l.user_id = $1 AND l.invoice_id = $2
		  ORDER BY l.sort_order ASC`,
		userID, invoiceID)
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
		timeLines, resolvedCompanyID, err := s.invoiceLinesFromTimeEntries(ctx, userID, input.TimeEntryIDs, companyID, input.ProjectID, input.WorkstreamID)
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
	// VAT is summed per line (each line may carry its own rate) so a mixed-rate
	// invoice gets a correct header VAT/total instead of one rate over the subtotal.
	subtotal := 0
	vat := 0
	for _, line := range lines {
		subtotal += line.TotalCents
		lineRate := vatRate
		if line.VatRateBps != nil {
			lineRate = *line.VatRateBps
		}
		vat += vatCents(line.TotalCents, lineRate)
	}
	total := subtotal + vat
	id := uuid.New()

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Allocate the invoice number inside the tx so concurrent creates can't collide.
	number, err := s.nextLCNumber(ctx, tx, userID, "lc_invoices", "invoice_number", "LC-FAC")
	if err != nil {
		return nil, err
	}
	merchantReference := number

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
		tag, err := tx.Exec(ctx,
			`UPDATE lc_time_entries
			    SET invoice_id = $3, status = 'gefactureerd', updated_at = $4
			  WHERE user_id = $1 AND id = ANY($2)`,
			userID, input.TimeEntryIDs, id, now)
		if err != nil {
			return nil, err
		}
		if int(tag.RowsAffected()) != len(input.TimeEntryIDs) {
			return nil, pgx.ErrNoRows
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
		        i.payment_url, i.document_url, i.document_generated_at, i.ubl_xml, i.ubl_generated_at,
		        i.payment_checked_at, i.payment_status, i.payment_last_error, i.reminder_count,
		        i.last_reminder_at, i.sent_at, i.paid_at, i.notes, i.created_at, i.updated_at,
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
		        i.payment_url, i.document_url, i.document_generated_at, i.ubl_xml, i.ubl_generated_at,
		        i.payment_checked_at, i.payment_status, i.payment_last_error, i.reminder_count,
		        i.last_reminder_at, i.sent_at, i.paid_at, i.notes, i.created_at, i.updated_at,
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
	if err := validateInvoiceStatusTransition(invoice.Status, status, input.PaidCents, invoice.PaidCents); err != nil {
		return err
	}
	now := time.Now().UTC()
	paidAt := parseDateTimePtr(input.PaidAt)
	sentAt := parseDateTimePtr(input.SentAt)
	paymentCheckedAt := parseDateTimePtr(input.PaymentCheckedAt)
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
		        payment_status = COALESCE($9, payment_status),
		        payment_last_error = COALESCE($10, payment_last_error),
		        payment_checked_at = COALESCE($11, payment_checked_at),
		        sent_at = COALESCE($12, sent_at),
		        paid_at = COALESCE($13, paid_at),
		        updated_at = $14
		  WHERE id = $1 AND user_id = $2`,
		id, userID, status, paidCents, cleanStringPtr(input.PaymentProvider),
		cleanStringPtr(input.ProviderRequestID), cleanStringPtr(input.MerchantReference),
		cleanStringPtr(input.PaymentURL), cleanStringPtr(input.PaymentStatus),
		input.PaymentLastError, paymentCheckedAt, sentAt, paidAt, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *LaventeCareStore) GenerateInvoiceDocument(ctx context.Context, userID string, id uuid.UUID) (*model.LCInvoiceDocument, error) {
	invoice, err := s.GetInvoice(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	lines, err := s.ListInvoiceLinesByInvoice(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, pgx.ErrNoRows
	}

	var company *model.LCCompany
	if invoice.CompanyID != nil {
		company, err = s.GetCompany(ctx, userID, *invoice.CompanyID)
		if err != nil && err != pgx.ErrNoRows {
			return nil, err
		}
	}

	generatedAt := time.Now().UTC()
	htmlBody := buildInvoiceHTML(invoice, company, lines, generatedAt)
	textBody := buildInvoiceText(invoice, company, lines)
	ublXML := buildInvoiceUBL(invoice, company, lines)
	documentURL := fmt.Sprintf("/api/v1/laventecare/invoices/%s/document", id.String())

	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_invoices
		    SET document_url = $3,
		        document_generated_at = $4,
		        ubl_xml = $5,
		        ubl_generated_at = $4,
		        updated_at = $4
		  WHERE id = $1 AND user_id = $2`,
		id, userID, documentURL, generatedAt, ublXML)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, pgx.ErrNoRows
	}

	updated, err := s.GetInvoice(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return &model.LCInvoiceDocument{
		Invoice:      *updated,
		Lines:        lines,
		Company:      company,
		HTML:         htmlBody,
		Text:         textBody,
		UBLXML:       ublXML,
		DownloadName: invoiceDocumentDownloadName(updated.InvoiceNumber, "html"),
		GeneratedAt:  generatedAt,
	}, nil
}

func buildInvoiceHTML(invoice *model.LCInvoice, company *model.LCCompany, lines []model.LCInvoiceLine, generatedAt time.Time) string {
	customerName := invoiceCustomerName(invoice, company)
	billingAddress := invoiceCustomerAddress(company)
	paymentText := "Betaalinformatie volgt separaat."
	paymentLink := ""
	if invoice.PaymentURL != nil && strings.TrimSpace(*invoice.PaymentURL) != "" {
		paymentLink = strings.TrimSpace(*invoice.PaymentURL)
		paymentText = "Betaal veilig via de gekoppelde bunq betaallink."
	}

	var rows strings.Builder
	for _, line := range lines {
		rows.WriteString("<tr>")
		rows.WriteString("<td>" + html.EscapeString(line.Description) + "</td>")
		rows.WriteString("<td>" + html.EscapeString(invoiceQuantityLabel(line.QuantityMinutes)) + "</td>")
		rows.WriteString("<td>" + html.EscapeString(invoiceMoney(invoiceLineUnitCents(line), invoice.Currency)) + "</td>")
		rows.WriteString("<td class=\"right\">" + html.EscapeString(invoiceMoney(line.TotalCents, invoice.Currency)) + "</td>")
		rows.WriteString("</tr>")
	}

	var payButton string
	if paymentLink != "" {
		payButton = `<a class="button" href="` + html.EscapeString(paymentLink) + `">Factuur betalen</a>`
	}

	return fmt.Sprintf(`<!doctype html>
<html lang="nl">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  <style>
    body { margin: 0; background: #eef3f8; color: #102033; font-family: Arial, Helvetica, sans-serif; }
    .page { max-width: 860px; margin: 24px auto; background: #ffffff; border: 1px solid #dbe5f0; border-radius: 18px; overflow: hidden; }
    .head { background: #07172b; color: #ffffff; padding: 34px 40px; display: flex; justify-content: space-between; gap: 24px; }
    .brand { font-size: 24px; font-weight: 800; letter-spacing: .01em; }
    .brand span { color: #16d5e8; }
    .tag { margin-top: 6px; color: #b9d7e7; text-transform: uppercase; font-size: 12px; font-weight: 800; letter-spacing: .14em; }
    .meta { text-align: right; font-size: 14px; color: #d8e7f3; line-height: 1.7; }
    .body { padding: 36px 40px 42px; }
    .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; margin-bottom: 28px; }
    .box { border: 1px solid #dbe5f0; border-radius: 12px; padding: 18px; background: #f8fbfe; }
    .label { color: #007c89; text-transform: uppercase; font-size: 11px; font-weight: 800; letter-spacing: .14em; margin-bottom: 8px; }
    .value { line-height: 1.55; }
    table { width: 100%%; border-collapse: collapse; margin-top: 18px; }
    th { text-align: left; color: #496176; font-size: 12px; text-transform: uppercase; letter-spacing: .08em; border-bottom: 1px solid #dbe5f0; padding: 12px 8px; }
    td { border-bottom: 1px solid #edf2f7; padding: 14px 8px; vertical-align: top; }
    .right { text-align: right; }
    .totals { margin-left: auto; margin-top: 20px; width: min(360px, 100%%); }
    .totals div { display: flex; justify-content: space-between; padding: 9px 0; border-bottom: 1px solid #edf2f7; }
    .totals .grand { font-size: 20px; font-weight: 800; color: #07172b; border-bottom: 0; }
    .pay { margin-top: 30px; border-left: 4px solid #00a1b2; background: #f3fbfd; border-radius: 12px; padding: 18px; }
    .button { display: inline-block; margin-top: 14px; background: #06956f; color: #ffffff; text-decoration: none; font-weight: 800; padding: 12px 20px; border-radius: 10px; }
    .foot { padding: 22px 40px; border-top: 1px solid #dbe5f0; color: #5a6d7f; font-size: 13px; text-align: center; }
    @media print { body { background: #ffffff; } .page { margin: 0; border: 0; border-radius: 0; } }
    @media (max-width: 680px) { .page { margin: 0; border-radius: 0; } .head, .body, .foot { padding-left: 22px; padding-right: 22px; } .head { display: block; } .meta { text-align: left; margin-top: 18px; } .grid { grid-template-columns: 1fr; } table { font-size: 13px; } }
  </style>
</head>
<body>
  <main class="page">
    <header class="head">
      <div>
        <div class="brand">Lavente<span>Care</span></div>
        <div class="tag">Van idee tot werkend systeem</div>
      </div>
      <div class="meta">
        <strong>Factuur %s</strong><br>
        Factuurdatum: %s<br>
        Vervaldatum: %s
      </div>
    </header>
    <section class="body">
      <div class="grid">
        <div class="box">
          <div class="label">Factuur aan</div>
          <div class="value"><strong>%s</strong><br>%s</div>
        </div>
        <div class="box">
          <div class="label">Referentie</div>
          <div class="value">%s<br>Status: %s<br>Gegenereerd: %s</div>
        </div>
      </div>
      <table>
        <thead><tr><th>Omschrijving</th><th>Aantal</th><th>Tarief</th><th class="right">Bedrag</th></tr></thead>
        <tbody>%s</tbody>
      </table>
      <div class="totals">
        <div><span>Subtotaal</span><strong>%s</strong></div>
        <div><span>BTW %s</span><strong>%s</strong></div>
        <div class="grand"><span>Totaal</span><span>%s</span></div>
        <div><span>Betaald</span><strong>%s</strong></div>
        <div><span>Openstaand</span><strong>%s</strong></div>
      </div>
      <div class="pay">
        <strong>Betaling</strong><br>
        %s
        %s
      </div>
    </section>
    <footer class="foot">LaventeCare - KVK 88162710 - Dronten, Nederland - jeffrey@laventecare.nl</footer>
  </main>
</body>
</html>`,
		html.EscapeString(invoice.InvoiceNumber),
		html.EscapeString(invoice.InvoiceNumber),
		html.EscapeString(invoice.IssueDate),
		html.EscapeString(deref(invoice.DueDate)),
		html.EscapeString(customerName),
		html.EscapeString(billingAddress),
		html.EscapeString(invoiceReference(invoice, company)),
		html.EscapeString(invoice.Status),
		html.EscapeString(generatedAt.Format("2006-01-02 15:04")),
		rows.String(),
		html.EscapeString(invoiceMoney(invoice.SubtotalCents, invoice.Currency)),
		html.EscapeString(invoiceVATLabel(invoice.VatRateBps)),
		html.EscapeString(invoiceMoney(invoice.VatCents, invoice.Currency)),
		html.EscapeString(invoiceMoney(invoice.TotalCents, invoice.Currency)),
		html.EscapeString(invoiceMoney(invoice.PaidCents, invoice.Currency)),
		html.EscapeString(invoiceMoney(maxInt(invoice.TotalCents-invoice.PaidCents, 0), invoice.Currency)),
		html.EscapeString(paymentText),
		payButton,
	)
}

func buildInvoiceText(invoice *model.LCInvoice, company *model.LCCompany, lines []model.LCInvoiceLine) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Factuur %s\n", invoice.InvoiceNumber)
	fmt.Fprintf(&b, "Klant: %s\n", invoiceCustomerName(invoice, company))
	fmt.Fprintf(&b, "Factuurdatum: %s\n", invoice.IssueDate)
	if invoice.DueDate != nil {
		fmt.Fprintf(&b, "Vervaldatum: %s\n", *invoice.DueDate)
	}
	b.WriteString("\nRegels\n")
	for _, line := range lines {
		fmt.Fprintf(&b, "- %s | %s | %s\n", line.Description, invoiceQuantityLabel(line.QuantityMinutes), invoiceMoney(line.TotalCents, invoice.Currency))
	}
	fmt.Fprintf(&b, "\nSubtotaal: %s\nBTW: %s\nTotaal: %s\nOpenstaand: %s\n",
		invoiceMoney(invoice.SubtotalCents, invoice.Currency),
		invoiceMoney(invoice.VatCents, invoice.Currency),
		invoiceMoney(invoice.TotalCents, invoice.Currency),
		invoiceMoney(maxInt(invoice.TotalCents-invoice.PaidCents, 0), invoice.Currency),
	)
	if invoice.PaymentURL != nil && strings.TrimSpace(*invoice.PaymentURL) != "" {
		fmt.Fprintf(&b, "Betaallink: %s\n", strings.TrimSpace(*invoice.PaymentURL))
	}
	return b.String()
}

func buildInvoiceUBL(invoice *model.LCInvoice, company *model.LCCompany, lines []model.LCInvoiceLine) string {
	var b strings.Builder
	currency := cleanCurrency(invoice.Currency)
	customerName := invoiceCustomerName(invoice, company)
	customerAddress := invoiceCustomerAddress(company)
	reference := invoiceReference(invoice, company)

	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2" xmlns:cac="urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2" xmlns:cbc="urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2">` + "\n")
	fmt.Fprintf(&b, "  <cbc:CustomizationID>%s</cbc:CustomizationID>\n", xmlEscape("urn:cen.eu:en16931:2017"))
	fmt.Fprintf(&b, "  <cbc:ID>%s</cbc:ID>\n", xmlEscape(invoice.InvoiceNumber))
	fmt.Fprintf(&b, "  <cbc:IssueDate>%s</cbc:IssueDate>\n", xmlEscape(invoice.IssueDate))
	if invoice.DueDate != nil {
		fmt.Fprintf(&b, "  <cbc:DueDate>%s</cbc:DueDate>\n", xmlEscape(*invoice.DueDate))
	}
	fmt.Fprintf(&b, "  <cbc:InvoiceTypeCode>380</cbc:InvoiceTypeCode>\n")
	fmt.Fprintf(&b, "  <cbc:DocumentCurrencyCode>%s</cbc:DocumentCurrencyCode>\n", xmlEscape(currency))
	fmt.Fprintf(&b, "  <cbc:BuyerReference>%s</cbc:BuyerReference>\n", xmlEscape(reference))
	b.WriteString("  <cac:AccountingSupplierParty><cac:Party>\n")
	b.WriteString("    <cac:PartyName><cbc:Name>LaventeCare</cbc:Name></cac:PartyName>\n")
	b.WriteString("    <cac:PartyLegalEntity><cbc:RegistrationName>LaventeCare</cbc:RegistrationName><cbc:CompanyID>88162710</cbc:CompanyID></cac:PartyLegalEntity>\n")
	b.WriteString("  </cac:Party></cac:AccountingSupplierParty>\n")
	b.WriteString("  <cac:AccountingCustomerParty><cac:Party>\n")
	fmt.Fprintf(&b, "    <cac:PartyName><cbc:Name>%s</cbc:Name></cac:PartyName>\n", xmlEscape(customerName))
	fmt.Fprintf(&b, "    <cac:PostalAddress><cbc:StreetName>%s</cbc:StreetName><cac:Country><cbc:IdentificationCode>NL</cbc:IdentificationCode></cac:Country></cac:PostalAddress>\n", xmlEscape(customerAddress))
	if company != nil && strings.TrimSpace(deref(company.VATNumber)) != "" {
		fmt.Fprintf(&b, "    <cac:PartyTaxScheme><cbc:CompanyID>%s</cbc:CompanyID><cac:TaxScheme><cbc:ID>VAT</cbc:ID></cac:TaxScheme></cac:PartyTaxScheme>\n", xmlEscape(deref(company.VATNumber)))
	}
	b.WriteString("  </cac:Party></cac:AccountingCustomerParty>\n")
	fmt.Fprintf(&b, "  <cac:TaxTotal><cbc:TaxAmount currencyID=\"%s\">%s</cbc:TaxAmount></cac:TaxTotal>\n", xmlEscape(currency), invoiceMoneyValue(invoice.VatCents))
	b.WriteString("  <cac:LegalMonetaryTotal>\n")
	fmt.Fprintf(&b, "    <cbc:LineExtensionAmount currencyID=\"%s\">%s</cbc:LineExtensionAmount>\n", xmlEscape(currency), invoiceMoneyValue(invoice.SubtotalCents))
	fmt.Fprintf(&b, "    <cbc:TaxExclusiveAmount currencyID=\"%s\">%s</cbc:TaxExclusiveAmount>\n", xmlEscape(currency), invoiceMoneyValue(invoice.SubtotalCents))
	fmt.Fprintf(&b, "    <cbc:TaxInclusiveAmount currencyID=\"%s\">%s</cbc:TaxInclusiveAmount>\n", xmlEscape(currency), invoiceMoneyValue(invoice.TotalCents))
	fmt.Fprintf(&b, "    <cbc:PayableAmount currencyID=\"%s\">%s</cbc:PayableAmount>\n", xmlEscape(currency), invoiceMoneyValue(maxInt(invoice.TotalCents-invoice.PaidCents, 0)))
	b.WriteString("  </cac:LegalMonetaryTotal>\n")
	for idx, line := range lines {
		b.WriteString(buildInvoiceLineUBL(idx+1, invoice, line))
	}
	b.WriteString("</Invoice>\n")
	return b.String()
}

func buildInvoiceLineUBL(index int, invoice *model.LCInvoice, line model.LCInvoiceLine) string {
	currency := cleanCurrency(invoice.Currency)
	quantity := invoiceQuantityValue(line.QuantityMinutes)
	unitCode := "HUR"
	if line.QuantityMinutes <= 0 {
		unitCode = "C62"
	}
	var b strings.Builder
	b.WriteString("  <cac:InvoiceLine>\n")
	fmt.Fprintf(&b, "    <cbc:ID>%d</cbc:ID>\n", index)
	fmt.Fprintf(&b, "    <cbc:InvoicedQuantity unitCode=\"%s\">%s</cbc:InvoicedQuantity>\n", unitCode, quantity)
	fmt.Fprintf(&b, "    <cbc:LineExtensionAmount currencyID=\"%s\">%s</cbc:LineExtensionAmount>\n", xmlEscape(currency), invoiceMoneyValue(line.TotalCents))
	fmt.Fprintf(&b, "    <cac:Item><cbc:Name>%s</cbc:Name></cac:Item>\n", xmlEscape(line.Description))
	fmt.Fprintf(&b, "    <cac:Price><cbc:PriceAmount currencyID=\"%s\">%s</cbc:PriceAmount></cac:Price>\n", xmlEscape(currency), invoiceMoneyValue(invoiceLineUnitCents(line)))
	b.WriteString("  </cac:InvoiceLine>\n")
	return b.String()
}

func invoiceCustomerName(invoice *model.LCInvoice, company *model.LCCompany) string {
	if company != nil && strings.TrimSpace(company.Naam) != "" {
		return strings.TrimSpace(company.Naam)
	}
	if invoice.CompanyName != nil && strings.TrimSpace(*invoice.CompanyName) != "" {
		return strings.TrimSpace(*invoice.CompanyName)
	}
	return "Klant"
}

func invoiceCustomerAddress(company *model.LCCompany) string {
	if company != nil && strings.TrimSpace(deref(company.BillingAddress)) != "" {
		return strings.TrimSpace(deref(company.BillingAddress))
	}
	return "Adres niet ingevuld"
}

func invoiceReference(invoice *model.LCInvoice, company *model.LCCompany) string {
	if company != nil && strings.TrimSpace(deref(company.BillingReference)) != "" {
		return strings.TrimSpace(deref(company.BillingReference))
	}
	if invoice.MerchantReference != nil && strings.TrimSpace(*invoice.MerchantReference) != "" {
		return strings.TrimSpace(*invoice.MerchantReference)
	}
	return invoice.InvoiceNumber
}

func invoiceVATLabel(vatRateBps int) string {
	if vatRateBps <= 0 {
		return "0%"
	}
	whole := vatRateBps / 100
	decimal := vatRateBps % 100
	if decimal == 0 {
		return fmt.Sprintf("%d%%", whole)
	}
	return fmt.Sprintf("%d.%02d%%", whole, decimal)
}

func invoiceQuantityLabel(minutes int) string {
	if minutes <= 0 {
		return "1 item"
	}
	return fmt.Sprintf("%.2f uur", float64(minutes)/60)
}

func invoiceQuantityValue(minutes int) string {
	if minutes <= 0 {
		return "1.00"
	}
	return fmt.Sprintf("%.2f", float64(minutes)/60)
}

func invoiceLineUnitCents(line model.LCInvoiceLine) int {
	if line.QuantityMinutes <= 0 {
		return line.TotalCents
	}
	return line.UnitAmountCents
}

func invoiceMoney(cents int, currency string) string {
	return fmt.Sprintf("%s %s", cleanCurrency(currency), invoiceMoneyValue(cents))
}

func invoiceMoneyValue(cents int) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

func invoiceDocumentDownloadName(number, ext string) string {
	safe := strings.ToLower(strings.TrimSpace(number))
	safe = strings.NewReplacer("/", "-", "\\", "-", " ", "-", "_", "-").Replace(safe)
	if safe == "" {
		safe = "factuur"
	}
	return safe + "." + strings.TrimPrefix(ext, ".")
}

func xmlEscape(value string) string {
	return html.EscapeString(strings.TrimSpace(value))
}

func (s *LaventeCareStore) invoiceLinesFromTimeEntries(ctx context.Context, userID string, ids []uuid.UUID, expectedCompanyID, expectedProjectID, expectedWorkstreamID *uuid.UUID) ([]model.LCInvoiceLineCreate, *uuid.UUID, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT te.id,
		        COALESCE(te.company_id, p.company_id, w.company_id, ae.company_id),
		        COALESCE(te.project_id, w.project_id, ae.project_id),
		        COALESCE(te.workstream_id, ae.workstream_id),
		        te.description,
		        te.minutes,
		        te.hourly_rate_cents
		   FROM lc_time_entries te
		   LEFT JOIN lc_projects p ON p.user_id = te.user_id AND p.id = te.project_id
		   LEFT JOIN lc_workstreams w ON w.user_id = te.user_id AND w.id = te.workstream_id
		   LEFT JOIN lc_activity_events ae ON ae.user_id = te.user_id AND ae.id = te.activity_event_id
		  WHERE te.user_id = $1
		    AND te.id = ANY($2)
		    AND te.billable = true
		    AND te.invoice_id IS NULL
		  ORDER BY te.entry_date ASC, te.created_at ASC`,
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
		var entryProjectID *uuid.UUID
		var entryWorkstreamID *uuid.UUID
		var description string
		var minutes int
		var rate int
		if err := rows.Scan(&id, &entryCompanyID, &entryProjectID, &entryWorkstreamID, &description, &minutes, &rate); err != nil {
			return nil, nil, err
		}
		companyID, err = mergeCompanyScope(companyID, entryCompanyID)
		if err != nil {
			return nil, nil, err
		}
		if _, err := mergeCompanyScope(expectedCompanyID, entryCompanyID); err != nil {
			return nil, nil, err
		}
		if _, err := mergeCompanyScope(expectedProjectID, entryProjectID); err != nil {
			return nil, nil, err
		}
		if _, err := mergeCompanyScope(expectedWorkstreamID, entryWorkstreamID); err != nil {
			return nil, nil, err
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
	if err := s.ensureCompanyExists(ctx, userID, companyID); err != nil {
		return err
	}
	scope := companyID
	var err error
	if scope, err = s.validateScopedCompanyObject(ctx, userID, scope, projectID,
		`SELECT company_id FROM lc_projects WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	if scope, err = s.validateScopedCompanyObject(ctx, userID, scope, workstreamID,
		`SELECT company_id FROM lc_workstreams WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	if _, err = s.validateScopedCompanyObject(ctx, userID, scope, activityEventID,
		`SELECT company_id FROM lc_activity_events WHERE user_id = $1 AND id = $2`); err != nil {
		return err
	}
	return nil
}

// nextLCNumber allocates the next sequential document number for (user, prefix,
// year). It MUST run inside the caller's transaction: a per-scope advisory lock
// serializes concurrent allocations until the tx ends, and it derives the number
// from MAX(existing)+1 — not COUNT(*)+1 — so a later deletion can never make a
// new number collide with an existing one (which would 500 on the UNIQUE
// constraint and jam invoice/quote creation).
func (s *LaventeCareStore) nextLCNumber(ctx context.Context, tx pgx.Tx, userID, table, column, prefix string) (string, error) {
	year := time.Now().UTC().Format("2006")
	needle := prefix + "-" + year + "-%"
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, userID+"|"+table+"|"+year); err != nil {
		return "", err
	}
	query := fmt.Sprintf(
		`SELECT COALESCE(MAX(NULLIF(regexp_replace(%s, '^.*-', ''), '')::int), 0)
		   FROM %s WHERE user_id = $1 AND %s LIKE $2`, column, table, column)
	var maxNum int
	if err := tx.QueryRow(ctx, query, userID, needle).Scan(&maxNum); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%04d", prefix, year, maxNum+1), nil
}

// ─── Cockpit (aggregated dashboard) ──────────────────────────────────────────

// cockpitEntityCounts returns true totals (in one round-trip) for the cockpit
// summary, so the headline metrics stay correct once a list outgrows its display
// cap instead of reporting len() of a 30/8-capped slice.
type cockpitCounts struct {
	companies, contacts, leads, projects, workstreams, documents, actions int
}

func (s *LaventeCareStore) cockpitEntityCounts(ctx context.Context, userID string) (cockpitCounts, error) {
	var c cockpitCounts
	err := s.db.Pool.QueryRow(ctx, `
SELECT (SELECT COUNT(*) FROM lc_companies     WHERE user_id = $1),
       (SELECT COUNT(*) FROM lc_contacts      WHERE user_id = $1),
       (SELECT COUNT(*) FROM lc_leads         WHERE user_id = $1),
       (SELECT COUNT(*) FROM lc_projects      WHERE user_id = $1),
       (SELECT COUNT(*) FROM lc_workstreams   WHERE user_id = $1),
       (SELECT COUNT(*) FROM lc_documents     WHERE user_id = $1),
       (SELECT COUNT(*) FROM lc_action_items  WHERE user_id = $1)`, userID).
		Scan(&c.companies, &c.contacts, &c.leads, &c.projects, &c.workstreams, &c.documents, &c.actions)
	return c, err
}

func (s *LaventeCareStore) GetCockpit(ctx context.Context, userID string) (*model.LCCockpit, error) {
	companies, err := s.ListCompanies(ctx, userID, 30, "")
	if err != nil {
		return nil, err
	}
	contacts, err := s.ListContacts(ctx, userID, nil, 30)
	if err != nil {
		return nil, err
	}
	accessCredentials, err := s.ListAccessCredentials(ctx, userID, 30, nil)
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
	workstreams, err := s.ListWorkstreams(ctx, userID, 30, true)
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
	accessCredentialCount, err := s.CountAccessCredentials(ctx, userID)
	if err != nil {
		return nil, err
	}
	counts, err := s.cockpitEntityCounts(ctx, userID)
	if err != nil {
		return nil, err
	}
	_ = s.SeedDefaultMailTemplates(ctx, userID)
	mailTemplates, _ := s.ListMailTemplates(ctx, userID, 80)
	mailboxSummary, _ := s.GetMailboxSummary(ctx, userID, mailTemplates, laventeCareMailConfiguredFromEnv(), strings.TrimSpace(os.Getenv("MICROSOFT_SENDER_EMAIL")))

	activeLeads := filterOpen(leads, func(l model.LCLead) string { return l.Status })
	activeWorkstreams := filterOpen(workstreams, func(w model.LCWorkstream) string { return w.Status })
	activeProjects := filterOpen(projects, func(p model.LCProject) string { return p.Status })

	incidents, _ := s.listSlaIncidents(ctx, userID, 5)
	changes, _ := s.listChangeRequests(ctx, userID, 5)
	decisions, _ := s.listDecisions(ctx, userID, 5)

	// Business signals: scan emails for business-term matches. Historical/converted
	// opdrachten still carry useful customer and system terms for matching.
	signals := s.buildBusinessSignals(ctx, userID, companies, leads, projects, workstreams)
	// Follow-ups: leads, opdrachten and projects with upcoming deadlines
	followUps := s.buildFollowUps(companies, activeLeads, activeProjects, activeWorkstreams)

	return &model.LCCockpit{
		Summary: model.LCCockpitSummary{
			Companies:         counts.companies,
			Contacts:          counts.contacts,
			AccessCredentials: accessCredentialCount,
			Leads:             counts.leads,
			ActiveLeads:       len(activeLeads),
			Workstreams:       counts.workstreams,
			ActiveWorkstreams: len(activeWorkstreams),
			Projects:          counts.projects,
			ActiveProjects:    len(activeProjects),
			Documents:         counts.documents,
			OpenIncidents:     len(incidents),
			OpenChanges:       len(changes),
			Decisions:         len(decisions),
			ActionItems:       counts.actions,
			DossierDocuments:  dossierDocumentCount,
			ActivityEvents:    activityEventCount,
			MailTemplates:     mailboxSummary.ActiveTemplates,
			MailOutbox:        mailboxSummary.Outbox,
			MailConfigured:    mailboxSummary.Configured,
			DocumentsSeeded:   counts.documents > 0,
			BusinessSignals:   len(signals),
			FollowUps:         len(followUps),
		},
		Companies:         take(companies, 12),
		Contacts:          take(contacts, 12),
		AccessCredentials: take(accessCredentials, 20),
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
		Mailbox:           &mailboxSummary,
		BusinessSignals:   signals,
		FollowUps:         followUps,
	}, nil
}

// buildBusinessSignals matches emails, calendar events and notes against CRM entity names.
func (s *LaventeCareStore) buildBusinessSignals(ctx context.Context, userID string, companies []model.LCCompany, leads []model.LCLead, projects []model.LCProject, workstreams []model.LCWorkstream) []model.LCBusinessSignal {
	terms := buildSignalTerms(companies, leads, projects, workstreams)
	if len(terms) == 0 {
		return nil
	}

	var signals []model.LCBusinessSignal
	seen := make(map[string]bool)
	sourceCounts := make(map[string]int)
	addSignal := func(signal model.LCBusinessSignal) {
		if len(signals) >= 18 {
			return
		}
		sourceLimit := map[string]int{"email": 8, "agenda": 6, "notitie": 8}[signal.Source]
		if sourceLimit > 0 && sourceCounts[signal.Source] >= sourceLimit {
			return
		}
		key := signal.Source + ":" + signal.ID
		if signal.ID == "" || seen[key] {
			return
		}
		seen[key] = true
		sourceCounts[signal.Source]++
		signals = append(signals, signal)
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT gmail_id, from_addr, subject, snippet, datum::text, is_gelezen
		 FROM emails WHERE user_id = $1 AND is_verwijderd = false
		 ORDER BY ontvangen DESC LIMIT 120`, userID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var gmailID, from, subject, snippet, datum string
			var isGelezen bool
			if err := rows.Scan(&gmailID, &from, &subject, &snippet, &datum, &isGelezen); err != nil {
				continue
			}
			matched := matchTerm(normalize(subject+" "+snippet+" "+from), terms)
			if matched == "" {
				continue
			}
			urgency := "normaal"
			hint := "Review of dit bij een lead/project hoort."
			if !isGelezen {
				urgency = "hoog"
				hint = "Ongelezen zakelijke email opvolgen."
			}
			addSignal(model.LCBusinessSignal{
				Source:      "email",
				ID:          gmailID,
				Title:       subject,
				Subtitle:    from,
				Date:        datum,
				MatchedTerm: matched,
				Urgency:     urgency,
				ActionHint:  hint,
			})
		}
	}

	eventRows, err := s.db.Pool.Query(ctx,
		`SELECT COALESCE(NULLIF(event_id, ''), id::text), titel, COALESCE(locatie, ''), COALESCE(beschrijving, ''),
		        start_datum::text, COALESCE(business_context_title, ''), COALESCE(business_context_type, '')
		   FROM personal_events
		  WHERE user_id = $1
		    AND COALESCE(status, '') <> 'cancelled'
		    AND eind_datum >= CURRENT_DATE - INTERVAL '14 days'
		  ORDER BY start_datum ASC
		  LIMIT 100`, userID)
	if err == nil {
		defer eventRows.Close()
		for eventRows.Next() {
			var eventID, title, location, description, date, contextTitle, contextType string
			if err := eventRows.Scan(&eventID, &title, &location, &description, &date, &contextTitle, &contextType); err != nil {
				continue
			}
			matched := matchTerm(normalize(title+" "+location+" "+description+" "+contextTitle+" "+contextType), terms)
			if matched == "" {
				continue
			}
			addSignal(model.LCBusinessSignal{
				Source:      "agenda",
				ID:          eventID,
				Title:       title,
				Subtitle:    strings.TrimSpace(location + " " + contextTitle),
				Date:        date,
				MatchedTerm: matched,
				Urgency:     businessSignalDateUrgency(date, "normaal"),
				ActionHint:  "Controleer of deze afspraak een klantactie, projectupdate of follow-up nodig heeft.",
			})
		}
	}

	noteRows, err := s.db.Pool.Query(ctx,
		`SELECT id::text, titel, inhoud, array_to_string(tags, ' '),
		        COALESCE(business_context_title, ''), COALESCE(business_context_type, ''),
		        COALESCE(deadline::date::text, gewijzigd::date::text),
		        COALESCE(prioriteit, 'normaal'), triage_flag
		   FROM notes
		  WHERE user_id = $1
		    AND is_archived = false
		    AND is_completed = false
		  ORDER BY COALESCE(deadline, gewijzigd) ASC
		  LIMIT 100`, userID)
	if err == nil {
		defer noteRows.Close()
		for noteRows.Next() {
			var id, title, content, tags, contextTitle, contextType, date, priority string
			var triageFlag bool
			if err := noteRows.Scan(&id, &title, &content, &tags, &contextTitle, &contextType, &date, &priority, &triageFlag); err != nil {
				continue
			}
			matched := matchTerm(normalize(title+" "+content+" "+tags+" "+contextTitle+" "+contextType), terms)
			if matched == "" {
				continue
			}
			urgency := businessSignalDateUrgency(date, priority)
			if triageFlag || priority == "hoog" {
				urgency = "hoog"
			}
			addSignal(model.LCBusinessSignal{
				Source:      "notitie",
				ID:          id,
				Title:       title,
				Subtitle:    strings.TrimSpace(contextTitle + " #" + tags),
				Date:        date,
				MatchedTerm: matched,
				Urgency:     urgency,
				ActionHint:  "Zet deze notitie om naar een klantactie of lead als er opvolging nodig is.",
			})
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

func businessSignalDateUrgency(date string, fallback string) string {
	urgency := strings.TrimSpace(fallback)
	if urgency == "" {
		urgency = "normaal"
	}
	if urgency == "hoog" {
		return "hoog"
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(date))
	if err != nil {
		return urgency
	}
	today := time.Now().Truncate(24 * time.Hour)
	if !parsed.After(today.AddDate(0, 0, 1)) {
		return "hoog"
	}
	return urgency
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

func (s *LaventeCareStore) ListSlaIncidents(ctx context.Context, userID string, limit int) ([]model.LCSlaIncident, error) {
	return s.listSlaIncidents(ctx, userID, limit)
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

func (s *LaventeCareStore) UpdateSlaIncidentStatus(ctx context.Context, userID string, id uuid.UUID, status string) error {
	status = strings.TrimSpace(status)
	if status == "" {
		return fmt.Errorf("status is verplicht")
	}
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_sla_incidents
		 SET status = $1, updated_at = NOW()
		 WHERE id = $2 AND user_id = $3`,
		status, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("incident niet gevonden")
	}
	return nil
}

func (s *LaventeCareStore) listChangeRequests(ctx context.Context, userID string, limit int) ([]model.LCChangeRequest, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, project_id, titel, impact, planning_impact,
		        budget_impact, status, created_at, updated_at
		 FROM lc_change_requests WHERE user_id = $1 AND status IN ('nieuw','beoordeeld','goedgekeurd')
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

func (s *LaventeCareStore) ListChangeRequests(ctx context.Context, userID string, limit int) ([]model.LCChangeRequest, error) {
	return s.listChangeRequests(ctx, userID, limit)
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

func (s *LaventeCareStore) UpdateChangeRequestStatus(ctx context.Context, userID string, id uuid.UUID, status string) error {
	status = strings.TrimSpace(status)
	if status == "" {
		return fmt.Errorf("status is verplicht")
	}
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_change_requests
		 SET status = $1, updated_at = NOW()
		 WHERE id = $2 AND user_id = $3`,
		status, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("change request niet gevonden")
	}
	return nil
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

func (s *LaventeCareStore) ListDecisions(ctx context.Context, userID string, limit int) ([]model.LCDecision, error) {
	return s.listDecisions(ctx, userID, limit)
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

func (s *LaventeCareStore) UpdateDecisionStatus(ctx context.Context, userID string, id uuid.UUID, status string) error {
	status = strings.TrimSpace(status)
	if status == "" {
		return fmt.Errorf("status is verplicht")
	}
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE lc_decisions
		 SET status = $1
		 WHERE id = $2 AND user_id = $3`,
		status, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("besluit niet gevonden")
	}
	return nil
}

// ─── Row scanners ────────────────────────────────────────────────────────────

func scanCompany(row pgx.CollectableRow) (model.LCCompany, error) {
	var c model.LCCompany
	err := row.Scan(&c.ID, &c.UserID, &c.Naam, &c.Website, &c.Sector, &c.Status,
		&c.RelatieType, &c.Notities, &c.LaatsteContact, &c.VolgendeActie,
		&c.KVKNumber, &c.VATNumber, &c.BillingEmail, &c.BillingAddress, &c.BillingReference,
		&c.PaymentTermsDays, &c.ContractStatus, &c.ServiceLevel, &c.PreferredChannel,
		&c.PortalURL, &c.DefaultLoginURL, &c.OnboardingStatus, &c.DataProcessStatus,
		&c.CreatedAt, &c.UpdatedAt, &c.Contacts, &c.Leads, &c.Workstreams,
		&c.Projects, &c.ActionItems, &c.DossierDocuments)
	return c, err
}

func scanContact(row pgx.CollectableRow) (model.LCContact, error) {
	var c model.LCContact
	err := row.Scan(&c.ID, &c.UserID, &c.CompanyID, &c.Naam, &c.Email, &c.Telefoon,
		&c.Rol, &c.IsPrimary, &c.Notities, &c.PreferredChannel, &c.DecisionRole,
		&c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func scanAccessCredential(row pgx.CollectableRow) (model.LCAccessCredential, error) {
	var c model.LCAccessCredential
	err := row.Scan(&c.ID, &c.UserID, &c.CompanyID, &c.ContactID, &c.ProjectID,
		&c.WorkstreamID, &c.Title, &c.LoginURL, &c.Username, &c.Role,
		&c.Environment, &c.Status, &c.OwnerContact, &c.SecretLabel,
		&c.SecretConfigured, &c.SecretHint, &c.SharingPolicy, &c.LastCheckedAt,
		&c.ExpiresAt, &c.RevokedAt, &c.Notes, &c.CreatedAt, &c.UpdatedAt,
		&c.CompanyName, &c.ContactName, &c.ProjectName, &c.WorkstreamTitle)
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
		&a.Summary, &a.ActionType, &a.Status, &a.Priority, &a.DueDate, &a.DueTime,
		&a.LinkedLeadID, &a.LinkedProjectID, &a.LinkedWorkstreamID, &a.LinkedCompanyID,
		&a.CreatedAt, &a.UpdatedAt,
		&a.CompanyName, &a.ProjectName, &a.WorkstreamTitle, &a.LeadTitle,
		&a.SourceActivityID, &a.SourceActivityTitle, &a.SourceActivityAt)
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
		&e.CompanyName, &e.ContactName, &e.ProjectName, &e.WorkstreamName,
		&e.LinkedActionTitle, &e.LinkedActionStatus)
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
		&i.PaymentURL, &i.DocumentURL, &i.DocumentGenerated, &i.UBLXML, &i.UBLGeneratedAt,
		&i.PaymentCheckedAt, &i.PaymentStatus, &i.PaymentLastError, &i.ReminderCount,
		&i.LastReminderAt, &i.SentAt, &i.PaidAt, &i.Notes, &i.CreatedAt, &i.UpdatedAt,
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

func cleanStatusPtr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return cleanStatus(*value, fallback)
}

func positiveIntOr(value *int, fallback int) int {
	if value == nil || *value <= 0 {
		return fallback
	}
	return *value
}

func positiveIntPtr(value *int) *int {
	if value == nil || *value <= 0 {
		return nil
	}
	return value
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
		// A time-based line's total is derived from minutes x hourly rate and must
		// never be trusted from the client; only a flat-fee line (no minutes/rate)
		// may carry an explicit total.
		total := line.TotalCents
		if minutes > 0 && unit > 0 {
			total = centsFromMinutes(minutes, unit)
		} else if total < 0 {
			total = 0
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

func encryptLaventeCareSecret(value *string) (*string, error) {
	raw := strings.TrimSpace(deref(value))
	if raw == "" {
		return nil, nil
	}
	keyMaterial := strings.TrimSpace(os.Getenv("LAVENTECARE_SECRET_KEY"))
	if keyMaterial == "" {
		keyMaterial = strings.TrimSpace(os.Getenv("APP_SECRET_KEY"))
	}
	if keyMaterial == "" || keyMaterial == "change-me" || keyMaterial == "change-me-to-a-long-random-secret" {
		return nil, fmt.Errorf("LAVENTECARE_SECRET_KEY of APP_SECRET_KEY is nodig om klanttoegang versleuteld op te slaan")
	}

	sum := sha256.Sum256([]byte(keyMaterial))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(raw), nil)
	payload := append([]byte("v1:"), append(nonce, ciphertext...)...)
	encoded := base64.StdEncoding.EncodeToString(payload)
	return &encoded, nil
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
