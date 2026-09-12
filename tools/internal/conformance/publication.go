package conformance

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type publicationBinding struct {
	ID               string                `json:"id"`
	ClusterID        string                `json:"clusterId"`
	ClusterUID       string                `json:"clusterUid"`
	Revision         int                   `json:"revision"`
	SchemaVersion    string                `json:"schemaVersion"`
	GatewayNamespace string                `json:"gatewayNamespace"`
	GatewayName      string                `json:"gatewayName"`
	Listeners        []publicationListener `json:"listeners"`
	Health           string                `json:"health"`
	ReasonCode       string                `json:"reasonCode"`
}

const publicationBindingSchemaVersion = "kubernetes-http.v1alpha1"

type publicationListener struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
}

type publicationObservation struct {
	State      string                          `json:"state"`
	ReasonCode string                          `json:"reasonCode"`
	Addresses  []publicationAddressObservation `json:"addresses"`
}

type publicationAddressObservation struct {
	Hostname    string                     `json:"hostname"`
	Destination publicationHTTPDestination `json:"destination"`
}

type publicationHTTPDestination struct {
	BindingID        string `json:"bindingId"`
	BindingRevision  int    `json:"bindingRevision"`
	SchemaVersion    string `json:"schemaVersion"`
	GatewayNamespace string `json:"gatewayNamespace"`
	GatewayName      string `json:"gatewayName"`
	SectionName      string `json:"sectionName"`
}

type publicationAppEnvironment struct {
	appEnvironment
	WithdrawalState        string                 `json:"withdrawalState"`
	PublicationObservation publicationObservation `json:"publicationObservation"`
}

type httpsEvidence struct {
	status                int
	body                  string
	certificateSHA256     string
	certificateValidUntil time.Time
}

