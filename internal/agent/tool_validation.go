package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/llm"
)

// validateToolCalls runs serially before any calls are admitted, so newly found
// scoped instructions can withhold mutations before execution begins.
func (a *Agent) validateToolCalls(calls []llm.ToolCall, finishReason string, history []llm.Message, results []callResult) ([]bool, []string) {
	skipped := make([]bool, len(calls))
	var newInstructions []string
	for i, call := range calls {
		if results[i].err != nil {
			skipped[i] = true
			continue
		}
		// Truncated arguments (generation cut off mid-call) are stumps, not
		// calls: executing them fails arg parsing here and 500s backends that
		// parse server-side. Refuse with reissue-smaller guidance instead.
		if !json.Valid(call.Arguments) || call.InputError != "" {
			skipped[i] = true
			kind := "invalid"
			if finishReason == "length" {
				kind = "truncated"
			}
			results[i].err = fmt.Errorf("%s JSON arguments; call %s (%s) was NOT executed. Arguments were supplied but could not be parsed, not omitted. finish_reason=%q. Reissue complete valid arguments%s. Original input excerpt (%d bytes):\n%s", kind, call.ID, call.Name, finishReason, map[bool]string{true: " in smaller calls because the output limit was reached", false: ""}[finishReason == "length"], len(call.Arguments), TruncateMiddle(string(call.Arguments), 1800))
			continue
		}
		// A read-only role is offered the full tool set so a mode switch
		// cannot rewrite the prompt prefix, so refusal has to be explicit:
		// nothing runs, nothing changes, and the model is told what to do
		// instead.
		if err := a.cfg.Tools.ValidateCall(call.Name, call.Arguments); err != nil {
			skipped[i] = true
			results[i].err = err
			continue
		}
		if !a.mayRunCall(call.Name, call.Arguments) {
			skipped[i] = true
			results[i].err = fmt.Errorf(
				"this run is read-only: %s is unavailable and nothing was changed - "+
					"state the change as a step in the plan and let the operator apply it", call.Name)
			continue
		}

		if a.cfg.Instructions != nil && !skipped[i] {
			text, err := a.cfg.Instructions(call.Arguments)
			if err != nil {
				skipped[i] = true
				results[i].err = fmt.Errorf("load scoped instructions: %w", err)
				continue
			}
			loaded := false
			for _, msg := range history {
				if strings.Contains(msg.Content, text) {
					loaded = true
					break
				}
			}
			if text != "" && !loaded {
				if !containsStr(newInstructions, text) {
					newInstructions = append(newInstructions, text)
				}
				if a.cfg.Tools.ModeOf(call.Name) == Exclusive {
					skipped[i] = true
					results[i].err = fmt.Errorf("new scoped instructions were discovered; nothing was executed. Read the following harness instructions and reconsider this call before retrying")
				}
			}
		}
	}

	return skipped, newInstructions
}

// callResources prefers the tool's canonical resources, then the frontend
// resolver, then the ordinary URL/command resource.
func (a *Agent) callResources(call llm.ToolCall) ([]string, error) {
	var resources []string
	if tool, ok := a.cfg.Tools.lookup(call.Name); ok {
		if scoped, ok := tool.(interface {
			Resources(json.RawMessage) ([]string, error)
		}); ok {
			var err error
			resources, err = scoped.Resources(call.Arguments)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(resources) == 0 && a.cfg.Resources != nil {
		var err error
		resources, err = a.cfg.Resources(call.Name, call.Arguments)
		if err != nil {
			return nil, err
		}
	}
	if len(resources) == 0 {
		resources = []string{toolResource(call.Arguments)}
	}
	return resources, nil
}
