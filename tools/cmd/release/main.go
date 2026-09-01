package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/molejo-platform/molejo/tools/internal/release"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return usageError()
	}
	command := os.Args[1]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	version := flags.String("version", "", "pre-release version without the v prefix")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if *version == "" || flags.NArg() != 0 {
		return usageError()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root, err := release.DiscoverRoot(ctx)
	if err != nil {
		return err
	}
	pipeline := release.New(root, *version, os.Stdout, os.Stderr)
	switch command {
	case "check":
		return pipeline.Check(ctx)
	case "build":
		return pipeline.Build(ctx)
	case "publish":
		return pipeline.Publish(ctx)
	case "verify":
		return pipeline.Verify(ctx)
	default:
		return usageError()
	}
}

func usageError() error {
	return fmt.Errorf("usage: go -C tools run ./cmd/release <check|build|publish|verify> --version 0.1.0-alpha.1")
}
