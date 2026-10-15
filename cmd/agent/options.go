package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/speech"
)

// launchOptions holds command-line values and records explicit overrides.
// Parsing does not open files, create clients, or start services.
type launchOptions struct {
	backendURL            string
	model                 string
	workspace             string
	debug                 bool
	grammar               bool
	maxTokens             int
	maxSteps              int
	thinkBudget           int
	resume                string
	answer                string
	verifyCmd             string
	logMax                int
	missionMode           bool
	direct                bool
	yes                   bool
	auditPath             string
	backendKind           string
	apiKey                string
	apiKeyEnv             string
	contextLimit          int
	allow                 string
	deny                  string
	pure                  bool
	stream                bool
	mcp                   string
	fork                  string
	revert                bool
	revertPreview         bool
	revertFrom            string
	plan                  bool
	outputMode            string
	serve                 bool
	tui                   bool
	noVerify              bool
	images                string
	profilePath           string
	checkProfile          bool
	chat                  bool
	systemPrompt          string
	chatDir               string
	characterPath         string
	userName              string
	postHistory           string
	preferCharacterPrompt bool
	preferCharacterPost   bool
	temperature           float64
	voice                 bool
	conversationMode      string
	explicitFlags         map[string]bool
	explicitNames         []string
	task                  string
}

