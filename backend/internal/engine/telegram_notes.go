package engine

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	tg "github.com/Jeffreasy/JeffriesBackend/internal/telegram"
	"github.com/google/uuid"
)

var (
	noteHashtagRe       = regexp.MustCompile(`(^|\s)#([A-Za-z0-9_-]+)`)
	notePriorityTokenRe = regexp.MustCompile(`(?i)(^|\s)!(hoog|high|urgent|laag|low|normaal|normal)(\s|$)`)
	notePrefixRe        = regexp.MustCompile(`(?i)^\s*(idee|todo|actie|notitie)\s*:\s*`)
	noteDateRe          = regexp.MustCompile(`\b(\d{1,2})[-/](\d{1,2})(?:[-/](\d{2,4}))?\b`)
	noteTimeRe          = regexp.MustCompile(`\b([01]?\d|2[0-3])[:.]([0-5]\d)\b`)
)

type noteStats struct {
	Active    int
	Today     int
	Pinned    int
	Completed int
	Attention int
	TopTags   []tagCount
}

type tagCount struct {
	Tag   string
	Count int
}

type noteCapture struct {
	Title      string
	Content    string
	Tags       []string
	Priority   *string
	Symbol     *string
	TriageFlag *bool
	Deadline   *time.Time
}

func (e *Engine) handleNotitiesDashboard(ctx context.Context, client *tg.Client, chatID int64) {
	nStore := store.NewNoteStore(e.db)
	notes, err := nStore.List(ctx, e.cfg.HomeappUserID)
	if err != nil {
		_ = client.SendMessage(chatID, "Fout bij ophalen notities.")
		return
	}

	active := activeNotes(notes)
	if len(active) == 0 {
		_ = client.SendMessage(chatID, "📝 Je hebt nog geen notities. Stuur een spraakbericht of typ een idee, en ik sla het voor je op!")
		return
	}

	loc, _ := time.LoadLocation("Europe/Amsterdam")
	now := time.Now().In(loc)
	stats := buildNoteStats(active, now, loc)
	focusNotes := selectFocusNotes(active, now, loc, 5)
	tagSummary := formatTopTags(stats.TopTags)

	var b strings.Builder
	fmt.Fprintf(&b, "🧠 Notitie cockpit\n")
	fmt.Fprintf(&b, "%s %02d %s %s\n\n", dutchDayName(now.Weekday()), now.Day(), dutchMonthName(now.Month()), now.Format("15:04"))
	fmt.Fprintf(&b, "Status\n")
	fmt.Fprintf(&b, "• Actief: %d\n", stats.Active)
	fmt.Fprintf(&b, "• Vandaag: %d nieuw/gewijzigd\n", stats.Today)
	fmt.Fprintf(&b, "• Pinned: %d · afgerond: %d\n", stats.Pinned, stats.Completed)
	fmt.Fprintf(&b, "• Deadline/triage: %d aandachtspunt(en)\n", stats.Attention)
	if tagSummary != "" {
		fmt.Fprintf(&b, "• Tags: %s\n", tagSummary)
	}

	fmt.Fprintf(&b, "\nFocus nu\n")
	for i, note := range focusNotes {
		fmt.Fprintf(&b, "%d. %s\n", i+1, formatNoteListLine(note, now, loc))
	}
	if len(focusNotes) == 0 {
		fmt.Fprintf(&b, "Geen open focuspunten. Mooi rustig.\n")
	}

	fmt.Fprintf(&b, "\nAI kan nu triage doen, zoeken, samenvatten of nieuwe notities capture'n via tekst/spraak.")

	keyboard := buildNotesDashboardKeyboard(focusNotes)
	_ = client.SendMessageWithKeyboard(chatID, b.String(), keyboard)
}

func activeNotes(notes []model.Note) []model.Note {
	active := make([]model.Note, 0, len(notes))
	for _, note := range notes {
		if !note.IsArchived {
			active = append(active, note)
		}
	}
	return active
}

func openNotes(notes []model.Note) []model.Note {
	open := make([]model.Note, 0, len(notes))
	for _, note := range notes {
		if !note.IsArchived && !note.IsCompleted {
			open = append(open, note)
		}
	}
	return open
}

func buildNoteStats(notes []model.Note, now time.Time, loc *time.Location) noteStats {
	today := now.In(loc).Format("2006-01-02")
	stats := noteStats{}
	tagCounts := make(map[string]int)

	for _, note := range notes {
		if note.Aangemaakt.In(loc).Format("2006-01-02") == today || note.Gewijzigd.In(loc).Format("2006-01-02") == today {
			stats.Today++
		}
		if note.IsPinned {
			stats.Pinned++
		}
		if note.IsCompleted {
			stats.Completed++
			continue
		}
		stats.Active++
		if noteNeedsAttention(note, now, loc) {
			stats.Attention++
		}
		for _, tag := range note.Tags {
			tag = strings.TrimSpace(strings.ToLower(tag))
			if tag != "" {
				tagCounts[tag]++
			}
		}
	}

	stats.TopTags = topTags(tagCounts, 4)
	return stats
}

