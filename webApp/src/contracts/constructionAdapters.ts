/**
 * Pure adapters mapping the Phase-3 construction wire models (api/construction.ts)
 * + the committed Phase-2 head-state (network × activityList) into render-ready
 * view models the Construction console consumes. No React here. Every function is
 * total and resilient to an absent session (the pump is dormant) — it returns a
 * safe awaiting view rather than throwing.
 *
 * The console reads from TWO honest sources, by design:
 *   - The TRACKER is the committed Phase-2 network (CPM over ActivityList ×
 *     Network), the same data the Phase-2 NetworkView renders — under a build
 *     lens. The per-activity construction head-state (constructionRows) is now
 *     the PRIMARY status source — integrated/in-review/in-construction statuses
 *     come from there first. The git head-state (merged PR) serves as a
 *     compatible secondary source. The network-derived eligible/blocked fills the
 *     remainder (activities not yet present in constructionRows).
 *   - The ACTIVE-ACTIVITY DETAIL (stage, pipeline phase, reviewer set, variance)
 *     comes from the live construction session endpoint.
 */
import type {
  ConstructionSessionState,
  ConstructionStage,
  ConstructionRow,
  NetworkModel,
  NetworkMilestone,
} from './types';
import type { FailureReason } from './enums.gen';

/** The build-status lens applied to a tracker node — mirrors the mock BuildStatus. */
export type BuildStatus =
  | 'integrated'
  | 'in-review'
  | 'in-construction'
  | 'in-detailed-design'
  | 'eligible'
  | 'blocked'
  | 'not-started'
  | 'failed'
  // The server could not classify this activity at all (Type/Kind/Variant/
  // Phase/BuildStatus all sit at their zero value and are dropped rather than
  // asserted — see ConstructionRow.status). Distinct from `not-started`:
  // `not-started` is a real, known state; `unclassified` is "we don't know",
  // and a consumer that collapses the two would render a confident, false
  // "Not started" chip for a row the server could not type at all.
  | 'unclassified';

export const BUILD_STATUS_META: Record<BuildStatus, { label: string; short: string }> = {
  integrated: { label: 'Integrated', short: 'INTEG' },
  'in-review': { label: 'In review', short: 'REVIEW' },
  'in-construction': { label: 'In construction', short: 'BUILD' },
  'in-detailed-design': { label: 'In detailed design', short: 'D-DSGN' },
  eligible: { label: 'Eligible', short: 'READY' },
  blocked: { label: 'Blocked', short: 'BLOCKED' },
  'not-started': { label: 'Not started', short: 'PEND' },
  failed: { label: 'Failed', short: 'FAILED' },
  unclassified: { label: 'Unclassified', short: 'UNCL' },
};

/**
 * Human labels for the terminal FailureReason recorded on a failed construction
 * row. Keyed by the generated app-string union, so a new Go FailureReason const
 * breaks tsc here rather than rendering a raw camelCase token to the operator.
 */
export const FAILURE_REASON_LABEL: Record<FailureReason, string> = {
  unknown: 'Unknown failure',
  pipelineFailed: 'Pipeline failed',
  pipelineCancelled: 'Pipeline cancelled',
  pipelineTimedOut: 'Pipeline timed out',
  varianceExhausted: 'Variance exhausted',
  escalationTimedOut: 'Escalation timed out',
  componentUnresolved: 'Component unresolved',
  dependencyUnresolved: 'Dependency unresolved',
  dependencyCycle: 'Dependency cycle',
  activityUnclassifiable: 'Activity unclassifiable',
};

/**
 * Maps the live ConstructionStage of the session's active activity onto the
 * tracker build-status lens. Only the ONE active activity gets a live status; the
 * rest of the network reads as not-started until the per-activity head-state
 * aggregate lands (documented projectStateAccess follow-up).
 */
export function buildStatusForStage(stage: ConstructionStage): BuildStatus {
  switch (stage) {
    case 'dispatching':
      return 'in-construction';
    case 'pipelineRunning':
      return 'in-construction';
    case 'reviewing':
      return 'in-review';
    case 'awaitingTakeover':
      return 'blocked';
    case 'awaitingApproval':
      return 'in-construction';
    case 'paused':
      return 'blocked';
    case 'exited':
      return 'integrated';
    case 'unknown':
      return 'not-started';
    default:
      return 'not-started';
  }
}

/**
 * Whether the session represents a live/active construction session at all (vs the
 * dormant-pump awaiting state). A 404 surfaces as an undefined session upstream;
 * an empty dispatching view with no activity / pipeline / review / variance is the
 * quiet-pump answer the console renders as awaiting.
 */
export function sessionIsLive(session: ConstructionSessionState | undefined): boolean {
  if (session === undefined) return false;
  const v = session.view;
  return (
    v.activityId !== undefined ||
    session.pipelinePhase !== undefined ||
    v.reviewSet !== undefined ||
    v.variance !== undefined ||
    (session.stage !== 'dispatching' && session.stage !== 'unknown')
  );
}