func runHTTPPublicationJourney(run *ScenarioContext) error {
	ctx := run.Context
	client := run.Config.Client
	publication := run.Config.Publication
	workspace, namespace, err := prepareWorkspace(run)
	if err != nil {
		return err
	}
	if err = run.Assert("workspace-ready", "publication workspace ready in "+namespace); err != nil {
		return err
	}
	if err = run.Reporter.SetOutputs(RunOutputs{WorkspaceID: workspace.ID, Namespace: namespace}); err != nil {
		return err
	}

	binding, err := ensurePublicationBinding(run)
	if err != nil {
		return err
	}
	if err = run.Assert("binding-ready", "publication binding targets the requested Gateway and listeners"); err != nil {
		return err
	}
	if err = run.Reporter.SetOutputs(RunOutputs{
		WorkspaceID: workspace.ID, Namespace: namespace,
		PublicationBindingID: binding.ID, PublicationBindingRevision: binding.Revision,
	}); err != nil {
		return err
	}

	suffix := resourceRunSuffix(run.Config.RunID)
	exactID := "cf-exact-" + suffix
	poolID := "cf-pool-" + suffix
	exact, err := createPublicationDomain(run, exactID, publication.ExactHostname, "Exact")
	if err != nil {
		return err
	}
	pool, err := createPublicationDomain(run, poolID, publication.PoolDomain, "SubdomainPool")
	if err != nil {
		return err
	}
	if err = createPublicationGrant(run, exact.ID, workspace.ID, binding.ID); err != nil {
		return err
	}
	if err = createPublicationGrant(run, pool.ID, workspace.ID, binding.ID); err != nil {
		return err
	}
	if err = run.Assert("domains-granted", "exact and subdomain-pool publication choices granted to the workspace"); err != nil {
		return err
	}

	projectPath := "/api/v1/workspaces/" + workspace.ID + "/projects"
	project, err := createResource(ctx, client, projectPath, "Publication "+suffix)
	if err != nil {
		return err
	}
	if err = registerCleanup(run, "Project", project, projectPath+"/"+project.ID); err != nil {
		return err
	}
	environmentPath := projectPath + "/" + project.ID + "/environments"
	environment, err := createResource(ctx, client, environmentPath, "Conformance")
	if err != nil {
		return err
	}
	if err = registerCleanup(run, "Environment", environment, environmentPath+"/"+environment.ID); err != nil {
		return err
	}
	appPath := projectPath + "/" + project.ID + "/apps"
	app, err := createResource(ctx, client, appPath, "Publication HTTP")
	if err != nil {
		return err
	}
	if err = registerCleanup(run, "App", app, appPath+"/"+app.ID); err != nil {
		return err
	}

	configuration := publicationRuntimeConfiguration(exact.ID, pool.ID, binding.ID, publication, true, false)
	appEnvironmentPath := appPath + "/" + app.ID + "/environments"
	var target publicationAppEnvironment
	if err = client.Post(ctx, appEnvironmentPath, map[string]any{
		"environmentId": environment.ID, "clusterId": run.Config.Target.ClusterID,
		"branch": "main", "workloadKind": "Stateless", "configuration": configuration,
	}, &target, nil, http.StatusCreated); err != nil {
		return fmt.Errorf("create published AppEnvironment: %w", err)
	}
	targetPath := appEnvironmentPath + "/" + target.ID
	withdrawHeaders := map[string]string{"Idempotency-Key": idempotencyKey(run.Config.RunID, "publication-withdraw"), "If-Match": fmt.Sprint(target.Version)}
	if err = run.Reporter.AddResource(ResourceRecord{Kind: "AppEnvironment", ID: target.ID, RunID: run.Config.RunID, CleanupPath: targetPath, Headers: withdrawHeaders, Required: true, State: "created"}); err != nil {
		return err
	}
	if err = run.Reporter.SetOutputs(RunOutputs{
		WorkspaceID: workspace.ID, Namespace: namespace, AppEnvironmentID: target.ID,
		PublicationBindingID: binding.ID, PublicationBindingRevision: binding.Revision,
	}); err != nil {
		return err
	}
	if target.CurrentDeploymentID != nil {
		return errors.New("saving publication configuration applied a deployment implicitly")
	}
	if err = assertPublicationDependents(ctx, client, exact.ID, map[string]bool{"Desired": true}); err != nil {
		return err
	}
	if err = run.Assert("desired-claim", "saving publication configuration reserved the exact hostname without deploying"); err != nil {
		return err
	}

	grantPath := publicationGrantPath(exact.ID, workspace.ID, binding.ID)
	if err = client.Delete(ctx, grantPath, nil, nil, http.StatusConflict); err != nil {
		return fmt.Errorf("expected grant revocation to be blocked: %w", err)
	}
	if err = run.Assert("dependent-protection", "grant revocation was blocked while a desired claim existed"); err != nil {
		return err
	}

	var release resource
	releasePath := appPath + "/" + app.ID + "/releases"
	if err = client.Post(ctx, releasePath, map[string]any{
		"artifact":   map[string]any{"kind": "OCIImage", "reference": run.Config.Image},
		"source":     map[string]any{"provider": "LocalConformance", "repository": "molejo-platform/conformance-http", "revision": "publication"},
		"provenance": map[string]any{"producer": "molejo-conformance"},
	}, &release, map[string]string{"Idempotency-Key": idempotencyKey(run.Config.RunID, "publication-release")}, http.StatusCreated, http.StatusOK); err != nil {
		return fmt.Errorf("register publication release: %w", err)
	}
	var accepted struct {
		Operation operation `json:"operation"`
	}
	if err = client.Post(ctx, targetPath+"/deployments", map[string]any{
		"releaseId": release.ID, "configurationVersion": target.ConfigurationVersion, "currentDeploymentId": nil,
	}, &accepted, map[string]string{"Idempotency-Key": idempotencyKey(run.Config.RunID, "publication-deploy"), "If-Match": fmt.Sprint(target.Version)}, http.StatusAccepted); err != nil {
		return fmt.Errorf("deploy published application: %w", err)
	}
	if err = waitOperation(ctx, client, accepted.Operation.ID); err != nil {
		return err
	}
	target, err = waitPublicationReady(ctx, client, targetPath, binding, map[string]string{
		publication.ExactHostname: publication.ExactListener,
	})
	if err != nil {
		return err
	}
	withdrawHeaders["If-Match"] = fmt.Sprint(target.Version)
	if err = run.Reporter.UpdateResourceHeaders("AppEnvironment", target.ID, withdrawHeaders); err != nil {
		return err
	}
	wantKinds := map[string]bool{"Desired": true, "Applied": true, "Executable": true}
	if err = assertPublicationDependents(ctx, client, exact.ID, wantKinds); err != nil {
		return err
	}
	if _, err = waitHTTPS(ctx, publication, publication.ExactHostname, http.StatusOK, true); err != nil {
		return err
	}
	if err = run.Assert("exact-publication-ready", "the exact address was deployed before adding a second address"); err != nil {
		return err
	}
	if target.CurrentDeploymentID == nil {
		return errors.New("ready exact publication omitted the current deployment identity")
	}
	currentDeploymentID := *target.CurrentDeploymentID

	configuration = publicationRuntimeConfiguration(exact.ID, pool.ID, binding.ID, publication, true, true)
	if err = client.Put(ctx, targetPath, map[string]any{"branch": "main", "configuration": configuration}, &target,
		map[string]string{"If-Match": fmt.Sprint(target.Version)}, http.StatusOK); err != nil {
		return fmt.Errorf("add pooled publication address: %w", err)
	}
	if target.CurrentDeploymentID == nil || *target.CurrentDeploymentID != currentDeploymentID {
		return errors.New("saving the pooled address changed the current deployment implicitly")
	}
	if err = assertPublicationDependents(ctx, client, pool.ID, map[string]bool{"Desired": true}); err != nil {
		return err
	}
	if err = run.Assert("pooled-address-desired", "saving the pooled address reserved it without applying a new deployment"); err != nil {
		return err
	}
	if err = client.Post(ctx, targetPath+"/deployments", map[string]any{
		"releaseId": release.ID, "configurationVersion": target.ConfigurationVersion, "currentDeploymentId": target.CurrentDeploymentID,
	}, &accepted, map[string]string{"Idempotency-Key": idempotencyKey(run.Config.RunID, "publication-add-pool"), "If-Match": fmt.Sprint(target.Version)}, http.StatusAccepted); err != nil {
		return fmt.Errorf("deploy pooled publication address: %w", err)
	}
	if err = waitOperation(ctx, client, accepted.Operation.ID); err != nil {
		return err
	}
	poolHostname := publication.PoolLabel + "." + publication.PoolDomain
	target, err = waitPublicationReady(ctx, client, targetPath, binding, map[string]string{
		publication.ExactHostname: publication.ExactListener,
		poolHostname:              publication.PoolListener,
	})
	if err != nil {
		return err
	}
	withdrawHeaders["If-Match"] = fmt.Sprint(target.Version)
	if err = run.Reporter.UpdateResourceHeaders("AppEnvironment", target.ID, withdrawHeaders); err != nil {
		return err
	}
	for _, domainID := range []string{exact.ID, pool.ID} {
		if err = assertPublicationDependents(ctx, client, domainID, wantKinds); err != nil {
			return err
		}
	}
	if err = run.Assert("publication-executable", "adding the pooled address produced desired, applied, and executable claims through bounded pagination"); err != nil {
		return err
	}

	addresses := []struct {
		hostname string
		listener string
	}{{publication.ExactHostname, publication.ExactListener}, {poolHostname, publication.PoolListener}}
	observations := make([]PublicationEvidence, 0, len(addresses))
	for _, address := range addresses {
		var evidence httpsEvidence
		evidence, err = waitHTTPS(ctx, publication, address.hostname, http.StatusOK, true)
		if err != nil {
			return err
		}
		observations = append(observations, PublicationEvidence{
			Hostname: address.hostname, Listener: address.listener, HTTPStatus: evidence.status,
			CertificateSHA256: evidence.certificateSHA256, CertificateValidUntil: evidence.certificateValidUntil,
		})
	}
	if err = run.Reporter.SetOutputs(RunOutputs{
		WorkspaceID: workspace.ID, Namespace: namespace, AppEnvironmentID: target.ID,
		PublicationBindingID: binding.ID, PublicationBindingRevision: binding.Revision,
		PublicationAddresses: observations,
	}); err != nil {
		return err
	}
	if err = run.Assert("trusted-tls", "exact and pooled hostnames served the same application with matching trusted TLS"); err != nil {
		return err
	}

	configuration = publicationRuntimeConfiguration(exact.ID, pool.ID, binding.ID, publication, false, true)
	if err = client.Put(ctx, targetPath, map[string]any{"branch": "main", "configuration": configuration}, &target,
		map[string]string{"If-Match": fmt.Sprint(target.Version)}, http.StatusOK); err != nil {
		return fmt.Errorf("remove exact publication address: %w", err)
	}
	if target.CurrentDeploymentID == nil {
		return errors.New("partial publication update lost the current deployment identity")
	}
	if err = client.Post(ctx, targetPath+"/deployments", map[string]any{
		"releaseId": release.ID, "configurationVersion": target.ConfigurationVersion, "currentDeploymentId": target.CurrentDeploymentID,
	}, &accepted, map[string]string{"Idempotency-Key": idempotencyKey(run.Config.RunID, "publication-remove-exact"), "If-Match": fmt.Sprint(target.Version)}, http.StatusAccepted); err != nil {
		return fmt.Errorf("deploy removal of exact publication address: %w", err)
	}
	if err = waitOperation(ctx, client, accepted.Operation.ID); err != nil {
		return err
	}
	target, err = waitPublicationReady(ctx, client, targetPath, binding, map[string]string{poolHostname: publication.PoolListener})
	if err != nil {
		return err
	}
	withdrawHeaders["If-Match"] = fmt.Sprint(target.Version)
	if err = run.Reporter.UpdateResourceHeaders("AppEnvironment", target.ID, withdrawHeaders); err != nil {
		return err
	}
	if _, err = waitHTTPS(ctx, publication, publication.ExactHostname, http.StatusNotFound, false); err != nil {
		return err
	}
	if _, err = waitHTTPS(ctx, publication, poolHostname, http.StatusOK, true); err != nil {
		return err
	}
	if err = waitPublicationDependentsEmpty(ctx, client, exact.ID); err != nil {
		return err
	}
	if err = run.Assert("partial-address-removal", "removing the exact address preserved the pooled address and its route"); err != nil {
		return err
	}

	var deletion operation
	if err = client.Delete(ctx, targetPath, &deletion, withdrawHeaders, http.StatusAccepted); err != nil {
		return fmt.Errorf("withdraw published application: %w", err)
	}
	if err = waitOperation(ctx, client, deletion.ID); err != nil {
		return err
	}
	if err = run.Reporter.UpdateResource("AppEnvironment", target.ID, "deleted"); err != nil {
		return err
	}
	for _, hostname := range []string{poolHostname} {
		if _, err = waitHTTPS(ctx, publication, hostname, http.StatusNotFound, false); err != nil {
			return err
		}
	}
	for _, domainID := range []string{exact.ID, pool.ID} {
		if err = waitPublicationDependentsEmpty(ctx, client, domainID); err != nil {
			return err
		}
	}
	if err = run.Assert("withdrawal-complete", "terminal runtime withdrawal was confirmed and published routes and claims were removed"); err != nil {
		return err
	}
	return nil
}

