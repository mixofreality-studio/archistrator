// Hooks for activityExecutionAccess, designSessionAccess, gitActivityStatusAccess, projectStateAccess scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.

package projectstate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// newSubjectActivityExecutionAccess builds the component under test against the host's real downstream
// doubles. Constructed via the generated constructor in internal/resourceaccess/projectstate.
func newSubjectActivityExecutionAccess(t *testing.T, _ *scenariohost.Host) projectstate.ActivityExecutionAccess {
	t.Helper()
	return projectstate.NewGitLocalActivityExecutionAccess(newScenarioRepo(t))
}

// stepActivityExecutionAccessAddAUseCaseToAnInFlightProjectP1S1 — Add a Use Case to an In-Flight Project — [variation] · [no], step 1: StageTaskOutput
// Projected stimulus (JSON): {"seq":1,"node":"redraft-review","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"StageTaskOutput","call":"add-a-use-case-to-an-in-flight-project/redraft-review/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"StageTaskOutput","inputs":[{"name":"projectID","value":"stp-aea-auc-p1"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"taskID","value":"coreUseCasesDraft"},{"name":"branch","value":"activity/D-REQ"},{"name":"model","value":"{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\",\"classification\":\"nonCore\",\"variationOf\":\"view-the-project-state-log\",\"activity\":{\"nodes\":[{\"id\":\"start\",\"kind\":\"start\",\"label\":\"\"},{\"id\":\"export-audit-trail\",\"kind\":\"action\",\"label\":\"Download every state commit with its operation name as one archive\",\"roleName\":\"Architect User\",\"linkedActorId\":\"architect-user\"},{\"id\":\"end\",\"kind\":\"end\",\"label\":\"\"}],\"edges\":[{\"from\":\"start\",\"to\":\"export-audit-trail\",\"kind\":\"controlFlow\",\"guard\":\"\"},{\"from\":\"export-audit-trail\",\"to\":\"end\",\"kind\":\"controlFlow\",\"guard\":\"\"}]}},\"essenceRationale\":\"Amendment staged for review by the test plan.\",\"rejectionReason\":\"\"}]}}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-auc-p1:stage:1"}],"expect":{"result":"{\"activityId\":\"D-REQ\",\"taskId\":\"coreUseCasesDraft\",\"branch\":\"activity/D-REQ\"}","errorExpected":false},"probes":[{"for":"","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-auc-p1"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-auc-p1\",\"slots\":{\"4\":{\"model\":{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\"},\"essenceRationale\":\"Amendment staged for review by the test plan.\",\"rejectionReason\":\"\"}]}}}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[variation], replan-check=[no] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessAddAUseCaseToAnInFlightProjectP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessAddAUseCaseToAnInFlightProjectP1S1) (any, error) {
	t.Helper()
	seedMain(t, inFlightAmendment(in.ProjectID, in.ExpectedVersion))
	return subject.StageTaskOutput(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.TaskID, in.Branch, in.Model, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessAddAUseCaseToAnInFlightProjectP1S1N1 — Add a Use Case to an In-Flight Project — [variation] · [no], step 1, probe 1: designSessionAccess.ReadProjectOnBranch proves .
// Probe (JSON): {"for":"","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-auc-p1"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-auc-p1\",\"slots\":{\"4\":{\"model\":{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\"},\"essenceRationale\":\"Amendment staged for review by the test plan.\",\"rejectionReason\":\"\"}]}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessAddAUseCaseToAnInFlightProjectP1S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-auc-p1", "activity/D-REQ")
}

// stepActivityExecutionAccessAddAUseCaseToAnInFlightProjectP2S1 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 1: StageTaskOutput
// Projected stimulus (JSON): {"seq":1,"node":"redraft-review","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"StageTaskOutput","call":"add-a-use-case-to-an-in-flight-project/redraft-review/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"StageTaskOutput","inputs":[{"name":"projectID","value":"stp-aea-auc-p2"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"taskID","value":"coreUseCasesDraft"},{"name":"branch","value":"activity/D-REQ"},{"name":"model","value":"{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\",\"classification\":\"nonCore\",\"variationOf\":\"view-the-project-state-log\",\"activity\":{\"nodes\":[{\"id\":\"start\",\"kind\":\"start\",\"label\":\"\"},{\"id\":\"export-audit-trail\",\"kind\":\"action\",\"label\":\"Download every state commit with its operation name as one archive\",\"roleName\":\"Architect User\",\"linkedActorId\":\"architect-user\"},{\"id\":\"end\",\"kind\":\"end\",\"label\":\"\"}],\"edges\":[{\"from\":\"start\",\"to\":\"export-audit-trail\",\"kind\":\"controlFlow\",\"guard\":\"\"},{\"from\":\"export-audit-trail\",\"to\":\"end\",\"kind\":\"controlFlow\",\"guard\":\"\"}]}},\"essenceRationale\":\"Amendment staged for review by the test plan.\",\"rejectionReason\":\"\"}]}}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-auc-p2:stage:1"}],"expect":{"result":"{\"activityId\":\"D-REQ\",\"taskId\":\"coreUseCasesDraft\",\"branch\":\"activity/D-REQ\"}","errorExpected":false},"probes":[{"for":"","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-auc-p2"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-auc-p2\",\"slots\":{\"4\":{\"model\":{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\"},\"essenceRationale\":\"Amendment staged for review by the test plan.\",\"rejectionReason\":\"\"}]}}}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[variation], replan-check=[yes] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessAddAUseCaseToAnInFlightProjectP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessAddAUseCaseToAnInFlightProjectP2S1) (any, error) {
	t.Helper()
	seedMain(t, inFlightAmendment(in.ProjectID, in.ExpectedVersion))
	return subject.StageTaskOutput(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.TaskID, in.Branch, in.Model, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessAddAUseCaseToAnInFlightProjectP2S1N1 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 1, probe 1: designSessionAccess.ReadProjectOnBranch proves .
// Probe (JSON): {"for":"","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-auc-p2"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-auc-p2\",\"slots\":{\"4\":{\"model\":{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\"},\"essenceRationale\":\"Amendment staged for review by the test plan.\",\"rejectionReason\":\"\"}]}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessAddAUseCaseToAnInFlightProjectP2S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-auc-p2", "activity/D-REQ")
}

// stepActivityExecutionAccessAddAUseCaseToAnInFlightProjectP3S1 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 1: StageTaskOutput
// Projected stimulus (JSON): {"seq":1,"node":"redraft-review","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"StageTaskOutput","call":"add-a-use-case-to-an-in-flight-project/redraft-review/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"StageTaskOutput","inputs":[{"name":"projectID","value":"stp-aea-auc-p3"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"taskID","value":"coreUseCasesDraft"},{"name":"branch","value":"activity/D-REQ"},{"name":"model","value":"{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\",\"classification\":\"nonCore\",\"variationOf\":\"view-the-project-state-log\",\"activity\":{\"nodes\":[{\"id\":\"start\",\"kind\":\"start\",\"label\":\"\"},{\"id\":\"export-audit-trail\",\"kind\":\"action\",\"label\":\"Download every state commit with its operation name as one archive\",\"roleName\":\"Architect User\",\"linkedActorId\":\"architect-user\"},{\"id\":\"end\",\"kind\":\"end\",\"label\":\"\"}],\"edges\":[{\"from\":\"start\",\"to\":\"export-audit-trail\",\"kind\":\"controlFlow\",\"guard\":\"\"},{\"from\":\"export-audit-trail\",\"to\":\"end\",\"kind\":\"controlFlow\",\"guard\":\"\"}]}},\"essenceRationale\":\"Amendment staged for review by the test plan.\",\"rejectionReason\":\"\"}]}}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-auc-p3:stage:1"}],"expect":{"result":"{\"activityId\":\"D-REQ\",\"taskId\":\"coreUseCasesDraft\",\"branch\":\"activity/D-REQ\"}","errorExpected":false},"probes":[{"for":"","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-auc-p3"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-auc-p3\",\"slots\":{\"4\":{\"model\":{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\"},\"essenceRationale\":\"Amendment staged for review by the test plan.\",\"rejectionReason\":\"\"}]}}}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[core: rare], replan-check=[no] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessAddAUseCaseToAnInFlightProjectP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessAddAUseCaseToAnInFlightProjectP3S1) (any, error) {
	t.Helper()
	seedMain(t, inFlightAmendment(in.ProjectID, in.ExpectedVersion))
	return subject.StageTaskOutput(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.TaskID, in.Branch, in.Model, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessAddAUseCaseToAnInFlightProjectP3S1N1 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 1, probe 1: designSessionAccess.ReadProjectOnBranch proves .
// Probe (JSON): {"for":"","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-auc-p3"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-auc-p3\",\"slots\":{\"4\":{\"model\":{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\"},\"essenceRationale\":\"Amendment staged for review by the test plan.\",\"rejectionReason\":\"\"}]}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessAddAUseCaseToAnInFlightProjectP3S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-auc-p3", "activity/D-REQ")
}

// stepActivityExecutionAccessAddAUseCaseToAnInFlightProjectP4S1 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 1: StageTaskOutput
// Projected stimulus (JSON): {"seq":1,"node":"redraft-review","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"StageTaskOutput","call":"add-a-use-case-to-an-in-flight-project/redraft-review/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"StageTaskOutput","inputs":[{"name":"projectID","value":"stp-aea-auc-p4"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"taskID","value":"coreUseCasesDraft"},{"name":"branch","value":"activity/D-REQ"},{"name":"model","value":"{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\",\"classification\":\"nonCore\",\"variationOf\":\"view-the-project-state-log\",\"activity\":{\"nodes\":[{\"id\":\"start\",\"kind\":\"start\",\"label\":\"\"},{\"id\":\"export-audit-trail\",\"kind\":\"action\",\"label\":\"Download every state commit with its operation name as one archive\",\"roleName\":\"Architect User\",\"linkedActorId\":\"architect-user\"},{\"id\":\"end\",\"kind\":\"end\",\"label\":\"\"}],\"edges\":[{\"from\":\"start\",\"to\":\"export-audit-trail\",\"kind\":\"controlFlow\",\"guard\":\"\"},{\"from\":\"export-audit-trail\",\"to\":\"end\",\"kind\":\"controlFlow\",\"guard\":\"\"}]}},\"essenceRationale\":\"Amendment staged for review by the test plan.\",\"rejectionReason\":\"\"}]}}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-auc-p4:stage:1"}],"expect":{"result":"{\"activityId\":\"D-REQ\",\"taskId\":\"coreUseCasesDraft\",\"branch\":\"activity/D-REQ\"}","errorExpected":false},"probes":[{"for":"","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-auc-p4"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-auc-p4\",\"slots\":{\"4\":{\"model\":{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\"},\"essenceRationale\":\"Amendment staged for review by the test plan.\",\"rejectionReason\":\"\"}]}}}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[core: rare], replan-check=[yes] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessAddAUseCaseToAnInFlightProjectP4S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessAddAUseCaseToAnInFlightProjectP4S1) (any, error) {
	t.Helper()
	seedMain(t, inFlightAmendment(in.ProjectID, in.ExpectedVersion))
	return subject.StageTaskOutput(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.TaskID, in.Branch, in.Model, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessAddAUseCaseToAnInFlightProjectP4S1N1 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 1, probe 1: designSessionAccess.ReadProjectOnBranch proves .
// Probe (JSON): {"for":"","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-auc-p4"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-auc-p4\",\"slots\":{\"4\":{\"model\":{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\"},\"essenceRationale\":\"Amendment staged for review by the test plan.\",\"rejectionReason\":\"\"}]}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessAddAUseCaseToAnInFlightProjectP4S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-auc-p4", "activity/D-REQ")
}