func parseLaunchOptions(args []string) (launchOptions, error) {
	var o launchOptions
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	flags.StringVar(&o.backendURL, "backend", "http://localhost:5001", "backend base URL: bare origin for a local server ("+
		"http://localhost:5001) or API root for a hosted OpenAI-compatible provider "+
		"(https://api.openai.com/v1, https://openrouter.ai/api/v1)")
	flags.StringVar(&o.model, "model", "local", "model name: ignored by most local servers (they serve "+
		"whatever weights they started with), required for remote providers")
	flags.StringVar(&o.workspace, "workspace", ".", "workspace root the agent may read/write")
	flags.BoolVar(&o.debug, "debug", false, "log raw request/response JSON to agent-debug.log")
	flags.BoolVar(&o.grammar, "grammar", true, "apply the backend's own content grammar. On koboldcpp "+
		"that blocks a model writing its native tool-call tags as plain text; on llama-server there "+
		"is none, because those tags are the protocol there and constraining them fights the "+
		"grammar the server builds from the tool schemas")
	flags.IntVar(&o.maxTokens, "max-tokens", 8192, "generation budget per response — too low truncates "+
		"large outputs (e.g. a full HTML+CSS+JS file) mid-JSON")
	flags.IntVar(&o.maxSteps, "max-steps", 0, "step budget per run — 0 takes the agent default (25). "+
		"Applies to direct runs; mission workers and subagents keep fixed budgets")
	flags.IntVar(&o.thinkBudget, "reasoning-budget", 0, "characters of <think> deliberation allowed per response "+
		"before a wrap-up round demands commitment; 0 takes the agent default (6000), negative disables wrapping")
	flags.StringVar(&o.resume, "resume", "", "path to a .tars/tasks/.../state.json snapshot to resume "+
		"an interrupted run from, instead of starting a new task")
	flags.StringVar(&o.answer, "answer", "", "answer to supply when resuming a run paused on ask_user "+
		"(use with -resume when the agent stopped to ask a clarifying question)")
	flags.StringVar(&o.verifyCmd, "verify-cmd", "", "command to run at the self-check checkpoint "+
		"(e.g. \"go build ./... && go vet ./... && go test ./...\") — real output gets fed back "+
		"as fact instead of trusting the model's own claim that the code works. Empty disables this.")
	flags.IntVar(&o.logMax, "log-max", 300, "max characters per line in step console output; "+
		"long text is truncated in the middle (head ... tail). 0 = no limit")
	flags.BoolVar(&o.missionMode, "mission", false, "run the task as a mission: an upfront model-generated "+
		"plan (approved by you), then one fresh-context worker per subtask, each verified mechanically. "+
		"For complex multi-file tasks a weak model can't hold in its head; simple tasks are better off "+
		"without it. Multi-file tasks receive a recommendation; mission mode is explicit")
	flags.BoolVar(&o.direct, "direct", false, "suppress the multi-file mission recommendation (direct mode is the default)")
	flags.BoolVar(&o.yes, "yes", false, "skip the mission plan approval gate and run the plan as generated")
	flags.StringVar(&o.auditPath, "audit", "", "append gate decisions as JSONL to this file (off when empty)")
	flags.StringVar(&o.backendKind, "backend-kind", "kobold", "which server: "+
		strings.Join(llm.Kinds(), ", ")+". kobold/llama are local; openai is any hosted "+
		"OpenAI-compatible API (OpenAI, OpenRouter, DeepSeek, Groq, Together, Mistral, xAI). "+
		"A wrong value is caught at startup rather "+
		"than run: the dialects disagree about grammar, sampler fields and structured "+
		"output, so a mismatched run completes and measures nothing")
	flags.StringVar(&o.apiKey, "api-key", "", "bearer token for a remote provider; prefer the "+
		"TARS_API_KEY / OPENAI_API_KEY / OPENROUTER_API_KEY env vars so the secret never lands in shell history")
	flags.StringVar(&o.apiKeyEnv, "api-key-env", "", "name of the env var holding the bearer token, "+
		"e.g. OPENROUTER_API_KEY — overrides the built-in lookup order")
	flags.IntVar(&o.contextLimit, "context-limit", 0, "context window in tokens, overriding the backend probe. "+
		"Required knowledge for -backend-kind openai, which has no probe endpoint: defaults to a "+
		"per-model estimate when 0")
	flags.StringVar(&o.allow, "allow", "", "comma-separated tool=pattern rules to allow, e.g. \"run_shell=go *,read_file=*.go\" (wins over defaults)")
	flags.StringVar(&o.deny, "deny", "", "comma-separated tool=pattern rules to deny, e.g. \"run_shell=rm *,read_file=.env\" (wins over -allow)")
	flags.BoolVar(&o.pure, "pure", false, "ignore project config for permissions; use built-in defaults plus -allow/-deny only")
	flags.BoolVar(&o.stream, "stream", false, "stream response tokens live (openai/llama backends only; koboldcpp falls back to unary)")
	flags.StringVar(&o.mcp, "mcp", "", "MCP servers: \"name=cmd args...;name2=https://host/mcp\" (stdio JSON-RPC or Streamable HTTP, tools appear as mcp__name__tool)")
	flags.StringVar(&o.fork, "fork", "", "history file to branch from: loads its transcript but writes to a fresh task id")
	flags.BoolVar(&o.revert, "revert", false, "restore the pre-run workspace checkpoint and Git index, then exit")
	flags.BoolVar(&o.revertPreview, "revert-preview", false, "list checkpoint restore actions without changing files")
	flags.StringVar(&o.revertFrom, "revert-from", "", "select a checkpoint archive for -revert or -revert-preview; default is the latest")
	flags.BoolVar(&o.plan, "plan", false, "plan mode: read-only tools, propose a plan and change nothing")
	flags.StringVar(&o.outputMode, "mode", "print", "output mode: print (human-readable) or json (one JSON object per line)")
	flags.BoolVar(&o.serve, "serve", false, "serve JSON-RPC over stdio instead of running one task: methods run/respond/cancel, events on stdout. See docs/rpc.md")
	flags.BoolVar(&o.tui, "tui", false, "fullscreen terminal UI instead of print mode: live transcript, status, and gate prompts")
	flags.BoolVar(&o.noVerify, "no-verify", false, "skip the self-check verify round (finish accepted without the extra verification turn). Saves 1-2 model calls; strong models only")
	flags.StringVar(&o.images, "image", "", "attach pictures to the task (comma-separated paths): the model sees them alongside the text. Needs a vision-capable backend (llama-server with --mmproj); koboldcpp refuses loudly")
	flags.StringVar(&o.profilePath, "profile", "", "path to a versioned JSON provider and generation profile; explicit flags override it")
	flags.BoolVar(&o.checkProfile, "check-profile", false, "validate and print a profile without contacting or starting its backend")
	flags.BoolVar(&o.chat, "chat", false, "tool-free text conversation; no workspace or coding instructions are loaded")
	flags.StringVar(&o.systemPrompt, "system-prompt", "You are a helpful conversational assistant.", "character identity instruction for Chat or Agent")
	flags.StringVar(&o.chatDir, "chat-dir", "", "directory for -chat -tui conversation archives; defaults to var/chats beside the executable")
	flags.StringVar(&o.characterPath, "character", "", "PNG or JSON character card for tool-free chat")
	flags.StringVar(&o.characterPath, "character-png", "", "alias for -character; reads PNG metadata or JSON without changing the card")
	flags.StringVar(&o.userName, "user-name", "User", "name replacing {{user}} in chat character cards")
	flags.StringVar(&o.postHistory, "post-history", "", "chat system instruction placed after the latest user turn")
	flags.BoolVar(&o.preferCharacterPrompt, "character-main-prompt", true, "allow the card to override the base chat system prompt")
	flags.BoolVar(&o.preferCharacterPost, "character-post-history", true, "allow the card to override the base post-history instruction")
	flags.Float64Var(&o.temperature, "temperature", 0, "chat sampling temperature, 0 uses transport default 0.4; literal greedy zero is unsupported")
	flags.BoolVar(&o.voice, "voice", false, "enable the profile speech service for -chat -tui; requires speech assets")
	flags.StringVar(&o.conversationMode, "conversation", "", "conversation runner: chat or agent")
	if err := flags.Parse(args); err != nil {
		return launchOptions{}, err
	}
	if o.conversationMode != "" {
		if o.conversationMode != "chat" && o.conversationMode != "agent" {
			return launchOptions{}, fmt.Errorf("conversation must be chat or agent")
		}
		o.chat = o.conversationMode == "chat"
	}
	o.explicitFlags = map[string]bool{}
	flags.Visit(func(f *flag.Flag) {
		o.explicitFlags[f.Name] = true
		o.explicitNames = append(o.explicitNames, f.Name)
	})
	o.task = strings.Join(flags.Args(), " ")
	return o, nil
}

