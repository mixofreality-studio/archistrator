/**
 * Choose-your-path walkthrough of a use case's activity diagram. One node is in
 * focus at a time (large + readable — which also sidesteps the fit-to-view
 * shrink that makes the whole-graph view illegible); the reader advances with a
 * "Next" step, and at every decision/fork the outgoing edges become branch
 * buttons (labeled by their guard) so the reader picks the path. The route taken
 * is a breadcrumb (click to rewind) and is mirrored live onto the activity
 * diagram beside it as a "you-are-here" map (current node ringed, path
 * emphasized, rest dimmed). Bound to a UseCaseView (adapters.toCoreUseCasesView).
 *
 * It is also the STEPPER other surfaces are driven by: `onCurrentNodeChange`
 * publishes the focused activity node, which the Architecture step's Dynamic
 * lens uses to light that step's fragment of the realized call chain (founder QA
 * round 2 — the same controls, now walking two diagrams at once), and
 * `onPathChange` publishes the whole route behind it, which the same lens turns
 * into a lit trail of everything already walked (founder QA round 4).
 */
import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import ButtonBase from '@mui/material/ButtonBase';
import Chip from '@mui/material/Chip';
import Collapse from '@mui/material/Collapse';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import ArrowForwardIcon from '@mui/icons-material/ArrowForward';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import UndoIcon from '@mui/icons-material/Undo';
import RestartAltIcon from '@mui/icons-material/RestartAlt';
import FlagIcon from '@mui/icons-material/Flag';
import type { UseCaseView } from '../../contracts/adapters';
import type { Finding } from '../../contracts/types';
import type { RealizedStep } from '../../contracts/realization';
import { ActivityFlow, type ActivityHighlight } from './ActivityFlow';
import { useComments, activityNodeAnchor } from '../comments/CommentContext';
import { laneColors } from './laneColors';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { walkthroughRoots, walkthroughNavFloor } from './walkthroughRoots';
import { stepBadgeState, toneColor } from './useCaseChip';
import { StepLink } from '../shared/StepLink';

type NodeView = UseCaseView['nodes'][number];
type EdgeView = UseCaseView['edges'][number];

/** Human header for a node kind. Actions have no header — their label speaks. */
const KIND_HEADER: Record<string, string> = {
  start: 'Start',
  end: 'End',
  merge: 'Paths merge',
  fork: 'Parallel split',
  join: 'Parallel join',
  decision: 'Decision',
  switch: 'Decision',
  timeEvent: 'Time event',
  acceptEvent: 'Event received',
};

/** ~2 wrapped rows of the breadcrumb's 11px chips (fix 8, founder QA round 5):
 *  the path trail otherwise grows without bound as the reader advances,
 *  eventually pushing the map (or, in the trace embedding, everything below
 *  it) off-screen. Collapse's `collapsedSize` clips to this and a "Show full
 *  path" toggle appears only once the trail actually exceeds it. */
const PATH_COLLAPSED_HEIGHT = 44;

/** Fallback text when a node (start/end/merge) carries no label of its own. */
function nodeText(n: NodeView): string {
  if (n.label.trim().length > 0) return n.label;
  return KIND_HEADER[n.kind] ?? n.kind;
}

/** A branch button's label: the guard (brackets stripped) or the destination. */
function branchLabel(e: EdgeView, nodesById: Map<string, NodeView>): string {
  const g = e.guard.replace(/[[\]]/g, '').trim();
  if (g.length > 0) return g;
  const tgt = nodesById.get(e.to);
  return tgt !== undefined ? nodeText(tgt) : 'this path';
}

/** The realization badge shown on the focus card for the CURRENT node — thin
 *  data marshalling over useCaseChip's pure `stepBadgeState` (which gates on
 *  eligibility FIRST: a decision/switch node carrying a realized step is still
 *  not badge-worthy). `stepFindings` is only invoked once a step is known to
 *  exist, avoiding a wasted lookup for nodes with none. */
