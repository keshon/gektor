// Command eval scores agent runs against frozen workspace, tool and answer criteria.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	pathmatch "path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/capabilities/documents"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/script"
	"github.com/keshon/gektor/internal/workspace"
)

// Probe is one frozen scenario. It pairs with a human-readable write-up in
// eval/prompts/; Prompt points at that file so the two can't drift apart
// silently.
type Probe struct {
	Name string `json:"-"`

	// Prompt is the eval/prompts/*.md file this probe mechanizes.
	Prompt string `json:"prompt"`

	// Task is the instruction, exactly as a user would type it.
	Task string `json:"task"`

	// Tier selects how often a probe is worth running, assigned from
	// measured behaviour rather than taste:
	//
	//   smoke   never failed and its step count never varied. Cheap, and
	//           a failure here means something is badly broken.
	//   signal  varies across runs. This is where the information is, and
	//           where most of the wall clock goes.
	Tier string `json:"tier"`

	// Exclusive means this probe must not run alongside anything else —
	// it binds a fixed port or otherwise owns a machine-wide resource, so
	// a second copy would fail for reasons that say nothing about the
	// agent.
	Exclusive bool `json:"exclusive,omitempty"`

	// Seed is a directory copied in as the starting workspace, relative to
	// the repo root. Empty means start from an empty directory.
	Seed      string `json:"seed"`
	Generated string `json:"generated,omitempty"`
	// Attachments are generated fixture keys delivered outside the workspace,
	// like clipboard input. Their bytes still enter the input fingerprint.
	Attachments []string `json:"attachments,omitempty"`
	Git         bool     `json:"git,omitempty"`
	Replies     []string `json:"replies,omitempty"`
	// Continuation replays recorded assistant/tool turns after Task, then
	// resumes without inventing a new user message.
	Continuation []llm.Message `json:"continuation,omitempty"`

	// Verify is the workspace verdict. Every check must pass.
	Verify []Check `json:"verify"`

	// Trace is the tool-usage verdict. Every check must pass.
	Trace []TraceCheck `json:"trace"`

	// Answer asserts on the run's final text. Several probes are about
	// what got reported rather than what changed on disk — "reports
	// ~150000 bytes", "correct commit list in the final answer" — and a
	// read-only probe has no workspace change to assert on at all.
	Answer []AnswerCheck `json:"answer"`
	// AnswerSchema optionally validates the complete final answer as JSON.
	// It reuses the operation schema validator; no JSON is extracted from prose.
	AnswerSchema json.RawMessage `json:"answer_schema,omitempty"`

	// MaxSteps fails a run that took more round-trips than the probe
	// considers reasonable. Zero disables the limit. Finishing correctly
	// after twenty steps of thrashing is worth knowing about.
	MaxSteps int `json:"max_steps"`

	// TimeoutSec bounds one probe run. Zero takes the default.
	TimeoutSec int `json:"timeout_sec"`

	// Oses constrains the probe to host operating systems ("windows",
	// "linux", "darwin" as runtime.GOOS spells them). Empty means anywhere.
	// A probe whose checks only run on one OS (a findstr pipeline, a
	// cmd.exe builtin) reports SKIP elsewhere: scoring it would measure
	// the host, not the gektor - the same reason a run that never reached
	// the model reports ERR instead of a rate.
	Oses []string `json:"oses,omitempty"`

	// InjectOverflowOnce fails the run's first model call with a synthetic
	// context-overflow error, then passes through to the live backend. It
	// scores the recovery path in agent.run — compact and continue — which
	// no live backend produces on demand. Without recovery the run would
	// die on step 0 with a backend failure.
	InjectOverflowOnce bool `json:"inject_overflow_once,omitempty"`
}

// TraceCheck asserts that a tool was, or was not, used.
type TraceCheck struct {
	// Tool is an exact name or glob: start_* accepts either managed launcher.
	Tool        string `json:"tool"`
	Success     *bool  `json:"success,omitempty"`
	OutputRegex string `json:"output_regex,omitempty"`

	// Mode is "required", "forbidden", or empty when only MaxCalls
	// applies.
	Mode string `json:"mode"`

	// MaxCalls caps how many times the tool may be called; zero means no
	// cap. Several probes' fail conditions are about repetition rather
	// than presence — "blind patch_file that errors 3+ times without
	// strategy change", "more than 2 ask_user calls" — and a total step
	// budget can't express those without also punishing runs that took a
	// legitimately longer route.
	MaxCalls int `json:"max_calls,omitempty"`

	// ArgsRegex narrows the check to calls whose JSON arguments match —
	// run_shell is legitimate in general and forbidden for `ls`, so the
	// tool name alone can't express the rule.
	ArgsRegex string `json:"args_regex,omitempty"`

	// Why is quoted in the failure so a red result explains itself
	// without opening eval/prompts.
	Why string `json:"why,omitempty"`
}

// AnswerCheck asserts that the final answer does, or does not, match a
// pattern. Case-insensitive unless the pattern says otherwise.
type AnswerCheck struct {
	Regex string `json:"regex"`

	// Mode is "required" or "forbidden".
	Mode string `json:"mode"`

	Why string `json:"why,omitempty"`
}

// call is one tool invocation as it actually happened.
type call struct {
	id      string
	tool    string
	args    string
	success *bool
	output  string
}

