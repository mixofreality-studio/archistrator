/// <reference types="node" />
/**
 * deriveOperating against the SHARED fixture corpus at
 * testdata/operating_fixtures.json — shared byte-identically with the Go side's
 * server/internal/resourceaccess/projectstate/testdata/operating_fixtures.json
 * (see that file's own sync comment on TestIsConstructionComplete_Fixtures in
 * server/internal/resourceaccess/projectstate/access_test.go) so both languages
 * assert the exact same construction-complete cases: all-integrated true;
 * one-failed/one-in-review/empty/skipped-shaped-row/not-construction-phase all
 * false. A drift check between the two copies is a plain `diff` (trivial;
 * intentionally not wired as a systemtest — see task-14-report.md).
 *
 * The fixture rows are already shaped exactly as deriveOperating's parameter
 * (raw {phase, buildStatus} ordinals) — no reshaping needed. Only projectPhase is
 * adapted: the fixture carries the raw Phase ordinal (Go's iota), which is mapped
 * through the SAME generated ordinal table wire.ts uses (PROJECT_PHASE_ORDINAL_TO_APP)
 * onto the app ProjectPhase string deriveOperating's signature takes.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { deriveOperating, type OperatingRow } from './operating.ts';
import { PROJECT_PHASE_ORDINAL_TO_APP } from './enums.gen.ts';

interface FixtureCase {
  name: string;
  rows: OperatingRow[];
  projectPhase: number;
  expect: boolean;
}

const fixturePath = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  'testdata',
  'operating_fixtures.json'
);
const cases = JSON.parse(readFileSync(fixturePath, 'utf8')) as FixtureCase[];

assert.ok(cases.length > 0, 'fixture corpus is empty; this test would pass vacuously');

for (const tc of cases) {
  void test(`deriveOperating: ${tc.name}`, () => {
    const projectPhase = PROJECT_PHASE_ORDINAL_TO_APP[tc.projectPhase] ?? 'unknown';
    const got = deriveOperating(tc.rows, projectPhase);
    assert.equal(
      got,
      tc.expect,
      `deriveOperating(${tc.name}) = ${String(got)}, want ${String(tc.expect)}`
    );
  });
}