func topTags(counts map[string]int, limit int) []tagCount {
	tags := make([]tagCount, 0, len(counts))
	for tag, count := range counts {
		tags = append(tags, tagCount{Tag: tag, Count: count})
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Count == tags[j].Count {
			return tags[i].Tag < tags[j].Tag
		}
		return tags[i].Count > tags[j].Count
	})
	if len(tags) > limit {
		tags = tags[:limit]
	}
	return tags
}

func formatTopTags(tags []tagCount) string {
	parts := make([]string, 0, len(tags))
	for _, tag := range tags {
		parts = append(parts, fmt.Sprintf("#%s (%d)", tag.Tag, tag.Count))
	}
	return strings.Join(parts, ", ")
}

func selectFocusNotes(notes []model.Note, now time.Time, loc *time.Location, limit int) []model.Note {
	candidates := make([]model.Note, 0, len(notes))
	for _, note := range notes {
		if note.IsCompleted {
			continue
		}
		candidates = append(candidates, note)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := noteUrgencyScore(candidates[i], now, loc)
		right := noteUrgencyScore(candidates[j], now, loc)
		if left == right {
			return candidates[i].Gewijzigd.After(candidates[j].Gewijzigd)
		}
		return left > right
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func noteNeedsAttention(note model.Note, now time.Time, loc *time.Location) bool {
	if note.IsCompleted {
		return false
	}
	if note.TriageFlag != nil && *note.TriageFlag {
		return true
	}
	if strings.EqualFold(optionalNoteString(note.Prioriteit), "hoog") {
		return true
	}
	if note.Deadline != nil {
		deadline := note.Deadline.In(loc)
		return !deadline.After(now.AddDate(0, 0, 3))
	}
	checked, total := checklistProgress(note.Inhoud)
	return total > 0 && checked < total
}

func noteUrgencyScore(note model.Note, now time.Time, loc *time.Location) int {
	score := 0
	if note.IsPinned {
		score += 20
	}
	if note.TriageFlag != nil && *note.TriageFlag {
		score += 35
	}
	switch strings.ToLower(optionalNoteString(note.Prioriteit)) {
	case "hoog":
		score += 45
	case "normaal":
		score += 15
	case "laag":
		score += 5
	}
	if note.Deadline != nil {
		deadline := note.Deadline.In(loc)
		hours := deadline.Sub(now).Hours()
		switch {
		case hours < 0:
			score += 100
		case hours <= 24:
			score += 80
		case hours <= 72:
			score += 55
		case hours <= 168:
			score += 30
		default:
			score += 10
		}
	}
	checked, total := checklistProgress(note.Inhoud)
	if total > 0 {
		score += 10 + (total - checked)
	}
	return score
}

// noteSymbolEmoji maps inferNoteSymbol's internal English codewords to a
// real emoji for display. These codewords were never meant to be
// user-facing vocabulary — rendering "symbool warning"/"symbool note"
// verbatim (as this used to) fires on essentially every note shown,
// leaking internal plumbing into an otherwise all-Dutch UI.
func noteSymbolEmoji(symbol string) string {
	switch strings.ToLower(strings.TrimSpace(symbol)) {
	case "warning":
		return "⚠️"
	case "check":
		return "✅"
	case "calendar":
		return "📅"
	case "work":
		return "💼"
	case "finance":
		return "💰"
	case "habit":
		return "🔁"
	case "shield":
		return "🔒"
	case "sparkles":
		return "✨"
	default:
		return "📝"
	}
}

func formatNoteListLine(note model.Note, now time.Time, loc *time.Location) string {
	parts := []string{}
	if note.Symbol != nil && strings.TrimSpace(*note.Symbol) != "" {
		parts = append(parts, noteSymbolEmoji(*note.Symbol))
	}
	if note.IsPinned {
		parts = append(parts, "📌")
	}
	if note.TriageFlag != nil && *note.TriageFlag {
		parts = append(parts, "⚑")
	}

	title := noteTitle(note)
	prefix := strings.Join(parts, " ")
	if prefix != "" {
		title = prefix + " " + title
	}

	meta := []string{}
	if note.Prioriteit != nil && strings.TrimSpace(*note.Prioriteit) != "" {
		meta = append(meta, "prio "+strings.ToLower(strings.TrimSpace(*note.Prioriteit)))
	}
	if note.Deadline != nil {
		meta = append(meta, "deadline "+formatNoteDeadline(*note.Deadline, now, loc))
	}
	checked, total := checklistProgress(note.Inhoud)
	if total > 0 {
		meta = append(meta, fmt.Sprintf("%d/%d checklist", checked, total))
	}
	if note.BusinessContextTitle != nil && strings.TrimSpace(*note.BusinessContextTitle) != "" {
		meta = append(meta, strings.TrimSpace(*note.BusinessContextTitle))
	} else if note.BusinessContextType != nil && strings.TrimSpace(*note.BusinessContextType) != "" {
		meta = append(meta, strings.TrimSpace(*note.BusinessContextType))
	}
	if len(note.Tags) > 0 {
		meta = append(meta, "#"+strings.Join(note.Tags[:minInt(len(note.Tags), 2)], " #"))
	}
	if len(meta) > 0 {
		return title + " — " + strings.Join(meta, " · ")
	}
	return title
}

func formatNoteDeadline(deadline time.Time, now time.Time, loc *time.Location) string {
	deadline = deadline.In(loc)
	dayUTC := time.Date(deadline.Year(), deadline.Month(), deadline.Day(), 0, 0, 0, 0, time.UTC)
	todayUTC := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	diff := int(dayUTC.Sub(todayUTC).Hours() / 24)
	switch diff {
	case 0:
		return "vandaag"
	case 1:
		return "morgen"
	case 2:
		return "overmorgen"
	}
	if diff < 0 {
		return fmt.Sprintf("%d dag(en) te laat", -diff)
	}
	return dayUTC.Format("02-01-2006")
}

func checklistProgress(content string) (checked int, total int) {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(strings.ToLower(line))
		switch {
		case strings.HasPrefix(trimmed, "- [x]"), strings.HasPrefix(trimmed, "* [x]"), strings.HasPrefix(trimmed, "✅"):
			checked++
			total++
		case strings.HasPrefix(trimmed, "- [ ]"), strings.HasPrefix(trimmed, "* [ ]"), strings.HasPrefix(trimmed, "☐"):
			total++
		}
	}
	return checked, total
}

func buildNotesDashboardKeyboard(notes []model.Note) tg.InlineKeyboardMarkup {
	keyboard := [][]tg.InlineKeyboardButton{
		{
			{Text: "🧠 AI triage", CallbackData: "/noteai"},
			{Text: "🔎 Zoek", CallbackData: "/zoeknote"},
		},
		{
			{Text: "📍 Vandaag", CallbackData: "/vandaag"},
			{Text: "📅 Week", CallbackData: "/week"},
		},
		{
			{Text: "✍️ Capture", CallbackData: "/notehelp"},
			{Text: "🏠 Start", CallbackData: "/start"},
		},
	}

	for i, note := range notes {
		keyboard = append(keyboard, []tg.InlineKeyboardButton{
			{Text: fmt.Sprintf("👁️ %d", i+1), CallbackData: "note_read_" + note.ID.String()},
			{Text: "✅", CallbackData: "note_done_" + note.ID.String()},
			{Text: "📌", CallbackData: "note_pin_" + note.ID.String()},
			{Text: "📥", CallbackData: "note_archive_" + note.ID.String()},
		})
	}

	return tg.InlineKeyboardMarkup{InlineKeyboard: keyboard}
}

func (e *Engine) handleNoteRead(ctx context.Context, client *tg.Client, chatID int64, noteIDStr string) {
	id, err := uuid.Parse(noteIDStr)
	if err != nil {
		_ = client.SendMessage(chatID, "Ongeldig notitie ID.")
		return
	}
	nStore := store.NewNoteStore(e.db)
	note, err := nStore.GetForUser(ctx, e.cfg.HomeappUserID, id)
	if err != nil {
		_ = client.SendMessage(chatID, "Notitie niet gevonden.")
		return
	}

	loc, _ := time.LoadLocation("Europe/Amsterdam")
	now := time.Now().In(loc)
	_ = client.SendMessageWithKeyboard(chatID, formatNoteDetail(note, now, loc), buildSingleNoteKeyboard(note))
}

// sendNoteActionOutcome sends (or, if originMessageID is set — meaning this
// action was triggered by tapping a button — edits in place) the result of
// a note action, replacing the origin message's keyboard so a now-stale
// button (e.g. "archive" on a note that's already archived) can't be
// re-tapped from the old view.
func (e *Engine) sendNoteActionOutcome(client *tg.Client, chatID int64, text string, keyboard tg.InlineKeyboardMarkup, originMessageID *int64) {
	if originMessageID != nil {
		_ = client.EditMessageText(chatID, *originMessageID, text, &keyboard)
		return
	}
	_ = client.SendMessageWithKeyboard(chatID, text, keyboard)
}

var startMenuOnlyKeyboard = tg.InlineKeyboardMarkup{
	InlineKeyboard: [][]tg.InlineKeyboardButton{{{Text: "🏠 Startmenu", CallbackData: "/start"}}},
}

func (e *Engine) handleNoteArchive(ctx context.Context, client *tg.Client, chatID int64, noteIDStr string, originMessageID *int64) {
	id, err := uuid.Parse(noteIDStr)
	if err != nil {
		_ = client.SendMessage(chatID, "Ongeldig notitie ID.")
		return
	}
	nStore := store.NewNoteStore(e.db)

	_, err = nStore.UpdateForUser(ctx, e.cfg.HomeappUserID, id, map[string]any{"is_archived": true})
	if err != nil {
		_ = client.SendMessage(chatID, "Fout bij archiveren.")
		return
	}

	e.sendNoteActionOutcome(client, chatID, "✅ Notitie gearchiveerd.", startMenuOnlyKeyboard, originMessageID)
	e.handleNotitiesDashboard(ctx, client, chatID)
}

func (e *Engine) handleNoteDone(ctx context.Context, client *tg.Client, chatID int64, noteIDStr string, originMessageID *int64) {
	id, err := uuid.Parse(noteIDStr)
	if err != nil {
		_ = client.SendMessage(chatID, "Ongeldig notitie ID.")
		return
	}

	now := time.Now()
	nStore := store.NewNoteStore(e.db)
	note, err := nStore.UpdateForUser(ctx, e.cfg.HomeappUserID, id, map[string]any{
		"is_completed": true,
		"completed_at": now,
		"triage_flag":  false,
	})
	if err != nil {
		_ = client.SendMessage(chatID, "Fout bij afronden.")
		return
	}

	text := fmt.Sprintf("✅ Afgerond\n📝 %s", noteTitle(note))
	e.sendNoteActionOutcome(client, chatID, text, buildSingleNoteKeyboard(note), originMessageID)
}

func (e *Engine) handleNoteSearch(ctx context.Context, client *tg.Client, chatID int64, query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		_ = client.SendMessageWithKeyboard(chatID, buildNoteSearchHelpText(), buildNotesMenu())
		return
	}

	nStore := store.NewNoteStore(e.db)
	var notes []model.Note
	var err error

	if strings.HasPrefix(query, "#") {
		var all []model.Note
		all, err = nStore.List(ctx, e.cfg.HomeappUserID)
		notes = fallbackNoteSearch(all, query, 8)
	} else {
		notes, err = nStore.Search(ctx, e.cfg.HomeappUserID, query, 8)
		if err == nil {
			notes = activeNotes(notes)
		}
		if err == nil && len(notes) == 0 {
			var all []model.Note
			all, err = nStore.List(ctx, e.cfg.HomeappUserID)
			notes = fallbackNoteSearch(all, query, 8)
		}
	}

	if err != nil {
		_ = client.SendMessage(chatID, "Fout bij zoeken in notities.")
		return
	}

	loc, _ := time.LoadLocation("Europe/Amsterdam")
	now := time.Now().In(loc)
	var b strings.Builder
	fmt.Fprintf(&b, "🔎 Notities zoeken\n")
	fmt.Fprintf(&b, "Query: %s\n\n", query)
	if len(notes) == 0 {
		fmt.Fprintf(&b, "Geen actieve notities gevonden.\n\nTip: zoek ook op #tag, bijvoorbeeld /zoeknote #dkl.")
		_ = client.SendMessageWithKeyboard(chatID, b.String(), buildNotesMenu())
		return
	}

	for i, note := range notes {
		fmt.Fprintf(&b, "%d. %s\n", i+1, formatNoteListLine(note, now, loc))
	}

	_ = client.SendMessageWithKeyboard(chatID, b.String(), buildNotesSearchKeyboard(notes))
}

