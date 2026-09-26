# Stage 4a — the package-merge rename table

Commit **`5354b1f2`** ("feat(delivery): three Managers become one — the model and the
package together") merged `internal/manager/{systemdesign,projectdesign,construction}`
into `internal/manager/delivery`. `arch.CheckFileLayout` allows one impl file and one
test file per component package, so the three `*manager.go` bodies concatenated into
`deliverymanager.go` and the three `manager_test.go` into one — which made every
package-private name the three rails happened to share a redeclaration. Each collision
was resolved into one of four classes: **A** — a generated-type collision, no action,
regenerated once from the merged contract; **B** — an exported registration entrypoint,
collapsed to exactly one (`RegisterManagerWorker`, `RegisterWorker`,
`RegisterSchedules`, `TaskQueue`, `MaterializeActivityPlan`); **C** — a byte-identical
twin, collapsed to ONE copy and marked in place (191 declarations); **D** — a shared
name over a DIFFERENT body, prefixed by rail (`sd`/`pd`/`cs` for helpers,
`Test_SD_`/`Test_PD_`/`Test_CS_` for test functions so `go test` still finds them).
This file is class D: the 199 rows below are what a reader of `coauthorartifact.go`,
`coauthorphase2artifact.go` or `constructactivity.go` needs in order to find the symbol
the pre-merge blame names. It lives here, tracked, because `5354b1f2`'s message pointed
at `.superpowers/sdd/`, which is gitignored.

Class D is large on the projectDesign rail (175 of the 199 rows) for one structural
reason. The merged contract carries **both** session-stage enums — the projectDesign one
inserts `StageAssemblingSDP` at ordinal 2 and shifts `StageAwaitingReview` (2→3) …
`StageDraftFailed` (7→8), and renumbering a wire-visible ordinal is forbidden — so the
Phase-2 enum family became `ProjectSessionStage` / `ProjectStage*` (the last 11 rows
here, and the names `webApp/scripts/gen-enums.mjs` already maps). The taint then
propagates: a body that is byte-identical *today* stops being identical the moment a
symbol it names is prefixed, so every projectDesign declaration that reads its own
`SessionStage` keeps its own copy rather than collapsing onto the systemDesign one.
Without that fixpoint, seven byte-identical twins (`committedSessionView`,
`failedSessionView`, `wedgedSessionView`, `withStageName`, `stageForAttempt`,
`checkReviewPrecondition`, `fakeEncodedSessionView`) would have collapsed onto the
systemDesign copy and silently compared the Phase-2 rail against the systemDesign
ordinals.

## `GetEpisodeTimeline` is routed, not collapsed

`QueryProjectView(timeline)` forwards to the **construction** `GetEpisodeTimeline`. The
three rails' bodies are *behaviour-equivalent*, **not byte-identical** — they are methods
on three different receivers, so they never collided and class C never applied to them;
the dispatcher picks one deliberately. The three leaves they reach differ only in ways
that cannot change an answer:

- `sdMapRAError` vs the surviving `mapRAError` — identical apart from the function name
  (the bodies' only other divergence is a doc comment).
- `sdEpisodeViewOutcome` vs the surviving `episodeViewOutcome` — likewise.
- `csEpisodeViewKind` vs the surviving `episodeViewKind` — the five real
  `episode.EpisodeKind` variants map identically; the two differ ONLY in the
  `default:` arm (`EpisodeKindConstruction` vs `EpisodeKindDesign`), which the
  exhaustive linter makes unreachable because every defined variant has its own case.

So the timeline read answers the same thing for a design episode as it did before the
merge. 4b collapses the three into one for real.

## The advisory-count delta, re-measured

Measured on the committed state at `b10c9fce` (the commit `5354b1f2` landed on) with
the current rule code:

```
mkdir -p /tmp/base/.aiarch/state
git show b10c9fce:.aiarch/state/project.json > /tmp/base/.aiarch/state/project.json
cd server && GOWORK=off go run ./cmd/aiarch-state-mcp validate --root /tmp/base --slot System
```

| Δ | rule | why |
|---|---|---|
| **−1** | `APPC-CARD-SUB-MGR` | "strives for ≤3"; 5 Managers become 3, so it goes silent |
| **−3 / +1** | `APPC-SVC-STRIVE` | counts by COMPONENT: the three deleted Managers stop reporting "has 0 ops", `DeliveryManager` joins the same tally (21 → 19) |
| **−2** | `DH-CONTRACT-OPCOUNT-MAX` | the rule is `case n > 12`; its two subjects were `systemDesignManager` (16) and `constructionManager` (13), and the largest survivor is 12 (`deliveryManager`, `constructionTransitionAccess`, `activityExecutionAccess`, all exactly at the limit) |
| **0** | `SYS-CARD-RATIO` | re-words itself from "7 Engines but only 5 Managers" to "7 vs 3" — same id, same severity |
| **0** | `DH-CONTRACT-DEADOP` | **2 before, 2 after, byte-identical messages**: `AcknowledgeStaleBasis` (activityExecutionAccess + projectStateAccess) and `RecordOperatorNote` (activityExecutionAccess + constructionTransitionAccess) |
| **0** | `DH-CARD-ENGINES`, `APPC-SVC-AVOID-12`, `DEP-PLANNED-SKIPPED` | unmoved |

**Baseline 48 → end state 43, net −5, 0 errors throughout.** The end state (43/0) is
also what the stage-4a plan predicted.

> Settled in re-review: baseline 48, end state 43, net −5, with `DH-CONTRACT-DEADOP`
> unmoved at 2→2 (`AcknowledgeStaleBasis`, `RecordOperatorNote`) — the 46 came from a
> stale scratch blob (sha `1883c2a1`) rather than `b10c9fce`'s `ccf09f4d`.

## The table

199 rows: 160 projectDesign, 35 construction, 4 systemDesign.

| old name | rail | new name | where it lives now |
|---|---|---|---|
| `SessionStage` | projectDesign | `ProjectSessionStage` | contract.gen.go (Step-3 enum family) |
| `SessionStageUnknown` | projectDesign | `ProjectSessionStageUnknown` | contract.gen.go (Step-3 enum family) |
| `SessionStateView` | projectDesign | `ProjectSessionStateView` | contract.gen.go (Step-3 enum family) |
| `StageAssemblingSDP` | projectDesign | `ProjectStageAssemblingSDP` | contract.gen.go (Step-3 enum family) |
| `StageAwaitingReview` | projectDesign | `ProjectStageAwaitingReview` | contract.gen.go (Step-3 enum family) |
| `StageCommitted` | projectDesign | `ProjectStageCommitted` | contract.gen.go (Step-3 enum family) |
| `StageDraftFailed` | projectDesign | `ProjectStageDraftFailed` | contract.gen.go (Step-3 enum family) |
| `StageDrafting` | projectDesign | `ProjectStageDrafting` | contract.gen.go (Step-3 enum family) |
| `StageRedrafting` | projectDesign | `ProjectStageRedrafting` | contract.gen.go (Step-3 enum family) |
| `StageRefused` | projectDesign | `ProjectStageRefused` | contract.gen.go (Step-3 enum family) |
| `StageWithdrawn` | projectDesign | `ProjectStageWithdrawn` | contract.gen.go (Step-3 enum family) |
| `TestSessionStageLabel_Map` | projectDesign | `Test_PD_SessionStageLabel_Map` | manager_test.go |
| `TestWithStageName_StampsLabel` | projectDesign | `Test_PD_WithStageName_StampsLabel` | manager_test.go |
| `Test_AcknowledgeStaleBasis_CompletedSession_PassesLivenessGate` | projectDesign | `Test_PD_AcknowledgeStaleBasis_CompletedSession_PassesLivenessGate` | manager_test.go |
| `Test_AcknowledgeStaleBasis_LiveAmendmentSession_FailedPrecondition` | projectDesign | `Test_PD_AcknowledgeStaleBasis_LiveAmendmentSession_FailedPrecondition` | manager_test.go |
| `Test_AcknowledgeStaleBasis_NoSession_PassesLivenessGate` | projectDesign | `Test_PD_AcknowledgeStaleBasis_NoSession_PassesLivenessGate` | manager_test.go |
| `Test_AcknowledgeStaleBasis_RequiresNote` | projectDesign | `Test_PD_AcknowledgeStaleBasis_RequiresNote` | manager_test.go |
| `Test_AnswerEpisodeWatch_CancelledCallerContext_StillRecords` | projectDesign | `Test_PD_AnswerEpisodeWatch_CancelledCallerContext_StillRecords` | manager_test.go |
| `Test_AnswerEpisodeWatch_DeadlineRecordsGap` | projectDesign | `Test_PD_AnswerEpisodeWatch_DeadlineRecordsGap` | manager_test.go |
| `Test_AnswerEpisodeWatch_PermanentAppendFailure_GivesUpBounded` | projectDesign | `Test_PD_AnswerEpisodeWatch_PermanentAppendFailure_GivesUpBounded` | manager_test.go |
| `Test_AnswerEpisodeWatch_PersistsAnswerEpisode` | projectDesign | `Test_PD_AnswerEpisodeWatch_PersistsAnswerEpisode` | manager_test.go |
| `Test_AnswerEpisodeWatch_TransientAppendFailure_Retries` | projectDesign | `Test_PD_AnswerEpisodeWatch_TransientAppendFailure_Retries` | manager_test.go |
| `Test_CoAuthor_ActiveSubStep_ClearsAtDraftFailedGate` | projectDesign | `Test_PD_CoAuthor_ActiveSubStep_ClearsAtDraftFailedGate` | manager_test.go |
| `Test_CoAuthor_CancelledWithLateEpisode_RecordsTheEpisodeNotAGap` | projectDesign | `Test_PD_CoAuthor_CancelledWithLateEpisode_RecordsTheEpisodeNotAGap` | manager_test.go |
| `Test_CoAuthor_DraftFailedThenRetry_DistinctIdempotencyKey` | projectDesign | `Test_PD_CoAuthor_DraftFailedThenRetry_DistinctIdempotencyKey` | manager_test.go |
| `Test_CoAuthor_EpisodeAppendFailure_DoesNotFailBusinessFlow` | projectDesign | `Test_PD_CoAuthor_EpisodeAppendFailure_DoesNotFailBusinessFlow` | manager_test.go |
| `Test_CoAuthor_MissingSummary_PersistsGapRecord` | projectDesign | `Test_PD_CoAuthor_MissingSummary_PersistsGapRecord` | manager_test.go |
| `Test_CoAuthor_PhaseCancelled_LandsInStageDraftFailed` | projectDesign | `Test_PD_CoAuthor_PhaseCancelled_LandsInStageDraftFailed` | manager_test.go |
| `Test_CoAuthor_PhaseFailed_LandsInStageDraftFailed_NotPerpetualDrafting` | projectDesign | `Test_PD_CoAuthor_PhaseFailed_LandsInStageDraftFailed_NotPerpetualDrafting` | manager_test.go |
| `Test_CoAuthor_Reject_LoopsToFreshDispatch` | projectDesign | `Test_PD_CoAuthor_Reject_LoopsToFreshDispatch` | manager_test.go |
| `Test_CoAuthor_RemoteVenue_WritesNoGapRecord` | projectDesign | `Test_PD_CoAuthor_RemoteVenue_WritesNoGapRecord` | manager_test.go |
| `Test_CoAuthor_VibesAutogate_VersionGate_PreFeatureStaysHumanGated` | projectDesign | `Test_PD_CoAuthor_VibesAutogate_VersionGate_PreFeatureStaysHumanGated` | manager_test.go |
| `Test_CoAuthor_VibesPolicy_AutoApproves_NoHumanSignal` | projectDesign | `Test_PD_CoAuthor_VibesPolicy_AutoApproves_NoHumanSignal` | manager_test.go |
| `Test_CommittedSessionView_CarriesReviewThread` | projectDesign | `Test_PD_CommittedSessionView_CarriesReviewThread` | manager_test.go |
| `Test_DesignActivityFor_Phase2KindsAreNotTheSdpGate` | projectDesign | `Test_PD_DesignActivityFor_Phase2KindsAreNotTheSdpGate` | manager_test.go |
| `Test_DesignSession_AutogateMatchesTheEngine` | projectDesign | `Test_PD_DesignSession_AutogateMatchesTheEngine` | manager_test.go |
| `Test_GetEpisodeTimeline_EmptyEpisodeID_ContractMisuse` | construction | `Test_CS_GetEpisodeTimeline_EmptyEpisodeID_ContractMisuse` | manager_test.go |
| `Test_GetEpisodeTimeline_EmptyEpisodeID_ContractMisuse` | projectDesign | `Test_PD_GetEpisodeTimeline_EmptyEpisodeID_ContractMisuse` | manager_test.go |
| `Test_GetEpisodeTimeline_EmptyProjectID_ContractMisuse` | construction | `Test_CS_GetEpisodeTimeline_EmptyProjectID_ContractMisuse` | manager_test.go |
| `Test_GetEpisodeTimeline_EmptyProjectID_ContractMisuse` | projectDesign | `Test_PD_GetEpisodeTimeline_EmptyProjectID_ContractMisuse` | manager_test.go |
| `Test_GetEpisodeTimeline_GapRecord_ReturnsEmptyTimeline` | construction | `Test_CS_GetEpisodeTimeline_GapRecord_ReturnsEmptyTimeline` | manager_test.go |
| `Test_GetEpisodeTimeline_GapRecord_ReturnsEmptyTimeline` | projectDesign | `Test_PD_GetEpisodeTimeline_GapRecord_ReturnsEmptyTimeline` | manager_test.go |
| `Test_GetEpisodeTimeline_ListError_MapsInfrastructure` | construction | `Test_CS_GetEpisodeTimeline_ListError_MapsInfrastructure` | manager_test.go |
| `Test_GetEpisodeTimeline_ListError_MapsInfrastructure` | projectDesign | `Test_PD_GetEpisodeTimeline_ListError_MapsInfrastructure` | manager_test.go |
| `Test_GetEpisodeTimeline_StitchesSequentialEventsWithTypes` | construction | `Test_CS_GetEpisodeTimeline_StitchesSequentialEventsWithTypes` | manager_test.go |
| `Test_GetEpisodeTimeline_StitchesSequentialEventsWithTypes` | projectDesign | `Test_PD_GetEpisodeTimeline_StitchesSequentialEventsWithTypes` | manager_test.go |
| `Test_GetEpisodeTimeline_TraceFileNotFound_ReturnsEmptyTimeline` | construction | `Test_CS_GetEpisodeTimeline_TraceFileNotFound_ReturnsEmptyTimeline` | manager_test.go |
| `Test_GetEpisodeTimeline_TraceFileNotFound_ReturnsEmptyTimeline` | projectDesign | `Test_PD_GetEpisodeTimeline_TraceFileNotFound_ReturnsEmptyTimeline` | manager_test.go |
| `Test_GetEpisodeTimeline_TraceReadError_MapsInfrastructure` | construction | `Test_CS_GetEpisodeTimeline_TraceReadError_MapsInfrastructure` | manager_test.go |
| `Test_GetEpisodeTimeline_TraceReadError_MapsInfrastructure` | projectDesign | `Test_PD_GetEpisodeTimeline_TraceReadError_MapsInfrastructure` | manager_test.go |
| `Test_GetEpisodeTimeline_UnknownEpisodeID_NotFound` | construction | `Test_CS_GetEpisodeTimeline_UnknownEpisodeID_NotFound` | manager_test.go |
| `Test_GetEpisodeTimeline_UnknownEpisodeID_NotFound` | projectDesign | `Test_PD_GetEpisodeTimeline_UnknownEpisodeID_NotFound` | manager_test.go |
| `Test_GetSessionState_AbnormalClose_SubStepNeverLeaks` | projectDesign | `Test_PD_GetSessionState_AbnormalClose_SubStepNeverLeaks` | manager_test.go |
| `Test_GetSessionState_CompletedCommitted_ReturnsCommittedView` | projectDesign | `Test_PD_GetSessionState_CompletedCommitted_ReturnsCommittedView` | manager_test.go |
| `Test_GetSessionState_CompletedUncommitted_ReturnsHonestTerminal` | projectDesign | `Test_PD_GetSessionState_CompletedUncommitted_ReturnsHonestTerminal` | manager_test.go |
| `Test_GetSessionState_DeadWorkflow_SynthesizesFailedView` | projectDesign | `Test_PD_GetSessionState_DeadWorkflow_SynthesizesFailedView` | manager_test.go |
| `Test_GetSessionState_EmptyProjectID` | construction | `Test_CS_GetSessionState_EmptyProjectID` | manager_test.go |
| `Test_GetSessionState_EmptyProjectID` | projectDesign | `Test_PD_GetSessionState_EmptyProjectID` | manager_test.go |
| `Test_GetSessionState_NamespaceNotFound_IsInfrastructureNot404` | construction | `Test_CS_GetSessionState_NamespaceNotFound_IsInfrastructureNot404` | manager_test.go |
| `Test_GetSessionState_NamespaceNotFound_IsInfrastructureNot404` | projectDesign | `Test_PD_GetSessionState_NamespaceNotFound_IsInfrastructureNot404` | manager_test.go |
| `Test_ListEpisodesForArtifact_EmptyProjectID_ContractMisuse` | projectDesign | `Test_PD_ListEpisodesForArtifact_EmptyProjectID_ContractMisuse` | manager_test.go |
| `Test_ListEpisodesForArtifact_MapsRecordsWithTargetRef` | projectDesign | `Test_PD_ListEpisodesForArtifact_MapsRecordsWithTargetRef` | manager_test.go |
| `Test_ListEpisodesForArtifact_RAError_MapsInfrastructure` | projectDesign | `Test_PD_ListEpisodesForArtifact_RAError_MapsInfrastructure` | manager_test.go |
| `Test_ListEpisodesForArtifact_RoundTripsWritePathTargetRefForm` | projectDesign | `Test_PD_ListEpisodesForArtifact_RoundTripsWritePathTargetRefForm` | manager_test.go |
| `Test_RequestArtifactDraft_EmptyProjectID` | projectDesign | `Test_PD_RequestArtifactDraft_EmptyProjectID` | manager_test.go |
| `Test_RequestArtifactDraft_NoProjectRow_FailedPrecondition` | projectDesign | `Test_PD_RequestArtifactDraft_NoProjectRow_FailedPrecondition` | manager_test.go |
| `Test_RequestArtifactDraft_RejectsEmptyFeedbackEnvelope` | projectDesign | `Test_PD_RequestArtifactDraft_RejectsEmptyFeedbackEnvelope` | manager_test.go |
| `Test_ResolveQuestionBranch_ClosedWorkflowLeftoverBranch_SeedsOnMain` | projectDesign | `Test_PD_ResolveQuestionBranch_ClosedWorkflowLeftoverBranch_SeedsOnMain` | manager_test.go |
| `Test_SessionStageIsLive` | projectDesign | `Test_PD_SessionStageIsLive` | manager_test.go |
| `Test_SubmitReviewDecision_Approve_AtAwaitingReview_Signals` | projectDesign | `Test_PD_SubmitReviewDecision_Approve_AtAwaitingReview_Signals` | manager_test.go |
| `Test_SubmitReviewDecision_Approve_WhileDrafting_FailsWithoutSignal` | projectDesign | `Test_PD_SubmitReviewDecision_Approve_WhileDrafting_FailsWithoutSignal` | manager_test.go |
| `Test_SubmitReviewDecision_RejectRequiresFeedback` | projectDesign | `Test_PD_SubmitReviewDecision_RejectRequiresFeedback` | manager_test.go |
| `Test_SubmitReviewDecision_UnknownDecision` | projectDesign | `Test_PD_SubmitReviewDecision_UnknownDecision` | manager_test.go |
| `Test_SubmitReviewDecision_WrongPhaseKind` | projectDesign | `Test_PD_SubmitReviewDecision_WrongPhaseKind` | manager_test.go |
| `Test_WorkerManifest_ThreadsEveryDependency` | construction | `Test_CS_WorkerManifest_ThreadsEveryDependency` | manager_test.go |
| `Test_WorkerManifest_ThreadsEveryDependency` | projectDesign | `Test_PD_WorkerManifest_ThreadsEveryDependency` | manager_test.go |
| `Test_amendmentIndexFor_Rule` | projectDesign | `Test_PD_amendmentIndexFor_Rule` | manager_test.go |
| `Test_checkReviewPrecondition_Matrix` | projectDesign | `Test_PD_checkReviewPrecondition_Matrix` | manager_test.go |
| `Test_projectEnvelope_PreservesReviewThread` | projectDesign | `Test_PD_projectEnvelope_PreservesReviewThread` | manager_test.go |
| `activityOptions` | construction | `csActivityOptions` | constructionmanager.go |
| `activityOptions` | projectDesign | `pdActivityOptions` | projectdesignmanager.go |
| `answerEpisodeWatch` | projectDesign | `pdAnswerEpisodeWatch` | projectdesignmanager.go |
| `answerJobDispatchKey` | projectDesign | `pdAnswerJobDispatchKey` | projectdesignmanager.go |
| `askQuestionsIdempotencyKey` | projectDesign | `pdAskQuestionsIdempotencyKey` | projectdesignmanager.go |
| `assertSampleEpisodeRecordIdentity` | construction | `csAssertSampleEpisodeRecordIdentity` | manager_test.go |
| `assertSampleEpisodeRecordIdentity` | projectDesign | `pdAssertSampleEpisodeRecordIdentity` | manager_test.go |
| `assertSampleEpisodeRecordView` | construction | `csAssertSampleEpisodeRecordView` | manager_test.go |
| `assertSampleEpisodeRecordView` | projectDesign | `pdAssertSampleEpisodeRecordView` | manager_test.go |
| `captureSeamSummary` | construction | `csCaptureSeamSummary` | manager_test.go |
| `checkNoReplyTo` | projectDesign | `pdCheckNoReplyTo` | projectdesignmanager.go |
| `checkReviewPrecondition` | projectDesign | `pdCheckReviewPrecondition` | projectdesignmanager.go |
| `coAuthorInput` | projectDesign | `pdCoAuthorInput` | coauthorphase2artifact.go |
| `coAuthorState` | projectDesign | `pdCoAuthorState` | projectdesignmanager.go |
| `coAuthorStep` | projectDesign | `pdCoAuthorStep` | coauthorphase2artifact.go |
| `committedSessionView` | projectDesign | `pdCommittedSessionView` | projectdesignmanager.go |
| `committedSlot` | projectDesign | `pdCommittedSlot` | manager_test.go |
| `designActivityFor` | projectDesign | `pdDesignActivityFor` | coauthorphase2artifact.go |
| `designActorOperator` | projectDesign | `pdDesignActorOperator` | coauthorphase2artifact.go |
| `designApproverActor` | projectDesign | `pdDesignApproverActor` | coauthorphase2artifact.go |
| `designPRBody` | projectDesign | `pdDesignPRBody` | coauthorphase2artifact.go |
| `designPRTitle` | projectDesign | `pdDesignPRTitle` | coauthorphase2artifact.go |
| `designPipelinePhase` | projectDesign | `pdDesignPipelinePhase` | coauthorphase2artifact.go |
| `designRoleHuman` | projectDesign | `pdDesignRoleHuman` | coauthorphase2artifact.go |
| `designRoundKeyFor` | projectDesign | `pdDesignRoundKeyFor` | coauthorphase2artifact.go |
| `designRoundReviewers` | projectDesign | `pdDesignRoundReviewers` | coauthorphase2artifact.go |
| `dispatchDesignJobArgs` | projectDesign | `pdDispatchDesignJobArgs` | coauthorphase2artifact.go |
| `dispatchInputJobMode` | projectDesign | `pdDispatchInputJobMode` | projectdesignmanager.go |
| `draftFailedReason` | projectDesign | `pdDraftFailedReason` | coauthorphase2artifact.go |
| `encodeProject` | projectDesign | `pdEncodeProject` | projectdesignmanager.go |
| `episodeIDSeed` | construction | `csEpisodeIDSeed` | constructactivity.go |
| `episodeIDSeed` | projectDesign | `pdEpisodeIDSeed` | coauthorphase2artifact.go |
| `episodeMgr` | construction | `csEpisodeMgr` | manager_test.go |
| `episodeMgr` | projectDesign | `pdEpisodeMgr` | manager_test.go |
| `episodeRecordToView` | construction | `csEpisodeRecordToView` | constructionmanager.go |
| `episodeRecordToView` | systemDesign | `sdEpisodeRecordToView` | systemdesignmanager.go |
| `episodeRecordViews` | construction | `csEpisodeRecordViews` | constructionmanager.go |
| `episodeRecordViews` | systemDesign | `sdEpisodeRecordViews` | systemdesignmanager.go |
| `episodeViewKind` | construction | `csEpisodeViewKind` | constructionmanager.go |
| `episodeViewOutcome` | systemDesign | `sdEpisodeViewOutcome` | systemdesignmanager.go |
| `execLedgerBase` | projectDesign | `pdExecLedgerBase` | manager_test.go |
| `executionKindCoAuthor` | projectDesign | `pdExecutionKindCoAuthor` | projectdesignmanager.go |
| `executionKindPhaseAdvance` | projectDesign | `pdExecutionKindPhaseAdvance` | projectdesignmanager.go |
| `executionKindSDPReview` | projectDesign | `pdExecutionKindSDPReview` | deliverymanager.go (const/var group member) |
| `f29BranchFake` | projectDesign | `pdF29BranchFake` | manager_test.go |
| `failedSessionView` | projectDesign | `pdFailedSessionView` | projectdesignmanager.go |
| `fakeActivityExecution` | construction | `csFakeActivityExecution` | manager_test.go |
| `fakeActivityExecution` | projectDesign | `pdFakeActivityExecution` | manager_test.go |
| `fakeEncodedSessionView` | projectDesign | `pdFakeEncodedSessionView` | manager_test.go |
| `fakePipeline` | construction | `csFakePipeline` | manager_test.go |
| `fakePipeline` | projectDesign | `pdFakePipeline` | manager_test.go |
| `fakeProjectState` | construction | `csFakeProjectState` | manager_test.go |
| `fakeProjectState` | projectDesign | `pdFakeProjectState` | manager_test.go |
| `fakeQueryClient` | construction | `csFakeQueryClient` | manager_test.go |
| `fakeQueryClient` | projectDesign | `pdFakeQueryClient` | manager_test.go |
| `feedbackToLedgerComments` | projectDesign | `pdFeedbackToLedgerComments` | coauthorphase2artifact.go |
| `isLiveSessionStage` | projectDesign | `pdIsLiveSessionStage` | projectdesignmanager.go |
| `isRAConflict` | construction | `csIsRAConflict` | constructionmanager.go |
| `jobModeAnswer` | projectDesign | `pdJobModeAnswer` | projectdesignmanager.go |
| `jobModeDraft` | projectDesign | `pdJobModeDraft` | projectdesignmanager.go |
| `mapQueryError` | construction | `csMapQueryError` | constructionmanager.go |
| `mapQueryError` | projectDesign | `pdMapQueryError` | projectdesignmanager.go |
| `mapRAError` | systemDesign | `sdMapRAError` | systemdesignmanager.go |
| `mapReadProjectError` | projectDesign | `pdMapReadProjectError` | projectdesignmanager.go |
| `mapStartError` | construction | `csMapStartError` | constructionmanager.go |
| `mapStartError` | projectDesign | `pdMapStartError` | projectdesignmanager.go |
| `neutralToRAPhase` | projectDesign | `pdNeutralToRAPhase` | manager_test.go |
| `newFakePipeline` | construction | `csNewFakePipeline` | manager_test.go |
| `newFakePipeline` | projectDesign | `pdNewFakePipeline` | manager_test.go |
| `newRailWorkflows` | projectDesign | `pdNewRailWorkflows` | manager_test.go |
| `newScriptedRail` | projectDesign | `pdNewScriptedRail` | manager_test.go |
| `newWorkflows` | construction | `csNewWorkflows` | constructionmanager.go |
| `newWorkflows` | projectDesign | `pdNewWorkflows` | manager_test.go |
| `phaseAdvanceWorkflowID` | projectDesign | `pdPhaseAdvanceWorkflowID` | projectdesignmanager.go |
| `pipelineCancelled` | projectDesign | `pdPipelineCancelled` | coauthorphase2artifact.go |
| `pipelineFailed` | projectDesign | `pdPipelineFailed` | coauthorphase2artifact.go |
| `pipelineObservation` | construction | `csPipelineObservation` | constructactivity.go |
| `pipelineObservation` | projectDesign | `pdPipelineObservation` | coauthorphase2artifact.go |
| `pipelinePending` | projectDesign | `pdPipelinePending` | coauthorphase2artifact.go |
| `pipelinePhase` | projectDesign | `pdPipelinePhase` | coauthorphase2artifact.go |
| `pipelinePhaseUnknown` | projectDesign | `pdPipelinePhaseUnknown` | deliverymanager.go (const/var group member) |
| `pipelineRunning` | projectDesign | `pdPipelineRunning` | coauthorphase2artifact.go |
| `pipelineSucceeded` | projectDesign | `pdPipelineSucceeded` | coauthorphase2artifact.go |
| `pullRequestStatusView` | construction | `csPullRequestStatusView` | constructactivity.go |
| `querySessionState` | projectDesign | `pdQuerySessionState` | projectdesignmanager.go |
| `railAuthRetryBaseBackoff` | projectDesign | `pdRailAuthRetryBaseBackoff` | coauthorphase2artifact.go |
| `railAuthRetryLongBaseBackoff` | projectDesign | `pdRailAuthRetryLongBaseBackoff` | coauthorphase2artifact.go |
| `railAuthRetryLongMaxAttempts` | projectDesign | `pdRailAuthRetryLongMaxAttempts` | coauthorphase2artifact.go |
| `railAuthRetryLongMaxBackoff` | projectDesign | `pdRailAuthRetryLongMaxBackoff` | coauthorphase2artifact.go |
| `railAuthRetryMaxAttempts` | projectDesign | `pdRailAuthRetryMaxAttempts` | coauthorphase2artifact.go |
| `railAuthRetryMaxBackoff` | projectDesign | `pdRailAuthRetryMaxBackoff` | coauthorphase2artifact.go |
| `readProjectActivityOptions` | construction | `csReadProjectActivityOptions` | constructionmanager.go |
| `readProjectActivityOptions` | projectDesign | `pdReadProjectActivityOptions` | projectdesignmanager.go |
| `registerCoAuthor` | projectDesign | `pdRegisterCoAuthor` | manager_test.go |
| `registerGenActivities` | projectDesign | `pdRegisterGenActivities` | manager_test.go |
| `registerGenActivityExecution` | construction | `csRegisterGenActivityExecution` | manager_test.go |
| `registerGenActivityExecution` | projectDesign | `pdRegisterGenActivityExecution` | manager_test.go |
| `registerRailCoAuthor` | projectDesign | `pdRegisterRailCoAuthor` | manager_test.go |
| `reviewDecisionSignal` | projectDesign | `pdReviewDecisionSignal` | coauthorphase2artifact.go |
| `reviewerUtteranceRole` | projectDesign | `pdReviewerUtteranceRole` | coauthorphase2artifact.go |
| `sampleEpisodeRecord` | construction | `csSampleEpisodeRecord` | manager_test.go |
| `sampleEpisodeRecord` | projectDesign | `pdSampleEpisodeRecord` | manager_test.go |
| `scriptedRail` | projectDesign | `pdScriptedRail` | manager_test.go |
| `seqEvent` | projectDesign | `pdSeqEvent` | manager_test.go |
| `seqLog` | projectDesign | `pdSeqLog` | manager_test.go |
| `seqProjectState` | projectDesign | `pdSeqProjectState` | manager_test.go |
| `sessionStageIsLive` | projectDesign | `pdSessionStageIsLive` | projectdesignmanager.go |
| `sessionStageLabel` | projectDesign | `pdSessionStageLabel` | projectdesignmanager.go |
| `signalRedraft` | projectDesign | `pdSignalRedraft` | deliverymanager.go (const/var group member) |
| `signalReviewDecision` | projectDesign | `pdSignalReviewDecision` | projectdesignmanager.go |
| `signalSDPDecision` | projectDesign | `pdSignalSDPDecision` | deliverymanager.go (const/var group member) |
| `signalSetCommentStatus` | projectDesign | `pdSignalSetCommentStatus` | projectdesignmanager.go |
| `slotFor` | projectDesign | `pdSlotFor` | projectdesignmanager.go |
| `stageForAttempt` | projectDesign | `pdStageForAttempt` | coauthorphase2artifact.go |
| `stubEncodedStage` | projectDesign | `pdStubEncodedStage` | manager_test.go |
| `validateReviewDecisionArgs` | projectDesign | `pdValidateReviewDecisionArgs` | projectdesignmanager.go |
| `wedgedSessionView` | projectDesign | `pdWedgedSessionView` | projectdesignmanager.go |
| `withStageName` | projectDesign | `pdWithStageName` | projectdesignmanager.go |
| `workflows` | construction | `csWorkflows` | constructionmanager.go |
| `workflows` | projectDesign | `pdWorkflows` | projectdesignmanager.go |
