package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

var appPublicIDPattern = regexp.MustCompile(`^app-[a-z2-7]{20}$`)

type Request struct {
	ContextDirectory string
	AppPublicID      string
	CommitSHA        string
}

type Result struct {
	Digest string
	Image  string
}

type Builder interface {
	Build(context.Context, Request, io.Writer) (Result, error)
}

type CommandRunner interface {
	Run(context.Context, string, []string, io.Writer) error
}

type BuildKitRunner struct {
	Address               string
	ImageRepositoryPrefix string
	TLSCACert             string
	TLSCert               string
	TLSKey                string
	Binary                string
	Command               CommandRunner
}

func (r BuildKitRunner) Build(ctx context.Context, request Request, output io.Writer) (Result, error) {
	if strings.TrimSpace(r.Address) == "" || strings.TrimSpace(r.ImageRepositoryPrefix) == "" {
		return Result{}, errors.New("BuildKit configuration is incomplete")
	}
	tlsValues := []string{strings.TrimSpace(r.TLSCACert), strings.TrimSpace(r.TLSCert), strings.TrimSpace(r.TLSKey)}
	if (tlsValues[0] != "" || tlsValues[1] != "" || tlsValues[2] != "") && (tlsValues[0] == "" || tlsValues[1] == "" || tlsValues[2] == "") {
		return Result{}, errors.New("BuildKit TLS configuration is incomplete")
	}
	if !appPublicIDPattern.MatchString(request.AppPublicID) {
		return Result{}, errors.New("App identifier is invalid")
	}
	if err := domain.ValidateCommitSHA(request.CommitSHA); err != nil {
		return Result{}, err
	}
	dockerfile, err := os.Stat(filepath.Join(request.ContextDirectory, "Dockerfile"))
	if err != nil || !dockerfile.Mode().IsRegular() {
		return Result{}, errors.New("repository must contain a regular Dockerfile at its root")
	}
	metadata, err := os.CreateTemp("", "molejo-build-metadata-*.json")
	if err != nil {
		return Result{}, err
	}
	metadataPath := metadata.Name()
	if err = metadata.Close(); err != nil {
		return Result{}, err
	}
	defer os.Remove(metadataPath)

	repository := strings.TrimRight(r.ImageRepositoryPrefix, "/") + "/" + request.AppPublicID
	tag := repository + ":" + request.CommitSHA
	binary := r.Binary
	if binary == "" {
		binary = "buildctl"
	}
	command := r.Command
	if command == nil {
		command = execCommandRunner{}
	}
	args := []string{"--addr", r.Address}
	if tlsValues[0] != "" {
		args = append(args, "--tlscacert", tlsValues[0], "--tlscert", tlsValues[1], "--tlskey", tlsValues[2])
	}
	args = append(args,
		"build",
		"--frontend", "dockerfile.v0",
		"--local", "context="+request.ContextDirectory,
		"--local", "dockerfile="+request.ContextDirectory,
		"--opt", "filename=Dockerfile",
		"--opt", "platform="+domain.BuildPlatform,
		"--output", "type=image,name="+tag+",push=true",
		"--metadata-file", metadataPath,
	)
	if err = command.Run(ctx, binary, args, output); err != nil {
		return Result{}, fmt.Errorf("BuildKit build failed: %w", err)
	}
	contents, err := os.ReadFile(metadataPath)
	if err != nil {
		return Result{}, fmt.Errorf("read BuildKit metadata: %w", err)
	}
	var values map[string]any
	if err = json.Unmarshal(contents, &values); err != nil {
		return Result{}, errors.New("BuildKit returned invalid metadata")
	}
	digest, _ := values["containerimage.digest"].(string)
	image, err := domain.ReleaseImageReference(repository, digest)
	if err != nil {
		return Result{}, errors.New("BuildKit did not return a valid image digest")
	}
	return Result{Digest: digest, Image: image}, nil
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args []string, output io.Writer) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = output
	command.Stderr = output
	return command.Run()
}
