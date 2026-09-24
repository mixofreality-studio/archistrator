// cmd/migrate-activity-execution rewrites this repository's own committed state into
// stage 3's execution shape: `.activityConstruction` becomes `.activityExecution`, the
// five derived members stop being stored, every row gains the per-activity `Version` and
// the LifecyclePin it ran under, and the review history that until now lived only as a
// slot thread or a one-line operator note becomes REVIEW ROUNDS on the row's own ledger
// (spec 2026-09-20 §5.3 F).
//
// It is a ONE-SHOT tool whose output is a REVIEWABLE COMMITTED DIFF, modelled on its
// sibling `cmd/backfill-attempts` down to the splice discipline: it reads the document
// raw, decodes it through the PRODUCTION codec, edits the decoded aggregate in memory,
// re-encodes, and splices back only the two members it owns — so every other member of
// project.json is byte-identical, in the same order, in the file it writes.
//
// THE MIGRATION IS EVIDENCE OF NOTHING. Every attempt it carries keeps its own
// Provenance.Origin verbatim (a backfilled attempt stays backfilled), and every round it
// synthesizes is stamped OriginBackfilled with a basis naming the slot thread or the
// operator note it was made from. Nothing it writes is ever `observed`: nobody watched
// any of it run, and a migration that laundered provenance would make the ledger's own
// vocabulary useless for telling a reader what actually happened.
//
// WHAT IT REFUSES. The legacy member must be carried WHOLE by the typed legacy reader
// (projectstate.LegacyActivityConstructionRow) — read off the RAW bytes before any
// decode/encode cycle — or the run stops and names the member that would be lost. A
// document that re-indenting would not reproduce is refused too: rewriting one would
// smuggle a reformat of unrelated state into the diff.
//
// RE-RUNNING IT IS A NO-OP. A row already at a version, already pinned, already holding
// the round an id names is left exactly as it stands, so the second run writes nothing.
//
// Usage (from server/):
//
//	GOWORK=off go run ./cmd/migrate-activity-execution -root .. [-dry-run]
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// statePath is the project.json location relative to the repository root.
var statePath = filepath.Join(".aiarch", "state", "project.json")

// generatorID identifies this tool in every provenance stamp it writes.
const generatorID = "cmd/migrate-activity-execution"

// The two top-level members this tool owns: the one it retires and the one it writes.
const (
	legacyMember    = "activityConstruction"
	executionMember = "activityExecution"
)

// critiqueRoleProductManager is the role a slot critique's verdict is recorded under —
// the same wire label the design rails' appendCriticVerdict uses for the business-
// alignment critic, so a migrated verdict and a live one name one reviewer, not two.
const critiqueRoleProductManager = "productManager"

// The vocabulary a synthesized send-back round records its decider in. The operator note
// it was made from names no person — the phase-gate signal carried feedback, not an
// identity — so the ROLE is what the round can honestly claim, exactly as the
// construction rail's own gate rounds do.
const (
	gateRoleHuman     = "human"
	gateActorOperator = "operator"
)

// derivedMembers are the five members the new shape no longer stores, because each is
// DERIVED from the attempt ledger on read (spec §5.3). They are listed so the report can
// name what the migration dropped from each row rather than leaving a reader to diff
// 26 rows by eye.
var derivedMembers = []string{"phase", "phases", "currentPhase", "kind", "buildStatus"}

// owned reports whether key is one of the two members this tool may write.
func owned(key string) bool { return key == legacyMember || key == executionMember }

// ---- the design slot table -----------------------------------------------------------

// designSlot is one artifact slot, the design prefix activity whose lifecycle produces
// it, and the lifecycle PHASE it is the product of.
//
// It re-states, for a one-shot tool, the mapping the two design Managers hold as
// designActivityFor + designRoundKeyFor (systemdesign/coauthorartifact.go,
// projectdesign/coauthorphase2artifact.go). It is not shared with them: a cmd may not
// import a Manager package, and the Managers deliberately hold two copies of the rule for
// the same reason. Their parity with this table is what makes the ids below the ones a
// future dual-write would mint.
type designSlot struct {
	Kind           projectstate.ArtifactKind
	Activity       string
	LifecyclePhase projectstate.ActivityMethodPhase
	Of             func(projectstate.Project) projectstate.ArtifactSlot
}

