// Hooks for operationsManager scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file.
//
// The subject is the REAL operationsManager, built through its generated public
// constructor (operations.NewOperationsManager) and served by a real worker
// (operations.RegisterManagerWorker) on the scenario host's Temporal dev server — the
// wiring cmd/server/main.gen.go performs. Every collaborator is real and built through
// its own public constructor (DCT §12 B2); no ResourceAccess is doubled:
//
//   - operatedSystemStateAccess, usageAccess and billingStateAccess are the Postgres
//     implementations, each scenario on its OWN Postgres (every binding names the same
//     operated app and customer, so a shared database would leak one scenario's head
//     state and usage into the next);
//   - artifactAccess and projectStateAccess are the git-local implementations, each over
//     its own bare repository; the project the operated app renders from is committed to
//     the project repository as the store's own document (projectstate.EncodeProjectJSON)
//     — the Resource, not the ResourceAccess, is what the arrangement writes;
//   - operatedRuntimeAccess is the REAL profile: it commits rendered desired state to a
//     bare GitOps repository and reads the cluster API and Prometheus. Those two backends
//     are the one Resource the host does not boot, so they are doubled at the Resource
//     boundary (runtimeDouble below): an Argo that lists what is committed to the GitOps
//     repository once it syncs, and a Prometheus that answers the three queries the profile
//     makes from per-namespace series the hooks arrange;
//   - interventionEngine, autoscalerEngine and operationEstimationEngine are the real
//     pure engines; messageBus is nil, as WorkerManifest threads it.
//
// Arrangement goes through the subject's own public verbs wherever the use case has one
// (register, deploy, a reconcile tick, a withdrawal) and otherwise through the named
// ResourceAccess's own write verbs; every probe calls the named component's read-only op
// on this scenario's stack.

package operations_test

import (
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ghtestinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	pgtestinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres/testinfra"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

// replayWorkflows tells the scenario host which workflows this package's workers run:
// at every flush the host replays each workflow history a scenario started against
// them, and a non-determinism error fails that scenario. The registration is the
// composition root's own (operations.RegisterManagerWorker over a manager built by the
// generated constructor). Replay re-executes workflow code, so the in-workflow engines
// are the real ones; activities never execute on replay, so the ResourceAccess
// dependencies and the client stay nil. The live workers run with the SDK's default
// converters and no interceptors, so the replayer's options are the defaults too.
func replayWorkflows() scenariohost.Workflows {
	return scenariohost.Workflows{Register: func(w scenariohost.Worker) {
		ie, ae, oe := newEngines()
		operations.RegisterManagerWorker(w, operations.NewOperationsManager(nil, nil, nil, nil, nil, nil, ie, ae, oe, nil))
	}}
}

// newSubject builds the component under test: the generated constructor over this
// scenario's own stack, served by a worker on the host's Temporal.
func newSubject(t *testing.T, h *scenariohost.Host) operations.OperationsManager {
	t.Helper()
	ctx := h.Context(t)

	pool := pgtestinfra.StartPostgres(t)
	state, err := operatedsystemstate.NewPostgresOperatedSystemStateAccess(ctx, pool)
	if err != nil {
		t.Fatalf("hooks: operatedSystemStateAccess: %v", err)
	}
	ledger, err := usage.NewPostgresUsageAccess(ctx, pool)
	if err != nil {
		t.Fatalf("hooks: usageAccess: %v", err)
	}
	billing, err := billingstate.NewPostgresBillingStateAccess(ctx, pool)
	if err != nil {
		t.Fatalf("hooks: billingStateAccess: %v", err)
	}
	artifacts := artifact.NewGitLocalArtifactAccess(ghtestinfra.StartLocalGitRepo(t, "main").URL)
	projectRepo := ghtestinfra.StartLocalGitRepo(t, "main")
	commitProject(t, projectRepo, fixtureProject())
	projects := projectstate.NewGitLocalProjectStateAccess(projectRepo.URL)

	rt := newRuntimeDouble(t, ghtestinfra.StartLocalGitRepo(t, "main"))
	runtime, err := operatedruntime.NewProfiledOperatedRuntimeAccess(operatedruntime.RuntimeProfileReal, operatedruntime.RuntimeConfig{
		GitOpsRepoURL:       rt.gitops.URL,
		ClusterAPIURL:       rt.cluster.URL,
		ClusterAPIToken:     clusterToken,
		ClusterAPICACertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rt.cluster.Certificate().Raw}),
		PrometheusURL:       rt.prom.URL,
	})
	if err != nil {
		t.Fatalf("hooks: operatedRuntimeAccess: %v", err)
	}

	ie, ae, oe := newEngines()
	subject := operations.NewOperationsManager(h.TemporalClient(), state, runtime, ledger, artifacts, nil, ie, ae, oe, projects)
	h.StartWorker(t, operations.TaskQueue, func(w scenariohost.Worker) { operations.RegisterManagerWorker(w, subject) })

	stacksMu.Lock()
	stacks[subject] = &stack{state: state, usage: ledger, billing: billing, artifacts: artifacts, runtime: rt}
	stacksMu.Unlock()
	t.Cleanup(func() {
		stacksMu.Lock()
		delete(stacks, subject)
		stacksMu.Unlock()
	})
	return subject
}

// stepEnforceCustomerDelinquencyOnOperatedSystemsP1S1 — Enforce Customer Delinquency on Operated Systems — [no: unsaid], step 1: ApplyDelinquencyPolicy
// Projected stimulus (JSON): {"seq":1,"node":"delinquency-arrives","input":{"kind":"call","from":"billing-manager","to":"operationsmanager","op":"ApplyDelinquencyPolicy","call":"enforce-customer-delinquency-on-operated-systems/delinquency-arrives/0","via":["billing-manager","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ApplyDelinquencyPolicy","inputs":[{"name":"customerID","value":"6f1c2b9e-0c00-4c00-8000-0000000000c1"},{"name":"delinquencyContext","value":"{\"action\":0}"}],"expect":{"errorExpected":true},"probes":[{"for":"directive-named","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"Version\":3,\"Status\":2,\"InFlight\":true}","errorExpected":false}}],"unobservable":[],"hook":true}
// Hook: the customer's app is an operating system (registered, deployed, observed healthy: v3); the directive names no action, so the façade refuses it and nothing is enqueued.
func stepEnforceCustomerDelinquencyOnOperatedSystemsP1S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputEnforceCustomerDelinquencyOnOperatedSystemsP1S1) (any, error) {
	t.Helper()
	operating(t, h, subject, planApp.ID)
	return nil, subject.ApplyDelinquencyPolicy(callContext(t, h, "1"), in.CustomerID, in.DelinquencyContext)
}

// probeEnforceCustomerDelinquencyOnOperatedSystemsP1S1N1 — Enforce Customer Delinquency on Operated Systems — [no: unsaid], step 1, probe 1: operatedSystemStateAccess.ReadOperatedSystem proves directive-named.
// Probe (JSON): {"for":"directive-named","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"Version\":3,\"Status\":2,\"InFlight\":true}","errorExpected":false}}
// Hook: read the app's head-state row.
func probeEnforceCustomerDelinquencyOnOperatedSystemsP1S1N1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// stepEnforceCustomerDelinquencyOnOperatedSystemsP2S1 — Enforce Customer Delinquency on Operated Systems — [yes] · [pause], step 1: ApplyDelinquencyPolicy
// Projected stimulus (JSON): {"seq":1,"node":"delinquency-arrives","input":{"kind":"call","from":"billing-manager","to":"operationsmanager","op":"ApplyDelinquencyPolicy","call":"enforce-customer-delinquency-on-operated-systems/delinquency-arrives/0","via":["billing-manager","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ApplyDelinquencyPolicy","inputs":[{"name":"customerID","value":"6f1c2b9e-0c00-4c00-8000-0000000000c1"},{"name":"delinquencyContext","value":"{\"action\":1}"}],"expect":{"errorExpected":false},"probes":[{"for":"system-delinquency-recorded","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"Version\":4,\"InFlight\":true}","errorExpected":false}}],"unobservable":[],"hook":true}
// Hook: the customer's app is an operating system (v3); the pause is enqueued and the hook waits for the enforcement workflow to record it.
func stepEnforceCustomerDelinquencyOnOperatedSystemsP2S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputEnforceCustomerDelinquencyOnOperatedSystemsP2S1) (any, error) {
	t.Helper()
	v := operating(t, h, subject, planApp.ID)
	return nil, applyDelinquency(t, h, subject, in.CustomerID, in.DelinquencyContext, planApp.ID, v)
}

