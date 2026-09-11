package conformance

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type RunConfig struct {
	RunID         string
	RunnerVersion string
	Target        Target
	Profile       Profile
	Client        *Client
	Password      string
	Image         string
	OutputDir     string
	Progress      func(string)
}

type ScenarioContext struct {
	Context  context.Context
	Config   RunConfig
	Reporter *Reporter
	Scenario int
}

func (c *ScenarioContext) Assert(id, observed string) error {
	if c.Config.Progress != nil {
		c.Config.Progress(observed)
	}
	return c.Reporter.AddAssertion(c.Scenario, id, observed)
}

func RunProfile(ctx context.Context, config RunConfig) (Report, error) {
	if err := validateRunConfig(config); err != nil {
		return Report{}, err
	}
	reporter, err := NewReporter(config.OutputDir, config.RunnerVersion, config.RunID, config.Profile, config.Target)
	if err != nil {
		return Report{}, err
	}
	if err = config.Client.Login(ctx, config.Password); err != nil {
		_ = reporter.Finish(StatusBlocked, "authentication failed")
		return reporter.Snapshot(), fmt.Errorf("authenticate conformance runner: %w", err)
	}
	defer func() {
		logoutContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = config.Client.Logout(logoutContext)
	}()
	target, err := verifyTarget(ctx, config.Client, config.Target)
	if err != nil {
		_ = reporter.Finish(StatusBlocked, err.Error())
		return reporter.Snapshot(), err
	}
	if err = reporter.SetTarget(target); err != nil {
		return reporter.Snapshot(), err
	}

	for _, scenario := range config.Profile.Scenarios {
		index, startErr := reporter.StartScenario(scenario)
		if startErr != nil {
			return reporter.Snapshot(), startErr
		}
		scenarioCtx, cancel := context.WithTimeout(ctx, scenario.Timeout)
		runErr := scenario.Run(&ScenarioContext{Context: scenarioCtx, Config: config, Reporter: reporter, Scenario: index})
		cancel()
		if runErr != nil {
			_ = reporter.FinishScenario(index, StatusFail, runErr.Error())
			cleanup := cleanupResources(reporter, config.Client)
			_ = reporter.SetCleanup(cleanup)
			_ = reporter.Finish(StatusFail, runErr.Error())
			return reporter.Snapshot(), runErr
		}
		if err = reporter.FinishScenario(index, StatusPass, ""); err != nil {
			return reporter.Snapshot(), err
		}
	}

	cleanup := cleanupResources(reporter, config.Client)
	if err = reporter.SetCleanup(cleanup); err != nil {
		return reporter.Snapshot(), err
	}
	if cleanup.Status != StatusPass {
		err = errors.New("conformance cleanup did not complete")
		_ = reporter.Finish(StatusFail, err.Error())
		return reporter.Snapshot(), err
	}
	if err = reporter.Finish(StatusPass, ""); err != nil {
		return reporter.Snapshot(), err
	}
	return reporter.Snapshot(), nil
}

func validateRunConfig(config RunConfig) error {
	if config.RunID == "" || config.Client == nil || config.Password == "" || config.Image == "" || config.OutputDir == "" {
		return errors.New("run ID, client, password, image, and output directory are required")
	}
	if config.Target.ClusterID == "" {
		return errors.New("target cluster ID is required")
	}
	if !config.Target.Disposable && config.Target.WorkspaceID == "" {
		return errors.New("a persistent target requires an existing workspace ID")
	}
	return nil
}

func Plan(profile Profile, target Target) ([]string, error) {
	if target.ClusterID == "" {
		return nil, errors.New("target cluster ID is required")
	}
	if !target.Disposable && target.WorkspaceID == "" {
		return nil, errors.New("a persistent target requires an existing workspace ID")
	}
	lines := []string{
		fmt.Sprintf("Profile: %s/%s", profile.ID, profile.Version),
		fmt.Sprintf("Target cluster: %s", target.ClusterID),
		fmt.Sprintf("Kubernetes context: %s", target.KubeContext),
		fmt.Sprintf("Disposable target: %t", target.Disposable),
	}
	for _, scenario := range profile.Scenarios {
		lines = append(lines, fmt.Sprintf("Scenario: %s (required=%t, timeout=%s)", scenario.ID, scenario.Required, scenario.Timeout.Round(time.Second)))
	}
	lines = append(lines, "Effects: create and remove conformance-owned application resources")
	return lines, nil
}
