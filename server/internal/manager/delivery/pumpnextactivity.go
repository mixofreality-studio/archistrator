package delivery

import (
	"encoding/json"
	"slices"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// ===========================================================================
// PumpNextActivityWorkflow — op 2.1's execution: the project's ONE pump,
// {projectId}:nextActivity (pumpWorkflowID). Started — or joined — by
// ExecuteNextActivity (Begin / MCP) and by PumpSweepWorkflow's 30s Schedule fan-out.
//
// STAGE 4b2 CHANGED ITS SHAPE. It used to select ONE activity, start a child and
// BLOCK on child.Get until that child reached its terminal, then ContinueAsNew to
// pick the next one — a strictly serial cascade, one pump RUN per dispatched
// activity. It now selects EVERY eligible activity, starts a child for each, and
// PARKS on a workflow.Selector over {the child futures, lease requests, finish
// signals, the pause channel, a reconcile timer}. Exactly ONE workflow, not two:
// the queue is the pump.
//
// WHAT THE PUMP SERIALISES IS ADMISSION, NOT WORK — AND THE SCOPE IS ONE PUMP CHAIN,
// NOT THE PROJECT. At most one activity holds the MAIN-WRITE LEASE at a time WITHIN a
// single pump's continue-as-new chain. That is the true bound and it is narrower than
// this header used to claim, because LeaseHolder lives in pumpState/pumpInput and a
// ContinueAsNew payload is the only thing it survives. The child takes the lease
// immediately before its merge tail and gives it up at its terminal; the pump never
// learns what a merge is.
//
// WHAT HAPPENS AT A CHAIN BOUNDARY, stated plainly because it is the ORDINARY case and
// not an exotic one. Any pump run that ENDS while a lease is held exits holding it: a
// pause that lands while the run is parked (pumpPark → paused → pumpLoop returns), a
// child failure stopping the cascade (G-P12), or any error return. Thirty seconds later
// the sweep starts a FRESH chain with LeaseHolder == nil and an empty Started. Child A is
// meanwhile still inside its merge tail — which holds a HUMAN approval gate, so "still
// there an hour later" is normal — and the new chain knows nothing of it: it dispatches
// D, D asks, D is granted, and A and D write main at once. NOTHING IN THIS FILE PREVENTS
// THAT. What prevents DAMAGE there is the SECOND mechanism, unchanged and exactly the
// state this wave started from: the per-activity CAS and applyMutationOnBranchFiles'
// dedup + version guard + ref-CAS, which serialise per ROW. Row-level and main-level are
// TWO mechanisms, and conflating them is how an earlier ruling put the merge in the pump,
// which spec §10 forbids. Widening the lease past a chain would mean durable admission
// state outside the pump — a different design, not a fix to this one.
//
// AND "THE LEASE FAILS OPEN" IS THE NORMAL STATE AFTER ANY PUMP RESTART, not a rare fault
// path. A lease request from a child that predates the restart names an id the new chain
// never started, so pumpGrantLease drops it — correctly, because Temporal does not
// authenticate a signaler — and requestMainWriteLease's bounded wait expires into an
// UNLEASED merge tail, held by the row-level mechanism alone.
//
// WHAT THE PUMP DOES NOT DO: the merge. The child's merge tail is three things — a
// variance loop with a HUMAN hold (runWalkMerge), a credential re-mint with merge
// preconditions (mergeAndRecord) and N slot commits (commitDesignArtifacts) — and
// moving them up would put a gate, an escalation inbox and a gate ledger in the
// pump. That is the god-workflow arriving by the back door, one wave after seven
// workflows were deleted to avoid exactly that.
//
// ON READ AMPLIFICATION, stated so nobody re-derives it wrong: the 27 whole-aggregate
// reads an earlier draft claimed this change collapses were 27 RUNS (one ContinueAsNew
// per dispatch), and a single long-lived pump STILL RE-READS ON EVERY WAKE-UP —
// because the CHILD writes head-state and the frontier genuinely moves, so deriving
// the frontier from carried in-memory state is stale by construction. The read-count
// win is the BATCHED plan read, and that is a later wave's. This change buys
// parallelism and a non-blocking façade, not fewer reads.
// ===========================================================================

// pumpInput is the start (and ContinueAsNew) payload for PumpNextActivityWorkflow.
//
// IT IS ALSO pumpState's wire form, which is why every member is JSON-serialisable and
// BOUNDED. The old one-field input had the unbounded-history property for free; this one
// owes it, and Test_Pump_ContinueAsNewPayloadIsBoundedByThePlan is where the debt is
// collected — a field added without a stated bound fails that test.
//
// THE BOUND: Started holds at most one id per activity in the committed plan (30 on this
// repo), Carried at most one finish and one lease request per activity, and the lease is
// ONE activity. There is no per-EVENT accumulation anywhere in it, which is the property
// that matters: a cascade of any length carries the same shape.
type pumpInput struct {
	ProjectID ProjectID
	// OperatorDriven is RETIRED for new pumps (plan B1.7). It marked a pump the operator
	// started (Begin), which used to ignore the RECORDED pause — the de-facto resume
	// before ResumeProject existed. No caller sets it any more, and a pump on
	// pump-honors-recorded-pause v2 ignores it: the recorded pause binds every pump, and
	// ResumeProject clears it. The field stays only so that an in-flight pump's
	// ContinueAsNew input and a history recorded at v1 still decode and replay (v1 keeps
	// its exemption). Retire it with the version markers after the drain. Explicit pause
	// SIGNALS are honoured regardless.
	OperatorDriven bool
	// Started is every activity THIS pump chain has EVER dispatched, finished or not. It
	// exists independent of the lease and it is NOT an optimisation: see pumpStartFrontier
	// for the measurement that makes it load-bearing.
	//
	// WHY "EVER" AND NOT "IN FLIGHT". A child start is idempotent on its id only while the
	// execution is RUNNING; once it closes, the same id starts a SECOND execution. So a
	// frontier pass that ran against a head-state read taken before a just-finished child's
	// row landed would re-dispatch an activity that is already done — a second walk over one
	// row. One pump chain dispatches one activity at most once, and this is where that is
	// remembered.
	Started []ActivityID
	// Finished is the subset of Started this pump knows to be over. In-flight is the
	// difference, and it is computed rather than stored so the two can never disagree.
	Finished []ActivityID
	// LeaseHolder is the one activity holding the main-write lease, or nil. AT MOST ONE,
	// EVER — that is the invariant the whole queue exists for.
	LeaseHolder *ActivityID
	// LeaseEpoch is bumped on every grant. A child that was revoked and re-granted
	// underneath itself must be able to refuse a stale grant, and the epoch is how.
	LeaseEpoch int64
	// LeaseGrantedAt is workflow.Now at the grant, for the deadline check.
	LeaseGrantedAt *time.Time
	// Carried is what the pre-ContinueAsNew drain found. See pumpDrainBeforeContinue.
	Carried []pumpCarriedSignal
}

// The two kinds of signal a pump run can be holding when it continues as new. Plain
// strings rather than a typed enum: they cross a JSON boundary in pumpCarriedSignal and
// the vocabulary is closed at two, so a named-int would buy an encode/decode rule and no
// safety.
const (
	pumpCarriedFinish       = "finish"
	pumpCarriedLeaseRequest = "leaseRequest"
)

// pumpCarriedSignal is one message that arrived on a run that is about to ContinueAsNew.
// The SDK DISCARDS a buffered signal at the boundary, so a run that continues must drain
// what it holds and put it here; the next run replays them before it computes anything.
type pumpCarriedSignal struct {
	Kind       string                       `json:"kind"`
	ActivityID ActivityID                   `json:"activityId"`
	Outcome    projectstate.ActivityOutcome `json:"outcome,omitempty"`
}

// activityLeaseRequest is the child → pump ask for the main-write lease.
type activityLeaseRequest struct {
	ActivityID ActivityID `json:"activityId"`
}

// activityLeaseGrant is the pump → child answer. Epoch is what makes a stale grant
// refusable: the pump bumps it on every grant, so a child can tell the grant it asked for
// from one issued to a successor after the pump decided the child was gone.
type activityLeaseGrant struct {
	ActivityID ActivityID `json:"activityId"`
	Epoch      int64      `json:"epoch"`
}

// activityFinishedSignal is the child → pump report that an activity's walk reached a
// terminal the platform RECORDED. It releases the lease if the activity held one.
//
// IT IS NOT SENT BY failWalk, and that asymmetry is the whole of how G-P12 survives for a
// child whose future this run does not hold — see pumpReconcile.
type activityFinishedSignal struct {
	ActivityID ActivityID                   `json:"activityId"`
	Outcome    projectstate.ActivityOutcome `json:"outcome,omitempty"`
}

// pumpLeaseSignal is the constraint pumpReceiveSignal decodes into: the three lease-channel
// payloads and nothing else. It is a closed union rather than `any` so that a fourth signal
// kind cannot be routed through this decoder without being named here.
type pumpLeaseSignal interface {
	activityLeaseRequest | activityLeaseGrant | activityFinishedSignal
}

// pumpReceiveSignal decodes one message off a lease channel, and it exists because the
// obvious code was WRONG ON THE WIRE — measured on a real dev server, not reasoned about.
//
// THE DEFECT IT FIXES (stage 4b2 Task 14). All three lease messages are delivered through
// messageBus.deliverSignal, which hands the Temporal client raw []byte and therefore encodes
// them as binary/plain. The SDK's ByteSlicePayloadConverter can only assign such a payload to
// a *[]byte, so ReceiveChannel.Receive into a STRUCT fails, and a failed assign is not an
// error a workflow can see: the SDK logs "Corrupted signal received on channel X" and DROPS
// the message. The first capture of pump-dispatch-then-quiesce recorded exactly that —
//
//	ERROR Deserialization error. Corrupted signal received on channel activityLeaseRequested.
//	  WorkflowType constructionPumpNextActivity
//	  Error payload item 0: type *delivery.activityLeaseRequest: type is not *[]byte
//	DEBUG NewTimer WorkflowID shape-p:activity:C-ONE Duration 2h0m0s
//
// — every child arming activityLeaseGrantWaitBudget and the pump never seeing a request. The
// whole main-write lease was INERT end to end: no request reached the pump, no grant reached
// a child, no finish reached the pump. It failed open every time (which is why nothing else
// caught it: the tails ran unleased on the row-level mechanism, exactly as the restart path
// is documented to), and it cost two hours of workflow time per activity.
//
// THIS FILE ALREADY KNEW. pumpPauseRequested decodes the pause channel into `any` for this
// precise reason and says so — "a struct target would drop the former; `any` accepts both" —
// and the lease channels were written against the same transport without inheriting it.
//
// SO: receive into `any` (which the converter serves for BOTH wire forms) and normalise.
// json/plain — a struct signalled directly, which every test environment and no production
// caller produces — arrives as map[string]any; binary/plain arrives as []byte. Both are
// re-decoded into the typed message. Deterministic: json.Marshal sorts map keys, and a
// receive-plus-decode emits no workflow command at all, which is why this change cannot move
// a recorded command sequence and the eight post-4b1 child fixtures stay green.
//
// A PAYLOAD THAT DECODES INTO NEITHER IS DROPPED, and that is the opposite of the pause
// channel's rule on purpose: an undecodable PAUSE still counts as a pause because the channel
// NAME carries the operator's intent, whereas a lease message whose ACTIVITY ID cannot be
// read names nobody — granting or releasing on it would be acting on a message the pump
// cannot attribute. The caller logs the drop.
// It reports (decoded, present). The two answers are SEPARATE because the drain loop needs
// them to be: "nothing left on this channel" ends the loop, while "one message taken and
// dropped" must not — a drain that stopped on an undecodable message would leave the
// decodable ones behind it to be discarded at the ContinueAsNew boundary, which is the exact
// loss rule 2 exists to prevent.
func pumpReceiveSignal[T pumpLeaseSignal](ctx workflow.Context, ch workflow.ReceiveChannel, out *T) (decoded, present bool) {
	var raw any
	if !ch.ReceiveAsync(&raw) {
		return false, false
	}
	return pumpDecodeSignal(ctx, raw, out), true
}

// pumpReceiveSignalBlocking is pumpReceiveSignal for a selector callback, where the message
// is already known to be there and Receive is the required read. It reports false on a
// payload it cannot attribute, having consumed it.
func pumpReceiveSignalBlocking[T pumpLeaseSignal](ctx workflow.Context, ch workflow.ReceiveChannel, out *T) bool {
	var raw any
	ch.Receive(ctx, &raw)
	return pumpDecodeSignal(ctx, raw, out)
}

func pumpDecodeSignal[T pumpLeaseSignal](ctx workflow.Context, raw any, out *T) bool {
	var b []byte
	switch v := raw.(type) {
	case []byte:
		b = v
	case map[string]any:
		m, err := json.Marshal(v)
		if err != nil {
			workflow.GetLogger(ctx).Error("pump: a lease-channel payload could not be re-encoded; dropping it", "err", err.Error())
			return false
		}
		b = m
	default:
		workflow.GetLogger(ctx).Error("pump: a lease-channel payload arrived in an unreadable wire form; dropping it")
		return false
	}
	if err := json.Unmarshal(b, out); err != nil {
		workflow.GetLogger(ctx).Error("pump: a lease-channel payload could not be decoded; dropping it", "err", err.Error())
		return false
	}
	return true
}

// pumpDispatch is THIS pump RUN's synchronous dispatch decision, surfaced to the
// façade via the queryPumpDispatch Query so ExecuteNextActivity can return the run's
// outcome (dispatched X, or quiescent) WITHOUT blocking on the background cascade. A
// caller that joined an already-running pump reads that run's decision, not a fresh one
// of its own. Decided flips true the moment this run reaches its decision point —
// quiescent (pause / no-project / nothing eligible) or a dispatched frontier — which is
// BEFORE the selector park below; until then the façade keeps polling.
type pumpDispatch struct {
	Decided    bool        `json:"decided"`
	Dispatched bool        `json:"dispatched"`
	ActivityID *ActivityID `json:"activityId,omitempty"`
	// ActivityIDs is the WHOLE frontier this run started, in declaration order. ActivityID
	// is the first of them and is kept because the façade's PumpResult carries exactly one
	// (see the BLOCKED note at awaitDispatchDecision): this Query is where the rest is
	// observable until the contract grows PumpResult.activityIds.
	ActivityIDs []ActivityID `json:"activityIds,omitempty"`
}

// pumpPaceInterval is the short durable wait between the pump's loop ITERATIONS — a
// workflow.Sleep, NOT time.Sleep. It used to pace the continue-as-new self-cascade; it now
// paces the wake-up loop, and it guards the same failure: a wake-up that resolves to "re-read,
// nothing changed" must not spin, because each iteration builds a durable read Activity and a
// fresh timer. The first iteration is NOT paced — a Begin must reach its decision point
// without a second of latency in front of it.
const pumpPaceInterval = 1 * time.Second

// pumpReconcileInterval is the tick. It is a RECONCILE, not a poll: see pumpReconcile.
// 30 s matches the sweep's cadence deliberately — the sweep is the pump's own restart
// backstop, so a fact that costs at most one tick here costs at most one sweep there.
const pumpReconcileInterval = 30 * time.Second

// pumpLeaseDeadline is how long a lease may be held before the pump asks whether its
// holder is still alive. It is NOT a revocation timeout: a live holder is RENEWED. Ten
// minutes is far above a merge tail that is going well and far below a human hold, which
// is exactly why the deadline cannot be allowed to revoke on its own — see pumpCheckLease.
const pumpLeaseDeadline = 10 * time.Minute

// pumpHistoryBudget is the pump's own ceiling, below the server's, and it is the ONLY
// reason this workflow continues as new any more. The old pump continued once per
// dispatched activity; this one parks, so its history grows with wake-ups (one timer per
// 30 s, one event per signal) rather than with dispatches. The same shape and the same
// number the child uses (deliveryActivityHistoryBudget), for the same reason.
const pumpHistoryBudget = 4000

// pumpLivenessProbeEpoch is the epoch at or below which a CHILD refuses a grant, and it is
// 0 because no genuine grant can ever carry it: pumpDeliverGrant bumps LeaseEpoch before it
// delivers, so every real grant carries at least 1.
//
// NO SIGNAL THE PUMP SENDS EVER CARRIES IT, and this comment used to say the opposite (fix
// round 1, F4). The liveness probe in pumpCheckLease deliberately re-delivers the HOLDER'S
// OWN grant at st.LeaseEpoch and never at 0, because the probe is a RENEWAL — a live holder
// that reads it sees the grant it already has, which is the whole reason the deadline does
// not revoke. The probe's only observable is the DELIVERY's answer (NotFound means the
// execution is gone), and a grant at epoch 0 would be one the holder is required to ignore,
// so sending one would buy nothing.
//
// What this constant IS, therefore: a child-side FLOOR, read at exactly one site —
// requestMainWriteLease's `grant.Epoch <= pumpLivenessProbeEpoch` arm in deliveryactivity.go
// — where it refuses a zero-valued or replayed grant envelope, which is the only way one can
// arrive.
const pumpLivenessProbeEpoch int64 = 0

// pumpState is what a run of the pump knows. Everything in it rides ContinueAsNew through
// pumpInput, plus the two things that cannot: the futures of children started in THIS run,
// and the per-run blocked set.
type pumpState struct {
	Started        []ActivityID
	Finished       []ActivityID
	LeaseHolder    *ActivityID
	LeaseEpoch     int64
	LeaseGrantedAt *time.Time
	Carried        []pumpCarriedSignal

	// requests is the queue of unanswered lease asks, in arrival order. It is NOT carried
	// as its own member: an unanswered request blocks ContinueAsNew outright (rule 1), so
	// the only way one reaches the boundary is through the drain, and the drain puts it in
	// Carried.
	requests []ActivityID

	// futures are the children THIS run started, by activity id. They are the run's direct
	// line to Temporal's execution state — ordering, the error channel (G-P12), liveness
	// and guaranteed delivery, all four of child.Get's affordances, for children of this
	// run. A child that predates a ContinueAsNew has none, which is what pumpReconcile is
	// for.
	futures map[ActivityID]workflow.ChildWorkflowFuture

	// blocked is the set of activity ids whose plan defect this RUN has already recorded.
	// Without it the frontier loop would re-record the same verdictBlocked every pass:
	// RecordActivityFailed takes the activity out of NotStarted in the STORE, but this run
	// holds a snapshot read before the write.
	blocked map[string]bool

	// vanished is the set of activity ids whose EXECUTION a delivery answered NotFound for —
	// the pump's one positive fact that a child is gone. It is a note for the NEXT
	// pumpReconcile and NOT a terminal: see markVanished.
	vanished map[ActivityID]bool
}

func pumpStateFrom(in pumpInput) *pumpState {
	return &pumpState{
		Started:        append([]ActivityID(nil), in.Started...),
		Finished:       append([]ActivityID(nil), in.Finished...),
		LeaseHolder:    in.LeaseHolder,
		LeaseEpoch:     in.LeaseEpoch,
		LeaseGrantedAt: in.LeaseGrantedAt,
		futures:        map[ActivityID]workflow.ChildWorkflowFuture{},
		blocked:        map[string]bool{},
		vanished:       map[ActivityID]bool{},
	}
}

// toInput is pumpStateFrom's inverse: the payload the next run resumes from.
func (st *pumpState) toInput(in pumpInput) pumpInput {
	return pumpInput{
		ProjectID:      in.ProjectID,
		OperatorDriven: in.OperatorDriven,
		Started:        append([]ActivityID(nil), st.Started...),
		Finished:       append([]ActivityID(nil), st.Finished...),
		LeaseHolder:    st.LeaseHolder,
		LeaseEpoch:     st.LeaseEpoch,
		LeaseGrantedAt: st.LeaseGrantedAt,
		Carried:        append([]pumpCarriedSignal(nil), st.Carried...),
	}
}

func pumpHolds(ids []ActivityID, id ActivityID) bool {
	return slices.Contains(ids, id)
}

func (st *pumpState) isStarted(id ActivityID) bool  { return pumpHolds(st.Started, id) }
func (st *pumpState) isFinished(id ActivityID) bool { return pumpHolds(st.Finished, id) }

// inFlight is Started minus Finished, in dispatch order.
func (st *pumpState) inFlight() []ActivityID {
	var out []ActivityID
	for _, s := range st.Started {
		if !st.isFinished(s) {
			out = append(out, s)
		}
	}
	return out
}

// idle reports the quiescent condition: nothing in flight, no lease held and no request
// waiting for one.
func (st *pumpState) idle() bool {
	return len(st.inFlight()) == 0 && st.LeaseHolder == nil && len(st.requests) == 0
}

func (st *pumpState) markStarted(id ActivityID) {
	if !st.isStarted(id) {
		st.Started = append(st.Started, id)
	}
}

// releaseLease frees whatever MAIN-WRITE ADMISSION this activity holds — the lease itself
// and any request of its own still queued — without declaring the activity over. It is the
// half of markFinished that a finish signal is allowed to do on its own; see applyFinish.
//
// IDEMPOTENT, and that matters rather than being tidy: a release is normally learned TWICE
// — once from the signal and once from the reconcile — and the second must not free a lease
// a SUCCESSOR has since been granted, so it is conditional on the holder still being this
// activity.
func (st *pumpState) releaseLease(id ActivityID) {
	if st.LeaseHolder != nil && *st.LeaseHolder == id {
		st.LeaseHolder, st.LeaseGrantedAt = nil, nil
	}
	reqs := st.requests[:0]
	for _, r := range st.requests {
		if r != id {
			reqs = append(reqs, r)
		}
	}
	st.requests = reqs
}

// markFinished records an activity as OVER: it leaves the in-flight set, its future is
// discharged and any admission it held is released. Only a fact the pump established for
// itself may call this — a resolved future, or the activity's own terminal ROW.
func (st *pumpState) markFinished(id ActivityID) {
	if !st.isFinished(id) {
		st.Finished = append(st.Finished, id)
	}
	delete(st.futures, id)
	st.releaseLease(id)
}

// markVanished is what BOTH NotFound arms of the lease do, and it is deliberately not
// markFinished — which is what they were, and which left G-P12 with two back doors that
// fix round 1 did not close (fix round 2, C1).
//
// THE DEFECT, and it is F1's twin one door along. A child takes the lease in its merge tail
// and then failWalks: failWalk sends NO finish, so the lease stays held, and after
// pumpLeaseDeadline the liveness probe answers NotFound. markFinished then DISCHARGED THE
// FUTURE, pumpReconcile stopped iterating over the id, f.Get was never called, the error was
// swallowed and the run went QUIESCENT with the cascade free to build on a broken
// dependency. A NotFound genuinely IS a fact the pump established for itself, so the LETTER
// of applyFinish's rule was kept — but "the execution is gone" is not "the activity
// SUCCEEDED", and only the future (or the row) can tell those two apart.
//
// SO A VANISHED EXECUTION RELEASES ITS ADMISSION AND NOTHING ELSE. releaseLease is exactly
// what both sites need — the lease must not be held by a corpse, and a gone child's other
// queued asks must not be answered — and the verdict is left to the next pumpReconcile,
// which reads the FUTURE for a child of this run and a FRESH head-state row for one that
// predates a ContinueAsNew. Deferring is not laziness: the row this run already holds was
// read before the child closed, and judging a vanished child off a stale snapshot is how a
// completed activity gets called broken.
//
// AND THE NOTE IS WHY IT IS A SET AND NOT JUST A RELEASE. A child that is gone, holds no
// future here and never recorded a terminal would otherwise sit in inFlight forever — the
// pump would never quiesce and never end. The mark is what lets pumpReconcile answer that
// case instead of waiting on it. It is RUN-LOCAL, like futures and blocked: across a
// ContinueAsNew it degrades to the row-only reconcile that pre-CAN children already get.
func (st *pumpState) markVanished(id ActivityID) {
	st.vanished[id] = true
	st.releaseLease(id)
}

// applyFinish is the whole meaning of a finish signal, and it is deliberately NOT
// markFinished — which is what it used to be, and which cost this file its most important
// guard.
//
// THE DEFECT, MEASURED (fix round 1, F1). finalizeWalk releases the lease from a DEFER that
// fires on EVERY exit from the merge tail, including the ones that then FAIL the execution,
// and that release rides this signal. So a child whose merge tail broke reported a finish
// FIRST and failed SECOND; markFinished deleted its future; pumpReconcile therefore never
// called f.Get, never saw the error, and the run ended QUIESCENT with the cascade free to
// build on a broken dependency. That is G-P12's stated BreaksAs, reached through the one
// message that was supposed to be the guard's friend.
//
// SO A FINISH NEVER DISCHARGES A FUTURE THIS RUN HOLDS. That is not a special case bolted
// on; it is this file's own doctrine finally obeyed — pumpReconcile's header already says
// NO FACT COMES FROM A SIGNAL ALONE and calls the finish "an OPTIMISATION over this
// function and never the source of truth". For a child of this run the FUTURE is the source
// of truth: it carries the error and nothing else does. The signal still earns its place —
// it WAKES the pump, so the lease is free within a workflow task instead of within a
// reconcile tick, and it is the only fact available for a child that predates a
// ContinueAsNew and has no future here.
//
// WHY THE RULE IS NOT KEYED ON THE OUTCOME. ActivityOutcome is {Unknown, Completed,
// Skipped, TakenOver}, and it CANNOT tell a clean give-up (walkTasks' exited arm, which
// reports Unknown and must let the cascade continue past it) from a broken tail (which now
// also reports Unknown — fix round 1, F5). Keying on the outcome would either let the broken
// tail through or stop the cascade on a clean give-up. Keying on "do I hold the future" keys
// the rule to the thing that actually carries the error.
//
// VALIDATION, for pumpGrantLease's reason: Temporal does not authenticate a signaler. An id
// this pump never started is a CLAIM and is logged and dropped. Without this drop
// pumpContinueAsNewCarry's "Finished … a subset of Started" was simply false — a reviewer
// fabricated fifty ids and the pump accepted and carried all fifty.
func (st *pumpState) applyFinish(ctx workflow.Context, in pumpInput, fin activityFinishedSignal) {
	logger := workflow.GetLogger(ctx)
	if !st.isStarted(fin.ActivityID) {
		logger.Error("pump: dropping a finish report for an activity this pump never started",
			"projectId", string(in.ProjectID), "activityId", string(fin.ActivityID),
			"consequence", "the report is ignored; Temporal does not authenticate a signaler, so an unknown id is a claim and not a fact")
		return
	}
	logger.Info("pump: an activity reported its terminal",
		"projectId", string(in.ProjectID), "activityId", string(fin.ActivityID), "outcome", fin.Outcome.String())
	if _, held := st.futures[fin.ActivityID]; held {
		st.releaseLease(fin.ActivityID)
		logger.Info("pump: the main-write lease is free; the child's own future — not this report — is what says the activity is over",
			"projectId", string(in.ProjectID), "activityId", string(fin.ActivityID))
		return
	}
	st.markFinished(fin.ActivityID)
}

func (wf *csWorkflows) PumpNextActivityWorkflow(ctx workflow.Context, in pumpInput) (PumpResult, error) {
	logger := workflow.GetLogger(ctx)

	// The per-run dispatch decision the façade reads synchronously. THE HANDLER IS
	// REGISTERED FIRST, BEFORE ANYTHING THAT CAN BLOCK (guard G-P1), because
	// awaitDispatchDecision polls this Query against the exact run it started or joined: a
	// handler registered late means the poll gets "unknown query" while the pump is inside
	// its first Activity, and the façade falls through to terminalPumpResult — a slow
	// dispatch reported as a closed pump. Registering a handler and reading a captured
	// local emit NO workflow commands.
	var dispatch pumpDispatch
	if err := workflow.SetQueryHandler(ctx, queryPumpDispatch, func() (pumpDispatch, error) {
		return dispatch, nil
	}); err != nil {
		return PumpResult{}, err
	}

	// THE FOUR CHANNELS. The pause channel is the one that predates this wave; the lease
	// and finish channels are stage 4b2's, and G-P9's rule — "a signal still buffered on a
	// run that ends in ContinueAsNew is NOT carried into the next run" — was written for
	// one channel and is now true for three.
	//
	// PAUSE GATE (Task 3; pause-delivery co-gate 2026-09-12). PauseProject reaches the pump
	// through the supervision workflow's pause branch (runPauseBranch,
	// projectsupervision.go), which relays it onto this pump's id via
	// messageBus.deliverSignal on the SAME operatorPauseRequested signal name. The channel
	// is checked non-blocking (ReceiveAsync — emits no command, replay-deterministic) at
	// the same three points it always was, and it is now ALSO an arm of the selector, so a
	// pause that lands while the pump is parked wakes it immediately instead of waiting out
	// the tick. Separately, the RECORDED-pause gate quiets a pump on a project whose pause
	// is already recorded. A paused pump goes quiet WITHOUT ContinueAsNew. The resume path
	// is ResumeProject; Begin on a paused project is refused at the façade.
	pauseCh := workflow.GetSignalChannel(ctx, signalOperatorPauseRequested)
	leaseCh := workflow.GetSignalChannel(ctx, signalActivityLeaseRequested)
	finishCh := workflow.GetSignalChannel(ctx, signalActivityFinished)

	st := pumpStateFrom(in)
	// RULE 3 OF THE CAN RULE, and it runs BEFORE the frontier is computed: a carried finish
	// must be applied before this run decides what is eligible, or the run re-selects an
	// activity whose completion it is holding in its own input.
	st.replayCarried(ctx, in)

	if reason, paused := pumpPausedAtRunStart(pauseCh); paused {
		logger.Info("pump cascade paused by operator signal — going quiet without continue-as-new",
			"projectId", string(in.ProjectID), "reason", reason)
		dispatch = pumpDispatch{Decided: true, Dispatched: false}
		return PumpResult{Dispatched: false}, nil
	}
	return wf.pumpLoop(ctx, in, st, pumpChannels{pause: pauseCh, lease: leaseCh, finish: finishCh}, &dispatch)
}

// pumpChannels is the three signal channels the loop reads, passed as one so the loop's
// signature stays readable and no caller can hand them over in the wrong order.
type pumpChannels struct{ pause, lease, finish workflow.ReceiveChannel }

// replayCarried applies what the previous run drained at its ContinueAsNew boundary. It is
// rule 3 of the drain-and-carry and the reason rules 1 and 2 are worth anything.
func (st *pumpState) replayCarried(ctx workflow.Context, in pumpInput) {
	logger := workflow.GetLogger(ctx)
	for _, c := range in.Carried {
		switch c.Kind {
		case pumpCarriedFinish:
			logger.Info("pump: replaying a finish carried across continue-as-new",
				"projectId", string(in.ProjectID), "activityId", string(c.ActivityID))
			// Through applyFinish, not markFinished, so the unknown-id drop applies here too
			// (fix round 1, F2). This run holds no futures yet — they do not survive a
			// ContinueAsNew — so a carried finish for a started id marks it finished, which is
			// the right answer: for a pre-CAN child the signal and the ROW are all there is.
			st.applyFinish(ctx, in, activityFinishedSignal{ActivityID: c.ActivityID, Outcome: c.Outcome})
		case pumpCarriedLeaseRequest:
			logger.Info("pump: replaying a lease request carried across continue-as-new",
				"projectId", string(in.ProjectID), "activityId", string(c.ActivityID))
			st.requests = append(st.requests, c.ActivityID)
		}
	}
}

// pumpLoop is the pump proper: read, reconcile, start the frontier, grant, then park. It
// is split out of the entry func so the entry func stays the run's setup and this stays
// the queue (gocognit, and they are two different readings).
func (wf *csWorkflows) pumpLoop(
	ctx workflow.Context, in pumpInput, st *pumpState, chans pumpChannels, dispatch *pumpDispatch,
) (PumpResult, error) {
	logger := workflow.GetLogger(ctx)
	var frontier []ActivityID
	for iteration := 0; ; iteration++ {
		proj, halted, err := wf.pumpWakeUp(ctx, in, chans, iteration, dispatch)
		if err != nil {
			return PumpResult{}, err
		}
		if halted {
			return pumpDispatchedResult(frontier), nil
		}

		if err := wf.pumpReconcile(ctx, in, st, proj); err != nil {
			return PumpResult{}, err
		}

		started, quiet, err := wf.pumpStartFrontier(ctx, in, st, &proj, chans.pause)
		if err != nil {
			return PumpResult{}, err
		}
		frontier = append(frontier, started...)
		if iteration == 0 {
			// G-P8. THE DECISION IS RECORDED BEFORE ANY BLOCK, once, after the WHOLE frontier
			// is started and before the selector. The façade's synchronous ExecuteNextActivity
			// reads this Query; a decision recorded after the park is a Begin that hangs for the
			// length of a cascade and then times out at pumpDispatchWaitBudget.
			*dispatch = pumpDispatchDecision(frontier)
		}
		if quiet {
			return pumpDispatchedResult(frontier), nil
		}

		if err := wf.pumpGrantLease(ctx, in, st); err != nil {
			return PumpResult{}, err
		}

		// QUIESCENCE STILL ENDS THE PUMP (guard G-P6), and it is asked BEFORE the
		// continue-as-new question deliberately: with nothing started, no lease held, no
		// request outstanding and nothing eligible, the run RETURNS. The cascade's own
		// drain-to-quiet is what ends the pump, and a continue here is an infinite pump — one
		// run per second, per project, forever.
		if st.idle() {
			logger.Info("no eligible activity and nothing in flight — cascade quiescent",
				"projectId", string(in.ProjectID))
			return pumpDispatchedResult(frontier), nil
		}

		if res, can, err := wf.pumpContinueAsNew(ctx, in, st, chans, frontier); can || err != nil {
			return res, err
		}
		if paused := wf.pumpPark(ctx, in, st, chans); paused {
			logger.Info("pump cascade paused while parked — going quiet without continue-as-new",
				"projectId", string(in.ProjectID))
			return pumpDispatchedResult(frontier), nil
		}
	}
}

// pumpWakeUp is the head of one loop iteration: pace, honour a pause, re-read head-state,
// honour a RECORDED pause. It reports the fresh project and whether the run must stop here.
// Split out of pumpLoop for the complexity budget, and the split is along a real seam —
// this is everything that happens BEFORE the run may act on the frontier, which is exactly
// the ordering guards G-P2, G-P3, G-P4 and G-P7 are about.
//
// THE RE-READ IS ON EVERY WAKE-UP, and that is deliberate rather than lazy: the CHILD
// writes head-state, so the frontier genuinely moves underneath this run, and a pump that
// derived eligibility from state it carried in memory would be stale by construction. This
// is the read the batched-plan wave shrinks; it is not a read this wave removes.
func (wf *csWorkflows) pumpWakeUp(
	ctx workflow.Context, in pumpInput, chans pumpChannels, iteration int, dispatch *pumpDispatch,
) (projectstate.Project, bool, error) {
	logger := workflow.GetLogger(ctx)
	if iteration > 0 {
		// G-P14. THE PACE. An unpaced wake-up loop burns a workflow task, a durable read and
		// a fresh timer per iteration; the constant appeared in ZERO test files before the
		// census. The FIRST iteration is unpaced — a Begin must reach its decision point
		// without a second of latency in front of it.
		if err := workflow.Sleep(ctx, pumpPaceInterval); err != nil {
			return projectstate.Project{}, false, err
		}
		if reason, paused := pumpPauseRequested(chans.pause); paused {
			logger.Info("pump cascade paused by operator signal — going quiet without continue-as-new",
				"projectId", string(in.ProjectID), "reason", reason)
			return projectstate.Project{}, true, nil
		}
	}
	proj, err := wf.readProject(ctx, in.ProjectID)
	if err != nil {
		if isReadNotFound(err) {
			// No project state yet — a normal quiet tick, not an error. Returned as an error it
			// would fail the Schedule's child start and log a platform-wide sweep error every
			// 30 s. Every OTHER read error still fails the run.
			if iteration == 0 {
				*dispatch = pumpDispatch{Decided: true, Dispatched: false}
			}
			return projectstate.Project{}, true, nil
		}
		return projectstate.Project{}, false, err
	}
	// RECORDED-PAUSE GATE (I2 ruling, 2026-09-12), and it is still BEFORE the frontier: the
	// supervision pause branch RECORDS before it relays, so a pump the sweep restarts inside
	// the relay window sees the recorded pause here and goes quiet — no dispatch, no
	// ContinueAsNew, and no blocked-activity failure record. Placed after the frontier it
	// would still act on it.
	if pumpHonorsRecordedPause(proj) {
		logger.Info("pump honours the recorded operator pause — going quiet without continue-as-new",
			"projectId", string(in.ProjectID), "reason", proj.PauseReason)
		if iteration == 0 {
			*dispatch = pumpDispatch{Decided: true, Dispatched: false}
		}
		return projectstate.Project{}, true, nil
	}
	return proj, false, nil
}

// pumpDispatchDecision renders the frontier as the façade's synchronous answer.
func pumpDispatchDecision(frontier []ActivityID) pumpDispatch {
	d := pumpDispatch{Decided: true, Dispatched: len(frontier) > 0}
	if len(frontier) > 0 {
		first := frontier[0]
		d.ActivityID, d.ActivityIDs = &first, append([]ActivityID(nil), frontier...)
	}
	return d
}

// pumpDispatchedResult is the run's terminal PumpResult.
//
// BLOCKED, AND STATED RATHER THAN WORKED AROUND: PumpResult carries ONE activity id, so a
// run that started five reports the first. The contract delta this owes is
// deliveryManager.$defs.PumpResult.activityIds; it is NOT hand-edited here (this wave has
// exactly one model edit and it is already spent), so the whole frontier is observable on
// queryPumpDispatch.ActivityIDs and in the logs until that delta lands.
func pumpDispatchedResult(frontier []ActivityID) PumpResult {
	if len(frontier) == 0 {
		return PumpResult{Dispatched: false}
	}
	first := frontier[0]
	return PumpResult{Dispatched: true, ActivityID: &first}
}

// pumpStartFrontier starts a child for EVERY eligible activity, not one, and reports what
// it started plus whether the run must go quiet here.
//
// nextEligibleActivity is PURE over the project and returns the FIRST eligible activity in
// declaration order, so one pass is a loop. Starts are idempotent on the child id —
// pumpsweep.go has relied on exactly that since stage 4a — so a redundant tick collapses
// onto the running child instead of forking a second walk.
//
// THE LOOP CARRIES ITS OWN STARTED SET, and this is measured rather than defensive. The
// exclusion the selection rule offers is isActivityDispatchable's PumpWroteRow arm, which
// reads r.StartedAt — a field the CHILD stamps in its own first durable write
// (OpenActivity). Within one pass this run holds a snapshot read before any of that, so
// the rule would return the SAME activity forever. Two mechanisms answer it, and both are
// needed: the local row is marked started so a real selection rule advances, and
// pumpState.Started is consulted so a rule that ignores the project at all (every test
// double does) cannot spin.
func (wf *csWorkflows) pumpStartFrontier(
	ctx workflow.Context, in pumpInput, st *pumpState, proj *projectstate.Project, pauseCh workflow.ReceiveChannel,
) (started []ActivityID, quiet bool, err error) {
	logger := workflow.GetLogger(ctx)
	rule := pumpEligibilityRule()
	for {
		sel := wf.nextEligible(*proj, rule)
		switch sel.Verdict {
		case verdictQuiescent:
			return started, st.idle(), nil
		case verdictBlocked:
			if st.blocked[sel.BlockedActivityID] {
				// Already recorded by this run; the snapshot cannot show the write, so stop here
				// rather than record it again.
				return started, false, nil
			}
			if err := wf.pumpRecordBlocked(ctx, in, *proj, sel); err != nil {
				return started, false, err
			}
			st.blocked[sel.BlockedActivityID] = true
			// AND THE LOOP CONTINUES, which the serial pump could not do: it returned, so ONE
			// plan defect took the whole frontier down with it. A blocked activity records its
			// failure and the rest of the network is still considered on this same pass.
			markRowBlocked(proj, sel.BlockedActivityID, sel.BlockedFailureReason)
			continue
		case verdictDispatch:
		}
		id := ActivityID(sel.Activity.ActivityID)
		if st.isStarted(id) {
			// The selection rule handed back something already in flight — the frontier this
			// pass can see is exhausted.
			return started, false, nil
		}
		// PRE-DISPATCH RE-CHECK (signal check 2). A pause that landed while readProject ran
		// must not dispatch a NEW activity: nothing would cancel it — the pause plan's
		// PipelinesToCancel is empty because InFlightPipelines is never populated. THE BOUND:
		// this honours a pause DELIVERED BEFORE the dispatching workflow task starts. A pause
		// arriving DURING that task is honoured at the drain gate or at the selector.
		if reason, paused := pumpPausedBehindGate(pauseCh); paused {
			logger.Info("pump cascade paused by operator signal before dispatch — going quiet without continue-as-new",
				"projectId", string(in.ProjectID), "activityId", sel.Activity.ActivityID, "reason", reason)
			return started, true, nil
		}
		st.futures[id] = wf.startActivityChild(ctx, in.ProjectID, sel.Activity)
		st.markStarted(id)
		started = append(started, id)
		markRowStarted(ctx, proj, sel.Activity.ActivityID)
	}
}

// markRowStarted stamps the LOCAL snapshot's row so the next call of the selection rule in
// this same pass does not hand back the activity just dispatched. StartedAt is exactly what
// PumpWroteRow reads and exactly what the child is about to write, so the synthesis is the
// truth arriving a few hundred milliseconds early rather than a fiction.
func markRowStarted(ctx workflow.Context, proj *projectstate.Project, activityID string) {
	if proj.ActivityExecution == nil {
		proj.ActivityExecution = map[string]projectstate.ActivityExecution{}
	}
	row := proj.ActivityExecution[activityID]
	row.ActivityID = activityID
	now := workflow.Now(ctx)
	row.StartedAt = &now
	proj.ActivityExecution[activityID] = row
}

// markRowBlocked stamps the LOCAL snapshot's row with the terminal the pump just recorded,
// so the same pass's next selection does not re-pick the activity it has already failed.
func markRowBlocked(proj *projectstate.Project, activityID string, reason projectstate.FailureReason) {
	if activityID == "" {
		return
	}
	if proj.ActivityExecution == nil {
		proj.ActivityExecution = map[string]projectstate.ActivityExecution{}
	}
	row := proj.ActivityExecution[activityID]
	row.ActivityID = activityID
	row.FailureReason = reason
	proj.ActivityExecution[activityID] = row
}

// pumpRecordBlocked is the verdictBlocked write, unchanged in substance.
//
// LOUD, DURABLE, APP-VISIBLE (spec §4.3). The log line alone is the failure mode being
// eliminated — a warning buried in a serve log is how this defect consumed an entire
// benchmark run undetected — so the escalation is the HEAD-STATE record:
// ActivityConstructionFailed is sticky via CoarsePhaseFor, so the operator sees a red node
// carrying its FailureReason and the reason. NOT a returned workflow error: a failed
// Temporal execution is invisible in the console. Recording the terminal also takes the
// activity out of NotStarted, so the next tick considers the rest of the network.
//
// THE RECORD'S OWN ERROR ARM FAILS THE RUN (guard G-P15). G-P5's loudness is not
// best-effort: if the durable record cannot land, the run must not report a clean quiet
// tick, because a blocked frontier with a swallowed write is exactly the silent quiescent
// pump G-P5 exists to end.
//
// verdictBlocked also covers two SIBLING plan defects surfaced by nextEligibleActivity: an
// authored dependency id that names neither a known activity nor a known milestone
// (DependencyUnresolved), or a milestone dependency cycle (DependencyCycle). Each defect
// class is recorded through its OWN FailureReason variant per the ruling that one
// FailureReason covers one repair class; FailureDetail discriminates instances WITHIN a
// class, never between classes.
func (wf *csWorkflows) pumpRecordBlocked(
	ctx workflow.Context, in pumpInput, proj projectstate.Project, sel pumpSelection,
) error {
	workflow.GetLogger(ctx).Error("construction pump: activity cannot be dispatched",
		"projectId", string(in.ProjectID),
		"activityId", sel.BlockedActivityID,
		"reason", sel.BlockedReason)
	_, ferr := wf.applyRecovering(ctx, in.ProjectID, proj.Version, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordActivityFailed(
			ctx, projectstate.ProjectID(in.ProjectID), expected,
			sel.BlockedActivityID, sel.BlockedFailureReason, sel.BlockedReason,
			railCredEnvelope{}.toProjectState())
	})
	return ferr
}

