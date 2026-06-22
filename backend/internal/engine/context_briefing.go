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
			"start":    schedule.StartTijd,
			"end":      schedule.EindTijd,
			"duration": schedule.Duur,
			"location": schedule.Locatie,
			"team":     schedule.Team,
		})
	}
	for _, event := range events {
		items = append(items, map[string]any{
			"type":   "afspraak",
			"id":     event.EventID,
			"title":  event.Titel,
			"date":   event.StartDatum,
			"start":  optionalPtrValue(event.StartTijd),
			"end":    optionalPtrValue(event.EindTijd),
			"allDay": event.Heledag,
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

func recommendedContextActions(notes []model.Note, events []model.PersonalEvent, cockpit *model.LCCockpit, unread int, now time.Time, loc *time.Location, limit int) []map[string]string {
	actions := make([]map[string]string, 0, limit)
	add := func(domain, title, reason string) {
		if len(actions) >= limit {
			return
		}
		actions = append(actions, map[string]string{"domain": domain, "title": title, "reason": reason})
	}

	for _, note := range selectFocusNotes(activeNotes(notes), now, loc, limit) {
		if noteNeedsAttention(note, now, loc) {
			add("notities", noteTitle(note), "triage/deadline/checklist vraagt aandacht")
		}
	}
	if unread > 0 {
		add("email", "Inbox review", "er staan ongelezen Gmail-items open")
	}
	if cockpit != nil {
		for _, followUp := range cockpit.FollowUps {
			add("laventecare", followUp.Title, followUp.ActionHint)
		}
		for _, signal := range cockpit.BusinessSignals {
			add("laventecare", signal.Title, signal.ActionHint)
		}
		for _, action := range cockpit.ActionItems {
			reason := action.Priority
			if action.DueDate != nil {
				reason += " · deadline " + *action.DueDate
			}
			add("laventecare", action.Title, reason)
		}
	}
	for _, event := range events {
		if event.Status == store.PersonalEventStatusPendingCreate || event.Status == store.PersonalEventStatusPendingUpdate || event.Status == store.PersonalEventStatusPendingDelete {
			add("agenda", event.Titel, "staat nog in Google Calendar sync-wachtrij")
		}
	}
	return actions
}

func optionalPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