// stepActivityExecutionAccessAskAClarifyingQuestionDuringReviewP1S1 — Ask a Clarifying Question During Review — [yes: session branch] · [yes], step 1: AppendReviewVerdict
// Projected stimulus (JSON): {"seq":1,"node":"append-session-branch","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"AppendReviewVerdict","call":"ask-a-clarifying-question-during-review/append-session-branch/0"},"expectedOutputs":null,"expectedStates":[{"node":"thread-open-on-session-branch","object":"ReviewThread","inState":"[question entries open on session branch]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"AppendReviewVerdict","inputs":[{"name":"projectID","value":"stp-aea-ask-p1"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"roundID","value":"D-REQ:missionReview:mission:1"},{"name":"verdict","value":"{\"reviewerRole\":\"operator\",\"actor\":\"operator\",\"verdict\":\"abstain\",\"summary\":\"Clarifying questions before a verdict.\",\"attemptId\":\"D-REQ:missionDraft:1\",\"at\":\"\"}"},{"name":"comments","value":"[{\"id\":\"q-1\",\"anchor\":\"$.mission\",\"anchorText\":\"operated on the customer's behalf\",\"text\":\"Does 'operated' include billing the customer?\",\"authorRole\":\"operator\",\"round\":1,\"status\":\"open\",\"response\":\"\",\"replies\":[],\"reopened\":false,\"type\":\"question\",\"addressee\":\"system-architect\"}]"},{"name":"replies","value":"[]"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-ask-p1:verdict:1"}],"expect":{"errorExpected":false},"probes":[{"for":"thread-open-on-session-branch","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-ask-p1"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"reviews\":[{\"roundId\":\"D-REQ:missionReview:mission:1\",\"verdicts\":[{\"verdict\":\"abstain\"}],\"thread\":[{\"id\":\"r1c1\",\"type\":\"question\",\"status\":\"open\"}]}]}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards artifact-state=[yes: session branch], answer-ran=[yes] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessAskAClarifyingQuestionDuringReviewP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessAskAClarifyingQuestionDuringReviewP1S1) (any, error) {
	t.Helper()
	seedMissionUnderReview(t, in.ProjectID, in.ExpectedVersion, "Deliver predictably engineered software, operated on the customer's behalf.")
	return subject.AppendReviewVerdict(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.RoundID, in.Verdict, in.Comments, in.Replies, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessAskAClarifyingQuestionDuringReviewP2S1 — Ask a Clarifying Question During Review — [yes: session branch] · [no: dispatch miss], step 1: AppendReviewVerdict
// Projected stimulus (JSON): {"seq":1,"node":"append-session-branch","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"AppendReviewVerdict","call":"ask-a-clarifying-question-during-review/append-session-branch/0"},"expectedOutputs":null,"expectedStates":[{"node":"thread-open-on-session-branch","object":"ReviewThread","inState":"[question entries open on session branch]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"AppendReviewVerdict","inputs":[{"name":"projectID","value":"stp-aea-ask-p2"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"roundID","value":"D-REQ:missionReview:mission:1"},{"name":"verdict","value":"{\"reviewerRole\":\"operator\",\"actor\":\"operator\",\"verdict\":\"abstain\",\"summary\":\"Clarifying questions before a verdict.\",\"attemptId\":\"D-REQ:missionDraft:1\",\"at\":\"\"}"},{"name":"comments","value":"[{\"id\":\"q-1\",\"anchor\":\"$.mission\",\"anchorText\":\"operated on the customer's behalf\",\"text\":\"Does 'operated' include billing the customer?\",\"authorRole\":\"operator\",\"round\":1,\"status\":\"open\",\"response\":\"\",\"replies\":[],\"reopened\":false,\"type\":\"question\",\"addressee\":\"system-architect\"}]"},{"name":"replies","value":"[]"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-ask-p2:verdict:1"}],"expect":{"errorExpected":false},"probes":[{"for":"thread-open-on-session-branch","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-ask-p2"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"reviews\":[{\"roundId\":\"D-REQ:missionReview:mission:1\",\"verdicts\":[{\"verdict\":\"abstain\"}],\"thread\":[{\"id\":\"r1c1\",\"type\":\"question\",\"status\":\"open\"}]}]}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards artifact-state=[yes: session branch], answer-ran=[no: dispatch miss] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessAskAClarifyingQuestionDuringReviewP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessAskAClarifyingQuestionDuringReviewP2S1) (any, error) {
	t.Helper()
	seedMissionUnderReview(t, in.ProjectID, in.ExpectedVersion, "Deliver predictably engineered software, operated on the customer's behalf.")
	return subject.AppendReviewVerdict(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.RoundID, in.Verdict, in.Comments, in.Replies, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessAskAClarifyingQuestionDuringReviewP3S1 — Ask a Clarifying Question During Review — [no: committed] · [yes], step 1: AppendReviewVerdict
// Projected stimulus (JSON): {"seq":1,"node":"record-answers","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"AppendReviewVerdict","call":"ask-a-clarifying-question-during-review/record-answers/0"},"expectedOutputs":null,"expectedStates":[{"node":"question-answered","object":"ReviewQuestion","inState":"[answered]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"AppendReviewVerdict","inputs":[{"name":"projectID","value":"stp-aea-ask-p3"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"roundID","value":"D-REQ:missionReview:mission:1"},{"name":"verdict","value":"{\"reviewerRole\":\"architect\",\"actor\":\"agent\",\"verdict\":\"abstain\",\"summary\":\"Answers to the reviewer's questions.\",\"attemptId\":\"D-REQ:missionDraft:1\",\"at\":\"\"}"},{"name":"comments","value":"[]"},{"name":"replies","value":"[{\"commentId\":\"r1c1\",\"authorRole\":\"architect\",\"text\":\"Yes: operating the system includes billing the customer for it.\",\"at\":\"\"}]"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-ask-p3:answer:1"}],"expect":{"errorExpected":false},"probes":[{"for":"question-answered","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-ask-p3"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"reviews\":[{\"roundId\":\"D-REQ:missionReview:mission:1\",\"thread\":[{\"id\":\"r1c1\",\"type\":\"question\",\"status\":\"answered\"}]}]}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards artifact-state=[no: committed], answer-ran=[yes] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessAskAClarifyingQuestionDuringReviewP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessAskAClarifyingQuestionDuringReviewP3S1) (any, error) {
	t.Helper()
	seedMain(t, questionAwaitingAnswer(in.ProjectID, in.ExpectedVersion))
	return subject.AppendReviewVerdict(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.RoundID, in.Verdict, in.Comments, in.Replies, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessExecuteAProjectActivityP1S1 — Execute a Project Activity — dispatch · yes, step 1: OpenActivity
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"OpenActivity","call":"execute-a-project-activity/activity-eligible/5"},"expectedOutputs":null,"expectedStates":[{"node":"activity-lifecycle-open","object":"ActivityExecution","inState":"[lifecycle open, revision 1]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"OpenActivity","inputs":[{"name":"projectID","value":"stp-aea-epa-p1"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"typ","value":"7"},{"name":"variant","value":"0"},{"name":"pin","value":"{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-epa-p1:open"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-lifecycle-open","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-epa-p1"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"activityID\":\"D-REQ\",\"type\":\"requirements\",\"lifecyclePin\":{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessExecuteAProjectActivityP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessExecuteAProjectActivityP1S1) (any, error) {
	t.Helper()
	seedMain(t, eligibleActivity(in.ProjectID, in.ExpectedVersion))
	return subject.OpenActivity(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.Typ, in.Variant, in.Pin, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessExecuteAProjectActivityP1S3 — Execute a Project Activity — dispatch · yes, step 3: StageTaskOutput
// Projected stimulus (JSON): {"seq":3,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"StageTaskOutput","call":"execute-a-project-activity/observe-validate/6"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"StageTaskOutput","inputs":[{"name":"projectID","value":"stp-aea-epa-p1"},{"name":"expectedVersion","value":"1"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"taskID","value":"missionDraft"},{"name":"branch","value":"activity/D-REQ"},{"name":"model","value":"{\"kind\":\"mission\",\"model\":{\"vision\":\"Predictably engineered software, delivered and operated on the customer's behalf.\",\"mission\":\"Deliver predictably engineered software.\",\"objectives\":[{\"number\":1,\"statement\":\"Customers receive a duration, cost and risk estimate before any code is written.\"}]}}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-epa-p1:stage:1"}],"expect":{"result":"{\"activityId\":\"D-REQ\",\"taskId\":\"missionDraft\",\"branch\":\"activity/D-REQ\"}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessExecuteAProjectActivityP1S3(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessExecuteAProjectActivityP1S3) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.StageTaskOutput(callContext(t, h, "3"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.TaskID, in.Branch, in.Model, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessExecuteAProjectActivityP1S4N1 — Execute a Project Activity — dispatch · yes, step 4, probe 1: designSessionAccess.ReadProjectOnBranch proves staged-artifact.
// Probe (JSON): {"for":"staged-artifact","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-epa-p1"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-epa-p1\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Deliver predictably engineered software.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessExecuteAProjectActivityP1S4N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-epa-p1", "activity/D-REQ")
}

// probeActivityExecutionAccessExecuteAProjectActivityP1S4N2 — Execute a Project Activity — dispatch · yes, step 4, probe 2: designSessionAccess.ReadProjectOnBranch proves observe-validate.
// Probe (JSON): {"for":"observe-validate","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-epa-p1"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-epa-p1\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Deliver predictably engineered software.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessExecuteAProjectActivityP1S4N2(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-epa-p1", "activity/D-REQ")
}

// stepActivityExecutionAccessExecuteAProjectActivityP2S1 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 1: OpenActivity
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"OpenActivity","call":"execute-a-project-activity/activity-eligible/5"},"expectedOutputs":null,"expectedStates":[{"node":"activity-lifecycle-open","object":"ActivityExecution","inState":"[lifecycle open, revision 1]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"OpenActivity","inputs":[{"name":"projectID","value":"stp-aea-epa-p2"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"typ","value":"7"},{"name":"variant","value":"0"},{"name":"pin","value":"{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-epa-p2:open"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-lifecycle-open","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-epa-p2"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"activityID\":\"D-REQ\",\"type\":\"requirements\",\"lifecyclePin\":{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessExecuteAProjectActivityP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessExecuteAProjectActivityP2S1) (any, error) {
	t.Helper()
	seedMain(t, eligibleActivity(in.ProjectID, in.ExpectedVersion))
	return subject.OpenActivity(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.Typ, in.Variant, in.Pin, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessExecuteAProjectActivityP2S3 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 3: StageTaskOutput
// Projected stimulus (JSON): {"seq":3,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"StageTaskOutput","call":"execute-a-project-activity/observe-validate/6"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"StageTaskOutput","inputs":[{"name":"projectID","value":"stp-aea-epa-p2"},{"name":"expectedVersion","value":"1"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"taskID","value":"missionDraft"},{"name":"branch","value":"activity/D-REQ"},{"name":"model","value":"{\"kind\":\"mission\",\"model\":{\"vision\":\"Predictably engineered software, delivered and operated on the customer's behalf.\",\"mission\":\"Draft that misses the exit criterion.\",\"objectives\":[{\"number\":1,\"statement\":\"Customers receive a duration, cost and risk estimate before any code is written.\"}]}}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-epa-p2:stage:1"}],"expect":{"result":"{\"activityId\":\"D-REQ\",\"taskId\":\"missionDraft\",\"branch\":\"activity/D-REQ\"}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessExecuteAProjectActivityP2S3(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessExecuteAProjectActivityP2S3) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.StageTaskOutput(callContext(t, h, "3"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.TaskID, in.Branch, in.Model, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessExecuteAProjectActivityP2S4N1 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 4, probe 1: designSessionAccess.ReadProjectOnBranch proves staged-artifact.
// Probe (JSON): {"for":"staged-artifact","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-epa-p2"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-epa-p2\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Draft that misses the exit criterion.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessExecuteAProjectActivityP2S4N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-epa-p2", "activity/D-REQ")
}

// probeActivityExecutionAccessExecuteAProjectActivityP2S4N2 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 4, probe 2: designSessionAccess.ReadProjectOnBranch proves observe-validate.
// Probe (JSON): {"for":"observe-validate","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-epa-p2"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-epa-p2\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Draft that misses the exit criterion.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessExecuteAProjectActivityP2S4N2(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-epa-p2", "activity/D-REQ")
}

// stepActivityExecutionAccessExecuteAProjectActivityP3S1 — Execute a Project Activity — review · agents only · approve, step 1: OpenActivity
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"OpenActivity","call":"execute-a-project-activity/activity-eligible/5"},"expectedOutputs":null,"expectedStates":[{"node":"activity-lifecycle-open","object":"ActivityExecution","inState":"[lifecycle open, revision 1]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"OpenActivity","inputs":[{"name":"projectID","value":"stp-aea-epa-p3"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"typ","value":"7"},{"name":"variant","value":"0"},{"name":"pin","value":"{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-epa-p3:open"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-lifecycle-open","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-epa-p3"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"activityID\":\"D-REQ\",\"type\":\"requirements\",\"lifecyclePin\":{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=agents only, verdict=approve hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessExecuteAProjectActivityP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessExecuteAProjectActivityP3S1) (any, error) {
	t.Helper()
	seedMain(t, eligibleActivity(in.ProjectID, in.ExpectedVersion))
	return subject.OpenActivity(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.Typ, in.Variant, in.Pin, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessExecuteAProjectActivityP4S1 — Execute a Project Activity — review · human required · approve, step 1: OpenActivity
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"OpenActivity","call":"execute-a-project-activity/activity-eligible/5"},"expectedOutputs":null,"expectedStates":[{"node":"activity-lifecycle-open","object":"ActivityExecution","inState":"[lifecycle open, revision 1]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"OpenActivity","inputs":[{"name":"projectID","value":"stp-aea-epa-p4"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"typ","value":"7"},{"name":"variant","value":"0"},{"name":"pin","value":"{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-epa-p4:open"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-lifecycle-open","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-epa-p4"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"activityID\":\"D-REQ\",\"type\":\"requirements\",\"lifecyclePin\":{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=human required, verdict=approve hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessExecuteAProjectActivityP4S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessExecuteAProjectActivityP4S1) (any, error) {
	t.Helper()
	seedMain(t, eligibleActivity(in.ProjectID, in.ExpectedVersion))
	return subject.OpenActivity(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.Typ, in.Variant, in.Pin, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessExecuteAProjectActivityP5S1 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 1: OpenActivity
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"OpenActivity","call":"execute-a-project-activity/activity-eligible/5"},"expectedOutputs":null,"expectedStates":[{"node":"activity-lifecycle-open","object":"ActivityExecution","inState":"[lifecycle open, revision 1]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"OpenActivity","inputs":[{"name":"projectID","value":"stp-aea-epa-p5"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"typ","value":"7"},{"name":"variant","value":"0"},{"name":"pin","value":"{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-epa-p5:open"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-lifecycle-open","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-epa-p5"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"activityID\":\"D-REQ\",\"type\":\"requirements\",\"lifecyclePin\":{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=yes: retry, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessExecuteAProjectActivityP5S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessExecuteAProjectActivityP5S1) (any, error) {
	t.Helper()
	seedMain(t, eligibleActivity(in.ProjectID, in.ExpectedVersion))
	return subject.OpenActivity(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.Typ, in.Variant, in.Pin, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessExecuteAProjectActivityP5S3 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 3: StageTaskOutput
// Projected stimulus (JSON): {"seq":3,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"StageTaskOutput","call":"execute-a-project-activity/observe-validate/6"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"StageTaskOutput","inputs":[{"name":"projectID","value":"stp-aea-epa-p5"},{"name":"expectedVersion","value":"1"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"taskID","value":"missionDraft"},{"name":"branch","value":"activity/D-REQ"},{"name":"model","value":"{\"kind\":\"mission\",\"model\":{\"vision\":\"Predictably engineered software, delivered and operated on the customer's behalf.\",\"mission\":\"First draft that misses the exit criterion.\",\"objectives\":[{\"number\":1,\"statement\":\"Customers receive a duration, cost and risk estimate before any code is written.\"}]}}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-epa-p5:stage:1"}],"expect":{"result":"{\"activityId\":\"D-REQ\",\"taskId\":\"missionDraft\",\"branch\":\"activity/D-REQ\"}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=yes: retry, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessExecuteAProjectActivityP5S3(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessExecuteAProjectActivityP5S3) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.StageTaskOutput(callContext(t, h, "3"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.TaskID, in.Branch, in.Model, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessExecuteAProjectActivityP5S4N1 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 4, probe 1: designSessionAccess.ReadProjectOnBranch proves staged-artifact.
// Probe (JSON): {"for":"staged-artifact","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-epa-p5"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-epa-p5\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"First draft that misses the exit criterion.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessExecuteAProjectActivityP5S4N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-epa-p5", "activity/D-REQ")
}

// probeActivityExecutionAccessExecuteAProjectActivityP5S4N2 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 4, probe 2: designSessionAccess.ReadProjectOnBranch proves observe-validate.
// Probe (JSON): {"for":"observe-validate","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-epa-p5"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-epa-p5\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"First draft that misses the exit criterion.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessExecuteAProjectActivityP5S4N2(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-epa-p5", "activity/D-REQ")
}

// probeActivityExecutionAccessExecuteAProjectActivityP5S6N1 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 6, probe 1: designSessionAccess.ReadProjectOnBranch proves staged-artifact.
// Probe (JSON): {"for":"staged-artifact","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-epa-p5"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-epa-p5\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Deliver predictably engineered software, retried.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessExecuteAProjectActivityP5S6N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-epa-p5", "activity/D-REQ")
}

// probeActivityExecutionAccessExecuteAProjectActivityP5S6N2 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 6, probe 2: designSessionAccess.ReadProjectOnBranch proves observe-validate.
// Probe (JSON): {"for":"observe-validate","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-epa-p5"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-epa-p5\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Deliver predictably engineered software, retried.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessExecuteAProjectActivityP5S6N2(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-epa-p5", "activity/D-REQ")
}

// stepActivityExecutionAccessExecuteAProjectActivityP6S1 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 1: OpenActivity
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"OpenActivity","call":"execute-a-project-activity/activity-eligible/5"},"expectedOutputs":null,"expectedStates":[{"node":"activity-lifecycle-open","object":"ActivityExecution","inState":"[lifecycle open, revision 1]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"OpenActivity","inputs":[{"name":"projectID","value":"stp-aea-epa-p6"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"typ","value":"7"},{"name":"variant","value":"0"},{"name":"pin","value":"{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-epa-p6:open"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-lifecycle-open","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-epa-p6"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"activityID\":\"D-REQ\",\"type\":\"requirements\",\"lifecyclePin\":{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=no, operator-intervened=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessExecuteAProjectActivityP6S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessExecuteAProjectActivityP6S1) (any, error) {
	t.Helper()
	seedMain(t, eligibleActivity(in.ProjectID, in.ExpectedVersion))
	return subject.OpenActivity(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.Typ, in.Variant, in.Pin, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessExecuteAProjectActivityP6S3 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 3: StageTaskOutput
// Projected stimulus (JSON): {"seq":3,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"StageTaskOutput","call":"execute-a-project-activity/observe-validate/6"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"StageTaskOutput","inputs":[{"name":"projectID","value":"stp-aea-epa-p6"},{"name":"expectedVersion","value":"1"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"taskID","value":"missionDraft"},{"name":"branch","value":"activity/D-REQ"},{"name":"model","value":"{\"kind\":\"mission\",\"model\":{\"vision\":\"Predictably engineered software, delivered and operated on the customer's behalf.\",\"mission\":\"Draft that misses the exit criterion.\",\"objectives\":[{\"number\":1,\"statement\":\"Customers receive a duration, cost and risk estimate before any code is written.\"}]}}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-epa-p6:stage:1"}],"expect":{"result":"{\"activityId\":\"D-REQ\",\"taskId\":\"missionDraft\",\"branch\":\"activity/D-REQ\"}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=no, operator-intervened=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessExecuteAProjectActivityP6S3(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessExecuteAProjectActivityP6S3) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.StageTaskOutput(callContext(t, h, "3"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.TaskID, in.Branch, in.Model, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessExecuteAProjectActivityP6S4N1 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 4, probe 1: designSessionAccess.ReadProjectOnBranch proves staged-artifact.
// Probe (JSON): {"for":"staged-artifact","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-epa-p6"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-epa-p6\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Draft that misses the exit criterion.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessExecuteAProjectActivityP6S4N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-epa-p6", "activity/D-REQ")
}

// probeActivityExecutionAccessExecuteAProjectActivityP6S4N2 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 4, probe 2: designSessionAccess.ReadProjectOnBranch proves observe-validate.
// Probe (JSON): {"for":"observe-validate","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-epa-p6"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-epa-p6\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Draft that misses the exit criterion.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessExecuteAProjectActivityP6S4N2(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-epa-p6", "activity/D-REQ")
}

// stepActivityExecutionAccessExecuteAProjectActivityP7S1 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 1: OpenActivity
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"OpenActivity","call":"execute-a-project-activity/activity-eligible/5"},"expectedOutputs":null,"expectedStates":[{"node":"activity-lifecycle-open","object":"ActivityExecution","inState":"[lifecycle open, revision 1]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"OpenActivity","inputs":[{"name":"projectID","value":"stp-aea-epa-p7"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"typ","value":"7"},{"name":"variant","value":"0"},{"name":"pin","value":"{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-epa-p7:open"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-lifecycle-open","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-epa-p7"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"activityID\":\"D-REQ\",\"type\":\"requirements\",\"lifecyclePin\":{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=agents only, verdict=sendBack: reopens the judged task as revision n+1, task-kind=dispatch, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessExecuteAProjectActivityP7S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessExecuteAProjectActivityP7S1) (any, error) {
	t.Helper()
	seedMain(t, eligibleActivity(in.ProjectID, in.ExpectedVersion))
	return subject.OpenActivity(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.Typ, in.Variant, in.Pin, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessExecuteAProjectActivityP7S7 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 7: StageTaskOutput
// Projected stimulus (JSON): {"seq":7,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"StageTaskOutput","call":"execute-a-project-activity/observe-validate/6"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":7,"operation":"StageTaskOutput","inputs":[{"name":"projectID","value":"stp-aea-epa-p7"},{"name":"expectedVersion","value":"4"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"taskID","value":"missionDraft"},{"name":"branch","value":"activity/D-REQ"},{"name":"model","value":"{\"kind\":\"mission\",\"model\":{\"vision\":\"Predictably engineered software, delivered and operated on the customer's behalf.\",\"mission\":\"Deliver predictably engineered software (revision 2).\",\"objectives\":[{\"number\":1,\"statement\":\"Customers receive a duration, cost and risk estimate before any code is written.\"}]}}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-epa-p7:stage:2"}],"expect":{"result":"{\"activityId\":\"D-REQ\",\"taskId\":\"missionDraft\",\"branch\":\"activity/D-REQ\"}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=agents only, verdict=sendBack: reopens the judged task as revision n+1, task-kind=dispatch, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessExecuteAProjectActivityP7S7(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessExecuteAProjectActivityP7S7) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.StageTaskOutput(callContext(t, h, "7"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.TaskID, in.Branch, in.Model, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessExecuteAProjectActivityP7S8N1 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 8, probe 1: designSessionAccess.ReadProjectOnBranch proves staged-artifact.
// Probe (JSON): {"for":"staged-artifact","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-epa-p7"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-epa-p7\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Deliver predictably engineered software (revision 2).\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessExecuteAProjectActivityP7S8N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-epa-p7", "activity/D-REQ")
}

// probeActivityExecutionAccessExecuteAProjectActivityP7S8N2 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 8, probe 2: designSessionAccess.ReadProjectOnBranch proves observe-validate.
// Probe (JSON): {"for":"observe-validate","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-epa-p7"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-epa-p7\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Deliver predictably engineered software (revision 2).\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessExecuteAProjectActivityP7S8N2(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-epa-p7", "activity/D-REQ")
}

// stepActivityExecutionAccessReplanUnderScopeChangeP2S1 — Replan Under Scope Change — [no: scope change] · [accept], step 1: StageTaskOutput
// Projected stimulus (JSON): {"seq":1,"node":"present","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"StageTaskOutput","call":"replan-under-scope-change/present/4"},"expectedOutputs":[{"node":"signal-sdp-revision-ready","kind":"sendSignal","to":"architect-user","label":"Deliver the SDP revision with deltas for the management decision"}],"expectedStates":[{"node":"sdp-revision-staged","object":"SDP review revision","inState":"[staged for decision]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"StageTaskOutput","inputs":[{"name":"projectID","value":"stp-aea-rp-p2"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-PD"},{"name":"taskID","value":"sdpReview"},{"name":"branch","value":"activity/D-PD"},{"name":"model","value":"{\"kind\":\"sdpReview\",\"model\":{\"options\":[{\"optionId\":\"opt-normal\",\"solutionKind\":\"normalSolution\",\"durationDays\":120,\"buildCost\":{\"minorUnits\":1200000,\"currency\":\"USD\"},\"compositeRisk\":0.62,\"projectedMonthlyCost\":{\"minorUnits\":80000,\"currency\":\"USD\"},\"expectedPerCycleNet\":{\"minorUnits\":12000,\"currency\":\"USD\"}}],\"recommendation\":\"opt-normal\",\"rationale\":\"Revised for the added use case: one more service activity.\"}}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-rp-p2:stage:1"}],"expect":{"result":"{\"activityId\":\"D-PD\",\"taskId\":\"sdpReview\",\"branch\":\"activity/D-PD\"}","errorExpected":false},"probes":[{"for":"sdp-revision-staged","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-rp-p2"},{"name":"branch","value":"activity/D-PD"}],"expect":{"result":"{\"id\":\"stp-aea-rp-p2\",\"slots\":{\"16\":{\"model\":{\"kind\":\"sdpReview\",\"model\":{\"recommendation\":\"opt-normal\",\"rationale\":\"Revised for the added use case: one more service activity.\"}}}}}","errorExpected":false}}],"unobservable":["signal-sdp-revision-ready"],"hook":true}
// Arranges the state in which guards classify=[no: scope change], mgmt=[accept] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessReplanUnderScopeChangeP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessReplanUnderScopeChangeP2S1) (any, error) {
	t.Helper()
	seedMain(t, sdpRevisionPresented(in.ProjectID, in.ExpectedVersion))
	return subject.StageTaskOutput(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.TaskID, in.Branch, in.Model, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessReplanUnderScopeChangeP2S1N1 — Replan Under Scope Change — [no: scope change] · [accept], step 1, probe 1: designSessionAccess.ReadProjectOnBranch proves sdp-revision-staged.
// Probe (JSON): {"for":"sdp-revision-staged","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-rp-p2"},{"name":"branch","value":"activity/D-PD"}],"expect":{"result":"{\"id\":\"stp-aea-rp-p2\",\"slots\":{\"16\":{\"model\":{\"kind\":\"sdpReview\",\"model\":{\"recommendation\":\"opt-normal\",\"rationale\":\"Revised for the added use case: one more service activity.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessReplanUnderScopeChangeP2S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-rp-p2", "activity/D-PD")
}

// stepActivityExecutionAccessReplanUnderScopeChangeP2S2 — Replan Under Scope Change — [no: scope change] · [accept], step 2: DecideReviewRound
// Projected stimulus (JSON): {"seq":2,"node":"adopt","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"DecideReviewRound","call":"replan-under-scope-change/adopt/0.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DecideReviewRound","inputs":[{"name":"projectID","value":"stp-aea-rp-p2"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-PD"},{"name":"roundID","value":"D-PD:sdpReview:sdpReview:2"},{"name":"outcome","value":"passed"},{"name":"decidedBy","value":"architect-user"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-rp-p2:decide:1"}],"expect":{"errorExpected":false},"probes":[{"for":"","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-rp-p2"},{"name":"activityID","value":"D-PD"}],"expect":{"result":"{\"activityID\":\"D-PD\",\"reviews\":[{\"roundId\":\"D-PD:sdpReview:sdpReview:2\",\"taskId\":\"sdpReview\",\"outcome\":\"passed\",\"decidedBy\":\"architect-user\"}]}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards classify=[no: scope change], mgmt=[accept] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessReplanUnderScopeChangeP2S2(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessReplanUnderScopeChangeP2S2) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.DecideReviewRound(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.RoundID, in.Outcome, in.DecidedBy, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessReplanUnderScopeChangeP3S1 — Replan Under Scope Change — [no: scope change] · [reject], step 1: StageTaskOutput
// Projected stimulus (JSON): {"seq":1,"node":"present","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"StageTaskOutput","call":"replan-under-scope-change/present/4"},"expectedOutputs":[{"node":"signal-sdp-revision-ready","kind":"sendSignal","to":"architect-user","label":"Deliver the SDP revision with deltas for the management decision"}],"expectedStates":[{"node":"sdp-revision-staged","object":"SDP review revision","inState":"[staged for decision]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"StageTaskOutput","inputs":[{"name":"projectID","value":"stp-aea-rp-p3"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-PD"},{"name":"taskID","value":"sdpReview"},{"name":"branch","value":"activity/D-PD"},{"name":"model","value":"{\"kind\":\"sdpReview\",\"model\":{\"options\":[{\"optionId\":\"opt-normal\",\"solutionKind\":\"normalSolution\",\"durationDays\":120,\"buildCost\":{\"minorUnits\":1200000,\"currency\":\"USD\"},\"compositeRisk\":0.62,\"projectedMonthlyCost\":{\"minorUnits\":80000,\"currency\":\"USD\"},\"expectedPerCycleNet\":{\"minorUnits\":12000,\"currency\":\"USD\"}}],\"recommendation\":\"opt-normal\",\"rationale\":\"Revised for the added use case: one more service activity.\"}}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-rp-p3:stage:1"}],"expect":{"result":"{\"activityId\":\"D-PD\",\"taskId\":\"sdpReview\",\"branch\":\"activity/D-PD\"}","errorExpected":false},"probes":[{"for":"sdp-revision-staged","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-rp-p3"},{"name":"branch","value":"activity/D-PD"}],"expect":{"result":"{\"id\":\"stp-aea-rp-p3\",\"slots\":{\"16\":{\"model\":{\"kind\":\"sdpReview\",\"model\":{\"recommendation\":\"opt-normal\",\"rationale\":\"Revised for the added use case: one more service activity.\"}}}}}","errorExpected":false}}],"unobservable":["signal-sdp-revision-ready"],"hook":true}
// Arranges the state in which guards classify=[no: scope change], mgmt=[reject] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessReplanUnderScopeChangeP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessReplanUnderScopeChangeP3S1) (any, error) {
	t.Helper()
	seedMain(t, sdpRevisionPresented(in.ProjectID, in.ExpectedVersion))
	return subject.StageTaskOutput(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.TaskID, in.Branch, in.Model, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessReplanUnderScopeChangeP3S1N1 — Replan Under Scope Change — [no: scope change] · [reject], step 1, probe 1: designSessionAccess.ReadProjectOnBranch proves sdp-revision-staged.
// Probe (JSON): {"for":"sdp-revision-staged","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-rp-p3"},{"name":"branch","value":"activity/D-PD"}],"expect":{"result":"{\"id\":\"stp-aea-rp-p3\",\"slots\":{\"16\":{\"model\":{\"kind\":\"sdpReview\",\"model\":{\"recommendation\":\"opt-normal\",\"rationale\":\"Revised for the added use case: one more service activity.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessReplanUnderScopeChangeP3S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-rp-p3", "activity/D-PD")
}

// stepActivityExecutionAccessReplanUnderScopeChangeP3S2 — Replan Under Scope Change — [no: scope change] · [reject], step 2: DecideReviewRound
// Projected stimulus (JSON): {"seq":2,"node":"record-reject","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"DecideReviewRound","call":"replan-under-scope-change/record-reject/0"},"expectedOutputs":null,"expectedStates":[{"node":"sdp-revision-rejected","object":"SDP review revision","inState":"[rejected; current commitment stands]"}],"hints":null}
// Binding (JSON): {"seq":2,"operation":"DecideReviewRound","inputs":[{"name":"projectID","value":"stp-aea-rp-p3"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-PD"},{"name":"roundID","value":"D-PD:sdpReview:sdpReview:2"},{"name":"outcome","value":"sentBack"},{"name":"decidedBy","value":"architect-user"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-rp-p3:decide:1"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-revision-rejected","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-rp-p3"},{"name":"activityID","value":"D-PD"}],"expect":{"result":"{\"activityID\":\"D-PD\",\"reviews\":[{\"roundId\":\"D-PD:sdpReview:sdpReview:2\",\"taskId\":\"sdpReview\",\"outcome\":\"sentBack\",\"decidedBy\":\"architect-user\"}]}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards classify=[no: scope change], mgmt=[reject] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessReplanUnderScopeChangeP3S2(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessReplanUnderScopeChangeP3S2) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.DecideReviewRound(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.RoundID, in.Outcome, in.DecidedBy, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessRequeueAFailedConstructionActivityP1S1 — Requeue a Failed Construction Activity, step 1: RecordOperatorNote
// Projected stimulus (JSON): {"seq":1,"node":"record-reopen","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"RecordOperatorNote","call":"requeue-a-failed-construction-activity/record-reopen/1"},"expectedOutputs":null,"expectedStates":[{"node":"activity-reopened-state","object":"Construction Activity","inState":"[reopened]"},{"node":"failed-attempt-kept-state","object":"Failed Attempt","inState":"[on record]"},{"node":"requeue-note-pending-state","object":"Operator Note","inState":"[pending delivery]"}],"hints":["The requeue note is recorded with kind=requeue (wire name \"requeue\", not \"retry\") under gate \"reopen\". Its noteId is keyed to the terminal exit stamp it re-opens, so re-submitting the same re-queue is idempotent. The precondition accepts terminal rows only (failed, done, completedNotLanded); a live row is refused with FailedPrecondition. The failed attempt's outcome is never rewritten: the attempts[] count and the last outcome stay unchanged after the re-queue."]}
// Binding (JSON): {"seq":1,"operation":"RecordOperatorNote","inputs":[{"name":"projectID","value":"stp-aea-rq-p1"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"note","value":"{\"noteId\":\"requeue:D-REQ:2026-10-01T12:00:00Z\",\"kind\":6,\"gate\":\"reopen\",\"text\":\"Re-queue after the upstream fix landed.\",\"comments\":null}"},{"name":"deliveredToAttemptID","value":""},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-rq-p1:note:1"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-reopened-state","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-rq-p1"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"activityID\":\"D-REQ\",\"lifecyclePin\":{\"typeKey\":\"requirements\",\"assetsVersion\":\"v0.10.1\"},\"operatorNotes\":[{\"noteId\":\"requeue:D-REQ:2026-10-01T12:00:00Z\",\"kind\":6,\"gate\":\"reopen\",\"text\":\"Re-queue after the upstream fix landed.\"}]}","errorExpected":false}},{"for":"failed-attempt-kept-state","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-rq-p1"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"attempts\":[{\"attemptId\":\"D-REQ:missionDraft:1\",\"attempt\":1,\"outcome\":\"failed\",\"evidence\":{\"kind\":\"episode\",\"ref\":\"ep-stp-aea-rq-p1-missionDraft-1\"}}]}","errorExpected":false}},{"for":"requeue-note-pending-state","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-rq-p1"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"operatorNotes\":[{\"noteId\":\"requeue:D-REQ:2026-10-01T12:00:00Z\",\"kind\":6,\"gate\":\"reopen\",\"text\":\"Re-queue after the upstream fix landed.\"}]}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards (none) hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessRequeueAFailedConstructionActivityP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessRequeueAFailedConstructionActivityP1S1) (any, error) {
	t.Helper()
	seedMain(t, failedActivity(in.ProjectID, in.ExpectedVersion))
	return subject.RecordOperatorNote(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.Note, in.DeliveredToAttemptID, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessRequeueAFailedConstructionActivityP1S2 — Requeue a Failed Construction Activity, step 2: RecordOperatorNote
// Projected stimulus (JSON): {"seq":2,"node":"resume-activity","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"RecordOperatorNote","call":"requeue-a-failed-construction-activity/resume-activity/1"},"expectedOutputs":null,"expectedStates":[{"node":"activity-resumed-state","object":"Construction Activity","inState":"[in progress at first incomplete phase]"},{"node":"requeue-note-delivered-state","object":"Operator Note","inState":"[delivered]"}],"hints":["Resume point = the first lifecycle phase without a passed attempt, not phase 0. Phases that already passed keep their attempts and are not re-dispatched. 'Delivered' means the note's deliveredToAttemptId equals the new attempt's attemptId."]}
// Binding (JSON): {"seq":2,"operation":"RecordOperatorNote","inputs":[{"name":"projectID","value":"stp-aea-rq-p1"},{"name":"expectedVersion","value":"3"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"note","value":"{\"noteId\":\"requeue:D-REQ:2026-10-01T12:00:00Z\",\"kind\":6,\"gate\":\"reopen\",\"text\":\"Re-queue after the upstream fix landed.\",\"comments\":null}"},{"name":"deliveredToAttemptID","value":"D-REQ:missionDraft:2"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-rq-p1:note:2"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-resumed-state","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-rq-p1"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"attempts\":[{\"attemptId\":\"D-REQ:missionDraft:1\",\"attempt\":1,\"outcome\":\"failed\",\"evidence\":{\"kind\":\"episode\",\"ref\":\"ep-stp-aea-rq-p1-missionDraft-1\"}},{\"attemptId\":\"D-REQ:missionDraft:2\",\"attempt\":2}]}","errorExpected":false}},{"for":"requeue-note-delivered-state","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-rq-p1"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"operatorNotes\":[{\"noteId\":\"requeue:D-REQ:2026-10-01T12:00:00Z\",\"kind\":6,\"gate\":\"reopen\",\"text\":\"Re-queue after the upstream fix landed.\",\"deliveredToAttemptId\":\"D-REQ:missionDraft:2\"}]}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards (none) hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessRequeueAFailedConstructionActivityP1S2(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessRequeueAFailedConstructionActivityP1S2) (any, error) {
	t.Helper()
	resumeRequeuedWalk(t, h, subject, in.ProjectID, in.ActivityID, in.DeliveredToAttemptID)
	return subject.RecordOperatorNote(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.Note, in.DeliveredToAttemptID, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessResumePausedConstructionP1S1 — Resume Paused Construction — [yes], step 1: OpenActivity
// Projected stimulus (JSON): {"seq":1,"node":"continue","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"OpenActivity","call":"resume-paused-construction/continue/0"},"expectedOutputs":null,"expectedStates":[{"node":"activity-started","object":"Project activity","inState":"[started]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"OpenActivity","inputs":[{"name":"projectID","value":"stp-aea-rs-p1"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-ARCH"},{"name":"typ","value":"8"},{"name":"variant","value":"0"},{"name":"pin","value":"{\"typeKey\":\"architecture\",\"assetsVersion\":\"v0.10.1\"}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-rs-p1:open"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-started","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-rs-p1"},{"name":"activityID","value":"D-ARCH"}],"expect":{"result":"{\"activityID\":\"D-ARCH\",\"type\":\"architecture\",\"lifecyclePin\":{\"typeKey\":\"architecture\",\"assetsVersion\":\"v0.10.1\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards pause-settled=[yes] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessResumePausedConstructionP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessResumePausedConstructionP1S1) (any, error) {
	t.Helper()
	seedMain(t, resumedAfterPause(in.ProjectID, in.ExpectedVersion))
	return subject.OpenActivity(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.Typ, in.Variant, in.Pin, in.Cred, in.IdempotencyKey)
}

// stepActivityExecutionAccessSendBackChangeRequestsForARedraftP1S1 — Send Back Change Requests for a Redraft — [yes], step 1: AppendReviewVerdict
// Projected stimulus (JSON): {"seq":1,"node":"retain-thread","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"AppendReviewVerdict","call":"send-back-change-requests-for-a-redraft/retain-thread/0"},"expectedOutputs":null,"expectedStates":[{"node":"obj-thread-open","object":"Review thread","inState":"[change requests open, retained on session branch]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"AppendReviewVerdict","inputs":[{"name":"projectID","value":"stp-aea-sb-p1"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"roundID","value":"D-REQ:missionReview:mission:1"},{"name":"verdict","value":"{\"reviewerRole\":\"architect\",\"actor\":\"architect-user\",\"verdict\":\"sendBack\",\"summary\":\"Change requests attached.\",\"attemptId\":\"D-REQ:missionDraft:1\",\"at\":\"\"}"},{"name":"comments","value":"[{\"id\":\"cr-1\",\"anchor\":\"$.mission\",\"anchorText\":\"operated on the customer's behalf\",\"text\":\"Name the customer outcome the mission commits to.\",\"authorRole\":\"architect\",\"round\":1,\"status\":\"open\",\"response\":\"\",\"replies\":[],\"reopened\":false,\"type\":\"changeRequest\",\"addressee\":\"system-architect\"}]"},{"name":"replies","value":"[]"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-sb-p1:verdict:1"}],"expect":{"errorExpected":false},"probes":[{"for":"obj-thread-open","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-sb-p1"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"reviews\":[{\"roundId\":\"D-REQ:missionReview:mission:1\",\"outcome\":\"pending\",\"verdicts\":[{\"verdict\":\"sendBack\"}],\"thread\":[{\"id\":\"r1c1\",\"type\":\"changeRequest\",\"status\":\"open\"}]}]}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards resolved=[yes] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessSendBackChangeRequestsForARedraftP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessSendBackChangeRequestsForARedraftP1S1) (any, error) {
	t.Helper()
	seedMissionUnderReview(t, in.ProjectID, in.ExpectedVersion, "Deliver predictably engineered software.")
	return subject.AppendReviewVerdict(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.RoundID, in.Verdict, in.Comments, in.Replies, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessSendBackChangeRequestsForARedraftP1S2N1 — Send Back Change Requests for a Redraft — [yes], step 2, probe 1: designSessionAccess.ReadProjectOnBranch proves obj-draft-restaged.
// Probe (JSON): {"for":"obj-draft-restaged","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-sb-p1"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-sb-p1\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Deliver predictably engineered software so customers know cost before code.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessSendBackChangeRequestsForARedraftP1S2N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-sb-p1", "activity/D-REQ")
}

// stepActivityExecutionAccessSendBackChangeRequestsForARedraftP2S1 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 1: AppendReviewVerdict
// Projected stimulus (JSON): {"seq":1,"node":"retain-thread","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"AppendReviewVerdict","call":"send-back-change-requests-for-a-redraft/retain-thread/0"},"expectedOutputs":null,"expectedStates":[{"node":"obj-thread-open","object":"Review thread","inState":"[change requests open, retained on session branch]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"AppendReviewVerdict","inputs":[{"name":"projectID","value":"stp-aea-sb-p2"},{"name":"expectedVersion","value":"0"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"D-REQ"},{"name":"roundID","value":"D-REQ:missionReview:mission:1"},{"name":"verdict","value":"{\"reviewerRole\":\"architect\",\"actor\":\"architect-user\",\"verdict\":\"sendBack\",\"summary\":\"Change requests attached.\",\"attemptId\":\"D-REQ:missionDraft:1\",\"at\":\"\"}"},{"name":"comments","value":"[{\"id\":\"cr-1\",\"anchor\":\"$.mission\",\"anchorText\":\"operated on the customer's behalf\",\"text\":\"Name the customer outcome the mission commits to.\",\"authorRole\":\"architect\",\"round\":1,\"status\":\"open\",\"response\":\"\",\"replies\":[],\"reopened\":false,\"type\":\"changeRequest\",\"addressee\":\"system-architect\"}]"},{"name":"replies","value":"[]"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-sb-p2:verdict:1"}],"expect":{"errorExpected":false},"probes":[{"for":"obj-thread-open","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-sb-p2"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"reviews\":[{\"roundId\":\"D-REQ:missionReview:mission:1\",\"outcome\":\"pending\",\"verdicts\":[{\"verdict\":\"sendBack\"}],\"thread\":[{\"id\":\"r1c1\",\"type\":\"changeRequest\",\"status\":\"open\"}]}]}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards resolved=[no: iterate], resolved=[yes] hold, then calls the operation exactly as its caller does and returns its result.
func stepActivityExecutionAccessSendBackChangeRequestsForARedraftP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessSendBackChangeRequestsForARedraftP2S1) (any, error) {
	t.Helper()
	seedMissionUnderReview(t, in.ProjectID, in.ExpectedVersion, "Deliver predictably engineered software.")
	return subject.AppendReviewVerdict(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.RoundID, in.Verdict, in.Comments, in.Replies, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessSendBackChangeRequestsForARedraftP2S2N1 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 2, probe 1: designSessionAccess.ReadProjectOnBranch proves obj-draft-restaged.
// Probe (JSON): {"for":"obj-draft-restaged","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-sb-p2"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-sb-p2\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Redraft one: customers know cost before code.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessSendBackChangeRequestsForARedraftP2S2N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-sb-p2", "activity/D-REQ")
}

// probeActivityExecutionAccessSendBackChangeRequestsForARedraftP2S4N1 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 4, probe 1: designSessionAccess.ReadProjectOnBranch proves obj-draft-restaged.
// Probe (JSON): {"for":"obj-draft-restaged","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aea-sb-p2"},{"name":"branch","value":"activity/D-REQ"}],"expect":{"result":"{\"id\":\"stp-aea-sb-p2\",\"slots\":{\"0\":{\"model\":{\"kind\":\"mission\",\"model\":{\"mission\":\"Redraft two: customers know duration, cost and risk before code.\"}}}}}","errorExpected":false}}
// Reads the probe's branch through designSessionAccess.ReadProjectOnBranch over the scenario's own repo.
func probeActivityExecutionAccessSendBackChangeRequestsForARedraftP2S4N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readOnBranch(t, h, "stp-aea-sb-p2", "activity/D-REQ")
}

// newSubjectDesignSessionAccess builds the component under test.
// Constructed via the generated constructor in internal/resourceaccess/projectstate, over the scenario's own repo.
func newSubjectDesignSessionAccess(t *testing.T, _ *scenariohost.Host) projectstate.DesignSessionAccess {
	t.Helper()
	return projectstate.NewGitLocalDesignSessionAccess(newScenarioRepo(t))
}

// stepDesignSessionAccessAddAUseCaseToAnInFlightProjectP1S1 — Add a Use Case to an In-Flight Project — [variation] · [no], step 1: CommitArtifactWithProvenance
// Projected stimulus (JSON): {"seq":1,"node":"commit-stale","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"CommitArtifactWithProvenance","call":"add-a-use-case-to-an-in-flight-project/commit-stale/0"},"expectedOutputs":[{"node":"commit-stale","kind":"inferred","to":"","label":"Re-commit bumps the revision; downstream committed artifacts are flagged stale"}],"expectedStates":[{"node":"use-case-model-recommitted","object":"Use-case model","inState":"[committed, revision bumped]"},{"node":"downstream-artifacts-stale","object":"Downstream committed artifacts","inState":"[stale]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"CommitArtifactWithProvenance","inputs":[{"name":"projectID","value":"stp-dsa-auc-p1"},{"name":"expectedVersion","value":"20"},{"name":"kind","value":"4"},{"name":"approvedBy","value":"founder@stp.test"},{"name":"draftedBy","value":"architect"}],"expect":{"result":"21","errorExpected":false},"probes":[{"for":"use-case-model-recommitted","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p1"}],"expect":{"result":"{\"ID\":\"stp-dsa-auc-p1\",\"CoreUseCases\":{\"Status\":2,\"Revisions\":2,\"StaleBasis\":false,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"architect\"}}}","errorExpected":false}},{"for":"downstream-artifacts-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p1"}],"expect":{"result":"{\"SystemDesign\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}},\"Network\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}}}","errorExpected":false}},{"for":"commit-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p1"}],"expect":{"result":"{\"Version\":21,\"CoreUseCases\":{\"Revisions\":2},\"SystemDesign\":{\"StaleBasis\":true}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the amended use-case model staged on main for its re-commit (committed once before), with its downstream artifacts committed, then calls the operation.
func stepDesignSessionAccessAddAUseCaseToAnInFlightProjectP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessAddAUseCaseToAnInFlightProjectP1S1) (any, error) {
	t.Helper()
	seedMain(t, amendmentAwaitingCommit(in.ProjectID, in.ExpectedVersion))
	return subject.CommitArtifactWithProvenance(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Kind, in.ApprovedBy, in.DraftedBy)
}

// probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP1S1N1 — Add a Use Case to an In-Flight Project — [variation] · [no], step 1, probe 1: projectStateAccess.ReadProject proves use-case-model-recommitted.
// Probe (JSON): {"for":"use-case-model-recommitted","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p1"}],"expect":{"result":"{\"ID\":\"stp-dsa-auc-p1\",\"CoreUseCases\":{\"Status\":2,\"Revisions\":2,\"StaleBasis\":false,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"architect\"}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP1S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-auc-p1")
}

// probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP1S1N2 — Add a Use Case to an In-Flight Project — [variation] · [no], step 1, probe 2: projectStateAccess.ReadProject proves downstream-artifacts-stale.
// Probe (JSON): {"for":"downstream-artifacts-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p1"}],"expect":{"result":"{\"SystemDesign\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}},\"Network\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP1S1N2(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-auc-p1")
}

// probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP1S1N3 — Add a Use Case to an In-Flight Project — [variation] · [no], step 1, probe 3: projectStateAccess.ReadProject proves commit-stale.
// Probe (JSON): {"for":"commit-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p1"}],"expect":{"result":"{\"Version\":21,\"CoreUseCases\":{\"Revisions\":2},\"SystemDesign\":{\"StaleBasis\":true}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP1S1N3(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-auc-p1")
}

// stepDesignSessionAccessAddAUseCaseToAnInFlightProjectP2S1 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 1: CommitArtifactWithProvenance
// Projected stimulus (JSON): {"seq":1,"node":"commit-stale","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"CommitArtifactWithProvenance","call":"add-a-use-case-to-an-in-flight-project/commit-stale/0"},"expectedOutputs":[{"node":"commit-stale","kind":"inferred","to":"","label":"Re-commit bumps the revision; downstream committed artifacts are flagged stale"}],"expectedStates":[{"node":"use-case-model-recommitted","object":"Use-case model","inState":"[committed, revision bumped]"},{"node":"downstream-artifacts-stale","object":"Downstream committed artifacts","inState":"[stale]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"CommitArtifactWithProvenance","inputs":[{"name":"projectID","value":"stp-dsa-auc-p2"},{"name":"expectedVersion","value":"20"},{"name":"kind","value":"4"},{"name":"approvedBy","value":"founder@stp.test"},{"name":"draftedBy","value":"architect"}],"expect":{"result":"21","errorExpected":false},"probes":[{"for":"use-case-model-recommitted","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p2"}],"expect":{"result":"{\"ID\":\"stp-dsa-auc-p2\",\"CoreUseCases\":{\"Status\":2,\"Revisions\":2,\"StaleBasis\":false,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"architect\"}}}","errorExpected":false}},{"for":"downstream-artifacts-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p2"}],"expect":{"result":"{\"SystemDesign\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}},\"Network\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}}}","errorExpected":false}},{"for":"commit-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p2"}],"expect":{"result":"{\"Version\":21,\"CoreUseCases\":{\"Revisions\":2},\"SystemDesign\":{\"StaleBasis\":true}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the amended use-case model staged on main for its re-commit (committed once before), with its downstream artifacts committed, then calls the operation.
func stepDesignSessionAccessAddAUseCaseToAnInFlightProjectP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessAddAUseCaseToAnInFlightProjectP2S1) (any, error) {
	t.Helper()
	seedMain(t, amendmentAwaitingCommit(in.ProjectID, in.ExpectedVersion))
	return subject.CommitArtifactWithProvenance(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Kind, in.ApprovedBy, in.DraftedBy)
}

// probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP2S1N1 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 1, probe 1: projectStateAccess.ReadProject proves use-case-model-recommitted.
// Probe (JSON): {"for":"use-case-model-recommitted","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p2"}],"expect":{"result":"{\"ID\":\"stp-dsa-auc-p2\",\"CoreUseCases\":{\"Status\":2,\"Revisions\":2,\"StaleBasis\":false,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"architect\"}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP2S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-auc-p2")
}

// probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP2S1N2 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 1, probe 2: projectStateAccess.ReadProject proves downstream-artifacts-stale.
// Probe (JSON): {"for":"downstream-artifacts-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p2"}],"expect":{"result":"{\"SystemDesign\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}},\"Network\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP2S1N2(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-auc-p2")
}

// probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP2S1N3 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 1, probe 3: projectStateAccess.ReadProject proves commit-stale.
// Probe (JSON): {"for":"commit-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p2"}],"expect":{"result":"{\"Version\":21,\"CoreUseCases\":{\"Revisions\":2},\"SystemDesign\":{\"StaleBasis\":true}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP2S1N3(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-auc-p2")
}

// stepDesignSessionAccessAddAUseCaseToAnInFlightProjectP3S1 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 1: CommitArtifactWithProvenance
// Projected stimulus (JSON): {"seq":1,"node":"commit-stale","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"CommitArtifactWithProvenance","call":"add-a-use-case-to-an-in-flight-project/commit-stale/0"},"expectedOutputs":[{"node":"commit-stale","kind":"inferred","to":"","label":"Re-commit bumps the revision; downstream committed artifacts are flagged stale"}],"expectedStates":[{"node":"use-case-model-recommitted","object":"Use-case model","inState":"[committed, revision bumped]"},{"node":"downstream-artifacts-stale","object":"Downstream committed artifacts","inState":"[stale]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"CommitArtifactWithProvenance","inputs":[{"name":"projectID","value":"stp-dsa-auc-p3"},{"name":"expectedVersion","value":"20"},{"name":"kind","value":"4"},{"name":"approvedBy","value":"founder@stp.test"},{"name":"draftedBy","value":"architect"}],"expect":{"result":"21","errorExpected":false},"probes":[{"for":"use-case-model-recommitted","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p3"}],"expect":{"result":"{\"ID\":\"stp-dsa-auc-p3\",\"CoreUseCases\":{\"Status\":2,\"Revisions\":2,\"StaleBasis\":false,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"architect\"}}}","errorExpected":false}},{"for":"downstream-artifacts-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p3"}],"expect":{"result":"{\"SystemDesign\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}},\"Network\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}}}","errorExpected":false}},{"for":"commit-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p3"}],"expect":{"result":"{\"Version\":21,\"CoreUseCases\":{\"Revisions\":2},\"SystemDesign\":{\"StaleBasis\":true}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the amended use-case model staged on main for its re-commit (committed once before), with its downstream artifacts committed, then calls the operation.
func stepDesignSessionAccessAddAUseCaseToAnInFlightProjectP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessAddAUseCaseToAnInFlightProjectP3S1) (any, error) {
	t.Helper()
	seedMain(t, amendmentAwaitingCommit(in.ProjectID, in.ExpectedVersion))
	return subject.CommitArtifactWithProvenance(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Kind, in.ApprovedBy, in.DraftedBy)
}

// probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP3S1N1 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 1, probe 1: projectStateAccess.ReadProject proves use-case-model-recommitted.
// Probe (JSON): {"for":"use-case-model-recommitted","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p3"}],"expect":{"result":"{\"ID\":\"stp-dsa-auc-p3\",\"CoreUseCases\":{\"Status\":2,\"Revisions\":2,\"StaleBasis\":false,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"architect\"}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP3S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-auc-p3")
}

// probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP3S1N2 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 1, probe 2: projectStateAccess.ReadProject proves downstream-artifacts-stale.
// Probe (JSON): {"for":"downstream-artifacts-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p3"}],"expect":{"result":"{\"SystemDesign\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}},\"Network\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP3S1N2(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-auc-p3")
}

// probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP3S1N3 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 1, probe 3: projectStateAccess.ReadProject proves commit-stale.
// Probe (JSON): {"for":"commit-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p3"}],"expect":{"result":"{\"Version\":21,\"CoreUseCases\":{\"Revisions\":2},\"SystemDesign\":{\"StaleBasis\":true}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP3S1N3(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-auc-p3")
}