// ---------------------------------------------------------------------------
// THE LEASE
// ---------------------------------------------------------------------------

// pumpGrantLease answers at most one outstanding request.
//
// THE INVARIANT, AT ITS TRUE SCOPE: at most one activity holds the main-write lease at a
// time WITHIN ONE PUMP CHAIN. LeaseHolder lives in pumpState/pumpInput and survives nothing
// else, so a pump run that ends holding a lease — a pause while parked, G-P12 firing on
// another child, any error return — hands the project to a fresh chain that starts with
// LeaseHolder nil and an empty Started, while the old holder is still in its merge tail
// behind a human gate. The module header spells that boundary out; the row-level mechanism
// is what holds across it.
//
// WHY A LEASE AND NOT "LET THEM RACE". applyRecovering's bound is
// maxMutateConflictAttempts, and stage 4b1 MEASURED that bound being exhausted by ONE
// coroutine's own writes with no sibling present — the 65-override storm,
// MutateConflictExhausted, an activity killed by an operator pressing a button. Under N
// parallel children each landing 1-5 slot commits plus a ref-CAS against main, exhaustion
// is not a hypothesis.
//
// WHAT THE LEASE DOES NOT REPLACE: the per-activity CAS and applyMutationOnBranchFiles'
// dedup + version guard + ref-CAS stay exactly as they are, and they are what serialise
// per ROW. Row-level and main-level are TWO mechanisms.
//
// VALIDATION, because Temporal does not authenticate a signaler: an id is granted only if
// it is in this pump's Started set AND in the committed plan this run already read. An id
// in neither is LOGGED AND DROPPED, never granted.
//
// WHICH MAKES "THE LEASE FAILS OPEN" THE NORMAL STATE AFTER ANY PUMP RESTART, not a rare
// fault path — the one consequence of that validation worth stating at the site it happens.
// A child that predates the restart is an id THIS chain never started, so its request is
// dropped here, its bounded wait in requestMainWriteLease expires, and its merge tail runs
// UNLEASED with the per-row CAS and the branch-file version guard as its only serialisation.
// That is the pre-wave state, deliberately, and it is strictly better than failing an
// activity that did all of its work because the admission queue restarted underneath it.
func (wf *csWorkflows) pumpGrantLease(ctx workflow.Context, in pumpInput, st *pumpState) error {
	if err := wf.pumpCheckLease(ctx, in, st); err != nil {
		return err
	}
	for st.LeaseHolder == nil && len(st.requests) > 0 {
		id := st.requests[0]
		st.requests = st.requests[1:]
		if !st.isStarted(id) {
			workflow.GetLogger(ctx).Error("pump: dropping a main-write lease request for an activity this pump never started",
				"projectId", string(in.ProjectID), "activityId", string(id),
				"consequence", "the request is not granted; Temporal does not authenticate a signaler, so an unknown id is a claim and not a fact")
			continue
		}
		if err := wf.pumpDeliverGrant(ctx, in, st, id); err != nil {
			return err
		}
	}
	return nil
}

