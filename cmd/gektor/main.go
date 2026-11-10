// Command gektor runs Agent, Workflows and their terminal or stdio frontends.
//
//	gektor "create a snake game in plain JS under ./game"
//	gektor -debug "..."   # also writes agent-debug.log with raw request/response JSON
package main

import (
	"context"
	"errors"
	"log"
	"os"
)

func main() {
	if err := runMain(); err != nil {
		log.Print(err)
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		os.Exit(1)
	}
}

func runMain() error { return runApplication(os.Args[1:]) }
