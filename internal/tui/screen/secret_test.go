package screen

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSecretInputIsMaskedAndConsumedOutsideOrdinaryForms(t *testing.T) {
	controls := &Controls{}
	controls.Props.Width, controls.Props.Height = 80, 24
	controls.PresentSecret("Save encrypted credential", "Windows account storage")
	controls.SecretInput(tea.PasteMsg{Content: "private-secret-sentinel"})
	rendered := controls.DialogView()
	if strings.Contains(rendered, "private-secret-sentinel") || controls.Dialog.Form != nil {
		t.Fatal("secret entered ordinary form or rendered plaintext")
	}
	if strings.Contains(fmt.Sprintf("%#v", controls.Dialog.Secret), "sentinel") {
		t.Fatal("diagnostic representation exposed secret")
	}
	controls.TakeEvents()
	controls.SecretInput(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	events := controls.TakeEvents()
	if len(events) != 1 || events[0].Kind != SecretApply || events[0].Values != nil {
		t.Fatal("secret entered ordinary submission values")
	}
	value := controls.Dialog.Secret.Take()
	if value != "private-secret-sentinel" || controls.Dialog.Secret.Take() != "" {
		t.Fatal("secret was not consumed once")
	}
}

func TestSecretInputClearsOnEveryExitAndRejectsMultilineAndOversize(t *testing.T) {
	for _, exit := range []string{"close", "replace", "suspend"} {
		controls := &Controls{}
		controls.PresentSecret("Secret", "Account storage")
		controls.SecretInput(tea.PasteMsg{Content: "private-secret-sentinel"})
		previous := controls.Dialog
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
		if controls.Dialog.Secret.Error == "" || controls.Dialog.Secret.Take() != "" {
			t.Fatal("invalid secret retained or silently truncated")
		}
	}
}