func fallbackNoteSearch(notes []model.Note, query string, limit int) []model.Note {
	needle := strings.TrimSpace(strings.ToLower(strings.TrimPrefix(query, "#")))
	if needle == "" {
		return nil
	}

	results := make([]model.Note, 0, limit)
	for _, note := range notes {
		if note.IsArchived {
			continue
		}
		if noteMatchesQuery(note, needle) {
			results = append(results, note)
			if len(results) >= limit {
				break
			}
		}
	}
	return results
}

func noteMatchesQuery(note model.Note, needle string) bool {
	for _, tag := range note.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == needle || strings.Contains(tag, needle) {
			return true
		}
	}
	haystack := strings.ToLower(strings.Join([]string{
		noteTitle(note),
		note.Inhoud,
		strings.Join(note.Tags, " "),
		optionalNoteString(note.Prioriteit),
		optionalNoteString(note.Symbol),
	}, " "))
	return strings.Contains(haystack, needle)
}

func buildNotesSearchKeyboard(notes []model.Note) tg.InlineKeyboardMarkup {
	keyboard := [][]tg.InlineKeyboardButton{
		{
			{Text: "🧠 AI triage", CallbackData: "/noteai"},
			{Text: "📝 Cockpit", CallbackData: "/notities"},
		},
	}

	limit := minInt(len(notes), 5)
	for i := 0; i < limit; i++ {
		note := notes[i]
		keyboard = append(keyboard, []tg.InlineKeyboardButton{
			{Text: fmt.Sprintf("👁️ %d", i+1), CallbackData: "note_read_" + note.ID.String()},
			{Text: "✅", CallbackData: "note_done_" + note.ID.String()},
			{Text: "📌", CallbackData: "note_pin_" + note.ID.String()},
			{Text: "📥", CallbackData: "note_archive_" + note.ID.String()},
		})
	}

	return tg.InlineKeyboardMarkup{InlineKeyboard: keyboard}
}

