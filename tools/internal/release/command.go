package release

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type commandRunner struct {
	directory string
	stdout    io.Writer
	stderr    io.Writer
}

func (r commandRunner) run(ctx context.Context, environment []string, stdin io.Reader, name string, arguments ...string) error {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = r.directory
	command.Env = append(os.Environ(), environment...)
	command.Stdin = stdin
	command.Stdout = r.stdout
	command.Stderr = r.stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run %s: %w", displayCommand(name, arguments), err)
	}
	return nil
}

func (r commandRunner) output(ctx context.Context, name string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = r.directory
	command.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return "", fmt.Errorf("run %s: %w: %s", displayCommand(name, arguments), err, message)
		}
		return "", fmt.Errorf("run %s: %w", displayCommand(name, arguments), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func displayCommand(name string, arguments []string) string {
	return strings.Join(append([]string{name}, arguments...), " ")
}