// stepDesignSessionAccessAddAUseCaseToAnInFlightProjectP4S1 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 1: CommitArtifactWithProvenance
// Projected stimulus (JSON): {"seq":1,"node":"commit-stale","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"CommitArtifactWithProvenance","call":"add-a-use-case-to-an-in-flight-project/commit-stale/0"},"expectedOutputs":[{"node":"commit-stale","kind":"inferred","to":"","label":"Re-commit bumps the revision; downstream committed artifacts are flagged stale"}],"expectedStates":[{"node":"use-case-model-recommitted","object":"Use-case model","inState":"[committed, revision bumped]"},{"node":"downstream-artifacts-stale","object":"Downstream committed artifacts","inState":"[stale]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"CommitArtifactWithProvenance","inputs":[{"name":"projectID","value":"stp-dsa-auc-p4"},{"name":"expectedVersion","value":"20"},{"name":"kind","value":"4"},{"name":"approvedBy","value":"founder@stp.test"},{"name":"draftedBy","value":"architect"}],"expect":{"result":"21","errorExpected":false},"probes":[{"for":"use-case-model-recommitted","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p4"}],"expect":{"result":"{\"ID\":\"stp-dsa-auc-p4\",\"CoreUseCases\":{\"Status\":2,\"Revisions\":2,\"StaleBasis\":false,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"architect\"}}}","errorExpected":false}},{"for":"downstream-artifacts-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p4"}],"expect":{"result":"{\"SystemDesign\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}},\"Network\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}}}","errorExpected":false}},{"for":"commit-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p4"}],"expect":{"result":"{\"Version\":21,\"CoreUseCases\":{\"Revisions\":2},\"SystemDesign\":{\"StaleBasis\":true}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the amended use-case model staged on main for its re-commit (committed once before), with its downstream artifacts committed, then calls the operation.
func stepDesignSessionAccessAddAUseCaseToAnInFlightProjectP4S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessAddAUseCaseToAnInFlightProjectP4S1) (any, error) {
	t.Helper()
	seedMain(t, amendmentAwaitingCommit(in.ProjectID, in.ExpectedVersion))
	return subject.CommitArtifactWithProvenance(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Kind, in.ApprovedBy, in.DraftedBy)
}

// probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP4S1N1 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 1, probe 1: projectStateAccess.ReadProject proves use-case-model-recommitted.
// Probe (JSON): {"for":"use-case-model-recommitted","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p4"}],"expect":{"result":"{\"ID\":\"stp-dsa-auc-p4\",\"CoreUseCases\":{\"Status\":2,\"Revisions\":2,\"StaleBasis\":false,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"architect\"}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP4S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-auc-p4")
}

// probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP4S1N2 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 1, probe 2: projectStateAccess.ReadProject proves downstream-artifacts-stale.
// Probe (JSON): {"for":"downstream-artifacts-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p4"}],"expect":{"result":"{\"SystemDesign\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}},\"Network\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\",\"upstreamRevision\":2}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP4S1N2(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-auc-p4")
}

// probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP4S1N3 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 1, probe 3: projectStateAccess.ReadProject proves commit-stale.
// Probe (JSON): {"for":"commit-stale","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-auc-p4"}],"expect":{"result":"{\"Version\":21,\"CoreUseCases\":{\"Revisions\":2},\"SystemDesign\":{\"StaleBasis\":true}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAddAUseCaseToAnInFlightProjectP4S1N3(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-auc-p4")
}

// stepDesignSessionAccessAskAClarifyingQuestionDuringReviewP3S1 — Ask a Clarifying Question During Review — [no: committed] · [yes], step 1: SeedReviewCommentsOnBranch
// Projected stimulus (JSON): {"seq":1,"node":"seed-main-thread","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"SeedReviewCommentsOnBranch","call":"ask-a-clarifying-question-during-review/seed-main-thread/0"},"expectedOutputs":[{"node":"seed-main-thread","kind":"inferred","to":"","label":"Seed a question-only thread on main; no amendment session opens"}],"expectedStates":[{"node":"thread-seeded-on-main","object":"ReviewThread","inState":"[question-only thread on main; no amendment session]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"SeedReviewCommentsOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-acq-p3"},{"name":"expectedVersion","value":"40"},{"name":"branch","value":""},{"name":"kind","value":"5"},{"name":"round","value":"1"},{"name":"comments","value":"[{\"id\":\"\",\"anchor\":\"components.designSessionAccess\",\"anchorText\":\"designSessionAccess\",\"text\":\"Why does ReconcileBranchFromMain live on the design-session facet rather than sourceControlAccess?\",\"authorRole\":\"founder\",\"round\":1,\"status\":\"open\",\"replies\":[],\"reopened\":false,\"type\":\"question\",\"addressee\":\"architect\"}]"},{"name":"replies","value":"[]"},{"name":"idempotencyKey","value":"stp-dsa-acq-p3:seed:1"}],"expect":{"result":"41","errorExpected":false},"probes":[{"for":"thread-seeded-on-main","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-acq-p3"},{"name":"branch","value":""}],"expect":{"result":"{\"slots\":{\"5\":{\"status\":2,\"reviewThread\":[{\"id\":\"r1c1\",\"round\":1,\"type\":\"question\",\"addressee\":\"architect\",\"status\":\"open\",\"authorRole\":\"founder\",\"replies\":[]}]}},\"id\":\"stp-dsa-acq-p3\",\"version\":41}","errorExpected":false}},{"for":"seed-main-thread","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-acq-p3"}],"expect":{"result":"{\"SystemDesign\":{\"Status\":2,\"reviewThread\":[{\"id\":\"r1c1\",\"type\":\"question\",\"status\":\"open\"}]}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges artifact-state=[no: committed]: the system design is committed on main and no amendment session is open, then calls the operation.
func stepDesignSessionAccessAskAClarifyingQuestionDuringReviewP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessAskAClarifyingQuestionDuringReviewP3S1) (any, error) {
	t.Helper()
	seedMain(t, committedSystem(in.ProjectID, in.ExpectedVersion))
	return subject.SeedReviewCommentsOnBranch(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Branch, in.Kind, in.Round, in.Comments, in.Replies, in.IdempotencyKey)
}

// probeDesignSessionAccessAskAClarifyingQuestionDuringReviewP3S1N2 — Ask a Clarifying Question During Review — [no: committed] · [yes], step 1, probe 2: projectStateAccess.ReadProject proves seed-main-thread.
// Probe (JSON): {"for":"seed-main-thread","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-acq-p3"}],"expect":{"result":"{\"SystemDesign\":{\"Status\":2,\"reviewThread\":[{\"id\":\"r1c1\",\"type\":\"question\",\"status\":\"open\"}]}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAskAClarifyingQuestionDuringReviewP3S1N2(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-acq-p3")
}

// stepDesignSessionAccessAskAClarifyingQuestionDuringReviewP4S1 — Ask a Clarifying Question During Review — [no: committed] · [no: dispatch miss], step 1: SeedReviewCommentsOnBranch
// Projected stimulus (JSON): {"seq":1,"node":"seed-main-thread","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"SeedReviewCommentsOnBranch","call":"ask-a-clarifying-question-during-review/seed-main-thread/0"},"expectedOutputs":[{"node":"seed-main-thread","kind":"inferred","to":"","label":"Seed a question-only thread on main; no amendment session opens"}],"expectedStates":[{"node":"thread-seeded-on-main","object":"ReviewThread","inState":"[question-only thread on main; no amendment session]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"SeedReviewCommentsOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-acq-p4"},{"name":"expectedVersion","value":"40"},{"name":"branch","value":""},{"name":"kind","value":"5"},{"name":"round","value":"1"},{"name":"comments","value":"[{\"id\":\"\",\"anchor\":\"components.designSessionAccess\",\"anchorText\":\"designSessionAccess\",\"text\":\"Why does ReconcileBranchFromMain live on the design-session facet rather than sourceControlAccess?\",\"authorRole\":\"founder\",\"round\":1,\"status\":\"open\",\"replies\":[],\"reopened\":false,\"type\":\"question\",\"addressee\":\"architect\"}]"},{"name":"replies","value":"[]"},{"name":"idempotencyKey","value":"stp-dsa-acq-p4:seed:1"}],"expect":{"result":"41","errorExpected":false},"probes":[{"for":"thread-seeded-on-main","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-acq-p4"},{"name":"branch","value":""}],"expect":{"result":"{\"slots\":{\"5\":{\"status\":2,\"reviewThread\":[{\"id\":\"r1c1\",\"round\":1,\"type\":\"question\",\"addressee\":\"architect\",\"status\":\"open\",\"authorRole\":\"founder\",\"replies\":[]}]}},\"id\":\"stp-dsa-acq-p4\",\"version\":41}","errorExpected":false}},{"for":"seed-main-thread","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-acq-p4"}],"expect":{"result":"{\"SystemDesign\":{\"Status\":2,\"reviewThread\":[{\"id\":\"r1c1\",\"type\":\"question\",\"status\":\"open\"}]}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges artifact-state=[no: committed]: the system design is committed on main and no amendment session is open, then calls the operation.
func stepDesignSessionAccessAskAClarifyingQuestionDuringReviewP4S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessAskAClarifyingQuestionDuringReviewP4S1) (any, error) {
	t.Helper()
	seedMain(t, committedSystem(in.ProjectID, in.ExpectedVersion))
	return subject.SeedReviewCommentsOnBranch(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Branch, in.Kind, in.Round, in.Comments, in.Replies, in.IdempotencyKey)
}

// probeDesignSessionAccessAskAClarifyingQuestionDuringReviewP4S1N2 — Ask a Clarifying Question During Review — [no: committed] · [no: dispatch miss], step 1, probe 2: projectStateAccess.ReadProject proves seed-main-thread.
// Probe (JSON): {"for":"seed-main-thread","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-acq-p4"}],"expect":{"result":"{\"SystemDesign\":{\"Status\":2,\"reviewThread\":[{\"id\":\"r1c1\",\"type\":\"question\",\"status\":\"open\"}]}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessAskAClarifyingQuestionDuringReviewP4S1N2(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-acq-p4")
}

// stepDesignSessionAccessExecuteAProjectActivityP1S1 — Execute a Project Activity — dispatch · yes, step 1: ReadProjectOnBranch
// Projected stimulus (JSON): {"seq":1,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"ReadProjectOnBranch","call":"execute-a-project-activity/observe-validate/3"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-eapa-p1"},{"name":"branch","value":"activity/requirements"}],"expect":{"result":"{\"id\":\"stp-dsa-eapa-p1\",\"version\":11,\"slots\":{\"4\":{\"model\":{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\"},\"essenceRationale\":\"Drafted by the dispatched job; read back for the exit criterion.\",\"rejectionReason\":\"\"}]}}}}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges exit-met=yes: the dispatched job committed its use-case draft to the activity branch, then reads the branch back.
func stepDesignSessionAccessExecuteAProjectActivityP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessExecuteAProjectActivityP1S1) (any, error) {
	t.Helper()
	seedEligibleRequirements(t, in.ProjectID, 11, useCaseModel("stp-added-use-case", "STP Added Use Case", "Drafted by the dispatched job; read back for the exit criterion."))
	return subject.ReadProjectOnBranch(callContext(t, h, "1"), in.ProjectID, in.Branch)
}

// stepDesignSessionAccessExecuteAProjectActivityP2S1 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 1: ReadProjectOnBranch
// Projected stimulus (JSON): {"seq":1,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"ReadProjectOnBranch","call":"execute-a-project-activity/observe-validate/3"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-eapa-p2"},{"name":"branch","value":"activity/requirements"}],"expect":{"result":"{\"id\":\"stp-dsa-eapa-p2\",\"version\":10}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges exit-met=no: the dispatched job left the activity branch without a draft, then reads the branch back.
func stepDesignSessionAccessExecuteAProjectActivityP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessExecuteAProjectActivityP2S1) (any, error) {
	t.Helper()
	seedEligibleRequirements(t, in.ProjectID, 10, nil)
	return subject.ReadProjectOnBranch(callContext(t, h, "1"), in.ProjectID, in.Branch)
}

// stepDesignSessionAccessExecuteAProjectActivityP3S1 — Execute a Project Activity — review · agents only · approve, step 1: StageArtifactForReviewOnBranch
// Projected stimulus (JSON): {"seq":1,"node":"commit-artifact","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"StageArtifactForReviewOnBranch","call":"execute-a-project-activity/commit-artifact/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"StageArtifactForReviewOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-eapa-p3"},{"name":"expectedVersion","value":"30"},{"name":"branch","value":""},{"name":"model","value":"{\"kind\":\"network\",\"model\":{\"dependencies\":[{\"activity\":\"architecture\",\"dependsOn\":[\"requirements\"]},{\"activity\":\"projectDesign\",\"dependsOn\":[\"architecture\"]}],\"criticalPath\":[\"architecture\",\"projectDesign\",\"requirements\"]}}"},{"name":"idempotencyKey","value":"stp-dsa-eapa-p3:m0:stage:network"}],"expect":{"result":"31","errorExpected":false},"probes":[{"for":"","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-eapa-p3"},{"name":"branch","value":""}],"expect":{"result":"{\"id\":\"stp-dsa-eapa-p3\",\"version\":31,\"slots\":{\"10\":{\"status\":1,\"model\":{\"kind\":\"network\"}}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the project-design walk: main at the binding's version and the activity branch carrying its in-flight planning-assumptions draft, then calls the operation.
func stepDesignSessionAccessExecuteAProjectActivityP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessExecuteAProjectActivityP3S1) (any, error) {
	t.Helper()
	seedNetworkReview(t, in.ProjectID, in.ExpectedVersion)
	return subject.StageArtifactForReviewOnBranch(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Branch, in.Model, in.IdempotencyKey)
}

// probeDesignSessionAccessExecuteAProjectActivityP3S2N1 — Execute a Project Activity — review · agents only · approve, step 2, probe 1: projectStateAccess.ReadProject proves .
// Probe (JSON): {"for":"","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-eapa-p3"}],"expect":{"result":"{\"Version\":32,\"Network\":{\"Status\":2,\"Revisions\":1,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"platform:estimation-engine\"}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessExecuteAProjectActivityP3S2N1(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-eapa-p3")
}

// stepDesignSessionAccessExecuteAProjectActivityP4S1 — Execute a Project Activity — review · human required · approve, step 1: StageArtifactForReviewOnBranch
// Projected stimulus (JSON): {"seq":1,"node":"commit-artifact","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"StageArtifactForReviewOnBranch","call":"execute-a-project-activity/commit-artifact/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"StageArtifactForReviewOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-eapa-p4"},{"name":"expectedVersion","value":"30"},{"name":"branch","value":""},{"name":"model","value":"{\"kind\":\"network\",\"model\":{\"dependencies\":[{\"activity\":\"architecture\",\"dependsOn\":[\"requirements\"]},{\"activity\":\"projectDesign\",\"dependsOn\":[\"architecture\"]}],\"criticalPath\":[\"architecture\",\"projectDesign\",\"requirements\"]}}"},{"name":"idempotencyKey","value":"stp-dsa-eapa-p4:m0:stage:network"}],"expect":{"result":"31","errorExpected":false},"probes":[{"for":"","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-eapa-p4"},{"name":"branch","value":""}],"expect":{"result":"{\"id\":\"stp-dsa-eapa-p4\",\"version\":31,\"slots\":{\"10\":{\"status\":1,\"model\":{\"kind\":\"network\"}}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the project-design walk: main at the binding's version and the activity branch carrying its in-flight planning-assumptions draft, then calls the operation.
func stepDesignSessionAccessExecuteAProjectActivityP4S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessExecuteAProjectActivityP4S1) (any, error) {
	t.Helper()
	seedNetworkReview(t, in.ProjectID, in.ExpectedVersion)
	return subject.StageArtifactForReviewOnBranch(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Branch, in.Model, in.IdempotencyKey)
}

// probeDesignSessionAccessExecuteAProjectActivityP4S2N1 — Execute a Project Activity — review · human required · approve, step 2, probe 1: projectStateAccess.ReadProject proves .
// Probe (JSON): {"for":"","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-eapa-p4"}],"expect":{"result":"{\"Version\":32,\"Network\":{\"Status\":2,\"Revisions\":1,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"platform:estimation-engine\"}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessExecuteAProjectActivityP4S2N1(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-eapa-p4")
}

// stepDesignSessionAccessExecuteAProjectActivityP5S1 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 1: ReadProjectOnBranch
// Projected stimulus (JSON): {"seq":1,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"ReadProjectOnBranch","call":"execute-a-project-activity/observe-validate/3"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-eapa-p5"},{"name":"branch","value":"activity/requirements"}],"expect":{"result":"{\"id\":\"stp-dsa-eapa-p5\",\"version\":10}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges exit-met=no: the dispatched job left the activity branch without a draft, then reads the branch back.
func stepDesignSessionAccessExecuteAProjectActivityP5S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessExecuteAProjectActivityP5S1) (any, error) {
	t.Helper()
	seedEligibleRequirements(t, in.ProjectID, 10, nil)
	return subject.ReadProjectOnBranch(callContext(t, h, "1"), in.ProjectID, in.Branch)
}

// stepDesignSessionAccessExecuteAProjectActivityP5S2 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 2: ReadProjectOnBranch
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"ReadProjectOnBranch","call":"execute-a-project-activity/observe-validate/3"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-eapa-p5"},{"name":"branch","value":"activity/requirements"}],"expect":{"result":"{\"id\":\"stp-dsa-eapa-p5\",\"version\":11,\"slots\":{\"4\":{\"model\":{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-added-use-case\",\"name\":\"STP Added Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\"},\"essenceRationale\":\"Drafted by the dispatched job; read back for the exit criterion.\",\"rejectionReason\":\"\"}]}}}}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges exit-met=yes: the dispatched job committed its use-case draft to the activity branch, then reads the branch back.
func stepDesignSessionAccessExecuteAProjectActivityP5S2(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessExecuteAProjectActivityP5S2) (any, error) {
	t.Helper()
	seedEligibleRequirements(t, in.ProjectID, 11, useCaseModel("stp-added-use-case", "STP Added Use Case", "Drafted by the dispatched job; read back for the exit criterion."))
	return subject.ReadProjectOnBranch(callContext(t, h, "2"), in.ProjectID, in.Branch)
}

// stepDesignSessionAccessExecuteAProjectActivityP6S1 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 1: ReadProjectOnBranch
// Projected stimulus (JSON): {"seq":1,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"ReadProjectOnBranch","call":"execute-a-project-activity/observe-validate/3"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-eapa-p6"},{"name":"branch","value":"activity/requirements"}],"expect":{"result":"{\"id\":\"stp-dsa-eapa-p6\",\"version\":10}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges exit-met=no: the dispatched job left the activity branch without a draft, then reads the branch back.
func stepDesignSessionAccessExecuteAProjectActivityP6S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessExecuteAProjectActivityP6S1) (any, error) {
	t.Helper()
	seedEligibleRequirements(t, in.ProjectID, 10, nil)
	return subject.ReadProjectOnBranch(callContext(t, h, "1"), in.ProjectID, in.Branch)
}

