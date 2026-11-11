package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// scanSSE returns the matching response without waiting for EOF. Notifications
// and unrelated responses do not complete the call; each event has a byte bound.
func scanSSE(reader io.Reader, expectedID int) (json.RawMessage, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), wireMax)
	var lines []string
	eventBytes := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			raw, err := decodeSSEEvent(lines, expectedID)
			if err != nil || raw != nil {
				return raw, err
			}
			lines = nil
			eventBytes = 0
			continue
		}

		value, data := strings.CutPrefix(line, "data:")
		if !data {
			continue
		}
		eventBytes += len(value) + 1
		if eventBytes > wireMax {
			return nil, fmt.Errorf("SSE event exceeds %d bytes", wireMax)
		}
		lines = append(lines, strings.TrimPrefix(value, " "))
	}

	err := scanner.Err()
	if err != nil {
		return nil, err
	}
	raw, err := decodeSSEEvent(lines, expectedID)
	if err != nil || raw != nil {
		return raw, err
	}
	return nil, fmt.Errorf("SSE stream ended without response for request %d", expectedID)
}

func decodeSSEEvent(lines []string, expectedID int) (json.RawMessage, error) {
	if len(lines) == 0 {
		return nil, nil
	}
	payload := strings.Join(lines, "\n")
	if payload == "[DONE]" {
		return nil, nil
	}

	var response rpcResponse
	err := json.Unmarshal([]byte(payload), &response)
	if err != nil {
		return nil, fmt.Errorf("decode SSE response: %w", err)
	}
	matches := response.Method == "" && response.ID == expectedID
	hasResponse := response.Result != nil || response.Error != nil
	if matches && hasResponse {
		return json.RawMessage(payload), nil
	}
	return nil, nil
}
