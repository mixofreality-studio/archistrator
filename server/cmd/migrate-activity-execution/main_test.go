package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// migratedAt is the clock every test migrates under, so a fixture's provenance stamp is
// a fixed string rather than whatever the test happened to run at.
var migratedAt = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

// ---- fixtures ---------------------------------------------------------------------

func strp(s string) *string { return &s }

// committedSlot is a sealed slot holding a model, a thread and a critique.
func committedSlot(m projectstate.ArtifactModel, thread []projectstate.ReviewComment) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: m, ReviewThread: thread, Revisions: int64(len(thread))}
}

func comment(id string, round int64, role, text string) projectstate.ReviewComment {
	return projectstate.ReviewComment{
		ID: id, Text: text, AuthorRole: role, Round: round,
		Status: projectstate.ReviewCommentAnswered, Type: projectstate.ReviewCommentTypeChangeRequest,
		Response: strp(""),
	}
}

// fixtureProject is a project whose architecture activity has a two-round sealed slot
// thread, a critique, and a legacy execution row.
func fixtureProject() projectstate.Project {
	return projectstate.Project{
		ID: "p1", Version: 7, Owner: "o", Name: "fixture",
		SystemDesign: committedSlot(&projectstate.System{Components: []projectstate.Component{{ID: "c", Name: "c"}}}, []projectstate.ReviewComment{
			comment("r1c1", 1, "architect", "first round, first comment"),
			comment("r1c2", 1, "productManager", "first round, second comment"),
			comment("r2c1", 2, "architect", "second round"),
		}),
		ActivityList: committedSlot(&projectstate.ActivityList{Activities: []projectstate.ActivityItem{
			{Name: "architecture"}, {Name: "C-thing", Coding: true, WorkerClass: "senior"},
		}}, nil),
	}
}

// legacyRows is the fixture's `.activityConstruction` member as the migration finds it: one
// design row and one construction row, each carrying the five derived members the
// migration drops.
func legacyRows() map[string]projectstate.LegacyActivityConstructionRow {
	ended := migratedAt.Add(-48 * time.Hour)
	return map[string]projectstate.LegacyActivityConstructionRow{
		"architecture": {
			ActivityID: "architecture", Type: projectstate.ActivityTypeArchitecture,
			Phase:        projectstate.LegacyPhaseDone,
			CurrentPhase: "architecture",
			Attempts: []projectstate.TaskAttempt{{
				AttemptID: "architecture:architectureReview:1", Task: "architectureReview", Phase: "architecture",
				Attempt: 1, Actor: projectstate.ActorAgent, EndedAt: &ended, Outcome: projectstate.OutcomePassed,
				Provenance: projectstate.AttemptProvenance{Origin: projectstate.OriginBackfilled, Basis: "committed design artifacts"},
			}},
		},
		"C-thing": {
			ActivityID: "C-thing", Type: projectstate.ActivityTypeService,
			Phase: projectstate.LegacyPhaseRunning,
			Attempts: []projectstate.TaskAttempt{{
				AttemptID: "C-thing:srsReview:1", Task: "srsReview", Phase: projectstate.MethodPhaseRequirements,
				Attempt: 1, Actor: projectstate.ActorAgent, EndedAt: &ended, Outcome: projectstate.OutcomePassed,
				Provenance: projectstate.AttemptProvenance{Origin: projectstate.OriginBackfilled, Basis: "code"},
			}},
		},
	}
}

// legacyDocument materializes a project.json in the LEGACY shape: the codec's own encoding of p
// with the legacy member spliced in where the codec would put the execution one. Built
// through the codec so the fidelity gate (re-indenting reproduces the document) holds.
func legacyDocument(t *testing.T, p projectstate.Project, rows map[string]projectstate.LegacyActivityConstructionRow) []byte {
	t.Helper()
	value, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal the legacy rows: %v", err)
	}
	return legacyDocumentWith(t, p, value)
}