func ensurePublicationBinding(run *ScenarioContext) (publicationBinding, error) {
	ctx := run.Context
	client := run.Config.Client
	publication := run.Config.Publication
	path := "/api/v1/admin/clusters/" + run.Config.Target.ClusterID + "/bindings/publication/http"
	wantListeners := []publicationListener{
		{Name: publication.ExactListener, Hostname: exactListenerHostname(publication)},
		{Name: publication.PoolListener, Hostname: poolListenerHostname(publication)},
	}
	body := map[string]any{
		"schemaVersion": publicationBindingSchemaVersion, "gatewayNamespace": publication.GatewayNamespace,
		"gatewayName": publication.GatewayName, "listeners": wantListeners,
	}
	var binding publicationBinding
	err := client.Get(ctx, path, &binding)
	var status *StatusError
	// Binding configuration is installation state. A disposable run may own its
	// complete lifecycle; a persistent run can only inspect an existing binding.
	if publication.ManageBinding {
		if err == nil {
			return publicationBinding{}, errors.New("refuse to manage a pre-existing publication binding")
		}
		if !errors.As(err, &status) || status.Code != http.StatusNotFound {
			return publicationBinding{}, fmt.Errorf("inspect publication binding: %w", err)
		}
		if err = client.Put(ctx, path, body, &binding, nil, http.StatusCreated); err != nil {
			return publicationBinding{}, fmt.Errorf("create publication binding: %w", err)
		}
		if err = run.Reporter.AddResource(ResourceRecord{Kind: "PublicationBinding", ID: binding.ID, RunID: run.Config.RunID, CleanupPath: path, Headers: map[string]string{"If-Match": fmt.Sprint(binding.Revision)}, Required: true, State: "created"}); err != nil {
			return publicationBinding{}, err
		}
	} else if err != nil {
		return publicationBinding{}, fmt.Errorf("read existing publication binding: %w", err)
	}
	if binding.ID == "" || binding.Revision < 1 || binding.SchemaVersion != publicationBindingSchemaVersion || binding.ClusterID != run.Config.Target.ClusterID || binding.GatewayNamespace != publication.GatewayNamespace || binding.GatewayName != publication.GatewayName || !sameListeners(binding.Listeners, wantListeners) {
		return publicationBinding{}, errors.New("publication binding differs from the requested target and listeners")
	}
	if run.Config.Target.ExpectedClusterUID != "" && binding.ClusterUID != run.Config.Target.ExpectedClusterUID {
		return publicationBinding{}, errors.New("publication binding belongs to a different Kubernetes cluster incarnation")
	}
	return waitPublicationBindingHealthy(ctx, client, path, binding)
}

