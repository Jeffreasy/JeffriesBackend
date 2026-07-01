package handler

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type FocusHandler struct {
	db  *store.DB
	cfg *config.Config
}

func NewFocusHandler(db *store.DB, cfg *config.Config) *FocusHandler {
	return &FocusHandler{db: db, cfg: cfg}
}

type FocusSummary struct {
	UserID      string              `json:"userId"`
	GeneratedAt string              `json:"generatedAt"`
	Timezone    string              `json:"timezone"`
	Date        string              `json:"date"`
	Time        string              `json:"time"`
	Period      string              `json:"period"`
	Health      FocusHealth         `json:"health"`
	Sync        FocusSyncSummary    `json:"sync"`
	Counts      FocusCounts         `json:"counts"`
	Business    FocusBusinessStatus `json:"business"`
	Attention   []FocusAttention    `json:"attention"`
	Errors      []string            `json:"errors,omitempty"`
}

type FocusHealth struct {
	DevicesTotal       int        `json:"devicesTotal"`
	DevicesOnline      int        `json:"devicesOnline"`
	DevicesOn          int        `json:"devicesOn"`
	DevicesOffline     int        `json:"devicesOffline"`
	BridgeOnline       bool       `json:"bridgeOnline"`
	BridgeStatus       string     `json:"bridgeStatus"`
	BridgeLastSeenAt   *time.Time `json:"bridgeLastSeenAt,omitempty"`
	CommandsPending    int        `json:"commandsPending"`
	CommandsProcessing int        `json:"commandsProcessing"`
	CommandsFailed     int        `json:"commandsFailed"`
}

type FocusSyncSummary struct {
	Schedule FocusSyncTarget `json:"schedule"`
	Personal FocusSyncTarget `json:"personal"`
	Gmail    FocusSyncTarget `json:"gmail"`
}

type FocusSyncTarget struct {
	Status        string     `json:"status"`
	Enabled       bool       `json:"enabled"`
	Configured    bool       `json:"configured"`
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
	Total         int        `json:"total,omitempty"`
	Pending       int        `json:"pending,omitempty"`
}

type FocusCounts struct {
	ScheduleTotal    int `json:"scheduleTotal"`
	ScheduleUpcoming int `json:"scheduleUpcoming"`
	PersonalUpcoming int `json:"personalUpcoming"`
	PersonalPending  int `json:"personalPending"`
	NotesActive      int `json:"notesActive"`
	NotesPinned      int `json:"notesPinned"`
	NotesOverdue     int `json:"notesOverdue"`
	NotesDueToday    int `json:"notesDueToday"`
	NotesTriage      int `json:"notesTriage"`
	HabitsActive     int `json:"habitsActive"`
	HabitsTodayDue   int `json:"habitsTodayDue"`
	HabitsCompleted  int `json:"habitsCompleted"`
	UnreadEmails     int `json:"unreadEmails"`
}

type FocusBusinessStatus struct {
	ActiveLeads       int `json:"activeLeads"`
	ActiveWorkstreams int `json:"activeWorkstreams"`
	ActiveProjects    int `json:"activeProjects"`
	OpenActions       int `json:"openActions"`
	OverdueActions    int `json:"overdueActions"`
	OpenQuotes        int `json:"openQuotes"`
	OpenInvoices      int `json:"openInvoices"`
	OutstandingCents  int `json:"outstandingCents"`
}

