/**
 * WHERE a committed artifact goes in the detail pane, and WHAT its frame may
 * claim — the designer's placement ruling (renderers-placement.md §2, §4, §5),
 * pure and tested without a renderer (node:test cannot load `.tsx`; relative
 * VALUE imports carry an explicit `.ts`).
 *
 * THE GOVERNING PRINCIPLE
 * -----------------------
 * An artifact is COMMITTED PROJECT STATE. A task's lifecycle is BUILD EVIDENCE.
 * They are separate facts with separate provenance, so the pane stacks them as
 * two blocks — the attempt's provenance note (hatched when reconstructed), then
 * the artifact frame, which carries its own caption and never the hatch.
 *
 * Two placement families:
 *   - COMPANIONS sit above whatever body the selection would otherwise get (the
 *     episodes, or the unknown card): the bare-click contract summary card, the
 *     collapsed REFERENCE card under a construction task, and the overview-depth
 *     gap statements. They never replace the body.
 *   - PRIMARIES are the body: the artifact outranks "no record" — a Not started
 *     row still shows its committed contract, with its state said in one line.
 *     A gate task wraps a primary above its verdict.
 */
import type { ConstructionRow, RecordOriginRow } from '../../../../contracts/types';
import type { ContractJoin } from '../../../../contracts/serviceContracts.ts';
import type { LensSelection } from '../../lens/useLensSelection';
import { lifecyclePhaseOfTask } from './bodyDispatch.ts';

export type Placement =
  | { kind: 'none' }
  // --- companions -----------------------------------------------------------
  /** Bare activity click: the COMMITTED NOW contract summary card, no canvas. */
  | { kind: 'contractSummary' }
  /** A task that does not produce the contract but works against it: one line. */
  | { kind: 'contractReference'; line: string }
  /** Bare click on a row whose contract is a real gap (§5.1), no canvas. */
  | { kind: 'gapOverview' }
  /** Bare click on a Resource row: the by-design line + WHO REACHES IT (§2.4). */
  | { kind: 'byDesignOverview' }
  // --- primaries ------------------------------------------------------------
  /** The ServiceContractView (Code · Component · Dynamic · Facets), opening on Code. */
  | { kind: 'contractFull' }
  /** Detailed Design of a row whose contract is missing: the gap + its Component view. */
  | { kind: 'contractGap' }
  /** Detailed Design (Provisioning Spec) of a Resource: by design + WHO REACHES IT. */
  | { kind: 'byDesignSpec' }
  /** Code Review: the commit under review, and the contract as REFERENCE. */
  | { kind: 'codeReview' }
  /** SRS Review: the SRS is not readable in this console (earmark E3). */
  | { kind: 'srsUnreadable' }
  /** Test Plan: the component's own plan (not recorded) + system test coverage. */
  | { kind: 'componentTestPlan' }
  /**
   * The SPA's Construction: its built surfaces, or NO SURFACES RECORDED — whatever
   * the attempt state, a Not started row included (designer check, polish 8).
   */
  | { kind: 'frontendSurfaces' }
  /** Detailed Design where the committed data does not place the activity at all. */
  | { kind: 'contractUnresolved' };

export type PlacementKind = Placement['kind'];

const PRIMARY: ReadonlySet<PlacementKind> = new Set([
  'contractFull',
  'contractGap',
  'byDesignSpec',
  'codeReview',
  'srsUnreadable',
  'componentTestPlan',
  'frontendSurfaces',
  'contractUnresolved',
]);

/** A primary placement IS the body; a companion rides above the ordinary one. */
export function isPrimaryPlacement(p: Placement): boolean {
  return PRIMARY.has(p.kind);
}

export const REFERENCE_LINE_DESIGN = 'This phase produces the service contract';
export const REFERENCE_LINE_CONSTRUCTION = 'The contract this code implements';

const NONE: Placement = { kind: 'none' };

/**
 * The placement for a selection. Scoped by the SELECTED TASK's own phase (read
 * from the generated profile), never the row's current phase — the same rule
 * bodyDispatch.artifactRendererKeyFor keeps.
 */