func buildSingleNoteKeyboard(note model.Note) tg.InlineKeyboardMarkup {
	primary := []tg.InlineKeyboardButton{
		{Text: "📌 Pin", CallbackData: "note_pin_" + note.ID.String()},
		{Text: "📥 Archiveer", CallbackData: "note_archive_" + note.ID.String()},
	}
	if !note.IsCompleted {
		primary = append([]tg.InlineKeyboardButton{{Text: "✅ Rond af", CallbackData: "note_done_" + note.ID.String()}}, primary...)
	}

	return tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			primary,
			{
				{Text: "🧠 AI triage", CallbackData: "/noteai"},
				{Text: "📝 Cockpit", CallbackData: "/notities"},
			},
		},
	}
}

func formatNoteDetail(note model.Note, now time.Time, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📝 %s\n", noteTitle(note))

	meta := []string{}
	if note.IsPinned {
		meta = append(meta, "pinned")
	}
	if note.IsCompleted {
		meta = append(meta, "afgerond")
	}
	if note.TriageFlag != nil && *note.TriageFlag {
		meta = append(meta, "triage")
	}
	if note.Prioriteit != nil && strings.TrimSpace(*note.Prioriteit) != "" {
		meta = append(meta, "prio "+strings.ToLower(strings.TrimSpace(*note.Prioriteit)))
	}
	if note.Deadline != nil {
		meta = append(meta, "deadline "+formatNoteDeadline(*note.Deadline, now, loc))
	}
	checked, total := checklistProgress(note.Inhoud)
	if total > 0 {
		meta = append(meta, fmt.Sprintf("%d/%d checklist", checked, total))
	}
	if len(note.Tags) > 0 {
		meta = append(meta, "#"+strings.Join(note.Tags, " #"))
	}
	if len(meta) > 0 {
		fmt.Fprintf(&b, "%s\n", strings.Join(meta, " · "))
	}

	content := strings.TrimSpace(note.Inhoud)
	if content == "" {
		content = "(geen inhoud)"
	}
	fmt.Fprintf(&b, "\n%s\n", truncateRunes(content, 1800))
	fmt.Fprintf(&b, "\nGewijzigd: %s", note.Gewijzigd.In(loc).Format("02-01-2006 15:04"))
	return b.String()
}

