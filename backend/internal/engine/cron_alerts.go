package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

// appointmentReminderLeadMinutes is how far ahead of an appointment's start the
// "begint over ~X min" reminder fires.
const appointmentReminderLeadMinutes = 60

// cronDailyAgendaDigest sends a deterministic, once-a-morning overview of today
// (agenda, notes-with-deadline, LaventeCare actions and follow-ups). It reuses
// the SAME briefing_time preference + [t, t+15min) window as cronTelegramBriefing.
// Unlike the AI briefing, this is assembled deterministically from the stores so
// it always sends the same structured facts. The day is only claimed once there
// is at least one item to report, so an empty morning can still fire later if
// data appears (realistically it just skips the day) and never sends an empty
// digest.
func cronDailyAgendaDigest(e *Engine, cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		now := time.Now().In(amsterdam)

		// Time-window gate: reuse the user's briefing_time preference and the same
		// [target, target+15min) computation as cronTelegramBriefing.
		prefs, err := store.NewPreferencesStore(e.db.Pool).Get(ctx, cfg.UserID)
		if err != nil {
			slog.Warn("cronDailyAgendaDigest: failed to get user preferences", "error", err)
			return nil
		}
		briefingTime := "08:00"
		if prefs.BriefingTime != nil && *prefs.BriefingTime != "" {
			briefingTime = *prefs.BriefingTime
		}
		parts := strings.Split(briefingTime, ":")
		if len(parts) != 2 {
			slog.Warn("cronDailyAgendaDigest: invalid briefing_time format", "time", briefingTime)
			return nil
		}
		targetHour, _ := strconv.Atoi(parts[0])
		targetMinute, _ := strconv.Atoi(parts[1])
		targetMinutes := targetHour*60 + targetMinute
		currentMinutes := now.Hour()*60 + now.Minute()
		if currentMinutes < targetMinutes || currentMinutes >= targetMinutes+15 {
			return nil
		}

		// Build the digest first: if all sections are empty we neither claim nor
		// send, so the day's slot stays open.
		msg := e.buildDailyAgendaDigest(ctx, cfg, now)
		if msg == "" {
			return nil
		}

		// Claim the day only now that there's something to send — persistent (per
		// calendar day) so a redeploy inside the window can't double-fire.
		if !e.claimCronWindow(ctx, "daily-agenda-digest", now.Format("2006-01-02")) {
			return nil
		}

		slog.Info("🌅 cronDailyAgendaDigest: sending daily agenda digest", "time", now.Format("15:04"))
		if err := e.SendProactiveNotification(ctx, msg); err != nil {
			slog.Warn("cronDailyAgendaDigest: send failed", "error", err)
		}
		return nil
	}
}

