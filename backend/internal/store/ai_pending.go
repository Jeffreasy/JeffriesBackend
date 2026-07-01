package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PendingAction represents an AI action awaiting user confirmation.
type PendingAction struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	AgentID   string    `json:"agent_id"`
	ToolName  string    `json:"tool_name"`
	ArgsJSON  string    `json:"args_json"`
	Summary   string    `json:"summary"`
	Code      string    `json:"code"`
	Status    string    `json:"status"`
	Result    *string   `json:"result,omitempty"`
	Error     *string   `json:"error,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// PendingStore handles AI pending action persistence.
type PendingStore struct {
	pool *pgxpool.Pool
}

func NewPendingStore(pool *pgxpool.Pool) *PendingStore {
	return &PendingStore{pool: pool}
}

// Create inserts a new pending action and returns it with generated code.
func (s *PendingStore) Create(ctx context.Context, userID, agentID, toolName, argsJSON, summary string) (*PendingAction, error) {
	code := generateCode()
	expiresAt := time.Now().Add(10 * time.Minute)

	var pa PendingAction
	err := s.pool.QueryRow(ctx,
		`INSERT INTO ai_pending_actions (user_id, agent_id, tool_name, args_json, summary, code, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, user_id, agent_id, tool_name, args_json, summary, code, status, expires_at, created_at`,
		userID, agentID, toolName, argsJSON, summary, code, expiresAt,
	).Scan(&pa.ID, &pa.UserID, &pa.AgentID, &pa.ToolName, &pa.ArgsJSON, &pa.Summary, &pa.Code, &pa.Status, &pa.ExpiresAt, &pa.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &pa, nil
}

// ListPending returns all pending actions for a user.
func (s *PendingStore) ListPending(ctx context.Context, userID string) ([]PendingAction, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, agent_id, tool_name, args_json, summary, code, status, expires_at, created_at
		 FROM ai_pending_actions
		 WHERE user_id = $1 AND status = 'pending' AND expires_at > now()
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []PendingAction
	for rows.Next() {
		var a PendingAction
		if err := rows.Scan(&a.ID, &a.UserID, &a.AgentID, &a.ToolName, &a.ArgsJSON, &a.Summary, &a.Code, &a.Status, &a.ExpiresAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		actions = append(actions, a)
	}
	return actions, nil
}

// FindPendingByToolArgs returns an existing pending action for the same tool call.
func (s *PendingStore) FindPendingByToolArgs(ctx context.Context, userID, toolName, argsJSON string) (*PendingAction, error) {
	var pa PendingAction
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, agent_id, tool_name, args_json, summary, code, status, expires_at, created_at
		 FROM ai_pending_actions
		 WHERE user_id = $1 AND tool_name = $2 AND args_json = $3 AND status = 'pending' AND expires_at > now()
		 ORDER BY created_at DESC
		 LIMIT 1`,
		userID, toolName, argsJSON,
	).Scan(&pa.ID, &pa.UserID, &pa.AgentID, &pa.ToolName, &pa.ArgsJSON, &pa.Summary, &pa.Code, &pa.Status, &pa.ExpiresAt, &pa.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &pa, nil
}

// Claim atomically claims a pending action for execution. Returns (nil, nil)
// — not an error — if no matching pending row exists (already claimed,
// rejected, or expired, or a TOCTOU race with a second claim attempt via a
// different UI entry point). Callers must check for a nil result rather than
// assuming a non-nil error means "not found".
func (s *PendingStore) Claim(ctx context.Context, id, userID string) (*PendingAction, error) {
	var pa PendingAction
	err := s.pool.QueryRow(ctx,
		`UPDATE ai_pending_actions
		 SET status = 'confirmed', updated_at = now()
		 WHERE id = $1 AND user_id = $2 AND status = 'pending' AND expires_at > now()
		 RETURNING id, user_id, agent_id, tool_name, args_json, summary, code, status, expires_at, created_at`,
		id, userID,
	).Scan(&pa.ID, &pa.UserID, &pa.AgentID, &pa.ToolName, &pa.ArgsJSON, &pa.Summary, &pa.Code, &pa.Status, &pa.ExpiresAt, &pa.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &pa, nil
}

// Cancel marks a pending action as cancelled for a user. Returns (nil, nil)
// — not an error — if no matching pending row exists, matching Claim/
// FindByCode's convention.
func (s *PendingStore) Cancel(ctx context.Context, id, userID string) (*PendingAction, error) {
	var pa PendingAction
	err := s.pool.QueryRow(ctx,
		`UPDATE ai_pending_actions
		 SET status = 'cancelled', updated_at = now()
		 WHERE id = $1 AND user_id = $2 AND status = 'pending'
		 RETURNING id, user_id, agent_id, tool_name, args_json, summary, code, status, expires_at, created_at`,
		id, userID,
	).Scan(&pa.ID, &pa.UserID, &pa.AgentID, &pa.ToolName, &pa.ArgsJSON, &pa.Summary, &pa.Code, &pa.Status, &pa.ExpiresAt, &pa.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &pa, nil
}

// MarkStatus updates an action's status.
func (s *PendingStore) MarkStatus(ctx context.Context, id, status string, result, errMsg *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE ai_pending_actions SET status = $2, result = $3, error = $4, updated_at = now() WHERE id = $1`,
		id, status, result, errMsg,
	)
	return err
}

// FindByCode finds a pending action by its confirmation code. Returns
// (nil, nil) — not an error — if the code is unknown, already used, or
// expired, matching FindPendingByToolArgs's convention above so callers
// can show a specific "code onbekend/verlopen" message instead of
// forwarding a raw pgx.ErrNoRows ("no rows in result set") to the user.
func (s *PendingStore) FindByCode(ctx context.Context, userID, code string) (*PendingAction, error) {
	var pa PendingAction
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, agent_id, tool_name, args_json, summary, code, status, expires_at, created_at
		 FROM ai_pending_actions
		 WHERE user_id = $1 AND code = $2 AND status = 'pending' AND expires_at > now()`,
		userID, strings.ToUpper(code),
	).Scan(&pa.ID, &pa.UserID, &pa.AgentID, &pa.ToolName, &pa.ArgsJSON, &pa.Summary, &pa.Code, &pa.Status, &pa.ExpiresAt, &pa.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &pa, nil
}

// ExpireOld marks all expired pending actions.
func (s *PendingStore) ExpireOld(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE ai_pending_actions SET status = 'expired', updated_at = now()
		 WHERE status = 'pending' AND expires_at <= now()`,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func generateCode() string {
	b := make([]byte, 3)
	rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}