// legacyDocumentWith is legacyDocument over an arbitrary legacy member value, so a test
// can put a member in it that the typed legacy row does not model.
func legacyDocumentWith(t *testing.T, p projectstate.Project, value json.RawMessage) []byte {
	t.Helper()
	encoded, err := encodeCompact(p)
	if err != nil {
		t.Fatalf("encode the fixture: %v", err)
	}
	ms, err := members(encoded)
	if err != nil {
		t.Fatalf("read the fixture's members: %v", err)
	}
	at := len(ms)
	for i, m := range ms {
		if m.key == "slots" {
			at = i + 1
		}
	}
	ms = slices.Insert(ms, at, member{key: legacyMember, value: value})
	var out bytes.Buffer
	if err := json.Indent(&out, joinMembers(ms), "", "  "); err != nil {
		t.Fatalf("indent the fixture: %v", err)
	}
	return out.Bytes()
}

// migrated runs the whole rewrite over raw and returns the new document.
func migrated(t *testing.T, raw []byte) []byte {
	t.Helper()
	out, _, err := migrate(raw, migratedAt)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return out
}

func decode(t *testing.T, raw []byte) projectstate.Project {
	t.Helper()
	p, err := decodeDocument(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return p
}

// derivedPhases is the read path's own answer for every row: the phase completions
// ResolveConstructionRow resolves, which is what every consumer of a row reads.
func derivedPhases(p projectstate.Project) map[string][]projectstate.PhaseCompletion {
	items := map[string]projectstate.ActivityItem{}
	if list, ok := p.ActivityList.Model.(*projectstate.ActivityList); ok && list != nil {
		for _, it := range list.Activities {
			items[it.Name] = it
		}
	}
	out := make(map[string][]projectstate.PhaseCompletion, len(p.ActivityExecution))
	for id, row := range p.ActivityExecution {
		_, _, resolved, _ := projectstate.ResolveConstructionRow(row, items[id])
		out[id] = resolved
	}
	return out
}

// ---- the acceptance criterion ------------------------------------------------------

// THE MIGRATION'S ONE ACCEPTANCE CRITERION (spec §5.3 F): what the document used to say
// about phase completion and what the new shape derives must be the same answer for every
// row. A migration that changes an activity's history is not a migration.
//
// It is stated over the REAL read path on BOTH sides — the legacy-shaped document decoded
// through DecodeProjectJSON (which carries the legacy rows forward) and the post-migration
// one decoded the same way — because a comparison against a hand-computed expectation
// would only prove the test and the tool agree.
func TestMigrate_DerivedPhasesEqualThePreMigrationDerivation(t *testing.T) {
	raw := preMigrationState(t)
	before := derivedPhases(decode(t, raw))
	after := derivedPhases(decode(t, migrated(t, raw)))
	if len(before) == 0 {
		t.Fatal("the fixture derived nothing; the acceptance test would assert nothing")
	}
	if len(before) != len(after) {
		t.Fatalf("the row set changed: %d rows before, %d after", len(before), len(after))
	}
	for id, was := range before {
		now, ok := after[id]
		if !ok {
			t.Fatalf("row %s vanished", id)
		}
		if !reflect.DeepEqual(was, now) {
			t.Fatalf("row %s: derived phases differ\n before %+v\n after  %+v", id, was, now)
		}
	}
}

// realState is this repository's own committed project.json — the document the migration
// was written to run on, and did.
func realState(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", statePath))
	if err != nil {
		t.Fatalf("read the repository's own state: %v", err)
	}
	return raw
}

// preMigrationState rebuilds this repository's state AS IT STOOD before the migration
// ran: the execution member back under its legacy name, each row stripped of the three
// things the migration added (the version, the pin and the rounds) and given back the
// coarse roll-up it stored.
//
// THE ACCEPTANCE TEST NEEDS A DOCUMENT IN THE LEGACY SHAPE, and once the migration has landed
// the repository no longer holds one. Reading it out of git history would tie the test to
// a sha that a rebase moves; committing a 1.1MB copy of it as a fixture would put the same
// bytes in the tree twice. Reconstructing it from the migrated state keeps the acceptance
// criterion live over the REAL 26 rows for as long as they exist: the conversion runs
// again, over the same input it ran on, and its answer must still be the same one the
// committed state gives.
func preMigrationState(t *testing.T) []byte {
	t.Helper()
	body, trailer, err := compactDocument(realState(t))
	if err != nil {
		t.Fatalf("compact the committed state: %v", err)
	}
	ms, err := members(body)
	if err != nil {
		t.Fatalf("read the committed state's members: %v", err)
	}
	var rows map[string]map[string]json.RawMessage
	if err := json.Unmarshal(valueOf(ms, executionMember), &rows); err != nil {
		t.Fatalf("read the execution member: %v", err)
	}
	for _, row := range rows {
		delete(row, "version")
		delete(row, "lifecyclePin")
		delete(row, "reviews")
		row["phase"] = json.RawMessage("0")
	}
	value, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal the legacy rows: %v", err)
	}
	for i := range ms {
		if ms[i].key == executionMember {
			ms[i] = member{key: legacyMember, value: value}
		}
	}
	var out bytes.Buffer
	if err := json.Indent(&out, joinMembers(ms), "", "  "); err != nil {
		t.Fatalf("indent the reconstructed document: %v", err)
	}
	out.Write(trailer)
	return out.Bytes()
}

