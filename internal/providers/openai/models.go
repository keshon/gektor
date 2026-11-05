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
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("invalid ChatGPT model catalog")
	}
	var models []string
	for _, model := range payload.Models {
		if model.Visibility == "list" && model.Slug != "" {
			models = append(models, model.Slug)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("ChatGPT account returned no selectable models")
	}
	return models, nil
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
