package config

import "testing"

func TestAppearanceStyleValidation(t *testing.T) {
	for _, style := range []string{"", "commander", "modern"} {
		appearance := Appearance{Style: style}
		if err := appearance.Validate(); err != nil {
			t.Fatal(style, err)
		}
	}
	appearance := Appearance{Style: "other"}
	if appearance.Validate() == nil {
		t.Fatal("unknown UI style was accepted")
	}
}
