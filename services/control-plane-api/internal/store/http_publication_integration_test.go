package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/packages/runtimecontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func TestPublicationCatalogUsesBoundedKeysetPagesAndPointGrantReads(t *testing.T) {
	ctx := t.Context()
	s, wid, actor := newIntegrationFixture(t)
	for i := range 101 {
		id := fmt.Sprintf("catalog-%03d", i)
		name := fmt.Sprintf("catalog-%03d.example.test", i)
		if _, err := s.Pool.Exec(ctx, `INSERT INTO publication_domains(id,name,kind,reserved_names,created_by,updated_by) VALUES($1,$2,'Exact','[]',$3,$3)`, id, name, actor); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.PublicationDomains(ctx, "", 50)
	if err != nil || len(first) != 51 {
		t.Fatalf("first keyset page: %d %v", len(first), err)
	}
	first = first[:50]
	if _, err = s.Pool.Exec(ctx, `INSERT INTO publication_domains(id,name,kind,reserved_names,created_by,updated_by) VALUES('catalog-025a','catalog-025a.example.test','Exact','[]',$1,$1)`, actor); err != nil {
		t.Fatal(err)
	}
	second, err := s.PublicationDomains(ctx, first[len(first)-1].ID, 50)
	if err != nil || len(second) != 51 || second[0].ID <= first[len(first)-1].ID {
		t.Fatalf("second keyset page: %+v %v", second, err)
	}
	var workspaceID, bindingID string
	if err = s.Pool.QueryRow(ctx, `SELECT w.public_id,g.binding_id FROM publication_grants g JOIN workspaces w ON w.id=g.workspace_id WHERE g.workspace_id=$1 LIMIT 1`, wid).Scan(&workspaceID, &bindingID); err != nil {
		t.Fatal(err)
	}
	grant, err := s.PublicationGrant(ctx, "default", workspaceID, bindingID)
	if err != nil || grant.WorkspaceID != workspaceID || grant.BindingID != bindingID {
		t.Fatalf("point grant read: %+v %v", grant, err)
	}
}

func TestHTTPPublicationReservationsSurviveSaveSupersessionAndWithdrawal(t *testing.T) {
	ctx := t.Context()
	s, wid, actor := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, s, wid)
	original := integrationConfiguration("first")
	original.PublicEndpoints[0].Addresses = append(original.PublicEndpoints[0].Addresses, domain.HTTPAssociation{DomainID: "default", BindingID: "pbd-test", Label: "second"})
	target, err := s.CreateAppEnvironment(ctx, wid, actor, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", original)
	if err != nil {
		t.Fatal(err)
	}
	// Resolution must not mutate caller-owned slices, nor turn a replay into a new revision.
	if original.PublicEndpoints[0].Addresses[0].Hostname != "" {
		t.Fatal("mutated caller configuration")
	}
	same, err := s.UpdateAppEnvironment(ctx, wid, actor, target.PublicID, "main", original, target.Version)
	if err != nil || same.Version != target.Version {
		t.Fatalf("save replay: %+v %v", same, err)
	}
	release, image := createRelease(t, s, wid, actor, project, app, target)
	dep, _, _, err := s.CreateDeployment(ctx, actor, domain.DeploymentRequest{WorkspaceID: wid, AppEnvironmentPublicID: target.PublicID, DeploymentPublicID: newID(t, "dpl"), ReleasePublicID: release, ConfigurationVersion: target.ConfigurationVersion, ExpectedVersion: target.Version, IdempotencyHash: domain.SHA256([]byte("first")), PayloadHash: domain.SHA256([]byte("first"))}, deploymentAudit(t))
	if err != nil {
		t.Fatal(err)
	}
	apply, _, _, ok, err := s.ClaimNext(ctx, "publication-test", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: %v", err)
	}
	if err = s.RegisterAttempt(ctx, apply, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	edited, err := s.UpdateAppEnvironment(ctx, wid, actor, target.PublicID, "main", integrationConfiguration("third"), target.Version+1)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := s.PublicationDependents(ctx, "default", "", "", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, d := range deps {
		counts[d.Kind]++
	}
	if counts["Desired"] != 1 || counts["Executable"] != 2 {
		t.Fatalf("lost executable reservations: %+v", deps)
	}
	snapshot, err := s.PublicationSnapshotForDeployment(ctx, dep.ID)
	if err != nil || len(snapshot.Addresses) != 2 || snapshot.Addresses[0].Hostname != "first.molejo.dev" {
		t.Fatalf("snapshot mutated: %+v %v", snapshot, err)
	}
	var cluster string
	if err = s.Pool.QueryRow(ctx, `SELECT public_id FROM agent_installations WHERE id=$1`, target.ClusterID).Scan(&cluster); err != nil {
		t.Fatal(err)
	}
	binding, err := s.ClusterPublicationBinding(ctx, cluster)
	if err != nil {
		t.Fatal(err)
	}
	revision := binding.Revision
	binding.Listeners = append(binding.Listeners, kubernetesbinding.HTTPListener{Name: "internal", Hostname: "service.internal"})
	updatedBinding, err := s.PutClusterPublicationBinding(ctx, cluster, binding.HTTPBinding, actor, &revision, deploymentAudit(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicationSnapshotForDeployment(ctx, dep.ID); err != nil {
		t.Fatalf("additive binding broke snapshot: %v", err)
	}
	changed := updatedBinding.HTTPBinding
	changed.GatewayName = "another"
	revision = changed.Revision
	if _, err = s.PutClusterPublicationBinding(ctx, cluster, changed, actor, &revision, deploymentAudit(t)); !errors.Is(err, ErrPublicationDependency) {
		t.Fatalf("destructive update accepted: %v", err)
	}
	var workspace string
	if err = s.Pool.QueryRow(ctx, `SELECT public_id FROM workspaces WHERE id=$1`, wid).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	if err = s.SetPublicationGrant(ctx, "default", workspace, "pbd-test", actor, true, deploymentAudit(t)); !errors.Is(err, ErrPublicationDependency) {
		t.Fatalf("revoked live grant: %v", err)
	}
	policy := AdministrativeDomain{Name: "molejo.dev", Kind: domain.PublicationPool, ReservedNames: []string{"unused.molejo.dev"}}
	v := int64(1)
	updatedPolicy, err := s.PutPublicationDomain(ctx, "default", policy, actor, &v, deploymentAudit(t))
	if err != nil {
		t.Fatalf("unrelated pool reservation blocked: %v", err)
	}
	policy.ReservedNames = append(policy.ReservedNames, "third.molejo.dev")
	v = updatedPolicy.Version
	if _, err = s.PutPublicationDomain(ctx, "default", policy, actor, &v, deploymentAudit(t)); !errors.Is(err, ErrPublicationDependency) {
		t.Fatalf("active name invalidated: %v", err)
	}
	remove, err := s.DeleteAppEnvironment(ctx, wid, actor, target.PublicID, edited.Version, domain.SHA256([]byte("remove")), domain.SHA256([]byte("remove")))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteDeployment(ctx, apply, "late", image, strings.Repeat("a", 64)); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("late completion accepted: %v", err)
	}
	if err = s.FinishAttempt(ctx, cluster, apply.PublicID, apply.FencingToken, AttemptResult{RuntimeUID: "test-runtime"}); err != nil {
		t.Fatal(err)
	}
	deps, err = s.PublicationDependents(ctx, "default", "", "", "", "", 100)
	if err != nil || len(deps) != 3 {
		t.Fatalf("late ACK released claims: %+v %v", deps, err)
	}
	claimed, _, _, ok, err := s.ClaimNext(ctx, "withdrawal-test", time.Minute)
	if err != nil || !ok || claimed.PublicID != remove.PublicID {
		t.Fatalf("withdrawal dispatch: %+v %v", claimed, err)
	}
	if err = s.CompleteAppEnvironmentDeletion(ctx, claimed, "unconfirmed"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("skipped withdrawal FSM: %v", err)
	}
	if err = s.RegisterAttempt(ctx, claimed, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishAttempt(ctx, cluster, claimed.PublicID, claimed.FencingToken, AttemptResult{RuntimeUID: "test-runtime", WithdrawalConfirmed: true}); err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteAppEnvironmentDeletion(ctx, claimed, "terminal barrier and child removal confirmed"); err != nil {
		t.Fatal(err)
	}
	deps, err = s.PublicationDependents(ctx, "default", "", "", "", "", 100)
	if err != nil || len(deps) != 0 {
		t.Fatalf("claims after confirmed withdrawal: %+v %v", deps, err)
	}
	if err = s.SetPublicationGrant(ctx, "default", workspace, "pbd-test", actor, true, deploymentAudit(t)); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPPublicationConcurrentOverlappingSetsAreAtomic(t *testing.T) {
	s, wid, actor := newIntegrationFixture(t)
	project, first, environment := createHierarchy(t, s, wid)
	second, err := s.CreateApp(t.Context(), wid, project.PublicID, newID(t, "app"), "Second", "second")
	if err != nil {
		t.Fatal(err)
	}
	configs := []domain.RuntimeConfig{integrationConfiguration("unique-a"), integrationConfiguration("unique-b")}
	for i := range configs {
		configs[i].PublicEndpoints[0].Addresses = append(configs[i].PublicEndpoints[0].Addresses, domain.HTTPAssociation{DomainID: "default", BindingID: "pbd-test", Label: "shared"})
	}
	apps := []domain.App{first, second}
	ids := []string{newID(t, "aev"), newID(t, "aev")}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range configs {
		wg.Go(func() {
			<-start
			_, err := s.CreateAppEnvironment(t.Context(), wid, actor, ids[i], project.PublicID, apps[i].PublicID, environment.PublicID, "main", configs[i])
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrPublicationConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	var claims, revisions, environments int
	if err = s.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM publication_claims),(SELECT count(*) FROM app_environment_configuration_revisions),(SELECT count(*) FROM app_environments)`).Scan(&claims, &revisions, &environments); err != nil {
		t.Fatal(err)
	}
	if claims != 2 || revisions != 1 || environments != 1 {
		t.Fatalf("partial transaction: claims=%d revisions=%d environments=%d", claims, revisions, environments)
	}
}

func TestHTTPBindingObservationsCannotRefreshOlderRevisionOrSample(t *testing.T) {
	s, _, actor := newIntegrationFixture(t)
	ctx := t.Context()
	var cluster string
	if err := s.Pool.QueryRow(ctx, `SELECT public_id FROM agent_installations LIMIT 1`).Scan(&cluster); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE agent_installations SET control_session_sequence=7`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sample := kubernetesbinding.Observation{ID: "pbd-test:https-molejo", Kind: kubernetesbinding.KindPublicationHTTP, Version: 1, Health: kubernetesbinding.HealthHealthy, SampledAt: now, Publication: &kubernetesbinding.PublicationObservation{GatewayNamespace: "molejo-system", GatewayName: "molejo", SectionName: "https-molejo", GatewayClassName: "test", GatewayClassAccepted: true, GatewayProgrammed: true, ListenerReady: true, SupportedRouteKinds: []string{"HTTPRoute"}}}
	if err := s.ReconcileBindingObservations(ctx, cluster, "test-session", 7, []kubernetesbinding.Observation{sample}, true, now); err != nil {
		t.Fatal(err)
	}
	sample.SampledAt = now.Add(-time.Minute)
	sample.Health = kubernetesbinding.HealthDegraded
	sample.ReasonCode = "older_failure"
	if err := s.ReconcileBindingObservations(ctx, cluster, "test-session", 7, []kubernetesbinding.Observation{sample}, true, now); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := s.Pool.QueryRow(ctx, `SELECT observation FROM cluster_publication_bindings`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "older_failure") {
		t.Fatal("older sample replaced fresh evidence")
	}
	binding, err := s.ClusterPublicationBinding(ctx, cluster)
	if err != nil {
		t.Fatal(err)
	}
	v := binding.Revision
	if _, err = s.PutClusterPublicationBinding(ctx, cluster, binding.HTTPBinding, actor, &v, deploymentAudit(t)); err != nil {
		t.Fatal(err)
	}
	sample.SampledAt = now
	if err = s.ReconcileBindingObservations(ctx, cluster, "test-session", 7, []kubernetesbinding.Observation{sample}, true, now); err != nil {
		t.Fatal(err)
	}
	if err = s.Pool.QueryRow(ctx, `SELECT observation FROM cluster_publication_bindings`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var facts map[string]any
	if err = json.Unmarshal(raw, &facts); err != nil || len(facts) != 0 {
		t.Fatalf("old revision accepted: %s %v", raw, err)
	}
}

func TestPublicationObservationAggregateDoesNotClaimTLS(t *testing.T) {
	observation := runtimecontract.PublicationObservation{ObservedAt: time.Now(), Addresses: []runtimecontract.PublicationAddressObservation{{Conditions: []runtimecontract.PublicationCondition{{Type: "RouteReady", Status: "True"}, {Type: "GatewayReady", Status: "False", Reason: "GatewayNotReady"}}}}}
	observation.SetAggregate(time.Now())
	if observation.State != "Degraded" {
		t.Fatalf("aggregate=%s", observation.State)
	}
	observation.SetAggregate(time.Now().Add(time.Hour))
	if observation.State != "Unknown" {
		t.Fatal("stale evidence established readiness")
	}
}

func TestHTTPGrantRevocationSerializesWithReservation(t *testing.T) {
	s, wid, actor := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, s, wid)
	ctx := t.Context()
	var workspace string
	if err := s.Pool.QueryRow(ctx, `SELECT public_id FROM workspaces WHERE id=$1`, wid).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	var createErr, revokeErr error
	start := make(chan struct{})
	var wg sync.WaitGroup
	id := newID(t, "aev")
	event := deploymentAudit(t)
	wg.Go(func() {
		<-start
		_, createErr = s.CreateAppEnvironment(ctx, wid, actor, id, project.PublicID, app.PublicID, environment.PublicID, "main", integrationConfiguration("race"))
	})
	wg.Go(func() {
		<-start
		revokeErr = s.SetPublicationGrant(ctx, "default", workspace, "pbd-test", actor, true, event)
	})
	close(start)
	wg.Wait()
	if createErr == nil {
		if !errors.Is(revokeErr, ErrPublicationDependency) {
			t.Fatalf("grant removed after reservation: %v", revokeErr)
		}
	} else if revokeErr != nil || !errors.Is(createErr, domain.ErrPublicationNotGranted) {
		t.Fatalf("create=%v revoke=%v", createErr, revokeErr)
	}
}

func TestHTTPBindingRecreationRejectsPreviousIncarnationObservation(t *testing.T) {
	s, wid, actor := newIntegrationFixture(t)
	ctx := t.Context()
	var workspace, cluster string
	if err := s.Pool.QueryRow(ctx, `SELECT w.public_id,i.public_id FROM workspaces w JOIN workspace_clusters wc ON wc.workspace_id=w.id JOIN agent_installations i ON i.id=wc.installation_id WHERE w.id=$1`, wid).Scan(&workspace, &cluster); err != nil {
		t.Fatal(err)
	}
	old, err := s.ClusterPublicationBinding(ctx, cluster)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"default", "stateful"} {
		if err = s.SetPublicationGrant(ctx, id, workspace, old.ID, actor, true, deploymentAudit(t)); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.DeleteClusterPublicationBinding(ctx, cluster, actor, old.Revision, deploymentAudit(t)); err != nil {
		t.Fatal(err)
	}
	created, err := s.PutClusterPublicationBinding(ctx, cluster, old.HTTPBinding, actor, nil, deploymentAudit(t))
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == old.ID || created.Revision != 1 {
		t.Fatalf("identity reused: %+v", created)
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE agent_installations SET control_session_sequence=7`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sample := kubernetesbinding.Observation{ID: old.ID + ":https-molejo", Kind: kubernetesbinding.KindPublicationHTTP, Version: 1, Health: kubernetesbinding.HealthHealthy, SampledAt: now, Publication: &kubernetesbinding.PublicationObservation{GatewayNamespace: old.GatewayNamespace, GatewayName: old.GatewayName, SectionName: "https-molejo"}}
	if err = s.ReconcileBindingObservations(ctx, cluster, "test-session", 7, []kubernetesbinding.Observation{sample}, true, now); err != nil {
		t.Fatal(err)
	}
	current, err := s.ClusterPublicationBinding(ctx, cluster)
	if err != nil || current.Health != kubernetesbinding.HealthUnknown {
		t.Fatalf("old incarnation evidence accepted: %+v %v", current, err)
	}
}

func TestHTTPGrantCannotBeBorrowedByAnotherWorkspace(t *testing.T) {
	s, _, actor := newIntegrationFixture(t)
	ctx := t.Context()
	publicID := newID(t, "ws")
	var wid int64
	if err := s.Pool.QueryRow(ctx, `INSERT INTO workspaces(public_id,name,namespace_name,bootstrap_state) VALUES($1,'Other workspace',$1,'Ready') RETURNING id`, publicID).Scan(&wid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO workspace_clusters(workspace_id,installation_id,namespace_name,state,observed_generation) SELECT $1,id,$2,'Ready',1 FROM agent_installations LIMIT 1`, wid, publicID); err != nil {
		t.Fatal(err)
	}
	project, app, environment := createHierarchy(t, s, wid)
	var cluster string
	if err := s.Pool.QueryRow(ctx, `SELECT public_id FROM agent_installations LIMIT 1`).Scan(&cluster); err != nil {
		t.Fatal(err)
	}
	choices, err := s.PublicationOptions(ctx, wid, cluster, "", "", 100)
	if err != nil || len(choices) != 0 {
		t.Fatalf("borrowed catalogue: %+v %v", choices, err)
	}
	_, err = s.CreateAppEnvironment(ctx, wid, actor, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", integrationConfiguration("ungranted"))
	if !errors.Is(err, domain.ErrPublicationNotGranted) {
		t.Fatalf("borrowed another workspace grant: %v", err)
	}
	var count int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM app_environments WHERE workspace_id=$1`, wid).Scan(&count); err != nil || count != 0 {
		t.Fatalf("ungranted application persisted: count=%d %v", count, err)
	}
}
