package prompts

import "testing"

func TestPresetLibraryIsCompleteAndIndependent(t *testing.T) {
	seen := map[string]bool{}
	for _, item := range Presets() {
		if seen[item.ID] {
			t.Fatal("duplicate preset", item.ID)
		}
		seen[item.ID] = true
		preset, err := ResponsePreset(item.ID)
		if err != nil || preset.Name == "" || (preset.ID != "custom" && preset.Instruction == "") {
			t.Fatal(preset, err)
		}
	}
	if preset, err := ResponsePreset(""); err != nil || preset.ID != "custom" || preset.Instruction != "" {
		t.Fatal(preset, err)
	}
	copy := Presets()
	copy[0].Name = "Changed"
	if Presets()[0].Name == "Changed" {
		t.Fatal("caller mutated library")
	}
}
