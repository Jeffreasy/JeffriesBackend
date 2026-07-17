package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ChatMessage represents a Telegram chat message.
type ChatMessage struct {
	ID        string    `json:"id"`
	ChatID    int64     `json:"chat_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	AgentID   *string   `json:"agent_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ChatStore handles chat message persistence.
type ChatStore struct {
	pool *pgxpool.Pool
}

func NewChatStore(pool *pgxpool.Pool) *ChatStore {
	return &ChatStore{pool: pool}
}

// SaveMessage saves a chat message.
func (s *ChatStore) SaveMessage(ctx context.Context, chatID int64, role, content string, agentID *string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chat_messages (chat_id, role, content, agent_id) VALUES ($1, $2, $3, $4)`,
		chatID, role, content, agentID,
	)
	return err
}

// GetHistory returns recent messages for a chat, most recent first reversed to chronological.
func (s *ChatStore) GetHistory(ctx context.Context, chatID int64, limit int) ([]ChatMessage, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, chat_id, role, content, agent_id, created_at
		 FROM chat_messages
		 WHERE chat_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		chatID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.ChatID, &m.Role, &m.Content, &m.AgentID, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse to chronological order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}
