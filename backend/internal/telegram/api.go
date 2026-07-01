package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const tgBase = "https://api.telegram.org/bot"

const (
	// telegramHardLimit is Telegram's actual max message length (post-escaping).
	telegramHardLimit = 4096
	// telegramChunkByteTarget is the ESCAPED-byte budget splitForTelegram aims
	// for per chunk, safely under telegramHardLimit. This MUST be a byte
	// budget, not a rune count: Dutch text is full of multi-byte UTF-8 (€=3
	// bytes, é/ë=2 bytes, emoji=4 bytes), so a fixed rune count can produce a
	// chunk many times larger than intended in bytes — a message dense with
	// € (the finance/cockpit replies) would blow straight past
	// telegramHardLimit and get silently truncated at send time regardless
	// of chunking, exactly the bug this rework exists to prevent.
	telegramChunkByteTarget = 3500
)

// Client wraps the Telegram Bot API.
type Client struct {
	token      string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token:      token,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) apiURL(method string) string {
	return fmt.Sprintf("%s%s/%s", tgBase, c.token, method)
}

// post is the single choke point every public Client method routes through.
// Every call site across the codebase historically did `_ = client.SendMessage(...)`
// with no error check, so a failed send (transient network error, Telegram
// 429, "message is too long" after escaping) left zero diagnostic trail —
// nothing to explain why a reply, note-action confirmation, or pending-action
// outcome silently never arrived. Logging here once, rather than requiring
// every one of the ~30+ call sites to check and log individually, means new
// call sites get this for free too.
func (c *Client) post(method string, body any) (json.RawMessage, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(mustReq("POST", c.apiURL(method), data))
	if err != nil {
		slog.Warn("telegram API request failed", "method", method, "chatID", extractChatID(body), "error", err)
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var result struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		slog.Warn("telegram API response parse failed", "method", method, "chatID", extractChatID(body), "error", err)
		return nil, fmt.Errorf("telegram parse: %w", err)
	}
	if !result.OK {
		slog.Warn("telegram API returned not-ok", "method", method, "chatID", extractChatID(body), "response", safeTruncateBytes(string(raw), 300))
		return nil, fmt.Errorf("telegram API: %s", string(raw))
	}
	return result.Result, nil
}

// extractChatID best-effort pulls chat_id out of a request body for log
// context — every relevant Client method builds body as map[string]any with
// a chat_id key, but this stays nil-safe for the few that don't (setMyCommands).
func extractChatID(body any) any {
	if m, ok := body.(map[string]any); ok {
		return m["chat_id"]
	}
	return nil
}

// SendMessage sends a plain text message (HTML parse mode), splitting into
// multiple sequential messages if it exceeds Telegram's length limit instead
// of silently hard-truncating. This matters most for the densest replies
// (dagbriefing, LaventeCare cockpit) — those are exactly the ones most
// likely to get cut off mid-sentence with no indication anything was lost.
func (c *Client) SendMessage(chatID int64, text string) error {
	for _, chunk := range splitForTelegram(text) {
		if _, err := c.post("sendMessage", map[string]any{
			"chat_id":    chatID,
			"text":       escapeAndCapForTelegram(chunk),
			"parse_mode": "HTML",
		}); err != nil {
			return err
		}
	}
	return nil
}

// SendMessageWithKeyboard sends a text message with an inline keyboard. Not
// chunked (a keyboard belongs to exactly one message), so long text is
// safety-truncated after escaping rather than before — escaping first and
// truncating the escaped string means entity expansion can never push the
// payload past Telegram's real limit.
func (c *Client) SendMessageWithKeyboard(chatID int64, text string, keyboard InlineKeyboardMarkup) error {
	_, err := c.post("sendMessage", map[string]any{
		"chat_id":      chatID,
		"text":         escapeAndCapForTelegram(text),
		"parse_mode":   "HTML",
		"reply_markup": keyboard,
	})
	return err
}

// BotCommand is one entry in the Telegram "/" command menu.
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// SetMyCommands registers the bot's command list so Telegram shows the native
// "/" autocomplete menu. Call once at startup.
func (c *Client) SetMyCommands(commands []BotCommand) error {
	_, err := c.post("setMyCommands", map[string]any{
		"commands": commands,
	})
	return err
}