// probeEnforceCustomerDelinquencyOnOperatedSystemsP2S1N1 — Enforce Customer Delinquency on Operated Systems — [yes] · [pause], step 1, probe 1: operatedSystemStateAccess.ReadOperatedSystem proves system-delinquency-recorded.
// Probe (JSON): {"for":"system-delinquency-recorded","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"Version\":4,\"InFlight\":true}","errorExpected":false}}
// Hook: read the app's head-state row.
func probeEnforceCustomerDelinquencyOnOperatedSystemsP2S1N1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// stepEnforceCustomerDelinquencyOnOperatedSystemsP3S1 — Enforce Customer Delinquency on Operated Systems — [yes] · [withdraw], step 1: ApplyDelinquencyPolicy
// Projected stimulus (JSON): {"seq":1,"node":"delinquency-arrives","input":{"kind":"call","from":"billing-manager","to":"operationsmanager","op":"ApplyDelinquencyPolicy","call":"enforce-customer-delinquency-on-operated-systems/delinquency-arrives/0","via":["billing-manager","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ApplyDelinquencyPolicy","inputs":[{"name":"customerID","value":"6f1c2b9e-0c00-4c00-8000-0000000000c1"},{"name":"delinquencyContext","value":"{\"action\":2}"}],"expect":{"errorExpected":false},"probes":[{"for":"system-delinquency-recorded","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"InFlight\":false}","errorExpected":false}},{"for":"remove-runtime","component":"operatedSystemStateAccess","operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"],\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\"}"}],"expect":{"result":"[]","errorExpected":false}}],"unobservable":[],"hook":true}
// Hook: the customer's app is an operating system (v3); the withdrawal is enqueued and the hook waits for the enforcement workflow to record it.
func stepEnforceCustomerDelinquencyOnOperatedSystemsP3S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputEnforceCustomerDelinquencyOnOperatedSystemsP3S1) (any, error) {
	t.Helper()
	v := operating(t, h, subject, planApp.ID)
	return nil, applyDelinquency(t, h, subject, in.CustomerID, in.DelinquencyContext, planApp.ID, v)
}

// probeEnforceCustomerDelinquencyOnOperatedSystemsP3S1N1 — Enforce Customer Delinquency on Operated Systems — [yes] · [withdraw], step 1, probe 1: operatedSystemStateAccess.ReadOperatedSystem proves system-delinquency-recorded.
// Probe (JSON): {"for":"system-delinquency-recorded","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"InFlight\":false}","errorExpected":false}}
// Hook: read the app's head-state row.
func probeEnforceCustomerDelinquencyOnOperatedSystemsP3S1N1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// probeEnforceCustomerDelinquencyOnOperatedSystemsP3S1N2 — Enforce Customer Delinquency on Operated Systems — [yes] · [withdraw], step 1, probe 2: operatedSystemStateAccess.ReadInFlightOperatedApps proves remove-runtime.
// Probe (JSON): {"for":"remove-runtime","component":"operatedSystemStateAccess","operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"],\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\"}"}],"expect":{"result":"[]","errorExpected":false}}
// Hook: read the customer's in-flight apps with the probe's scope.
func probeEnforceCustomerDelinquencyOnOperatedSystemsP3S1N2(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readInFlight(t, h, subject, `{"AppIDs":["6f1c2b9e-0a11-4c00-8000-0000000000a1"],"CustomerID":"6f1c2b9e-0c00-4c00-8000-0000000000c1"}`)
}

// stepOperateADeliveredSystemP1S1 — Operate a Delivered System, step 1: DeployAfterConstruction
// Projected stimulus (JSON): {"seq":1,"node":"publish-trigger","input":{"kind":"call","from":"web-client","to":"operationsmanager","op":"DeployAfterConstruction","call":"operate-a-delivered-system/publish-trigger/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeployAfterConstruction","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"},{"name":"change","value":"{\"reason\":1,\"patchKind\":1,\"changeId\":\"stp-ops-P1-deploy-0001\"}"}],"expect":{"result":"{\"published\":true}","errorExpected":false},"probes":[{"for":"operated-system-recorded-a","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Version\":2,\"InFlight\":true}","errorExpected":false}}],"unobservable":[],"hook":true}
// Hook: the app is registered with a stored deployable bundle under the fixture project (v1); deploy it.
func stepOperateADeliveredSystemP1S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputOperateADeliveredSystemP1S1) (any, error) {
	t.Helper()
	registered(t, h, subject, in.OperatedAppID)
	return subject.DeployAfterConstruction(callContext(t, h, "1"), in.OperatedAppID, in.Change)
}

// probeOperateADeliveredSystemP1S1N1 — Operate a Delivered System, step 1, probe 1: operatedSystemStateAccess.ReadOperatedSystem proves operated-system-recorded-a.
// Probe (JSON): {"for":"operated-system-recorded-a","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Version\":2,\"InFlight\":true}","errorExpected":false}}
// Hook: read the app's head-state row.
func probeOperateADeliveredSystemP1S1N1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// stepOperateADeliveredSystemP2S1 — Operate a Delivered System — [no] · [yes], step 1: ReconcileOperatedState
// Projected stimulus (JSON): {"seq":1,"node":"schedule-fires","input":{"kind":"timeEvent","from":"scheduler-client","to":"operationsmanager","op":"ReconcileOperatedState","call":"operate-a-delivered-system/schedule-fires/0","via":["scheduler-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReconcileOperatedState","inputs":[{"name":"tickID","value":"stp-ops-P2-tick-0001"},{"name":"scope","value":"{\"appIds\":[\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"]}"}],"expect":{"result":"{\"observed\":1,\"transitions\":0,\"republished\":0}","errorExpected":false},"probes":[{"for":"state-transition","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":2,\"Version\":3,\"InFlight\":true}","errorExpected":false}}],"unobservable":[],"hook":true}
// Hook: the app is an operating system (v3, Healthy); the cluster still reports it Healthy with every replica available, so the tick observes no transition and the autoscaler proposes no change.
func stepOperateADeliveredSystemP2S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputOperateADeliveredSystemP2S1) (any, error) {
	t.Helper()
	operating(t, h, subject, planApp.ID)
	return subject.ReconcileOperatedState(callContext(t, h, "1"), in.TickID, in.Scope)
}