/**
 * Maps a ConstructionRow status string onto the tracker BuildStatus lens.
 * The row-state union is a subset of BuildStatus, so every present member maps
 * 1:1.
 *
 * `row.status` is absent for TWO different reasons, and this function keeps them
 * apart (see ConstructionRow.status):
 *
 *   - `classified === false` — the server could not work out what the activity
 *     even is. That degrades to `'unclassified'`, never to `'not-started'`: the
 *     two mean different things, and folding an unknown state into a known one
 *     is the exact false-positive this row-state union exists to avoid.
 *   - `classified === true` with no build evidence — the server knows exactly
 *     what the activity is and has NO record of progress on it (no stored
 *     phases, no attempt ledger). Twenty committed rows are in this state. It is
 *     not `'unclassified'` (we do know what it is), not `'in-construction'` (the
 *     lie this stage removes) and not `'not-started'` (a positive claim nothing
 *     records either). The row simply has nothing to say, so `undefined` is
 *     returned and the CALLER supplies the honest answer from another source —
 *     the network-derived eligible/blocked in computeActivityStatuses, or no
 *     chip at all in a render site (spec §7.2).
 */
export function buildStatusForConstructionRow(row: ConstructionRow): BuildStatus | undefined {
  switch (row.status) {
    case 'integrated':
      return 'integrated';
    case 'in-review':
      return 'in-review';
    case 'in-construction':
      return 'in-construction';
    case 'failed':
      return 'failed';
    case undefined:
      return row.classified ? undefined : 'unclassified';
    // Both `case undefined` (eslint's switch-exhaustiveness-check wants every
    // union member named explicitly) and `default` (tsc's noImplicitReturns
    // does not treat the case list above as exhaustive without one) are
    // required to satisfy both gates; they agree on the same answer.
    default:
      return row.classified ? undefined : 'unclassified';
  }
}

/**
 * Derives the build-status for every activity in the committed network from
 * four pure sources — no Temporal pump, no server round-trip:
 *
 *   1. constructionRowFor(id) — PRIMARY: the per-activity construction head-state
 *      aggregate (integrated / in-review / in-construction / failed). When present this
 *      WINS over all other sources except the live session override (see §3) — unless
 *      the row asserts no status at all (classified with no build evidence), in which
 *      case it is skipped and §4 answers instead.
 *   2. gitFor(id)?.merged === true — SECONDARY/COMPATIBLE: the PR landing on main
 *      is also treated as integrated. Used when constructionRowFor returns nothing.
 *   3. liveActiveStatus — the ONE activity currently in-flight (from the session).
 *      Applies only when the pump is live and overrides even the constructionRow for
 *      that activity (the pump is the ground truth while running).
 *   4. eligible / blocked — derived from the predecessor graph. Crucially, the
 *      "done" set used for eligibility cascade is the UNION of constructionRows-
 *      integrated + git-merged, so eligibility propagates correctly off the real
 *      integrated set (not just the git-merged subset).
 *
 * The map contains every activity id that appears in the network's dependency
 * rows (as `activity` or inside any `dependsOn` array), EXCLUDING milestone ids
 * (network.milestones[]) — a milestone is a zero-duration event node, not a
 * constructable activity, so it never gets a BuildStatus entry of its own. IDs
 * absent from the network are not included — callers should fall back to
 * `'not-started'`.
 *
 * A dependsOn entry naming a milestone id is resolved through
 * `isDependencySatisfied` below rather than the activity `done` set directly —
 * see that function for the recursive-satisfaction rule, which mirrors the
 * server's resolveDependencySatisfied.
 *
 * When `constructionRowFor` is undefined (no construction data at all), behaviour
 * is identical to the pre-constructionRows derivation (git-merged → integrated,
 * network-derived eligible/blocked).
 */