function stepBadge(
  node: NodeView | undefined,
  realization: Map<string, RealizedStep>,
  stepFindings: (nodeId: string) => Finding[]
): { label: string; tone: 'ok' | 'warn' | 'error' } | undefined {
  if (node === undefined) return undefined;
  const realized = realization.get(node.id);
  const findings = realized !== undefined ? stepFindings(node.id) : [];
  return stepBadgeState(node.kind, realized, findings);
}

export function UseCaseWalkthrough({
  uc,
  useCaseIndex,
  height = 580,
  realization,
  stepFindings,
  callChainKey,
  firstSeqOfNode,
  initialPath: seedPath,
  hideCallChainLink = false,
  hideMap = false,
  compactCalls = false,
  onCurrentNodeChange,
  onPathChange,
}: {
  uc: UseCaseView;
  useCaseIndex: number;
  /** A fixed pixel height (the historical default) or a CSS length, forwarded
   *  to the you-are-here map (ActivityFlow) — the Architecture lens' trace
   *  passes a viewport-relative `clamp(...)` (founder QA round 5). */
  height?: number | string;
  /** This use case's realized calls, keyed by activity node id (empty map when
   *  the use case links no dynamic view, or none of its steps are realized). */
  realization: Map<string, RealizedStep>;
  /** Design-Health findings anchored to one step (activity node) of this use
   *  case's dynamic view — [] when the node carries none (or no view exists). */
  stepFindings: (nodeId: string) => Finding[];
  /** The System dynamic-view key this use case's call chain renders under, for
   *  the "View call chain" join — undefined when no such view exists. */
  callChainKey: string | undefined;
  /** The realized call chain's 1-based GLOBAL sequence position of a given
   *  step's first call, for the call-chain join's `?step=` deep link —
   *  undefined when the node has no step, or no call chain exists at all. */
  firstSeqOfNode: (nodeId: string) => number | undefined;
  /** Optional route to OPEN on (walkthroughPathTo), for a caller landing the
   *  reader mid-diagram — the Architecture lens' `?step=` deep link. Consumed
   *  once, on mount: remount (a `key` change) to land somewhere else. Restart
   *  still returns to the natural beginning, not here. */
  initialPath?: readonly string[];
  /** Suppress the focus card's "View call chain →" join. Set by a host that IS
   *  the call chain (the Architecture lens' walkthrough-driven trace), where the
   *  link would navigate to the screen the reader is already on. */
  hideCallChainLink?: boolean;
  /** Collapse the you-are-here map behind a "Show activity map ▾" text toggle
   *  (closed by default) instead of always rendering its canvas. Set by the
   *  Architecture lens' walkthrough-driven trace, where the map duplicates
   *  the call chain beside it and the always-on canvas was the single
   *  largest claim on vertical space above the fold (founder QA round 5).
   *  The standalone Use Cases screen leaves this false — the map stays
   *  always-on there, unchanged. */
  hideMap?: boolean;
  /** Collapse the current step's per-call list to a single "N calls →"
   *  summary chip (styled like the STEP_BADGE realization chip) instead of a
   *  full from → to · label row per call. Set by the Architecture lens'
   *  trace, where the call chain's own FragmentBar caption is the single
   *  source of call detail and the focus card's full list was pure
   *  duplication (founder QA round 5). The standalone screen leaves this
   *  false — the full list stays, unchanged. */
  compactCalls?: boolean;
  /** Optional notification of the step now in focus — the activity node id, or
   *  '' while the multi-root entry chooser is up (no step chosen yet). Fired on
   *  mount and on every path change, so a driven surface (the Architecture lens'
   *  call-chain fragment) follows the walk. Keep the handler stable
   *  (useCallback) — it is an effect dependency. */
  onCurrentNodeChange?: ((nodeId: string) => void) | undefined;
  /** Optional notification of the whole ROUTE walked so far — the breadcrumb
   *  path, oldest first, ending on the node in focus; empty while the
   *  multi-root entry chooser is up. Fired on mount and on every path change,
   *  exactly like `onCurrentNodeChange` (which reports only its last element).
   *  A driven surface uses it to accrete: the Architecture lens' call chain
   *  keeps every call authored on a node the reader has already LEFT lit as a
   *  visited trail (founder QA round 4). Because it is the path — not a
   *  separate accumulator — Back and Restart shrink the trail for free. Keep
   *  the handler stable (useCallback): it is an effect dependency. */
  onPathChange?: ((nodeIds: string[]) => void) | undefined;
}): ReactNode {
  const t = useTokens();
  const colors = laneColors(t, uc.lanes);
  const { setAnchor, enabled, anchor: armedAnchor, comments } = useComments();

  // Roots: every `start` node plus every edge-less (in-degree-0) node — an
  // activity can begin either at its literal start pseudostate or at an
  // accept/time-event that has no incoming edge. A cyclic diagram with no
  // start and no in-degree-0 node (a degenerate/malformed case) falls back to
  // the first node so the walkthrough always has somewhere to begin.
  const { nodesById, outEdges, roots } = useMemo(() => {
    const byId = new Map<string, NodeView>(uc.nodes.map((n) => [n.id, n]));
    const outs = new Map<string, EdgeView[]>();
    for (const e of uc.edges) {
      const arr = outs.get(e.from) ?? [];
      arr.push(e);
      outs.set(e.from, arr);
    }
    const detected = walkthroughRoots(uc.nodes, uc.edges);
    const first = uc.nodes[0];
    const rootIds = detected.length > 0 ? detected : first !== undefined ? [first.id] : [];
    return { nodesById: byId, outEdges: outs, roots: rootIds };
  }, [uc]);

  // A single root behaves exactly as before (auto-focused as step 1). Multiple
  // roots start with no step chosen — the initial focus card becomes an entry
  // chooser instead. This is also where Restart returns to: a seeded landing
  // (below) is where the reader came IN, not the beginning of the story.
  const initialPath = roots.length === 1 ? roots : [];
  const [path, setPath] = useState<string[]>(() =>
    seedPath !== undefined && seedPath.length > 0 ? [...seedPath] : initialPath
  );
  // Back/Restart rewind floor: single-root diagrams can't go below the start
  // (path length 1); multi-root diagrams can rewind all the way back to the
  // entry chooser (path length 0), since re-choosing the beginning is itself
  // a legal move once more than one beginning exists.
  const navFloor = walkthroughNavFloor(roots.length);

  // Container-aware layout: MUI viewport breakpoints can't see that this widget
  // lives inside a narrow hero (a 300px meta sidebar already eats the row), which
  // is what squeezed the you-are-here map down to a ~100px strip with clipped
  // text. Measure our *own* width and, when the row can't give the map a usable
  // share, stack the map full-width beneath the controls instead.
  const rootRef = useRef<HTMLDivElement>(null);
  // Focus target after every step change: the controls that were clicked (Next /
  // a branch / Back / Restart) can unmount or disable on advance, which would
  // silently drop keyboard focus to <body>. The step title is the stable landing
  // spot, and its role="status" live region announces the new step to AT.
  const stepTitleRef = useRef<HTMLElement>(null);
  const [wide, setWide] = useState(true);
  useEffect(() => {
    const el = rootRef.current;
    if (el === null) return undefined;
    const ro = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry !== undefined) setWide(entry.contentRect.width >= 780);
    });
    ro.observe(el);
    return (): void => {
      ro.disconnect();
    };
  }, []);

  // hideMap (fix 3): the you-are-here map opens closed in the trace embedding.
  // Irrelevant when hideMap is false (the map always renders) — kept as plain
  // state either way since it costs nothing unused.
  const [mapOpen, setMapOpen] = useState(false);
  // LAZY MOUNT (fix-round-1 Finding 1): MUI's Collapse hides its children via
  // height/overflow — it never unmounts them — so an always-mounted
  // <Collapse><ActivityFlow/></Collapse> ran a full second invisible
  // React-Flow instance (layout, ResizeObserver, fitView) on every render of
  // the trace, confirmed by the fix-1..8 verification's own measurement JSON
  // (activityCanvasRect.height=403 at every step, collapsed or not). The
  // ActivityFlow subtree now renders only once the reader has opened it at
  // least once; after that first open it stays mounted (never reverts to
  // "not yet opened") so re-collapsing keeps the Collapse animation and the
  // camera's fitted state instead of remounting from scratch.
  const [mapEverOpened, setMapEverOpened] = useState(false);
  // Stable ids (fix-round-1 MINOR) linking each disclosure toggle to the
  // Collapse region it controls, for aria-controls.
  const mapRegionId = useId();
  const pathRegionId = useId();

  // hideMap /path breadcrumb cap (fix 8): whether the path trail's natural
  // height exceeds its 2-line collapsed cap — measured (not computed), the
  // same ResizeObserver-threshold idiom as `wide`/`sideBySide` above, since
  // whether wrapped text overflows a fixed height depends on the rendered
  // font metrics and container width, not a value derivable from `path`
  // alone. Drives whether the "Show full path" toggle appears at all.
  const pathBoxRef = useRef<HTMLDivElement>(null);
  const [pathOverflowing, setPathOverflowing] = useState(false);
  const [pathExpanded, setPathExpanded] = useState(false);
  useEffect(() => {
    const el = pathBoxRef.current;
    if (el === null) return undefined;
    const check = (): void => {
      setPathOverflowing(el.scrollHeight > PATH_COLLAPSED_HEIGHT + 1);
    };
    check();
    const ro = new ResizeObserver(check);
    ro.observe(el);
    return (): void => {
      ro.disconnect();
    };
  }, [path]);

  // No highlight at all while the path is empty (the multi-root entry chooser):
  // an empty ActivityHighlight would dim every node/edge to 25% with nothing
  // ringed, which is illegible right when the reader most needs the map to
  // orient them among the candidate entries.
  const highlight: ActivityHighlight | undefined = useMemo(() => {
    if (path.length === 0) return undefined;
    const visitedEdges = new Set<string>();
    for (let k = 0; k < path.length - 1; k++) {
      visitedEdges.add(`${path[k] ?? ''}-${path[k + 1] ?? ''}`);
    }
    return {
      current: path[path.length - 1] ?? '',
      visitedNodes: new Set(path),
      visitedEdges,
    };
  }, [path]);

  // With multiple roots, the walkthrough opens on no step at all — the entry
  // chooser occupies the focus card until the reader picks a starting event.
  const showEntryChooser = path.length === 0 && roots.length > 1;
  const currentId = path[path.length - 1] ?? '';

  // Publish the focused step to a host that follows the walk (the Architecture
  // lens' dynamic trace, which lights that step's fragment of the call chain).
  // '' while the entry chooser is up: no step is chosen yet, and holding a stale
  // id would light a fragment the reader has not walked to.
  useEffect(() => {
    onCurrentNodeChange?.(showEntryChooser ? '' : currentId);
  }, [onCurrentNodeChange, showEntryChooser, currentId]);

  // …and the whole route behind it, for a host that accretes rather than
  // replaces (the Dynamic lens' visited call trail). Same construction as the
  // publish above — `path` is state, so this fires exactly when the walk moves
  // and never loops. A fresh array per publish keeps the state the host stores
  // from aliasing ours.
  useEffect(() => {
    onPathChange?.(showEntryChooser ? [] : [...path]);
  }, [onPathChange, showEntryChooser, path]);

  if (uc.nodes.length === 0) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        This use case has no activity diagram to walk through.
      </Box>
    );
  }

  const node = nodesById.get(currentId);
  const outs = outEdges.get(currentId) ?? [];
  const kindHeader = node !== undefined ? KIND_HEADER[node.kind] : undefined;
  const isBranch = outs.length > 1;
  const isEnd = !showEntryChooser && outs.length === 0;

  // Realization badge + calls list for the current node — both naturally absent
  // during the entry chooser (currentId is '', which no node/step ever keys).
  const badge = stepBadge(node, realization, stepFindings);
  const realizedStep = realization.get(currentId);
  const firstSeq = realizedStep !== undefined ? firstSeqOfNode(currentId) : undefined;

  // Per-step comment button, revealed like a CommentableList row's: hidden at rest,
  // shown on hover / keyboard focus-within of the step card, and pinned visible when
  // this step is the armed anchor or already carries a comment. It arms the SAME
  // anchor as the card's drag-select-to-quote path (activityNodeAnchor on the current
  // node) so a reviewer needn't select text to comment on the whole step.
  const stepAnchorPath = activityNodeAnchor(useCaseIndex, currentId);
  const stepArmed = armedAnchor?.jsonPath === stepAnchorPath;
  const stepHasComments = comments.some((c) => c.anchor?.jsonPath === stepAnchorPath);
  const stepRevealed = stepArmed || stepHasComments;

  const advance = (toId: string): void => {
    setPath((p) => [...p, toId]);
    stepTitleRef.current?.focus();
  };

  return (
    <Box ref={rootRef} sx={{ display: 'flex', gap: 2, flexDirection: wide ? 'row' : 'column' }}>
      {/* focused current step + controls */}
      <Box
        sx={{
          width: wide ? 380 : '100%',
          flexShrink: 0,
          display: 'flex',
          flexDirection: 'column',
          gap: 1.5,
        }}
      >
        {/* data-commentable + data-comment-anchor: highlighting text in this card
            arms the SAME anchor as clicking the current node on the diagram. */}
        <Paper
          data-artifact-kind="coreUseCases"
          data-comment-anchor={stepAnchorPath}
          data-commentable={`${uc.name} · activity diagram`}
          sx={{
            p: 2.5,
            display: 'flex',
            flexDirection: 'column',
            gap: 1.5,
            minHeight: 210,
            // Capped + internally scrollable (fix 4, founder QA round 5): a long
            // call list (before `compactCalls` collapses it) or step label could
            // otherwise push the Next/branch controls below the fold. Those
            // controls are pinned sticky (below) so they stay reachable even
            // while this card scrolls.
            maxHeight: 'min(46vh, 420px)',
            overflowY: 'auto',
            '& .walkthrough-step-action': {
              opacity: stepRevealed ? 1 : 0,
              transition: 'opacity 120ms',
            },
            '&:hover .walkthrough-step-action, &:focus-within .walkthrough-step-action': {
              opacity: 1,
            },
            '@media (hover: none)': { '& .walkthrough-step-action': { opacity: 1 } },
          }}
        >
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Typography
              sx={{
                fontFamily: t.mono,
                fontSize: 11,
                letterSpacing: '0.08em',
                textTransform: 'uppercase',
                color: t.muted,
              }}
            >
              {showEntryChooser
                ? 'Entry'
                : `${kindHeader !== undefined ? `${kindHeader} · ` : ''}step ${String(path.length)}`}
            </Typography>
            <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: 1 }}>
              {node !== undefined && (
                <Chip
                  label={node.lane}
                  size="small"
                  sx={{
                    height: 20,
                    fontFamily: t.mono,
                    fontSize: 10,
                    bgcolor: t.paperAlt,
                    borderLeft: `4px solid ${colors[node.lane] ?? t.muted}`,
                    borderRadius: 0.5,
                  }}
                />
              )}
              {badge !== undefined ? (
                <Chip
                  data-testid={UI_IDENTIFIERS.UseCaseCarousel.STEP_BADGE}
                  label={badge.label}
                  size="small"
                  sx={{
                    height: 20,
                    fontFamily: t.mono,
                    fontSize: 10,
                    fontWeight: 700,
                    bgcolor: 'transparent',
                    color: toneColor(badge.tone, t),
                    border: `1.5px solid ${toneColor(badge.tone, t)}`,
                    borderRadius: 0.5,
                  }}
                />
              ) : null}
              {enabled && !showEntryChooser ? (
                <Tooltip title={`Comment on step ${String(path.length)}`}>
                  <IconButton
                    aria-label={`Comment on step ${String(path.length)}`}
                    className="walkthrough-step-action"
                    size="small"
                    sx={{
                      flexShrink: 0,
                      color: t.accentText,
                      bgcolor: t.accent,
                      border: `1.5px solid ${t.line}`,
                      borderRadius: 1,
                      '&:hover': { bgcolor: t.accent2 },
                    }}
                    onClick={() => {
                      setAnchor({
                        kind: 'node',
                        label: node !== undefined ? nodeText(node) : `Step ${String(path.length)}`,
                        source: `${uc.name} · activity diagram`,
                        jsonPath: stepAnchorPath,
                      });
                    }}
                  >
                    <ChatBubbleOutlineIcon sx={{ fontSize: 15 }} />
                  </IconButton>
                </Tooltip>
              ) : null}
            </Box>
          </Box>

          <Typography
            aria-live="polite"
            ref={stepTitleRef}
            role="status"
            sx={{
              fontFamily: t.body,
              fontWeight: 700,
              fontSize: 20,
              lineHeight: 1.25,
              color: t.ink,
            }}
            tabIndex={-1}
          >
            {showEntryChooser
              ? 'How does this use case begin?'
              : node !== undefined
                ? nodeText(node)
                : '—'}
          </Typography>

          {/* realized calls for the current step, when it has one — the
              from → to · label list plus a join into the Architecture step's
              Dynamic lens, landing on this exact step (?step= = its global seq). */}
          {realizedStep !== undefined ? (
            <Box
              data-testid={UI_IDENTIFIERS.UseCaseCarousel.STEP_CALLS}
              sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}
            >
              {compactCalls ? (
                // The call chain's own FragmentBar caption is the single source
                // of call detail in this embedding (the Architecture lens'
                // trace) — the focus card just names how many (fix 6, founder
                // QA round 5), styled like the STEP_BADGE realization chip. No
                // trailing arrow (fix-round-1 Finding 2): this chip is not a
                // link — nothing here navigates or expands — and an arrow
                // reads as a broken promise in a file where the identical
                // glyph IS a real link (the "View call chain →" StepLink
                // below, and the non-compact "N calls →"-shaped affordance
                // would otherwise imply). Plain count, no punctuation to
                // misread as an affordance.
                <Chip
                  label={`${String(realizedStep.calls.length)} call${
                    realizedStep.calls.length === 1 ? '' : 's'
                  }`}
                  size="small"
                  sx={{
                    alignSelf: 'flex-start',
                    height: 20,
                    fontFamily: t.mono,
                    fontSize: 10,
                    fontWeight: 700,
                    bgcolor: 'transparent',
                    color: t.muted,
                    border: `1.5px solid ${t.line}`,
                    borderRadius: 0.5,
                  }}
                />
              ) : (
                realizedStep.calls.map((c, idx) => (
                  <Typography
                    key={`${c.from}-${c.to}-${String(idx)}`}
                    sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}
                  >
                    {c.from} → {c.to} · {c.label}
                  </Typography>
                ))
              )}
              {callChainKey !== undefined && !hideCallChainLink ? (
                <StepLink
                  kind="system"
                  label={`${uc.name} call chain`}
                  search={
                    firstSeq !== undefined
                      ? { view: callChainKey, step: firstSeq }
                      : { view: callChainKey }
                  }
                  sx={{ fontFamily: t.mono, fontSize: 11 }}
                >
                  View call chain →
                </StepLink>
              ) : null}
            </Box>
          ) : null}

          {/* controls — pinned to the bottom of the (now internally scrollable)
              focus card so Next/branch buttons never scroll out of view
              (fix 4, founder QA round 5). `mt: 'auto'` still pushes the block
              to the bottom in the common case where the card's own content is
              shorter than its cap; `position: sticky` is what keeps it in
              place once the card actually scrolls. */}
          <Box sx={{ mt: 'auto', position: 'sticky', bottom: 0, bgcolor: t.paper, pt: 1 }}>
            {showEntryChooser ? (
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75 }}>
                {roots.map((rootId) => {
                  const n = nodesById.get(rootId);
                  return (
                    <Button
                      data-testid={UI_IDENTIFIERS.UseCaseCarousel.walkthroughBranch(
                        `entry-${rootId}`
                      )}
                      key={rootId}
                      sx={{
                        justifyContent: 'flex-start',
                        textAlign: 'left',
                        textTransform: 'none',
                        color: t.ink,
                        borderColor: t.line,
                        '&:hover': { borderColor: t.accent, bgcolor: t.paperAlt },
                      }}
                      variant="outlined"
                      onClick={() => {
                        advance(rootId);
                      }}
                    >
                      <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13 }}>
                        {n !== undefined ? nodeText(n) : rootId}
                      </Typography>
                    </Button>
                  );
                })}
              </Box>
            ) : isEnd ? (
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <FlagIcon sx={{ fontSize: 18, color: t.committedDot }} />
                <Typography sx={{ color: t.muted, fontSize: 13, fontFamily: t.mono }}>
                  End of this path.
                </Typography>
              </Box>
            ) : isBranch ? (
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75 }}>
                <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
                  Which branch?
                </Typography>
                {outs.map((e) => {
                  const tgt = nodesById.get(e.to);
                  return (
                    <Button
                      data-testid={UI_IDENTIFIERS.UseCaseCarousel.walkthroughBranch(
                        `${e.from}-${e.to}`
                      )}
                      key={`${e.from}-${e.to}`}
                      sx={{
                        justifyContent: 'flex-start',
                        textAlign: 'left',
                        textTransform: 'none',
                        color: t.ink,
                        borderColor: t.line,
                        '&:hover': { borderColor: t.accent, bgcolor: t.paperAlt },
                      }}
                      variant="outlined"
                      onClick={() => {
                        advance(e.to);
                      }}
                    >
                      <Box>
                        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13 }}>
                          {branchLabel(e, nodesById)}
                        </Typography>
                        <Typography sx={{ fontSize: 11, color: t.muted }}>
                          → {tgt !== undefined ? nodeText(tgt) : e.to}
                        </Typography>
                      </Box>
                    </Button>
                  );
                })}
              </Box>
            ) : (
              <Button
                data-testid={UI_IDENTIFIERS.UseCaseCarousel.WALKTHROUGH_NEXT}
                endIcon={<ArrowForwardIcon />}
                sx={{ alignSelf: 'flex-start', textTransform: 'none' }}
                variant="contained"
                onClick={() => {
                  const next = outs[0];
                  if (next !== undefined) advance(next.to);
                }}
              >
                Next
              </Button>
            )}
          </Box>
        </Paper>

        {/* nav: back / restart */}
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button
            data-testid={UI_IDENTIFIERS.UseCaseCarousel.WALKTHROUGH_BACK}
            disabled={path.length <= navFloor}
            size="small"
            startIcon={<UndoIcon sx={{ fontSize: 15 }} />}
            sx={{ color: t.ink, textTransform: 'none' }}
            onClick={() => {
              setPath((p) => (p.length > navFloor ? p.slice(0, -1) : p));
              stepTitleRef.current?.focus();
            }}
          >
            Back
          </Button>
          <Button
            data-testid={UI_IDENTIFIERS.UseCaseCarousel.WALKTHROUGH_RESTART}
            disabled={path.length <= navFloor}
            size="small"
            startIcon={<RestartAltIcon sx={{ fontSize: 15 }} />}
            sx={{ color: t.ink, textTransform: 'none' }}
            onClick={() => {
              setPath(initialPath);
              stepTitleRef.current?.focus();
            }}
          >
            Restart
          </Button>
        </Box>

        {/* breadcrumb trail (click a step to rewind) — nothing to show yet
            while the entry chooser is up (path is empty). Capped to ~2 wrapped
            lines (fix 8, founder QA round 5): an uncapped trail grows without
            bound as the reader advances, eventually pushing everything below
            it off-screen in EITHER embedding (the standalone screen and the
            Architecture lens' trace both render this same component). The
            "Show full path" toggle only appears once the trail actually
            exceeds the cap (`pathOverflowing`, measured above). */}
        {path.length > 0 && (
          <Paper sx={{ p: 1.5, bgcolor: t.paperAlt }}>
            <Typography
              sx={{
                fontFamily: t.mono,
                fontSize: 10,
                letterSpacing: '0.08em',
                textTransform: 'uppercase',
                color: t.muted,
                mb: 0.75,
              }}
            >
              Path
            </Typography>
            <Collapse collapsedSize={PATH_COLLAPSED_HEIGHT} id={pathRegionId} in={pathExpanded}>
              <Box
                ref={pathBoxRef}
                sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 0.5 }}
              >
                {path.map((id, idx) => {
                  const n = nodesById.get(id);
                  const last = idx === path.length - 1;
                  return (
                    <Box
                      key={`${id}-${String(idx)}`}
                      sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}
                    >
                      {idx > 0 && <Typography sx={{ color: t.muted, fontSize: 11 }}>→</Typography>}
                      <Typography
                        data-testid={UI_IDENTIFIERS.UseCaseCarousel.walkthroughPathStep(idx)}
                        role="button"
                        sx={{
                          fontFamily: t.mono,
                          fontSize: 11,
                          cursor: 'pointer',
                          color: last ? t.accent : t.muted,
                          fontWeight: last ? 700 : 400,
                          '&:hover': { color: t.ink },
                        }}
                        tabIndex={0}
                        onClick={() => {
                          setPath((p) => p.slice(0, idx + 1));
                        }}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            setPath((p) => p.slice(0, idx + 1));
                          }
                        }}
                      >
                        {n !== undefined ? nodeText(n) : id}
                      </Typography>
                    </Box>
                  );
                })}
              </Box>
            </Collapse>
            {pathOverflowing || pathExpanded ? (
              <ButtonBase
                aria-controls={pathRegionId}
                aria-expanded={pathExpanded}
                sx={{
                  mt: 0.5,
                  justifyContent: 'flex-start',
                  fontFamily: t.mono,
                  fontSize: 10.5,
                  fontWeight: 700,
                  color: t.muted,
                  letterSpacing: '0.03em',
                  '&:hover': { color: t.ink },
                }}
                onClick={() => {
                  setPathExpanded((o) => !o);
                }}
              >
                {pathExpanded ? 'Show less ▴' : 'Show full path ▾'}
              </ButtonBase>
            ) : null}
          </Paper>
        )}
      </Box>

      {/* live "you-are-here" map — collapsed behind a text toggle in the
          Architecture lens' trace (`hideMap`, fix 3, founder QA round 5): the
          map duplicates the call chain beside it there, and the always-on
          canvas was the single largest claim on vertical space above the
          fold. The standalone Use Cases screen keeps it always-on. */}
      {hideMap ? (
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <ButtonBase
            aria-controls={mapRegionId}
            aria-expanded={mapOpen}
            sx={{
              mb: 0.75,
              justifyContent: 'flex-start',
              fontFamily: t.mono,
              fontSize: 11,
              fontWeight: 700,
              color: t.muted,
              letterSpacing: '0.03em',
              '&:hover': { color: t.ink },
            }}
            onClick={() => {
              setMapOpen((o) => !o);
              setMapEverOpened(true);
            }}
          >
            {mapOpen ? 'Hide activity map ▴' : 'Show activity map ▾'}
          </ButtonBase>
          {/* Lazy-mounted (fix-round-1 Finding 1): nothing here — not even a
              hidden React-Flow instance — until the first open. */}
          {mapEverOpened ? (
            <Collapse id={mapRegionId} in={mapOpen}>
              <ActivityFlow
                height={height}
                uc={uc}
                useCaseIndex={useCaseIndex}
                {...(highlight !== undefined ? { highlight } : {})}
              />
            </Collapse>
          ) : null}
        </Box>
      ) : (
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <ActivityFlow
            height={height}
            uc={uc}
            useCaseIndex={useCaseIndex}
            {...(highlight !== undefined ? { highlight } : {})}
          />
        </Box>
      )}
    </Box>
  );
}
