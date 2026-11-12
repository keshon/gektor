package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/credentials"
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
		auth := "Private key"
		if p.PasswordRef != "" {
			auth = "Password"
		}
		if p.PasswordEnv != "" {
			auth = "Password"
		}
		rows = append(rows, table.Row{ID: name, Cells: []string{clean(name), clean(p.User + "@" + p.Host + ":" + strconv.Itoa(p.Port)), auth}})
	}

	description := "Run commands and transfer files using a password or private key. Passwords are entered in a secure prompt. Test verifies host key and authentication, then closes. Save and restart rebuilds both runtimes; restart independent workflow workers separately."
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

const (
	sshPassword                 = "Password"
	sshPrivateKey               = "Private key"
	sshEnterPassword            = "Enter password when saving"
	sshKeepPassword             = "Saved — keep password"
	sshKeepConfiguredPassword   = "Keep current password"
	sshChangePassword           = "Change password"
	sshNoPassphrase             = "No passphrase"
	sshEnterPassphrase          = "Enter passphrase when saving"
	sshKeepPassphrase           = "Saved — keep passphrase"
	sshKeepConfiguredPassphrase = "Keep current passphrase"
	sshChangePassphrase         = "Change passphrase"
	sshEnvironment              = "Environment variable"
	sshSharedCredential         = "Another connection"
)