// buildDailyAgendaDigest deterministically composes the morning overview from the
// same stores the AI briefing reads (personal_events, notes, LaventeCare
// cockpit). It returns "" when there is nothing to report. Per-domain query
// failures are logged and skipped so one broken store never suppresses the rest.
func (e *Engine) buildDailyAgendaDigest(ctx context.Context, cfg CronConfig, now time.Time) string {
	today := now.Format("2006-01-02")
	var sections []string

	// ── 📅 Agenda vandaag ──────────────────────────────────────────────────────
	events, err := store.NewPersonalEventStore(e.db).ListRange(ctx, cfg.UserID, today, today)
	if err != nil {
		slog.Warn("buildDailyAgendaDigest: agenda query failed", "error", err)
	} else {
		agenda := visiblePersonalEvents(events)
		sort.SliceStable(agenda, func(i, j int) bool {
			return agendaSortKey(agenda[i]) < agendaSortKey(agenda[j])
		})
		lines := make([]string, 0, len(agenda))
		for _, ev := range agenda {
			lines = append(lines, "• "+formatAgendaLine(ev))
		}
		if len(lines) > 0 {
			sections = append(sections, "📅 Agenda vandaag:\n"+strings.Join(lines, "\n"))
		}
	}

	// ── 📝 Notes met deadline vandaag of eerder ────────────────────────────────
	notes, err := store.NewNoteStore(e.db).List(ctx, cfg.UserID)
	if err != nil {
		slog.Warn("buildDailyAgendaDigest: notes query failed", "error", err)
	} else {
		due := make([]model.Note, 0)
		for _, note := range openNotes(notes) {
			if note.Deadline == nil {
				continue
			}
			if note.Deadline.In(amsterdam).Format("2006-01-02") <= today {
				due = append(due, note)
			}
		}
		sort.SliceStable(due, func(i, j int) bool { return due[i].Deadline.Before(*due[j].Deadline) })
		lines := make([]string, 0, len(due))
		for _, note := range due {
			lines = append(lines, fmt.Sprintf("• %s (%s)", noteTitle(note), lateLabel(note.Deadline.In(amsterdam), now)))
		}
		if len(lines) > 0 {
			sections = append(sections, "📝 Notes:\n"+strings.Join(lines, "\n"))
		}
	}

	// ── ✅ Acties + 🎯 Follow-ups (LaventeCare cockpit) ─────────────────────────
	cockpit, err := store.NewLaventeCareStore(e.db).GetCockpit(ctx, cfg.UserID)
	if err != nil {
		slog.Warn("buildDailyAgendaDigest: laventecare cockpit query failed", "error", err)
	} else if cockpit != nil {
		// Action items with a due_date today or overdue (ListActions already
		// returns only open/bezig/wacht_op_klant items).
		type dueAction struct {
			title string
			due   time.Time
		}
		acts := make([]dueAction, 0)
		for _, a := range cockpit.ActionItems {
			if a.DueDate == nil || strings.TrimSpace(*a.DueDate) == "" || *a.DueDate > today {
				continue
			}
			due, perr := time.ParseInLocation("2006-01-02", *a.DueDate, amsterdam)
			if perr != nil {
				continue
			}
			acts = append(acts, dueAction{title: strings.TrimSpace(a.Title), due: due})
		}
		sort.SliceStable(acts, func(i, j int) bool { return acts[i].due.Before(acts[j].due) })
		if len(acts) > 0 {
			lines := make([]string, 0, len(acts))
			for _, a := range acts {
				lines = append(lines, fmt.Sprintf("• %s (%s)", a.title, lateLabel(a.due, now)))
			}
			sections = append(sections, "✅ Acties:\n"+strings.Join(lines, "\n"))
		}

		// Follow-ups (leads/opdrachten/projecten/klanten) whose deadline or
		// next-action lands today or earlier. FollowUps are already date-sorted.
		type dueFollowUp struct {
			title string
			due   time.Time
		}
		fus := make([]dueFollowUp, 0)
		for _, f := range cockpit.FollowUps {
			if strings.TrimSpace(f.Date) == "" || f.Date > today {
				continue
			}
			due, perr := time.ParseInLocation("2006-01-02", f.Date, amsterdam)
			if perr != nil {
				continue
			}
			fus = append(fus, dueFollowUp{title: strings.TrimSpace(f.Title), due: due})
		}
		sort.SliceStable(fus, func(i, j int) bool { return fus[i].due.Before(fus[j].due) })
		if len(fus) > 0 {
			lines := make([]string, 0, len(fus))
			for _, f := range fus {
				lines = append(lines, fmt.Sprintf("• %s (%s)", f.title, lateLabel(f.due, now)))
			}
			sections = append(sections, "🎯 Follow-ups:\n"+strings.Join(lines, "\n"))
		}
	}

	// ── 💶 Facturen: openstaande/verlopen facturen ─────────────────────────────
	invoices, err := store.NewLaventeCareStore(e.db).ListInvoices(ctx, cfg.UserID, 0, nil)
	if err != nil {
		slog.Warn("buildDailyAgendaDigest: invoices query failed", "error", err)
	} else {
		type dueInvoice struct {
			line string
			due  time.Time
		}
		invs := make([]dueInvoice, 0)
		for _, inv := range invoices {
			switch inv.Status {
			case "betaald", "geannuleerd", "concept":
				continue
			}
			if inv.DueDate == nil || strings.TrimSpace(*inv.DueDate) == "" || *inv.DueDate > today {
				continue
			}
			due, perr := time.ParseInLocation("2006-01-02", *inv.DueDate, amsterdam)
			if perr != nil {
				continue
			}
			outstanding := inv.TotalCents - inv.PaidCents
			invs = append(invs, dueInvoice{
				line: fmt.Sprintf("• %s — %s (%s)", strings.TrimSpace(inv.InvoiceNumber), formatEURCents(outstanding), lateLabel(due, now)),
				due:  due,
			})
		}
		sort.SliceStable(invs, func(i, j int) bool { return invs[i].due.Before(invs[j].due) })
		if len(invs) > 0 {
			lines := make([]string, 0, len(invs))
			for _, inv := range invs {
				lines = append(lines, inv.line)
			}
			sections = append(sections, "💶 Facturen:\n"+strings.Join(lines, "\n"))
		}
	}

	// ── 🚨 SLA: open incidenten op of vlak vóór hun reactiedeadline ─────────────
	incidents, err := store.NewLaventeCareStore(e.db).ListSlaIncidents(ctx, cfg.UserID, 20)
	if err != nil {
		slog.Warn("buildDailyAgendaDigest: sla incidents query failed", "error", err)
	} else {
		type dueIncident struct {
			line     string
			deadline time.Time
		}
		incs := make([]dueIncident, 0)
		cutoff := now.Add(24 * time.Hour)
		for _, inc := range incidents {
			if inc.ReactieDeadline == nil {
				continue
			}
			deadline := inc.ReactieDeadline.In(amsterdam)
			if deadline.After(cutoff) {
				continue
			}
			var urgency string
			switch until := deadline.Sub(now); {
			case until <= 0:
				urgency = "deadline verlopen"
			case until < time.Hour:
				urgency = fmt.Sprintf("binnen %d min", int(until.Minutes())+1)
			default:
				urgency = fmt.Sprintf("binnen %d uur", int(until.Hours()))
			}
			incs = append(incs, dueIncident{
				line:     fmt.Sprintf("• %s [%s] — %s", strings.TrimSpace(inc.Titel), strings.TrimSpace(inc.Prioriteit), urgency),
				deadline: deadline,
			})
		}
		// Soonest/most-overdue deadline first — breached incidents (earliest) lead.
		sort.SliceStable(incs, func(i, j int) bool { return incs[i].deadline.Before(incs[j].deadline) })
		if len(incs) > 0 {
			lines := make([]string, 0, len(incs))
			for _, inc := range incs {
				lines = append(lines, inc.line)
			}
			sections = append(sections, "🚨 SLA:\n"+strings.Join(lines, "\n"))
		}
	}

	if len(sections) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🌅 Overzicht %s %d %s", dutchDayName(now.Weekday()), now.Day(), dutchMonthName(now.Month())))
	for _, sec := range sections {
		b.WriteString("\n\n" + sec)
	}
	return b.String()
}

