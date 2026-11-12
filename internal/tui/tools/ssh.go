package tools

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
)

func (m *Model) SSH(editable bool) screen.Dialog {
	profiles, err := m.SSHProfiles.Load()
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	slices.Sort(names)

	rows := make([]table.Row, 0, len(names))
	for _, name := range names {
		p := profiles[name]
		auth := "Key file"
		if p.PasswordEnv != "" {
			auth = "Password environment"
		}
		rows = append(rows, table.Row{ID: name, Cells: []string{clean(name), clean(p.User + "@" + p.Host + ":" + strconv.Itoa(p.Port)), auth}})
	}

	description := "Key files and environment references only; enter variable names, never passwords. Test verifies host key and authentication, then closes. Save and restart rebuilds both runtimes; restart independent workflow workers separately."
	if len(rows) == 0 {
		description = "No SSH connections. Add one to configure commands and file transfers. " + description
	}
	if m.checking != "" {
		description = "Testing " + clean(m.checking) + "… Close to cancel. " + description
	}
	if err != nil {
		description = "Configuration error: " + clean(err.Error())
	}
	enabled := editable && err == nil && m.checking == ""
	return screen.Dialog{Title: "SSH connections", Table: &screen.Table{
		Model: newTable(rows), Columns: []table.Column{{Label: "Name", Width: 16}, {Label: "Endpoint"}, {Label: "Authentication", Width: 21, HideNarrow: true}}, Description: description,
		Actions: []table.Action{
			{ID: "add", Label: "Add", Key: "a", Enabled: enabled},
			{ID: "edit", Label: "Edit", Key: "enter", Enabled: enabled && len(rows) > 0},
			{ID: "test", Label: "Test connection", Key: "t", Enabled: err == nil && len(rows) > 0 && m.checking == ""},
			{ID: "delete", Label: "Delete", Key: "d", Enabled: enabled && len(rows) > 0},
			{ID: "operations", Label: "Operations", Key: "o", Enabled: m.checking == ""},
		},
	}}
}

func (m *Model) SSHPreferences(name string) (screen.Form, error) {
	profiles, err := m.SSHProfiles.Load()
	if err != nil {
		return screen.Form{}, err
	}

	p := profiles[name]
	auth := "key file"
	if p.PasswordEnv != "" {
		auth = "password environment"
	}
	if p.Port == 0 {
		p.Port = 22
	}

	fields := []form.Field{}
	if name == "" {
		fields = append(fields, form.Field{Key: "name", Label: "Connection name"})
	}
	fields = append(fields,
		form.Field{Key: "host", Label: "Host", Value: p.Host},
		form.Field{Key: "port", Label: "Port", Value: strconv.Itoa(p.Port)},
		form.Field{Key: "user", Label: "User", Value: p.User},
		form.Field{Key: "known_hosts", Label: "Known hosts file", Value: p.KnownHosts},
		form.Field{Key: "auth", Label: "Authentication", Value: auth},
		form.Field{Key: "private_key", Label: "Private key file", Value: p.PrivateKey},
		form.Field{Key: "passphrase_env", Label: "Key passphrase variable", Value: p.PassphraseEnv},
		form.Field{Key: "password_env", Label: "Password variable", Value: p.PasswordEnv},
	)

	title := "Add SSH connection"
	if name != "" {
		title = "Edit SSH " + name
	}
	return screen.Form{Title: title, Hint: "Variable names only, never secret values. Files require absolute paths. Authentication selects which references are saved. Apply restarts GEKTOR; restart independent workers separately.", ApplyLabel: "Apply and restart", Fields: fields,
		Choices:    map[string][]string{"auth": {"key file", "password environment"}},
		FileFields: map[string]settings.FilePicker{"known_hosts": {}, "private_key": {}},
	}, nil
}

func (m *Model) SaveSSH(name string, values map[string]string) error {
	profiles, err := m.SSHProfiles.Load()
	if err != nil {
		return err
	}

	if name == "" {
		name = strings.TrimSpace(values["name"])
		if _, exists := profiles[name]; exists {
			return fmt.Errorf("SSH connection already exists")
		}
	} else if _, exists := profiles[name]; !exists {
		return fmt.Errorf("SSH connection no longer exists")
	}

	port, err := strconv.Atoi(strings.TrimSpace(values["port"]))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be a number from 1 to 65535")
	}

	p := ssh.Profile{
		Host:       strings.TrimSpace(values["host"]),
		Port:       port,
		User:       strings.TrimSpace(values["user"]),
		KnownHosts: strings.TrimSpace(values["known_hosts"]),
	}
	switch values["auth"] {
	case "key file":
		p.PrivateKey = strings.TrimSpace(values["private_key"])
		p.PassphraseEnv = strings.TrimSpace(values["passphrase_env"])
	case "password environment":
		p.PasswordEnv = strings.TrimSpace(values["password_env"])
	default:
		return fmt.Errorf("choose an authentication method")
	}

	profiles[name] = p
	return m.SSHProfiles.Save(profiles)
}

func (m *Model) DeleteSSH(name, confirmation string) error {
	if confirmation != name {
		return fmt.Errorf("type the connection name to confirm deletion")
	}
	profiles, err := m.SSHProfiles.Load()
	if err != nil {
		return err
	}
	if _, exists := profiles[name]; !exists {
		return fmt.Errorf("SSH connection no longer exists")
	}
	delete(profiles, name)
	return m.SSHProfiles.Save(profiles)
}

type SSHCheckResult struct {
	id   uint64
	name string
	err  error
	at   time.Time
}

func (m *Model) CheckSSH(ctx context.Context, name string) tea.Cmd {
	m.Close()
	ctx, cancel := context.WithCancel(ctx)
	m.cancel, m.done, m.checking = cancel, make(chan struct{}), name
	id, done := m.epoch, m.done
	service := ssh.New(ssh.Config{ProfilesPath: m.SSHProfiles.Path})
	results := make(chan SSHCheckResult, 1)
	go func() {
		defer close(done)
		err := service.Check(ctx, name)
		results <- SSHCheckResult{id: id, name: name, err: err, at: time.Now()}
	}()
	return func() tea.Msg { return <-results }
}

func (m *Model) AcceptSSH(result SSHCheckResult) bool {
	if result.id != m.epoch || m.cancel == nil {
		return false
	}
	<-m.done
	m.cancel()
	m.cancel, m.done, m.checking = nil, nil, ""
	return true
}

func (result SSHCheckResult) Dialog() screen.Dialog {
	status := "Host key and authentication verified."
	if result.err != nil {
		status = "Connection test failed: " + clean(result.err.Error())
	}
	return screen.Dialog{Title: "SSH " + clean(result.name), Lines: []string{status, "Checked: " + result.at.UTC().Format(time.RFC3339), "", "Connection closed. No command or file transfer was attempted.", "SFTP and hard-link support were not tested."}}
}
