// Package whatsapp parses WhatsApp "Exporteer chat" (.txt) files into structured
// messages. WhatsApp has no API for personal chats, so the export file is the
// only ToS-safe source; formats vary by platform/locale, so the parser is
// deliberately lenient and best-effort about timestamps.
package whatsapp

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Message is one parsed chat line.
type Message struct {
	Sender string     `json:"sender"`
	Body   string     `json:"body"`
	Kind   string     `json:"kind"` // text | media | system
	SentAt *time.Time `json:"sent_at"`
}

// Result is the outcome of parsing an export.
type Result struct {
	Messages     []Message `json:"messages"`
	Participants []string  `json:"participants"`
}

var (
	// iOS: "[06-07-2026, 14:32:15] Naam: bericht" (comma optional; seconds optional;
	// optional AM/PM). A leading LTR mark (U+200E) is common.
	iosLine = regexp.MustCompile(`^\x{200e}?\[(\d{1,2})[-/.](\d{1,2})[-/.](\d{2,4})[,]?\s+(\d{1,2}):(\d{2})(?::(\d{2}))?\s*([AaPp]\.?[Mm]\.?)?\]\s*(.*)$`)
	// Android: "06-07-2026, 14:32 - Naam: bericht" (comma optional; seconds optional).
	androidLine = regexp.MustCompile(`^(\d{1,2})[-/.](\d{1,2})[-/.](\d{2,4})[,]?\s+(\d{1,2}):(\d{2})(?::(\d{2}))?\s*([AaPp]\.?[Mm]\.?)?\s+-\s+(.*)$`)
	// "Sender: rest" — a sender name never contains a colon, so split on the first.
	senderSplit = regexp.MustCompile(`^([^:]{1,120}?):\s?(.*)$`)
	// Media placeholders across locales.
	mediaHints = []string{"<media omitted>", "media weggelaten", "<media weggelaten>", "image omitted", "video omitted", "afbeelding weggelaten", "video weggelaten", "audio weggelaten", "sticker weggelaten", "gif weggelaten", "document weggelaten"}
)

// Parse turns exported chat text into messages + the distinct participants.
func Parse(text string) Result {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	messages := make([]Message, 0, len(lines))
	seen := map[string]bool{}
	participants := []string{}

	for _, raw := range lines {
		line := strings.TrimRight(strings.TrimPrefix(raw, "‎"), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		ts, rest, ok := matchHeader(line)
		if !ok {
			// Continuation of the previous message (a multi-line body).
			if n := len(messages); n > 0 {
				messages[n-1].Body += "\n" + strings.TrimRight(raw, "\r")
			}
			continue
		}
		rest = strings.TrimPrefix(rest, "‎")
		if m := senderSplit.FindStringSubmatch(rest); m != nil {
			sender := strings.TrimSpace(m[1])
			body := m[2]
			msg := Message{Sender: sender, Body: body, Kind: classify(body), SentAt: ts}
			messages = append(messages, msg)
			if sender != "" && !seen[sender] {
				seen[sender] = true
				participants = append(participants, sender)
			}
		} else {
			// A dated line with no "Name:" is a system notification (encryption
			// notice, "je hebt de groep aangemaakt", etc.).
			messages = append(messages, Message{Sender: "", Body: rest, Kind: "system", SentAt: ts})
		}
	}
	return Result{Messages: messages, Participants: participants}
}

// matchHeader returns the timestamp + the "Sender: body" remainder if the line
// starts a new message, else ok=false (a continuation line).
func matchHeader(line string) (*time.Time, string, bool) {
	if m := iosLine.FindStringSubmatch(line); m != nil {
		return buildTime(m[1], m[2], m[3], m[4], m[5], m[6], m[7]), m[8], true
	}
	if m := androidLine.FindStringSubmatch(line); m != nil {
		return buildTime(m[1], m[2], m[3], m[4], m[5], m[6], m[7]), m[8], true
	}
	return nil, "", false
}

var amsterdam = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// buildTime parses a day-first (NL/EU) timestamp best-effort; a value it can't
// make sense of yields nil rather than a wrong date.
func buildTime(dayS, monthS, yearS, hourS, minS, secS, ampm string) *time.Time {
	day, _ := strconv.Atoi(dayS)
	month, _ := strconv.Atoi(monthS)
	year, _ := strconv.Atoi(yearS)
	hour, _ := strconv.Atoi(hourS)
	minute, _ := strconv.Atoi(minS)
	sec := 0
	if secS != "" {
		sec, _ = strconv.Atoi(secS)
	}
	if year < 100 {
		year += 2000
	}
	switch strings.ToLower(strings.ReplaceAll(ampm, ".", "")) {
	case "pm":
		if hour < 12 {
			hour += 12
		}
	case "am":
		if hour == 12 {
			hour = 0
		}
	}
	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 {
		return nil
	}
	t := time.Date(year, time.Month(month), day, hour, minute, sec, 0, amsterdam)
	return &t
}

func classify(body string) string {
	low := strings.ToLower(strings.TrimSpace(body))
	for _, hint := range mediaHints {
		if strings.Contains(low, hint) {
			return "media"
		}
	}
	return "text"
}