export function computeActivityStatuses(
  network: NetworkModel,
  gitFor: (id: string) => { merged: boolean } | undefined,
  liveActiveId: string | undefined,
  liveActiveStatus: BuildStatus,
  constructionRowFor?: (id: string) => ConstructionRow | undefined
): Map<string, BuildStatus> {
  const deps = network.dependencies ?? [];

  const milestoneById = new Map<string, NetworkMilestone>();
  for (const m of network.milestones ?? []) milestoneById.set(m.id, m);

  // Collect the full activity universe from the dependency rows, excluding
  // milestone ids — those are event nodes, not constructable activities, and are
  // resolved via isDependencySatisfied instead of appearing in the status map.
  const allIds = new Set<string>();
  for (const d of deps) {
    if (!milestoneById.has(d.activity)) allIds.add(d.activity);
    for (const p of d.dependsOn ?? []) {
      if (!milestoneById.has(p)) allIds.add(p);
    }
  }

  // Build predecessor index (id → predecessor ids[]) over real activities only;
  // a predecessor entry may still name a milestone id, resolved below.
  const predecessors = new Map<string, string[]>();
  for (const id of allIds) predecessors.set(id, []);
  for (const d of deps) {
    if (milestoneById.has(d.activity)) continue;
    for (const p of d.dependsOn ?? []) {
      predecessors.get(d.activity)?.push(p);
    }
  }

  // Pass 1: compute the done (integrated) set.
  // Primary source: constructionRows with status === 'integrated'.
  // Secondary source: git merged PR.
  // Both contribute to the "done" set used for eligibility cascade.
  const done = new Set<string>();
  for (const id of allIds) {
    const constructionRow = constructionRowFor !== undefined ? constructionRowFor(id) : undefined;
    if (constructionRow?.status === 'integrated') {
      done.add(id);
    } else if (gitFor(id)?.merged === true) {
      done.add(id);
    }
  }

  // Pass 2: assign statuses.
  const result = new Map<string, BuildStatus>();
  for (const id of allIds) {
    if (done.has(id)) {
      // integrated — either constructionRows or git-merged
      result.set(id, 'integrated');
    } else if (liveActiveId !== undefined && id === liveActiveId) {
      // Live session override: the pump is actively supervising this activity.
      result.set(id, liveActiveStatus);
    } else {
      // Check constructionRows for in-review / in-construction (non-done states).
      // A row that asserts NOTHING (classified, but no build evidence — no stored
      // phases and no attempt ledger) must NOT short-circuit here: it used to,
      // because its wire zero decoded as 'in-construction', so twenty activities
      // never reached the network-derived readiness below and rendered as builds
      // in progress instead of the eligible/blocked they actually are. An
      // unclassified row still short-circuits, on 'unclassified' — there the row
      // IS the answer.
      const constructionRow = constructionRowFor !== undefined ? constructionRowFor(id) : undefined;
      const rowStatus =
        constructionRow !== undefined ? buildStatusForConstructionRow(constructionRow) : undefined;
      const pending = constructionRow?.pendingResume;
      if (pending !== undefined) {
        // Integration-pending (architect (D), D.3): its coarse status says in-review,
        // but nothing runs it. The SERVER's waitsOn — the pump's own dependency rule —
        // decides readiness, not this mirror's done set: blocked while it waits on
        // something, eligible when it is next in line.
        result.set(id, pending.waitsOn.length > 0 ? 'blocked' : 'eligible');
      } else if (rowStatus !== undefined) {
        result.set(id, rowStatus);
      } else {
        // Network-derived fallback: eligible if all predecessors are satisfied, else
        // blocked. A predecessor naming a milestone resolves recursively through
        // isDependencySatisfied rather than a plain `done` lookup.
        const preds = predecessors.get(id) ?? [];
        const allPredsDone = preds.every((p) =>
          isDependencySatisfied(p, milestoneById, done, new Set())
        );
        result.set(id, allPredsDone ? 'eligible' : 'blocked');
      }
    }
  }

  return result;
}

/**
 * Recursively resolves whether one dependency id is satisfied — the SPA mirror of
 * the server's projectstate.ResolveDependencySatisfied
 * (server/internal/resourceaccess/projectstate/projectstateaccess.go:8470-8506).
 * The rule used to live unexported in the construction Manager; it moved into
 * projectstate so the pump and the catalog's construction-complete signal read
 * one copy. Change the rule there and here together.
 *
 *   - An activity id is satisfied iff it's a member of the integrated `done` set.
 *   - A milestone id (present in `milestoneById`) is satisfied iff EVERY id in its
 *     own `dependsOn` is, recursively, satisfied. A milestone with no `dependsOn`
 *     (the project-start gate) is satisfied.
 *   - A dangling id (neither a known activity nor an authored milestone) and a
 *     milestone dependency cycle both resolve to "not satisfied" — the dependent
 *     activity simply stays blocked. Unlike the server, the SPA render doesn't
 *     need to distinguish DependencyUnresolved from DependencyCycle as separate
 *     failure kinds; both are authored-network defects surfaced elsewhere (the
 *     construction pump's own terminal-failure reporting), not something the
 *     tracker view computes.
 *
 * `visiting` is the set of milestone ids on THIS call's recursion stack — passing
 * a fresh Set() per top-level caller and threading it through recursive calls
 * guarantees termination on an authored cycle instead of recursing forever.
 */
function isDependencySatisfied(
  depID: string,
  milestoneById: Map<string, NetworkMilestone>,
  done: Set<string>,
  visiting: Set<string>
): boolean {
  const milestone = milestoneById.get(depID);
  if (milestone !== undefined) {
    if (visiting.has(depID)) return false;
    visiting.add(depID);
    const satisfied = (milestone.dependsOn ?? []).every((sub) =>
      isDependencySatisfied(sub, milestoneById, done, visiting)
    );
    visiting.delete(depID);
    return satisfied;
  }
  return done.has(depID);
}
