// Package llamacpp inspects backend metadata independently of inference protocols.
package llamacpp

import (
	"context"
	"fmt"
	"net/http"

	"github.com/keshon/gektor/internal/providers/internal/probe"
)

type Inspector struct {
	baseURL, model string
	http           *http.Client
}

func New(baseURL, model string, client *http.Client) *Inspector {
	if client == nil {
		client = http.DefaultClient
	}
	return &Inspector{baseURL: baseURL, model: model, http: client}
}

// /props carries both the loaded model's path and the context size the
// server was actually started with, plus the modalities its current
// vision/audio projectors enable.
type llamaProps struct {
	ModelPath string `json:"model_path"`
	Default   struct {
		NCtx int `json:"n_ctx"`
	} `json:"default_generation_settings"`
	Modalities struct {
		Vision *bool `json:"vision"`
	} `json:"modalities"`
}

func (p *Inspector) MaxContextLength(ctx context.Context) (int, error) {
	var props llamaProps
	err := probe.GetJSON(ctx, p.http, probe.Endpoint(p.baseURL, "/props"), &props)
	return props.Default.NCtx, err
}

func (p *Inspector) ModelName(ctx context.Context) (string, error) {
	var props llamaProps
	err := probe.GetJSON(ctx, p.http, probe.Endpoint(p.baseURL, "/props"), &props)
	return props.ModelPath, err
}

// vision reads the modalities block /props reports for the loaded
// weights and their projector. Missing metadata stays unknown.
func (p *Inspector) SupportsVision(ctx context.Context) (bool, error) {
	var props llamaProps
	err := probe.GetJSON(ctx, p.http, probe.Endpoint(p.baseURL, "/props"), &props)
	if err != nil {
		return false, err
	}
	if props.Modalities.Vision == nil {
		return false, fmt.Errorf("server did not report image input support")
	}
	return *props.Modalities.Vision, nil
}

func (p *Inspector) Models(ctx context.Context) ([]string, error) {
	return probe.Models(ctx, p.http, p.baseURL)
}