// probeOperateADeliveredSystemP2S1N1 — Operate a Delivered System — [no] · [yes], step 1, probe 1: operatedSystemStateAccess.ReadOperatedSystem proves state-transition.
// Probe (JSON): {"for":"state-transition","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":2,\"Version\":3,\"InFlight\":true}","errorExpected":false}}
// Hook: read the app's head-state row.
func probeOperateADeliveredSystemP2S1N1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// stepOperateADeliveredSystemP3S1 — Operate a Delivered System — [no] · [no: scale/pause/resume], step 1: ReconcileOperatedState
// Projected stimulus (JSON): {"seq":1,"node":"schedule-fires","input":{"kind":"timeEvent","from":"scheduler-client","to":"operationsmanager","op":"ReconcileOperatedState","call":"operate-a-delivered-system/schedule-fires/0","via":["scheduler-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReconcileOperatedState","inputs":[{"name":"tickID","value":"stp-ops-P3-tick-0001"},{"name":"scope","value":"{\"appIds\":[\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"]}"}],"expect":{"result":"{\"observed\":1,\"transitions\":0,\"republished\":1}","errorExpected":false},"probes":[{"for":"desired-state-published-c","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Scale\":{\"Replicas\":3}}","errorExpected":false}},{"for":"operated-system-recorded-c","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":2,\"Version\":4,\"InFlight\":true,\"Scale\":{\"Replicas\":3}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Hook: the app is an operating system (v3, Healthy, two replicas under a 1..4 policy targeting 70% CPU) burning a full CPU-day with every replica available and its server at 90% of its CPU request: no transition, and the autoscaler scales it up to three.
func stepOperateADeliveredSystemP3S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputOperateADeliveredSystemP3S1) (any, error) {
	t.Helper()
	operating(t, h, subject, planApp.ID)
	rt := stackOf(t, subject).runtime
	rt.usage(planProjectRef, 86400)
	rt.utilization(planProjectRef, 0.9)
	return subject.ReconcileOperatedState(callContext(t, h, "1"), in.TickID, in.Scope)
}

// probeOperateADeliveredSystemP3S1N1 — Operate a Delivered System — [no] · [no: scale/pause/resume], step 1, probe 1: operatedSystemStateAccess.ReadOperatedSystem proves desired-state-published-c.
// Probe (JSON): {"for":"desired-state-published-c","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Scale\":{\"Replicas\":3}}","errorExpected":false}}
// Hook: read the app's head-state row (its recorded desired scale).
func probeOperateADeliveredSystemP3S1N1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// probeOperateADeliveredSystemP3S1N2 — Operate a Delivered System — [no] · [no: scale/pause/resume], step 1, probe 2: operatedSystemStateAccess.ReadOperatedSystem proves operated-system-recorded-c.
// Probe (JSON): {"for":"operated-system-recorded-c","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":2,\"Version\":4,\"InFlight\":true,\"Scale\":{\"Replicas\":3}}","errorExpected":false}}
// Hook: read the app's head-state row.
func probeOperateADeliveredSystemP3S1N2(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// stepOperateADeliveredSystemP4S1 — Operate a Delivered System — [yes] · [yes], step 1: ReconcileOperatedState
// Projected stimulus (JSON): {"seq":1,"node":"schedule-fires","input":{"kind":"timeEvent","from":"scheduler-client","to":"operationsmanager","op":"ReconcileOperatedState","call":"operate-a-delivered-system/schedule-fires/0","via":["scheduler-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReconcileOperatedState","inputs":[{"name":"tickID","value":"stp-ops-P4-tick-0001"},{"name":"scope","value":"{\"appIds\":[\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"]}"}],"expect":{"result":"{\"observed\":1,\"transitions\":1,\"republished\":0}","errorExpected":false},"probes":[{"for":"operated-system-status-changed","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":3,\"Version\":4,\"InFlight\":true}","errorExpected":false}},{"for":"record-status","component":"operationsManager","operation":"QueryOperatedSystemView","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"},{"name":"requestID","value":"stp-ops-P4-probe-view"}],"expect":{"result":"{\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Phase\":3}","errorExpected":false}},{"for":"compute-usage-recorded","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"CycleID\":\"\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}"}],"expect":{"result":"[{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}]","errorExpected":false}}],"unobservable":[],"hook":true}
// Hook: the app is an operating system (v3, Healthy); Argo then reports it Degraded with every replica available (its SLO holds) while it burns an hour of CPU, so the tick observes a Healthy→Degraded transition, records the compute, and has nothing to scale for.
func stepOperateADeliveredSystemP4S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputOperateADeliveredSystemP4S1) (any, error) {
	t.Helper()
	operating(t, h, subject, planApp.ID)
	rt := stackOf(t, subject).runtime
	rt.degradeHealth(planProjectRef)
	rt.usage(planProjectRef, 3600)
	return subject.ReconcileOperatedState(callContext(t, h, "1"), in.TickID, in.Scope)
}

// probeOperateADeliveredSystemP4S1N1 — Operate a Delivered System — [yes] · [yes], step 1, probe 1: operatedSystemStateAccess.ReadOperatedSystem proves operated-system-status-changed.
// Probe (JSON): {"for":"operated-system-status-changed","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":3,\"Version\":4,\"InFlight\":true}","errorExpected":false}}
// Hook: read the app's head-state row.
func probeOperateADeliveredSystemP4S1N1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// probeOperateADeliveredSystemP4S1N3 — Operate a Delivered System — [yes] · [yes], step 1, probe 3: usageAccess.ReadRange proves compute-usage-recorded.
// Probe (JSON): {"for":"compute-usage-recorded","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"CycleID\":\"\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}"}],"expect":{"result":"[{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}]","errorExpected":false}}
// Hook: read the usage the subject attributed to the app's customer in the cycle of its own clock.
func probeOperateADeliveredSystemP4S1N3(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readUsage(t, h, subject, planApp.CustomerID, planApp.ID)
}

// stepOperateADeliveredSystemP5S1 — Operate a Delivered System — [yes] · [no: scale/pause/resume], step 1: ReconcileOperatedState
// Projected stimulus (JSON): {"seq":1,"node":"schedule-fires","input":{"kind":"timeEvent","from":"scheduler-client","to":"operationsmanager","op":"ReconcileOperatedState","call":"operate-a-delivered-system/schedule-fires/0","via":["scheduler-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReconcileOperatedState","inputs":[{"name":"tickID","value":"stp-ops-P5-tick-0001"},{"name":"scope","value":"{\"appIds\":[\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"]}"}],"expect":{"result":"{\"observed\":1,\"transitions\":1,\"republished\":1}","errorExpected":false},"probes":[{"for":"operated-system-status-changed","component":"operationsManager","operation":"QueryOperatedSystemView","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"},{"name":"requestID","value":"stp-ops-P5-probe-view-status"}],"expect":{"result":"{\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Phase\":3}","errorExpected":false}},{"for":"compute-usage-recorded","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"CycleID\":\"\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}"}],"expect":{"result":"[{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}]","errorExpected":false}},{"for":"desired-state-published-c","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Scale\":{\"Replicas\":3}}","errorExpected":false}},{"for":"operated-system-recorded-c","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":3,\"Version\":5,\"InFlight\":true,\"Scale\":{\"Replicas\":3}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Hook: the app is an operating system (v3, Healthy, two replicas); its server Deployment then degrades a replica short (an SLO violation) while it burns a full CPU-day, so the tick observes a transition and the burning SLO scales it up to three.
func stepOperateADeliveredSystemP5S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputOperateADeliveredSystemP5S1) (any, error) {
	t.Helper()
	operating(t, h, subject, planApp.ID)
	rt := stackOf(t, subject).runtime
	rt.degrade(planProjectRef, planProjectRef+"-server", 1)
	rt.usage(planProjectRef, 86400)
	return subject.ReconcileOperatedState(callContext(t, h, "1"), in.TickID, in.Scope)
}

// probeOperateADeliveredSystemP5S1N2 — Operate a Delivered System — [yes] · [no: scale/pause/resume], step 1, probe 2: usageAccess.ReadRange proves compute-usage-recorded.
// Probe (JSON): {"for":"compute-usage-recorded","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"CycleID\":\"\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}"}],"expect":{"result":"[{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}]","errorExpected":false}}
// Hook: read the usage the subject attributed to the app's customer in the cycle of its own clock.
func probeOperateADeliveredSystemP5S1N2(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readUsage(t, h, subject, planApp.CustomerID, planApp.ID)
}

