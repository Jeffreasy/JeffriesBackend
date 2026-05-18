package todoist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
)

const baseURL = "https://api.todoist.com/api/v1/"

// Client wraps the Todoist REST API.
type Client struct {
	token     string
	projectID string
}

// NewClient creates a new Todoist API client.
func NewClient(token, projectID string) *Client {
	return &Client{token: token, projectID: projectID}
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

	for _, d := range aankomend {
		hash := makeHash(d)
		payload := c.buildPayload(d)
		existing, exists := taskByEID[d.EventID]

		if exists {
			existingHash := extractHash(existing.Description)
			if existingHash != hash {
				_ = c.doRequest(ctx, "POST", "tasks/"+existing.ID, payload)
				result.Updated++
			}
			delete(taskByEID, d.EventID)
		} else {
			_ = c.doRequest(ctx, "POST", "tasks", payload)
			result.Created++
		}
	}

	// Close expired tasks
	for _, t := range taskByEID {
		if t.Due == nil {
			continue
		}
		dueStr := t.Due.DateTime
		if dueStr == "" {
			dueStr = t.Due.Date
		}
		if dueStr == "" || dueStr[:10] >= today {
			continue
		}
		_ = c.doRequest(ctx, "POST", "tasks/"+t.ID+"/close", nil)
		result.Closed++
	}

	slog.Info("✅ todoist sync done", "created", result.Created, "updated", result.Updated, "closed", result.Closed)
	return result, nil
}

func (c *Client) fetchAllTasks(ctx context.Context) ([]Task, error) {
	var all []Task
	cursor := ""

	for {
		endpoint := "tasks"
		if cursor != "" {
			endpoint += "?cursor=" + cursor
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

	resp, err := http.DefaultClient.Do(req)
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

func (c *Client) buildPayload(d Dienst) map[string]any {
	team := getTeam(d.Locatie)
	title := fmt.Sprintf("Dienst (%s)", d.Titel)
	if team != "?" {
		title = fmt.Sprintf("%s %s", team, d.ShiftType)
	}

	durationMin := int(d.Duur * 60)
	if durationMin < 15 {
		durationMin = 15
	}

	hash := makeHash(d)
	desc := fmt.Sprintf("Locatie: %s\nDuur: %.1f uur\nHash: %s\n\n[EID:%s]",
		d.Locatie, d.Duur, hash, d.EventID)

	payload := map[string]any{
		"content":     title,
		"description": desc,
		"labels":      []string{"Rooster"},
	}

	if c.projectID != "" {
		payload["project_id"] = c.projectID
	}

	if d.Heledag {
		payload["due_date"] = d.StartDatum
	} else {
		startTijd := d.StartTijd
		if startTijd == "" {
			startTijd = "09:00"
		}
		payload["due_datetime"] = d.StartDatum + "T" + startTijd + ":00"
		payload["duration"] = durationMin
		payload["duration_unit"] = "minute"
	}

	return payload
}

func makeHash(d Dienst) string {
	return strings.ReplaceAll(
		fmt.Sprintf("%s|%s|%s|%s", d.StartDatum, d.StartTijd, d.EindTijd, d.Locatie),
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
