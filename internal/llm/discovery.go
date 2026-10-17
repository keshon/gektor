package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// discoveryTimeout bounds a model-list probe, matching what DetectKind
// allows. http.DefaultClient has none, so a black-holed endpoint hung the
// settings picker instead of failing.
const discoveryTimeout = 5 * time.Second

// DiscoverModels queries a compatible backend using the caller's cancellation and credentials.
func DiscoverModels(ctx context.Context, kind, base, apiKey string) ([]string, error) {
	path := "/v1/models"
	if kind == "kobold" {
		path = "/api/v1/model"
	} else if strings.HasSuffix(base, "/v1") {
		path = "/models"
	}
	request, err := http.NewRequestWithContext(ctx, "GET", endpoint(base, path), nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	// http.DefaultClient has no timeout, so a black-holed endpoint would
	// hang the settings picker instead of failing.
	client := &http.Client{Timeout: discoveryTimeout}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("model discovery failed")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("model discovery HTTP %d", response.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Result string `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("unsupported model list")
	}
	items := []string{}
	if kind == "kobold" && payload.Result != "" {
		items = append(items, payload.Result)
	}
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
