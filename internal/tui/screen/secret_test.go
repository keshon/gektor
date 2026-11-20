package screen

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSecretResultsCannotReadOrChangeReplacement(t *testing.T) {
	var controls, other Controls
	controls.Props.Width, controls.Props.Height = 80, 24
	controls.PresentSecret("first", "")
	old := controls.DialogID()
	controls.SecretInput(tea.PasteMsg{Content: "discarded"})
	controls.PresentSecret("replacement", "")
	current := controls.DialogID()
	controls.SecretInput(tea.PasteMsg{Content: "retained"})
	if value, ok := controls.TakeSecret(old); ok || value != "" {
		t.Fatal("stale event read the replacement secret")
	}
	if controls.SetSecretError(old, errors.New("stale")) || other.SetSecretError(current, errors.New("foreign")) {
		t.Fatal("stale or foreign result accepted")
	}
	if !controls.SetSecretError(current, errors.New("store failed")) || !strings.Contains(controls.DialogView(), "store failed") {
		t.Fatal("current validation feedback missing")
	}
	if value, ok := controls.TakeSecret(current); !ok || value != "retained" {
		t.Fatal("current secret was consumed by a stale event")
	}
}

func TestSecretInputIsMaskedAndConsumedOutsideOrdinaryForms(t *testing.T) {
	controls := &Controls{}
	controls.Props.Width, controls.Props.Height = 80, 24
	controls.PresentSecret("Save encrypted credential", "Windows account storage")
	controls.SecretInput(tea.PasteMsg{Content: "private-secret-sentinel"})
	rendered := controls.DialogView()
	if strings.Contains(rendered, "private-secret-sentinel") || controls.dialog.Form != nil {
		t.Fatal("secret entered ordinary form or rendered plaintext")
	}
	if strings.Contains(fmt.Sprintf("%#v", controls.dialog.Secret), "sentinel") {
		t.Fatal("diagnostic representation exposed secret")
	}
	controls.TakeEvents()
	controls.SecretInput(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	events := controls.TakeEvents()
	if len(events) != 1 || events[0].Kind != SecretApply || events[0].Values != nil {
		t.Fatal("secret entered ordinary submission values")
	}
	value := controls.dialog.Secret.Take()
	if value != "private-secret-sentinel" || controls.dialog.Secret.Take() != "" {
		t.Fatal("secret was not consumed once")
	}
}

func TestSecretInputClearsOnEveryExitAndRejectsMultilineAndOversize(t *testing.T) {
	for _, exit := range []string{"close", "replace", "suspend"} {
		controls := &Controls{}
		controls.PresentSecret("Secret", "Account storage")
		controls.SecretInput(tea.PasteMsg{Content: "private-secret-sentinel"})
		previous := controls.dialog
		switch exit {
		case "close":
			controls.Close()
		case "replace":
			controls.Open("Other", nil)
		case "suspend":
			controls.Suspend()
		}
		if previous.Secret.Take() != "" {
			t.Fatal(exit, "retained secret")
		}
	}
	controls := &Controls{}
	controls.PresentSecret("Secret", "Account storage")
	for _, value := range []string{"line\nbreak", strings.Repeat("x", 16385)} {
		controls.SecretInput(tea.PasteMsg{Content: value})
		if controls.dialog.Secret.Error == "" || controls.dialog.Secret.Take() != "" {
			t.Fatal("invalid secret retained or silently truncated")
		}
	}
}
