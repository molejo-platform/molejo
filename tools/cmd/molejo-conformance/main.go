package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/molejo-platform/molejo/tools/internal/conformance"
)

var version = "dev"

func main() {
	code := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	var err error
	switch args[0] {
	case "profile":
		err = runProfile(args[1:], stdout, stderr)
	case "plan":
		err = runPlan(args[1:], stdout, stderr)
	case "run":
		err = runConformance(ctx, args[1:], stdout, stderr)
	case "cleanup":
		err = runCleanup(ctx, args[1:], stdout, stderr)
	default:
		printUsage(stderr)
		return 2
	}
	if err == nil {
		return 0
	}
	_, _ = fmt.Fprintln(stderr, "molejo-conformance:", err)
	var usage *usageError
	if errors.As(err, &usage) {
		return 2
	}
	return 1
}

type usageError struct{ message string }

func (e *usageError) Error() string { return e.message }

func runProfile(args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 || args[0] != "list" {
		return &usageError{message: "usage: molejo-conformance profile list"}
	}
	for _, profile := range conformance.Profiles() {
		_, _ = fmt.Fprintf(stdout, "%s/%s\t%s\n", profile.ID, profile.Version, profile.Description)
	}
	return nil
}

func runPlan(args []string, stdout, stderr io.Writer) error {
	options, err := parseOptions("plan", args, stderr, false)
	if err != nil {
		return err
	}
	profile, err := conformance.ProfileByID(options.profile)
	if err != nil {
		return &usageError{message: err.Error()}
	}
	lines, err := conformance.Plan(profile, options.target())
	if err != nil {
		return &usageError{message: err.Error()}
	}
	for _, line := range lines {
		_, _ = fmt.Fprintln(stdout, line)
	}
	return nil
}

func runConformance(parent context.Context, args []string, stdout, stderr io.Writer) error {
	options, err := parseOptions("run", args, stderr, true)
	if err != nil {
		return err
	}
	profile, err := conformance.ProfileByID(options.profile)
	if err != nil {
		return &usageError{message: err.Error()}
	}
	password, err := readSecret(options.passwordFile)
	if err != nil {
		return err
	}
	client, err := conformance.NewClient(options.endpoint, options.serverName, options.host, options.origin, options.caFile)
	if err != nil {
		return &usageError{message: err.Error()}
	}
	runID := options.runID
	if runID == "" {
		runID, err = newRunID()
		if err != nil {
			return err
		}
	}
	parent, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, options.timeout)
	defer cancel()
	report, err := conformance.RunProfile(ctx, conformance.RunConfig{
		RunID: runID, RunnerVersion: version, Target: options.target(), Profile: profile,
		Client: client, Password: password, Image: options.image, OutputDir: options.outputDir,
		Progress: func(message string) { _, _ = fmt.Fprintln(stdout, "PASS", message) },
	})
	_, _ = fmt.Fprintf(stdout, "Result: %s profile=%s/%s run=%s report=%s/report.json\n", report.Status, profile.ID, profile.Version, runID, options.outputDir)
	return err
}

func runCleanup(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	runDir := flags.String("run-dir", "", "directory containing report.json")
	endpoint := flags.String("endpoint", "", "Control Plane API HTTPS endpoint")
	serverName := flags.String("server-name", "control-plane-api.molejo-control-plane.svc.cluster.local", "API TLS server name")
	host := flags.String("host", "control-plane-api.molejo-control-plane.svc.cluster.local", "HTTP Host accepted by the API")
	origin := flags.String("origin", "http://127.0.0.1:8080", "trusted mutation origin")
	caFile := flags.String("ca-file", "", "API CA certificate file")
	passwordFile := flags.String("password-file", "", "owner password file")
	if err := flags.Parse(args); err != nil {
		return &usageError{message: err.Error()}
	}
	if flags.NArg() != 0 || *runDir == "" || *endpoint == "" || *caFile == "" || *passwordFile == "" {
		return &usageError{message: "run-dir, endpoint, ca-file, and password-file are required"}
	}
	reporter, err := conformance.LoadReporter(*runDir)
	if err != nil {
		return err
	}
	report := reporter.Snapshot()
	if report.Target.Endpoint != "" && report.Target.Endpoint != *endpoint {
		return &usageError{message: "cleanup endpoint differs from the recorded target"}
	}
	password, err := readSecret(*passwordFile)
	if err != nil {
		return err
	}
	client, err := conformance.NewClient(*endpoint, *serverName, *host, *origin, *caFile)
	if err != nil {
		return &usageError{message: err.Error()}
	}
	if err = client.Login(ctx, password); err != nil {
		return fmt.Errorf("authenticate cleanup: %w", err)
	}
	defer func() {
		logoutContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Logout(logoutContext)
	}()
	observed, err := conformance.VerifyTarget(ctx, client, conformance.Target{
		Endpoint: *endpoint, ClusterID: report.Target.ClusterID, ExpectedClusterUID: report.Target.ClusterUID,
		KubeContext: report.Target.KubeContext, Disposable: report.Target.Disposable,
	})
	if err != nil {
		return fmt.Errorf("verify cleanup target: %w", err)
	}
	if observed.ClusterID != report.Target.ClusterID {
		return errors.New("cleanup target identity mismatch")
	}
	result := conformance.RecoverCleanup(reporter, client)
	_, _ = fmt.Fprintf(stdout, "Cleanup: %s run=%s report=%s/report.json\n", result.Status, report.RunID, *runDir)
	if result.Status != conformance.StatusPass {
		return errors.New("cleanup did not complete")
	}
	return nil
}