// probeOperateADeliveredSystemP5S1N3 — Operate a Delivered System — [yes] · [no: scale/pause/resume], step 1, probe 3: operatedSystemStateAccess.ReadOperatedSystem proves desired-state-published-c.
// Probe (JSON): {"for":"desired-state-published-c","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Scale\":{\"Replicas\":3}}","errorExpected":false}}
// Hook: read the app's head-state row (its recorded desired scale).
func probeOperateADeliveredSystemP5S1N3(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// probeOperateADeliveredSystemP5S1N4 — Operate a Delivered System — [yes] · [no: scale/pause/resume], step 1, probe 4: operatedSystemStateAccess.ReadOperatedSystem proves operated-system-recorded-c.
// Probe (JSON): {"for":"operated-system-recorded-c","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":3,\"Version\":5,\"InFlight\":true,\"Scale\":{\"Replicas\":3}}","errorExpected":false}}
// Hook: read the app's head-state row.
func probeOperateADeliveredSystemP5S1N4(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// probeRegisterADeliveredSystemForOperationP1S1N1 — Register a Delivered System for Operation, step 1, probe 1: operatedSystemStateAccess.ReadOperatedSystem proves system-registered.
// Probe (JSON): {"for":"system-registered","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a2"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a2\",\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"Version\":1,\"ProjectRef\":\"stp-ops-fixture-project\",\"DeployableBundleRef\":\"bundle:stp-ops-fixture-0001\"}","errorExpected":false}}
// Hook: read the registered app's head-state row.
func probeRegisterADeliveredSystemForOperationP1S1N1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, registeredApp.ID)
}

// probeRegisterADeliveredSystemForOperationP1S1N2 — Register a Delivered System for Operation, step 1, probe 2: operatedSystemStateAccess.ReadInFlightOperatedApps proves system-registered.
// Probe (JSON): {"for":"system-registered","component":"operatedSystemStateAccess","operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[\"6f1c2b9e-0a11-4c00-8000-0000000000a2\"]}"}],"expect":{"result":"[]","errorExpected":false}}
// Hook: read the in-flight apps with the probe's scope.
func probeRegisterADeliveredSystemForOperationP1S1N2(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readInFlight(t, h, subject, `{"AppIDs":["6f1c2b9e-0a11-4c00-8000-0000000000a2"]}`)
}

// stepRetryADeclinedServiceInvoiceP3S1 — Retry a Declined Service Invoice — [no: exhausted] · [no: aged out], step 1: ApplyDelinquencyPolicy
// Projected stimulus (JSON): {"seq":1,"node":"escalate-delinquent","input":{"kind":"call","from":"billing-manager","to":"operationsmanager","op":"ApplyDelinquencyPolicy","call":"retry-a-declined-service-invoice/escalate-delinquent/1"},"expectedOutputs":[{"node":"signal-delinquency-escalation","kind":"sendSignal","to":"settlement-manager","label":"Escalate persistent delinquency to the Settlement Manager"}],"expectedStates":[{"node":"customer-delinquent","object":"Customer","inState":"[delinquent]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"ApplyDelinquencyPolicy","inputs":[{"name":"customerID","value":"6f1c2b9e-0c00-4c00-8000-0000000000c1"},{"name":"delinquencyContext","value":"{\"action\":1}"}],"expect":{"errorExpected":false},"probes":[{"for":"customer-delinquent","component":"billingStateAccess","operation":"ReadPersistentlyDelinquentCustomers","inputs":[{"name":"scope","value":"{\"ProjectID\":\"stp-ops-fixture-project\"}"}],"expect":{"result":"[{\"ID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"PauseNotWithdraw\":true}]","errorExpected":false}},{"for":"escalate-delinquent","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"Version\":4,\"InFlight\":true}","errorExpected":false}}],"unobservable":["signal-delinquency-escalation"],"hook":true}
// Hook: the customer is persistently delinquent on the fixture project (registered, its cycle settled escalated) and its app is an operating system (v3); billing's escalation pauses it, and the hook waits for the enforcement workflow to record the pause.
func stepRetryADeclinedServiceInvoiceP3S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputRetryADeclinedServiceInvoiceP3S1) (any, error) {
	t.Helper()
	v := operating(t, h, subject, planApp.ID)
	delinquent(t, h, subject)
	return nil, applyDelinquency(t, h, subject, in.CustomerID, in.DelinquencyContext, planApp.ID, v)
}

// probeRetryADeclinedServiceInvoiceP3S1N1 — Retry a Declined Service Invoice — [no: exhausted] · [no: aged out], step 1, probe 1: billingStateAccess.ReadPersistentlyDelinquentCustomers proves customer-delinquent.
// Probe (JSON): {"for":"customer-delinquent","component":"billingStateAccess","operation":"ReadPersistentlyDelinquentCustomers","inputs":[{"name":"scope","value":"{\"ProjectID\":\"stp-ops-fixture-project\"}"}],"expect":{"result":"[{\"ID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"PauseNotWithdraw\":true}]","errorExpected":false}}
// Hook: read the fixture project's persistently delinquent customers.
func probeRetryADeclinedServiceInvoiceP3S1N1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	var scope billingstate.DelinquencyScope
	mustUnmarshal(t, `{"ProjectID":"stp-ops-fixture-project"}`, &scope)
	return stackOf(t, subject).billing.ReadPersistentlyDelinquentCustomers(h.ResourceAccessContext(t, "read"), scope)
}

// probeRetryADeclinedServiceInvoiceP3S1N2 — Retry a Declined Service Invoice — [no: exhausted] · [no: aged out], step 1, probe 2: operatedSystemStateAccess.ReadOperatedSystem proves escalate-delinquent.
// Probe (JSON): {"for":"escalate-delinquent","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"Version\":4,\"InFlight\":true}","errorExpected":false}}
// Hook: read the app's head-state row.
func probeRetryADeclinedServiceInvoiceP3S1N2(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// stepViewLiveDeploymentHealthP1S1 — View Live Deployment Health — [yes], step 1: QueryDeploymentHealth
// Projected stimulus (JSON): {"seq":1,"node":"request-health","input":{"kind":"call","from":"web-client","to":"operationsmanager","op":"QueryDeploymentHealth","call":"view-live-deployment-health/request-health/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryDeploymentHealth","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"Nodes\":[{\"ModelKey\":\"stp-ns-stp-ops-fixture-project\",\"Health\":0},{\"ModelKey\":\"stp-infra-gateway\",\"Health\":1},{\"ModelKey\":\"stp-infra-idp\",\"Health\":0},{\"ModelKey\":\"stp-infra-db\",\"Health\":1},{\"ModelKey\":\"stp-operated-app\",\"Health\":1},{\"ModelKey\":\"stp-architect-laptop\",\"Health\":0}]}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Hook: the app is deployed and Argo has synced it — the cluster lists exactly the objects the subject committed, all Healthy; read its deployment health.
func stepViewLiveDeploymentHealthP1S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputViewLiveDeploymentHealthP1S1) (any, error) {
	t.Helper()
	operating(t, h, subject, in.OperatedAppID)
	return subject.QueryDeploymentHealth(callContext(t, h, "1"), in.OperatedAppID)
}

// stepViewLiveDeploymentHealthP2S1 — View Live Deployment Health — [no: no deployable bundle], step 1: QueryDeploymentHealth
// Projected stimulus (JSON): {"seq":1,"node":"request-health","input":{"kind":"call","from":"web-client","to":"operationsmanager","op":"QueryDeploymentHealth","call":"view-live-deployment-health/request-health/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryDeploymentHealth","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000ff"}],"expect":{"errorExpected":true,"errorCode":"FailedPrecondition"},"probes":[],"unobservable":[],"hook":true}
// Hook: nothing was ever registered or deployed for the app; read its deployment health.
func stepViewLiveDeploymentHealthP2S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputViewLiveDeploymentHealthP2S1) (any, error) {
	t.Helper()
	return subject.QueryDeploymentHealth(callContext(t, h, "1"), in.OperatedAppID)
}