func waitPublicationBindingHealthy(ctx context.Context, client *Client, path string, expected publicationBinding) (publicationBinding, error) {
	var binding publicationBinding
	err := await(ctx, time.Second, "publication binding health", func(ctx context.Context) (bool, error) {
		if err := client.Get(ctx, path, &binding); err != nil {
			return false, retryUnavailable(err)
		}
		if !samePublicationBindingIdentity(binding, expected) {
			return false, errors.New("publication binding identity changed while waiting for health")
		}
		return binding.Health == "Healthy", nil
	})
	return binding, err
}

func samePublicationBindingIdentity(left, right publicationBinding) bool {
	return left.ID == right.ID && left.ClusterID == right.ClusterID && left.ClusterUID == right.ClusterUID &&
		left.Revision == right.Revision && left.SchemaVersion == right.SchemaVersion &&
		left.GatewayNamespace == right.GatewayNamespace && left.GatewayName == right.GatewayName &&
		sameListeners(left.Listeners, right.Listeners)
}

func createPublicationDomain(run *ScenarioContext, id, name, kind string) (resource, error) {
	path := "/api/v1/admin/publication/domains/" + id
	var domain resource
	if err := run.Config.Client.Put(run.Context, path, map[string]any{"name": name, "kind": kind, "reservedNames": []string{}}, &domain, nil, http.StatusCreated); err != nil {
		return resource{}, fmt.Errorf("create publication domain %s: %w", id, err)
	}
	if err := run.Reporter.AddResource(ResourceRecord{Kind: "PublicationDomain", ID: domain.ID, RunID: run.Config.RunID, CleanupPath: path, Headers: map[string]string{"If-Match": fmt.Sprint(domain.Version)}, Required: true, State: "created"}); err != nil {
		return resource{}, err
	}
	return domain, nil
}

