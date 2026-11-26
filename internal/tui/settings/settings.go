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
	"github.com/keshon/gektor/pkg/tui/screen"
)

type EditSession struct {
	Draft               *Draft
	Section, LoadedPath string
	PersistCurrent      bool
	AccountLabel        string
	SavedKey            bool
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
	f.FieldLabels = map[string]string{"kind": "Provider", "endpoint": "Server address"}
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
	f.InlineChoices = map[string]int{"theme": 6, "share-user-name": 2}
	for _, field := range f.Fields {
		if len(f.Choices[field.Key]) == 0 {
			f.Choices[field.Key] = Choices(field.Key)
		}
		labels := map[string]string{}
		for _, value := range f.Choices[field.Key] {
			labels[value] = ChoiceLabel(field.Key, value)
		}
		f.Labels[field.Key] = labels
		if field.Key == "kind" {
			if f.previousKind != "" && f.previousKind != field.Value {
				next, nextErr := providers.Lookup(field.Value)
				previous, previousErr := providers.Lookup(f.previousKind)
				switchingPreset := nextErr == nil && previousErr == nil && (next.Preset || previous.Preset)
				for j := range f.Fields {
					switch f.Fields[j].Key {
					case "endpoint":
						if switchingPreset {
							f.Fields[j].Value = next.DefaultURL
						} else {
							f.Fields[j].Value = SuggestedBackendURL(f.Fields[j].Value, f.previousKind, field.Value)
						}
					case "model":
						if switchingPreset {
							f.Fields[j].Value = ""
						}
					}
				}
				var endpoint string
				for _, current := range f.Fields {
					if current.Key == "endpoint" {
						endpoint = current.Value
					}
				}
				mode, env := authenticationFields(config.Provider{Kind: field.Value, URL: endpoint})
				for j := range f.Fields {
					switch f.Fields[j].Key {
					case "auth":
						f.Fields[j].Value = string(mode)
					case "key-env":
						f.Fields[j].Value = env
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
		if _, _, ok := f.SavedKeyProvider(); ok {
			f.Shortcuts = append([]popup.Shortcut{{Key: "F6", Action: "API key"}}, f.Shortcuts...)
			f.Note = "F6 opens the API key dialog."
			if f.EditSession.SavedKey {
				f.Note = "API key saved. Pick a model or apply the connection."
			}
		}
	}
	for i, field := range f.Fields {
		f.Hidden[field.Key] = !f.FieldVisible(i)
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
	case "ctrl+d":
		if f.EditSession.Section == "profiles" {
			return fieldform.Intent{Action: fieldform.SaveCopy}, true
		}
	}
	return fieldform.Intent{}, false
}

func (f *Model) FieldVisible(index int) bool {
	if index >= 0 && index < len(f.Fields) && f.EditSession != nil {
		switch f.Fields[index].Key {
		case "network-host", "network-port":
			return f.Fields[0].Value != ""
		}
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
			case "auth":
				return descriptor.Auth == providers.Bearer
			case "key-env":
				return descriptor.Auth == providers.Bearer && f.fieldValue("auth") == string(providers.KeyEnv)
			case "endpoint":
				if descriptor.FixedURL != "" {
					return false
				}
			}
		}
	}

	return true
}

func (f *Model) SavedKeyProvider() (string, string, bool) {
	if f.EditSession == nil || f.EditSession.Section != "model" || f.fieldValue("auth") != string(providers.KeySaved) {
		return "", "", false
	}

	d, err := providers.Lookup(f.providerKind())
	endpoint := f.fieldValue("endpoint")
	return d.ID, endpoint, err == nil && d.Auth == providers.Bearer
}

func (f *Model) fieldValue(key string) string {
	for _, field := range f.Fields {
		if field.Key == key {
			return field.Value
		}
	}
	return ""
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

	return f.EditSession.PersistCurrent || f.EditSession.SavedKey || f.EditSession.Draft.Dirty(f.EditSession.Section, f.Fields)
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
		return "Ctrl+D saves a copy" + screen.Separator() + "Apply selects this profile for subsequent requests and launches"
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
