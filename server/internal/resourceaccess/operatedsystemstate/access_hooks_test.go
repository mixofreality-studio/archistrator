// Hooks for operatedSystemStateAccess scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.

package operatedsystemstate_test

import (
	"fmt"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres/testinfra"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
)

// newSubject builds the component under test the way the composition root does:
// cmd/server/main.gen.go constructs operatedSystemStateAccess with the generated
// NewPostgresOperatedSystemStateAccess over a pgx pool, which applies the
// package's own schema. Every scenario gets its own empty Postgres, so the head
// state a scenario observes is exactly what its own arrangement and stimuli wrote.
//
// The pool comes from the platform's Postgres testinfra rather than the host's
// PostgresURL: opening a pool on a URL needs pgxpool or the postgres
// infrastructure module, neither of which a hooks file may import, while a
// …/testinfra package is admitted (the billingstate hooks do the same).
func newSubject(t *testing.T, h *scenariohost.Host) operatedsystemstate.OperatedSystemStateAccess {
	t.Helper()
	pool := testinfra.StartPostgres(t)
	subject, err := operatedsystemstate.NewPostgresOperatedSystemStateAccess(h.Context(t), pool)
	if err != nil {
		t.Fatalf("hooks: NewPostgresOperatedSystemStateAccess: %v", err)
	}
	return subject
}

// The arrangement calls the contract's own write ops. Arguments that are not
// already typed on a step's input are decoded from JSON into the generated
// step-input types of those ops, so a hook names no type outside this package,
// the contract package and the generated scenarios file.
type (
	registerArgs = inputRegisterADeliveredSystemForOperationP1S1 // RegisterOperatedSystem
	publishArgs  = inputOperateADeliveredSystemP1S2              // PublishDesiredState
	withdrawArgs = inputWithdrawADeliveredSystemP2S2             // WithdrawSystem
)

// The bindings name every operated app 0c5a0000-…-NNNNNNNNNNNN and its customer
// 0c5ac000-…-NNNNNNNNNNNN, project proj-osa-NNNN and bundle bundle-osa-NNNN, NNNN
// being the app id's last four hex digits.
func customerOf(appID string) string { return "0c5ac" + appID[5:] }
func projectOf(appID string) string  { return "proj-osa-" + appID[len(appID)-4:] }
func bundleOf(appID string) string   { return "bundle-osa-" + appID[len(appID)-4:] }

// arrangeRegistered registers the operated app as the operator's Register a
// Delivered System for Operation does: head state version 1, status Unknown,
// NOT in flight (spec §12 B9 — the first desired-state publish puts it in flight).
func arrangeRegistered(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, appID, customerID, projectRef, bundleRef string) {
	t.Helper()
	var a registerArgs
	mustUnmarshal(t, fmt.Sprintf(`{"operatedAppID":%q,"customerID":%q,"projectRef":%q,"deployableBundleRef":%q,"idempotencyKey":"arrange-register:%s"}`,
		appID, customerID, projectRef, bundleRef, appID), &a)
	v, err := subject.RegisterOperatedSystem(callContext(t, h, "arrange-register"), a.OperatedAppID, a.CustomerID, a.ProjectRef, a.DeployableBundleRef, a.IdempotencyKey)
	if err != nil {
		t.Fatalf("arrange: RegisterOperatedSystem %s: %v", appID, err)
	}
	if v != 1 {
		t.Fatalf("arrange: RegisterOperatedSystem %s: version %d, want 1", appID, v)
	}
}

