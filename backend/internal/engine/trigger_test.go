package engine

import (
	"testing"
	"time"
)

func TestShouldFireTimeTrigger(t *testing.T) {
	now := time.Date(2026, 6, 5, 8, 0, 0, 0, time.UTC)
	auto := map[string]any{
		"_id": "auto_1",
		"trigger": map[string]any{
			"triggerType": "time",
			"time":        "08:00",
		},
	}

	if !ShouldFire(auto, now, nil, nil) {
		t.Error("expected ShouldFire to return true for matching time trigger")
	}
}

func TestShouldFireLastFiredAtTypes(t *testing.T) {
	now := time.Date(2026, 6, 5, 8, 0, 0, 0, time.UTC)
	autoBase := map[string]any{
		"_id": "auto_1",
		"trigger": map[string]any{
			"triggerType": "time",
			"time":        "08:00",
		},
	}

	// 1. lastFiredAt as RFC3339 string (recently fired)
	autoStr := copyMap(autoBase)
	autoStr["lastFiredAt"] = now.Add(-10 * time.Second).Format(time.RFC3339)
	if ShouldFire(autoStr, now, nil, nil) {
		t.Error("expected ShouldFire to return false when lastFiredAt is a recent RFC3339 string")
	}

	// 2. lastFiredAt as int64 unix millis (recently fired)
	autoInt64 := copyMap(autoBase)
	autoInt64["lastFiredAt"] = now.Add(-10 * time.Second).UnixMilli()
	if ShouldFire(autoInt64, now, nil, nil) {
		t.Error("expected ShouldFire to return false when lastFiredAt is a recent int64 timestamp")
	}

	// 3. lastFiredAt as float64 unix millis (recently fired, common in JSON unmarshaling)
	autoFloat64 := copyMap(autoBase)
	autoFloat64["lastFiredAt"] = float64(now.Add(-10 * time.Second).UnixMilli())
	if ShouldFire(autoFloat64, now, nil, nil) {
		t.Error("expected ShouldFire to return false when lastFiredAt is a recent float64 timestamp")
	}

	// 4. lastFiredAt as int unix millis (recently fired)
	autoInt := copyMap(autoBase)
	autoInt["lastFiredAt"] = int(now.Add(-10 * time.Second).UnixMilli())
	if ShouldFire(autoInt, now, nil, nil) {
		t.Error("expected ShouldFire to return false when lastFiredAt is a recent int timestamp")
	}

	// 5. lastFiredAt in the past (long ago) - should fire
	autoPast := copyMap(autoBase)
	autoPast["lastFiredAt"] = now.Add(-24 * time.Hour).UnixMilli()
	if !ShouldFire(autoPast, now, nil, nil) {
		t.Error("expected ShouldFire to return true when lastFiredAt was on a previous day")
	}
}

func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
func TestShouldFireHandlesAmsterdamDSTTransitions(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	auto := map[string]any{
		"_id":     "dst-auto",
		"trigger": map[string]any{"triggerType": "time", "time": "02:30"},
	}
	spring := time.Date(2026, 3, 29, 3, 0, 0, 0, loc)
	if !ShouldFire(auto, spring, nil, nil) {
		t.Fatal("spring-forward 02:30 automation was not caught up at 03:00")
	}
	first := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC).In(loc)
	second := time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC).In(loc)
	auto["lastFiredAt"] = first.Format(time.RFC3339)
	if ShouldFire(auto, second, nil, nil) {
		t.Fatal("fall-back repeated 02:30 fired twice")
	}
}