// TestMigrate_TheRealStateKeepsEveryRowAndEveryAttempt is the other half of the
// acceptance: the rows and their ledgers are carried VERBATIM, so the derivation above
// has the same input on both sides for a reason a reader can check.
func TestMigrate_TheRealStateKeepsEveryRowAndEveryAttempt(t *testing.T) {
	raw := preMigrationState(t)
	before, after := decode(t, raw), decode(t, migrated(t, raw))
	for id, was := range before.ActivityExecution {
		now, ok := after.ActivityExecution[id]
		if !ok {
			t.Fatalf("row %s vanished", id)
		}
		if !reflect.DeepEqual(was.Attempts, now.Attempts) {
			t.Fatalf("row %s: the attempt ledger changed", id)
		}
		if !reflect.DeepEqual(was.OperatorNotes, now.OperatorNotes) || was.Produced != nil && !reflect.DeepEqual(was.Produced, now.Produced) {
			t.Fatalf("row %s: a carried member changed", id)
		}
	}
}

// Provenance is not laundered: nothing the migration writes is observed, and an attempt
// that was backfilled stays backfilled with the generator that made it.
func TestMigrate_PreservesAttemptProvenanceOrigin(t *testing.T) {
	raw := preMigrationState(t)
	before, after := decode(t, raw), decode(t, migrated(t, raw))
	for id, now := range after.ActivityExecution {
		was := before.ActivityExecution[id]
		for i, a := range now.Attempts {
			if !reflect.DeepEqual(a.Provenance, was.Attempts[i].Provenance) {
				t.Fatalf("row %s attempt %s: provenance rewritten", id, a.AttemptID)
			}
			if a.Provenance.Origin == projectstate.OriginObserved {
				t.Fatalf("row %s attempt %s: nothing migrated may claim to have been observed", id, a.AttemptID)
			}
		}
		for _, r := range now.Reviews {
			if r.Provenance.Origin != projectstate.OriginBackfilled {
				t.Fatalf("row %s round %s: origin %q — the migration is evidence of nothing", id, r.RoundID, r.Provenance.Origin)
			}
			if r.Provenance.Generator != generatorID || r.Provenance.Basis == "" {
				t.Fatalf("row %s round %s: a backfilled round must name its generator and its basis", id, r.RoundID)
			}
		}
	}
}

// THE TASK-7 CARRY-OVER, as an invariant over the real state. A gate's recorded attempts
// numbered BELOW the lowest round on that gate keep their reconstructed revisions
// (constructionmanager.go splitAtLowestRound), so a minted round must never take a number
// a recorded gate attempt already holds — that would silently re-bind an existing
// revision to a round that did not judge it.
func TestMigrate_AMintedRoundNeverTakesARecordedGateAttemptsNumber(t *testing.T) {
	after := decode(t, realState(t))
	rounds := 0
	for id, row := range after.ActivityExecution {
		for _, r := range row.Reviews {
			rounds++
			for _, a := range row.Attempts {
				if a.Task == r.TaskID && int64(a.Attempt) == r.Round {
					t.Fatalf("row %s: round %s took attempt %s's number", id, r.RoundID, a.AttemptID)
				}
			}
		}
	}
	if rounds == 0 {
		t.Fatal("the real state minted no rounds at all; this invariant asserted nothing")
	}
}