func (e *Engine) handleVandaagNotities(ctx context.Context, client *tg.Client, chatID int64) {
	nStore := store.NewNoteStore(e.db)
	notes, err := nStore.List(ctx, e.cfg.HomeappUserID)
	if err != nil {
		_ = client.SendMessage(chatID, "Fout bij ophalen notities.")
		return
	}

	loc, _ := time.LoadLocation("Europe/Amsterdam")
	now := time.Now().In(loc)
	todayStr := now.Format("2006-01-02")
	dagNaam := dutchDayName(now.Weekday())
	datumStr := fmt.Sprintf("%d %s", now.Day(), dutchMonthName(now.Month()))

	var todayNotes []model.Note
	for _, n := range notes {
		if !n.IsArchived && (n.Aangemaakt.In(loc).Format("2006-01-02") == todayStr || n.Gewijzigd.In(loc).Format("2006-01-02") == todayStr) {
			todayNotes = append(todayNotes, n)
		}
	}

	text := fmt.Sprintf("📝 Vandaag — %s %s\n\n", dagNaam, datumStr)

	if len(todayNotes) == 0 {
		text += "Nog geen notities vandaag.\nGebruik /noteer [tekst] of stuur een spraakbericht.\n"
	} else {
		for i, n := range todayNotes {
			titel := "Naamloze notitie"
			if n.Titel != nil && *n.Titel != "" {
				titel = *n.Titel
			}
			pinStr := ""
			if n.IsPinned {
				pinStr = "📌 "
			}
			tijdStr := n.Aangemaakt.In(loc).Format("15:04")
			text += fmt.Sprintf("%d. %s%s  (%s)\n", i+1, pinStr, titel, tijdStr)

			// Preview first line
			preview := strings.SplitN(n.Inhoud, "\n", 2)[0]
			if len(preview) > 60 {
				preview = preview[:57] + "..."
			}
			if preview != "" && preview != titel {
				text += fmt.Sprintf("   %s\n", preview)
			}

			if len(n.Tags) > 0 {
				text += fmt.Sprintf("   🏷️ %s\n", strings.Join(n.Tags, ", "))
			}
			text += "\n"
		}
	}

	text += "━━━━━━━━━━━━━━━━━━━━━"

	keyboard := tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "📋 Alle notities", CallbackData: "/notities"},
				{Text: "📅 Week overzicht", CallbackData: "/week"},
			},
		},
	}
	_ = client.SendMessageWithKeyboard(chatID, text, keyboard)
}

