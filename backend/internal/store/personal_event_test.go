package store

import (
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func TestPersonalEventIsPastAllDayPastHoliday(t *testing.T) {
	event := model.PersonalEvent{
		Titel:      "Eerste Pinksterdag",
		StartDatum: "2026-05-24",
		EindDatum:  "2026-05-24",
		Heledag:    true,
		Status:     PersonalEventStatusUpcoming,
	}

	if !personalEventIsPast(event, personalEventClock{date: "2026-06-03", time: "12:00"}) {
		t.Fatal("expected 2026-05-24 all-day event to be past on 2026-06-03")
	}
}

func TestPersonalEventIsPastAllDayToday(t *testing.T) {
	event := model.PersonalEvent{
		Titel:      "Hele dag vandaag",
		StartDatum: "2026-06-03",
		EindDatum:  "2026-06-03",
		Heledag:    true,
		Status:     PersonalEventStatusUpcoming,
	}

	if personalEventIsPast(event, personalEventClock{date: "2026-06-03", time: "23:30"}) {
		t.Fatal("expected same-day all-day event to remain upcoming/current until tomorrow")
	}
}

func TestNormalizePersonalEventStatuses(t *testing.T) {
	events := []model.PersonalEvent{
		{
			Titel:      "Stale upcoming",
			StartDatum: "2026-05-24",
			EindDatum:  "2026-05-24",
			Heledag:    true,
			Status:     PersonalEventStatusUpcoming,
		},
		{
			Titel:      "Stale past",
			StartDatum: "2026-06-04",
			EindDatum:  "2026-06-04",
			Heledag:    true,
			Status:     PersonalEventStatusPast,
		},
	}

	now := personalEventClock{date: "2026-06-03", time: "12:00"}
	for i := range events {
		normalizePersonalEventStatus(&events[i], now)
	}

	if events[0].Status != PersonalEventStatusPast {
		t.Fatalf("expected stale upcoming event to normalize to past, got %q", events[0].Status)
	}
	if events[1].Status != PersonalEventStatusUpcoming {
		t.Fatalf("expected future stale past event to normalize to upcoming, got %q", events[1].Status)
	}
}
