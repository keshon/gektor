package retrospect

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/app"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/fs"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/llm/chatcompletions"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/workspace"
)

type learningTrial struct {
	Pair           int            `json:"pair"`
	Kind           string         `json:"kind"`
	Arm            string         `json:"arm"`
	Passed         bool           `json:"passed"`
	ArtifactPassed bool           `json:"artifact_passed"`
	LessonLoaded   bool           `json:"lesson_loaded"`
	Steps          int            `json:"steps"`
	ToolCalls      int            `json:"tool_calls"`
	ToolFailures   int            `json:"tool_failures"`
	Seconds        float64        `json:"seconds"`
	Usage          llm.Usage      `json:"usage"`
	Tools          map[string]int `json:"tools"`
	Nudges         []string       `json:"nudges,omitempty"`
	Error          string         `json:"error,omitempty"`
	Answer         string         `json:"answer"`
	Artifact       string         `json:"artifact"`
	Expected       string         `json:"expected"`
}

// This experiment measures an existing approved image-inspection lesson. It
// deliberately does not teach a new lesson using the test answers or reward
// loading the skill: only independently checked artifacts determine success.
func TestLiveLearnedImageSkill(t *testing.T) {
	if os.Getenv("GEKTOR_LIVE_LEARNING") != "1" {
		t.Skip("set GEKTOR_LIVE_LEARNING=1 for the opt-in paired experiment")
	}
	backend := os.Getenv("GEKTOR_LIVE_BACKEND")
	if backend == "" {
		backend = "http://127.0.0.1:9931"
	}
	skillPath := os.Getenv("GEKTOR_LIVE_LESSON")
	output := os.Getenv("GEKTOR_LIVE_OUTPUT")
	if skillPath == "" || output == "" {
		t.Fatal("set GEKTOR_LIVE_LESSON to an approved SKILL.md and GEKTOR_LIVE_OUTPUT to a new directory")
	}
	output, err := filepath.Abs(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal("use a fresh output directory:", err)
	}
	guide, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	lessonID := filepath.Base(filepath.Dir(skillPath))
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	client, err := app.NewBackend(t.Context(), app.BackendOptions{Kind: "llama", URL: backend, Model: "local"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	model, err := client.ModelName(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	vision, err := client.SupportsVision(ctx)
	cancel()
	if err != nil || !vision {
		t.Fatalf("image experiment needs confirmed vision support: %t %v", vision, err)
	}
	// Freeze stock workspace guides once. User libraries and other learned skills
	// are excluded equally in both arms; this is an isolated efficacy experiment.
	seed := filepath.Join(output, "seed")
	liveCopyTree(t, filepath.Join(repo, "eval", "fixtures"), seed, false)
	liveCopyTree(t, filepath.Join(repo, "skills"), filepath.Join(seed, "skills"), true)
	liveWrite(t, filepath.Join(output, "approved-SKILL.md"), guide)
	manifest := map[string]any{"model": model, "backend": backend, "lesson_sha256": fmt.Sprintf("%x", sha256.Sum256(guide)), "image_pairs": 10, "coding_pairs": 5, "max_steps": 12, "max_tokens": 8192, "temperature": 0.7, "context_limit": 131072, "timeout_seconds": 180, "order": "deterministically shuffled balanced AB/BA blocks, seed 20261031/71499", "success": "run completes and answer.txt exactly matches generated image ground truth; coding control preserves Version v1 and changes Greet hello to hi in <=8 steps", "scope": "production Interactive role, no forced retrieval; stock project skills included, user skill libraries and browser/MCP absent in both arms"}
	liveJSON(t, filepath.Join(output, "manifest.json"), manifest)
	t.Logf("model=%s output=%s", model, output)
	// A local image origin avoids Internet latency, changing pages and answer
	// leakage through filenames or HTML. Pixels and ground truth vary by pair.
	images := filepath.Join(output, "images")
	if err := os.Mkdir(images, 0700); err != nil {
		t.Fatal(err)
	}
	wants := make([]string, 10)
	for i := range wants {
		wants[i] = liveDrawPanel(t, filepath.Join(images, fmt.Sprintf("%02d.png", i)), i)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(images)))
	defer server.Close()
	results, err := os.OpenFile(filepath.Join(output, "results.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer results.Close()
	rng := rand.New(rand.NewPCG(20261031, 71499))
	order := []bool{true, true, true, true, true, false, false, false, false, false}
	rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	for pair := 0; pair < 15; pair++ {
		kind := "image"
		first := false
		if pair < 10 {
			first = order[pair]
		} else {
			kind = "coding"
			first = pair%2 == 0
		}
		for _, enabled := range []bool{first, !first} {
			arm := "without"
			if enabled {
				arm = "with"
			}
			name := fmt.Sprintf("%02d-%s-%s", pair+1, kind, arm)
			t.Run(name, func(t *testing.T) {
				dir := filepath.Join(output, name)
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				root := t.TempDir()
				liveCopyTree(t, seed, root, false)
				if enabled {
					liveWrite(t, filepath.Join(root, "skills", lessonID, "SKILL.md"), guide)
				}
				ws, err := workspace.New(root)
				if err != nil {
					t.Fatal(err)
				}
				defer ws.Close()
				inventory, err := ws.Skills()
				if err != nil {
					t.Fatal(err)
				}
				liveJSON(t, filepath.Join(dir, "skills.json"), inventory)
				trace, err := os.Create(filepath.Join(dir, "trace.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				defer trace.Close()
				encoder := json.NewEncoder(trace)
				res := learningTrial{Pair: pair + 1, Kind: kind, Arm: arm, Tools: map[string]int{}}
				calls := map[string]llm.ToolCall{}
				procs := tools.NewBackgroundProcesses()
				defer procs.StopAll()
				e := roles.Env{WS: ws, Procs: procs, Client: client, BackendKind: "llama", Vision: vision, MaxSteps: 12, MaxTokens: 8192, Temperature: 0.7, ContextLimit: 131072,
					OnStep: func(_ string, _ int, m llm.Message) {
						if err := encoder.Encode(map[string]any{"event": "assistant", "message": m}); err != nil {
							t.Error(err)
						}
						for _, call := range m.ToolCalls {
							calls[call.ID] = call
							res.ToolCalls++
							res.Tools[call.Name]++
						}
					},
					OnResult: func(r agent.ToolResult) {
						if err := encoder.Encode(map[string]any{"event": "tool", "result": r}); err != nil {
							t.Error(err)
						}
						if r.Failed {
							res.ToolFailures++
						}
						call := calls[r.CallID]
						if !r.Failed && (call.Name == "load_skill" || call.Name == "read_file") && strings.Contains(string(call.Arguments), lessonID) && strings.Contains(r.Output, "## Procedure") {
							res.LessonLoaded = true
						}
					},
					OnUsage: func(_ int, u llm.Usage) {
						res.Usage.PromptTokens += u.PromptTokens
						res.Usage.CompletionTokens += u.CompletionTokens
						res.Usage.CachedTokens += u.CachedTokens
						res.Usage.Estimated = res.Usage.Estimated || u.Estimated
					},
					OnNudge: func(kind, _ string) { res.Nudges = append(res.Nudges, kind) },
				}
				task := `find which file defines Greet and change it to return "hi" instead of "hello"`
				if kind == "image" {
					task = fmt.Sprintf("Inspect the image at %s/%02d.png. Identify the color and shape of the left-hand object and the right-hand object. Create answer.txt with exactly two lowercase lines: 'left: COLOR SHAPE' and 'right: COLOR SHAPE' (without quotes). Report only what you actually see.", server.URL, pair)
					res.Expected = wants[pair]
				}
				liveWrite(t, filepath.Join(dir, "task.txt"), []byte(task))
				a := roles.Interactive(e, "", filepath.Join(dir, "state.json"), func(context.Context, string) (string, error) {
					return "No human is available. Use the task instructions and report any blocker honestly.", nil
				}, nil)
				runCtx, stop := context.WithTimeout(t.Context(), 180*time.Second)
				started := time.Now()
				res.Answer, err = a.Run(runCtx, task)
				res.Seconds = time.Since(started).Seconds()
				stop()
				if err != nil {
					res.Error = err.Error()
				}
				res.Steps = a.Report().Steps
				artifactPath := "answer.txt"
				if kind == "coding" {
					artifactPath = "sample.go"
				}
				artifact, readErr := os.ReadFile(filepath.Join(root, artifactPath))
				res.Artifact = string(artifact)
				if kind == "image" {
					res.ArtifactPassed = readErr == nil && strings.Join(strings.Fields(res.Artifact), " ") == strings.Join(strings.Fields(res.Expected), " ")
				} else {
					res.ArtifactPassed = readErr == nil && strings.Contains(res.Artifact, `return "hi"`) && strings.Contains(res.Artifact, `const Version = "v1"`) && res.Tools["grep_files"] > 0 && res.Steps <= 8
				}
				res.Passed = res.ArtifactPassed && err == nil
				liveJSON(t, filepath.Join(dir, "result.json"), res)
				if err := json.NewEncoder(results).Encode(res); err != nil {
					t.Fatal(err)
				}
				if err := results.Sync(); err != nil {
					t.Fatal(err)
				}
				t.Logf("pass=%t loaded=%t steps=%d tools=%d failures=%d seconds=%.1f prompt=%d generated=%d error=%s", res.Passed, res.LessonLoaded, res.Steps, res.ToolCalls, res.ToolFailures, res.Seconds, res.Usage.PromptTokens, res.Usage.CompletionTokens, res.Error)
			})
		}
	}
}

func liveWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func liveJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	liveWrite(t, path, data)
}
func liveCopyTree(t *testing.T, source, destination string, excludeLearned bool) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if excludeLearned && (strings.HasPrefix(entry.Name(), "learned-") || entry.Name() == "project-memory") {
				return fs.SkipDir
			}
			return os.MkdirAll(filepath.Join(destination, rel), 0700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("fixture is not a regular file: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(destination, rel), data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
}
func liveDrawPanel(t *testing.T, path string, index int) string {
	t.Helper()
	colors := []color.RGBA{{220, 30, 30, 255}, {30, 70, 220, 255}, {20, 160, 50, 255}, {245, 195, 15, 255}}
	names := []string{"red", "blue", "green", "yellow"}
	shapes := []string{"circle", "square", "triangle"}
	img := image.NewRGBA(image.Rect(0, 0, 480, 240))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	labels := make([]string, 2)
	for side := 0; side < 2; side++ {
		ci := (index + side*2) % len(colors)
		si := (index/2 + side) % len(shapes)
		cx := 120 + side*240
		for y := 50; y < 190; y++ {
			for x := cx - 70; x < cx+70; x++ {
				dx, dy := x-cx, y-120
				inside := si == 1 || (si == 0 && dx*dx+dy*dy <= 65*65) || (si == 2 && y >= 55 && y <= 185 && absLive(dx)*2 <= y-55)
				if inside {
					img.SetRGBA(x, y, colors[ci])
				}
			}
		}
		labels[side] = names[ci] + " " + shapes[si]
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return "left: " + labels[0] + "\nright: " + labels[1] + "\n"
}
func absLive(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Measure extraction separately from reuse. The archive is copied and no
// proposal is approved, so this cannot alter the user's session or skills.
func TestLiveLearningReviewCost(t *testing.T) {
	if os.Getenv("GEKTOR_LIVE_LEARNING_REVIEW") != "1" {
		t.Skip("set GEKTOR_LIVE_LEARNING_REVIEW=1 to measure retrospective cost")
	}
	source := os.Getenv("GEKTOR_LIVE_REVIEW_SOURCE")
	scope := os.Getenv("GEKTOR_LIVE_REVIEW_SCOPE")
	if scope == "" {
		scope = "last"
	}
	output := os.Getenv("GEKTOR_LIVE_OUTPUT")
	backend := os.Getenv("GEKTOR_LIVE_BACKEND")
	if backend == "" {
		backend = "http://127.0.0.1:9931"
	}
	if source == "" || output == "" {
		t.Fatal("set GEKTOR_LIVE_REVIEW_SOURCE and a fresh GEKTOR_LIVE_OUTPUT")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(output, "state.json")
	liveWrite(t, state, data)
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	server := chatcompletions.NewLlamaClient(backend, "local")
	var usage llm.Usage
	requests := 0
	client := clientFunc(func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
		requests++
		response, err := server.Chat(ctx, request)
		usage.PromptTokens += response.Usage.PromptTokens
		usage.CompletionTokens += response.Usage.CompletionTokens
		usage.CachedTokens += response.Usage.CachedTokens
		usage.Estimated = usage.Estimated || response.Usage.Estimated
		return response, err
	})
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	started := time.Now()
	result, err := Evaluate(ctx, Config{Client: client, Workspace: ws, StateFile: state, Scope: scope, ContextLimit: 131072, MaxTokens: 8192})
	seconds := time.Since(started).Seconds()
	errorText := ""
	if err != nil {
		errorText = err.Error()
	}
	liveJSON(t, filepath.Join(output, "cost.json"), map[string]any{"scope": scope, "seconds": seconds, "requests": requests, "usage": usage, "proposals": len(result.Memory), "error": errorText, "source_sha256": fmt.Sprintf("%x", sha256.Sum256(data)), "approved": false})
	current, readErr := os.ReadFile(source)
	if readErr != nil || sha256.Sum256(current) != sha256.Sum256(data) {
		t.Fatal("source archive changed during cost measurement")
	}
	t.Logf("seconds=%.1f requests=%d proposals=%d prompt=%d generated=%d error=%s", seconds, requests, len(result.Memory), usage.PromptTokens, usage.CompletionTokens, errorText)
}