func (e *Engine) handleWeekNotities(ctx context.Context, client *tg.Client, chatID int64) {
	nStore := store.NewNoteStore(e.db)
	notes, err := nStore.List(ctx, e.cfg.HomeappUserID)
	if err != nil {
		_ = client.SendMessage(chatID, "Fout bij ophalen notities.")
		return
	}

	loc, _ := time.LoadLocation("Europe/Amsterdam")
	now := time.Now().In(loc)

	weekday := now.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	monday := now.AddDate(0, 0, -int(weekday-time.Monday))
	monday = time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, loc)

	_, weekNr := monday.ISOWeek()
	sundayDate := monday.AddDate(0, 0, 6)

	text := fmt.Sprintf("📅 Week %d — %d %s – %d %s\n\n",
		weekNr,
		monday.Day(), dutchMonthName(monday.Month()),
		sundayDate.Day(), dutchMonthName(sundayDate.Month()))

	totalWeek := 0
	for i := 0; i < 7; i++ {
		day := monday.AddDate(0, 0, i)
		dayStr := day.Format("2006-01-02")
		dagNaam := dutchDayShort(day.Weekday())
		datumStr := fmt.Sprintf("%d %s", day.Day(), dutchMonthName(day.Month()))

		count := 0
		for _, n := range notes {
			if !n.IsArchived && n.Aangemaakt.In(loc).Format("2006-01-02") == dayStr {
				count++
			}
		}
		totalWeek += count

		indicator := "📋"
		if dayStr == now.Format("2006-01-02") {
			indicator = "📍"
		}
		countStr := fmt.Sprintf("%d notitie", count)
		if count != 1 {
			countStr += "s"
		}
		text += fmt.Sprintf("%s %s %s — %s\n", indicator, dagNaam, datumStr, countStr)
	}

	text += fmt.Sprintf("\nTotaal: %d notities deze week\n", totalWeek)
	text += "━━━━━━━━━━━━━━━━━━━━━"

	keyboard := tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "📝 Vandaag", CallbackData: "/vandaag"},
				{Text: "📋 Alle notities", CallbackData: "/notities"},
			},
		},
	}
	_ = client.SendMessageWithKeyboard(chatID, text, keyboard)
}

func (e *Engine) handleQuickNote(ctx context.Context, client *tg.Client, chatID int64, text string) {
	if strings.TrimSpace(text) == "" {
		_ = client.SendMessage(chatID, "Gebruik: /noteer [jouw notitie tekst]")
		return
	}

	loc, _ := time.LoadLocation("Europe/Amsterdam")
	capture := parseNoteCapture(text, time.Now().In(loc), loc)
	nStore := store.NewNoteStore(e.db)
	n, err := nStore.Create(ctx, e.cfg.HomeappUserID, model.Note{
		Titel:      &capture.Title,
		Inhoud:     capture.Content,
		Tags:       capture.Tags,
		Prioriteit: capture.Priority,
		Symbol:     capture.Symbol,
		TriageFlag: capture.TriageFlag,
		Deadline:   capture.Deadline,
	})
	if err != nil {
		_ = client.SendMessage(chatID, fmt.Sprintf("Fout: %s", err.Error()))
		return
	}

	reply := buildNoteCaptureReply(n, capture, loc)

	chatStore := store.NewChatStore(e.db.Pool)
	agentID := "notes"
	_ = chatStore.SaveMessage(ctx, chatID, "assistant", reply, &agentID)

	_ = client.SendMessageWithKeyboard(chatID, reply, buildSingleNoteKeyboard(n))
}

func (e *Engine) handleNotePin(ctx context.Context, client *tg.Client, chatID int64, noteIDStr string, originMessageID *int64) {
	id, err := uuid.Parse(noteIDStr)
	if err != nil {
		_ = client.SendMessage(chatID, "Ongeldig notitie ID.")
		return
	}
	nStore := store.NewNoteStore(e.db)

	note, err := nStore.GetForUser(ctx, e.cfg.HomeappUserID, id)
	if err != nil {
		_ = client.SendMessage(chatID, "Notitie niet gevonden.")
		return
	}

	newPinned := !note.IsPinned
	_, err = nStore.UpdateForUser(ctx, e.cfg.HomeappUserID, id, map[string]any{"is_pinned": newPinned})
	if err != nil {
		_ = client.SendMessage(chatID, "Fout bij pinnen.")
		return
	}
	// Reflect the new state locally (no extra round-trip) so the outcome
	// message/keyboard shows the note's actual current pin status instead
	// of stale pre-toggle data.
	note.IsPinned = newPinned

	var text string
	if newPinned {
		text = "📌 Notitie vastgezet."
	} else {
		text = "📍 Pin verwijderd."
	}
	e.sendNoteActionOutcome(client, chatID, text, buildSingleNoteKeyboard(note), originMessageID)
	if originMessageID == nil {
		e.handleNotitiesDashboard(ctx, client, chatID)
	}
}

