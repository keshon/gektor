// Package openaicompat inspects backend metadata independently of inference protocols.
package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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

// A hosted gateway has no probe endpoint for the context window. Report
// that plainly so the caller falls back to its configured model table or
// an explicit override instead of silently running with no budget.
func (p *Inspector) MaxContextLength(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("openai-compatible backends do not report a context window; pass it explicitly")
}

func (p *Inspector) ModelName(ctx context.Context) (string, error) {
	return p.model, nil
}

// openaiModels is the part of /models that carries capability metadata.
// OpenRouter and a few gateways publish architecture.input_modalities;
// OpenAI itself publishes only ids.
type openaiModels struct {
	Data []struct {
		ID           string `json:"id"`
		Architecture struct {
			Modality        string   `json:"modality"`
			InputModalities []string `json:"input_modalities"`
		} `json:"architecture"`
	} `json:"data"`
}

// vision asks the provider's model list before falling back to a name
// heuristic. A provider that reports capabilities is authoritative even
// when it says no; one that reports only ids cannot answer, and there the
// name is the only evidence available.
func (p *Inspector) SupportsVision(ctx context.Context) (bool, error) {
	path := "/v1/models"
	if strings.HasSuffix(strings.TrimRight(p.baseURL, "/"), "/v1") {
		path = "/models"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.Endpoint(p.baseURL, path), nil)
	if err != nil {
		return modelVisionName(p.model), err
	}
	resp, err := p.http.Do(request)
	if err != nil {
		return modelVisionName(p.model), err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return modelVisionName(p.model), fmt.Errorf("%s returned %s", request.URL, resp.Status)
	}
	var list openaiModels
	// Gateway model lists carry per-model metadata and OpenRouter's runs
	// past a megabyte; truncating early would lose the entry this probe
	// needs. The limit still bounds a hostile or broken endpoint.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&list); err != nil {
		return modelVisionName(p.model), fmt.Errorf("decode model list: %w", err)
	}
	for _, item := range list.Data {
		if !strings.EqualFold(item.ID, p.model) {
			continue
		}
		for _, m := range item.Architecture.InputModalities {
			if m == "image" {
				return true, nil
			}
		}
		if item.Architecture.Modality != "" {
			return strings.Contains(item.Architecture.Modality, "image"), nil
		}
		break // the provider lists ids only; the name is all we have
	}
	return modelVisionName(p.model), nil
}

// modelVisionName is the fallback for providers that do not publish
// capabilities: the families known to accept image input. Unknown names
// are treated as text-only, because a wrong "yes" fails a whole turn while
// a wrong "no" only withholds a read_file convenience.
func modelVisionName(model string) bool {
	m := strings.ToLower(model)
	for _, marker := range []string{
		"gpt-4o", "gpt-4.1", "gpt-4-turbo", "gpt-5",
		"claude", "gemini", "grok-4", "grok-2-vision",
		"llava", "pixtral", "internvl", "vision",
		"qwen-vl", "qwen2-vl", "qwen2.5-vl", "qwen3-vl", "qvq",
		"glm-4v", "glm-4.1v", "gemma-3", "gemma-4",
		"llama-3.2-vision", "llama-4", "phi-3.5-vision", "phi-4-multimodal",
		"mistral-small-3",
	} {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

// ContextLimit is the best-known window for a model name, matched
// case-insensitively on substring. Unknown names get the conservative
// 128k most current hosted models meet or exceed; callers with an exact
// number should pass it explicitly instead.
func ContextLimit(model string) int {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude"):
		return 200000
	case strings.Contains(m, "grok"):
		return 131072
	case strings.Contains(m, "deepseek"):
		return 128000
	case strings.Contains(m, "llama-4"), strings.Contains(m, "llama4"):
		return 1000000
	case strings.Contains(m, "gpt-4.1"), strings.Contains(m, "gpt-4o"), strings.Contains(m, "gpt-4 "),
		strings.Contains(m, "gpt-5"), strings.Contains(m, "o1"), strings.Contains(m, "o3"), strings.Contains(m, "o4"):
		return 128000
	case strings.Contains(m, "gemini"):
		return 1000000
	case strings.Contains(m, "mistral"), strings.Contains(m, "mixtral"), strings.Contains(m, "qwen"):
		return 128000
	default:
		return 128000
	}
}

func (p *Inspector) Models(ctx context.Context) ([]string, error) {
	return probe.Models(ctx, p.http, p.baseURL)
}
