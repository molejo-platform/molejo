package controlplaneinstall

import "errors"

type controlPlaneObservedState struct {
	databaseCredentials bool
	bootstrapIdentity   bool
	agentCA             bool
	serverCA            bool
	serverCATrustUsed   bool
	serverIdentity      bool
	databasePVC         bool
	releaseInstalled    bool
	agentPaired         bool
}

type controlPlaneInstallPlan struct {
	createDatabaseCredentials bool
	createBootstrapIdentity   bool
	createAgentCA             bool
	createServerCA            bool
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
	if observed.releaseInstalled && observed.serverCATrustUsed && !observed.serverCA {
		return controlPlaneInstallPlan{}, errors.New("control plane server CA is missing; restore its Secret from backup before retrying")
	}
	return controlPlaneInstallPlan{
		createDatabaseCredentials: !observed.databaseCredentials,
		createBootstrapIdentity:   !observed.bootstrapIdentity,
		createAgentCA:             !observed.agentCA,
		createServerCA:            !observed.serverCA,
		createServerIdentity:      !observed.serverIdentity,
		configureAgent:            !observed.agentPaired,
		installChart:              !observed.releaseInstalled,
	}, nil
}
