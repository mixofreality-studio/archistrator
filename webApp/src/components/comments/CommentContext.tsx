/* eslint-disable react-refresh/only-export-components -- provider + hook colocated */
/**
 * Anchored-comment accumulator for the System Design experience.
 *
 * The architect selects a location in the rendered artifact — a prose quote, a
 * diagram node/edge, or a scatter point — and attaches a comment. Each comment
 * carries a JSONPath `anchor` that references the spot in the TYPED head-state
 * model (ArtifactModelEnvelope.model). The server treats jsonPath as opaque
 * guidance (it does not evaluate it), so the scheme only needs to be stable and
 * human-meaningful. On "Send back" the accumulated comments are submitted as the
 * review `comments: AnchoredComment[]` array, which the Manager weaves beneath
 * the feedback into the architect-role redraft prompt.
 *
 * ── JSONPath anchoring scheme (per artifact kind) ───────────────────────────
 * Roots at `$` = the typed model payload for the active artifact kind.
 *
 *   mission              $.vision | $.mission | $.objectives[n]
 *   glossary             $.items[n]                  (n = glossary item index)
 *   scrubbedRequirements $.items[n]
 *   operationalConcepts  $.decisions[n]
 *   standardCheck        $.items[n]
 *   volatilities         $.items[n]                  (n = scatter-point index)
 *   coreUseCases         $.decisions[n].useCase                 (whole use case)
 *                        $.decisions[n].useCase.activity.nodes[m]  (a step node)
 *   system               $.components[id=<compId>]   (a C4 component)
 *                        $.relationships[from=<a>,to=<b>]          (a call edge)
 *                        $.dynamicViews[key=<k>].edges[seq=<n>]    (a sequence step)
 *   operationalConcepts  $.decisions[n]
 *                        $.deployment.environments[profile=<p>]..[name=<name>]  (a topology node)
 *
 * For free prose selection without a structured index we fall back to a section
 * anchor: `$..[?(section="<heading>")]` carrying the quoted text in the comment,
 * which is still meaningful to a human reader of the redraft prompt.
 */
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';
import type { AnchoredComment } from '../../api/types';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

/** A pending selection the architect may turn into an anchored comment. */
export interface Anchor {
  /** Discriminates the selection origin for the chip icon + copy. */
  kind: 'text' | 'node';
  /** Short human label shown on the chip (node name / quoted text). */
  label: string;
  /** Provenance, e.g. "Architecture · C4" or "Volatilities · axis map". */
  source: string;
  /** The JSONPath into the typed model this selection refers to. */
  jsonPath: string;
}

/**
 * One posted entry this gate cycle: the architect's text plus the location it
 * anchors to — or `null` anchor for FREE-FORM feedback typed without arming a
 * selection. Both ride the next "Send back": anchored entries become the wire
 * `comments`, free-form entries become the reject `feedback` notes.
 */
export interface PostedComment {
  text: string;
  anchor: Anchor | null;
}

interface CommentCtx {
  /** Entries accumulated this gate cycle (anchored + free-form), oldest first. */
  comments: PostedComment[];
  /** The currently-armed selection (drives the chat composer affordance). */
  anchor: Anchor | null;
  /** Arm/disarm a selection. Arming bumps `requestId` so the rail can open. */
  setAnchor: (a: Anchor | null) => void;
  /**
   * Commit `text` as a posted entry. With an armed anchor it becomes an anchored
   * comment (clears the anchor); with no anchor a non-empty `text` becomes a
   * free-form feedback note.
   */
  post: (text: string) => void;
  /** Drop a previously-posted entry by index. */
  remove: (index: number) => void;
  /** Clear all accumulated entries (after a successful send-back). */
  reset: () => void;
  /** Maps the ANCHORED entries into the wire AnchoredComment[] shape. */
  toWire: () => AnchoredComment[];
  /** The FREE-FORM entries joined into the reject `feedback` notes string. */
  freeformNotes: () => string;
  /** Monotonic counter; bumps whenever an anchor is armed. */
  requestId: number;
}

const Ctx = createContext<CommentCtx | null>(null);