// stepViewOperatingCostProjectionP1S1 — View Operating-Cost Projection — [yes], step 1: QueryOperatedSystemView
// Projected stimulus (JSON): {"seq":1,"node":"open-console","input":{"kind":"call","from":"web-client","to":"operationsmanager","op":"QueryOperatedSystemView","call":"view-operating-cost-projection/open-console/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryOperatedSystemView","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"},{"name":"requestID","value":"stp-ops-cost-P1-view"}],"expect":{"result":"{\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Phase\":2,\"InFlight\":true}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Hook: the app is an operating system (v3, Healthy) whose one observed tick measured an hour of CPU; open its operator view.
func stepViewOperatingCostProjectionP1S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputViewOperatingCostProjectionP1S1) (any, error) {
	t.Helper()
	operatingWithUsage(t, h, subject, in.OperatedAppID, 3600)
	return subject.QueryOperatedSystemView(callContext(t, h, "1"), in.OperatedAppID, in.RequestID)
}

// stepViewOperatingCostProjectionP1S2 — View Operating-Cost Projection — [yes], step 2: QueryCostProjection
// Projected stimulus (JSON): {"seq":2,"node":"request-projection","input":{"kind":"call","from":"web-client","to":"operationsmanager","op":"QueryCostProjection","call":"view-operating-cost-projection/request-projection/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryCostProjection","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"},{"name":"requestID","value":"stp-ops-cost-P1-projection"},{"name":"points","value":"{\"points\":[{\"replicas\":1},{\"replicas\":3}]}"}],"expect":{"result":"{\"CurrentRunRate\":{\"Currency\":\"USD\"},\"ProjectedMonthlyCost\":{\"Currency\":\"USD\"}}","errorExpected":false},"probes":[{"for":"read-usage","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"CycleID\":\"\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}"}],"expect":{"result":"[{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}]","errorExpected":false}}],"unobservable":[],"hook":true}
// Hook: no further arrangement — the state step 1 read IS this step's precondition; project the app's operating cost.
func stepViewOperatingCostProjectionP1S2(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputViewOperatingCostProjectionP1S2) (any, error) {
	t.Helper()
	return subject.QueryCostProjection(callContext(t, h, "2"), in.OperatedAppID, in.RequestID, in.Points)
}

// probeViewOperatingCostProjectionP1S2N1 — View Operating-Cost Projection — [yes], step 2, probe 1: usageAccess.ReadRange proves read-usage.
// Probe (JSON): {"for":"read-usage","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"CycleID\":\"\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}"}],"expect":{"result":"[{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}]","errorExpected":false}}
// Hook: read the usage the subject attributed to the app's customer in the cycle of its own clock.
func probeViewOperatingCostProjectionP1S2N1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readUsage(t, h, subject, planApp.CustomerID, planApp.ID)
}

// stepWithdrawADeliveredSystemP1S1 — Withdraw a Delivered System — [yes], step 1: WithdrawSystem
// Projected stimulus (JSON): {"seq":1,"node":"request-withdraw","input":{"kind":"call","from":"web-client","to":"operationsmanager","op":"WithdrawSystem","call":"withdraw-a-delivered-system/request-withdraw/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"WithdrawSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"},{"name":"changeID","value":"stp-ops-wd-P1-0001"},{"name":"reason","value":"{\"notes\":\"stp: operator withdraws the fixture system\"}"}],"expect":{"result":"{\"withdrawn\":true}","errorExpected":false},"probes":[{"for":"already-withdrawn","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":4,\"Version\":2,\"InFlight\":false}","errorExpected":false}}],"unobservable":[],"hook":true}
// Hook: the app was registered and then withdrawn through the subject (v2, Withdrawn); withdraw it again.
func stepWithdrawADeliveredSystemP1S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputWithdrawADeliveredSystemP1S1) (any, error) {
	t.Helper()
	registered(t, h, subject, in.OperatedAppID)
	if _, err := subject.WithdrawSystem(callContext(t, h, "arrange-withdraw"), in.OperatedAppID, "stp-ops-arrange-withdraw", in.Reason); err != nil {
		t.Fatalf("hooks: arrange withdrawal: %v", err)
	}
	return subject.WithdrawSystem(callContext(t, h, "1"), in.OperatedAppID, in.ChangeID, in.Reason)
}

// probeWithdrawADeliveredSystemP1S1N1 — Withdraw a Delivered System — [yes], step 1, probe 1: operatedSystemStateAccess.ReadOperatedSystem proves already-withdrawn.
// Probe (JSON): {"for":"already-withdrawn","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":4,\"Version\":2,\"InFlight\":false}","errorExpected":false}}
// Hook: read the app's head-state row.
func probeWithdrawADeliveredSystemP1S1N1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// stepWithdrawADeliveredSystemP2S1 — Withdraw a Delivered System — [no: in flight], step 1: WithdrawSystem
// Projected stimulus (JSON): {"seq":1,"node":"request-withdraw","input":{"kind":"call","from":"web-client","to":"operationsmanager","op":"WithdrawSystem","call":"withdraw-a-delivered-system/request-withdraw/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"WithdrawSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"},{"name":"changeID","value":"stp-ops-wd-P2-0001"},{"name":"reason","value":"{\"notes\":\"stp: operator withdraws the fixture system\"}"}],"expect":{"result":"{\"withdrawn\":true}","errorExpected":false},"probes":[{"for":"operated-system-withdrawn","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":4,\"InFlight\":false}","errorExpected":false}},{"for":"compute-usage-final","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"CycleID\":\"\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}"}],"expect":{"result":"[{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}]","errorExpected":false}},{"for":"tear-down-runtime","component":"operatedSystemStateAccess","operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"]}"}],"expect":{"result":"[]","errorExpected":false}}],"unobservable":[],"hook":true}
// Hook: the app is deployed and in flight (v2) and Argo has synced it; half an hour of CPU is still unbilled when the operator withdraws it.
func stepWithdrawADeliveredSystemP2S1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, in inputWithdrawADeliveredSystemP2S1) (any, error) {
	t.Helper()
	deployed(t, h, subject, in.OperatedAppID)
	stackOf(t, subject).runtime.usage(planProjectRef, 1800)
	return subject.WithdrawSystem(callContext(t, h, "1"), in.OperatedAppID, in.ChangeID, in.Reason)
}

// probeWithdrawADeliveredSystemP2S1N1 — Withdraw a Delivered System — [no: in flight], step 1, probe 1: operatedSystemStateAccess.ReadOperatedSystem proves operated-system-withdrawn.
// Probe (JSON): {"for":"operated-system-withdrawn","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":4,\"InFlight\":false}","errorExpected":false}}
// Hook: read the app's head-state row.
func probeWithdrawADeliveredSystemP2S1N1(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readApp(t, h, subject, planApp.ID)
}

// probeWithdrawADeliveredSystemP2S1N2 — Withdraw a Delivered System — [no: in flight], step 1, probe 2: usageAccess.ReadRange proves compute-usage-final.
// Probe (JSON): {"for":"compute-usage-final","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"CycleID\":\"\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}"}],"expect":{"result":"[{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}]","errorExpected":false}}
// Hook: read the usage the subject attributed to the app's customer in the cycle of its own clock.
func probeWithdrawADeliveredSystemP2S1N2(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readUsage(t, h, subject, planApp.CustomerID, planApp.ID)
}