// designSlots is every artifact slot, in ArtifactKind order.
//
// THE NINE PHASE-2 KINDS NAME A PHASE NO LIFECYCLE CARRIES. designActivityFor maps them
// to the projectDesign activity at the kind's OWN wire name — deliberately not at the
// "sdp" M0 gate — and the projectDesign lifecycle has exactly one phase, "sdp". So
// GateTaskFor answers "" for them and no round can be minted, which is precisely what the
// live rail does with them today (openDesignRound logs "no review task in the pinned
// lifecycle for this kind"). Their slot threads stay where they are; stage 6 decides what
// becomes of them.
var designSlots = []designSlot{
	{projectstate.KindMission, "requirements", "mission", func(p projectstate.Project) projectstate.ArtifactSlot { return p.Mission }},
	{projectstate.KindGlossary, "requirements", "glossary", func(p projectstate.Project) projectstate.ArtifactSlot { return p.Glossary }},
	{projectstate.KindScrubbedRequirements, "requirements", "glossary", func(p projectstate.Project) projectstate.ArtifactSlot { return p.ScrubbedRequirements }},
	{projectstate.KindVolatilities, "requirements", "volatilities", func(p projectstate.Project) projectstate.ArtifactSlot { return p.Volatilities }},
	{projectstate.KindCoreUseCases, "requirements", "coreUseCases", func(p projectstate.Project) projectstate.ArtifactSlot { return p.CoreUseCases }},
	{projectstate.KindSystem, "architecture", "architecture", func(p projectstate.Project) projectstate.ArtifactSlot { return p.SystemDesign }},
	{projectstate.KindOperationalConcepts, "architecture", "architecture", func(p projectstate.Project) projectstate.ArtifactSlot { return p.OperationalConcepts }},
	{projectstate.KindStandardCheck, "architecture", "architecture", func(p projectstate.Project) projectstate.ArtifactSlot { return p.StandardCheck }},
	{projectstate.KindPlanningAssumptions, "projectDesign", "planningAssumptions", func(p projectstate.Project) projectstate.ArtifactSlot { return p.PlanningAssumptions }},
	{projectstate.KindActivityList, "projectDesign", "activityList", func(p projectstate.Project) projectstate.ArtifactSlot { return p.ActivityList }},
	{projectstate.KindNetwork, "projectDesign", "network", func(p projectstate.Project) projectstate.ArtifactSlot { return p.Network }},
	{projectstate.KindNormalSolution, "projectDesign", "normalSolution", func(p projectstate.Project) projectstate.ArtifactSlot { return p.NormalSolution }},
	{projectstate.KindSubcriticalSolution, "projectDesign", "subcriticalSolution", func(p projectstate.Project) projectstate.ArtifactSlot { return p.SubcriticalSolution }},
	{projectstate.KindCompressedSolution, "projectDesign", "compressedSolution", func(p projectstate.Project) projectstate.ArtifactSlot { return p.CompressedSolution }},
	{projectstate.KindDecompressedSolution, "projectDesign", "decompressedSolution", func(p projectstate.Project) projectstate.ArtifactSlot { return p.DecompressedSolution }},
	{projectstate.KindRiskModel, "projectDesign", "riskModel", func(p projectstate.Project) projectstate.ArtifactSlot { return p.RiskModel }},
	{projectstate.KindSdpReview, "projectDesign", "sdpReview", func(p projectstate.Project) projectstate.ArtifactSlot { return p.SdpReview }},
}

// ---- rounds from a sealed slot thread --------------------------------------------------

// sealedOutcome is the outcome the LAST round of a sealed slot decided, read off the
// slot's own terminal review status. Every round before it is a send-back: the artifact
// came back for another draft, which is what a further round means.
//
// Total over the status vocabulary with no default arm. A slot that is not sealed —
// nothing filed, or a review still open — has no decided last round, and its zero outcome
// is how sealedSlot refuses it.
func sealedOutcome(s projectstate.ArtifactReviewStatus) projectstate.ReviewRoundOutcome {
	switch s {
	case projectstate.ReviewCommitted:
		return projectstate.RoundPassed
	case projectstate.ReviewRejected:
		return projectstate.RoundSentBack
	case projectstate.ReviewWithdrawn:
		// Not sentBack: nobody asked for another draft, the artifact was pulled. The
		// closed vocabulary has the exact word, and using the wrong one would tell a
		// reader the work was returned to its author when it was not.
		return projectstate.RoundWithdrawn
	case projectstate.ReviewNone, projectstate.ReviewAwaitingReview:
		return ""
	}
	return ""
}

// subjectRefFor names WHAT a synthesized design round judged.
//
// NO COMMIT SHA IS RESOLVABLE, and that is a fact about the record rather than a shortcut:
// ArtifactSlot.Provenance holds committedAt / approvedBy / draftedBy and no sha at all, and
// project.json's git history is the history of the WHOLE document — the commit that last
// touched the file is not the commit that staged this slot's draft. So the subject is the
// staged artifact, which is exactly the fallback the live rail's designSubjectRef takes
// when it has no pull request to point at, and the basis says which slot it came from.
//
// The Kind always comes from the closed three-value vocabulary (commit | artifact |
// pullRequest): the store validates only the Ref, so an invented kind would reach the wire
// enum unchecked.
func subjectRefFor(kind projectstate.ArtifactKind) (projectstate.SubjectRef, string) {
	return projectstate.SubjectRef{Kind: projectstate.SubjectArtifact, Ref: kind.WireName()},
		"slots." + kind.WireName() + " (no commit sha is recorded for a slot; the staged artifact is the subject)"
}

