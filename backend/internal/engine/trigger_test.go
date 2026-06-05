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
	autoPast["lastFiredAt"] = now.Add(-2 * time.Minute).UnixMilli()
	if !ShouldFire(autoPast, now, nil, nil) {
		t.Error("expected ShouldFire to return true when lastFiredAt is in the past")
	}
}

func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
