/**
 * SYSTEM TEST COVERAGE for one component, from the committed system test plan
 * (N-STP) — the designer's §2.6 section 2, on data already on the wire.
 *
 * Two row types, and neither may read as "untested" (Q4):
 *   - DIRECT: scenarios whose steps call this component. System tests are
 *     black-box and call Managers only, so only the five managers get these.
 *   - REACHED THROUGH: for everything else, the scenarios whose use case's
 *     dynamic view includes this component. The join is
 *     `scenario.useCase === dynamicView.useCaseId` (verified exact for all five
 *     STP scenarios); `via` names the components those scenarios DO call.
 *
 * These are the system plan's tests, never this component's own: the caller
 * captions them so, and the component's own plan is a separate, empty section.
 */
import type { TestScenarioView } from '../../../../contracts/types';

export interface ReachedThroughRow {
  scenario: TestScenarioView;
  /** The components the scenario's steps call, in first-seen order. */
  via: string[];
}

export interface SystemTestCoverage {
  direct: TestScenarioView[];
  reachedThrough: ReachedThroughRow[];
}

/**
 * @param names  every name the plan's steps may use for this component — its
 *               contract key (the plan's steps use it today) and its component id.
 * @param useCaseIds  the use cases whose dynamic view includes the component.
 */
export function systemTestCoverageFor(
  scenarios: readonly TestScenarioView[],
  names: readonly string[],
  useCaseIds: readonly string[]
): SystemTestCoverage {
  const own = new Set(names.filter((n) => n.length > 0));
  const useCases = new Set(useCaseIds);
  const direct: TestScenarioView[] = [];
  const reachedThrough: ReachedThroughRow[] = [];
  for (const scenario of scenarios) {
    const called = stepComponents(scenario);
    if (called.some((c) => own.has(c))) {
      direct.push(scenario);
    } else if (useCases.has(scenario.useCase)) {
      reachedThrough.push({ scenario, via: called });
    }
  }
  return { direct, reachedThrough };
}

function stepComponents(scenario: TestScenarioView): string[] {
  const seen: string[] = [];
  for (const c of scenario.cases ?? []) {
    for (const step of c.steps ?? []) {
      if (step.component.length > 0 && !seen.includes(step.component)) seen.push(step.component);
    }
  }
  return seen;
}

/** `STP-UC3 · Execute a Construction Activity — reached through constructionManager`. */
export function reachedThroughLabel(row: ReachedThroughRow): string {
  const via = row.via.length > 0 ? row.via.join(', ') : 'no recorded step';
  return `${row.scenario.id} · ${row.scenario.title} — reached through ${via}`;
}

export const DIRECT_CAPTION =
  'Black-box scenarios from the system test plan that call this component directly. These are not this component’s own tests.';
export const REACHED_CAPTION =
  'System tests call Managers only. This component is exercised through them, never called directly.';
export const NO_COVERAGE_SENTENCE =
  'No system test scenario calls this component or runs a use case whose flow includes it.';

/** "Appears in N use-case flows. These are designed call chains, not tests." */
export function useCaseFlowsSentence(count: number): string {
  if (count === 0) return 'Appears in no use-case flow.';
  return `Appears in ${String(count)} use-case ${count === 1 ? 'flow' : 'flows'}. These are designed call chains, not tests.`;
}
