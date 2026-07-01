package store

import (
	"testing"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func TestHabitDueOnDateRespectsFrequencyAndPause(t *testing.T) {
	monday := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	saturday := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		habit model.Habit
		date  time.Time
		want  bool
	}{
		{name: "daily", habit: model.Habit{IsActief: true, Frequentie: "dagelijks"}, date: monday, want: true},
		{name: "paused", habit: model.Habit{IsActief: true, IsPauze: true, Frequentie: "dagelijks"}, date: monday, want: false},
		{name: "weekdays on monday", habit: model.Habit{IsActief: true, Frequentie: "weekdagen"}, date: monday, want: true},
		{name: "weekdays on saturday", habit: model.Habit{IsActief: true, Frequentie: "weekdagen"}, date: saturday, want: false},
		{name: "weekend on saturday", habit: model.Habit{IsActief: true, Frequentie: "weekenddagen"}, date: saturday, want: true},
		{name: "custom monday", habit: model.Habit{IsActief: true, Frequentie: "aangepast", AangepasteDagen: []int32{1, 3}}, date: monday, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := habitDueOnDate(tt.habit, tt.date.Format("2006-01-02"), habitScheduleContext{})
			if got != tt.want {
				t.Fatalf("habitDueOnDate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHabitDueOnDateRespectsRoosterFilter(t *testing.T) {
	filter := "vroegeDienst"
	habit := model.Habit{IsActief: true, Frequentie: "dagelijks", RoosterFilter: &filter}

	if !habitDueOnDate(habit, "2026-06-01", habitScheduleContext{HasWork: true, HasVroeg: true}) {
		t.Fatal("expected habit due on early shift")
	}
	if habitDueOnDate(habit, "2026-06-01", habitScheduleContext{HasWork: true, HasLaat: true}) {
		t.Fatal("did not expect habit due on late-only shift")
	}
}

func TestCalculatePositiveHabitProgress(t *testing.T) {
	habit := model.Habit{Type: "positief", XPPerVoltooiing: 10}
	logs := []habitProgressLog{
		{Datum: "2026-06-01", Voltooid: true, XPVerdiend: 10},
		{Datum: "2026-06-02", Voltooid: true, XPVerdiend: 10},
		{Datum: "2026-06-04", Voltooid: true, XPVerdiend: 10},
	}

	alwaysDue := func(string) bool { return true }
	current, longest, total, xp := calculateHabitProgress(habit, logs, "2026-06-04", alwaysDue)
	if current != 1 || longest != 2 || total != 3 || xp != 30 {
		t.Fatalf("progress = current %d longest %d total %d xp %d, want 1/2/3/30", current, longest, total, xp)
	}
}

func TestCalculatePositiveHabitProgressSkipsNonDueDays(t *testing.T) {
	// A weekend-only habit completed two consecutive weekends; the weekdays between
	// are not due and must NOT break the streak.
	habit := model.Habit{Type: "positief", Frequentie: "weekenddagen"}
	logs := []habitProgressLog{
		{Datum: "2026-06-06", Voltooid: true}, // Sat
		{Datum: "2026-06-07", Voltooid: true}, // Sun
		{Datum: "2026-06-13", Voltooid: true}, // next Sat
		{Datum: "2026-06-14", Voltooid: true}, // next Sun
	}
	isDue := func(date string) bool {
		d, _ := time.Parse("2006-01-02", date)
		wd := d.Weekday()
		return wd == time.Saturday || wd == time.Sunday
	}
	current, longest, _, _ := calculateHabitProgress(habit, logs, "2026-06-14", isDue)
	if current != 4 || longest != 4 {
		t.Fatalf("weekend streak current %d longest %d, want 4/4 (weekdays must not break it)", current, longest)
	}
}

func TestNormalizeHabitLogDoesNotCompleteNoteOnly(t *testing.T) {
	habit := model.Habit{Type: "positief", XPPerVoltooiing: 10}
	log := normalizeHabitLogForHabit(habit, model.HabitLog{Notitie: strPtr("alleen context")})

	if log.Voltooid || log.XPVerdiend != 0 {
		t.Fatalf("note-only log completed=%v xp=%d, want false/0", log.Voltooid, log.XPVerdiend)
	}

	done := normalizeHabitLogForHabit(habit, model.HabitLog{Voltooid: true})
	if !done.Voltooid || done.XPVerdiend != 10 {
		t.Fatalf("done log completed=%v xp=%d, want true/10", done.Voltooid, done.XPVerdiend)
	}
}

func TestCalculateNegativeHabitProgress(t *testing.T) {
	created := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	habit := model.Habit{Type: "negatief", Aangemaakt: created}
	logs := []habitProgressLog{{Datum: "2026-06-03", IsIncident: true}}

	current, longest, total, xp := calculateHabitProgress(habit, logs, "2026-06-05", func(string) bool { return true })
	if current != 2 || longest != 2 || total != 4 || xp != 0 {
		t.Fatalf("progress = current %d longest %d total %d xp %d, want 2/2/4/0", current, longest, total, xp)
	}
}

func TestCalculateWeeklyHabitProgress(t *testing.T) {
	goal := 2
	habit := model.Habit{Type: "positief", Frequentie: "x_per_week", DoelAantal: &goal, XPPerVoltooiing: 10}
	logs := []habitProgressLog{
		// Week 2026-W23 (1-7 jun): 2 completions → satisfied
		{Datum: "2026-06-01", Voltooid: true, XPVerdiend: 10},
		{Datum: "2026-06-04", Voltooid: true, XPVerdiend: 10},
		// Week 2026-W24 (8-14 jun): 2 completions → satisfied
		{Datum: "2026-06-09", Voltooid: true, XPVerdiend: 10},
		{Datum: "2026-06-13", Voltooid: true, XPVerdiend: 10},
		// Week 2026-W25 (15-21 jun): current week, only 1 so far → in progress
		{Datum: "2026-06-16", Voltooid: true, XPVerdiend: 10},
	}
	alwaysDue := func(string) bool { return true }
	current, longest, total, xp := calculateHabitProgress(habit, logs, "2026-06-17", alwaysDue)
	if current != 2 || longest != 2 || total != 5 || xp != 50 {
		t.Fatalf("weekly progress = current %d longest %d total %d xp %d, want 2/2/5/50 (partial current week must not break the streak)", current, longest, total, xp)
	}

	// Once the current week reaches the goal it joins the streak.
	logs = append(logs, habitProgressLog{Datum: "2026-06-17", Voltooid: true, XPVerdiend: 10})
	current, longest, _, _ = calculateHabitProgress(habit, logs, "2026-06-17", alwaysDue)
	if current != 3 || longest != 3 {
		t.Fatalf("weekly progress after goal = current %d longest %d, want 3/3", current, longest)
	}
}

func TestCalculateWeeklyHabitStreakBreaksOnMissedWeek(t *testing.T) {
	habit := model.Habit{Type: "positief", Frequentie: "x_per_week"} // doel_aantal default 1
	logs := []habitProgressLog{
		{Datum: "2026-06-01", Voltooid: true}, // W23
		// W24 (8-14 jun) skipped entirely
		{Datum: "2026-06-16", Voltooid: true}, // W25
	}
	current, longest, _, _ := calculateHabitProgress(habit, logs, "2026-06-17", func(string) bool { return true })
	if current != 1 || longest != 1 {
		t.Fatalf("weekly progress = current %d longest %d, want 1/1 (missed week breaks the run)", current, longest)
	}
}

func TestCalculateMonthlyHabitProgress(t *testing.T) {
	goal := 3
	habit := model.Habit{Type: "positief", Frequentie: "x_per_maand", DoelAantal: &goal}
	logs := []habitProgressLog{
		{Datum: "2026-04-02", Voltooid: true},
		{Datum: "2026-04-15", Voltooid: true},
		{Datum: "2026-04-28", Voltooid: true}, // april satisfied
		{Datum: "2026-05-05", Voltooid: true},
		{Datum: "2026-05-06", Voltooid: true},
		{Datum: "2026-05-07", Voltooid: true}, // mei satisfied
		{Datum: "2026-06-01", Voltooid: true}, // juni in progress
	}
	current, longest, total, _ := calculateHabitProgress(habit, logs, "2026-06-10", func(string) bool { return true })
	if current != 2 || longest != 2 || total != 7 {
		t.Fatalf("monthly progress = current %d longest %d total %d, want 2/2/7", current, longest, total)
	}
}

func TestHabitDueOnDateNotBeforeCreation(t *testing.T) {
	created := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	habit := model.Habit{IsActief: true, Frequentie: "dagelijks", Aangemaakt: created}
	if habitDueOnDate(habit, "2026-06-05", habitScheduleContext{}) {
		t.Fatal("habit must not be due before its creation date (heatmap rewrote history)")
	}
	if !habitDueOnDate(habit, "2026-06-10", habitScheduleContext{}) {
		t.Fatal("habit must be due on its creation date")
	}
}

func TestPeriodBoundsForDate(t *testing.T) {
	ws, we, err := PeriodBoundsForDate("2026-07-01", true) // Wednesday
	if err != nil || ws != "2026-06-29" || we != "2026-07-05" {
		t.Fatalf("week bounds = %s..%s (%v), want 2026-06-29..2026-07-05", ws, we, err)
	}
	ms, me, err := PeriodBoundsForDate("2026-02-10", false)
	if err != nil || ms != "2026-02-01" || me != "2026-02-28" {
		t.Fatalf("month bounds = %s..%s (%v), want 2026-02-01..2026-02-28", ms, me, err)
	}
}

func strPtr(value string) *string { return &value }
