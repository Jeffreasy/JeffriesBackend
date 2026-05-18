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

	// Check time window (±1 minute)
	nowTotal := now.Hour()*60 + now.Minute()
	targetTotal := tH*60 + tM
	if int(math.Abs(float64(nowTotal-targetTotal))) > 1 {
		return false
	}

	// Check Convex lastFiredAt — prevent double fire after restart
	autoID, _ := auto["_id"].(string)
	if autoID != "" {
		if lastFiredStr, _ := auto["lastFiredAt"].(string); lastFiredStr != "" {
			if lastFired, err := time.Parse(time.RFC3339, strings.Replace(lastFiredStr, "Z", "+00:00", 1)); err == nil {
				nowUTC := now.UTC()
				if nowUTC.Sub(lastFired).Seconds() < MinFireInterval {
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
