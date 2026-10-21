package retrospect

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
	"github.com/keshon/gektor/internal/workspace"
)

type clientFunc func(context.Context, llm.ChatRequest) (llm.ChatResponse, error)

func (f clientFunc) Chat(ctx context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	return f(ctx, r)
}

func archiveFixture(t *testing.T) (Config, session.Snapshot) {
	t.Helper()
	ok := false
	s := session.New([]llm.Message{
		{Role: llm.RoleSystem, Content: "original system"},
		{Role: llm.RoleUser, Content: "first task"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "a", Name: "run_shell", Arguments: json.RawMessage(`{"command":"go test"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "a", ToolFailed: &ok, Content: "PASS first verification"},
		{Role: llm.RoleAssistant, Content: "first answer"},
		{Role: llm.RoleUser, Content: "second task"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "b", Name: "run_shell", Arguments: json.RawMessage(`{"command":"go vet"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "b", ToolFailed: &ok, Content: "PASS second verification"},
		{Role: llm.RoleAssistant, Content: "second answer"},
	})
	s.Runs = []session.Run{{Start: 1, End: 5, Outcome: "completed", Steps: 2}, {Start: 5, End: 9, Outcome: "completed", Steps: 2}}
	// Two compactions hide the first run from the working context.
	s.Compact(append(append([]llm.Message(nil), s.History[:2]...), append([]llm.Message{{Role: llm.RoleUser, Content: "summary one"}}, s.History[5:]...)...))
	s.Compact(append(append([]llm.Message(nil), s.History[:2]...), llm.Message{Role: llm.RoleUser, Content: "summary two"}, s.History[8]))
	path := filepath.Join(t.TempDir(), "state.json")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Config{Workspace: ws, StateFile: path}, s
}

func reviewClient(t *testing.T, start, evidence int, memory bool, beforeFinish func()) llm.Client {
	t.Helper()
	step := 0
	return clientFunc(func(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		if len(req.Tools) != 2 || req.Tools[0].Name == "run_shell" || req.Messages[0].Content == "original system" {
			t.Fatalf("review inherited working tools or context: %+v", req)
		}
		step++
		var call llm.ToolCall
		switch step {
		case 1:
			args, _ := json.Marshal(map[string]int{"start": start, "count": 4})
			call = llm.ToolCall{ID: "read", Name: "read_history", Arguments: args}
		case 2:
			if last := req.Messages[len(req.Messages)-1]; last.ToolFailed == nil || *last.ToolFailed || !strings.Contains(last.Content, "verification") {
				t.Fatalf("archive read failed: %+v", last)
			}
			r := Result{Summary: "Observed a successful command, not proof of general correctness.", Findings: []Finding{{Text: "Verification is archived.", Evidence: []int{evidence}}}}
			if memory {
				r.Memory = []Memory{{When: "Repeating the first check", Lesson: "The recorded command passed in this session; check current files first.", Evidence: []int{evidence}, Quote: "PASS first verification"}}
			}
			args, _ := json.Marshal(struct {
				Summary  string    `json:"summary"`
				Findings []Finding `json:"findings"`
				Memory   []Memory  `json:"memory"`
			}{r.Summary, r.Findings, r.Memory})
			call = llm.ToolCall{ID: "submit", Name: "submit_evaluation", Arguments: args}
		case 3:
			if last := req.Messages[len(req.Messages)-1]; last.ToolFailed == nil || *last.ToolFailed {
				t.Fatalf("submission failed: %+v", last)
			}
			if beforeFinish != nil {
				beforeFinish()
			}
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "Review complete."}}, nil
		default:
			t.Fatal("unexpected review continuation")
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}}}, nil
	})
}

