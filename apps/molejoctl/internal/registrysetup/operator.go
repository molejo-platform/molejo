package registrysetup

import (
	"context"
	"errors"
	"fmt"
)

type Options struct {
	ContextName  string
	SetupPath    string
	DockerConfig []byte
}

type Operator struct {
	newEnvironment environmentFactory
}

func New() *Operator { return &Operator{newEnvironment: newKubernetesEnvironment} }

type environment interface {
	Discover(context.Context, Setup, []byte) (Facts, error)
	Execute(context.Context, Setup, []byte, Operation) error
	Smoke(context.Context, Setup) (SmokeResult, error)
}

type environmentFactory func(string) (environment, error)

func (o *Operator) Plan(ctx context.Context, options Options) (Report, error) {
	setup, desired, target, err := o.load(options, true)
	if err != nil {
		return Report{}, err
	}
	facts, err := target.Discover(ctx, setup, desired)
	if err != nil {
		return Report{}, err
	}
	plan := BuildPlan(setup, facts)
	if !plan.Valid() {
		return Report{}, diagnosticsError(plan.Diagnostics)
	}
	return Report{Setup: setup, Plan: plan}, nil
}

func (o *Operator) Apply(ctx context.Context, options Options) (Report, error) {
	setup, desired, target, err := o.load(options, true)
	if err != nil {
		return Report{}, err
	}
	changed := false
	for attempt := 0; attempt < 3; attempt++ {
		facts, discoverErr := target.Discover(ctx, setup, desired)
		if discoverErr != nil {
			return Report{}, discoverErr
		}
		plan := BuildPlan(setup, facts)
		if !plan.Valid() {
			return Report{}, diagnosticsError(plan.Diagnostics)
		}
		if plan.Ready {
			return Report{Setup: setup, Plan: plan, Changed: changed}, nil
		}
		for _, operation := range plan.Operations {
			if err = target.Execute(ctx, setup, desired, operation); err != nil {
				return Report{}, fmt.Errorf("execute %s: %w", operation.ID, err)
			}
		}
		changed = true
	}
	return Report{}, errors.New("registry setup did not converge after three planning passes")
}

func (o *Operator) Verify(ctx context.Context, options Options) (Report, error) {
	setup, _, target, err := o.load(options, false)
	if err != nil {
		return Report{}, err
	}
	facts, err := target.Discover(ctx, setup, nil)
	if err != nil {
		return Report{}, err
	}
	if diagnostics := Verify(facts); len(diagnostics) > 0 {
		return Report{}, diagnosticsError(diagnostics)
	}
	return Report{Setup: setup, Plan: Plan{Ready: true}}, nil
}

func (o *Operator) Smoke(ctx context.Context, options Options) (Report, error) {
	setup, _, target, err := o.load(options, false)
	if err != nil {
		return Report{}, err
	}
	facts, err := target.Discover(ctx, setup, nil)
	if err != nil {
		return Report{}, err
	}
	if diagnostics := Verify(facts); len(diagnostics) > 0 {
		return Report{}, diagnosticsError(diagnostics)
	}
	result, err := target.Smoke(ctx, setup)
	if err != nil {
		return Report{}, err
	}
	return Report{Setup: setup, Plan: Plan{Ready: true}, Smoke: result}, nil
}

func (o *Operator) load(options Options, requireCredential bool) (Setup, []byte, environment, error) {
	setup, err := Load(options.SetupPath)
	if err != nil {
		return Setup{}, nil, nil, err
	}
	setup, diagnostics := NormalizeAndValidate(setup)
	if len(diagnostics) > 0 {
		return Setup{}, nil, nil, diagnosticsError(diagnostics)
	}
	var desired []byte
	if requireCredential {
		desired, err = FilterDockerConfig(options.DockerConfig, setup.Spec.Registry.Host)
		if err != nil {
			return Setup{}, nil, nil, err
		}
	}
	target, err := o.newEnvironment(options.ContextName)
	return setup, desired, target, err
}
