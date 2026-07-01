package google

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrGoogleReauthRequired signals that the refresh token is expired or revoked
// (Google returns invalid_grant). Callers can errors.Is() this to surface a
// single actionable "re-authenticate" message instead of a generic error.
var ErrGoogleReauthRequired = errors.New("google re-authentication required (refresh token invalid_grant)")

// APIError is returned for non-2xx Google API responses and exposes the HTTP
// status so callers can branch on it (e.g. a 404 expired Gmail historyId, which
// is expected and warrants a full resync, versus a transient 5xx/429).
type APIError struct {
	Method string
	URL    string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d — %s", e.Method, e.URL, e.Status, e.Body)
}

// StatusCode extracts the HTTP status from a Google APIError anywhere in the
// error chain, or 0 if the error is not an APIError.
func StatusCode(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || (status >= 500 && status <= 599)
}

const (
	maxGetRetries  = 3
	retryBaseDelay = 400 * time.Millisecond
)

// OAuthClient manages Google OAuth2 token refresh and authenticated HTTP calls.
type OAuthClient struct {
	clientID     string
	clientSecret string
	refreshToken string
	httpClient   *http.Client

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}


// NewOAuthClient creates a new Google OAuth client.
func NewOAuthClient(clientID, clientSecret, refreshToken string) *OAuthClient {
	return &OAuthClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		refreshToken: refreshToken,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}


var (
	sharedClientOnce sync.Once
	sharedClient     *OAuthClient
)

// SharedOAuthClient returns a process-wide OAuthClient so the access-token cache
// is shared across the crons and all HTTP handlers, instead of every call site
// building its own client and re-refreshing the token on each request. All call
// sites use the same env-derived credentials, which only change on redeploy
// (process restart), so a singleton is safe.
func SharedOAuthClient(clientID, clientSecret, refreshToken string) *OAuthClient {
	sharedClientOnce.Do(func() {
		sharedClient = NewOAuthClient(clientID, clientSecret, refreshToken)
	})
	return sharedClient
}

// tokenResponse maps the Google token endpoint JSON response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// getAccessToken returns a valid access token, refreshing if expired.
func (c *OAuthClient) getAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-60*time.Second)) {
		return c.accessToken, nil
	}

	data := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"refresh_token": {c.refreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		// Classify an expired/revoked refresh token so callers can react to it
		// distinctly (one re-auth alert) instead of treating it as a transient error.
		var gerr struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &gerr)
		if gerr.Error == "invalid_grant" {
			return "", fmt.Errorf("%w (HTTP %d): %s", ErrGoogleReauthRequired, resp.StatusCode, string(body))
		}
		return "", fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}

	c.accessToken = tok.AccessToken
	c.expiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)

	return c.accessToken, nil
}

// Do executes an authenticated HTTP request against a Google API.
func (c *OAuthClient) Do(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// GetJSON performs a GET request and decodes the JSON response into result.
// Transient failures (HTTP 429/5xx) are retried with bounded exponential backoff
// plus jitter, since GETs are idempotent. Non-retryable statuses return an
// *APIError so callers can branch on the status code.
func (c *OAuthClient) GetJSON(ctx context.Context, url string, result any) error {
	var lastErr error
	for attempt := 0; attempt <= maxGetRetries; attempt++ {
		if attempt > 0 {
			delay := retryBaseDelay*time.Duration(1<<(attempt-1)) + time.Duration(rand.Int63n(int64(retryBaseDelay)))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		resp, err := c.Do(ctx, "GET", url, nil)
		if err != nil {
			// Token errors (incl. invalid_grant) are not retryable here.
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return json.Unmarshal(body, result)
		}

		apiErr := &APIError{Method: "GET", URL: url, Status: resp.StatusCode, Body: string(body)}
		if !isRetryableStatus(resp.StatusCode) {
			return apiErr
		}
		lastErr = apiErr
		slog.Warn("google GET transient error, retrying", "url", url, "status", resp.StatusCode, "attempt", attempt+1)
	}
	return lastErr
}

// SendJSON performs an authenticated JSON request and decodes an optional JSON response.
func (c *OAuthClient) SendJSON(ctx context.Context, method, url string, payload any, result any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	resp, err := c.Do(ctx, method, url, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// No retry on writes: a retried POST could duplicate a created event.
		// Idempotency is handled by callers supplying a deterministic event id.
		return &APIError{Method: method, URL: url, Status: resp.StatusCode, Body: string(respBody)}
	}
	if result == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, result)
}
