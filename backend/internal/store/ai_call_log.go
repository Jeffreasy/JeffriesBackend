package store

import (
	"context"
	"strings"
	"time"
)

// AICallLog is one recorded AI interaction (Grok chat or web search).
type AICallLog struct {
	UserID           string
	AgentID          string
	Model            string
	Kind             string // "chat" | "web_search"
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Rounds           int
	DurationMs       int
	ToolsUsed        []string
	FinishReason     string
	OK               bool
	Error            string
}

// AICallLogStore persists and aggregates AI call telemetry.
type AICallLogStore struct {
	db *DB
}

func NewAICallLogStore(db *DB) *AICallLogStore {
	return &AICallLogStore{db: db}
}

// Insert records a single AI call. Best-effort: callers typically ignore the error.
func (s *AICallLogStore) Insert(ctx context.Context, e AICallLog) error {
	if e.Kind == "" {
		e.Kind = "chat"
	}
	errMsg := e.Error
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO ai_call_log
			(user_id, agent_id, model, kind, prompt_tokens, completion_tokens,
			 total_tokens, rounds, duration_ms, tools_used, finish_reason, ok, error)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		e.UserID, e.AgentID, e.Model, e.Kind,
		e.PromptTokens, e.CompletionTokens, e.TotalTokens,
		e.Rounds, e.DurationMs, strings.Join(e.ToolsUsed, ","),
		e.FinishReason, e.OK, errMsg,
	)
	return err
}

// AIUsageWindow is an aggregate over a time window.
type AIUsageWindow struct {
	Calls            int   `json:"calls"`
	Errors           int   `json:"errors"`
	PromptTokens     int64 `json:"promptTokens"`
	CompletionTokens int64 `json:"completionTokens"`
	TotalTokens      int64 `json:"totalTokens"`
	AvgDurationMs    int   `json:"avgDurationMs"`
	MaxDurationMs    int   `json:"maxDurationMs"`
}

// UsageSince aggregates AI calls created at or after `since`.
func (s *AICallLogStore) UsageSince(ctx context.Context, since time.Time) (AIUsageWindow, error) {
	var w AIUsageWindow
	err := s.db.Pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE NOT ok),
			COALESCE(SUM(prompt_tokens),0),
			COALESCE(SUM(completion_tokens),0),
			COALESCE(SUM(total_tokens),0),
			COALESCE(ROUND(AVG(duration_ms))::int,0),
			COALESCE(MAX(duration_ms),0)
		FROM ai_call_log
		WHERE created_at >= $1`, since,
	).Scan(&w.Calls, &w.Errors, &w.PromptTokens, &w.CompletionTokens,
		&w.TotalTokens, &w.AvgDurationMs, &w.MaxDurationMs)
	return w, err
}
