package todoist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const baseURL = "https://api.todoist.com/api/v1/"

// Client wraps the Todoist REST API.
type Client struct {
	token      string
	projectID  string
	httpClient *http.Client
}


// NewClient creates a new Todoist API client.
func NewClient(token, projectID string) *Client {
	return &Client{
		token:      token,
		projectID:  projectID,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}


// Task represents a Todoist task.
type Task struct {
	ID          string `json:"id"`
	Content     string `json:"content"`
	Description string `json:"description"`
	Due         *struct {
		DateTime string `json:"datetime"`
		Date     string `json:"date"`
	} `json:"due"`
}

type tasksResponse struct {
	Results    []Task `json:"results"`
	NextCursor string `json:"next_cursor"`
}

// Dienst is the minimal shift info needed for Todoist sync.
type Dienst struct {
	EventID    string  `json:"event_id"`
	Titel      string  `json:"titel"`
	StartDatum string  `json:"start_datum"`
	StartTijd  string  `json:"start_tijd"`
	EindTijd   string  `json:"eind_tijd"`
	Locatie    string  `json:"locatie"`
	ShiftType  string  `json:"shift_type"`
	Duur       float64 `json:"duur"`
	Heledag    bool    `json:"heledag"`
	Status     string  `json:"status"`
}

// SyncResult holds sync operation counts.
type SyncResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Closed  int `json:"closed"`
	Deleted int `json:"deleted"`
	Failed  int `json:"failed"`
}

// syncCommand is one Todoist Sync API command (item_add/item_update/item_close).
type syncCommand struct {
	Type   string         `json:"type"`
	TempID string         `json:"temp_id,omitempty"`
	UUID   string         `json:"uuid"`
	Args   map[string]any `json:"args"`
}

type syncResponse struct {
	SyncStatus map[string]json.RawMessage `json:"sync_status"`
}

var eidRegex = regexp.MustCompile(`\[EID:(.*?)\]`)

// SyncDiensten syncs upcoming shifts to Todoist.
func (c *Client) SyncDiensten(ctx context.Context, diensten []Dienst) (*SyncResult, error) {
	if c.token == "" {
		return nil, fmt.Errorf("TODOIST_API_TOKEN not configured")
	}

	today := strings.Split(fmt.Sprintf("%v", ctx.Value("today")), " ")[0]
	if today == "<nil>" || today == "" {
		today = "2026-01-01" // fallback
	}

	// Filter upcoming
	var aankomend []Dienst
	for _, d := range diensten {
		if d.Status != "VERWIJDERD" && d.StartDatum >= today {
			aankomend = append(aankomend, d)
		}
	}

	// Fetch existing Todoist tasks
	allTasks, err := c.fetchAllTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch tasks: %w", err)
	}

	taskByEID := make(map[string]Task)
	for _, t := range allTasks {
		match := eidRegex.FindStringSubmatch(t.Description)
		if len(match) < 2 {
			continue
		}
		eid := match[1]
		if _, exists := taskByEID[eid]; exists {
			// Duplicate — delete
			_ = c.doRequest(ctx, "DELETE", "tasks/"+t.ID, nil)
			continue
		}
		taskByEID[eid] = t
	}

	result := &SyncResult{}
	var commands []syncCommand

	for _, d := range aankomend {
		hash := makeHash(d)
		existing, exists := taskByEID[d.EventID]
		if exists {
			if extractHash(existing.Description) != hash {
				commands = append(commands, c.itemUpdate(existing.ID, d))
				result.Updated++
			}
			delete(taskByEID, d.EventID)
		} else {
			commands = append(commands, c.itemAdd(d))
			result.Created++
		}
	}

	// Tasks still mapped here matched no upcoming shift — either the shift already
	// happened (past → mark complete) or it was deleted/cancelled in Google
	// (future → remove the task so a cancelled shift doesn't linger as a reminder).
	for _, t := range taskByEID {
		if t.Due == nil {
			continue
		}
		dueStr := t.Due.DateTime
		if dueStr == "" {
			dueStr = t.Due.Date
		}
		if dueStr == "" {
			continue
		}
		if dueStr[:10] < today {
			commands = append(commands, itemClose(t.ID))
			result.Closed++
		} else {
			commands = append(commands, itemDelete(t.ID))
			result.Deleted++
		}
	}

	if len(commands) == 0 {
		return result, nil
	}

	// Batch every change through the Sync API (up to 100 commands per request)
	// instead of one REST call per task.
	failed, err := c.runSyncBatch(ctx, commands)
	if err != nil {
		return result, err
	}
	result.Failed = failed
	slog.Info("✅ todoist sync done", "created", result.Created, "updated", result.Updated, "closed", result.Closed, "deleted", result.Deleted, "failed", failed)
	return result, nil
}

// taskArgs builds the Sync API args for a shift task (content/description/due/
// duration/labels), shared by item_add and item_update.
func (c *Client) taskArgs(d Dienst) map[string]any {
	team := getTeam(d.Locatie)
	title := fmt.Sprintf("Dienst (%s)", d.Titel)
	if team != "?" {
		title = fmt.Sprintf("%s %s", team, d.ShiftType)
	}
	desc := fmt.Sprintf("Locatie: %s\nDuur: %.1f uur\nHash: %s\n\n[EID:%s]",
		d.Locatie, d.Duur, makeHash(d), d.EventID)

	args := map[string]any{
		"content":     title,
		"description": desc,
		"labels":      []string{"Rooster"},
	}
	// The Sync API's due object uses the `date` field for BOTH a date and a
	// datetime (a floating local time when no timezone is given) — `datetime` is
	// ignored here, verified by a live round-trip test. Getting this wrong sets
	// due=null and silently strips the shift's date.
	if d.Heledag {
		args["due"] = map[string]any{"date": d.StartDatum}
	} else {
		startTijd := d.StartTijd
		if startTijd == "" {
			startTijd = "09:00"
		}
		args["due"] = map[string]any{"date": d.StartDatum + "T" + startTijd + ":00"}
		durationMin := int(d.Duur * 60)
		if durationMin < 15 {
			durationMin = 15
		}
		args["duration"] = map[string]any{"amount": durationMin, "unit": "minute"}
	}
	return args
}