// countingClient records how many times the backend was actually reached.
//
// Step counts cannot answer that question: they come from OnStep, which
// planning reports zero steps having made two model calls. Scoring that
// as "never ran" excludes a real failure from the rate — the inversion of
// the bug the Errored flag exists to prevent.
type countingClient struct {
	inner llm.Client
	calls int64

	// peakPrompt is the largest prompt any call sent, across every agent
	// and worker in the run. It is the one number that says whether a
	// result is about the model or about the context filling up, and
	// without it that question costs a trawl through the raw trace.
	peakPrompt int64

	// floorPrompt is the smallest, which on the first call of a run is
	// essentially the fixed cost: system prompt plus tool schemas plus a
	// one-line task. Measured because that floor turned out to be most of
	// a small prompt — roughly 1,700 tokens of the ~2,000 a simple probe
	// uses — so the pair says how much of a result is the work and how
	// much is boilerplate the harness sends on every call.
	floorPrompt                                  int64
	promptTokens, cachedTokens, completionTokens int64
	mu                                           sync.Mutex
	timings                                      []requestTiming
}

func (c *countingClient) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	started := time.Now()
	var first atomic.Int64
	progress := req.OnProgress
	req.OnProgress = func(p llm.Progress) {
		if p.CompletionTokens > 0 {
			first.CompareAndSwap(0, time.Since(started).Nanoseconds())
		}

		if progress != nil {
			progress(p)
		}
	}
	atomic.AddInt64(&c.calls, 1)
	resp, err := c.inner.Chat(ctx, req)
	timing := requestTiming{
		Seconds:           time.Since(started).Seconds(),
		FirstTokenSeconds: time.Duration(first.Load()).Seconds(),
		FinishReason:      resp.FinishReason,
		PromptTokens:      resp.Usage.PromptTokens,
		CompletionTokens:  resp.Usage.CompletionTokens,
	}
	if err != nil {
		timing.Error = firstLine(err.Error())
	}

	c.mu.Lock()
	c.timings = append(c.timings, timing)
	c.mu.Unlock()
	atomic.AddInt64(&c.promptTokens, int64(resp.Usage.PromptTokens))
	atomic.AddInt64(&c.cachedTokens, int64(resp.Usage.CachedTokens))
	atomic.AddInt64(&c.completionTokens, int64(resp.Usage.CompletionTokens))
	got := int64(resp.Usage.PromptTokens)

	for {
		peak := atomic.LoadInt64(&c.peakPrompt)
		if got <= peak || atomic.CompareAndSwapInt64(&c.peakPrompt, peak, got) {
			break
		}
	}

	for got > 0 {
		floor := atomic.LoadInt64(&c.floorPrompt)
		if (floor != 0 && got >= floor) || atomic.CompareAndSwapInt64(&c.floorPrompt, floor, got) {
			break
		}
	}

	return resp, err
}

func (c *countingClient) reached() bool { return atomic.LoadInt64(&c.calls) > 0 }

// overflowInjector fails its first Chat with a synthetic overflow, then
// delegates. Probe 18's way of scoring the recovery path: the injected
// error is indistinguishable from a backend 400-context-exceeded, so the
// run exercises the real compact-and-continue code in agent.run.
type overflowInjector struct {
	inner llm.Client
	fired int64
}

func (o *overflowInjector) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	if atomic.CompareAndSwapInt64(&o.fired, 0, 1) {
		return llm.ChatResponse{}, &llm.APIError{
			Status:     400,
			StatusText: "bad request",
			Body:       "exceeds the context window",
			Overflow:   true,
		}
	}

	return o.inner.Chat(ctx, req)
}

// gitInit turns the throwaway workspace into a committed repo for probe
// 19. Identity is passed per-command so the probe never depends on the
// operator's global git config.
func gitInit(dir string) error {
	for _, a := range [][]string{
		{"init"},
		{"add", "-A"},
		{"-c", "user.email=gektor-eval@localhost", "-c", "user.name=gektor-eval", "commit", "-qm", "seed"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, a...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w: %s", a[0], err, strings.TrimSpace(string(out)))
		}
	}

	return nil
}

type Result struct {
	Probe            string            `json:"probe"`
	Run              int               `json:"run"`
	Passed           bool              `json:"passed"`
	Steps            int               `json:"steps"`
	Seconds          float64           `json:"seconds"`
	ModelCalls       int64             `json:"model_calls"`
	PromptTokens     int64             `json:"prompt_tokens"`
	CachedTokens     int64             `json:"cached_tokens"`
	CompletionTokens int64             `json:"completion_tokens"`
	FirstToolSeconds float64           `json:"first_tool_seconds,omitempty"`
	RunSeconds       float64           `json:"run_seconds"`
	CheckSeconds     float64           `json:"check_seconds"`
	ArtifactPassed   *bool             `json:"artifact_passed,omitempty"`
	ArtifactFailures []string          `json:"artifact_failures,omitempty"`
	Requests         []requestTiming   `json:"requests,omitempty"`
	Warnings         []string          `json:"warnings,omitempty"`
	Summary          string            `json:"incomplete_summary,omitempty"`
	StartupContext   llm.ContextReport `json:"startup_context"`
	ToolSeconds      float64           `json:"tool_seconds"`
	ToolFailures     map[string]int    `json:"tool_failures,omitempty"`

	// Errored means the run never reached the model — a dead backend, a
	// bad seed path — so it says nothing about the agent and must not be
	// scored. Without this the first thing a stopped koboldcpp produces
	// is a table of red rows and failed checks, which reads exactly like
	// a model that got worse. An instrument that cannot tell "the agent
	// failed" from "nothing ran" is the same disease as a model grading
	// its own work.
	Errored bool `json:"errored,omitempty"`

	// Skipped means the probe does not run on this host OS (see Probe.Oses)
	// and was never attempted. Like Errored it is excluded from the rate:
	// a skip is the instrument declining to measure, not a verdict.
	Skipped bool `json:"skipped,omitempty"`

	// PeakPromptTokens is the largest prompt sent during the run, and
	// PeakContextPct that as a share of the window. A probe that failed at
	// 90% is a context story; the same failure at 5% is not.
	PeakPromptTokens int `json:"peak_prompt_tokens,omitempty"`
	PeakContextPct   int `json:"peak_context_pct,omitempty"`

	// FloorPromptTokens is the smallest prompt of the run, which is
	// essentially what the harness costs before any work: system prompt
	// plus tool schemas. Peak minus floor is the conversation.
	FloorPromptTokens int `json:"floor_prompt_tokens,omitempty"`

	// Tools counts calls by name — the shape of a run in one line,
	// without opening the trace.
	Tools map[string]int `json:"tools,omitempty"`

	// Nudges records every harness notice the run produced, in order.
	// A nudge rides a turn the loop was taking anyway, so it costs
	// tokens and not latency -- except the ones that bounce a finish or
	// force a retry, which cost a whole turn. Counting by kind is how we
	// find out which of those earn their keep on a given model instead
	// of arguing about it.
	Nudges []string `json:"nudges,omitempty"`

	RunError  string            `json:"run_error,omitempty"`
	Delegates []delegateMetrics `json:"delegates,omitempty"`
	Failures  []string          `json:"failures,omitempty"`
	Trace     string            `json:"trace_log,omitempty"`
}

