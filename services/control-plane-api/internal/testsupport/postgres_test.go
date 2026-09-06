package testsupport

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSuiteRunnerUsesAnExternalDatabaseWithoutStartingAContainer(t *testing.T) {
	t.Setenv(testDatabaseURLEnv, "postgres://external.example/molejo")
	t.Setenv(testcontainersEnv, "true")
	started := false
	runner := suiteRunner{
		start: func(context.Context) (postgresEnvironment, error) {
			started = true
			return postgresEnvironment{}, nil
		},
		stderr: &bytes.Buffer{},
	}

	exitCode := runner.run(func() int { return 7 })

	if started {
		t.Fatal("container was started despite the external database override")
	}
	if exitCode != 7 {
		t.Fatalf("exit code = %d, want 7", exitCode)
	}
}

func TestSuiteRunnerSkipsContainerWhenIntegrationIsDisabled(t *testing.T) {
	t.Setenv(testDatabaseURLEnv, "")
	t.Setenv(testcontainersEnv, "false")
	started := false
	runner := suiteRunner{
		start: func(context.Context) (postgresEnvironment, error) {
			started = true
			return postgresEnvironment{}, nil
		},
		stderr: &bytes.Buffer{},
	}

	exitCode := runner.run(func() int { return 0 })

	if started {
		t.Fatal("container was started while integration was disabled")
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
}

func TestSuiteRunnerProvidesAndCleansUpTheContainerDatabase(t *testing.T) {
	t.Setenv(testDatabaseURLEnv, "")
	t.Setenv(testcontainersEnv, "true")
	terminated := false
	runner := suiteRunner{
		start: func(context.Context) (postgresEnvironment, error) {
			return postgresEnvironment{
				databaseURL: "postgres://container.example/molejo",
				terminate: func(context.Context) error {
					terminated = true
					return nil
				},
			}, nil
		},
		stderr: &bytes.Buffer{},
	}

	exitCode := runner.run(func() int {
		if got := osDatabaseURL(); got != "postgres://container.example/molejo" {
			t.Fatalf("database URL during tests = %q", got)
		}
		return 0
	})

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if !terminated {
		t.Fatal("container was not terminated")
	}
	if got := osDatabaseURL(); got != "" {
		t.Fatalf("database URL after tests = %q, want empty", got)
	}
}

func TestSuiteRunnerReportsStartupAndCleanupFailures(t *testing.T) {
	t.Run("startup", func(t *testing.T) {
		t.Setenv(testDatabaseURLEnv, "")
		t.Setenv(testcontainersEnv, "true")
		output := &bytes.Buffer{}
		runner := suiteRunner{
			start: func(context.Context) (postgresEnvironment, error) {
				return postgresEnvironment{}, errors.New("docker unavailable")
			},
			stderr: output,
		}

		if exitCode := runner.run(func() int { return 0 }); exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
		if !strings.Contains(output.String(), "start PostgreSQL test container") {
			t.Fatalf("startup error = %q", output.String())
		}
	})

	t.Run("cleanup", func(t *testing.T) {
		t.Setenv(testDatabaseURLEnv, "")
		t.Setenv(testcontainersEnv, "true")
		output := &bytes.Buffer{}
		runner := suiteRunner{
			start: func(context.Context) (postgresEnvironment, error) {
				return postgresEnvironment{
					databaseURL: "postgres://container.example/molejo",
					terminate:   func(context.Context) error { return errors.New("cleanup failed") },
				}, nil
			},
			stderr: output,
		}

		if exitCode := runner.run(func() int { return 0 }); exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
		if !strings.Contains(output.String(), "terminate PostgreSQL test container") {
			t.Fatalf("cleanup error = %q", output.String())
		}
	})
}

func TestSuiteRunnerPreservesATestFailureWhenCleanupFails(t *testing.T) {
	t.Setenv(testDatabaseURLEnv, "")
	t.Setenv(testcontainersEnv, "true")
	runner := suiteRunner{
		start: func(context.Context) (postgresEnvironment, error) {
			return postgresEnvironment{
				databaseURL: "postgres://container.example/molejo",
				terminate:   func(context.Context) error { return errors.New("cleanup failed") },
			}, nil
		},
		stderr: &bytes.Buffer{},
	}

	if exitCode := runner.run(func() int { return 9 }); exitCode != 9 {
		t.Fatalf("exit code = %d, want 9", exitCode)
	}
}

func osDatabaseURL() string {
	return strings.TrimSpace(os.Getenv(testDatabaseURLEnv))
}