// arrangePublish records one desired-state publish against expectedVersion and
// checks the head state moved to want. expectedVersion 0 is the contract's create
// seam: it creates the head-state row in flight at version 1, carrying no
// customer, project or bundle. The publish carries arrangedScale, a deploy's
// desired scale: two server replicas under a 1..4 policy targeting 70% CPU.
func arrangePublish(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, appID string, expectedVersion, want int) {
	t.Helper()
	var a publishArgs
	mustUnmarshal(t, fmt.Sprintf(`{"operatedAppID":%q,"expectedVersion":%d,"reason":1,"scale":%s,"idempotencyKey":"arrange-publish:%s:%d"}`,
		appID, expectedVersion, arrangedScale, appID, expectedVersion), &a)
	v, err := subject.PublishDesiredState(callContext(t, h, "arrange-publish"), a.OperatedAppID, a.ExpectedVersion, a.Reason, a.Scale, a.IdempotencyKey)
	if err != nil {
		t.Fatalf("arrange: PublishDesiredState %s @%d: %v", appID, expectedVersion, err)
	}
	if int(v) != want {
		t.Fatalf("arrange: PublishDesiredState %s @%d: version %d, want %d", appID, expectedVersion, v, want)
	}
}

// arrangedScale is the desired scale every arranged publish records.
const arrangedScale = `{"Replicas":2,"MinReplicas":1,"MaxReplicas":4,"TargetUtilizationPct":70,"ScaleToZero":false,"ScaledAt":"2026-10-01T00:00:00Z"}`

// arrangeInFlight brings the app into operation through the contract's create
// seam: a version-0 PublishDesiredState, head state version 1, in flight. The
// bindings that use it assert only the app's identity, version and flight, none
// of the onboarding columns a registration would carry.
func arrangeInFlight(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, appID string) {
	t.Helper()
	arrangePublish(t, h, subject, appID, 0, 1)
}

// arrangeDeployed is the production onboarding path for a delivered system:
// registered with its customer, project and bundle (version 1, not in flight),
// then its first deploy-after-construction publish (version 2, in flight) — what
// operationsManager's register then deploy workflows write.
func arrangeDeployed(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, appID, customerID string) {
	t.Helper()
	arrangeRegistered(t, h, subject, appID, customerID, projectOf(appID), bundleOf(appID))
	arrangePublish(t, h, subject, appID, 1, 2)
}

// arrangeWithdrawn brings the app into operation and withdraws it (version 2,
// Withdrawn, no longer in flight).
func arrangeWithdrawn(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, appID string) {
	t.Helper()
	arrangeInFlight(t, h, subject, appID)
	var a withdrawArgs
	mustUnmarshal(t, fmt.Sprintf(`{"operatedAppID":%q,"expectedVersion":1,"idempotencyKey":"arrange-withdraw:%s"}`, appID, appID), &a)
	v, err := subject.WithdrawSystem(callContext(t, h, "arrange-withdraw"), a.OperatedAppID, a.ExpectedVersion, a.IdempotencyKey)
	if err != nil {
		t.Fatalf("arrange: WithdrawSystem %s: %v", appID, err)
	}
	if v != 2 {
		t.Fatalf("arrange: WithdrawSystem %s: version %d, want 2", appID, v)
	}
}

// delinquentCustomerApp is the one operated app each delinquency binding expects
// the customer-scoped read to find: the customer's id with its app prefix.
func delinquentCustomerApp(t *testing.T, scope operatedsystemstate.InFlightScope) string {
	t.Helper()
	if scope.CustomerID == nil {
		t.Fatal("arrange: the delinquency read is customer-scoped, but the binding names no CustomerID")
	}
	c := scope.CustomerID.String()
	return "0c5a0" + c[5:]
}

// onlyAppID is the single app a subset-scoped read names.
func onlyAppID(t *testing.T, scope operatedsystemstate.InFlightScope) string {
	t.Helper()
	if len(scope.AppIDs) != 1 {
		t.Fatalf("arrange: want one AppID in scope, got %d", len(scope.AppIDs))
	}
	return scope.AppIDs[0].String()
}

