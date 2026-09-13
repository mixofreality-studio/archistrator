/**
 * tests/meta/project-creation-guard.spec.ts: no spec but the seed step can create
 * a real project (fix-G review ruling).
 *
 * A META-check: it drives no page. It pins two things:
 *
 *   (a) the guard's rule (guardLetsThrough): GET and HEAD pass; every other write is
 *       aborted unless the spec named it; and creating a project, or any
 *       construction write, is aborted WHATEVER a spec names;
 *   (b) the suite's SOURCE, statically: openSharedProject never reaches the create
 *       flow; the real create flow runs only in the seed step, or under a stub that
 *       answers create-project in the browser; and every spec that opens a project
 *       runs under the shared dispatch guard.
 */
import { test, expect } from '@playwright/test';
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
    expect(guardLetsThrough(method, `${API}/system-design/create-project`, [])).toBe(true);
  }
  expect(guardLetsThrough('POST', `${API}/system-design/request-artifact-draft/p1`, [])).toBe(
    false
  );
  // The live-drafting set lets the co-author loop through...
  for (const op of [
    'start-system-design',
    'set-research-input',
    'request-artifact-draft',
    'submit-review-decision',
  ]) {
    expect(guardLetsThrough('POST', `${API}/system-design/${op}/p1`, LIVE_DRAFTING_WRITES), op).toBe(
      true
    );
  }
  // ...and never a create, a create's operating model, or any construction write,
  // even under an allowance that names everything.
  const everything = [/.*/];
  for (const url of [
    `${API}/system-design/create-project`,
    `${API}/system-design/set-operating-model/p1`,
    `${API}/construction/execute-next-activity/p1`,
    `${API}/construction/submit-phase-decision/p1`,
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
  expect(body).not.toMatch(/createProjectFromLanding\(|create-project|newProjectButton|newProjectCard/);
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
  expect(specs.length).toBeGreaterThan(6);
  for (const f of specs) {
    const src = read(f);
    expect(src, `${f} opens a project without the dispatch guard`).toMatch(guarded);
    expect(src, `${f} also imports an unguarded test`).not.toMatch(
      /import \{[^}]*\btest\b[^}]*\} from '@playwright\/test'/
    );
  }
});
