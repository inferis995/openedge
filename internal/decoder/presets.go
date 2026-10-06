package decoder

import (
	"embed"
	"sort"
	"strings"
)

//go:embed presets/*.js
var presetFiles embed.FS

// Preset is a decoder that ships with OpenEdge for a known device.
type Preset struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Script string `json:"script"`
}

var presetNames = map[string]string{
	"mido": "MiDo data logger (UP/AN, base64)",
}

// Presets lists the shipped decoders.
func Presets() []Preset {
	entries, _ := presetFiles.ReadDir("presets")
	out := make([]Preset, 0, len(entries))
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".js")
		b, err := presetFiles.ReadFile("presets/" + e.Name())
		if err != nil {
			continue
		}
		name := presetNames[id]
		if name == "" {
			name = id
		}
		out = append(out, Preset{ID: id, Name: name, Script: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// PresetScript returns the script of a shipped decoder.
func PresetScript(id string) (string, bool) {
	b, err := presetFiles.ReadFile("presets/" + id + ".js")
	if err != nil || strings.ContainsAny(id, "/\\.") {
		return "", false
	}
	return string(b), true
}

// FromConfig builds a gateway's decoder from its connection_config:
// "decoder_preset" names a shipped one, "decoder_script" is a custom script
// (it wins when both are set). Neither set: no decoder, (nil, nil).
func FromConfig(cfg map[string]interface{}) (*Decoder, error) {
	if cfg == nil {
		return nil, nil
	}
	if src, ok := cfg["decoder_script"].(string); ok && strings.TrimSpace(src) != "" {
		return Compile(src)
	}
	if id, ok := cfg["decoder_preset"].(string); ok && strings.TrimSpace(id) != "" {
		src, found := PresetScript(strings.TrimSpace(id))
		if !found {
			return nil, &UnknownPresetError{ID: id}
		}
		return Compile(src)
	}
	return nil, nil
}

// UnknownPresetError names a decoder_preset that does not ship.
type UnknownPresetError struct{ ID string }

func (e *UnknownPresetError) Error() string { return "unknown decoder preset " + e.ID }
