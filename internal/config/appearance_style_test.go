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

func TestAppearanceCaretValidation(t *testing.T) {
	for _, caret := range []string{"", "block", "bar", "underline"} {
		appearance := Appearance{Caret: caret}
		err := appearance.Validate()
		if err != nil {
			t.Fatal(caret, err)
		}
	}
	appearance := Appearance{Caret: "beam"}
	if appearance.Validate() == nil {
		t.Fatal("unknown caret accepted")
	}
}
