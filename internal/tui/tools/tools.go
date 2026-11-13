// Package tools presents tool preferences using the application's shared controls.
package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/capabilities/media"
	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/credentials"
	"github.com/keshon/gektor/internal/executables"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/userdirs"
)

type observation struct {
	status executables.Status
	err    error
	at     time.Time
}

// Model owns tool preference views and explicit checks. Close cancels and joins pending work.
type Model struct {
	Resolver    *executables.Resolver
	SSHProfiles ssh.Profiles
	checks      map[string]observation
	cancel      context.CancelFunc
	done        chan struct{}
	epoch       uint64
	checking    string
}

type CheckResult struct {
	id          uint64
	name        string
	observation observation
}

func New(resolver *executables.Resolver) *Model {
	profiles := ssh.Profiles{
		Path:        userdirs.Sub("ssh.json"),
		Credentials: credentials.Store{Directory: userdirs.Sub("credentials", "ssh")},
	}
	return &Model{
		Resolver:    resolver,
		SSHProfiles: profiles,
		checks:      map[string]observation{},
	}
}

func (m *Model) Close() {
	m.epoch++
	if m.cancel != nil {
		m.cancel()
		<-m.done
	}
	m.cancel = nil
	m.done = nil
	m.checking = ""
}

func (m *Model) Check(ctx context.Context, name string) tea.Cmd {
	m.Close()
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.done = make(chan struct{})
	m.checking = name
	id, done, resolver := m.epoch, m.done, m.Resolver
	results := make(chan CheckResult, 1)
	go func() {
		defer close(done)
		status, err := resolver.Probe(ctx, name, true)
		results <- CheckResult{id: id, name: name, observation: observation{status: status, err: err, at: time.Now()}}
	}()
	return func() tea.Msg { return <-results }
}

func (m *Model) Accept(result CheckResult) bool {
	if result.id != m.epoch || m.cancel == nil {
		return false
	}
	<-m.done
	m.cancel()
	m.cancel = nil
	m.done = nil
	m.checking = ""
	m.checks[result.name] = result.observation
	return true
}

func newTable(rows []table.Row) *table.Model {
	model := table.New(screen.TextInputStyles(textinput.New().Styles()))
	model.SetRows(rows)
	return model
}

func Inventory(sets []app.Toolset) screen.Dialog {
	rows := make([]table.Row, 0, len(sets))
	for _, set := range sets {
		rows = append(rows, table.Row{ID: set.ID, Cells: []string{clean(set.Name), set.Group, set.Loading, set.Status}})
	}
	return screen.Dialog{Title: "Tools", Table: &screen.Table{
		Model: newTable(rows), Columns: []table.Column{{Label: "Toolset", Width: 18}, {Label: "Source", Width: 12, HideNarrow: true}, {Label: "Loading", Width: 12, HideNarrow: true}, {Label: "Status", Width: 12}},
		Description: "Choose which tools this conversation can use.",
		Actions:     []table.Action{{ID: "details", Label: "Details", Key: "enter", Enabled: true}},
	}}
}

func Details(set app.Toolset) screen.Dialog {
	lines := []string{clean(set.Description), "", "Source: " + clean(set.Source), "Loading: " + set.Loading, "Status: " + set.Status}
	if set.Connection != "" {
		lines = append(lines, "Connection: "+clean(set.Connection))
	}
	lines = append(lines, "", "Agent operations:")
	if len(set.Agent) == 0 {
		lines = append(lines, "Catalog is discovered when the extension connects.")
	}
	for _, name := range set.Agent {
		lines = append(lines, "  "+clean(name))
	}
	lines = append(lines, "", "Workflow nodes:")
	if len(set.Workflow) == 0 {
		lines = append(lines, "No matching workflow nodes in this toolset.")
	}
	for _, name := range set.Workflow {
		lines = append(lines, "  "+clean(name))
	}
	return screen.Dialog{Title: clean(set.Name), Lines: lines}
}

