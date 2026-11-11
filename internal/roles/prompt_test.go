package roles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/llm/chatcompletions"
	"github.com/keshon/gektor/internal/prompts"
)

func TestPlanAndActUseProfileIdentity(t *testing.T) {
	for _, plan := range []bool{true, false} {
		e := testEnv(t)
		client := &identityClient{t: t}
		e.Client = client
		e.Identity = "Assistant name: Ada"
		e.IdentityName = "Ada"

		a, aErr := Interactive(e, "", "", nil, nil)
		if aErr != nil {
			t.Fatal(aErr)
		}
		if plan {
			a, aErr = Planner(e, "", "", nil)
			if aErr != nil {
				t.Fatal(aErr)
			}
		}
		if _, err := a.Run(context.Background(), "Hello"); err != nil {
			t.Fatal(err)
		}
		if client.calls == 0 {
			t.Fatal("no backend request")
		}
	}
}

func TestConversationPromptDoesNotDependOnBackendKind(t *testing.T) {
	e := testEnv(t)
	var baseline llm.ChatRequest
	for _, kind := range []string{"llama", "kobold", "openai"} {
		client := &recordingClient{}
		e.Client, e.BackendKind = client, kind
		interactiveAgent, interactiveAgentErr := Interactive(e, "", "", nil, nil)
		if interactiveAgentErr != nil {
			t.Fatal(interactiveAgentErr)
		}
		if _, err := interactiveAgent.Run(t.Context(), "task"); err != nil {
			t.Fatal(err)
		}
		request := client.reqs[0]
		if baseline.Messages == nil {
			baseline = request
			continue
		}
		if request.Messages[0].Content != baseline.Messages[0].Content || !reflect.DeepEqual(request.Tools, baseline.Tools) {
			t.Fatalf("backend %s changed harness instructions or capabilities", kind)
		}
	}
}

// A backend reuses a cached prompt only while the new one starts with the
// old one. Plan and act are the same conversation in different stances, so
// they must send the same system prompt and the same tool definitions -
// otherwise every mode switch makes the backend re-read the whole
// conversation, which on a large window is minutes of prefill.
func TestPlanAndActSendTheSamePrefix(t *testing.T) {
	e := testEnv(t)
	ask := func(context.Context, string) (string, error) { return "", nil }

	run := func(a *agent.Agent, rec *recordingClient) llm.ChatRequest {
		t.Helper()
		if _, err := a.Run(context.Background(), "task"); err != nil {
			t.Fatal(err)
		}
		if len(rec.reqs) == 0 {
			t.Fatal("no backend request")
		}
		return rec.reqs[0]
	}

	planRec := &recordingClient{}
	e.Client = planRec
	plannerAgent, plannerAgentErr := Planner(e, "plan", "", ask)
	if plannerAgentErr != nil {
		t.Fatal(plannerAgentErr)
	}
	plan := run(plannerAgent, planRec)

	actRec := &recordingClient{}
	e.Client = actRec
	interactiveAgent, interactiveAgentErr := Interactive(e, "", "", ask, nil)
	if interactiveAgentErr != nil {
		t.Fatal(interactiveAgentErr)
	}
	act := run(interactiveAgent, actRec)

	if plan.Messages[0].Content != act.Messages[0].Content {
		t.Fatal("plan and act send different system prompts; a mode switch would re-read the whole conversation")
	}
	if plan.Messages[1].Content != act.Messages[1].Content {
		t.Fatalf("plan and act send different tasks: %q / %q", plan.Messages[1].Content, act.Messages[1].Content)
	}
	if !reflect.DeepEqual(plan.Tools, act.Tools) {
		t.Fatal("plan and act send different tool definitions; a mode switch would re-read the whole conversation")
	}
	if got, want := plan.Messages[len(plan.Messages)-1].Content, "[harness] "+prompts.PlanMode; got != want {
		t.Fatalf("plan stance = %q, want %q appended at the tail", got, want)
	}
	if len(act.Messages) != 3 || act.Messages[2].Content != "[harness] "+prompts.ActMode {
		t.Fatalf("act run lacks an explicit stance: %+v", act.Messages)
	}
}

