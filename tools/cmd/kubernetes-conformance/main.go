package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/molejo-platform/molejo/tools/internal/conformance"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Kubernetes conformance:", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("kubernetes-conformance", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	endpoint := flags.String("endpoint", "", "forwarded Control Plane API endpoint")
	serverName := flags.String("server-name", "control-plane-api.molejo-control-plane.svc.cluster.local", "API TLS server name")
	host := flags.String("host", "control-plane-api.molejo-control-plane.svc.cluster.local", "HTTP Host accepted by the API")
	origin := flags.String("origin", "http://127.0.0.1:8080", "trusted mutation origin")
	caFile := flags.String("ca-file", "", "API CA certificate file")
	passwordFile := flags.String("password-file", "", "owner password file")
	image := flags.String("image", "", "fixture OCI image pinned by sha256 digest")
	resultFile := flags.String("result-file", "", "path for the non-secret JSON result")
	timeout := flags.Duration("timeout", 8*time.Minute, "journey timeout")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *endpoint == "" || *caFile == "" || *passwordFile == "" || *image == "" || *resultFile == "" {
		return errors.New("endpoint, ca-file, password-file, image, and result-file are required")
	}
	password, err := os.ReadFile(*passwordFile)
	if err != nil {
		return fmt.Errorf("read owner password: %w", err)
	}
	if strings.TrimSpace(string(password)) == "" {
		return errors.New("owner password is empty")
	}
	client, err := conformance.NewClient(*endpoint, *serverName, *host, *origin, *caFile)
	if err != nil {
		return err
	}
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, *timeout)
	defer cancel()
	result, err := conformance.Run(ctx, client, conformance.Config{
		Image:    strings.TrimSpace(*image),
		Password: strings.TrimSpace(string(password)),
		Progress: func(message string) { _, _ = fmt.Fprintln(os.Stdout, "PASS ", message) },
	})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	if err = os.WriteFile(*resultFile, append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	_, _ = fmt.Fprintln(os.Stdout, "Result: healthy")
	return nil
}
