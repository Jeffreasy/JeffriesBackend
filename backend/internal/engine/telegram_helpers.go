package engine

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

var (
	markdownLinkRe             = regexp.MustCompile(`\[(.*?)\]\((https?://[^)]+)\)`)
	markdownHeaderRe           = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	markdownBoldRe             = regexp.MustCompile(`(\*\*|__)`)
	markdownStrikeRe           = regexp.MustCompile(`~~([^~]+)~~`)
	markdownItalicAsteriskRe   = regexp.MustCompile(`\*([^\s*_\n][^*_\n]*)\*`)
	markdownItalicUnderscoreRe = regexp.MustCompile(`_([^\s*_\n][^*_\n]*)_`)
)

func telegramLocation() *time.Location {
	return amsterdamLocation()
}

// classifyUserFacingError maps common technical error text (timeouts,
// network failures, malformed API responses) into short, actionable Dutch
// messages instead of forwarding a raw Go/API error string straight into a
// Telegram reply — a bot that otherwise speaks fluent, friendly Dutch
// breaking character with e.g. "parse error: unexpected EOF" right when the
// user most needs reassurance undermines trust in the whole assistant. The
// original text is always logged server-side so nothing is lost for
// debugging.
func classifyUserFacingError(raw string) string {
	lower := strings.ToLower(raw)

	// Already a friendly, actionable Dutch message (circuit-breaker-open or
	// missing-config messages built elsewhere) — pass through unchanged.
	if strings.Contains(lower, "tijdelijk onbereikbaar") || strings.Contains(lower, "niet geconfigureerd") {
		return raw
	}

	var dutch string
	switch {
	case strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "timeout"):
		dutch = "De AI reageerde niet op tijd. Probeer het nog eens."
	case strings.Contains(lower, "429") || strings.Contains(lower, "rate limit"):
		dutch = "Te veel aanvragen bij de AI. Wacht even en probeer het opnieuw."
	case strings.Contains(lower, "dial tcp") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host") || strings.Contains(lower, "network"):
		dutch = "Kon geen verbinding maken met de AI-server. Probeer het zo nog eens."
	case strings.Contains(lower, "parse error") || strings.Contains(lower, "marshal error"):
		dutch = "Onverwacht antwoord van de AI-server. Probeer het opnieuw."
	case strings.Contains(lower, "download") || strings.Contains(lower, "getfile") || strings.Contains(lower, "transcri"):
		dutch = "Kon je spraakbericht niet verwerken. Probeer het opnieuw of typ je bericht."
	default:
		dutch = "Er ging iets mis bij het verwerken van je bericht. Probeer het opnieuw."
	}
	slog.Warn("AI/telegram error shown to user", "raw", raw, "shown", dutch)
	return dutch
}

func dutchMonthName(m time.Month) string {
	names := [...]string{"", "jan", "feb", "mrt", "apr", "mei", "jun", "jul", "aug", "sep", "okt", "nov", "dec"}
	return names[m]
}

func dutchDayName(d time.Weekday) string {
	names := [...]string{"zondag", "maandag", "dinsdag", "woensdag", "donderdag", "vrijdag", "zaterdag"}
	return names[d]
}

func dutchDayShort(d time.Weekday) string {
	names := [...]string{"Zo", "Ma", "Di", "Wo", "Do", "Vr", "Za"}
	return names[d]
}

func formatEuroTelegram(value float64) string {
	if value < 0 {
		return fmt.Sprintf("-€%.2f", -value)
	}
	return fmt.Sprintf("€%.2f", value)
}

func pluralNL(count int, singular, plural string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", count, plural)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func relativeDateLabel(iso string, now time.Time) string {
	loc := now.Location()
	date, err := time.ParseInLocation("2006-01-02", iso, loc)
	if err != nil {
		return iso
	}
	todayUTC := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	targetUTC := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	diff := int(targetUTC.Sub(todayUTC).Hours() / 24)
	dateLabel := targetUTC.Format("02-01-2006")
	switch diff {
	case 0:
		return "vandaag (" + dateLabel + ")"
	case 1:
		return "morgen (" + dateLabel + ")"
	case 2:
		return "overmorgen (" + dateLabel + ")"
	default:
		return dutchDayName(targetUTC.Weekday()) + " (" + dateLabel + ")"
	}
}

func formatNextSchedule(schedule *model.Schedule, now time.Time) string {
	if schedule == nil {
		return "Geen aankomende dienst gevonden."
	}

	label := relativeDateLabel(schedule.StartDatum, now)
	title := strings.TrimSpace(schedule.ShiftType)
	if title == "" {
		title = strings.TrimSpace(schedule.Titel)
	}
	if title == "" {
		title = "Dienst"
	}

	timeLabel := schedule.StartTijd
	if schedule.EindTijd != "" {
		timeLabel += "–" + schedule.EindTijd
	}
	if timeLabel == "" {
		timeLabel = "hele dag"
	}

	location := strings.TrimSpace(schedule.Locatie)
	if location != "" {
		return fmt.Sprintf("%s — %s (%s) · %s", title, label, timeLabel, location)
	}
	return fmt.Sprintf("%s — %s (%s)", title, label, timeLabel)
}

func scheduleStartsAfter(schedule model.Schedule, now time.Time, loc *time.Location) bool {
	start, err := parseScheduleDateTime(schedule.StartDatum, schedule.StartTijd, loc)
	if err != nil {
		return false
	}
	end, err := parseScheduleDateTime(emptyFallback(schedule.EindDatum, schedule.StartDatum), schedule.EindTijd, loc)
	if err != nil {
		end = start
	}
	if end.Before(start) {
		end = end.AddDate(0, 0, 1)
	}
	return !end.Before(now.Add(-15 * time.Minute))
}

func parseScheduleDateTime(datePart, timePart string, loc *time.Location) (time.Time, error) {
	datePart = strings.TrimSpace(datePart)
	timePart = strings.TrimSpace(timePart)
	if timePart == "" {
		return time.ParseInLocation("2006-01-02", datePart, loc)
	}
	return time.ParseInLocation("2006-01-02 15:04", datePart+" "+timePart, loc)
}

func stripTelegramPlainText(text string) string {
	text = markdownLinkRe.ReplaceAllString(text, "$1 ($2)")
	text = markdownHeaderRe.ReplaceAllString(text, "")
	text = markdownBoldRe.ReplaceAllString(text, "")
	text = markdownStrikeRe.ReplaceAllString(text, "$1")
	text = markdownItalicAsteriskRe.ReplaceAllString(text, "$1")
	text = markdownItalicUnderscoreRe.ReplaceAllString(text, "$1")
	text = strings.ReplaceAll(text, "```", "")
	text = strings.ReplaceAll(text, "`", "")
	return strings.TrimSpace(text)
}

func normalizeAssistantText(text string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err == nil {
		if t, ok := parsed["telegramText"].(string); ok {
			return stripTelegramPlainText(t)
		}
		if t, ok := parsed["antwoord"].(string); ok {
			return stripTelegramPlainText(t)
		}
	}
	return stripTelegramPlainText(text)
}
