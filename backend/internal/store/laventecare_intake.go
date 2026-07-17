package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrIntakeIdempotencyConflict = errors.New("idempotency key is already used for a different intake")
	ErrIntakeStillProcessing     = errors.New("intake idempotency reservation is not complete")
)

// ProcessPublicIntake atomically reserves the idempotency key and creates (or
// reuses) the company/contact, then creates one lead and one follow-up action.
// A committed reservation always has all four result IDs, so a caller retry can
// return the original response without repeating any CRM side effect.
func (s *LaventeCareStore) ProcessPublicIntake(
	ctx context.Context,
	userID, idempotencyKey, payloadHash string,
	input model.LCPublicIntakeRequest,
) (*model.LCPublicIntakeResult, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	intakeID := uuid.New()
	var reservedID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO lc_public_intakes
			(id, user_id, idempotency_key, request_id, payload_hash, status, submitted_at)
		VALUES ($1,$2,$3,$4,$5,'processing',$6)
		ON CONFLICT (user_id, idempotency_key) DO NOTHING
		RETURNING id`,
		intakeID, userID, idempotencyKey, input.RequestID, payloadHash, intakeSubmittedAt(input.SubmittedAt),
	).Scan(&reservedID)
	if err == pgx.ErrNoRows {
		return existingPublicIntake(ctx, tx, userID, idempotencyKey, payloadHash)
	}
	if err != nil {
		return nil, err
	}
	intakeID = reservedID

	// Serialize intake upserts for the same email. This prevents two different
	// contact-form request IDs racing into duplicate contacts.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"laventecare-intake:"+userID+":"+strings.ToLower(input.Email)); err != nil {
		return nil, err
	}

	companyID, err := resolveIntakeCompany(ctx, tx, userID, input)
	if err != nil {
		return nil, err
	}
	contactID, err := resolveIntakeContact(ctx, tx, userID, companyID, input)
	if err != nil {
		return nil, err
	}

	leadID := uuid.New()
	title := "Website-aanvraag: " + input.Name
	if input.ProjectType != "" {
		title = input.ProjectType + ": " + input.Name
	}
	painPoint := intakeStringPtr(strings.TrimSpace(strings.Join(nonEmptyIntakeParts(input.Goal, input.Message), "\n\n")))
	nextStep := "Neem contact op met " + input.Name
	priority := "normaal"
	if _, err := tx.Exec(ctx, `
		INSERT INTO lc_leads
			(id, user_id, company_id, contact_id, titel, bron, source_id, status,
			 pijnpunt, prioriteit, volgende_stap, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,'website',$6,'nieuw',$7,$8,$9,now(),now())`,
		leadID, userID, companyID, contactID, title, idempotencyKey, painPoint, priority, nextStep); err != nil {
		return nil, err
	}

	actionID := uuid.New()
	actionTitle := "Website-aanvraag opvolgen: " + input.Name
	summary := intakeStringPtr(buildIntakeSummary(input))
	if _, err := tx.Exec(ctx, `
		INSERT INTO lc_action_items
			(id, user_id, source, source_id, title, summary, action_type, status,
			 priority, linked_lead_id, linked_company_id, created_at, updated_at)
		VALUES ($1,$2,'website_intake',$3,$4,$5,'opvolgen','open',$6,$7,$8,now(),now())`,
		actionID, userID, idempotencyKey, actionTitle, summary, priority, leadID, companyID); err != nil {
		return nil, err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE lc_public_intakes
		SET status='accepted', company_id=$3, contact_id=$4, lead_id=$5,
			action_id=$6, updated_at=now()
		WHERE user_id=$1 AND id=$2 AND status='processing'`,
		userID, intakeID, companyID, contactID, leadID, actionID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, ErrIntakeStillProcessing
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// Mirror is derived data and has its own reconciliation cron. Do it only
	// after the intake transaction commits, without weakening intake atomicity.
	if _, err := SyncLaventeCareContactMirror(ctx, s.db, userID); err != nil {
		slog.Warn("public intake contact mirror sync failed", "intakeID", intakeID, "error", err)
	}
	return &model.LCPublicIntakeResult{
		Status: "accepted", IntakeID: intakeID, CompanyID: companyID,
		ContactID: contactID, LeadID: leadID, ActionID: actionID,
	}, nil
}

