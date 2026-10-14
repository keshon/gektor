package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/llm"
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
