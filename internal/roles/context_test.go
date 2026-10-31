package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/workspace"
)

func TestStartupContextMatchesRequest(t *testing.T) {
	e := testEnv(t)
	c := &recordingClient{}
	e.Client = c
	var reports []llm.ContextReport
	e.OnPrompt = func(r llm.ContextReport) { reports = append(reports, r) }
	a := Interactive(e, "", "", func(context.Context, string) (string, error) { return "", nil }, nil)
	before := a.StartupContext("Explain this project")
	if _, err := a.Run(t.Context(), "Explain this project"); err != nil {
		t.Fatal(err)
	}
	if len(c.reqs) != 1 || len(reports) != 1 || before.EstimatedTokens != llm.EstimatePromptTokens(c.reqs[0]) {
		t.Fatal(before, reports)
	}
	if before.EstimatedTokens != reports[0].EstimatedTokens {
		t.Fatal("preview drifted from request")
	}
}

func TestBrowserEnabledStartupStaysWithinPromptBudget(t *testing.T) {
	e := testEnv(t)
	e.Browser = &tools.Browser{Channel: "chrome", Dir: e.WS.Root()}
	a := Interactive(e, "", "", func(context.Context, string) (string, error) { return "", nil }, nil)
	report := a.StartupContext("Explain this project")
	if report.EstimatedTokens > 5000 {
		t.Fatalf("startup estimate %d exceeds 5000; keep instructions dense and optional tools deferred", report.EstimatedTokens)
	}
	for _, def := range report.Tools {
		if def.Name == "browser_observe" || strings.HasPrefix(def.Name, "mcp__browser__") {
			t.Fatal("browser schema loaded at startup", def.Name)
		}
	}
}

func TestInactiveCatalogSizeDoesNotGrowStartupContext(t *testing.T) {
	e := testEnv(t)
	loads := 0
	var baseline int
	for _, count := range []int{1, 100} {
		e.Capabilities = nil
		for i := range count {
			e.Capabilities = append(e.Capabilities, &tools.Capability{ID: fmt.Sprintf("optional-%d", i), Description: "Read optional content", Load: func(context.Context, func(string)) ([]agent.Tool, error) {
				loads++
				return nil, fmt.Errorf("must not run")
			}})
		}
		a := Interactive(e, "", "", nil, nil)
		report := a.StartupContext("explain")
		if count == 1 {
			baseline = report.EstimatedTokens
		} else if report.EstimatedTokens != baseline {
			t.Fatal("inactive catalogs grew prompt", baseline, report.EstimatedTokens)
		}
		for _, tool := range report.Tools {
			if tool.Name == "cap__social__read_post" {
				t.Fatal("native reader schema leaked into startup")
			}
		}
	}
	if loads != 0 {
		t.Fatal("startup loaded adapters")
	}
}

// One-token request measures prompt tokens; no model-issued tools are executed.
func TestStartupContextLive(t *testing.T) {
	url := os.Getenv("GEKTOR_CONTEXT_LIVE_URL")
	if url == "" {
		t.Skip("set GEKTOR_CONTEXT_LIVE_URL to measure a local llama.cpp prompt")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.NewWithSkillRoots(root, workspace.DefaultSkillRoots())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	c := &recordingClient{}
	e := Env{WS: ws, Procs: tools.NewBackgroundProcesses(), Client: c, Identity: "You are a helpful assistant."}
	a := Interactive(e, "", "", func(context.Context, string) (string, error) { return "", nil }, nil)
	if _, err := a.Run(t.Context(), "Explain this project"); err != nil {
		t.Fatal(err)
	}
	req := c.reqs[0]
	req.MaxTokens = 1
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	server := llm.NewLlamaClient(url, "local")
	resp, err := server.Chat(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	report := a.Report().Prompt
	report.BackendTokens, report.CachedTokens, report.BackendEstimated = resp.Usage.PromptTokens, resp.Usage.CachedTokens, resp.Usage.Estimated
	data, _ := json.MarshalIndent(report, "", "  ")
	if path := os.Getenv("GEKTOR_CONTEXT_LIVE_OUT"); path != "" {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("estimate=%d backend=%d cached=%d", report.EstimatedTokens, report.BackendTokens, report.CachedTokens)
	if report.BackendTokens <= 0 || report.BackendEstimated {
		t.Fatal("backend did not measure prompt tokens")
	}
}