// stepDesignSessionAccessExecuteAProjectActivityP7S1 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 1: ReadProjectOnBranch
// Projected stimulus (JSON): {"seq":1,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"ReadProjectOnBranch","call":"execute-a-project-activity/observe-validate/3"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-eapa-p7"},{"name":"branch","value":"activity/requirements"}],"expect":{"result":"{\"id\":\"stp-dsa-eapa-p7\",\"version\":12,\"slots\":{\"4\":{\"model\":{\"kind\":\"coreUseCases\",\"model\":{\"decisions\":[{\"useCase\":{\"id\":\"stp-revised-use-case\",\"name\":\"STP Revised Use Case\",\"actors\":[{\"id\":\"architect-user\",\"role\":\"Architect User\"}],\"trigger\":\"clientAction\"},\"essenceRationale\":\"Revision n+1 after the reviewer's sendBack.\",\"rejectionReason\":\"\"}]}}}}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges verdict=sendBack: revision n+1 of the draft is on the activity branch, then reads the branch back.
func stepDesignSessionAccessExecuteAProjectActivityP7S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessExecuteAProjectActivityP7S1) (any, error) {
	t.Helper()
	seedEligibleRequirements(t, in.ProjectID, 12, useCaseModel("stp-revised-use-case", "STP Revised Use Case", "Revision n+1 after the reviewer's sendBack."))
	return subject.ReadProjectOnBranch(callContext(t, h, "1"), in.ProjectID, in.Branch)
}

// stepDesignSessionAccessManageProjectsP1S1 — Manage Projects — [no: self-operated], step 1: StageArtifactForReviewOnBranch
// Projected stimulus (JSON): {"seq":1,"node":"create-head-state","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"StageArtifactForReviewOnBranch","call":"manage-projects/create-head-state/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"StageArtifactForReviewOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-mp-p1"},{"name":"expectedVersion","value":"1"},{"name":"branch","value":""},{"name":"model","value":"{\"kind\":\"activityList\",\"model\":{\"activities\":[{\"name\":\"requirements\",\"title\":\"Requirements\",\"effortDays\":5,\"workerClass\":\"system-architect\",\"coding\":false,\"riskBucket\":2},{\"name\":\"architecture\",\"title\":\"Architecture\",\"effortDays\":5,\"workerClass\":\"system-architect\",\"coding\":false,\"riskBucket\":2},{\"name\":\"projectDesign\",\"title\":\"Project Design\",\"effortDays\":5,\"workerClass\":\"project-manager\",\"coding\":false,\"riskBucket\":2}]}}"},{"name":"idempotencyKey","value":"stp-dsa-mp-p1:birth:stage:activityList"}],"expect":{"result":"2","errorExpected":false},"probes":[{"for":"","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-mp-p1"},{"name":"branch","value":""}],"expect":{"result":"{\"id\":\"stp-dsa-mp-p1\",\"version\":2,\"slots\":{\"9\":{\"status\":1,\"model\":{\"kind\":\"activityList\"}}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges a just-created project (CreateProject's version), then stages the derived design-prefix activity list on main.
func stepDesignSessionAccessManageProjectsP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessManageProjectsP1S1) (any, error) {
	t.Helper()
	seedMain(t, bornProject(in.ProjectID, in.ExpectedVersion))
	return subject.StageArtifactForReviewOnBranch(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Branch, in.Model, in.IdempotencyKey)
}

// probeDesignSessionAccessManageProjectsP1S2N2 — Manage Projects — [no: self-operated], step 2, probe 2: projectStateAccess.ReadProject proves create-head-state.
// Probe (JSON): {"for":"create-head-state","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-mp-p1"}],"expect":{"result":"{\"ID\":\"stp-dsa-mp-p1\",\"ActivityList\":{\"Status\":2,\"Revisions\":1,\"StaleBasis\":false,\"Provenance\":{\"approvedBy\":\"platform:project-birth\",\"draftedBy\":\"platform:design-prefix-derivation\"}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessManageProjectsP1S2N2(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-mp-p1")
}

// stepDesignSessionAccessManageProjectsP2S1 — Manage Projects — [yes], step 1: StageArtifactForReviewOnBranch
// Projected stimulus (JSON): {"seq":1,"node":"create-head-state","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"StageArtifactForReviewOnBranch","call":"manage-projects/create-head-state/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"StageArtifactForReviewOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-mp-p2"},{"name":"expectedVersion","value":"1"},{"name":"branch","value":""},{"name":"model","value":"{\"kind\":\"activityList\",\"model\":{\"activities\":[{\"name\":\"requirements\",\"title\":\"Requirements\",\"effortDays\":5,\"workerClass\":\"system-architect\",\"coding\":false,\"riskBucket\":2},{\"name\":\"architecture\",\"title\":\"Architecture\",\"effortDays\":5,\"workerClass\":\"system-architect\",\"coding\":false,\"riskBucket\":2},{\"name\":\"projectDesign\",\"title\":\"Project Design\",\"effortDays\":5,\"workerClass\":\"project-manager\",\"coding\":false,\"riskBucket\":2}]}}"},{"name":"idempotencyKey","value":"stp-dsa-mp-p2:birth:stage:activityList"}],"expect":{"result":"2","errorExpected":false},"probes":[{"for":"","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-mp-p2"},{"name":"branch","value":""}],"expect":{"result":"{\"id\":\"stp-dsa-mp-p2\",\"version\":2,\"slots\":{\"9\":{\"status\":1,\"model\":{\"kind\":\"activityList\"}}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges a just-created project (CreateProject's version), then stages the derived design-prefix activity list on main.
func stepDesignSessionAccessManageProjectsP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessManageProjectsP2S1) (any, error) {
	t.Helper()
	seedMain(t, bornProject(in.ProjectID, in.ExpectedVersion))
	return subject.StageArtifactForReviewOnBranch(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Branch, in.Model, in.IdempotencyKey)
}

// probeDesignSessionAccessManageProjectsP2S2N2 — Manage Projects — [yes], step 2, probe 2: projectStateAccess.ReadProject proves create-head-state.
// Probe (JSON): {"for":"create-head-state","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-mp-p2"}],"expect":{"result":"{\"ID\":\"stp-dsa-mp-p2\",\"ActivityList\":{\"Status\":2,\"Revisions\":1,\"StaleBasis\":false,\"Provenance\":{\"approvedBy\":\"platform:project-birth\",\"draftedBy\":\"platform:design-prefix-derivation\"}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessManageProjectsP2S2N2(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-mp-p2")
}

// stepDesignSessionAccessReplanUnderScopeChangeP2S1 — Replan Under Scope Change — [no: scope change] · [accept], step 1: CommitArtifactWithProvenance
// Projected stimulus (JSON): {"seq":1,"node":"adopt","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"CommitArtifactWithProvenance","call":"replan-under-scope-change/adopt/0.1"},"expectedOutputs":[{"node":"adopt","kind":"inferred","to":"","label":"Adopt the new network as the commitment; reset the planned earned-value curve"}],"expectedStates":[{"node":"sdp-revision-adopted","object":"SDP review revision","inState":"[adopted]"},{"node":"committed-network-revised","object":"Committed network","inState":"[revised option committed; planned earned-value curve reset]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"CommitArtifactWithProvenance","inputs":[{"name":"projectID","value":"stp-dsa-rusc-p2"},{"name":"expectedVersion","value":"50"},{"name":"kind","value":"16"},{"name":"approvedBy","value":"founder@stp.test"},{"name":"draftedBy","value":"project-manager"}],"expect":{"result":"51","errorExpected":false},"probes":[{"for":"sdp-revision-adopted","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-rusc-p2"}],"expect":{"result":"{\"ID\":\"stp-dsa-rusc-p2\",\"SdpReview\":{\"Status\":2,\"Revisions\":2,\"StaleBasis\":false,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"project-manager\"}}}","errorExpected":false}},{"for":"committed-network-revised","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-dsa-rusc-p2"},{"name":"branch","value":""}],"expect":{"result":"{\"id\":\"stp-dsa-rusc-p2\",\"version\":51,\"slots\":{\"16\":{\"status\":2,\"model\":{\"kind\":\"sdpReview\",\"model\":{\"recommendation\":\"opt-compressed\"}}}}}","errorExpected":false}},{"for":"adopt","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-rusc-p2"}],"expect":{"result":"{\"Version\":51,\"SdpReview\":{\"Status\":2,\"Revisions\":2}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges mgmt=[accept]: the revised SDP review (revision 2) is staged on main over the committed plan, then calls the operation.
func stepDesignSessionAccessReplanUnderScopeChangeP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessReplanUnderScopeChangeP2S1) (any, error) {
	t.Helper()
	seedMain(t, sdpRevisionAwaitingAdoption(in.ProjectID, in.ExpectedVersion))
	return subject.CommitArtifactWithProvenance(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Kind, in.ApprovedBy, in.DraftedBy)
}

// probeDesignSessionAccessReplanUnderScopeChangeP2S1N1 — Replan Under Scope Change — [no: scope change] · [accept], step 1, probe 1: projectStateAccess.ReadProject proves sdp-revision-adopted.
// Probe (JSON): {"for":"sdp-revision-adopted","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-rusc-p2"}],"expect":{"result":"{\"ID\":\"stp-dsa-rusc-p2\",\"SdpReview\":{\"Status\":2,\"Revisions\":2,\"StaleBasis\":false,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"project-manager\"}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessReplanUnderScopeChangeP2S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-rusc-p2")
}

// probeDesignSessionAccessReplanUnderScopeChangeP2S1N3 — Replan Under Scope Change — [no: scope change] · [accept], step 1, probe 3: projectStateAccess.ReadProject proves adopt.
// Probe (JSON): {"for":"adopt","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-rusc-p2"}],"expect":{"result":"{\"Version\":51,\"SdpReview\":{\"Status\":2,\"Revisions\":2}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessReplanUnderScopeChangeP2S1N3(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-rusc-p2")
}

// newSubjectGitActivityStatusAccess builds the component under test against the host's real downstream
// doubles. Constructed via the generated constructor in internal/resourceaccess/projectstate.
func newSubjectGitActivityStatusAccess(t *testing.T, _ *scenariohost.Host) projectstate.GitActivityStatusAccess {
	t.Helper()
	return projectstate.NewGitLocalGitActivityStatusAccess(newScenarioRepo(t))
}