// AnswerCallbackQuery removes the loading state from a clicked inline button.
func (c *Client) AnswerCallbackQuery(callbackQueryID string, text string) error {
	payload := map[string]any{
		"callback_query_id": callbackQueryID,
	}
	if text != "" {
		payload["text"] = text
	}
	_, err := c.post("answerCallbackQuery", payload)
	return err
}

// EditMessageText replaces the text and optionally keyboard of an existing message.
func (c *Client) EditMessageText(chatID int64, messageID int64, text string, keyboard *InlineKeyboardMarkup) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       escapeAndCapForTelegram(text),
		"parse_mode": "HTML",
	}
	if keyboard != nil {
		payload["reply_markup"] = *keyboard
	}
	_, err := c.post("editMessageText", payload)
	return err
}

// SendTyping sends the "typing..." indicator.
func (c *Client) SendTyping(chatID int64) error {
	_, err := c.post("sendChatAction", map[string]any{
		"chat_id": chatID,
		"action":  "typing",
	})
	return err
}

// GetFile retrieves file metadata by file_id.
func (c *Client) GetFile(fileID string) (string, error) {
	raw, err := c.post("getFile", map[string]any{"file_id": fileID})
	if err != nil {
		return "", err
	}
	var f struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return "", err
	}
	return f.FilePath, nil
}