// threadRounds are the distinct round numbers the slot thread holds, ascending.
func threadRounds(thread []projectstate.ReviewComment) []int64 {
	var out []int64
	for _, c := range thread {
		if !slices.Contains(out, c.Round) {
			out = append(out, c.Round)
		}
	}
	slices.Sort(out)
	return out
}

// commentsInRound is the slot round's comments, in thread order, VERBATIM.
func commentsInRound(thread []projectstate.ReviewComment, round int64) []projectstate.ReviewComment {
	out := make([]projectstate.ReviewComment, 0, len(thread))
	for _, c := range thread {
		if c.Round == round {
			out = append(out, c)
		}
	}
	return out
}

// designRounds converts one design activity's sealed slot threads into rounds.
//
// ONE ROUND PER DISTINCT SLOT ROUND, and the number is the slot round PLUS ONE. The slot
// ledger's own counter is 0-based (a session's first review files r0c1) while the round
// ledger's is 1-based, so the live rails mint `<activity>:<gate>:<kind>:<slotRound+1>`
// (openDesignRound). Using the same offset here is what makes a future dual-write
// IDEMPOTENT on these ids rather than a second, colliding history — and it is also why a
// gate attempt recorded below the lowest minted number keeps its own reconstructed
// revision instead of being re-bound to a round that did not judge it (task 7's
// splitAtLowestRound).
//
// The artifact kind is part of the id because three kinds share the architecture gate and
// each counts its own rounds; a three-part id would fold two unrelated review histories
// into one.
func designRounds(activityID string, p projectstate.Project, held []projectstate.ReviewRound, now time.Time) ([]projectstate.ReviewRound, []string) {
	var out []projectstate.ReviewRound
	var skipped []string
	for _, s := range designSlots {
		if s.Activity != activityID {
			continue
		}
		slot := s.Of(p)
		if len(slot.ReviewThread) == 0 && slot.CritiqueVerdict == "" {
			continue
		}
		gate, work := projectstate.GateTaskFor(s.LifecyclePhase), projectstate.AgentTaskFor(s.LifecyclePhase)
		outcome := sealedOutcome(slot.Status)
		switch {
		case gate == "" || work == "":
			skipped = append(skipped, fmt.Sprintf("slots.%s: the pinned lifecycles carry no review task for phase %q — no round is minted",
				s.Kind.WireName(), s.LifecyclePhase))
		case outcome == "":
			skipped = append(skipped, fmt.Sprintf("slots.%s: not sealed (review status %d) — its review has not been decided",
				s.Kind.WireName(), slot.Status))
		default:
			out = append(out, slotRounds(activityID, s, slot, gate, work, outcome, held, out, now)...)
		}
	}
	return out, skipped
}

// slotRounds is designRounds' per-slot half: the rounds ONE sealed slot yields, with the
// slot's critique landed on the last of them.
func slotRounds(
	activityID string,
	s designSlot,
	slot projectstate.ArtifactSlot,
	gate, work projectstate.MethodTask,
	last projectstate.ReviewRoundOutcome,
	held, minted []projectstate.ReviewRound,
	now time.Time,
) []projectstate.ReviewRound {
	rounds := threadRounds(slot.ReviewThread)
	var out []projectstate.ReviewRound
	for i, slotRound := range rounds {
		n := slotRound + 1
		id := fmt.Sprintf("%s:%s:%s:%d", activityID, gate, s.Kind.WireName(), n)
		if holdsRound(held, id) || holdsRound(minted, id) || holdsRound(out, id) {
			continue
		}
		outcome := projectstate.RoundSentBack
		if i == len(rounds)-1 {
			outcome = last
		}
		subject, subjectBasis := subjectRefFor(s.Kind)
		out = append(out, projectstate.ReviewRound{
			RoundID: id, TaskID: gate, Reviews: work, Round: n, SubjectRef: subject,
			Thread:  commentsInRound(slot.ReviewThread, slotRound),
			Outcome: outcome,
			Provenance: backfilled(now, fmt.Sprintf("slots.%s.reviewThread round %d; subject from %s",
				s.Kind.WireName(), slotRound, subjectBasis)),
		})
	}
	return withCritique(out, slot, now, s.Kind)
}

// withCritique lands the slot's recorded critique on the round it judged: the one whose
// number matches it, and the LAST otherwise — a critique carries no round of its own, and
// the draft it judged is the one the last round staged.
//
// The verdict is the product manager's, which is the role the Method assigns the critic
// of a design draft and the role the live rail records (appendCriticVerdict). REVISE is a
// send-back — the critic asking for another draft — and the other member of that closed
// two-value carrier is a ratification.
func withCritique(rounds []projectstate.ReviewRound, slot projectstate.ArtifactSlot, now time.Time, kind projectstate.ArtifactKind) []projectstate.ReviewRound {
	if slot.CritiqueVerdict == "" || len(rounds) == 0 {
		return rounds
	}
	verdict := projectstate.VerdictApprove
	if slot.CritiqueVerdict == projectstate.CritiqueVerdictRevise {
		verdict = projectstate.VerdictSendBack
	}
	at := len(rounds) - 1
	rounds[at].Verdicts = append(rounds[at].Verdicts, projectstate.ReviewVerdict{
		ReviewerRole: critiqueRoleProductManager, Actor: critiqueRoleProductManager,
		Verdict: verdict, Summary: slot.CritiqueNotes,
	})
	rounds[at].Provenance = backfilled(now, rounds[at].Provenance.Basis+
		"; critique from slots."+kind.WireName()+".critiqueVerdict")
	return rounds
}