func createPublicationGrant(run *ScenarioContext, domainID, workspaceID, bindingID string) error {
	path := publicationGrantPath(domainID, workspaceID, bindingID)
	if err := run.Config.Client.Put(run.Context, path, nil, nil, nil, http.StatusNoContent); err != nil {
		return fmt.Errorf("create publication grant: %w", err)
	}
	return run.Reporter.AddResource(ResourceRecord{Kind: "PublicationGrant", ID: domainID + "/" + workspaceID + "/" + bindingID, RunID: run.Config.RunID, CleanupPath: path, Required: true, State: "created"})
}

func publicationGrantPath(domainID, workspaceID, bindingID string) string {
	return "/api/v1/admin/publication/domains/" + domainID + "/grants/" + workspaceID + "/" + bindingID
}

func publicationRuntimeConfiguration(exactID, poolID, bindingID string, config PublicationConfig, includeExact, includePool bool) map[string]any {
	configuration := runtimeConfiguration()
	addresses := []map[string]any{}
	if includeExact {
		addresses = append(addresses, map[string]any{"domainId": exactID, "bindingId": bindingID, "listenerName": config.ExactListener})
	}
	if includePool {
		addresses = append(addresses, map[string]any{"domainId": poolID, "bindingId": bindingID, "label": config.PoolLabel, "listenerName": config.PoolListener})
	}
	configuration["publicEndpoints"] = []map[string]any{{"name": "web", "type": "HTTP", "portName": "http", "addresses": addresses}}
	return configuration
}

