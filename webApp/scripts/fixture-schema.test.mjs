/**
 * The OAS-generated fixture schema (scripts/fixture-schema.mjs): every fixture
 * file on disk passes it, and it rejects the drift it exists to catch.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { BUNDLE_MARKERS } from './bundle-markers.mjs';
import {
  SURFACE,
  VIEW_OP,
  buildFixtureSchema,
  compileFixtureValidator,
  loadOas,
  validateFixtureTree,
  viewKinds,
} from './fixture-schema.mjs';
import { opBindings } from './op-bindings.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const doc = loadOas();
const validate = compileFixtureValidator(doc);

// The test-local fixtures the uitests build the preview over (design §2′.5 item
// 4), and the recorded-design location the U-SPA-web-client Design phase will fill.
const UITESTS_FIXTURES = join(here, '..', '..', 'uitests', 'preview-fixtures');
const DESIGN_FIXTURES = join(here, '..', 'preview', 'fixtures');

void test('every uitests fixture passes the OAS-generated schema (and there are some)', () => {
  const { files, errors } = validateFixtureTree(UITESTS_FIXTURES, { validate });
  assert.deepEqual(errors, []);
  assert.ok(files.length >= 3, `expected the uitests fixture states, found ${files.length}`);
});

// THE SCHEMA SAYS A FIXTURE IS WELL-SHAPED; IT CANNOT SAY IT IS SERVABLE. A
// construction row carries members the server DERIVES rather than stores (stage-3 task 4,
// spec §5.3): `CurrentPhase` is the first phase of the row's own resolved set that is not
// complete, and `hasBuildEvidence` is whether that set materialized at all. A fixture that
// hand-edits one without the other asserts a state no server can serve, and every preview
// spec over it then tests a lie — which is exactly what happened when the map key was
// renamed by hand and two rows kept a CurrentPhase the new derivation no longer produces.
// So the fixtures are checked against the SERVER'S OWN RULES, not merely against the
// shape, and a future hand-edit fails here instead of in a preview nobody re-recorded.
const firstIncompletePhase = (phases) =>
  (phases ?? []).find((p) => !p.Completed)?.Phase ?? '';

// The GATE task of each lifecycle phase, mirroring projectstate's own lifecyclePhaseTasks
// (the method-assets lifecycles' `gate`). It is keyed on the PHASE ID alone, exactly as
// the Go map is, and for the same reason: the eleven construction lifecycles share the
// canonical five phase ids and name the same gate under each, while the three DESIGN
// lifecycles use phase ids that collide with none of them. Service and frontend agree on
// all five construction phases; the short profiles (testing/deployment/documentation) name
// a subset of them.
//
// The six design-prefix rows below were missing until a fixture captured from the live
// server carried them: Requirements, Architecture and Project Design became activities in
// stage 2, and their rows completed phases this table could not name — which read here as
// "its gate (unknown) has no attempt". A table that cannot name a phase must not be read
// as a table that condemns it, so the unknown-phase case is now an offence in its own
// right rather than a silent mis-accusation.
const GATE_TASK = {
  mission: 'missionReview',
  glossary: 'glossaryReview',
  volatilities: 'volatilitiesReview',
  coreUseCases: 'coreUseCasesReview',
  architecture: 'architectureReview',
  sdp: 'sdpReview',
  requirements: 'srsReview',
  detailed_design: 'designReview',
  test_plan: 'stpReview',
  construction: 'codeReview',
  integration: 'testing',
};

void test('every construction fixture row is one the server could serve', () => {
  const { files } = validateFixtureTree(UITESTS_FIXTURES, { validate });
  const offences = [];
  let rowsChecked = 0;
  for (const file of files) {
    const doc_ = JSON.parse(readFileSync(file, 'utf8'));
    // The head-state read is the merged project view's `summary` kind (stage 4a):
    // ops.deliveryQueryProjectView.summary.result IS a DeliveryProjectView, and the
    // project state is its `summary` member.
    const rows =
      doc_.ops?.deliveryQueryProjectView?.summary?.result?.summary?.activityExecution;
    if (!rows) continue;
    for (const [id, row] of Object.entries(rows)) {
      rowsChecked += 1;
      const where = `${file.split('/').slice(-2).join('/')} ${id}`;
      // An unclassified row asserts nothing: the server leaves every one of these at its
      // zero value rather than deriving from a profile it refused to pick.
      const wantPhase = row.classified ? firstIncompletePhase(row.Phases) : '';
      if ((row.CurrentPhase ?? '') !== wantPhase) {
        offences.push(`${where}: CurrentPhase ${JSON.stringify(row.CurrentPhase)}, derived ${JSON.stringify(wantPhase)}`);
      }
      // `hasBuildEvidence` is `len(resolved) > 0`, and `resolved` is nil for an empty
      // ledger (projectstate.ResolvePhaseCompletions returns nil when there are no
      // attempts). So for a classified row it is exactly "the ledger is non-empty" — and
      // the emitted `Phases` must agree, being the same resolved set.
      const wantEvidence = row.classified ? (row.attempts ?? []).length > 0 : false;
      if (row.hasBuildEvidence !== wantEvidence) {
        offences.push(`${where}: hasBuildEvidence ${row.hasBuildEvidence}, derived ${wantEvidence} from ${(row.attempts ?? []).length} attempts`);
      }
      if (((row.Phases ?? []).length > 0) !== wantEvidence) {
        offences.push(`${where}: ${(row.Phases ?? []).length} emitted phases over ${(row.attempts ?? []).length} attempts — the phase set IS the resolved ledger`);
      }
      // THE LEDGER IS THE ONLY THING THAT CAN COMPLETE A PHASE. App A's binary exit
      // criterion, as projectstate.phaseCompleteFromAttempts implements it: a phase is
      // complete iff its GATE task's LATEST attempt passed. A work attempt is not a gate,
      // and a fixture that marks a phase complete without one is asserting a completion
      // no server could derive — which is how `built-surface-link` came to claim an
      // integrated surface over a single `construction` attempt.
      for (const phase of row.Phases ?? []) {
        if (!phase.Completed) continue;
        const gate = GATE_TASK[phase.Phase];
        if (gate === undefined) {
          offences.push(`${where}: ${phase.Phase} is a lifecycle phase GATE_TASK cannot name; add its gate from methodassets.Lifecycles()`);
          continue;
        }
        const attempts = (row.attempts ?? []).filter((a) => a.task === gate);
        const latest = attempts.at(-1);
        if (latest?.outcome !== 'passed') {
          offences.push(
            `${where}: ${phase.Phase} is Completed but its gate ${gate} has ${attempts.length === 0 ? 'no attempt' : `latest outcome ${String(latest?.outcome)}`}`
          );
        }
      }
    }
  }
  assert.deepEqual(offences, []);
  assert.ok(rowsChecked > 0, 'no construction rows were checked; this test would pass vacuously');
});

void test('every recorded design fixture passes too, when there are any', () => {
  if (!existsSync(join(DESIGN_FIXTURES, SURFACE))) return;
  assert.deepEqual(validateFixtureTree(DESIGN_FIXTURES, { validate }).errors, []);
});

void test('the schema keys ops by exactly the OpsClient OpIds, composition routes included', () => {
  const schemaOps = Object.keys(buildFixtureSchema(doc).properties.ops.properties).sort();
  assert.deepEqual(schemaOps, Object.keys(opBindings(doc)).sort());
  assert.ok(schemaOps.includes('compositionGetUserinfo'));
  // The whole roster, MEASURED off the bindings rather than typed here: stage 4a
  // took it from 51 ops to 23, and a floor written by hand would be the one thing
  // in this file that does not move when the contract does.
  assert.equal(schemaOps.filter((o) => o.startsWith('delivery')).length, 12);
  assert.equal(schemaOps.length, 23);
});

void test(`${VIEW_OP} is keyed by the OAS view kinds, not by one answer`, () => {
  // The one selector-keyed op (src/api/fixtureOps.ts): it absorbed thirteen
  // per-rail readers, so a single answer under its op id could serve only one of
  // the kinds a screen reads and the rest would silently overwrite each other.
  const entry = buildFixtureSchema(doc).properties.ops.properties[VIEW_OP];
  assert.deepEqual(Object.keys(entry.properties), viewKinds(doc));
  assert.deepEqual(viewKinds(doc), [
    'summary',
    'projects',
    'session',
    'pump',
    'designHealth',
    'episodes',
    'timeline',
  ]);
  assert.equal(entry.additionalProperties, false, 'an unknown kind is refused');
  assert.equal(entry.oneOf, undefined, 'it is not a bare answer');
});

const ok = (ops) => validate({ route: '/', ops });

void test('it accepts a result, an error, or pending', () => {
  assert.equal(ok({ compositionGetCapabilities: { result: { operations: false } } }), true);
  assert.equal(
    ok({ deliveryQueryActivityView: { error: { status: 500, message: 'down' } } }),
    true
  );
  assert.equal(ok({ deliveryQueryActivityView: { pending: true } }), true);
  // A void (204) op's result is never read.
  assert.equal(ok({ deliverySubmitReviewDecision: { result: null } }), true);
});

void test('the view op takes an answer per kind, and each kind may answer differently', () => {
  assert.equal(
    ok({
      deliveryQueryProjectView: {
        summary: { pending: true },
        projects: { result: { kind: 'projects', projects: [] } },
        timeline: { error: { status: 503 } },
      },
    }),
    true
  );
  // A bare answer under the op id: the transport would never resolve it, because
  // it selects by the kind the call carries.
  assert.equal(ok({ deliveryQueryProjectView: { pending: true } }), false);
  assert.equal(ok({ deliveryQueryProjectView: { result: { kind: 'summary' } } }), false);
  assert.equal(ok({ deliveryQueryProjectView: { sumary: { pending: true } } }), false);
  // `kind` is required of a DeliveryProjectView: the body says which view it is.
  assert.equal(ok({ deliveryQueryProjectView: { projects: { result: { projects: [] } } } }), false);
});

void test('it rejects an op the transport does not have', () => {
  assert.equal(ok({ deliveryQueryActivityViw: { pending: true } }), false);
});

void test('it rejects a result that drifted from the contract', () => {
  assert.equal(ok({ compositionGetCapabilities: { result: { operations: 'yes' } } }), false);
  assert.equal(
    ok({ deliveryQueryProjectView: { projects: { result: [{ ProjectID: 'x' }] } } }),
    false
  );
  assert.equal(ok({ compositionGetUserinfo: { result: { kind: 'user' } } }), false);
});

void test('it rejects an ambiguous or malformed answer', () => {
  assert.equal(
    ok({ deliveryQueryActivityView: { result: {}, error: { status: 500 } } }),
    false,
    'a result and an error at once'
  );
  assert.equal(ok({ deliveryQueryActivityView: { pending: false } }), false);
  assert.equal(ok({ deliveryQueryActivityView: { error: { message: 'no status' } } }), false);
  assert.equal(ok({ deliveryQueryActivityView: { error: { status: 200 } } }), false);
  assert.equal(ok({ deliveryQueryActivityView: {} }), false);
  // The same three refusals, one level deeper, under a kind.
  assert.equal(
    ok({ deliveryQueryProjectView: { summary: { result: {}, error: { status: 500 } } } }),
    false
  );
  assert.equal(ok({ deliveryQueryProjectView: { summary: { pending: false } } }), false);
  assert.equal(ok({ deliveryQueryProjectView: { summary: {} } }), false);
});

void test('it rejects a fixture without an absolute route', () => {
  assert.equal(validate({ route: 'project/x', ops: {} }), false);
  assert.equal(validate({ ops: {} }), false);
});

// THE ACTIVITY-EXPERIENCE FIXTURES LIVE IN THE UITESTS TREE, because that is the only one
// the preview build is pointed at (playwright.config.ts's ARCHISTRATOR_PREVIEW_FIXTURES);
// a scenario recorded under preview/fixtures is a scenario the suite cannot open. ONE
// smoke fixture stays behind so the recorded-design location still validates something
// real and this file's DESIGN_FIXTURES clauses are not vacuous. Every clause below walks
// BOTH trees, so a fixture is held to the same rules wherever it was recorded.
const activityViews = (root) => {
  const { files, errors } = validateFixtureTree(root, { validate });
  assert.deepEqual(errors, [], `${root}: fixtures must validate before they are read`);
  return files
    .filter((p) => p.includes(join(SURFACE, 'activity-experience')))
    .map((p) => [p, JSON.parse(readFileSync(p, 'utf8'))])
    .map(([p, doc_]) => [p, doc_.ops?.deliveryQueryActivityView?.result])
    .filter(([, view]) => view !== undefined);
};

const FIXTURE_ROOTS = [UITESTS_FIXTURES, DESIGN_FIXTURES];

void test('the activity-experience fixtures are recorded where the preview can open them', () => {
  const states = (root) => activityViews(root).map(([p]) => p.slice(p.lastIndexOf('/') + 1)).sort();
  assert.deepEqual(states(UITESTS_FIXTURES), [
    'architecture-round.json',
    'deployment-linear.json',
    'done.json',
    'failed.json',
    'project-design-m0-history.json',
    'project-design-m0.json',
    'requirements-backfilled.json',
    'review-set-error.json',
    'service-fork-sent-back.json',
    'sub-attempts.json',
  ]);
  assert.deepEqual(states(DESIGN_FIXTURES), ['not-started.json'], 'the one smoke fixture');
});

void test('the activity-experience fixtures answer the one read, with a closed task DAG', () => {
  for (const root of FIXTURE_ROOTS) {
    for (const [f, view] of activityViews(root)) {
      const ids = new Set(view.tasks.map((t) => t.id));
      for (const t of view.tasks) {
        for (const dep of t.dependsOn) assert.ok(ids.has(dep), `${f}: ${t.id} depends on unknown ${dep}`);
        if (t.reviews !== undefined) assert.ok(ids.has(t.reviews), `${f}: ${t.id} reviews unknown ${t.reviews}`);
      }
      assert.equal(view.phases.reduce((sum, p) => sum + p.weight, 0), 100, `${f}: weights`);
    }
  }
});

// THE SCHEMA SAYS A VIEW IS WELL-SHAPED; IT CANNOT SAY QueryActivityView COULD HAVE
// DERIVED IT. Two of the view's members are derived from a third and are not free to
// disagree with it: a lifecycle phase's `completed` is the state of its GATE task
// (`constructionmanager.go`'s deriveTaskViews, and the contract's own words — "True iff
// the gate task's state is passed"), and a revision's `n` is 1-based and ascending, with a
// dispatch task and the review that judges it sharing numbers. A fixture that completes a
// phase whose gate did not pass, or that numbers revisions from 0, asserts a view no
// server emits — and every spec written over it then tests a lie, which is exactly the
// failure the construction-row clause above already caught once.
void test('every activity-experience fixture is a view the server could derive', () => {
  for (const root of FIXTURE_ROOTS) {
    for (const [path, view] of activityViews(root)) {
      const byId = new Map(view.tasks.map((t) => [t.id, t]));
      for (const phase of view.phases) {
        const gate = byId.get(phase.gateTaskId);
        assert.ok(gate !== undefined, `${path}: phase ${phase.id} names a gate task that is not in tasks[]`);
        assert.equal(
          phase.completed,
          gate.state === 'passed',
          `${path}: phase ${phase.id} completion disagrees with its gate task state (deriveTaskViews derives one from the other)`
        );
      }
      for (const task of view.tasks) {
        const ns = task.revisions.map((r) => r.n);
        assert.deepEqual(ns, [...ns].sort((a, b) => a - b), `${path}: ${task.id} revisions must be oldest first`);
        assert.ok(ns.every((n) => n >= 1), `${path}: ${task.id} revisions are 1-based`);
        // A dispatch task and its reviewer share revision numbers: revision n IS the n-th
        // work that reached the gate and the n-th gate attempt that judged it. A review
        // cannot judge work that was never produced.
        if (task.reviews !== undefined) {
          const work = byId.get(task.reviews).revisions.map((r) => r.n);
          for (const n of ns) {
            assert.ok(work.includes(n), `${path}: ${task.id} judges revision ${n}, which ${task.reviews} never produced`);
          }
        }
      }
    }
  }
});

// A REVISION READ FROM A PERSISTED ROUND IS NOT A RECONSTRUCTION, and the fixtures have
// to hold both or the preview only ever shows one of them (stage-3 tasks 7 and 8). The
// schema cannot tell them apart — every new member is optional, because a pre-ledger row
// genuinely has none of them — so the distinction is asserted here, against the
// derivation's own rules in constructionmanager.go:
//
//   - the round's facts travel together: a revision with any of them carries the round
//     number and the subject (the store refuses a round with an empty one), and a round a
//     run wrote is `observed`, never `backfilled`;
//   - a round is OPENED before it is judged, and the store stamps openedAt there, so a
//     round-backed revision ALWAYS has a startedAt; a DECIDED round says who decided it
//     and when, and its endedAt IS that decidedAt;
//   - a PENDING round says neither and cites NO attempt, because the rail writes the gate
//     attempt only when the round is decided (which is why every gate a human is looking
//     at right now has a round and no attempt);
//   - a reconstruction offers `note` and `comments` and nothing a round owns, and its
//     provenance is the WORST origin among the attempts behind it: `backfilled` when they
//     were rebuilt from evidence recorded elsewhere, `synthesized` when any was fabricated;
//   - a dispatch revision has no round at all.
//
// `thread` and `verdicts` are NOT required of every round: both are omitempty on the wire,
// so a round nobody has commented on carries no thread at all rather than an empty array.
// A DECIDED round does carry a verdict — the rails append the deciding reviewer's before
// they stamp the terminal.
const ROUND_ONLY = ['verdicts', 'thread', 'reviewers', 'subjectRef', 'decidedBy', 'decidedAt'];

void test('the activity-experience fixtures hold a persisted round AND a reconstruction', () => {
  const offences = [];
  let persisted = 0;
  let reconstructed = 0;
  for (const [f, view] of FIXTURE_ROOTS.flatMap(activityViews)) {
    for (const task of view.tasks) {
      for (const rev of task.revisions) {
        const where = `${f.slice(f.lastIndexOf('/') + 1)} ${task.id}#${rev.n}`;
        const round = ROUND_ONLY.some((k) => rev[k] !== undefined);
        if (task.kind === 'dispatch') {
          if (round || rev.round !== undefined) offences.push(`${where}: a dispatch revision has no round`);
          continue;
        }
        if (rev.round === undefined) offences.push(`${where}: a review revision carries the round it is`);
        if (!round) {
          reconstructed += 1;
          if (rev.provenance === 'observed') {
            offences.push(`${where}: a reconstruction is backfilled or synthesized, not observed`);
          }
          continue;
        }
        persisted += 1;
        if (rev.provenance !== 'observed') offences.push(`${where}: a round a run wrote is observed, not ${rev.provenance}`);
        for (const k of ['subjectRef', 'reviewers']) {
          if (rev[k] === undefined) offences.push(`${where}: a persisted round carries ${k}`);
        }
        if (rev.startedAt === undefined) offences.push(`${where}: a round is opened before it is judged, so it has a startedAt`);
        const decided = rev.outcome === 'passed' || rev.outcome === 'sentBack';
        if (decided !== (rev.decidedBy !== undefined)) offences.push(`${where}: outcome ${rev.outcome} vs decidedBy ${rev.decidedBy}`);
        if (decided !== (rev.decidedAt !== undefined)) offences.push(`${where}: outcome ${rev.outcome} vs decidedAt ${rev.decidedAt}`);
        if ((rev.endedAt ?? undefined) !== rev.decidedAt) {
          offences.push(`${where}: a round ends when it is decided — endedAt ${rev.endedAt} vs decidedAt ${rev.decidedAt}`);
        }
        if (!decided && rev.attemptIds.length > 0) {
          offences.push(`${where}: an undecided round has no gate attempt yet, but cites ${rev.attemptIds}`);
        }
        if (decided && (rev.verdicts ?? []).length === 0) {
          offences.push(`${where}: a decided round carries the deciding reviewer's verdict`);
        }
        if (rev.outcome === 'sentBack' && !(rev.verdicts ?? []).some((v) => v.verdict === 'sendBack')) {
          offences.push(`${where}: a sentBack round was sent back by someone`);
        }
        // `note` is the send-back's prose and nothing else: a round that PASSED over one
        // reviewer's dissent still holds that dissent in `verdicts`, and rendering it as
        // the revision's note would say the work was returned when it was not.
        if ((rev.note !== undefined) !== (rev.outcome === 'sentBack')) {
          offences.push(`${where}: outcome ${rev.outcome} vs note ${JSON.stringify(rev.note)}`);
        }
        if ((rev.thread ?? []).length !== rev.commentCount || (rev.comments ?? []).length !== rev.commentCount) {
          offences.push(`${where}: comments are the thread's flat projection — ${(rev.thread ?? []).length} thread, ${(rev.comments ?? []).length} comments, count ${rev.commentCount}`);
        }
      }
    }
  }
  assert.deepEqual(offences, []);
  assert.ok(persisted > 0, 'no fixture exercises a persisted round; the new members render nowhere');
  assert.ok(reconstructed > 0, 'no fixture exercises a pre-ledger row; the reconstruction renders nowhere');
});

// A REVIEW REVISION IS EITHER A PROJECTION OR A RECONSTRUCTION, AND NEVER BOTH. The test
// above reads that split from the round's own members; this one reads it from the other
// end — from `provenance`, which is the word the screen shows the reader — so a fixture
// cannot say "rebuilt from a pre-ledger row" while carrying facts only a live round has.
// schema.ts says it verbatim: `reviewers` is "Empty on a reconstructed revision: a
// pre-ledger row recorded who reviewed nowhere", `decidedAt` and `subjectRef` are
// "Omitted ... on a reconstructed revision". And every revision IS its attempts — the
// attemptIds list is what a revision is made of — with exactly one exception, an OPEN
// round, for which the rail has not written the gate attempt yet.
void test('a revision provenance matches what it carries', () => {
  let reviewRevisions = 0;
  for (const [path, view] of FIXTURE_ROOTS.flatMap(activityViews)) {
    for (const task of view.tasks) {
      for (const rev of task.revisions) {
        const where = `${path.slice(path.lastIndexOf('/') + 1)}: ${task.id} rev ${rev.n}`;
        const openRound = rev.round !== undefined && rev.outcome === 'awaitingHuman';
        if (!openRound) {
          assert.ok(rev.attemptIds.length >= 1, `${where}: a revision is its attempts, and cites none`);
        } else {
          assert.deepEqual(rev.attemptIds, [], `${where}: an open round has no gate attempt yet`);
        }
        if (task.kind !== 'review') continue;
        reviewRevisions += 1;
        if (rev.provenance === 'backfilled' || rev.provenance === 'synthesized') {
          assert.deepEqual(rev.reviewers ?? [], [], `${where}: a reconstructed revision has an empty roster`);
          assert.equal(rev.decidedAt, undefined, `${where}: a reconstructed revision carries no decision stamp`);
          assert.equal(rev.subjectRef, undefined, `${where}: a reconstructed revision names no subject`);
        }
        // An OBSERVED review revision is a round, and a round that reached a terminal was
        // decided by someone at a moment the store stamped. `running` and `awaitingHuman`
        // are the two outcomes that mean it has not: the round is open.
        if (rev.provenance === 'observed' && rev.outcome !== 'running' && rev.outcome !== 'awaitingHuman') {
          assert.ok(rev.decidedAt !== undefined, `${where}: a decided, observed revision carries decidedAt`);
        }
      }
    }
  }
  assert.ok(reviewRevisions > 0, 'no review revision was checked; this test would pass vacuously');
});

void test('a fixture whose text carries a bundle marker is refused', () => {
  // Fixtures are bundled into the preview; a marker in their text would keep the
  // preview bundle check green with the marked code gone (P1 mutant M2c).
  for (const marker of BUNDLE_MARKERS) {
    const root = mkdtempSync(join(tmpdir(), 'preview-fixtures-'));
    mkdirSync(join(root, SURFACE, 'landing'), { recursive: true });
    writeFileSync(
      join(root, SURFACE, 'landing', 'resting.json'),
      JSON.stringify({ route: '/', note: `mentions ${marker}`, ops: {} })
    );
    const { errors } = validateFixtureTree(root, { validate });
    assert.equal(errors.length, 1, marker);
    assert.match(errors[0], /bundle marker/);
  }
});
