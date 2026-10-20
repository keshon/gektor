package tui

import (
	"encoding/json"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
)

type usageState struct {
	source     string
	at         time.Time
	last       usageSnapshot
	thisRun    bool
	historical bool
	liveModel  bool
}

type usageSnapshot struct {
	Tokens    int       `json:"tokens"`
	Estimated bool      `json:"estimated"`
	At        time.Time `json:"at"`
	Model     string    `json:"model"`
}

func safeEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return ""
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}

func (m *model) clearContext() {
	m.tokens, m.tokensEst, m.usage.source = 0, false, ""
	m.usage = usageState{}
}

func (m *model) restoreContext(history []llm.Message) {
	m.clearContext()
	if len(history) > 0 {
		if m.stateFile != "" {
			if active, err := session.ReadContext(m.stateFile); err == nil {
				history = active
			}
		}
		m.tokens, m.tokensEst = llm.EstimatePromptTokens(llm.ChatRequest{Messages: history}), true
		m.usage.source = "Saved history estimate"
		m.usage.historical = true
	}
	if m.stateFile != "" {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(m.stateFile), "usage.json"))
		var saved usageSnapshot
		if err == nil && json.Unmarshal(data, &saved) == nil && saved.Tokens > 0 {
			m.usage.last = saved
		}
	}
}

func (m *model) displayedModel() string {
	if !m.usage.liveModel && m.usage.last.Model != "" {
		return m.usage.last.Model
	}
	if !m.usage.liveModel && m.usage.historical {
		return ""
	}
	return m.modelName
}

// Backend model identifiers can be paths from either operating system.
func modelLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "—"
	}
	return path.Base(strings.ReplaceAll(name, `\`, "/"))
}

func (m *model) persistUsage() {
	if !m.usage.thisRun || m.stateFile == "" {
		return
	}
	data, err := json.Marshal(m.usage.last)
	if err == nil {
		_ = os.WriteFile(filepath.Join(filepath.Dir(m.stateFile), "usage.json"), data, 0644)
	}
}
