/**
 * The test-run panel's rules, as data — pure (no React).
 *
 * The testing task's run (spec §6.4) is one `results.json` joined with the
 * component's projected scenarios. What this module decides:
 *
 *   summarize        the header's three tiles — a skip is NOT a pass (§3.1).
 *   tabsFor          which detail tabs a scenario offers. A server scenario
 *                    records no video, so it offers no Video tab; mounting a
 *                    `<video>` over an empty src draws a broken player
 *                    (Review Focus 4).
 *   goTestNameFor    the generated test's name — app-generator/testgen `ident`
 *                    maps every non-alphanumeric rune of the scenario id to `_`.
 *   goTestOutputFor  one scenario's stdout/stderr out of the venue's
 *                    `go test -json` stream: the `output` events of its test
 *                    and that test's subtests (scenariohost runs the scenario
 *                    body under `t.Run(id, …)`).
 *   venueRunUrl      the GitHub run page, derived from the activity's PR url
 *                    (same repo); a local run has no page.
 *   isNoRecordedRun  whether the testRun view's failure is the server's
 *                    NotFound (404): no run recorded yet, the normal state of
 *                    every integration task until the venue runs, so the panel
 *                    shows the empty state rather than an error.
 */
import { ApiError } from '../../../contracts/errors.ts';

export interface ScenarioStatusCounts {
  pass: number;
  fail: number;
  skip: number;
}

export function summarize(scenarios: readonly { status: string }[]): ScenarioStatusCounts {
  const out: ScenarioStatusCounts = { pass: 0, fail: 0, skip: 0 };
  for (const s of scenarios) {
    if (s.status === 'pass') out.pass += 1;
    else if (s.status === 'fail') out.fail += 1;
    else if (s.status === 'skip') out.skip += 1;
  }
  return out;
}

export type ScenarioTab = 'Output' | 'Video';

export function tabsFor(s: { videoRef: string; traceRef: string }): ScenarioTab[] {
  return s.videoRef.length > 0 ? ['Output', 'Video'] : ['Output'];
}

export function goTestNameFor(scenarioId: string): string {
  return `TestScenario_${scenarioId.replace(/[^A-Za-z0-9]/g, '_')}`;
}

interface GoTestEvent {
  Action?: unknown;
  Test?: unknown;
  Output?: unknown;
}

export function goTestOutputFor(jsonl: string, scenarioId: string): string {
  const name = goTestNameFor(scenarioId);
  const prefix = `${name}/`;
  let out = '';
  for (const line of jsonl.split('\n')) {
    if (line.trim().length === 0) continue;
    let ev: GoTestEvent;
    try {
      ev = JSON.parse(line) as GoTestEvent;
    } catch {
      continue;
    }
    if (ev.Action !== 'output' || typeof ev.Test !== 'string' || typeof ev.Output !== 'string') {
      continue;
    }
    if (ev.Test === name || ev.Test.startsWith(prefix)) out += ev.Output;
  }
  return out;
}

export function venueRunUrl(prUrl: string | undefined, runId: string): string | undefined {
  if (prUrl === undefined || runId.length === 0) return undefined;
  const m = /^(https:\/\/github\.com\/[^/]+\/[^/]+)\/pull\/\d+/.exec(prUrl);
  if (m?.[1] === undefined) return undefined;
  return `${m[1]}/actions/runs/${encodeURIComponent(runId)}`;
}

export function isNoRecordedRun(error: Error | null): boolean {
  return error instanceof ApiError && error.status === 404;
}
