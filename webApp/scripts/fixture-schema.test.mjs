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
  buildFixtureSchema,
  compileFixtureValidator,
  loadOas,
  validateFixtureTree,
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
// (the method-assets lifecycles' `gate`). Service and frontend agree on all five; the
// short profiles (testing/deployment/documentation) name a subset of the same phases, so
// one table answers for every profile a fixture carries.
const GATE_TASK = {
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
    const rows = doc_.ops?.systemDesignGetProject?.result?.activityExecution;
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
        const attempts = (row.attempts ?? []).filter((a) => a.task === gate);
        const latest = attempts.at(-1);
        if (latest?.outcome !== 'passed') {
          offences.push(
            `${where}: ${phase.Phase} is Completed but its gate ${gate ?? '(unknown)'} has ${attempts.length === 0 ? 'no attempt' : `latest outcome ${String(latest?.outcome)}`}`
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
});

const ok = (ops) => validate({ route: '/', ops });

void test('it accepts a result, an error, or pending', () => {
  assert.equal(ok({ compositionGetCapabilities: { result: { operations: false } } }), true);
  assert.equal(ok({ systemDesignGetProject: { error: { status: 500, message: 'down' } } }), true);
  assert.equal(ok({ systemDesignGetProject: { pending: true } }), true);
  // A void (204) op's result is never read.
  assert.equal(ok({ constructionSubmitPhaseDecision: { result: null } }), true);
});

void test('it rejects an op the transport does not have', () => {
  assert.equal(ok({ systemDesignGetProjct: { pending: true } }), false);
});

void test('it rejects a result that drifted from the contract', () => {
  assert.equal(ok({ compositionGetCapabilities: { result: { operations: 'yes' } } }), false);
  assert.equal(ok({ systemDesignListProjects: { result: [{ ProjectID: 'x' }] } }), false);
  assert.equal(ok({ compositionGetUserinfo: { result: { kind: 'user' } } }), false);
});

void test('it rejects an ambiguous or malformed answer', () => {
  assert.equal(
    ok({ systemDesignGetProject: { result: {}, error: { status: 500 } } }),
    false,
    'a result and an error at once'
  );
  assert.equal(ok({ systemDesignGetProject: { pending: false } }), false);
  assert.equal(ok({ systemDesignGetProject: { error: { message: 'no status' } } }), false);
  assert.equal(ok({ systemDesignGetProject: { error: { status: 200 } } }), false);
  assert.equal(ok({ systemDesignGetProject: {} }), false);
});

void test('it rejects a fixture without an absolute route', () => {
  assert.equal(validate({ route: 'project/x', ops: {} }), false);
  assert.equal(validate({ ops: {} }), false);
});

void test('the activity-experience design fixtures are recorded, valid, and answer the one read', () => {
  const { files, errors } = validateFixtureTree(DESIGN_FIXTURES, { validate });
  assert.deepEqual(errors, []);
  const states = files
    .filter((f) => f.includes(join(SURFACE, 'activity-experience')))
    .map((f) => f.slice(f.lastIndexOf('/') + 1))
    .sort();
  assert.deepEqual(states, [
    'deployment-linear.json',
    'done.json',
    'not-started.json',
    'service-fork-sent-back.json',
  ]);
  for (const f of files.filter((p) => p.includes(join(SURFACE, 'activity-experience')))) {
    const view = JSON.parse(readFileSync(f, 'utf8')).ops.constructionQueryActivityView.result;
    const ids = new Set(view.tasks.map((t) => t.id));
    for (const t of view.tasks) {
      for (const dep of t.dependsOn) assert.ok(ids.has(dep), `${f}: ${t.id} depends on unknown ${dep}`);
      if (t.reviews !== undefined) assert.ok(ids.has(t.reviews), `${f}: ${t.id} reviews unknown ${t.reviews}`);
    }
    assert.equal(view.phases.reduce((sum, p) => sum + p.weight, 0), 100, `${f}: weights`);
  }
});

// A REVISION READ FROM A PERSISTED ROUND IS NOT A RECONSTRUCTION, and the fixtures have
// to hold both or the preview only ever shows one of them (stage-3 tasks 7 and 8). The
// schema cannot tell them apart — every new member is optional, because a pre-ledger row
// genuinely has none of them — so the distinction is asserted here, against the
// derivation's own rules in constructionmanager.go:
//
//   - the round's facts travel together: a revision with any of them carries the round
//     number too, and a round a run wrote is `observed`, never `backfilled`;
//   - a DECIDED round says who decided it and when; a PENDING one says neither and cites
//     NO attempt, because the rail writes the gate attempt only when the round is decided
//     (which is why every gate a human is looking at right now has a round and no attempt);
//   - a reconstruction offers `note` and `comments` and nothing a round owns;
//   - a dispatch revision has no round at all.
const ROUND_ONLY = ['verdicts', 'thread', 'reviewers', 'subjectRef', 'decidedBy', 'decidedAt'];

void test('the activity-experience fixtures hold a persisted round AND a reconstruction', () => {
  const { files } = validateFixtureTree(DESIGN_FIXTURES, { validate });
  const offences = [];
  let persisted = 0;
  let reconstructed = 0;
  for (const f of files.filter((p) => p.includes(join(SURFACE, 'activity-experience')))) {
    const view = JSON.parse(readFileSync(f, 'utf8')).ops.constructionQueryActivityView.result;
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
          if (rev.provenance !== 'backfilled') offences.push(`${where}: a reconstruction is backfilled, not ${rev.provenance}`);
          continue;
        }
        persisted += 1;
        if (rev.provenance !== 'observed') offences.push(`${where}: a round a run wrote is observed, not ${rev.provenance}`);
        for (const k of ['verdicts', 'thread', 'reviewers', 'subjectRef']) {
          if (rev[k] === undefined) offences.push(`${where}: a persisted round carries ${k}`);
        }
        const decided = rev.outcome === 'passed' || rev.outcome === 'sentBack';
        if (decided !== (rev.decidedBy !== undefined)) offences.push(`${where}: outcome ${rev.outcome} vs decidedBy ${rev.decidedBy}`);
        if (decided !== (rev.decidedAt !== undefined)) offences.push(`${where}: outcome ${rev.outcome} vs decidedAt ${rev.decidedAt}`);
        if (!decided && rev.attemptIds.length > 0) {
          offences.push(`${where}: an undecided round has no gate attempt yet, but cites ${rev.attemptIds}`);
        }
        if (rev.outcome === 'sentBack' && !rev.verdicts.some((v) => v.verdict === 'sendBack')) {
          offences.push(`${where}: a sentBack round was sent back by someone`);
        }
      }
    }
  }
  assert.deepEqual(offences, []);
  assert.ok(persisted > 0, 'no fixture exercises a persisted round; the new members render nowhere');
  assert.ok(reconstructed > 0, 'no fixture exercises a pre-ledger row; the reconstruction renders nowhere');
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