const defaultTimeout = 15 * time.Minute

func main() {
	if len(os.Args) > 2 && os.Args[1] == "workflow" && os.Args[2] == "internal-document-worker" {
		if err := documents.Serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		return
	}

	if len(os.Args) > 2 && os.Args[1] == "workflow" && os.Args[2] == "internal-script-worker" {
		if err := script.Serve(os.Stdin, os.Stdout); err != nil {
			log.Fatal(err)
		}

		return
	}

	provider := config.DefaultProfile().Provider
	backend := flag.String("backend", provider.URL, "backend base URL: bare origin for a local "+
		"server or API root for a hosted OpenAI-compatible provider")
	model := flag.String("model", "local", "model name: ignored by most local servers, required for remote providers")
	dir := flag.String("probes", "eval/probes", "directory of probe .json files")
	only := flag.String("only", "", "run only probes whose name contains this substring")
	suite := flag.String("suite", "", "named subset: core, workflows, pipelines, visual, documents, delegation, memory, media or media-decisions")
	jobsN := flag.Int("jobs", 1, "how many probes to run at once. Both backends batch concurrent "+
		"requests (koboldcpp --parallelrequests, llama-server --parallel) and neither helps unless "+
		"the client sends them; raising this past what the server was started with just queues")
	tier := flag.String("tier", "", "run only probes in this tier: smoke (fast canary), signal "+
		"(where the information is). Empty runs every tier")
	runs := flag.Int("runs", 1, "runs per probe — a weak model is stochastic, so one pass "+
		"proves very little")
	outDir := flag.String("out", "eval/results", "directory for results.jsonl and per-run traces")
	maxTokens := flag.Int("max-tokens", 8192, "generation budget per response")
	deadlineAware := flag.Bool("deadline-aware", false, "reserve part of each probe timeout for a tool-free incomplete-work report")
	delegateSteps := flag.Int("delegate-steps", 0, "default model steps per delegate (default 12)")
	delegateMaxSteps := flag.Int("delegate-max-steps", 0, "maximum requested delegate steps (default equals delegate-steps)")
	dry := flag.Bool("dry", false, "load and validate every probe, then exit — checking a probe "+
		"should not cost a model round-trip")
	dry_ := flag.Bool("dry-sampler", false, "enable the DRY sampler — off by default because it "+
		"penalizes verbatim repetition, which patch_file requires. Here so the choice can be measured "+
		"rather than argued")
	force := flag.Bool("force", false, "start even if another eval appears to be running against "+
		"this backend — only correct when they use different backends")
	requireModel := flag.String("require-model", "", "refuse to run unless the model the backend "+
		"reports contains this substring — a local server loads whatever it was last started "+
		"with, and comparing a score against one from different weights is worse than having no score")
	backendKind := flag.String("backend-kind", provider.Kind, "which server: llama, openai or kobold "+
		"(any hosted OpenAI-compatible API)")
	apiKey := flag.String("api-key", "", "bearer token for a remote provider")
	apiKeyEnv := flag.String("api-key-env", "", "env var holding the bearer token")
	contextLimitFlag := flag.Int("context-limit", 0, "context window override; remote backends default to a per-model estimate")
	flag.Parse()
	delegateBudget := config.Harness{DelegateSteps: *delegateSteps, DelegateMaxSteps: *delegateMaxSteps}
	steps, maximum, budgetErr := delegateBudget.DelegateBudget()
	if budgetErr != nil {
		log.Fatal(budgetErr)
	}

	delegateBudget.DelegateSteps, delegateBudget.DelegateMaxSteps = steps, maximum
	if *runs < 1 || *jobsN < 1 {
		log.Fatal("-runs and -jobs must be positive")
	}

	if *dry {
		// Deliberately ignores -only: the point is to validate the whole
		// set before an overnight run, not the subset you were editing.
		probes, err := loadProbes(*dir, "")
		if err != nil {
			log.Fatalf("load probes: %v", err)
		}

		validated := len(probes)
		probes, err = selectSuite(probes, *suite)
		if err != nil {
			log.Fatal(err)
		}

		selected := keepTier(probes, *tier)

		for _, p := range selected {
			mode := "agent"
			fmt.Printf("  %-22s %-7s %-7s %d verify, %d trace, %d answer, JSON schema=%t\n",
				p.Name, p.Tier, mode, len(p.Verify), len(p.Trace), len(p.Answer), len(p.AnswerSchema) > 0)
		}

		fmt.Printf("%d probes OK; %d selected\n", validated, len(selected))

		return
	}

	probes, err := loadProbes(*dir, "")
	if err != nil {
		log.Fatalf("load probes: %v", err)
	}

	probes, err = selectSuite(probes, *suite)
	if err != nil {
		log.Fatal(err)
	}

	if *only != "" {
		selected := []Probe{}

		for _, p := range probes {
			if strings.Contains(p.Name, *only) {
				selected = append(selected, p)
			}
		}

		probes = selected
	}

	probes = keepTier(probes, *tier)
	if len(probes) == 0 {
		log.Fatalf("no probes matched in %s", *dir)
	}

	release, err := takeLock(*outDir, *force)
	if err != nil {
		log.Fatalf("%v", err)
	}

	defer release()

	runDir := filepath.Join(*outDir, time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		log.Fatalf("create results dir: %v", err)
	}

	resultsPath := filepath.Join(runDir, "results.jsonl")
	results, err := os.Create(resultsPath)
	if err != nil {
		log.Fatalf("create results file: %v", err)
	}

	defer results.Close()

	// A pass rate without the model that produced it is worse than no
	// number — comparing today's score against one from a different
	// quantization is the exact mistake this tool exists to prevent.
	probe := newBackend(*backendKind, *backend, *model, *apiKey, *apiKeyEnv)
	ctx := context.Background()
	facts, err := probe.Inspect(ctx, *backend, *contextLimitFlag, true)
	if err != nil {
		log.Fatal(err)
	}

	contextLimit, backendModel := facts.ContextLimit, facts.Model
	if !facts.ModelKnown {
		backendModel = "(unknown)"
	}

	// Live on 2026-09-05: koboldcpp was restarted and came back holding
	// different weights, so a 4/8 looked like a refactor regression next
	// to the previous day's 8/8. Nothing in the run said otherwise until
	// someone read the model line.
	if *requireModel != "" && !strings.Contains(backendModel, *requireModel) {
		log.Fatalf("backend is serving %q, which does not contain %q — "+
			"load the intended model or drop -require-model", backendModel, *requireModel)
	}

	fmt.Printf("backend %s (%s)\nmodel   %s\ncontext %d\n%d probes x %d runs\n\n",
		*backend, probe.Backend(), backendModel, contextLimit, len(probes), *runs)
	if err := writeMeta(runDir, evalMetadata{
		BackendKind:      probe.Backend(),
		Backend:          *backend,
		Model:            backendModel,
		ContextLimit:     contextLimit,
		Jobs:             *jobsN,
		MaxTokens:        *maxTokens,
		Runs:             *runs,
		DrySampler:       *dry_,
		DeadlineAware:    *deadlineAware,
		Suite:            *suite,
		DelegateSteps:    steps,
		DelegateMaxSteps: maximum,
	}, probes); err != nil {
		log.Fatalf("record experiment metadata: %v", err)
	}

	// Every (probe, run) pair is one job. Both backends batch concurrent
	// requests — koboldcpp with --parallelrequests, llama-server with
	// --parallel — and neither helps while the client sends one request
	// at a time, which is what this loop used to do.

	var (
		mu  sync.Mutex
		all []Result
	)
	record := func(r Result) {
		mu.Lock()
		defer mu.Unlock()

		all = append(all, r)

		writeProbeResult(results, r)
	}

	// minutes, and printing only on completion means a working run looks
	// identical to a hung one — results.jsonl stays empty either way.
	run1 := func(j evalJob) {
		mu.Lock()
		fmt.Printf("  ....  %-22s run %d  started\n", j.probe.Name, j.run)
		mu.Unlock()
		record(runOnce(ctx, j.probe, j.run, runDir, *backendKind, *backend, *model,
			contextLimit, *maxTokens, *dry_, *apiKey, *apiKeyEnv, *deadlineAware, delegateBudget))
	}

	runProbeJobs(probes, *runs, *jobsN, run1)

	// Concurrent completion order is not run order.
	sort.Slice(all, func(i, k int) bool {
		if all[i].Probe != all[k].Probe {
			return all[i].Probe < all[k].Probe
		}

		return all[i].Run < all[k].Run
	})

	fmt.Printf("\n%s\n\nresults: %s\n", summarize(all), resultsPath)
	if err := writeSummary(runDir, all); err != nil {
		log.Fatalf("write summary: %v", err)
	}

	stats := aggregateRuns(all)
	fmt.Printf("median %.1fs / %.1f steps; tool failures %d; tokens prompt=%d cached=%d completion=%d\nsummary: %s\n", stats.MedianSeconds, stats.MedianSteps, stats.ToolFailures, stats.PromptTokens, stats.CachedTokens, stats.CompletionTokens, filepath.Join(runDir, "summary.json"))
}

