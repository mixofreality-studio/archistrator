// Hooks for operatedRuntimeAccess scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.
//
// The subject is the REAL profile, built through the generated public constructor
// against real downstream doubles: the scenario host's bare git repository is the GitOps
// repository it commits to, and a per-scenario runtime stub (below) stands in for the two
// other backends the real profile reads — a TLS Kubernetes API serving the Argo
// Application list, and a Prometheus HTTP API. Hooks arrange that stub (and, for an
// in-flight system, publish it through the subject's own PublishDesiredState) so the
// step's guards hold, then call the operation exactly as operationsManager does.

package operatedruntime_test

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
)

// clusterToken is the bearer token the stub Kubernetes API demands; a LIST without it is 401.
const clusterToken = "scenario-cluster-token"

// argoListPath is the one Kubernetes API path the real profile reads.
const argoListPath = "/apis/argoproj.io/v1alpha1/namespaces/argocd/applications"

// stubResource is one status.resources[] entry of a live Argo Application.
type stubResource struct {
	Kind, Name, Namespace, Sync, Health string
}

// stubApp is one live Argo Application: what the cluster reports for an app Argo synced.
type stubApp struct {
	AppID, Name, Namespace, Health string
	Resources                      []stubResource
}

// runtimeStub is the operated runtime's substrate for one scenario: the live Argo
// Applications the Kubernetes API lists, plus the per-namespace series Prometheus answers
// the real profile's two queries from.
type runtimeStub struct {
	mu        sync.Mutex
	apps      []stubApp
	cpu       map[string]float64            // namespace -> CPU-seconds over the queried window
	shortfall map[string]map[string]float64 // namespace -> deployment -> replicas unavailable

	cluster *httptest.Server
	prom    *httptest.Server
}

// stub is the current scenario's runtime stub; newSubject replaces it, and the scenario's
// hooks (which run after newSubject in the same goroutine) arrange it.
var stub *runtimeStub

func newRuntimeStub(t *testing.T) *runtimeStub {
	t.Helper()
	st := &runtimeStub{cpu: map[string]float64{}, shortfall: map[string]map[string]float64{}}
	st.cluster = httptest.NewTLSServer(http.HandlerFunc(st.serveCluster))
	t.Cleanup(st.cluster.Close)
	st.prom = httptest.NewServer(http.HandlerFunc(st.servePrometheus))
	t.Cleanup(st.prom.Close)
	return st
}

// caPEM is the stub API server's certificate, the only CA the subject is told to trust.
func (st *runtimeStub) caPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: st.cluster.Certificate().Raw})
}

