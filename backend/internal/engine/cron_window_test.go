package engine

import "testing"

func TestBriefingWindowAllowsDeployCatchupButNotLateDay(t *testing.T) {
	if !briefingWindowOpen(8*60+45, 8*60, 120) {
		t.Fatal("45-minute deploy delay should still send briefing")
	}
	if briefingWindowOpen(11*60, 8*60, 120) {
		t.Fatal("three-hour-late restart should not send stale briefing")
	}
	if briefingWindowOpen(7*60+59, 8*60, 120) {
		t.Fatal("briefing must not send before target")
	}
}
