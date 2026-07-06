package model

import (
	"time"

	"github.com/google/uuid"
)

// WhatsAppConversation is one imported chat export, linked to a contact.
type WhatsAppConversation struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	UserID         string     `json:"user_id" db:"user_id"`
	ContactID      *uuid.UUID `json:"contact_id" db:"contact_id"`
	ChatName       string     `json:"chat_name" db:"chat_name"`
	IsGroup        bool       `json:"is_group" db:"is_group"`
	MessageCount   int        `json:"message_count" db:"message_count"`
	FirstMessageAt *time.Time `json:"first_message_at" db:"first_message_at"`
	LastMessageAt  *time.Time `json:"last_message_at" db:"last_message_at"`
	SourceFilename *string    `json:"source_filename" db:"source_filename"`
	ImportedAt     time.Time  `json:"imported_at" db:"imported_at"`
}

// WhatsAppMessage is one line of an imported conversation (kept locally for the
// app UI/search; never sent to the AI — only summaries are).
type WhatsAppMessage struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	UserID         string     `json:"user_id" db:"user_id"`
	ConversationID uuid.UUID  `json:"conversation_id" db:"conversation_id"`
	Sender         string     `json:"sender" db:"sender"`
	SentAt         *time.Time `json:"sent_at" db:"sent_at"`
	Body           string     `json:"body" db:"body"`
	Kind           string     `json:"kind" db:"kind"` // text | media | system
	Seq            int        `json:"seq" db:"seq"`
}

// WhatsAppSummary is the distilled, non-verbatim summary of a conversation — the
// ONLY WhatsApp data exposed to the (external) Grok AI.
type WhatsAppSummary struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	UserID         string     `json:"user_id" db:"user_id"`
	ContactID      *uuid.UUID `json:"contact_id" db:"contact_id"`
	ConversationID *uuid.UUID `json:"conversation_id" db:"conversation_id"`
	Summary        string     `json:"summary" db:"summary"`
	MessageCount   int        `json:"message_count" db:"message_count"`
	GeneratedAt    time.Time  `json:"generated_at" db:"generated_at"`
}
