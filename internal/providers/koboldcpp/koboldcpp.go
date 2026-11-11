// Package koboldcpp inspects backend metadata independently of inference protocols.
package koboldcpp

import (
	"context"
	"fmt"
	"net/http"

	"github.com/keshon/gektor/internal/providers/internal/probe"
)

type Inspector struct {
	baseURL string
	http    *http.Client
}

func New(baseURL, _ string, client *http.Client) *Inspector {
	if client == nil {
		client = http.DefaultClient
	}

	return &Inspector{baseURL: baseURL, http: client}
}
func (p *Inspector) MaxContextLength(ctx context.Context) (int, error) {
	var out struct {
		Value int `json:"value"`
	}
	err := probe.GetJSON(ctx, p.http, probe.Endpoint(p.baseURL, "/api/extra/true_max_context_length"), &out)
	return out.Value, err
}

func (p *Inspector) ModelName(ctx context.Context) (string, error) {
	var out struct {
		Result string `json:"result"`
	}
	err := probe.GetJSON(ctx, p.http, probe.Endpoint(p.baseURL, "/api/v1/model"), &out)
	return out.Result, err
}

// koboldcpp refuses image content parts outright, so the agent must not
// offer a tool that produces them.
func (p *Inspector) SupportsVision(context.Context) (bool, error) {
	return false, nil
}

func (p *Inspector) Models(ctx context.Context) ([]string, error) {
	model, err := p.ModelName(ctx)
	if err != nil {
		return nil, err
	}

	if model == "" {
		return nil, fmt.Errorf("no models reported")
	}

	return []string{model}, nil
}