// ---- rounds from a send-back operator note ----------------------------------------------

// noteRounds converts the row's send-back operator notes into rounds — the record a
// send-back used to leave as one line of prose with no roster, no verdict, no subject and
// no round number.
//
// THE ROUND NUMBER IS THE GATE ATTEMPT THE NOTE BELONGS TO, matched TAILS ALIGNED exactly
// as the read path's reconstruction matches them (reconstructedReviewRevisions R4): notes
// only exist since B1.1, so it is the OLDEST rejections that have none, and the last
// len(rejections) notes pair with the rejections in order. A note with no rejection to
// pair with sits BELOW the ledger's lowest number, which is where
// appendPreLedgerRejections puts the attempt it reconstructs for it — so the round and
// that reconstruction name the same review rather than double-counting it.
func noteRounds(activityID string, row projectstate.ActivityExecution, held []projectstate.ReviewRound, now time.Time) []projectstate.ReviewRound {
	var out []projectstate.ReviewRound
	for _, phase := range notedPhases(row.OperatorNotes) {
		gate := projectstate.GateTaskFor(phase)
		if gate == "" {
			continue
		}
		notes := sendBacksAt(row.OperatorNotes, phase)
		for i, n := range noteNumbers(row.Attempts, gate, len(notes)) {
			id := projectstate.AttemptID(activityID, gate, n)
			if holdsRound(held, id) || holdsRound(out, id) || holdsNumber(held, gate, int64(n)) {
				continue
			}
			out = append(out, noteRound(id, gate, phase, n, notes[i], now))
		}
	}
	return out
}

// noteRound is one send-back note as a round: the operator's own words as the human
// verdict's summary, and the anchored comments that rode with it as the round's thread.
func noteRound(id string, gate projectstate.MethodTask, phase projectstate.ActivityMethodPhase, n int, note projectstate.OperatorNote, now time.Time) projectstate.ReviewRound {
	return projectstate.ReviewRound{
		RoundID: id, TaskID: gate, Reviews: projectstate.AgentTaskFor(phase), Round: int64(n),
		SubjectRef: projectstate.SubjectRef{Kind: projectstate.SubjectArtifact, Ref: string(phase)},
		Reviewers:  []projectstate.RoundReviewer{{Role: gateRoleHuman, Actor: gateActorOperator, Required: true}},
		Verdicts: []projectstate.ReviewVerdict{{
			ReviewerRole: gateRoleHuman, Actor: gateActorOperator,
			Verdict: projectstate.VerdictSendBack, Summary: note.Text,
			At: note.RecordedAt.UTC().Format(time.RFC3339),
		}},
		Thread:     noteThread(note, int64(n)),
		Outcome:    projectstate.RoundSentBack,
		DecidedBy:  gateActorOperator,
		DecidedAt:  note.RecordedAt.UTC().Format(time.RFC3339),
		Provenance: backfilled(now, "operatorNotes["+note.NoteID+"]"),
	}
}

