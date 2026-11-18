package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/keshon/gektor/internal/workflow/credentials"
)

func workflowCredentials(args []string, input io.Reader, out io.Writer, directory string) error {
	command := args[0]
	flags := flag.NewFlagSet("workflow "+command, flag.ContinueOnError)
	var config credentials.Config
	var stored bool
	var revision string
	if command == "register-credential" || command == "update-credential" {
		flags.StringVar(&config.Origin, "origin", "", "exact HTTPS origin permitted to receive this credential")
		flags.StringVar(&config.Env, "env", "", "environment variable containing the secret")
		flags.StringVar(&config.Header, "header", "Authorization", "Authorization or X-API-Key")
		flags.StringVar(&config.Prefix, "prefix", "Bearer ", "header prefix; use an empty prefix for API keys")
		flags.BoolVar(&stored, "secret-stdin", false, "read the secret from stdin and protect it for the current account")
	}
	if command == "update-credential" {
		flags.StringVar(&revision, "revision", "", "current binding revision from inspect-credential")
	}
	err := flags.Parse(args[1:])
	if err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("%s requires one name", command)
	}
	name := flags.Arg(0)
	store := credentials.Store{Directory: directory}

	switch command {
	case "register-credential", "update-credential":
		if command == "update-credential" && revision == "" {
			return fmt.Errorf("update-credential requires revision from inspect-credential")
		}
		if stored {
			if config.Env != "" {
				return fmt.Errorf("secret-stdin cannot combine with env")
			}
			raw, readErr := io.ReadAll(io.LimitReader(input, 8195))
			if readErr != nil {
				return fmt.Errorf("reading credential from stdin")
			}
			raw = bytes.TrimSuffix(raw, []byte("\n"))
			raw = bytes.TrimSuffix(raw, []byte("\r"))
			if command == "update-credential" {
				err = store.UpdateStored(name, revision, config, raw)
			} else {
				err = store.RegisterStored(name, config, raw)
			}
		} else {
			if command == "update-credential" {
				err = store.Update(name, revision, config)
			} else {
				err = store.Register(name, config)
			}
		}
	case "revoke-credential":
		err = store.Revoke(name)
	case "inspect-credential":
	default:
		return fmt.Errorf("unknown credential command")
	}
	if err != nil {
		return err
	}
	binding, err := store.Inspect(name)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(binding)
}
