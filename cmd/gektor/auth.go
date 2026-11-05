package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"time"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/providers/openai"
)

func runAuth(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gektor auth login [--new] | status | accounts | select <id> | logout | models | test <model>")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	m := openai.Default()
	switch args[0] {
	case "login":
		if len(args) > 2 || (len(args) == 2 && args[1] != "--new") {
			return fmt.Errorf("usage: gektor auth login [--new]")
		}
		fmt.Println("Continue with ChatGPT in the browser. Gektor will request permission to use your ChatGPT plan.")
		if err := m.Login(ctx, func(url string) error {
			fmt.Println(url)
			if err := openAuthBrowser(url); err != nil {
				fmt.Println("Open the URL above in a browser to continue.")
			}
			return nil
		}, len(args) == 2); err != nil {
			return err
		}
		status, err := m.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Println("Connected:", status)
	case "status":
		status, err := m.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Println(status)
	case "accounts":
		list, err := m.Connections(ctx)
		if err != nil {
			return err
		}
		for _, a := range list {
			marker := " "
			if a.Active {
				marker = "*"
			}
			fmt.Printf("%s %s  %s\n", marker, a.ID, a.Label)
		}
	case "select":
		if len(args) != 2 {
			return fmt.Errorf("usage: gektor auth select <registration-id>")
		}
		return m.Select(ctx, args[1])
	case "logout":
		if err := m.Logout(ctx); err != nil {
			return err
		}
		fmt.Println("Disconnected from ChatGPT.")
	case "models":
		models, err := m.Models(ctx)
		if err != nil {
			return err
		}
		for _, model := range models {
			fmt.Println(model)
		}
	case "test":
		if len(args) != 2 {
			return fmt.Errorf("usage: gektor auth test <model-from-auth-models>")
		}
		client, err := app.NewBackend(ctx, app.BackendOptions{Kind: "chatgpt", URL: openai.API, Model: args[1]})
		if err != nil {
			return err
		}
		response, err := client.Stream(ctx, llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "Reply exactly: Gektor connection works."}}}, nil, func(s string) { fmt.Print(s) })
		if err != nil {
			return err
		}
		fmt.Printf("\nCompleted. Input: %d tokens; output: %d tokens.\n", response.Usage.PromptTokens, response.Usage.CompletionTokens)
	default:
		return fmt.Errorf("unknown auth action %q", args[0])
	}
	return nil
}

func openAuthBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
