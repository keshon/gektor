package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/credentials"
	"github.com/keshon/gektor/internal/userdirs"
)

const sshUsage = "usage: gektor ssh list | save NAME PROFILE.json | delete NAME | check NAME"

func runSSH(args []string, output io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", sshUsage)
	}
	store := ssh.Profiles{Path: userdirs.Sub("ssh.json")}
	if args[0] == "check" {
		if len(args) != 2 {
			return fmt.Errorf("%s", sshUsage)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		service := ssh.New(ssh.Config{
			ProfilesPath: store.Path,
			Credentials:  credentials.Store{Directory: userdirs.Sub("credentials", "ssh")},
		})
		err := service.Check(ctx, args[1])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, "Host key and authentication verified; connection closed. SFTP support was not tested.")
		return err
	}

	profiles, err := store.Load()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("%s", sshUsage)
		}
		encoder := json.NewEncoder(output)
		return encoder.Encode(profiles)
	case "save":
		if len(args) != 3 {
			return fmt.Errorf("%s", sshUsage)
		}
		file, openErr := os.Open(args[2])
		if openErr != nil {
			return openErr
		}
		defer file.Close() // Read-only profile input; a close error cannot change the saved configuration.

		input := io.LimitReader(file, (64<<10)+1)
		decoder := json.NewDecoder(input)
		decoder.DisallowUnknownFields()
		var profile ssh.Profile
		err = decoder.Decode(&profile)
		if err != nil {
			return fmt.Errorf("invalid SSH profile JSON: %w", err)
		}
		if decoder.Decode(new(any)) != io.EOF {
			return fmt.Errorf("expected one SSH profile JSON object")
		}
		profiles[args[1]] = profile
	case "delete":
		if len(args) != 2 {
			return fmt.Errorf("%s", sshUsage)
		}
		if _, exists := profiles[args[1]]; !exists {
			return fmt.Errorf("SSH connection does not exist")
		}
		delete(profiles, args[1])
	default:
		return fmt.Errorf("%s", sshUsage)
	}
	return store.Save(profiles)
}