// resolvedLaunch combines CLI overrides with the selected profile. Service
// ownership stays with runConfigured; resolving a launch starts no services.
type resolvedLaunch struct {
	launchOptions
	selectedProfile  config.Profile
	speechSettings   *speech.Config
	thinkingSettings config.Provider
}

func resolveLaunch(launch launchRequest) (resolvedLaunch, error) {
	options, err := parseLaunchOptions(launch.args)
	if err != nil {
		return resolvedLaunch{}, err
	}
	o := resolvedLaunch{launchOptions: options}
	if o.checkProfile && o.profilePath == "" {
		return resolvedLaunch{}, fmt.Errorf("-check-profile requires -profile")
	}
	if o.profilePath != "" {
		p, err := config.Load(o.profilePath)
		if launch.profile != nil {
			p = cloneLaunchProfile(*launch.profile)
			err = p.Validate()
		}
		if err != nil {
			return resolvedLaunch{}, err
		}
		if p.Provider.Ownership == "managed" && !o.chat && !o.tui && !o.checkProfile {
			return resolvedLaunch{}, fmt.Errorf("managed thinking requires -tui or -chat")
		}
		o.speechSettings = p.Speech
		o.selectedProfile = p
		o.thinkingSettings = p.Provider
		if o.speechSettings != nil && (o.explicitFlags["voice"] || launch.common) {
			o.speechSettings.Enabled = o.voice
		}
		if o.speechSettings != nil && !o.chat && !o.tui && !o.checkProfile {
			return resolvedLaunch{}, fmt.Errorf("speech profile requires -chat")
		}
		explicit := o.explicitFlags
		if !explicit["backend-kind"] {
			o.backendKind = p.Provider.Kind
		}
		if !explicit["backend"] {
			o.backendURL = p.Provider.URL
		}
		if !explicit["model"] {
			o.model = p.Provider.Model
		}
		if !explicit["api-key-env"] {
			o.apiKeyEnv = p.Provider.APIKeyEnv
		}
		if !explicit["max-tokens"] {
			o.maxTokens = p.Generation.MaxTokens
		}
		if !explicit["context-limit"] {
			o.contextLimit = p.Generation.ContextLimit
		}
		if !explicit["temperature"] {
			o.temperature = p.Generation.Temperature
		}
		if p.Conversation != nil {
			c := p.Conversation
			if !explicit["character"] && !explicit["character-png"] {
				o.characterPath = c.Character
			}
			if !explicit["user-name"] && c.UserName != "" {
				o.userName = c.UserName
			}
			if !explicit["system-prompt"] && c.SystemPrompt != "" {
				o.systemPrompt = c.SystemPrompt
			}
			if !explicit["post-history"] {
				o.postHistory = c.PostHistory
			}
			if !explicit["character-main-prompt"] && c.PreferCharacterPrompt != nil {
				o.preferCharacterPrompt = *c.PreferCharacterPrompt
			}
			if !explicit["character-post-history"] && c.PreferCharacterPostHistory != nil {
				o.preferCharacterPost = *c.PreferCharacterPostHistory
			}
		}
		p.Generation.Temperature = o.temperature
		if p.Conversation != nil || o.characterPath != "" || o.postHistory != "" {
			p.Conversation = &config.Conversation{PromptPreset: selectedProfilePreset(o.selectedProfile), Name: selectedProfileName(o.selectedProfile), UserPersona: selectedProfilePersona(o.selectedProfile), GreetingIndex: selectedGreetingIndex(o.selectedProfile), Character: o.characterPath, UserName: o.userName, SystemPrompt: o.systemPrompt, PostHistory: o.postHistory, PreferCharacterPrompt: &o.preferCharacterPrompt, PreferCharacterPostHistory: &o.preferCharacterPost}
		}
		p.Provider.Kind, p.Provider.URL, p.Provider.Model, p.Provider.APIKeyEnv = o.backendKind, o.backendURL, o.model, o.apiKeyEnv
		p.Generation.MaxTokens, p.Generation.ContextLimit = o.maxTokens, o.contextLimit
		if err := p.Validate(); err != nil {
			return resolvedLaunch{}, err
		}
		o.selectedProfile = p
	}

	o.thinkingSettings.Kind, o.thinkingSettings.URL, o.thinkingSettings.Model = o.backendKind, o.backendURL, o.model
	o.thinkingSettings.APIKeyEnv = o.apiKeyEnv
	if o.selectedProfile.Version != 0 {
		o.selectedProfile.Provider = o.thinkingSettings
		o.selectedProfile.Generation = config.Generation{MaxTokens: o.maxTokens, ContextLimit: o.contextLimit, Temperature: o.temperature}
	}
	return o, nil
}
