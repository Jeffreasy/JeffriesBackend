package engine

import (
	"sort"

	"github.com/Jeffreasy/JeffriesBackend/internal/wiz"
)

// SceneDefinitions maps scene keys to WiZ state options.
// Mirrors lib/automations.ts SCENE_DEFINITIONS from the frontend.
var SceneDefinitions = map[string]wiz.StateOpts{
	"helder":  withOnBrightTemp(100, miredsToKelvin(200)),
	"avond":   withOnBrightTemp(60, miredsToKelvin(370)),
	"nacht":   withOnBrightTemp(15, miredsToKelvin(455)),
	"film":    withOnBrightRGB(30, 100, 0, 180),
	"focus":   withOnBrightTemp(90, miredsToKelvin(165)),
	"ochtend": withOnBrightTemp(40, miredsToKelvin(400)),
}

func knownSceneKeys() []string {
	keys := make([]string, 0, len(SceneDefinitions))
	for k := range SceneDefinitions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func miredsToKelvin(m int) int {
	if m <= 0 {
		return 4000
	}
	return int(1_000_000 / m)
}

func withOnBrightTemp(brightness, kelvin int) wiz.StateOpts {
	on := true
	return wiz.StateOpts{On: &on, Brightness: &brightness, ColorTemp: &kelvin}
}

func withOnBrightRGB(brightness, r, g, b int) wiz.StateOpts {
	on := true
	return wiz.StateOpts{On: &on, Brightness: &brightness, R: &r, G: &g, B: &b}
}
