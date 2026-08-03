/**
 * Infra gating — the UI analogue of systemtests' InfraFromEnv + requireStack.
 *
 * The SPA gates its whole tree on `GET /api/userinfo` returning 200 (session
 * probe, GTD parity). In dev mode the Go server injects a dev principal, so a
 * Postgres-backed server answers 200 with no IdP. Two tiers, mirroring
 * systemtests' "structural always-runs / wire-skips-without-stack" split:
 *
 *   1. SERVER REACHABLE — needed by every spec. We probe /api/userinfo through
 *      the SPA origin (the Vite proxy forwards it to the Go server). If it is
 *      not 200, the pure-UI specs SKIP with an annotation rather than failing on
 *      a backend that was never provisioned.
 *
 *   2. LIVE DRAFTING — the co-author flow (request-draft → generating → render →
 *      gate) additionally needs Temporal + a worker provider. That stack is
 *      opt-in via UITESTS_LIVE_DRAFTING, now a four-value flag
 *      (off | replay | WHEN_REQUIRED | live; legacy 1/true ⇒ live) — see
 *      draftingMode() below. Specs needing it SKIP unless the mode is non-off,
 *      exactly as systemtests' wire tests skip without the ARCHISTRATOR_* infra env.
 */
import { test, type Page, type APIRequestContext } from '@playwright/test';
import { TESTID } from './testids.js';

/**
 * Drafting modes (design 2026-06-05). The uitests do not start the Go server, so
 * this only decides whether to RUN the live-drafting specs; the operator brings up
 * the server in the matching worker mode (replay-strict / record-on-miss / live).
 *
 *   off  (or unset) — skip live-drafting specs (pure-UI only).
 *   replay          — run; server replays cassettes strictly (offline, default CI).
 *   WHEN_REQUIRED   — run; server records misses via a real delegate.
 *   live            — run; server hits a real model, cache ignored.
 *
 * Legacy '1' / 'true' are accepted as aliases meaning "run".
 */
export type DraftingMode = 'off' | 'replay' | 'WHEN_REQUIRED' | 'live';

export function draftingMode(): DraftingMode {
  const v = (process.env.UITESTS_LIVE_DRAFTING ?? '').trim();
  switch (v) {
    case 'replay':
      return 'replay';
    case 'WHEN_REQUIRED':
      return 'WHEN_REQUIRED';
    case 'live':
    case '1':
    case 'true':
      return 'live';
    default:
      return 'off';
  }
}

/** True when the live-drafting specs should run (any mode other than off). */
export function liveDraftingEnabled(): boolean {
  return draftingMode() !== 'off';
}

/**
 * serverReachable probes the SPA's `/api/userinfo` (same origin → Vite proxy →
 * Go server). 200 means a dev-mode, Postgres-backed server is answering.
 */
export async function serverReachable(request: APIRequestContext, baseURL: string): Promise<boolean> {
  try {
    const res = await request.get(`${baseURL}/api/userinfo`, {
      headers: { Accept: 'application/json' },
      timeout: 5_000,
    });
    return res.status() === 200;
  } catch {
    return false;
  }
}

/**
 * skipUnlessServer skips the current test (with a clear reason) when the
 * dev-mode server behind the SPA proxy is not reachable. Used by the pure-UI
 * specs so they degrade gracefully on a bare checkout, matching systemtests.
 */
export async function skipUnlessServer(
  request: APIRequestContext,
  baseURL: string,
): Promise<void> {
  const ok = await serverReachable(request, baseURL);
  test.skip(
    !ok,
    'uitests: SPA-proxied server not reachable at /api/userinfo — bring up the dev-mode Go server (Postgres) behind the SPA proxy. See README.',
  );
}

/**
 * skipUnlessLiveDrafting skips when the drafting flag is `off`/unset — the UI
 * analogue of requireStack. Set UITESTS_LIVE_DRAFTING to replay (offline cassette
 * replay), WHEN_REQUIRED, or live to run, with a matching server up (see README).
 */
