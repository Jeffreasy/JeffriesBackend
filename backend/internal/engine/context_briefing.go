package engine

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type contextBriefingOptions struct {
	Scope string `json:"scope"`
	Dagen int    `json:"dagen"`
	Limit int    `json:"limit"`
}

func (e *HomeBotExecutor) buildContextBriefing(ctx context.Context, opts contextBriefingOptions) (map[string]any, error) {
	loc := amsterdamLocation()
	now := time.Now().In(loc)
	scope := normalizeBriefingScope(opts.Scope)
	days := clampToolLimit(opts.Dagen, defaultBriefingDays(scope), 14)
	limit := clampToolLimit(opts.Limit, 5, 12)

	start := startOfLocalDay(now)
	if scope == "morgen" {
		start = start.AddDate(0, 0, 1)
	}
	end := start.AddDate(0, 0, days-1)
	startIso := start.Format("2006-01-02")
	endIso := end.Format("2006-01-02")

	var errors []string
	recordErr := func(err error) {
		if err != nil {
			errors = append(errors, err.Error())
		}
	}

	schedules, err := e.scheduleStore.ListRange(ctx, e.userID, startIso, endIso)
	recordErr(err)
	schedules = visibleSchedules(schedules)

	events, err := e.personalEvStore.ListRange(ctx, e.userID, startIso, endIso)
	recordErr(err)
	events = visiblePersonalEvents(events)

	notes, err := e.noteStore.List(ctx, e.userID)
	recordErr(err)
	noteSnapshot := map[string]any{}
	if err == nil {
		noteSnapshot = compactNotesSnapshot(notes, now, loc, limit)
	}

	emails, err := e.emailStore.List(ctx, e.userID, limit, 0)
	recordErr(err)
	unread, unreadErr := e.emailStore.CountUnread(ctx, e.userID)
	recordErr(unreadErr)
	emailMeta, metaErr := e.emailStore.GetSyncMeta(ctx, e.userID)
	recordErr(metaErr)

	cockpit, err := e.laventeCareStore.GetCockpit(ctx, e.userID)
	recordErr(err)
	var dossierAdvice *model.LCDossierAdvice
	if scope == "laventecare" {
		dossierAdvice, err = e.laventeCareStore.BuildDossierAdvice(ctx, e.userID, model.LCDossierAdviceRequest{Query: "laventecare", Limit: limit})
		recordErr(err)
	}

	scheduleMeta, scheduleMetaErr := e.scheduleStore.GetMeta(ctx, e.userID)
	recordErr(scheduleMetaErr)

	return map[string]any{
		"scope":       scope,
		"generatedAt": now.Format(time.RFC3339),
		"periode": map[string]string{
			"startIso": startIso,
			"eindIso":  endIso,
			"label":    periodLabel(start, end, loc),
		},
		"sync": map[string]any{
			"scheduleImportedAt":     scheduleMetaValue(scheduleMeta, "importedAt"),
			"scheduleTotalRows":      scheduleMetaValue(scheduleMeta, "totalRows"),
			"gmailLastSuccessAt":     emailMetaValue(emailMeta, "updatedAt"),
			"gmailLastFullSync":      emailMetaValue(emailMeta, "lastFullSync"),
			"gmailLastSuccessfulCnt": emailMetaValue(emailMeta, "totalSynced"),
			"gmailHistoryIDSet":      emailMeta != nil && strings.TrimSpace(emailMeta.HistoryID) != "",
			// CURRENT health — the only authoritative ok/failed signal. The count
			// above is historical (last success) and must not be read as "ok now".
			"gmailSyncStatus": emailSyncStatus(emailMeta),
			"gmailLastError":  emailSyncLastError(emailMeta),
			"instruction":     "gmailSyncStatus is de enige bron voor huidige sync-gezondheid. Rapporteer Gmail-sync alleen als 'ok' wanneer gmailSyncStatus == 'ok'; bij 'failed' meld je de storing met gmailLastError. gmailLastSuccessfulCnt is historisch, geen bewijs van huidige werking.",
		},
		"planning": map[string]any{
			"aantalDiensten":  len(schedules),
			"aantalAfspraken": len(events),
			"totaalUur":       totalScheduleHours(schedules),
			"eerstvolgend":    compactNextPlanning(schedules, events, limit),
			"laventecare":     filterBusinessEvents(events, limit),
		},
		"email": map[string]any{
			"unread":      unread,
			"recent":      compactEmails(emails),
			"instruction": "Gebruik recente e-mails als signaal, en roep leesEmail aan als de gebruiker inhoudelijke details of een antwoord wil.",
		},
		"notes":       noteSnapshot,
		"laventecare": compactLaventeCare(cockpit, dossierAdvice, limit),
		"actions":     recommendedContextActions(notes, events, cockpit, unread, now, loc, limit),
		"errors":      errors,
		"instruction": "Dit is de cross-domain live briefing voor Telegram/Grok. Combineer planning, email, notities en LaventeCare; zeg niet dat data ontbreekt wanneer de bijbehorende aantallen groter zijn dan 0.",
	}, nil
}

