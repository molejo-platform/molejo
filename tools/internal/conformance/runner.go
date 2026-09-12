package conformance

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var runIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

type RunConfig struct {
	RunID         string
	RunnerVersion string
	Revision      string
	Target        Target
	Profile       Profile
	Client        *Client
	Password      string
	Image         string
	Publication   PublicationConfig
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
	reporter, err := NewReporter(config.OutputDir, config.RunnerVersion, config.Revision, config.RunID, config.Profile, config.Target)
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
			cleanup, cleanupErr := cleanupResources(reporter, config.Client)
			if persistErr := reporter.SetCleanup(cleanup); persistErr != nil {
				cleanupErr = errors.Join(cleanupErr, persistErr)
			}
			_ = reporter.Finish(StatusFail, runErr.Error())
			return reporter.Snapshot(), errors.Join(runErr, cleanupErr)
		}
		if err = reporter.FinishScenario(index, StatusPass, ""); err != nil {
			return reporter.Snapshot(), err
		}
	}

	cleanup, cleanupErr := cleanupResources(reporter, config.Client)
	if err = reporter.SetCleanup(cleanup); err != nil {
		return reporter.Snapshot(), err
	}
	if cleanupErr != nil {
		return reporter.Snapshot(), cleanupErr
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
	if !runIDPattern.MatchString(config.RunID) {
		return errors.New("run ID must contain 1 to 64 lowercase letters, digits, or internal hyphens")
	}
	if config.Target.ClusterID == "" {
		return errors.New("target cluster ID is required")
	}
	if !config.Target.Disposable && config.Target.WorkspaceID == "" {
		return errors.New("a persistent target requires an existing workspace ID")
	}
	if config.Profile.ID == HTTPPublicationProfileID {
		if err := validatePublicationConfig(config.Publication, config.Target); err != nil {
			return err
		}
	}
	return nil
}

func resourceRunSuffix(runID string) string {
	digest := sha256.Sum256([]byte(runID))
	return fmt.Sprintf("%x", digest[:6])
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

func PlanWithConfig(profile Profile, target Target, publication PublicationConfig) ([]string, error) {
	lines, err := Plan(profile, target)
	if err != nil {
		return nil, err
	}
	if profile.ID != HTTPPublicationProfileID {
		return lines, nil
	}
	if err = validatePublicationConfig(publication, target); err != nil {
		return nil, err
	}
	bindingEffect := "read and verify the existing publication binding"
	if publication.ManageBinding {
		bindingEffect = "create and remove the publication binding"
	}
	return append(lines,
		"Publication Gateway: "+publication.GatewayNamespace+"/"+publication.GatewayName,
		fmt.Sprintf("Exact address: %s via listener %s (%s)", publication.ExactHostname, publication.ExactListener, exactListenerHostname(publication)),
		fmt.Sprintf("Pooled address: %s.%s via listener %s (%s)", publication.PoolLabel, publication.PoolDomain, publication.PoolListener, poolListenerHostname(publication)),
		"Binding effect: "+bindingEffect,
		"Effects: create and remove publication domains, grants, and application fixtures",
		"TLS: serve and verify exact and pooled hostnames through the configured Gateway",
	), nil
}

func validatePublicationConfig(publication PublicationConfig, target Target) error {
	if publication.GatewayNamespace == "" || publication.GatewayName == "" || publication.ExactHostname == "" || publication.PoolDomain == "" || publication.PoolLabel == "" || publication.ExactListener == "" || publication.PoolListener == "" || publication.ProbeAddress == "" || publication.CAFile == "" {
		return errors.New("HTTP publication profile requires Gateway, address, listener, probe, and CA options")
	}
	if publication.ManageBinding && !target.Disposable {
		return errors.New("binding management is allowed only on a disposable target")
	}
	return nil
}
