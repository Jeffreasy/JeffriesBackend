package handler

import "testing"

func TestValidateBridgeDeviceStatus(t *testing.T) {
	valid := bridgeDeviceStatusRequest{
		Status: " ONLINE ",
		CurrentState: map[string]any{
			"on": true, "brightness": float64(0), "color_temp": float64(6500),
			"r": float64(0), "g": float64(128), "b": float64(255), "scene_id": float64(32),
		},
	}
	status, state, err := validateBridgeDeviceStatus(valid)
	if err != nil {
		t.Fatalf("valid status rejected: %v", err)
	}
	if status != "online" || state["brightness"] != 0 || state["scene_id"] != 32 {
		t.Fatalf("unexpected normalization: status=%q state=%#v", status, state)
	}

	invalid := []bridgeDeviceStatusRequest{
		{Status: "compromised"},
		{CurrentState: map[string]any{"admin": true}},
		{CurrentState: map[string]any{"brightness": float64(101)}},
		{CurrentState: map[string]any{"brightness": float64(12.5)}},
		{CurrentState: map[string]any{"color_temp": float64(1000)}},
		{CurrentState: map[string]any{"r": float64(-1)}},
		{CurrentState: map[string]any{"scene_id": float64(33)}},
	}
	for index, input := range invalid {
		if _, _, err := validateBridgeDeviceStatus(input); err == nil {
			t.Fatalf("invalid case %d was accepted: %#v", index, input)
		}
	}
}