// pumpDeliverGrant bumps the epoch and hands the lease over. A grant whose delivery
// answers NotFound is a grant to an execution that is GONE: the lease is NOT handed to a
// corpse (this returns before LeaseHolder is set) and the requester's other queued asks go
// with it — but the activity is NOT declared over here. Whether it succeeded or broke is a
// question only its future or its row can answer; see markVanished.
func (wf *csWorkflows) pumpDeliverGrant(ctx workflow.Context, in pumpInput, st *pumpState, id ActivityID) error {
	epoch := st.LeaseEpoch + 1
	err := wf.pumpSignalChild(ctx, in.ProjectID, id, activityLeaseGrant{ActivityID: id, Epoch: epoch})
	switch {
	case isSignalTargetNotFound(err):
		workflow.GetLogger(ctx).Info("pump: the activity that asked for the lease is already gone; its ask is dropped and the reconcile will say whether it finished or broke",
			"projectId", string(in.ProjectID), "activityId", string(id))
		st.markVanished(id)
		return nil
	case err != nil:
		return err
	}
	now := workflow.Now(ctx)
	st.LeaseEpoch, st.LeaseHolder, st.LeaseGrantedAt = epoch, &id, &now
	workflow.GetLogger(ctx).Info("pump: main-write lease granted",
		"projectId", string(in.ProjectID), "activityId", string(id), "epoch", epoch)
	return nil
}

