package retrospect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
)

func TestReviewBudgetFailureRetainsDiagnosticsAndClearsOnSuccess(t *testing.T) {
	c, _ := archiveFixture(t)
	c.Scope = "session"
	step := 0
	c.Client = clientFunc(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		step++
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: fmt.Sprint(step), Name: "read_history", Arguments: json.RawMessage(fmt.Sprintf(`{"start":%d,"count":1}`, 2+step%7))}}}}, nil
	})
	var selected Progress
	c.OnProgress = func(p Progress) {
		if p.Scope != "" {
			selected = p
		}
	}
	_, err := Evaluate(t.Context(), c)
	if !errors.Is(err, agent.ErrMaxSteps) {
		t.Fatal(err)
	}
	var failure *Failure
	if !errors.As(err, &failure) || failure.Progress.Step != 12 || failure.Progress.Read == 0 || selected.Request != "first task" {
		t.Fatalf("%+v, progress=%+v", failure, selected)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(c.StateFile), "evaluation-failure.json"))
	if err != nil || !strings.Contains(string(data), "budget exhausted") {
		t.Fatalf("%s %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(c.StateFile), "evaluation.json")); !os.IsNotExist(err) {
		t.Fatal("failed review saved a result")
	}
	c.Client = reviewClient(t, 2, 4, true, nil)
	if _, err := Evaluate(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(failure.Path); !os.IsNotExist(err) {
		t.Fatal("stale failure survived success", err)
	}
}

func TestReviewFailureRecordsRejectedEvidence(t *testing.T) {
	c, _ := archiveFixture(t)
	c.Scope = "session"
	step := 0
	c.Client = clientFunc(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		step++
		if step == 1 {
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "bad", Name: "submit_evaluation", Arguments: json.RawMessage(`{"summary":"claim","findings":[{"text":"not read","evidence":[4]}],"memory":[]}`)}}}}, nil
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
	})
	_, err := Evaluate(t.Context(), c)
	var failure *Failure
	if !errors.As(err, &failure) || !strings.Contains(failure.LastValidation, "read message 4") {
		t.Fatalf("%v", err)
	}
}
