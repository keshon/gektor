package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DiscoverModels queries a compatible backend using the caller's cancellation and credentials.
func DiscoverModels(ctx context.Context, kind, endpoint, apiKey string) ([]string, error) {
	endpoint = strings.TrimSuffix(endpoint, "/")
	path := "/v1/models"
	if kind == "kobold" {
		path = "/api/v1/model"
	} else if strings.HasSuffix(endpoint, "/v1") {
		path = "/models"
	}
	request, err := http.NewRequestWithContext(ctx, "GET", endpoint+path, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	response, err := http.DefaultClient.Do(request)
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
