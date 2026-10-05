// Hooks for artifactAccess scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.
//
// The subject is the LOCAL-profile artifactAccess (the file:// variant the
// composition root builds) over the host's real bare git repo, so every store
// and retrieve below is a real commit / read in git. Two kinds of arrangement
// recur:
//
//   - A RetrieveConstructionOutput step's binding names its address with a
//     placeholder ("sha-from-store-step", "sha-of-hook-arranged-output"): the
//     caller holds the address StoreConstructionOutput handed it, which the
//     generated step does not thread through. The hook recovers that address
//     the way the contract defines it — storing byte-identical content returns
//     the EXISTING address (artifactAccess §2.1 content-addressable
//     idempotency) — checks that a second store collapses to the same address,
//     then retrieves by it as the caller does.
//   - A GetTestRunIndex step reads a LOCAL test run: TestRunRef.Artifact is the
//     results directory relative to the server's working directory (the state
//     repo `serve` pins it to). The hook lays that directory down in a fresh
//     working directory and moves the scenario into it for its duration, so a
//     later unhooked ReadTestRunFile step reads the same run.
package artifact_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
)

// newSubject builds the component under test against the host's real downstream
// doubles: the LOCAL-profile variant over the host's bare git repo (file://).
func newSubject(t *testing.T, h *scenariohost.Host) artifact.ArtifactAccess {
	t.Helper()
	if h.Repo.URL == "" {
		t.Fatal("scenariohost: no local git repo booted")
	}
	return artifact.NewGitLocalArtifactAccess(h.Repo.URL)
}

// The construction outputs the scenarios' store steps commit (the step inputs'
// bytes, decoded) and the outputs operations reads back as already delivered.
const (
	outputP1         = `{"activity":"stp-act-P1","task":"build","revision":1,"files":["main.go"]}`
	outputP2         = `{"activity":"stp-act-P2","task":"build","revision":1,"files":["handler.go"]}`
	outputP5Rev1     = `{"activity":"stp-act-P5","task":"build","revision":1,"files":["store.go"]}`
	outputP5Rev2     = `{"activity":"stp-act-P5","task":"build","revision":2,"files":["store.go","store_fix.go"]}`
	outputP6         = `{"activity":"stp-act-P6","task":"build","revision":1,"files":["engine.go"]}`
	outputP7Rev2     = `{"activity":"stp-act-P7","task":"build","revision":2,"files":["client.go"]}`
	deployedManifest = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: stp-operated-app\n"
	healthManifest   = "apiVersion: v1\nkind: Service\nmetadata:\n  name: stp-health-app\n"
)

const (
	mimeJSON = "application/json"
	mimeYAML = "application/yaml"
)

// addressOf recovers the content address the caller of a store holds for
// content: storing byte-identical content returns the existing address. Two
// stores must agree — a store that minted a fresh address for identical
// content would break the contract's dedup guarantee, and with it every
// address a caller persisted.
func addressOf(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, placeholder string, content artifact.ConstructionOutput) string {
	t.Helper()
	first, err := subject.StoreConstructionOutput(callContext(t, h, "arrange-"+placeholder), content)
	if err != nil {
		t.Fatalf("arrange %s: store: %v", placeholder, err)
	}
	again, err := subject.StoreConstructionOutput(callContext(t, h, "arrange-"+placeholder+"-again"), content)
	if err != nil {
		t.Fatalf("arrange %s: re-store: %v", placeholder, err)
	}
	if again != first {
		t.Fatalf("arrange %s: identical content got two addresses %q and %q", placeholder, first, again)
	}
	return first
}

// retrieveStored resolves the step's placeholder address to the one the
// caller holds for content and retrieves by it.
func retrieveStored(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, placeholder, bytes, mime string) (any, error) {
	t.Helper()
	addr := addressOf(t, h, subject, placeholder, artifact.ConstructionOutput{Bytes: []byte(bytes), MIMEType: mime})
	return subject.RetrieveConstructionOutput(callContext(t, h, "retrieve"), addr)
}