// cronAppointmentReminders alerts once for each non-all-day appointment whose
// start falls in the (now, now+lead] window, so a "begint over ~X min" nudge
// arrives shortly before it. It runs all day (SendProactiveNotification handles
// quiet hours). Each appointment is de-duplicated by event_id via claimCronWindow
// so it is alerted exactly once.
func cronAppointmentReminders(e *Engine, cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		now := time.Now().In(amsterdam)
		windowEnd := now.Add(appointmentReminderLeadMinutes * time.Minute)

		// Query today's events, plus tomorrow's when the lead window crosses
		// midnight, then filter to the exact time window in Go.
		evStore := store.NewPersonalEventStore(e.db)
		dates := []string{now.Format("2006-01-02")}
		if endDay := windowEnd.Format("2006-01-02"); endDay != dates[0] {
			dates = append(dates, endDay)
		}
		var events []model.PersonalEvent
		for _, d := range dates {
			evs, err := evStore.ListRange(ctx, cfg.UserID, d, d)
			if err != nil {
				slog.Warn("cronAppointmentReminders: query failed", "date", d, "error", err)
				return nil
			}
			events = append(events, evs...)
		}
		events = visiblePersonalEvents(events)

		for _, ev := range events {
			if ev.Heledag || ev.StartTijd == nil || strings.TrimSpace(*ev.StartTijd) == "" {
				continue
			}
			start, err := time.ParseInLocation("2006-01-02 15:04", ev.StartDatum+" "+clockHM(*ev.StartTijd), amsterdam)
			if err != nil {
				continue
			}
			// Window is (now, now+lead]: skip already-started and too-far-out events.
			if !start.After(now) || start.After(windowEnd) {
				continue
			}
			// One alert per appointment ever — persistent so a redeploy can't
			// re-notify. Only send when the claim succeeds.
			if !e.claimCronWindow(ctx, "appt-reminder", ev.EventID) {
				continue
			}
			mins := int(start.Sub(now).Minutes())
			if mins < 1 {
				mins = 1
			}
			if err := e.SendProactiveNotification(ctx, formatAppointmentReminder(ev, mins)); err != nil {
				slog.Warn("cronAppointmentReminders: send failed", "eventId", ev.EventID, "error", err)
			}
		}
		return nil
	}
}

