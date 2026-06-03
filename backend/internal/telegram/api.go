package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const tgBase = "https://api.telegram.org/bot"

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

func (c *Client) post(method string, body any) (json.RawMessage, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(mustReq("POST", c.apiURL(method), data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var result struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("telegram parse: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram API: %s", string(raw))
	}
	return result.Result, nil
}

// SendMessage sends a plain text message (HTML parse mode).
func (c *Client) SendMessage(chatID int64, text string) error {
	if len(text) > 4000 {
		text = text[:3997] + "..."
	}
	_, err := c.post("sendMessage", map[string]any{
		"chat_id":    chatID,
		"text":       escapeHTML(text),
		"parse_mode": "HTML",
	})
	return err
}

// SendMessageWithKeyboard sends a text message with an inline keyboard.
func (c *Client) SendMessageWithKeyboard(chatID int64, text string, keyboard InlineKeyboardMarkup) error {
	if len(text) > 4000 {
		text = text[:3997] + "..."
	}
	_, err := c.post("sendMessage", map[string]any{
		"chat_id":      chatID,
		"text":         escapeHTML(text),
		"parse_mode":   "HTML",
		"reply_markup": keyboard,
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
	if len(text) > 4000 {
		text = text[:3997] + "..."
	}
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       escapeHTML(text),
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
	// Use a longer timeout for the HTTP client during long-polling
	client := &http.Client{Timeout: time.Duration(timeout+5) * time.Second}
	data, _ := json.Marshal(map[string]any{
		"offset":          offset,
		"timeout":         timeout,
		"allowed_updates": []string{"message", "callback_query"},
	})
	resp, err := client.Do(mustReq("POST", c.apiURL("getUpdates"), data))
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

func mustReq(method, url string, body []byte) *http.Request {
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}