func (m *Model) SSHPreferences(name string) (screen.Form, error) {
	profiles, err := m.SSHProfiles.Load()
	if err != nil {
		return screen.Form{}, err
	}
	p := profiles[name]
	auth := sshPassword
	if p.PrivateKey != "" || !credentials.Encrypted() {
		auth = sshPrivateKey
	}
	password := sshEnterPassword
	passwordChoices := []string{sshEnterPassword}
	if p.PasswordRef != "" {
		password = sshKeepPassword
		passwordChoices = []string{sshKeepPassword, sshChangePassword}
	}
	passphrase := sshNoPassphrase
	passphraseChoices := []string{sshNoPassphrase, sshEnterPassphrase}
	if p.PassphraseRef != "" {
		passphrase = sshKeepPassphrase
		passphraseChoices = []string{sshKeepPassphrase, sshChangePassphrase, sshNoPassphrase}
	}
	advanced := "No"
	passwordSource, passphraseSource := password, passphrase
	if p.PasswordEnv != "" {
		auth = sshPassword
		advanced = "Yes"
		passwordSource = sshEnvironment
		password = sshKeepConfiguredPassword
		passwordChoices = []string{sshKeepConfiguredPassword, sshChangePassword}
	}
	if p.PassphraseEnv != "" {
		advanced = "Yes"
		passphraseSource = sshEnvironment
		passphrase = sshKeepConfiguredPassphrase
		passphraseChoices = []string{sshKeepConfiguredPassphrase, sshChangePassphrase, sshNoPassphrase}
	}
	if p.Port == 0 {
		p.Port = 22
	}
	if p.KnownHosts == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr == nil {
			p.KnownHosts = filepath.Join(home, ".ssh", "known_hosts")
		}
	}

	fields := []form.Field{}
	if name == "" {
		fields = append(fields, form.Field{Key: "name", Label: "Connection name"})
	}
	fields = append(fields,
		form.Field{Key: "host", Label: "Host", Value: p.Host},
		form.Field{Key: "port", Label: "Port", Value: strconv.Itoa(p.Port)},
		form.Field{Key: "user", Label: "User", Value: p.User},
		form.Field{Key: "auth", Label: "Authentication", Value: auth},
	)
	if p.PasswordRef != "" || p.PasswordEnv != "" {
		fields = append(fields, form.Field{Key: "password", Label: "Password", Value: password})
	}
	fields = append(fields,
		form.Field{Key: "private_key", Label: "Private key file", Value: p.PrivateKey},
		form.Field{Key: "passphrase", Label: "Key passphrase", Value: passphrase},
		form.Field{Key: "advanced", Label: "Advanced settings", Value: advanced},
		form.Field{Key: "known_hosts", Label: "Trusted hosts file", Value: p.KnownHosts},
		form.Field{Key: "password_source", Label: "Password source", Value: passwordSource},
		form.Field{Key: "password_env", Label: "Environment variable name", Value: p.PasswordEnv},
		form.Field{Key: "password_connection", Label: "Use password from"},
		form.Field{Key: "passphrase_source", Label: "Passphrase source", Value: passphraseSource},
		form.Field{Key: "passphrase_env", Label: "Environment variable name", Value: p.PassphraseEnv},
		form.Field{Key: "passphrase_connection", Label: "Use passphrase from"},
	)
	title := "Add SSH connection"
	if name != "" {
		title = "Edit SSH " + name
	}
	passwordConnections, passphraseConnections := []string{}, []string{}
	for other, profile := range profiles {
		if profile.PasswordRef != "" {
			passwordConnections = append(passwordConnections, other)
		}
		if profile.PassphraseRef != "" {
			passphraseConnections = append(passphraseConnections, other)
		}
	}
	slices.Sort(passwordConnections)
	slices.Sort(passphraseConnections)
	passwordSources := append(slices.Clone(passwordChoices), sshEnvironment)
	if len(passwordConnections) > 0 {
		passwordSources = append(passwordSources, sshSharedCredential)
	}
	passphraseSources := append(slices.Clone(passphraseChoices), sshEnvironment)
	if len(passphraseConnections) > 0 {
		passphraseSources = append(passphraseSources, sshSharedCredential)
	}
	return screen.Form{
		Title:      title,
		Hint:       "New passwords are entered after Save. Key files open a file picker. Advanced settings are optional.",
		ApplyLabel: "Save and restart",
		Fields:     fields,
		Choices: map[string][]string{
			"auth":                  {sshPassword, sshPrivateKey},
			"password":              passwordChoices,
			"passphrase":            passphraseChoices,
			"advanced":              {"No", "Yes"},
			"password_source":       passwordSources,
			"password_connection":   passwordConnections,
			"passphrase_source":     passphraseSources,
			"passphrase_connection": passphraseConnections,
		},
		ShowWhen: map[string][]form.Condition{
			"port":                  {{Key: "advanced", Value: "Yes"}},
			"password":              {{Key: "auth", Value: sshPassword}, {Key: "advanced", Value: "No"}},
			"private_key":           {{Key: "auth", Value: sshPrivateKey}},
			"passphrase":            {{Key: "auth", Value: sshPrivateKey}, {Key: "advanced", Value: "No"}},
			"known_hosts":           {{Key: "advanced", Value: "Yes"}},
			"password_source":       {{Key: "auth", Value: sshPassword}, {Key: "advanced", Value: "Yes"}},
			"password_env":          {{Key: "auth", Value: sshPassword}, {Key: "advanced", Value: "Yes"}, {Key: "password_source", Value: sshEnvironment}},
			"password_connection":   {{Key: "auth", Value: sshPassword}, {Key: "advanced", Value: "Yes"}, {Key: "password_source", Value: sshSharedCredential}},
			"passphrase_source":     {{Key: "auth", Value: sshPrivateKey}, {Key: "advanced", Value: "Yes"}},
			"passphrase_env":        {{Key: "auth", Value: sshPrivateKey}, {Key: "advanced", Value: "Yes"}, {Key: "passphrase_source", Value: sshEnvironment}},
			"passphrase_connection": {{Key: "auth", Value: sshPrivateKey}, {Key: "advanced", Value: "Yes"}, {Key: "passphrase_source", Value: sshSharedCredential}},
		},
		FileFields: map[string]settings.FilePicker{"known_hosts": {}, "private_key": {}},
	}, nil
}

type SSHSettings struct {
	Name      string
	Profile   ssh.Profile
	NewSecret ssh.SecretKind
}

