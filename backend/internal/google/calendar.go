package google

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
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
	Date     string `json:"date,omitempty"`
	DateTime string `json:"dateTime,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type calendarEventWrite struct {
	ID          string           `json:"id,omitempty"`
	Summary     string           `json:"summary"`
	Description string           `json:"description,omitempty"`
	Location    string           `json:"location,omitempty"`
	Start       calendarDateTime `json:"start"`
	End         calendarDateTime `json:"end"`
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

// ScheduleSyncResult carries parsed shifts plus the raw Google event IDs
// fetched in the active sync window. The raw IDs are used to remove stale
// schedule rows when events are deleted from Google Calendar.
type ScheduleSyncResult struct {
	Diensten        []ScheduleDienst `json:"diensten"`
	FetchedEventIDs []string         `json:"fetched_event_ids"`
	PruneStartDatum string           `json:"prune_start_datum"`
	PruneEindDatum  string           `json:"prune_eind_datum"`
}

// PersonalEventSync represents a parsed personal event for PostgreSQL.
type PersonalEventSync struct {
	UserID               string `json:"user_id"`
	EventID              string `json:"event_id"`
	Titel                string `json:"titel"`
	StartDatum           string `json:"start_datum"`
	StartTijd            string `json:"start_tijd"`
	EindDatum            string `json:"eind_datum"`
	EindTijd             string `json:"eind_tijd"`
	Heledag              bool   `json:"heledag"`
	Locatie              string `json:"locatie"`
	Beschrijving         string `json:"beschrijving"`
	Symbol               string `json:"symbol"`
	BusinessContextType  string `json:"business_context_type"`
	BusinessContextID    string `json:"business_context_id"`
	BusinessContextTitle string `json:"business_context_title"`
	Status               string `json:"status"`
	Kalender             string `json:"kalender"`
}

// PersonalEventsSyncResult carries synced personal events plus the source
// calendar scope used to mark locally stale Google Calendar rows as deleted.
type PersonalEventsSyncResult struct {
	Events          []PersonalEventSync `json:"events"`
	FetchedEventIDs []string            `json:"fetched_event_ids"`
	SyncedKalenders []string            `json:"synced_kalenders"`
	PruneStartDatum string              `json:"prune_start_datum"`
	PruneEindDatum  string              `json:"prune_eind_datum"`
}

// ─── Constants ───────────────────────────────────────────────────────────────

const (
	calendarBase    = "https://www.googleapis.com/calendar/v3"
	syncDaysBack    = 30
	syncDaysForward = 90
)

var (
	amsterdam                   *time.Location
	nlDays                      = []string{"Zondag", "Maandag", "Dinsdag", "Woensdag", "Donderdag", "Vrijdag", "Zaterdag"}
	keywordsIncl                = []string{"dienst", "sdb", "shift"}
	keywordsExcl                = []string{"vrij", "vakantie"}
	symbolMetadataPattern       = regexp.MustCompile(`(?i)\[symbol:([a-z0-9_-]+)\]`)
	contextMetadataPattern      = regexp.MustCompile(`(?i)\[context:([^\]]+)\]`)
	businessContextTypePattern  = regexp.MustCompile(`(?i)\[(?:businessContextType|business_context_type):([^\]]+)\]`)
	businessContextIDPattern    = regexp.MustCompile(`(?i)\[(?:businessContextId|business_context_id):([^\]]+)\]`)
	businessContextTitlePattern = regexp.MustCompile(`(?i)\[(?:businessContextTitle|business_context_title):([^\]]+)\]`)
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
	result, err := SyncScheduleDetailed(ctx, client, userID, calendarID)
	if err != nil {
		return nil, err
	}
	return result.Diensten, nil
}

// SyncScheduleDetailed fetches work shifts and the raw Google event IDs needed
// to reconcile deleted Google Calendar events from the local schedule table.
func SyncScheduleDetailed(ctx context.Context, client *OAuthClient, userID, calendarID string) (*ScheduleSyncResult, error) {
	now := time.Now().In(amsterdam)
	timeMin := now.AddDate(0, 0, -syncDaysBack)
	timeMax := now.AddDate(0, 0, syncDaysForward)

	events, err := fetchCalendarEvents(ctx, client, calendarID, timeMin, timeMax)
	if err != nil {
		return nil, fmt.Errorf("fetch SDB calendar: %w", err)
	}

	var diensten []ScheduleDienst
	fetchedEventIDs := make([]string, 0, len(events))
	for _, ev := range events {
		if eventID := calendarEventStableID(ev); eventID != "" {
			fetchedEventIDs = append(fetchedEventIDs, eventID)
		}

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
	return &ScheduleSyncResult{
		Diensten:        diensten,
		FetchedEventIDs: fetchedEventIDs,
		// Prune across the full fetched window (timeMin..timeMax), not just from
		// today, so a recently-past shift deleted in Google is reconciled too.
		PruneStartDatum: timeMin.In(amsterdam).Format("2006-01-02"),
		PruneEindDatum:  timeMax.In(amsterdam).Format("2006-01-02"),
	}, nil
}

// SyncPersonalEvents fetches personal calendar events and returns them.
func SyncPersonalEvents(ctx context.Context, client *OAuthClient, userID string, calendarIDs []string, sdbCalendarID string) ([]PersonalEventSync, error) {
	result, err := SyncPersonalEventsDetailed(ctx, client, userID, calendarIDs, sdbCalendarID)
	if err != nil {
		return nil, err
	}
	return result.Events, nil
}

// SyncPersonalEventsDetailed fetches personal calendar events and the event IDs
// needed to reconcile local rows that were deleted remotely.
func SyncPersonalEventsDetailed(ctx context.Context, client *OAuthClient, userID string, calendarIDs []string, sdbCalendarID string) (*PersonalEventsSyncResult, error) {
	now := time.Now().In(amsterdam)
	timeMin := now.AddDate(0, 0, -syncDaysBack)
	timeMax := now.AddDate(0, 0, syncDaysForward)

	if len(calendarIDs) == 0 {
		calendarIDs = []string{"primary"}
	}

	var allEvents []PersonalEventSync
	fetchedEventIDs := []string{}
	syncedKalenders := []string{}
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
		syncedKalenders = append(syncedKalenders, kalenderName)

		for _, ev := range events {
			pe := parsePersonalEvent(ev, userID, kalenderName, calID == "primary", now)
			if pe != nil {
				fetchedEventIDs = append(fetchedEventIDs, pe.EventID)
				allEvents = append(allEvents, *pe)
			}
		}
	}

	slog.Info("📅 personal events sync parsed", "events", len(allEvents))
	return &PersonalEventsSyncResult{
		Events:          allEvents,
		FetchedEventIDs: fetchedEventIDs,
		SyncedKalenders: syncedKalenders,
		// Prune across the full fetched window so recently-past events deleted in
		// Google are reconciled, matching the schedule sync behaviour.
		PruneStartDatum: timeMin.In(amsterdam).Format("2006-01-02"),
		PruneEindDatum:  timeMax.In(amsterdam).Format("2006-01-02"),
	}, nil
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

// CreatePersonalEvent creates a Google Calendar event and returns the Google event id.
//
// It supplies a deterministic, client-side event id derived from the local
// pending id, which makes the insert idempotent: if a previous attempt created
// the event in Google but failed to update the local row, the retry returns HTTP
// 409 (conflict) instead of creating a duplicate, and we treat that as success.
func CreatePersonalEvent(ctx context.Context, client *OAuthClient, calendarID string, event model.PersonalEvent) (string, error) {
	payload, err := personalEventPayload(event)
	if err != nil {
		return "", err
	}
	desiredID := deterministicEventID(event.EventID)
	payload.ID = desiredID

	u := fmt.Sprintf("%s/calendars/%s/events", calendarBase, url.PathEscape(calendarID))
	var created calendarEvent
	if err := client.SendJSON(ctx, "POST", u, payload, &created); err != nil {
		if StatusCode(err) == http.StatusConflict {
			// Event already exists from a prior attempt — our id is authoritative.
			return desiredID, nil
		}
		return "", err
	}
	if created.ID == "" {
		return desiredID, nil
	}
	return created.ID, nil
}

// deterministicEventID maps a local pending event id to a stable Google Calendar
// event id. Google requires base32hex (lowercase a-v + 0-9, length 5–1024); a
// hex SHA-256 is a valid subset, so the same local id always yields the same
// remote id, giving idempotent inserts. Falls back to empty (Google generates an
// id) when there is no local id to key on.
func deterministicEventID(localID string) string {
	if localID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(localID))
	return hex.EncodeToString(sum[:])
}

// UpdatePersonalEvent patches a Google Calendar event in place.
func UpdatePersonalEvent(ctx context.Context, client *OAuthClient, calendarID, eventID string, event model.PersonalEvent) error {
	if eventID == "" {
		return fmt.Errorf("google event id required")
	}
	payload, err := personalEventPayload(event)
	if err != nil {
		return err
	}

	u := fmt.Sprintf("%s/calendars/%s/events/%s", calendarBase, url.PathEscape(calendarID), url.PathEscape(eventID))
	err = client.SendJSON(ctx, "PATCH", u, payload, nil)
	if err != nil && isInvalidStartTimeError(err) {
		return client.SendJSON(ctx, "PUT", u, payload, nil)
	}
	return err
}

// DeletePersonalEvent removes a Google Calendar event. Missing remote events are treated as already deleted.
func DeletePersonalEvent(ctx context.Context, client *OAuthClient, calendarID, eventID string) error {
	if eventID == "" {
		return nil
	}

	u := fmt.Sprintf("%s/calendars/%s/events/%s", calendarBase, url.PathEscape(calendarID), url.PathEscape(eventID))
	resp, err := client.Do(ctx, "DELETE", u, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 || resp.StatusCode == 410 {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE %s: HTTP %d — %s", u, resp.StatusCode, string(body))
	}
	return nil
}

// ─── Calendar target resolution ──────────────────────────────────────────────
//
// These helpers are the single source of truth for translating a stored
// PersonalEvent (its Kalender column + namespaced EventID) into the (calendarID,
// googleEventID) pair used against the Google Calendar API. Both the engine
// crons, the HTTP /sync handler and the Telegram sync path call them so the
// alias rules can never drift between paths.

// NormalizeCalendarID maps stored calendar aliases to a real Google calendar id.
// "" and "Main" are the user's primary calendar. "AI" is a synthetic marker the
// assistant historically wrote into the kalender column when staging an
// AI-created appointment — it is NOT a real calendar id, so it must also resolve
// to "primary" (otherwise CreatePersonalEvent POSTs to /calendars/AI/events and
// Google returns a permanent 404).
func NormalizeCalendarID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" || strings.EqualFold(id, "Main") || strings.EqualFold(id, "AI") {
		return "primary"
	}
	return id
}

// ResolveCalendarTarget returns the Google calendar id and the bare Google event
// id for a stored personal event, stripping the "calendarName:" namespace prefix
// that non-primary calendars carry.
func ResolveCalendarTarget(event model.PersonalEvent) (calendarID, googleEventID string) {
	calendarID = NormalizeCalendarID(event.Kalender)
	googleEventID = event.EventID
	if calendarID != "primary" {
		googleEventID = strings.TrimPrefix(googleEventID, calendarID+":")
	}
	return calendarID, googleEventID
}

// StoredCalendarEventID namespaces a freshly created Google event id with its
// calendar so non-primary events stay addressable on later edits/deletes.
func StoredCalendarEventID(calendarID, googleEventID string) string {
	if calendarID == "" || calendarID == "primary" {
		return googleEventID
	}
	return calendarID + ":" + googleEventID
}

// SplitCalendarIDs parses a comma-separated calendar id list, defaulting to the
// primary calendar when empty.
func SplitCalendarIDs(raw string) []string {
	parts := strings.Split(raw, ",")
	calendarIDs := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			calendarIDs = append(calendarIDs, part)
		}
	}
	if len(calendarIDs) == 0 {
		return []string{"primary"}
	}
	return calendarIDs
}

// ─── Parsing helpers ─────────────────────────────────────────────────────────

func personalEventPayload(event model.PersonalEvent) (calendarEventWrite, error) {
	payload := calendarEventWrite{
		Summary:  event.Titel,
		Location: ptrValue(event.Locatie),
		Description: descriptionWithPersonalEventMetadata(
			ptrValue(event.Beschrijving),
			event.Symbol,
			event.BusinessContextType,
			event.BusinessContextID,
			event.BusinessContextTitle,
		),
	}

	if event.Heledag {
		start := event.StartDatum
		end := event.EindDatum
		if end == "" || end < start {
			end = start
		}
		exclusiveEnd, err := addDaysISO(end, 1)
		if err != nil {
			return payload, err
		}
		payload.Start = calendarDateTime{Date: start}
		payload.End = calendarDateTime{Date: exclusiveEnd}
		return payload, nil
	}

	startTime := ptrValue(event.StartTijd)
	endTime := ptrValue(event.EindTijd)
	if startTime == "" {
		startTime = "09:00"
	}
	if endTime == "" {
		endTime = startTime
	}

	start, err := localRFC3339(event.StartDatum, startTime)
	if err != nil {
		return payload, err
	}
	endDate := event.EindDatum
	if endDate == "" {
		endDate = event.StartDatum
	}
	end, err := localRFC3339(endDate, endTime)
	if err != nil {
		return payload, err
	}
	payload.Start = calendarDateTime{DateTime: start, TimeZone: "Europe/Amsterdam"}
	payload.End = calendarDateTime{DateTime: end, TimeZone: "Europe/Amsterdam"}
	return payload, nil
}

func ptrValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func descriptionWithPersonalEventMetadata(description string, symbol, contextType, contextID, contextTitle *string) string {
	cleaned := strings.TrimSpace(symbolMetadataPattern.ReplaceAllString(description, ""))
	cleaned = strings.TrimSpace(contextMetadataPattern.ReplaceAllString(cleaned, ""))
	cleaned = strings.TrimSpace(businessContextTypePattern.ReplaceAllString(cleaned, ""))
	cleaned = strings.TrimSpace(businessContextIDPattern.ReplaceAllString(cleaned, ""))
	cleaned = strings.TrimSpace(businessContextTitlePattern.ReplaceAllString(cleaned, ""))

	tokens := []string{}
	if value := cleanMetadataValue(ptrValue(symbol)); value != "" {
		tokens = append(tokens, "[symbol:"+value+"]")
	}

	contextValue := cleanMetadataValue(ptrValue(contextType))
	if contextValue != "" {
		if strings.HasPrefix(strings.ToLower(contextValue), "laventecare") {
			tokens = append(tokens, "[context:laventecare]")
		}
		tokens = append(tokens, "[businessContextType:"+contextValue+"]")
		if value := cleanMetadataValue(ptrValue(contextID)); value != "" {
			tokens = append(tokens, "[businessContextId:"+value+"]")
		}
		title := cleanMetadataTitle(ptrValue(contextTitle))
		if title == "" && strings.EqualFold(contextValue, "laventecare") {
			title = "LaventeCare"
		}
		if title != "" {
			tokens = append(tokens, "[businessContextTitle:"+title+"]")
		}
	}

	if len(tokens) == 0 {
		return cleaned
	}
	if cleaned == "" {
		return strings.Join(tokens, " ")
	}
	return cleaned + " " + strings.Join(tokens, " ")
}

func symbolFromDescription(description string) string {
	match := symbolMetadataPattern.FindStringSubmatch(description)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func businessContextFromDescription(description string) (string, string, string) {
	contextType := metadataMatch(businessContextTypePattern, description)
	if contextType == "" {
		context := metadataMatch(contextMetadataPattern, description)
		if strings.EqualFold(context, "laventecare") {
			contextType = "laventecare"
		}
	}
	contextID := metadataMatch(businessContextIDPattern, description)
	contextTitle := metadataMatch(businessContextTitlePattern, description)
	if contextTitle == "" && strings.EqualFold(contextType, "laventecare") {
		contextTitle = "LaventeCare"
	}
	return contextType, contextID, contextTitle
}

func metadataMatch(pattern *regexp.Regexp, description string) string {
	match := pattern.FindStringSubmatch(description)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func cleanMetadataValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "]", ")")
	value = strings.ReplaceAll(value, "[", "(")
	return value
}

func cleanMetadataTitle(value string) string {
	value = cleanMetadataValue(value)
	if len(value) > 120 {
		value = strings.TrimSpace(value[:120])
	}
	return value
}

func localRFC3339(date, clock string) (string, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, amsterdam)
	if err != nil {
		return "", fmt.Errorf("parse calendar time %s %s: %w", date, clock, err)
	}
	return t.Format(time.RFC3339), nil
}

func addDaysISO(date string, days int) (string, error) {
	t, err := time.ParseInLocation("2006-01-02", date, amsterdam)
	if err != nil {
		return "", fmt.Errorf("parse calendar date %s: %w", date, err)
	}
	return t.AddDate(0, 0, days).Format("2006-01-02"), nil
}

func isInvalidStartTimeError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "invalid start time")
}

func inclusiveAllDayEnd(startDt, googleEndDt time.Time) time.Time {
	endDt := googleEndDt.AddDate(0, 0, -1)
	if endDt.Before(startDt) {
		return startDt
	}
	return endDt
}

func calendarEventEndInstant(endDt time.Time, isAllDay bool) time.Time {
	if isAllDay {
		return endDt.AddDate(0, 0, 1)
	}
	return endDt
}

func calendarEventStableID(ev calendarEvent) string {
	if ev.ID != "" {
		return ev.ID
	}
	if ev.Start == nil {
		return strings.TrimSpace(ev.Summary)
	}

	startDate := ev.Start.Date
	if startDate == "" && ev.Start.DateTime != "" {
		if startDt, err := time.Parse(time.RFC3339, ev.Start.DateTime); err == nil {
			startDate = startDt.In(amsterdam).Format("2006-01-02")
		}
	}
	if startDate == "" {
		return strings.TrimSpace(ev.Summary)
	}
	return fmt.Sprintf("%s-%s", ev.Summary, startDate)
}

func parseScheduleEvent(ev calendarEvent, userID string, now time.Time) *ScheduleDienst {
	if ev.Start == nil || ev.End == nil {
		return nil
	}

	isAllDay := ev.Start.Date != "" && ev.Start.DateTime == ""
	var startDt, eindDt time.Time
	var startErr, endErr error

	if isAllDay {
		startDt, startErr = time.ParseInLocation("2006-01-02", ev.Start.Date, amsterdam)
		var googleEndDt time.Time
		googleEndDt, endErr = time.ParseInLocation("2006-01-02", ev.End.Date, amsterdam)
		if startErr == nil && endErr == nil {
			eindDt = inclusiveAllDayEnd(startDt, googleEndDt)
		}
	} else {
		startDt, startErr = time.Parse(time.RFC3339, ev.Start.DateTime)
		eindDt, endErr = time.Parse(time.RFC3339, ev.End.DateTime)
		startDt = startDt.In(amsterdam)
		eindDt = eindDt.In(amsterdam)
	}
	if startErr != nil || endErr != nil {
		slog.Warn("schedule event skipped: unparseable time",
			"summary", ev.Summary, "startErr", startErr, "endErr", endErr)
		return nil
	}

	locatie := ev.Location
	shiftType := getShiftType(startDt, isAllDay)
	var duur float64
	if !isAllDay {
		duur = math.Round(eindDt.Sub(startDt).Hours()*100) / 100
	} else {
		// All-day events carry no explicit hours; assume a standard shift per day
		// spanned so one isn't counted as 0h in the contract-hours total. 7.5h is
		// the median of the owner's real shifts (Vroeg ~7.75h, Laat ~7.25h). In
		// practice all-day shifts don't occur, so this is an edge-case default.
		// (The end is inclusive, so end−start ≈ 24h per day.)
		const standardShiftHours = 7.5
		days := int(math.Round(eindDt.Sub(startDt).Hours() / 24))
		if days < 1 {
			days = 1
		}
		duur = float64(days) * standardShiftHours
	}

	eventID := calendarEventStableID(ev)

	status := "Opkomend"
	eventEnd := calendarEventEndInstant(eindDt, isAllDay)
	if eventEnd.Before(now) {
		status = "Gedraaid"
	} else if startDt.Before(now) && eventEnd.After(now) {
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
	if ev.Start == nil || ev.End == nil {
		return nil
	}

	isAllDay := ev.Start.Date != "" && ev.Start.DateTime == ""
	var startDt, eindDt time.Time
	var startErr, endErr error

	if isAllDay {
		startDt, startErr = time.ParseInLocation("2006-01-02", ev.Start.Date, amsterdam)
		var googleEndDt time.Time
		googleEndDt, endErr = time.ParseInLocation("2006-01-02", ev.End.Date, amsterdam)
		if startErr == nil && endErr == nil {
			eindDt = inclusiveAllDayEnd(startDt, googleEndDt)
		}
	} else {
		startDt, startErr = time.Parse(time.RFC3339, ev.Start.DateTime)
		eindDt, endErr = time.Parse(time.RFC3339, ev.End.DateTime)
		startDt = startDt.In(amsterdam)
		eindDt = eindDt.In(amsterdam)
	}
	if startErr != nil || endErr != nil {
		slog.Warn("personal event skipped: unparseable time",
			"summary", ev.Summary, "startErr", startErr, "endErr", endErr)
		return nil
	}

	eventID := ev.ID
	if eventID == "" {
		eventID = fmt.Sprintf("%s-%s", ev.Summary, startDt.Format("2006-01-02"))
	}
	if !isPrimary {
		eventID = kalenderName + ":" + eventID
	}

	status := "Aankomend"
	if calendarEventEndInstant(eindDt, isAllDay).Before(now) {
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
	businessContextType, businessContextID, businessContextTitle := businessContextFromDescription(ev.Description)

	return &PersonalEventSync{
		UserID:               userID,
		EventID:              eventID,
		Titel:                titel,
		StartDatum:           startDt.Format("2006-01-02"),
		StartTijd:            startTijd,
		EindDatum:            eindDt.Format("2006-01-02"),
		EindTijd:             eindTijd,
		Heledag:              isAllDay,
		Locatie:              ev.Location,
		Beschrijving:         ev.Description,
		Symbol:               symbolFromDescription(ev.Description),
		BusinessContextType:  businessContextType,
		BusinessContextID:    businessContextID,
		BusinessContextTitle: businessContextTitle,
		Status:               status,
		Kalender:             kalenderName,
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
	// Use the ISO year (not the calendar Year()) so the year-week is correct at
	// year boundaries — e.g. 2025-12-29 is ISO 2026-W01, not 2025-W01.
	isoYear, week := d.ISOWeek()
	return fmt.Sprintf("%d-W%02d", isoYear, week)
}
