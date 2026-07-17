package engine

import "testing"

func TestCommandToWizParamsClampsValues(t *testing.T) {
	got := commandToWizParams(map[string]any{
		"brightness":        float64(999),
		"color_temp_mireds": float64(1),
		"r":                 float64(-4),
		"g":                 float64(500),
		"b":                 float64(128),
		"scene_id":          float64(99),
	})
	want := map[string]int{
		"dimming": 100,
		"temp":    6500,
		"r":       0,
		"g":       255,
		"b":       128,
		"sceneId": 32,
	}
	for key, expected := range want {
		if got[key] != expected {
			t.Errorf("%s=%v, want %d", key, got[key], expected)
		}
	}
}

func TestCommandToWizParamsOmitsInvalidScene(t *testing.T) {
	got := commandToWizParams(map[string]any{"scene_id": float64(0)})
	if _, ok := got["sceneId"]; ok {
		t.Fatalf("invalid scene id was forwarded: %#v", got)
	}
}
