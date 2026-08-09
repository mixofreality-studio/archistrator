// package operatedruntime (internal test package, not operatedruntime_test):
// the render tests below call the unexported render() func and construct
// manifest values directly. TestFileLayout is zero-waiver on file COUNT, not
// package clause, so every test in this component — the profile-selection
// tests migrated from the former external test package, plus the renderer
// tests — lives in this one access_test.go.
package operatedruntime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

func rc() fwra.Context { return fwra.Context{Context: context.Background()} }

// TestUnknownProfileFailsFast: an unset profile is a construction-time ContractMisuse.
func TestUnknownProfileFailsFast(t *testing.T) {
	_, err := NewProfiledOperatedRuntimeAccess(RuntimeProfileUnknown, RuntimeConfig{})
	var e *fwra.Error
	if !errors.As(err, &e) || e.Kind != fwra.ContractMisuse {
		t.Fatalf("unknown profile: want ContractMisuse, got %v", err)
	}
}

// TestLocalProfileDeterministic: the LOCAL/dry-run profile accepts writes as no-ops,
// reports a deterministic Healthy/SLO-met snapshot, and invents no usage facts.
func TestLocalProfileDeterministic(t *testing.T) {
	rt, err := NewProfiledOperatedRuntimeAccess(RuntimeProfileLocal, RuntimeConfig{})
	if err != nil {
		t.Fatalf("local build: %v", err)
	}
	app := uuid.New()

	if err := rt.PublishDesiredState(rc(), app, RuntimeDesiredState{}, "k1"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := rt.Withdraw(rc(), app, "k2"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if err := rt.WirePaymentConfig(rc(), app, GatewayBinding{ConnectedAccountID: "acct"}, "k3"); err != nil {
		t.Fatalf("wire: %v", err)
	}

	health, err := rt.GetApplicationHealth(rc(), app)
	if err != nil || health != RuntimeStatusHealthy {
		t.Fatalf("health = %v, err = %v; want Healthy", health, err)
	}
	slo, err := rt.GetSloStatus(rc(), app)
	if err != nil || !slo.SloMet {
		t.Fatalf("slo = %+v, err = %v; want SloMet", slo, err)
	}
	attr, err := rt.ReadComputeAttribution(rc(), app, AttributionWindow{})
	if err != nil {
		t.Fatalf("attribution: %v", err)
	}
	if attr.RuntimeEventID != "" {
		t.Fatalf("dry-run fabricated a usage fact: %+v", attr)
	}
}

// TestRealProfileExplicitNotImplemented: the REAL profile constructs (server still boots).
// WirePaymentConfig (still no backend — N-DEP follow-up) returns an explicit, diagnosable,
// non-retryable error naming that follow-up — NOT a silent generated stub.
// PublishDesiredState/Withdraw (Task 7) have their own, different explicit diagnostic when
// unconfigured (no GitOpsRepoURL) — exercised separately below — but must be equally
// non-retryable. GetApplicationHealth (Task 9) is exercised in the same test running
// outside a cluster: it must fail loudly, not report a false Healthy — see
// TestGetApplicationHealth_NotInCluster below for the dedicated, fail-closed assertion.
// GetSloStatus/ReadComputeAttribution (Task 9, D7) are deliberately inert and must succeed
// with their fixed, honest values — never error, since the 30s Schedule depends on it.
func TestRealProfileExplicitNotImplemented(t *testing.T) {
	rt, err := NewProfiledOperatedRuntimeAccess(RuntimeProfileReal, RuntimeConfig{})
	if err != nil {
		t.Fatalf("real build should not fail at construction (preserves boot): %v", err)
	}
	app := uuid.New()

	// Unconfigured GitOps repo: PublishDesiredState must still fail loudly and
	// non-retryably, not attempt a git operation against an empty URL.
	perr := rt.PublishDesiredState(rc(), app, RuntimeDesiredState{AppName: "example"}, "k")
	var e *fwra.Error
	if !errors.As(perr, &e) {
		t.Fatalf("real publish: want *fwra.Error, got %T %v", perr, perr)
	}
	if e.Retryable {
		t.Fatalf("real-profile error must be non-retryable (fail-fast wire behaviour), got retryable")
	}
	if _, herr := rt.GetApplicationHealth(rc(), app); herr == nil {
		t.Fatalf("real getApplicationHealth: want explicit error, got nil")
	}

	// D7: deliberately inert, and must never error — the 30s Schedule stays green.
	slo, serr := rt.GetSloStatus(rc(), app)
	if serr != nil {
		t.Fatalf("real getSloStatus must not error (D7 keeps the Schedule green): %v", serr)
	}
	if !slo.SloMet || slo.Detail != "SLO monitoring not configured" {
		t.Fatalf("real getSloStatus = %+v, want {SloMet:true Detail:%q}", slo, "SLO monitoring not configured")
	}
	attr, aerr := rt.ReadComputeAttribution(rc(), app, AttributionWindow{})
	if aerr != nil {
		t.Fatalf("real readComputeAttribution must not error (D7): %v", aerr)
	}
	if attr != (ComputeAttribution{}) {
		t.Fatalf("real readComputeAttribution fabricated a usage fact: %+v, want zero value", attr)
	}
}

// TestGetApplicationHealth_NotInCluster: running outside a cluster (as every test does —
// KUBERNETES_SERVICE_HOST is unset) must fail closed with a diagnosable, retryable
// fwra.Infrastructure error, never a silently reported RuntimeStatusHealthy. This is the
// fail-open pattern this plan already rejected twice elsewhere (an auth check that no-ops
// on a blank client ID; a delete-guard that disarms on unset config) — unknown must mean
// unknown here too.
func TestGetApplicationHealth_NotInCluster(t *testing.T) {
	rt, err := NewProfiledOperatedRuntimeAccess(RuntimeProfileReal, RuntimeConfig{})
	if err != nil {
		t.Fatalf("real build: %v", err)
	}
	status, herr := rt.GetApplicationHealth(rc(), uuid.New())
	if herr == nil {
		t.Fatal("want explicit error when not running in-cluster, got nil")
	}
	if status != RuntimeStatusUnknown {
		t.Fatalf("status = %v, want RuntimeStatusUnknown on error", status)
	}
	var e *fwra.Error
	if !errors.As(herr, &e) {
		t.Fatalf("want *fwra.Error, got %T %v", herr, herr)
	}
	if e.Kind != fwra.Infrastructure {
		t.Fatalf("kind = %v, want Infrastructure", e.Kind)
	}
	if !e.Retryable {
		t.Fatal("not-in-cluster error should be retryable by default (Infrastructure kind) — the Schedule keeps polling, cheaply")
	}
}

// TestMapArgoHealth: mapArgoHealth's mapping table, including the deliberate D10
// asymmetry — Progressing maps to Degraded, not Healthy — and the Unknown/absent case.
func TestMapArgoHealth(t *testing.T) {
	cases := []struct {
		in   string
		want RuntimeStatus
	}{
		{"Healthy", RuntimeStatusHealthy},
		{"Progressing", RuntimeStatusDegraded},
		{"Degraded", RuntimeStatusDegraded},
		{"Missing", RuntimeStatusDegraded},
		{"Suspended", RuntimeStatusDegraded},
		{"", RuntimeStatusUnknown},
	}
	for _, c := range cases {
		if got := mapArgoHealth(c.in); got != c.want {
			t.Errorf("mapArgoHealth(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestParseResourceHealth: parses a real-shaped Argo Application CR fixture and confirms
// status.resources[] — including both a Healthy and a Degraded entry — comes through.
func TestParseResourceHealth(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "argo", "application-healthy.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rs, err := parseResourceHealth(raw)
	if err != nil {
		t.Fatalf("parseResourceHealth: %v", err)
	}
	if len(rs) == 0 {
		t.Fatal("no resources parsed from status.resources[]")
	}
	var sawHealthy, sawDegraded bool
	for _, r := range rs {
		if r.Kind == "" || r.Name == "" || r.Namespace == "" {
			t.Errorf("resource missing Kind/Name/Namespace: %+v", r)
		}
		switch r.Health {
		case "Healthy":
			sawHealthy = true
		case "Degraded":
			sawDegraded = true
		}
	}
	if !sawHealthy {
		t.Error("fixture should contain at least one Healthy resource")
	}
	if !sawDegraded {
		t.Error("fixture should contain at least one Degraded resource")
	}
}

// loadArgoFixture reads a fixture under testdata/argo/, failing the test on error.
func loadArgoFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "argo", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

// ---------------------------------------------------------------------------
// parseModelKeyHealth — the health-overlay join (Task 10, task-10-brief Step 2/4).
// ---------------------------------------------------------------------------

// TestGetDeploymentResourceHealth_MapsPerResourceHealthToModelKeys: a Healthy
// Deployment and a Degraded Cluster, matching what render() emits for
// testDesiredState(), map onto their model keys — and the Postgres Cluster's
// Degraded status fans out onto all three database-role model keys it answers
// for (spec §5.1a).
func TestGetDeploymentResourceHealth_MapsPerResourceHealthToModelKeys(t *testing.T) {
	got, err := parseModelKeyHealth(loadArgoFixture(t, "application-mixed.json"), testDesiredState())
	if err != nil {
		t.Fatalf("parseModelKeyHealth: %v", err)
	}
	byKey := map[string]RuntimeStatus{}
	for _, h := range got {
		byKey[h.ModelKey] = h.Status
	}
	if byKey["cloud-node-server-deployment"] != RuntimeStatusHealthy {
		t.Errorf("server = %v, want Healthy", byKey["cloud-node-server-deployment"])
	}
	// All three database model keys collapse onto the one Cluster resource and
	// must therefore all report its health.
	for _, k := range []string{"cloud-infra-operatedsystemstate", "cloud-infra-billingstate", "cloud-infra-usagelog"} {
		if byKey[k] != RuntimeStatusDegraded {
			t.Errorf("%s = %v, want Degraded (all three share one Cluster)", k, byKey[k])
		}
	}
}

// TestGetDeploymentResourceHealth_RenderedButAbsentFromClusterIsDegraded: a manifest
// the renderer emits but the cluster does not report (here, the Postgres Cluster is
// entirely absent from status.resources[]) must read Degraded, not Healthy and not
// silently dropped — task-10-brief Step 4's fail-closed rule.
func TestGetDeploymentResourceHealth_RenderedButAbsentFromClusterIsDegraded(t *testing.T) {
	got, err := parseModelKeyHealth(loadArgoFixture(t, "application-missing-cluster.json"), testDesiredState())
	if err != nil {
		t.Fatalf("parseModelKeyHealth: %v", err)
	}
	byKey := map[string]RuntimeStatus{}
	for _, h := range got {
		byKey[h.ModelKey] = h.Status
	}
	for _, h := range got {
		if h.ModelKey == "cloud-infra-billingstate" && h.Status == RuntimeStatusHealthy {
			t.Error("a resource missing from the cluster must not report Healthy")
		}
	}
	if byKey["cloud-infra-billingstate"] != RuntimeStatusDegraded {
		t.Errorf("cloud-infra-billingstate = %v, want Degraded (rendered but not reported)", byKey["cloud-infra-billingstate"])
	}
}

// TestJoinModelKeyHealth_NoLiveResourcesDegradesEveryManifest: the never-synced path
// (argoFindApplicationByAppID found == false) hands joinModelKeyHealth a nil resources
// slice directly, with no raw JSON to round-trip. Every model key render() would emit
// must still come back Degraded — never Healthy, never simply absent from the result.
func TestJoinModelKeyHealth_NoLiveResourcesDegradesEveryManifest(t *testing.T) {
	manifests, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := joinModelKeyHealth(manifests, nil, RuntimeStatusDegraded)
	if len(got) == 0 {
		t.Fatal("expected one ModelKeyHealth per rendered manifest ModelKey, got none")
	}
	for _, h := range got {
		if h.Status != RuntimeStatusDegraded {
			t.Errorf("%s = %v, want Degraded (no live resources at all)", h.ModelKey, h.Status)
		}
	}
}

// ---------------------------------------------------------------------------
// The two joins the 2026-08-08 final review found wrong (fix 2). Both would have
// painted a healthy production cluster red, and neither was caught because every
// existing fixture hand-sets a health field on every resource — a shape the real
// Argo status.resources[] does not have.
// ---------------------------------------------------------------------------

// TestGetDeploymentResourceHealth_FullySyncedClusterIsAllHealthy exercises the real
// shape: EVERY object the renderer emits is present in status.resources[], the ones Argo
// has a health checker for carry Healthy, and the ones it has NO checker for
// (HTTPRoute, BackendTrafficPolicy, SecurityPolicy, KeycloakRealmImport) carry no health
// field at all. Every diagram node must read Healthy — including the namespace node,
// whose Application never appears in its own resource list, and the gateway and
// identity-provider nodes, whose objects Argo cannot grade.
func TestGetDeploymentResourceHealth_FullySyncedClusterIsAllHealthy(t *testing.T) {
	got, err := parseModelKeyHealth(loadArgoFixture(t, "application-fully-synced.json"), testDesiredState())
	if err != nil {
		t.Fatalf("parseModelKeyHealth: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no model keys reported")
	}
	for _, h := range got {
		if h.Status != RuntimeStatusHealthy {
			t.Errorf("%s = %v, want Healthy — a fully synced cluster must not read red anywhere", h.ModelKey, h.Status)
		}
	}
	// Named explicitly so a future change that simply drops these keys from the result
	// (which the loop above would not notice) still fails.
	byKey := map[string]RuntimeStatus{}
	for _, h := range got {
		byKey[h.ModelKey] = h.Status
	}
	for _, k := range []string{"cloud-node-ns-archistrator", "cloud-infra-gateway", "cloud-infra-keycloak"} {
		if byKey[k] != RuntimeStatusHealthy {
			t.Errorf("%s = %v, want Healthy", k, byKey[k])
		}
	}
}

// TestGetDeploymentResourceHealth_ApplicationTakesItsOwnRollup: an Argo Application never
// lists ITSELF among status.resources[], so joining it there hit the fail-closed no-match
// rule and painted the app's namespace node permanently red. Its model key must instead
// carry the Application's own app-level health — Healthy here, on a fixture whose
// individual resources are a mixture (the point being that the namespace node follows the
// ROLLUP, which is what the Application reports).
func TestGetDeploymentResourceHealth_ApplicationTakesItsOwnRollup(t *testing.T) {
	got, err := parseModelKeyHealth(loadArgoFixture(t, "application-healthy.json"), testDesiredState())
	if err != nil {
		t.Fatalf("parseModelKeyHealth: %v", err)
	}
	byKey := map[string]RuntimeStatus{}
	for _, h := range got {
		byKey[h.ModelKey] = h.Status
	}
	if _, ok := byKey["cloud-node-ns-archistrator"]; !ok {
		t.Fatal("the Application's model key must still be reported, never dropped")
	}
	if byKey["cloud-node-ns-archistrator"] != RuntimeStatusHealthy {
		t.Errorf("cloud-node-ns-archistrator = %v, want Healthy (the Application's own status.health.status)", byKey["cloud-node-ns-archistrator"])
	}
	// The reverse direction: a Degraded rollup must reach the same node.
	degraded, err := parseModelKeyHealth(loadArgoFixture(t, "application-mixed.json"), testDesiredState())
	if err != nil {
		t.Fatalf("parseModelKeyHealth: %v", err)
	}
	for _, h := range degraded {
		if h.ModelKey == "cloud-node-ns-archistrator" && h.Status != RuntimeStatusDegraded {
			t.Errorf("cloud-node-ns-archistrator = %v, want Degraded (the Application's own rollup)", h.Status)
		}
	}
}

// TestMapArgoResourceHealth pins all three facts the per-resource fold turns on:
// ABSENT from the resource set is fail-closed Degraded (covered by
// TestGetDeploymentResourceHealth_RenderedButAbsentFromClusterIsDegraded above); PRESENT
// WITH NO HEALTH FIELD means Argo has no checker for the kind, so its SYNC status is the
// only fact there is and only "Synced" may be green; and a graded kind keeps Argo's own
// verdict unchanged, including the D10 Progressing asymmetry.
func TestMapArgoResourceHealth(t *testing.T) {
	for _, c := range []struct {
		name         string
		health, sync string
		want         RuntimeStatus
	}{
		// Ungradeable kinds — sync is the whole verdict.
		{"ungradeable and synced", "", "Synced", RuntimeStatusHealthy},
		{"ungradeable and out of sync", "", "OutOfSync", RuntimeStatusDegraded},
		{"ungradeable with no sync status at all", "", "", RuntimeStatusDegraded},
		{"ungradeable with an unrecognized sync status", "", "Weird", RuntimeStatusDegraded},
		// Graded kinds — Argo's own health verdict, whatever the sync status says.
		{"graded healthy", "Healthy", "Synced", RuntimeStatusHealthy},
		{"graded progressing", "Progressing", "Synced", RuntimeStatusDegraded},
		{"graded degraded", "Degraded", "Synced", RuntimeStatusDegraded},
		{"graded missing", "Missing", "OutOfSync", RuntimeStatusDegraded},
	} {
		if got := mapArgoResourceHealth(c.health, c.sync); got != c.want {
			t.Errorf("%s: mapArgoResourceHealth(%q, %q) = %v, want %v", c.name, c.health, c.sync, got, c.want)
		}
	}
}

// TestGetDeploymentResourceHealth_UngradeableButOutOfSyncIsNotGreen is the manual-sync
// window, which for archistrator's own Application is not an edge case but a normal state
// on every deploy (D8: manual sync IS the self-guard). A published-but-unsynced
// SecurityPolicy and an unsynced KeycloakRealmImport are present in status.resources[] and
// ungradeable, so before this rule they read green — the diagram claiming a change landed
// when the operator had not yet pressed Sync. Both must read red; the objects that ARE
// synced in the same fixture must still read green, or the rule would just be a blanket
// pessimism.
func TestGetDeploymentResourceHealth_UngradeableButOutOfSyncIsNotGreen(t *testing.T) {
	got, err := parseModelKeyHealth(loadArgoFixture(t, "application-awaiting-manual-sync.json"), testDesiredState())
	if err != nil {
		t.Fatalf("parseModelKeyHealth: %v", err)
	}
	byKey := map[string][]RuntimeStatus{}
	for _, h := range got {
		byKey[h.ModelKey] = append(byKey[h.ModelKey], h.Status)
	}

	// The SecurityPolicy is OutOfSync and the KeycloakRealmImport carries no sync status
	// at all — neither is evidence the object is applied.
	if !slices.Contains(byKey["cloud-infra-gateway"], RuntimeStatusDegraded) {
		t.Errorf("cloud-infra-gateway = %v, want at least one Degraded (its SecurityPolicy is OutOfSync)", byKey["cloud-infra-gateway"])
	}
	for _, s := range byKey["cloud-infra-keycloak"] {
		if s == RuntimeStatusHealthy {
			t.Error("cloud-infra-keycloak reads Healthy for a resource carrying no sync status at all")
		}
	}
	// Synced-and-graded objects in the same read are unaffected.
	for _, k := range []string{"cloud-node-server-deployment", "cloud-infra-static-assets", "cloud-infra-billingstate"} {
		for _, s := range byKey[k] {
			if s != RuntimeStatusHealthy {
				t.Errorf("%s = %v, want Healthy (synced and graded healthy)", k, byKey[k])
			}
		}
	}
	// The routes and traffic policies ARE synced, so the ungradeable rule must not
	// blanket-degrade them — the gateway node's red comes from the SecurityPolicy alone.
	if !slices.Contains(byKey["cloud-infra-gateway"], RuntimeStatusHealthy) {
		t.Errorf("cloud-infra-gateway = %v, want the synced routes/policies still reporting Healthy", byKey["cloud-infra-gateway"])
	}
}

// ---------------------------------------------------------------------------
// parseApplicationListEnvelope — Task 10 fold-in of a Task 9 Minor finding: an
// unexpected-shape LIST response must be a diagnosable error, not a benign-looking
// "nothing found" that reads identically to a genuine never-synced app.
// ---------------------------------------------------------------------------

func TestParseApplicationListEnvelope_AcceptsWellFormedList(t *testing.T) {
	body := []byte(`{"kind":"ApplicationList","items":[{"metadata":{"annotations":{"archistrator.dev/app-id":"x"}}}]}`)
	envelope, err := parseApplicationListEnvelope(body)
	if err != nil {
		t.Fatalf("parseApplicationListEnvelope: %v", err)
	}
	if len(envelope.Items) != 1 {
		t.Errorf("Items = %d, want 1", len(envelope.Items))
	}
}

func TestParseApplicationListEnvelope_RejectsMissingKind(t *testing.T) {
	// Valid JSON, but not the ApplicationList envelope — no "kind" key at all.
	// Before this check this unmarshaled into a zero-Items envelope, which read
	// identically to a genuine "no Application has synced yet".
	body := []byte(`{"items":[]}`)
	if _, err := parseApplicationListEnvelope(body); err == nil {
		t.Fatal("want an explicit error for a response missing kind=ApplicationList, got nil")
	}
}

func TestParseApplicationListEnvelope_RejectsWrongKind(t *testing.T) {
	body := []byte(`{"kind":"Status","items":[]}`)
	if _, err := parseApplicationListEnvelope(body); err == nil {
		t.Fatal("want an explicit error for the wrong kind, got nil")
	}
}

// ---------------------------------------------------------------------------
// Renderer tests — Task 4 of the 2026-08-07 operations/ArgoCD plan.
// ---------------------------------------------------------------------------

// testDesiredState is the shared fixture: archistrator's own desired state,
// matching the values .superpowers/sdd/2026-08-07-operations-argocd-deployment/
// task-4-brief.md specifies and testdata/golden/production/ was captured from.
func testDesiredState() RuntimeDesiredState {
	return RuntimeDesiredState{
		AppName:         "archistrator",
		Namespace:       "archistrator",
		Host:            "archistrator.capture-gtd.com",
		ModelKey:        "cloud-node-ns-archistrator",
		GatewayModelKey: "cloud-infra-gateway",
		Server: Workload{
			ModelKey: "cloud-node-server-deployment",
			Image:    "ghcr.io/mixofreality-studio/archistrator-server:0.8.16",
			// Production runs 2 replicas of both workloads (verified against
			// testdata/golden/production/archistrator-{server,webapp}.yaml,
			// which the Task 6 golden diff caught: the Task 4 fixture had
			// hardcoded 1). Kept at the real value so the golden diff is a
			// true parity check, not one with a known-wrong input baked in.
			Replicas: 2,
		},
		WebApp: Workload{
			ModelKey: "cloud-infra-static-assets",
			Image:    "ghcr.io/mixofreality-studio/archistrator-webapp:0.6.14",
			Replicas: 2,
		},
		Postgres: PostgresSpec{
			ModelKeys:    []string{"cloud-infra-billingstate", "cloud-infra-operatedsystemstate", "cloud-infra-usagelog"},
			Enabled:      true,
			Instances:    1,
			StorageClass: "do-block-storage",
		},
		OIDC: OIDCSpec{
			ModelKey:        "cloud-infra-keycloak",
			Issuer:          "https://keycloak.capture-gtd.com/realms/archistrator",
			ClientID:        "archistrator-webapp",
			ClientSecretRef: "archistrator-oidc-client-secret",
		},
		SelfManaged: true,
	}
}

func TestRender_EmitsServerDeploymentWithModelKey(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var found *manifest
	for i := range ms {
		if ms[i].Kind == "Deployment" && ms[i].Name == "archistrator-server" {
			found = &ms[i]
		}
	}
	if found == nil {
		t.Fatal("no Deployment/archistrator-server in rendered output")
	}
	if len(found.ModelKeys) != 1 || found.ModelKeys[0] != "cloud-node-server-deployment" {
		t.Errorf("ModelKeys = %v, want [cloud-node-server-deployment]", found.ModelKeys)
	}
	if found.Namespace != "archistrator" {
		t.Errorf("Namespace = %q, want archistrator", found.Namespace)
	}

	var svc *manifest
	for i := range ms {
		if ms[i].Kind == "Service" && ms[i].Name == "archistrator-server" {
			svc = &ms[i]
		}
	}
	if svc == nil {
		t.Fatal("no Service/archistrator-server in rendered output")
	}
	if len(svc.ModelKeys) != 1 || svc.ModelKeys[0] != "cloud-node-server-deployment" {
		t.Errorf("Service ModelKeys = %v, want [cloud-node-server-deployment]", svc.ModelKeys)
	}
}

func TestRender_EmitsWebAppDeploymentAndServiceWithModelKey(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, kind := range []string{"Deployment", "Service"} {
		var found *manifest
		for i := range ms {
			if ms[i].Kind == kind && ms[i].Name == "archistrator-webapp" {
				found = &ms[i]
			}
		}
		if found == nil {
			t.Fatalf("no %s/archistrator-webapp in rendered output", kind)
		}
		if len(found.ModelKeys) != 1 || found.ModelKeys[0] != "cloud-infra-static-assets" {
			t.Errorf("%s ModelKeys = %v, want [cloud-infra-static-assets]", kind, found.ModelKeys)
		}
		if found.Namespace != "archistrator" {
			t.Errorf("%s Namespace = %q, want archistrator", kind, found.Namespace)
		}
	}
}

func TestRender_IsDeterministic(t *testing.T) {
	a, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	b, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("manifest count differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		// manifest carries a []string (ModelKeys), so it is not comparable with
		// == — reflect.DeepEqual is the right tool, not a manual field walk,
		// since a future field addition must not silently fall out of this check.
		if !reflect.DeepEqual(a[i], b[i]) {
			t.Errorf("manifest %d differs between renders:\n%+v\n%+v", i, a[i], b[i])
		}
	}
}

// TestRender_OutputIsSortedByKindThenName pins the ordering contract render()
// promises — ascending Kind, then ascending Name — over the complete object
// set. The full expected list is spelled out rather than merely checked for
// sortedness so that an accidentally DROPPED or ADDED object fails here: the
// health overlay re-renders to rebuild its model-key map, and a silently
// missing manifest would leave a diagram node permanently uncoloured.
func TestRender_OutputIsSortedByKindThenName(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := []struct{ Kind, Name string }{
		{"Application", "archistrator"},
		{"BackendTrafficPolicy", "archistrator-api-route-policy"},
		{"BackendTrafficPolicy", "archistrator-healthz-route-policy"},
		{"BackendTrafficPolicy", "archistrator-readyz-route-policy"},
		{"BackendTrafficPolicy", "archistrator-webapp-route-policy"},
		{"Cluster", "archistrator-postgres"},
		{"Deployment", "archistrator-server"},
		{"Deployment", "archistrator-webapp"},
		{"HTTPRoute", "archistrator-api-route"},
		{"HTTPRoute", "archistrator-healthz-route"},
		{"HTTPRoute", "archistrator-readyz-route"},
		{"HTTPRoute", "archistrator-webapp-route"},
		{"KeycloakRealmImport", "archistrator-realm"},
		{"SecurityPolicy", "archistrator-oidc-policy"},
		{"Service", "archistrator-server"},
		{"Service", "archistrator-webapp"},
	}
	if len(ms) != len(want) {
		got := make([]string, len(ms))
		for i, m := range ms {
			got[i] = m.Kind + "/" + m.Name
		}
		t.Fatalf("manifest count = %d, want %d; got %v", len(ms), len(want), got)
	}
	for i, w := range want {
		if ms[i].Kind != w.Kind || ms[i].Name != w.Name {
			t.Errorf("manifest[%d] = %s/%s, want %s/%s", i, ms[i].Kind, ms[i].Name, w.Kind, w.Name)
		}
	}
}

// TestRender_EveryManifestIsValidYAML catches the failure mode no substring
// assertion can: a template whose indentation is subtly wrong still contains
// every expected string but parses as a different document — or not at all —
// and only fails once ArgoCD tries to apply it. Also pins each object's
// apiVersion/kind/metadata against the manifest's own Kind/Name/Namespace, so
// the health overlay's index can never disagree with the YAML it indexes.
func TestRender_EveryManifestIsValidYAML(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		var obj struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
			Metadata   struct {
				Name      string `yaml:"name"`
				Namespace string `yaml:"namespace"`
			} `yaml:"metadata"`
		}
		if err := yaml.Unmarshal([]byte(m.YAML), &obj); err != nil {
			t.Errorf("%s/%s is not valid YAML: %v\n%s", m.Kind, m.Name, err, m.YAML)
			continue
		}
		if obj.APIVersion == "" {
			t.Errorf("%s/%s has no apiVersion", m.Kind, m.Name)
		}
		if obj.Kind != m.Kind {
			t.Errorf("%s/%s: YAML kind = %q", m.Kind, m.Name, obj.Kind)
		}
		if obj.Metadata.Name != m.Name {
			t.Errorf("%s/%s: YAML metadata.name = %q", m.Kind, m.Name, obj.Metadata.Name)
		}
		if obj.Metadata.Namespace != m.Namespace {
			t.Errorf("%s/%s: YAML metadata.namespace = %q, manifest.Namespace = %q", m.Kind, m.Name, obj.Metadata.Namespace, m.Namespace)
		}
	}
}

// TestRender_EveryManifestCarriesSortedModelKeys is the health-overlay
// precondition: a manifest with no model key can never be attributed back to a
// diagram node, and unsorted keys would make the render non-deterministic.
func TestRender_EveryManifestCarriesSortedModelKeys(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		if len(m.ModelKeys) == 0 {
			t.Errorf("%s/%s carries no ModelKeys", m.Kind, m.Name)
			continue
		}
		if !sort.StringsAreSorted(m.ModelKeys) {
			t.Errorf("%s/%s ModelKeys are unsorted: %v", m.Kind, m.Name, m.ModelKeys)
		}
		for _, k := range m.ModelKeys {
			if k == "" {
				t.Errorf("%s/%s carries an empty ModelKey: %v", m.Kind, m.Name, m.ModelKeys)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Argo Application — the self-managed guard (spec §5.3).
// ---------------------------------------------------------------------------

// TestRender_SelfManagedApplicationDisablesPrune: archistrator renders the
// manifests that govern archistrator. A renderer bug must never be able to
// delete the control plane, so the self-managed Application syncs MANUALLY and
// never prunes.
func TestRender_SelfManagedApplicationDisablesPrune(t *testing.T) {
	ms, err := render(testDesiredState()) // SelfManaged: true
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var app *manifest
	for i := range ms {
		if ms[i].Kind == "Application" {
			app = &ms[i]
		}
	}
	if app == nil {
		t.Fatal("no Argo Application rendered")
	}
	if strings.Contains(app.YAML, "prune: true") {
		t.Error("self-managed Application must not enable prune")
	}
	if strings.Contains(app.YAML, "automated:") {
		t.Error("self-managed Application must not enable automated sync")
	}
}

func TestRender_TenantApplicationEnablesAutomatedSync(t *testing.T) {
	d := testDesiredState()
	d.SelfManaged = false
	d.AppName = "gtdapp"
	d.Namespace = "gtdapp"

	ms, err := render(d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		if m.Kind != "Application" {
			continue
		}
		if !strings.Contains(m.YAML, "prune: true") {
			t.Error("tenant Application should enable prune")
		}
		if !strings.Contains(m.YAML, "selfHeal: true") {
			t.Error("tenant Application should enable selfHeal")
		}
		return
	}
	t.Fatal("no Argo Application rendered")
}

// TestRender_SelfManagedApplicationOmitsTheResourcesFinalizer closes the second
// route to self-destruction. prune: false stops a RENDERER bug from deleting
// archistrator's control plane; the resources-finalizer would let deleting or
// renaming the Application OBJECT cascade-delete every resource it manages —
// with no archistrator left to repair the outcome. Tenant apps keep it, because
// there cascading delete on withdrawal is the desired behaviour.
func TestRender_SelfManagedApplicationOmitsTheResourcesFinalizer(t *testing.T) {
	const finalizer = "resources-finalizer.argocd.argoproj.io"

	selfManaged := testDesiredState() // SelfManaged: true
	tenant := testDesiredState()
	tenant.SelfManaged = false
	tenant.AppName = "gtdapp"
	tenant.Namespace = "gtdapp"

	for _, tc := range []struct {
		name string
		in   RuntimeDesiredState
		want bool
	}{
		{"self-managed", selfManaged, false},
		{"tenant", tenant, true},
	} {
		ms, err := render(tc.in)
		if err != nil {
			t.Fatalf("%s: render: %v", tc.name, err)
		}
		var app *manifest
		for i := range ms {
			if ms[i].Kind == "Application" {
				app = &ms[i]
			}
		}
		if app == nil {
			t.Fatalf("%s: no Argo Application rendered", tc.name)
		}
		if got := strings.Contains(app.YAML, finalizer); got != tc.want {
			t.Errorf("%s: finalizer present = %v, want %v\n%s", tc.name, got, tc.want, app.YAML)
		}
	}
}

// TestRender_ApplicationDestinationIsTheAppsOwnNamespace pins the spec §5.3
// invariant that every destination.namespace equals the app's own namespace.
func TestRender_ApplicationDestinationIsTheAppsOwnNamespace(t *testing.T) {
	d := testDesiredState()
	d.SelfManaged = false
	d.AppName = "gtdapp"
	d.Namespace = "gtdapp"

	ms, err := render(d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		if m.Kind != "Application" {
			continue
		}
		if !strings.Contains(m.YAML, "    namespace: gtdapp\n") {
			t.Errorf("Application destination.namespace must be the app's own namespace:\n%s", m.YAML)
		}
		if m.Namespace != "argocd" {
			t.Errorf("Application object namespace = %q, want argocd", m.Namespace)
		}
		return
	}
	t.Fatal("no Argo Application rendered")
}

// ---------------------------------------------------------------------------
// CNPG Cluster.
// ---------------------------------------------------------------------------

// TestRender_PostgresClusterCarriesAllThreeDatabaseModelKeys: production runs
// ONE cluster serving three logical stores, each its own diagram node, so all
// three must colour from this one resource's health (spec §5.1a trap #2).
func TestRender_PostgresClusterCarriesAllThreeDatabaseModelKeys(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		if m.Kind != "Cluster" {
			continue
		}
		if m.Name != "archistrator-postgres" || m.Namespace != "archistrator" {
			t.Errorf("Cluster = %s/%s, want archistrator/archistrator-postgres", m.Namespace, m.Name)
		}
		want := []string{"cloud-infra-billingstate", "cloud-infra-operatedsystemstate", "cloud-infra-usagelog"}
		if !reflect.DeepEqual(m.ModelKeys, want) {
			t.Errorf("Cluster ModelKeys = %v, want %v (sorted)", m.ModelKeys, want)
		}
		for _, want := range []string{
			"apiVersion: postgresql.cnpg.io/v1",
			"instances: 1",
			"storageClass: do-block-storage",
			"database: archistrator",
			"owner: app",
		} {
			if !strings.Contains(m.YAML, want) {
				t.Errorf("Cluster YAML missing %q:\n%s", want, m.YAML)
			}
		}
		return
	}
	t.Fatal("no CNPG Cluster rendered")
}

// TestRender_PostgresDisabledEmitsNoCluster: an app that brings its own
// database must not have one provisioned for it.
func TestRender_PostgresDisabledEmitsNoCluster(t *testing.T) {
	d := testDesiredState()
	d.Postgres.Enabled = false

	ms, err := render(d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		if m.Kind == "Cluster" {
			t.Fatalf("Postgres.Enabled == false still rendered a Cluster:\n%s", m.YAML)
		}
	}
}

// ---------------------------------------------------------------------------
// Gateway routes and Envoy policies.
// ---------------------------------------------------------------------------

// TestRender_GatewayRoutesMatchProduction pins the four routes and their four
// BackendTrafficPolicies against testdata/golden/production/
// archistrator-gateway-routes.yaml.
func TestRender_GatewayRoutesMatchProduction(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	routes := map[string]*manifest{}
	policies := map[string]*manifest{}
	for i := range ms {
		switch ms[i].Kind {
		case "HTTPRoute":
			routes[ms[i].Name] = &ms[i]
		case "BackendTrafficPolicy":
			policies[ms[i].Name] = &ms[i]
		}
	}
	if len(routes) != 4 {
		t.Fatalf("HTTPRoute count = %d, want 4 (webapp, api, healthz, readyz)", len(routes))
	}
	if len(policies) != 4 {
		t.Fatalf("BackendTrafficPolicy count = %d, want 4 (one per route)", len(policies))
	}

	for _, tc := range []struct{ name, path, backend, port string }{
		{"archistrator-webapp-route", "value: /\n", "name: archistrator-webapp", "port: 80"},
		{"archistrator-api-route", "value: /api\n", "name: archistrator-server", "port: 8080"},
		{"archistrator-healthz-route", "value: /healthz\n", "name: archistrator-server", "port: 8080"},
		{"archistrator-readyz-route", "value: /readyz\n", "name: archistrator-server", "port: 8080"},
	} {
		r, ok := routes[tc.name]
		if !ok {
			t.Errorf("no HTTPRoute %s", tc.name)
			continue
		}
		for _, want := range []string{
			"apiVersion: gateway.networking.k8s.io/v1",
			"hostnames:\n  - archistrator.capture-gtd.com",
			"  - name: gateway\n    namespace: gtd\n    sectionName: https-archistrator",
			tc.path, tc.backend, tc.port,
		} {
			if !strings.Contains(r.YAML, want) {
				t.Errorf("%s missing %q:\n%s", tc.name, want, r.YAML)
			}
		}
		p, ok := policies[tc.name+"-policy"]
		if !ok {
			t.Errorf("no BackendTrafficPolicy %s-policy", tc.name)
			continue
		}
		for _, want := range []string{
			"apiVersion: gateway.envoyproxy.io/v1alpha1",
			"kind: HTTPRoute\n      name: " + tc.name,
			"loadBalancer:\n    type: RoundRobin",
		} {
			if !strings.Contains(p.YAML, want) {
				t.Errorf("%s-policy missing %q:\n%s", tc.name, want, p.YAML)
			}
		}
	}
}

// TestRender_GatewayObjectsCarryTheGatewayNodeKey: the founder's requirement is
// that EACH Kubernetes component on the deployment diagram shows green or red.
// The routes and Envoy policies are what the gateway node colours from, so they
// must carry the gateway node's key — and must NOT carry the namespace node's,
// which would both strand the gateway node uncoloured and misattribute route
// health to the namespace. The Argo Application is the deliberate exception:
// it governs the whole app, so the namespace node is its honest owner.
func TestRender_GatewayObjectsCarryTheGatewayNodeKey(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	gatewayKinds := map[string]bool{"HTTPRoute": true, "BackendTrafficPolicy": true, "SecurityPolicy": true}
	seen := 0
	for _, m := range ms {
		switch {
		case gatewayKinds[m.Kind]:
			seen++
			if !reflect.DeepEqual(m.ModelKeys, []string{"cloud-infra-gateway"}) {
				t.Errorf("%s/%s ModelKeys = %v, want [cloud-infra-gateway]", m.Kind, m.Name, m.ModelKeys)
			}
		case m.Kind == "Application":
			if !reflect.DeepEqual(m.ModelKeys, []string{"cloud-node-ns-archistrator"}) {
				t.Errorf("Application ModelKeys = %v, want the namespace node [cloud-node-ns-archistrator]", m.ModelKeys)
			}
		}
	}
	if seen != 9 {
		t.Errorf("gateway-owned manifest count = %d, want 9 (4 HTTPRoute + 4 BackendTrafficPolicy + 1 SecurityPolicy)", seen)
	}
}

// TestRender_MissingGatewayModelKeyFailsLoudly: an unattributable route set is
// a real misconfiguration, not something to render anyway.
func TestRender_MissingGatewayModelKeyFailsLoudly(t *testing.T) {
	d := testDesiredState()
	d.GatewayModelKey = ""

	if _, err := render(d); err == nil {
		t.Fatal("render with an empty GatewayModelKey should fail, not emit unattributable routes")
	}
}

// TestRender_HasNoDedicatedOAuth2Route is a load-bearing ABSENCE, not an
// omission. The Envoy OIDC filter installed by the SecurityPolicy on the `/`
// (webapp) route intercepts /oauth2/callback itself. A dedicated /oauth2 route
// would be MORE specific than `/`, so Envoy would match it first and steal the
// callback away from the policy-attached route — the filter would never run and
// no session would ever be established. Login would break in a way the
// manifests alone do not reveal. Do NOT "complete" the route set by adding one.
func TestRender_HasNoDedicatedOAuth2Route(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		if m.Kind != "HTTPRoute" {
			continue
		}
		if strings.Contains(m.YAML, "value: /oauth2") {
			t.Errorf("HTTPRoute %s declares an /oauth2 path match; the OIDC filter on the webapp route owns the callback:\n%s", m.Name, m.YAML)
		}
	}
}

// TestRender_SecurityPolicyTargetsOnlyBrowserFacingRoutes: /healthz and /readyz
// must stay unauthenticated so probes answer without a session.
func TestRender_SecurityPolicyTargetsOnlyBrowserFacingRoutes(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		if m.Kind != "SecurityPolicy" {
			continue
		}
		if m.Name != "archistrator-oidc-policy" {
			t.Errorf("SecurityPolicy name = %q, want archistrator-oidc-policy", m.Name)
		}
		for _, want := range []string{
			"name: archistrator-api-route",
			"name: archistrator-webapp-route",
			`issuer: "https://keycloak.capture-gtd.com/realms/archistrator"`,
			`uri: "https://keycloak.capture-gtd.com/realms/archistrator/protocol/openid-connect/certs"`,
			`clientID: "archistrator-webapp"`,
			"clientSecret:\n      name: archistrator-oidc-client-secret",
			`redirectURL: "https://archistrator.capture-gtd.com/oauth2/callback"`,
			"passThroughAuthHeader: true",
			"idToken: ArchistratorIdToken",
		} {
			if !strings.Contains(m.YAML, want) {
				t.Errorf("SecurityPolicy missing %q:\n%s", want, m.YAML)
			}
		}
		for _, unwanted := range []string{"healthz-route", "readyz-route"} {
			if strings.Contains(m.YAML, "name: archistrator-"+unwanted) {
				t.Errorf("SecurityPolicy targets %s; health endpoints must stay unauthenticated:\n%s", unwanted, m.YAML)
			}
		}
		return
	}
	t.Fatal("no SecurityPolicy rendered")
}

// ---------------------------------------------------------------------------
// Keycloak realm/client CR (spec D12).
//
// This is the ONE rendered object with no production counterpart to diff
// against — production's realm and client are hand-managed in the admin
// console. These assertions ARE the safety net: a wrong realm name, clientId,
// or redirect URI breaks login for the whole app.
// ---------------------------------------------------------------------------

func TestRender_KeycloakRealmImportRealmClientAndRedirectURIs(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var cr *manifest
	for i := range ms {
		if ms[i].Kind == "KeycloakRealmImport" {
			cr = &ms[i]
		}
	}
	if cr == nil {
		t.Fatal("no KeycloakRealmImport rendered (spec D12)")
	}

	// The CR must live beside the Keycloak CR it names: the operator resolves
	// keycloakCRName in the CR's OWN namespace, and placeholder Secrets must be
	// in that same namespace too.
	if cr.Namespace != "keycloak" {
		t.Errorf("KeycloakRealmImport namespace = %q, want keycloak (same namespace as the Keycloak CR)", cr.Namespace)
	}
	if len(cr.ModelKeys) != 1 || cr.ModelKeys[0] != "cloud-infra-keycloak" {
		t.Errorf("KeycloakRealmImport ModelKeys = %v, want [cloud-infra-keycloak]", cr.ModelKeys)
	}

	// The API group/version the cluster's pinned keycloak-k8s-resources 26.4.2
	// actually serves. v2alpha1 is the ONLY served version in that release.
	if !strings.Contains(cr.YAML, "apiVersion: k8s.keycloak.org/v2alpha1") {
		t.Errorf("wrong API version:\n%s", cr.YAML)
	}
	if !strings.Contains(cr.YAML, "keycloakCRName: keycloak") {
		t.Errorf("keycloakCRName must name the cluster's Keycloak CR:\n%s", cr.YAML)
	}

	// Realm name — must be the LAST path segment of the OIDC issuer, or the
	// server's JWKS/issuer checks and the edge's OIDC provider disagree.
	if !strings.Contains(cr.YAML, "\n    realm: archistrator\n") {
		t.Errorf("realm must be named archistrator (issuer .../realms/archistrator):\n%s", cr.YAML)
	}

	// clientId — must equal the SecurityPolicy's clientID exactly.
	if !strings.Contains(cr.YAML, "clientId: archistrator-webapp") {
		t.Errorf("clientId must be archistrator-webapp:\n%s", cr.YAML)
	}

	// Redirect URI — must equal the SecurityPolicy's redirectURL exactly, or
	// Keycloak rejects the authorization-code redirect and login fails.
	if !strings.Contains(cr.YAML, "- https://archistrator.capture-gtd.com/oauth2/callback\n") {
		t.Errorf("redirectUris must contain the OIDC callback:\n%s", cr.YAML)
	}

	// Confidential client, authorization-code flow only.
	for _, want := range []string{
		"publicClient: false",
		"standardFlowEnabled: true",
		"implicitFlowEnabled: false",
		"directAccessGrantsEnabled: false",
		"serviceAccountsEnabled: false",
		"clientAuthenticatorType: client-secret",
	} {
		if !strings.Contains(cr.YAML, want) {
			t.Errorf("client must be a confidential authorization-code client, missing %q:\n%s", want, cr.YAML)
		}
	}
}

// TestRender_KeycloakClientSecretIsReferencedNotInlined: the client secret
// reaches Keycloak through the operator's placeholders stanza (a Secret name +
// key), never as a literal in the CR.
func TestRender_KeycloakClientSecretIsReferencedNotInlined(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		if m.Kind != "KeycloakRealmImport" {
			continue
		}
		if !strings.Contains(m.YAML, "placeholders:\n    ARCHISTRATOR_OIDC_CLIENT_SECRET:\n      secret:\n        name: archistrator-oidc-client-secret\n        key: client-secret\n") {
			t.Errorf("client secret must be referenced via the operator's placeholders stanza:\n%s", m.YAML)
		}
		if !strings.Contains(m.YAML, `secret: "${ARCHISTRATOR_OIDC_CLIENT_SECRET}"`) {
			t.Errorf("client.secret must be the placeholder reference, not a value:\n%s", m.YAML)
		}
		return
	}
	t.Fatal("no KeycloakRealmImport rendered")
}

// TestRender_MissingOIDCClientFailsLoudly: the auth path must fail CLOSED. An
// empty ClientID once yielded routes with no SecurityPolicy and no realm — a
// complete-looking manifest set that publishes the app's front door with
// authentication removed. Unreachable from today's assembly (ClientID is
// derived), which is exactly the assumption that stops holding later.
func TestRender_MissingOIDCClientFailsLoudly(t *testing.T) {
	d := testDesiredState()
	d.OIDC.ClientID = ""

	ms, err := render(d)
	if err == nil {
		var kinds []string
		for _, m := range ms {
			kinds = append(kinds, m.Kind+"/"+m.Name)
		}
		t.Fatalf("render with no OIDC client should fail, not publish an unauthenticated front door; got %v", kinds)
	}
	var e *fwra.Error
	if !errors.As(err, &e) || e.Kind != fwra.ContractMisuse {
		t.Errorf("want ContractMisuse like its neighbouring guards, got %v", err)
	}
}

// TestRender_KeycloakAndSecurityPolicyAgree is the cross-object check that no
// single-object test can make: the edge's OIDC config and the realm's client
// must name the SAME client and the SAME redirect URI. If they drift, the
// manifests are individually valid and login is still broken.
func TestRender_KeycloakAndSecurityPolicyAgree(t *testing.T) {
	d := testDesiredState()
	d.SelfManaged = false
	d.AppName = "gtdapp"
	d.Namespace = "gtdapp"
	d.Host = "gtdapp.capture-gtd.com"
	d.OIDC.Issuer = "https://keycloak.capture-gtd.com/realms/gtdapp"
	d.OIDC.ClientID = "gtdapp-webapp"
	d.OIDC.ClientSecretRef = "gtdapp-oidc-client-secret"

	ms, err := render(d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var policy, cr *manifest
	for i := range ms {
		switch ms[i].Kind {
		case "SecurityPolicy":
			policy = &ms[i]
		case "KeycloakRealmImport":
			cr = &ms[i]
		}
	}
	if policy == nil || cr == nil {
		t.Fatal("expected both a SecurityPolicy and a KeycloakRealmImport")
	}
	if !strings.Contains(policy.YAML, `clientID: "gtdapp-webapp"`) || !strings.Contains(cr.YAML, "clientId: gtdapp-webapp") {
		t.Errorf("clientID disagreement between edge policy and realm client:\n%s\n%s", policy.YAML, cr.YAML)
	}
	if !strings.Contains(policy.YAML, `redirectURL: "https://gtdapp.capture-gtd.com/oauth2/callback"`) ||
		!strings.Contains(cr.YAML, "- https://gtdapp.capture-gtd.com/oauth2/callback\n") {
		t.Errorf("redirect URI disagreement between edge policy and realm client:\n%s\n%s", policy.YAML, cr.YAML)
	}
	if !strings.Contains(cr.YAML, "\n    realm: gtdapp\n") {
		t.Errorf("realm name must track the issuer's realm segment:\n%s", cr.YAML)
	}
	if !strings.Contains(cr.YAML, "placeholders:\n    GTDAPP_OIDC_CLIENT_SECRET:") {
		t.Errorf("placeholder env name must be derived from the app name:\n%s", cr.YAML)
	}
}

// TestRender_ServerEnvMatchesProductionVariableNames pins the server
// container's env block to the variable names testdata/golden/production/
// archistrator-server.yaml actually uses, per the Task 4 brief's instruction
// to derive it from that file rather than invent one.
func TestRender_ServerEnvMatchesProductionVariableNames(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var server *manifest
	for i := range ms {
		if ms[i].Kind == "Deployment" && ms[i].Name == "archistrator-server" {
			server = &ms[i]
		}
	}
	if server == nil {
		t.Fatal("no Deployment/archistrator-server in rendered output")
	}
	for _, name := range []string{
		"ARCHISTRATOR_LISTEN_ADDR",
		"ARCHISTRATOR_SHUTDOWN_TIMEOUT",
		"DATABASE_USERNAME",
		"DATABASE_PASSWORD",
		"ARCHISTRATOR_POSTGRES_URL",
		"ARCHISTRATOR_TEMPORAL_HOSTPORT",
		"ARCHISTRATOR_TEMPORAL_NAMESPACE",
		"ARCHISTRATOR_GITHUB_APP_ID",
		"ARCHISTRATOR_GITHUB_ACCOUNT",
		"ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM",
		"ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL",
		"ARCHISTRATOR_GITHUB_WEBHOOK_SECRET",
		"ARCHISTRATOR_KEYCLOAK_JWKS_URL",
		"ARCHISTRATOR_KEYCLOAK_ISSUER",
		"ARCHISTRATOR_AUTH_DEV_MODE",
		"ARCHISTRATOR_DEV_SUBJECT",
		"ARCHISTRATOR_DEV_ROLES",
		"OTEL_SERVICE_NAME",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_INSECURE",
	} {
		if !strings.Contains(server.YAML, "name: "+name) {
			t.Errorf("server Deployment env is missing %s\nYAML:\n%s", name, server.YAML)
		}
	}
	if !strings.Contains(server.YAML, "value: \"https://keycloak.capture-gtd.com/realms/archistrator\"") {
		t.Error("ARCHISTRATOR_KEYCLOAK_ISSUER must equal RuntimeDesiredState.OIDC.Issuer")
	}
}

// TestRender_WebAppHasNoEnvBlock pins the webapp Deployment to the production
// golden's shape: static-asset nginx, no runtime env needed.
func TestRender_WebAppHasNoEnvBlock(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for i := range ms {
		if ms[i].Kind == "Deployment" && ms[i].Name == "archistrator-webapp" {
			if strings.Contains(ms[i].YAML, "\n        env:") {
				t.Errorf("webapp Deployment should carry no env block:\n%s", ms[i].YAML)
			}
			return
		}
	}
	t.Fatal("no Deployment/archistrator-webapp in rendered output")
}

// TestRender_NeverRendersSecretValues is the Task 4 slice of the invariant
// gate Task 6 formalizes: secrets are referenced via secretKeyRef, never
// inlined as data/stringData.
func TestRender_NeverRendersSecretValues(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		if m.Kind == "Secret" {
			t.Errorf("renderer emitted a Secret (%s); all secrets are created out-of-band", m.Name)
		}
		if strings.Contains(m.YAML, "\ndata:") || strings.Contains(m.YAML, "\nstringData:") {
			t.Errorf("manifest %s/%s carries inline secret data", m.Kind, m.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Task 6: invariant gates and the production golden diff.
//
// The no-Secret-data gate (TestRender_NeverRendersSecretValues, above) and the
// every-ModelKey-non-empty-and-sorted gate (TestRender_EveryManifestCarriesSortedModelKeys,
// above) already cover two of the three gates the task brief sketched; they
// are not duplicated here. What is missing is a blanket check that every
// manifest lands in the namespace it is supposed to.
// ---------------------------------------------------------------------------

// TestRender_AllManifestsTargetTheCorrectNamespace is the general form of the
// namespace checks scattered across the tests above (Deployment/Service in
// d.Namespace, Application destination in d.Namespace but the object itself
// in argocd, KeycloakRealmImport in Keycloak's own namespace): every rendered
// object's namespace is pinned to exactly one of the three legitimate values,
// so a copy-paste bug that leaves a NEW manifest kind in the wrong namespace
// (or with the wrong namespace ENTIRELY, not just a wrong value of the app's
// namespace) cannot land unnoticed the way a per-kind test alone would allow.
func TestRender_AllManifestsTargetTheCorrectNamespace(t *testing.T) {
	d := testDesiredState()
	ms, err := render(d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		switch m.Kind {
		case "Application":
			// The Application object lives in argocd; its destination (checked
			// elsewhere) is the app's own namespace.
			if m.Namespace != "argocd" {
				t.Errorf("Application namespace = %q, want argocd", m.Namespace)
			}
		case "KeycloakRealmImport":
			// Must live beside the Keycloak CR it names (renderKeycloakRealm's
			// doc: the operator resolves keycloakCRName, and placeholder
			// Secrets must live, in the CR's OWN namespace).
			if m.Namespace != "keycloak" {
				t.Errorf("KeycloakRealmImport namespace = %q, want keycloak", m.Namespace)
			}
		default:
			if m.Namespace != d.Namespace {
				t.Errorf("%s/%s namespace = %q, want %q", m.Kind, m.Name, m.Namespace, d.Namespace)
			}
		}
	}
}

// toolIdentityLabelKeys are label keys production's Helm charts set that this
// renderer legitimately cannot: they identify the DEPLOYING TOOL, not the
// resource, and this renderer isn't a chart, so there is nothing true to put
// there. Every key here is one-directional — production carries it, the
// renderer inherently cannot — which is what makes normalizing it away
// inevitable rather than a choice:
//
//   - helm.sh/chart, app.kubernetes.io/version: Helm chart metadata (chart
//     name+semver, chart appVersion — sourced from Chart.yaml).
//   - app.kubernetes.io/managed-by: production says "Helm"; this renderer
//     says "archistrator-operatedRuntimeAccess". These are SUPPOSED to
//     differ — that is the fact the label records — so comparing them would
//     make every workload manifest fail forever, not catch a real bug.
//
// app.kubernetes.io/part-of was REMOVED from deploymentTmpl/serviceTmpl
// rather than normalized here: production sets it nowhere, so it was the
// renderer adding a difference, not Helm creating one the renderer can't
// close. Normalizing it away would have hidden a real, closable gap instead
// of an inevitable one — the whole point of keeping this list minimal.
//
// app.kubernetes.io/name and app.kubernetes.io/instance — the labels
// Deployment/Service selectors actually key off — are deliberately NOT in
// this list and stay compared exactly. If a future manifest silently dropped
// or renamed one of those, this test must still fail.
var toolIdentityLabelKeys = []string{
	"helm.sh/chart",
	"app.kubernetes.io/version",
	"app.kubernetes.io/managed-by",
}

// stripToolIdentityLabels deletes toolIdentityLabelKeys from obj's
// metadata.labels, in place. Applied symmetrically to both the rendered and
// the golden/production object before comparison.
func stripToolIdentityLabels(obj map[string]any) {
	metadata, ok := obj["metadata"].(map[string]any)
	if !ok {
		return
	}
	labels, ok := metadata["labels"].(map[string]any)
	if !ok {
		return
	}
	for _, k := range toolIdentityLabelKeys {
		delete(labels, k)
	}
}

// parseYAMLDocuments decodes a (possibly multi-document, "---"-separated)
// YAML stream into generic objects. Using yaml.Decoder rather than splitting
// the text on "---" is what makes Helm's document separators and its
// "# Source: <chart>/templates/<file>" comment lines disappear for free:
// YAML comments are never part of the parsed structure, so there is nothing
// to strip by hand, and the comparison below is over structure, not text.
func parseYAMLDocuments(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var out []map[string]any
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("parse YAML document: %v", err)
		}
		if doc == nil { // a stray leading/trailing "---" decodes as a nil document
			continue
		}
		out = append(out, doc)
	}
	return out
}

// objKey is a (kind, name) pair — enough to match a rendered manifest to its
// golden document within one golden file, since no golden file here mixes
// two objects of the same kind and name.
type objKey struct{ kind, name string }

func keyOf(obj map[string]any) objKey {
	k, _ := obj["kind"].(string)
	var n string
	if md, ok := obj["metadata"].(map[string]any); ok {
		n, _ = md["name"].(string)
	}
	return objKey{k, n}
}

// compareAgainstProductionGolden reads the production golden file named golden,
// parses it into (kind, name)-keyed documents, and compares each of keys against
// both rendered and the golden — reporting via t.Errorf/t.Fatalf exactly as
// TestRender_MatchesProductionGoldens's per-golden-file loop body did before this
// was pulled out into its own function. Every key checked is marked in covered
// (whether or not the comparison against it succeeded), which is what the
// caller's follow-up drift check (every rendered manifest is either covered or
// listed in noProductionCounterpart) relies on.
func compareAgainstProductionGolden(t *testing.T, golden string, keys []objKey, rendered map[objKey]manifest, covered map[objKey]bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "golden", "production", golden))
	if err != nil {
		t.Fatalf("read golden %s: %v", golden, err)
	}
	goldenDocs := parseYAMLDocuments(t, data)
	if len(goldenDocs) != len(keys) {
		t.Errorf("%s: production golden has %d objects, this test expects %d — the golden and the object list below have drifted", golden, len(goldenDocs), len(keys))
	}
	goldenByKey := map[objKey]map[string]any{}
	for _, doc := range goldenDocs {
		goldenByKey[keyOf(doc)] = doc
	}

	for _, key := range keys {
		covered[key] = true
		m, ok := rendered[key]
		if !ok {
			t.Errorf("%s: renderer did not emit %s/%s, which production golden has", golden, key.kind, key.name)
			continue
		}
		gdoc, ok := goldenByKey[key]
		if !ok {
			t.Errorf("%s: production golden has no %s/%s", golden, key.kind, key.name)
			continue
		}
		var rdoc map[string]any
		if err := yaml.Unmarshal([]byte(m.YAML), &rdoc); err != nil {
			t.Fatalf("%s: parse rendered %s/%s: %v", golden, key.kind, key.name, err)
		}
		stripToolIdentityLabels(rdoc)
		stripToolIdentityLabels(gdoc)
		if !reflect.DeepEqual(rdoc, gdoc) {
			rY, _ := yaml.Marshal(rdoc)
			gY, _ := yaml.Marshal(gdoc)
			t.Errorf("%s: %s/%s differs from production after normalizing tool-identity labels %v:\n--- rendered ---\n%s\n--- production ---\n%s",
				golden, key.kind, key.name, toolIdentityLabelKeys, rY, gY)
		}
	}
}

// TestRender_MatchesProductionGoldens is the acceptance bar this whole plan
// exists to automate: for every object production actually runs, the
// renderer must reproduce it. Objects are matched by (kind, name) and
// compared as PARSED YAML structure, not raw text — so document ordering,
// Helm's "---"/"# Source:" artifacts, and differing prose comments cannot
// masquerade as parity, and the only edits applied before comparing are the
// small, explicit, commented normalization in stripToolIdentityLabels.
//
// Two rendered objects have NO production counterpart and are deliberately
// excluded here — production's Keycloak realm is hand-managed in the admin
// console (see renderKeycloakRealm's doc), and production splits archistrator
// across four separate Argo Applications where this renderer emits one (spec
// D2/D3/D4). Both are covered by their own dedicated tests elsewhere in this
// file (TestRender_KeycloakRealmImportRealmClientAndRedirectURIs and the
// Argo-Application tests above) — excluding them from THIS test is not the
// same as leaving them untested.
func TestRender_MatchesProductionGoldens(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	rendered := map[objKey]manifest{}
	for _, m := range ms {
		rendered[objKey{m.Kind, m.Name}] = m
	}

	noProductionCounterpart := map[objKey]bool{
		{"KeycloakRealmImport", "archistrator-realm"}: true,
		{"Application", "archistrator"}:               true,
	}

	goldenObjects := map[string][]objKey{
		"archistrator-server.yaml": {
			{"Deployment", "archistrator-server"},
			{"Service", "archistrator-server"},
		},
		"archistrator-webapp.yaml": {
			{"Deployment", "archistrator-webapp"},
			{"Service", "archistrator-webapp"},
		},
		"archistrator-postgres.yaml": {
			{"Cluster", "archistrator-postgres"},
		},
		"archistrator-gateway-routes.yaml": {
			{"HTTPRoute", "archistrator-api-route"},
			{"HTTPRoute", "archistrator-healthz-route"},
			{"HTTPRoute", "archistrator-readyz-route"},
			{"HTTPRoute", "archistrator-webapp-route"},
			{"BackendTrafficPolicy", "archistrator-api-route-policy"},
			{"BackendTrafficPolicy", "archistrator-healthz-route-policy"},
			{"BackendTrafficPolicy", "archistrator-readyz-route-policy"},
			{"BackendTrafficPolicy", "archistrator-webapp-route-policy"},
			{"SecurityPolicy", "archistrator-oidc-policy"},
		},
	}

	covered := map[objKey]bool{}
	for golden, keys := range goldenObjects {
		compareAgainstProductionGolden(t, golden, keys, rendered, covered)
	}

	// Every rendered manifest must be accounted for: either matched against a
	// golden document above, or explicitly named as having no production
	// counterpart. A manifest landing in neither bucket means this test
	// silently stopped covering an object — usually because a new render*
	// section was added and nobody updated goldenObjects/noProductionCounterpart
	// — and that is exactly the kind of drift this task exists to catch.
	for key := range rendered {
		if !covered[key] && !noProductionCounterpart[key] {
			t.Errorf("%s/%s is rendered but neither compared against a production golden nor listed in noProductionCounterpart — this test's coverage has drifted from render()", key.kind, key.name)
		}
	}
	if len(rendered) != len(covered)+len(noProductionCounterpart) {
		t.Errorf("rendered %d manifests, but golden-covered (%d) + no-production-counterpart (%d) = %d — counts should add up",
			len(rendered), len(covered), len(noProductionCounterpart), len(covered)+len(noProductionCounterpart))
	}
}

// ---------------------------------------------------------------------------
// GitOps commit-path tests — Task 7 of the 2026-08-07 operations/ArgoCD plan.
//
// HARD SAFETY BOUNDARY: every test below operates on a scratch repo created
// under t.TempDir() with a file:// remote. NONE of them may point at, clone,
// commit to, or push to the real GitOps repository
// (https://github.com/davidmarne/aiarchmultiplatform.git, checked out locally
// at /Users/davidmarne/mixofrealitystudio/software) or any other real remote
// — see the task brief's HARD SAFETY BOUNDARY section.
// ---------------------------------------------------------------------------

// mustRun runs a command in dir, failing the test immediately on error. Test-only
// scratch-repo setup — never used against anything but a t.TempDir() fixture.
func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...) //nolint:gosec // fixed trusted binary, test-only fixed args, never user input
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v: %s", name, strings.Join(args, " "), err, out)
	}
}