type FocusAttention struct {
	ID       string `json:"id"`
	Domain   string `json:"domain"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Href     string `json:"href,omitempty"`
}

// Summary returns a compact, read-only status snapshot for the 24/7 Focus page.
// @Summary Get Focus summary
// @Description Returns a compact read-only cross-domain status snapshot for the tablet focus dashboard
// @Tags Focus
// @Produce json
// @Param userId query string false "User ID"
// @Success 200 {object} handler.FocusSummary
// @Router /focus/summary [get]
func (h *FocusHandler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if userID == "" {
		userID = h.cfg.HomeappUserID
	}

	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	today := now.Format("2006-01-02")

	errors := []string{}
	agg := h.focusAggregateCounts(ctx, userID, today, &errors)
	health := h.focusHealth(ctx, now, agg, &errors)
	counts := h.focusCounts(ctx, userID, agg, &errors)
	business := focusBusinessFromAggregate(agg)
	sync := h.focusSync(ctx, userID, counts.PersonalPending, counts.UnreadEmails, &errors)
	attention := buildFocusAttention(health, counts, business, sync)

	JSON(w, http.StatusOK, FocusSummary{
		UserID:      userID,
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Timezone:    loc.String(),
		Date:        today,
		Time:        now.Format("15:04"),
		Period:      today[:7],
		Health:      health,
		Sync:        sync,
		Counts:      counts,
		Business:    business,
		Attention:   attention,
		Errors:      errors,
	})
}

// bridgeOfflineThreshold: the bridge claims commands every ~2s, so a healthy
// bridge should never be silent for more than a few minutes.
const bridgeOfflineThreshold = 3 * time.Minute

// focusAggregate carries every scalar count for the focus summary. All values
// come from ONE SELECT with subqueries (see focusAggregateCounts) instead of
// ~20 sequential round-trips per poll (M8).
type focusAggregate struct {
	devicesTotal, devicesOnline, devicesOn           int
	commandsPending, commandsProcessing              int
	commandsFailed                                   int
	scheduleTotal, scheduleUpcoming                  int
	personalUpcoming, personalPending                int
	unreadEmails                                     int
	notesActive, notesPinned, notesDueToday          int
	notesOverdue, notesTriage                        int
	lcLeads, lcWorkstreams, lcProjects               int
	lcOpenActions, lcOverdueActions                  int
	lcOpenQuotes, lcOpenInvoices, lcOutstandingCents int
}

func (h *FocusHandler) focusAggregateCounts(ctx context.Context, userID, today string, errs *[]string) focusAggregate {
	var a focusAggregate
	err := h.db.Pool.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM devices),
  (SELECT COUNT(*) FROM devices WHERE status = 'online'),
  (SELECT COUNT(*) FROM devices WHERE current_state->>'on' = 'true'),
  (SELECT COUNT(*) FROM device_commands WHERE status = 'pending'),
  (SELECT COUNT(*) FROM device_commands WHERE status = 'processing'),
  (SELECT COUNT(*) FROM device_commands WHERE status = 'failed' AND COALESCE(completed_at, updated_at) > now() - interval '24 hours'),
  (SELECT COUNT(*) FROM schedule WHERE user_id = $1),
  (SELECT COUNT(*) FROM schedule WHERE user_id = $1 AND start_datum >= $2::date AND UPPER(COALESCE(status, '')) <> 'VERWIJDERD'),
  (SELECT COUNT(*) FROM personal_events WHERE user_id = $1 AND eind_datum >= $2::date AND status NOT IN ('VERWIJDERD', 'cancelled', 'PendingDelete')),
  (SELECT COUNT(*) FROM personal_events WHERE user_id = $1 AND status IN ('PendingCreate', 'PendingUpdate', 'PendingDelete')),
  (SELECT COUNT(*) FROM emails WHERE user_id = $1 AND NOT is_gelezen AND NOT is_verwijderd),
  (SELECT COUNT(*) FROM notes WHERE user_id = $1 AND NOT is_archived AND NOT is_completed),
  (SELECT COUNT(*) FROM notes WHERE user_id = $1 AND NOT is_archived AND NOT is_completed AND is_pinned),
  (SELECT COUNT(*) FROM notes WHERE user_id = $1 AND NOT is_archived AND NOT is_completed AND deadline::date = $2::date),
  (SELECT COUNT(*) FROM notes WHERE user_id = $1 AND NOT is_archived AND NOT is_completed AND deadline IS NOT NULL AND deadline::date < $2::date),
  (SELECT COUNT(*) FROM notes WHERE user_id = $1 AND NOT is_archived AND NOT is_completed AND COALESCE(triage_flag, false) = true),
  (SELECT COUNT(*) FROM lc_leads WHERE user_id = $1 AND status NOT IN `+lcClosedStatuses+`),
  (SELECT COUNT(*) FROM lc_workstreams WHERE user_id = $1 AND status NOT IN `+lcClosedStatuses+`),
  (SELECT COUNT(*) FROM lc_projects WHERE user_id = $1 AND status NOT IN `+lcClosedStatuses+`),
  (SELECT COUNT(*) FROM lc_action_items WHERE user_id = $1 AND status IN ('open','bezig','wacht_op_klant')),
  (SELECT COUNT(*) FROM lc_action_items WHERE user_id = $1 AND status IN ('open','bezig','wacht_op_klant') AND due_date IS NOT NULL AND due_date::date <= $2::date),
  (SELECT COUNT(*) FROM lc_quotes WHERE user_id = $1 AND status NOT IN ('afgewezen','verlopen','geaccepteerd')),
  (SELECT COUNT(*) FROM lc_invoices WHERE user_id = $1 AND status NOT IN ('betaald','geannuleerd')),
  (SELECT COALESCE(SUM(GREATEST(total_cents - paid_cents, 0)), 0) FROM lc_invoices WHERE user_id = $1 AND status NOT IN ('betaald','geannuleerd'))`,
		userID, today).Scan(
		&a.devicesTotal, &a.devicesOnline, &a.devicesOn,
		&a.commandsPending, &a.commandsProcessing, &a.commandsFailed,
		&a.scheduleTotal, &a.scheduleUpcoming,
		&a.personalUpcoming, &a.personalPending,
		&a.unreadEmails,
		&a.notesActive, &a.notesPinned, &a.notesDueToday, &a.notesOverdue, &a.notesTriage,
		&a.lcLeads, &a.lcWorkstreams, &a.lcProjects,
		&a.lcOpenActions, &a.lcOverdueActions,
		&a.lcOpenQuotes, &a.lcOpenInvoices, &a.lcOutstandingCents,
	)
	if err != nil {
		// The raw pgx error stays server-side (N12): the kiosk renders this array.
		slog.Error("focus aggregate counts failed", "error", err, "userId", userID)
		*errs = append(*errs, "Statistieken tijdelijk niet beschikbaar")
	}
	return a
}