// stepGitActivityStatusAccessExecuteAProjectActivityP1S1 — Execute a Project Activity — dispatch · yes, step 1: RecordActivityBranchOpened
// Projected stimulus (JSON): {"seq":1,"node":"dispatch-task","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityBranchOpened","call":"execute-a-project-activity/dispatch-task/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"RecordActivityBranchOpened","inputs":[{"name":"projectID","value":"stp-gas-P1"},{"name":"expectedVersion","value":"12"},{"name":"activityID","value":"C-DM"},{"name":"branch","value":"activity/C-DM"},{"name":"branchRef","value":"refs/heads/activity/C-DM"},{"name":"prRef","value":"https://github.com/mixofreality-studio/stp-fixture/pull/101"},{"name":"crLabel","value":""},{"name":"isRevert","value":"false"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P1-s1-branch-opened"}],"expect":{"result":"13","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP1S1) (any, error) {
	t.Helper()
	seedMain(t, constructionDispatching(in.ProjectID, in.ExpectedVersion, in.ActivityID))
	return subject.RecordActivityBranchOpened(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Branch, in.BranchRef, in.PrRef, in.CrLabel, in.IsRevert, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP1S2 — Execute a Project Activity — dispatch · yes, step 2: RecordActivityCIObserved
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityCIObserved","call":"execute-a-project-activity/observe-validate/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"RecordActivityCIObserved","inputs":[{"name":"projectID","value":"stp-gas-P1"},{"name":"expectedVersion","value":"13"},{"name":"activityID","value":"C-DM"},{"name":"ci","value":"1"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P1-s2-ci-success"}],"expect":{"result":"14","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP1S2(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP1S2) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.RecordActivityCIObserved(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Ci, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP2S1 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 1: RecordActivityBranchOpened
// Projected stimulus (JSON): {"seq":1,"node":"dispatch-task","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityBranchOpened","call":"execute-a-project-activity/dispatch-task/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"RecordActivityBranchOpened","inputs":[{"name":"projectID","value":"stp-gas-P2"},{"name":"expectedVersion","value":"12"},{"name":"activityID","value":"C-DM"},{"name":"branch","value":"activity/C-DM"},{"name":"branchRef","value":"refs/heads/activity/C-DM"},{"name":"prRef","value":"https://github.com/mixofreality-studio/stp-fixture/pull/102"},{"name":"crLabel","value":""},{"name":"isRevert","value":"false"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P2-s1-branch-opened"}],"expect":{"result":"13","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP2S1) (any, error) {
	t.Helper()
	seedMain(t, constructionDispatching(in.ProjectID, in.ExpectedVersion, in.ActivityID))
	return subject.RecordActivityBranchOpened(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Branch, in.BranchRef, in.PrRef, in.CrLabel, in.IsRevert, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP2S2 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 2: RecordActivityCIObserved
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityCIObserved","call":"execute-a-project-activity/observe-validate/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"RecordActivityCIObserved","inputs":[{"name":"projectID","value":"stp-gas-P2"},{"name":"expectedVersion","value":"13"},{"name":"activityID","value":"C-DM"},{"name":"ci","value":"2"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P2-s2-ci-failure"}],"expect":{"result":"14","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP2S2(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP2S2) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.RecordActivityCIObserved(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Ci, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP3S1 — Execute a Project Activity — review · agents only · approve, step 1: RecordActivityCIObserved
// Projected stimulus (JSON): {"seq":1,"node":"commit-artifact","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityCIObserved","call":"execute-a-project-activity/commit-artifact/6"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"RecordActivityCIObserved","inputs":[{"name":"projectID","value":"stp-gas-P3"},{"name":"expectedVersion","value":"12"},{"name":"activityID","value":"C-DM"},{"name":"ci","value":"1"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P3-s1-ci-success"}],"expect":{"result":"13","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=agents only, verdict=approve hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP3S1) (any, error) {
	t.Helper()
	seedMain(t, pullRequestAwaitingCI(in.ProjectID, in.ExpectedVersion, in.ActivityID))
	return subject.RecordActivityCIObserved(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Ci, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP4S1 — Execute a Project Activity — review · human required · approve, step 1: RecordActivityCIObserved
// Projected stimulus (JSON): {"seq":1,"node":"commit-artifact","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityCIObserved","call":"execute-a-project-activity/commit-artifact/6"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"RecordActivityCIObserved","inputs":[{"name":"projectID","value":"stp-gas-P4"},{"name":"expectedVersion","value":"12"},{"name":"activityID","value":"C-DM"},{"name":"ci","value":"1"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P4-s1-ci-success"}],"expect":{"result":"13","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=human required, verdict=approve hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP4S1(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP4S1) (any, error) {
	t.Helper()
	seedMain(t, pullRequestAwaitingCI(in.ProjectID, in.ExpectedVersion, in.ActivityID))
	return subject.RecordActivityCIObserved(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Ci, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP5S1 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 1: RecordActivityBranchOpened
// Projected stimulus (JSON): {"seq":1,"node":"dispatch-task","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityBranchOpened","call":"execute-a-project-activity/dispatch-task/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"RecordActivityBranchOpened","inputs":[{"name":"projectID","value":"stp-gas-P5"},{"name":"expectedVersion","value":"12"},{"name":"activityID","value":"C-DM"},{"name":"branch","value":"activity/C-DM"},{"name":"branchRef","value":"refs/heads/activity/C-DM"},{"name":"prRef","value":"https://github.com/mixofreality-studio/stp-fixture/pull/105"},{"name":"crLabel","value":""},{"name":"isRevert","value":"false"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P5-s1-branch-opened"}],"expect":{"result":"13","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=yes: retry, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP5S1(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP5S1) (any, error) {
	t.Helper()
	seedMain(t, constructionDispatching(in.ProjectID, in.ExpectedVersion, in.ActivityID))
	return subject.RecordActivityBranchOpened(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Branch, in.BranchRef, in.PrRef, in.CrLabel, in.IsRevert, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP5S2 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 2: RecordActivityCIObserved
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityCIObserved","call":"execute-a-project-activity/observe-validate/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"RecordActivityCIObserved","inputs":[{"name":"projectID","value":"stp-gas-P5"},{"name":"expectedVersion","value":"13"},{"name":"activityID","value":"C-DM"},{"name":"ci","value":"2"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P5-s2-ci-failure"}],"expect":{"result":"14","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=yes: retry, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP5S2(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP5S2) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.RecordActivityCIObserved(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Ci, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP5S3 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 3: RecordActivityBranchOpened
// Projected stimulus (JSON): {"seq":3,"node":"dispatch-task","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityBranchOpened","call":"execute-a-project-activity/dispatch-task/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"RecordActivityBranchOpened","inputs":[{"name":"projectID","value":"stp-gas-P5"},{"name":"expectedVersion","value":"14"},{"name":"activityID","value":"C-DM"},{"name":"branch","value":"activity/C-DM"},{"name":"branchRef","value":"refs/heads/activity/C-DM"},{"name":"prRef","value":"https://github.com/mixofreality-studio/stp-fixture/pull/105"},{"name":"crLabel","value":""},{"name":"isRevert","value":"false"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P5-s3-branch-opened"}],"expect":{"result":"15","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=yes: retry, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP5S3(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP5S3) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.RecordActivityBranchOpened(callContext(t, h, "3"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Branch, in.BranchRef, in.PrRef, in.CrLabel, in.IsRevert, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP5S4 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 4: RecordActivityCIObserved
// Projected stimulus (JSON): {"seq":4,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityCIObserved","call":"execute-a-project-activity/observe-validate/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"RecordActivityCIObserved","inputs":[{"name":"projectID","value":"stp-gas-P5"},{"name":"expectedVersion","value":"15"},{"name":"activityID","value":"C-DM"},{"name":"ci","value":"1"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P5-s4-ci-success"}],"expect":{"result":"16","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=yes: retry, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP5S4(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP5S4) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.RecordActivityCIObserved(callContext(t, h, "4"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Ci, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP6S1 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 1: RecordActivityBranchOpened
// Projected stimulus (JSON): {"seq":1,"node":"dispatch-task","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityBranchOpened","call":"execute-a-project-activity/dispatch-task/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"RecordActivityBranchOpened","inputs":[{"name":"projectID","value":"stp-gas-P6"},{"name":"expectedVersion","value":"12"},{"name":"activityID","value":"C-DM"},{"name":"branch","value":"activity/C-DM"},{"name":"branchRef","value":"refs/heads/activity/C-DM"},{"name":"prRef","value":"https://github.com/mixofreality-studio/stp-fixture/pull/106"},{"name":"crLabel","value":""},{"name":"isRevert","value":"false"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P6-s1-branch-opened"}],"expect":{"result":"13","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=no, operator-intervened=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP6S1(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP6S1) (any, error) {
	t.Helper()
	seedMain(t, constructionDispatching(in.ProjectID, in.ExpectedVersion, in.ActivityID))
	return subject.RecordActivityBranchOpened(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Branch, in.BranchRef, in.PrRef, in.CrLabel, in.IsRevert, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP6S2 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 2: RecordActivityCIObserved
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityCIObserved","call":"execute-a-project-activity/observe-validate/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"RecordActivityCIObserved","inputs":[{"name":"projectID","value":"stp-gas-P6"},{"name":"expectedVersion","value":"13"},{"name":"activityID","value":"C-DM"},{"name":"ci","value":"2"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P6-s2-ci-failure"}],"expect":{"result":"14","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=no, operator-intervened=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP6S2(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP6S2) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.RecordActivityCIObserved(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Ci, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP7S1 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 1: RecordActivityBranchOpened
// Projected stimulus (JSON): {"seq":1,"node":"dispatch-task","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityBranchOpened","call":"execute-a-project-activity/dispatch-task/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"RecordActivityBranchOpened","inputs":[{"name":"projectID","value":"stp-gas-P7"},{"name":"expectedVersion","value":"12"},{"name":"activityID","value":"C-DM"},{"name":"branch","value":"activity/C-DM"},{"name":"branchRef","value":"refs/heads/activity/C-DM"},{"name":"prRef","value":"https://github.com/mixofreality-studio/stp-fixture/pull/107"},{"name":"crLabel","value":""},{"name":"isRevert","value":"false"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P7-s1-branch-opened"}],"expect":{"result":"13","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=agents only, verdict=sendBack: reopens the judged task as revision n+1, task-kind=dispatch, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP7S1(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP7S1) (any, error) {
	t.Helper()
	seedMain(t, revisionRedispatch(in.ProjectID, in.ExpectedVersion, in.ActivityID))
	return subject.RecordActivityBranchOpened(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Branch, in.BranchRef, in.PrRef, in.CrLabel, in.IsRevert, in.Cred, in.IdempotencyKey)
}

// stepGitActivityStatusAccessExecuteAProjectActivityP7S2 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 2: RecordActivityCIObserved
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"gitactivitystatusaccess","op":"RecordActivityCIObserved","call":"execute-a-project-activity/observe-validate/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"RecordActivityCIObserved","inputs":[{"name":"projectID","value":"stp-gas-P7"},{"name":"expectedVersion","value":"13"},{"name":"activityID","value":"C-DM"},{"name":"ci","value":"1"},{"name":"cred","value":"{\"Bytes\":\"c3RwLWdhcy1yZXBvLXRva2Vu\",\"ExpiresAt\":\"2026-10-03T12:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-gas-P7-s2-ci-success"}],"expect":{"result":"14","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=agents only, verdict=sendBack: reopens the judged task as revision n+1, task-kind=dispatch, exit-met=yes hold, then calls the operation exactly as its caller does and returns its result.
func stepGitActivityStatusAccessExecuteAProjectActivityP7S2(t *testing.T, h *scenariohost.Host, subject projectstate.GitActivityStatusAccess, in inputGitActivityStatusAccessExecuteAProjectActivityP7S2) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.RecordActivityCIObserved(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.ActivityID, in.Ci, in.Cred, in.IdempotencyKey)
}

// newSubjectProjectStateAccess builds the component under test.
// Constructed via the generated constructor in internal/resourceaccess/projectstate, over the scenario's own repo.
func newSubjectProjectStateAccess(t *testing.T, _ *scenariohost.Host) projectstate.ProjectStateAccess {
	t.Helper()
	return projectstate.NewGitLocalProjectStateAccess(newScenarioRepo(t))
}

// stepProjectStateAccessAddAUseCaseToAnInFlightProjectP1S1 — Add a Use Case to an In-Flight Project — [variation] · [no], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"reopen-slot","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"add-a-use-case-to-an-in-flight-project/reopen-slot/2"},"expectedOutputs":null,"expectedStates":[{"node":"use-case-model-amendment-open","object":"Use-case model","inState":"[amendment session open]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-auc-p1\",\"Version\":10,\"CoreUseCases\":{\"Status\":2}}","errorExpected":false},"probes":[{"for":"use-case-model-amendment-open","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p1"}],"expect":{"result":"{\"CoreUseCases\":{\"Status\":2,\"StaleBasis\":false}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges a project in construction with its committed use-case model and downstream artifacts, then reads it.
func stepProjectStateAccessAddAUseCaseToAnInFlightProjectP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAddAUseCaseToAnInFlightProjectP1S1) (any, error) {
	t.Helper()
	seedMain(t, inConstructionWithUseCases(in.ProjectID, 10))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessAddAUseCaseToAnInFlightProjectP1S2 — Add a Use Case to an In-Flight Project — [variation] · [no], step 2: AcknowledgeStaleBasis
// Projected stimulus (JSON): {"seq":2,"node":"reconcile","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"AcknowledgeStaleBasis","call":"add-a-use-case-to-an-in-flight-project/reconcile/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"AcknowledgeStaleBasis","inputs":[{"name":"projectID","value":"stp-psa-auc-p1"},{"name":"expectedVersion","value":"11"},{"name":"kind","value":"5"},{"name":"note","value":"reviewed — unaffected"},{"name":"idempotencyKey","value":"stp-psa-auc-p1:ack:system"}],"expect":{"result":"12","errorExpected":false},"probes":[{"for":"stale-artifacts-reconciled","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p1"}],"expect":{"result":"{\"Version\":12,\"SystemDesign\":{\"Status\":2,\"StaleBasis\":false,\"reviewThread\":[{\"type\":\"staleAck\",\"authorRole\":\"architect\",\"text\":\"Reviewed — unaffected: reviewed — unaffected\"}]}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges what the amendment's re-commit (designSessionAccess.CommitArtifactWithProvenance) left: the use-case model at revision 2 and every committed downstream artifact stale, then acknowledges the system design.
func stepProjectStateAccessAddAUseCaseToAnInFlightProjectP1S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAddAUseCaseToAnInFlightProjectP1S2) (any, error) {
	t.Helper()
	seedMain(t, useCasesRecommitted(in.ProjectID, in.ExpectedVersion))
	return subject.AcknowledgeStaleBasis(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.Kind, in.Note, in.IdempotencyKey)
}

// stepProjectStateAccessAddAUseCaseToAnInFlightProjectP2S1 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"reopen-slot","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"add-a-use-case-to-an-in-flight-project/reopen-slot/2"},"expectedOutputs":null,"expectedStates":[{"node":"use-case-model-amendment-open","object":"Use-case model","inState":"[amendment session open]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p2"}],"expect":{"result":"{\"ID\":\"stp-psa-auc-p2\",\"Version\":10,\"CoreUseCases\":{\"Status\":2}}","errorExpected":false},"probes":[{"for":"use-case-model-amendment-open","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p2"}],"expect":{"result":"{\"CoreUseCases\":{\"Status\":2,\"StaleBasis\":false}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges a project in construction with its committed use-case model and downstream artifacts, then reads it.
func stepProjectStateAccessAddAUseCaseToAnInFlightProjectP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAddAUseCaseToAnInFlightProjectP2S1) (any, error) {
	t.Helper()
	seedMain(t, inConstructionWithUseCases(in.ProjectID, 10))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessAddAUseCaseToAnInFlightProjectP2S2 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 2: AcknowledgeStaleBasis
// Projected stimulus (JSON): {"seq":2,"node":"reconcile","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"AcknowledgeStaleBasis","call":"add-a-use-case-to-an-in-flight-project/reconcile/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"AcknowledgeStaleBasis","inputs":[{"name":"projectID","value":"stp-psa-auc-p2"},{"name":"expectedVersion","value":"11"},{"name":"kind","value":"5"},{"name":"note","value":"reviewed — unaffected"},{"name":"idempotencyKey","value":"stp-psa-auc-p2:ack:system"}],"expect":{"result":"12","errorExpected":false},"probes":[{"for":"stale-artifacts-reconciled","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p2"}],"expect":{"result":"{\"Version\":12,\"SystemDesign\":{\"Status\":2,\"StaleBasis\":false,\"reviewThread\":[{\"type\":\"staleAck\",\"authorRole\":\"architect\",\"text\":\"Reviewed — unaffected: reviewed — unaffected\"}]}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges what the amendment's re-commit (designSessionAccess.CommitArtifactWithProvenance) left: the use-case model at revision 2 and every committed downstream artifact stale, then acknowledges the system design.
func stepProjectStateAccessAddAUseCaseToAnInFlightProjectP2S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAddAUseCaseToAnInFlightProjectP2S2) (any, error) {
	t.Helper()
	seedMain(t, useCasesRecommitted(in.ProjectID, in.ExpectedVersion))
	return subject.AcknowledgeStaleBasis(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.Kind, in.Note, in.IdempotencyKey)
}

// stepProjectStateAccessAddAUseCaseToAnInFlightProjectP2S3 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 3: ReadProject
// Projected stimulus (JSON): {"seq":3,"node":"replan","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"","call":"add-a-use-case-to-an-in-flight-project/replan/0"},"expectedOutputs":null,"expectedStates":[{"node":"replan-trigger-recorded","object":"Scope-change replan trigger","inState":"[recorded]"}],"hints":["Replan is a separate, architect-initiated use case. This path only records the trigger; the architect rebuilds the options and recommends the SDP review later through Replan Under Scope Change (var-replan-scope-change), so do not expect rebuilt options or an SDP-review signal on this path."]}
// Binding (JSON): {"seq":3,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p2"}],"expect":{"result":"{\"ID\":\"stp-psa-auc-p2\",\"Version\":12}","errorExpected":false},"probes":[{"for":"replan-trigger-recorded","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p2"}],"expect":{"result":"{\"ActivityList\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\"}},\"Network\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\"}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Reads the project the acknowledgement left.
func stepProjectStateAccessAddAUseCaseToAnInFlightProjectP2S3(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAddAUseCaseToAnInFlightProjectP2S3) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.ReadProject(callContext(t, h, "3"), in.ProjectID)
}

// stepProjectStateAccessAddAUseCaseToAnInFlightProjectP3S1 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"revalidate","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"add-a-use-case-to-an-in-flight-project/revalidate/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p3"}],"expect":{"result":"{\"ID\":\"stp-psa-auc-p3\",\"Version\":10,\"Phase\":2,\"Volatilities\":{\"Status\":2},\"SystemDesign\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges core-check=[core: rare]: a project in construction with its committed volatilities, use-case model and system design to revalidate, then reads it.
func stepProjectStateAccessAddAUseCaseToAnInFlightProjectP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAddAUseCaseToAnInFlightProjectP3S1) (any, error) {
	t.Helper()
	p := inConstructionWithUseCases(in.ProjectID, 10)
	p.Volatilities = committed(&projectstate.Volatilities{})
	seedMain(t, p)
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessAddAUseCaseToAnInFlightProjectP3S3 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 3: AcknowledgeStaleBasis
// Projected stimulus (JSON): {"seq":3,"node":"reconcile","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"AcknowledgeStaleBasis","call":"add-a-use-case-to-an-in-flight-project/reconcile/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"AcknowledgeStaleBasis","inputs":[{"name":"projectID","value":"stp-psa-auc-p3"},{"name":"expectedVersion","value":"11"},{"name":"kind","value":"5"},{"name":"note","value":"reviewed — unaffected"},{"name":"idempotencyKey","value":"stp-psa-auc-p3:ack:system"}],"expect":{"result":"12","errorExpected":false},"probes":[{"for":"stale-artifacts-reconciled","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p3"}],"expect":{"result":"{\"Version\":12,\"SystemDesign\":{\"Status\":2,\"StaleBasis\":false,\"reviewThread\":[{\"type\":\"staleAck\",\"authorRole\":\"architect\",\"text\":\"Reviewed — unaffected: reviewed — unaffected\"}]}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges what the amendment's re-commit (designSessionAccess.CommitArtifactWithProvenance) left: the use-case model at revision 2 and every committed downstream artifact stale, then acknowledges the system design.
func stepProjectStateAccessAddAUseCaseToAnInFlightProjectP3S3(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAddAUseCaseToAnInFlightProjectP3S3) (any, error) {
	t.Helper()
	seedMain(t, useCasesRecommitted(in.ProjectID, in.ExpectedVersion))
	return subject.AcknowledgeStaleBasis(callContext(t, h, "3"), in.ProjectID, in.ExpectedVersion, in.Kind, in.Note, in.IdempotencyKey)
}

// stepProjectStateAccessAddAUseCaseToAnInFlightProjectP4S1 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"revalidate","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"add-a-use-case-to-an-in-flight-project/revalidate/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p4"}],"expect":{"result":"{\"ID\":\"stp-psa-auc-p4\",\"Version\":10,\"Phase\":2,\"Volatilities\":{\"Status\":2},\"SystemDesign\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges core-check=[core: rare]: a project in construction with its committed volatilities, use-case model and system design to revalidate, then reads it.
func stepProjectStateAccessAddAUseCaseToAnInFlightProjectP4S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAddAUseCaseToAnInFlightProjectP4S1) (any, error) {
	t.Helper()
	p := inConstructionWithUseCases(in.ProjectID, 10)
	p.Volatilities = committed(&projectstate.Volatilities{})
	seedMain(t, p)
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessAddAUseCaseToAnInFlightProjectP4S3 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 3: AcknowledgeStaleBasis
// Projected stimulus (JSON): {"seq":3,"node":"reconcile","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"AcknowledgeStaleBasis","call":"add-a-use-case-to-an-in-flight-project/reconcile/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"AcknowledgeStaleBasis","inputs":[{"name":"projectID","value":"stp-psa-auc-p4"},{"name":"expectedVersion","value":"11"},{"name":"kind","value":"5"},{"name":"note","value":"reviewed — unaffected"},{"name":"idempotencyKey","value":"stp-psa-auc-p4:ack:system"}],"expect":{"result":"12","errorExpected":false},"probes":[{"for":"stale-artifacts-reconciled","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p4"}],"expect":{"result":"{\"Version\":12,\"SystemDesign\":{\"Status\":2,\"StaleBasis\":false,\"reviewThread\":[{\"type\":\"staleAck\",\"authorRole\":\"architect\",\"text\":\"Reviewed — unaffected: reviewed — unaffected\"}]}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges what the amendment's re-commit (designSessionAccess.CommitArtifactWithProvenance) left: the use-case model at revision 2 and every committed downstream artifact stale, then acknowledges the system design.
func stepProjectStateAccessAddAUseCaseToAnInFlightProjectP4S3(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAddAUseCaseToAnInFlightProjectP4S3) (any, error) {
	t.Helper()
	seedMain(t, useCasesRecommitted(in.ProjectID, in.ExpectedVersion))
	return subject.AcknowledgeStaleBasis(callContext(t, h, "3"), in.ProjectID, in.ExpectedVersion, in.Kind, in.Note, in.IdempotencyKey)
}

// stepProjectStateAccessAddAUseCaseToAnInFlightProjectP4S4 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 4: ReadProject
// Projected stimulus (JSON): {"seq":4,"node":"replan","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"","call":"add-a-use-case-to-an-in-flight-project/replan/0"},"expectedOutputs":null,"expectedStates":[{"node":"replan-trigger-recorded","object":"Scope-change replan trigger","inState":"[recorded]"}],"hints":["Replan is a separate, architect-initiated use case. This path only records the trigger; the architect rebuilds the options and recommends the SDP review later through Replan Under Scope Change (var-replan-scope-change), so do not expect rebuilt options or an SDP-review signal on this path."]}
// Binding (JSON): {"seq":4,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p4"}],"expect":{"result":"{\"ID\":\"stp-psa-auc-p4\",\"Version\":12}","errorExpected":false},"probes":[{"for":"replan-trigger-recorded","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-auc-p4"}],"expect":{"result":"{\"ActivityList\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\"}},\"Network\":{\"Status\":2,\"StaleBasis\":true,\"StaleBasisCause\":{\"upstreamKind\":\"coreUseCases\"}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Reads the project the acknowledgement left.
func stepProjectStateAccessAddAUseCaseToAnInFlightProjectP4S4(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAddAUseCaseToAnInFlightProjectP4S4) (any, error) {
	t.Helper()
	// No further arrange: the state the earlier steps left IS this step's precondition.
	return subject.ReadProject(callContext(t, h, "4"), in.ProjectID)
}

// stepProjectStateAccessAskAClarifyingQuestionDuringReviewP1S1 — Ask a Clarifying Question During Review — [yes: session branch] · [yes], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"write-questions","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"ask-a-clarifying-question-during-review/write-questions/4"},"expectedOutputs":null,"expectedStates":null,"hints":["'Live review' means the artifact is staged on an open session branch; a committed artifact (on main only) takes the [no: committed] branch"]}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-acq-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-acq-p1\",\"Version\":6,\"Phase\":0,\"Mission\":{\"Status\":0}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges artifact-state=[yes: session branch] with the reviewer's question answered by the drafting agent, then reads the project.
func stepProjectStateAccessAskAClarifyingQuestionDuringReviewP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAskAClarifyingQuestionDuringReviewP1S1) (any, error) {
	t.Helper()
	seedQuestionOnMission(t, in.ProjectID, 6, false, true)
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessAskAClarifyingQuestionDuringReviewP2S1 — Ask a Clarifying Question During Review — [yes: session branch] · [no: dispatch miss], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"write-questions","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"ask-a-clarifying-question-during-review/write-questions/4"},"expectedOutputs":null,"expectedStates":null,"hints":["'Live review' means the artifact is staged on an open session branch; a committed artifact (on main only) takes the [no: committed] branch"]}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-acq-p2"}],"expect":{"result":"{\"ID\":\"stp-psa-acq-p2\",\"Version\":6,\"Phase\":0,\"Mission\":{\"Status\":0}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges artifact-state=[yes: session branch] with the reviewer's question still unanswered (the answer job never ran), then reads the project.
func stepProjectStateAccessAskAClarifyingQuestionDuringReviewP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAskAClarifyingQuestionDuringReviewP2S1) (any, error) {
	t.Helper()
	seedQuestionOnMission(t, in.ProjectID, 6, false, false)
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessAskAClarifyingQuestionDuringReviewP3S1 — Ask a Clarifying Question During Review — [no: committed] · [yes], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"write-questions","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"ask-a-clarifying-question-during-review/write-questions/4"},"expectedOutputs":null,"expectedStates":null,"hints":["'Live review' means the artifact is staged on an open session branch; a committed artifact (on main only) takes the [no: committed] branch"]}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-acq-p3"}],"expect":{"result":"{\"ID\":\"stp-psa-acq-p3\",\"Version\":6,\"Phase\":0,\"Mission\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges artifact-state=[no: committed] with the reviewer's question answered by the drafting agent, then reads the project.
func stepProjectStateAccessAskAClarifyingQuestionDuringReviewP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAskAClarifyingQuestionDuringReviewP3S1) (any, error) {
	t.Helper()
	seedQuestionOnMission(t, in.ProjectID, 6, true, true)
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessAskAClarifyingQuestionDuringReviewP4S1 — Ask a Clarifying Question During Review — [no: committed] · [no: dispatch miss], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"write-questions","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"ask-a-clarifying-question-during-review/write-questions/4"},"expectedOutputs":null,"expectedStates":null,"hints":["'Live review' means the artifact is staged on an open session branch; a committed artifact (on main only) takes the [no: committed] branch"]}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-acq-p4"}],"expect":{"result":"{\"ID\":\"stp-psa-acq-p4\",\"Version\":6,\"Phase\":0,\"Mission\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges artifact-state=[no: committed] with the reviewer's question still unanswered (the answer job never ran), then reads the project.
func stepProjectStateAccessAskAClarifyingQuestionDuringReviewP4S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessAskAClarifyingQuestionDuringReviewP4S1) (any, error) {
	t.Helper()
	seedQuestionOnMission(t, in.ProjectID, 6, true, false)
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessCommitToAProjectOptionP1S1 — Commit to a Project Option — [commit] · [no], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"await-decision","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"commit-to-a-project-option/await-decision/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-cpo-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-cpo-p1\",\"Version\":20,\"Phase\":1,\"SdpReview\":{\"Status\":1}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the SDP review presented for management's decision in project design, then reads the project.
func stepProjectStateAccessCommitToAProjectOptionP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessCommitToAProjectOptionP1S1) (any, error) {
	t.Helper()
	seedMain(t, sdpAwaitingDecision(in.ProjectID, 20))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessCommitToAProjectOptionP1S2 — Commit to a Project Option — [commit] · [no], step 2: AdvancePhase
// Projected stimulus (JSON): {"seq":2,"node":"commit-option","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"AdvancePhase","call":"commit-to-a-project-option/commit-option/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"AdvancePhase","inputs":[{"name":"projectID","value":"stp-psa-cpo-p1"},{"name":"expectedVersion","value":"21"}],"expect":{"result":"22","errorExpected":false},"probes":[{"for":"sdp-review-committed","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-cpo-p1"}],"expect":{"result":"{\"Phase\":2,\"SdpReview\":{\"Status\":2}}","errorExpected":false}},{"for":"commit-option","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-cpo-p1"}],"expect":{"result":"{\"Phase\":2,\"SdpReview\":{\"Status\":2}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges decision=[commit]: designSessionAccess commits the chosen SDP review on main (the commit-option write before the seal), then advances the phase.
func stepProjectStateAccessCommitToAProjectOptionP1S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessCommitToAProjectOptionP1S2) (any, error) {
	t.Helper()
	commitOnMain(t, h, in.ProjectID, projectstate.KindSdpReview, "founder@stp.test", "project-manager")
	return subject.AdvancePhase(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion)
}

// stepProjectStateAccessCommitToAProjectOptionP2S1 — Commit to a Project Option — [commit] · [yes], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"await-decision","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"commit-to-a-project-option/await-decision/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-cpo-p2"}],"expect":{"result":"{\"ID\":\"stp-psa-cpo-p2\",\"Version\":20,\"Phase\":1,\"SdpReview\":{\"Status\":1}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the SDP review presented for management's decision in project design, then reads the project.
func stepProjectStateAccessCommitToAProjectOptionP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessCommitToAProjectOptionP2S1) (any, error) {
	t.Helper()
	seedMain(t, sdpAwaitingDecision(in.ProjectID, 20))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessCommitToAProjectOptionP2S2 — Commit to a Project Option — [commit] · [yes], step 2: AdvancePhase
// Projected stimulus (JSON): {"seq":2,"node":"commit-option","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"AdvancePhase","call":"commit-to-a-project-option/commit-option/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"AdvancePhase","inputs":[{"name":"projectID","value":"stp-psa-cpo-p2"},{"name":"expectedVersion","value":"21"}],"expect":{"result":"22","errorExpected":false},"probes":[{"for":"sdp-review-committed","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-cpo-p2"}],"expect":{"result":"{\"Phase\":2,\"SdpReview\":{\"Status\":2}}","errorExpected":false}},{"for":"commit-option","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-cpo-p2"}],"expect":{"result":"{\"Phase\":2,\"SdpReview\":{\"Status\":2}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges decision=[commit]: designSessionAccess commits the chosen SDP review on main (the commit-option write before the seal), then advances the phase.
func stepProjectStateAccessCommitToAProjectOptionP2S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessCommitToAProjectOptionP2S2) (any, error) {
	t.Helper()
	commitOnMain(t, h, in.ProjectID, projectstate.KindSdpReview, "founder@stp.test", "project-manager")
	return subject.AdvancePhase(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion)
}

// stepProjectStateAccessCommitToAProjectOptionP2S3 — Commit to a Project Option — [commit] · [yes], step 3: RecordSdpReviewOutcome
// Projected stimulus (JSON): {"seq":3,"node":"revoke","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"RecordSdpReviewOutcome","call":"commit-to-a-project-option/revoke/4"},"expectedOutputs":null,"expectedStates":[{"node":"sdp-review-revoked","object":"SDP Review","inState":"[revoked]"}],"hints":null}
// Binding (JSON): {"seq":3,"operation":"RecordSdpReviewOutcome","inputs":[{"name":"projectID","value":"stp-psa-cpo-p2"},{"name":"expectedVersion","value":"23"},{"name":"outcome","value":"4"},{"name":"feedback","value":"Revoked inside the grace window: the decompressed option is preferred."},{"name":"idempotencyKey","value":"stp-psa-cpo-p2:revoke"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-review-revoked","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-cpo-p2"}],"expect":{"result":"{\"Phase\":1,\"SdpReview\":{\"Status\":4}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the construction work items opened after the commitment (activityExecutionAccess opens the first construction activity, the head version the revoke expects), then revokes the commitment inside its grace window.
func stepProjectStateAccessCommitToAProjectOptionP2S3(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessCommitToAProjectOptionP2S3) (any, error) {
	t.Helper()
	openActivity(t, h, in.ProjectID, "C-project-state-access", projectstate.ActivityTypeService, "service")
	return subject.RecordSdpReviewOutcome(callContext(t, h, "3"), in.ProjectID, in.ExpectedVersion, in.Outcome, in.Feedback, in.IdempotencyKey)
}

// stepDesignSessionAccessCommitToAProjectOptionP1S1 — Commit to a Project Option — [commit] · [no], step 1: CommitArtifactWithProvenance
// Projected stimulus (JSON): {"seq":1,"node":"commit-option","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"CommitArtifactWithProvenance","call":"commit-to-a-project-option/commit-option/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"CommitArtifactWithProvenance","inputs":[{"name":"projectID","value":"stp-dsa-cpo-p1"},{"name":"expectedVersion","value":"20"},{"name":"kind","value":"16"},{"name":"approvedBy","value":"founder@stp.test"},{"name":"draftedBy","value":"project-manager"}],"expect":{"result":"21","errorExpected":false},"probes":[{"for":"commit-option","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-cpo-p1"}],"expect":{"result":"{\"ID\":\"stp-dsa-cpo-p1\",\"SdpReview\":{\"Status\":2,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"project-manager\"}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the SDP review presented for the decision (staged on main), then commits it as the architect's commit-option decision does.
func stepDesignSessionAccessCommitToAProjectOptionP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessCommitToAProjectOptionP1S1) (any, error) {
	t.Helper()
	seedMain(t, sdpAwaitingDecision(in.ProjectID, in.ExpectedVersion))
	return subject.CommitArtifactWithProvenance(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Kind, in.ApprovedBy, in.DraftedBy)
}

// probeDesignSessionAccessCommitToAProjectOptionP1S1N1 — Commit to a Project Option — [commit] · [no], step 1, probe 1: projectStateAccess.ReadProject proves commit-option.
// Probe (JSON): {"for":"commit-option","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-cpo-p1"}],"expect":{"result":"{\"ID\":\"stp-dsa-cpo-p1\",\"SdpReview\":{\"Status\":2,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"project-manager\"}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessCommitToAProjectOptionP1S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-cpo-p1")
}

// stepActivityExecutionAccessCommitToAProjectOptionP1S1 — Commit to a Project Option — [commit] · [no], step 1: OpenActivity
// Projected stimulus (JSON): {"seq":1,"node":"commit-option","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"OpenActivity","call":"commit-to-a-project-option/commit-option/2"},"expectedOutputs":[{"node":"commit-option","kind":"inferred","to":"","label":"Record the commitment; project approved for construction"}],"expectedStates":[{"node":"sdp-review-committed","object":"SDP Review (chosen option + build-cost ceiling)","inState":"[committed]"},{"node":"construction-work-items-opened","object":"Construction Work Items","inState":"[opened]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"OpenActivity","inputs":[{"name":"projectID","value":"stp-aea-cpo-p1"},{"name":"expectedVersion","value":"22"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"C-project-state-access"},{"name":"typ","value":"0"},{"name":"variant","value":"0"},{"name":"pin","value":"{\"typeKey\":\"service\",\"assetsVersion\":\"v0.10.1\"}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-cpo-p1:open"}],"expect":{"errorExpected":false},"probes":[{"for":"commit-option","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aea-cpo-p1"}],"expect":{"result":"{\"ID\":\"stp-aea-cpo-p1\",\"Version\":23,\"Phase\":2}","errorExpected":false}},{"for":"sdp-review-committed","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aea-cpo-p1"}],"expect":{"result":"{\"Phase\":2,\"SdpReview\":{\"Status\":2}}","errorExpected":false}},{"for":"construction-work-items-opened","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-cpo-p1"},{"name":"activityID","value":"C-project-state-access"}],"expect":{"result":"{\"activityID\":\"C-project-state-access\"}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the commitment as delivery records it before the work items open — the SDP review committed through designSessionAccess and the project advanced into construction through projectStateAccess — then opens the first construction activity.
func stepActivityExecutionAccessCommitToAProjectOptionP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessCommitToAProjectOptionP1S1) (any, error) {
	t.Helper()
	seedMain(t, sdpAwaitingDecision(in.ProjectID, 20))
	commitOnMain(t, h, in.ProjectID, projectstate.KindSdpReview, "founder@stp.test", "project-manager")
	if _, err := psaOn(t).AdvancePhase(callContext(t, h, "arrange-advance"), in.ProjectID, headVersion(t, h, in.ProjectID)); err != nil {
		t.Fatalf("arrange: advance %s: %v", in.ProjectID, err)
	}
	return subject.OpenActivity(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.Typ, in.Variant, in.Pin, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessCommitToAProjectOptionP1S1N1 — Commit to a Project Option — [commit] · [no], step 1, probe 1: projectStateAccess.ReadProject proves commit-option.
// Probe (JSON): {"for":"commit-option","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aea-cpo-p1"}],"expect":{"result":"{\"ID\":\"stp-aea-cpo-p1\",\"Version\":23,\"Phase\":2}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeActivityExecutionAccessCommitToAProjectOptionP1S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-aea-cpo-p1")
}

// probeActivityExecutionAccessCommitToAProjectOptionP1S1N2 — Commit to a Project Option — [commit] · [no], step 1, probe 2: projectStateAccess.ReadProject proves sdp-review-committed.
// Probe (JSON): {"for":"sdp-review-committed","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aea-cpo-p1"}],"expect":{"result":"{\"Phase\":2,\"SdpReview\":{\"Status\":2}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeActivityExecutionAccessCommitToAProjectOptionP1S1N2(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-aea-cpo-p1")
}

// stepDesignSessionAccessCommitToAProjectOptionP2S1 — Commit to a Project Option — [commit] · [yes], step 1: CommitArtifactWithProvenance
// Projected stimulus (JSON): {"seq":1,"node":"commit-option","input":{"kind":"call","from":"delivery-manager","to":"designsessionaccess","op":"CommitArtifactWithProvenance","call":"commit-to-a-project-option/commit-option/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"CommitArtifactWithProvenance","inputs":[{"name":"projectID","value":"stp-dsa-cpo-p2"},{"name":"expectedVersion","value":"20"},{"name":"kind","value":"16"},{"name":"approvedBy","value":"founder@stp.test"},{"name":"draftedBy","value":"project-manager"}],"expect":{"result":"21","errorExpected":false},"probes":[{"for":"commit-option","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-cpo-p2"}],"expect":{"result":"{\"ID\":\"stp-dsa-cpo-p2\",\"SdpReview\":{\"Status\":2,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"project-manager\"}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the SDP review presented for the decision (staged on main), then commits it as the architect's commit-option decision does.
func stepDesignSessionAccessCommitToAProjectOptionP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.DesignSessionAccess, in inputDesignSessionAccessCommitToAProjectOptionP2S1) (any, error) {
	t.Helper()
	seedMain(t, sdpAwaitingDecision(in.ProjectID, in.ExpectedVersion))
	return subject.CommitArtifactWithProvenance(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Kind, in.ApprovedBy, in.DraftedBy)
}

// probeDesignSessionAccessCommitToAProjectOptionP2S1N1 — Commit to a Project Option — [commit] · [yes], step 1, probe 1: projectStateAccess.ReadProject proves commit-option.
// Probe (JSON): {"for":"commit-option","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-dsa-cpo-p2"}],"expect":{"result":"{\"ID\":\"stp-dsa-cpo-p2\",\"SdpReview\":{\"Status\":2,\"Provenance\":{\"approvedBy\":\"founder@stp.test\",\"draftedBy\":\"project-manager\"}}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeDesignSessionAccessCommitToAProjectOptionP2S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.DesignSessionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-dsa-cpo-p2")
}

// stepActivityExecutionAccessCommitToAProjectOptionP2S1 — Commit to a Project Option — [commit] · [yes], step 1: OpenActivity
// Projected stimulus (JSON): {"seq":1,"node":"commit-option","input":{"kind":"call","from":"delivery-manager","to":"activityexecutionaccess","op":"OpenActivity","call":"commit-to-a-project-option/commit-option/2"},"expectedOutputs":[{"node":"commit-option","kind":"inferred","to":"","label":"Record the commitment; project approved for construction"}],"expectedStates":[{"node":"sdp-review-committed","object":"SDP Review (chosen option + build-cost ceiling)","inState":"[committed]"},{"node":"construction-work-items-opened","object":"Construction Work Items","inState":"[opened]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"OpenActivity","inputs":[{"name":"projectID","value":"stp-aea-cpo-p2"},{"name":"expectedVersion","value":"22"},{"name":"expectedActivityVersion","value":"0"},{"name":"activityID","value":"C-project-state-access"},{"name":"typ","value":"0"},{"name":"variant","value":"0"},{"name":"pin","value":"{\"typeKey\":\"service\",\"assetsVersion\":\"v0.10.1\"}"},{"name":"cred","value":"{\"Bytes\":\"\",\"ExpiresAt\":\"2027-01-01T00:00:00Z\"}"},{"name":"idempotencyKey","value":"stp-aea-cpo-p2:open"}],"expect":{"errorExpected":false},"probes":[{"for":"commit-option","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aea-cpo-p2"}],"expect":{"result":"{\"ID\":\"stp-aea-cpo-p2\",\"Version\":23,\"Phase\":2}","errorExpected":false}},{"for":"sdp-review-committed","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aea-cpo-p2"}],"expect":{"result":"{\"Phase\":2,\"SdpReview\":{\"Status\":2}}","errorExpected":false}},{"for":"construction-work-items-opened","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-aea-cpo-p2"},{"name":"activityID","value":"C-project-state-access"}],"expect":{"result":"{\"activityID\":\"C-project-state-access\"}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the commitment as delivery records it before the work items open — the SDP review committed through designSessionAccess and the project advanced into construction through projectStateAccess — then opens the first construction activity.
func stepActivityExecutionAccessCommitToAProjectOptionP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, in inputActivityExecutionAccessCommitToAProjectOptionP2S1) (any, error) {
	t.Helper()
	seedMain(t, sdpAwaitingDecision(in.ProjectID, 20))
	commitOnMain(t, h, in.ProjectID, projectstate.KindSdpReview, "founder@stp.test", "project-manager")
	if _, err := psaOn(t).AdvancePhase(callContext(t, h, "arrange-advance"), in.ProjectID, headVersion(t, h, in.ProjectID)); err != nil {
		t.Fatalf("arrange: advance %s: %v", in.ProjectID, err)
	}
	return subject.OpenActivity(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.ExpectedActivityVersion, in.ActivityID, in.Typ, in.Variant, in.Pin, in.Cred, in.IdempotencyKey)
}

// probeActivityExecutionAccessCommitToAProjectOptionP2S1N1 — Commit to a Project Option — [commit] · [yes], step 1, probe 1: projectStateAccess.ReadProject proves commit-option.
// Probe (JSON): {"for":"commit-option","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aea-cpo-p2"}],"expect":{"result":"{\"ID\":\"stp-aea-cpo-p2\",\"Version\":23,\"Phase\":2}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeActivityExecutionAccessCommitToAProjectOptionP2S1N1(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-aea-cpo-p2")
}

// probeActivityExecutionAccessCommitToAProjectOptionP2S1N2 — Commit to a Project Option — [commit] · [yes], step 1, probe 2: projectStateAccess.ReadProject proves sdp-review-committed.
// Probe (JSON): {"for":"sdp-review-committed","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aea-cpo-p2"}],"expect":{"result":"{\"Phase\":2,\"SdpReview\":{\"Status\":2}}","errorExpected":false}}
// Reads projectStateAccess.ReadProject with the probe's inputs over the scenario's own repo.
func probeActivityExecutionAccessCommitToAProjectOptionP2S1N2(t *testing.T, h *scenariohost.Host, _ projectstate.ActivityExecutionAccess) (any, error) {
	t.Helper()
	return readProject(t, h, "stp-aea-cpo-p2")
}

// stepProjectStateAccessCommitToAProjectOptionP3S1 — Commit to a Project Option — [reject-all + feedback], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"await-decision","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"commit-to-a-project-option/await-decision/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-cpo-p3"}],"expect":{"result":"{\"ID\":\"stp-psa-cpo-p3\",\"Version\":20,\"Phase\":1,\"SdpReview\":{\"Status\":1}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges the SDP review presented for management's decision in project design, then reads the project.
func stepProjectStateAccessCommitToAProjectOptionP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessCommitToAProjectOptionP3S1) (any, error) {
	t.Helper()
	seedMain(t, sdpAwaitingDecision(in.ProjectID, 20))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessCommitToAProjectOptionP3S2 — Commit to a Project Option — [reject-all + feedback], step 2: RecordSdpReviewOutcome
// Projected stimulus (JSON): {"seq":2,"node":"reject-all","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"RecordSdpReviewOutcome","call":"commit-to-a-project-option/reject-all/0"},"expectedOutputs":null,"expectedStates":[{"node":"sdp-review-rejected","object":"SDP Review (feedback attached)","inState":"[rejectAll]"}],"hints":null}
// Binding (JSON): {"seq":2,"operation":"RecordSdpReviewOutcome","inputs":[{"name":"projectID","value":"stp-psa-cpo-p3"},{"name":"expectedVersion","value":"24"},{"name":"outcome","value":"3"},{"name":"feedback","value":"Reject all options: the compressed option exceeds the build-cost ceiling."},{"name":"idempotencyKey","value":"stp-psa-cpo-p3:reject-all"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-review-rejected","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-psa-cpo-p3"},{"name":"activityID","value":"P-SDP"}],"expect":{"result":"{\"reviews\":[{\"roundId\":\"P-SDP:sdpReview:sdpReview:1\",\"outcome\":\"sentBack\",\"thread\":[{\"id\":\"r1c1\",\"text\":\"Reject all options: the compressed option exceeds the build-cost ceiling.\"}]}]}","errorExpected":false}},{"for":"sdp-review-rejected","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-cpo-p3"}],"expect":{"result":"{\"Phase\":1,\"SdpReview\":{\"Status\":3,\"reviewThread\":[{\"text\":\"Reject all options: the compressed option exceeds the build-cost ceiling.\",\"type\":\"changeRequest\",\"status\":\"open\"}]}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges decision=[reject-all + feedback] through activityExecutionAccess: the SDP review round is opened, sent back with the reviewer's feedback and decided sentBack, then reads the project.
func stepProjectStateAccessCommitToAProjectOptionP3S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessCommitToAProjectOptionP3S2) (any, error) {
	t.Helper()
	rejectAllOptions(t, h, in.ProjectID)
	return subject.RecordSdpReviewOutcome(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.Outcome, in.Feedback, in.IdempotencyKey)
}

