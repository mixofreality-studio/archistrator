/// <reference types="node" />
/**
 * deriveOperating against the SHARED fixture corpus at
 * testdata/operating_fixtures.json — shared byte-identically with the Go side's
 * server/internal/resourceaccess/projectstate/testdata/operating_fixtures.json
 * (see that file's own sync comment on TestIsConstructionComplete_Fixtures in
 * server/internal/resourceaccess/projectstate/access_test.go) so both languages
 * assert the exact same construction-complete cases, including the list-driven
 * ones (listed-activity-without-row, rows-without-a-list,
 * row-outside-the-list-is-ignored). A drift check between the two copies is a
 * plain `diff` (trivial; intentionally not wired as a systemtest — see
 * task-14-report.md).
 *
 * The fixture's activities and rows are already shaped exactly as deriveOperating's
 * parameters (committed activity names; raw {phase, buildStatus} ordinals keyed by
 * activity id) — no reshaping needed. Only projectPhase is adapted: the fixture
 * carries the raw Phase ordinal (Go's iota), which is mapped through the SAME
 * generated ordinal table wire.ts uses (PROJECT_PHASE_ORDINAL_TO_APP) onto the app
 * ProjectPhase string deriveOperating's signature takes.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { deriveOperating, type OperatingRow } from './operating.ts';
import { PROJECT_PHASE_ORDINAL_TO_APP } from './enums.gen.ts';
import type { components } from './schema.ts';
import { mapProjectState } from './wire.ts';

interface FixtureCase {
  name: string;
  activities: string[];
  rows: Record<string, OperatingRow>;
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
    const got = deriveOperating(tc.activities, tc.rows, projectPhase);
    assert.equal(
      got,
      tc.expect,
      `deriveOperating(${tc.name}) = ${String(got)}, want ${String(tc.expect)}`
    );
  });
}

// --- the wire seam: mapProjectState feeds deriveOperating the COMMITTED list -----------

type Schemas = components['schemas'];

/** ArtifactStage ordinals, as the Go consts order them (empty, awaitingReview, committed…). */
const STAGE_AWAITING_REVIEW = 1 as const;
const STAGE_COMMITTED = 2 as const;

function doneIntegratedRow(id: string): Schemas['SystemDesignActivityConstructionStatus'] {
  return {
    ActivityID: id,
    BuildStatus: 2,
    CurrentPhase: 'integration',
    FailureDetail: '',
    FailureReason: 0,
    Kind: 0,
    Phase: 2,
    Phases: null,
    Produced: null,
    Type: 0,
    Variant: 0,
    classified: true,
    hasBuildEvidence: true,
    worstOrigin: 'observed',
    layer: '',
    layerBand: '',
  };
}

function wireState(
  stage: Schemas['SystemDesignArtifactStage'],
  listed: readonly string[],
  rowIds: readonly string[]
): Schemas['SystemDesignProjectState'] {
  return {
    ActivityConstruction: Object.fromEntries(rowIds.map((id) => [id, doneIntegratedRow(id)])),
    GitRows: {},
    Name: 'fixture',
    Owner: 'fixture-owner',
    Phase: 2,
    PhaseName: 'construction',
    ProjectID: 'fixture-project',
    Research: { sources: null },
    ServiceContracts: {},
    Slots: [
      {
        kind: 'activityList',
        stage,
        model: {
          kind: 'activityList',
          model: {
            activities: listed.map((name) => ({
              name,
              coding: true,
              effortDays: 5,
              riskBucket: 1,
              workerClass: 'junior-developer',
            })),
          },
        },
      },
    ],
    Version: 1,
    operatingModel: 'local',
  };
}

void test('mapProjectState: operating once every committed activity is done and integrated', () => {
  const state = mapProjectState(wireState(STAGE_COMMITTED, ['A', 'B'], ['A', 'B']));
  assert.equal(state.operating, true);
});

void test('mapProjectState: a committed activity with no row keeps the project out of operating', () => {
  const state = mapProjectState(wireState(STAGE_COMMITTED, ['A', 'B', 'C'], ['A', 'B']));
  assert.equal(state.operating, undefined, 'C is listed but has no row: it has not started');
});

void test('mapProjectState: an activity list awaiting review is not the committed plan', () => {
  const state = mapProjectState(wireState(STAGE_AWAITING_REVIEW, ['A', 'B'], ['A', 'B']));
  assert.equal(state.operating, undefined);
});
