package handler

import (
	"context"
	"database/sql"
	"fmt"
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
	health := h.focusHealth(ctx, now, &errors)
	counts := h.focusCounts(ctx, userID, today, &errors)
	business := h.focusBusiness(ctx, userID, today, &errors)
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

func (h *FocusHandler) focusHealth(ctx context.Context, now time.Time, errors *[]string) FocusHealth {
	health := FocusHealth{}
	health.DevicesTotal = h.count(ctx, errors, "devices.total", `SELECT COUNT(*) FROM devices`)
	health.DevicesOnline = h.count(ctx, errors, "devices.online", `SELECT COUNT(*) FROM devices WHERE status = 'online'`)
	health.DevicesOn = h.count(ctx, errors, "devices.on", `SELECT COUNT(*) FROM devices WHERE current_state->>'on' = 'true'`)
	health.DevicesOffline = maxInt(0, health.DevicesTotal-health.DevicesOnline)
	health.CommandsPending = h.count(ctx, errors, "commands.pending", `SELECT COUNT(*) FROM device_commands WHERE status = 'pending'`)
	health.CommandsProcessing = h.count(ctx, errors, "commands.processing", `SELECT COUNT(*) FROM device_commands WHERE status = 'processing'`)
	// Only recent failures matter — without a window, historical failures (e.g.
	// from a past bridge-auth outage) would alert forever.
	health.CommandsFailed = h.count(ctx, errors, "commands.failed", `SELECT COUNT(*) FROM device_commands WHERE status = 'failed' AND COALESCE(completed_at, updated_at) > now() - interval '24 hours'`)
	// Bridge liveness comes from the dedicated heartbeat (bumped on every /bridge/*
	// call), NOT MAX(devices.last_seen) which only moves when a per-device UDP
	// status POST lands — so it stays stale while the bridge is actively polling.
	health.BridgeLastSeenAt = h.timePtr(ctx, errors, "bridge.heartbeat", `SELECT MAX(last_seen) FROM bridge_heartbeat`)
	health.BridgeOnline = health.BridgeLastSeenAt != nil && now.Sub(health.BridgeLastSeenAt.In(now.Location())) <= bridgeOfflineThreshold
	if health.BridgeOnline {
		health.BridgeStatus = "online"
	} else {
		health.BridgeStatus = "offline"
	}
	return health
}

func (h *FocusHandler) focusCounts(ctx context.Context, userID, today string, errors *[]string) FocusCounts {
	counts := FocusCounts{}
	counts.ScheduleTotal = h.count(ctx, errors, "schedule.total", `SELECT COUNT(*) FROM schedule WHERE user_id = $1`, userID)
	counts.ScheduleUpcoming = h.count(ctx, errors, "schedule.upcoming", `SELECT COUNT(*) FROM schedule WHERE user_id = $1 AND start_datum >= $2::date AND UPPER(COALESCE(status, '')) <> 'VERWIJDERD'`, userID, today)
	counts.PersonalUpcoming = h.count(ctx, errors, "personal.upcoming", `SELECT COUNT(*) FROM personal_events WHERE user_id = $1 AND eind_datum >= $2::date AND status NOT IN ('VERWIJDERD', 'cancelled', 'PendingDelete')`, userID, today)
	counts.PersonalPending = h.count(ctx, errors, "personal.pending", `SELECT COUNT(*) FROM personal_events WHERE user_id = $1 AND status IN ('PendingCreate', 'PendingUpdate', 'PendingDelete')`, userID)
	counts.UnreadEmails = h.count(ctx, errors, "emails.unread", `SELECT COUNT(*) FROM emails WHERE user_id = $1 AND NOT is_gelezen AND NOT is_verwijderd`, userID)
	counts.NotesActive = h.count(ctx, errors, "notes.active", `SELECT COUNT(*) FROM notes WHERE user_id = $1 AND NOT is_archived AND NOT is_completed`, userID)
	counts.NotesPinned = h.count(ctx, errors, "notes.pinned", `SELECT COUNT(*) FROM notes WHERE user_id = $1 AND NOT is_archived AND NOT is_completed AND is_pinned`, userID)
	counts.NotesDueToday = h.count(ctx, errors, "notes.dueToday", `SELECT COUNT(*) FROM notes WHERE user_id = $1 AND NOT is_archived AND NOT is_completed AND deadline::date = $2::date`, userID, today)
	counts.NotesOverdue = h.count(ctx, errors, "notes.overdue", `SELECT COUNT(*) FROM notes WHERE user_id = $1 AND NOT is_archived AND NOT is_completed AND deadline IS NOT NULL AND deadline::date < $2::date`, userID, today)
	counts.NotesTriage = h.count(ctx, errors, "notes.triage", `SELECT COUNT(*) FROM notes WHERE user_id = $1 AND NOT is_archived AND NOT is_completed AND COALESCE(triage_flag, false) = true`, userID)
	stats, err := store.NewHabitStore(h.db).Stats(ctx, userID)
	if err != nil {
		*errors = append(*errors, "habits.stats: "+err.Error())
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

func (h *FocusHandler) focusBusiness(ctx context.Context, userID, today string, errors *[]string) FocusBusinessStatus {
	return FocusBusinessStatus{
		ActiveLeads:       h.count(ctx, errors, "lc.leads", `SELECT COUNT(*) FROM lc_leads WHERE user_id = $1 AND status NOT IN `+lcClosedStatuses, userID),
		ActiveWorkstreams: h.count(ctx, errors, "lc.workstreams", `SELECT COUNT(*) FROM lc_workstreams WHERE user_id = $1 AND status NOT IN `+lcClosedStatuses, userID),
		ActiveProjects:    h.count(ctx, errors, "lc.projects", `SELECT COUNT(*) FROM lc_projects WHERE user_id = $1 AND status NOT IN `+lcClosedStatuses, userID),
		// Mirrors the allow-list pattern already used for action items elsewhere
		// in store/laventecare.go (e.g. GetCompanies' per-company action count,
		// ListActions) instead of an ad-hoc blacklist that can miss statuses the
		// shared lcKnownStatus validator otherwise accepts on this same table.
		OpenActions:      h.count(ctx, errors, "lc.actions", `SELECT COUNT(*) FROM lc_action_items WHERE user_id = $1 AND status IN ('open','bezig','wacht_op_klant')`, userID),
		OverdueActions:   h.count(ctx, errors, "lc.actions.overdue", `SELECT COUNT(*) FROM lc_action_items WHERE user_id = $1 AND status IN ('open','bezig','wacht_op_klant') AND due_date IS NOT NULL AND due_date::date <= $2::date`, userID, today),
		OpenQuotes:       h.count(ctx, errors, "lc.quotes", `SELECT COUNT(*) FROM lc_quotes WHERE user_id = $1 AND status NOT IN ('afgewezen','verlopen','geaccepteerd')`, userID),
		OpenInvoices:     h.count(ctx, errors, "lc.invoices", `SELECT COUNT(*) FROM lc_invoices WHERE user_id = $1 AND status NOT IN ('betaald','geannuleerd')`, userID),
		OutstandingCents: h.count(ctx, errors, "lc.outstanding", `SELECT COALESCE(SUM(GREATEST(total_cents - paid_cents, 0)), 0) FROM lc_invoices WHERE user_id = $1 AND status NOT IN ('betaald','geannuleerd')`, userID),
	}
}

func (h *FocusHandler) focusSync(ctx context.Context, userID string, pendingPersonal, unreadEmails int, errors *[]string) FocusSyncSummary {
	googleConfigured := focusGoogleConfigured(h.cfg)
	scheduleLast := h.timePtr(ctx, errors, "sync.schedule", `SELECT MAX(imported_at) FROM schedule_meta WHERE user_id = $1`, userID)
	emailLast := h.timePtr(ctx, errors, "sync.gmail", `SELECT MAX(synced_at) FROM emails WHERE user_id = $1`, userID)

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

func (h *FocusHandler) count(ctx context.Context, errors *[]string, label, query string, args ...any) int {
	var value int
	if err := h.db.Pool.QueryRow(ctx, query, args...).Scan(&value); err != nil {
		*errors = append(*errors, label+": "+err.Error())
		return 0
	}
	return value
}

func (h *FocusHandler) timePtr(ctx context.Context, errors *[]string, label, query string, args ...any) *time.Time {
	var value sql.NullTime
	if err := h.db.Pool.QueryRow(ctx, query, args...).Scan(&value); err != nil {
		*errors = append(*errors, label+": "+err.Error())
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
			detail = "Laatste heartbeat " + health.BridgeLastSeenAt.UTC().Format(time.RFC3339)
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
			Title: "Rooster sync controleren", Detail: sync.Schedule.Status, Href: "/settings",
		})
	}
	if sync.Gmail.Status != "success" {
		items = append(items, FocusAttention{
			ID: "gmail-sync", Domain: "sync", Severity: "medium",
			Title: "Gmail sync controleren", Detail: sync.Gmail.Status, Href: "/settings",
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
