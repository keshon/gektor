// Package settings owns GEKTOR's staged preferences and their form presentation.
package settings

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/providers"
	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/components/popup"
)

type EditSession struct {
	Draft                    *Draft
	Section, LoadedPath      string
	Advanced, PersistCurrent bool
	AccountLabel             string
}

// Model owns staged preference policy and copied page values. Controls own the
// editor, focus, picker and scrolling; no widget pointer crosses this boundary.
type Model struct {
	fieldform.Presentation
	Fields                        []form.Field
	EditSession                   *EditSession
	previousKind, selectedProfile string
	loadError                     error
}

// Prepare supplies application choices and visibility without retaining effects
// in the shared controls. The application calls it after input and before drawing.
func (f *Model) Prepare(snapshot fieldform.Snapshot) fieldform.Presentation {
	f.Fields = slices.Clone(snapshot.Fields)
	f.Error, f.FieldErrors = snapshot.Error, maps.Clone(snapshot.FieldErrors)
	f.Values = map[string]string{}
	f.FieldLabels = map[string]string{"kind": "Protocol", "endpoint": "Server address"}
	if f.Choices == nil {
		f.Choices = map[string][]string{}
	}
	if f.Labels == nil {
		f.Labels = map[string]map[string]string{}
	}
	if f.FileFields == nil {
		f.FileFields = map[string]fieldform.FilePicker{}
	}
	f.Hidden = map[string]bool{}
	f.DiscoveryFields = map[string]string{"model": "model", "reasoning-effort": "reasoning"}
	f.NoSearchFields = map[string]bool{"theme": true}
	f.InlineChoices = map[string]int{"theme": 6}
	for i, field := range f.Fields {
		if len(f.Choices[field.Key]) == 0 {
			f.Choices[field.Key] = Choices(field.Key)
		}
		labels := map[string]string{}
		for _, value := range f.Choices[field.Key] {
			labels[value] = ChoiceLabel(field.Key, value)
		}
		f.Labels[field.Key] = labels
		f.Hidden[field.Key] = !f.FieldVisible(i)
		if field.Key == "kind" {
			if f.previousKind != "" && f.previousKind != field.Value {
				for j := range f.Fields {
					if f.Fields[j].Key == "endpoint" {
						f.Fields[j].Value = SuggestedBackendURL(f.Fields[j].Value, f.previousKind, field.Value)
					}
				}
			}
			f.previousKind = field.Value
		}
		if field.Key == "app-workspace" {
			f.FileFields[field.Key] = fieldform.FilePicker{Root: field.Value, Directory: true}
		}
		if field.Key == "load-profile" {
			if _, ok := f.FileFields[field.Key]; !ok {
				executable, err := os.Executable()
				if err != nil {
					f.Error = "Locating profiles: " + err.Error()
					continue
				}
				f.FileFields[field.Key] = fieldform.FilePicker{Root: filepath.Join(filepath.Dir(executable), "profiles"), Extensions: []string{".json"}, Recursive: true}
			}
			if f.EditSession != nil && field.Value != "" && field.Value != f.selectedProfile {
				f.stageProfile(field.Value)
			}
		}
	}
	if f.EditSession != nil {
		f.Hint = Hint(f.EditSession.Section)
		f.Note = f.note()
		f.DisablePrimary = !f.Dirty() || f.loadError != nil
		f.Shortcuts = []popup.Shortcut{{Key: "Esc", Action: "cancel"}, {Key: "Ctrl+S", Action: "apply"}, {Key: "Tab", Action: "controls"}}
	}
	for i, field := range f.Fields {
		if field.Value != snapshot.Fields[i].Value {
			f.Values[field.Key] = field.Value
		}
	}
	return f.Presentation
}

// Shortcut handles preference-specific actions; generic editing stays in controls.
func (f *Model) Shortcut(key string) (fieldform.Intent, bool) {
	if f.EditSession == nil {
		return fieldform.Intent{}, false
	}
	switch key {
	case "f4":
		f.EditSession.Advanced = !f.EditSession.Advanced
		return fieldform.Intent{}, true
	case "ctrl+d":
		if f.EditSession.Section == "profiles" {
			return fieldform.Intent{Action: fieldform.SaveCopy}, true
		}
	}
	return fieldform.Intent{}, false
}

func (f *Model) FieldVisible(index int) bool {
	if index >= 0 && index < len(f.Fields) && f.EditSession != nil {
		descriptor, lookupErr := providers.Lookup(f.providerKind())
		if lookupErr == nil {
			switch f.Fields[index].Key {
			case "reasoning-effort", "reasoning-summary":
				return descriptor.Reasoning
			case "temperature":
				if !descriptor.Sampling {
					return false
				}
			case "max":
				if !descriptor.OutputLimit {
					return false
				}
			case "key-env":
				if descriptor.Auth != providers.Bearer {
					return false
				}
			case "endpoint":
				if descriptor.FixedURL != "" {
					return false
				}
			}
		}
	}

	if index >= len(f.Fields) || f.EditSession == nil || f.EditSession.Section != "model" {
		return true
	}

	if f.Fields[index].Key == "key-env" {
		return f.EditSession.Advanced
	}

	return true
}

func (f *Model) providerKind() string {
	if f.EditSession == nil {
		return ""
	}

	if f.EditSession.Section == "model" {
		for _, field := range f.Fields {
			if field.Key == "kind" {
				return field.Value
			}
		}
	}

	if fields, ok := f.EditSession.Draft.Fields("model"); ok {
		for _, field := range fields {
			if field.Key == "kind" {
				return field.Value
			}
		}
	}

	return f.EditSession.Draft.Base().Provider.Kind
}

func (f *Model) StorePage() {
	f.EditSession.Draft.Store(f.EditSession.Section, f.Fields)
}

func (f *Model) Dirty() bool {
	if f.EditSession == nil {
		return true
	}

	return f.EditSession.PersistCurrent || f.EditSession.Draft.Dirty(f.EditSession.Section, f.Fields)
}
func (f *Model) note() string {
	descriptor, lookupErr := providers.Lookup(f.providerKind())
	if lookupErr == nil && descriptor.Auth == providers.ChatGPTAccount {
		if f.EditSession.Section == "generation" {
			return "Effort choices come from this model; thinking shows a summary when available"
		}
		if f.EditSession.Section == "model" {
			if f.EditSession.AccountLabel != "" {
				return "Account: " + f.EditSession.AccountLabel + " | gektor auth login/logout"
			}
			return "Connect: gektor auth login | Account: gektor auth status"
		}
	}
	switch {
	case f.Error != "":
		return ""
	case f.EditSession.Section == "generation":
		return "Temperature: default uses " + strconv.FormatFloat(DefaultTemperature, 'g', -1, 64)
	case f.EditSession.Section == "profiles":
		return "Ctrl+D saves a copy · Apply selects this profile for subsequent requests and launches"
	case f.EditSession.PersistCurrent:
		return "Apply saves the current launch settings"
	case f.Dirty():
		return "Apply saves a custom profile"
	default:
		return ""
	}
}

func (f *Model) stageProfile(path string) {
	f.selectedProfile = path
	loaded, err := config.Load(path)
	f.loadError = err
	if err != nil {
		f.Error = err.Error()
		f.FieldErrors = map[string]string{"load-profile": err.Error()}
		return
	}

	f.FieldErrors = nil
	f.EditSession.LoadedPath = path
	f.EditSession.Draft.Load(loaded)
	f.Error = "Profile loaded. Apply to use it."
}