func (c *Client) itemAdd(d Dienst) syncCommand {
	args := c.taskArgs(d)
	if c.projectID != "" {
		args["project_id"] = c.projectID
	}
	return syncCommand{Type: "item_add", TempID: uuid.New().String(), UUID: uuid.New().String(), Args: args}
}

func (c *Client) itemUpdate(taskID string, d Dienst) syncCommand {
	args := c.taskArgs(d) // no project_id — item_update would move the task
	args["id"] = taskID
	return syncCommand{Type: "item_update", UUID: uuid.New().String(), Args: args}
}

func itemClose(taskID string) syncCommand {
	return syncCommand{Type: "item_close", UUID: uuid.New().String(), Args: map[string]any{"id": taskID}}
}

func itemDelete(taskID string) syncCommand {
	return syncCommand{Type: "item_delete", UUID: uuid.New().String(), Args: map[string]any{"id": taskID}}
}

// runSyncBatch posts the commands to POST /sync in chunks of 100, returning how
// many commands failed (per-command sync_status is parsed and logged).
func (c *Client) runSyncBatch(ctx context.Context, commands []syncCommand) (failed int, err error) {
	const maxPerBatch = 100
	for start := 0; start < len(commands); start += maxPerBatch {
		end := start + maxPerBatch
		if end > len(commands) {
			end = len(commands)
		}
		cmdJSON, mErr := json.Marshal(commands[start:end])
		if mErr != nil {
			return failed, mErr
		}
		form := url.Values{}
		form.Set("commands", string(cmdJSON))

		body, rErr := c.doForm(ctx, "sync", form)
		if rErr != nil {
			return failed, rErr
		}
		var sr syncResponse
		if uErr := json.Unmarshal(body, &sr); uErr != nil {
			return failed, fmt.Errorf("parse sync response: %w", uErr)
		}
		for cmdUUID, status := range sr.SyncStatus {
			var ok string
			if json.Unmarshal(status, &ok) == nil && ok == "ok" {
				continue
			}
			failed++
			slog.Warn("todoist sync command failed", "uuid", cmdUUID, "status", string(status))
		}
	}
	return failed, nil
}

func (c *Client) fetchAllTasks(ctx context.Context) ([]Task, error) {
	var all []Task
	cursor := ""

	for {
		// Scope to our own "Rooster" label so the sync only ever reads/closes the
		// shift tasks it manages — never the user's other Todoist tasks.
		endpoint := "tasks?label=Rooster"
		if cursor != "" {
			endpoint += "&cursor=" + url.QueryEscape(cursor)
		}

		body, err := c.doRequestRaw(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, err
		}

		var resp tasksResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parse tasks: %w", err)
		}

		all = append(all, resp.Results...)
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}

	return all, nil
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, payload map[string]any) error {
	_, err := c.doRequestRaw(ctx, method, endpoint, payload)
	return err
}

func (c *Client) doRequestRaw(ctx context.Context, method, endpoint string, payload map[string]any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, _ := json.Marshal(payload)
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if method == "DELETE" || resp.StatusCode == 204 {
		return nil, nil
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("todoist %s %s: HTTP %d — %s", method, endpoint, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// doForm posts an application/x-www-form-urlencoded body (the Sync API), with a
// small Retry-After-aware backoff on 429 so a burst of commands doesn't fail.
func (c *Client) doForm(ctx context.Context, endpoint string, form url.Values) ([]byte, error) {
	const maxAttempts = 3
	for attempt := 1; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxAttempts {
			wait := todoistRetryAfter(resp.Header.Get("Retry-After"), time.Duration(attempt)*2*time.Second)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("todoist POST %s: HTTP %d — %s", endpoint, resp.StatusCode, string(respBody))
		}
		return respBody, nil
	}
}

// todoistRetryAfter parses a Retry-After header (seconds), capped, else the fallback.
func todoistRetryAfter(header string, fallback time.Duration) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && secs > 0 {
		if d := time.Duration(secs) * time.Second; d < 60*time.Second {
			return d
		}
		return 60 * time.Second
	}
	return fallback
}

func makeHash(d Dienst) string {
	// Include title, shift type and duration so an edit to any of them changes the
	// hash and re-syncs the existing Todoist task (they were previously ignored).
	return strings.ReplaceAll(
		fmt.Sprintf("%s|%s|%s|%s|%s|%s|%.2f", d.StartDatum, d.StartTijd, d.EindTijd, d.Locatie, d.Titel, d.ShiftType, d.Duur),
		" ", "",
	)
}

func extractHash(desc string) string {
	for _, line := range strings.Split(desc, "\n") {
		if strings.HasPrefix(line, "Hash: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Hash: "))
		}
	}
	return ""
}

func getTeam(locatie string) string {
	l := strings.ToLower(locatie)
	if strings.Contains(l, "appartementen") {
		return "R."
	}
	if strings.Contains(l, "aa") {
		return "A."
	}
	return "?"
}
