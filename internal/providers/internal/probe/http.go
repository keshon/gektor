// Package probe shares HTTP model-inspection requests between provider adapters.
package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func Endpoint(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

func GetJSON(ctx context.Context, hc *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("call backend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// Models reads the common OpenAI-compatible model-list shape.
func Models(ctx context.Context, hc *http.Client, base string) ([]string, error) {
	path := "/v1/models"
	if strings.HasSuffix(strings.TrimRight(base, "/"), "/v1") {
		path = "/models"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, Endpoint(base, path), nil)
	if err != nil {
		return nil, err
	}
	response, err := hc.Do(request)
	if err != nil {
		return nil, fmt.Errorf("model discovery failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model discovery HTTP %d", response.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("unsupported model list: %w", err)
	}
	items := []string{}
	for _, item := range payload.Data {
		if item.ID != "" {
			items = append(items, item.ID)
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no models reported")
	}
	return items, nil
}
