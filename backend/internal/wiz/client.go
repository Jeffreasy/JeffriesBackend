package wiz

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net"
	"time"
)

const (
	UDPPort    = 38899
	UDPTimeout = 3 * time.Second
)

// State represents the parsed state from a WiZ bulb getPilot response.
type State struct {
	On         bool `json:"on"`
	Brightness int  `json:"brightness"`
	ColorTemp  int  `json:"color_temp"`
	R          int  `json:"r"`
	G          int  `json:"g"`
	B          int  `json:"b"`
	Speed      int  `json:"speed"`
	SceneID    int  `json:"scene_id"`
}

// Client controls WiZ smart bulbs via local UDP.
type Client struct{}

// NewClient creates a new WiZ UDP client.
func NewClient() *Client {
	return &Client{}
}

// SendCommand sends a UDP command to a WiZ bulb and returns the response.
func (c *Client) SendCommand(ip, method string, params map[string]any) (map[string]any, error) {
	payload := map[string]any{
		"method": method,
		"params": params,
	}
	if params == nil {
		payload["params"] = map[string]any{}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal command: %w", err)
	}

	addr := net.JoinHostPort(ip, fmt.Sprintf("%d", UDPPort))
	conn, err := net.DialTimeout("udp", addr, UDPTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial udp %s: %w", addr, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(UDPTimeout)); err != nil {
		return nil, fmt.Errorf("set deadline: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("send udp: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("read udp response: %w", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return resp, nil
}

// GetState reads the current bulb state.
func (c *Client) GetState(ip string) (*State, error) {
	resp, err := c.SendCommand(ip, "getPilot", nil)
	if err != nil {
		return nil, err
	}
	return parseState(resp), nil
}

// TurnOn turns the bulb on.
func (c *Client) TurnOn(ip string) error {
	_, err := c.SendCommand(ip, "setPilot", map[string]any{"state": true})
	return err
}

// TurnOff turns the bulb off.
func (c *Client) TurnOff(ip string) error {
	_, err := c.SendCommand(ip, "setPilot", map[string]any{"state": false})
	return err
}

// SetBrightness sets brightness (10-100%).
func (c *Client) SetBrightness(ip string, pct int) error {
	pct = clamp(pct, 10, 100)
	_, err := c.SendCommand(ip, "setPilot", map[string]any{
		"state":   true,
		"dimming": pct,
	})
	return err
}

// SetColorTemp sets white color temperature in Kelvin (2200-6500).
func (c *Client) SetColorTemp(ip string, kelvin int) error {
	kelvin = clamp(kelvin, 2200, 6500)
	_, err := c.SendCommand(ip, "setPilot", map[string]any{
		"state": true,
		"temp":  kelvin,
	})
	return err
}

// SetColor sets RGB color (0-255 per channel).
func (c *Client) SetColor(ip string, r, g, b int) error {
	_, err := c.SendCommand(ip, "setPilot", map[string]any{
		"state":   true,
		"r":       r,
		"g":       g,
		"b":       b,
		"dimming": 100,
	})
	return err
}

// SetScene activates a WiZ preset scene (1-32).
func (c *Client) SetScene(ip string, sceneID int) error {
	_, err := c.SendCommand(ip, "setPilot", map[string]any{
		"state":   true,
		"sceneId": sceneID,
	})
	return err
}

// SetState applies a generic state update.
func (c *Client) SetState(ip string, opts StateOpts) error {
	params := map[string]any{}

	if opts.On != nil {
		params["state"] = *opts.On
	}
	if opts.Brightness != nil {
		params["dimming"] = clamp(*opts.Brightness, 10, 100)
	}
	if opts.ColorTemp != nil {
		params["temp"] = clamp(*opts.ColorTemp, 2200, 6500)
	}
	if opts.R != nil && opts.G != nil && opts.B != nil {
		params["r"] = *opts.R
		params["g"] = *opts.G
		params["b"] = *opts.B
	}

	if len(params) == 0 {
		return nil
	}

	if _, ok := params["state"]; !ok {
		params["state"] = true
	}

	_, err := c.SendCommand(ip, "setPilot", params)
	return err
}

// StateOpts defines optional fields for SetState.
type StateOpts struct {
	On         *bool
	Brightness *int
	ColorTemp  *int
	R          *int
	G          *int
	B          *int
}

// MiredsToKelvin converts mireds to Kelvin.
func MiredsToKelvin(mireds int) int {
	if mireds <= 0 {
		return 4000
	}
	return int(math.Round(1_000_000.0 / float64(mireds)))
}

// HexToRGB converts a hex color string to RGB values.
func HexToRGB(hex string) (int, int, int) {
	if len(hex) > 0 && hex[0] == '#' {
		hex = hex[1:]
	}
	if len(hex) != 6 {
		return 255, 255, 255
	}
	var r, g, b int
	_, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	if err != nil {
		slog.Warn("invalid hex color", "hex", hex, "error", err)
		return 255, 255, 255
	}
	return r, g, b
}

// --- internal ---

func parseState(resp map[string]any) *State {
	p, ok := resp["result"].(map[string]any)
	if !ok {
		p, _ = resp["params"].(map[string]any)
	}
	if p == nil {
		p = map[string]any{}
	}

	return &State{
		On:         toBool(p["state"]),
		Brightness: toInt(p["dimming"], 100),
		ColorTemp:  toInt(p["temp"], 4000),
		R:          toInt(p["r"], 0),
		G:          toInt(p["g"], 0),
		B:          toInt(p["b"], 0),
		Speed:      toInt(p["speed"], 100),
		SceneID:    toInt(p["sceneId"], 0),
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func toBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func toInt(v any, fallback int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return fallback
	}
}
