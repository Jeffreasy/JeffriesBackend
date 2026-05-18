package google

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strings"
	"time"
)

// ─── Calendar API types ──────────────────────────────────────────────────────

type calendarListResponse struct {
	Items         []calendarEvent `json:"items"`
	NextPageToken string          `json:"nextPageToken"`
}

type calendarEvent struct {
	ID          string            `json:"id"`
	Summary     string            `json:"summary"`
	Description string            `json:"description"`
	Location    string            `json:"location"`
	Start       *calendarDateTime `json:"start"`
	End         *calendarDateTime `json:"end"`
}

type calendarDateTime struct {
	Date     string `json:"date"`
	DateTime string `json:"dateTime"`
}

// ScheduleDienst represents a parsed work shift ready for PostgreSQL.
type ScheduleDienst struct {
	UserID       string  `json:"user_id"`
	EventID      string  `json:"event_id"`
	Titel        string  `json:"titel"`
	StartDatum   string  `json:"start_datum"`
	StartTijd    string  `json:"start_tijd"`
	EindDatum    string  `json:"eind_datum"`
	EindTijd     string  `json:"eind_tijd"`
	Werktijd     string  `json:"werktijd"`
	Locatie      string  `json:"locatie"`
	Team         string  `json:"team"`
	ShiftType    string  `json:"shift_type"`
	Prioriteit   int     `json:"prioriteit"`
	Duur         float64 `json:"duur"`
	Weeknr       string  `json:"weeknr"`
	Dag          string  `json:"dag"`
	Status       string  `json:"status"`
	Beschrijving string  `json:"beschrijving"`
	Heledag      bool    `json:"heledag"`
}

// PersonalEventSync represents a parsed personal event for PostgreSQL.
type PersonalEventSync struct {
	UserID       string `json:"user_id"`
	EventID      string `json:"event_id"`
	Titel        string `json:"titel"`
	StartDatum   string `json:"start_datum"`
	StartTijd    string `json:"start_tijd"`
	EindDatum    string `json:"eind_datum"`
	EindTijd     string `json:"eind_tijd"`
	Heledag      bool   `json:"heledag"`
	Locatie      string `json:"locatie"`
	Beschrijving string `json:"beschrijving"`
	Status       string `json:"status"`
	Kalender     string `json:"kalender"`
}

// ─── Constants ───────────────────────────────────────────────────────────────

const (
	calendarBase    = "https://www.googleapis.com/calendar/v3"
	syncDaysBack    = 30
	syncDaysForward = 90
)

var (
	amsterdam    *time.Location
	nlDays       = []string{"Zondag", "Maandag", "Dinsdag", "Woensdag", "Donderdag", "Vrijdag", "Zaterdag"}
	keywordsIncl = []string{"dienst", "sdb", "shift"}
	keywordsExcl = []string{"vrij", "vakantie"}
)

func init() {
	var err error
	amsterdam, err = time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		amsterdam = time.UTC
	}
}

// ─── Schedule Sync (SDB Calendar) ────────────────────────────────────────────

