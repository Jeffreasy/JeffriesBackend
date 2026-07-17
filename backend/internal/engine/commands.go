package engine

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/Jeffreasy/JeffriesBackend/internal/wiz"
	"github.com/google/uuid"
)

// loopDeviceCommands polls PostgreSQL for pending device commands and executes them.
func (e *Engine) loopDeviceCommands(ctx context.Context) {
	slog.Info("🌉 device command poller started (PostgreSQL)")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		e.pollCommands(ctx)
		sleepCtx(ctx, 2*time.Second)
	}
}

func (e *Engine) pollCommands(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("command poller panic", "recover", r)
		}
	}()

	commands, err := e.cmdStore.ClaimPending(ctx, 25)
	if err != nil {
		slog.Error("command poller error", "error", err)
		return
	}

	if len(commands) == 0 {
		return
	}

	deviceMap, err := e.getDeviceMap(ctx)
	if err != nil {
		return
	}

	for _, cmd := range commands {
		e.processCommand(ctx, cmd, deviceMap)
	}
}

func (e *Engine) processCommand(ctx context.Context, cmd store.DeviceCommand, deviceMap map[string]deviceInfo) {
	var command map[string]any
	if err := json.Unmarshal(cmd.Command, &command); err != nil {
		slog.Error("unmarshal device command", "id", cmd.ID, "error", err)
		_ = e.cmdStore.MarkDone(ctx, cmd.ID, store.DeviceCommandStatusFailed)
		return
	}

	// Determine target devices
	var infos []deviceInfo
	if cmd.DeviceID != nil {
		deviceID := cmd.DeviceID.String()
		if info, ok := deviceMap[deviceID]; ok {
			infos = append(infos, info)
		} else {
			slog.Warn("device command target not found", "cmdID", cmd.ID, "device", deviceID)
			_ = e.cmdStore.MarkDone(ctx, cmd.ID, store.DeviceCommandStatusFailed)
			return
		}
	} else {
		// Broadcast to all
		for _, info := range deviceMap {
			infos = append(infos, info)
		}
	}

	if len(infos) == 0 {
		slog.Warn("device command has no target devices", "cmdID", cmd.ID)
		_ = e.cmdStore.MarkDone(ctx, cmd.ID, store.DeviceCommandStatusFailed)
		return
	}

	wizParams := commandToWizParams(command)

	// Send to all target devices concurrently
	var wg sync.WaitGroup
	var successCount int
	var mu sync.Mutex
	var failedDeviceIDs []uuid.UUID // targets that did not respond (for offline marking)

	for _, di := range infos {
		wg.Add(1)
		go func(info deviceInfo) {
			defer wg.Done()
			_, wizErr := e.wiz.SendCommand(info.IP, "setPilot", wizParams)
			mu.Lock()
			defer mu.Unlock()
			if wizErr != nil {
				slog.Warn("WiZ command failed", "ip", info.IP, "error", wizErr, "cmdID", cmd.ID)
				if info.ID != uuid.Nil {
					failedDeviceIDs = append(failedDeviceIDs, info.ID)
				}
			} else {
				slog.Info("WiZ command OK", "ip", info.IP, "cmdID", cmd.ID)
				successCount++
			}
		}(di)
	}
	wg.Wait()

	if successCount > 0 {
		// At least one target reacted → command done.
		if err := e.cmdStore.MarkDone(ctx, cmd.ID, store.DeviceCommandStatusDone); err != nil {
			slog.Error("mark command done failed", "id", cmd.ID, "error", err)
		}
		return
	}

	// Total failure: retry via RequeueOrFail rather than failing on the first
	// blip — a transient LAN/UDP hiccup should not permanently fail the command
	// (mirrors the bridge HTTP path in handler/bridge.go). Only on the terminal
	// attempt do we mark the affected device(s) offline so the UI stops showing
	// the optimistic state as if the command landed.
	requeued, deviceID, err := e.cmdStore.RequeueOrFail(ctx, cmd.ID, 3)
	if err != nil {
		slog.Error("requeue command failed", "id", cmd.ID, "error", err)
		return
	}
	if requeued {
		return
	}
	// Terminal failure. For a targeted command RequeueOrFail returns its device_id;
	// for a broadcast (device_id NULL) mark every target that failed to respond.
	offlineIDs := failedDeviceIDs
	if deviceID != nil {
		offlineIDs = []uuid.UUID{*deviceID}
	}
	for _, id := range offlineIDs {
		if serr := e.devStore.SetStatus(ctx, id, "offline"); serr != nil {
			slog.Warn("failed to mark device offline after terminal command failure",
				"cmdID", cmd.ID, "deviceID", id, "error", serr)
		} else {
			slog.Warn("device command failed terminally; device marked offline",
				"cmdID", cmd.ID, "deviceID", id)
		}
	}
}

func (e *Engine) enqueueDeviceCommand(ctx context.Context, deviceID *uuid.UUID, command map[string]any) error {
	raw, err := json.Marshal(command)
	if err != nil {
		return err
	}
	_, err = e.cmdStore.Create(ctx, e.cfg.HomeappUserID, deviceID, raw)
	return err
}

