package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/molejo-platform/molejo/tools/internal/release"
)

type imageFlags map[string]string

func (values imageFlags) String() string { return "name=repository@sha256:digest" }

func (values imageFlags) Set(value string) error {
	name, reference, found := strings.Cut(value, "=")
	if !found || strings.TrimSpace(name) == "" || strings.TrimSpace(reference) == "" {
		return errors.New("image must use name=repository@sha256:digest")
	}
	values[strings.TrimSpace(name)] = strings.TrimSpace(reference)
	return nil
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "conformance bundle:", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("conformance-bundle", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	version := flags.String("version", "0.0.0-kind.1", "local chart version")
	output := flags.String("output", "", "directory for packaged charts")
	images := imageFlags{}
	flags.Var(images, "image", "immutable image reference as name=repository@sha256:digest; repeat for each Molejo image")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*output) == "" {
		return errors.New("usage: conformance-bundle --output <directory> [--version <version>] --image <name=reference>...")
	}
	for _, image := range release.Images {
		if images[image.Name] == "" {
			return fmt.Errorf("missing --image for %s", image.Name)
		}
	}
	if len(images) != len(release.Images) {
		return errors.New("one or more unknown images were provided")
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root, err := release.DiscoverRoot(ctx)
	if err != nil {
		return err
	}
	pipeline := release.New(root, strings.TrimSpace(*version), io.Discard, os.Stderr)
	charts, err := pipeline.PackageCharts(ctx, *output, images)
	if err != nil {
		return err
	}
	for _, chart := range release.Charts {
		_, _ = fmt.Fprintf(os.Stdout, "%s=%s\n", chart.Name, charts[chart.Name].File)
	}
	return nil
}
