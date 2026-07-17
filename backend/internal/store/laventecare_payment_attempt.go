package store

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

type PaymentRequestAttempt struct {
	Status            string
	ExecutionKey      string
	ProviderRequestID *string
	UpdatedAt         time.Time
}

// BeginPaymentRequestAttempt reserves the one allowed remote create for an
// invoice. shouldCreate=false means an earlier create is still unknown/active
// and the caller must reconcile rather than issue another POST.
func (s *LaventeCareStore) BeginPaymentRequestAttempt(ctx context.Context, userID string, invoiceID uuid.UUID, executionKey string) (*PaymentRequestAttempt, bool, error) {
	tag, err := s.db.Pool.Exec(ctx, `
		INSERT INTO lc_payment_request_attempts
			(user_id,invoice_id,execution_key,status,attempt_count,created_at,updated_at)
		VALUES ($1,$2,$3,'creating',1,now(),now())
		ON CONFLICT (user_id,invoice_id) DO NOTHING`, userID, invoiceID, executionKey)
	if err != nil {
		return nil, false, err
	}
	if tag.RowsAffected() == 1 {
		return &PaymentRequestAttempt{Status: "creating", ExecutionKey: executionKey, UpdatedAt: time.Now().UTC()}, true, nil
	}
	attempt, err := s.GetPaymentRequestAttempt(ctx, userID, invoiceID)
	if err != nil {
		return nil, false, err
	}
	if attempt.Status == "failed" {
		tag, err = s.db.Pool.Exec(ctx, `
			UPDATE lc_payment_request_attempts
			SET execution_key=$3,status='creating',attempt_count=attempt_count+1,
				last_error=NULL,updated_at=now()
			WHERE user_id=$1 AND invoice_id=$2 AND status='failed'`, userID, invoiceID, executionKey)
		if err != nil {
			return nil, false, err
		}
		if tag.RowsAffected() == 1 {
			attempt.Status = "creating"
			attempt.ExecutionKey = executionKey
			return attempt, true, nil
		}
	}
	return attempt, false, nil
}

func (s *LaventeCareStore) GetPaymentRequestAttempt(ctx context.Context, userID string, invoiceID uuid.UUID) (*PaymentRequestAttempt, error) {
	var attempt PaymentRequestAttempt
	err := s.db.Pool.QueryRow(ctx, `
		SELECT status,execution_key,provider_request_id,updated_at
		FROM lc_payment_request_attempts WHERE user_id=$1 AND invoice_id=$2`, userID, invoiceID,
	).Scan(&attempt.Status, &attempt.ExecutionKey, &attempt.ProviderRequestID, &attempt.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (s *LaventeCareStore) SavePaymentRequestAttemptSuccess(ctx context.Context, userID string, invoiceID uuid.UUID, executionKey, providerID string) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO lc_payment_request_attempts
			(user_id,invoice_id,execution_key,status,provider_request_id,created_at,updated_at)
		VALUES ($1,$2,$3,'succeeded',$4,now(),now())
		ON CONFLICT (user_id,invoice_id) DO UPDATE SET
			status='succeeded', provider_request_id=EXCLUDED.provider_request_id,
			last_error=NULL, updated_at=now()`, userID, invoiceID, executionKey, providerID)
	return err
}

func (s *LaventeCareStore) MarkPaymentRequestAttemptUnknown(ctx context.Context, userID string, invoiceID uuid.UUID, message string) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE lc_payment_request_attempts SET status='unknown',last_error=$3,updated_at=now()
		WHERE user_id=$1 AND invoice_id=$2`, userID, invoiceID, truncatePaymentAttemptError(message))
	return err
}

func (s *LaventeCareStore) MarkPaymentRequestAttemptFailed(ctx context.Context, userID string, invoiceID uuid.UUID, message string) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE lc_payment_request_attempts SET status='failed',last_error=$3,updated_at=now()
		WHERE user_id=$1 AND invoice_id=$2`, userID, invoiceID, truncatePaymentAttemptError(message))
	return err
}

func truncatePaymentAttemptError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		return value[:500]
	}
	return value
}
