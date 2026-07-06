package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/whatsapp"
)

// ensureWhatsappSchema creates the WhatsApp-import tables idempotently. Raw
// messages stay local (app UI/search); only whatsapp_summaries is exposed to the
// AI. Called from EnsureRuntimeSchema after the contacts tables exist.
func ensureWhatsappSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS whatsapp_conversations (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          TEXT NOT NULL,
    contact_id       UUID REFERENCES contacts(id) ON DELETE CASCADE,
    chat_name        TEXT NOT NULL,
    is_group         BOOLEAN NOT NULL DEFAULT false,
    message_count    INTEGER NOT NULL DEFAULT 0,
    first_message_at TIMESTAMPTZ,
    last_message_at  TIMESTAMPTZ,
    source_filename  TEXT,
    imported_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_wa_conv_contact ON whatsapp_conversations (contact_id);
CREATE INDEX IF NOT EXISTS idx_wa_conv_user ON whatsapp_conversations (user_id);

CREATE TABLE IF NOT EXISTS whatsapp_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         TEXT NOT NULL,
    conversation_id UUID NOT NULL REFERENCES whatsapp_conversations(id) ON DELETE CASCADE,
    sender          TEXT NOT NULL DEFAULT '',
    sent_at         TIMESTAMPTZ,
    body            TEXT NOT NULL,
    kind            TEXT NOT NULL DEFAULT 'text',
    seq             INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_wa_msg_conv ON whatsapp_messages (conversation_id, seq);

CREATE TABLE IF NOT EXISTS whatsapp_summaries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         TEXT NOT NULL,
    contact_id      UUID REFERENCES contacts(id) ON DELETE CASCADE,
    conversation_id UUID REFERENCES whatsapp_conversations(id) ON DELETE CASCADE,
    summary         TEXT NOT NULL,
    message_count   INTEGER NOT NULL DEFAULT 0,
    generated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_wa_summary_contact ON whatsapp_summaries (contact_id);
CREATE INDEX IF NOT EXISTS idx_wa_summary_user ON whatsapp_summaries (user_id);
`)
	return err
}

// ImportWhatsAppConversation stores a parsed export (conversation + messages +
// a metadata summary) for a contact, in one transaction.
func (s *ContactStore) ImportWhatsAppConversation(
	ctx context.Context, userID string, contactID uuid.UUID,
	chatName, sourceFilename string, isGroup bool, messages []whatsapp.Message,
) (model.WhatsAppConversation, model.WhatsAppSummary, error) {
	if err := s.assertOwns(ctx, userID, contactID); err != nil {
		return model.WhatsAppConversation{}, model.WhatsAppSummary{}, err
	}

	var first, last *time.Time
	for i := range messages {
		if messages[i].SentAt == nil {
			continue
		}
		if first == nil || messages[i].SentAt.Before(*first) {
			first = messages[i].SentAt
		}
		if last == nil || messages[i].SentAt.After(*last) {
			last = messages[i].SentAt
		}
	}
	var srcPtr *string
	if strings.TrimSpace(sourceFilename) != "" {
		src := strings.TrimSpace(sourceFilename)
		srcPtr = &src
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return model.WhatsAppConversation{}, model.WhatsAppSummary{}, err
	}
	defer tx.Rollback(ctx)

	var conv model.WhatsAppConversation
	err = tx.QueryRow(ctx, `
		INSERT INTO whatsapp_conversations
			(user_id, contact_id, chat_name, is_group, message_count, first_message_at, last_message_at, source_filename)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, user_id, contact_id, chat_name, is_group, message_count, first_message_at, last_message_at, source_filename, imported_at`,
		userID, contactID, chatName, isGroup, len(messages), first, last, srcPtr,
	).Scan(&conv.ID, &conv.UserID, &conv.ContactID, &conv.ChatName, &conv.IsGroup,
		&conv.MessageCount, &conv.FirstMessageAt, &conv.LastMessageAt, &conv.SourceFilename, &conv.ImportedAt)
	if err != nil {
		return model.WhatsAppConversation{}, model.WhatsAppSummary{}, err
	}

	if len(messages) > 0 {
		rows := make([][]any, 0, len(messages))
		for i := range messages {
			rows = append(rows, []any{userID, conv.ID, messages[i].Sender, messages[i].SentAt, messages[i].Body, messages[i].Kind, i})
		}
		if _, err = tx.CopyFrom(ctx,
			pgx.Identifier{"whatsapp_messages"},
			[]string{"user_id", "conversation_id", "sender", "sent_at", "body", "kind", "seq"},
			pgx.CopyFromRows(rows),
		); err != nil {
			return model.WhatsAppConversation{}, model.WhatsAppSummary{}, err
		}
	}

	summaryText := buildWhatsAppSummary(chatName, messages)
	var sum model.WhatsAppSummary
	err = tx.QueryRow(ctx, `
		INSERT INTO whatsapp_summaries (user_id, contact_id, conversation_id, summary, message_count)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, contact_id, conversation_id, summary, message_count, generated_at`,
		userID, contactID, conv.ID, summaryText, len(messages),
	).Scan(&sum.ID, &sum.UserID, &sum.ContactID, &sum.ConversationID, &sum.Summary, &sum.MessageCount, &sum.GeneratedAt)
	if err != nil {
		return model.WhatsAppConversation{}, model.WhatsAppSummary{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return model.WhatsAppConversation{}, model.WhatsAppSummary{}, err
	}
	return conv, sum, nil
}

// buildWhatsAppSummary produces a NON-verbatim metadata summary (no message
// bodies) — the only WhatsApp data ever handed to the external AI.
func buildWhatsAppSummary(chatName string, messages []whatsapp.Message) string {
	counts := map[string]int{}
	order := []string{}
	media := 0
	var first, last *time.Time
	for i := range messages {
		m := messages[i]
		if m.Kind == "media" {
			media++
		}
		if m.Kind == "system" {
			continue
		}
		if m.Sender != "" {
			if _, ok := counts[m.Sender]; !ok {
				order = append(order, m.Sender)
			}
			counts[m.Sender]++
		}
		if m.SentAt != nil {
			if first == nil || m.SentAt.Before(*first) {
				first = m.SentAt
			}
			if last == nil || m.SentAt.After(*last) {
				last = m.SentAt
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "WhatsApp-gesprek '%s': %d berichten", chatName, len(messages))
	if first != nil && last != nil {
		fmt.Fprintf(&b, " tussen %s en %s", first.Format("02-01-2006"), last.Format("02-01-2006"))
	}
	b.WriteString(". ")
	if len(order) > 0 {
		sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
		parts := make([]string, 0, len(order))
		for _, s := range order {
			parts = append(parts, fmt.Sprintf("%s (%d)", s, counts[s]))
		}
		fmt.Fprintf(&b, "Deelnemers: %s. ", strings.Join(parts, ", "))
	}
	if media > 0 {
		fmt.Fprintf(&b, "%d media-items. ", media)
	}
	b.WriteString("Alleen deze samenvatting is zichtbaar voor de AI; de berichten zelf blijven lokaal in de app.")
	return b.String()
}

// ListWhatsAppSummaries returns summaries, optionally for one contact (newest first).
func (s *ContactStore) ListWhatsAppSummaries(ctx context.Context, userID string, contactID *uuid.UUID, limit int) ([]model.WhatsAppSummary, error) {
	q := `SELECT id, user_id, contact_id, conversation_id, summary, message_count, generated_at
		FROM whatsapp_summaries WHERE user_id = $1`
	args := []any{userID}
	if contactID != nil {
		args = append(args, *contactID)
		q += fmt.Sprintf(" AND contact_id = $%d", len(args))
	}
	q += " ORDER BY generated_at DESC"
	if limit > 0 {
		args = append(args, limit)
		q += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := s.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WhatsAppSummary{}
	for rows.Next() {
		var m model.WhatsAppSummary
		if err := rows.Scan(&m.ID, &m.UserID, &m.ContactID, &m.ConversationID, &m.Summary, &m.MessageCount, &m.GeneratedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListWhatsAppConversations returns a contact's imported conversations (newest first).
func (s *ContactStore) ListWhatsAppConversations(ctx context.Context, userID string, contactID uuid.UUID) ([]model.WhatsAppConversation, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, user_id, contact_id, chat_name, is_group, message_count, first_message_at, last_message_at, source_filename, imported_at
		FROM whatsapp_conversations WHERE user_id = $1 AND contact_id = $2
		ORDER BY imported_at DESC`, userID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WhatsAppConversation{}
	for rows.Next() {
		var c model.WhatsAppConversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.ContactID, &c.ChatName, &c.IsGroup, &c.MessageCount, &c.FirstMessageAt, &c.LastMessageAt, &c.SourceFilename, &c.ImportedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListWhatsAppMessages returns messages of a conversation (in order).
func (s *ContactStore) ListWhatsAppMessages(ctx context.Context, userID string, conversationID uuid.UUID, limit int) ([]model.WhatsAppMessage, error) {
	q := `SELECT id, user_id, conversation_id, sender, sent_at, body, kind, seq
		FROM whatsapp_messages WHERE user_id = $1 AND conversation_id = $2 ORDER BY seq ASC`
	args := []any{userID, conversationID}
	if limit > 0 {
		args = append(args, limit)
		q += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := s.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WhatsAppMessage{}
	for rows.Next() {
		var m model.WhatsAppMessage
		if err := rows.Scan(&m.ID, &m.UserID, &m.ConversationID, &m.Sender, &m.SentAt, &m.Body, &m.Kind, &m.Seq); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