func existingPublicIntake(ctx context.Context, tx pgx.Tx, userID, key, payloadHash string) (*model.LCPublicIntakeResult, error) {
	var result model.LCPublicIntakeResult
	var storedHash, status string
	var companyID *uuid.UUID
	var contactID, leadID, actionID *uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id, payload_hash, status, company_id, contact_id, lead_id, action_id
		FROM lc_public_intakes WHERE user_id=$1 AND idempotency_key=$2`, userID, key,
	).Scan(&result.IntakeID, &storedHash, &status, &companyID, &contactID, &leadID, &actionID)
	if err != nil {
		return nil, err
	}
	if storedHash != payloadHash {
		return nil, ErrIntakeIdempotencyConflict
	}
	if status != "accepted" || contactID == nil || leadID == nil || actionID == nil {
		return nil, ErrIntakeStillProcessing
	}
	result.Status = "accepted"
	result.CompanyID = companyID
	result.ContactID = *contactID
	result.LeadID = *leadID
	result.ActionID = *actionID
	result.Duplicate = true
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &result, nil
}

func resolveIntakeCompany(ctx context.Context, tx pgx.Tx, userID string, input model.LCPublicIntakeRequest) (*uuid.UUID, error) {
	if input.CompanyName == "" {
		return nil, nil
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM lc_companies
		WHERE user_id=$1 AND (
			LOWER(TRIM(naam))=LOWER(TRIM($2)) OR
			($3 <> '' AND website IS NOT NULL AND LOWER(TRIM(website))=LOWER(TRIM($3)))
		) ORDER BY updated_at DESC LIMIT 1`, userID, input.CompanyName, input.Website).Scan(&id)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE lc_companies SET website=COALESCE(website,NULLIF($3,'')), updated_at=now() WHERE user_id=$1 AND id=$2`, userID, id, input.Website)
		return &id, err
	}
	if err != pgx.ErrNoRows {
		return nil, err
	}
	id = uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO lc_companies (id,user_id,naam,website,status,relatie_type,created_at,updated_at)
		VALUES ($1,$2,$3,NULLIF($4,''),'actief','prospect',now(),now())`,
		id, userID, input.CompanyName, input.Website); err != nil {
		return nil, err
	}
	return &id, nil
}

func resolveIntakeContact(ctx context.Context, tx pgx.Tx, userID string, companyID *uuid.UUID, input model.LCPublicIntakeRequest) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM lc_contacts
		WHERE user_id=$1 AND LOWER(TRIM(email))=LOWER(TRIM($2))
		ORDER BY updated_at DESC LIMIT 1`, userID, input.Email).Scan(&id)
	if err == nil {
		_, err = tx.Exec(ctx, `
			UPDATE lc_contacts SET company_id=COALESCE(company_id,$3),
				telefoon=COALESCE(telefoon,NULLIF($4,'')), updated_at=now()
			WHERE user_id=$1 AND id=$2`, userID, id, companyID, input.Phone)
		return id, err
	}
	if err != pgx.ErrNoRows {
		return uuid.Nil, err
	}
	id = uuid.New()
	note := fmt.Sprintf("Publieke intake via %s (%s)", input.Source, input.RequestID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO lc_contacts
			(id,user_id,company_id,naam,email,telefoon,is_primary,notities,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,now(),now())`,
		id, userID, companyID, input.Name, input.Email, input.Phone, companyID != nil, note); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func intakeSubmittedAt(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}

func buildIntakeSummary(input model.LCPublicIntakeRequest) string {
	parts := nonEmptyIntakeParts(
		"Naam: "+input.Name,
		"E-mail: "+input.Email,
		prefixedIntakePart("Telefoon", input.Phone),
		prefixedIntakePart("Bedrijf", input.CompanyName),
		prefixedIntakePart("Projecttype", input.ProjectType),
		prefixedIntakePart("Budget", input.Budget),
		prefixedIntakePart("Timing", input.Timeline),
		prefixedIntakePart("Doel", input.Goal),
		prefixedIntakePart("Bericht", input.Message),
		prefixedIntakePart("Pagina", input.PageURL),
	)
	return strings.Join(parts, "\n")
}

func nonEmptyIntakeParts(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func prefixedIntakePart(label, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return label + ": " + strings.TrimSpace(value)
}

func intakeStringPtr(value string) *string {
	if value = strings.TrimSpace(value); value != "" {
		return &value
	}
	return nil
}
