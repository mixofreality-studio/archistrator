/**
 * tests/meta/project-creation-guard.spec.ts: no spec but the seed step can create
 * a real project (fix-G review ruling).
 *
 * A META-check: it drives no page. It pins two things:
 *
 *   (a) the guard's rule (guardLetsThrough): GET, HEAD and the one merged READ pass;
 *       every other write is aborted unless the spec named it; and creating a
 *       project, or any pump write, is aborted WHATEVER a spec names;
 *   (b) the suite's SOURCE, statically: openSharedProject never reaches the create
 *       flow; the real create flow runs only in the seed step, or under a stub that
 *       answers start-project in the browser; and every spec that opens a project
 *       runs under the shared dispatch guard.
 */
import { test, expect } from '../support/dispatchGuard.js';
import { readdirSync, readFileSync } from 'node:fs';
import { join, relative } from 'node:path';
import { guardLetsThrough, LIVE_DRAFTING_WRITES } from '../support/dispatchGuard.js';

const TESTS = join(import.meta.dirname, '..');
const API = 'http://localhost:5173/api/v1';
const SEED = 'seed/shared-project.setup.ts';

/** Every .ts under tests/, as a path relative to tests/. */
function sources(dir = TESTS): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name);
    return e.isDirectory() ? sources(p) : e.name.endsWith('.ts') ? [relative(TESTS, p)] : [];
  });
}

const read = (rel: string): string => readFileSync(join(TESTS, rel), 'utf8');

test('the guard: reads pass, unnamed writes do not, and creates never do', () => {
  for (const method of ['GET', 'HEAD']) {
    expect(guardLetsThrough(method, `${API}/delivery/start-project`, [])).toBe(true);
  }
  expect(guardLetsThrough('POST', `${API}/delivery/dispatch-activity-task/p1/a1`, [])).toBe(false);
  // The MERGED READ is a POST and always passes (stage 4a): its selector is a
  // ProjectViewQuery in the body, so it could not be a GET, and aborting it would
  // abort the project read of every spec in the suite.
  expect(guardLetsThrough('POST', `${API}/delivery/query-project-view`, [])).toBe(true);
  // Named exactly, never by shape: the next POST spelled like a read is still a write.
  expect(guardLetsThrough('POST', `${API}/delivery/query-project-view/extra`, [])).toBe(false);
  // The live-drafting set lets the co-author loop through...
  for (const op of [
    'dispatch-activity-task',
    'submit-review-decision',
    'ask-questions',
    'acknowledge-stale-basis',
  ]) {
    expect(
      guardLetsThrough('POST', `${API}/delivery/${op}/p1/a1`, LIVE_DRAFTING_WRITES),
      op
    ).toBe(true);
  }
  // ...and never a create (start-project both creates a project and names its
  // operating model now), nor any write that drives the pump, even under an
  // allowance that names everything.
  const everything = [/.*/];
  for (const url of [
    `${API}/delivery/start-project`,
    `${API}/delivery/execute-next-activity/p1`,
    `${API}/delivery/override-activity/p1/a1`,
    `${API}/delivery/replan-project/p1`,
    `${API}/delivery/set-project-run-state/p1`,
    `${API}/delivery/set-project-execution-policy/p1`,
  ]) {
    expect(guardLetsThrough('POST', url, LIVE_DRAFTING_WRITES), url).toBe(false);
    expect(guardLetsThrough('POST', url, everything), url).toBe(false);
  }
});

test('openSharedProject never reaches the create flow', () => {
  const flows = read('support/flows.ts');
  const start = flows.indexOf('export async function openSharedProject');
  expect(start, 'openSharedProject is defined in support/flows.ts').toBeGreaterThanOrEqual(0);
  const end = flows.indexOf('\nexport ', start + 1);
  const body = flows.slice(start, end === -1 ? undefined : end);
  expect(body).not.toMatch(
    /createProjectFromLanding\(|start-project|newProjectButton|newProjectCard/,
  );
});

test('the real create flow runs only in the seed step, or under a stub that answers it', () => {
  const callers = sources().filter(
    (f) => f !== 'support/flows.ts' && /createProjectFromLanding\(/.test(read(f))
  );
  expect(callers, 'someone still exercises the create flow').toContain(SEED);
  for (const f of callers) {
    if (f === SEED) continue;
    expect(read(f), `${f} runs the create flow without faking create-project`).toMatch(
      /stubCreatedProject\(/
    );
  }
});

test('every spec that opens a project runs under the shared dispatch guard', () => {
  const opens = /openSharedProject\(|createProjectFromLanding\(|openStubbedProject\(|stubCreatedProject\(/;
  const guarded = /import \{[^}]*\btest\b[^}]*\} from '\.\/support\/dispatchGuard\.js'/;
  const specs = sources().filter((f) => f.endsWith('.spec.ts') && opens.test(read(f)));
  // A floor on the SCAN, not on the suite: it fails if the regex above ever stops
  // matching real specs (a renamed flow helper, a moved directory) and this check
  // starts passing over an empty set. Stage 5 Task 13 retired the 38 specs that
  // drove the construction console and the design rails, taking this set from 7
  // to 5 — billing, close-and-no-render, homebase, landing, team — so the floor
  // moves with it. Lower it only for the same reason: a spec genuinely left.
  expect(specs.length).toBeGreaterThan(4);
  for (const f of specs) {
    const src = read(f);
    expect(src, `${f} opens a project without the dispatch guard`).toMatch(guarded);
    expect(src, `${f} also imports an unguarded test`).not.toMatch(
      /import \{[^}]*\btest\b[^}]*\} from '@playwright\/test'/
    );
  }
});
