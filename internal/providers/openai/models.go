package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

func (m *Manager) Models(ctx context.Context) ([]string, error) {
	bound, err := m.Bind(ctx)
	if err != nil {
		return nil, err
	}
	return bound.Models(ctx)
}

type Model struct {
	Slug                     string   `json:"slug"`
	InputModalities          []string `json:"input_modalities"`
	Visibility               string   `json:"visibility"`
	DefaultReasoningLevel    string   `json:"default_reasoning_level"`
	SupportedReasoningLevels []struct {
		Effort      string `json:"effort"`
		Description string `json:"description"`
	} `json:"supported_reasoning_levels"`
	SupportsReasoningSummaries        bool `json:"supports_reasoning_summaries"`
	SupportsReasoningSummaryParameter bool `json:"supports_reasoning_summary_parameter"`
}

func (m *BoundAccount) Models(ctx context.Context) ([]string, error) {
	catalog, err := m.ModelCatalog(ctx)
	if err != nil {
		return nil, err
	}
	models := []string{}
	for _, model := range catalog {
		if model.Visibility == "list" && model.Slug != "" {
			models = append(models, model.Slug)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("ChatGPT account returned no selectable models")
	}
	return models, nil
}
func (m *BoundAccount) ModelCatalog(ctx context.Context) ([]Model, error) {
	token, err := m.Token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", API+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("ChatGPT model discovery could not connect")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ChatGPT model discovery HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Models []Model `json:"models"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("invalid ChatGPT model catalog")
	}
	return payload.Models, nil
}

type Connection struct {
	ID, Label string
	Active    bool
}

func (m *Manager) Connections(ctx context.Context) ([]Connection, error) {
	var result []Connection
	err := m.withStore(ctx, func(s *credentials) (bool, error) {
		for id, a := range s.Accounts {
			result = append(result, Connection{ID: id, Label: a.Email, Active: id == s.Active})
		}
		return false, nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, err
}
func (m *Manager) Select(ctx context.Context, id string) error {
	return m.withStore(ctx, func(s *credentials) (bool, error) {
		if _, ok := s.Accounts[id]; !ok {
			return false, fmt.Errorf("unknown ChatGPT account registration")
		}
		s.Active = id
		return true, nil
	})
}

// Vision distinguishes absent metadata from an explicitly text-only model.
func (m Model) Vision() (bool, error) {
	if m.InputModalities == nil {
		return false, fmt.Errorf("image input support is unknown for %s", m.Slug)
	}
	for _, modality := range m.InputModalities {
		if modality == "image" {
			return true, nil
		}
	}
	return false, nil
}
func (m *BoundAccount) SupportsVision(ctx context.Context, model string) (bool, error) {
	catalog, err := m.ModelCatalog(ctx)
	if err != nil {
		return false, err
	}
	for _, entry := range catalog {
		if entry.Slug == model {
			return entry.Vision()
		}
	}
	return false, fmt.Errorf("model %q is absent from the account catalog", model)
}