export function placementFor(
  row: ConstructionRow | undefined,
  selection: LensSelection,
  join: ContractJoin | undefined
): Placement {
  if (row === undefined || join === undefined || join.kind === 'noComponent') return NONE;
  const task = selection.task;
  const phase = task !== undefined ? lifecyclePhaseOfTask(row, task) : selection.lifecyclePhase;
  const bare = task === undefined && selection.lifecyclePhase === undefined;

  if (row.kind === 'deployment') return deploymentPlacement(join, bare, phase, task);
  if (row.kind !== 'service' && row.kind !== 'frontend') return NONE;
  const service = row.kind === 'service';

  if (bare) {
    switch (join.kind) {
      case 'contract':
        return { kind: 'contractSummary' };
      case 'missing':
        return { kind: 'gapOverview' };
      case 'byDesign':
        return { kind: 'byDesignOverview' };
      case 'unresolved':
        return NONE;
    }
  }

  switch (phase) {
    case 'requirements':
      // The frontend's UX Requirements body waits on phaseArtifacts (the SPA slice).
      return service && task === 'srsReview' ? { kind: 'srsUnreadable' } : NONE;
    case 'detailed_design':
      if (task === 'someConstruction') {
        return join.kind === 'contract'
          ? { kind: 'contractReference', line: REFERENCE_LINE_DESIGN }
          : NONE;
      }
      return designPrimary(join);
    case 'test_plan':
      return join.kind === 'unresolved' ? NONE : { kind: 'componentTestPlan' };
    case 'construction':
      // The frontend's Construction task (and the phase itself) is its built
      // surfaces, said even when nothing has run; its review and client tests
      // keep their ordinary bodies until the SPA slice.
      if (!service) {
        return task === undefined || task === 'construction' ? { kind: 'frontendSurfaces' } : NONE;
      }
      if (task === 'codeReview') return { kind: 'codeReview' };
      return join.kind === 'contract'
        ? { kind: 'contractReference', line: REFERENCE_LINE_CONSTRUCTION }
        : NONE;
    case undefined:
    default:
      return NONE;
  }
}

function designPrimary(join: ContractJoin): Placement {
  switch (join.kind) {
    case 'contract':
      return { kind: 'contractFull' };
    case 'missing':
      return { kind: 'contractGap' };
    case 'byDesign':
      return { kind: 'byDesignSpec' };
    case 'unresolved':
    case 'noComponent':
      return { kind: 'contractUnresolved' };
  }
}

function deploymentPlacement(
  join: ContractJoin,
  bare: boolean,
  phase: string | undefined,
  task: string | undefined
): Placement {
  if (join.kind !== 'byDesign') return NONE;
  if (bare) return { kind: 'byDesignOverview' };
  // Provisioning Spec, at phase depth or its own task. Everything else in a
  // deployment profile stays cut for this stage (§10).
  if (phase === 'detailed_design' && (task === undefined || task === 'detailedDesign')) {
    return { kind: 'byDesignSpec' };
  }
  return NONE;
}

// ---------------------------------------------------------------------------
// What the focus view shows (§3)
// ---------------------------------------------------------------------------

/**
 * The artifact the focus view draws for a placement, or undefined when it has
 * nothing to show (then `focus=1` is ignored rather than opening an empty layer).
 *   contract       — the full ServiceContractView, whatever depth opened it
 *   relationships  — the architecture's view of a component with no contract
 *   testPlan       — the component test plan body
 */
export type FocusTarget = 'contract' | 'relationships' | 'testPlan';

export function focusTargetFor(
  placement: Placement,
  join: ContractJoin | undefined
): FocusTarget | undefined {
  if (placement.kind === 'none' || join === undefined) return undefined;
  if (placement.kind === 'componentTestPlan') return 'testPlan';
  if (
    placement.kind === 'srsUnreadable' ||
    placement.kind === 'contractUnresolved' ||
    placement.kind === 'frontendSurfaces'
  )
    return undefined;
  if (join.kind === 'contract') return 'contract';
  if (join.kind === 'missing' || join.kind === 'byDesign') return 'relationships';
  return undefined;
}

/** The focus view's role: the pane frame's own, except a REFERENCE stays one. */
export function focusRoleFor(placement: Placement, paneRole: ArtifactRole): ArtifactRole {
  return placement.kind === 'contractReference' || placement.kind === 'codeReview'
    ? 'reference'
    : paneRole;
}

// ---------------------------------------------------------------------------
// The frame's role label (§1)
// ---------------------------------------------------------------------------

/**
 * `reviewed` belongs to CODE alone: an observed Code Review attempt that is not
 * owed now reviewed a commit, and says which (codeReviewFrameFor). Code is never
 * COMMITTED NOW — there is no committed code artifact, only a commit an attempt
 * pointed at.
 */
export type ArtifactRole = 'underReview' | 'committedNow' | 'reference' | 'reviewed';

export const ROLE_LABEL: Record<ArtifactRole, string> = {
  underReview: 'UNDER REVIEW',
  committedNow: 'COMMITTED NOW',
  reference: 'REFERENCE',
  reviewed: 'REVIEWED',
};

