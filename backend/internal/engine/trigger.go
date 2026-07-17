package engine

import (
	"math"
	"strings"
	"time"
)

// ShouldFire determines whether an automation should trigger based on time, day, and schedule.
func ShouldFire(auto map[string]any, now time.Time, todayShiftTypes map[string]bool, lastFiredAt map[string]time.Time) bool {
	trigger, _ := auto["trigger"].(map[string]any)
	if trigger == nil {
		return false
	}

	triggerType, _ := trigger["triggerType"].(string)
	if triggerType == "" {
		triggerType = "time"
	}

	timeStr, _ := trigger["time"].(string)
	if timeStr == "" {
		return false
	}

	// Parse time string "HH:MM"
	parts := strings.SplitN(timeStr, ":", 2)
	if len(parts) != 2 {
		return false
	}
	tH := parseInt(parts[0])
	tM := parseInt(parts[1])
	if tH < 0 || tM < 0 {
		return false
	}

	// Check time window (±1 minute). On the Europe/Amsterdam spring-forward
	// day, 02:xx does not exist; run those jobs once at 03:00 instead of silently
	// skipping the day.
	nowTotal := now.Hour()*60 + now.Minute()
	targetTotal := tH*60 + tM
	inWindow := int(math.Abs(float64(nowTotal-targetTotal))) <= 1
	if !inWindow && !(tH == 2 && now.Hour() == 3 && now.Minute() <= 1 && isSpringForwardDay(now)) {
		return false
	}

	// Check Convex lastFiredAt — prevent double fire after restart
	autoID, _ := auto["_id"].(string)
	if autoID != "" {
		if val, exists := auto["lastFiredAt"]; exists && val != nil {
			var lastFired time.Time
			var parsed bool

			switch v := val.(type) {
			case string:
				if t, err := time.Parse(time.RFC3339, strings.Replace(v, "Z", "+00:00", 1)); err == nil {
					lastFired = t
					parsed = true
				}
			case int64:
				lastFired = time.UnixMilli(v)
				parsed = true
			case float64:
				lastFired = time.UnixMilli(int64(v))
				parsed = true
			case int:
				lastFired = time.UnixMilli(int64(v))
				parsed = true
			}

			if parsed {
				// One automation has one wall-clock trigger, so it may fire at most once
				// per Amsterdam calendar day. This prevents the repeated 02:xx hour on
				// the autumn DST transition (and ordinary adjacent-window double fires).
				lastLocal := lastFired.In(now.Location())
				if lastLocal.Year() == now.Year() && lastLocal.YearDay() == now.YearDay() {
					return false
				}
				if now.UTC().Sub(lastFired.UTC()).Seconds() < MinFireInterval {
					return false
				}
			}
		}
	}

	// Check day of week (0=monday, 6=sunday)
	currentWeekday := int(now.Weekday())
	// Convert Go weekday (0=Sunday) to Python weekday (0=Monday)
	currentWeekday = (currentWeekday + 6) % 7

	days := getDays(trigger)
	if days != nil {
		if len(days) == 0 {
			return false // Explicitly empty = never fire
		}
		found := false
		for _, d := range days {
			if d == currentWeekday {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	// days == nil means ALL_DAYS (always fire)

	// Schedule trigger: check shiftType
	if triggerType == "schedule" {
		shiftType, _ := trigger["shiftType"].(string)
		if shiftType == "" {
			shiftType = "any"
		}
		if shiftType != "any" && !todayShiftTypes[shiftType] {
			return false
		}
	}

	// Smart Exclusions: check if any excluded shift is active today
	if excluded, ok := trigger["excludedShifts"].([]any); ok {
		for _, ex := range excluded {
			if shiftType, ok := ex.(string); ok {
				if todayShiftTypes[shiftType] {
					return false // Do not fire if an excluded shift is active today
				}
			}
		}
	}

	return true
}

func isSpringForwardDay(now time.Time) bool {
	loc := now.Location()
	before := time.Date(now.Year(), now.Month(), now.Day(), 1, 59, 0, 0, loc)
	after := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, loc)
	_, beforeOffset := before.Zone()
	_, afterOffset := after.Zone()
	return afterOffset > beforeOffset
}

// getDays extracts the days array from trigger config.
// Returns nil if days field is absent (= all days).
func getDays(trigger map[string]any) []int {
	v, exists := trigger["days"]
	if !exists {
		return nil
	}

	arr, ok := v.([]any)
	if !ok {
		return nil
	}

	result := make([]int, 0, len(arr))
	for _, item := range arr {
		switch n := item.(type) {
		case float64:
			result = append(result, int(n))
		case int:
			result = append(result, n)
		}
	}
	return result
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}