// headCommit returns repo's current HEAD commit SHA, failing the test if repo has no
// commits — a before/after comparison is meaningless without that guarantee. Works
// directly against repo whether it is bare or not. Uses `--verify`: plain `rev-parse
// HEAD` against a BARE repo with an unborn branch exits 0 and prints the literal string
// "HEAD" instead of failing (a bare repo has no working tree to fail the "is this a path"
// check against, unlike a non-bare repo, where the same command correctly exits
// non-zero) — `--verify` closes that gap and fails consistently in both cases.
func headCommit(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "HEAD") //nolint:gosec // fixed trusted binary, fixed args
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse --verify HEAD in %s: %v: %s", repo, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repoHasNoCommits reports whether repo has never been committed to (an unborn HEAD) —
// the state a NotFound⇒success withdraw that touches nothing must leave it in. See
// headCommit's doc for why `--verify` is required here, not plain `rev-parse HEAD`.
func repoHasNoCommits(t *testing.T, repo string) bool {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "HEAD") //nolint:gosec // fixed trusted binary, fixed args
	cmd.Dir = repo
	return cmd.Run() != nil
}

// testCtx builds the fwra.Context the GitOps commit-path tests call the real profile
// with. A plain background context — none of these tests exercise cancellation.
func testCtx(t *testing.T) fwra.Context {
	t.Helper()
	return fwra.Context{Context: context.Background()}
}