func parseNoteCapture(raw string, now time.Time, loc *time.Location) noteCapture {
	if loc == nil {
		loc = time.Local
	}
	content := strings.TrimSpace(raw)
	tags := inferNoteTags(content, extractNoteTags(content))
	priority := inferNotePriority(content)
	deadline := inferNoteDeadline(content, now, loc)
	symbol := inferNoteSymbol(content, tags, priority, deadline)
	triage := inferNoteTriageFlag(content, priority, deadline, now)

	title := cleanNoteTitle(content)
	if title == "" {
		title = "Nieuwe notitie"
	}

	return noteCapture{
		Title:      title,
		Content:    content,
		Tags:       tags,
		Priority:   priority,
		Symbol:     symbol,
		TriageFlag: triage,
		Deadline:   deadline,
	}
}

func buildNoteCaptureReply(note model.Note, capture noteCapture, loc *time.Location) string {
	meta := []string{}
	if len(capture.Tags) > 0 {
		meta = append(meta, "#"+strings.Join(capture.Tags, " #"))
	}
	if capture.Priority != nil {
		meta = append(meta, "prio "+strings.ToLower(*capture.Priority))
	}
	if capture.Deadline != nil {
		meta = append(meta, "deadline "+capture.Deadline.In(loc).Format("02-01-2006 15:04"))
	}
	if capture.TriageFlag != nil && *capture.TriageFlag {
		meta = append(meta, "triage")
	}

	icon := "📝"
	if capture.Symbol != nil {
		icon = noteSymbolEmoji(*capture.Symbol)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "✅ Notitie opgeslagen\n")
	fmt.Fprintf(&b, "%s %s", icon, noteTitle(note))
	if len(meta) > 0 {
		fmt.Fprintf(&b, "\nAI herkend: %s", strings.Join(meta, " · "))
	}
	fmt.Fprintf(&b, "\n\nGebruik /noteai voor triage of de knoppen hieronder voor snelle acties.")
	return b.String()
}

func extractNoteTags(content string) []string {
	matches := noteHashtagRe.FindAllStringSubmatch(content, -1)
	tags := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		tags = appendUniqueTag(tags, match[2])
	}
	return tags
}

func inferNoteTags(content string, tags []string) []string {
	lower := strings.ToLower(content)
	for _, candidate := range []struct {
		tag      string
		keywords []string
	}{
		{tag: "dkl", keywords: []string{"dkl"}},
		{tag: "laventecare", keywords: []string{"laventecare"}},
		{tag: "werk", keywords: []string{"werk", "dienst", "rooster"}},
		{tag: "planning", keywords: []string{"agenda", "afspraak", "planning"}},
		{tag: "finance", keywords: []string{"geld", "salaris", "factuur", "bank", "betaling"}},
		{tag: "idee", keywords: []string{"idee:", "idee ", "concept"}},
		{tag: "actie", keywords: []string{"todo", "actie", "moet", "bellen", "terugbellen", "mailen"}},
	} {
		for _, keyword := range candidate.keywords {
			if strings.Contains(lower, keyword) {
				tags = appendUniqueTag(tags, candidate.tag)
				break
			}
		}
	}
	if len(tags) > 6 {
		tags = tags[:6]
	}
	return tags
}

func appendUniqueTag(tags []string, tag string) []string {
	tag = normalizeNoteTag(tag)
	if tag == "" {
		return tags
	}
	for _, existing := range tags {
		if existing == tag {
			return tags
		}
	}
	return append(tags, tag)
}

func normalizeNoteTag(tag string) string {
	return strings.ToLower(strings.Trim(tag, " \t\r\n.,;:!?/#"))
}

func inferNotePriority(content string) *string {
	lower := strings.ToLower(content)
	switch {
	case strings.Contains(lower, "!hoog"), strings.Contains(lower, "!high"), strings.Contains(lower, "!urgent"),
		strings.Contains(lower, "urgent"), strings.Contains(lower, "belangrijk"), strings.Contains(lower, "spoed"):
		return strPtr("hoog")
	case strings.Contains(lower, "!laag"), strings.Contains(lower, "!low"):
		return strPtr("laag")
	case strings.Contains(lower, "!normaal"), strings.Contains(lower, "!normal"):
		return strPtr("normaal")
	default:
		return nil
	}
}

func inferNoteSymbol(content string, tags []string, priority *string, deadline *time.Time) *string {
	lower := strings.ToLower(content)
	if priority != nil && strings.EqualFold(*priority, "hoog") {
		return strPtr("warning")
	}
	if checklistTotal(content) > 0 || containsAny(lower, "todo", "actie", "moet", "bellen", "terugbellen", "mailen") {
		return strPtr("check")
	}
	if deadline != nil || containsAny(lower, "agenda", "afspraak", "datum", "vandaag", "morgen", "overmorgen") {
		return strPtr("calendar")
	}
	if containsAny(lower, "werk", "dienst", "rooster", "laventecare", "dkl") || hasTag(tags, "werk") {
		return strPtr("work")
	}
	if containsAny(lower, "geld", "salaris", "factuur", "bank", "betaling") || hasTag(tags, "finance") {
		return strPtr("finance")
	}
	if containsAny(lower, "habit", "gewoonte", "streak") {
		return strPtr("habit")
	}
	if containsAny(lower, "prive", "privé", "persoonlijk") {
		return strPtr("shield")
	}
	if containsAny(lower, "idee", "concept", "bedenk") || hasTag(tags, "idee") {
		return strPtr("sparkles")
	}
	return strPtr("note")
}