// SyncSchedule fetches work shifts from Google Calendar and returns parsed diensten.
func SyncSchedule(ctx context.Context, client *OAuthClient, userID, calendarID string) ([]ScheduleDienst, error) {
	now := time.Now().In(amsterdam)
	timeMin := now.AddDate(0, 0, -syncDaysBack)
	timeMax := now.AddDate(0, 0, syncDaysForward)

	events, err := fetchCalendarEvents(ctx, client, calendarID, timeMin, timeMax)
	if err != nil {
		return nil, fmt.Errorf("fetch SDB calendar: %w", err)
	}

	var diensten []ScheduleDienst
	for _, ev := range events {
		titleL := strings.ToLower(ev.Summary)
		descL := strings.ToLower(ev.Description)

		match := false
		for _, kw := range keywordsIncl {
			if strings.Contains(titleL, kw) || strings.Contains(descL, kw) {
				match = true
				break
			}
		}
		if !match {
			continue
		}

		excluded := false
		for _, kw := range keywordsExcl {
			if strings.Contains(titleL, kw) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		d := parseScheduleEvent(ev, userID, now)
		if d != nil {
			diensten = append(diensten, *d)
		}
	}

	slog.Info("📅 schedule sync parsed", "events", len(events), "diensten", len(diensten))
	return diensten, nil
}

// SyncPersonalEvents fetches personal calendar events and returns them.
func SyncPersonalEvents(ctx context.Context, client *OAuthClient, userID string, calendarIDs []string, sdbCalendarID string) ([]PersonalEventSync, error) {
	now := time.Now().In(amsterdam)
	timeMin := now.AddDate(0, 0, -syncDaysBack)
	timeMax := now.AddDate(0, 0, syncDaysForward)

	if len(calendarIDs) == 0 {
		calendarIDs = []string{"primary"}
	}

	var allEvents []PersonalEventSync
	for _, calID := range calendarIDs {
		if calID == sdbCalendarID {
			continue
		}

		events, err := fetchCalendarEvents(ctx, client, calID, timeMin, timeMax)
		if err != nil {
			slog.Warn("personal calendar fetch failed", "calendarId", calID, "error", err)
			continue
		}

		kalenderName := "Main"
		if calID != "primary" {
			kalenderName = calID
		}

		for _, ev := range events {
			pe := parsePersonalEvent(ev, userID, kalenderName, calID == "primary", now)
			if pe != nil {
				allEvents = append(allEvents, *pe)
			}
		}
	}

	slog.Info("📅 personal events sync parsed", "events", len(allEvents))
	return allEvents, nil
}

// ─── Calendar API fetching ───────────────────────────────────────────────────

func fetchCalendarEvents(ctx context.Context, client *OAuthClient, calendarID string, timeMin, timeMax time.Time) ([]calendarEvent, error) {
	var allEvents []calendarEvent
	pageToken := ""

	for {
		params := url.Values{
			"timeMin":      {timeMin.Format(time.RFC3339)},
			"timeMax":      {timeMax.Format(time.RFC3339)},
			"singleEvents": {"true"},
			"orderBy":      {"startTime"},
			"maxResults":   {"250"},
		}
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		u := fmt.Sprintf("%s/calendars/%s/events?%s", calendarBase, url.PathEscape(calendarID), params.Encode())

		var resp calendarListResponse
		if err := client.GetJSON(ctx, u, &resp); err != nil {
			return nil, err
		}

		allEvents = append(allEvents, resp.Items...)
		pageToken = resp.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return allEvents, nil
}

// ─── Parsing helpers ─────────────────────────────────────────────────────────

func parseScheduleEvent(ev calendarEvent, userID string, now time.Time) *ScheduleDienst {
	if ev.Start == nil {
		return nil
	}

	isAllDay := ev.Start.Date != "" && ev.Start.DateTime == ""
	var startDt, eindDt time.Time

	if isAllDay {
		startDt, _ = time.ParseInLocation("2006-01-02", ev.Start.Date, amsterdam)
		eindDt, _ = time.ParseInLocation("2006-01-02", ev.End.Date, amsterdam)
	} else {
		startDt, _ = time.Parse(time.RFC3339, ev.Start.DateTime)
		eindDt, _ = time.Parse(time.RFC3339, ev.End.DateTime)
		startDt = startDt.In(amsterdam)
		eindDt = eindDt.In(amsterdam)
	}

	locatie := ev.Location
	shiftType := getShiftType(startDt, isAllDay)
	var duur float64
	if !isAllDay {
		duur = math.Round(eindDt.Sub(startDt).Hours()*100) / 100
	}

	eventID := ev.ID
	if eventID == "" {
		eventID = fmt.Sprintf("%s-%s", ev.Summary, startDt.Format("2006-01-02"))
	}

	status := "Opkomend"
	if eindDt.Before(now) {
		status = "Gedraaid"
	} else if startDt.Before(now) && eindDt.After(now) {
		status = "Bezig"
	}

	titel := ev.Summary
	if titel == "" {
		titel = "(onbekend)"
	}

	startTijd := ""
	eindTijd := ""
	werktijd := "Hele Dag"
	if !isAllDay {
		startTijd = startDt.Format("15:04")
		eindTijd = eindDt.Format("15:04")
		werktijd = fmt.Sprintf("%s - %s", startTijd, eindTijd)
	}

	return &ScheduleDienst{
		UserID:       userID,
		EventID:      eventID,
		Titel:        titel,
		StartDatum:   startDt.Format("2006-01-02"),
		StartTijd:    startTijd,
		EindDatum:    eindDt.Format("2006-01-02"),
		EindTijd:     eindTijd,
		Werktijd:     werktijd,
		Locatie:      locatie,
		Team:         getTeam(locatie),
		ShiftType:    shiftType,
		Prioriteit:   getPrioriteit(shiftType),
		Duur:         duur,
		Weeknr:       weeknr(startDt),
		Dag:          nlDays[startDt.Weekday()],
		Status:       status,
		Beschrijving: ev.Description,
		Heledag:      isAllDay,
	}
}

func parsePersonalEvent(ev calendarEvent, userID, kalenderName string, isPrimary bool, now time.Time) *PersonalEventSync {
	if ev.Start == nil {
		return nil
	}

	isAllDay := ev.Start.Date != "" && ev.Start.DateTime == ""
	var startDt, eindDt time.Time

	if isAllDay {
		startDt, _ = time.ParseInLocation("2006-01-02", ev.Start.Date, amsterdam)
		eindDt, _ = time.ParseInLocation("2006-01-02", ev.End.Date, amsterdam)
	} else {
		startDt, _ = time.Parse(time.RFC3339, ev.Start.DateTime)
		eindDt, _ = time.Parse(time.RFC3339, ev.End.DateTime)
		startDt = startDt.In(amsterdam)
		eindDt = eindDt.In(amsterdam)
	}

	eventID := ev.ID
	if eventID == "" {
		eventID = fmt.Sprintf("%s-%s", ev.Summary, startDt.Format("2006-01-02"))
	}
	if !isPrimary {
		eventID = kalenderName + ":" + eventID
	}

	status := "Aankomend"
	if eindDt.Before(now) {
		status = "Voorbij"
	}

	titel := ev.Summary
	if titel == "" {
		titel = "(Geen titel)"
	}

	startTijd := ""
	eindTijd := ""
	if !isAllDay {
		startTijd = startDt.Format("15:04")
		eindTijd = eindDt.Format("15:04")
	}

	return &PersonalEventSync{
		UserID:       userID,
		EventID:      eventID,
		Titel:        titel,
		StartDatum:   startDt.Format("2006-01-02"),
		StartTijd:    startTijd,
		EindDatum:    eindDt.Format("2006-01-02"),
		EindTijd:     eindTijd,
		Heledag:      isAllDay,
		Locatie:      ev.Location,
		Beschrijving: ev.Description,
		Status:       status,
		Kalender:     kalenderName,
	}
}

func getShiftType(start time.Time, isAllDay bool) string {
	if isAllDay {
		return "Dienst"
	}
	h := start.Hour()
	if h < 10 {
		return "Vroeg"
	}
	if h >= 13 {
		return "Laat"
	}
	return "Dienst"
}

func getPrioriteit(shiftType string) int {
	switch shiftType {
	case "Vroeg":
		return 4
	case "Laat":
		return 2
	default:
		return 1
	}
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

func weeknr(d time.Time) string {
	_, week := d.ISOWeek()
	return fmt.Sprintf("%d-W%02d", d.Year(), week)
}