// The return value is named so the deferred timing write lands in what
// the caller receives: with an unnamed result, `return res` copies before
// the defer runs and every duration is reported as zero.
// keepTier filters by tier; an empty tier keeps everything.
// newBackend picks a dialect. Unknown names fail loudly rather than
// silently defaulting: a run against the wrong backend is the same class
// of wasted measurement as a run against the wrong model.
func newBackend(kind, baseURL, model, apiKey, apiKeyEnv string) *app.Backend {
	c, err := app.NewBackend(context.Background(), app.BackendOptions{
		Kind:      kind,
		URL:       baseURL,
		Model:     model,
		APIKey:    apiKey,
		APIKeyEnv: apiKeyEnv,
	})
	if err != nil {
		log.Fatalf("%v", err)
	}

	return c
}

func keepTier(probes []Probe, tier string) []Probe {
	if tier == "" {
		return probes
	}

	kept := make([]Probe, 0, len(probes))

	for _, p := range probes {
		if p.Tier == tier {
			kept = append(kept, p)
		}
	}

	return kept
}

// osSkipReason reports whether a probe with Oses declines this host, and
// why. Pure so the gate is unit-testable without a backend.
func osSkipReason(oses []string) (bool, string) {
	if len(oses) > 0 && !slices.Contains(oses, runtime.GOOS) {
		return true, fmt.Sprintf("probe needs %s, host is %s", strings.Join(oses, "/"), runtime.GOOS)
	}

	return false, ""
}