export function skipUnlessLiveDrafting(): void {
  test.skip(
    !liveDraftingEnabled(),
    'uitests: drafting specs gated off — set UITESTS_LIVE_DRAFTING=replay (offline cassettes) or WHEN_REQUIRED/live with a matching Postgres+Temporal+worker server (see README).',
  );
}

/**
 * constructionArtifactsAvailable probes GetProject for the well-known "archistrator"
 * dogfood project (the SAME id `system-design` defaults an id-less committed
 * project.json to — see server DecodeProjectJSON) and reports whether it carries a
 * REAL, non-empty testingState.systemTestPlan. This is only true when the server's
 * project-state git substrate is pointed at a repo whose committed
 * `.aiarch/state/project.json` already holds the dogfood construction-phase state
 * (e.g. this repo's own checkout, or a seed built from it) — NOT the throwaway
 * empty repo CI provisions for the project-CREATION specs (see
 * .github/workflows/uitests.yml's "Project state" note: that empty repo is
 * intentional, so CreateProject's permissive-resume path hands fresh-phase-0 state
 * to the tests that create projects; it has no "archistrator" project at all).
 */
export async function constructionArtifactsAvailable(
  request: APIRequestContext,
  baseURL: string,
): Promise<boolean> {
  try {
    const res = await request.get(`${baseURL}/api/v1/system-design/get-project/archistrator`, {
      headers: { Accept: 'application/json' },
      timeout: 5_000,
    });
    if (res.status() !== 200) return false;
    const data = (await res.json()) as {
      testingState?: { systemTestPlan?: { scenarios?: unknown[] } };
    };
    return (data.testingState?.systemTestPlan?.scenarios?.length ?? 0) > 0;
  } catch {
    return false;
  }
}

/**
 * skipUnlessConstructionArtifacts skips specs that assert against the REAL
 * committed N-STP/N-IT system-test-plan content (artifact-systemtest.spec.ts) when
 * the server behind the SPA proxy has no such data — e.g. CI's fresh/empty
 * project-state repo. Mirrors skipUnlessServer / skipUnlessLiveDrafting: an honest
 * self-skip with a clear reason rather than a false failure against infra that was
 * never provisioned with this content.
 */
export async function skipUnlessConstructionArtifacts(
  request: APIRequestContext,
  baseURL: string,
): Promise<void> {
  const ok = await constructionArtifactsAvailable(request, baseURL);
  test.skip(
    !ok,
    'uitests: no committed construction-phase "archistrator" project with a system-test plan behind the ' +
      'SPA proxy — this spec asserts REAL N-STP/N-IT content and cannot run against a fresh/empty ' +
      'project-state repo. Point ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL at a repo seeded from this ' +
      "checkout's .aiarch/state/project.json to run it locally.",
  );
}

/**
 * The two episodes episodes-panel.spec.ts drives, resolved OFF THE WIRE rather
 * than hardcoded: whichever captured episode on the target artifact has a
 * readable trace, and whichever one is a GAP. Reading them from the server (a)
 * keeps the spec independent of the seeding tool's own id constants, and (b)
 * lets the spec run unchanged against episodes produced by a REAL agentic
 * dispatch instead of the seeder.
 */
export interface DesignEpisodes {
  /** An episode with outcome `succeeded` that carries a trace to expand. */
  succeededId: string;
  /** An episode with outcome `gap` — the "badges are the point" acceptance. */
  gapId: string;
  /** That gap episode's own reason string, as the server reports it. */
  gapReason: string;
}

/** The bits of the episode list wire shape this file reads. */
interface EpisodeListEntry {
  episodeId?: string;
  outcome?: number;
  gapReason?: string;
  tracePath?: string;
}

/**
 * fetchDesignEpisodes reads the captured episodes recorded against the
 * `mission` artifact of the SAME well-known "archistrator" dogfood project
 * gating.ts already reads above (constructionArtifactsAvailable / fetchCoreUseCases).
 *
 * `artifactKind=0` is the wire ORDINAL for `mission` (SystemDesignArtifactKind);
 * this file already hardcodes REST paths for the same project, and the ordinal
 * is the same published wire contract the SPA's own artifactKindToOrdinal
 * resolves — the spec itself never touches it, it drives the real UI.
 *
 * Returns `undefined` unless BOTH a traced succeeded episode and a gap episode
 * are present, so the spec self-skips (rather than half-failing) on a stack
 * whose episode ledger was never provisioned — the same posture as
 * skipUnlessConstructionArtifacts.
 */
