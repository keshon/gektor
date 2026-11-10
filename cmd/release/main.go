// Command release stages local distributions and builds versioned release archives.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/keshon/gektor/internal/release"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("release", flag.ContinueOnError)
	target := flags.String("target", "", "dist target: windows-amd64 or linux-amd64; defaults to host OS")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: go run ./cmd/release [-target OS-amd64] dist | vMAJOR.MINOR.PATCH")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return fmt.Errorf("choose dist or a release version")
	}
	options := release.Options{Root: ".", Target: *target, Log: os.Stderr}
	if flags.Arg(0) != "dist" {
		options.Version = flags.Arg(0)
	}
	outputs, err := release.Build(ctx, options)
	if err != nil {
		return err
	}
	for _, output := range outputs {
		fmt.Println(output)
	}
	return nil
}