// newScratchRepo creates a fresh, empty (no commits) BARE git repo under t.TempDir(), the
// ONLY kind of repo any test in this file may operate on (see the HARD SAFETY BOUNDARY
// above). Bare, not a plain working copy, so these tests exercise the exact same kind of
// remote gitOpsCommit talks to in production (GitHub repos are bare) — a non-bare "git
// refuses to push into its own checked-out branch" workaround has no place in the
// production code path, and making the test fixture bare is what lets that workaround be
// deleted entirely rather than kept alive only for tests. Returns the repo's filesystem
// path; inspect its committed content via inspectClone, never by statting this path
// directly (a bare repo has no working tree).
func newScratchRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	mustRun(t, repo, "git", "init", "--bare", "--initial-branch=main")
	return repo
}

// inspectClone clones repo (a bare scratch "remote" from newScratchRepo) into a fresh,
// ordinary temp directory so a test can assert on committed file paths/contents with
// plain os.Stat/os.ReadFile — exactly how ArgoCD's own clone would see the repo. Keeps
// all "how do I look inside a bare repo" concern in test code, never in gitOpsCommit.
func inspectClone(t *testing.T, repo string) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, "", "git", "clone", "file://"+repo, dir)
	return dir
}

// seedBareRepo commits raw content directly into repo at relPath, entirely bypassing
// PublishDesiredState. Used only to construct Application documents this component did
// NOT itself write — a hand-edited or pre-existing entry — which is exactly what the
// fail-closed determination tests below need to exist in order to exercise it.
func seedBareRepo(t *testing.T, repo, relPath, content string) {
	t.Helper()
	clone := t.TempDir()
	mustRun(t, "", "git", "clone", "file://"+repo, clone)
	mustRun(t, clone, "git", "config", "user.email", "seed@example.com")
	mustRun(t, clone, "git", "config", "user.name", "seed")
	full := filepath.Join(clone, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("seedBareRepo: mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("seedBareRepo: write: %v", err)
	}
	mustRun(t, clone, "git", "add", "-A")
	mustRun(t, clone, "git", "commit", "-m", "seed")
	mustRun(t, clone, "git", "push", "origin", "HEAD:main")
}

func TestPublishDesiredState_WritesManifestsAndIsContentIdempotent(t *testing.T) {
	repo := newScratchRepo(t)

	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	appID := uuid.New()

	if err := rt.PublishDesiredState(testCtx(t), appID, testDesiredState(), "idem-1"); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	first := headCommit(t, repo)

	appPath := filepath.Join(inspectClone(t, repo), "k8s", "argocd", "apps", "archistrator")
	if _, err := os.Stat(appPath); err != nil {
		t.Fatalf("expected rendered manifests at %s: %v", appPath, err)
	}

	// Identical content must NOT produce a second commit.
	if err := rt.PublishDesiredState(testCtx(t), appID, testDesiredState(), "idem-2"); err != nil {
		t.Fatalf("republish: %v", err)
	}
	if headCommit(t, repo) != first {
		t.Error("republishing identical content created a new commit; publish must be content-idempotent")
	}
}

// TestPublishDesiredState_ApplicationCarriesAppIDAnnotation is the write-side half of the
// annotation mechanism gitOpsResolveAppByID reads back on Withdraw (SEAM note 1): if the
// annotation is ever missing or malformed, Withdraw can never find this app again by
// appID and it would silently keep running forever.
func TestPublishDesiredState_ApplicationCarriesAppIDAnnotation(t *testing.T) {
	repo := newScratchRepo(t)
	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	appID := uuid.New()

	if err := rt.PublishDesiredState(testCtx(t), appID, testDesiredState(), "idem-1"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	appFile := filepath.Join(inspectClone(t, repo), "k8s", "argocd", "applications", "archistrator.yaml")
	raw, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatalf("read %s: %v", appFile, err)
	}
	got, ok := gitOpsAppIDAnnotationValue(string(raw))
	if !ok {
		t.Fatalf("no %s annotation found in %s:\n%s", gitOpsAppIDAnnotation, appFile, raw)
	}
	if got != appID.String() {
		t.Errorf("archistrator.dev/app-id annotation = %q, want %q", got, appID.String())
	}
}

// TestPublishDesiredState_ChangedContentCreatesNewCommit is the necessary flip side of
// the idempotency test above: without it, an implementation that never commits ANYTHING
// would trivially pass "republishing produces no new commit". A real content change must
// still land.
func TestPublishDesiredState_ChangedContentCreatesNewCommit(t *testing.T) {
	repo := newScratchRepo(t)
	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	appID := uuid.New()

	if err := rt.PublishDesiredState(testCtx(t), appID, testDesiredState(), "idem-1"); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	first := headCommit(t, repo)

	changed := testDesiredState()
	changed.Server.Replicas = 3
	if err := rt.PublishDesiredState(testCtx(t), appID, changed, "idem-2"); err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if second := headCommit(t, repo); second == first {
		t.Error("a genuinely changed desired state did not produce a new commit")
	}
}

// TestWithdraw_RemovesTheAppDirectory covers the happy removal path for a NON-self-managed
// app. testDesiredState() defaults to SelfManaged: true (it is archistrator's own
// fixture), so this test explicitly flips it — withdrawing the self-managed flavor of this
// exact fixture is covered separately by TestWithdraw_RefusesTheSelfManagedApp, and must
// NOT succeed (see that test).
func TestWithdraw_RemovesTheAppDirectory(t *testing.T) {
	repo := newScratchRepo(t)
	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	appID := uuid.New()

	tenant := testDesiredState()
	tenant.SelfManaged = false

	if err := rt.PublishDesiredState(testCtx(t), appID, tenant, "idem-1"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := rt.Withdraw(testCtx(t), appID, "idem-2"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	appPath := filepath.Join(inspectClone(t, repo), "k8s", "argocd", "apps", "archistrator")
	if _, err := os.Stat(appPath); !os.IsNotExist(err) {
		t.Errorf("withdraw left %s behind", appPath)
	}
}

// TestWithdraw_RemovesApplicationFile rounds out the removal test above: the governing
// Application object (gitOpsResolveAppByID's whole mechanism lives inside it — no
// separate sidecar file to also clean up) must go too, or a re-publish/re-withdraw cycle
// would leave a stale, orphaned Application behind in the GitOps repo.
func TestWithdraw_RemovesApplicationFile(t *testing.T) {
	repo := newScratchRepo(t)
	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	appID := uuid.New()

	tenant := testDesiredState()
	tenant.SelfManaged = false

	if err := rt.PublishDesiredState(testCtx(t), appID, tenant, "idem-1"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := rt.Withdraw(testCtx(t), appID, "idem-2"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	appFile := filepath.Join(inspectClone(t, repo), "k8s", "argocd", "applications", "archistrator.yaml")
	if _, err := os.Stat(appFile); !os.IsNotExist(err) {
		t.Errorf("withdraw left %s behind", appFile)
	}
}

// TestWithdraw_OfUnknownAppIsSuccessAndCreatesNoCommit: withdrawing an app that was never
// published (or already withdrawn) is success — matching the contract's NotFound⇒success
// withdraw semantics — and must not create an empty/no-op commit either: nothing is
// staged, so gitOpsCommit's content-idempotency check must skip the commit entirely.
func TestWithdraw_OfUnknownAppIsSuccessAndCreatesNoCommit(t *testing.T) {
	repo := newScratchRepo(t)
	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}

	if err := rt.Withdraw(testCtx(t), uuid.New(), "idem-1"); err != nil {
		t.Fatalf("withdraw of an unknown app: want success, got %v", err)
	}
	if !repoHasNoCommits(t, repo) {
		t.Error("withdraw of an unknown app created a commit; NotFound must be a true no-op")
	}
}

// TestWithdraw_RefusesTheSelfManagedApp closes the third of three paths to deleting
// archistrator's own control plane — prune:false and the omitted Argo finalizer
// (renderApplication) close the other two. The guard reads NO config (there is none to
// read — RuntimeConfig carries no self-managed identity, on purpose: see Withdraw's doc
// and the GitOps commit path section doc for why an earlier, config-driven version of
// this check was rejected as fail-open). It must fire terminally and BEFORE anything is
// staged: the already-published app directory must be untouched and no commit may be
// created, because there is nothing recoverable about this outcome if it fires wrong.
func TestWithdraw_RefusesTheSelfManagedApp(t *testing.T) {
	repo := newScratchRepo(t)
	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	appID := uuid.New()

	// testDesiredState() IS the self-managed fixture (SelfManaged: true, AppName
	// "archistrator") — used as-is, deliberately not overridden, unlike every other test
	// in this file.
	if err := rt.PublishDesiredState(testCtx(t), appID, testDesiredState(), "idem-1"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	before := headCommit(t, repo)

	err := rt.Withdraw(testCtx(t), appID, "idem-2")
	var e *fwra.Error
	if !errors.As(err, &e) || e.Kind != fwra.ContractMisuse {
		t.Fatalf("withdraw of the self-managed app: want ContractMisuse, got %v", err)
	}
	if e.Retryable {
		t.Fatalf("self-managed withdraw guard must be non-retryable (terminal), got retryable")
	}
	if !strings.Contains(e.Detail, "archistrator") {
		t.Errorf("guard error should name the app: %q does not mention archistrator", e.Detail)
	}

	appPath := filepath.Join(inspectClone(t, repo), "k8s", "argocd", "apps", "archistrator")
	if _, statErr := os.Stat(appPath); statErr != nil {
		t.Fatalf("guard must refuse before anything is staged — app directory should be untouched: %v", statErr)
	}
	if headCommit(t, repo) != before {
		t.Error("self-managed withdraw guard must not create a commit")
	}
}

// TestWithdraw_DoesNotAffectOtherAppsInTheSameRepo: the self-managed determination must be
// PER-APPLICATION (read from the specific Application gitOpsResolveAppByID matches), not a
// blanket switch — withdrawing a tenant app must succeed and must leave a self-managed
// app published alongside it in the very same repo untouched.
func TestWithdraw_DoesNotAffectOtherAppsInTheSameRepo(t *testing.T) {
	repo := newScratchRepo(t)
	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	selfManagedID := uuid.New()
	tenantID := uuid.New()

	if err := rt.PublishDesiredState(testCtx(t), selfManagedID, testDesiredState(), "idem-1"); err != nil {
		t.Fatalf("publish self-managed: %v", err)
	}
	tenant := testDesiredState()
	tenant.AppName = "tenant-app"
	tenant.SelfManaged = false
	if err := rt.PublishDesiredState(testCtx(t), tenantID, tenant, "idem-2"); err != nil {
		t.Fatalf("publish tenant: %v", err)
	}

	if err := rt.Withdraw(testCtx(t), tenantID, "idem-3"); err != nil {
		t.Fatalf("withdraw of a non-self-managed app: want success, got %v", err)
	}

	clone := inspectClone(t, repo)
	if _, err := os.Stat(filepath.Join(clone, "k8s", "argocd", "apps", "tenant-app")); !os.IsNotExist(err) {
		t.Errorf("withdraw left the tenant app's directory behind")
	}
	if _, err := os.Stat(filepath.Join(clone, "k8s", "argocd", "apps", "archistrator")); err != nil {
		t.Errorf("withdraw of the tenant app affected the unrelated self-managed app: %v", err)
	}

	// And the self-managed one, published in this same repo, is still refused.
	if err := rt.Withdraw(testCtx(t), selfManagedID, "idem-4"); err == nil {
		t.Error("withdraw of the self-managed app succeeded after an unrelated tenant withdraw")
	}
}

// TestWithdraw_SkipsUnrelatedApplicationsAndStillFindsTheRealOne: gitOpsResolveAppByID
// scans every file under k8s/argocd/applications/, but a file that simply does not carry
// the annotation being searched for (garbage, hand-authored, or otherwise foreign content)
// must not stop it from finding the real match elsewhere in the same directory.
func TestWithdraw_SkipsUnrelatedApplicationsAndStillFindsTheRealOne(t *testing.T) {
	repo := newScratchRepo(t)
	seedBareRepo(t, repo, "k8s/argocd/applications/mystery.yaml", "not: [valid: yaml at all {{{\n  - broken\n")

	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	appID := uuid.New()
	tenant := testDesiredState()
	tenant.SelfManaged = false
	if err := rt.PublishDesiredState(testCtx(t), appID, tenant, "idem-1"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := rt.Withdraw(testCtx(t), appID, "idem-2"); err != nil {
		t.Fatalf("withdraw alongside an unrelated foreign Application file: want success, got %v", err)
	}
	appPath := filepath.Join(inspectClone(t, repo), "k8s", "argocd", "apps", "archistrator")
	if _, err := os.Stat(appPath); !os.IsNotExist(err) {
		t.Errorf("withdraw left %s behind", appPath)
	}
}

// TestGitOpsSelfManaged_MatchesByPositionNotSubstring is a direct unit test of the
// function that closes the Critical review finding: it must key off the FIRST
// substantive line after "  syncPolicy:", not off searching the whole document for
// "automated:" anywhere. A decoy "automated:"-looking text earlier in the document (e.g.
// inside an unrelated comment) must not flip a genuinely self-managed Application to
// "not self-managed" — that is the dangerous direction to get this wrong, since it is
// exactly what would let Withdraw delete it.
func TestGitOpsSelfManaged_MatchesByPositionNotSubstring(t *testing.T) {
	selfManagedYAML, sErr := renderTemplate(applicationTmpl, applicationData{
		AppName: "archistrator", Namespace: "archistrator", RepoURL: "https://example.invalid/repo.git",
		Path: "k8s/argocd/apps/archistrator", SelfManaged: true,
	})
	if sErr != nil {
		t.Fatalf("render self-managed fixture: %v", sErr)
	}
	tenantYAML, tErr := renderTemplate(applicationTmpl, applicationData{
		AppName: "tenant", Namespace: "tenant", RepoURL: "https://example.invalid/repo.git",
		Path: "k8s/argocd/apps/tenant", SelfManaged: false,
	})
	if tErr != nil {
		t.Fatalf("render tenant fixture: %v", tErr)
	}

	if got, ok := gitOpsSelfManaged(selfManagedYAML); !ok || !got {
		t.Errorf("self-managed fixture: got (selfManaged=%v, ok=%v), want (true, true)", got, ok)
	}
	if got, ok := gitOpsSelfManaged(tenantYAML); !ok || got {
		t.Errorf("tenant fixture: got (selfManaged=%v, ok=%v), want (false, true)", got, ok)
	}

	// The decoy: a comment mentioning "automated:" BEFORE syncPolicy, on an otherwise
	// genuinely self-managed document. Must still resolve to self-managed=true.
	decoy := "# this workflow used to be automated:\n" + selfManagedYAML
	if got, ok := gitOpsSelfManaged(decoy); !ok || !got {
		t.Errorf("decoyed self-managed fixture: got (selfManaged=%v, ok=%v), want (true, true) — a substring match elsewhere was wrongly used", got, ok)
	}

	if _, ok := gitOpsSelfManaged("kind: Application\nmetadata:\n  name: x\n"); ok {
		t.Error("a document with no syncPolicy at all must not report ok=true")
	}

	// Multi-document input must never anchor on the first document's syncPolicy: a
	// hand-edited file with a tenant document first and a self-managed document second is
	// the last remaining way a self-managed Application could misread as a tenant if this
	// weren't rejected outright (see gitOpsHasMultipleDocuments).
	if _, ok := gitOpsSelfManaged(tenantYAML + "---\n" + selfManagedYAML); ok {
		t.Error("multi-document input (--- separator) must report ok=false, not silently resolve off the first document")
	}
	if _, ok := gitOpsSelfManaged(tenantYAML + selfManagedYAML); ok {
		t.Error("multi-document input (two kind: Application lines, no --- separator) must report ok=false")
	}
}

// TestWithdraw_FailsClosedWhenSelfManagedStatusCannotBeDetermined: an Application that
// parses fine, and DOES carry the matching appID annotation, but has no spec.syncPolicy at
// all does not resemble anything applicationTmpl could have rendered. gitOpsResolveAppByID
// must refuse to guess which way that ambiguity resolves — the whole point of the Critical
// review finding this closes is that "cannot tell" must never silently mean "safe to
// delete".
func TestWithdraw_FailsClosedWhenSelfManagedStatusCannotBeDetermined(t *testing.T) {
	repo := newScratchRepo(t)
	appID := uuid.New()
	noSyncPolicy := "apiVersion: argoproj.io/v1alpha1\n" +
		"kind: Application\n" +
		"metadata:\n" +
		"  name: mystery\n" +
		"  namespace: argocd\n" +
		"  annotations:\n" +
		"    " + gitOpsAppIDAnnotation + ": " + appID.String() + "\n" +
		"spec:\n" +
		"  project: default\n"
	seedBareRepo(t, repo, "k8s/argocd/applications/mystery.yaml", noSyncPolicy)

	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	before := headCommit(t, repo)

	err := rt.Withdraw(testCtx(t), appID, "idem-1")
	var e *fwra.Error
	if !errors.As(err, &e) || e.Kind != fwra.ContractMisuse {
		t.Fatalf("withdraw of an app whose syncPolicy shape is unrecognizable: want ContractMisuse, got %v", err)
	}
	if e.Retryable {
		t.Fatalf("an undeterminable self-managed status must fail closed non-retryably, got retryable")
	}
	if headCommit(t, repo) != before {
		t.Error("a failed determination must not still push a commit")
	}
}

// TestWithdraw_FailsClosedOnMultiDocumentApplicationFile is the end-to-end version of the
// last case in TestGitOpsSelfManaged_MatchesByPositionNotSubstring: a hand-edited file
// holding a TENANT document (carrying the matching annotation, automated: present) first
// and a SELF-MANAGED document second. Anchoring on the first document's syncPolicy would
// read this as safe to delete — the last remaining way a self-managed Application could
// misread as a tenant. Withdraw must refuse instead.
func TestWithdraw_FailsClosedOnMultiDocumentApplicationFile(t *testing.T) {
	repo := newScratchRepo(t)
	appID := uuid.New()

	tenantDoc := "apiVersion: argoproj.io/v1alpha1\n" +
		"kind: Application\n" +
		"metadata:\n" +
		"  name: mystery\n" +
		"  namespace: argocd\n" +
		"  annotations:\n" +
		"    " + gitOpsAppIDAnnotation + ": " + appID.String() + "\n" +
		"spec:\n" +
		"  project: default\n" +
		"  syncPolicy:\n" +
		"    automated:\n" +
		"      prune: true\n" +
		"      selfHeal: true\n" +
		"    syncOptions:\n" +
		"    - CreateNamespace=true\n"
	selfManagedDoc := "apiVersion: argoproj.io/v1alpha1\n" +
		"kind: Application\n" +
		"metadata:\n" +
		"  name: mystery\n" +
		"  namespace: argocd\n" +
		"spec:\n" +
		"  project: default\n" +
		"  syncPolicy:\n" +
		"    syncOptions:\n" +
		"    - CreateNamespace=true\n"
	seedBareRepo(t, repo, "k8s/argocd/applications/mystery.yaml", tenantDoc+"---\n"+selfManagedDoc)

	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	before := headCommit(t, repo)

	err := rt.Withdraw(testCtx(t), appID, "idem-1")
	var e *fwra.Error
	if !errors.As(err, &e) || e.Kind != fwra.ContractMisuse {
		t.Fatalf("withdraw of a multi-document Application file: want ContractMisuse (refuse), got %v — a tenant-first read would have allowed a self-managed delete", err)
	}
	if e.Retryable {
		t.Fatalf("multi-document refusal must be non-retryable, got retryable")
	}
	if headCommit(t, repo) != before {
		t.Error("a failed determination must not still push a commit")
	}
}

// ---------------------------------------------------------------------------
// The FOURTH path to self-destruction (2026-08-08 final review, fix 5): a publish
// that rewrites a self-managed Application in tenant shape disarms prune:false, the
// omitted finalizer AND the Withdraw refusal in one commit, because all three derive
// from the same single boolean.
// ---------------------------------------------------------------------------

// TestPublishDesiredState_RefusesSelfManagedDowngrade: the committed Application is
// self-managed and the incoming desired state is not. Nothing may be written — not the
// Application, not the app directory — and the previously committed self-managed shape
// must survive byte-for-byte.
func TestPublishDesiredState_RefusesSelfManagedDowngrade(t *testing.T) {
	repo := newScratchRepo(t)
	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	appID := uuid.New()

	// testDesiredState() IS the self-managed fixture.
	if err := rt.PublishDesiredState(testCtx(t), appID, testDesiredState(), "idem-1"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	before := headCommit(t, repo)
	appFile := filepath.Join(inspectClone(t, repo), "k8s", "argocd", "applications", "archistrator.yaml")
	original, err := os.ReadFile(appFile)
	if err != nil {
		t.Fatalf("read %s: %v", appFile, err)
	}

	tenant := testDesiredState()
	tenant.SelfManaged = false // the whole attack: one flipped boolean.
	perr := rt.PublishDesiredState(testCtx(t), appID, tenant, "idem-2")

	var e *fwra.Error
	if !errors.As(perr, &e) || e.Kind != fwra.ContractMisuse {
		t.Fatalf("self-managed -> tenant downgrade: want ContractMisuse (refuse), got %v", perr)
	}
	if e.Retryable {
		t.Fatal("the downgrade refusal must be terminal, not retryable")
	}
	if headCommit(t, repo) != before {
		t.Fatal("a refused downgrade must not create a commit")
	}
	after, rerr := os.ReadFile(filepath.Join(inspectClone(t, repo), "k8s", "argocd", "applications", "archistrator.yaml"))
	if rerr != nil {
		t.Fatalf("re-read %s: %v", appFile, rerr)
	}
	if string(after) != string(original) {
		t.Error("the committed self-managed Application was modified by a refused publish")
	}
	if selfManaged, ok := gitOpsSelfManaged(string(after)); !ok || !selfManaged {
		t.Errorf("the committed Application is no longer self-managed (selfManaged=%v ok=%v)", selfManaged, ok)
	}
}

// TestPublishDesiredState_SelfManagedRepublishIsNotADowngrade: the guard must not brick
// archistrator's own redeploys. A self-managed publish over a self-managed Application is
// the normal case and must go through.
func TestPublishDesiredState_SelfManagedRepublishIsNotADowngrade(t *testing.T) {
	repo := newScratchRepo(t)
	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	appID := uuid.New()

	if err := rt.PublishDesiredState(testCtx(t), appID, testDesiredState(), "idem-1"); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	changed := testDesiredState()
	changed.Server.Image = "ghcr.io/example/archistrator-server:0.9.0"
	if err := rt.PublishDesiredState(testCtx(t), appID, changed, "idem-2"); err != nil {
		t.Fatalf("self-managed republish must be allowed: %v", err)
	}
}

// TestPublishDesiredState_RefusesWhenExistingShapeIsUndeterminable: a hand-edited
// Application whose syncPolicy is not one this renderer could have produced cannot be
// confirmed NOT self-managed, so a tenant-shaped publish over it is refused rather than
// guessed — the same fail-closed direction Withdraw already takes.
func TestPublishDesiredState_RefusesWhenExistingShapeIsUndeterminable(t *testing.T) {
	repo := newScratchRepo(t)
	mystery := "apiVersion: argoproj.io/v1alpha1\n" +
		"kind: Application\n" +
		"metadata:\n" +
		"  name: archistrator\n" +
		"  namespace: argocd\n" +
		"spec:\n" +
		"  project: default\n"
	seedBareRepo(t, repo, "k8s/argocd/applications/archistrator.yaml", mystery)

	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	before := headCommit(t, repo)

	tenant := testDesiredState()
	tenant.SelfManaged = false
	err := rt.PublishDesiredState(testCtx(t), uuid.New(), tenant, "idem-1")

	var e *fwra.Error
	if !errors.As(err, &e) || e.Kind != fwra.ContractMisuse {
		t.Fatalf("publish over an undeterminable Application: want ContractMisuse, got %v", err)
	}
	if headCommit(t, repo) != before {
		t.Error("a refused publish must not create a commit")
	}
}

// ---------------------------------------------------------------------------
// AppName is a path segment (2026-08-08 final review, fix 4). It originates in a
// user-supplied repository name and reaches os.RemoveAll.
// ---------------------------------------------------------------------------

func TestPublishDesiredState_RejectsAnUnsafeAppName(t *testing.T) {
	for _, name := range []string{
		"../../etc",
		"/absolute",
		"..",
		".",
		"has/slash",
		"Upper",
		"-leading-hyphen",
		"trailing-hyphen-",
		"has space",
	} {
		t.Run(name, func(t *testing.T) {
			repo := newScratchRepo(t)
			rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}

			d := testDesiredState()
			d.AppName = name
			err := rt.PublishDesiredState(testCtx(t), uuid.New(), d, "idem-1")

			var e *fwra.Error
			if !errors.As(err, &e) || e.Kind != fwra.ContractMisuse {
				t.Fatalf("AppName %q: want ContractMisuse, got %v", name, err)
			}
			// The check runs before the repository is even cloned, so the scratch
			// remote is still empty — nothing was written anywhere.
			if _, cerr := runGit(repo, "rev-parse", "--verify", "HEAD"); cerr == nil {
				t.Errorf("AppName %q: a rejected publish must not create a commit", name)
			}
		})
	}
}

func TestValidAppName_AcceptsRealAppNames(t *testing.T) {
	for _, name := range []string{"archistrator", "gtdapp", "a", "a-b-c", "app123"} {
		if !validAppName(name) {
			t.Errorf("validAppName(%q) = false, want true", name)
		}
	}
	if validAppName(strings.Repeat("a", 64)) {
		t.Error("a 64-character app name must be rejected (DNS-1123 labels stop at 63)")
	}
}