func (h *FocusHandler) focusHealth(ctx context.Context, now time.Time, agg focusAggregate, errors *[]string) FocusHealth {
	health := FocusHealth{
		DevicesTotal:       agg.devicesTotal,
		DevicesOnline:      agg.devicesOnline,
		DevicesOn:          agg.devicesOn,
		CommandsPending:    agg.commandsPending,
		CommandsProcessing: agg.commandsProcessing,
		// Only recent failures matter — without a window, historical failures (e.g.
		// from a past bridge-auth outage) would alert forever.
		CommandsFailed: agg.commandsFailed,
	}
	health.DevicesOffline = maxInt(0, health.DevicesTotal-health.DevicesOnline)
	// Bridge liveness comes from the dedicated heartbeat (bumped on every /bridge/*
	// call), NOT MAX(devices.last_seen) which only moves when a per-device UDP
	// status POST lands — so it stays stale while the bridge is actively polling.
	health.BridgeLastSeenAt = h.timePtr(ctx, errors, "Bridge-status", `SELECT MAX(last_seen) FROM bridge_heartbeat`)
	health.BridgeOnline = health.BridgeLastSeenAt != nil && now.Sub(health.BridgeLastSeenAt.In(now.Location())) <= bridgeOfflineThreshold
	if health.BridgeOnline {
		health.BridgeStatus = "online"
	} else {
		health.BridgeStatus = "offline"
	}
	return health
}

func (h *FocusHandler) focusCounts(ctx context.Context, userID string, agg focusAggregate, errors *[]string) FocusCounts {
	counts := FocusCounts{
		ScheduleTotal:    agg.scheduleTotal,
		ScheduleUpcoming: agg.scheduleUpcoming,
		PersonalUpcoming: agg.personalUpcoming,
		PersonalPending:  agg.personalPending,
		UnreadEmails:     agg.unreadEmails,
		NotesActive:      agg.notesActive,
		NotesPinned:      agg.notesPinned,
		NotesDueToday:    agg.notesDueToday,
		NotesOverdue:     agg.notesOverdue,
		NotesTriage:      agg.notesTriage,
	}
	stats, err := store.NewHabitStore(h.db).Stats(ctx, userID)
	if err != nil {
		slog.Error("focus habit stats failed", "error", err, "userId", userID)
		*errors = append(*errors, "Habits tijdelijk niet beschikbaar")
	} else {
		counts.HabitsActive = stats.ActiveHabits
		counts.HabitsTodayDue = stats.TodayDue
		counts.HabitsCompleted = stats.TodayCompleted
	}
	return counts
}

// lcClosedStatuses mirrors the shared isClosedStatus vocabulary in
// internal/store/laventecare.go (leads/projects/workstreams share one status
// model). Kept as a literal here rather than importing store's unexported
// predicate — but every value must stay in sync with isClosedStatus, since a
// lead marked "gewonnen" or a workstream marked "omgezet_project" must stop
// counting as active here too, or these dashboard counts silently drift from
// what the CRM itself considers open/closed.
const lcClosedStatuses = `('afgerond','done','gesloten','gearchiveerd','omgezet_project','gewonnen','verloren','gediskwalificeerd','geannuleerd')`