func TestPlanActPlanKeepsLatestModeAndPrefix(t *testing.T) {
	e := testEnv(t)
	c := &recordingClient{}
	e.Client = c
	var history []llm.Message
	for i, plan := range []bool{true, false, true, false, true} {
		a, aErr := Interactive(e, "", "", nil, nil)
		if aErr != nil {
			t.Fatal(aErr)
		}
		directive := prompts.ActMode
		if plan {
			a, aErr = Planner(e, "", "", nil)
			if aErr != nil {
				t.Fatal(aErr)
			}
			directive = prompts.PlanMode
		}
		var err error
		if i == 0 {
			_, err = a.Run(t.Context(), "task")
		} else {
			_, err = a.Resume(t.Context(), history, "next")
		}
		if err != nil {
			t.Fatal(err)
		}
		r := c.reqs[i]
		if r.Messages[len(r.Messages)-1].Content != "[harness] "+directive {
			t.Fatalf("switch %d has stale mode: %+v", i, r.Messages)
		}
		if i > 0 && !reflect.DeepEqual(c.reqs[0].Tools, r.Tools) {
			t.Fatal("tool schemas changed")
		}
		history = append(append([]llm.Message(nil), r.Messages...), llm.Message{Role: llm.RoleAssistant, Content: "done"})
	}
}

func TestModeSwitchWirePrefixAcrossProviders(t *testing.T) {
	for _, kind := range []string{"llama", "openai", "kobold"} {
		t.Run(kind, func(t *testing.T) {
			var requests []struct {
				Messages []json.RawMessage `json:"messages"`
				Tools    json.RawMessage   `json:"tools"`
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Messages []json.RawMessage `json:"messages"`
					Tools    json.RawMessage   `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				requests = append(requests, req)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2000,"completion_tokens":2}}`))
			}))
			defer srv.Close()
			e := testEnv(t)
			client, err := chatcompletions.ClientFor(kind, srv.URL, "test")
			if err != nil {
				t.Fatal(err)
			}
			e.Client = client
			e.BackendKind = kind
			e.ContextLimit = 8192
			e.MaxTokens = 100
			state := filepath.Join(t.TempDir(), "state.json")
			var history []llm.Message
			for i, plan := range []bool{true, false, true} {
				a, aErr := Interactive(e, "", state, nil, nil)
				if aErr != nil {
					t.Fatal(aErr)
				}
				directive := prompts.ActMode
				if plan {
					a, aErr = Planner(e, "", state, nil)
					if aErr != nil {
						t.Fatal(aErr)
					}
					directive = prompts.PlanMode
				}
				if i == 0 {
					_, err = a.Run(t.Context(), "respond with done")
				} else {
					_, err = a.Resume(t.Context(), history, "next")
				}
				if err != nil {
					t.Fatal(err)
				}
				history, err = agent.LoadState(state)
				if err != nil {
					t.Fatal(err)
				}
				var last struct {
					Content string `json:"content"`
				}
				if err := json.Unmarshal(requests[i].Messages[len(requests[i].Messages)-1], &last); err != nil {
					t.Fatal(err)
				}
				if last.Content != "[harness] "+directive && !(kind == "llama" && strings.HasPrefix(last.Content, "[harness] Attribution reminder") && strings.Contains(last.Content, directive)) {
					t.Fatalf("stale stance: %q", last.Content)
				}
				if i > 0 {
					if string(requests[i-1].Tools) != string(requests[i].Tools) {
						t.Fatal("wire tool definitions changed")
					}
					for j, msg := range requests[i-1].Messages {
						if kind == "llama" && j == len(requests[i-1].Messages)-1 && strings.Contains(string(msg), "Attribution reminder") {
							continue
						}
						if string(msg) != string(requests[i].Messages[j]) {
							t.Fatalf("wire prefix changed at message %d", j)
						}
					}
				}
			}
		})
	}
}