// pumpCheckLease is the deadline, and it DOES NOT BLINDLY REVOKE.
//
// On expiry the pump asks whether the holder is still alive, by re-delivering its OWN
// grant at its OWN epoch: a signal to a live execution is delivered, and a signal to one
// that has closed answers RA NotFound (messagebus.classifyCommon). The re-delivery is a
// RENEWAL — same epoch, same holder — so a child that happens to read it sees the grant it
// already has.
//
// A LIVE HOLDER THAT IS MERELY SLOW MUST NEVER LOSE ITS LEASE MID-MERGE. The merge tail
// holds a HUMAN approval gate, so "held longer than ten minutes" is a completely ordinary
// state and a naive timeout would revoke exactly the thing the lease exists to protect —
// producing the second concurrent main-writer as the DIRECT consequence of the mechanism
// meant to prevent it. The liveness check is what buys the difference.
func (wf *csWorkflows) pumpCheckLease(ctx workflow.Context, in pumpInput, st *pumpState) error {
	if st.LeaseHolder == nil || st.LeaseGrantedAt == nil {
		return nil
	}
	if workflow.Now(ctx).Sub(*st.LeaseGrantedAt) < pumpLeaseDeadline {
		return nil
	}
	holder := *st.LeaseHolder
	err := wf.pumpSignalChild(ctx, in.ProjectID, holder, activityLeaseGrant{ActivityID: holder, Epoch: st.LeaseEpoch})
	switch {
	case isSignalTargetNotFound(err):
		workflow.GetLogger(ctx).Error("pump: the main-write lease holder's execution has closed; revoking and re-granting at the next epoch — and the reconcile, not this probe, says whether it finished or broke",
			"projectId", string(in.ProjectID), "activityId", string(holder), "epoch", st.LeaseEpoch)
		st.markVanished(holder)
		return nil
	case err != nil:
		return err
	}
	now := workflow.Now(ctx)
	st.LeaseGrantedAt = &now
	workflow.GetLogger(ctx).Info("pump: the lease holder is alive and its lease is renewed, not revoked",
		"projectId", string(in.ProjectID), "activityId", string(holder), "epoch", st.LeaseEpoch)
	return nil
}

