package release

import "fmt"

func releaseNotes(tag, commit string) string {
	return fmt.Sprintf(`Molejo %s is an experimental alpha distribution for validating the Kubernetes application platform baseline.

Included in this alpha:

- provider-neutral capability observations, bindings, and Feature Availability;
- outbound mTLS Cluster Agent pairing and bounded current Kubernetes diagnostics;
- namespaced Workspace placement, RBAC boundaries, and versioned secret delivery;
- Control Plane, Console, Platform Operator, Cluster Agent, and molejoctl distribution;
- disposable Kind conformance and accepted K3s application-loop validation.

Commit: %s

Alpha lifecycle: contracts may change between alpha releases and the supported transition is a clean experimental reinstall. EKS acceptance and optional historical telemetry providers remain deferred.
`, tag, "`"+commit+"`")
}