func runOnce(ctx context.Context, p Probe, run int, runDir, backendKind, backend, model string,
	contextLimit, maxTokens int, drySampler bool, apiKey, apiKeyEnv string, deadlineAware bool, budgets ...config.Harness) (res Result) {

	res = Result{
		Probe:   p.Name,
		Run:     run,
		Errored: true,
	}
	started := time.Now()
	defer func() { res.Seconds = time.Since(started).Seconds() }()

	// OS-gated probes decline before touching anything: no temp dir, no
	// backend call, no trace file. A skip is recorded, not scored.
	if skip, reason := osSkipReason(p.Oses); skip {
		res.Skipped = true
		res.Errored = false
		res.RunError = reason

		return res
	}

	// A fresh copy per run. Without it the second run starts from the
	// first run's output and measures nothing.
	tempRoot, err := os.MkdirTemp("", "gektor-eval-")
	if err != nil {
		res.RunError = "mkdir temp: " + err.Error()
		return res
	}

	defer os.RemoveAll(tempRoot)

	work := filepath.Join(tempRoot, "workspace")
	if err := os.Mkdir(work, 0700); err != nil {
		res.RunError = "mkdir workspace: " + err.Error()
		return res
	}

	if err := seedProbeWorkspace(p, work); err != nil {
		res.RunError = err.Error()
		return res
	}

	ws, err := workspace.New(work)
	if err != nil {
		res.RunError = "workspace: " + err.Error()
		return res
	}

	defer ws.Close()

	kobold := newBackend(backendKind, backend, model, apiKey, apiKeyEnv)
	kobold.SetDRY(drySampler)
	client := kobold
	var images []string
	if len(p.Attachments) > 0 {
		vision, err := kobold.SupportsVision(ctx)
		if err != nil || !vision {
			res.Skipped = true
			res.Errored = false
			res.RunError = "probe requires confirmed image support"

			return res
		}

		images, err = prepareProbeAttachments(p, work, tempRoot)
		if err != nil {
			res.RunError = err.Error()
			return res
		}
	}

	// Every model call, verbatim. When a probe regresses this is the only
	// thing that says why — reading traces is how the three harness bugs
	// fixed on 2026-09-04 were found.
	tracePath := filepath.Join(runDir, fmt.Sprintf("%s-run%d.log", p.Name, run))
	if tf, err := os.Create(tracePath); err == nil {
		defer tf.Close()

		kobold.SetDebug(tf)
		res.Trace = tracePath
	}

	counter := &countingClient{inner: client}

	// Probe 18 machinery: one synthetic overflow on the first model call,
	// then the live backend. Wrapping outside the counter keeps
	// counter.reached() honest — the backend is still reached.
	var modelClient llm.Client = counter
	if p.InjectOverflowOnce {
		modelClient = &overflowInjector{inner: counter}
	}

	// Probe 19 machinery: the throwaway workspace becomes a git repo so
	if p.Git {
		if err := gitInit(work); err != nil {
			res.RunError = "git init: " + err.Error()
			return res
		}
	}

	timeout := time.Duration(p.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	procs := tools.NewBackgroundProcesses()
	// A probe that leaves a dev server running holds its temp workspace
	// open, so the next run inherits a directory that won't delete and a
	// port that's taken. Both look like model failures and are neither.
	defer procs.StopAll()

	var calls []call
	record := func(step int, msg llm.Message) {
		if step+1 > res.Steps {
			res.Steps = step + 1
		}

		for _, tc := range msg.ToolCalls {
			calls = append(calls, call{
				id:   tc.ID,
				tool: tc.Name,
				args: string(tc.Arguments),
			})
		}
	}

	var answer string
	env := evalEnv(modelClient, ws, procs, contextLimit, maxTokens, record, func(kind string) {
		res.Nudges = append(res.Nudges, kind)
	})
	env.WorkflowDir = workflowStoreDir(ws)
	if len(budgets) > 0 {
		env.DelegateSteps, env.DelegateMaxSteps = budgets[0].DelegateSteps, budgets[0].DelegateMaxSteps
	}

	env.Vision = len(images) > 0
	env = observeProbeResults(env, &res, &calls, started)
	if deadlineAware {
		env.RunTimeout = timeout
	}

	answer = executeProbeAgent(runCtx, p, env, images, &res)
	res.Requests = counter.timings
	res.Errored = !counter.reached() && !p.InjectOverflowOnce

	// The backend was never reached, so the workspace checks below would
	// only be measuring the seed. Report it as an error rather than
	// scoring it.
	//
	// Probes that inject their own first-call failure are exempt: with
	// recovery broken, probe 18 dies on step 0 having never reached the
	// backend, and ERR would misreport a harness regression as "check the
	// backend". Scored, it fails honestly on its trace and answer checks.
	if !counter.reached() && res.RunError != "" && !p.InjectOverflowOnce {
		res.Errored = true
		return res
	}

	// The agent ran but did not finish — it hit MaxSteps, or the backend
	// died mid-run. The workspace may still satisfy every check by luck,
	// which is how probe 06 scored green on a run whose own error said
	// "reached max steps (25) without finishing". A run the harness had
	// to cut off is not a pass.
	if res.RunError != "" {
		res.Failures = append(res.Failures, "run did not complete: "+res.RunError)
	}

	checkProbeOutcome(ctx, p, ws, images, calls, answer, &res)

	recordProbeUsage(&res, counter, contextLimit, calls)
	if p.MaxSteps > 0 && res.Steps > p.MaxSteps {
		res.Failures = append(res.Failures,
			fmt.Sprintf("took %d steps, probe allows %d", res.Steps, p.MaxSteps))
	}

	res.Passed = len(res.Failures) == 0

	return res
}

// evalEnv adapts the harness's step recorder, which ignores labels
// because a probe scores one agent at a time, to the labelled callback
// roles.Env hands out.
//
// nudge receives the kind of every harness notice the run produced. It
// is measurement, not control: the count is what tells us whether a
// nudge earns the step it costs, per model, instead of us guessing which
// ones to keep.
func evalEnv(client llm.Client, ws *workspace.Workspace, procs *tools.BackgroundProcesses,
	contextLimit, maxTokens int, record func(int, llm.Message), nudge func(kind string)) roles.Env {

	return roles.Env{
		Client:       client,
		WS:           ws,
		Procs:        procs,
		MaxTokens:    maxTokens,
		ContextLimit: contextLimit,
		OnStep:       func(_ string, step int, msg llm.Message) { record(step, msg) },
		OnNudge:      func(kind, _ string) { nudge(kind) },
	}
}

func checkWorkspace(ctx context.Context, p Probe, ws *workspace.Workspace) []string {
	var out []string

	for _, c := range p.Verify {
		if msg, ok := RunCheck(ctx, c, ws); !ok {
			out = append(out, firstLine(msg))
		}
	}

	return out
}

// Providers can reuse tool IDs across turns. Match the newest pending
// occurrence so later success never rewrites an earlier failure.
func recordToolResult(calls []call, result agent.ToolResult) {
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].id == result.CallID && calls[i].success == nil {
			ok := !result.Failed
			calls[i].success = &ok
			calls[i].output = result.Output

			return
		}
	}
}