func buildDeviceCommandFromAction(actionType string, action map[string]any) (map[string]any, bool) {
	switch actionType {
	case "off":
		return map[string]any{"on": false}, true
	case "on":
		return map[string]any{"on": true}, true
	case "brightness":
		return map[string]any{
			"on":         true,
			"brightness": getIntField(action, "brightness", 80),
		}, true
	case "color_temp":
		return map[string]any{
			"on":                true,
			"color_temp_mireds": getIntField(action, "colorTempMireds", 250),
		}, true
	case "scene":
		if sid := getIntField(action, "scene_id", 0); sid > 0 {
			return map[string]any{"on": true, "scene_id": sid}, true
		}
		sceneKey := getStringField(action, "sceneId", "helder")
		sceneDef, ok := SceneDefinitions[sceneKey]
		if !ok {
			sceneDef = SceneDefinitions["helder"]
		}
		return commandFromStateOpts(sceneDef), true
	case "color":
		hexColor := getStringField(action, "colorHex", "#ffffff")
		r, g, b := wiz.HexToRGB(hexColor)
		return map[string]any{"on": true, "r": r, "g": g, "b": b}, true
	default:
		return nil, false
	}
}

// statePatchFromAction derives the DB current_state patch for an automation
// action, matching the direct-path (applyAction) semantics exactly so queued
// automations converge to the same optimistic state the direct path writes.
// Note this differs from buildDeviceCommandFromAction: the command carries
// color_temp_mireds, the state patch carries color_temp in kelvin (and clears
// r/g/b + scene_id for white mode), as the frontend expects.
func statePatchFromAction(actionType string, action map[string]any) (map[string]any, bool) {
	switch actionType {
	case "off":
		return map[string]any{"on": false}, true
	case "on":
		return map[string]any{"on": true}, true
	case "brightness":
		return map[string]any{"on": true, "brightness": getIntField(action, "brightness", 80)}, true
	case "color_temp":
		kelvin := wiz.MiredsToKelvin(getIntField(action, "colorTempMireds", 250))
		return map[string]any{"on": true, "color_temp": kelvin, "r": 0, "g": 0, "b": 0, "scene_id": 0}, true
	case "scene":
		if sid := getIntField(action, "scene_id", 0); sid > 0 {
			return map[string]any{"on": true, "scene_id": sid}, true
		}
		sceneKey := getStringField(action, "sceneId", "helder")
		sceneDef, ok := SceneDefinitions[sceneKey]
		if !ok {
			sceneDef = SceneDefinitions["helder"]
		}
		return statePatchFromStateOpts(sceneDef), true
	case "color":
		r, g, b := wiz.HexToRGB(getStringField(action, "colorHex", "#ffffff"))
		return map[string]any{"on": true, "r": r, "g": g, "b": b, "scene_id": 0}, true
	default:
		return nil, false
	}
}

func commandFromStateOpts(opts wiz.StateOpts) map[string]any {
	command := map[string]any{"on": true}
	if opts.On != nil {
		command["on"] = *opts.On
	}
	if opts.Brightness != nil {
		command["brightness"] = *opts.Brightness
	}
	if opts.ColorTemp != nil {
		command["color_temp"] = *opts.ColorTemp
	}
	if opts.R != nil {
		command["r"] = *opts.R
	}
	if opts.G != nil {
		command["g"] = *opts.G
	}
	if opts.B != nil {
		command["b"] = *opts.B
	}
	return command
}

func cmdToInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func clampCommandInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func commandToWizParams(command map[string]any) map[string]any {
	wizParams := map[string]any{}

	if state, ok := command["state"].(bool); ok {
		wizParams["state"] = state
	} else if on, ok := command["on"].(bool); ok {
		wizParams["state"] = on
	} else {
		wizParams["state"] = true
	}

	if b, ok := command["dimming"]; ok {
		wizParams["dimming"] = clampCommandInt(cmdToInt(b), 10, 100)
	} else if b, ok := command["brightness"]; ok {
		wizParams["dimming"] = clampCommandInt(cmdToInt(b), 10, 100)
	}

	if temp, ok := command["temp"]; ok {
		wizParams["temp"] = clampCommandInt(cmdToInt(temp), 2200, 6500)
	} else if kelvin, ok := command["color_temp"]; ok {
		wizParams["temp"] = clampCommandInt(cmdToInt(kelvin), 2200, 6500)
	} else if mireds, ok := command["color_temp_mireds"]; ok {
		wizParams["temp"] = clampCommandInt(wiz.MiredsToKelvin(cmdToInt(mireds)), 2200, 6500)
	}

	if r, ok := command["r"]; ok {
		wizParams["r"] = clampCommandInt(cmdToInt(r), 0, 255)
		wizParams["g"] = clampCommandInt(cmdToInt(command["g"]), 0, 255)
		wizParams["b"] = clampCommandInt(cmdToInt(command["b"]), 0, 255)
	}

	if sid, ok := command["sceneId"]; ok {
		if sceneID := cmdToInt(sid); sceneID > 0 {
			wizParams["sceneId"] = clampCommandInt(sceneID, 1, 32)
		}
	} else if sid, ok := command["scene_id"]; ok {
		if sceneID := cmdToInt(sid); sceneID > 0 {
			wizParams["sceneId"] = clampCommandInt(sceneID, 1, 32)
		}
	}

	return wizParams
}

// sleepCtx sleeps for the given duration or until the context is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