// stepEnforceCustomerDelinquencyOnOperatedSystemsP2S1 — Enforce Customer Delinquency on Operated Systems — [yes] · [pause], step 1: ReadInFlightOperatedApps
// Projected stimulus (JSON): {"seq":1,"node":"read-customer-systems","input":{"kind":"call","from":"operations-manager","to":"operatedsystemstateaccess","op":"ReadInFlightOperatedApps","call":"enforce-customer-delinquency-on-operated-systems/read-customer-systems/0"},"expectedOutputs":[{"node":"read-customer-systems","kind":"inferred","to":"","label":"Read the delinquent customer's in-flight systems"}],"expectedStates":null,"hints":["Pause does not yet publish a runtime scale-to-zero (replica-override follow-up). It is recorded at head-state level only, so expect no runtime call on this arm."]}
// Binding (JSON): {"seq":1,"operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[],\"CustomerID\":\"0c5ac000-0000-4000-8000-000000000001\"}"}],"expect":{"result":"[{\"ID\":\"0c5a0000-0000-4000-8000-000000000001\",\"Version\":2}]","errorExpected":false},"probes":[{"for":"read-customer-systems","component":"operatedSystemStateAccess","operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[],\"CustomerID\":\"0c5ac000-0000-4000-8000-000000000001\"}"}],"expect":{"result":"[{\"ID\":\"0c5a0000-0000-4000-8000-000000000001\",\"Version\":2}]","errorExpected":false}}],"unobservable":[],"hook":true}
// Guards directive-named=[yes] and action-kind=[pause] are the Manager's: the
// directive names a customer with one operated system. Arranged on the production
// onboarding path (register under the customer, then the first deploy), which
// leaves that system in flight at version 2.
func stepEnforceCustomerDelinquencyOnOperatedSystemsP2S1(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, in inputEnforceCustomerDelinquencyOnOperatedSystemsP2S1) (any, error) {
	t.Helper()
	app := delinquentCustomerApp(t, in.Scope)
	arrangeDeployed(t, h, subject, app, in.Scope.CustomerID.String())
	return subject.ReadInFlightOperatedApps(callContext(t, h, "1"), in.Scope)
}

// stepEnforceCustomerDelinquencyOnOperatedSystemsP3S1 — Enforce Customer Delinquency on Operated Systems — [yes] · [withdraw], step 1: ReadInFlightOperatedApps
// Projected stimulus (JSON): {"seq":1,"node":"read-customer-systems","input":{"kind":"call","from":"operations-manager","to":"operatedsystemstateaccess","op":"ReadInFlightOperatedApps","call":"enforce-customer-delinquency-on-operated-systems/read-customer-systems/0"},"expectedOutputs":[{"node":"read-customer-systems","kind":"inferred","to":"","label":"Read the delinquent customer's in-flight systems"}],"expectedStates":null,"hints":["Pause does not yet publish a runtime scale-to-zero (replica-override follow-up). It is recorded at head-state level only, so expect no runtime call on this arm."]}
// Binding (JSON): {"seq":1,"operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[],\"CustomerID\":\"0c5ac000-0000-4000-8000-000000000002\"}"}],"expect":{"result":"[{\"ID\":\"0c5a0000-0000-4000-8000-000000000002\",\"Version\":2}]","errorExpected":false},"probes":[{"for":"read-customer-systems","component":"operatedSystemStateAccess","operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[],\"CustomerID\":\"0c5ac000-0000-4000-8000-000000000002\"}"}],"expect":{"result":"[{\"ID\":\"0c5a0000-0000-4000-8000-000000000002\",\"Version\":2}]","errorExpected":false}}],"unobservable":[],"hook":true}
// Guards directive-named=[yes] and action-kind=[withdraw] are the Manager's: the
// directive names a customer with one operated system. Arranged on the production
// onboarding path (register under the customer, then the first deploy), which
// leaves that system in flight at version 2.
func stepEnforceCustomerDelinquencyOnOperatedSystemsP3S1(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, in inputEnforceCustomerDelinquencyOnOperatedSystemsP3S1) (any, error) {
	t.Helper()
	app := delinquentCustomerApp(t, in.Scope)
	arrangeDeployed(t, h, subject, app, in.Scope.CustomerID.String())
	return subject.ReadInFlightOperatedApps(callContext(t, h, "1"), in.Scope)
}