// probeProjectStateAccessCommitToAProjectOptionP3S2N1 — Commit to a Project Option — [reject-all + feedback], step 2, probe 1: activityExecutionAccess.ReadActivityExecution proves sdp-review-rejected.
// Probe (JSON): {"for":"sdp-review-rejected","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-psa-cpo-p3"},{"name":"activityID","value":"P-SDP"}],"expect":{"result":"{\"reviews\":[{\"roundId\":\"P-SDP:sdpReview:sdpReview:1\",\"outcome\":\"sentBack\",\"thread\":[{\"id\":\"r1c1\",\"text\":\"Reject all options: the compressed option exceeds the build-cost ceiling.\"}]}]}","errorExpected":false}}
// Reads activityExecutionAccess.ReadActivityExecution with the probe's inputs over the scenario's own repo.
func probeProjectStateAccessCommitToAProjectOptionP3S2N1(t *testing.T, h *scenariohost.Host, _ projectstate.ProjectStateAccess) (any, error) {
	t.Helper()
	return readExecution(t, h, "stp-psa-cpo-p3", "P-SDP")
}

// stepProjectStateAccessDownloadGeneratedSourceCodeP1S1 — Download Generated Source Code — [clone or pull], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"open-repo","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"download-generated-source-code/open-repo/4"},"expectedOutputs":[{"node":"show-repo-location","kind":"sendSignal","to":"architect-user","label":"Show the owner the adopted repo location and the merged-activity ledger"}],"expectedStates":null,"hints":["Binding convention: the returned location is the adopted repo's provider ref (owner/name) on branch main; the ledger lists only activities whose pull requests are merged. Seed state with at least one merged activity so the output is non-empty"]}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-dgs-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-dgs-p1\",\"Phase\":2,\"ActivityGit\":{\"C-project-state-access\":{\"ActivityID\":\"C-project-state-access\",\"BranchName\":\"activity/C-project-state-access\",\"PullRequestRef\":\"stp-psa-dgs-p1#pr-1\",\"IsRevert\":false}}}","errorExpected":false},"probes":[],"unobservable":["show-repo-location"],"hook":true}
// Arranges a construction activity whose branch and pull request are on record, then reads the project.
func stepProjectStateAccessDownloadGeneratedSourceCodeP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessDownloadGeneratedSourceCodeP1S1) (any, error) {
	t.Helper()
	seedMain(t, constructionWithBranch(in.ProjectID, 30, "C-project-state-access", string(in.ProjectID)+"#pr-1"))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessDownloadGeneratedSourceCodeP2S1 — Download Generated Source Code — [archive], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"open-repo","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"download-generated-source-code/open-repo/4"},"expectedOutputs":[{"node":"show-repo-location","kind":"sendSignal","to":"architect-user","label":"Show the owner the adopted repo location and the merged-activity ledger"}],"expectedStates":null,"hints":["Binding convention: the returned location is the adopted repo's provider ref (owner/name) on branch main; the ledger lists only activities whose pull requests are merged. Seed state with at least one merged activity so the output is non-empty"]}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-dgs-p2"}],"expect":{"result":"{\"ID\":\"stp-psa-dgs-p2\",\"Phase\":2,\"ActivityGit\":{\"C-project-state-access\":{\"ActivityID\":\"C-project-state-access\",\"BranchName\":\"activity/C-project-state-access\",\"PullRequestRef\":\"stp-psa-dgs-p2#pr-1\",\"IsRevert\":false}}}","errorExpected":false},"probes":[],"unobservable":["show-repo-location"],"hook":true}
// Arranges a construction activity whose branch and pull request are on record, then reads the project.
func stepProjectStateAccessDownloadGeneratedSourceCodeP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessDownloadGeneratedSourceCodeP2S1) (any, error) {
	t.Helper()
	seedMain(t, constructionWithBranch(in.ProjectID, 30, "C-project-state-access", string(in.ProjectID)+"#pr-1"))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessExecuteAProjectActivityP1S1 — Execute a Project Activity — dispatch · yes, step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"execute-a-project-activity/activity-eligible/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-epa-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-epa-p1\",\"Version\":4,\"Phase\":0,\"ActivityList\":{\"Status\":2},\"Network\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a born project whose committed design-prefix plan makes its first activity eligible, then reads it.
func stepProjectStateAccessExecuteAProjectActivityP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessExecuteAProjectActivityP1S1) (any, error) {
	t.Helper()
	seedMain(t, plannedDesignPrefix(in.ProjectID, 4))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessExecuteAProjectActivityP2S1 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"execute-a-project-activity/activity-eligible/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-epa-p2"}],"expect":{"result":"{\"ID\":\"stp-psa-epa-p2\",\"Version\":4,\"Phase\":0,\"ActivityList\":{\"Status\":2},\"Network\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a born project whose committed design-prefix plan makes its first activity eligible, then reads it.
func stepProjectStateAccessExecuteAProjectActivityP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessExecuteAProjectActivityP2S1) (any, error) {
	t.Helper()
	seedMain(t, plannedDesignPrefix(in.ProjectID, 4))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessExecuteAProjectActivityP2S2 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 2: ReadProject
// Projected stimulus (JSON): {"seq":2,"node":"operator-intervened","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"execute-a-project-activity/operator-intervened/0","guard":"yes"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-epa-p2"}],"expect":{"result":"{\"ActivityExecution\":{\"D-REQ\":{\"activityID\":\"D-REQ\",\"operatorNotes\":[{\"kind\":2,\"text\":\"Narrow the glossary to the billing terms and retry.\"}]}}}","errorExpected":false},"probes":[{"for":"operator-note-recorded","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-psa-epa-p2"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"activityID\":\"D-REQ\",\"operatorNotes\":[{\"kind\":2,\"text\":\"Narrow the glossary to the billing terms and retry.\"}]}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges operator-intervened=yes through activityExecutionAccess: the activity is open and the operator's retry note is recorded on it, then reads the project.
func stepProjectStateAccessExecuteAProjectActivityP2S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessExecuteAProjectActivityP2S2) (any, error) {
	t.Helper()
	openActivity(t, h, in.ProjectID, "D-REQ", projectstate.ActivityTypeRequirements, "requirements")
	recordNote(t, h, in.ProjectID, "D-REQ", retryNote)
	return subject.ReadProject(callContext(t, h, "2"), in.ProjectID)
}

// probeProjectStateAccessExecuteAProjectActivityP2S2N1 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 2, probe 1: activityExecutionAccess.ReadActivityExecution proves operator-note-recorded.
// Probe (JSON): {"for":"operator-note-recorded","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-psa-epa-p2"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"activityID\":\"D-REQ\",\"operatorNotes\":[{\"kind\":2,\"text\":\"Narrow the glossary to the billing terms and retry.\"}]}","errorExpected":false}}
// Reads activityExecutionAccess.ReadActivityExecution with the probe's inputs over the scenario's own repo.
func probeProjectStateAccessExecuteAProjectActivityP2S2N1(t *testing.T, h *scenariohost.Host, _ projectstate.ProjectStateAccess) (any, error) {
	t.Helper()
	return readExecution(t, h, "stp-psa-epa-p2", "D-REQ")
}

// stepProjectStateAccessExecuteAProjectActivityP3S1 — Execute a Project Activity — review · agents only · approve, step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"execute-a-project-activity/activity-eligible/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-epa-p3"}],"expect":{"result":"{\"ID\":\"stp-psa-epa-p3\",\"Version\":4,\"Phase\":0,\"ActivityList\":{\"Status\":2},\"Network\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a born project whose committed design-prefix plan makes its first activity eligible, then reads it.
func stepProjectStateAccessExecuteAProjectActivityP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessExecuteAProjectActivityP3S1) (any, error) {
	t.Helper()
	seedMain(t, plannedDesignPrefix(in.ProjectID, 4))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessExecuteAProjectActivityP3S2 — Execute a Project Activity — review · agents only · approve, step 2: AdvancePhase
// Projected stimulus (JSON): {"seq":2,"node":"seal-project-phase","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"AdvancePhase","call":"execute-a-project-activity/seal-project-phase/0"},"expectedOutputs":null,"expectedStates":[{"node":"project-phase-advanced","object":"Project","inState":"[phase advanced]"}],"hints":null}
// Binding (JSON): {"seq":2,"operation":"AdvancePhase","inputs":[{"name":"projectID","value":"stp-psa-epa-p3"},{"name":"expectedVersion","value":"15"}],"expect":{"result":"16","errorExpected":false},"probes":[{"for":"project-phase-advanced","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-epa-p3"}],"expect":{"result":"{\"Version\":16,\"Phase\":1}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the head the approved review left (the walk's writes brought it to the binding's version) in system design, then seals the phase.
func stepProjectStateAccessExecuteAProjectActivityP3S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessExecuteAProjectActivityP3S2) (any, error) {
	t.Helper()
	seedMain(t, plannedDesignPrefix(in.ProjectID, in.ExpectedVersion))
	return subject.AdvancePhase(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion)
}

// stepProjectStateAccessExecuteAProjectActivityP4S1 — Execute a Project Activity — review · human required · approve, step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"execute-a-project-activity/activity-eligible/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-epa-p4"}],"expect":{"result":"{\"ID\":\"stp-psa-epa-p4\",\"Version\":4,\"Phase\":0,\"ActivityList\":{\"Status\":2},\"Network\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a born project whose committed design-prefix plan makes its first activity eligible, then reads it.
func stepProjectStateAccessExecuteAProjectActivityP4S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessExecuteAProjectActivityP4S1) (any, error) {
	t.Helper()
	seedMain(t, plannedDesignPrefix(in.ProjectID, 4))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessExecuteAProjectActivityP4S2 — Execute a Project Activity — review · human required · approve, step 2: AdvancePhase
// Projected stimulus (JSON): {"seq":2,"node":"seal-project-phase","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"AdvancePhase","call":"execute-a-project-activity/seal-project-phase/0"},"expectedOutputs":null,"expectedStates":[{"node":"project-phase-advanced","object":"Project","inState":"[phase advanced]"}],"hints":null}
// Binding (JSON): {"seq":2,"operation":"AdvancePhase","inputs":[{"name":"projectID","value":"stp-psa-epa-p4"},{"name":"expectedVersion","value":"15"}],"expect":{"result":"16","errorExpected":false},"probes":[{"for":"project-phase-advanced","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-epa-p4"}],"expect":{"result":"{\"Version\":16,\"Phase\":1}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the head the approved review left (the walk's writes brought it to the binding's version) in system design, then seals the phase.
func stepProjectStateAccessExecuteAProjectActivityP4S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessExecuteAProjectActivityP4S2) (any, error) {
	t.Helper()
	seedMain(t, plannedDesignPrefix(in.ProjectID, in.ExpectedVersion))
	return subject.AdvancePhase(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion)
}

// stepProjectStateAccessExecuteAProjectActivityP5S1 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"execute-a-project-activity/activity-eligible/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-epa-p5"}],"expect":{"result":"{\"ID\":\"stp-psa-epa-p5\",\"Version\":4,\"Phase\":0,\"ActivityList\":{\"Status\":2},\"Network\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a born project whose committed design-prefix plan makes its first activity eligible, then reads it.
func stepProjectStateAccessExecuteAProjectActivityP5S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessExecuteAProjectActivityP5S1) (any, error) {
	t.Helper()
	seedMain(t, plannedDesignPrefix(in.ProjectID, 4))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessExecuteAProjectActivityP6S1 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"execute-a-project-activity/activity-eligible/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-epa-p6"}],"expect":{"result":"{\"ID\":\"stp-psa-epa-p6\",\"Version\":4,\"Phase\":0,\"ActivityList\":{\"Status\":2},\"Network\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a born project whose committed design-prefix plan makes its first activity eligible, then reads it.
func stepProjectStateAccessExecuteAProjectActivityP6S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessExecuteAProjectActivityP6S1) (any, error) {
	t.Helper()
	seedMain(t, plannedDesignPrefix(in.ProjectID, 4))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessExecuteAProjectActivityP6S2 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 2: ReadProject
// Projected stimulus (JSON): {"seq":2,"node":"operator-intervened","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"execute-a-project-activity/operator-intervened/0","guard":"no"},"expectedOutputs":[{"node":"escalate-operator","kind":"sendSignal","to":"operator","label":"Escalate to the operator: the activity's retries are exhausted and it needs a steer"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-epa-p6"}],"expect":{"result":"{\"ActivityExecution\":{\"D-REQ\":{\"activityID\":\"D-REQ\"}}}","errorExpected":false},"probes":[],"unobservable":["escalate-operator"],"hook":true}
// Arranges operator-intervened=no: activityExecutionAccess has the activity open and no operator note is on it yet, then reads the project.
func stepProjectStateAccessExecuteAProjectActivityP6S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessExecuteAProjectActivityP6S2) (any, error) {
	t.Helper()
	openActivity(t, h, in.ProjectID, "D-REQ", projectstate.ActivityTypeRequirements, "requirements")
	return subject.ReadProject(callContext(t, h, "2"), in.ProjectID)
}

// stepProjectStateAccessExecuteAProjectActivityP6S3 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 3: ReadProject
// Projected stimulus (JSON): {"seq":3,"node":"operator-intervened","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"execute-a-project-activity/operator-intervened/0","guard":"yes"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-epa-p6"}],"expect":{"result":"{\"ActivityExecution\":{\"D-REQ\":{\"activityID\":\"D-REQ\",\"operatorNotes\":[{\"kind\":2,\"text\":\"Narrow the glossary to the billing terms and retry.\"}]}}}","errorExpected":false},"probes":[{"for":"operator-note-recorded","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-psa-epa-p6"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"operatorNotes\":[{\"kind\":2,\"text\":\"Narrow the glossary to the billing terms and retry.\"}]}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges operator-intervened=yes: activityExecutionAccess records the operator's retry note after the escalation, then reads the project.
func stepProjectStateAccessExecuteAProjectActivityP6S3(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessExecuteAProjectActivityP6S3) (any, error) {
	t.Helper()
	recordNote(t, h, in.ProjectID, "D-REQ", retryNote)
	return subject.ReadProject(callContext(t, h, "3"), in.ProjectID)
}

// probeProjectStateAccessExecuteAProjectActivityP6S3N1 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 3, probe 1: activityExecutionAccess.ReadActivityExecution proves operator-note-recorded.
// Probe (JSON): {"for":"operator-note-recorded","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-psa-epa-p6"},{"name":"activityID","value":"D-REQ"}],"expect":{"result":"{\"operatorNotes\":[{\"kind\":2,\"text\":\"Narrow the glossary to the billing terms and retry.\"}]}","errorExpected":false}}
// Reads activityExecutionAccess.ReadActivityExecution with the probe's inputs over the scenario's own repo.
func probeProjectStateAccessExecuteAProjectActivityP6S3N1(t *testing.T, h *scenariohost.Host, _ projectstate.ProjectStateAccess) (any, error) {
	t.Helper()
	return readExecution(t, h, "stp-psa-epa-p6", "D-REQ")
}

// stepProjectStateAccessExecuteAProjectActivityP7S1 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"activity-eligible","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"execute-a-project-activity/activity-eligible/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-epa-p7"}],"expect":{"result":"{\"ID\":\"stp-psa-epa-p7\",\"Version\":4,\"Phase\":0,\"ActivityList\":{\"Status\":2},\"Network\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a born project whose committed design-prefix plan makes its first activity eligible, then reads it.
func stepProjectStateAccessExecuteAProjectActivityP7S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessExecuteAProjectActivityP7S1) (any, error) {
	t.Helper()
	seedMain(t, plannedDesignPrefix(in.ProjectID, 4))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessManageProjectsP1S1 — Manage Projects — [no: self-operated], step 1: CreateProject
// Projected stimulus (JSON): {"seq":1,"node":"create-head-state","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"CreateProject","call":"manage-projects/create-head-state/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"CreateProject","inputs":[{"name":"projectID","value":"stp-psa-mp-p1"},{"name":"owner","value":"stp-psa-owner-mp-p1"},{"name":"name","value":"STP Manage Projects P1"}],"expect":{"result":"1","errorExpected":false},"probes":[{"for":"obj-project-head-state-created","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-mp-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-mp-p1\",\"Version\":1,\"Phase\":0,\"Owner\":\"stp-psa-owner-mp-p1\",\"Name\":\"STP Manage Projects P1\",\"OperatingModel\":\"selfOperated\"}","errorExpected":false}}],"unobservable":[],"hook":true}
// Creates the project in the scenario's empty repo.
func stepProjectStateAccessManageProjectsP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessManageProjectsP1S1) (any, error) {
	t.Helper()
	// No arrange: create-head-state runs against the scenario's empty repo.
	return subject.CreateProject(callContext(t, h, "1"), in.ProjectID, in.Owner, in.Name)
}

// stepProjectStateAccessManageProjectsP2S1 — Manage Projects — [yes], step 1: CreateProject
// Projected stimulus (JSON): {"seq":1,"node":"create-head-state","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"CreateProject","call":"manage-projects/create-head-state/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"CreateProject","inputs":[{"name":"projectID","value":"stp-psa-mp-p2"},{"name":"owner","value":"stp-psa-owner-mp-p2"},{"name":"name","value":"STP Manage Projects P2"}],"expect":{"result":"1","errorExpected":false},"probes":[{"for":"obj-project-head-state-created","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-mp-p2"}],"expect":{"result":"{\"ID\":\"stp-psa-mp-p2\",\"Version\":1,\"Phase\":0,\"Owner\":\"stp-psa-owner-mp-p2\",\"Name\":\"STP Manage Projects P2\",\"OperatingModel\":\"selfOperated\"}","errorExpected":false}}],"unobservable":[],"hook":true}
// Creates the project in the scenario's empty repo.
func stepProjectStateAccessManageProjectsP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessManageProjectsP2S1) (any, error) {
	t.Helper()
	// No arrange: create-head-state runs against the scenario's empty repo.
	return subject.CreateProject(callContext(t, h, "1"), in.ProjectID, in.Owner, in.Name)
}

// stepProjectStateAccessOnboardANewCustomerP2S1 — Onboard a New Customer — [no], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"register-customer","input":{"kind":"call","from":"billing-manager","to":"projectstateaccess","op":"ReadProject","call":"onboard-a-new-customer/register-customer/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-onb-p2"}],"expect":{"result":"{\"ID\":\"stp-psa-onb-p2\",\"SdpReview\":{\"Status\":2},\"PlanningAssumptions\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a project whose SDP option is committed under planning assumptions carrying
// its billing terms, then reads it as billing does when a customer registers for it.
func stepProjectStateAccessOnboardANewCustomerP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessOnboardANewCustomerP2S1) (any, error) {
	t.Helper()
	seedMain(t, committedCommitment(in.ProjectID, 40))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessOperateADeliveredSystemP1S1 — Operate a Delivered System, step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"render-manifests-a","input":{"kind":"call","from":"operations-manager","to":"projectstateaccess","op":"ReadProject","call":"operate-a-delivered-system/render-manifests-a/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-ods-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-ods-p1\",\"Version\":40,\"Phase\":2,\"OperationalConcepts\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a delivered project with its committed deployment & operations model, then reads it.
func stepProjectStateAccessOperateADeliveredSystemP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessOperateADeliveredSystemP1S1) (any, error) {
	t.Helper()
	seedMain(t, deliveredSystem(in.ProjectID, 40))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessOperateADeliveredSystemP3S1 — Operate a Delivered System — [no] · [no: scale/pause/resume], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"render-manifests-c","input":{"kind":"call","from":"operations-manager","to":"projectstateaccess","op":"ReadProject","call":"operate-a-delivered-system/render-manifests-c/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-ods-p3"}],"expect":{"result":"{\"ID\":\"stp-psa-ods-p3\",\"Version\":40,\"Phase\":2,\"OperationalConcepts\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a delivered project with its committed deployment & operations model, then reads it for an autoscale re-render.
func stepProjectStateAccessOperateADeliveredSystemP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessOperateADeliveredSystemP3S1) (any, error) {
	t.Helper()
	seedMain(t, deliveredSystem(in.ProjectID, 40))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessOperateADeliveredSystemP5S1 — Operate a Delivered System — [yes] · [no: scale/pause/resume], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"render-manifests-c","input":{"kind":"call","from":"operations-manager","to":"projectstateaccess","op":"ReadProject","call":"operate-a-delivered-system/render-manifests-c/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-ods-p5"}],"expect":{"result":"{\"ID\":\"stp-psa-ods-p5\",\"Version\":40,\"Phase\":2,\"OperationalConcepts\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a delivered project with its committed deployment & operations model, then reads it for an autoscale re-render.
func stepProjectStateAccessOperateADeliveredSystemP5S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessOperateADeliveredSystemP5S1) (any, error) {
	t.Helper()
	seedMain(t, deliveredSystem(in.ProjectID, 40))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessPauseConstructionP1S1 — Pause Construction, step 1: RecordOperatorPaused
// Projected stimulus (JSON): {"seq":1,"node":"record-paused","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"RecordOperatorPaused","call":"pause-construction/record-paused/0"},"expectedOutputs":[{"node":"record-paused","kind":"inferred","to":"","label":"Record that construction is paused, with the reason"}],"expectedStates":[{"node":"run-state-paused","object":"Project run state","inState":"[paused]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"RecordOperatorPaused","inputs":[{"name":"projectID","value":"stp-psa-pc-p1"},{"name":"expectedVersion","value":"8"},{"name":"reason","value":"Operator pause: upstream incident under investigation"},{"name":"idempotencyKey","value":"stp-psa-pc-p1:pause"}],"expect":{"result":"9","errorExpected":false},"probes":[{"for":"run-state-paused","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-pc-p1"}],"expect":{"result":"{\"OperatorPaused\":true,\"PauseReason\":\"Operator pause: upstream incident under investigation\"}","errorExpected":false}},{"for":"record-paused","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-pc-p1"}],"expect":{"result":"{\"Version\":9,\"Phase\":2,\"OperatorPaused\":true,\"PauseReason\":\"Operator pause: upstream incident under investigation\"}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges a running project in construction at the binding's version, then pauses it.
func stepProjectStateAccessPauseConstructionP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessPauseConstructionP1S1) (any, error) {
	t.Helper()
	seedMain(t, baseProject(in.ProjectID, in.ExpectedVersion, projectstate.PhaseConstruction))
	return subject.RecordOperatorPaused(callContext(t, h, "1"), in.ProjectID, in.ExpectedVersion, in.Reason, in.IdempotencyKey)
}

// stepProjectStateAccessReplanUnderScopeChangeP1S1 — Replan Under Scope Change — [yes], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"snapshot","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"replan-under-scope-change/snapshot/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-rsc-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-rsc-p1\",\"Version\":50,\"Phase\":2,\"ActivityExecution\":{\"C-project-state-access\":{\"activityID\":\"C-project-state-access\"}}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a project in construction with a construction activity under way, then snapshots it.
func stepProjectStateAccessReplanUnderScopeChangeP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessReplanUnderScopeChangeP1S1) (any, error) {
	t.Helper()
	seedMain(t, constructionUnderway(in.ProjectID, 50))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessReplanUnderScopeChangeP2S1 — Replan Under Scope Change — [no: scope change] · [accept], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"snapshot","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"replan-under-scope-change/snapshot/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-rsc-p2"}],"expect":{"result":"{\"ID\":\"stp-psa-rsc-p2\",\"Version\":50,\"Phase\":2,\"ActivityExecution\":{\"C-project-state-access\":{\"activityID\":\"C-project-state-access\"}}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a project in construction with a construction activity under way, then snapshots it.
func stepProjectStateAccessReplanUnderScopeChangeP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessReplanUnderScopeChangeP2S1) (any, error) {
	t.Helper()
	seedMain(t, constructionUnderway(in.ProjectID, 50))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessReplanUnderScopeChangeP3S1 — Replan Under Scope Change — [no: scope change] · [reject], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"snapshot","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"replan-under-scope-change/snapshot/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-rsc-p3"}],"expect":{"result":"{\"ID\":\"stp-psa-rsc-p3\",\"Version\":50,\"Phase\":2,\"ActivityExecution\":{\"C-project-state-access\":{\"activityID\":\"C-project-state-access\"}}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a project in construction with a construction activity under way, then snapshots it.
func stepProjectStateAccessReplanUnderScopeChangeP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessReplanUnderScopeChangeP3S1) (any, error) {
	t.Helper()
	seedMain(t, constructionUnderway(in.ProjectID, 50))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessRequeueAFailedConstructionActivityP1S1 — Requeue a Failed Construction Activity, step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"record-reopen","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"requeue-a-failed-construction-activity/record-reopen/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-rq-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-rq-p1\",\"Phase\":2,\"ActivityExecution\":{\"C-project-state-access\":{\"activityID\":\"C-project-state-access\",\"failureDetail\":\"pipeline failed: build red\"}}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a construction activity that exited failed (pipeline red), then reads the project.
func stepProjectStateAccessRequeueAFailedConstructionActivityP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessRequeueAFailedConstructionActivityP1S1) (any, error) {
	t.Helper()
	seedMain(t, failedConstruction(in.ProjectID, 30))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessRequeueAFailedConstructionActivityP1S2 — Requeue a Failed Construction Activity, step 2: ReadProject
// Projected stimulus (JSON): {"seq":2,"node":"pump-fires","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"requeue-a-failed-construction-activity/pump-fires/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-rq-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-rq-p1\",\"Phase\":2,\"OperatorPaused\":false,\"ActivityExecution\":{\"C-project-state-access\":{\"activityID\":\"C-project-state-access\",\"operatorNotes\":[{\"kind\":6,\"gate\":\"reopen\",\"text\":\"Re-queue after the upstream fix landed.\"}]}}}","errorExpected":false},"probes":[{"for":"","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-psa-rq-p1"},{"name":"activityID","value":"C-project-state-access"}],"expect":{"result":"{\"activityID\":\"C-project-state-access\",\"operatorNotes\":[{\"kind\":6,\"gate\":\"reopen\",\"text\":\"Re-queue after the upstream fix landed.\"}]}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the operator's re-queue: activityExecutionAccess records the requeue note (which re-arms the failed row), then reads the project the pump fires on.
func stepProjectStateAccessRequeueAFailedConstructionActivityP1S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessRequeueAFailedConstructionActivityP1S2) (any, error) {
	t.Helper()
	recordNote(t, h, in.ProjectID, "C-project-state-access", requeueNote)
	return subject.ReadProject(callContext(t, h, "2"), in.ProjectID)
}

// probeProjectStateAccessRequeueAFailedConstructionActivityP1S2N1 — Requeue a Failed Construction Activity, step 2, probe 1: activityExecutionAccess.ReadActivityExecution proves .
// Probe (JSON): {"for":"","component":"activityExecutionAccess","operation":"ReadActivityExecution","inputs":[{"name":"projectID","value":"stp-psa-rq-p1"},{"name":"activityID","value":"C-project-state-access"}],"expect":{"result":"{\"activityID\":\"C-project-state-access\",\"operatorNotes\":[{\"kind\":6,\"gate\":\"reopen\",\"text\":\"Re-queue after the upstream fix landed.\"}]}","errorExpected":false}}
// Reads activityExecutionAccess.ReadActivityExecution with the probe's inputs over the scenario's own repo.
func probeProjectStateAccessRequeueAFailedConstructionActivityP1S2N1(t *testing.T, h *scenariohost.Host, _ projectstate.ProjectStateAccess) (any, error) {
	t.Helper()
	return readExecution(t, h, "stp-psa-rq-p1", "C-project-state-access")
}

// stepProjectStateAccessResumePausedConstructionP1S1 — Resume Paused Construction — [yes], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"pause-settled","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"resume-paused-construction/pause-settled/0","guard":"[yes]"},"expectedOutputs":null,"expectedStates":null,"hints":["'Fully applied' is read from readProject (phase + operatorPaused); the [no] branch needs the binding to arrange a pause still in flight, so this decision is internal and the step is a Hook candidate."]}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-rs-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-rs-p1\",\"Version\":9,\"Phase\":2,\"OperatorPaused\":true,\"PauseReason\":\"Operator pause: upstream incident under investigation\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges a paused project in construction, then reads it.
func stepProjectStateAccessResumePausedConstructionP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessResumePausedConstructionP1S1) (any, error) {
	t.Helper()
	seedMain(t, pausedConstruction(in.ProjectID, 9))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessResumePausedConstructionP2S1 — Resume Paused Construction — [no], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"pause-settled","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"resume-paused-construction/pause-settled/0","guard":"[no]"},"expectedOutputs":[{"node":"retry-later","kind":"sendSignal","to":"operator","label":"Tell the operator the pause is still being applied; the resume can be retried"}],"expectedStates":[{"node":"run-state-still-paused","object":"Project run state","inState":"[paused]"}],"hints":["'Fully applied' is read from readProject (phase + operatorPaused); the [no] branch needs the binding to arrange a pause still in flight, so this decision is internal and the step is a Hook candidate."]}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-rs-p2"}],"expect":{"result":"{\"ID\":\"stp-psa-rs-p2\",\"Version\":9,\"Phase\":2,\"OperatorPaused\":true}","errorExpected":false},"probes":[{"for":"run-state-still-paused","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-rs-p2"}],"expect":{"result":"{\"Version\":9,\"OperatorPaused\":true,\"PauseReason\":\"Operator pause: upstream incident under investigation\"}","errorExpected":false}}],"unobservable":["retry-later"],"hook":true}