// ---- the conversion ----------------------------------------------------------------

func TestMigrate_ASealedSlotThreadBecomesOneRoundPerDistinctRound(t *testing.T) {
	raw := legacyDocument(t, fixtureProject(), legacyRows())
	row := decode(t, migrated(t, raw)).ActivityExecution["architecture"]
	if len(row.Reviews) != 2 {
		t.Fatalf("want one round per distinct slot round (2), got %d: %+v", len(row.Reviews), row.Reviews)
	}
	// Task 6's four-part id and its +1 offset: the slot's round counter is 0-based and
	// the round ledger's is 1-based, so a dual-write would mint these very ids.
	want := []string{"architecture:architectureReview:system:2", "architecture:architectureReview:system:3"}
	for i, r := range row.Reviews {
		if r.RoundID != want[i] {
			t.Fatalf("round %d id %q, want %q", i, r.RoundID, want[i])
		}
		if r.Round != int64(i+2) {
			t.Fatalf("round %d number %d, want %d", i, r.Round, i+2)
		}
		if r.TaskID != "architectureReview" || r.Reviews != "architectureDraft" {
			t.Fatalf("round %d judges %q of %q", i, r.TaskID, r.Reviews)
		}
		if r.SubjectRef.Kind != projectstate.SubjectArtifact || r.SubjectRef.Ref != "system" {
			t.Fatalf("round %d subject %+v", i, r.SubjectRef)
		}
	}
	if n := len(row.Reviews[0].Thread); n != 2 {
		t.Fatalf("round 1 carries %d comments, want the slot round's 2", n)
	}
	if got := row.Reviews[0].Thread[0].ID; got != "r1c1" {
		t.Fatalf("the thread is not the slot's own comments verbatim: first id %q", got)
	}
	if row.Reviews[0].Outcome != projectstate.RoundSentBack {
		t.Fatalf("every round but the last is a send-back, got %q", row.Reviews[0].Outcome)
	}
	if row.Reviews[1].Outcome != projectstate.RoundPassed {
		t.Fatalf("the last round of a COMMITTED slot passed, got %q", row.Reviews[1].Outcome)
	}
}

func TestMigrate_ACritiqueBecomesAProductManagerVerdictOnItsRound(t *testing.T) {
	p := fixtureProject()
	slot := p.SystemDesign
	slot.CritiqueVerdict = projectstate.CritiqueVerdictRevise
	slot.CritiqueNotes = "the decomposition leaks a resource into a manager"
	p.SystemDesign = slot
	row := decode(t, migrated(t, legacyDocument(t, p, legacyRows()))).ActivityExecution["architecture"]
	last := row.Reviews[len(row.Reviews)-1]
	if len(last.Verdicts) != 1 {
		t.Fatalf("want one critique verdict on the last round, got %d", len(last.Verdicts))
	}
	v := last.Verdicts[0]
	if v.ReviewerRole != critiqueRoleProductManager || v.Verdict != projectstate.VerdictSendBack {
		t.Fatalf("critique verdict %+v", v)
	}
	if v.Summary != slot.CritiqueNotes {
		t.Fatalf("critique notes not carried verbatim: %q", v.Summary)
	}
}