// pumpSignalChild delivers one message to a per-activity child through the GENERATED
// messageBus.deliverSignal invoker — the same invoker relayPauseToPump uses, which is why
// the lease needs NO new ResourceAccess producer and no contract change.
func (wf *csWorkflows) pumpSignalChild(ctx workflow.Context, projectID ProjectID, id ActivityID, grant activityLeaseGrant) error {
	b, err := json.Marshal(grant)
	if err != nil {
		return err
	}
	return wf.Acts.MessageBusDeliverSignal(ctx,
		messagebus.ExecutionID(deliveryActivityWorkflowID(projectID, id)),
		messagebus.SignalName(signalActivityLeaseGranted),
		messagebus.ExecutionPayload{Bytes: b})
}

// ---------------------------------------------------------------------------
// THE TICK
// ---------------------------------------------------------------------------

// pumpReconcile is the tick, and IT IS A RECONCILE, NOT A POLL: NO FACT COMES FROM A
// SIGNAL ALONE.
//
// The finish signal is an OPTIMISATION OVER THIS FUNCTION and never the source of truth.
// On every wake-up, for each started-and-unfinished activity, the pump asks the two
// sources that are independent of any signal:
//
//   - the FUTURE of a child this run started, which is Temporal's own execution state and
//     carries the error;
//   - the activity's ROW, which the child writes itself (RecordActivityOutcome), for a
//     child that predates a ContinueAsNew and therefore has no future here.
//
// That answers three failure modes at once: a lost signal costs at most one tick; a child
// that dies between finishing and signalling is found terminal in its row; a child that
// dies holding the lease is found gone by the lease's own liveness check and the lease is
// re-granted at epoch+1 — while THE VERDICT ON THAT CHILD IS STILL TAKEN HERE, off its
// future or its row, because "the execution is gone" says nothing about whether it worked
// (fix round 2, C1; see markVanished).
//
// G-P12 AND WHAT IT COSTS TO KEEP IT. A failed child FAILS THE PUMP RUN — that is how a
// cascade STOPS on a broken activity instead of marching down the frontier — and nothing
// in this repo had ever failed a child before the census. For a child of THIS run the
// future carries the error and the guard is preserved exactly. For a child that predates a
// ContinueAsNew there is no future, so this tick must APPLY the same semantics rather than
// merely observe them, and it does: an activity this pump started whose row carries a
// terminal FAILURE and that reported NO finish signal is a child that died in failWalk,
// and the run fails. THE DEGRADATION IS REAL AND IS WRITTEN DOWN HERE: for such a child
// the cascade stops within one wake-up rather than instantly, and only if the row was
// written — a child killed before it could record anything is invisible to this rule, and
// the sweep's restart is its only backstop.
//
// WHY A TERMINAL FAILURE ROW ALONE IS NOT ENOUGH, and this is the distinction the rule
// turns on: an activity that GIVES UP cleanly (constructionGaveUp / constructionExited)
// also records a failure row, and stage 4b1 decided deliberately that such a child returns
// nil so the cascade continues past it. failWalk is the one that returns the cause — and
// it is the one that sends NO finish signal. So "terminal failure row, no finish reported"
// is precisely "the child's own run broke", and nothing else is.
func (wf *csWorkflows) pumpReconcile(ctx workflow.Context, in pumpInput, st *pumpState, proj projectstate.Project) error {
	logger := workflow.GetLogger(ctx)
	for _, id := range st.inFlight() {
		if f, ok := st.futures[id]; ok {
			if !f.IsReady() {
				continue
			}
			if err := f.Get(ctx, nil); err != nil {
				logger.Error("pump: the activity child FAILED — stopping the cascade rather than walking past it",
					"projectId", string(in.ProjectID), "activityId", string(id), "err", err.Error())
				return err
			}
			logger.Info("pump: the activity child reached its terminal",
				"projectId", string(in.ProjectID), "activityId", string(id))
			st.markFinished(id)
			continue
		}
		// The ZERO ROW IS READ, not skipped: "started, no future, no row at all" is exactly
		// the shape a child that vanished without recording anything has, and the arm below is
		// the only thing that answers it. A missing row's zero value is Unknown/nil, so the two
		// terminal arms behave as they did when this was guarded by an existence check.
		row := proj.ActivityExecution[string(id)]
		switch {
		case row.FailureReason != projectstate.FailureReasonUnknown:
			// No future, a terminal FAILURE row, and no finish signal ever arrived (a finish
			// removes the id from Started). That is failWalk, one continue-as-new ago.
			logger.Error("pump: an activity started before this run's continue-as-new is terminally failed and never reported a finish — stopping the cascade",
				"projectId", string(in.ProjectID), "activityId", string(id), "failureReason", row.FailureReason.String())
			return temporal.NewNonRetryableApplicationError(
				"the pump's cascade stops: activity "+string(id)+" failed before this run and never reported a terminal",
				"ActivityChildFailed", nil)
		case row.CompletedAt != nil:
			logger.Info("pump: an activity started before this run's continue-as-new is complete in head-state",
				"projectId", string(in.ProjectID), "activityId", string(id))
			st.markFinished(id)
		case st.vanished[id]:
			// THE VANISHED ARM (fix round 2, C1). The pump found this execution GONE — a lease
			// delivery answered NotFound — it holds no future for it, and the head-state read at
			// the top of THIS iteration, taken after the execution closed, shows no terminal
			// either way. A closed execution has finished writing, so this is not a race: it is
			// an activity that died without recording an outcome, which is a broken child by
			// every rule this file has. It stops the cascade, and it is also what keeps the pump
			// from waiting on a future that will never resolve.
			logger.Error("pump: an activity's execution is GONE and it recorded no terminal — stopping the cascade",
				"projectId", string(in.ProjectID), "activityId", string(id))
			return temporal.NewNonRetryableApplicationError(
				"the pump's cascade stops: activity "+string(id)+"'s execution is gone and it recorded no terminal",
				"ActivityChildVanished", nil)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// THE PARK, AND THE CONTINUE-AS-NEW BOUNDARY
// ---------------------------------------------------------------------------

// pumpPark waits for the next thing that can change the answer: a child terminal, a lease
// request, a finish report, a pause, or the reconcile tick. It reports whether the wake-up
// was a PAUSE.
//
// THE FUTURES ARE ARMS OF THIS SELECTOR, which is what keeps all four of child.Get's
// affordances for children of this run while giving up the one thing that had to go: the
// blocking wait. ORDERING IS REPLACED, NOT PRESERVED — see the note at
// Test_Pump_TheNextSelectionWaitsForTheChildsTerminal.
func (wf *csWorkflows) pumpPark(ctx workflow.Context, in pumpInput, st *pumpState, chans pumpChannels) bool {
	tctx, cancelTimer := workflow.WithCancel(ctx)
	defer cancelTimer()
	paused := false
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(chans.pause, func(c workflow.ReceiveChannel, _ bool) {
		var raw any
		c.Receive(ctx, &raw)
		paused = true
	})
	sel.AddReceive(chans.lease, func(c workflow.ReceiveChannel, _ bool) {
		var req activityLeaseRequest
		if !pumpReceiveSignalBlocking(ctx, c, &req) {
			return
		}
		st.requests = append(st.requests, req.ActivityID)
	})
	sel.AddReceive(chans.finish, func(c workflow.ReceiveChannel, _ bool) {
		var fin activityFinishedSignal
		if !pumpReceiveSignalBlocking(ctx, c, &fin) {
			return
		}
		st.applyFinish(ctx, in, fin)
	})
	// THE ARM ORDER IS DETERMINISTIC, and it was not (fix round 1, F3). This loop used to
	// range over st.futures, so which arm fired when two futures were ready in one workflow
	// task depended on Go's randomised map iteration — and therefore varied between a run and
	// its own REPLAY. It was benign only because these callbacks emit nothing but a log; the
	// first line of state a callback ever mutates would turn it into a non-determinism panic
	// on the project's ONE pump. st.inFlight() is Started minus Finished in DISPATCH order,
	// which is the same order on every replay.
	for _, id := range st.inFlight() {
		f, ok := st.futures[id]
		if !ok {
			continue
		}
		sel.AddFuture(f, func(workflow.Future) {
			workflow.GetLogger(ctx).Info("pump: a child future is ready", "activityId", string(id))
		})
	}
	sel.AddFuture(workflow.NewTimer(tctx, pumpReconcileInterval), func(workflow.Future) {})
	sel.Select(ctx)
	return paused
}

// pumpContinueAsNew is the wave's riskiest ten lines, and they are the only part of it
// that fails SILENTLY.
//
// CONTINUE-AS-NEW ONLY AT ZERO HELD LEASE AND ZERO UNANSWERED REQUEST, AND DRAIN FIRST.
//
// This file has documented the trap since the pause co-gate: "a signal still buffered on a
// run that ends in ContinueAsNew is NOT carried into the next run". That was true for ONE
// channel and it is now true for three. A finish or a lease request lost across the
// boundary is not an error ANYWHERE — the pump simply waits out a lease deadline for an
// activity that already finished, or never re-selects an activity whose finish was the
// thing lost — and pump-singular-per-project means there is no second pump to cover it.
// The blast radius is that the project stops.
//
// So, three rules, and they are one mechanism:
//
//  1. CAN only when LeaseHolder == nil and no lease request is unanswered — the child's own
//     shouldContinueAsNew-at-inflight==0 discipline, one level up. A lease handed over a
//     boundary would be a lease whose holder the next run cannot name;
//  2. immediately before CAN, ReceiveAsync every channel until empty and put what is found
//     in pumpState.Carried;
//  3. at run start, replay Carried BEFORE the frontier is computed (replayCarried).
//
// The reconcile tick is the backstop for all three, which is why losing one costs a tick
// rather than a project.
func (wf *csWorkflows) pumpContinueAsNew(
	ctx workflow.Context, in pumpInput, st *pumpState, chans pumpChannels, frontier []ActivityID,
) (PumpResult, bool, error) {
	if !pumpShouldContinueAsNew(ctx) || st.LeaseHolder != nil || len(st.requests) > 0 {
		return PumpResult{}, false, nil
	}
	// DRAIN BEFORE THE HAND-OFF, pause first (the gate this file has always had). A pause
	// that arrived while this run was parked sits in the buffer ContinueAsNew discards, so
	// honour it here: no further child starts.
	if reason, paused := pumpPausedBehindGate(chans.pause); paused {
		workflow.GetLogger(ctx).Info("pump cascade paused by operator signal before continue-as-new — going quiet",
			"projectId", string(in.ProjectID), "reason", reason)
		return pumpDispatchedResult(frontier), true, nil
	}
	st.drainForContinue(ctx, in, chans)
	return PumpResult{}, true, workflow.NewContinueAsNewError(ctx, executionKindPump, st.toInput(in))
}

// drainForContinue is rule 2. It is non-blocking (ReceiveAsync emits no command and is
// replay-deterministic) and it runs until every channel is empty, because "drain one" is
// the same defect with a smaller window.
//
// IT VALIDATES WHAT IT CARRIES (fix round 1, F2). The drain is the OTHER door into
// pumpState.Carried — the selector arms are the first — and it is the door the declared
// bound is paid at, because Carried is what crosses the ContinueAsNew boundary. An id this
// pump never started is dropped here for applyFinish's reason: a signaler Temporal did not
// authenticate is making a claim, and a claim must not be able to grow a payload whose stated
// bound is "the plan's activity count".
func (st *pumpState) drainForContinue(ctx workflow.Context, in pumpInput, chans pumpChannels) {
	logger := workflow.GetLogger(ctx)
	drop := func(kind string, id ActivityID) bool {
		if st.isStarted(id) {
			return false
		}
		logger.Error("pump: dropping a buffered message for an activity this pump never started",
			"projectId", string(in.ProjectID), "activityId", string(id), "kind", kind,
			"consequence", "it is not carried across the continue-as-new; an unknown id is a claim and not a fact")
		return true
	}
	for {
		var fin activityFinishedSignal
		if decoded, present := pumpReceiveSignal(ctx, chans.finish, &fin); present {
			if decoded && !drop(pumpCarriedFinish, fin.ActivityID) {
				st.Carried = append(st.Carried, pumpCarriedSignal{
					Kind: pumpCarriedFinish, ActivityID: fin.ActivityID, Outcome: fin.Outcome})
			}
			continue
		}
		var req activityLeaseRequest
		if decoded, present := pumpReceiveSignal(ctx, chans.lease, &req); present {
			if decoded && !drop(pumpCarriedLeaseRequest, req.ActivityID) {
				st.Carried = append(st.Carried, pumpCarriedSignal{
					Kind: pumpCarriedLeaseRequest, ActivityID: req.ActivityID})
			}
			continue
		}
		return
	}
}

// pumpShouldContinueAsNew is the pump's history ceiling. It is a plain function over the
// SDK's own two accessors with NO injectable budget, exactly as the child's is: the test
// environment exposes SetCurrentHistoryLength, so a case that drives a continue drives the
// PRODUCTION const rather than a smaller number substituted for it.
func pumpShouldContinueAsNew(ctx workflow.Context) bool {
	info := workflow.GetInfo(ctx)
	return info.GetContinueAsNewSuggested() || info.GetCurrentHistoryLength() > pumpHistoryBudget
}

// ---------------------------------------------------------------------------
// THE VERSION FENCES ARE GONE (stage 4b3 Task 3). There were FIVE GetVersion call
// sites over SIX change ids — the header here used to say "five over four", which was
// wrong — and every one existed to keep a pre-change execution's recorded command
// sequence replayable. There are no such executions: the founder has ruled there are no
// production users, and the one drain this release rides kills every {p}:nextActivity
// execution that could hold a recorded marker.
//
// WHAT DID NOT LICENSE THIS, and the distinction matters because the obvious argument is
// the wrong one: a replay fixture does NOT pin a GetVersion rung. Deleting the
// changeDesignActivitiesDispatchable rung left all six pump fixtures GREEN, because the
// SDK tolerates a recorded Version marker the replayed code never asks for; deleting the
// pace Sleep, by contrast, fails 2 of 6 at a named event position. Fixtures pin COMMANDS.
// The four census rows that hung off these arms, and the five …_DefaultVersion_… tests
// that were those rows' ONLY pin, are deleted in this same commit for the same reason —
// an arm that cannot exist needs no guard, and a guard for it is a test nobody can ever
// make fail honestly.
// ---------------------------------------------------------------------------

// pumpPausedBehindGate is a signal pause check (2: pre-dispatch, 3: pre-ContinueAsNew). It
// was named for the GetVersion fence it sat behind — one func, two change ids — and the
// fence is discharged, so the check is all there is: both sites now run on every execution.
func pumpPausedBehindGate(ch workflow.ReceiveChannel) (reason string, paused bool) {
	return pumpPauseRequested(ch)
}

// pumpHonorsRecordedPause reports whether this run must go quiet on the project's RECORDED
// pause. EVERY pump honours it — ResumeProject is the one way back. The three-armed
// GetVersion ladder that used to stand here (no gate / operator-driven exempt / binds every
// pump) is discharged with the other fences; only its last arm was ever reachable on a new
// execution, and it is now the whole rule.
func pumpHonorsRecordedPause(proj projectstate.Project) bool {
	return proj.OperatorPaused
}

// pumpPausedAtRunStart is pause check 1. Its pre-change body received into an
// operatorPauseSignal struct, which silently DROPS a relayed (binary/plain) pause; that arm
// lived behind the pump-pause-decode-any fence and is discharged with it. It was also the
// consumer wire-form rule's one live production exception, so the rule now holds over this
// package with no exception at all.
func pumpPausedAtRunStart(ch workflow.ReceiveChannel) (reason string, paused bool) {
	return pumpPauseRequested(ch)
}

// pumpPauseRequested drains one pending operatorPauseRequested signal, non-blocking,
// and reports whether a pause was pending (plus its best-effort reason, for the log).
//
// It decodes into `any`, deliberately: ReceiveAsync SILENTLY DISCARDS a signal whose
// payload cannot decode into the target (the SDK logs "Corrupted signal" and moves
// on), and the pump's pause arrives as messageBus.deliverSignal's raw bytes
// (binary/plain — see relayPauseToPump) while a directly-sent operatorPauseSignal
// arrives as JSON. A struct target would drop the former; `any` accepts both.
//
// AN UNDECODABLE PAUSE STILL COUNTS AS A PAUSE (decided, pinned by
// Test_Pump_UndecodablePauseSignal_StillPauses). The channel NAME carries the
// operator's intent; the body only carries a reason for the log. Dropping a pause over
// a malformed body fails OPEN — the cascade keeps dispatching through an operator halt,
// the exact defect this gate exists to prevent — while counting it fails SAFE: the pump
// goes quiet, nothing is lost, and the next Begin resumes. Only the reason is lost.
func pumpPauseRequested(ch workflow.ReceiveChannel) (reason string, paused bool) {
	var raw any
	if !ch.ReceiveAsync(&raw) {
		return "", false
	}
	reason, _ = pauseSignalReason(raw)
	return reason, true
}

// pauseSignalReason normalises ONE operatorPauseRequested payload, whichever wire form it
// arrived in. It is the one decode of that signal name, shared by both of its consumers —
// the pump's pumpPauseRequested above and ProjectSupervisionWorkflow — so the two cannot
// drift apart the way the three lease channels drifted from this file's own pause rule
// (stage 4b2 Task 14) and the way supervision had already drifted by stage 4b3.
//
// THE TWO WIRE FORMS. messageBus.deliverSignal hands the Temporal client raw []byte, which
// the default converter tags binary/plain, and the SDK's ByteSlicePayloadConverter can
// assign such a payload to nothing but a *[]byte — so a concrete-struct receive target
// makes the SDK log "Corrupted signal received on channel operatorPauseRequested" and DROP
// the message, which is not an error a workflow can see. A struct signalled directly
// (PauseProject's SignalWithStartWorkflow, and every test written before 4b3) is json/plain
// and arrives as map[string]any. Receiving into `any` serves BOTH; this reads what came
// back. Deterministic — a receive plus a decode emits no workflow command at all.
//
// `decoded` reports the FACT, never the disposition: both callers deliberately fail SAFE on
// false and say so at their own site, which is the opposite of the lease channels' drop
// rule (pumpDecodeSignal) and must stay visibly opposite.
func pauseSignalReason(raw any) (reason string, decoded bool) {
	switch v := raw.(type) {
	case []byte:
		var sig operatorPauseSignal
		if err := json.Unmarshal(v, &sig); err != nil {
			return "", false
		}
		return sig.Reason, true
	case map[string]any:
		// The reason is read off the map rather than re-marshalled: operatorPauseSignal
		// carries no json tags, so the key is the field name.
		r, _ := v["Reason"].(string)
		return r, true
	default:
		return "", false
	}
}

// pumpEligibilityRule is the selection rule this pump run uses, and there is now only one.
//
// It used to be a TWO-FENCE, THREE-ARM LADDER — eligibleNotStarted for an execution that
// recorded no ledger-partial-resume marker, then eligibleDispatchable for one that recorded
// no design-activities-dispatchable marker, then eligibleWithDesign — because the rungs
// genuinely change WHICH activity a tick picks on state that already exists: this repo's
// committed slot 9 opens with requirements/architecture/projectDesign and none of the three
// has an execution row, so a history that walked past them and dispatched a construction
// activity would, replayed under eligibleWithDesign, select `requirements` and start a
// DIFFERENT child id. Both rungs are discharged with the other fences (see the section
// header): with no pre-change execution left to replay there is no history that recorded a
// lower rung, and a rung nothing can select is a branch that cannot be tested honestly.
//
// eligibleNotStarted and eligibleDispatchable SURVIVE as rules — eligibleUnder is written
// as a ladder (`rule >= eligibleDispatchable`) and nextEligibleActivity's own tests walk all
// three — but the pump no longer has a way to ask for either.
func pumpEligibilityRule() eligibilityRule {
	return eligibleWithDesign
}

// startActivityChild starts the child that runs ONE activity and returns its future.
//
// ONE CHILD, ONE ID — idempotent on deliveryActivityWorkflowID, so a redundant tick
// collapses onto the running child instead of starting a second one, and the façade signals
// the same id the pump started.
//
// PARENT_CLOSE_POLICY_ABANDON: the activity is its own durable execution, independent of
// this pump's continue-as-new chain. Drop it and the pump's own close — every
// ContinueAsNew, every failure — terminates every in-flight activity, silently and
// catastrophically. Pinned STRUCTURALLY (pumpChildOptionsField), not by substring: this
// comment names the constant, so a text search over this file would stay green with the
// assignment itself deleted.
//
// (Stage 4b1 Task 11 fenced this start with changeGenericActivityChild; Task 13 RETIRED
// that fence with the workflow it protected, because a GetVersion marker whose other arm
// names a workflow the build no longer has is worse than no marker — it compiles, records a
// version, and then panics differently.)
func (wf *csWorkflows) startActivityChild(
	ctx workflow.Context, projectID ProjectID, activity constructionActivity,
) workflow.ChildWorkflowFuture {
	id := ActivityID(activity.ActivityID)
	cctx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID:        deliveryActivityWorkflowID(projectID, id),
		ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_ABANDON,
	})
	workflow.GetLogger(ctx).Info("delivery pump: starting the generic DAG child",
		"projectId", string(projectID), "activityId", activity.ActivityID, "activityType", activity.Type.String())
	return workflow.ExecuteChildWorkflow(cctx, executionKindDeliveryActivity, deliveryActivityInput{
		ProjectID: projectID, ActivityID: id, Activity: activity,
	})
}

// nextEligible resolves the next selection via the injected helper. With no helper
// wired it is a quiet tick — an unwired pump dispatches NOTHING rather than panicking or
// dispatching arbitrarily, fail-safe by construction.
func (wf *csWorkflows) nextEligible(proj projectstate.Project, rule eligibilityRule) pumpSelection {
	if wf.NextEligibleActivity == nil {
		return pumpSelection{Verdict: verdictQuiescent}
	}
	return wf.NextEligibleActivity(proj, rule)
}

// Shared workflow-context helper (used by 3 csWorkflows); lives in its first caller's file per the file-layout standard.
// readProject reads the whole-aggregate head-state through the GENERATED
// designSessionAccess.readProjectOnBranch invoker with branch "" — the RA-side
// empty-branch fallback always reads main (pinned by
// TestDesignSessionAccess_ReadProjectOnBranch_EmptyBranchAlwaysBase,
// projectstate/designsession_test.go). This replaced the last CUSTOM Activity
// (ReadProjectActivity) once the shared projectstate.ProjectEnvelope grew the three
// construction-fidelity sections the pump reads (ActivityConstruction /
// ServiceContracts / ReviewPolicy — B8 follow-up, envelope.go); Decode restores the
// committed Network/ActivityList slots concretely typed, so nextEligibleActivity's
// committed-slot guards and type assertions are served identically to the former
// local codec. Decode is pure JSON reconstruction over the history-recorded activity
// result — deterministic, replay-safe in-workflow.
func (wf *csWorkflows) readProject(ctx workflow.Context, projectID ProjectID) (projectstate.Project, error) {
	env, err := wf.Acts.DesignSessionReadProjectOnBranch(ctx, projectstate.ProjectID(projectID), "")
	if err != nil {
		return projectstate.Project{}, err
	}
	return env.Decode()
}