// focusBusinessFromAggregate maps the consolidated counts onto the business
// block. The action-item queries mirror the allow-list pattern used for action
// items elsewhere in store/laventecare.go (e.g. GetCompanies' per-company
// action count, ListActions) instead of an ad-hoc blacklist.
func focusBusinessFromAggregate(agg focusAggregate) FocusBusinessStatus {
	return FocusBusinessStatus{
		ActiveLeads:       agg.lcLeads,
		ActiveWorkstreams: agg.lcWorkstreams,
		ActiveProjects:    agg.lcProjects,
		OpenActions:       agg.lcOpenActions,
		OverdueActions:    agg.lcOverdueActions,
		OpenQuotes:        agg.lcOpenQuotes,
		OpenInvoices:      agg.lcOpenInvoices,
		OutstandingCents:  agg.lcOutstandingCents,
	}
}

func (h *FocusHandler) focusSync(ctx context.Context, userID string, pendingPersonal, unreadEmails int, errors *[]string) FocusSyncSummary {
	googleConfigured := focusGoogleConfigured(h.cfg)
	scheduleLast := h.timePtr(ctx, errors, "Rooster-syncstatus", `SELECT MAX(imported_at) FROM schedule_meta WHERE user_id = $1`, userID)
	emailLast := h.timePtr(ctx, errors, "Gmail-syncstatus", `SELECT MAX(synced_at) FROM emails WHERE user_id = $1`, userID)

	return FocusSyncSummary{
		Schedule: FocusSyncTarget{
			Status:        focusSyncStatus(h.cfg.GoogleCalendarEnabled, googleConfigured, scheduleLast),
			Enabled:       h.cfg.GoogleCalendarEnabled,
			Configured:    googleConfigured,
			LastSuccessAt: scheduleLast,
		},
		Personal: FocusSyncTarget{
			Status:     focusPendingAwareStatus(h.cfg.GoogleCalendarEnabled, googleConfigured, pendingPersonal),
			Enabled:    h.cfg.GoogleCalendarEnabled,
			Configured: googleConfigured,
			Pending:    pendingPersonal,
		},
		Gmail: FocusSyncTarget{
			Status:        focusSyncStatus(h.cfg.GmailEnabled, googleConfigured, emailLast),
			Enabled:       h.cfg.GmailEnabled,
			Configured:    googleConfigured,
			LastSuccessAt: emailLast,
			Total:         unreadEmails,
		},
	}
}