/**
 * UNDER REVIEW is the one claim "this is the thing your Approve / Send back
 * decides", so it needs all of: a gate task selected, that gate OWED NOW (the
 * live workflow at it), and an OBSERVED current attempt — never a reconstructed
 * one, and never one "Observed only" stripped. Anything less is COMMITTED NOW.
 */
export function artifactRoleFor(args: {
  gateSelected: boolean;
  gateOwedNow: boolean;
  /** The gate task's current attempt's origin; undefined when none is recorded yet. */
  attemptOrigin: RecordOriginRow | undefined;
  /** Attempts "Observed only" set aside in this selection. */
  hiddenCount: number;
}): ArtifactRole {
  const observed = args.attemptOrigin === undefined || args.attemptOrigin === 'observed';
  return args.gateSelected && args.gateOwedNow && observed && args.hiddenCount === 0
    ? 'underReview'
    : 'committedNow';
}

// ---------------------------------------------------------------------------
// Source lines and the honesty sentences
// ---------------------------------------------------------------------------

export const OBSERVED_ONLY_SOURCE_SUFFIX =
  ' · shown under Observed only: project state, not evidence';

/**
 * `serviceContracts.<key> · current · no revision history recorded`, or
 * `· revision N of N`. Nothing here may claim authorship: `· written by attempt N`
 * needs a revision that records its attempt (earmark E7), and none does.
 */
export function contractSourceLine(
  contractKey: string,
  revisionCount: number,
  observedOnly: boolean
): string {
  const history =
    revisionCount === 0
      ? 'no revision history recorded'
      : `revision ${String(revisionCount)} of ${String(revisionCount)}`;
  return `serviceContracts.${contractKey} · current · ${history}${observedOnly ? OBSERVED_ONLY_SOURCE_SUFFIX : ''}`;
}

/** The architecture's own source line, for the relationships views. */
export function relationshipsSourceLine(observedOnly: boolean): string {
  return `system.relationships · committed architecture${observedOnly ? OBSERVED_ONLY_SOURCE_SUFFIX : ''}`;
}

/**
 * The one sentence between a RECONSTRUCTED provenance note and the contract
 * frame (§4.2): the frame shows today's contract, and nothing links it to the
 * attempt(s) above.
 */
export function reconstructedArtifactNote(scope: 'task' | 'wider', revisionCount: number): string {
  const link =
    scope === 'task'
      ? 'Nothing recorded links it to this attempt — the attempt was reconstructed'
      : 'Nothing recorded links it to the attempts here — they were reconstructed';
  const history =
    revisionCount === 0
      ? 'and the contract has no revision history'
      : 'and no revision records the attempt that wrote it';
  return `The contract below is the one committed today. ${link}, ${history}.`;
}

/**
 * The placements that show the committed contract itself — the subject of the
 * sentence above ("the contract below"). Anywhere else it would name a contract
 * the selection does not show.
 */
export function showsCommittedContract(placement: Placement): boolean {
  return placement.kind === 'contractSummary' || placement.kind === 'contractFull';
}

// ---------------------------------------------------------------------------
// Code Review's CODE block (designer check on renderers S1, B3)
// ---------------------------------------------------------------------------

/**
 * What Code Review may say about the code:
 *   - a RECONSTRUCTED attempt (or none, with nothing owed) reviewed nothing
 *     anyone watched: no CODE frame at all, one line — "No code view in this
 *     stage." A frame there claimed a review of a commit nobody reviewed;
 *   - OWED NOW on an observed attempt (or before any attempt): UNDER REVIEW;
 *   - OBSERVED and not owed: REVIEWED, sourced to the commit and the attempt,
 *     `evidence · git <sha> · attempt N`.
 * Never COMMITTED NOW: code is not committed project state here, a commit is
 * evidence an attempt pointed at.
 */
export type CodeReviewFrame =
  | { kind: 'noCodeView' }
  | {
      kind: 'frame';
      role: 'underReview' | 'reviewed';
      source: string;
      /** The commit, in mono; undefined when the attempt recorded none. */
      commit: string | undefined;
    };

export function codeReviewFrameFor(args: {
  /** The selected Code Review attempt's origin; undefined when none is recorded. */
  origin: RecordOriginRow | undefined;
  /** The gate is owed now — the live workflow stands at it. */
  owedNow: boolean;
  evidence: { kind: string; ref: string } | undefined;
  /** The selected attempt's 1-based number; undefined when none is recorded. */
  attempt: number | undefined;
}): CodeReviewFrame {
  const reconstructed = args.origin === 'backfilled' || args.origin === 'synthesized';
  if (reconstructed) return { kind: 'noCodeView' };
  const observed = args.origin === 'observed';
  if (!args.owedNow && !observed) return { kind: 'noCodeView' };
  const ev = args.evidence;
  const commit = ev !== undefined && ev.kind.length > 0 && ev.ref.length > 0 ? ev.ref : undefined;
  const attempt = args.attempt !== undefined ? ` · attempt ${String(args.attempt)}` : '';
  const source =
    commit !== undefined && ev !== undefined
      ? `evidence · ${ev.kind} ${commit}${attempt}`
      : `evidence · no commit recorded${attempt}`;
  return { kind: 'frame', role: args.owedNow ? 'underReview' : 'reviewed', source, commit };
}