// probeWithdrawADeliveredSystemP2S1N3 — Withdraw a Delivered System — [no: in flight], step 1, probe 3: operatedSystemStateAccess.ReadInFlightOperatedApps proves tear-down-runtime.
// Probe (JSON): {"for":"tear-down-runtime","component":"operatedSystemStateAccess","operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"]}"}],"expect":{"result":"[]","errorExpected":false}}
// Hook: read the in-flight apps with the probe's scope.
func probeWithdrawADeliveredSystemP2S1N3(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) (any, error) {
	t.Helper()
	return readInFlight(t, h, subject, `{"AppIDs":["6f1c2b9e-0a11-4c00-8000-0000000000a1"]}`)
}

// ---------------------------------------------------------------------------
// Identities the bindings name, decoded through the contracts' own JSON (the binding
// values are JSON).
// ---------------------------------------------------------------------------

// planApp is the operated app most bindings name, and its customer.
var planApp = decodeSystem(`{"ID":"6f1c2b9e-0a11-4c00-8000-0000000000a1","CustomerID":"6f1c2b9e-0c00-4c00-8000-0000000000c1"}`)

// registeredApp is the app register-a-delivered-system-for-operation registers.
var registeredApp = decodeSystem(`{"ID":"6f1c2b9e-0a11-4c00-8000-0000000000a2","CustomerID":"6f1c2b9e-0c00-4c00-8000-0000000000c1"}`)

func decodeSystem(raw string) operatedsystemstate.OperatedSystem {
	var s operatedsystemstate.OperatedSystem
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		panic("hooks: decode identity: " + err.Error())
	}
	return s
}

// planProjectRef is the project the operated app renders from. It is also the app name
// (a DNS-1123 label): the namespace, every rendered object's name prefix and the
// directory the GitOps repository holds the app's manifests under.
const planProjectRef = "stp-ops-fixture-project"

// planSettledCycle is the cycle a delinquent customer failed to pay.
const planSettledCycle = "2026-09"

// awaitEffect bounds how long a queued op's head-state effect is waited for.
const awaitEffect = 60 * time.Second

// ---------------------------------------------------------------------------
// The scenario's stack.
// ---------------------------------------------------------------------------

// stack is one scenario's downstream wiring, kept so step and probe hooks reach the
// instances the subject's worker runs against.
type stack struct {
	state     operatedsystemstate.OperatedSystemStateAccess
	usage     usage.UsageAccess
	billing   billingstate.BillingStateAccess
	artifacts artifact.ArtifactAccess
	runtime   *runtimeDouble
	writes    int
	ticks     int
}

var (
	stacksMu sync.Mutex
	stacks   = map[operations.OperationsManager]*stack{}
)

func stackOf(t *testing.T, subject operations.OperationsManager) *stack {
	t.Helper()
	stacksMu.Lock()
	defer stacksMu.Unlock()
	s, ok := stacks[subject]
	if !ok {
		t.Fatal("hooks: subject was not built by newSubject")
	}
	return s
}

// newEngines are the three pure engines the subject's workflows call in-workflow.
func newEngines() (intervention.InterventionEngine, autoscaler.AutoscalerEngine, operationestimation.OperationEstimationEngine) {
	return intervention.NewInterventionEngine(), autoscaler.NewAutoscalerEngine(), operationestimation.NewOperationEstimationEngine()
}

// arrangeKey names one arranging write of this scenario: the host derives the
// write's idempotency key from it (h.ResourceAccessContext), and no other write of
// the scenario uses it.
func (s *stack) arrangeKey(what string) string {
	s.writes++
	return fmt.Sprintf("arrange-%s-%d", what, s.writes)
}

// ---------------------------------------------------------------------------
// Arrangement through the subject's own verbs.
// ---------------------------------------------------------------------------

// registered stores the deployable bundle through artifactAccess's own write verb and
// registers app under the fixture project through the subject: version 1, not yet in
// flight (spec §12 B9).
func registered(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, app usage.OperatedAppID) {
	t.Helper()
	s := stackOf(t, subject)
	manifest, err := json.Marshal(map[string]string{
		"serverImage": "registry.example/stp-ops-server:1",
		"webAppImage": "registry.example/stp-ops-webapp:1",
	})
	if err != nil {
		t.Fatalf("hooks: encode the bundle manifest: %v", err)
	}
	bundle, err := s.artifacts.StoreConstructionOutput(h.ResourceAccessContext(t, s.arrangeKey("bundle")), artifact.ConstructionOutput{Bytes: manifest, MIMEType: "application/json"})
	if err != nil {
		t.Fatalf("hooks: store the deployable bundle: %v", err)
	}
	v, err := subject.RegisterOperatedApp(callContext(t, h, "arrange-register"), app, planApp.CustomerID, planProjectRef, bundle)
	if err != nil {
		t.Fatalf("hooks: register operated app %s: %v", app, err)
	}
	if v != 1 {
		t.Fatalf("hooks: register operated app %s: version %d, want 1", app, v)
	}
}

// deployed registers app and takes it live through the subject's first
// deploy-after-construction (version 2, in flight), then lets Argo sync what the
// subject committed.
func deployed(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, app usage.OperatedAppID) {
	t.Helper()
	registered(t, h, subject, app)
	var change operations.DesiredStateChange
	mustUnmarshal(t, `{"reason":1,"patchKind":1,"changeId":"stp-ops-arrange-deploy"}`, &change)
	res, err := subject.DeployAfterConstruction(callContext(t, h, "arrange-deploy"), app, change)
	if err != nil {
		t.Fatalf("hooks: deploy operated app %s: %v", app, err)
	}
	if !res.Published {
		t.Fatalf("hooks: deploy operated app %s: nothing published", app)
	}
	stackOf(t, subject).runtime.sync(t)
}

// operating deploys app and runs one reconcile tick over it, which observes the synced
// app Healthy: version 3, Healthy, in flight. It returns that version.
func operating(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, app usage.OperatedAppID) operatedsystemstate.Version {
	t.Helper()
	deployed(t, h, subject, app)
	tick(t, h, subject, app)
	op := readHead(t, h, subject, app)
	if op.Status != operatedsystemstate.RuntimeStatusHealthy || !op.InFlight {
		t.Fatalf("hooks: operated app %s is not operating after its first tick: %+v", app, op)
	}
	return op.Version
}

// operatingWithUsage is operating with cpuSeconds of CPU measured over the first tick's
// window — the one compute-usage event the app's customer is billed for.
func operatingWithUsage(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, app usage.OperatedAppID, cpuSeconds float64) {
	t.Helper()
	rt := stackOf(t, subject).runtime
	rt.usage(planProjectRef, cpuSeconds)
	operating(t, h, subject, app)
	rt.usage(planProjectRef, 0)
}

// tick runs one reconcile tick over app through the subject.
func tick(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, app usage.OperatedAppID) {
	t.Helper()
	s := stackOf(t, subject)
	s.ticks++
	var scope operations.ReconcileScope
	mustUnmarshal(t, fmt.Sprintf(`{"appIds":[%q]}`, app), &scope)
	if _, err := subject.ReconcileOperatedState(callContext(t, h, "arrange-tick"), fmt.Sprintf("stp-ops-arrange:%s:%d", t.Name(), s.ticks), &scope); err != nil {
		t.Fatalf("hooks: reconcile tick over %s: %v", app, err)
	}
}

