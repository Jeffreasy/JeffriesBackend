package convex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// Client communicates with the Convex Site (HTTP) API.
type Client struct {
	baseURL    string
	secret     string
	userID     string
	httpClient *http.Client
}

// NewClient creates a new Convex HTTP API client.
func NewClient(siteURL, secret, userID string) *Client {
	return &Client{
		baseURL: siteURL,
		secret:  secret,
		userID:  userID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ─── Device Operations ───────────────────────────────────────────────────────

// FetchDevices returns all devices for the configured user.
func (c *Client) FetchDevices(ctx context.Context) ([]map[string]any, error) {
	params := url.Values{"userId": {c.userID}}
	var resp struct {
		Devices []map[string]any `json:"devices"`
	}
	if err := c.get(ctx, "/devices", params, &resp); err != nil {
		return nil, err
	}
	return resp.Devices, nil
}

// FetchDevice returns a single device by ID.
func (c *Client) FetchDevice(ctx context.Context, deviceID string) (map[string]any, error) {
	var resp struct {
		Device map[string]any `json:"device"`
	}
	if err := c.get(ctx, "/devices/"+deviceID, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Device, nil
}

// CreateDevice registers a new device in Convex.
func (c *Client) CreateDevice(ctx context.Context, payload map[string]any) (map[string]any, int, error) {
	var resp struct {
		Device map[string]any `json:"device"`
		Error  string         `json:"error"`
	}
	statusCode, err := c.post(ctx, "/devices/create", payload, &resp)
	if err != nil {
		return nil, statusCode, err
	}
	return resp.Device, statusCode, nil
}

// UpdateDevice patches device fields in Convex.
func (c *Client) UpdateDevice(ctx context.Context, deviceID string, payload map[string]any) (map[string]any, int, error) {
	var resp struct {
		Device map[string]any `json:"device"`
	}
	statusCode, err := c.patch(ctx, "/devices/"+deviceID, payload, &resp)
	if err != nil {
		return nil, statusCode, err
	}
	return resp.Device, statusCode, nil
}

// UpdateDeviceState patches the device currentState in Convex.
func (c *Client) UpdateDeviceState(ctx context.Context, deviceID string, statePatch map[string]any) error {
	_, err := c.patch(ctx, "/devices/"+deviceID+"/state", statePatch, nil)
	return err
}

// UpdateDeviceStatus updates device online/offline status.
func (c *Client) UpdateDeviceStatus(ctx context.Context, deviceID string, status string) error {
	_, err := c.patch(ctx, "/devices/"+deviceID+"/status", map[string]any{"status": status}, nil)
	return err
}

// DeleteDevice removes a device from Convex.
func (c *Client) DeleteDevice(ctx context.Context, deviceID string) (int, error) {
	return c.delete(ctx, "/devices/"+deviceID)
}

// ─── Automation Operations ───────────────────────────────────────────────────

// FetchAutomations returns all automations for the user.
func (c *Client) FetchAutomations(ctx context.Context) ([]map[string]any, error) {
	params := url.Values{"userId": {c.userID}}
	var resp struct {
		OK          bool             `json:"ok"`
		Automations []map[string]any `json:"automations"`
	}
	if err := c.get(ctx, "/automations", params, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("automations API returned ok=false")
	}
	return resp.Automations, nil
}

// FetchSchedule returns today's schedule.
func (c *Client) FetchSchedule(ctx context.Context, date string) ([]map[string]any, error) {
	params := url.Values{"userId": {c.userID}, "date": {date}}
	var resp struct {
		OK       bool             `json:"ok"`
		Diensten []map[string]any `json:"diensten"`
	}
	if err := c.get(ctx, "/schedule/today", params, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("schedule API returned ok=false")
	}
	return resp.Diensten, nil
}

// MarkFired marks an automation as fired.
func (c *Client) MarkFired(ctx context.Context, automationID string) error {
	_, err := c.post(ctx, "/mark-fired", map[string]any{"automationId": automationID}, nil)
	return err
}

// ─── Convex Client API (for mutations/queries) ──────────────────────────────

// Query calls a Convex function via the Convex Cloud API.
func (c *Client) Query(ctx context.Context, functionName string, args map[string]any) (json.RawMessage, error) {
	cloudURL := c.cloudURL()
	payload := map[string]any{
		"path":   functionName,
		"args":   args,
		"format": "json",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cloudURL+"/api/query", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Status string          `json:"status"`
		Value  json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result.Value, nil
}

// Mutation calls a Convex mutation via the Cloud API.
func (c *Client) Mutation(ctx context.Context, functionName string, args map[string]any) error {
	cloudURL := c.cloudURL()
	payload := map[string]any{
		"path":   functionName,
		"args":   args,
		"format": "json",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cloudURL+"/api/mutation", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mutation %s failed: %s", functionName, string(data))
	}
	return nil
}

// Action calls a Convex action via the Cloud API.
func (c *Client) Action(ctx context.Context, functionName string, args map[string]any) (json.RawMessage, error) {
	cloudURL := c.cloudURL()
	payload := map[string]any{
		"path":   functionName,
		"args":   args,
		"format": "json",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cloudURL+"/api/action", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Status string          `json:"status"`
		Value  json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result.Value, nil
}

// ─── HTTP helpers ────────────────────────────────────────────────────────────

func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) post(ctx context.Context, path string, payload any, out any) (int, error) {
	return c.doJSON(ctx, http.MethodPost, path, payload, out)
}

func (c *Client) patch(ctx context.Context, path string, payload any, out any) (int, error) {
	return c.doJSON(ctx, http.MethodPatch, path, payload, out)
}

func (c *Client) delete(ctx context.Context, path string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return 0, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return resp.StatusCode, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload any, out any) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, &HTTPError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if out == nil {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.secret)
}

func (c *Client) cloudURL() string {
	// .site → .cloud
	u := c.baseURL
	if len(u) > 5 {
		u = u[:len(u)-5] + ".cloud"
	}
	return u
}

// HTTPError represents a non-2xx response from Convex.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("convex: HTTP %d: %s", e.StatusCode, e.Body)
}

// StatusCodeFromError extracts status code from HTTPError, or 500 as default.
func StatusCodeFromError(err error) int {
	if he, ok := err.(*HTTPError); ok {
		return he.StatusCode
	}
	slog.Warn("non-HTTP error from convex", "error", err)
	return 500
}