func (st *runtimeStub) serveCluster(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+clusterToken {
		http.Error(w, `{"kind":"Status","code":401}`, http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet || r.URL.Path != argoListPath {
		http.NotFound(w, r)
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	items := make([]any, 0, len(st.apps))
	for _, a := range st.apps {
		resources := make([]any, 0, len(a.Resources))
		for _, res := range a.Resources {
			entry := map[string]any{"kind": res.Kind, "name": res.Name, "namespace": res.Namespace, "status": res.Sync}
			if res.Health != "" {
				entry["health"] = map[string]any{"status": res.Health}
			}
			resources = append(resources, entry)
		}
		items = append(items, map[string]any{
			"metadata": map[string]any{
				"name":        a.Name,
				"namespace":   "argocd",
				"annotations": map[string]any{"archistrator.dev/app-id": a.AppID},
			},
			"spec":   map[string]any{"destination": map[string]any{"namespace": a.Namespace}},
			"status": map[string]any{"health": map[string]any{"status": a.Health}, "resources": resources},
		})
	}
	writeJSON(w, map[string]any{"kind": "ApplicationList", "items": items})
}

var namespaceMatcher = regexp.MustCompile(`namespace="([^"]+)"`)

func (st *runtimeStub) servePrometheus(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/query" {
		http.NotFound(w, r)
		return
	}
	query := r.URL.Query().Get("query")
	at, err := strconv.ParseInt(r.URL.Query().Get("time"), 10, 64)
	m := namespaceMatcher.FindStringSubmatch(query)
	if err != nil || m == nil {
		writeJSON(w, map[string]any{"status": "error", "errorType": "bad_data", "error": "unscoped or untimed query"})
		return
	}
	ns := m[1]
	st.mu.Lock()
	defer st.mu.Unlock()
	result := []any{}
	switch {
	case strings.Contains(query, "container_cpu_usage_seconds_total"):
		if v, ok := st.cpu[ns]; ok {
			result = append(result, map[string]any{"metric": map[string]any{}, "value": []any{at, strconv.FormatFloat(v, 'f', -1, 64)}})
		}
	case strings.Contains(query, "kube_deployment_status_replicas_available"):
		for dep, short := range st.shortfall[ns] {
			result = append(result, map[string]any{
				"metric": map[string]any{"namespace": ns, "deployment": dep},
				"value":  []any{at, strconv.FormatFloat(short, 'f', -1, 64)},
			})
		}
	default:
		writeJSON(w, map[string]any{"status": "error", "errorType": "bad_data", "error": "series not stubbed"})
		return
	}
	writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": result}})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// live arranges a: Argo has synced it and the cluster lists it.
func (st *runtimeStub) live(a stubApp) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.apps = append(st.apps, a)
}

// usage arranges the CPU-seconds Prometheus reports for namespace.
func (st *runtimeStub) usage(namespace string, cpuSeconds float64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.cpu[namespace] = cpuSeconds
}

// short arranges deployment in namespace to be missing replicas (an SLO violation).
func (st *runtimeStub) short(namespace, deployment string, replicas float64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.shortfall[namespace] == nil {
		st.shortfall[namespace] = map[string]float64{}
	}
	st.shortfall[namespace][deployment] = replicas
}

// workloadResources is the synced server + web-app objects of app appName in namespace,
// the server's Deployment reporting serverHealth.
func workloadResources(appName, namespace, serverHealth string) []stubResource {
	return []stubResource{
		{Kind: "Deployment", Name: appName + "-server", Namespace: namespace, Sync: "Synced", Health: serverHealth},
		{Kind: "Service", Name: appName + "-server", Namespace: namespace, Sync: "Synced", Health: "Healthy"},
		{Kind: "Deployment", Name: appName + "-webapp", Namespace: namespace, Sync: "Synced", Health: "Healthy"},
		{Kind: "Service", Name: appName + "-webapp", Namespace: namespace, Sync: "Synced", Health: "Healthy"},
	}
}

// desiredState is a delivered system's desired runtime state, as operationsManager
// assembles it.
func desiredState(appName string) operatedruntime.RuntimeDesiredState {
	return operatedruntime.RuntimeDesiredState{
		AppName:         appName,
		Namespace:       "op-" + appName,
		Host:            appName + ".apps.example.test",
		ModelKey:        "cloud-env",
		GatewayModelKey: "gateway",
		Server:          operatedruntime.Workload{ModelKey: "server", Image: "ghcr.io/example/" + appName + "-server:1.0.0", Replicas: 1},
		WebApp:          operatedruntime.Workload{ModelKey: "web-app", Image: "ghcr.io/example/" + appName + "-web:1.0.0", Replicas: 1},
		OIDC: operatedruntime.OIDCSpec{
			ModelKey: "keycloak", Issuer: "https://auth.example.test/realms/" + appName,
			ClientID: appName, ClientSecretRef: appName + "-oidc-client-secret",
		},
	}
}

// arrangeInFlight arranges a delivered, running system named appName: its desired state
// d published through the subject (so its Application is committed to the GitOps
// repository, annotated with its appID), and synced (so the cluster lists it Healthy).
func arrangeInFlight(t *testing.T, publish func() error, appID, appName string) operatedruntime.RuntimeDesiredState {
	t.Helper()
	d := desiredState(appName)
	if err := publish(); err != nil {
		t.Fatalf("arrange: publish %s: %v", appName, err)
	}
	stub.live(stubApp{AppID: appID, Name: appName, Namespace: d.Namespace, Health: "Healthy", Resources: workloadResources(appName, d.Namespace, "Healthy")})
	return d
}

// newSubject builds the component under test against the host's real downstream
// doubles: the REAL profile, through the generated constructor, committing to the host's
// bare git repository and reading this scenario's runtime stub.
func newSubject(t *testing.T, h *scenariohost.Host) operatedruntime.OperatedRuntimeAccess {
	t.Helper()
	stub = newRuntimeStub(t)
	rt, err := operatedruntime.NewProfiledOperatedRuntimeAccess(operatedruntime.RuntimeProfileReal, operatedruntime.RuntimeConfig{
		GitOpsRepoURL:       h.Repo.URL,
		ClusterAPIURL:       stub.cluster.URL,
		ClusterAPIToken:     clusterToken,
		ClusterAPICACertPEM: stub.caPEM(),
		PrometheusURL:       stub.prom.URL,
	})
	if err != nil {
		t.Fatalf("newSubject: %v", err)
	}
	return rt
}

// stepEnforceCustomerDelinquencyOnOperatedSystemsP3S1 — Enforce Customer Delinquency on Operated Systems — [yes] · [withdraw], step 1: Withdraw
// Projected stimulus (JSON): {"seq":1,"node":"remove-runtime","input":{"kind":"call","from":"operations-manager","to":"operatedruntimeaccess","op":"Withdraw","call":"enforce-customer-delinquency-on-operated-systems/remove-runtime/0"},"expectedOutputs":[{"node":"remove-runtime","kind":"inferred","to":"","label":"Remove each system's runtime from the infrastructure"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"Withdraw","inputs":[{"name":"appID","value":"7a0e5c11-0000-4000-8000-000000000001"},{"name":"idempotencyKey","value":"stp-ort-delinq-P3-withdraw-0001"}],"expect":{"errorExpected":false},"probes":[{"for":"remove-runtime","component":"operatedRuntimeAccess","operation":"GetApplicationHealth","inputs":[{"name":"appID","value":"7a0e5c11-0000-4000-8000-000000000001"}],"expect":{"result":"4","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges state so that guards directive-named=[yes], action-kind=[withdraw] hold, then calls the operation as operationsManager does.
func stepEnforceCustomerDelinquencyOnOperatedSystemsP3S1(t *testing.T, h *scenariohost.Host, subject operatedruntime.OperatedRuntimeAccess, in inputEnforceCustomerDelinquencyOnOperatedSystemsP3S1) (any, error) {
	t.Helper()
	// The delinquent customer's system is in flight: published and running.
	const name = "delinquent-p3"
	arrangeInFlight(t, func() error {
		return subject.PublishDesiredState(callContext(t, h, "arrange"), in.AppID, desiredState(name), "arrange-"+name)
	}, in.AppID.String(), name)
	return nil, subject.Withdraw(callContext(t, h, "1"), in.AppID, in.IdempotencyKey)
}

// stepOperateADeliveredSystemP2S1 — Operate a Delivered System — [no] · [yes], step 1: GetApplicationHealth
// Projected stimulus (JSON): {"seq":1,"node":"pull-telemetry","input":{"kind":"call","from":"operations-manager","to":"operatedruntimeaccess","op":"GetApplicationHealth","call":"operate-a-delivered-system/pull-telemetry/0.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"GetApplicationHealth","inputs":[{"name":"appID","value":"7a0e5c11-0000-4000-8000-000000000003"}],"expect":{"result":"2","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges state so that guards state-transition=[no], decision-nochange=[yes] hold, then calls the operation as operationsManager does.
func stepOperateADeliveredSystemP2S1(t *testing.T, h *scenariohost.Host, subject operatedruntime.OperatedRuntimeAccess, in inputOperateADeliveredSystemP2S1) (any, error) {
	t.Helper()
	// No state transition: the system is still Healthy, its SLO holds, and it burned an
	// hour of CPU over the tick's window.
	stub.live(stubApp{AppID: in.AppID.String(), Name: "gtd", Namespace: "op-gtd", Health: "Healthy", Resources: workloadResources("gtd", "op-gtd", "Healthy")})
	stub.usage("op-gtd", 3600)
	return subject.GetApplicationHealth(callContext(t, h, "1"), in.AppID)
}

// stepOperateADeliveredSystemP3S1 — Operate a Delivered System — [no] · [no: scale/pause/resume], step 1: GetApplicationHealth
// Projected stimulus (JSON): {"seq":1,"node":"pull-telemetry","input":{"kind":"call","from":"operations-manager","to":"operatedruntimeaccess","op":"GetApplicationHealth","call":"operate-a-delivered-system/pull-telemetry/0.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"GetApplicationHealth","inputs":[{"name":"appID","value":"7a0e5c11-0000-4000-8000-000000000004"}],"expect":{"result":"2","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges state so that guards state-transition=[no], decision-nochange=[no: scale/pause/resume] hold, then calls the operation as operationsManager does.
func stepOperateADeliveredSystemP3S1(t *testing.T, h *scenariohost.Host, subject operatedruntime.OperatedRuntimeAccess, in inputOperateADeliveredSystemP3S1) (any, error) {
	t.Helper()
	// No state transition (still Healthy, SLO holds); a full CPU-day of usage over the
	// window is what drives the autoscaler to scale.
	stub.live(stubApp{AppID: in.AppID.String(), Name: "gtd", Namespace: "op-gtd", Health: "Healthy", Resources: workloadResources("gtd", "op-gtd", "Healthy")})
	stub.usage("op-gtd", 86400)
	return subject.GetApplicationHealth(callContext(t, h, "1"), in.AppID)
}

// stepOperateADeliveredSystemP4S1 — Operate a Delivered System — [yes] · [yes], step 1: GetApplicationHealth
// Projected stimulus (JSON): {"seq":1,"node":"pull-telemetry","input":{"kind":"call","from":"operations-manager","to":"operatedruntimeaccess","op":"GetApplicationHealth","call":"operate-a-delivered-system/pull-telemetry/0.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"GetApplicationHealth","inputs":[{"name":"appID","value":"7a0e5c11-0000-4000-8000-000000000005"}],"expect":{"result":"3","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges state so that guards state-transition=[yes], decision-nochange=[yes] hold, then calls the operation as operationsManager does.
func stepOperateADeliveredSystemP4S1(t *testing.T, h *scenariohost.Host, subject operatedruntime.OperatedRuntimeAccess, in inputOperateADeliveredSystemP4S1) (any, error) {
	t.Helper()
	// A state transition: the server Deployment has degraded and is a replica short, so
	// the SLO is violated; usage stays at an hour of CPU (no scaling decision).
	stub.live(stubApp{AppID: in.AppID.String(), Name: "gtd", Namespace: "op-gtd", Health: "Degraded", Resources: workloadResources("gtd", "op-gtd", "Degraded")})
	stub.short("op-gtd", "gtd-server", 1)
	stub.usage("op-gtd", 3600)
	return subject.GetApplicationHealth(callContext(t, h, "1"), in.AppID)
}

// stepOperateADeliveredSystemP5S1 — Operate a Delivered System — [yes] · [no: scale/pause/resume], step 1: GetApplicationHealth
// Projected stimulus (JSON): {"seq":1,"node":"pull-telemetry","input":{"kind":"call","from":"operations-manager","to":"operatedruntimeaccess","op":"GetApplicationHealth","call":"operate-a-delivered-system/pull-telemetry/0.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"GetApplicationHealth","inputs":[{"name":"appID","value":"7a0e5c11-0000-4000-8000-000000000006"}],"expect":{"result":"3","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges state so that guards state-transition=[yes], decision-nochange=[no: scale/pause/resume] hold, then calls the operation as operationsManager does.
func stepOperateADeliveredSystemP5S1(t *testing.T, h *scenariohost.Host, subject operatedruntime.OperatedRuntimeAccess, in inputOperateADeliveredSystemP5S1) (any, error) {
	t.Helper()
	// A state transition (Degraded, a replica short, SLO violated) AND a full CPU-day of
	// usage, so the autoscaler republishes.
	stub.live(stubApp{AppID: in.AppID.String(), Name: "gtd", Namespace: "op-gtd", Health: "Degraded", Resources: workloadResources("gtd", "op-gtd", "Degraded")})
	stub.short("op-gtd", "gtd-server", 1)
	stub.usage("op-gtd", 86400)
	return subject.GetApplicationHealth(callContext(t, h, "1"), in.AppID)
}

// stepViewLiveDeploymentHealthP1S1 — View Live Deployment Health — [yes], step 1: GetDeploymentResourceHealth
// Projected stimulus (JSON): {"seq":1,"node":"read-live-health","input":{"kind":"call","from":"operations-manager","to":"operatedruntimeaccess","op":"GetDeploymentResourceHealth","call":"view-live-deployment-health/read-live-health/0"},"expectedOutputs":[{"node":"read-live-health","kind":"inferred","to":"","label":"Read live health for each node the system actually deploys"},{"node":"send-deployment-health","kind":"sendSignal","to":"operator","label":"Return the health of every deployment-diagram node (healthy / unhealthy / neutral) to the Operator"}],"expectedStates":null,"hints":["Nodes on the diagram that the system does not deploy (the architect's laptop, their browser, another app's namespace) read neutral, never unhealthy. Arrange the runtime stub to return only the deployed model keys."]}
// Binding (JSON): {"seq":1,"operation":"GetDeploymentResourceHealth","inputs":[{"name":"appID","value":"7a0e5c11-0000-4000-8000-000000000007"},{"name":"desired","value":"{\"AppName\":\"gtd\",\"Namespace\":\"op-gtd\",\"Host\":\"gtd.apps.example.test\",\"ModelKey\":\"cloud-env\",\"GatewayModelKey\":\"gateway\",\"Server\":{\"ModelKey\":\"server\",\"Image\":\"ghcr.io/example/gtd-server:1.0.0\",\"Replicas\":1},\"WebApp\":{\"ModelKey\":\"web-app\",\"Image\":\"ghcr.io/example/gtd-web:1.0.0\",\"Replicas\":1},\"Postgres\":{\"ModelKeys\":[],\"Enabled\":false,\"Instances\":0,\"StorageClass\":\"\"},\"OIDC\":{\"ModelKey\":\"keycloak\",\"Issuer\":\"https://auth.example.test/realms/gtd\",\"ClientID\":\"gtd\",\"ClientSecretRef\":\"gtd-oidc-client-secret\"},\"SelfManaged\":false}"}],"expect":{"result":"[{\"ModelKey\":\"server\",\"Status\":2},{\"ModelKey\":\"web-app\",\"Status\":2}]","errorExpected":false},"probes":[{"for":"read-live-health","component":"operatedRuntimeAccess","operation":"GetApplicationHealth","inputs":[{"name":"appID","value":"7a0e5c11-0000-4000-8000-000000000007"}],"expect":{"result":"2","errorExpected":false}}],"unobservable":["send-deployment-health"],"hook":true}
// Arranges state so that guards anything-deployed=[yes] hold, then calls the operation as operationsManager does.
func stepViewLiveDeploymentHealthP1S1(t *testing.T, h *scenariohost.Host, subject operatedruntime.OperatedRuntimeAccess, in inputViewLiveDeploymentHealthP1S1) (any, error) {
	t.Helper()
	// Something is deployed: the cluster runs exactly the system's own server and web-app
	// objects, all synced and Healthy. The gateway routes, the realm import and the
	// environment are not listed — their nodes must read neutral (absent), never unhealthy.
	stub.live(stubApp{AppID: in.AppID.String(), Name: in.Desired.AppName, Namespace: in.Desired.Namespace, Health: "Healthy",
		Resources: workloadResources(in.Desired.AppName, in.Desired.Namespace, "Healthy")})
	return subject.GetDeploymentResourceHealth(callContext(t, h, "1"), in.AppID, in.Desired)
}

// stepWithdrawADeliveredSystemP2S1 — Withdraw a Delivered System — [no: in flight], step 1: Withdraw
// Projected stimulus (JSON): {"seq":1,"node":"tear-down-runtime","input":{"kind":"call","from":"operations-manager","to":"operatedruntimeaccess","op":"Withdraw","call":"withdraw-a-delivered-system/tear-down-runtime/0"},"expectedOutputs":[{"node":"tear-down-runtime","kind":"inferred","to":"","label":"Tear down the system's runtime"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"Withdraw","inputs":[{"name":"appID","value":"7a0e5c11-0000-4000-8000-000000000008"},{"name":"idempotencyKey","value":"stp-ort-wd-P2-withdraw-0001"}],"expect":{"errorExpected":false},"probes":[{"for":"tear-down-runtime","component":"operatedRuntimeAccess","operation":"GetApplicationHealth","inputs":[{"name":"appID","value":"7a0e5c11-0000-4000-8000-000000000008"}],"expect":{"result":"4","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges state so that guards already-withdrawn=[no: in flight] hold, then calls the operation as operationsManager does.
func stepWithdrawADeliveredSystemP2S1(t *testing.T, h *scenariohost.Host, subject operatedruntime.OperatedRuntimeAccess, in inputWithdrawADeliveredSystemP2S1) (any, error) {
	t.Helper()
	// Not yet withdrawn: the system is in flight, and half an hour of CPU is still
	// unbilled for the final-usage read that follows the teardown.
	const name = "withdrawn-p2"
	d := arrangeInFlight(t, func() error {
		return subject.PublishDesiredState(callContext(t, h, "arrange"), in.AppID, desiredState(name), "arrange-"+name)
	}, in.AppID.String(), name)
	stub.usage(d.Namespace, 1800)
	return nil, subject.Withdraw(callContext(t, h, "1"), in.AppID, in.IdempotencyKey)
}