// delinquent arranges the plan's customer as persistently delinquent on the fixture
// project — registered under it, with an escalated (unpaid) cycle — through
// billingStateAccess's own write verbs.
func delinquent(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager) {
	t.Helper()
	s := stackOf(t, subject)
	rc := h.ResourceAccessContext(t, s.arrangeKey("customer"))
	v, err := s.billing.RegisterCustomer(rc, planApp.CustomerID, 0, billingstate.CustomerProfile{ProjectID: planProjectRef}, rc.IdempotencyKey)
	if err != nil {
		t.Fatalf("hooks: register customer: %v", err)
	}
	outcome := billingstate.BillingOutcome{
		Net:       billingstate.Money{MinorUnits: 4200, Currency: "USD"},
		Directive: billingstate.RoutingCharge,
		Escalated: true,
	}
	rc = h.ResourceAccessContext(t, s.arrangeKey("settle"))
	if _, err := s.billing.SettleCycle(rc, planApp.CustomerID, v, planSettledCycle, outcome, rc.IdempotencyKey); err != nil {
		t.Fatalf("hooks: settle an escalated cycle: %v", err)
	}
}

// applyDelinquency calls ApplyDelinquencyPolicy — a queued op: it returns once the
// signal is enqueued — and, once the directive is accepted, waits for the enforcement
// workflow to record the action on app's head-state row (a version past seed).
func applyDelinquency(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, customer usage.CustomerID, dc operations.DelinquencyContext, app usage.OperatedAppID, seed operatedsystemstate.Version) error {
	t.Helper()
	if err := subject.ApplyDelinquencyPolicy(callContext(t, h, "1"), customer, dc); err != nil {
		return err
	}
	deadline := time.Now().Add(awaitEffect)
	for {
		op, err := stackOf(t, subject).state.ReadOperatedSystem(h.ResourceAccessContext(t, "read"), app)
		if err == nil && op.Version > seed {
			return nil
		}
		if time.Now().After(deadline) {
			t.Fatalf("hooks: operated app %s: no delinquency action recorded within %s (last read %+v, err %v)", app, awaitEffect, op, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// readHead reads app's head-state row for an arrangement check.
func readHead(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, app usage.OperatedAppID) operatedsystemstate.OperatedSystem {
	t.Helper()
	op, err := stackOf(t, subject).state.ReadOperatedSystem(h.ResourceAccessContext(t, "read"), app)
	if err != nil {
		t.Fatalf("hooks: read operated app %s: %v", app, err)
	}
	return op
}

// ---------------------------------------------------------------------------
// Probe bodies.
// ---------------------------------------------------------------------------

// readApp is every operatedSystemStateAccess.ReadOperatedSystem probe.
func readApp(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, app usage.OperatedAppID) (any, error) {
	t.Helper()
	return stackOf(t, subject).state.ReadOperatedSystem(h.ResourceAccessContext(t, "read"), app)
}

// readInFlight is every operatedSystemStateAccess.ReadInFlightOperatedApps probe;
// scopeJSON is the probe's scope input, verbatim from the binding.
func readInFlight(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, scopeJSON string) (any, error) {
	t.Helper()
	var scope operatedsystemstate.InFlightScope
	mustUnmarshal(t, scopeJSON, &scope)
	return stackOf(t, subject).state.ReadInFlightOperatedApps(h.ResourceAccessContext(t, "read"), scope)
}

// readUsage is every usageAccess.ReadRange probe: the usage the subject attributed to
// customer for app, in every cycle (an empty CycleID) — the binding names customer and
// app and never a cycle derived from the clock.
func readUsage(t *testing.T, h *scenariohost.Host, subject operations.OperationsManager, customer usage.CustomerID, app usage.OperatedAppID) (any, error) {
	t.Helper()
	q := usage.UsageRangeQuery{CustomerID: customer, OperatedAppID: &app}
	return stackOf(t, subject).usage.ReadRange(h.ResourceAccessContext(t, "read"), q)
}

// ---------------------------------------------------------------------------
// The project the operated app renders from, committed to the project repository.
// ---------------------------------------------------------------------------

// fixtureProject is the project planProjectRef names, in construction with its
// deployment & operations model committed: one cloud environment holding the app's own
// namespace — the server workload ("stp-operated-app", which also hosts the SPA's
// container) with its gateway (which delivers the SPA), identity provider and database —
// and the architect's laptop.
func fixtureProject() projectstate.Project {
	app := planProjectRef
	operated := projectstate.DeploymentNode{
		Key: "stp-operated-app", Name: "server Deployment", Technology: "Kubernetes Deployment", Instances: 2,
		ContainerInstances: []projectstate.ContainerInstance{
			{Key: "stp-ci-server", ContainerKey: app + "-server"},
			{Key: "stp-ci-webapp", ContainerKey: app + "-webapp"},
		},
	}
	namespace := projectstate.DeploymentNode{
		Key: "stp-ns-" + app, Name: app + " namespace", Technology: "k8s-namespace",
		Children: []projectstate.DeploymentNode{operated},
		InfrastructureNodes: []projectstate.InfrastructureNode{
			{Key: "stp-infra-gateway", Name: "Gateway", Role: projectstate.RoleGateway},
			{Key: "stp-infra-idp", Name: "Identity provider", Role: projectstate.RoleIdentityProvider},
			{Key: "stp-infra-db", Name: "Database", Role: projectstate.RoleDatabase},
		},
	}
	laptop := projectstate.DeploymentNode{Key: "stp-architect-laptop", Name: "Architect's laptop", Technology: "macOS, Windows or Linux"}
	model := &projectstate.DeploymentOperationsModel{
		// The committed scaling policy a deploy records with the published replicas: the
		// server's two instances may scale between one and four, targeting 70% CPU.
		ScalingPolicy: projectstate.ScalingPolicy{MinInstances: 1, MaxInstances: 4, TargetUtilizationPct: 70},
		Deployment: projectstate.DeploymentTopology{
			Environments: []projectstate.DeploymentEnvironment{{
				Profile: projectstate.ProfileCloud,
				Title:   "Cloud",
				Nodes:   []projectstate.DeploymentNode{namespace, laptop},
				Relationships: []projectstate.DeploymentRelationship{
					{From: "stp-infra-gateway", To: "stp-ci-webapp", Label: "Delivers the SPA", Technology: "HTTPS", Mode: projectstate.CallSync},
				},
			}},
		},
	}
	return projectstate.Project{
		ID:                  projectstate.ProjectID(app),
		Name:                "STP operations fixture",
		Version:             1,
		Phase:               projectstate.PhaseConstruction,
		OperationalConcepts: projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: model, Revisions: 1},
	}
}

// commitProject commits p to repo's main as .aiarch/state/project.json, written by the
// store's own encoder (projectstate.EncodeProjectJSON).
func commitProject(t *testing.T, repo ghtestinfra.LocalGitRepo, p projectstate.Project) {
	t.Helper()
	raw, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("hooks: encode project %s: %v", p.ID, err)
	}
	work := filepath.Join(t.TempDir(), "arrange")
	git(t, "", "clone", "--quiet", repo.Dir, work)
	stateDir := filepath.Join(work, ".aiarch", "state")
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		t.Fatalf("hooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "project.json"), raw, 0o600); err != nil {
		t.Fatalf("hooks: %v", err)
	}
	git(t, work, "add", ".aiarch/state/project.json")
	git(t, work, "-c", "user.name=scenario-arrange", "-c", "user.email=arrange@aiarch.local",
		"-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "arrange "+string(p.ID))
	git(t, work, "push", "--quiet", "origin", "main")
}

// git runs one git command in dir and returns its standard output.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("hooks: git %v: %v\n%s", args, err, stderr)
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// The operated runtime's backends, doubled at the Resource boundary.
// ---------------------------------------------------------------------------

// clusterToken is the bearer token the cluster double demands; a LIST without it is 401.
const clusterToken = "stp-ops-cluster-token"

// argoListPath is the one Kubernetes API path the real profile reads.
const argoListPath = "/apis/argoproj.io/v1alpha1/namespaces/argocd/applications"

// liveResource is one object Argo applied for an app (an entry of status.resources[]).
type liveResource struct{ Kind, Name, Namespace string }

// liveApp is one Argo Application the cluster lists.
type liveApp struct {
	AppID, Name, Namespace string
	Resources              []liveResource
}

// runtimeDouble is the operated runtime for one scenario. Its Argo applies what the real
// profile committed to the GitOps repository when sync is called — not before (Argo
// polls; a commit is not yet a running app) and, for a withdrawal, not until the next
// sync (Argo prunes asynchronously, so the withdrawn app still runs, and burns CPU, when
// the subject captures its final usage). Every synced object reports Healthy unless the
// scenario degrades its app. Its Prometheus answers the profile's three queries from the
// per-namespace series the scenario arranges.
type runtimeDouble struct {
	gitops ghtestinfra.LocalGitRepo

	mu        sync.Mutex
	live      []liveApp
	degraded  map[string]bool               // namespace -> the app's server is degraded
	cpu       map[string]float64            // namespace -> CPU-seconds over any window
	util      map[string]float64            // namespace -> server CPU use over its request (unset: no series)
	shortfall map[string]map[string]float64 // namespace -> deployment -> replicas unavailable

	cluster *httptest.Server
	prom    *httptest.Server
}

func newRuntimeDouble(t *testing.T, gitops ghtestinfra.LocalGitRepo) *runtimeDouble {
	t.Helper()
	rt := &runtimeDouble{gitops: gitops, degraded: map[string]bool{}, cpu: map[string]float64{}, util: map[string]float64{}, shortfall: map[string]map[string]float64{}}
	rt.cluster = httptest.NewTLSServer(http.HandlerFunc(rt.serveCluster))
	t.Cleanup(rt.cluster.Close)
	rt.prom = httptest.NewServer(http.HandlerFunc(rt.servePrometheus))
	t.Cleanup(rt.prom.Close)
	return rt
}

// sync is Argo applying the GitOps repository's main: every committed Application (and
// the objects under the directory it governs) is live, and nothing else is.
func (rt *runtimeDouble) sync(t *testing.T) {
	t.Helper()
	const applications, apps = "k8s/argocd/applications/", "k8s/argocd/apps/"
	files := strings.Fields(git(t, "", "--git-dir", rt.gitops.Dir, "ls-tree", "-r", "--name-only", "main"))
	var live []liveApp
	for _, f := range files {
		name, ok := strings.CutPrefix(f, applications)
		if !ok || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		name = strings.TrimSuffix(name, ".yaml")
		application := git(t, "", "--git-dir", rt.gitops.Dir, "show", "main:"+f)
		app := liveApp{Name: name, AppID: yamlValue(application, "    archistrator.dev/app-id: "), Namespace: yamlValue(application, "    namespace: ")}
		for _, m := range files {
			if strings.HasPrefix(m, apps+name+"/") {
				doc := git(t, "", "--git-dir", rt.gitops.Dir, "show", "main:"+m)
				app.Resources = append(app.Resources, liveResource{Kind: yamlValue(doc, "kind: "), Name: yamlValue(doc, "  name: "), Namespace: yamlValue(doc, "  namespace: ")})
			}
		}
		if app.AppID == "" || app.Namespace == "" {
			t.Fatalf("hooks: committed Application %s carries no app-id annotation or destination namespace", f)
		}
		live = append(live, app)
	}
	rt.mu.Lock()
	rt.live = live
	rt.mu.Unlock()
}

// yamlValue is the value of the first line of doc that starts with prefix.
func yamlValue(doc, prefix string) string {
	for line := range strings.SplitSeq(doc, "\n") {
		if v, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// degrade arranges the app in namespace degraded: its Application and its server
// Deployment report Degraded, and deployment is short replicas available.
func (rt *runtimeDouble) degrade(namespace, deployment string, replicas float64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.degraded[namespace] = true
	if rt.shortfall[namespace] == nil {
		rt.shortfall[namespace] = map[string]float64{}
	}
	rt.shortfall[namespace][deployment] = replicas
}

// degradeHealth arranges the app in namespace degraded with every replica available:
// Argo reports it Degraded, and its SLO still holds.
func (rt *runtimeDouble) degradeHealth(namespace string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.degraded[namespace] = true
}

// utilization arranges the server workload's CPU use over its request in namespace.
func (rt *runtimeDouble) utilization(namespace string, fraction float64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.util[namespace] = fraction
}

// usage arranges the CPU-seconds Prometheus reports for namespace (0: none).
func (rt *runtimeDouble) usage(namespace string, cpuSeconds float64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.cpu[namespace] = cpuSeconds
}

func (rt *runtimeDouble) serveCluster(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+clusterToken {
		http.Error(w, `{"kind":"Status","code":401}`, http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet || r.URL.Path != argoListPath {
		http.NotFound(w, r)
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	items := make([]any, 0, len(rt.live))
	for _, a := range rt.live {
		health := "Healthy"
		if rt.degraded[a.Namespace] {
			health = "Degraded"
		}
		resources := make([]any, 0, len(a.Resources))
		for _, res := range a.Resources {
			resHealth := "Healthy"
			if health == "Degraded" && res.Kind == "Deployment" && res.Name == a.Name+"-server" {
				resHealth = "Degraded"
			}
			resources = append(resources, map[string]any{
				"kind": res.Kind, "name": res.Name, "namespace": res.Namespace, "status": "Synced",
				"health": map[string]any{"status": resHealth},
			})
		}
		items = append(items, map[string]any{
			"metadata": map[string]any{"name": a.Name, "namespace": "argocd", "annotations": map[string]any{"archistrator.dev/app-id": a.AppID}},
			"spec":     map[string]any{"destination": map[string]any{"namespace": a.Namespace}},
			"status":   map[string]any{"health": map[string]any{"status": health}, "resources": resources},
		})
	}
	writeJSON(w, map[string]any{"kind": "ApplicationList", "items": items})
}

var namespaceMatcher = regexp.MustCompile(`namespace="([^"]+)"`)

func (rt *runtimeDouble) servePrometheus(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/query" {
		http.NotFound(w, r)
		return
	}
	query := r.URL.Query().Get("query")
	at, err := strconv.ParseFloat(r.URL.Query().Get("time"), 64)
	m := namespaceMatcher.FindStringSubmatch(query)
	if err != nil || m == nil {
		writeJSON(w, map[string]any{"status": "error", "errorType": "bad_data", "error": "unscoped or untimed query"})
		return
	}
	ns := m[1]
	rt.mu.Lock()
	defer rt.mu.Unlock()
	result := []any{}
	switch {
	case strings.Contains(query, "kube_pod_container_resource_requests"):
		// The server's CPU utilization — checked before the plain CPU series, whose
		// metric name this query also contains.
		if v, ok := rt.util[ns]; ok {
			result = append(result, map[string]any{"metric": map[string]any{}, "value": []any{at, strconv.FormatFloat(v, 'f', -1, 64)}})
		}
	case strings.Contains(query, "container_cpu_usage_seconds_total"):
		if v := rt.cpu[ns]; v > 0 {
			result = append(result, map[string]any{"metric": map[string]any{}, "value": []any{at, strconv.FormatFloat(v, 'f', -1, 64)}})
		}
	case strings.Contains(query, "kube_deployment_status_replicas_available"):
		for dep, short := range rt.shortfall[ns] {
			result = append(result, map[string]any{
				"metric": map[string]any{"namespace": ns, "deployment": dep},
				"value":  []any{at, strconv.FormatFloat(short, 'f', -1, 64)},
			})
		}
	default:
		writeJSON(w, map[string]any{"status": "error", "errorType": "bad_data", "error": "series not doubled"})
		return
	}
	writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": result}})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