func (m *Model) SSHSettings(name string, values map[string]string) (SSHSettings, error) {
	profiles, err := m.SSHProfiles.Load()
	if err != nil {
		return SSHSettings{}, err
	}

	if name == "" {
		name = strings.TrimSpace(values["name"])
		if _, exists := profiles[name]; exists {
			return SSHSettings{}, fmt.Errorf("SSH connection already exists")
		}
	} else if _, exists := profiles[name]; !exists {
		return SSHSettings{}, fmt.Errorf("SSH connection no longer exists")
	}

	port, err := strconv.Atoi(strings.TrimSpace(values["port"]))
	if err != nil || port < 1 || port > 65535 {
		return SSHSettings{}, fmt.Errorf("port must be a number from 1 to 65535")
	}

	p := ssh.Profile{
		Host:       strings.TrimSpace(values["host"]),
		Port:       port,
		User:       strings.TrimSpace(values["user"]),
		KnownHosts: strings.TrimSpace(values["known_hosts"]),
	}
	existing := profiles[name]
	password, passphrase := values["password"], values["passphrase"]
	if password == "" && existing.PasswordRef == "" && existing.PasswordEnv == "" {
		password = sshEnterPassword
	}
	if values["advanced"] == "Yes" {
		password, passphrase = values["password_source"], values["passphrase_source"]
	}
	settings := SSHSettings{Name: name, Profile: p}
	switch values["auth"] {
	case sshPassword:
		switch password {
		case sshKeepPassword:
			settings.Profile.PasswordRef = existing.PasswordRef
		case sshKeepConfiguredPassword:
			settings.Profile.PasswordEnv = existing.PasswordEnv
		case sshEnterPassword, sshChangePassword:
			settings.NewSecret = ssh.PasswordSecret
		case sshEnvironment:
			settings.Profile.PasswordEnv = strings.TrimSpace(values["password_env"])
		case sshSharedCredential:
			settings.Profile.PasswordRef = profiles[values["password_connection"]].PasswordRef
			if settings.Profile.PasswordRef == "" {
				return SSHSettings{}, fmt.Errorf("choose a connection with a saved password")
			}
		default:
			return SSHSettings{}, fmt.Errorf("choose how to supply the password")
		}
	case sshPrivateKey:
		settings.Profile.PrivateKey = strings.TrimSpace(values["private_key"])
		switch passphrase {
		case sshNoPassphrase:
		case sshKeepPassphrase:
			settings.Profile.PassphraseRef = existing.PassphraseRef
		case sshKeepConfiguredPassphrase:
			settings.Profile.PassphraseEnv = existing.PassphraseEnv
		case sshEnterPassphrase, sshChangePassphrase:
			settings.NewSecret = ssh.PassphraseSecret
		case sshEnvironment:
			settings.Profile.PassphraseEnv = strings.TrimSpace(values["passphrase_env"])
		case sshSharedCredential:
			settings.Profile.PassphraseRef = profiles[values["passphrase_connection"]].PassphraseRef
			if settings.Profile.PassphraseRef == "" {
				return SSHSettings{}, fmt.Errorf("choose a connection with a saved key passphrase")
			}
		default:
			return SSHSettings{}, fmt.Errorf("choose whether the key needs a passphrase")
		}
	default:
		return SSHSettings{}, fmt.Errorf("choose Password or Private key")
	}
	if settings.NewSecret != "" && !credentials.Encrypted() {
		return SSHSettings{}, fmt.Errorf("encrypted storage is unavailable; use a key file or choose an environment variable in Advanced settings")
	}
	candidate := settings.Profile
	if settings.NewSecret == ssh.PasswordSecret {
		candidate.PasswordRef = strings.Repeat("0", 32)
	}
	if settings.NewSecret == ssh.PassphraseSecret {
		candidate.PassphraseRef = strings.Repeat("0", 32)
	}
	err = ssh.ValidateProfile(name, candidate)
	return settings, err
}

func (m *Model) SaveSSH(settings SSHSettings) error {
	profiles, err := m.SSHProfiles.Load()
	if err != nil {
		return err
	}
	profiles[settings.Name] = settings.Profile
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
	service := ssh.New(ssh.Config{
		ProfilesPath: m.SSHProfiles.Path,
		Credentials:  m.SSHProfiles.Credentials,
	})
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