export function useComments(): CommentCtx {
  const c = useContext(Ctx);
  if (c === null) throw new Error('useComments must be used within a CommentProvider');
  return c;
}

export function CommentProvider({ children }: { children: ReactNode }): ReactNode {
  const [comments, setComments] = useState<PostedComment[]>([]);
  const [armedAnchor, setArmedAnchor] = useState<Anchor | null>(null);
  const [requestId, setRequestId] = useState(0);

  const setAnchor = useCallback((a: Anchor | null): void => {
    setArmedAnchor(a);
    if (a !== null) setRequestId((n) => n + 1);
  }, []);

  const post = useCallback(
    (text: string): void => {
      const trimmed = text.trim();
      if (armedAnchor === null) {
        // Free-form feedback: only post when the architect actually typed something.
        if (trimmed.length === 0) return;
        setComments((prev) => [...prev, { text: trimmed, anchor: null }]);
        return;
      }
      const body = trimmed.length > 0 ? trimmed : `(comment on ${armedAnchor.label})`;
      setComments((prev) => [...prev, { text: body, anchor: armedAnchor }]);
      setArmedAnchor(null);
    },
    [armedAnchor]
  );

  const remove = useCallback((index: number): void => {
    setComments((prev) => prev.filter((_, i) => i !== index));
  }, []);

  const reset = useCallback((): void => {
    setComments([]);
    setArmedAnchor(null);
  }, []);

  const toWire = useCallback((): AnchoredComment[] => {
    const out: AnchoredComment[] = [];
    for (const c of comments) {
      if (c.anchor !== null) out.push({ jsonPath: c.anchor.jsonPath, text: c.text });
    }
    return out;
  }, [comments]);

  const freeformNotes = useCallback(
    (): string =>
      comments
        .filter((c) => c.anchor === null)
        .map((c) => c.text)
        .join('\n'),
    [comments]
  );

  const value = useMemo<CommentCtx>(
    () => ({
      comments,
      anchor: armedAnchor,
      setAnchor,
      post,
      remove,
      reset,
      toWire,
      freeformNotes,
      requestId,
    }),
    [comments, armedAnchor, setAnchor, post, remove, reset, toWire, freeformNotes, requestId]
  );

  return (
    <Ctx.Provider value={value}>
      {/* Invisible test probe: reflects the currently-armed anchor so black-box
          uitests (and headless smokes) can assert that ANY commentable surface —
          diagram edge/node, sequence step, deployment node, use case, or a text
          selection — armed its anchor, without depending on the ChatRail (which
          needs a live co-author session). Empty attributes when nothing is armed. */}
      <span
        data-anchor-label={armedAnchor?.label ?? ''}
        data-anchor-path={armedAnchor?.jsonPath ?? ''}
        data-anchor-source={armedAnchor?.source ?? ''}
        data-testid={UI_IDENTIFIERS.Comments.ARMED_ANCHOR}
        style={{ display: 'none' }}
      />
      {children}
    </Ctx.Provider>
  );
}

// ── JSONPath builders — the single source of the anchoring scheme ───────────

