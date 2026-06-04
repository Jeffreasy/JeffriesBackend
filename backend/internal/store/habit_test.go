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

	current, longest, total, xp := calculateHabitProgress(habit, logs, "2026-06-04")
	if current != 1 || longest != 2 || total != 3 || xp != 30 {
		t.Fatalf("progress = current %d longest %d total %d xp %d, want 1/2/3/30", current, longest, total, xp)
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

	current, longest, total, xp := calculateHabitProgress(habit, logs, "2026-06-05")
	if current != 2 || longest != 2 || total != 4 || xp != 0 {
		t.Fatalf("progress = current %d longest %d total %d xp %d, want 2/2/4/0", current, longest, total, xp)
	}
}

func strPtr(value string) *string { return &value }
