package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

func TestCompletionRetainsArrivingNotificationsAndInput(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		for _, forced := range []bool{false, true} {
			for _, limit := range []bool{false, true} {
				for _, event := range []string{"notification", "notification_with_inbox", "input", "both"} {
					t.Run(fmt.Sprintf("persisted=%v/forced=%v/limit=%v/%s", persisted, forced, limit, event), func(t *testing.T) {
						var inbox *Inbox
						if event != "notification" {
							inbox = &Inbox{}
						}
						wantInput := event == "input" || event == "both"
						wantNote := event != "input"
						finishCall := 1
						if forced {
							finishCall = 4
						}
						maxSteps := 8
						if limit {
							maxSteps = finishCall
						}
						calls, delivered := 0, 0
						pending := false
						client := inputClient(func(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
							calls++
							if calls < finishCall {
								return stepOf(llm.ToolCall{ID: fmt.Sprint(calls), Name: "echo", Arguments: json.RawMessage(`{}`)}), nil
							}
							if calls == finishCall {
								pending = wantNote
								if wantInput {
									if err := inbox.Send("new direction"); err != nil {
										t.Fatal(err)
									}
								}
								return say("early answer"), nil
							}
							if wantNote && !historyContains(req.Messages, "background finished") {
								t.Fatal("notification missing from next request")
							}
							if wantInput && !historyContains(req.Messages, "new direction") {
								t.Fatal("input missing from next request")
							}
							if forced {
								toolResults := 0
								for _, message := range req.Messages {
									if message.Role == llm.RoleTool {
										toolResults++
									}
								}
								if toolResults != 3 || !historyContains(req.Messages, "early answer") {
									t.Fatal("forced continuation lost history", req.Messages)
								}
							}
							return say("updated answer"), nil
						})
						path := ""
						if persisted {
							path = filepath.Join(t.TempDir(), "state.json")
						}
						a := New(Config{Client: client, Tools: NewRegistry(echoToolStub{}), Inbox: inbox, MaxSteps: maxSteps, StateFile: path,
							Notifications: func(context.Context) []string {
								if !pending {
									return nil
								}
								pending = false
								delivered++
								return []string{"background finished"}
							},
						})
						answer, err := a.Run(t.Context(), "task")
						if limit {
							if !errors.Is(err, ErrMaxSteps) || answer != "" || a.Report().Final != "" || calls != finishCall {
								t.Fatalf("limit: answer=%q err=%v calls=%d report=%+v", answer, err, calls, a.Report())
							}
						} else if err != nil || answer != "updated answer" || calls != finishCall+1 {
							t.Fatalf("answer=%q err=%v calls=%d", answer, err, calls)
						}
						if a.Report().Steps != calls || pending || (wantNote && delivered != 1) {
							t.Fatalf("calls=%d delivered=%d pending=%v report=%+v", calls, delivered, pending, a.Report())
						}
						history := a.snapshot.History
						if persisted {
							var err error
							history, err = conversation.ReadHistory(path)
							if err != nil {
								t.Fatal(err)
							}
						}
						if wantNote && !historyContains(history, "background finished") {
							t.Fatal("notification not saved")
						}
						if wantInput && !historyContains(history, "new direction") {
							t.Fatal("input not saved")
						}
						if inbox != nil && !errors.Is(inbox.Send("late"), ErrInputClosed) {
							t.Fatal("completed run still accepts input")
						}
					})
				}
			}
		}
	}
}

func historyContains(history []llm.Message, text string) bool {
	for _, message := range history {
		if strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}
