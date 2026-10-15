package settings

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/prompts"
	"github.com/keshon/tars/internal/tui/components/form"
)

func PageFields(profile config.Profile, section string, preferences config.Preferences, voice string) (string, []form.Field) {
	conversation := config.Conversation{UserName: "User"}
	if profile.Conversation != nil {
		conversation = *profile.Conversation
	}
	profile.Conversation = &conversation
	if profile.Speech != nil {
		copy := *profile.Speech
		profile.Speech = &copy
	}
	fields := []form.Field{}
	add := func(key, label, value string, multi bool) {
		fields = append(fields, form.Field{Key: key, Label: label, Value: value, Multiline: multi})
	}
	title := ""
	switch section {
	case "application":
		title = "Application"
		p := preferences
		add("app-mode", "Mode", p.Mode, false)
		add("app-workspace", "Workspace", p.Workspace, false)
		add("app-voice", "Voice", voice, false)
	case "profiles":
		title = "Profiles"
		add("load-profile", "Load setup from profile", "", false)
		add("profile-name", "Save current setup as (Ctrl+D)", "custom", false)
	case "character":
		title = "Character"
		add("character", "Character card (PNG/JSON; blank removes card)", conversation.Character, false)
		add("name", "Assistant name (without a card)", conversation.Name, false)
		add("greeting", "Opening greeting", strconv.Itoa(conversation.GreetingIndex), false)
	case "you":
		title = "You"
		add("user", "Your name", conversation.UserName, false)
		add("persona", "Your persona description", conversation.UserPersona, true)
	case "generation":
		title = "Generation"
		add("temperature", "Temperature (0 = default 0.4)", strconv.FormatFloat(profile.Generation.Temperature, 'f', -1, 64), false)
		add("max", "Response budget (tokens)", strconv.Itoa(profile.Generation.MaxTokens), false)
		add("context", "Context window (tokens)", strconv.Itoa(profile.Generation.ContextLimit), false)
	case "prompts":
		title = "Conversation prompts"
		preset, _ := prompts.ResponsePreset(conversation.PromptPreset)
		add("prompt-preset", "Prompt preset", preset.ID, false)
		add("system", "Base system prompt", conversation.SystemPrompt, true)
		add("post", "Base post-history directive", conversation.PostHistory, true)
		main, post := true, true
		if conversation.PreferCharacterPrompt != nil {
			main = *conversation.PreferCharacterPrompt
		}
		if conversation.PreferCharacterPostHistory != nil {
			post = *conversation.PreferCharacterPostHistory
		}
		add("prefer-main", "Use card main prompt (true/false)", strconv.FormatBool(main), false)
		add("prefer-post", "Use card post-history (true/false)", strconv.FormatBool(post), false)
	case "speech":
		title = "Speech services"
		add("microphone", "Microphone (Windows device name)", profile.Speech.Microphone, false)
		add("variant", "Chatterbox variant", profile.Speech.Speaking.Variant, false)
		add("speech-model", "Speech model directory", profile.Speech.Speaking.Model, false)
		add("seconds", "Maximum recording seconds", strconv.Itoa(profile.Speech.MaxSeconds), false)
		mode := profile.Speech.VAD
		if mode == "" {
			mode = "silero"
		}
		add("vad", "Speech detector", mode, false)
		add("mic-start", "Speech onset (ms; 0 = 200)", strconv.Itoa(profile.Speech.MicStartMS), false)
		add("mic-end", "End silence (ms; 0 = 1100)", strconv.Itoa(profile.Speech.MicEndMS), false)
		add("mic-threshold", "Energy threshold (0 = adaptive)", strconv.FormatFloat(profile.Speech.MicThreshold, 'f', -1, 64), false)
	case "model":
		if profile.Provider.Runtime == "" {
			exe, err := os.Executable()
			if err == nil {
				profile.Provider.Runtime = filepath.Join(filepath.Dir(exe), "assets", "runtimes", "llamacpp", "llama-server.exe")
			}
		}
		title = "Model"
		add("ownership", "Thinking service (external/managed)", profile.Provider.Ownership, false)
		add("runtime", "llama-server executable", profile.Provider.Runtime, false)
		add("model-root", "Model directory (optional; scans nested folders)", profile.Provider.ModelRoot, false)
		add("model-file", "GGUF model file (any directory)", profile.Provider.ModelFile, false)
		add("gpu-layers", "GPU layers (0 CPU; 99 all layers)", strconv.Itoa(profile.Provider.GPULayers), false)
		add("kind", "Protocol (llama/kobold/openai)", profile.Provider.Kind, false)
		add("endpoint", "Backend URL", profile.Provider.URL, false)
		add("model", "Model identifier", profile.Provider.Model, false)
		add("key-env", "API key environment variable", profile.Provider.APIKeyEnv, false)
	}
	return title, fields
}
