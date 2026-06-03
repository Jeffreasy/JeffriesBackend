package engine

import (
	"context"
	"encoding/json"
	"log/slog"
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

	// Build WiZ setPilot params directly from frontend command
	wizParams := map[string]any{}

	if state, ok := command["state"].(bool); ok {
		wizParams["state"] = state
	} else if on, ok := command["on"].(bool); ok {
		wizParams["state"] = on
	} else {
		wizParams["state"] = true
	}

	if b, ok := command["dimming"]; ok {
		wizParams["dimming"] = cmdToInt(b)
	} else if b, ok := command["brightness"]; ok {
		wizParams["dimming"] = cmdToInt(b)
	}

	if temp, ok := command["temp"]; ok {
		wizParams["temp"] = cmdToInt(temp)
	} else if kelvin, ok := command["color_temp"]; ok {
		wizParams["temp"] = cmdToInt(kelvin)
	} else if mireds, ok := command["color_temp_mireds"]; ok {
		kelvin := wiz.MiredsToKelvin(cmdToInt(mireds))
		wizParams["temp"] = kelvin
	}

	if r, ok := command["r"]; ok {
		wizParams["r"] = cmdToInt(r)
		wizParams["g"] = cmdToInt(command["g"])
		wizParams["b"] = cmdToInt(command["b"])
	}

	if sid, ok := command["sceneId"]; ok {
		wizParams["sceneId"] = cmdToInt(sid)
	} else if sid, ok := command["scene_id"]; ok {
		wizParams["sceneId"] = cmdToInt(sid)
	}

	// Send to all target devices
	var success, failed int
	for _, di := range infos {
		_, wizErr := e.wiz.SendCommand(di.IP, "setPilot", wizParams)
		if wizErr != nil {
			slog.Warn("WiZ command failed", "ip", di.IP, "error", wizErr, "cmdID", cmd.ID)
			failed++
		} else {
			slog.Info("WiZ command OK", "ip", di.IP, "cmdID", cmd.ID)
			success++
		}
	}

	status := store.DeviceCommandStatusDone
	if success == 0 && failed > 0 {
		status = store.DeviceCommandStatusFailed
	}
	if err := e.cmdStore.MarkDone(ctx, cmd.ID, status); err != nil {
		slog.Error("mark command done failed", "id", cmd.ID, "error", err)
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

// sleepCtx sleeps for the given duration or until the context is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
