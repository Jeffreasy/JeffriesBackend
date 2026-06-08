package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
)

const (
	graphBaseURL = "https://graph.microsoft.com/v1.0"
	graphScope   = "https://graph.microsoft.com/.default"
)

var ErrNotConfigured = errors.New("laventecare mail is not configured")

type Sender struct {
	cfg   *config.Config
	http  *http.Client
	mu    sync.Mutex
	token *tokenCache
}

type tokenCache struct {
	accessToken string
	expiresAt   time.Time
}

type SendInput struct {
	To      []string
	CC      []string
	BCC     []string
	Subject string
	HTML    string
	Text    string
}

type SendResult struct {
	ProviderMessageID string
}

func NewSender(cfg *config.Config) *Sender {
	return &Sender{
		cfg: cfg,
		http: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (s *Sender) Configured() bool {
	return s != nil && s.cfg != nil && s.cfg.LaventeCareMailConfigured()
}

func (s *Sender) SenderEmail() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	return s.cfg.MicrosoftSenderEmail
}

func (s *Sender) Send(ctx context.Context, input SendInput) (*SendResult, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	to := normalizeAddresses(input.To)
	if len(to) == 0 {
		return nil, errors.New("mail recipient is required")
	}
	subject := strings.TrimSpace(input.Subject)
	if subject == "" {
		return nil, errors.New("mail subject is required")
	}
	content := strings.TrimSpace(input.HTML)
	contentType := "HTML"
	if content == "" {
		content = strings.TrimSpace(input.Text)
		contentType = "Text"
	}
	if content == "" {
		return nil, errors.New("mail body is required")
	}

	payload := map[string]any{
		"message": map[string]any{
			"subject": subject,
			"body": map[string]string{
				"contentType": contentType,
				"content":     content,
			},
			"toRecipients":  toRecipients(to),
			"ccRecipients":  toRecipients(normalizeAddresses(input.CC)),
			"bccRecipients": toRecipients(normalizeAddresses(input.BCC)),
		},
		"saveToSentItems": true,
	}

	if err := s.graphRequest(ctx, "POST", fmt.Sprintf("/users/%s/sendMail", url.PathEscape(s.cfg.MicrosoftSenderEmail)), payload, nil); err != nil {
		return nil, err
	}

	return &SendResult{
		ProviderMessageID: "graph-send-" + time.Now().UTC().Format("20060102T150405.000000000Z"),
	}, nil
}

func (s *Sender) graphRequest(ctx context.Context, method, path string, body any, out any) error {
	token, err := s.accessToken(ctx)
	if err != nil {
		return err
	}

	var bodyReader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, graphBaseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("microsoft graph request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(text)))
	}
	if out == nil || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusAccepted {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *Sender) accessToken(ctx context.Context) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	now := time.Now().UTC()
	s.mu.Lock()
	if s.token != nil && s.token.expiresAt.After(now.Add(60*time.Second)) {
		token := s.token.accessToken
		s.mu.Unlock()
		return token, nil
	}
	s.mu.Unlock()

	form := url.Values{}
	form.Set("client_id", s.cfg.MicrosoftClientID)
	form.Set("client_secret", s.cfg.MicrosoftClientSecret)
	form.Set("grant_type", "client_credentials")
	form.Set("scope", graphScope)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", url.PathEscape(s.cfg.MicrosoftTenantID)),
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("microsoft token request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(text)))
	}

	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", errors.New("microsoft token response has no access_token")
	}
	if token.ExpiresIn <= 0 {
		token.ExpiresIn = 3600
	}

	s.mu.Lock()
	s.token = &tokenCache{
		accessToken: token.AccessToken,
		expiresAt:   now.Add(time.Duration(token.ExpiresIn) * time.Second),
	}
	s.mu.Unlock()

	return token.AccessToken, nil
}

func normalizeAddresses(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		address := strings.ToLower(strings.TrimSpace(value))
		if address == "" || seen[address] {
			continue
		}
		seen[address] = true
		out = append(out, address)
	}
	return out
}

func toRecipients(addresses []string) []map[string]map[string]string {
	recipients := make([]map[string]map[string]string, 0, len(addresses))
	for _, address := range addresses {
		recipients = append(recipients, map[string]map[string]string{
			"emailAddress": {"address": address},
		})
	}
	return recipients
}
