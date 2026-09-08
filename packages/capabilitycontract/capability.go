// Package capabilitycontract defines provider-neutral capability vocabulary
// shared by Molejo binaries. It contains no persistence or provider behavior.
package capabilitycontract

type ID string

const ContractVersion = "v1alpha1"

const (
	RuntimeWorkloadApply        ID = "runtime.workload.apply"
	RuntimeWorkloadObserve      ID = "runtime.workload.observe"
	RuntimeLogsCurrent          ID = "runtime.logs.current"
	RuntimeMetricsCurrent       ID = "runtime.metrics.current"
	RuntimeEventsCurrent        ID = "runtime.events.current"
	TelemetryLogsHistorical     ID = "telemetry.logs.historical"
	TelemetryMetricsHistorical  ID = "telemetry.metrics.historical"
	TelemetryEventsHistorical   ID = "telemetry.events.historical"
	ParametersPlain             ID = "parameters.plain"
	ParametersSecretStatic      ID = "parameters.secret.static"
	PublicationHTTP             ID = "publication.http"
	PublicationTCP              ID = "publication.tcp"
	StorageRWO                  ID = "storage.rwo"
	StorageExpand               ID = "storage.expand"
	SourceGitHub                ID = "source.github"
	BuildManaged                ID = "build.managed"
	ReleaseExternal             ID = "release.external"
	ReleaseHistory              ID = "release.history"
	ControlPlaneOperationEvents ID = "events.control-plane"
)

var knownIDs = map[ID]struct{}{
	RuntimeWorkloadApply: {}, RuntimeWorkloadObserve: {}, RuntimeLogsCurrent: {}, RuntimeMetricsCurrent: {}, RuntimeEventsCurrent: {},
	TelemetryLogsHistorical: {}, TelemetryMetricsHistorical: {}, TelemetryEventsHistorical: {}, ParametersPlain: {}, ParametersSecretStatic: {},
	PublicationHTTP: {}, PublicationTCP: {}, StorageRWO: {}, StorageExpand: {}, SourceGitHub: {}, BuildManaged: {}, ReleaseExternal: {},
	ReleaseHistory: {}, ControlPlaneOperationEvents: {},
}

func Known(id ID) bool {
	_, ok := knownIDs[id]
	return ok
}