func (m *Model) Media(editable bool) screen.Dialog {
	config, err := m.Resolver.Load()
	rows := []table.Row{}
	description := "Saved executable preferences, shared by Agent and Workflows. Blank paths use PATH. Check runs a bounded version command. Native YouTube metadata, captions and single streams need no local executable."
	description += "\nPreferences: " + clean(m.Resolver.ConfigPath)
	if err != nil {
		description = "Settings error: " + clean(err.Error()) + "\n" + description
	}
	for _, name := range media.Applications() {
		setting := config.Applications[name]
		path := setting.Path
		if path == "" {
			path = "PATH"
		}
		state := "Unchecked"
		if setting.Disabled {
			state = "Disabled"
		}
		if checked, ok := m.checks[name]; ok {
			if !setting.Disabled && (setting.Path == "" || setting.Path == checked.status.Path) {
				state = checked.status.State
			}
			description += "\n" + name + " checked " + checked.at.UTC().Format(time.RFC3339) + ": " + clean(checked.status.Path+" "+checked.status.Version+" "+checked.status.Reason)
			if checked.err != nil {
				description += " " + clean(checked.err.Error())
			}
		}
		if m.checking == name {
			state = "Checking…"
		}
		rows = append(rows, table.Row{ID: name, Cells: []string{name, clean(state), clean(path)}})
	}
	return screen.Dialog{Title: "Media executables", Table: &screen.Table{
		Model: newTable(rows), Columns: []table.Column{{Label: "Program", Width: 10}, {Label: "Check", Width: 12}, {Label: "Path"}}, Description: description,
		Actions: []table.Action{{ID: "operations", Label: "Operations", Key: "o", Enabled: true}, {ID: "details", Label: "Preferences", Key: "enter", Enabled: editable && m.checking == "" && err == nil}, {ID: "check", Label: "Check executable", Key: "c", Enabled: m.checking == "" && err == nil}},
	}}
}

func (m *Model) Preferences(name string) (screen.Form, error) {
	config, err := m.Resolver.Load()
	if err != nil {
		return screen.Form{}, err
	}
	setting := config.Applications[name]
	return screen.Form{Title: name + " preferences", Hint: "Blank path uses PATH. Choose an absolute executable to override it. Apply saves and relaunches GEKTOR at idle; both Agent and Workflows use this preference.", ApplyLabel: "Apply and restart", Fields: []form.Field{
		{Key: "path", Label: "Executable path", Value: setting.Path},
		{Key: "disabled", Label: "Disabled", Value: strconv.FormatBool(setting.Disabled)},
	}, Choices: map[string][]string{"disabled": {"false", "true"}}, FileFields: map[string]settings.FilePicker{"path": {}}}, nil
}

func (m *Model) Save(name string, values map[string]string) error {
	disabled, err := strconv.ParseBool(values["disabled"])
	if err != nil {
		return fmt.Errorf("choose whether this executable is disabled")
	}
	config, err := m.Resolver.Load()
	if err != nil {
		return err
	}
	if config.Applications == nil {
		config.Applications = map[string]executables.Setting{}
	}
	config.Applications[name] = executables.Setting{Path: strings.TrimSpace(values["path"]), Disabled: disabled}
	return m.Resolver.Save(config)
}

func clean(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return ' '
		}
		return r
	}, ansi.Strip(text))
}

// Dialog reports the bounded version observation from one explicit check.
func (result CheckResult) Dialog() screen.Dialog {
	checked := result.observation
	lines := []string{"State: " + clean(checked.status.State), "Executable: " + clean(checked.status.Path), "Version: " + clean(checked.status.Version), "Checked: " + checked.at.UTC().Format(time.RFC3339)}
	if checked.status.Reason != "" {
		lines = append(lines, clean(checked.status.Reason))
	}
	if checked.err != nil {
		lines = append(lines, clean(checked.err.Error()))
	}
	lines = append(lines, "", "Version check only; codec support and remote access were not tested.")
	return screen.Dialog{Title: result.name + " executable check", Lines: lines}
}
