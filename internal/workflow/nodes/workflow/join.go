package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
)

const maxJoinRows = 10000
const maxJoinKeyBytes = 1024

type joinInput struct {
	Left     []json.RawMessage `json:"left"`
	Right    []json.RawMessage `json:"right"`
	LeftKey  string            `json:"left_key"`
	RightKey string            `json:"right_key"`
	Mode     string            `json:"mode"`
}

type joinPair struct {
	Left  json.RawMessage `json:"left"`
	Right json.RawMessage `json:"right"`
}

func joinDefinition() workflow.Node {
	return workflow.Node{
		Name:        "workflow_join",
		Version:     1,
		ReadOnly:    true,
		Concurrent:  true,
		RetrySafe:   true,
		Description: "Join two arrays by left_key/right_key JSON Pointers. mode is inner, left or full. Keys must be non-null strings or integer literals, without coercion; missing keys fail. Duplicate keys produce every matching pair in left order, then right order; full joins append unmatched right rows. Output is an array of {left,right} pairs, with null for unmatched sides. At most 10000 input rows per side, 10000 output pairs and 1 MiB output. Normal dependency rules still apply to skipped sources.",
		InputSchema: json.RawMessage(`{
			"type":"object","properties":{
				"left":{"type":"array","maxItems":10000},
				"right":{"type":"array","maxItems":10000},
				"left_key":{"type":"string","maxLength":1024},
				"right_key":{"type":"string","maxLength":1024},
				"mode":{"type":"string","enum":["inner","left","full"]}
			},"required":["left","right","left_key","right_key","mode"],"additionalProperties":false
		}`),
		OutputSchema: json.RawMessage(`{"type":"array","maxItems":10000,"items":{"type":"object","properties":{"left":{},"right":{}},"required":["left","right"],"additionalProperties":false}}`),
		Resources:    func(json.RawMessage) ([]string, error) { return nil, nil },
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			output, err := executeJoin(ctx, raw)
			if err != nil && ctx.Err() == nil {
				return output, &workflow.ExecutionError{Code: "join_failed", Err: err}
			}
			return output, err
		},
	}
}

func joinKey(row json.RawMessage, pointer string) (string, error) {
	raw, err := recipe.OutputAt(row, pointer)
	if err != nil {
		return "", err
	}
	if len(raw) > maxJoinKeyBytes {
		return "", fmt.Errorf("join key exceeds %d bytes", maxJoinKeyBytes)
	}
	if len(raw) > 0 && raw[0] == '"' {
		var key string
		err = json.Unmarshal(raw, &key)
		return "string:" + key, err
	}
	if strings.ContainsAny(string(raw), ".eE") {
		return "", fmt.Errorf("join keys must be strings or integer literals")
	}
	var integer big.Int
	_, ok := integer.SetString(string(raw), 10)
	if !ok {
		return "", fmt.Errorf("join keys must be non-null strings or integer literals")
	}
	return "integer:" + integer.String(), nil
}

func executeJoin(ctx context.Context, raw json.RawMessage) (string, error) {
	err := ctx.Err()
	if err != nil {
		return "", err
	}
	if len(raw) > recipe.MaxArguments {
		return "", fmt.Errorf("join arguments exceed %d bytes", recipe.MaxArguments)
	}
	var in joinInput
	err = json.Unmarshal(raw, &in)
	if err != nil {
		return "", err
	}
	if in.Left == nil || in.Right == nil || len(in.Left) > maxJoinRows || len(in.Right) > maxJoinRows {
		return "", fmt.Errorf("join requires two arrays of at most %d rows", maxJoinRows)
	}
	if in.Mode != "inner" && in.Mode != "left" && in.Mode != "full" {
		return "", fmt.Errorf("join mode must be inner, left or full")
	}
	// Empty arrays still require valid pointers.
	escapes := strings.NewReplacer("~0", "", "~1", "")
	for _, pointer := range []string{in.LeftKey, in.RightKey} {
		unescaped := escapes.Replace(pointer)
		validStart := pointer == "" || strings.HasPrefix(pointer, "/")
		if !validStart || strings.Contains(unescaped, "~") || len(pointer) > maxJoinKeyBytes {
			return "", fmt.Errorf("invalid join key pointer %q", pointer)
		}
	}

	rightIndex := make(map[string][]int, len(in.Right))
	for i, row := range in.Right {
		err = ctx.Err()
		if err != nil {
			return "", err
		}
		key, keyErr := joinKey(row, in.RightKey)
		if keyErr != nil {
			return "", fmt.Errorf("right row %d: %w", i, keyErr)
		}
		rightIndex[key] = append(rightIndex[key], i)
	}

	pairs := []joinPair{}
	matched := make([]bool, len(in.Right))
	outputBytes := 2 // JSON array brackets; each pair contributes its comma below.
	appendPair := func(pair joinPair) error {
		contextErr := ctx.Err()
		if contextErr != nil {
			return contextErr
		}
		encoded, encodeErr := json.Marshal(pair)
		if encodeErr != nil {
			return encodeErr
		}
		outputBytes += len(encoded)
		if len(pairs) > 0 {
			outputBytes++
		}
		if len(pairs) >= maxJoinRows || outputBytes > recipe.MaxOutput {
			return fmt.Errorf("join exceeds %d pairs or %d output bytes", maxJoinRows, recipe.MaxOutput)
		}
		pairs = append(pairs, pair)
		return nil
	}
	for i, row := range in.Left {
		err = ctx.Err()
		if err != nil {
			return "", err
		}
		key, keyErr := joinKey(row, in.LeftKey)
		if keyErr != nil {
			return "", fmt.Errorf("left row %d: %w", i, keyErr)
		}
		matches := rightIndex[key]
		if len(matches) == 0 && in.Mode != "inner" {
			err = appendPair(joinPair{Left: row})
			if err != nil {
				return "", err
			}
		}
		for _, j := range matches {
			err = appendPair(joinPair{Left: row, Right: in.Right[j]})
			if err != nil {
				return "", err
			}
			matched[j] = true
		}
	}
	if in.Mode == "full" {
		for i, row := range in.Right {
			if matched[i] {
				continue
			}
			err = appendPair(joinPair{Right: row})
			if err != nil {
				return "", err
			}
		}
	}
	encoded, err := json.Marshal(pairs)
	return string(encoded), err
}