// Arranges a paused project in construction, then reads it.
func stepProjectStateAccessResumePausedConstructionP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessResumePausedConstructionP2S1) (any, error) {
	t.Helper()
	seedMain(t, pausedConstruction(in.ProjectID, 9))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessTrackWeeklyProjectProgressP1S1 — Track Weekly Project Progress — [no] · [yes], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"read-exits","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"track-weekly-project-progress/read-exits/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-twp-p1\",\"Version\":60,\"Phase\":2,\"ConstructionProgress\":{\"Week\":3,\"points\":[{\"week\":1},{\"week\":2}]}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges week 3 of construction with 2 tracking points on record, then reads the exits.
func stepProjectStateAccessTrackWeeklyProjectProgressP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessTrackWeeklyProjectProgressP1S1) (any, error) {
	t.Helper()
	seedMain(t, trackedConstruction(in.ProjectID, 60, 3, 2))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessTrackWeeklyProjectProgressP1S2 — Track Weekly Project Progress — [no] · [yes], step 2: RecordTrackingPoint
// Projected stimulus (JSON): {"seq":2,"node":"no-trend","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"RecordTrackingPoint","call":"track-weekly-project-progress/no-trend/0"},"expectedOutputs":null,"expectedStates":[{"node":"tracking-point-no-trend","object":"Tracking point","inState":"[recorded without trend]"}],"hints":null}
// Binding (JSON): {"seq":2,"operation":"RecordTrackingPoint","inputs":[{"name":"projectID","value":"stp-psa-twp-p1"},{"name":"expectedVersion","value":"60"},{"name":"point","value":"{\"week\":3,\"earnedPct\":0.25,\"plannedPct\":0.3,\"note\":\"no trend: fewer than four points\"}"},{"name":"idempotencyKey","value":"stp-psa-twp-p1:point:2"}],"expect":{"errorExpected":false},"probes":[{"for":"tracking-point-no-trend","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p1"}],"expect":{"result":"{\"ConstructionProgress\":{\"points\":[{\"week\":1},{\"week\":2},{\"week\":3,\"note\":\"no trend: fewer than four points\"}]}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Reads the project; nothing is arranged, because no operation of this unit records a tracking point or a replan trigger.
func stepProjectStateAccessTrackWeeklyProjectProgressP1S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessTrackWeeklyProjectProgressP1S2) (any, error) {
	t.Helper()
	// No arrange: no operation of this unit records a tracking point or a replan
	// trigger (see the unit's blockers) — the read returns what is on record.
	return subject.RecordTrackingPoint(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.Point, in.IdempotencyKey)
}

// stepProjectStateAccessTrackWeeklyProjectProgressP1S3 — Track Weekly Project Progress — [no] · [yes], step 3: RecordTrackingPoint
// Projected stimulus (JSON): {"seq":3,"node":"record-point","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"RecordTrackingPoint","call":"track-weekly-project-progress/record-point/0"},"expectedOutputs":[{"node":"record-point","kind":"inferred","to":"","label":"Append the tracking point to the durable progress record"}],"expectedStates":[{"node":"progress-record-appended","object":"Progress record","inState":"[point appended]"}],"hints":null}
// Binding (JSON): {"seq":3,"operation":"RecordTrackingPoint","inputs":[{"name":"projectID","value":"stp-psa-twp-p1"},{"name":"expectedVersion","value":"61"},{"name":"point","value":"{\"week\":3,\"earnedPct\":0.25,\"plannedPct\":0.3,\"note\":\"no trend: fewer than four points\"}"},{"name":"idempotencyKey","value":"stp-psa-twp-p1:point:3"}],"expect":{"errorExpected":false},"probes":[{"for":"progress-record-appended","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p1"}],"expect":{"result":"{\"ConstructionProgress\":{\"points\":[{\"week\":1},{\"week\":2},{\"week\":3}]}}","errorExpected":false}},{"for":"record-point","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p1"}],"expect":{"result":"{\"ConstructionProgress\":{\"Week\":3,\"points\":[{\"week\":1},{\"week\":2},{\"week\":3}]}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Reads the project; nothing is arranged, because no operation of this unit records a tracking point or a replan trigger.
func stepProjectStateAccessTrackWeeklyProjectProgressP1S3(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessTrackWeeklyProjectProgressP1S3) (any, error) {
	t.Helper()
	// No arrange: no operation of this unit records a tracking point or a replan
	// trigger (see the unit's blockers) — the read returns what is on record.
	return subject.RecordTrackingPoint(callContext(t, h, "3"), in.ProjectID, in.ExpectedVersion, in.Point, in.IdempotencyKey)
}

// stepProjectStateAccessTrackWeeklyProjectProgressP1S4 — Track Weekly Project Progress — [no] · [yes], step 4: RecordTrackingPoint
// Projected stimulus (JSON): {"seq":4,"node":"corrective","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"RecordTrackingPoint","call":"track-weekly-project-progress/corrective/0"},"expectedOutputs":[{"node":"show-weekly-progress","kind":"sendSignal","to":"architect-user","label":"Show the week's progress (EV, EAC, trend) to the Architect User"}],"expectedStates":[{"node":"replan-trigger-pending","object":"Replan trigger","inState":"[pending]"}],"hints":null}
// Binding (JSON): {"seq":4,"operation":"RecordTrackingPoint","inputs":[{"name":"projectID","value":"stp-psa-twp-p1"},{"name":"expectedVersion","value":"62"},{"name":"point","value":"{\"week\":3,\"earnedPct\":0.25,\"plannedPct\":0.3,\"note\":\"corrective action: replan trigger pending\"}"},{"name":"idempotencyKey","value":"stp-psa-twp-p1:point:4"}],"expect":{"errorExpected":false},"probes":[{"for":"replan-trigger-pending","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p1"}],"expect":{"result":"{\"ConstructionProgress\":{\"points\":[{\"week\":1},{\"week\":2},{\"week\":3,\"note\":\"corrective action: replan trigger pending\"}]}}","errorExpected":false}}],"unobservable":["show-weekly-progress"],"hook":true}
// Reads the project; nothing is arranged, because no operation of this unit records a tracking point or a replan trigger.
func stepProjectStateAccessTrackWeeklyProjectProgressP1S4(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessTrackWeeklyProjectProgressP1S4) (any, error) {
	t.Helper()
	// No arrange: no operation of this unit records a tracking point or a replan
	// trigger (see the unit's blockers) — the read returns what is on record.
	return subject.RecordTrackingPoint(callContext(t, h, "4"), in.ProjectID, in.ExpectedVersion, in.Point, in.IdempotencyKey)
}

// stepProjectStateAccessTrackWeeklyProjectProgressP2S1 — Track Weekly Project Progress — [no] · [no], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"read-exits","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"track-weekly-project-progress/read-exits/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p2"}],"expect":{"result":"{\"ID\":\"stp-psa-twp-p2\",\"Version\":60,\"Phase\":2,\"ConstructionProgress\":{\"Week\":3,\"points\":[{\"week\":1},{\"week\":2}]}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges week 3 of construction with 2 tracking points on record, then reads the exits.
func stepProjectStateAccessTrackWeeklyProjectProgressP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessTrackWeeklyProjectProgressP2S1) (any, error) {
	t.Helper()
	seedMain(t, trackedConstruction(in.ProjectID, 60, 3, 2))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessTrackWeeklyProjectProgressP2S2 — Track Weekly Project Progress — [no] · [no], step 2: RecordTrackingPoint
// Projected stimulus (JSON): {"seq":2,"node":"no-trend","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"RecordTrackingPoint","call":"track-weekly-project-progress/no-trend/0"},"expectedOutputs":null,"expectedStates":[{"node":"tracking-point-no-trend","object":"Tracking point","inState":"[recorded without trend]"}],"hints":null}
// Binding (JSON): {"seq":2,"operation":"RecordTrackingPoint","inputs":[{"name":"projectID","value":"stp-psa-twp-p2"},{"name":"expectedVersion","value":"60"},{"name":"point","value":"{\"week\":3,\"earnedPct\":0.25,\"plannedPct\":0.3,\"note\":\"no trend: fewer than four points\"}"},{"name":"idempotencyKey","value":"stp-psa-twp-p2:point:2"}],"expect":{"errorExpected":false},"probes":[{"for":"tracking-point-no-trend","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p2"}],"expect":{"result":"{\"ConstructionProgress\":{\"points\":[{\"week\":1},{\"week\":2},{\"week\":3,\"note\":\"no trend: fewer than four points\"}]}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Reads the project; nothing is arranged, because no operation of this unit records a tracking point or a replan trigger.
func stepProjectStateAccessTrackWeeklyProjectProgressP2S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessTrackWeeklyProjectProgressP2S2) (any, error) {
	t.Helper()
	// No arrange: no operation of this unit records a tracking point or a replan
	// trigger (see the unit's blockers) — the read returns what is on record.
	return subject.RecordTrackingPoint(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.Point, in.IdempotencyKey)
}

// stepProjectStateAccessTrackWeeklyProjectProgressP2S3 — Track Weekly Project Progress — [no] · [no], step 3: RecordTrackingPoint
// Projected stimulus (JSON): {"seq":3,"node":"record-point","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"RecordTrackingPoint","call":"track-weekly-project-progress/record-point/0"},"expectedOutputs":[{"node":"record-point","kind":"inferred","to":"","label":"Append the tracking point to the durable progress record"},{"node":"show-weekly-progress","kind":"sendSignal","to":"architect-user","label":"Show the week's progress (EV, EAC, trend) to the Architect User"}],"expectedStates":[{"node":"progress-record-appended","object":"Progress record","inState":"[point appended]"}],"hints":null}
// Binding (JSON): {"seq":3,"operation":"RecordTrackingPoint","inputs":[{"name":"projectID","value":"stp-psa-twp-p2"},{"name":"expectedVersion","value":"61"},{"name":"point","value":"{\"week\":3,\"earnedPct\":0.25,\"plannedPct\":0.3,\"note\":\"no trend: fewer than four points\"}"},{"name":"idempotencyKey","value":"stp-psa-twp-p2:point:3"}],"expect":{"errorExpected":false},"probes":[{"for":"progress-record-appended","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p2"}],"expect":{"result":"{\"ConstructionProgress\":{\"points\":[{\"week\":1},{\"week\":2},{\"week\":3}]}}","errorExpected":false}},{"for":"record-point","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p2"}],"expect":{"result":"{\"ConstructionProgress\":{\"Week\":3,\"points\":[{\"week\":1},{\"week\":2},{\"week\":3}]}}","errorExpected":false}}],"unobservable":["show-weekly-progress"],"hook":true}
// Reads the project; nothing is arranged, because no operation of this unit records a tracking point or a replan trigger.
func stepProjectStateAccessTrackWeeklyProjectProgressP2S3(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessTrackWeeklyProjectProgressP2S3) (any, error) {
	t.Helper()
	// No arrange: no operation of this unit records a tracking point or a replan
	// trigger (see the unit's blockers) — the read returns what is on record.
	return subject.RecordTrackingPoint(callContext(t, h, "3"), in.ProjectID, in.ExpectedVersion, in.Point, in.IdempotencyKey)
}

// stepProjectStateAccessTrackWeeklyProjectProgressP3S1 — Track Weekly Project Progress — [yes] · [yes], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"read-exits","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"track-weekly-project-progress/read-exits/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p3"}],"expect":{"result":"{\"ID\":\"stp-psa-twp-p3\",\"Version\":60,\"Phase\":2,\"ConstructionProgress\":{\"Week\":5,\"points\":[{\"week\":1},{\"week\":2},{\"week\":3},{\"week\":4}]}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges week 5 of construction with 4 tracking points on record, then reads the exits.
func stepProjectStateAccessTrackWeeklyProjectProgressP3S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessTrackWeeklyProjectProgressP3S1) (any, error) {
	t.Helper()
	seedMain(t, trackedConstruction(in.ProjectID, 60, 5, 4))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessTrackWeeklyProjectProgressP3S2 — Track Weekly Project Progress — [yes] · [yes], step 2: RecordTrackingPoint
// Projected stimulus (JSON): {"seq":2,"node":"record-point","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"RecordTrackingPoint","call":"track-weekly-project-progress/record-point/0"},"expectedOutputs":[{"node":"record-point","kind":"inferred","to":"","label":"Append the tracking point to the durable progress record"}],"expectedStates":[{"node":"progress-record-appended","object":"Progress record","inState":"[point appended]"}],"hints":null}
// Binding (JSON): {"seq":2,"operation":"RecordTrackingPoint","inputs":[{"name":"projectID","value":"stp-psa-twp-p3"},{"name":"expectedVersion","value":"60"},{"name":"point","value":"{\"week\":5,\"earnedPct\":0.5,\"plannedPct\":0.55,\"note\":\"\"}"},{"name":"idempotencyKey","value":"stp-psa-twp-p3:point:2"}],"expect":{"errorExpected":false},"probes":[{"for":"progress-record-appended","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p3"}],"expect":{"result":"{\"ConstructionProgress\":{\"points\":[{\"week\":1},{\"week\":2},{\"week\":3},{\"week\":4},{\"week\":5}]}}","errorExpected":false}},{"for":"record-point","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p3"}],"expect":{"result":"{\"ConstructionProgress\":{\"Week\":5,\"points\":[{\"week\":1},{\"week\":2},{\"week\":3},{\"week\":4},{\"week\":5}]}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Reads the project; nothing is arranged, because no operation of this unit records a tracking point or a replan trigger.
func stepProjectStateAccessTrackWeeklyProjectProgressP3S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessTrackWeeklyProjectProgressP3S2) (any, error) {
	t.Helper()
	// No arrange: no operation of this unit records a tracking point or a replan
	// trigger (see the unit's blockers) — the read returns what is on record.
	return subject.RecordTrackingPoint(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.Point, in.IdempotencyKey)
}

// stepProjectStateAccessTrackWeeklyProjectProgressP3S3 — Track Weekly Project Progress — [yes] · [yes], step 3: RecordTrackingPoint
// Projected stimulus (JSON): {"seq":3,"node":"corrective","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"RecordTrackingPoint","call":"track-weekly-project-progress/corrective/0"},"expectedOutputs":[{"node":"show-weekly-progress","kind":"sendSignal","to":"architect-user","label":"Show the week's progress (EV, EAC, trend) to the Architect User"}],"expectedStates":[{"node":"replan-trigger-pending","object":"Replan trigger","inState":"[pending]"}],"hints":null}
// Binding (JSON): {"seq":3,"operation":"RecordTrackingPoint","inputs":[{"name":"projectID","value":"stp-psa-twp-p3"},{"name":"expectedVersion","value":"61"},{"name":"point","value":"{\"week\":5,\"earnedPct\":0.5,\"plannedPct\":0.55,\"note\":\"corrective action: replan trigger pending\"}"},{"name":"idempotencyKey","value":"stp-psa-twp-p3:point:3"}],"expect":{"errorExpected":false},"probes":[{"for":"replan-trigger-pending","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p3"}],"expect":{"result":"{\"ConstructionProgress\":{\"points\":[{\"week\":1},{\"week\":2},{\"week\":3},{\"week\":4},{\"week\":5,\"note\":\"corrective action: replan trigger pending\"}]}}","errorExpected":false}}],"unobservable":["show-weekly-progress"],"hook":true}
// Reads the project; nothing is arranged, because no operation of this unit records a tracking point or a replan trigger.
func stepProjectStateAccessTrackWeeklyProjectProgressP3S3(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessTrackWeeklyProjectProgressP3S3) (any, error) {
	t.Helper()
	// No arrange: no operation of this unit records a tracking point or a replan
	// trigger (see the unit's blockers) — the read returns what is on record.
	return subject.RecordTrackingPoint(callContext(t, h, "3"), in.ProjectID, in.ExpectedVersion, in.Point, in.IdempotencyKey)
}

// stepProjectStateAccessTrackWeeklyProjectProgressP4S1 — Track Weekly Project Progress — [yes] · [no], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"read-exits","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"ReadProject","call":"track-weekly-project-progress/read-exits/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p4"}],"expect":{"result":"{\"ID\":\"stp-psa-twp-p4\",\"Version\":60,\"Phase\":2,\"ConstructionProgress\":{\"Week\":5,\"points\":[{\"week\":1},{\"week\":2},{\"week\":3},{\"week\":4}]}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges week 5 of construction with 4 tracking points on record, then reads the exits.
func stepProjectStateAccessTrackWeeklyProjectProgressP4S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessTrackWeeklyProjectProgressP4S1) (any, error) {
	t.Helper()
	seedMain(t, trackedConstruction(in.ProjectID, 60, 5, 4))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessTrackWeeklyProjectProgressP4S2 — Track Weekly Project Progress — [yes] · [no], step 2: RecordTrackingPoint
// Projected stimulus (JSON): {"seq":2,"node":"record-point","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"RecordTrackingPoint","call":"track-weekly-project-progress/record-point/0"},"expectedOutputs":[{"node":"record-point","kind":"inferred","to":"","label":"Append the tracking point to the durable progress record"},{"node":"show-weekly-progress","kind":"sendSignal","to":"architect-user","label":"Show the week's progress (EV, EAC, trend) to the Architect User"}],"expectedStates":[{"node":"progress-record-appended","object":"Progress record","inState":"[point appended]"}],"hints":null}
// Binding (JSON): {"seq":2,"operation":"RecordTrackingPoint","inputs":[{"name":"projectID","value":"stp-psa-twp-p4"},{"name":"expectedVersion","value":"60"},{"name":"point","value":"{\"week\":5,\"earnedPct\":0.5,\"plannedPct\":0.55,\"note\":\"\"}"},{"name":"idempotencyKey","value":"stp-psa-twp-p4:point:2"}],"expect":{"errorExpected":false},"probes":[{"for":"progress-record-appended","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p4"}],"expect":{"result":"{\"ConstructionProgress\":{\"points\":[{\"week\":1},{\"week\":2},{\"week\":3},{\"week\":4},{\"week\":5}]}}","errorExpected":false}},{"for":"record-point","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-twp-p4"}],"expect":{"result":"{\"ConstructionProgress\":{\"Week\":5,\"points\":[{\"week\":1},{\"week\":2},{\"week\":3},{\"week\":4},{\"week\":5}]}}","errorExpected":false}}],"unobservable":["show-weekly-progress"],"hook":true}
// Reads the project; nothing is arranged, because no operation of this unit records a tracking point or a replan trigger.
func stepProjectStateAccessTrackWeeklyProjectProgressP4S2(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessTrackWeeklyProjectProgressP4S2) (any, error) {
	t.Helper()
	// No arrange: no operation of this unit records a tracking point or a replan
	// trigger (see the unit's blockers) — the read returns what is on record.
	return subject.RecordTrackingPoint(callContext(t, h, "2"), in.ProjectID, in.ExpectedVersion, in.Point, in.IdempotencyKey)
}

// stepProjectStateAccessViewLiveDeploymentHealthP1S1 — View Live Deployment Health — [yes], step 1: ReadProject
// Projected stimulus (JSON): {"seq":1,"node":"resolve-deployment","input":{"kind":"call","from":"operations-manager","to":"projectstateaccess","op":"ReadProject","call":"view-live-deployment-health/resolve-deployment/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-psa-vdh-p1"}],"expect":{"result":"{\"ID\":\"stp-psa-vdh-p1\",\"Version\":40,\"Phase\":2,\"OperationalConcepts\":{\"Status\":2}}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges anything-deployed=[yes]: a delivered project with its committed deployment & operations model, then reads it.
func stepProjectStateAccessViewLiveDeploymentHealthP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessViewLiveDeploymentHealthP1S1) (any, error) {
	t.Helper()
	seedMain(t, deliveredSystem(in.ProjectID, 40))
	return subject.ReadProject(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessViewTheProjectStateLogP1S1 — View the Project State Log — [yes], step 1: ReadProjectVersion
// Projected stimulus (JSON): {"seq":1,"node":"read-commits","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"","call":"view-the-project-state-log/read-commits/0"},"expectedOutputs":[{"node":"read-commits","kind":"inferred","to":"","label":"Read the state repo's commit history; each mutation is one commit naming its operation"}],"expectedStates":null,"hints":["Value convention: one commit per mutation, and the commit message names its operation. The binding matches log entries to seeded mutations by operation name and commit order."]}
// Binding (JSON): {"seq":1,"operation":"ReadProjectVersion","inputs":[{"name":"projectID","value":"stp-psa-vsl-p1"}],"expect":{"result":"3","errorExpected":false},"probes":[],"unobservable":["read-commits"],"hook":true}
// Arranges three state commits through this facet's own writes (create, operating model, review policy), then reads the version.
func stepProjectStateAccessViewTheProjectStateLogP1S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessViewTheProjectStateLogP1S1) (any, error) {
	t.Helper()
	createConfiguredProject(t, h, subject, in.ProjectID)
	return subject.ReadProjectVersion(callContext(t, h, "1"), in.ProjectID)
}

// stepProjectStateAccessViewTheProjectStateLogP2S1 — View the Project State Log — [no], step 1: ReadProjectVersion
// Projected stimulus (JSON): {"seq":1,"node":"read-commits","input":{"kind":"call","from":"delivery-manager","to":"projectstateaccess","op":"","call":"view-the-project-state-log/read-commits/0"},"expectedOutputs":[{"node":"read-commits","kind":"inferred","to":"","label":"Read the state repo's commit history; each mutation is one commit naming its operation"}],"expectedStates":null,"hints":["Value convention: one commit per mutation, and the commit message names its operation. The binding matches log entries to seeded mutations by operation name and commit order."]}
// Binding (JSON): {"seq":1,"operation":"ReadProjectVersion","inputs":[{"name":"projectID","value":"stp-psa-vsl-p2"}],"expect":{"result":"3","errorExpected":false},"probes":[],"unobservable":["read-commits"],"hook":true}
// Arranges three state commits through this facet's own writes (create, operating model, review policy), then reads the version.
func stepProjectStateAccessViewTheProjectStateLogP2S1(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, in inputProjectStateAccessViewTheProjectStateLogP2S1) (any, error) {
	t.Helper()
	createConfiguredProject(t, h, subject, in.ProjectID)
	return subject.ReadProjectVersion(callContext(t, h, "1"), in.ProjectID)
}

// assetsVersion is the method-assets release every fixture's lifecycle pin names; it is
// the one the bindings pin, so a fixture row and a bound OpenActivity agree.
const assetsVersion = "v0.10.1"

// arrangedAt is the fixed instant every fixture timestamp uses, so a seeded document is
// byte-stable across runs.
var arrangedAt = time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)

// scenarioRepos maps a running scenario (its *testing.T) to its own bare repo.
var scenarioRepos sync.Map

// newScenarioRepo creates the scenario's repo and returns the file:// URL the
// component's local-profile constructors take.
func newScenarioRepo(t *testing.T) string {
	t.Helper()
	repo := testinfra.StartLocalGitRepo(t, "main")
	scenarioRepos.Store(t, repo)
	t.Cleanup(func() { scenarioRepos.Delete(t) })
	return repo.URL
}

func scenarioRepo(t *testing.T) testinfra.LocalGitRepo {
	t.Helper()
	v, ok := scenarioRepos.Load(t)
	if !ok {
		t.Fatal("arrange: no repo for this scenario — newSubject must run first")
	}
	return v.(testinfra.LocalGitRepo)
}

// readOnBranch is every designSessionAccess.ReadProjectOnBranch probe: the facet the
// probe names, built over the scenario's repo.
func readOnBranch(t *testing.T, h *scenariohost.Host, projectID projectstate.ProjectID, branch string) (any, error) {
	t.Helper()
	dsa := projectstate.NewGitLocalDesignSessionAccess(scenarioRepo(t).URL)
	return dsa.ReadProjectOnBranch(callContext(t, h, "probe"), projectID, branch)
}

// seedMain commits p as main's project.json.
func seedMain(t *testing.T, p projectstate.Project) {
	t.Helper()
	commitProjectDoc(t, "main", p)
}

// seedBranch commits p as the project.json at the tip of branch, forked from main.
func seedBranch(t *testing.T, branch string, p projectstate.Project) {
	t.Helper()
	commitProjectDoc(t, branch, p)
}

// commitProjectDoc writes the canonical document for p (EncodeProjectJSON is the
// component's own writer) to .aiarch/state/project.json on branch and pushes it.
func commitProjectDoc(t *testing.T, branch string, p projectstate.Project) {
	t.Helper()
	raw, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("arrange: encode project %s: %v", p.ID, err)
	}
	work := filepath.Join(t.TempDir(), "arrange")
	runGit(t, "", "clone", "--quiet", scenarioRepo(t).Dir, work)
	runGit(t, work, "checkout", "--quiet", "-B", branch)
	stateDir := filepath.Join(work, ".aiarch", "state")
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		t.Fatalf("arrange: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "project.json"), raw, 0o600); err != nil {
		t.Fatalf("arrange: %v", err)
	}
	runGit(t, work, "add", ".aiarch/state/project.json")
	runGit(t, work, "-c", "user.name=scenario-arrange", "-c", "user.email=arrange@aiarch.local",
		"-c", "commit.gpgsign=false", "commit", "--quiet", "--allow-empty", "-m", "arrange "+string(p.ID))
	runGit(t, work, "push", "--quiet", "--force", "origin", branch)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("arrange: git %v: %v\n%s", args, err, out)
	}
}

// ---------------------------------------------------------------------------
// Fixtures.
// ---------------------------------------------------------------------------

func baseProject(id projectstate.ProjectID, v projectstate.Version, phase projectstate.Phase) projectstate.Project {
	return projectstate.Project{ID: id, Version: v, Phase: phase, Name: "Scenario " + string(id)}
}

// openRow is an activity the pump has opened and is walking.
func openRow(activityID string, typ projectstate.ActivityType, typeKey string) projectstate.ActivityExecution {
	started := arrangedAt
	return projectstate.ActivityExecution{
		ActivityID: activityID,
		Type:       typ,
		Pin:        &projectstate.LifecyclePin{TypeKey: typeKey, AssetsVersion: assetsVersion},
		StartedAt:  &started,
	}
}

func rows(rs ...projectstate.ActivityExecution) map[string]projectstate.ActivityExecution {
	m := make(map[string]projectstate.ActivityExecution, len(rs))
	for _, r := range rs {
		m[r.ActivityID] = r
	}
	return m
}

func doneRow(activityID string, typ projectstate.ActivityType, typeKey string) projectstate.ActivityExecution {
	r := openRow(activityID, typ, typeKey)
	done := arrangedAt.Add(time.Hour)
	r.CompletedAt = &done
	return r
}

func missionStatement(text string) *projectstate.MissionStatement {
	return &projectstate.MissionStatement{
		Vision:     "Predictably engineered software, delivered and operated on the customer's behalf.",
		Mission:    text,
		Objectives: []projectstate.Objective{{Number: 1, Statement: "Customers receive a duration, cost and risk estimate before any code is written."}},
	}
}

func committed(m projectstate.ArtifactModel) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: m}
}

func awaitingReview(m projectstate.ArtifactModel) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{Status: projectstate.ReviewAwaitingReview, Model: m}
}

func endedAttempt(activityID, task string, n int, outcome projectstate.TaskOutcome, projectID projectstate.ProjectID) projectstate.TaskAttempt {
	started, ended := arrangedAt, arrangedAt.Add(10*time.Minute)
	id := activityID + ":" + task + ":" + itoa(n)
	return projectstate.TaskAttempt{
		AttemptID: id,
		Task:      projectstate.MethodTask(task),
		Attempt:   n,
		Actor:     projectstate.ActorAgent,
		StartedAt: &started,
		EndedAt:   &ended,
		Outcome:   outcome,
		Evidence:  projectstate.EvidenceRef{Kind: projectstate.EvidenceEpisode, Ref: "ep-" + string(projectID) + "-" + task + "-" + itoa(n)},
	}
}

func itoa(n int) string {
	const digits = "0123456789"
	if n < 10 {
		return digits[n : n+1]
	}
	return itoa(n/10) + digits[n%10:n%10+1]
}