// stepOperateADeliveredSystemP1S1 — Operate a Delivered System, step 1: ReadOperatedSystem
// Projected stimulus (JSON): {"seq":1,"node":"render-manifests-a","input":{"kind":"call","from":"operations-manager","to":"operatedsystemstateaccess","op":"ReadOperatedSystem","call":"operate-a-delivered-system/render-manifests-a/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"0c5a0000-0000-4000-8000-000000000003"}],"expect":{"result":"{\"ID\":\"0c5a0000-0000-4000-8000-000000000003\",\"Version\":2,\"InFlight\":true,\"DeployableBundleRef\":\"bundle-osa-0003\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// No guards: the publish trigger reads the delivered system before rendering.
// Arranged on the production onboarding path: the system registered with its
// customer, project and deployable bundle and taken live by its first deploy.
func stepOperateADeliveredSystemP1S1(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, in inputOperateADeliveredSystemP1S1) (any, error) {
	t.Helper()
	app := in.OperatedAppID.String()
	arrangeDeployed(t, h, subject, app, customerOf(app))
	return subject.ReadOperatedSystem(callContext(t, h, "1"), in.OperatedAppID)
}

// stepOperateADeliveredSystemP2S1 — Operate a Delivered System — [no] · [yes], step 1: ReadInFlightOperatedApps
// Projected stimulus (JSON): {"seq":1,"node":"read-app-set","input":{"kind":"call","from":"operations-manager","to":"operatedsystemstateaccess","op":"ReadInFlightOperatedApps","call":"operate-a-delivered-system/read-app-set/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[\"0c5a0000-0000-4000-8000-000000000004\"]}"}],"expect":{"result":"[{\"ID\":\"0c5a0000-0000-4000-8000-000000000004\",\"Version\":1}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Guards state-transition and decision-nochange are decided downstream (runtime
// telemetry, autoscaler); for this component the reconcile tick needs one
// in-flight app at version 1. Arranged: that app brought into operation.
func stepOperateADeliveredSystemP2S1(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, in inputOperateADeliveredSystemP2S1) (any, error) {
	t.Helper()
	arrangeInFlight(t, h, subject, onlyAppID(t, in.Scope))
	return subject.ReadInFlightOperatedApps(callContext(t, h, "1"), in.Scope)
}

// stepOperateADeliveredSystemP3S1 — Operate a Delivered System — [no] · [no: scale/pause/resume], step 1: ReadInFlightOperatedApps
// Projected stimulus (JSON): {"seq":1,"node":"read-app-set","input":{"kind":"call","from":"operations-manager","to":"operatedsystemstateaccess","op":"ReadInFlightOperatedApps","call":"operate-a-delivered-system/read-app-set/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[\"0c5a0000-0000-4000-8000-000000000005\"]}"}],"expect":{"result":"[{\"ID\":\"0c5a0000-0000-4000-8000-000000000005\",\"Version\":1}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Guards state-transition and decision-nochange are decided downstream (runtime
// telemetry, autoscaler); for this component the reconcile tick needs one
// in-flight app at version 1. Arranged: that app brought into operation.
func stepOperateADeliveredSystemP3S1(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, in inputOperateADeliveredSystemP3S1) (any, error) {
	t.Helper()
	arrangeInFlight(t, h, subject, onlyAppID(t, in.Scope))
	return subject.ReadInFlightOperatedApps(callContext(t, h, "1"), in.Scope)
}

// stepOperateADeliveredSystemP4S1 — Operate a Delivered System — [yes] · [yes], step 1: ReadInFlightOperatedApps
// Projected stimulus (JSON): {"seq":1,"node":"read-app-set","input":{"kind":"call","from":"operations-manager","to":"operatedsystemstateaccess","op":"ReadInFlightOperatedApps","call":"operate-a-delivered-system/read-app-set/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[\"0c5a0000-0000-4000-8000-000000000006\"]}"}],"expect":{"result":"[{\"ID\":\"0c5a0000-0000-4000-8000-000000000006\",\"Version\":1}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Guards state-transition and decision-nochange are decided downstream (runtime
// telemetry, autoscaler); for this component the reconcile tick needs one
// in-flight app at version 1. Arranged: that app brought into operation.
func stepOperateADeliveredSystemP4S1(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, in inputOperateADeliveredSystemP4S1) (any, error) {
	t.Helper()
	arrangeInFlight(t, h, subject, onlyAppID(t, in.Scope))
	return subject.ReadInFlightOperatedApps(callContext(t, h, "1"), in.Scope)
}