// noteThread turns a note's anchored comments into the round's thread. The ids are the
// store's own deterministic (round, index) minting, so a reader meets the same shape of id
// a real round carries.
func noteThread(note projectstate.OperatorNote, round int64) []projectstate.ReviewComment {
	out := make([]projectstate.ReviewComment, 0, len(note.Comments))
	for i, c := range note.Comments {
		out = append(out, projectstate.ReviewComment{
			ID: projectstate.ReviewCommentID(round, i), Anchor: c.JSONPath, Text: c.Text,
			AuthorRole: gateRoleHuman, Round: round,
			Status: projectstate.ReviewCommentAnswered,
			Type:   projectstate.ReviewCommentTypeChangeRequest,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// notedPhases are the lifecycle phases the row's send-back notes were written at, in
// recorded order (append-only slice order IS RecordedAt order).
func notedPhases(notes []projectstate.OperatorNote) []projectstate.ActivityMethodPhase {
	var out []projectstate.ActivityMethodPhase
	for _, n := range notes {
		p := projectstate.ActivityMethodPhase(n.Gate)
		if n.Kind == projectstate.NoteSendBack && n.Gate != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// sendBacksAt is the phase's send-back notes in recorded order.
func sendBacksAt(notes []projectstate.OperatorNote, phase projectstate.ActivityMethodPhase) []projectstate.OperatorNote {
	out := make([]projectstate.OperatorNote, 0, len(notes))
	for _, n := range notes {
		if n.Kind == projectstate.NoteSendBack && projectstate.ActivityMethodPhase(n.Gate) == phase {
			out = append(out, n)
		}
	}
	return out
}

// noteNumbers is the round number each of the phase's notes takes, tails aligned onto the
// gate's RECORDED rejections. A note with no rejection to pair with numbers below the
// ledger's lowest attempt, which is where the read path's own reconstruction puts it.
func noteNumbers(attempts []projectstate.TaskAttempt, gate projectstate.MethodTask, notes int) []int {
	var rejections []int
	lowest := 0
	for _, a := range attempts {
		if a.Task != gate {
			continue
		}
		if lowest == 0 || a.Attempt < lowest {
			lowest = a.Attempt
		}
		if a.Outcome == projectstate.OutcomeRejected {
			rejections = append(rejections, a.Attempt)
		}
	}
	offset := notes - len(rejections)
	out := make([]int, 0, notes)
	for i := range notes {
		switch {
		case i >= offset:
			out = append(out, rejections[i-offset])
		case lowest > 0:
			out = append(out, lowest-(offset-i))
		default:
			out = append(out, 1+i)
		}
	}
	return out
}

// ---- the edit ---------------------------------------------------------------------------

// backfilled is the provenance stamp every round this tool synthesizes carries. Never
// observed, never synthesized: each one is derived from a real record that exists
// elsewhere in the document, and basis names which.
func backfilled(now time.Time, basis string) projectstate.AttemptProvenance {
	at := now
	return projectstate.AttemptProvenance{
		Origin: projectstate.OriginBackfilled, Generator: generatorID, GeneratedAt: &at, Basis: basis,
	}
}

func holdsRound(rounds []projectstate.ReviewRound, id string) bool {
	return slices.ContainsFunc(rounds, func(r projectstate.ReviewRound) bool { return r.RoundID == id })
}

func holdsNumber(rounds []projectstate.ReviewRound, gate projectstate.MethodTask, n int64) bool {
	return slices.ContainsFunc(rounds, func(r projectstate.ReviewRound) bool { return r.TaskID == gate && r.Round == n })
}

// report is the whole run's account of itself: every row it changed, every round it
// backfilled, every derived member it dropped and every slot it deliberately left alone.
type report struct {
	Rows    int
	Rounds  int
	Lines   []string
	Dropped []string
	Skipped []string
}

// convert is the WHOLE transformation, over the decoded aggregate. p.ActivityExecution is
// already the legacy rows carried forward by the codec's own read-both decoder
// (activityExecutionOrLegacy → toActivityExecution), which is the one implementation of
// that carry-forward; this adds what the carry-forward cannot know: the version, the pin,
// and the rounds the review history was hiding in.
func convert(p *projectstate.Project, stored map[string]json.RawMessage, now time.Time) report {
	rep := report{}
	items := committedItems(*p)
	dropped := map[string]bool{}
	for _, id := range rowIDs(p.ActivityExecution) {
		row := p.ActivityExecution[id]
		before := row
		row.Version, row.Pin = stampedVersion(row), stampedPin(row, items[id])
		rounds, skipped := designRounds(id, *p, row.Reviews, now)
		rounds = append(rounds, noteRounds(id, row, append(slices.Clone(row.Reviews), rounds...), now)...)
		row.Reviews = append(slices.Clone(row.Reviews), rounds...)
		rep.Skipped = append(rep.Skipped, skipped...)
		for _, m := range droppedMembers(stored[id]) {
			dropped[m] = true
		}
		if sameRow(before, row) {
			continue
		}
		p.ActivityExecution[id] = row
		rep.Rows++
		rep.Rounds += len(rounds)
		rep.Lines = append(rep.Lines, rowLine(id, row, rounds, stored[id]))
	}
	rep.Dropped = sortedKeys(dropped)
	return rep
}

// sameRow reports whether the edit left the row exactly as it stood — the idempotence
// test, asked of the VALUE rather than of a flag each edit would have to remember to set.
func sameRow(was, now projectstate.ActivityExecution) bool {
	return was.Version == now.Version && samePin(was.Pin, now.Pin) && len(was.Reviews) == len(now.Reviews)
}

func samePin(was, now *projectstate.LifecyclePin) bool {
	if was == nil || now == nil {
		return was == now
	}
	return *was == *now
}

// stampedVersion is the row's per-activity optimistic counter. A row the migration
// reaches has never been written by the new verbs, so it starts at 1 — but a counter a
// real writer has already advanced is NEVER lowered: a migration that reset it would let
// a stale expected-version write land.
func stampedVersion(row projectstate.ActivityExecution) int64 {
	if row.Version > 0 {
		return row.Version
	}
	return 1
}

// stampedPin is the lifecycle the row's task DAG is read against: the type key its
// classified type names, and the method-assets release in force. A row already pinned
// keeps its pin — that is the whole point of a pin — and a row ClassifyType refuses to
// type gets none, because naming a lifecycle for it would be a guess.
func stampedPin(row projectstate.ActivityExecution, item projectstate.ActivityItem) *projectstate.LifecyclePin {
	if row.Pin != nil {
		return row.Pin
	}
	typ, variant, _, classified := projectstate.ResolveConstructionRow(row, item)
	if !classified {
		return nil
	}
	return &projectstate.LifecyclePin{
		TypeKey:       projectstate.LifecycleKeyFor(typ, variant),
		AssetsVersion: methodassets.Version(),
	}
}

// committedItems is the committed plan's activity items by name — the authority on what
// each row IS, which is what ResolveConstructionRow classifies from.
func committedItems(p projectstate.Project) map[string]projectstate.ActivityItem {
	out := map[string]projectstate.ActivityItem{}
	list, ok := p.ActivityList.Model.(*projectstate.ActivityList)
	if !ok || list == nil {
		return out
	}
	for _, it := range list.Activities {
		out[it.Name] = it
	}
	return out
}

// droppedMembers names the derived members the stored legacy row held and the new shape
// does not. Read off the STORED bytes, not off the typed row: the report's job is to say
// what left the document.
func droppedMembers(stored json.RawMessage) []string {
	if len(stored) == 0 {
		return nil
	}
	var held map[string]json.RawMessage
	if err := json.Unmarshal(stored, &held); err != nil {
		return nil
	}
	var out []string
	for _, m := range derivedMembers {
		if _, ok := held[m]; ok {
			out = append(out, m)
		}
	}
	return out
}

func rowLine(id string, row projectstate.ActivityExecution, rounds []projectstate.ReviewRound, stored json.RawMessage) string {
	ids := make([]string, 0, len(rounds))
	for _, r := range rounds {
		ids = append(ids, fmt.Sprintf("%s [%s]", r.RoundID, r.Outcome))
	}
	pin := "unclassified — no pin"
	if row.Pin != nil {
		pin = row.Pin.TypeKey + "@" + row.Pin.AssetsVersion
	}
	line := fmt.Sprintf("  %-34s %2d attempts  version %d  pin %s  dropped [%s]",
		id, len(row.Attempts), row.Version, pin, strings.Join(droppedMembers(stored), " "))
	if len(ids) > 0 {
		line += "\n" + strings.Repeat(" ", 4) + "rounds: " + strings.Join(ids, ", ")
	}
	return line
}

// rowIDs is every execution row's id, sorted, so a run's report and its edit order are
// deterministic.
func rowIDs(rows map[string]projectstate.ActivityExecution) []string {
	out := make([]string, 0, len(rows))
	for id := range rows {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- the splice ---------------------------------------------------------------------

// member is one member of a JSON object, in document order.
type member struct {
	key   string
	value json.RawMessage
}

// members decodes a compact JSON object into its members, in document order. An object
// that holds a member twice is refused: which copy counts is up to the reader, so a splice
// over it could write one copy and leave the other standing.
func members(raw []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}
	var out []member
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("read member key: %w", err)
		}
		key, _ := tok.(string)
		if seen[key] {
			return nil, fmt.Errorf("the document holds member %q twice — which copy counts is ambiguous; refusing", key)
		}
		seen[key] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("read member %q: %w", key, err)
		}
		out = append(out, member{key: key, value: value})
	}
	return out, nil
}

// joinMembers re-emits members as a compact JSON object.
func joinMembers(ms []member) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(m.key) // a string always marshals
		b.Write(key)
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// valueOf returns the value of key among ms, or nil.
func valueOf(ms []member, key string) json.RawMessage {
	for _, m := range ms {
		if m.key == key {
			return m.value
		}
	}
	return nil
}

// compactDocument returns the document compacted, plus the whitespace after its closing
// brace. FIDELITY GATE: it refuses a document that re-indenting would not reproduce
// byte-for-byte — writing one would smuggle a reformat of unrelated state into the diff.
func compactDocument(raw []byte) ([]byte, []byte, error) {
	trimmed := bytes.TrimRight(raw, " \t\r\n")
	trailer := raw[len(trimmed):]
	var body bytes.Buffer
	if err := json.Compact(&body, trimmed); err != nil {
		return nil, nil, fmt.Errorf("parse the project document: %w", err)
	}
	var again bytes.Buffer
	if err := json.Indent(&again, body.Bytes(), "", "  "); err != nil {
		return nil, nil, fmt.Errorf("indent the project document: %w", err)
	}
	if !bytes.Equal(again.Bytes(), trimmed) {
		return nil, nil, errors.New("re-indenting the project document does not reproduce it byte-for-byte; a rewrite would reformat unrelated state — refusing")
	}
	return body.Bytes(), trailer, nil
}

// decodeDocument decodes a project document through the codec, under the id it carries.
func decodeDocument(raw []byte) (projectstate.Project, error) {
	var head struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return projectstate.Project{}, fmt.Errorf("read the project id: %w", err)
	}
	p, ok, err := projectstate.DecodeProjectJSON(raw, projectstate.ProjectID(head.ID))
	if err != nil {
		return projectstate.Project{}, err
	}
	if !ok {
		return projectstate.Project{}, errors.New("the file holds no project document")
	}
	return p, nil
}

// encodeCompact encodes p through the codec, compacted.
func encodeCompact(p projectstate.Project) ([]byte, error) {
	enc, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Compact(&out, enc); err != nil {
		return nil, fmt.Errorf("compact the encoded project: %w", err)
	}
	return out.Bytes(), nil
}

// legacyRowsOf reads the STORED legacy member off the raw document — before any
// decode/encode cycle — and proves the typed legacy reader carries every member of it.
//
// This is the migration's own fidelity gate, and it is the analogue of
// backfill-attempts' roundTripsExactly for a member the codec deliberately does NOT
// re-encode: the legacy map is read-only on the way in and vanishes on the way out, so a
// codec-vs-codec comparison can never see what the typed read dropped. Only a comparison
// against the ORIGINAL bytes can, and CodecCarriesEveryMember is what names the loss.
func legacyRowsOf(body []byte) (map[string]json.RawMessage, error) {
	ms, err := members(body)
	if err != nil {
		return nil, err
	}
	value := valueOf(ms, legacyMember)
	if value == nil {
		// Already migrated: there is no legacy member left to prove anything about, and an
		// empty map is what every caller below then reads nothing out of.
		return map[string]json.RawMessage{}, nil
	}
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(value, &stored); err != nil {
		return nil, fmt.Errorf("read the legacy %s member: %w", legacyMember, err)
	}
	var typed map[string]projectstate.LegacyActivityConstructionRow
	if err := json.Unmarshal(value, &typed); err != nil {
		return nil, fmt.Errorf("read the legacy %s member as typed rows: %w", legacyMember, err)
	}
	encoded, err := json.Marshal(typed)
	if err != nil {
		return nil, err
	}
	lost, err := projectstate.CodecCarriesEveryMember(value, encoded)
	if err != nil {
		return nil, err
	}
	if len(lost) > 0 {
		return nil, fmt.Errorf("the typed legacy reader does not carry %d member(s) of .%s — migrating would lose them: %s",
			len(lost), legacyMember, strings.Join(lost, ", "))
	}
	return stored, nil
}

// splice puts the codec's encoding of .activityExecution into the original document,
// removes the retired .activityConstruction, and changes nothing else.
//
//   - .activityExecution is REPLACED in place when the document holds it (its original
//     bytes must first equal the codec's own encoding of them, or the replacement would
//     silently drop whatever the codec does not carry), ADDED at the position the codec
//     gives it otherwise, and REMOVED when the codec omits it after the edit.
//   - .activityConstruction is dropped outright. It is the member being retired, and its
//     fidelity was proved against the ORIGINAL bytes before any of this ran.
func splice(body, before, after []byte) ([]byte, error) {
	original, err := members(body)
	if err != nil {
		return nil, err
	}
	was, err := members(before)
	if err != nil {
		return nil, err
	}
	now, err := members(after)
	if err != nil {
		return nil, err
	}
	if err := onlyOwnedEdited(was, now); err != nil {
		return nil, err
	}
	if err := roundTripsExactly(valueOf(original, executionMember), valueOf(was, executionMember)); err != nil {
		return nil, err
	}
	return joinMembers(placed(original, now)), nil
}

// placed rebuilds the document with the owned members in their new state.
func placed(original, encoded []member) []member {
	value := valueOf(encoded, executionMember)
	out := make([]member, 0, len(original)+1)
	done := false
	for _, m := range original {
		switch m.key {
		case executionMember:
			if value != nil {
				out = append(out, member{key: m.key, value: value})
			}
			done = true
		case legacyMember:
			// The rename: the retired member is where the new one belongs, and the codec
			// agrees (activityExecution immediately precedes it in projectDoc's order).
			if value != nil && !done {
				out = append(out, member{key: executionMember, value: value})
				done = true
			}
		default:
			out = append(out, m)
		}
	}
	if !done && value != nil {
		out = append(out, member{key: executionMember, value: value})
	}
	return out
}

// onlyOwnedEdited refuses an edit whose codec encoding moved any member this tool does
// not own.
func onlyOwnedEdited(was, now []member) error {
	for _, m := range now {
		if !owned(m.key) && !bytes.Equal(m.value, valueOf(was, m.key)) {
			return fmt.Errorf("the edit changed %s, which this tool may not touch — refusing", m.key)
		}
	}
	return nil
}

// roundTripsExactly refuses to replace a committed member whose original bytes differ
// from the codec's encoding of them. The codec drops whatever it does not carry, and a
// codec-vs-codec comparison cannot see that loss — only a comparison against the original
// bytes can.
func roundTripsExactly(held, encoded json.RawMessage) error {
	if held == nil {
		return nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, held); err != nil {
		return err
	}
	if !bytes.Equal(compact.Bytes(), encoded) {
		return fmt.Errorf("the committed .%s does not survive a codec round trip byte-for-byte (the codec would drop or reshape part of it) — replacing it would lose data; refusing", executionMember)
	}
	return nil
}

// confirmOnlyOwnedMoved proves every member this tool does not own is byte-identical, and
// in the same order, in the rewritten document.
func confirmOnlyOwnedMoved(original, rewritten []byte) error {
	was, err := members(original)
	if err != nil {
		return err
	}
	now, err := members(rewritten)
	if err != nil {
		return err
	}
	strip := func(ms []member) []member {
		out := make([]member, 0, len(ms))
		for _, m := range ms {
			if !owned(m.key) {
				out = append(out, m)
			}
		}
		return out
	}
	a, b := strip(was), strip(now)
	if len(a) != len(b) {
		return errors.New("the rewrite changed the member set outside the two members it owns — refusing")
	}
	for i := range a {
		if a[i].key != b[i].key || !bytes.Equal(a[i].value, b[i].value) {
			return fmt.Errorf("the rewrite changed %s — refusing", a[i].key)
		}
	}
	return nil
}

// migrate is the whole run over a document's bytes: read the legacy rows raw, decode,
// convert, re-encode, splice, and prove three ways that nothing outside the two owned
// members moved.
func migrate(raw []byte, now time.Time) ([]byte, report, error) {
	body, trailer, err := compactDocument(raw)
	if err != nil {
		return nil, report{}, err
	}
	stored, err := legacyRowsOf(body)
	if err != nil {
		return nil, report{}, err
	}
	p, err := decodeDocument(raw)
	if err != nil {
		return nil, report{}, err
	}
	before, err := encodeCompact(p)
	if err != nil {
		return nil, report{}, err
	}
	rep := convert(&p, stored, now)
	after, err := encodeCompact(p)
	if err != nil {
		return nil, report{}, err
	}
	spliced, err := splice(body, before, after)
	if err != nil {
		return nil, report{}, err
	}
	out, err := indented(body, spliced, after, trailer)
	if err != nil {
		return nil, report{}, err
	}
	return out, rep, nil
}

// indented runs the structural tripwires over the spliced document and returns it
// re-indented with its original trailer.
//
// The two checks hold by construction today — the splicer builds its output from the
// original members and swaps only the two it owns, and the codec re-encodes what it
// decoded — so no test can reach either. They stay on purpose: an edit to the splicer that
// reached another member, or a codec whose encoding of .activityExecution stopped being a
// fixed point, would otherwise be written to the state file silently.
func indented(body, spliced, after, trailer []byte) ([]byte, error) {
	if err := confirmOnlyOwnedMoved(body, spliced); err != nil {
		return nil, err
	}
	back, err := decodeDocument(spliced)
	if err != nil {
		return nil, err
	}
	again, err := encodeCompact(back)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(again, after) {
		return nil, errors.New("the spliced document does not decode to the edited project — refusing to write")
	}
	var out bytes.Buffer
	if err := json.Indent(&out, spliced, "", "  "); err != nil {
		return nil, fmt.Errorf("indent the rewritten document: %w", err)
	}
	out.Write(trailer)
	return out.Bytes(), nil
}

// ---- the run --------------------------------------------------------------------------

func run(root string, dryRun bool) error {
	file := filepath.Join(root, statePath)
	raw, err := readState(file)
	if err != nil {
		return err
	}
	out, rep, err := migrate(raw, time.Now().UTC())
	if err != nil {
		return err
	}
	printReport(rep, dryRun)
	if dryRun {
		return nil
	}
	if bytes.Equal(out, raw) {
		fmt.Println("nothing to write")
		return nil
	}
	if err := writeState(file, out); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", file)
	return nil
}

// readState reads the committed project document. Its own function so the one place this
// tool touches an operator-supplied path is named, reviewed and reachable from nowhere
// else — and CLEANED there, so the path that reaches the filesystem holds no traversal
// segments the caller typed. (Its sibling cmd/backfill-attempts suppresses the same
// finding with a //nolint; sanitizing is the fix the linter is actually asking for.)
func readState(file string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	return raw, nil
}

// writeState writes the migrated document back over the file it was read from.
func writeState(file string, out []byte) error {
	if err := os.WriteFile(file, out, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", file, err)
	}
	return nil
}

func printReport(rep report, dryRun bool) {
	mode := "write"
	if dryRun {
		mode = "dry-run"
	}
	fmt.Printf("migrate-activity-execution (%s)\n\nROWS\n", mode)
	for _, line := range rep.Lines {
		fmt.Println(line)
	}
	if len(rep.Skipped) > 0 {
		fmt.Println("\nSLOTS LEFT ALONE")
		for _, s := range rep.Skipped {
			fmt.Println("  " + s)
		}
	}
	fmt.Printf("\n  %d row(s) migrated, %d round(s) backfilled, derived members dropped: [%s]\n",
		rep.Rows, rep.Rounds, strings.Join(rep.Dropped, " "))
}

func main() {
	root := flag.String("root", "..", "path to the repository root containing .aiarch/state/project.json")
	dryRun := flag.Bool("dry-run", false, "report what would be written without writing")
	flag.Parse()

	if err := run(*root, *dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "migrate-activity-execution: %v\n", err)
		os.Exit(1)
	}
}