/** A prose section / quoted-text anchor for an artifact kind's markdown body. */
export function proseAnchor(kind: string, section: string): string {
  const safe = section.replace(/"/g, '\\"');
  return `$.${kind}..[?(section="${safe}")]`;
}

/** A volatility scatter-point anchor by its index in `items`. */
export function volatilityAnchor(index: number): string {
  return `$.items[${String(index)}]`;
}

/** A C4 component anchor by component id. */
export function componentAnchor(componentId: string): string {
  return `$.components[id=${componentId}]`;
}

/** A C4 relationship anchor by its endpoints. */
export function relationshipAnchor(from: string, to: string): string {
  return `$.relationships[from=${from},to=${to}]`;
}

/** A dynamic-view sequence-step anchor: a view's ordered call by 1-based seq. */
export function dynamicEdgeAnchor(viewKey: string, seq: number): string {
  return `$.dynamicViews[key=${viewKey}].edges[seq=${String(seq)}]`;
}

/** A deployment-topology node anchor by profile + node/instance name. */
export function deploymentAnchor(profile: string, name: string): string {
  const safe = name.replace(/"/g, '\\"');
  return `$.deployment.environments[profile=${profile}]..[name="${safe}"]`;
}

/** A use-case activity-node anchor within a use-case decision. */
export function activityNodeAnchor(useCaseIndex: number, nodeId: string): string {
  return `$.decisions[${String(useCaseIndex)}].useCase.activity.nodes[id=${nodeId}]`;
}

/** A whole use-case anchor. */
export function useCaseAnchor(useCaseIndex: number): string {
  return `$.decisions[${String(useCaseIndex)}].useCase`;
}

// ── Item-granularity builders for the CommentableList surfaces ───────────────
// One anchor per typed-model item. The server treats these as opaque, stable,
// human-meaningful pointers, so index-based paths are acceptable where the item
// carries no stable id; where it does (activity name, requirement id, option
// kind, component/signature), we prefer the id so the anchor survives a redraft
// that reorders items.

const q = (s: string): string => s.replace(/"/g, '\\"');

// Phase 1 — System Design list/prose kinds.
/** A mission business-objective by index → `$.objectives[n]`. */
export function missionObjectiveAnchor(index: number): string {
  return `$.objectives[${String(index)}]`;
}
/** A named mission prose section → `$.vision` / `$.mission`. */
export function missionProseAnchor(section: 'vision' | 'mission'): string {
  return `$.${section}`;
}
/** A glossary item by index → `$.items[n]`. */
export function glossaryItemAnchor(index: number): string {
  return `$.items[${String(index)}]`;
}
/** A scrubbed-requirement by stable id when present, else index → `$.items[…]`. */
export function scrubbedRequirementAnchor(index: number, id?: string): string {
  return id !== undefined && id !== '' ? `$.items[id="${q(id)}"]` : `$.items[${String(index)}]`;
}
/** A standard-check row by index → `$.items[n]`. */
export function standardCheckItemAnchor(index: number): string {
  return `$.items[${String(index)}]`;
}
/** An operational-concept decision by index → `$.decisions[n]`. */
export function operationalDecisionAnchor(index: number): string {
  return `$.decisions[${String(index)}]`;
}

// Phase 2 — Project Design.
/** A planning-assumptions risk flag by index → `$.notes.flags[n]`. */
export function planningFlagAnchor(index: number): string {
  return `$.notes.flags[${String(index)}]`;
}
/** A project activity by its (stable) name → `$.activities[name="…"]`. */
export function activityAnchor(name: string): string {
  return `$.activities[name="${q(name)}"]`;
}
/** A solution option, optionally drilling to a named knob/rate row. */
export function solutionAnchor(optionKind: string, knob?: string): string {
  const base = `$.options[kind=${optionKind}]`;
  return knob !== undefined && knob !== '' ? `${base}.${knob}` : base;
}
/** A risk-model option row by option kind → `$.options[kind=…]`. */
export function riskModelRowAnchor(optionKind: string): string {
  return `$.options[kind=${optionKind}]`;
}
/** An SDP-review option by option kind → `$.options[kind=…]`. */
export function sdpOptionAnchor(optionKind: string): string {
  return `$.options[kind=${optionKind}]`;
}

// Phase 3 — Construction.
/** A construction activity by ActivityID → `$.activityConstruction[id=…]`. */
export function activityConstructionAnchor(activityId: string): string {
  return `$.activityConstruction[id=${activityId}]`;
}
/** An intervention/gate decision by ActivityID → `$.interventions[activity=…]`. */
export function interventionAnchor(activityId: string): string {
  return `$.interventions[activity=${activityId}]`;
}
/** A service-contract operation by component + signature. */
export function contractOpAnchor(component: string, signature: string): string {
  return `$.serviceContracts[component=${component}].ops[signature="${q(signature)}"]`;
}
/** A test scenario/case step → `$.scenarios[id=…].cases[id=…].steps[seq]`. */
export function testScenarioStepAnchor(scenarioId: string, caseId: string, seq: number): string {
  return `$.scenarios[id=${scenarioId}].cases[id=${caseId}].steps[${String(seq)}]`;
}
