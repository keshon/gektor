package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/keshon/tars/internal/tui/components/composer"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/api"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/speech"
	"github.com/keshon/tars/internal/workspace"
)

// ChatConfig selects a tool-free runner and its own session store.
type ChatConfig struct {
	NewVoice  func(context.Context, string) (speech.Service, error)
	Stream    bool
	Warnings  []string
	Profile   config.Profile
	Configure func(config.Profile) (ChatConfig, error)
	Client    llm.Client
	Settings  chat.Config
	Directory string
}
type chatUI struct {
	config  ChatConfig
	current *chat.Conversation
}

func (m *model) startConversation(text string) error {
	c, err := chat.New(m.chat.config.Client, m.chat.config.Settings)
	if err != nil {
		return err
	}
	id := make([]byte, 12)
	if _, err = rand.Read(id); err != nil {
		return err
	}
	dir := filepath.Join(m.chat.config.Directory, hex.EncodeToString(id))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	title := workspace.TitleLine(text)
	if err = workspace.WriteSessionTitle(dir, title); err != nil {
		return err
	}
	m.saveDraft()
	m.chat.current = c
	m.stateFile = filepath.Join(dir, "conversation.json")
	m.sessionTitle = title
	if err := m.saveSessionSettings(); err != nil {
		return err
	}
	m.transcript.blocks = nil
	m.resetRunFacts()
	m.queued = ""
	m.input.Restore(composer.Snapshot{History: []string{text}, Index: 1})
	m.transcript.Follow = true
	m.clearContext()
	m.input.SetValue("")
	m.showGreeting()
	m.appendBlock(userBlock(text))
	m.runConversation(text)
	return nil
}
func (m *model) runConversation(text string) {
	c, path := m.chat.current, m.stateFile
	m.startRun(func(ctx context.Context) (string, error) {
		var output *speech.Output
		var audioContext context.Context
		if m.voice != nil && m.voiceEnabled {
			var cancel context.CancelFunc
			audioContext, cancel = context.WithCancel(ctx)
			defer m.registerSpeech(cancel)()
			output = speech.NewOutput(audioContext, m.voice, func(phase string) { m.emitVoice(ctx, phase) })
		}
		onDelta := func(delta string) {
			select {
			case m.events <- api.Event{Name: "delta", Fields: map[string]any{"text": delta}}:
			case <-ctx.Done():
			}
			if output != nil {
				if err := output.Delta(delta); err != nil {
					output.Cancel()
				}
			}
		}
		note := ""
		if output != nil {
			note = "Speech capability: input recognition is configured for English. Output uses Chatterbox English speech. Other-language replies remain text-only. Only dialogue is voiced; do not claim an unsupported vocal effect has actually been performed."
			note += "\n" + speech.DeliveryPrompt(m.voice)
		}
		var r chat.Result
		var err error
		if m.chat.config.Stream {
			r, err = c.AskAndSaveStreaming(ctx, text, path, onDelta, note)
		} else {
			r, err = c.AskAndSaveStreaming(ctx, text, path, nil, note)
			if err == nil && output != nil {
				onDelta(r.Response.Message.Content)
			}
		}
		if output != nil {
			if err != nil {
				output.Cancel()
			}
			if audioErr := output.Finish(); audioErr != nil && err == nil && ctx.Err() == nil && audioContext.Err() == nil {
				select {
				case m.events <- api.Event{Name: "nudge", Fields: map[string]any{"text": "Speech failed; text was saved: " + audioErr.Error()}}:
				case <-ctx.Done():
				}
			}
		}
		if err != nil {
			return "", err
		}
		if r.OmittedTurns > 0 || r.OmittedExamples > 0 {
			select {
			case m.events <- api.Event{Name: "nudge", Fields: map[string]any{"text": fmt.Sprintf("Context window omits %d earlier turns and %d example messages", r.OmittedTurns, r.OmittedExamples)}}:
			case <-ctx.Done():
			}
		}
		select {
		case m.events <- api.Event{Name: "usage", Fields: map[string]any{"prompt": float64(r.Response.Usage.PromptTokens), "estimated": r.Response.Usage.Estimated}}:
		case <-ctx.Done():
		}
		return r.Response.Message.Content, nil
	})
}
func (m *model) submitConversation(text string) (tea.Model, tea.Cmd) {
	original := text
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "/") {
		m.input.SetValue("")
		return m.command(text)
	}
	if text == "" {
		return m, nil
	}
	m.input.Push(text)
	m.input.SetValue("")
	m.fitInput()
	m.fitBottom()
	if m.chat.current == nil {
		if err := m.startConversation(text); err != nil {
			m.input.SetValue(original)
			m.appendBlock(errorBlock(err.Error()))
		}
	} else {
		m.appendBlock(userBlock(text))
		m.runConversation(text)
	}
	return m, nil
}
func (m *model) openConversation(cur sessionEntry) tea.Cmd {
	c, err := chat.Open(m.chat.config.Client, m.chat.config.Settings, filepath.Join(cur.dir, "conversation.json"))
	if err != nil {
		m.sessions.flash = err.Error()
		return nil
	}
	m.saveDraft()
	m.chat.current = c
	m.stateFile = filepath.Join(cur.dir, "conversation.json")
	m.sessionTitle = cur.title
	m.resetRunFacts()
	m.transcript.blocks = nil
	m.showGreeting()
	greetingBlocks := len(m.transcript.blocks)
	m.transcript.blocks = append(m.transcript.blocks, renderHistory(c.History())...)
	m.restoreContext(c.History())
	m.state = stDone
	m.navFocused = false
	m.closeSessions()
	m.restoreDraft()
	m.fitBottom()
	m.refreshContent()
	m.transcript.Viewport.GotoTop()
	if cur.historyMatch {
		target := historyMatchBlock(c.History(), cur.matchMessage) + greetingBlocks
		row := m.transcript.PartOffset(target)
		m.transcript.Follow = false
		m.transcript.Viewport.SetYOffset(row)
	}
	return m.input.Focus()
}

func (m *model) showGreeting() {
	if m.chat != nil && m.chat.config.Settings.Greeting != "" {
		for _, b := range assistantBlocks(m.chat.config.Settings.Greeting, "") {
			m.appendBlock(b)
		}
	}
}

func (m *model) createCarriedArchive() error {
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return err
	}
	dir := filepath.Join(m.chat.config.Directory, hex.EncodeToString(id))
	m.stateFile = filepath.Join(dir, "conversation.json")
	if err := m.chat.current.Save(m.stateFile); err != nil {
		return err
	}
	m.sessionTitle = "Continued conversation"
	return workspace.WriteSessionTitle(dir, m.sessionTitle)
}
