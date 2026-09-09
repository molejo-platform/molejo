package featureavailability

import "github.com/molejo-platform/molejo/packages/capabilitycontract"

var catalog = []capabilitycontract.ID{
	capabilitycontract.BuildManaged,
	capabilitycontract.ControlPlaneOperationEvents,
	capabilitycontract.ParametersPlain,
	capabilitycontract.ParametersSecretStatic,
	capabilitycontract.PublicationHTTP,
	capabilitycontract.PublicationTCP,
	capabilitycontract.ReleaseExternal,
	capabilitycontract.ReleaseHistory,
	capabilitycontract.RuntimeEventsCurrent,
	capabilitycontract.RuntimeLogsCurrent,
	capabilitycontract.RuntimeMetricsCurrent,
	capabilitycontract.RuntimeWorkloadApply,
	capabilitycontract.RuntimeWorkloadObserve,
	capabilitycontract.SourceGitHub,
	capabilitycontract.StorageExpand,
	capabilitycontract.StorageRWO,
	capabilitycontract.TelemetryEventsHistorical,
	capabilitycontract.TelemetryLogsHistorical,
	capabilitycontract.TelemetryMetricsHistorical,
	capabilitycontract.WorkspaceProvisioning,
}