func TestMigrate_ASendBackNoteBecomesARoundCarryingItsTextAndComments(t *testing.T) {
	rows := legacyRows()
	row := rows["C-thing"]
	rejected := migratedAt.Add(-72 * time.Hour)
	row.Attempts = append([]projectstate.TaskAttempt{{
		AttemptID: "C-thing:srsReview:1", Task: "srsReview", Phase: projectstate.MethodPhaseRequirements,
		Attempt: 1, Actor: projectstate.ActorHuman, EndedAt: &rejected, Outcome: projectstate.OutcomeRejected,
		Provenance: projectstate.AttemptProvenance{Origin: projectstate.OriginBackfilled, Basis: "code"},
	}}, projectstate.TaskAttempt{
		AttemptID: "C-thing:srsReview:2", Task: "srsReview", Phase: projectstate.MethodPhaseRequirements,
		Attempt: 2, Actor: projectstate.ActorAgent, EndedAt: &rejected, Outcome: projectstate.OutcomePassed,
		Provenance: projectstate.AttemptProvenance{Origin: projectstate.OriginBackfilled, Basis: "code"},
	})
	row.OperatorNotes = []projectstate.OperatorNote{{
		NoteID: "n1", Kind: projectstate.NoteSendBack, Gate: string(projectstate.MethodPhaseRequirements),
		Text: "the SRS does not state the volatility it encapsulates", RecordedAt: rejected,
		Comments: []projectstate.NoteComment{{JSONPath: "$.srs", Text: "name the volatility"}},
	}}
	rows["C-thing"] = row
	out := decode(t, migrated(t, legacyDocument(t, fixtureProject(), rows))).ActivityExecution["C-thing"]
	if len(out.Reviews) != 1 {
		t.Fatalf("want one round for the one send-back note, got %d", len(out.Reviews))
	}
	r := out.Reviews[0]
	if r.RoundID != projectstate.AttemptID("C-thing", "srsReview", 1) {
		t.Fatalf("a construction round is named with the three-part id, got %q", r.RoundID)
	}
	if r.Round != 1 {
		t.Fatalf("round number %d, want the matching gate attempt's 1", r.Round)
	}
	if r.Outcome != projectstate.RoundSentBack || len(r.Verdicts) != 1 {
		t.Fatalf("round %+v", r)
	}
	if r.Verdicts[0].Summary != row.OperatorNotes[0].Text {
		t.Fatalf("the note's text is not the verdict's summary: %q", r.Verdicts[0].Summary)
	}
	if len(r.Thread) != 1 || r.Thread[0].Text != "name the volatility" || r.Thread[0].Anchor != "$.srs" {
		t.Fatalf("the note's comments are not the round's thread: %+v", r.Thread)
	}
	// The ROUND takes the recorded rejection's number, so the reconstruction does not
	// keep a second, competing revision for the same review.
	if r.Round != 1 {
		t.Fatalf("round %d does not match the rejection it records", r.Round)
	}
}

func TestMigrate_StampsVersionAndTheCurrentLifecyclePin(t *testing.T) {
	after := decode(t, migrated(t, legacyDocument(t, fixtureProject(), legacyRows())))
	for id, row := range after.ActivityExecution {
		if row.Version != 1 {
			t.Fatalf("row %s version %d, want 1", id, row.Version)
		}
		if row.Pin == nil {
			t.Fatalf("row %s carries no lifecycle pin", id)
		}
		if row.Pin.AssetsVersion != methodassets.Version() {
			t.Fatalf("row %s pinned to %q, want the current release %q", id, row.Pin.AssetsVersion, methodassets.Version())
		}
	}
	if got := after.ActivityExecution["architecture"].Pin.TypeKey; got != "architecture" {
		t.Fatalf("the pin names the row's classified type, got %q", got)
	}
}

func TestMigrate_DropsTheDerivedMembersAndRenamesTheMap(t *testing.T) {
	out := migrated(t, legacyDocument(t, fixtureProject(), legacyRows()))
	ms, err := members(mustCompact(t, out))
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if valueOf(ms, legacyMember) != nil {
		t.Fatal("the legacy member survived the rename")
	}
	value := valueOf(ms, executionMember)
	if value == nil {
		t.Fatal("the execution member is missing")
	}
	// Asked of each ROW's own members, not of the member's bytes: "kind" also names a
	// SubjectRef's and an EvidenceRef's discriminator, and a substring search would call
	// those the legacy roll-up.
	var rows map[string]map[string]json.RawMessage
	if err := json.Unmarshal(value, &rows); err != nil {
		t.Fatalf("read the execution member: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the execution member holds no rows")
	}
	for id, row := range rows {
		for _, dropped := range derivedMembers {
			if _, held := row[dropped]; held {
				t.Fatalf("row %s still stores the derived member %q", id, dropped)
			}
		}
	}
}

func mustCompact(t *testing.T, raw []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		t.Fatalf("compact: %v", err)
	}
	return out.Bytes()
}