// pendingRound is a review round opened on subject and not yet decided.
func pendingRound(roundID, task, reviews string, kind projectstate.ArtifactKind, round int64, subject string) projectstate.ReviewRound {
	k := kind
	return projectstate.ReviewRound{
		RoundID:      roundID,
		TaskID:       projectstate.MethodTask(task),
		Reviews:      projectstate.MethodTask(reviews),
		ArtifactKind: &k,
		Round:        round,
		SubjectRef:   projectstate.SubjectRef{Kind: projectstate.SubjectPullRequest, Ref: subject},
		Outcome:      projectstate.RoundPending,
		OpenedAt:     arrangedAt.Format(time.RFC3339),
	}
}

// missionReviewRow is D-REQ with its mission draft passed and the mission review round open.
func missionReviewRow(projectID projectstate.ProjectID) projectstate.ActivityExecution {
	r := openRow("D-REQ", projectstate.ActivityTypeRequirements, "requirements")
	r.Attempts = []projectstate.TaskAttempt{endedAttempt("D-REQ", "missionDraft", 1, projectstate.OutcomePassed, projectID)}
	r.Reviews = []projectstate.ReviewRound{pendingRound("D-REQ:missionReview:mission:1", "missionReview", "missionDraft",
		projectstate.KindMission, 1, string(projectID)+"#pr-1")}
	return r
}

// seedMissionUnderReview: the mission draft is staged on the session branch and its review
// round is open on main (artifact-state = yes: session branch).
func seedMissionUnderReview(t *testing.T, id projectstate.ProjectID, v projectstate.Version, draft string) {
	t.Helper()
	p := baseProject(id, v, projectstate.PhaseSystemDesign)
	p.ActivityExecution = rows(missionReviewRow(id))
	seedMain(t, p)
	p.Mission = awaitingReview(missionStatement(draft))
	seedBranch(t, "activity/D-REQ", p)
}

// questionAwaitingAnswer: the mission is committed (artifact-state = no: committed) and the
// review round carries the reviewer's open question the answer job has just answered.
func questionAwaitingAnswer(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseSystemDesign)
	p.Mission = committed(missionStatement("Deliver predictably engineered software, operated on the customer's behalf."))
	r := missionReviewRow(id)
	r.Reviews[0].Verdicts = []projectstate.ReviewVerdict{{
		ReviewerRole: "operator", Actor: "operator", Verdict: projectstate.VerdictAbstain,
		Summary: "Clarifying questions before a verdict.", AttemptID: "D-REQ:missionDraft:1", At: arrangedAt.Format(time.RFC3339),
	}}
	r.Reviews[0].Thread = []projectstate.ReviewComment{{
		ID: "r1c1", Anchor: "$.mission", AnchorText: "operated on the customer's behalf",
		Text: "Does 'operated' include billing the customer?", AuthorRole: "operator", Round: 1,
		Status: projectstate.ReviewCommentOpen, Replies: []projectstate.ReviewCommentReply{},
		Type: "question", Addressee: "system-architect",
	}}
	p.ActivityExecution = rows(r)
	return p
}

// inFlightAmendment: a project already in construction, with the requirements activity
// re-opened to amend its use case model on the session branch.
func inFlightAmendment(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseConstruction)
	p.Mission = committed(missionStatement("Deliver predictably engineered software."))
	p.ActivityExecution = rows(openRow("D-REQ", projectstate.ActivityTypeRequirements, "requirements"))
	return p
}

// eligibleActivity: a fresh project whose first activity (D-REQ) the pump finds eligible and
// has not opened yet.
func eligibleActivity(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	return baseProject(id, v, projectstate.PhaseSystemDesign)
}

// sdpRevisionPresented: the committed SDP stands; the re-plan's revision round (2) is open for
// management's decision on the project-design activity.
func sdpRevisionPresented(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseConstruction)
	p.SdpReview = committed(&projectstate.SdpReview{Recommendation: "opt-normal", Rationale: "The committed plan."})
	r := openRow("D-PD", projectstate.ActivityTypeProjectDesign, "projectDesign")
	r.Reviews = []projectstate.ReviewRound{pendingRound("D-PD:sdpReview:sdpReview:2", "sdpReview", "sdp",
		projectstate.KindSdpReview, 2, string(id)+"#pr-2")}
	p.ActivityExecution = rows(r)
	return p
}

// failedActivity: D-REQ exited failed with its failed attempt on record.
func failedActivity(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseSystemDesign)
	r := openRow("D-REQ", projectstate.ActivityTypeRequirements, "requirements")
	r.FailureReason = projectstate.VarianceExhausted
	r.FailureDetail = "missionDraft failed its exit criterion on every attempt."
	r.Attempts = []projectstate.TaskAttempt{endedAttempt("D-REQ", "missionDraft", 1, projectstate.OutcomeFailed, id)}
	p.ActivityExecution = rows(r)
	return p
}

// resumeRequeuedWalk is the resumed walk the pump starts after a requeue: it re-opens the
// re-armed row and opens the pending attempt the requeue note is then delivered to. Each
// write holds the versions it just read, exactly as the delivery manager's child does.
func resumeRequeuedWalk(t *testing.T, h *scenariohost.Host, subject projectstate.ActivityExecutionAccess, projectID projectstate.ProjectID, activityID, attemptID string) {
	t.Helper()
	head := func() (projectstate.Version, projectstate.ActivityExecution) {
		env, err := projectstate.NewGitLocalDesignSessionAccess(scenarioRepo(t).URL).ReadProjectOnBranch(callContext(t, h, "arrange"), projectID, "")
		if err != nil {
			t.Fatalf("arrange: read head: %v", err)
		}
		row, err := subject.ReadActivityExecution(callContext(t, h, "arrange"), projectID, activityID)
		if err != nil {
			t.Fatalf("arrange: read row: %v", err)
		}
		return env.Version, row
	}
	v, row := head()
	if _, err := subject.OpenActivity(callContext(t, h, "arrange"), projectID, v, row.Version, activityID, row.Type, row.Variant,
		*row.Pin, projectstate.RepoCredential{}, callContext(t, h, "arrange-reopen").IdempotencyKey); err != nil {
		t.Fatalf("arrange: re-open %s: %v", activityID, err)
	}
	v, row = head()
	if _, err := subject.RecordAttemptOutcome(callContext(t, h, "arrange"), projectID, v, row.Version, activityID,
		projectstate.TaskAttemptInput{AttemptID: attemptID, TaskID: "missionDraft", Attempt: 2, Actor: projectstate.ActorAgent, Outcome: projectstate.OutcomePending},
		projectstate.RepoCredential{}, callContext(t, h, "arrange-attempt").IdempotencyKey); err != nil {
		t.Fatalf("arrange: open attempt %s: %v", attemptID, err)
	}
}

// resumedAfterPause: the operator paused and resumed; requirements is done and the
// architecture activity is next.
func resumedAfterPause(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseSystemDesign)
	p.Mission = committed(missionStatement("Deliver predictably engineered software."))
	p.ActivityExecution = rows(doneRow("D-REQ", projectstate.ActivityTypeRequirements, "requirements"))
	return p
}

// constructionDispatching: a service activity in construction whose task is being
// dispatched; no branch is recorded for it yet.
func constructionDispatching(id projectstate.ProjectID, v projectstate.Version, activityID string) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseConstruction)
	p.ActivityExecution = rows(openRow(activityID, projectstate.ActivityTypeService, "service"))
	return p
}

// pullRequestAwaitingCI: the activity's branch and PR are recorded and CI has not reported.
func pullRequestAwaitingCI(id projectstate.ProjectID, v projectstate.Version, activityID string) projectstate.Project {
	p := constructionDispatching(id, v, activityID)
	p.ActivityGit = map[string]projectstate.ActivityGitStatus{activityID: {
		ActivityID: activityID, BranchName: "activity/" + activityID, BranchRef: "refs/heads/activity/" + activityID,
		PullRequestRef: "https://github.com/mixofreality-studio/stp-fixture/pull/1", CICheck: projectstate.CICheckPending,
		UpdatedAt: arrangedAt,
	}}
	return p
}

// revisionRedispatch: revision n was sent back; its branch, PR and green CI are on record
// and revision n+1 is being dispatched.
func revisionRedispatch(id projectstate.ProjectID, v projectstate.Version, activityID string) projectstate.Project {
	p := pullRequestAwaitingCI(id, v, activityID)
	g := p.ActivityGit[activityID]
	g.CICheck = projectstate.CICheckSuccess
	p.ActivityGit[activityID] = g
	return p
}

// ---------------------------------------------------------------------------
// Collaborator facets over the scenario's repo. Every arrange write that is not the
// subject's own goes through the facet the use case names, built by its public
// constructor over the same repo.
// ---------------------------------------------------------------------------

func psaOn(t *testing.T) projectstate.ProjectStateAccess {
	t.Helper()
	return projectstate.NewGitLocalProjectStateAccess(scenarioRepo(t).URL)
}

func aeaOn(t *testing.T) projectstate.ActivityExecutionAccess {
	t.Helper()
	return projectstate.NewGitLocalActivityExecutionAccess(scenarioRepo(t).URL)
}

func dsaOn(t *testing.T) projectstate.DesignSessionAccess {
	t.Helper()
	return projectstate.NewGitLocalDesignSessionAccess(scenarioRepo(t).URL)
}

// readProject is every projectStateAccess.ReadProject probe.
func readProject(t *testing.T, h *scenariohost.Host, projectID projectstate.ProjectID) (any, error) {
	t.Helper()
	return psaOn(t).ReadProject(callContext(t, h, "probe"), projectID)
}

// readExecution is every activityExecutionAccess.ReadActivityExecution probe.
func readExecution(t *testing.T, h *scenariohost.Host, projectID projectstate.ProjectID, activityID string) (any, error) {
	t.Helper()
	return aeaOn(t).ReadActivityExecution(callContext(t, h, "probe"), projectID, activityID)
}

// headVersion is the project's current head version.
func headVersion(t *testing.T, h *scenariohost.Host, projectID projectstate.ProjectID) projectstate.Version {
	t.Helper()
	v, err := psaOn(t).ReadProjectVersion(callContext(t, h, "arrange-head"), projectID)
	if err != nil {
		t.Fatalf("arrange: read head version of %s: %v", projectID, err)
	}
	return v
}

// rowVersion is an existing activity row's version.
func rowVersion(t *testing.T, h *scenariohost.Host, projectID projectstate.ProjectID, activityID string) int64 {
	t.Helper()
	row, err := aeaOn(t).ReadActivityExecution(callContext(t, h, "arrange-row"), projectID, activityID)
	if err != nil {
		t.Fatalf("arrange: read row %s: %v", activityID, err)
	}
	return row.Version
}

// commitOnMain is designSessionAccess's commit of a slot staged on main, at the head version.
func commitOnMain(t *testing.T, h *scenariohost.Host, projectID projectstate.ProjectID, kind projectstate.ArtifactKind, approvedBy, draftedBy string) {
	t.Helper()
	if _, err := dsaOn(t).CommitArtifactWithProvenance(callContext(t, h, "arrange-commit"), projectID, headVersion(t, h, projectID), kind, approvedBy, draftedBy); err != nil {
		t.Fatalf("arrange: commit %v: %v", kind, err)
	}
}

// openActivity is activityExecutionAccess opening a not-yet-started activity at the head version.
func openActivity(t *testing.T, h *scenariohost.Host, projectID projectstate.ProjectID, activityID string, typ projectstate.ActivityType, typeKey string) {
	t.Helper()
	pin := projectstate.LifecyclePin{TypeKey: typeKey, AssetsVersion: assetsVersion}
	if _, err := aeaOn(t).OpenActivity(callContext(t, h, "arrange-open"), projectID, headVersion(t, h, projectID), 0, activityID, typ, 0, pin,
		projectstate.RepoCredential{}, callContext(t, h, "arrange-open-"+activityID).IdempotencyKey); err != nil {
		t.Fatalf("arrange: open %s: %v", activityID, err)
	}
}

// recordNote is activityExecutionAccess recording an operator note on an existing row.
func recordNote(t *testing.T, h *scenariohost.Host, projectID projectstate.ProjectID, activityID string, note projectstate.OperatorNoteInput) {
	t.Helper()
	if _, err := aeaOn(t).RecordOperatorNote(callContext(t, h, "arrange-note"), projectID, headVersion(t, h, projectID), rowVersion(t, h, projectID, activityID),
		activityID, note, "", projectstate.RepoCredential{}, callContext(t, h, "arrange-note-"+note.NoteID).IdempotencyKey); err != nil {
		t.Fatalf("arrange: record note %s on %s: %v", note.NoteID, activityID, err)
	}
}

// retryNote is the operator's retry intervention on the exhausted requirements walk.
var retryNote = projectstate.OperatorNoteInput{NoteID: "note-retry-1", Kind: projectstate.NoteRetry, Text: "Narrow the glossary to the billing terms and retry."}

// requeueNote is the operator's re-queue of the failed construction activity.
var requeueNote = projectstate.OperatorNoteInput{NoteID: "note-requeue-1", Kind: projectstate.NoteRequeue, Gate: "reopen", Text: "Re-queue after the upstream fix landed."}

// sdpRoundID is the SDP review round the reject-all decision closes.
const sdpRoundID = "P-SDP:sdpReview:sdpReview:1"

// rejectAllOptions is the reject-all decision through activityExecutionAccess: the SDP
// review round is opened, the reviewer's sendBack verdict and feedback are appended, and
// the round is decided sentBack.
func rejectAllOptions(t *testing.T, h *scenariohost.Host, projectID projectstate.ProjectID) {
	t.Helper()
	aea := aeaOn(t)
	openActivity(t, h, projectID, "P-SDP", projectstate.ActivityTypeProjectDesign, "projectDesign")
	kind := projectstate.KindSdpReview
	round := projectstate.ReviewRoundInput{
		RoundID: sdpRoundID, TaskID: "sdpReview", Reviews: "sdp", ArtifactKind: &kind, Round: 1,
		SubjectRef: projectstate.SubjectRef{Kind: projectstate.SubjectPullRequest, Ref: string(projectID) + "#pr-1"},
	}
	if _, err := aea.OpenReviewRound(callContext(t, h, "arrange-round"), projectID, headVersion(t, h, projectID), rowVersion(t, h, projectID, "P-SDP"),
		"P-SDP", round, projectstate.RepoCredential{}, callContext(t, h, "arrange-round-key").IdempotencyKey); err != nil {
		t.Fatalf("arrange: open SDP review round: %v", err)
	}
	verdict := projectstate.ReviewVerdict{ReviewerRole: "founder", Actor: "founder@stp.test", Verdict: projectstate.VerdictSendBack,
		Summary: "Reject all options.", AttemptID: "P-SDP:sdp:1", At: arrangedAt.Format(time.RFC3339)}
	feedback := []projectstate.ReviewComment{{Anchor: "$.options", Text: "Reject all options: the compressed option exceeds the build-cost ceiling.",
		AuthorRole: "founder", Round: 1, Status: projectstate.ReviewCommentOpen, Type: projectstate.ReviewCommentTypeChangeRequest}}
	if _, err := aea.AppendReviewVerdict(callContext(t, h, "arrange-verdict"), projectID, headVersion(t, h, projectID), rowVersion(t, h, projectID, "P-SDP"),
		"P-SDP", sdpRoundID, verdict, feedback, nil, projectstate.RepoCredential{}, callContext(t, h, "arrange-verdict-key").IdempotencyKey); err != nil {
		t.Fatalf("arrange: append sendBack verdict: %v", err)
	}
	if _, err := aea.DecideReviewRound(callContext(t, h, "arrange-decide"), projectID, headVersion(t, h, projectID), rowVersion(t, h, projectID, "P-SDP"),
		"P-SDP", sdpRoundID, projectstate.RoundSentBack, "founder@stp.test", projectstate.RepoCredential{}, callContext(t, h, "arrange-decide-key").IdempotencyKey); err != nil {
		t.Fatalf("arrange: decide SDP review round: %v", err)
	}
}

// createConfiguredProject is three state commits through projectStateAccess's own
// writes: the project is created, its operating model chosen and its review policy set.
func createConfiguredProject(t *testing.T, h *scenariohost.Host, subject projectstate.ProjectStateAccess, projectID projectstate.ProjectID) {
	t.Helper()
	v, err := subject.CreateProject(callContext(t, h, "arrange-create"), projectID, "stp-psa-owner-"+projectstate.OwnerScope(projectID), "STP "+string(projectID))
	if err != nil {
		t.Fatalf("arrange: create %s: %v", projectID, err)
	}
	if v, err = subject.SetOperatingModel(callContext(t, h, "arrange-operated"), projectID, v, projectstate.OperatingModelArchistratorOperated); err != nil {
		t.Fatalf("arrange: set operating model: %v", err)
	}
	full := "full"
	if _, err = subject.RecordReviewPolicy(callContext(t, h, "arrange-policy"), projectID, v, projectstate.ReviewPolicy{Preset: &full}, callContext(t, h, "arrange-policy-key").IdempotencyKey); err != nil {
		t.Fatalf("arrange: record review policy: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Models.
// ---------------------------------------------------------------------------

// useCaseModel is a one-decision use-case model whose use case carries the activity
// diagram (an entry and an action) every use case must have.
func useCaseModel(id, name, rationale string) *projectstate.CoreUseCases {
	return &projectstate.CoreUseCases{Decisions: []projectstate.UseCaseDecision{{
		UseCase: projectstate.UseCase{
			ID: projectstate.UseCaseID(id), Name: name,
			Actors:  []projectstate.Actor{{ID: "architect-user", Role: "Architect User"}},
			Trigger: projectstate.TriggerClientAction, Classification: projectstate.ClassCore,
			Activity: &projectstate.ActivityDiagram{
				Nodes: []projectstate.ActivityNode{{ID: "start", Kind: projectstate.NodeStart, Label: "Start"}, {ID: "act", Kind: projectstate.NodeAction, Label: "Act"}},
				Edges: []projectstate.ActivityEdge{{From: "start", To: "act", Kind: projectstate.EdgeControlFlow}},
			},
		},
		EssenceRationale: &rationale,
	}}}
}

// systemModel is a one-component system design (a System must decompose into at
// least one component, its layer matching its kind).
func systemModel() *projectstate.System {
	return &projectstate.System{Components: []projectstate.Component{{
		ID: "project-state-access", Name: "projectStateAccess", Kind: projectstate.CompResourceAccess,
		Layer: projectstate.LayerResourceAccess, Encapsulates: "Project state storage", AtomicBusinessVerbs: []string{"ReadProject"},
	}}}
}

func designPrefixList() *projectstate.ActivityList {
	return &projectstate.ActivityList{Activities: []projectstate.ActivityItem{
		{Name: "requirements", EffortDays: 5, WorkerClass: "system-architect"},
		{Name: "architecture", EffortDays: 5, WorkerClass: "system-architect"},
		{Name: "projectDesign", EffortDays: 5, WorkerClass: "project-manager"},
	}}
}

func designPrefixNetwork() *projectstate.Network {
	return &projectstate.Network{Dependencies: []projectstate.NetworkDependency{
		{Activity: "architecture", DependsOn: []string{"requirements"}},
		{Activity: "projectDesign", DependsOn: []string{"architecture"}},
	}}
}

// revision returns slot as a commit with n revisions on record.
func revision(slot projectstate.ArtifactSlot, n int64) projectstate.ArtifactSlot {
	slot.Revisions = n
	return slot
}

// staleOn marks a committed slot stale on upstream's revision rev, as commitTransition does.
func staleOn(slot projectstate.ArtifactSlot, upstream string, rev int64) projectstate.ArtifactSlot {
	slot.StaleBasis = true
	slot.StaleBasisCause = &projectstate.StaleCause{UpstreamKind: upstream, UpstreamRevision: rev}
	return slot
}

// ---------------------------------------------------------------------------
// Heads.
// ---------------------------------------------------------------------------

// inConstructionWithUseCases: a project in construction whose use-case model and every
// downstream artifact the scenarios read are committed once.
func inConstructionWithUseCases(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseConstruction)
	p.CoreUseCases = revision(committed(useCaseModel("stp-core-use-case", "STP Core Use Case", "The committed use case.")), 1)
	p.SystemDesign = revision(committed(systemModel()), 1)
	p.ActivityList = revision(committed(designPrefixList()), 1)
	p.Network = revision(committed(designPrefixNetwork()), 1)
	return p
}

// useCasesRecommitted: what the amendment's re-commit leaves — the use-case model at
// revision 2 and every committed downstream artifact stale on it.
func useCasesRecommitted(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := inConstructionWithUseCases(id, v)
	p.CoreUseCases = revision(p.CoreUseCases, 2)
	p.SystemDesign = staleOn(p.SystemDesign, "coreUseCases", 2)
	p.ActivityList = staleOn(p.ActivityList, "coreUseCases", 2)
	p.Network = staleOn(p.Network, "coreUseCases", 2)
	return p
}

// amendmentAwaitingCommit: the amended use-case model is staged on main for its re-commit
// (committed once before), its downstream artifacts committed.
func amendmentAwaitingCommit(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := inConstructionWithUseCases(id, v)
	p.CoreUseCases = revision(awaitingReview(useCaseModel("stp-added-use-case", "STP Added Use Case", "The amendment under review.")), 1)
	return p
}

// committedSystem: the system design is committed on main, with no review thread.
func committedSystem(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseSystemDesign)
	p.SystemDesign = revision(committed(systemModel()), 1)
	return p
}

// seedEligibleRequirements: main at v with the requirements activity open, and its
// activity branch at v carrying draft (when the dispatched job committed one).
func seedEligibleRequirements(t *testing.T, id projectstate.ProjectID, v projectstate.Version, draft *projectstate.CoreUseCases) {
	t.Helper()
	p := baseProject(id, v, projectstate.PhaseSystemDesign)
	p.ActivityExecution = rows(openRow("requirements", projectstate.ActivityTypeRequirements, "requirements"))
	seedMain(t, p)
	if draft != nil {
		p.CoreUseCases = awaitingReview(draft)
	}
	seedBranch(t, "activity/requirements", p)
}

// seedNetworkReview: main at v in project design with the plan's activity list committed;
// the project-design activity branch (forked at v, one write ahead) carries the
// in-flight planning-assumptions draft.
func seedNetworkReview(t *testing.T, id projectstate.ProjectID, v projectstate.Version) {
	t.Helper()
	p := baseProject(id, v, projectstate.PhaseProjectDesign)
	p.ActivityList = revision(committed(designPrefixList()), 1)
	seedMain(t, p)
	p.Version = v + 1
	p.PlanningAssumptions = awaitingReview(&projectstate.PlanningAssumptions{})
	seedBranch(t, "activity/projectDesign", p)
}

// bornProject: a project CreateProject has just created.
func bornProject(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseSystemDesign)
	p.Owner = "stp-owner-" + projectstate.OwnerScope(id)
	return p
}

// sdpRevisionAwaitingAdoption: the committed plan stands and the revised SDP review
// (its second revision) is staged on main for adoption.
func sdpRevisionAwaitingAdoption(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseConstruction)
	p.Network = revision(committed(designPrefixNetwork()), 1)
	p.SdpReview = revision(awaitingReview(&projectstate.SdpReview{Recommendation: "opt-compressed", Rationale: "The re-plan's revised option."}), 1)
	return p
}

// seedQuestionOnMission: the mission review round carries the reviewer's question q-1,
// answered by the drafting agent or still open. The mission is either committed on main
// or (artifact-state = session branch) staged on the activity branch with nothing on main.
func seedQuestionOnMission(t *testing.T, id projectstate.ProjectID, v projectstate.Version, missionCommitted, answered bool) {
	t.Helper()
	p := baseProject(id, v, projectstate.PhaseSystemDesign)
	r := missionReviewRow(id)
	q := projectstate.ReviewComment{
		ID: "q-1", Anchor: "$.mission", AnchorText: "operated on the customer's behalf",
		Text: "Does 'operated' include billing the customer?", AuthorRole: "operator", Round: 1,
		Status: projectstate.ReviewCommentOpen, Replies: []projectstate.ReviewCommentReply{},
		Type: projectstate.ReviewCommentTypeQuestion, Addressee: "system-architect",
	}
	if answered {
		q.Status = projectstate.ReviewCommentAnswered
		q.Replies = []projectstate.ReviewCommentReply{{ID: "q-1-r1", AuthorRole: "architect", Text: "No — billing is a separate service.", At: arrangedAt.Format(time.RFC3339)}}
	}
	r.Reviews[0].Thread = []projectstate.ReviewComment{q}
	p.ActivityExecution = rows(r)
	mission := missionStatement("Deliver predictably engineered software, operated on the customer's behalf.")
	if missionCommitted {
		p.Mission = committed(mission)
		seedMain(t, p)
		return
	}
	seedMain(t, p)
	p.Mission = awaitingReview(mission)
	seedBranch(t, "activity/D-REQ", p)
}

// sdpAwaitingDecision: the SDP review is presented for management's decision in project design.
func sdpAwaitingDecision(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseProjectDesign)
	p.ActivityList = revision(committed(designPrefixList()), 1)
	p.Network = revision(committed(designPrefixNetwork()), 1)
	p.SdpReview = awaitingReview(&projectstate.SdpReview{Recommendation: "opt-normal", Rationale: "The normal option meets the deadline."})
	return p
}

// constructionWithBranch: a construction activity whose branch and pull request are on record.
func constructionWithBranch(id projectstate.ProjectID, v projectstate.Version, activityID, prRef string) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseConstruction)
	p.ActivityExecution = rows(openRow(activityID, projectstate.ActivityTypeService, "service"))
	p.ActivityGit = map[string]projectstate.ActivityGitStatus{activityID: {
		ActivityID: activityID, BranchName: "activity/" + activityID, BranchRef: "refs/heads/activity/" + activityID,
		PullRequestRef: prRef, CICheck: projectstate.CICheckSuccess, UpdatedAt: arrangedAt,
	}}
	return p
}

// plannedDesignPrefix: a born project whose design-prefix activity list and network are committed.
func plannedDesignPrefix(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseSystemDesign)
	p.ActivityList = revision(committed(designPrefixList()), 1)
	p.Network = revision(committed(designPrefixNetwork()), 1)
	return p
}

// deliveredSystem: a delivered project with its committed deployment & operations model.
func deliveredSystem(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseConstruction)
	p.OperationalConcepts = revision(committed(&projectstate.DeploymentOperationsModel{}), 1)
	return p
}

// committedCommitment: a project in construction whose SDP review is committed (one
// option, recommended and chosen) under committed planning assumptions carrying its
// billing terms (tiered floors, no markup, monthly) and its USD cost currency.
func committedCommitment(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	chosen := projectstate.OptionID("opt-normal")
	p := baseProject(id, v, projectstate.PhaseConstruction)
	p.PlanningAssumptions = revision(committed(&projectstate.PlanningAssumptions{
		Terms:             projectstate.SettlementTerms{ComputeCost: projectstate.ComputeCostTieredFloors, Schedule: projectstate.ScheduleMonthly},
		IndirectDailyRate: projectstate.Money{MinorUnits: 50000, Currency: "USD"},
	}), 1)
	p.SdpReview = revision(committed(&projectstate.SdpReview{
		Options: []projectstate.SdpOptionRow{{
			OptionID: "opt-normal", SolutionKind: projectstate.KindNormalSolution, DurationDays: 100,
			BuildCost: projectstate.Money{MinorUnits: 1000000, Currency: "USD"},
		}},
		Recommendation: "opt-normal",
		ChosenOption:   &chosen,
	}), 1)
	return p
}

// constructionUnderway: a project in construction with its committed system design and a
// construction activity under way.
func constructionUnderway(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseConstruction)
	p.SystemDesign = revision(committed(systemModel()), 1)
	p.ActivityExecution = rows(openRow("C-project-state-access", projectstate.ActivityTypeService, "service"))
	return p
}

// failedConstruction: a construction activity that exited failed on a red pipeline.
func failedConstruction(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseConstruction)
	r := openRow("C-project-state-access", projectstate.ActivityTypeService, "service")
	ended := arrangedAt.Add(time.Hour)
	r.CompletedAt = &ended
	r.FailureReason = projectstate.PipelineFailed
	r.FailureDetail = "pipeline failed: build red"
	p.ActivityExecution = rows(r)
	return p
}

// pausedConstruction: the operator paused the project in construction.
func pausedConstruction(id projectstate.ProjectID, v projectstate.Version) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseConstruction)
	p.OperatorPaused = true
	p.PauseReason = "Operator pause: upstream incident under investigation"
	return p
}

// trackedConstruction: week `week` of construction with points tracking points on record.
func trackedConstruction(id projectstate.ProjectID, v projectstate.Version, week, points int) projectstate.Project {
	p := baseProject(id, v, projectstate.PhaseConstruction)
	cp := &projectstate.ConstructionProgress{Week: week, TotalWeeks: 10}
	for w := 1; w <= points; w++ {
		cp.Points = append(cp.Points, projectstate.EvPoint{Week: w, EarnedPct: float64(w) * 8, PlannedPct: float64(w) * 10})
	}
	p.ConstructionProgress = cp
	return p
}