func checkTrace(p Probe, calls []call) []string {
	var out []string

	for _, tc := range p.Trace {
		var resultRE *regexp.Regexp
		if tc.OutputRegex != "" {
			var err error
			resultRE, err = regexp.Compile(tc.OutputRegex)
			if err != nil {
				out = append(out, fmt.Sprintf("bad output_regex: %v", err))
				continue
			}
		}

		var re *regexp.Regexp
		if tc.ArgsRegex != "" {
			var err error
			if re, err = regexp.Compile(tc.ArgsRegex); err != nil {
				out = append(out, fmt.Sprintf("bad args_regex %q: %v", tc.ArgsRegex, err))
				continue
			}
		}

		matched := 0

		for _, c := range calls {
			if ok, _ := pathmatch.Match(tc.Tool, c.tool); !ok {
				continue
			}

			if tc.Success != nil && (c.success == nil || *c.success != *tc.Success) {
				continue
			}

			if resultRE != nil && !resultRE.MatchString(c.output) {
				continue
			}

			if re != nil && !re.MatchString(c.args) {
				continue
			}

			matched++
		}

		if tc.MaxCalls > 0 && matched > tc.MaxCalls {
			out = append(out, describe(tc,
				fmt.Sprintf("called %dx, probe allows %d", matched, tc.MaxCalls)))
		}

		switch tc.Mode {
		case "required":
			if matched == 0 {
				out = append(out, describe(tc, "no call matched the required arguments, result and success criteria"))
			}
		case "forbidden":
			if matched > 0 {
				out = append(out, describe(tc, fmt.Sprintf("called %dx", matched)))
			}
		case "":
			// MaxCalls-only check; presence is not asserted either way.
		default:
			out = append(out, fmt.Sprintf("probe %s: unknown trace mode %q", p.Name, tc.Mode))
		}
	}

	return out
}

func checkAnswer(p Probe, answer string) []string {
	var out []string
	if len(p.AnswerSchema) > 0 {
		if err := validateAnswerJSON(answer); err != nil {
			out = append(out, "answer JSON: "+err.Error())
		} else if err := workflow.ValidateJSON(json.RawMessage(answer), p.AnswerSchema); err != nil {
			out = append(out, "answer schema: "+err.Error())
		}
	}

	for _, ac := range p.Answer {
		// Case-insensitive by default: these assert on prose a model
		// wrote, and "150000 Bytes" is the same answer as "150000 bytes".
		re, err := regexp.Compile("(?i)" + ac.Regex)
		if err != nil {
			out = append(out, fmt.Sprintf("bad answer regex %q: %v", ac.Regex, err))
			continue
		}

		hit := re.MatchString(answer)

		switch {
		case ac.Mode == "required" && !hit:
			out = append(out, explain("answer does not match "+ac.Regex, ac.Why))
		case ac.Mode == "forbidden" && hit:
			out = append(out, explain("answer matches "+ac.Regex, ac.Why))
		case ac.Mode != "required" && ac.Mode != "forbidden":
			out = append(out, fmt.Sprintf("probe %s: unknown answer mode %q", p.Name, ac.Mode))
		}
	}

	return out
}

func explain(what, why string) string {
	if why != "" {
		return what + " — " + why
	}

	return what
}

func describe(tc TraceCheck, what string) string {
	label := tc.Tool
	if tc.ArgsRegex != "" {
		label += " matching " + tc.ArgsRegex
	}

	if tc.Why != "" {
		return fmt.Sprintf("%s %s — %s", label, what, tc.Why)
	}

	return fmt.Sprintf("%s %s", label, what)
}