// arrangeTestRun lays down the LOCAL test run run.Artifact names — the
// component's results.json plus the scenario's log — in a fresh working
// directory, and moves the scenario there until it ends.
func arrangeTestRun(t *testing.T, run artifact.TestRunRef, scenario string) {
	t.Helper()
	wd := t.TempDir()
	root := filepath.Join(wd, run.Artifact)
	results, err := json.Marshal([]map[string]any{{"scenario": scenario, "status": "pass", "durationMs": 1}})
	if err != nil {
		t.Fatalf("arrange test run: encode results: %v", err)
	}
	files := map[string][]byte{
		"results.json":                   results,
		"scenarios/" + scenario + ".log": []byte("--- PASS: " + scenario + "\n"),
	}
	for rel, b := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("arrange test run: %v", err)
		}
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatalf("arrange test run: %v", err)
		}
	}
	t.Chdir(wd)
}

// indexTestRun arranges the run and reads its index as the delivery manager
// does when a review task opens.
func indexTestRun(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, run artifact.TestRunRef, scenario string) (any, error) {
	t.Helper()
	arrangeTestRun(t, run, scenario)
	return subject.GetTestRunIndex(callContext(t, h, "index"), run)
}

// stepExecuteAProjectActivityP1S2 — Execute a Project Activity — dispatch · yes, step 2: RetrieveConstructionOutput
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"artifactaccess","op":"RetrieveConstructionOutput","call":"execute-a-project-activity/observe-validate/4.1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"RetrieveConstructionOutput","inputs":[{"name":"contentAddress","value":"sha-from-store-step"}],"expect":{"result":"{\"Bytes\":\"eyJhY3Rpdml0eSI6InN0cC1hY3QtUDEiLCJ0YXNrIjoiYnVpbGQiLCJyZXZpc2lvbiI6MSwiZmlsZXMiOlsibWFpbi5nbyJdfQ==\",\"MIMEType\":\"application/json\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// The address is the one step 1's store returned for outputP1.
func stepExecuteAProjectActivityP1S2(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, in inputExecuteAProjectActivityP1S2) (any, error) {
	t.Helper()
	return retrieveStored(t, h, subject, in.ContentAddress, outputP1, mimeJSON)
}

// stepExecuteAProjectActivityP2S2 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 2: RetrieveConstructionOutput
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"artifactaccess","op":"RetrieveConstructionOutput","call":"execute-a-project-activity/observe-validate/4.1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"RetrieveConstructionOutput","inputs":[{"name":"contentAddress","value":"sha-from-store-step"}],"expect":{"result":"{\"Bytes\":\"eyJhY3Rpdml0eSI6InN0cC1hY3QtUDIiLCJ0YXNrIjoiYnVpbGQiLCJyZXZpc2lvbiI6MSwiZmlsZXMiOlsiaGFuZGxlci5nbyJdfQ==\",\"MIMEType\":\"application/json\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// The address is the one step 1's store returned for outputP2.
func stepExecuteAProjectActivityP2S2(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, in inputExecuteAProjectActivityP2S2) (any, error) {
	t.Helper()
	return retrieveStored(t, h, subject, in.ContentAddress, outputP2, mimeJSON)
}

// stepExecuteAProjectActivityP3S1 — Execute a Project Activity — review · agents only · approve, step 1: GetTestRunIndex
// Projected stimulus (JSON): {"seq":1,"node":"requires-human","input":{"kind":"call","from":"delivery-manager","to":"artifactaccess","op":"GetTestRunIndex","call":"execute-a-project-activity/requires-human/1","guard":"agents only"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"GetTestRunIndex","inputs":[{"name":"run","value":"{\"RunID\":\"stp-artifact-P3-run-0001\",\"Artifact\":\"scenario-results\"}"}],"expect":{"result":"{\"Expired\":false,\"Files\":[\"results.json\",\"scenarios/execute-a-project-activity-P3.log\"]}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// The review task's run is recorded locally; the agents-only verdict reads its index.
func stepExecuteAProjectActivityP3S1(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, in inputExecuteAProjectActivityP3S1) (any, error) {
	t.Helper()
	return indexTestRun(t, h, subject, in.Run, "execute-a-project-activity-P3")
}

// stepExecuteAProjectActivityP4S1 — Execute a Project Activity — review · human required · approve, step 1: GetTestRunIndex
// Projected stimulus (JSON): {"seq":1,"node":"requires-human","input":{"kind":"call","from":"delivery-manager","to":"artifactaccess","op":"GetTestRunIndex","call":"execute-a-project-activity/requires-human/1","guard":"human required"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"GetTestRunIndex","inputs":[{"name":"run","value":"{\"RunID\":\"stp-artifact-P4-run-0001\",\"Artifact\":\"scenario-results\"}"}],"expect":{"result":"{\"Expired\":false,\"Files\":[\"results.json\",\"scenarios/execute-a-project-activity-P4.log\"]}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// The run stays in place for step 2, where the human reviewer opens the scenario log.
func stepExecuteAProjectActivityP4S1(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, in inputExecuteAProjectActivityP4S1) (any, error) {
	t.Helper()
	return indexTestRun(t, h, subject, in.Run, "execute-a-project-activity-P4")
}

// stepExecuteAProjectActivityP5S2 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 2: RetrieveConstructionOutput
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"artifactaccess","op":"RetrieveConstructionOutput","call":"execute-a-project-activity/observe-validate/4.1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"RetrieveConstructionOutput","inputs":[{"name":"contentAddress","value":"sha-from-store-step"}],"expect":{"result":"{\"Bytes\":\"eyJhY3Rpdml0eSI6InN0cC1hY3QtUDUiLCJ0YXNrIjoiYnVpbGQiLCJyZXZpc2lvbiI6MSwiZmlsZXMiOlsic3RvcmUuZ28iXX0=\",\"MIMEType\":\"application/json\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// The first attempt's output (step 1, revision 1).
func stepExecuteAProjectActivityP5S2(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, in inputExecuteAProjectActivityP5S2) (any, error) {
	t.Helper()
	return retrieveStored(t, h, subject, in.ContentAddress, outputP5Rev1, mimeJSON)
}

// stepExecuteAProjectActivityP5S4 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 4: RetrieveConstructionOutput
// Projected stimulus (JSON): {"seq":4,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"artifactaccess","op":"RetrieveConstructionOutput","call":"execute-a-project-activity/observe-validate/4.1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"RetrieveConstructionOutput","inputs":[{"name":"contentAddress","value":"sha-from-store-step-3"}],"expect":{"result":"{\"Bytes\":\"eyJhY3Rpdml0eSI6InN0cC1hY3QtUDUiLCJ0YXNrIjoiYnVpbGQiLCJyZXZpc2lvbiI6MiwiZmlsZXMiOlsic3RvcmUuZ28iLCJzdG9yZV9maXguZ28iXX0=\",\"MIMEType\":\"application/json\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// The retry's output (step 3, revision 2): a different address from revision 1's, which stays retrievable.
func stepExecuteAProjectActivityP5S4(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, in inputExecuteAProjectActivityP5S4) (any, error) {
	t.Helper()
	rev1 := addressOf(t, h, subject, "revision-1", artifact.ConstructionOutput{Bytes: []byte(outputP5Rev1), MIMEType: mimeJSON})
	rev2 := addressOf(t, h, subject, in.ContentAddress, artifact.ConstructionOutput{Bytes: []byte(outputP5Rev2), MIMEType: mimeJSON})
	if rev1 == rev2 {
		t.Fatalf("arrange: revisions 1 and 2 share address %q", rev1)
	}
	return subject.RetrieveConstructionOutput(callContext(t, h, "retrieve"), rev2)
}

// stepExecuteAProjectActivityP6S2 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 2: RetrieveConstructionOutput
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"artifactaccess","op":"RetrieveConstructionOutput","call":"execute-a-project-activity/observe-validate/4.1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"RetrieveConstructionOutput","inputs":[{"name":"contentAddress","value":"sha-from-store-step"}],"expect":{"result":"{\"Bytes\":\"eyJhY3Rpdml0eSI6InN0cC1hY3QtUDYiLCJ0YXNrIjoiYnVpbGQiLCJyZXZpc2lvbiI6MSwiZmlsZXMiOlsiZW5naW5lLmdvIl19\",\"MIMEType\":\"application/json\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// The address is the one step 1's store returned for outputP6.
func stepExecuteAProjectActivityP6S2(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, in inputExecuteAProjectActivityP6S2) (any, error) {
	t.Helper()
	return retrieveStored(t, h, subject, in.ContentAddress, outputP6, mimeJSON)
}

// stepExecuteAProjectActivityP7S1 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 1: GetTestRunIndex
// Projected stimulus (JSON): {"seq":1,"node":"requires-human","input":{"kind":"call","from":"delivery-manager","to":"artifactaccess","op":"GetTestRunIndex","call":"execute-a-project-activity/requires-human/1","guard":"agents only"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"GetTestRunIndex","inputs":[{"name":"run","value":"{\"RunID\":\"stp-artifact-P7-run-0001\",\"Artifact\":\"scenario-results\"}"}],"expect":{"result":"{\"Expired\":false,\"Files\":[\"results.json\",\"scenarios/execute-a-project-activity-P7.log\"]}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// The judged task's run is recorded locally; the sendBack verdict is reached from its index.
func stepExecuteAProjectActivityP7S1(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, in inputExecuteAProjectActivityP7S1) (any, error) {
	t.Helper()
	return indexTestRun(t, h, subject, in.Run, "execute-a-project-activity-P7")
}

// stepExecuteAProjectActivityP7S3 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 3: RetrieveConstructionOutput
// Projected stimulus (JSON): {"seq":3,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"artifactaccess","op":"RetrieveConstructionOutput","call":"execute-a-project-activity/observe-validate/4.1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"RetrieveConstructionOutput","inputs":[{"name":"contentAddress","value":"sha-from-store-step-2"}],"expect":{"result":"{\"Bytes\":\"eyJhY3Rpdml0eSI6InN0cC1hY3QtUDciLCJ0YXNrIjoiYnVpbGQiLCJyZXZpc2lvbiI6MiwiZmlsZXMiOlsiY2xpZW50LmdvIl19\",\"MIMEType\":\"application/json\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// The reopened revision's output (step 2).
func stepExecuteAProjectActivityP7S3(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, in inputExecuteAProjectActivityP7S3) (any, error) {
	t.Helper()
	return retrieveStored(t, h, subject, in.ContentAddress, outputP7Rev2, mimeJSON)
}

// stepOperateADeliveredSystemP1S1 — Operate a Delivered System, step 1: RetrieveConstructionOutput
// Projected stimulus (JSON): {"seq":1,"node":"render-manifests-a","input":{"kind":"call","from":"operations-manager","to":"artifactaccess","op":"RetrieveConstructionOutput","call":"operate-a-delivered-system/render-manifests-a/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"RetrieveConstructionOutput","inputs":[{"name":"contentAddress","value":"sha-of-hook-arranged-output"}],"expect":{"result":"{\"Bytes\":\"YXBpVmVyc2lvbjogYXBwcy92MQpraW5kOiBEZXBsb3ltZW50Cm1ldGFkYXRhOgogIG5hbWU6IHN0cC1vcGVyYXRlZC1hcHAK\",\"MIMEType\":\"application/yaml\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Delivery stored the deployable manifest earlier; operations reads it back to render.
func stepOperateADeliveredSystemP1S1(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, in inputOperateADeliveredSystemP1S1) (any, error) {
	t.Helper()
	return retrieveStored(t, h, subject, in.ContentAddress, deployedManifest, mimeYAML)
}

// stepViewLiveDeploymentHealthP1S1 — View Live Deployment Health — [yes], step 1: RetrieveConstructionOutput
// Projected stimulus (JSON): {"seq":1,"node":"resolve-deployment","input":{"kind":"call","from":"operations-manager","to":"artifactaccess","op":"RetrieveConstructionOutput","call":"view-live-deployment-health/resolve-deployment/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"RetrieveConstructionOutput","inputs":[{"name":"contentAddress","value":"sha-of-hook-arranged-output"}],"expect":{"result":"{\"Bytes\":\"YXBpVmVyc2lvbjogdjEKa2luZDogU2VydmljZQptZXRhZGF0YToKICBuYW1lOiBzdHAtaGVhbHRoLWFwcAo=\",\"MIMEType\":\"application/yaml\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Something is deployed: its manifest was stored by delivery and resolves by address.
func stepViewLiveDeploymentHealthP1S1(t *testing.T, h *scenariohost.Host, subject artifact.ArtifactAccess, in inputViewLiveDeploymentHealthP1S1) (any, error) {
	t.Helper()
	return retrieveStored(t, h, subject, in.ContentAddress, healthManifest, mimeYAML)
}