// DownloadFile downloads a file from the Telegram servers.
func (c *Client) DownloadFile(filePath string) ([]byte, error) {
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", c.token, filePath)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// GetMe returns bot info.
func (c *Client) GetMe() (BotInfo, error) {
	raw, err := c.post("getMe", map[string]any{})
	if err != nil {
		return BotInfo{}, err
	}
	var info BotInfo
	return info, json.Unmarshal(raw, &info)
}

// DeleteWebhook removes any active webhook.
func (c *Client) DeleteWebhook(dropPending bool) error {
	_, err := c.post("deleteWebhook", map[string]any{"drop_pending_updates": dropPending})
	return err
}

// GetUpdates long-polls for new messages.
func (c *Client) GetUpdates(offset int64, timeout int) ([]Update, error) {
	return c.GetUpdatesContext(context.Background(), offset, timeout)
}

// GetUpdatesContext long-polls for new messages and aborts when ctx is cancelled.
func (c *Client) GetUpdatesContext(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	// Use a longer timeout for the HTTP client during long-polling
	client := &http.Client{Timeout: time.Duration(timeout+5) * time.Second}
	data, _ := json.Marshal(map[string]any{
		"offset":          offset,
		"timeout":         timeout,
		"allowed_updates": []string{"message", "callback_query"},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL("getUpdates"), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var result struct {
		OK          bool     `json:"ok"`
		Result      []Update `json:"result"`
		ErrorCode   int      `json:"error_code,omitempty"`
		Description string   `json:"description,omitempty"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram getUpdates: %s", string(raw))
	}
	return result.Result, nil
}

// GetWebhookInfo returns current webhook/polling state from Telegram.
func (c *Client) GetWebhookInfo() (WebhookInfo, error) {
	raw, err := c.post("getWebhookInfo", map[string]any{})
	if err != nil {
		return WebhookInfo{}, err
	}
	var info WebhookInfo
	return info, json.Unmarshal(raw, &info)
}

// ─── Types ──────────────────────────────────────────────────────────────────

type BotInfo struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

type WebhookInfo struct {
	URL               string   `json:"url"`
	HasCustomCert     bool     `json:"has_custom_certificate"`
	PendingUpdates    int      `json:"pending_update_count"`
	LastErrorDate     int64    `json:"last_error_date,omitempty"`
	LastErrorMessage  string   `json:"last_error_message,omitempty"`
	MaxConnections    int      `json:"max_connections,omitempty"`
	AllowedUpdates    []string `json:"allowed_updates,omitempty"`
	IPAddress         string   `json:"ip_address,omitempty"`
	LastSyncErrorDate int64    `json:"last_synchronization_error_date,omitempty"`
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitempty"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Chat      *Chat  `json:"chat,omitempty"`
	Text      string `json:"text,omitempty"`
	Voice     *Voice `json:"voice,omitempty"`
	Audio     *Voice `json:"audio,omitempty"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type Voice struct {
	FileID string `json:"file_id"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func escapeHTML(text string) string {
	r := []byte(text)
	r = bytes.ReplaceAll(r, []byte("&"), []byte("&amp;"))
	r = bytes.ReplaceAll(r, []byte("<"), []byte("&lt;"))
	r = bytes.ReplaceAll(r, []byte(">"), []byte("&gt;"))
	return string(r)
}

// escapeAndCapForTelegram escapes HTML first, THEN caps the result to
// Telegram's real limit. Escaping expands &, <, > by 3-4 bytes each, so
// truncating the raw text first (as this used to do) could push an
// already-near-limit message over 4096 after escaping, causing Telegram to
// reject the send outright instead of merely truncating it.
func escapeAndCapForTelegram(text string) string {
	escaped := escapeHTML(text)
	if len(escaped) <= telegramHardLimit {
		return escaped
	}
	return safeTruncateBytes(escaped, telegramHardLimit-3) + "..."
}

// safeTruncateBytes truncates to at most maxBytes without splitting a
// multi-byte UTF-8 rune (Dutch text has €/é/ë, plus emoji). A naive check
// for "is this byte a rune-start byte" is NOT sufficient here — a lead byte
// with its continuation byte(s) cut off still passes that check while
// leaving an invalid dangling sequence; DecodeLastRuneInString correctly
// detects an incomplete trailing sequence via (RuneError, size==1).
func safeTruncateBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	b := s[:maxBytes]
	for len(b) > 0 {
		r, size := utf8.DecodeLastRuneInString(b)
		if r != utf8.RuneError || size != 1 {
			break
		}
		b = b[:len(b)-1]
	}
	return b
}

// splitForTelegram splits text into chunks bounded by telegramChunkByteTarget
// ESCAPED bytes (not runes), preferring to break at the last newline within
// the window so a chunk doesn't cut off mid-sentence any more than
// necessary. Leading/trailing blank lines at a break point are trimmed.
// Budgeting by escaped bytes (via escapedRuneByteLen) rather than a rune
// count is the whole point: it's the only way to guarantee every chunk
// actually stays under Telegram's byte-based hard limit regardless of how
// many multi-byte runes (€, é, ë, emoji) it contains.
func splitForTelegram(text string) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	if escapedByteLen(text) <= telegramChunkByteTarget {
		return []string{text}
	}

	var chunks []string
	start := 0
	for start < len(runes) {
		byteLen := 0
		lastNewline := -1
		end := start
		for end < len(runes) {
			next := byteLen + escapedRuneByteLen(runes[end])
			if next > telegramChunkByteTarget {
				break
			}
			byteLen = next
			if runes[end] == '\n' {
				lastNewline = end
			}
			end++
		}
		if end >= len(runes) {
			chunks = append(chunks, string(runes[start:]))
			break
		}
		cut := end
		// Prefer breaking at the last newline within this window, as long as
		// it isn't so far back it would produce a tiny chunk.
		if lastNewline >= 0 && lastNewline > start+(end-start)/2 {
			cut = lastNewline
		}
		if cut <= start {
			// No forward progress possible via the preferred boundary
			// (shouldn't happen — a single rune's escaped size is always far
			// smaller than telegramChunkByteTarget) — hard-cut one rune to
			// guarantee the loop terminates.
			cut = start + 1
		}
		chunk := strings.TrimRight(string(runes[start:cut]), "\n")
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		start = cut
		for start < len(runes) && runes[start] == '\n' {
			start++
		}
	}
	if len(chunks) == 0 {
		chunks = append(chunks, "")
	}
	return chunks
}

// escapedByteLen returns the byte length text would have after escapeHTML.
func escapedByteLen(text string) int {
	total := 0
	for _, r := range text {
		total += escapedRuneByteLen(r)
	}
	return total
}

// escapedRuneByteLen returns how many bytes a single rune contributes after
// HTML-escaping (&, <, > expand; everything else is its normal UTF-8 width).
func escapedRuneByteLen(r rune) int {
	switch r {
	case '&':
		return 5 // &amp;
	case '<':
		return 4 // &lt;
	case '>':
		return 4 // &gt;
	default:
		return utf8.RuneLen(r)
	}
}

func mustReq(method, url string, body []byte) *http.Request {
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}
