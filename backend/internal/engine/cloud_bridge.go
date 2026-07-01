package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/wiz"
)

const (
	cloudBridgeCommandInterval = 2 * time.Second
	cloudBridgeStatusInterval  = time.Duration(StatusPollEvery) * EngineInterval
)

type CloudBridge struct {
	cfg    *config.Config
	wiz    *wiz.Client
	client *http.Client
}

type cloudClaimResponse struct {
	Commands []cloudCommand `json:"commands"`
}

type cloudCommand struct {
	ID      string          `json:"id"`
	Command json.RawMessage `json:"command"`
	Devices []cloudDevice   `json:"devices"`
}

type cloudDevice struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	IPAddress  string         `json:"ip_address"`
	DeviceType string         `json:"device_type"`
	Status     string         `json:"status,omitempty"`
	State      map[string]any `json:"current_state,omitempty"`
}

type cloudCompleteRequest struct {
	Status string `json:"status"`
}

type cloudStatusRequest struct {
	Status       string         `json:"status,omitempty"`
	CurrentState map[string]any `json:"current_state,omitempty"`
}

// RunCloudBridge starts a local LAN bridge that talks to the Render API over HTTP.
func RunCloudBridge(ctx context.Context, cfg *config.Config) {
	bridge := &CloudBridge{
		cfg: cfg,
		wiz: wiz.NewClient(),
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
	bridge.Run(ctx)
}

func (b *CloudBridge) Run(ctx context.Context) {
	slog.Info("🌉 cloud bridge starting", "api", b.cfg.BridgeAPIURL)

	var done chan struct{}
	if b.cfg.BridgeStatusPollEnabled {
		done = make(chan struct{})
		go func() {
			defer close(done)
			b.loopStatus(ctx)
		}()
	}

	b.loopCommands(ctx)
	if done != nil {
		<-done
	}
	slog.Info("🛑 cloud bridge stopped")
}

func (b *CloudBridge) loopCommands(ctx context.Context) {
	slog.Info("🌉 cloud command poller started (HTTP)")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := b.pollCloudCommands(ctx); err != nil {
			slog.Warn("cloud command poll failed", "error", err)
		}
		sleepCtx(ctx, cloudBridgeCommandInterval)
	}
}

func (b *CloudBridge) pollCloudCommands(ctx context.Context) error {
	var resp cloudClaimResponse
	if err := b.doJSON(ctx, http.MethodPost, "/bridge/commands/claim", map[string]int{"limit": 25}, &resp); err != nil {
		return err
	}
	for _, cmd := range resp.Commands {
		b.processCloudCommand(ctx, cmd)
	}
	return nil
}

func (b *CloudBridge) processCloudCommand(ctx context.Context, cmd cloudCommand) {
	var command map[string]any
	if err := json.Unmarshal(cmd.Command, &command); err != nil {
		slog.Warn("cloud command unmarshal failed", "cmdID", cmd.ID, "error", err)
		_ = b.completeCommand(ctx, cmd.ID, "failed")
		return
	}

	params := commandToWizParams(command)
	var success, failed int
	for _, device := range cmd.Devices {
		if device.IPAddress == "" {
			failed++
			continue
		}
		if _, err := b.wiz.SendCommand(device.IPAddress, "setPilot", params); err != nil {
			slog.Warn("WiZ cloud command failed", "cmdID", cmd.ID, "device", device.Name, "ip", device.IPAddress, "error", err)
			failed++
			continue
		}
		slog.Info("WiZ cloud command OK", "cmdID", cmd.ID, "device", device.Name, "ip", device.IPAddress)
		success++
	}

	status := "done"
	if success == 0 && failed > 0 {
		status = "failed"
	}
	if err := b.completeCommand(ctx, cmd.ID, status); err != nil {
		slog.Warn("cloud command completion failed", "cmdID", cmd.ID, "status", status, "error", err)
	}
}

func (b *CloudBridge) completeCommand(ctx context.Context, id, status string) error {
	return b.doJSON(ctx, http.MethodPost, "/bridge/commands/"+id+"/complete", cloudCompleteRequest{Status: status}, nil)
}

func (b *CloudBridge) loopStatus(ctx context.Context) {
	slog.Info("🌉 cloud status poller started (HTTP)", "interval", cloudBridgeStatusInterval.String())
	b.pollCloudDeviceStatus(ctx)

	ticker := time.NewTicker(cloudBridgeStatusInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.pollCloudDeviceStatus(ctx)
		}
	}
}

func (b *CloudBridge) pollCloudDeviceStatus(ctx context.Context) {
	var devices []cloudDevice
	if err := b.doJSON(ctx, http.MethodGet, "/devices", nil, &devices); err != nil {
		slog.Warn("cloud device list failed", "error", err)
		return
	}

	var wg sync.WaitGroup
	for _, device := range devices {
		if device.ID == "" || device.IPAddress == "" {
			continue
		}

		wg.Add(1)
		go func(dev cloudDevice) {
			defer wg.Done()
			
			state, err := b.wiz.GetState(dev.IPAddress)
			status := "online"
			var currentState map[string]any
			if err != nil {
				status = "offline"
			} else {
				currentState = map[string]any{
					"on":         state.On,
					"brightness": state.Brightness,
					"color_temp": state.ColorTemp,
					"r":          state.R,
					"g":          state.G,
					"b":          state.B,
					"scene_id":   state.SceneID,
				}
			}

			err = b.doJSON(ctx, http.MethodPost, "/bridge/devices/"+dev.ID+"/status", cloudStatusRequest{
				Status:       status,
				CurrentState: currentState,
			}, nil)
			if err != nil {
				slog.Warn("cloud device status update failed", "device", dev.Name, "ip", dev.IPAddress, "error", err)
			}
		}(device)
	}
	wg.Wait()
	slog.Info("✅ cloud status poll done", "count", len(devices))
}

func (b *CloudBridge) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, b.bridgeURL(path), reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", b.cfg.BridgeAPIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("%s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}

	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (b *CloudBridge) bridgeURL(path string) string {
	base := strings.TrimRight(b.cfg.BridgeAPIURL, "/")
	if strings.HasSuffix(base, "/api/v1") {
		return base + path
	}
	return base + "/api/v1" + path
}