// formatAgendaLine renders one today-agenda line: "<HH:MM> <titel>[ — <locatie
// of CRM-context>]" (all-day events show "Hele dag" instead of a clock).
func formatAgendaLine(ev model.PersonalEvent) string {
	timePart := "Hele dag"
	if !ev.Heledag && ev.StartTijd != nil && strings.TrimSpace(*ev.StartTijd) != "" {
		timePart = clockHM(*ev.StartTijd)
	}
	line := timePart + " " + strings.TrimSpace(ev.Titel)
	if extra := agendaContext(ev); extra != "" {
		line += " — " + extra
	}
	return line
}

// formatAppointmentReminder renders the "begint over ~X min" reminder line.
func formatAppointmentReminder(ev model.PersonalEvent, mins int) string {
	start := ""
	if ev.StartTijd != nil {
		start = clockHM(*ev.StartTijd)
	}
	msg := fmt.Sprintf("⏰ Over ~%d min: %s (%s)", mins, strings.TrimSpace(ev.Titel), start)
	if ev.Locatie != nil && strings.TrimSpace(*ev.Locatie) != "" {
		msg += " — " + strings.TrimSpace(*ev.Locatie)
	}
	if ev.BusinessContextTitle != nil && strings.TrimSpace(*ev.BusinessContextTitle) != "" {
		msg += " · " + strings.TrimSpace(*ev.BusinessContextTitle)
	}
	return msg
}

// agendaContext prefers the event's location, falling back to its CRM/business
// context title, for the trailing "— …" hint on an agenda line.
func agendaContext(ev model.PersonalEvent) string {
	if ev.Locatie != nil && strings.TrimSpace(*ev.Locatie) != "" {
		return strings.TrimSpace(*ev.Locatie)
	}
	if ev.BusinessContextTitle != nil && strings.TrimSpace(*ev.BusinessContextTitle) != "" {
		return strings.TrimSpace(*ev.BusinessContextTitle)
	}
	return ""
}

// agendaSortKey sorts today's agenda soonest-first; all-day events sort ahead of
// timed ones.
func agendaSortKey(ev model.PersonalEvent) string {
	if ev.Heledag || ev.StartTijd == nil {
		return "00:00"
	}
	return clockHM(*ev.StartTijd)
}

// clockHM normalizes a stored time value to "HH:MM" (handles "HH:MM:SS").
func clockHM(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 5 {
		return s[:5]
	}
	return s
}

// lateLabel renders how overdue a due-date is relative to now: "vandaag" when it
// falls today, otherwise "N dag(en) te laat".
func lateLabel(due, now time.Time) string {
	days := daysBetween(due, now)
	if days <= 0 {
		return "vandaag"
	}
	return pluralNL(days, "dag", "dagen") + " te laat"
}

// daysBetween returns the whole-day difference (to - from), DST-safe by rounding.
func daysBetween(from, to time.Time) int {
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	to = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, to.Location())
	return int(to.Sub(from).Hours()/24 + 0.5)
}

// formatEURCents renders an amount in cents as a Dutch-formatted euro string,
// e.g. 123456 → "€ 1.234,56" (thousands grouped with ".", decimals with ",").
func formatEURCents(cents int) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	euros := strconv.Itoa(cents / 100)
	var grouped strings.Builder
	n := len(euros)
	for i, ch := range euros {
		if i > 0 && (n-i)%3 == 0 {
			grouped.WriteByte('.')
		}
		grouped.WriteRune(ch)
	}
	out := fmt.Sprintf("€ %s,%02d", grouped.String(), cents%100)
	if neg {
		out = "-" + out
	}
	return out
}