// timePtr fetches a nullable timestamp. On failure the errors array (rendered
// verbatim on the kiosk) only gets a Dutch label — the raw pgx text is logged
// server-side instead of leaking into the 200-payload (N12).
func (h *FocusHandler) timePtr(ctx context.Context, errors *[]string, label, query string, args ...any) *time.Time {
	var value sql.NullTime
	if err := h.db.Pool.QueryRow(ctx, query, args...).Scan(&value); err != nil {
		slog.Error("focus summary query failed", "label", label, "error", err)
		*errors = append(*errors, label+" tijdelijk niet beschikbaar")
		return nil
	}
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func buildFocusAttention(health FocusHealth, counts FocusCounts, business FocusBusinessStatus, sync FocusSyncSummary) []FocusAttention {
	items := make([]FocusAttention, 0, 8)
	if !health.BridgeOnline {
		detail := "Geen recente bridge heartbeat"
		if health.BridgeLastSeenAt != nil {
			// Format in local wall-clock time — a UTC RFC3339 stamp next to the
			// Amsterdam times elsewhere on the kiosk reads as off-by-1/2-hours.
			detail = "Laatste heartbeat " + health.BridgeLastSeenAt.In(amsterdamLoc).Format("02-01-2006 15:04")
		}
		items = append(items, FocusAttention{
			ID: "bridge-offline", Domain: "home", Severity: "high",
			Title: "Bridge offline", Detail: detail, Href: "/settings",
		})
	}
	if health.CommandsFailed > 0 {
		items = append(items, FocusAttention{
			ID: "lamp-commands-failed", Domain: "home", Severity: "high",
			Title: "Lampcommando's mislukt", Detail: fmt.Sprintf("%d command(s) gefaald", health.CommandsFailed), Href: "/lampen",
		})
	}
	if health.CommandsPending+health.CommandsProcessing > 0 {
		items = append(items, FocusAttention{
			ID: "lamp-commands-queue", Domain: "home", Severity: "medium",
			Title: "Lampwachtrij actief", Detail: fmt.Sprintf("%d pending, %d processing", health.CommandsPending, health.CommandsProcessing), Href: "/settings",
		})
	}
	if sync.Schedule.Status != "success" {
		items = append(items, FocusAttention{
			ID: "schedule-sync", Domain: "sync", Severity: "medium",
			Title: "Rooster sync controleren", Detail: focusStatusDutch(sync.Schedule.Status), Href: "/settings",
		})
	}
	if sync.Gmail.Status != "success" {
		items = append(items, FocusAttention{
			ID: "gmail-sync", Domain: "sync", Severity: "medium",
			Title: "Gmail sync controleren", Detail: focusStatusDutch(sync.Gmail.Status), Href: "/settings",
		})
	}
	if counts.PersonalPending > 0 {
		items = append(items, FocusAttention{
			ID: "personal-pending", Domain: "agenda", Severity: "high",
			Title: "Agenda wachtrij", Detail: fmt.Sprintf("%d afspraak/afspraken wachten op sync", counts.PersonalPending), Href: "/agenda",
		})
	}
	if counts.NotesOverdue > 0 || counts.NotesDueToday > 0 || counts.NotesTriage > 0 {
		items = append(items, FocusAttention{
			ID: "notes-focus", Domain: "notes", Severity: focusSeverity(counts.NotesOverdue > 0),
			Title: "Notities met aandacht", Detail: fmt.Sprintf("%d verlopen, %d vandaag, %d triage", counts.NotesOverdue, counts.NotesDueToday, counts.NotesTriage), Href: "/notities",
		})
	}
	if counts.HabitsTodayDue > counts.HabitsCompleted {
		items = append(items, FocusAttention{
			ID: "habits-open", Domain: "habits", Severity: "low",
			Title: "Habits vandaag", Detail: fmt.Sprintf("%d van %d afgerond", counts.HabitsCompleted, counts.HabitsTodayDue), Href: "/habits",
		})
	}
	if business.OverdueActions > 0 {
		items = append(items, FocusAttention{
			ID: "lc-overdue-actions", Domain: "laventecare", Severity: "high",
			Title: "LaventeCare acties", Detail: fmt.Sprintf("%d actie(s) vandaag of verlopen", business.OverdueActions), Href: "/laventecare",
		})
	}
	if business.OpenInvoices > 0 {
		items = append(items, FocusAttention{
			ID: "lc-open-invoices", Domain: "laventecare", Severity: "medium",
			Title: "Open facturen", Detail: fmt.Sprintf("%d open factuur/facturen", business.OpenInvoices), Href: "/laventecare",
		})
	}
	return items
}

// focusStatusDutch translates the machine status enum (kept as-is in the JSON
// contract) into a human Dutch detail for the attention list — the kiosk showed
// literal "stale"/"missing_config" between otherwise-Dutch copy (N12).
func focusStatusDutch(status string) string {
	switch status {
	case "stale":
		return "verouderd"
	case "missing_config":
		return "niet geconfigureerd"
	case "pending":
		return "wacht op eerste sync"
	case "disabled":
		return "uitgeschakeld"
	default:
		return status
	}
}

func focusSyncStatus(enabled, configured bool, lastSuccess *time.Time) string {
	if !enabled {
		return "disabled"
	}
	if !configured {
		return "missing_config"
	}
	if lastSuccess == nil {
		return "pending"
	}
	if time.Since(lastSuccess.UTC()) > 24*time.Hour {
		return "stale"
	}
	return "success"
}

func focusPendingAwareStatus(enabled, configured bool, pending int) string {
	if !enabled {
		return "disabled"
	}
	if !configured {
		return "missing_config"
	}
	if pending > 0 {
		return "pending"
	}
	return "success"
}

func focusGoogleConfigured(cfg *config.Config) bool {
	return strings.TrimSpace(cfg.GoogleClientID) != "" &&
		strings.TrimSpace(cfg.GoogleClientSecret) != "" &&
		strings.TrimSpace(cfg.GoogleRefreshToken) != ""
}

func focusSeverity(condition bool) string {
	if condition {
		return "high"
	}
	return "medium"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