func waitPublicationReady(ctx context.Context, client *Client, path string, binding publicationBinding, addresses map[string]string) (publicationAppEnvironment, error) {
	var target publicationAppEnvironment
	err := await(ctx, time.Second, "HTTP publication readiness", func(ctx context.Context) (bool, error) {
		if err := client.Get(ctx, path, &target); err != nil {
			return false, retryUnavailable(err)
		}
		if target.State == "Degraded" || target.PublicationObservation.State == "Degraded" {
			return false, fmt.Errorf("published AppEnvironment became degraded: %s", target.PublicationObservation.ReasonCode)
		}
		return target.State == "Ready" && target.PublicationObservation.State == "Ready" && samePublicationAddresses(target.PublicationObservation.Addresses, binding, addresses), nil
	})
	return target, err
}

func assertPublicationDependents(ctx context.Context, client *Client, domainID string, want map[string]bool) error {
	found := map[string]bool{}
	cursor := ""
	completed := false
	// The scenario intentionally requests the smallest server page. The local
	// page cap prevents a broken cursor from turning conformance into an overload.
	for page := 0; page < 10; page++ {
		path := "/api/v1/admin/publication/dependents?domainId=" + url.QueryEscape(domainID) + "&limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		var response struct {
			Items []struct {
				Kind string `json:"kind"`
			} `json:"items"`
			HasMore    bool    `json:"hasMore"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := client.Get(ctx, path, &response); err != nil {
			return fmt.Errorf("list publication dependents: %w", err)
		}
		for _, item := range response.Items {
			found[item.Kind] = true
		}
		if !response.HasMore {
			completed = true
			break
		}
		if response.NextCursor == nil || *response.NextCursor == "" {
			return errors.New("publication dependents pagination omitted next cursor")
		}
		cursor = *response.NextCursor
	}
	if !completed {
		return errors.New("publication dependents exceeded the ten-page conformance safety bound")
	}
	for kind := range want {
		if !found[kind] {
			keys := make([]string, 0, len(found))
			for observed := range found {
				keys = append(keys, observed)
			}
			sort.Strings(keys)
			return fmt.Errorf("publication dependent %s not found; observed %v", kind, keys)
		}
	}
	for kind := range found {
		if !want[kind] {
			return fmt.Errorf("unexpected publication dependent %s; expected only the requested claim states", kind)
		}
	}
	return nil
}

func waitPublicationDependentsEmpty(ctx context.Context, client *Client, domainID string) error {
	return await(ctx, time.Second, "publication claims removal", func(ctx context.Context) (bool, error) {
		var response struct {
			Items []any `json:"items"`
		}
		err := client.Get(ctx, "/api/v1/admin/publication/dependents?domainId="+url.QueryEscape(domainID)+"&limit=1", &response)
		if err != nil {
			return false, retryUnavailable(err)
		}
		return len(response.Items) == 0, nil
	})
}

func waitHTTPS(ctx context.Context, config PublicationConfig, hostname string, status int, requireMarker bool) (httpsEvidence, error) {
	var evidence httpsEvidence
	var lastProbeError error
	err := await(ctx, 500*time.Millisecond, "HTTPS "+hostname, func(ctx context.Context) (bool, error) {
		observed, err := probeHTTPS(ctx, config, hostname)
		if err != nil {
			lastProbeError = err
			return false, nil
		}
		lastProbeError = nil
		evidence = observed
		if observed.status != status {
			return false, nil
		}
		return !requireMarker || strings.Contains(observed.body, "molejo conformance"), nil
	})
	if err != nil && lastProbeError != nil {
		err = fmt.Errorf("%w; last probe failed: %v", err, lastProbeError)
	}
	return evidence, err
}

func probeHTTPS(ctx context.Context, config PublicationConfig, hostname string) (httpsEvidence, error) {
	certificate, err := os.ReadFile(config.CAFile)
	if err != nil {
		return httpsEvidence{}, fmt.Errorf("read publication CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		return httpsEvidence{}, errors.New("publication CA contains no certificates")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, config.ProbeAddress)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   3 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+hostname+"/", nil)
	if err != nil {
		return httpsEvidence{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return httpsEvidence{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return httpsEvidence{}, err
	}
	if response.TLS == nil || len(response.TLS.PeerCertificates) == 0 {
		return httpsEvidence{}, errors.New("HTTPS response omitted the peer certificate")
	}
	leaf := response.TLS.PeerCertificates[0]
	return httpsEvidence{
		status: response.StatusCode, body: string(body),
		certificateSHA256:     fmt.Sprintf("%x", sha256.Sum256(leaf.Raw)),
		certificateValidUntil: leaf.NotAfter.UTC(),
	}, nil
}

func sameListeners(left, right []publicationListener) bool {
	if len(left) != len(right) {
		return false
	}
	want := map[string]string{}
	for _, listener := range right {
		if _, duplicate := want[listener.Name]; duplicate {
			return false
		}
		want[listener.Name] = listener.Hostname
	}
	seen := map[string]bool{}
	for _, listener := range left {
		if seen[listener.Name] || want[listener.Name] != listener.Hostname {
			return false
		}
		seen[listener.Name] = true
	}
	return true
}

func samePublicationAddresses(observed []publicationAddressObservation, binding publicationBinding, addresses map[string]string) bool {
	if len(observed) != len(addresses) {
		return false
	}
	want := make(map[string]string, len(addresses))
	for hostname, listener := range addresses {
		want[hostname] = listener
	}
	for _, address := range observed {
		listener, exists := want[address.Hostname]
		destination := address.Destination
		if !exists || destination.BindingID != binding.ID || destination.BindingRevision != binding.Revision ||
			destination.SchemaVersion != binding.SchemaVersion || destination.GatewayNamespace != binding.GatewayNamespace ||
			destination.GatewayName != binding.GatewayName || destination.SectionName != listener {
			return false
		}
		delete(want, address.Hostname)
	}
	return len(want) == 0
}

func exactListenerHostname(config PublicationConfig) string {
	if config.ExactListenerHostname != "" {
		return config.ExactListenerHostname
	}
	return config.ExactHostname
}

func poolListenerHostname(config PublicationConfig) string {
	if config.PoolListenerHostname != "" {
		return config.PoolListenerHostname
	}
	return "*." + config.PoolDomain
}