// summarize reports per-probe rates as well as the total. The per-probe
// numbers matter more: a change that fixes one shape of task and breaks
// another shows up there and nowhere else.
func summarize(all []Result) string {
	type tally struct{ pass, total int }
	byProbe := map[string]*tally{}
	var names []string
	overall := tally{}

	errored := 0
	skipped := 0

	for _, r := range all {
		// A run that never reached the model is not evidence either way.
		// Counting it as a failure would mean a stopped backend silently
		// reports the agent as broken.
		if r.Errored {
			errored++
			continue
		}

		// A skipped OS-gated probe was never attempted: same exclusion.
		if r.Skipped {
			skipped++
			continue
		}

		t, seen := byProbe[r.Probe]
		if !seen {
			t = &tally{}
			byProbe[r.Probe] = t
			names = append(names, r.Probe)
		}

		t.total++
		overall.total++
		if r.Passed {
			t.pass++
			overall.pass++
		}
	}

	sort.Strings(names)

	if overall.total == 0 {
		return fmt.Sprintf("=== no scored runs ===\n  %d run(s) never reached the model — "+
			"check the backend is up, then re-run. No pass rate is reported because "+
			"there is nothing to report.", errored)
	}

	var b strings.Builder
	b.WriteString("=== pass rate ===\n")

	for _, n := range names {
		t := byProbe[n]
		fmt.Fprintf(&b, "  %-22s %d/%d  %3.0f%%\n", n, t.pass, t.total,
			100*float64(t.pass)/float64(t.total))
	}

	fmt.Fprintf(&b, "  %-22s %d/%d  %3.0f%%", "TOTAL", overall.pass, overall.total,
		100*float64(overall.pass)/float64(overall.total))
	if errored > 0 {
		fmt.Fprintf(&b, "\n\n  %d run(s) never reached the model and are excluded — "+
			"this rate covers %d of %d attempted.", errored, overall.total, overall.total+errored)
	}

	if skipped > 0 {
		fmt.Fprintf(&b, "\n\n  %d run(s) skipped (host OS outside the probe's oses) and are excluded.",
			skipped)
	}

	if line := summarizeNudges(all); line != "" {
		b.WriteString("\n\n")
		b.WriteString(line)
	}

	return b.String()
}

// summarizeNudges reports what the harness said to the model, per kind,
// split by whether the run that heard it passed.
//
// This exists to settle an argument with numbers. Nudges are not a
// uniform cost: most ride a turn the loop was taking anyway and cost
// only tokens, while a finish bounce, a wrap-up round or an overflow
// retry costs a whole turn. Seeing which kinds land on passing runs and
// which land on failing ones is the input to deciding what to keep, per
// model, instead of shipping one default tuned for weak local LLMs and
// paying for it on strong ones.
//
// Reads as "which nudges correlate with recovery". Correlation on a few
// dozen runs is a lead, not a verdict.
func summarizeNudges(all []Result) string {
	type stat struct{ total, onPass, onFail int }
	kinds := map[string]*stat{}
	scored := 0

	for _, r := range all {
		if r.Errored || r.Skipped {
			continue
		}

		scored++

		for _, k := range r.Nudges {
			s, ok := kinds[k]
			if !ok {
				s = &stat{}
				kinds[k] = s
			}

			s.total++
			if r.Passed {
				s.onPass++
			} else {
				s.onFail++
			}
		}
	}

	if len(kinds) == 0 {
		return ""
	}

	names := make([]string, 0, len(kinds))

	for k := range kinds {
		names = append(names, k)
	}

	sort.Slice(names, func(i, j int) bool { return kinds[names[i]].total > kinds[names[j]].total })

	var b strings.Builder
	fmt.Fprintf(&b, "=== harness nudges (%d scored run(s)) ===\n", scored)
	fmt.Fprintf(&b, "  %-18s %6s %8s %8s\n", "kind", "heard", "passed", "failed")

	for _, k := range names {
		s := kinds[k]
		fmt.Fprintf(&b, "  %-18s %6d %8d %8d\n", k, s.total, s.onPass, s.onFail)
	}

	return b.String()
}