func TestEvaluationProgressFollowsObservedWork(t *testing.T) {
	c, _ := archiveFixture(t)
	c.Scope = "1"
	c.Client = reviewClient(t, 2, 4, false, nil)
	var updates []Progress
	c.OnProgress = func(p Progress) { updates = append(updates, p) }
	if _, err := Evaluate(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if len(updates) == 0 || updates[0].Stage != "Loading saved archive" {
		t.Fatalf("missing archive load: %+v", updates)
	}
	waiting := map[int]bool{}
	read := false
	for _, p := range updates {
		if p.Stage == "Waiting for model response" {
			waiting[p.Step] = true
		}
		if p.Read == 4 && p.Total == 4 {
			read = true
		}
		if p.Read > p.Total || p.Step > p.Steps {
			t.Fatalf("invalid counters: %+v", p)
		}
	}
	if !waiting[1] || !waiting[2] || !waiting[3] || waiting[4] || !read {
		t.Fatalf("progress did not follow three requests and four archive excerpts: %+v", updates)
	}
	if last := updates[len(updates)-1]; last.Stage != "Saving evaluation" || last.Read != 4 {
		t.Fatalf("missing persistence phase: %+v", last)
	}
}

func TestEvaluateCompactedArchiveAndDeduplicatesMemory(t *testing.T) {
	c, _ := archiveFixture(t)
	catalogPath := filepath.Join(c.Workspace.Root(), "skills", "README.md")
	if err := os.MkdirAll(filepath.Dir(catalogPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, []byte("# Existing catalog\n- Keep this entry.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(c.StateFile)
	c.Scope = "1"
	for range 2 {
		c.Client = reviewClient(t, 2, 4, true, nil)
		r, err := Evaluate(t.Context(), c)
		if err != nil {
			t.Fatal(err)
		}
		if r.Start != 2 || r.End != 5 || r.Read != 4 || r.Partial != 0 || r.SourceSHA256 == "" || r.Saved == "" || r.Warning != "" {
			t.Fatalf("unexpected report: %+v", r)
		}
	}
	current, _ := os.ReadFile(c.StateFile)
	if string(current) != string(original) {
		t.Fatal("review changed original session")
	}
	memory, err := os.ReadFile(filepath.Join(c.Workspace.Root(), "skills", "project-memory", "SKILL.md"))
	if err != nil || strings.Count(string(memory), "## Repeating") != 1 || !strings.Contains(string(memory), "PASS first verification") {
		t.Fatalf("missing or duplicate memory: %s %v", memory, err)
	}
	catalog, err := os.ReadFile(catalogPath)
	if err != nil || strings.Count(string(catalog), "project-memory/SKILL.md") != 2 || !strings.Contains(string(catalog), "Keep this entry.") {
		t.Fatalf("invalid catalog: %s %v", catalog, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(c.StateFile), "evaluation.json")); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluateLastWithoutMemoryDoesNotCreateSkills(t *testing.T) {
	c, _ := archiveFixture(t)
	c.Client = reviewClient(t, 6, 8, false, nil)
	r, err := Evaluate(t.Context(), c)
	if err != nil || r.Start != 6 || r.End != 9 || r.Read != 4 || r.Saved != "" {
		t.Fatalf("last run: %+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(c.Workspace.Root(), "skills")); !os.IsNotExist(err) {
		t.Fatalf("empty memory changed workspace: %v", err)
	}
}

func TestEvaluateRejectsChangedArchiveAndCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed archive", true: "cancelled"}[cancel], func(t *testing.T) {
			c, _ := archiveFixture(t)
			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			c.Scope = "1"
			c.Client = reviewClient(t, 2, 4, true, func() {
				if cancel {
					stop()
				} else if err := os.WriteFile(c.StateFile, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			})
			if _, err := Evaluate(ctx, c); err == nil {
				t.Fatal("accepted invalidated review")
			}
			for _, path := range []string{filepath.Join(filepath.Dir(c.StateFile), "evaluation.json"), filepath.Join(c.Workspace.Root(), "skills")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("invalidated review wrote %s: %v", path, err)
				}
			}
		})
	}
}

func TestScopeLegacyAndTrailingRun(t *testing.T) {
	_, s := archiveFixture(t)
	for _, selection := range []string{"0", "3", "garbage"} {
		if _, _, _, err := scope(s, selection); err == nil {
			t.Fatalf("accepted %q", selection)
		}
	}
	s.History = append(s.History, llm.Message{Role: llm.RoleUser, Content: "unfinished"})
	if _, _, _, err := scope(s, ""); err == nil {
		t.Fatal("ignored unfinished trailing task")
	}
	s.Runs = nil
	if _, _, _, err := scope(s, ""); err == nil {
		t.Fatal("guessed legacy run boundaries")
	}
	start, end, _, err := scope(s, "session")
	if err != nil || start != 1 || end != len(s.History) {
		t.Fatalf("legacy whole session: %d %d %v", start, end, err)
	}
}

func TestSubmitRequiresReadSuccessfulLiteralEvidence(t *testing.T) {
	_, s := archiveFixture(t)
	for _, scenario := range []string{"valid", "unread", "outside", "failed", "legacy status", "invented quote", "forged path"} {
		t.Run(scenario, func(t *testing.T) {
			archive := &archiveTool{snapshot: s, seen: map[int]string{4: "PASS first verification"}}
			archive.snapshot.History = append([]llm.Message(nil), s.History...)
			tool := &submitTool{archive: archive, start: 1, end: 5}
			quote := "PASS first verification"
			switch scenario {
			case "unread":
				delete(archive.seen, 4)
			case "outside":
				tool.start = 5
			case "failed":
				failed := true
				archive.snapshot.History[3].ToolFailed = &failed
			case "legacy status":
				archive.snapshot.History[3].ToolFailed = nil
			case "invented quote":
				quote = "Never happened"
			}
			body, _ := json.Marshal(map[string]any{"summary": "Review", "findings": []Finding{}, "memory": []Memory{{When: "condition", Lesson: "observation", Evidence: []int{4}, Quote: quote}}})
			if scenario == "forged path" {
				body = []byte(strings.TrimSuffix(string(body), "}") + `,"saved":"arbitrary-path"}`)
			}
			_, err := tool.Run(t.Context(), body)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("submission: %v", err)
			}
		})
	}
}

func TestArchivePagesUnicodeAndKeepsReasoningAndCallArguments(t *testing.T) {
	text := strings.Repeat("界", 2500)
	s := session.New([]llm.Message{{Role: llm.RoleAssistant, Content: text, Reasoning: "reason", ToolCalls: []llm.ToolCall{{ID: "call", Name: "read_file", Arguments: json.RawMessage(`{"path":"original.go"}`)}}}})
	tool := &archiveTool{snapshot: s, seen: map[int]string{}}
	first, err := tool.Run(t.Context(), json.RawMessage(`{"start":1,"count":1}`))
	if err != nil || !utf8.ValidString(first) || !strings.Contains(first, `"next_offset":2000`) {
		t.Fatalf("first page: %v %s", err, first)
	}
	second, err := tool.Run(t.Context(), json.RawMessage(`{"start":1,"count":1,"offset":2000}`))
	if err != nil || !utf8.ValidString(second) || !strings.Contains(second, "reason") || !strings.Contains(second, "original.go") {
		t.Fatalf("second page: %v %s", err, second)
	}
	if !strings.HasPrefix(tool.seen[1], text) {
		t.Fatal("paging lost Unicode characters")
	}
	if tool.covered[1] != len([]rune(messageText(s.History[0]))) {
		t.Fatal("completed pages still reported as partial")
	}
}

func TestEvaluateRequiresSubmission(t *testing.T) {
	c, _ := archiveFixture(t)
	c.Client = clientFunc(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "unsupported claim"}}, nil
	})
	if _, err := Evaluate(t.Context(), c); err == nil || !strings.Contains(err.Error(), "without submitting") {
		t.Fatalf("accepted unstructured assessment: %v", err)
	}
}

func TestSaveMemoryCancelledLeavesNoFiles(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	root := t.TempDir()
	_, err := saveMemory(ctx, root, Result{}, "source")
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("cancelled review wrote files")
	}
}
