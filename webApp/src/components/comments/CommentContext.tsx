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
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import type { AnchoredComment } from '../../contracts/types';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

/** localStorage namespace for pending (client-side, unsent) send-back comments. */
const PENDING_STORAGE_PREFIX = 'aiarch.pendingComments';

function storageKeyFor(activeKey: string): string {
  return `${PENDING_STORAGE_PREFIX}.${activeKey}`;
}

/** Best-effort load of a slot's persisted pending entries (storage may be unavailable). */
function loadPending(activeKey: string): PostedComment[] {
  try {
    const raw = localStorage.getItem(storageKeyFor(activeKey));
    if (raw === null) return [];
    const parsed = JSON.parse(raw) as unknown;
    return Array.isArray(parsed) ? (parsed as PostedComment[]) : [];
  } catch {
    return [];
  }
}

/** Best-effort persist (empty list removes the slot so a cleared step leaves no orphan). */
function savePending(activeKey: string, list: PostedComment[]): void {
  try {
    if (list.length === 0) localStorage.removeItem(storageKeyFor(activeKey));
    else localStorage.setItem(storageKeyFor(activeKey), JSON.stringify(list));
  } catch {
    /* storage unavailable (private mode / quota) — pending stays in-memory only. */
  }
}

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
  /**
   * The anchored item's RENDERED text snapshot, sent to the review ledger as
   * `anchorText`. Optional at the arm sites: when omitted, `toWire` falls back to
   * `label` (which already carries the item's content for every arm surface).
   * SelectionPopover sets it to the full, untruncated quote.
   */
  anchorText?: string;
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
  /**
   * Whether commenting is active in this context. `true` inside the design /
   * construction review experiences; `false` for read-only renderings (the home
   * base). When `false`, `setAnchor` is a no-op, no test probe is emitted, and
   * every comment affordance renders nothing — so a read-only surface shows zero
   * comment UI (no icons, no hover chrome, no selection popover, no tab stops).
   */
  enabled: boolean;
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
  /**
   * Bind the accumulator to a (projectId, kind) localStorage slot. Loads that
   * slot's persisted pending entries and routes subsequent post/remove/reset
   * writes to it, so unsent notes survive a reload and switching artifact steps
   * swaps to that step's own pending set. A no-op on read-only surfaces.
   */
  setActiveKey: (key: string) => void;
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

export function CommentProvider({
  children,
  enabled = true,
}: {
  children: ReactNode;
  /** Defaults to active; pass `false` on read-only surfaces to suppress ALL comment UI. */
  enabled?: boolean;
}): ReactNode {
  const [comments, setComments] = useState<PostedComment[]>([]);
  const [armedAnchor, setArmedAnchor] = useState<Anchor | null>(null);
  const [requestId, setRequestId] = useState(0);
  // The (projectId, kind) localStorage slot the pending entries persist to. A ref
  // (not state) so post/remove/reset persist to the current slot synchronously
  // without re-subscribing every mutator on each key change.
  const activeKeyRef = useRef<string | null>(null);

  // Persist to the bound slot (no-op on read-only surfaces / before a key is set).
  const persist = useCallback(
    (list: PostedComment[]): void => {
      if (!enabled || activeKeyRef.current === null) return;
      savePending(activeKeyRef.current, list);
    },
    [enabled]
  );

  const setActiveKey = useCallback(
    (key: string): void => {
      if (!enabled || activeKeyRef.current === key) return;
      activeKeyRef.current = key;
      setComments(loadPending(key));
      setArmedAnchor(null);
    },
    [enabled]
  );

  const setAnchor = useCallback(
    (a: Anchor | null): void => {
      // Read-only surface: nothing may arm an anchor (belt-and-suspenders with the
      // affordances that don't render when disabled, e.g. a silent node-click arm).
      if (!enabled) return;
      setArmedAnchor(a);
      if (a !== null) setRequestId((n) => n + 1);
    },
    [enabled]
  );

  const post = useCallback(
    (text: string): void => {
      const trimmed = text.trim();
      let next: PostedComment[] | null = null;
      if (armedAnchor === null) {
        // Free-form feedback: only post when the architect actually typed something.
        if (trimmed.length === 0) return;
        next = [...comments, { text: trimmed, anchor: null }];
      } else {
        const body = trimmed.length > 0 ? trimmed : `(comment on ${armedAnchor.label})`;
        next = [...comments, { text: body, anchor: armedAnchor }];
        setArmedAnchor(null);
      }
      setComments(next);
      persist(next);
    },
    [armedAnchor, comments, persist]
  );

  const remove = useCallback(
    (index: number): void => {
      const next = comments.filter((_, i) => i !== index);
      setComments(next);
      persist(next);
    },
    [comments, persist]
  );

  const reset = useCallback((): void => {
    setComments([]);
    setArmedAnchor(null);
    persist([]);
  }, [persist]);

  const toWire = useCallback((): AnchoredComment[] => {
    const out: AnchoredComment[] = [];
    for (const c of comments) {
      if (c.anchor !== null) {
        // anchorText is the item's rendered-text snapshot; the label already carries
        // it for every arm surface, so fall back to it when no richer text was set.
        out.push({
          jsonPath: c.anchor.jsonPath,
          text: c.text,
          anchorText: c.anchor.anchorText ?? c.anchor.label,
        });
      }
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
      enabled,
      comments,
      anchor: armedAnchor,
      setAnchor,
      post,
      remove,
      reset,
      setActiveKey,
      toWire,
      freeformNotes,
      requestId,
    }),
    [
      enabled,
      comments,
      armedAnchor,
      setAnchor,
      post,
      remove,
      reset,
      setActiveKey,
      toWire,
      freeformNotes,
      requestId,
    ]
  );

  return (
    <Ctx.Provider value={value}>
      {/* Invisible test probe: reflects the currently-armed anchor so black-box
          uitests (and headless smokes) can assert that ANY commentable surface —
          diagram edge/node, sequence step, deployment node, use case, or a text
          selection — armed its anchor, without depending on the ChatRail (which
          needs a live co-author session). Empty attributes when nothing is armed.
          Suppressed entirely on read-only surfaces (enabled === false) so the DOM
          carries no comment-probe span there. */}
      {enabled ? (
        <span
          data-anchor-label={armedAnchor?.label ?? ''}
          data-anchor-path={armedAnchor?.jsonPath ?? ''}
          data-anchor-source={armedAnchor?.source ?? ''}
          data-testid={UI_IDENTIFIERS.Comments.ARMED_ANCHOR}
          style={{ display: 'none' }}
        />
      ) : null}
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
