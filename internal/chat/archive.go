package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/keshon/tars/internal/llm"
)

// Archive is a conversation snapshot, never an agent task checkpoint.
type Archive struct {
	Version int           `json:"version"`
	Config  Config        `json:"config"`
	History []llm.Message `json:"history"`
}

func ReadArchive(path string) (Archive, error) {
	var a Archive
	f, err := os.Open(path)
	if err != nil {
		return a, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return a, err
	}
	if info.Size() >= 16<<20 {
		return a, fmt.Errorf("chat: archive exceeds 16 MiB")
	}
	d := json.NewDecoder(io.LimitReader(f, 16<<20))
	d.DisallowUnknownFields()
	if err = d.Decode(&a); err != nil {
		return a, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return a, fmt.Errorf("chat: trailing archive content")
	}
	if a.Version != 1 || len(a.History)%2 != 0 {
		return a, fmt.Errorf("chat: invalid archive version or incomplete turns")
	}
	for i, m := range a.History {
		want := llm.RoleUser
		if i%2 == 1 {
			want = llm.RoleAssistant
		}
		if m.Role != want || len(m.ToolCalls) > 0 || m.ToolCallID != "" || len(m.Images) > 0 || m.Reasoning != "" {
			return a, fmt.Errorf("chat: invalid message %d", i)
		}
	}
	return a, nil
}
func Open(client llm.Client, cfg Config, path string) (*Conversation, error) {
	a, err := ReadArchive(path)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(a.Config, cfg) {
		return nil, fmt.Errorf("chat: saved prompt or generation settings differ; start a new conversation")
	}
	c, err := New(client, cfg)
	if err != nil {
		return nil, err
	}
	c.history = a.History
	return c, nil
}
func (c *Conversation) History() []llm.Message { return append([]llm.Message(nil), c.history...) }
func (c *Conversation) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(Archive{Version: 1, Config: c.config, History: c.history})
	if err != nil {
		return err
	}
	if len(data) >= 16<<20 {
		return fmt.Errorf("chat: archive exceeds 16 MiB")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".conversation-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// AskAndSave rolls back the committed turn if durable storage fails.
func (c *Conversation) AskAndSave(ctx context.Context, text, path string) (Result, error) {
	before := len(c.history)
	result, err := c.Ask(ctx, text)
	if err != nil {
		return result, err
	}
	if err = c.Save(path); err != nil {
		c.history = c.history[:before]
		return result, fmt.Errorf("chat: cannot save turn: %w", err)
	}
	return result, nil
}

func (c *Conversation) AskAndSaveStreaming(ctx context.Context, text, path string, onDelta func(string), systemNotes ...string) (Result, error) {
	before := len(c.history)
	result, err := c.AskStreaming(ctx, text, onDelta, systemNotes...)
	if err != nil {
		return result, err
	}
	if err = c.Save(path); err != nil {
		c.history = c.history[:before]
		return result, fmt.Errorf("chat: cannot save turn: %w", err)
	}
	return result, nil
}
