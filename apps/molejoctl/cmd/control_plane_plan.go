package cmd

import "errors"

type controlPlaneObservedState struct {
	databaseCredentials bool
	bootstrapIdentity   bool
	agentCA             bool
	serverIdentity      bool
	databasePVC         bool
	releaseInstalled    bool
	agentPaired         bool
}

type controlPlaneInstallPlan struct {
	createDatabaseCredentials bool
	createBootstrapIdentity   bool
	createAgentCA             bool
	createServerIdentity      bool
	configureAgent            bool
	installChart              bool
}

func buildControlPlaneInstallPlan(observed controlPlaneObservedState) (controlPlaneInstallPlan, error) {
	if observed.databasePVC && !observed.databaseCredentials {
		return controlPlaneInstallPlan{}, errors.New("PostgreSQL PVC exists without its credential Secret")
	}
	if observed.releaseInstalled && (!observed.databaseCredentials || !observed.bootstrapIdentity || !observed.agentCA || !observed.serverIdentity) {
		return controlPlaneInstallPlan{}, errors.New("control plane release exists without its required Secrets")
	}
	return controlPlaneInstallPlan{
		createDatabaseCredentials: !observed.databaseCredentials,
		createBootstrapIdentity:   !observed.bootstrapIdentity,
		createAgentCA:             !observed.agentCA,
		createServerIdentity:      !observed.serverIdentity,
		configureAgent:            !observed.agentPaired,
		installChart:              !observed.releaseInstalled,
	}, nil
}
