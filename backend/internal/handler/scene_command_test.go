package handler

import (
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func TestCommandFromSceneActionClampsLampValues(t *testing.T) {
	action := model.SceneAction{TargetState: map[string]any{
		"brightness": float64(999),
		"color_temp": float64(9000),
		"r":          float64(-3),
		"g":          float64(999),
		"b":          float64(128),
	}}
	command, patch := commandFromSceneAction(action)
	want := map[string]int{
		"brightness": 100,
		"color_temp": 6500,
		"r":          0,
		"g":          255,
		"b":          128,
	}
	for key, expected := range want {
		if got := command[key]; got != expected {
			t.Errorf("command[%q]=%v, want %d", key, got, expected)
		}
		if got := patch[key]; got != expected {
			t.Errorf("patch[%q]=%v, want %d", key, got, expected)
		}
	}
}

func TestToIntValRejectsFractionalJSONNumbers(t *testing.T) {
	if _, ok := toIntVal(float64(1.5)); ok {
		t.Fatal("fractional JSON number was accepted as an integer")
	}
}
