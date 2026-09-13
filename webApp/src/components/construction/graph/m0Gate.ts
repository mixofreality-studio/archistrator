/**
 * M0 — the SDP review gate, and exactly what the ribbon may claim about it
 * (PM Q4 ruling, binding copy).
 *
 * STATE comes from `project.phase` — the same gate the pump already applies
 * (constructionmanager.go:815-822): `construction` is PASSED, an earlier phase
 * is NOT PASSED, an unreadable phase is "—". It is never inferred from the SDP
 * review's contents.
 *
 * STALENESS comes only from the SDP review slot's `staleBasis` and its
 * `staleBasisCause` — a separate flag that never blocks anything, in the house
 * stale vocabulary (StaleBasisChip: amber, "basis changed"), never red, never
 * the provenance hatch. Only the passed state carries it (the ruling's chip
 * list): a gate not yet passed has no approval to have drifted from.
 *
 * LEFT OFF: no date, no chosen option, no option durations, costs or risk. The
 * "Observed only" toggle does not touch M0 — its facts are the project's phase
 * and a slot flag, not evidence.
 *
 * Pure — no React — pinned by m0Gate.test.ts.
 */
import type { ArtifactKindFull, ProjectPhase } from '../../../contracts/types.ts';
import { METHOD_METADATA, PHASE2_ORDER } from '../../../contracts/methodMetadata.ts';

export type M0State = 'passed' | 'notPassed' | 'unknown';

/** The fields of a head-state slot M0 reads (ArtifactSlotView fits). */
export interface M0SlotLike {
  kind: string;
  staleBasis?: boolean;
  staleCauseKind?: string;
  staleCauseRevision?: number;
}

export interface M0Facts {
  state: M0State;
  /** The SDP review slot's `staleBasis` — a separate, non-blocking flag. */
  stale: boolean;
  /** "the activity list (revision 3)"; absent when the read model names no cause. */
  cause?: string;
  /** Project Design artifacts marked stale, the SDP review included. */
  staleCount: number;
}

export function m0StateOf(phase: ProjectPhase | undefined): M0State {
  switch (phase) {
    case 'construction':
      return 'passed';
    case 'systemDesign':
    case 'projectDesign':
      return 'notPassed';
    case 'unknown':
    case undefined:
      return 'unknown';
  }
}

/** "the activity list (revision 3)" — the upstream slot's own title, lower-cased. */
export function staleCauseText(kind: string, revision: number | undefined): string {
  const meta = (METHOD_METADATA as Partial<Record<string, { title: string }>>)[kind];
  const name = `the ${(meta?.title ?? kind).toLowerCase()}`;
  return revision !== undefined ? `${name} (revision ${String(revision)})` : name;
}

const PROJECT_DESIGN_KINDS: ReadonlySet<string> = new Set<ArtifactKindFull>(PHASE2_ORDER);

/** The M0 facts from the project read; an unreadable project reads "—". */
export function m0FactsFor(
  project: { phase: ProjectPhase; slots: readonly M0SlotLike[] } | undefined
): M0Facts {
  if (project === undefined) return { state: 'unknown', stale: false, staleCount: 0 };
  const sdp = project.slots.find((s) => s.kind === 'sdpReview');
  const stale = sdp?.staleBasis === true;
  const causeKind = sdp?.staleCauseKind;
  const causeRevision = sdp?.staleCauseRevision;
  return {
    state: m0StateOf(project.phase),
    stale,
    ...(stale && causeKind !== undefined && causeKind.length > 0
      ? { cause: staleCauseText(causeKind, causeRevision) }
      : {}),
    staleCount: project.slots.filter(
      (s) => PROJECT_DESIGN_KINDS.has(s.kind) && s.staleBasis === true
    ).length,
  };
}

export const M0_STALE_LABEL = 'basis changed';
export const M0_OPEN_SDP_REVIEW = 'Open the SDP review →';

export interface M0Presentation {
  state: M0State;
  /** Shown only on a passed gate. */
  stale: boolean;
  stateText: 'passed' | 'not passed' | '—';
  gatesText: string;
  /** The chip, in order; the amber icon sits before `basis changed`. */
  chipParts: string[];
  title: string;
  body: string;
  /** Navigation only, and only while the approval is stale. */
  link?: string;
}

export function m0PresentationFor(facts: M0Facts, gates: number): M0Presentation {
  const n = String(gates);
  const gatesText = `gates ${n}`;
  switch (facts.state) {
    case 'passed': {
      if (!facts.stale) {
        return {
          state: 'passed',
          stale: false,
          stateText: 'passed',
          gatesText,
          chipParts: ['M0', 'SDP review', 'passed', gatesText],
          title: 'M0 — SDP review passed',
          body: `Phase 2 is sealed, so construction is authorized. ${n} activities start here.`,
        };
      }
      const cause = facts.cause ?? 'an upstream artifact';
      return {
        state: 'passed',
        stale: true,
        stateText: 'passed',
        gatesText,
        chipParts: ['M0', 'SDP review', 'passed', M0_STALE_LABEL, gatesText],
        title: 'M0 — SDP review passed; the plan has changed since',
        body:
          `Phase 2 is sealed, so construction is authorized. Since then, ${cause} was amended, ` +
          `and ${String(facts.staleCount)} Project Design artifacts, including the SDP review, ` +
          'are marked stale. Their durations, costs and risk no longer describe the plan being ' +
          'built. This does not block construction. To bring the approval back in line, ' +
          'reconcile the SDP review by amendment, or mark it reviewed — unaffected.',
        link: M0_OPEN_SDP_REVIEW,
      };
    }
    case 'notPassed':
      return {
        state: 'notPassed',
        stale: false,
        stateText: 'not passed',
        gatesText,
        chipParts: ['M0', 'SDP review', 'not passed', gatesText],
        title: 'M0 — SDP review not passed',
        body: `Construction cannot start until Phase 2 is sealed at the SDP review. ${n} activities wait on it.`,
      };
    case 'unknown':
      return {
        state: 'unknown',
        stale: false,
        stateText: '—',
        gatesText,
        chipParts: ['M0', 'SDP review', '—', gatesText],
        title: 'M0 — state not available',
        body: "The project's phase could not be read, so no state is shown rather than a guess.",
      };
  }
}
