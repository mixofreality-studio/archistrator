/**
 * The task spec panel's rules, as data — pure (no React), so `node --test`
 * bundles and runs it directly.
 *
 *   checkRows      every check of the spec joined with its result in the run on
 *                  screen. A check with no result, or a spec with no run at all,
 *                  is `not-run` (R35): never a pass, never an error.
 *   groupByKind    the checks section's groups, in a fixed order: rules, then
 *                  commands, then scenarios, then any kind this build does not
 *                  know (R45) in a trailing "Other" group, never dropped.
 *   checkCounts    the section header's "n passed · m failed · k not run". A
 *                  timeout is a failure (the gate is red on it); a skip and an
 *                  unknown status count in neither, because neither is a pass.
 *   formatUsd / formatTokens   the cost and prompt-size figures.
 *   sourceLabel    what a check runs, in one muted line.
 *   modelSourceTip where the task's model came from.
 *   scenarioViewOf a scenario check's result in the shape ScenarioDetail takes.
 */
import type {
  CheckResultView,
  SpecCheckView,
  TaskSpecView,
  TestRunScenarioView,
} from '../../contracts/types';

export interface CheckRowState {
  check: SpecCheckView;
  result?: CheckResultView;
  state: 'not-run' | 'pass' | 'fail' | 'skip' | 'timeout' | 'unknown';
}

export function checkRows(spec: TaskSpecView): CheckRowState[] {
  const byId = new Map((spec.checkRun?.results ?? []).map((r) => [r.checkId, r]));
  return spec.checks.map((check) => {
    const result = byId.get(check.id);
    return result === undefined
      ? { check, state: 'not-run' }
      : { check, result, state: result.status };
  });
}

export const KIND_ORDER = ['rule', 'command', 'scenario', 'unknown'] as const;

export const KIND_LABEL: Readonly<Record<SpecCheckView['kind'], string>> = {
  rule: 'Rules',
  command: 'Commands',
  scenario: 'Scenarios',
  unknown: 'Other',
};

export function groupByKind(
  rows: readonly CheckRowState[]
): { kind: SpecCheckView['kind']; rows: CheckRowState[] }[] {
  return KIND_ORDER.map((kind) => ({
    kind,
    rows: rows.filter((r) => r.check.kind === kind),
  })).filter((g) => g.rows.length > 0);
}

export interface CheckCounts {
  passed: number;
  failed: number;
  notRun: number;
}

export function checkCounts(rows: readonly CheckRowState[]): CheckCounts {
  const out: CheckCounts = { passed: 0, failed: 0, notRun: 0 };
  for (const r of rows) {
    if (r.state === 'pass') out.passed += 1;
    else if (r.state === 'fail' || r.state === 'timeout') out.failed += 1;
    else if (r.state === 'not-run') out.notRun += 1;
  }
  return out;
}

export function formatUsd(n: number): string {
  return n < 0.01 ? '<$0.01' : `$${n.toFixed(2)}`;
}

export function formatTokens(n: number): string {
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (n >= 1e3) return `${String(Math.round(n / 1e3))}k`;
  return String(n);
}

export function sourceLabel(c: SpecCheckView): string {
  if (c.kind === 'rule') {
    const prefixes = c.source.rulePrefixes ?? [];
    return prefixes.length > 0 ? prefixes.map((p) => `${p}*`).join(' ') : 'all method rules';
  }
  if (c.kind === 'command') return (c.source.argv ?? []).join(' ');
  if (c.kind === 'scenario') {
    return `${c.source.contract ?? ''} · use case ${c.source.useCase ?? ''}`;
  }
  // A kind this build does not know: its id is the one thing certain to say what it is.
  return c.id;
}

export function modelSourceTip(source: TaskSpecView['model']['source']): string {
  if (source === 'charter') return 'from charter';
  if (source === 'projectOverride') return 'project override';
  return 'source unknown';
}

/**
 * A scenario check's result as ScenarioDetail renders it: the scenario id from
 * the check's source (the check id is `scenario:<contract>:<id>`), its title,
 * and the evidence refs the result recorded ('' where it recorded none).
 */
export function scenarioViewOf(check: SpecCheckView, result: CheckResultView): TestRunScenarioView {
  return {
    id: check.source.scenario ?? check.id,
    title: check.title,
    guards: [],
    status: result.status,
    durationMs: result.durationMs,
    videoRef: result.videoRef ?? '',
    traceRef: result.traceRef ?? '',
    outputRef: result.outputRef ?? '',
  };
}

/** A recorded duration, as the row says it. */
export function formatDuration(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${String(ms)} ms`;
}
