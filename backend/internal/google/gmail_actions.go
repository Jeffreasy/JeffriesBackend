package google

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/mail"
	"strings"
)

type gmailSendResponse struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

// ModifyGmailLabels adds and removes labels on a Gmail message.
func ModifyGmailLabels(ctx context.Context, client *OAuthClient, gmailID string, add, remove []string) error {
	if client == nil {
		return fmt.Errorf("Google OAuth client ontbreekt")
	}
	payload := map[string]any{
		"addLabelIds":    add,
		"removeLabelIds": remove,
	}
	return client.SendJSON(ctx, "POST", fmt.Sprintf("%s/messages/%s/modify", gmailBase, gmailID), payload, nil)
}

// TrashGmailMessage moves a Gmail message to trash.
func TrashGmailMessage(ctx context.Context, client *OAuthClient, gmailID string) error {
	if client == nil {
		return fmt.Errorf("Google OAuth client ontbreekt")
	}
	return client.SendJSON(ctx, "POST", fmt.Sprintf("%s/messages/%s/trash", gmailBase, gmailID), nil, nil)
}

// SendGmailMessage sends a plain text email through Gmail.
func SendGmailMessage(ctx context.Context, client *OAuthClient, to, subject, body string) (*gmailSendResponse, error) {
	if client == nil {
		return nil, fmt.Errorf("Google OAuth client ontbreekt")
	}
	raw := buildRawMail(to, subject, body)
	var result gmailSendResponse
	err := client.SendJSON(ctx, "POST", gmailBase+"/messages/send", map[string]any{"raw": raw}, &result)
	return &result, err
}

// ReplyGmailMessage sends a reply in an existing Gmail thread.
func ReplyGmailMessage(ctx context.Context, client *OAuthClient, threadID, to, subject, body string) (*gmailSendResponse, error) {
	if client == nil {
		return nil, fmt.Errorf("Google OAuth client ontbreekt")
	}
	raw := buildRawMail(to, normalizeReplySubject(subject), body)
	var result gmailSendResponse
	err := client.SendJSON(ctx, "POST", gmailBase+"/messages/send", map[string]any{
		"raw":      raw,
		"threadId": threadID,
	}, &result)
	return &result, err
}

func buildRawMail(to, subject, body string) string {
	headers := []string{
		"To: " + sanitizeMailHeader(to),
		"Subject: " + sanitizeMailHeader(subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	raw := strings.Join(headers, "\r\n") + "\r\n\r\n" + body
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func sanitizeMailHeader(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func normalizeReplySubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if strings.HasPrefix(strings.ToLower(subject), "re:") {
		return subject
	}
	if subject == "" {
		return "Re:"
	}
	return "Re: " + subject
}

// ExtractEmailAddress returns the address part from a display-name email value.
func ExtractEmailAddress(value string) string {
	addr, err := mail.ParseAddress(value)
	if err == nil && addr.Address != "" {
		return addr.Address
	}
	return strings.TrimSpace(value)
}
