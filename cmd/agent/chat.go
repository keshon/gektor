package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/keshon/tars/internal/app"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/roles"
	"github.com/keshon/tars/internal/tui"
)

func runChat(ctx context.Context, client llm.Client, cfg chat.Config, initial string, input io.Reader, output io.Writer) error {
	conversation, err := chat.New(client, cfg)
	if err != nil {
		return err
	}
	ask := func(text string) error {
		result, err := conversation.Ask(ctx, text)
		if result.OmittedTurns > 0 {
			fmt.Fprintf(output, "[Context window omits %d earlier turns; /reset starts fresh.]\n", result.OmittedTurns)
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, result.Response.Message.Content)
		return err
	}
	if initial != "" {
		return ask(initial)
	}
	// Reading the terminal must not prevent Ctrl+C from returning. The CLI
	// process owns stdin; a pending OS read is released when the process exits.
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	type line struct {
		text string
		err  error
		done bool
	}
	lines := make(chan line)
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	go func() {
		for scanner.Scan() {
			select {
			case lines <- line{text: scanner.Text()}:
			case <-readCtx.Done():
				return
			}
		}
		select {
		case lines <- line{err: scanner.Err(), done: true}:
		case <-readCtx.Done():
		}
	}()
	if cfg.Greeting != "" {
		fmt.Fprintln(output, cfg.Greeting)
	}
	fmt.Fprintln(output, "Chat: /reset clears history; /quit exits. One message per line.")
	for {
		fmt.Fprint(output, "> ")
		var next line
		select {
		case <-ctx.Done():
			return ctx.Err()
		case next = <-lines:
		}
		if next.done {
			return next.err
		}
		text := strings.TrimSpace(next.text)
		switch text {
		case "/quit":
			return nil
		case "/reset":
			conversation.Reset()
			fmt.Fprintln(output, "Conversation reset.")
			continue
		case "":
			continue
		}
		if err := ask(text); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintf(output, "Error: %v\n", err)
		}
	}
}

// runChatLaunch owns the text or terminal Chat frontend for one launch.
func runChatLaunch(launch launchRequest, o resolvedLaunch) error {
	task := o.task
	if o.voice && o.speechSettings == nil {
		return fmt.Errorf("-voice requires a profile with speech configuration")
	}
	if o.speechSettings != nil && o.speechSettings.Enabled && !o.tui {
		return fmt.Errorf("speech currently requires -chat -tui")
	}
	allowed := map[string]bool{"conversation": true, "voice": true, "character": true, "character-png": true, "user-name": true, "post-history": true, "character-main-prompt": true, "character-post-history": true, "temperature": true, "chat": true, "tui": true, "chat-dir": true, "system-prompt": true, "profile": true, "check-profile": true, "backend": true, "backend-kind": true, "model": true, "api-key": true, "api-key-env": true, "max-tokens": true, "context-limit": true}
	var incompatible string
	for _, name := range o.explicitNames {
		if !allowed[name] && incompatible == "" {
			incompatible = name
		}
	}
	if incompatible != "" {
		return fmt.Errorf("-chat does not support -%s in this migration stage", incompatible)
	}
	client, err := llm.ClientFor(o.backendKind, o.backendURL, o.model)
	if err != nil {
		return err
	}
	client.NoGrammar = true
	app.ResolveAPIKey(o.apiKey, o.apiKeyEnv, client)
	client.ExtraHeaders = app.ExtraHeadersFromEnv()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	settings, warnings, err := app.CompileChatSettings(app.ChatOptions{Preset: selectedProfilePreset(o.selectedProfile), Name: selectedProfileName(o.selectedProfile), Persona: selectedProfilePersona(o.selectedProfile), GreetingIndex: selectedGreetingIndex(o.selectedProfile), Character: o.characterPath, User: o.userName, System: o.systemPrompt, Post: o.postHistory, PreferMain: o.preferCharacterPrompt, PreferPost: o.preferCharacterPost, MaxTokens: o.maxTokens, ContextLimit: o.contextLimit, Temperature: o.temperature})
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, "Character warning: "+warning)
	}
	if o.chatDir != "" && !o.tui {
		return fmt.Errorf("-chat-dir requires -chat -tui")
	}
	if o.tui {
		directory := o.chatDir
		if directory == "" {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			directory = filepath.Join(filepath.Dir(exe), "var", "chats")
		}
		directory, err = filepath.Abs(directory)
		if err != nil {
			return err
		}
		profile := config.Profile{Speech: o.speechSettings, Version: 1, Provider: config.Provider{Kind: o.backendKind, URL: o.backendURL, Model: o.model, Ownership: "external", APIKeyEnv: o.apiKeyEnv}, Generation: config.Generation{MaxTokens: o.maxTokens, ContextLimit: o.contextLimit, Temperature: o.temperature}, Conversation: &config.Conversation{PromptPreset: selectedProfilePreset(o.selectedProfile), Name: selectedProfileName(o.selectedProfile), UserPersona: selectedProfilePersona(o.selectedProfile), GreetingIndex: selectedGreetingIndex(o.selectedProfile), Character: o.characterPath, UserName: o.userName, SystemPrompt: o.systemPrompt, PostHistory: o.postHistory, PreferCharacterPrompt: &o.preferCharacterPrompt, PreferCharacterPostHistory: &o.preferCharacterPost}}
		profile.Provider.Ownership = o.thinkingSettings.Ownership
		if profile.Provider.Ownership == "" {
			profile.Provider.Ownership = "external"
		}
		profile.Provider.Runtime, profile.Provider.ModelFile, profile.Provider.GPULayers = o.thinkingSettings.Runtime, o.thinkingSettings.ModelFile, o.thinkingSettings.GPULayers
		profile.Provider.ModelRoot = o.thinkingSettings.ModelRoot
		configured, err := app.ConfigureChat(profile, o.apiKey, directory)
		if err != nil {
			return err
		}
		owned := app.NewRuntime(&configured)
		defer owned.Close()
		app := applicationControl(launch, profile, o.profilePath, o.chat, o.workspace)
		_, err = tui.Run(ctx, tui.Config{Runtime: owned, Application: app, SeedHistory: launch.history, InitialDraft: launch.draft, SessionDirectory: launch.directory, Chat: &configured, BackendURL: o.backendURL, Task: task, Env: roles.Env{BackendKind: o.backendKind, Model: o.model}})
		return err
	}
	if o.thinkingSettings.Ownership == "managed" {
		return fmt.Errorf("managed thinking currently requires -chat -tui")
	}
	return runChat(ctx, client, settings, task, os.Stdin, os.Stdout)
}