export const SRS_UNREADABLE = 'The SRS is not readable in this console yet.';
export const NO_CODE_VIEW = 'No code view in this stage.';
export const NO_COMMIT_RECORDED = 'No commit is recorded for this review.';

// ---------------------------------------------------------------------------
// Gap copy (§5) — by design (muted) versus a real gap (awaiting ink)
// ---------------------------------------------------------------------------

export const GAP_LABEL_MISSING = 'NO CONTRACT COMMITTED';
export const GAP_LABEL_BY_DESIGN = 'NO CONTRACT · BY DESIGN';
export const GAP_LABEL_NO_TEST_PLAN = 'NO COMPONENT TEST PLAN RECORDED';
export const GAP_LABEL_UNRESOLVED = 'CONTRACT NOT JOINED';

/** One inbound edge of the architecture, as the gap sentence names it. */
export interface NamedOperation {
  label: string;
  calledBy: string;
}

/**
 * §5.1 — a component built by code with no committed contract. Every clause is
 * computed: `othersMissing` says whether "every other component built by an
 * activity has one" is still true, and the operations are the architecture's own
 * inbound edge labels, never invented.
 */
export function missingContractSentence(args: {
  componentId: string;
  contractKey: string | undefined;
  othersMissing: number;
  operations: readonly NamedOperation[];
}): string {
  const head =
    args.contractKey !== undefined
      ? `${args.componentId} names the contract key ${args.contractKey}, but the committed service contracts hold no entry for it.`
      : `${args.componentId} has no entry in the committed service contracts.`;
  const others =
    args.othersMissing === 0
      ? ' Every other component built by an activity has one, so this is missing data, not a design choice.'
      : ` It is one of ${String(args.othersMissing + 1)} components built by an activity with none; this is missing data, not a design choice.`;
  const ops =
    args.operations.length === 0
      ? ' The architecture names no operation on it.'
      : args.operations.length === 1
        ? ` The architecture names one operation on it: ${opText(args.operations[0])}.`
        : ` The architecture names ${String(args.operations.length)} operations on it: ${args.operations.map(opText).join('; ')}.`;
  return `${head}${others} Detailed Design cannot honestly pass without it.${ops}`;
}

function opText(op: NamedOperation | undefined): string {
  return op === undefined ? '' : `${op.label}, called by ${op.calledBy}`;
}

/** §5.2 — a Resource (or Utility): nothing is missing. */
export function byDesignSentence(componentKind: string): string {
  return componentKind === 'utility'
    ? 'A Utility is shared infrastructure, reached from every layer; it carries no service contract here. Nothing is missing.'
    : 'A Resource is provisioned, not coded. It is reached only through its ResourceAccess and carries no service contract. Nothing is missing. Provisioning specs have no view in this stage.';
}

export const UNRESOLVED_SENTENCE =
  'The committed activity list and architecture do not place this activity on a component the console can read, so no contract can be joined to it. That is a gap in the data this view reads, not a statement about whether a contract exists.';

/**
 * §5.4 — the component's own test plan. "Is not recorded", never "has not been
 * written": the console reads project state, not whether anyone wrote one
 * (designer check, polish 7). The backfill clause only where it is true. A
 * client's plan is its FLOWS — the Flows task — never "callees stubbed".
 */
export function noTestPlanSentence(
  stpReconstructed: boolean,
  variant: 'service' | 'frontend' = 'service'
): string {
  if (variant === 'frontend') {
    return (
      "This client's own flow plan — the user flows a harness walks through its surfaces — is not recorded." +
      (stpReconstructed
        ? " The Flows tasks' done-records were backfilled with no plan behind them."
        : '') +
      ' System test coverage for this client is shown below; it is not a substitute.'
    );
  }
  return (
    "This component's own test plan — scenarios where a harness drives its operations with its callees stubbed — is not recorded." +
    (stpReconstructed
      ? " The Test Plan tasks' done-records were backfilled with no plan behind them."
      : '') +
    ' System test coverage for this component is shown below; it is not a substitute.'
  );
}