func loadProbes(dir, only string) ([]Probe, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var out []Probe

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		name := strings.TrimSuffix(e.Name(), ".json")
		if only != "" && !strings.Contains(name, only) {
			continue
		}

		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}

		var p Probe
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}

		if strings.TrimSpace(p.Task) == "" {
			return nil, fmt.Errorf("%s: task is empty", path)
		}

		for _, message := range p.Continuation {
			if message.Role != llm.RoleAssistant && message.Role != llm.RoleTool {
				return nil, fmt.Errorf("%s: continuation accepts assistant and tool messages only", path)
			}
		}

		// A probe with nothing to assert passes unconditionally, which is
		// worse than not having the probe at all.
		if len(p.Verify) == 0 && len(p.Trace) == 0 && len(p.Answer) == 0 && len(p.AnswerSchema) == 0 && p.MaxSteps == 0 {
			return nil, fmt.Errorf("%s: nothing asserted — it would score itself", path)
		}

		switch p.Tier {
		case "smoke", "signal":
		default:
			return nil, fmt.Errorf("%s: tier is %q, want smoke or signal", path, p.Tier)
		}

		for _, goos := range p.Oses {
			switch goos {
			case "windows", "linux", "darwin":
			default:
				return nil, fmt.Errorf("%s: oses has %q, want windows, linux or darwin (runtime.GOOS)", path, goos)
			}
		}

		// Selftest before spend: unknown check types, missing check
		// fields, and uncompilable regexes fail here, dry, instead of
		// mid-run after model calls.
		if len(p.AnswerSchema) > 0 {
			if _, err := workflow.CompileSchema(p.AnswerSchema); err != nil {
				return nil, fmt.Errorf("%s: answer_schema: %w", path, err)
			}
		}

		if p.Generated != "" {
			if _, err := generatedFixture(p.Generated); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
		}

		if len(p.Attachments) > 0 {
			files, err := generatedFixture(p.Generated)
			if err != nil {
				return nil, err
			}

			seen := map[string]bool{}

			for _, name := range p.Attachments {
				if _, ok := files[name]; !ok || seen[name] {
					return nil, fmt.Errorf("%s: unknown or duplicate generated attachment %q", path, name)
				}

				seen[name] = true
			}
		}

		for i, c := range p.Verify {
			if err := ValidateCheck(c); err != nil {
				return nil, fmt.Errorf("%s: verify[%d]: %w", path, i, err)
			}
		}

		for _, tc := range p.Trace {
			if tc.OutputRegex != "" {
				if _, err := regexp.Compile(tc.OutputRegex); err != nil {
					return nil, fmt.Errorf("%s: bad output_regex: %w", path, err)
				}
			}

			if tc.Tool == "" {
				return nil, fmt.Errorf("%s: trace tool is empty", path)
			}

			if _, err := pathmatch.Match(tc.Tool, ""); err != nil {
				return nil, fmt.Errorf("%s: bad tool pattern: %w", path, err)
			}

			if tc.ArgsRegex != "" {
				if _, err := regexp.Compile(tc.ArgsRegex); err != nil {
					return nil, fmt.Errorf("%s: trace %q: bad args_regex %q: %v", path, tc.Tool, tc.ArgsRegex, err)
				}
			}
		}

		for _, ac := range p.Answer {
			if _, err := regexp.Compile("(?i)" + ac.Regex); err != nil {
				return nil, fmt.Errorf("%s: bad answer regex %q: %v", path, ac.Regex, err)
			}
		}

		p.Name = name
		out = append(out, p)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out, nil
}

// copyTree copies src into dst, creating dst if needed.
func copyTree(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return fmt.Errorf("seed %s does not exist", src)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}

		defer in.Close()

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		out, err := os.Create(target)
		if err != nil {
			return err
		}

		defer out.Close()

		_, err = io.Copy(out, in)

		return err
	})
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}

	return strings.TrimSpace(s)
}

func seedProbeWorkspace(p Probe, work string) error {
	if p.Seed != "" {
		if err := copyTree(p.Seed, work); err != nil {
			return fmt.Errorf("seed workspace: %w", err)
		}
	}

	if p.Generated != "" {
		files, err := generatedFixture(p.Generated)
		if err != nil {
			return err
		}

		for path, data := range files {
			target := filepath.Join(work, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}

			if err := os.WriteFile(target, data, 0600); err != nil {
				return err
			}
		}
	}

	return nil
}

func recordProbeUsage(res *Result, counter *countingClient, contextLimit int, calls []call) {
	res.PeakPromptTokens = int(atomic.LoadInt64(&counter.peakPrompt))
	res.ModelCalls = atomic.LoadInt64(&counter.calls)
	res.PromptTokens = atomic.LoadInt64(&counter.promptTokens)
	res.CachedTokens = atomic.LoadInt64(&counter.cachedTokens)
	res.CompletionTokens = atomic.LoadInt64(&counter.completionTokens)
	res.FloorPromptTokens = int(atomic.LoadInt64(&counter.floorPrompt))
	if contextLimit > 0 {
		res.PeakContextPct = res.PeakPromptTokens * 100 / contextLimit
	}

	if len(calls) > 0 {
		res.Tools = map[string]int{}

		for _, c := range calls {
			res.Tools[c.tool]++
		}
	}
}

func writeProbeResult(results io.Writer, r Result) {
	line, _ := json.Marshal(r)
	fmt.Fprintln(results, string(line))

	status := "PASS"

	switch {
	case r.Skipped:
		status = "SKIP"
	case r.Errored:
		status = "ERR "
	case !r.Passed:
		status = "FAIL"
	}

	fmt.Printf("  %s  %-22s run %d  %6.1fs  %2d steps  ctx %2d%%",
		status, r.Probe, r.Run, r.Seconds, r.Steps, r.PeakContextPct)
	if len(r.Failures) > 0 {
		fmt.Printf("  %s", r.Failures[0])
	} else if r.RunError != "" {
		fmt.Printf("  (%s)", r.RunError)
	}

	fmt.Println()
}

type evalJob struct {
	probe Probe
	run   int
}

func runProbeJobs(probes []Probe, runs, workers int, run func(evalJob)) {
	// Exclusive probes are only special when something else could run
	// beside them. At one job everything is sequential already, and
	// hoisting them to the front just scrambles the reading order.
	concurrent := max(workers, 1) > 1
	var jobs, exclusive []evalJob

	for _, p := range probes {
		for run := 1; run <= runs; run++ {
			if p.Exclusive && concurrent {
				exclusive = append(exclusive, evalJob{p, run})
			} else {
				jobs = append(jobs, evalJob{p, run})
			}
		}
	}

	// Exclusive probes first and alone: one that binds a fixed port
	// cannot share the machine with another copy of itself.
	for _, j := range exclusive {
		run(j)
	}

	sem := make(chan struct{}, max(workers, 1))
	var wg sync.WaitGroup

	for _, j := range jobs {
		if !concurrent {
			run(j)
			continue
		}

		wg.Add(1)
		go func(j evalJob) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			run(j)
		}(j)
	}

	wg.Wait()

}