// stepOperateADeliveredSystemP5S1 — Operate a Delivered System — [yes] · [no: scale/pause/resume], step 1: ReadInFlightOperatedApps
// Projected stimulus (JSON): {"seq":1,"node":"read-app-set","input":{"kind":"call","from":"operations-manager","to":"operatedsystemstateaccess","op":"ReadInFlightOperatedApps","call":"operate-a-delivered-system/read-app-set/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadInFlightOperatedApps","inputs":[{"name":"scope","value":"{\"AppIDs\":[\"0c5a0000-0000-4000-8000-000000000007\"]}"}],"expect":{"result":"[{\"ID\":\"0c5a0000-0000-4000-8000-000000000007\",\"Version\":1}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Guards state-transition and decision-nochange are decided downstream (runtime
// telemetry, autoscaler); for this component the reconcile tick needs one
// in-flight app at version 1. Arranged: that app brought into operation.
func stepOperateADeliveredSystemP5S1(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, in inputOperateADeliveredSystemP5S1) (any, error) {
	t.Helper()
	arrangeInFlight(t, h, subject, onlyAppID(t, in.Scope))
	return subject.ReadInFlightOperatedApps(callContext(t, h, "1"), in.Scope)
}

// stepViewLiveDeploymentHealthP1S1 — View Live Deployment Health — [yes], step 1: ReadOperatedSystem
// Projected stimulus (JSON): {"seq":1,"node":"request-health","input":{"kind":"call","from":"operations-manager","to":"operatedsystemstateaccess","op":"ReadOperatedSystem","call":"view-live-deployment-health/request-health/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"0c5a0000-0000-4000-8000-000000000009"}],"expect":{"result":"{\"ID\":\"0c5a0000-0000-4000-8000-000000000009\",\"InFlight\":true,\"ProjectRef\":\"proj-osa-0009\",\"DeployableBundleRef\":\"bundle-osa-0009\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Guard anything-deployed=[yes]: the system has a deployable bundle and is
// operated. Arranged: registered with its project and bundle, then deployed.
func stepViewLiveDeploymentHealthP1S1(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, in inputViewLiveDeploymentHealthP1S1) (any, error) {
	t.Helper()
	app := in.OperatedAppID.String()
	arrangeDeployed(t, h, subject, app, customerOf(app))
	return subject.ReadOperatedSystem(callContext(t, h, "1"), in.OperatedAppID)
}

// stepViewLiveDeploymentHealthP2S1 — View Live Deployment Health — [no: no deployable bundle], step 1: ReadOperatedSystem
// Projected stimulus (JSON): {"seq":1,"node":"request-health","input":{"kind":"call","from":"operations-manager","to":"operatedsystemstateaccess","op":"ReadOperatedSystem","call":"view-live-deployment-health/request-health/2"},"expectedOutputs":[{"node":"send-nothing-deployed","kind":"sendSignal","to":"operator","label":"Report to the Operator that nothing has been deployed yet, so there is no live health (FailedPrecondition)"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"0c5a0000-0000-4000-8000-00000000000a"}],"expect":{"result":"{\"ID\":\"0c5a0000-0000-4000-8000-00000000000a\",\"ProjectRef\":\"proj-osa-000a\",\"DeployableBundleRef\":\"\"}","errorExpected":false},"probes":[],"unobservable":["send-nothing-deployed"],"hook":true}
// Guard anything-deployed=[no: no deployable bundle]: the system is registered
// with its project but no deployable bundle yet. Arranged: exactly that.
func stepViewLiveDeploymentHealthP2S1(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, in inputViewLiveDeploymentHealthP2S1) (any, error) {
	t.Helper()
	app := in.OperatedAppID.String()
	arrangeRegistered(t, h, subject, app, customerOf(app), projectOf(app), "")
	return subject.ReadOperatedSystem(callContext(t, h, "1"), in.OperatedAppID)
}

