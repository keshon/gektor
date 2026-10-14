package prompts

import "fmt"

// Preset controls response style, independently of identity and capabilities.
type Preset struct{ ID, Name, Instruction string }

func Presets() []Preset { return append([]Preset(nil), responsePresets...) }
func ResponsePreset(id string) (Preset, error) {
	if id == "" {
		id = "custom"
	}
	for _, preset := range responsePresets {
		if preset.ID == id {
			return preset, nil
		}
	}
	return Preset{}, fmt.Errorf("unknown prompt preset %q", id)
}
