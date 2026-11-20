package workflow

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow/credentials"
	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/screen"
)

type savedConnection struct {
	name, revision, label, origin string
}

func supportsSavedConnection(tool string) bool {
	switch tool {
	case "http_request", "ssh_exec", "ssh_upload", "ssh_download":
		return true
	}
	return false
}

func connectionChoices(tool string) ([]savedConnection, error) {
	choices := []savedConnection{}
	if tool == "http_request" {
		store := credentials.Store{Directory: userdirs.Sub("credentials")}
		bindings, err := store.List()
		if err != nil {
			return nil, err
		}
		for _, binding := range bindings {
			if binding.Revoked {
				continue
			}
			choices = append(choices, savedConnection{
				name:     binding.Name,
				revision: binding.Revision,
				label:    binding.Name + " / " + binding.Origin,
				origin:   binding.Origin,
			})
		}
		return choices, nil
	}

	store := ssh.Profiles{Path: userdirs.Sub("ssh.json")}
	profiles, err := store.Load()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		binding, inspectErr := store.Inspect(name)
		if inspectErr != nil {
			return nil, inspectErr
		}
		if binding.Revoked {
			continue
		}
		choices = append(choices, savedConnection{
			name:     binding.Name,
			revision: binding.Revision,
			label:    fmt.Sprintf("%s / %s@%s:%d", binding.Name, binding.User, binding.Host, binding.Port),
		})
	}
	return choices, nil
}

func checkSavedConnection(tool string, chosen savedConnection) error {
	var revision string
	var revoked bool
	if tool == "http_request" {
		store := credentials.Store{Directory: userdirs.Sub("credentials")}
		binding, err := store.Inspect(chosen.name)
		if err != nil {
			return err
		}
		revision, revoked = binding.Revision, binding.Revoked
	} else {
		store := ssh.Profiles{Path: userdirs.Sub("ssh.json")}
		binding, err := store.Inspect(chosen.name)
		if err != nil {
			return err
		}
		revision, revoked = binding.Revision, binding.Revoked
	}
	if revoked || revision != chosen.revision {
		return fmt.Errorf("connection changed or is unavailable; reopen saved connections")
	}
	return nil
}

func (m *Model) savedConnectionForm() {
	step := m.selectedStep()
	if !supportsSavedConnection(step.Tool) || m.selectedSchedule() != nil {
		m.setNotice(pane.NoticeInfo, "Select an HTTP or SSH node first")
		return
	}
	if len(step.ForEach) > 0 {
		m.setNotice(pane.NoticeInfo, "Mapped nodes take connection fields from each item's arguments")
		return
	}
	var args map[string]json.RawMessage
	err := json.Unmarshal(step.Args, &args)
	if err != nil || args == nil {
		m.setNotice(pane.NoticeError, "Node arguments must be a JSON object")
		return
	}
	bindings, err := connectionChoices(step.Tool)
	if err != nil {
		m.setNotice(pane.NoticeError, err.Error())
		return
	}

	nameKey, revisionKey := "connection", "connection_revision"
	options := []string{"Choose a connection"}
	if step.Tool == "http_request" {
		nameKey, revisionKey = "credential", "credential_revision"
		options = []string{"No credential"}
	}
	selected := ""
	var currentName string
	nameErr := json.Unmarshal(args[nameKey], &currentName)
	for _, binding := range bindings {
		options = append(options, binding.label)
		if nameErr == nil && currentName == binding.name {
			selected = binding.label
		}
	}
	if len(options) == 1 && step.Tool != "http_request" {
		m.setNotice(pane.NoticeInfo, "No active saved SSH connections · add one in Agent Settings")
		return
	}
	if selected == "" {
		selected = options[0]
	}
	current := "None"
	if len(args[nameKey]) > 0 {
		current = string(args[nameKey]) + " / " + string(args[revisionKey])
	}
	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:      step.ID + " / saved connection",
		Hint:       "Apply pins the inspected revision. HTTP credentials stay bound to their origin. This does not connect or test authentication.",
		ApplyLabel: "Apply",
		Fields: []formField{
			{Key: "current", Label: "Current name / revision", Value: current, ReadOnly: true},
			{Key: "connection", Label: "Saved connection", Value: selected},
		},
		Choices: map[string][]string{"connection": options},
	}, func(v map[string]string) error {
		if m.busy || m.run != nil || m.editor.Source() != draft {
			return fmt.Errorf("draft changed; reopen saved connections")
		}
		changed := make(map[string]json.RawMessage, len(args))
		for key, value := range args {
			changed[key] = value
		}
		remove := step.Tool == "http_request" && v["connection"] == "No credential"
		if remove {
			delete(changed, nameKey)
			delete(changed, revisionKey)
		} else {
			var chosen savedConnection
			for _, binding := range bindings {
				if binding.label == v["connection"] {
					chosen = binding
					break
				}
			}
			if chosen.name == "" {
				return fmt.Errorf("choose a saved connection")
			}
			inspectErr := checkSavedConnection(step.Tool, chosen)
			if inspectErr != nil {
				return inspectErr
			}
			var url string
			literalURL := json.Unmarshal(args["url"], &url) == nil && url != ""
			if chosen.origin != "" && literalURL {
				origin, originErr := credentials.Origin(url)
				if originErr != nil || origin != chosen.origin {
					return fmt.Errorf("credential requires origin %s", chosen.origin)
				}
			}
			nameJSON, marshalErr := json.Marshal(chosen.name)
			if marshalErr != nil {
				return marshalErr
			}
			revisionJSON, marshalErr := json.Marshal(chosen.revision)
			if marshalErr != nil {
				return marshalErr
			}
			changed[nameKey] = nameJSON
			changed[revisionKey] = revisionJSON
		}

		raw, encodeErr := json.Marshal(changed)
		if encodeErr != nil {
			return encodeErr
		}
		d := m.editor.Clone()
		for i, candidate := range d.Steps {
			if candidate.ID == step.ID {
				d.Steps[i].Args = raw
				break
			}
		}
		return m.replaceRecipe(d, draft)
	})
}
