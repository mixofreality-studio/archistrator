// package operatedruntime (internal test package, not operatedruntime_test):
// the render tests below call the unexported render() func and construct
// Manifest values directly. TestFileLayout is zero-waiver on file COUNT, not
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
	"path/filepath"
	"reflect"
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

// TestRealProfileExplicitNotImplemented: the REAL profile constructs (server still boots)
// but every verb returns an explicit, diagnosable error naming the follow-up — NOT a
// silent generated stub, and it preserves the non-retryable wire behaviour.
func TestRealProfileExplicitNotImplemented(t *testing.T) {
	rt, err := NewProfiledOperatedRuntimeAccess(RuntimeProfileReal, RuntimeConfig{})
	if err != nil {
		t.Fatalf("real build should not fail at construction (preserves boot): %v", err)
	}
	app := uuid.New()

	perr := rt.PublishDesiredState(rc(), app, RuntimeDesiredState{}, "k")
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
	var found *Manifest
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

	var svc *Manifest
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
		var found *Manifest
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
		// Manifest carries a []string (ModelKeys), so it is not comparable with
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
// apiVersion/kind/metadata against the Manifest's own Kind/Name/Namespace, so
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
			t.Errorf("%s/%s: YAML metadata.namespace = %q, Manifest.Namespace = %q", m.Kind, m.Name, obj.Metadata.Namespace, m.Namespace)
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
	var app *Manifest
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
		var app *Manifest
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
	routes := map[string]*Manifest{}
	policies := map[string]*Manifest{}
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
	var cr *Manifest
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
	var policy, cr *Manifest
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
	var server *Manifest
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
func stripToolIdentityLabels(obj map[string]interface{}) {
	metadata, ok := obj["metadata"].(map[string]interface{})
	if !ok {
		return
	}
	labels, ok := metadata["labels"].(map[string]interface{})
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
func parseYAMLDocuments(t *testing.T, data []byte) []map[string]interface{} {
	t.Helper()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var out []map[string]interface{}
	for {
		var doc map[string]interface{}
		err := dec.Decode(&doc)
		if err == io.EOF {
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

// objKey is a (kind, name) pair — enough to match a rendered Manifest to its
// golden document within one golden file, since no golden file here mixes
// two objects of the same kind and name.
type objKey struct{ kind, name string }

func keyOf(obj map[string]interface{}) objKey {
	k, _ := obj["kind"].(string)
	var n string
	if md, ok := obj["metadata"].(map[string]interface{}); ok {
		n, _ = md["name"].(string)
	}
	return objKey{k, n}
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
	rendered := map[objKey]Manifest{}
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
		data, err := os.ReadFile(filepath.Join("testdata", "golden", "production", golden))
		if err != nil {
			t.Fatalf("read golden %s: %v", golden, err)
		}
		goldenDocs := parseYAMLDocuments(t, data)
		if len(goldenDocs) != len(keys) {
			t.Errorf("%s: production golden has %d objects, this test expects %d — the golden and the object list below have drifted", golden, len(goldenDocs), len(keys))
		}
		goldenByKey := map[objKey]map[string]interface{}{}
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
			var rdoc map[string]interface{}
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