// stepViewOperatingCostProjectionP1S1 — View Operating-Cost Projection — [yes], step 1: ReadOperatedSystem
// Projected stimulus (JSON): {"seq":1,"node":"request-projection","input":{"kind":"call","from":"operations-manager","to":"operatedsystemstateaccess","op":"ReadOperatedSystem","call":"view-operating-cost-projection/request-projection/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"0c5a0000-0000-4000-8000-00000000000b"}],"expect":{"result":"{\"ID\":\"0c5a0000-0000-4000-8000-00000000000b\",\"Version\":1,\"InFlight\":true}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Guard app-known=[yes]: the app is operated. Arranged: brought into operation
// (version 1, in flight).
func stepViewOperatingCostProjectionP1S1(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, in inputViewOperatingCostProjectionP1S1) (any, error) {
	t.Helper()
	arrangeInFlight(t, h, subject, in.OperatedAppID.String())
	return subject.ReadOperatedSystem(callContext(t, h, "1"), in.OperatedAppID)
}

// stepWithdrawADeliveredSystemP1S1 — Withdraw a Delivered System — [yes], step 1: ReadOperatedSystem
// Projected stimulus (JSON): {"seq":1,"node":"request-withdraw","input":{"kind":"call","from":"operations-manager","to":"operatedsystemstateaccess","op":"ReadOperatedSystem","call":"withdraw-a-delivered-system/request-withdraw/4"},"expectedOutputs":[{"node":"send-withdrawn","kind":"sendSignal","to":"operator","label":"Confirm to the Operator that the system is withdrawn"}],"expectedStates":null,"hints":["Withdrawing an unknown or already-withdrawn system succeeds as a no-op. The desired post-condition, no running runtime, already holds."]}
// Binding (JSON): {"seq":1,"operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"0c5a0000-0000-4000-8000-00000000000d"}],"expect":{"result":"{\"ID\":\"0c5a0000-0000-4000-8000-00000000000d\",\"Version\":2,\"Status\":4,\"InFlight\":false}","errorExpected":false},"probes":[],"unobservable":["send-withdrawn"],"hook":true}
// Guard already-withdrawn=[yes]. Arranged: the system brought into operation,
// then withdrawn (version 2, Withdrawn, not in flight).
func stepWithdrawADeliveredSystemP1S1(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, in inputWithdrawADeliveredSystemP1S1) (any, error) {
	t.Helper()
	arrangeWithdrawn(t, h, subject, in.OperatedAppID.String())
	return subject.ReadOperatedSystem(callContext(t, h, "1"), in.OperatedAppID)
}

// stepWithdrawADeliveredSystemP2S1 — Withdraw a Delivered System — [no: in flight], step 1: ReadOperatedSystem
// Projected stimulus (JSON): {"seq":1,"node":"request-withdraw","input":{"kind":"call","from":"operations-manager","to":"operatedsystemstateaccess","op":"ReadOperatedSystem","call":"withdraw-a-delivered-system/request-withdraw/4"},"expectedOutputs":null,"expectedStates":null,"hints":["Withdrawing an unknown or already-withdrawn system succeeds as a no-op. The desired post-condition, no running runtime, already holds."]}
// Binding (JSON): {"seq":1,"operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"0c5a0000-0000-4000-8000-00000000000e"}],"expect":{"result":"{\"ID\":\"0c5a0000-0000-4000-8000-00000000000e\",\"Version\":1,\"InFlight\":true}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Guard already-withdrawn=[no: in flight]. Arranged: the system brought into
// operation (version 1, in flight).
func stepWithdrawADeliveredSystemP2S1(t *testing.T, h *scenariohost.Host, subject operatedsystemstate.OperatedSystemStateAccess, in inputWithdrawADeliveredSystemP2S1) (any, error) {
	t.Helper()
	arrangeInFlight(t, h, subject, in.OperatedAppID.String())
	return subject.ReadOperatedSystem(callContext(t, h, "1"), in.OperatedAppID)
}