func inferNoteTriageFlag(content string, priority *string, deadline *time.Time, now time.Time) *bool {
	lower := strings.ToLower(content)
	triage := false
	if priority != nil && strings.EqualFold(*priority, "hoog") {
		triage = true
	}
	if deadline != nil && !deadline.After(now.AddDate(0, 0, 3)) {
		triage = true
	}
	if checklistTotal(content) > 0 || containsAny(lower, "todo", "moet", "vergeet niet", "belangrijk", "urgent", "bellen", "terugbellen", "actie") {
		triage = true
	}
	if !triage {
		return nil
	}
	return &triage
}

func inferNoteDeadline(content string, now time.Time, loc *time.Location) *time.Time {
	lower := strings.ToLower(content)
	hour, minute := 17, 0
	if match := noteTimeRe.FindStringSubmatch(content); len(match) == 3 {
		hour, _ = strconv.Atoi(match[1])
		minute, _ = strconv.Atoi(match[2])
	}

	var date time.Time
	switch {
	case strings.Contains(lower, "overmorgen"):
		date = now.AddDate(0, 0, 2)
	case strings.Contains(lower, "morgen"):
		date = now.AddDate(0, 0, 1)
	case strings.Contains(lower, "vandaag"):
		date = now
	default:
		if match := noteDateRe.FindStringSubmatch(content); len(match) >= 3 {
			day, _ := strconv.Atoi(match[1])
			month, _ := strconv.Atoi(match[2])
			year := now.Year()
			if len(match) >= 4 && match[3] != "" {
				parsedYear, _ := strconv.Atoi(match[3])
				if parsedYear < 100 {
					parsedYear += 2000
				}
				year = parsedYear
			}
			date = time.Date(year, time.Month(month), day, hour, minute, 0, 0, loc)
			return &date
		}
		return nil
	}

	deadline := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, loc)
	return &deadline
}

func cleanNoteTitle(content string) string {
	line := strings.TrimSpace(strings.SplitN(content, "\n", 2)[0])
	line = notePrefixRe.ReplaceAllString(line, "")
	line = noteHashtagRe.ReplaceAllString(line, "$1")
	line = notePriorityTokenRe.ReplaceAllString(line, " ")
	line = strings.TrimSpace(collapseWhitespace(line))
	return truncateRunes(line, 80)
}

func checklistTotal(content string) int {
	_, total := checklistProgress(content)
	return total
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func hasTag(tags []string, needle string) bool {
	for _, tag := range tags {
		if tag == needle {
			return true
		}
	}
	return false
}

func noteTitle(note model.Note) string {
	if note.Titel != nil && strings.TrimSpace(*note.Titel) != "" {
		return truncateRunes(strings.TrimSpace(*note.Titel), 80)
	}
	firstLine := strings.TrimSpace(strings.SplitN(note.Inhoud, "\n", 2)[0])
	if firstLine != "" {
		return truncateRunes(firstLine, 80)
	}
	return "Naamloze notitie"
}

func optionalNoteString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func buildNotesMenu() tg.InlineKeyboardMarkup {
	return tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "🧠 AI triage", CallbackData: "/noteai"},
				{Text: "📝 Cockpit", CallbackData: "/notities"},
			},
			{
				{Text: "🔎 Zoek", CallbackData: "/zoeknote"},
				{Text: "📍 Vandaag", CallbackData: "/vandaag"},
			},
			{
				{Text: "📅 Week", CallbackData: "/week"},
				{Text: "📋 Samenvat", CallbackData: "/notesamenvatting"},
			},
			{
				{Text: "🏠 Startmenu", CallbackData: "/start"},
			},
		},
	}
}

func buildNoteSearchHelpText() string {
	return "🔎 Notities zoeken\n\nGebruik:\n/zoeknote HenkeWonen\n/zoeknote #dkl\n/zoeknote evaluatie\n\nZoeken kijkt naar titel, inhoud, tags, prioriteit en symbool. Gebruik /noteai als je liever wilt dat AI de notities interpreteert."
}

func buildNoteHelpText() string {
	return "✍️ Slim noteren\n\nGebruik:\n/noteer [jouw notitie tekst] #tag !hoog\n\nVoorbeelden:\n/noteer Bel HenkeWonen morgen 11:00 #werk !hoog\n/noteer idee: dashboard start sneller maken #frontend\n/noteer DKL evaluatie voorbereiden #dkl\n\nAI herkent tags, prioriteit, triage, symbool en simpele deadlines zoals vandaag, morgen, overmorgen of 05-06-2026.\n\nCommands:\n/noteai — slimme triage\n/notetriage — actiepunten voor vandaag\n/notesamenvatting — groepeer per thema\n/zoeknote [term of #tag] — zoeken"
}
