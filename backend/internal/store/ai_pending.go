package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)


// maxCodeGenerationAttempts bounds the collision-retry loop in Create. Codes
// are 6 hex chars (16.7M possibilities) scoped to a single user's pending
// rows, so a collision on the first attempt is already rare; this is a
// backstop against an unlucky run, not an expected path.
const maxCodeGenerationAttempts = 5

// wrapStoreError logs a raw database failure server-side and returns a
// static, safe Dutch message in its place. Only pgx.ErrNoRows and unique-
// violation are handled specially by callers above this — every other
// failure (pool exhaustion, network errors, an unmapped constraint) must
// never reach a caller as raw driver/SQLSTATE text, since some callers (the
// pending-action HTTP handler in particular) surface err.Error() directly in
// API responses.
func wrapStoreError(action string, err error) error {
	slog.Warn("ai_pending store failure", "action", action, "error", err)
	return fmt.Errorf("pending actie %s mislukt, probeer het opnieuw", action)
}

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
// The code is only unique among a user's other *pending* rows (enforced by
// idx_ai_pending_user_code_pending), so a collision is retried with a fresh
// code rather than surfaced as an error.
func (s *PendingStore) Create(ctx context.Context, userID, agentID, toolName, argsJSON, summary string) (*PendingAction, error) {
	argsJSON = normalizeJSON(argsJSON)
	expiresAt := time.Now().Add(10 * time.Minute)


	for attempt := 0; attempt < maxCodeGenerationAttempts; attempt++ {
		code := generateCode()

		var pa PendingAction
		err := s.pool.QueryRow(ctx,
			`INSERT INTO ai_pending_actions (user_id, agent_id, tool_name, args_json, summary, code, expires_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 RETURNING id, user_id, agent_id, tool_name, args_json, summary, code, status, expires_at, created_at`,
			userID, agentID, toolName, argsJSON, summary, code, expiresAt,
		).Scan(&pa.ID, &pa.UserID, &pa.AgentID, &pa.ToolName, &pa.ArgsJSON, &pa.Summary, &pa.Code, &pa.Status, &pa.ExpiresAt, &pa.CreatedAt)
		if err != nil {
			if isUniqueViolation(err) {
				continue
			}
			return nil, wrapStoreError("aanmaken", err)
		}
		return &pa, nil
	}
	return nil, fmt.Errorf("kon geen unieke bevestigingscode genereren na %d pogingen", maxCodeGenerationAttempts)
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
		return nil, wrapStoreError("ophalen", err)
	}
	defer rows.Close()

	var actions []PendingAction
	for rows.Next() {
		var a PendingAction
		if err := rows.Scan(&a.ID, &a.UserID, &a.AgentID, &a.ToolName, &a.ArgsJSON, &a.Summary, &a.Code, &a.Status, &a.ExpiresAt, &a.CreatedAt); err != nil {
			return nil, wrapStoreError("ophalen", err)
		}
		actions = append(actions, a)
	}
	// rows.Next() also returns false on a cursor/iteration failure (connection
	// drop mid-stream, server-side error), not just on genuine exhaustion —
	// without this check that failure silently looks like "no more rows" and
	// a partial list gets returned as if it were the complete one.
	if err := rows.Err(); err != nil {
		return nil, wrapStoreError("ophalen", err)
	}
	return actions, nil
}

// FindPendingByToolArgs returns an existing pending action for the same tool call.
func (s *PendingStore) FindPendingByToolArgs(ctx context.Context, userID, toolName, argsJSON string) (*PendingAction, error) {
	argsJSON = normalizeJSON(argsJSON)
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
		return nil, wrapStoreError("ophalen", err)
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
		return nil, wrapStoreError("claimen", err)
	}
	return &pa, nil
}

// Cancel marks a pending action as cancelled for a user. Returns (nil, nil)
// — not an error — if no matching pending row exists, matching Claim/
// FindByCode's convention. Also requires expires_at > now(), same as those
// two, so an already-expired action reads as "not found" instead of being
// mutable into 'cancelled' after the fact.
func (s *PendingStore) Cancel(ctx context.Context, id, userID string) (*PendingAction, error) {
	var pa PendingAction
	err := s.pool.QueryRow(ctx,
		`UPDATE ai_pending_actions
		 SET status = 'cancelled', updated_at = now()
		 WHERE id = $1 AND user_id = $2 AND status = 'pending' AND expires_at > now()
		 RETURNING id, user_id, agent_id, tool_name, args_json, summary, code, status, expires_at, created_at`,
		id, userID,
	).Scan(&pa.ID, &pa.UserID, &pa.AgentID, &pa.ToolName, &pa.ArgsJSON, &pa.Summary, &pa.Code, &pa.Status, &pa.ExpiresAt, &pa.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, wrapStoreError("annuleren", err)
	}
	return &pa, nil
}

// MarkStatus updates an action's status. Scoped to userID (not id alone) so a
// bad/spoofed action id can never mutate another user's row — matches the
// scoping every other mutating method here already uses.
func (s *PendingStore) MarkStatus(ctx context.Context, id, userID, status string, result, errMsg *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE ai_pending_actions SET status = $3, result = $4, error = $5, updated_at = now() WHERE id = $1 AND user_id = $2`,
		id, userID, status, result, errMsg,
	)
	if err != nil {
		return wrapStoreError("status bijwerken", err)
	}
	return nil
}

// FindByCode finds a pending action by its confirmation code. Returns
// (nil, nil) — not an error — if the code is unknown, already used, or
// expired, matching FindPendingByToolArgs's convention above so callers
// can show a specific "code onbekend/verlopen" message instead of
// forwarding a raw pgx.ErrNoRows ("no rows in result set") to the user.
// ORDER BY/LIMIT 1 is a defensive backstop for rows created before
// idx_ai_pending_user_code_pending existed — it picks the most recent match
// rather than erroring on multiple rows.
func (s *PendingStore) FindByCode(ctx context.Context, userID, code string) (*PendingAction, error) {
	var pa PendingAction
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, agent_id, tool_name, args_json, summary, code, status, expires_at, created_at
		 FROM ai_pending_actions
		 WHERE user_id = $1 AND code = $2 AND status = 'pending' AND expires_at > now()
		 ORDER BY created_at DESC
		 LIMIT 1`,
		userID, strings.ToUpper(code),
	).Scan(&pa.ID, &pa.UserID, &pa.AgentID, &pa.ToolName, &pa.ArgsJSON, &pa.Summary, &pa.Code, &pa.Status, &pa.ExpiresAt, &pa.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, wrapStoreError("opzoeken", err)
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
		return 0, wrapStoreError("opschonen", err)
	}
	return tag.RowsAffected(), nil
}

func generateCode() string {
	b := make([]byte, 3)
	rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

func normalizeJSON(js string) string {
	var val any
	if err := json.Unmarshal([]byte(js), &val); err != nil {
		return js
	}
	normalized, err := json.Marshal(val)
	if err != nil {
		return js
	}
	return string(normalized)
}