// The splice touches only the two members this tool owns (backfill-attempts'
// confirmOnlyConstructionMoved, over a member set of two).
func TestMigrate_OnlyTheOwnedMembersMoved(t *testing.T) {
	raw := legacyDocument(t, fixtureProject(), legacyRows())
	was, err := members(mustCompact(t, raw))
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	now, err := members(mustCompact(t, migrated(t, raw)))
	if err != nil {
		t.Fatalf("members: %v", err)
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
		t.Fatalf("the member set changed: %d → %d", len(a), len(b))
	}
	for i := range a {
		if a[i].key != b[i].key || !bytes.Equal(a[i].value, b[i].value) {
			t.Fatalf("member %s moved", a[i].key)
		}
	}
	// The execution member lands exactly where the legacy one stood.
	if mustCompact(t, migrated(t, raw))[0] != '{' {
		t.Fatal("not an object")
	}
}

func TestMigrate_ASecondRunIsANoOp(t *testing.T) {
	once := migrated(t, legacyDocument(t, fixtureProject(), legacyRows()))
	twice, report, err := migrate(once, migratedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !bytes.Equal(once, twice) {
		t.Fatal("a second run rewrote the document; the migration is not idempotent")
	}
	if report.Rows != 0 {
		t.Fatalf("the second run claims %d rows to migrate", report.Rows)
	}
}

func TestMigrate_RefusesALegacyMemberTheTypedReaderCannotCarry(t *testing.T) {
	// A member the typed legacy row does not model: reading through it would drop the
	// field silently, which is the one thing a migration may not do.
	raw := legacyDocumentWith(t, fixtureProject(), json.RawMessage(
		`{"C-thing":{"activityID":"C-thing","phase":0,"somethingNobodyModelled":1}}`))
	if _, _, err := migrate(raw, migratedAt); err == nil || !strings.Contains(err.Error(), "somethingNobodyModelled") {
		t.Fatalf("want a refusal naming the dropped member, got %v", err)
	}
}

func TestMigrate_RefusesADocumentItCannotReproduce(t *testing.T) {
	raw := append([]byte(nil), legacyDocument(t, fixtureProject(), legacyRows())...)
	raw = bytes.Replace(raw, []byte("\n  \"version\""), []byte("\n\t\"version\""), 1)
	if _, _, err := migrate(raw, migratedAt); err == nil {
		t.Fatal("want a refusal: re-indenting does not reproduce the document")
	}
}

// ---- the report ---------------------------------------------------------------------

// TestTheCommittedStateIsMigrated: the tool and the state it produced are each other's
// evidence, so the committed document must BE the tool's output — no legacy member, every
// row versioned and pinned, and re-running the migration over it a no-op.
func TestTheCommittedStateIsMigrated(t *testing.T) {
	raw := realState(t)
	body, _, err := compactDocument(raw)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	ms, err := members(body)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if valueOf(ms, legacyMember) != nil {
		t.Fatalf(".%s is still committed — run the migration", legacyMember)
	}
	p := decode(t, raw)
	for id, row := range p.ActivityExecution {
		if row.Version < 1 {
			t.Fatalf("row %s is committed at version %d", id, row.Version)
		}
		if row.Pin == nil {
			t.Fatalf("row %s is committed without a lifecycle pin", id)
		}
	}
	again, rep, err := migrate(raw, migratedAt)
	if err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if !bytes.Equal(again, raw) || rep.Rows != 0 {
		t.Fatalf("re-running the migration over the committed state would rewrite %d row(s)", rep.Rows)
	}
}

func TestReport_NamesEveryRowAndEveryBackfilledRound(t *testing.T) {
	_, report, err := migrate(preMigrationState(t), migratedAt)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if report.Rows == 0 || report.Rows != len(report.Lines) {
		t.Fatalf("the report names %d of %d rows", len(report.Lines), report.Rows)
	}
	if report.Rounds == 0 {
		t.Fatal("the real state's sealed design slots must yield rounds")
	}
	if len(report.Dropped) == 0 {
		t.Fatal("the report must name the derived members it dropped")
	}
}
