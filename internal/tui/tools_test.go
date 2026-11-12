package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/credentials"
	"github.com/keshon/gektor/internal/executables"
	toolcatalog "github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/tools/extension"
	"github.com/keshon/gektor/internal/tui/screen"
	toolsui "github.com/keshon/gektor/internal/tui/tools"
	"github.com/keshon/gektor/internal/userdirs"
)

func toolSettingsModel(t *testing.T) *Model {
	t.Helper()
	m := settingsModel(t)
	m.tools.Close()
	m.tools = toolsui.New(executables.New(filepath.Join(t.TempDir(), "applications.json")))
	return m
}

func chooseTool(t *testing.T, m *Model, id, action string) {
	t.Helper()
	view := m.chrome.Dialog.Table.Model
	found := false
	for i, row := range view.Rows {
		if row.ID == id {
			view.Cursor = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing row", id)
	}
	m.chrome.TableAction(action)
	m.settleControls()
}

func TestToolsNavigationAndCancelAreSharedByAgentAndWorkflow(t *testing.T) {
	for _, mode := range []string{"screen:agent", "screen:workflows"} {
		m := toolSettingsModel(t)
		m.runAction(mode)
		m.runAction("tools")
		if m.chrome.Dialog == nil || m.chrome.Dialog.Title != "Tools" {
			t.Fatal("no Tools in", mode)
		}
		chooseTool(t, m, "native_media", "details")
		if m.chrome.Dialog.Title != "Media executables" {
			t.Fatal(m.chrome.Dialog.Title)
		}
		chooseTool(t, m, "ffmpeg", "details")
		m.chrome.Dialog.Form.Fields[0].Value = filepath.Join(t.TempDir(), "unsaved.exe")
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.chrome.Dialog.Title != "Media executables" || m.modeChange != nil {
			t.Fatal("Cancel failed to return without applying")
		}
		_, err := os.Stat(m.tools.Resolver.ConfigPath)
		if !os.IsNotExist(err) {
			t.Fatal("Cancel saved preferences", err)
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.chrome.Dialog.Title != "Tools" {
			t.Fatal("Media back did not return to Tools")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.chrome.Dialog != nil {
			t.Fatal("Tools did not close")
		}
	}
}

func TestMediaApplyUsesRelaunchAndPreservesOtherSettings(t *testing.T) {
	m := toolSettingsModel(t)
	err := m.tools.Resolver.Save(executables.Config{Applications: map[string]executables.Setting{"node": {Disabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	m.runAction("tools")
	chooseTool(t, m, "native_media", "details")
	chooseTool(t, m, "ffmpeg", "details")
	err = m.applyExecutablePreference("ffmpeg", map[string]string{"path": "relative.exe", "disabled": "false"})
	if err == nil || m.modeChange != nil {
		t.Fatal("invalid path requested restart", err)
	}
	path := filepath.Join(t.TempDir(), "ffmpeg.exe")
	draft := draftOf(m)
	err = m.applyExecutablePreference("ffmpeg", map[string]string{"path": path, "disabled": "false"})
	if err != nil || m.modeChange == nil || m.modeChange.Draft != draft {
		t.Fatal("apply lost relaunch state", err)
	}
	saved, err := m.tools.Resolver.Load()
	if err != nil || saved.Applications["ffmpeg"].Path != path || !saved.Applications["node"].Disabled {
		t.Fatal(saved, err)
	}
}

func TestToolsRenderedViewsFitBothThemesAndTerminalSizes(t *testing.T) {
	t.Cleanup(func() { screen.SetTheme("dark") })
	for _, theme := range []string{"dark", "light"} {
		screen.SetTheme(theme)
		for _, size := range [][2]int{{120, 30}, {50, 18}} {
			m := toolSettingsModel(t)
			m.applyTheme(theme)
			if screen.CurrentPalette().Name != theme {
				t.Fatal("wrong fixture theme")
			}
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.runAction("tools")
			chooseTool(t, m, "native_media", "details")
			m.syncPresentation()
			rendered := ansi.Strip(m.View().Content)
			if !strings.Contains(rendered, "Media executables") || !strings.Contains(rendered, "ffmpeg") {
				t.Fatal(rendered)
			}
			for _, line := range strings.Split(rendered, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("overflow %s %v: %s", theme, size, line)
				}
			}
			t.Logf("%s %dx%d\n%s", theme, size[0], size[1], rendered)
		}
	}
	screen.SetTheme("dark")
}

func TestMediaFormApplyAndToolsOperationNavigation(t *testing.T) {
	m := toolSettingsModel(t)
	m.runAction("screen:workflows")
	m.runAction("tools")
	chooseTool(t, m, "native_media", "details")
	m.chrome.TableAction("operations")
	m.settleControls()
	if m.chrome.Dialog.Title != "Media" || !strings.Contains(strings.Join(m.chrome.Dialog.Lines, "\n"), "video_transcript") {
		t.Fatal("missing operation details")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	chooseTool(t, m, "ffmpeg", "details")
	path := filepath.Join(t.TempDir(), "chosen ffmpeg.exe")
	m.chrome.Dialog.Form.Editor.SetValue(path)
	m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.modeChange == nil || m.chrome.Dialog != nil {
		t.Fatal("form did not apply and request relaunch")
	}
	saved, err := m.tools.Resolver.Load()
	if err != nil || saved.Applications["ffmpeg"].Path != path {
		t.Fatal(saved, err)
	}
}

func TestMediaPreferencesReachFreshAgentAndWorkflowComposition(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	m := toolSettingsModel(t)
	m.tools = toolsui.New(executables.New(userdirs.Sub("applications.json")))
	values := map[string]executables.Setting{}
	for _, name := range []string{"ffmpeg", "ffprobe", "yt-dlp", "deno", "node"} {
		values[name] = executables.Setting{Disabled: true}
	}
	err := m.tools.Resolver.Save(executables.Config{Applications: values})
	if err != nil {
		t.Fatal(err)
	}
	err = m.applyExecutablePreference("ffmpeg", map[string]string{"disabled": "true"})
	if err != nil {
		t.Fatal(err)
	}
	catalog := &extension.Capabilities{}
	agent, err := toolcatalog.Compose(toolcatalog.Dependencies{WS: m.launch.Env.WS}, catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Registry = agent
	_, err = catalog.Enable(t.Context(), "native_media", nil)
	if err != nil {
		t.Fatal(err)
	}
	agentStatus, err := agent.Run(t.Context(), "media_status", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := app.WorkflowRegistry(m.launch.Env.WS, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	node, err := workflow.Node("media_status")
	if err != nil {
		t.Fatal(err)
	}
	workflowStatus, err := node.Execute(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{agentStatus, workflowStatus} {
		var report struct {
			Applications []executables.Status `json:"applications"`
		}
		err = json.Unmarshal([]byte(status), &report)
		if err != nil {
			t.Fatal(status, err)
		}
		if len(report.Applications) != 5 {
			t.Fatal(status)
		}
		for _, application := range report.Applications {
			if application.State != "disabled" {
				t.Fatal(application)
			}
		}
	}
}

func TestMediaApplyProtectsRetainedDirtyWorkflow(t *testing.T) {
	m := toolSettingsModel(t)
	seedWorkflow(t, m, workflowRecipe())
	m.runAction("screen:agent")
	err := m.applyExecutablePreference("ffmpeg", map[string]string{"disabled": "true"})
	if err == nil || m.modeChange != nil {
		t.Fatal("dirty workflow allowed restart", err)
	}
	_, err = os.Stat(m.tools.Resolver.ConfigPath)
	if !os.IsNotExist(err) {
		t.Fatal("guard wrote preferences", err)
	}
}

func TestToolsMouseActionsUseSharedControls(t *testing.T) {
	for _, mode := range []string{"screen:agent", "screen:workflows"} {
		m := toolSettingsModel(t)
		m.runAction(mode)
		m.runAction("tools")
		chooseTool(t, m, "native_media", "details")
		m.syncPresentation()
		clickVisible(t, m, "Operations")
		if m.chrome.Dialog.Title != "Media" {
			t.Fatal("mouse missed operation details")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m.syncPresentation()
		clickVisible(t, m, "[ Preferences ]")
		if m.chrome.Dialog.Form == nil {
			t.Fatal("mouse missed preferences")
		}
		m.syncPresentation()
		rendered := ansi.Strip(m.View().Content)
		if !strings.Contains(rendered, "Executable path") || !strings.Contains(rendered, "Apply and restart") {
			t.Fatal(rendered)
		}
	}
}

func TestSSHSettingsCancelApplyAndDeleteAcrossScreens(t *testing.T) {
	for _, mode := range []string{"screen:agent", "screen:workflows"} {
		m := toolSettingsModel(t)
		m.tools.SSHProfiles.Path = filepath.Join(t.TempDir(), "ssh.json")
		m.runAction(mode)
		m.runAction("tools")
		chooseTool(t, m, "native_ssh", "details")
		if m.chrome.Dialog.Title != "SSH connections" {
			t.Fatal("SSH list missing")
		}
		m.chrome.TableAction("add")
		m.settleControls()
		if m.chrome.Dialog.Form == nil {
			t.Fatal("Add unavailable on an empty list")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		_, err := os.Stat(m.tools.SSHProfiles.Path)
		if !os.IsNotExist(err) {
			t.Fatal("Cancel wrote SSH preferences", err)
		}
		m.chrome.TableAction("add")
		m.settleControls()
		values := map[string]string{"name": "dev", "host": "example.com", "port": "22", "user": "tester", "known_hosts": filepath.Join(t.TempDir(), "known_hosts"), "auth": "Password", "advanced": "Yes", "password_source": "Environment variable", "password_env": "SSH_PASSWORD"}
		for i := range m.chrome.Dialog.Form.Fields {
			field := &m.chrome.Dialog.Form.Fields[i]
			field.Value = values[field.Key]
		}
		m.chrome.Dialog.Form.Editor.SetValue("dev")
		m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		if m.modeChange == nil {
			t.Fatal("SSH form did not request restart")
		}
		profiles, err := m.tools.SSHProfiles.Load()
		if err != nil || profiles["dev"].PasswordEnv != "SSH_PASSWORD" {
			t.Fatal(profiles, err)
		}

		m.modeChange = nil
		m.openSSHTools()
		chooseTool(t, m, "dev", "delete")
		if m.chrome.Dialog.Form == nil || !m.chrome.Dialog.Form.Destructive {
			t.Fatal("missing delete confirmation")
		}
		err = m.deleteSSHPreference("dev", "wrong")
		if err == nil || m.modeChange != nil {
			t.Fatal("unconfirmed deletion requested restart")
		}
		err = m.deleteSSHPreference("dev", "dev")
		if err != nil || m.modeChange == nil {
			t.Fatal("confirmed deletion failed", err)
		}
	}
}

func TestSSHApplyProtectsDirtyWorkflow(t *testing.T) {
	m := toolSettingsModel(t)
	m.tools.SSHProfiles.Path = filepath.Join(t.TempDir(), "ssh.json")
	seedWorkflow(t, m, workflowRecipe())
	m.runAction("screen:agent")
	err := m.applySSHPreference("", map[string]string{"name": "dev"})
	if err == nil || m.modeChange != nil {
		t.Fatal("dirty workflow allowed restart", err)
	}
	_, err = os.Stat(m.tools.SSHProfiles.Path)
	if !os.IsNotExist(err) {
		t.Fatal("guard wrote SSH preferences", err)
	}
}

func TestSSHViewsFitSharedTerminalLayouts(t *testing.T) {
	t.Cleanup(func() { screen.SetTheme("dark") })
	for _, theme := range []string{"dark", "light"} {
		for _, size := range [][2]int{{120, 30}, {50, 18}} {
			m := toolSettingsModel(t)
			m.tools.SSHProfiles.Path = filepath.Join(t.TempDir(), "ssh.json")
			m.applyTheme(theme)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.openSSHTools()
			for _, title := range []string{"SSH connections", "Add SSH connection"} {
				m.syncPresentation()
				rendered := ansi.Strip(m.View().Content)
				if !strings.Contains(rendered, title) {
					t.Fatal(rendered)
				}
				for _, line := range strings.Split(rendered, "\n") {
					if ansi.StringWidth(line) > size[0] {
						t.Fatal("SSH view overflows", line)
					}
				}
				t.Logf("%s %v\n%s", theme, size, rendered)
				if title == "SSH connections" {
					m.chrome.TableAction("add")
					m.settleControls()
				}
			}
		}
	}
}

func TestSSHCheckCannotReplaceAnUnrelatedDialog(t *testing.T) {
	m := toolSettingsModel(t)
	m.tools.SSHProfiles.Path = filepath.Join(t.TempDir(), "ssh.json")
	m.openSSHTools()
	command := m.tools.CheckSSH(t.Context(), "missing")
	m.chrome.PresentDialog(screen.Dialog{Title: "Another view"})
	m.settleControls()
	m.Update(command())
	if m.chrome.Dialog.Title != "Another view" {
		t.Fatal("late SSH check replaced another view")
	}
}

func TestSSHMaskedEntrySavesOnlyReferencesAcrossBothScreens(t *testing.T) {
	if !credentials.Encrypted() {
		t.Skip("requires encrypted storage")
	}
	for _, mode := range []string{"screen:agent", "screen:workflows"} {
		m := toolSettingsModel(t)
		dir := t.TempDir()
		m.tools.SSHProfiles.Path = filepath.Join(dir, "ssh.json")
		m.tools.SSHProfiles.Credentials = credentials.Store{Directory: filepath.Join(dir, "secrets")}
		m.runAction(mode)
		values := map[string]string{"name": "saved", "host": "example.com", "user": "tester", "port": "22", "known_hosts": filepath.Join(dir, "known_hosts"), "auth": "Password", "password": "Enter password when saving"}
		err := m.applySSHPreference("", values)
		if err != nil || m.chrome.Dialog.Secret == nil || m.modeChange != nil {
			t.Fatal("masked prompt was not opened before saving", err)
		}
		m.Update(tea.PasteMsg{Content: "private-secret-sentinel"})
		previous := m.chrome.Dialog
		if strings.Contains(ansi.Strip(m.View().Content), "private-secret-sentinel") {
			t.Fatal("plaintext rendered")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if previous.Secret.Take() != "" || m.sshSecret != nil {
			t.Fatal("Cancel retained secret state")
		}
		_, err = os.Stat(m.tools.SSHProfiles.Path)
		if !os.IsNotExist(err) {
			t.Fatal("Cancel wrote profile", err)
		}
		_, err = os.Stat(m.tools.SSHProfiles.Credentials.Directory)
		if !os.IsNotExist(err) {
			t.Fatal("Cancel wrote credential", err)
		}

		err = m.applySSHPreference("", values)
		if err != nil {
			t.Fatal(err)
		}
		m.Update(tea.PasteMsg{Content: "private-secret-sentinel"})
		previous = m.chrome.Dialog
		if mode == "screen:workflows" {
			m.syncPresentation()
			clickVisible(t, m, "Save encrypted")
		} else {
			m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		}
		if m.modeChange == nil || m.sshSecret != nil || previous.Secret.Take() != "" {
			t.Fatal("saved secret was retained or restart missing")
		}
		raw, err := os.ReadFile(m.tools.SSHProfiles.Path)
		if err != nil || strings.Contains(string(raw), "private-secret-sentinel") {
			t.Fatal("secret in profile", err)
		}
		snapshot, err := json.Marshal(m.modeChange)
		if err != nil || strings.Contains(string(snapshot), "private-secret-sentinel") {
			t.Fatal("secret in relaunch state", err)
		}
		profiles, err := m.tools.SSHProfiles.Load()
		if err != nil || !credentials.ValidReference(profiles["saved"].PasswordRef) {
			t.Fatal("saved reference missing", err)
		}
	}
}

func TestSSHMaskedEntryFailureAndReplacementDiscardValues(t *testing.T) {
	if !credentials.Encrypted() {
		t.Skip("requires encrypted storage")
	}
	m := toolSettingsModel(t)
	dir := t.TempDir()
	m.tools.SSHProfiles.Path = filepath.Join(dir, "ssh.json")
	m.tools.SSHProfiles.Credentials = credentials.Store{Directory: filepath.Join(dir, "secrets")}
	values := map[string]string{"name": "saved", "host": "example.com", "user": "tester", "port": "22", "known_hosts": filepath.Join(dir, "known_hosts"), "auth": "Password", "password": "Enter password when saving"}
	err := m.applySSHPreference("", values)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.modeChange != nil || m.chrome.Dialog.Secret.Error == "" {
		t.Fatal("empty secret accepted")
	}
	m.Update(tea.PasteMsg{Content: "private-secret-sentinel"})
	previous := m.chrome.Dialog
	m.chrome.PresentDialog(screen.Dialog{Title: "Other"})
	m.settleControls()
	if previous.Secret.Take() != "" || m.sshSecret != nil {
		t.Fatal("replacement retained secret")
	}
}

func TestSSHSecretPromptFitsThemesAndSmallTerminals(t *testing.T) {
	if !credentials.Encrypted() {
		t.Skip("requires encrypted storage")
	}
	t.Cleanup(func() { screen.SetTheme("dark") })
	for _, theme := range []string{"dark", "light"} {
		for _, size := range [][2]int{{120, 30}, {50, 18}} {
			m := toolSettingsModel(t)
			m.applyTheme(theme)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			dir := t.TempDir()
			m.tools.SSHProfiles.Path = filepath.Join(dir, "ssh.json")
			m.tools.SSHProfiles.Credentials = credentials.Store{Directory: filepath.Join(dir, "secrets")}
			values := map[string]string{"name": "saved", "host": "example.com", "user": "tester", "port": "22", "known_hosts": filepath.Join(dir, "known_hosts"), "auth": "Password", "password": "Enter password when saving"}
			err := m.applySSHPreference("", values)
			if err != nil {
				t.Fatal(err)
			}
			m.Update(tea.PasteMsg{Content: "private-secret-sentinel"})
			m.syncPresentation()
			rendered := ansi.Strip(m.View().Content)
			if !strings.Contains(rendered, "Secret:") || strings.Contains(rendered, "private-secret-sentinel") || strings.Contains(rendered, "↑↓ scroll") {
				t.Fatal(rendered)
			}
			for _, line := range strings.Split(rendered, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("secret prompt overflows", line)
				}
			}
			t.Logf("%s %v\n%s", theme, size, rendered)
		}
	}
}

func TestSSHAuthenticationPickerUpdatesFieldsInBothScreens(t *testing.T) {
	if !credentials.Encrypted() {
		t.Skip("default password entry requires encrypted storage")
	}
	for _, mode := range []string{"screen:agent", "screen:workflows"} {
		m := toolSettingsModel(t)
		m.tools.SSHProfiles.Path = filepath.Join(t.TempDir(), "ssh.json")
		m.runAction(mode)
		m.runAction("tools")
		chooseTool(t, m, "native_ssh", "details")
		m.chrome.TableAction("add")
		m.settleControls()
		for i, field := range m.chrome.Dialog.Form.Fields {
			if field.Key == "auth" {
				m.focusForm(i)
				break
			}
		}
		if mode == "screen:workflows" {
			m.syncPresentation()
			clickVisible(t, m, "Private key")
		} else {
			m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
		}
		for i, field := range m.chrome.Dialog.Form.Fields {
			if field.Key == "private_key" && !m.chrome.Dialog.Form.FieldVisible(i) {
				t.Fatal("key picker still hidden")
			}
			if field.Key == "password" && m.chrome.Dialog.Form.FieldVisible(i) {
				t.Fatal("password row remains after selecting key")
			}
		}
	}
}

func TestSSHBasicFormRenderDoesNotExposeCredentialInternals(t *testing.T) {
	if !credentials.Encrypted() {
		t.Skip("default password entry requires encrypted storage")
	}
	t.Cleanup(func() { screen.SetTheme("dark") })
	for _, theme := range []string{"dark", "light"} {
		for _, size := range [][2]int{{120, 30}, {50, 18}} {
			m := toolSettingsModel(t)
			m.applyTheme(theme)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.tools.SSHProfiles.Path = filepath.Join(t.TempDir(), "ssh.json")
			m.runAction("tools")
			chooseTool(t, m, "native_ssh", "details")
			m.chrome.TableAction("add")
			m.settleControls()
			m.syncPresentation()
			rendered := ansi.Strip(m.View().Content)
			for _, internal := range []string{"variable", "reference", "password ID", "passphrase ID", "Private key file"} {
				if strings.Contains(rendered, internal) {
					t.Fatal("basic form exposes irrelevant detail", internal, rendered)
				}
			}
			for _, line := range strings.Split(rendered, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("SSH form overflows", line)
				}
			}
			t.Logf("%s %v\n%s", theme, size, rendered)
		}
	}
}
