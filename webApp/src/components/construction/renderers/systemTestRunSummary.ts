/**
 * System Testing (N-IT)'s run summary, as data (fix round B, designer P1-12).
 *
 * The run view used to call every scenario without a fully green run "failing",
 * in red, under an amber "0/5 scenarios green" tile. On a project where N-IT has
 * never been attempted, that asserts five failures nobody observed: an unrun step
 * carries no status at all, and "not green" is not "red".
 *
 * So a scenario's status is read from the steps' own recorded statuses — red only
 * where a step actually recorded red — and when nothing has been attempted (no
 * attempt on the N-IT row and no step status anywhere) the view says so: "not run ·
 * N scenarios planned", muted chips, no tile.
 *
 * Pure (no React), so node:test pins it (systemTestRunSummary.test.ts).
 */
import type { ConstructionRow, TestScenarioView } from '../../../contracts/types';

/** One scenario's run status, read from what its steps recorded. */
export type ScenarioRunStatus = 'green' | 'failing' | 'partial' | 'notRun';

function stepsOf(s: TestScenarioView): { status?: string }[] {
  return (s.cases ?? []).flatMap((c) => c.steps ?? []);
}

export function scenarioRunStatus(s: TestScenarioView): ScenarioRunStatus {
  const steps = stepsOf(s);
  if (steps.some((st) => st.status === 'red')) return 'failing';
  const green = steps.filter((st) => st.status === 'green').length;
  if (green === 0) return 'notRun';
  const everyCaseHasSteps = (s.cases ?? []).every((c) => (c.steps ?? []).length > 0);
  return green === steps.length && everyCaseHasSteps ? 'green' : 'partial';
}

export interface ScenarioChip {
  label: string;
  tone: 'good' | 'bad' | 'muted';
}

/** Red is reserved for a step that recorded red; everything unrun is muted. */
export function scenarioChipFor(status: ScenarioRunStatus): ScenarioChip {
  switch (status) {
    case 'green':
      return { label: 'green', tone: 'good' };
    case 'failing':
      return { label: 'failing', tone: 'bad' };
    case 'partial':
      return { label: 'partly run', tone: 'muted' };
    case 'notRun':
      return { label: 'not run', tone: 'muted' };
  }
}

export interface SystemTestRunSummary {
  /** Whether ANY run was attempted: an OBSERVED attempt on the row, or a recorded
   *  step status. A reconstructed attempt is not a run anyone watched. */
  attempted: boolean;
  headline: string;
  /** The green/total tile — only once something has been attempted. */
  tile?: { label: string; value: string; tone: 'good' | 'bad' };
}

export function systemTestRunSummaryFor(
  scenarios: readonly TestScenarioView[],
  row: ConstructionRow | undefined
): SystemTestRunSummary {
  const statuses = scenarios.map(scenarioRunStatus);
  // Only an OBSERVED attempt counts (fix-B review M4): the backfill writes
  // reconstructed attempts no pump ever ran, and one of those must not bring back
  // the red "0/5 green" tile over a system test nobody has run.
  const attempted =
    (row?.attempts ?? []).some((a) => a.provenance.origin === 'observed') ||
    statuses.some((s) => s !== 'notRun');
  if (!attempted) {
    const n = scenarios.length;
    return {
      attempted,
      headline: `not run · ${String(n)} ${n === 1 ? 'scenario' : 'scenarios'} planned`,
    };
  }
  const green = statuses.filter((s) => s === 'green').length;
  return {
    attempted,
    headline: 'first run against the real build',
    tile: {
      label: 'scenarios green',
      value: `${String(green)}/${String(scenarios.length)}`,
      tone: green === scenarios.length ? 'good' : 'bad',
    },
  };
}
