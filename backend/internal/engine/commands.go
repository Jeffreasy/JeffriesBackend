package engine

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/Jeffreasy/JeffriesBackend/internal/wiz"
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

	commands, err := e.cmdStore.ListPending(ctx)
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
		_ = e.cmdStore.MarkDone(ctx, cmd.ID, "error")
		return
	}

	// Determine target devices
	var infos []deviceInfo
	if cmd.DeviceID != nil {
		deviceID := cmd.DeviceID.String()
		if info, ok := deviceMap[deviceID]; ok {
			infos = append(infos, info)
		}
	}
	if len(infos) == 0 {
		// Broadcast to all
		for _, info := range deviceMap {
			infos = append(infos, info)
		}
	}

	// Build WiZ setPilot params directly from frontend command
	wizParams := map[string]any{}

	if on, ok := command["on"].(bool); ok {
		wizParams["state"] = on
	} else {
		wizParams["state"] = true
	}

	if b, ok := command["brightness"]; ok {
		wizParams["dimming"] = cmdToInt(b)
	}

	if mireds, ok := command["color_temp_mireds"]; ok {
		kelvin := wiz.MiredsToKelvin(cmdToInt(mireds))
		wizParams["temp"] = kelvin
	}

	if r, ok := command["r"]; ok {
		wizParams["r"] = cmdToInt(r)
		wizParams["g"] = cmdToInt(command["g"])
		wizParams["b"] = cmdToInt(command["b"])
	}

	if sid, ok := command["scene_id"]; ok {
		wizParams["sceneId"] = cmdToInt(sid)
	}

	// Send to all target devices
	for _, di := range infos {
		_, wizErr := e.wiz.SendCommand(di.IP, "setPilot", wizParams)
		if wizErr != nil {
			slog.Warn("WiZ command failed", "ip", di.IP, "error", wizErr, "cmdID", cmd.ID)
		} else {
			slog.Info("WiZ command OK", "ip", di.IP, "cmdID", cmd.ID)
		}
	}

	if err := e.cmdStore.MarkDone(ctx, cmd.ID, "done"); err != nil {
		slog.Error("mark command done failed", "id", cmd.ID, "error", err)
	}
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

// Ensure wiz import is used
var _ = wiz.MiredsToKelvin