export async function fetchDesignEpisodes(
  request: APIRequestContext,
  baseURL: string,
): Promise<DesignEpisodes | undefined> {
  try {
    const res = await request.get(
      `${baseURL}/api/v1/system-design/list-episodes-for-artifact/archistrator?artifactKind=0`,
      { headers: { Accept: 'application/json' }, timeout: 5_000 },
    );
    if (res.status() !== 200) return undefined;
    const records = (await res.json()) as EpisodeListEntry[];
    if (!Array.isArray(records)) return undefined;

    // Wire ordinals (episode.EpisodeOutcome): 0 succeeded, 1 failed, 2 cancelled, 3 gap.
    const succeeded = records.find(
      (r) => r.outcome === 0 && r.episodeId !== undefined && (r.tracePath ?? '').length > 0,
    );
    const gap = records.find((r) => r.outcome === 3 && r.episodeId !== undefined);
    if (succeeded?.episodeId === undefined || gap?.episodeId === undefined) return undefined;

    return {
      succeededId: succeeded.episodeId,
      gapId: gap.episodeId,
      gapReason: gap.gapReason ?? '',
    };
  } catch {
    return undefined;
  }
}

/** A CORE-classified use case from the committed `coreUseCases` slot. */
export interface CoreUseCase {
  id: string;
  name: string;
}

/** The shape of the bits of GetProject's response this file reads off the wire. */
interface GetProjectResponseShape {
  Slots?: {
    kind?: string;
    model?: {
      model?: {
        decisions?: {
          useCase?: { id?: string; name?: string; classification?: string };
        }[];
      };
    };
  }[];
}

/**
 * fetchCoreUseCases reads the committed `coreUseCases` slot off the SAME
 * well-known "archistrator" dogfood project GetProject reads above
 * (constructionArtifactsAvailable) and returns the Method Phase-1 "2-6 core
 * use cases" (classification === 'core'; see the-method-core-use-cases) —
 * the source of truth tests/meta/use-case-coverage.spec.ts checks UI spec
 * coverage against. Returns `undefined` when the server has no coreUseCases
 * slot committed (fresh/empty project-state repo), so callers can self-skip
 * rather than fail on infra that was never given this content.
 */
export async function fetchCoreUseCases(
  request: APIRequestContext,
  baseURL: string,
): Promise<CoreUseCase[] | undefined> {
  try {
    const res = await request.get(`${baseURL}/api/v1/system-design/get-project/archistrator`, {
      headers: { Accept: 'application/json' },
      timeout: 5_000,
    });
    if (res.status() !== 200) return undefined;
    const data = (await res.json()) as GetProjectResponseShape;
    const slot = data.Slots?.find((s) => s.kind === 'coreUseCases');
    const decisions = slot?.model?.model?.decisions ?? [];
    const core = decisions
      .map((d) => d.useCase)
      .filter(
        (uc): uc is { id: string; name: string; classification: string } =>
          uc?.id !== undefined && uc.name !== undefined && uc.classification === 'core',
      )
      .map((uc) => ({ id: uc.id, name: uc.name }));
    return core.length > 0 ? core : undefined;
  } catch {
    return undefined;
  }
}

/**
 * gotoApp navigates to a SPA route and waits past the session gate: the app
 * shows `loading-indicator` while probing /api/userinfo, then mounts the route.
 * We wait for the loader to detach (best-effort) so callers can assert on the
 * real screen.
 */
export async function gotoApp(page: Page, path: string): Promise<void> {
  await page.goto(path);
  await page.waitForLoadState('domcontentloaded');
  // The gate loader is transient; if present, let it resolve before asserting.
  const loader = page.getByTestId(TESTID.loading);
  if ((await loader.count()) > 0) {
    await loader.first().waitFor({ state: 'detached' }).catch(() => undefined);
  }
}