func emailSyncStatus(m *model.EmailSyncMeta) string {
	if m == nil || strings.TrimSpace(m.SyncStatus) == "" {
		return "unknown"
	}
	return m.SyncStatus
}

func emailSyncLastError(m *model.EmailSyncMeta) string {
	if m == nil {
		return ""
	}
	return m.LastError
}

func normalizeBriefingScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "laventecare", "business", "bedrijf":
		return "laventecare"
	case "week", "7d", "zeven dagen":
		return "week"
	case "morgen", "tomorrow":
		return "morgen"
	default:
		return "vandaag"
	}
}

func defaultBriefingDays(scope string) int {
	switch scope {
	case "week", "laventecare":
		return 7
	case "morgen":
		return 1
	default:
		return 1
	}
}

func startOfLocalDay(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

func periodLabel(start, end time.Time, loc *time.Location) string {
	if start.Format("2006-01-02") == end.Format("2006-01-02") {
		return start.In(loc).Format("2006-01-02")
	}
	return start.In(loc).Format("2006-01-02") + " t/m " + end.In(loc).Format("2006-01-02")
}

func compactNotesSnapshot(notes []model.Note, now time.Time, loc *time.Location, limit int) map[string]any {
	active := activeNotes(notes)
	stats := buildNoteStats(active, now, loc)
	focusNotes := selectFocusNotes(active, now, loc, limit)
	focus := make([]map[string]any, 0, len(focusNotes))
	for _, note := range focusNotes {
		focus = append(focus, noteAIItem(note, now, loc))
	}
	return map[string]any{
		"stats": map[string]any{
			"active":    stats.Active,
			"today":     stats.Today,
			"pinned":    stats.Pinned,
			"completed": stats.Completed,
			"attention": stats.Attention,
			"topTags":   stats.TopTags,
		},
		"focus": focus,
	}
}

func totalScheduleHours(schedules []model.Schedule) float64 {
	var total float64
	for _, schedule := range schedules {
		total += schedule.Duur
	}
	return total
}

func compactNextPlanning(schedules []model.Schedule, events []model.PersonalEvent, limit int) []map[string]any {
	items := make([]map[string]any, 0, len(schedules)+len(events))
	for _, schedule := range schedules {
		items = append(items, map[string]any{
			"type":     "dienst",
			"title":    firstNonEmpty(schedule.ShiftType, schedule.Titel),
			"date":     schedule.StartDatum,
			"weekday":  weekdayLabel(schedule.StartDatum),
			"start":    schedule.StartTijd,
			"end":      schedule.EindTijd,
			"duration": schedule.Duur,
			"location": schedule.Locatie,
			"team":     schedule.Team,
		})
	}
	for _, event := range events {
		items = append(items, map[string]any{
			"type":    "afspraak",
			"id":      event.EventID,
			"title":   event.Titel,
			"date":    event.StartDatum,
			"weekday": weekdayLabel(event.StartDatum),
			"start":   optionalPtrValue(event.StartTijd),
			"end":     optionalPtrValue(event.EindTijd),
			"allDay":  event.Heledag,
			"businessContext": map[string]any{
				"type":  optionalPtrValue(event.BusinessContextType),
				"id":    optionalPtrValue(event.BusinessContextID),
				"title": optionalPtrValue(event.BusinessContextTitle),
			},
			"status": event.Status,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return planningSortKey(items[i]) < planningSortKey(items[j])
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

// weekdayLabel returns the Dutch weekday name for an ISO date, or "" if it
// doesn't parse. This is defense-in-depth beyond the prompt-level 14-day
// lookup table (ai/prompt.go): the model can read the day directly off the
// tool payload instead of having to cross-reference a bare date against that
// table itself.
func weekdayLabel(dateIso string) string {
	loc := amsterdamLocation()
	d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateIso), loc)
	if err != nil {
		return ""
	}
	return dutchDayName(d.Weekday())
}

func planningSortKey(item map[string]any) string {
	date, _ := item["date"].(string)
	start, _ := item["start"].(string)
	if start == "" {
		start = "00:00"
	}
	return date + " " + start
}

func filterBusinessEvents(events []model.PersonalEvent, limit int) []map[string]any {
	items := make([]map[string]any, 0)
	for _, event := range events {
		businessType := strings.TrimSpace(optionalPtrValue(event.BusinessContextType))
		title := strings.ToLower(event.Titel)
		if businessType == "" && !strings.Contains(title, "laventecare") {
			continue
		}
		items = append(items, map[string]any{
			"id":      event.EventID,
			"title":   event.Titel,
			"date":    event.StartDatum,
			"start":   optionalPtrValue(event.StartTijd),
			"status":  event.Status,
			"context": optionalPtrValue(event.BusinessContextTitle),
		})
		if len(items) >= limit {
			break
		}
	}
	return items
}

func compactEmails(emails []model.Email) []map[string]any {
	items := make([]map[string]any, 0, len(emails))
	for _, email := range emails {
		items = append(items, map[string]any{
			"gmailId":     email.GmailID,
			"from":        email.FromAddr,
			"subject":     email.Subject,
			"snippet":     truncateRunes(email.Snippet, 160),
			"date":        email.Datum,
			"unread":      !email.IsGelezen,
			"starred":     email.IsSter,
			"attachments": email.HeeftBijlagen,
		})
	}
	return items
}

func compactLaventeCare(cockpit *model.LCCockpit, dossierAdvice *model.LCDossierAdvice, limit int) map[string]any {
	if cockpit == nil {
		return map[string]any{"available": false}
	}
	var advice any
	if dossierAdvice != nil {
		advice = map[string]any{
			"status":          dossierAdvice.Status,
			"coverage":        dossierAdvice.Coverage,
			"requirements":    takeBriefingItems(dossierAdvice.Requirements, limit),
			"recommendations": takeBriefingItems(dossierAdvice.Recommendations, limit),
			"nextActions":     takeBriefingItems(dossierAdvice.NextActions, limit),
		}
	}
	return map[string]any{
		"available":         true,
		"summary":           cockpit.Summary,
		"companies":         takeBriefingItems(cockpit.Companies, limit),
		"contacts":          minimizeBriefingContacts(takeBriefingItems(cockpit.Contacts, limit)),
		"activeLeads":       takeBriefingItems(cockpit.ActiveLeads, limit),
		"activeWorkstreams": takeBriefingItems(cockpit.ActiveWorkstreams, limit),
		"activeProjects":    takeBriefingItems(cockpit.ActiveProjects, limit),
		"actions":           takeBriefingItems(cockpit.ActionItems, limit),
		"signals":           takeBriefingItems(cockpit.BusinessSignals, limit),
		"followUps":         takeBriefingItems(cockpit.FollowUps, limit),
		"dossierRecent":     takeBriefingItems(cockpit.DossierDocuments, limit),
		"dossierAdvice":     advice,
		"documentsSeeded":   cockpit.Summary.DocumentsSeeded,
	}
}

// minimizeBriefingContacts strips direct PII (email, phone, free-text notes)
// from contacts before they enter the model prompt; name/role/company is enough
// context for the assistant to reason about who to follow up with.
func minimizeBriefingContacts(contacts []model.LCContact) []map[string]any {
	out := make([]map[string]any, 0, len(contacts))
	for _, c := range contacts {
		out = append(out, map[string]any{
			"naam":         c.Naam,
			"rol":          c.Rol,
			"isPrimary":    c.IsPrimary,
			"companyId":    c.CompanyID,
			"decisionRole": c.DecisionRole,
		})
	}
	return out
}

func takeBriefingItems[T any](items []T, limit int) []T {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

type scoredContextAction struct {
	domain string
	title  string
	reason string
	score  int
}

// recommendedContextActions gathers ALL candidate attention-items across
// every domain first, scores each on a shared urgency scale, and only THEN
// truncates to limit. Previously this appended in a fixed domain order
// (notes, then email, then LaventeCare, then agenda) and hard-stopped the
// moment `limit` candidates existed — so on a day with several
// notes-needing-attention, a same-day LaventeCare deadline could be dropped
// from the pool before the model ever saw it, purely due to domain order,
// not actual urgency.
func recommendedContextActions(notes []model.Note, events []model.PersonalEvent, cockpit *model.LCCockpit, unread int, now time.Time, loc *time.Location, limit int) []map[string]string {
	var candidates []scoredContextAction

	for _, note := range activeNotes(notes) {
		if !noteNeedsAttention(note, now, loc) {
			continue
		}
		candidates = append(candidates, scoredContextAction{
			domain: "notities",
			title:  noteTitle(note),
			reason: "triage/deadline/checklist vraagt aandacht",
			score:  noteUrgencyScore(note, now, loc),
		})
	}
	if unread > 0 {
		candidates = append(candidates, scoredContextAction{
			domain: "email",
			title:  "Inbox review",
			reason: "er staan ongelezen Gmail-items open",
			score:  20,
		})
	}
	if cockpit != nil {
		for _, followUp := range cockpit.FollowUps {
			candidates = append(candidates, scoredContextAction{
				domain: "laventecare",
				title:  followUp.Title,
				reason: followUp.ActionHint,
				score:  domainUrgencyScore(followUp.Priority, "", now, loc, 15),
			})
		}
		for _, signal := range cockpit.BusinessSignals {
			candidates = append(candidates, scoredContextAction{
				domain: "laventecare",
				title:  signal.Title,
				reason: signal.ActionHint,
				score:  domainUrgencyScore(signal.Urgency, "", now, loc, 15),
			})
		}
		for _, action := range cockpit.ActionItems {
			reason := action.Priority
			dueIso := ""
			if action.DueDate != nil {
				dueIso = *action.DueDate
				reason += " · deadline " + dueIso
			}
			candidates = append(candidates, scoredContextAction{
				domain: "laventecare",
				title:  action.Title,
				reason: reason,
				score:  domainUrgencyScore(action.Priority, dueIso, now, loc, 15),
			})
		}
	}
	for _, event := range events {
		if event.Status == store.PersonalEventStatusPendingCreate || event.Status == store.PersonalEventStatusPendingUpdate || event.Status == store.PersonalEventStatusPendingDelete {
			candidates = append(candidates, scoredContextAction{
				domain: "agenda",
				title:  event.Titel,
				reason: "staat nog in Google Calendar sync-wachtrij",
				score:  25,
			})
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	actions := make([]map[string]string, 0, len(candidates))
	for _, c := range candidates {
		actions = append(actions, map[string]string{"domain": c.domain, "title": c.title, "reason": c.reason})
	}
	return actions
}

// domainUrgencyScore scores a non-note candidate on the same rough scale as
// noteUrgencyScore (telegram_notes.go), so items from different domains are
// comparable before truncation. baseline lets a domain be weighted relative
// to others even with no priority/due-date signal.
func domainUrgencyScore(priority, dueDateIso string, now time.Time, loc *time.Location, baseline int) int {
	score := baseline
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "hoog", "high", "urgent":
		score += 45
	case "normaal", "normal", "medium":
		score += 15
	case "laag", "low":
		score += 5
	}
	if dueDateIso = strings.TrimSpace(dueDateIso); dueDateIso != "" {
		if due, err := time.ParseInLocation("2006-01-02", dueDateIso, loc); err == nil {
			hours := due.Sub(startOfLocalDay(now)).Hours()
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
	}
	return score
}

func optionalPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