type options struct {
	profile, endpoint, serverName, host, origin, caFile, passwordFile string
	image, outputDir, runID, clusterID, clusterUID, kubeContext       string
	workspaceID                                                       string
	disposable                                                        bool
	timeout                                                           time.Duration
}

func parseOptions(command string, args []string, stderr io.Writer, requireRuntime bool) (options, error) {
	var value options
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&value.profile, "profile", conformance.AlphaCoreProfileID, "compiled conformance profile")
	flags.StringVar(&value.endpoint, "endpoint", "", "Control Plane API HTTPS endpoint")
	flags.StringVar(&value.serverName, "server-name", "control-plane-api.molejo-control-plane.svc.cluster.local", "API TLS server name")
	flags.StringVar(&value.host, "host", "control-plane-api.molejo-control-plane.svc.cluster.local", "HTTP Host accepted by the API")
	flags.StringVar(&value.origin, "origin", "http://127.0.0.1:8080", "trusted mutation origin")
	flags.StringVar(&value.caFile, "ca-file", "", "API CA certificate file")
	flags.StringVar(&value.passwordFile, "password-file", "", "owner password file")
	flags.StringVar(&value.image, "image", "", "fixture OCI image pinned by sha256 digest")
	flags.StringVar(&value.outputDir, "output", "", "private result directory")
	flags.StringVar(&value.runID, "run-id", "", "explicit run identifier")
	flags.StringVar(&value.clusterID, "cluster-id", "", "target Molejo cluster ID")
	flags.StringVar(&value.clusterUID, "cluster-uid", "", "expected Kubernetes cluster UID")
	flags.StringVar(&value.kubeContext, "kube-context", "", "target Kubernetes context recorded in evidence")
	flags.StringVar(&value.workspaceID, "workspace-id", "", "existing test Workspace for a persistent target")
	flags.BoolVar(&value.disposable, "disposable-target", false, "allow creation of a Workspace because the entire target will be destroyed")
	flags.DurationVar(&value.timeout, "timeout", 10*time.Minute, "run timeout")
	if err := flags.Parse(args); err != nil {
		return options{}, &usageError{message: err.Error()}
	}
	if flags.NArg() != 0 || value.clusterID == "" {
		return options{}, &usageError{message: "profile and cluster-id are required"}
	}
	if !value.disposable && value.workspaceID == "" {
		return options{}, &usageError{message: "persistent targets require workspace-id"}
	}
	if requireRuntime && (value.endpoint == "" || value.caFile == "" || value.passwordFile == "" || value.image == "" || value.outputDir == "") {
		return options{}, &usageError{message: "endpoint, ca-file, password-file, image, and output are required"}
	}
	return value, nil
}

func (o options) target() conformance.Target {
	return conformance.Target{
		Endpoint: o.endpoint, ClusterID: o.clusterID, ExpectedClusterUID: o.clusterUID,
		KubeContext: o.kubeContext, Disposable: o.disposable, WorkspaceID: o.workspaceID,
	}
}

func readSecret(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read owner password: %w", err)
	}
	secret := strings.TrimSpace(string(contents))
	if secret == "" {
		return "", errors.New("owner password is empty")
	}
	return secret, nil
}

func newRunID() (string, error) {
	value := make([]byte, 10)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate run ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func printUsage(output io.Writer) {
	_, _ = fmt.Fprintln(output, "usage: molejo-conformance <profile list|plan|run|cleanup> [options]")
}
