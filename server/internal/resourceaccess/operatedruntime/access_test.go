// package operatedruntime (internal test package, not operatedruntime_test):
// the render tests below call the unexported render() func and construct
// Manifest values directly. TestFileLayout is zero-waiver on file COUNT, not
// package clause, so every test in this component — the profile-selection
// tests migrated from the former external test package, plus the renderer
// tests — lives in this one access_test.go.
package operatedruntime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

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
		AppName:   "archistrator",
		Namespace: "archistrator",
		Host:      "archistrator.capture-gtd.com",
		ModelKey:  "cloud-node-ns-archistrator",
		Server: Workload{
			ModelKey: "cloud-node-server-deployment",
			Image:    "ghcr.io/mixofreality-studio/archistrator-server:0.8.16",
			Replicas: 1,
		},
		WebApp: Workload{
			ModelKey: "cloud-infra-static-assets",
			Image:    "ghcr.io/mixofreality-studio/archistrator-webapp:0.6.14",
			Replicas: 1,
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
// promises: Deployment before Service (alphabetical Kind), and within each
// Kind, archistrator-server before archistrator-webapp.
func TestRender_OutputIsSortedByKindThenName(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(ms) != 4 {
		t.Fatalf("manifest count = %d, want 4 (server+webapp x Deployment+Service)", len(ms))
	}
	want := []struct{ Kind, Name string }{
		{"Deployment", "archistrator-server"},
		{"Deployment", "archistrator-webapp"},
		{"Service", "archistrator-server"},
		{"Service", "archistrator-webapp"},
	}
	for i, w := range want {
		if ms[i].Kind != w.Kind || ms[i].Name != w.Name {
			t.Errorf("manifest[%d] = %s/%s, want %s/%s", i, ms[i].Kind, ms[i].Name, w.Kind, w.Name)
		}
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
