/**
 * A DynamicViewModel (an ordered call chain — a system use-case sequence OR a
 * black-box test scenario) as a top-down layered C4 view with a Structurizr-style
 * STEP-THROUGH: participants are placed by Method layer (shared computeLayout, with
 * the row-label gutter and Utilities side bar), and the ordered calls are walked one
 * at a time. Each step highlights exactly one relationship (bold edge + glowing
 * endpoints, EVERY other participant muted to the static graph's hover opacity)
 * and surfaces that single call's text in a caption bar above the diagram — the
 * only place relationship labels appear. No call text sits on the lines.
 *
 * FRAGMENT MODE (`focusStepNodeId`, founder QA round 2): the walk can instead be
 * driven from outside by a use-case walkthrough. The chain then lights the whole
 * FRAGMENT its current activity step realizes — every call of that step at once —
 * and the internal Prev/Next paging gives way to a caption listing those calls.
 * The stepper lives on the other side of the split; this pane follows. A
 * call-less step's caption differentiates a real realization gap from a
 * by-design control-flow step using `focusStepKind` (founder QA round 3).
 *
 * THE TRAIL (founder QA round 4 — the architect's Playwright assessment): the
 * chain BUILDS as you walk. Three tiers, not two: the current fragment at full
 * strength with its global sequence number chipped onto each wire, the calls
 * already walked (`visitedSeqs`, derived from the walkthrough's breadcrumb) at a
 * mid tint, and the never-walked remainder as ghosts at the same opacity the
 * muted boxes take. So a call-less step no longer blanks the canvas — it shows
 * the chain so far, and only a start with nothing behind it is dark. Parallel
 * strands between one pair are fanned apart (parallelEdges.ts) so stepping
 * between two of them visibly moves, and a call to a Utility draws a real edge:
 * the static graph's no-lines-to-the-bar convention does not belong in a call
 * chain, where the call IS the content.
 *
 * The camera fits ONCE to the whole diagram when the view changes (`resetKey`)
 * and never moves again while stepping — the founder does not want the canvas
 * panning/zooming per step, only muting; the self-paced step-through keeps its
 * own per-step recenter, its own two-tier muting, and the Utilities carve-out.
 *
 * Decoupled from the System envelope: callers pass a prebuilt `dv` (system views via
 * toDynamicView; test scenarios via testScenarioToDynamicView) plus a `resetKey`
 * that restarts the walk (and, in fragment mode, re-fits the camera) when the
 * selected view changes. An optional `statusBySeq` colours each call red (target /
 * failing) or green (passing) — the test views' own run status, or (Architecture)
 * the owning step's CC findings via callStatus.ts. Reuses the shared C4 node,
 * colours, decoration, legend and canvas chrome.
 *
 * PEOPLE: a realized chain's endpoints include the use case's actors, which are not
 * System components. They are laid out in their own `person` row above the Clients
 * (flowLayout's FlowLayer) and drawn with PersonNode; calls touching them are drawn
 * like any other. An endpoint that resolves to NEITHER a component nor an actor is
 * surfaced as an "unresolved" warning chip above the canvas rather than silently
 * dropping the call's line.
 */
import { useMemo, useRef, useState, type ReactNode } from 'react';
import type { Edge, Node } from '@xyflow/react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import type { DynamicViewModel, SequencedCall } from '../../contracts/adapters';
import type { ActivityNodeKind } from '../../contracts/types';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import {
  type Layer,
  type LayoutComponent,
  LAYER_ORDER,
  MUTED_NODE_OPACITY,
  VISITED_OPACITY,
  layerColors,
  computeLayout,
  decorativeNodes,
  c4Node,
  personNode,
  flowEdge,
  sortByLayoutPosition,
} from './flowLayout';
import { LayerLegend, FlowCanvas, FlowEmpty, FocusNodes } from './flowShared';
import { fragmentCallLessCaption, fragmentPositionLabel } from './fragmentCaption';
import { parallelIndex } from './parallelEdges';

/** Per-call status for the test views: 'red' = target/failing, 'green' = passing. */
export type StepStatus = 'red' | 'green';

function statusColor(status: StepStatus | undefined, t: Tokens): string | undefined {
  if (status === undefined) return undefined;
  return status === 'green' ? t.committedDot : t.dangerFg;
}

/** The React-Flow id for one sequenced call: stable across renders (a seq is
 *  unique within a linearization) and the key the parallel-strand slots use. */
function edgeId(r: SequencedCall): string {
  return `${String(r.seq)}-${r.from}-${r.to}`;
}

/** No trail at all — the shared empty set, so a chain rendered outside fragment
 *  mode never rebuilds its memo on a fresh identity. */
const NO_VISITED: ReadonlySet<number> = new Set<number>();

function build(
  dv: DynamicViewModel,
  t: Tokens,
  focused: readonly SequencedCall[],
  focalComponentId: string | undefined,
  statusBySeq: Map<number, StepStatus> | undefined,
  /** Change 3 (founder QA round 3 addendum): a decision/switch call-less step
   *  highlights its DECIDER — one participant id (an actor or a component) —
   *  instead of leaving the current tier empty. Folded into `focusEndpoints`
   *  below so it gets the exact same glow a real call's endpoint gets, sitting
   *  ON TOP of the visited trail rather than replacing it. */
  focusNodeId: string | undefined,
  /** True when the walkthrough drives this chain. Fragment mode carries the
   *  three-tier treatment (current / visited / never-walked) and drops the
   *  Utilities carve-out; the self-paced step-through keeps its own, older
   *  two-tier treatment untouched. */
  fragmentMode: boolean,
  /** Founder QA round 4: the seqs of every call the reader has ALREADY walked
   *  past (callTrail.visitedSeqsForPath). Fragment mode only — this is the
   *  accreting trail that makes the chain build as you walk instead of
   *  re-rendering one lonely fragment at every step. */
  visitedSeqs: ReadonlySet<number>
): {
  nodes: Node[];
  edges: Edge[];
  colors: Record<Layer, string>;
  usedLayers: Layer[];
  /** Every id that got a node — the endpoints an edge may safely reference. */
  placed: Set<string>;
} {
  const colors = layerColors(t);
  // People occupy their own row above the Clients; the barycenter sweep then pulls
  // each Client under whoever drives it (flowLayout places row 0 in declared order).
  const placedComponents: LayoutComponent[] = [
    ...dv.persons.map((p) => ({ id: p.id, layer: 'person' as const })),
    ...dv.participants,
  ];
  const layout = computeLayout(placedComponents, dv.edges);
  const layerOf = new Map(dv.participants.map((c) => [c.id, c.layer]));
  // What is lit. THREE tiers in fragment mode (founder QA round 4): the calls the
  // current step realizes burn at full strength, the calls already WALKED hold a
  // mid tint so the chain accretes behind the reader, and everything never walked
  // stays a ghost at the same 0.12 the muted nodes take (the old 0.40 wires over
  // 0.12 boxes was the "glitch look" the architect flagged). The self-paced
  // step-through keeps its original two tiers: one call lit, the rest muted
  // exactly as the static graph mutes a hovered component's non-neighbours.
  const focusSeqs = new Set(focused.map((c) => c.seq));
  // The brighter tier always wins: a path that loops back onto its own node puts
  // that node's calls in BOTH sets, and they belong to the current fragment.
  const visited = new Set([...visitedSeqs].filter((s) => !focusSeqs.has(s)));
  const focusEndpoints = new Set([
    ...focused.flatMap((c) => [c.from, c.to]),
    // Change 3: the decider highlight rides the SAME endpoint set a real call's
    // endpoints use — same glow, same dimming of everyone else. With a trail
    // beneath it, the decider now lights ON TOP of the walked chain rather than
    // being the only thing on an otherwise blank canvas.
    ...(focusNodeId !== undefined ? [focusNodeId] : []),
  ]);
  const visitedEndpoints = new Set(
    dv.edges
      .filter((r) => visited.has(r.seq))
      .flatMap((r) => [r.from, r.to])
      .filter((id) => !focusEndpoints.has(id))
  );
  const hasFocus = focusEndpoints.size > 0;
  // Feeds the node tiers + the edges. Fragment mode ALWAYS mutes: a call-less
  // step with no trail behind it darkens the whole diagram (the honest picture —
  // no call has happened yet), and a step with a trail darkens everything the
  // reader has not walked.
  const muted = fragmentMode || hasFocus;
  /** Which of the three tiers a participant sits in. */
  const tierOf = (id: string): 'focus' | 'visited' | 'rest' =>
    focusEndpoints.has(id) ? 'focus' : visitedEndpoints.has(id) ? 'visited' : 'rest';

  // People first (top row), then the components in the layout's visual reading
  // order (row top→down, then x) so DOM/tab order matches what the eye sees.
  const nodes: Node[] = dv.persons.map((p) => {
    const tier = tierOf(p.id);
    const base = personNode(p, layout.pos.get(p.id) ?? { x: 0, y: 0 }, colors.person, {
      dimmed: muted && tier === 'rest',
    });
    if (tier === 'focus') return { ...base, style: { filter: `drop-shadow(0 0 6px ${t.accent})` } };
    if (tier === 'visited') return { ...base, style: { opacity: VISITED_OPACITY } };
    return base;
  });

  nodes.push(
    ...sortByLayoutPosition(dv.participants, layout).map((c) => {
      const isFocal = focalComponentId !== undefined && c.id === focalComponentId;
      const tier = tierOf(c.id);
      // Dynamic lens: names + layer tags only. The current call's detail lives in the
      // step caption rail, so the node bodies stay compact (no volatility prose) — this
      // keeps heights stable and stops tall cards overlapping their neighbours.
      //
      // THE UTILITIES CARVE-OUT (never dimmed, because the static graph draws it no
      // lines) survives ONLY in the self-paced step-through it was written for. In a
      // walkthrough-driven TRACE a call to a Utility is content like any other call
      // (change 2 draws its edge), so a permanently-lit DesignHealth was simply a lie
      // about which steps touch it — the founder read it as "every step calls
      // Design Health" (QA round 4).
      const carveOut = !fragmentMode && c.layer === 'utility';
      const base = c4Node(c, layout.pos.get(c.id) ?? { x: 0, y: 0 }, colors, {
        showEncapsulates: false,
        dimmed: muted && tier === 'rest' && !isFocal && !carveOut,
      });
      if (tier === 'focus' || isFocal) {
        return {
          ...base,
          data: { ...base.data, ...(isFocal ? { color: t.accent } : {}) },
          style: { filter: `drop-shadow(0 0 6px ${t.accent})` },
        };
      }
      if (muted && tier === 'visited') return { ...base, style: { opacity: VISITED_OPACITY } };
      return base;
    })
  );
  nodes.push(...decorativeNodes(layout));

  const placed = new Set(nodes.map((n) => n.id));

  // EVERY call gets a line, Utilities included (change 2, founder QA round 4).
  // The static graph's no-lines-to-the-Utilities-bar convention belongs to the
  // static graph: in a CALL CHAIN the call is the content, so ci-check's
  // SystemDesignManager→DesignHealth call must be a real, numbered, tintable
  // edge — routed through the side handles into the bar rather than dropped.
  //
  // No call TEXT on any line; the current fragment's lines carry only their
  // sequence chip (change 4a), which is what ties them to the caption's numbered
  // list. When a status map is supplied each call is tinted red (failing /
  // flagged) or green (passing / clean) so the whole picture reads at a glance.
  // A call with an UNRESOLVED endpoint gets no line (there is no node to attach
  // it to) — the unresolved ids are surfaced as chips above the canvas instead.
  const drawn = dv.edges.filter((r) => placed.has(r.from) && placed.has(r.to));
  const slots = parallelIndex(drawn.map((r) => ({ id: edgeId(r), from: r.from, to: r.to })));
  const edges: Edge[] = drawn.map((r) => {
    const isCurrent = focusSeqs.has(r.seq);
    const isVisited = visited.has(r.seq);
    const status = statusColor(statusBySeq?.get(r.seq), t);
    // Visited wires take the ink stroke a focused wire takes (they ARE part of
    // the chain), held back by opacity alone — a status tint still wins.
    const stroke = status ?? (fragmentMode && isVisited ? t.ink : undefined);
    // Fragment mode drives opacity off the tier. The step-through keeps its own
    // rule: only status-tinted wires were ever explicitly faded (to 0.4), and
    // everything else rides flowEdge's per-variant default.
    const opacity = fragmentMode
      ? isCurrent
        ? 1
        : isVisited
          ? VISITED_OPACITY
          : MUTED_NODE_OPACITY
      : status !== undefined
        ? isCurrent || !muted
          ? 1
          : 0.4
        : undefined;
    const slot = slots.get(edgeId(r));
    return flowEdge(edgeId(r), r.from, r.to, r.label, t, {
      variant: isCurrent
        ? 'focus'
        : fragmentMode && isVisited
          ? 'normal'
          : muted
            ? 'muted'
            : 'normal',
      dashed: r.mode !== 'sync', // queued / pub-sub calls render dashed
      toUtility: layerOf.get(r.to) === 'utility',
      ...(stroke !== undefined ? { stroke } : {}),
      ...(opacity !== undefined ? { opacity } : {}),
      ...(isCurrent ? { seqChip: r.altLabel ?? r.seq } : {}),
      ...(slot !== undefined ? { parallel: slot } : {}),
    });
  });

  const present = new Set(dv.participants.map((c) => c.layer));
  const usedLayers = LAYER_ORDER.filter((l) => present.has(l));
  return { nodes, edges, colors, usedLayers, placed };
}

/** The controls + caption for the current call in the sequence. */
/** Per-step concrete detail for the caption (test views): inputs → expected. */
export interface StepDetail {
  inputs: { name: string; value: string }[];
  result?: string;
  errorExpected: boolean;
  errorCode?: string;
  assertion?: string;
}

function StepBar({
  dv,
  stepIndex,
  setStepIndex,
  statusBySeq,
  detailBySeq,
  onCommentStep,
  t,
}: {
  dv: DynamicViewModel;
  stepIndex: number;
  setStepIndex: (i: number) => void;
  statusBySeq: Map<number, StepStatus> | undefined;
  detailBySeq: Map<number, StepDetail> | undefined;
  /** When provided, the caption bar shows a Comment button that anchors this step. */
  onCommentStep: ((edge: SequencedCall) => void) | undefined;
  t: Tokens;
}): ReactNode {
  const total = dv.edges.length;
  const current = dv.edges[stepIndex];
  // Endpoint display names: components by name, people by their (id) name.
  const nameOf = useMemo(
    () =>
      new Map([
        ...dv.persons.map((p): [string, string] => [p.id, p.id]),
        ...dv.participants.map((c): [string, string] => [c.id, c.name]),
      ]),
    [dv.participants, dv.persons]
  );
  // Focus target after every step change (mirrors UseCaseWalkthrough's treatment):
  // the Prev/Next button just clicked can DISABLE on reaching a boundary (step 1 /
  // step N), which would silently drop keyboard focus to <body>. The step caption's
  // first line is the stable landing spot, and its role="status" live region
  // announces the new step — activity step AND call — to AT.
  const captionRef = useRef<HTMLElement>(null);
  if (total === 0 || current === undefined) return null;

  const btnSx = {
    color: t.ink,
    border: `1.5px solid ${t.line}`,
    borderRadius: 1,
    p: 0.5,
    '&.Mui-disabled': { color: t.muted, opacity: 0.4 },
  };
  const status = statusBySeq?.get(current.seq);
  const captionAccent = statusColor(status, t) ?? t.accent;
  const detail = detailBySeq?.get(current.seq);
  // Two-level caption. Level 1 = WHERE in the use case you are (the activity step
  // this call was authored on, and which of that step's calls this is); level 2 =
  // the call itself. A blank step label (a view with no linked activity diagram)
  // degrades to the plain counter rather than a dangling dash.
  const stepCaption =
    `Step ${String(current.seq)} of ${String(total)}` +
    (current.stepLabel.length > 0 ? ` — ${current.stepLabel}` : '') +
    ` (call ${String(current.callInStep)}/${String(current.callsInStep)})`;

  return (
    <Box sx={{ mb: 1.5 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
        <IconButton
          aria-label="Previous step"
          data-testid={UI_IDENTIFIERS.Architecture.DYNAMIC_STEP_PREV}
          disabled={stepIndex <= 0}
          size="small"
          sx={btnSx}
          onClick={() => {
            setStepIndex(stepIndex - 1);
            captionRef.current?.focus();
          }}
        >
          <ChevronLeftIcon fontSize="small" />
        </IconButton>
        {/* Visual pagination between the arrows. The spoken counter (and the focus
            landing spot) is the caption's first line below, which carries the same
            position PLUS the activity step it belongs to — so it is aria-hidden
            here to avoid announcing the position twice. */}
        <Typography
          aria-hidden="true"
          sx={{
            fontFamily: t.mono,
            fontSize: 12,
            color: t.muted,
            minWidth: 60,
            textAlign: 'center',
          }}
        >
          {stepIndex + 1} / {total}
        </Typography>
        <IconButton
          aria-label="Next step"
          data-testid={UI_IDENTIFIERS.Architecture.DYNAMIC_STEP_NEXT}
          disabled={stepIndex >= total - 1}
          size="small"
          sx={btnSx}
          onClick={() => {
            setStepIndex(stepIndex + 1);
            captionRef.current?.focus();
          }}
        >
          <ChevronRightIcon fontSize="small" />
        </IconButton>
        {status !== undefined ? (
          <Chip
            label={status === 'green' ? 'passing ✓' : 'target'}
            size="small"
            sx={{
              height: 18,
              fontFamily: t.mono,
              fontSize: 9,
              fontWeight: 700,
              bgcolor: 'transparent',
              color: captionAccent,
              border: `1.5px solid ${captionAccent}`,
            }}
          />
        ) : null}
        {onCommentStep !== undefined ? (
          <>
            <Box sx={{ flexGrow: 1 }} />
            <Button
              data-testid={UI_IDENTIFIERS.Comments.STEP_COMMENT}
              size="small"
              startIcon={<ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />}
              sx={{
                py: 0.25,
                color: t.accentText,
                bgcolor: t.accent,
                border: `1.5px solid ${t.line}`,
                fontFamily: t.mono,
                '&:hover': { bgcolor: t.accent2 },
              }}
              onClick={() => {
                onCommentStep(current);
              }}
            >
              Comment
            </Button>
          </>
        ) : null}
      </Box>
      <Box
        sx={{
          p: 1.25,
          border: `1.5px solid ${t.line}`,
          borderLeft: `3px solid ${captionAccent}`,
          borderRadius: 1,
          bgcolor: t.paper,
        }}
      >
        {/* Level 1 — the activity step. Also the live region + focus landing spot. */}
        <Typography
          aria-live="polite"
          ref={captionRef}
          role="status"
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 13,
            color: t.ink,
            wordBreak: 'break-word',
            outline: 'none',
            '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 2 },
          }}
          tabIndex={-1}
        >
          {stepCaption}
        </Typography>
        {/* Level 2 — the call this step makes, and between whom. */}
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 12,
            color: t.ink,
            mt: 0.25,
            wordBreak: 'break-word',
          }}
        >
          {current.label}
          <Box component="span" sx={{ color: t.muted }}>
            {'  ·  '}
            {nameOf.get(current.from) ?? current.from} → {nameOf.get(current.to) ?? current.to}
          </Box>
        </Typography>
        {detail !== undefined ? (
          <Box sx={{ mt: 0.75, display: 'flex', flexDirection: 'column', gap: 0.35 }}>
            {detail.inputs.length > 0 ? (
              <CaptionRow k="in" t={t}>
                {detail.inputs.map((a) => `${a.name} = ${a.value}`).join(',  ')}
              </CaptionRow>
            ) : null}
            {detail.errorExpected ? (
              <CaptionRow c={t.dangerFg} k="err" t={t}>
                {detail.errorCode !== undefined && detail.errorCode.length > 0
                  ? detail.errorCode
                  : 'expected failure'}
              </CaptionRow>
            ) : detail.result !== undefined && detail.result.length > 0 ? (
              <CaptionRow c={t.committedDot} k="out" t={t}>
                {detail.result}
              </CaptionRow>
            ) : null}
            {detail.assertion !== undefined && detail.assertion.length > 0 ? (
              <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.ink, mt: 0.15 }}>
                {detail.assertion}
              </Typography>
            ) : null}
          </Box>
        ) : null}
      </Box>
    </Box>
  );
}

/**
 * The caption for the FRAGMENT the walkthrough is standing on: every call the
 * current activity step realizes, listed together, in the same card the
 * step-through's caption uses. There is deliberately no Prev/Next here — the
 * walkthrough beside it owns the navigation (founder QA round 2), so this rail
 * reports rather than steers.
 */
function FragmentBar({
  dv,
  calls,
  hasTrail,
  focusStepNodeId,
  focusStepKind,
  focusDecider,
  statusBySeq,
  onCommentStep,
  t,
}: {
  dv: DynamicViewModel;
  /** Founder QA round 4: the reader has already walked past at least one call,
   *  so the canvas is showing a lit trail even when this step realizes nothing.
   *  Drives the call-less copy — "nothing here" vs "nothing NEW here". */
  hasTrail: boolean;
  /** The focused step's calls in chain order — EMPTY when the step realizes
   *  none (an unrealized node the reader walked onto, or the entry chooser). */
  calls: readonly SequencedCall[];
  /** The activity node id the walkthrough is standing on ('' = the multi-root
   *  entry chooser) — needed only to pick the right call-less heading. */
  focusStepNodeId: string;
  /** That node's ActivityNodeKind, when the caller (ArchitectureView) knows
   *  it — differentiates a real realization gap from a by-design
   *  control-flow step when `calls` is empty (founder QA round 3). */
  focusStepKind: ActivityNodeKind | undefined;
  /** Change 3 (founder QA round 3 addendum): a decision/switch call-less step's
   *  resolved decider (an actor or the entry Manager) — the highest-precedence
   *  call-less caption (`fragmentCallLessCaption`) when `calls` is empty.
   *  Undefined for every other call-less kind, or when no decider resolved. */
  focusDecider: { id: string; label: string } | undefined;
  statusBySeq: Map<number, StepStatus> | undefined;
  /** When provided, a Comment button anchoring the fragment's FIRST call. */
  onCommentStep: ((edge: SequencedCall) => void) | undefined;
  t: Tokens;
}): ReactNode {
  // Endpoint display names: components by name, people by their (id) name.
  const nameOf = useMemo(
    () =>
      new Map([
        ...dv.persons.map((p): [string, string] => [p.id, p.id]),
        ...dv.participants.map((c): [string, string] => [c.id, c.name]),
      ]),
    [dv.participants, dv.persons]
  );
  const first = calls[0];
  // Loudest status across the fragment: one flagged call tints the whole card,
  // the same way a single failing call tints its step in the step-through.
  const statuses = calls.map((c) => statusBySeq?.get(c.seq));
  const worst: StepStatus | undefined = statuses.includes('red')
    ? 'red'
    : statuses.includes('green')
      ? 'green'
      : undefined;
  const captionAccent = statusColor(worst, t) ?? t.accent;
  const stepLabel = first !== undefined && first.stepLabel.length > 0 ? first.stepLabel : undefined;
  // Where this fragment sits in the WHOLE chain (founder QA round 4): a step's
  // own "3 calls" says nothing about progress through 22.
  const position = fragmentPositionLabel(
    calls.map((c) => c.seq),
    dv.edges.length
  );
  // A call-less fragment's two lines are decided TOGETHER (fix round 1) so the
  // heading and its gloss can never disagree — in particular so a realization
  // gap is never softened by the trail's reassuring second line.
  const callLess =
    first === undefined
      ? fragmentCallLessCaption(focusStepNodeId, focusStepKind, hasTrail, focusDecider?.label)
      : undefined;
  const heading =
    first !== undefined
      ? `Step: ${stepLabel ?? first.stepNodeId} — ${String(calls.length)} call${
          calls.length === 1 ? '' : 's'
        }${position !== undefined ? ` · ${position}` : ''}`
      : (callLess?.heading ?? '');

  return (
    <Box sx={{ mb: 1.5 }}>
      <Box
        data-testid={UI_IDENTIFIERS.Architecture.DYNAMIC_FRAGMENT}
        sx={{
          p: 1.25,
          border: `1.5px solid ${t.line}`,
          borderLeft: `3px solid ${captionAccent}`,
          borderRadius: 1,
          bgcolor: t.paper,
        }}
      >
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          {/* The live region: the walkthrough moved the focus, so this rail is
              what announces WHERE the chain now stands. */}
          <Typography
            aria-live="polite"
            role="status"
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 13,
              color: t.ink,
              wordBreak: 'break-word',
            }}
          >
            {heading}
          </Typography>
          {worst !== undefined ? (
            <Chip
              label={worst === 'green' ? 'passing ✓' : 'target'}
              size="small"
              sx={{
                height: 18,
                fontFamily: t.mono,
                fontSize: 9,
                fontWeight: 700,
                bgcolor: 'transparent',
                color: captionAccent,
                border: `1.5px solid ${captionAccent}`,
              }}
            />
          ) : null}
          {onCommentStep !== undefined && first !== undefined ? (
            <>
              <Box sx={{ flexGrow: 1 }} />
              <Button
                data-testid={UI_IDENTIFIERS.Comments.STEP_COMMENT}
                size="small"
                startIcon={<ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />}
                sx={{
                  py: 0.25,
                  color: t.accentText,
                  bgcolor: t.accent,
                  border: `1.5px solid ${t.line}`,
                  fontFamily: t.mono,
                  '&:hover': { bgcolor: t.accent2 },
                }}
                onClick={() => {
                  onCommentStep(first);
                }}
              >
                Comment
              </Button>
            </>
          ) : null}
        </Box>
        {calls.length > 0 ? (
          <Box sx={{ mt: 0.5, display: 'flex', flexDirection: 'column', gap: 0.35 }}>
            {calls.map((c) => (
              <Typography
                key={c.seq}
                sx={{
                  fontFamily: t.mono,
                  fontSize: 12,
                  color: t.ink,
                  wordBreak: 'break-word',
                }}
              >
                {c.altLabel ?? c.seq}. {c.label}
                <Box component="span" sx={{ color: t.muted }}>
                  {'  ·  '}
                  {nameOf.get(c.from) ?? c.from} → {nameOf.get(c.to) ?? c.to}
                </Box>
              </Typography>
            ))}
          </Box>
        ) : callLess?.body !== undefined ? (
          <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, mt: 0.5 }}>
            {callLess.body}
          </Typography>
        ) : null}
      </Box>
    </Box>
  );
}

/** One labelled monospace row in the step caption (in / out / err). */
function CaptionRow({
  k,
  c,
  t,
  children,
}: {
  k: string;
  c?: string;
  t: Tokens;
  children: ReactNode;
}): ReactNode {
  return (
    <Box sx={{ display: 'flex', gap: 0.75, alignItems: 'baseline' }}>
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 8.5,
          fontWeight: 700,
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
          color: c ?? t.muted,
          minWidth: 22,
        }}
      >
        {k}
      </Typography>
      <Typography
        sx={{ fontFamily: t.mono, fontSize: 11, color: c ?? t.ink, wordBreak: 'break-word' }}
      >
        {children}
      </Typography>
    </Box>
  );
}

/** The step the walk starts (and restarts) on: `initialStep` clamped into the
 *  view's range. Non-finite input degrades to the first step. */
function clampStep(initialStep: number | undefined, count: number): number {
  const last = Math.max(count - 1, 0);
  if (initialStep === undefined || !Number.isFinite(initialStep)) return 0;
  return Math.min(Math.max(Math.trunc(initialStep), 0), last);
}

export function DynamicViewFlow({
  dv,
  resetKey,
  height = 600,
  focalComponentId,
  initialStep,
  statusBySeq,
  detailBySeq,
  focusStepNodeId,
  focusStepKind,
  focusDecider,
  visitedSeqs,
  onCommentStep,
}: {
  /** The ordered call chain to render (system use case or test scenario). */
  dv: DynamicViewModel;
  /** Changing this restarts the step-through at `initialStep` (e.g. the picked
   *  view/scenario id). */
  resetKey: string;
  /** A fixed pixel height (the historical default) or a CSS length — the
   *  Architecture lens' walkthrough-driven trace passes a viewport-relative
   *  `clamp(...)` so the chain fits above the fold beside the walkthrough
   *  (founder QA round 5). */
  height?: number | string;
  /** Optional component id (kebab-case) to visually emphasize in the diagram. */
  focalComponentId?: string;
  /** Optional 0-based step to open on, clamped into range — the landing step of a
   *  deep link into one call of the chain. Re-applied whenever `resetKey` changes;
   *  the reader owns the position afterwards. Default: the first step. */
  initialStep?: number;
  /** Optional per-call status colouring: seq → 'red' | 'green' (test-view run
   *  status, or the owning step's CC findings — see callStatus.ts). */
  statusBySeq?: Map<number, StepStatus>;
  /** Optional per-call concrete detail (test views): seq → inputs / expected. */
  detailBySeq?: Map<number, StepDetail>;
  /** FRAGMENT MODE (founder QA round 2): follow a use-case walkthrough instead of
   *  paging calls yourself. Defined = the activity node the reader is standing
   *  on; ALL of that step's calls light up together and the internal step-through
   *  (Prev/Next + its single-call caption) gives way to a fragment caption
   *  listing them. An id no call was authored on — including '' (the multi-root
   *  entry chooser, or a step the chain does not realize) — renders the chain
   *  with nothing focused and says so. Undefined = the self-driven step-through. */
  focusStepNodeId?: string;
  /** The focused node's ActivityNodeKind, when the caller (ArchitectureView)
   *  knows it — used only to pick the right call-less caption (a real
   *  realization gap vs. a by-design control-flow step; founder QA round 3).
   *  Ignored outside fragment mode. */
  focusStepKind?: ActivityNodeKind;
  /** Change 3 (founder QA round 3 addendum): the resolved DECIDER for a
   *  decision/switch call-less step — one participant (an actor or the use
   *  case's entry Manager) to highlight instead of muting everything, per
   *  ArchitectureView's `resolveDecider`. Ignored when `calls` is non-empty
   *  or outside fragment mode; undefined falls back to `muteAll`. */
  focusDecider?: { id: string; label: string };
  /** FRAGMENT MODE, founder QA round 4 (the visited trail): the seqs of every
   *  call the reader has already walked past — the caller derives them from the
   *  walkthrough's breadcrumb path (callTrail.visitedSeqsForPath), so Back and
   *  Restart shrink the trail with no extra state anywhere. Those calls hold a
   *  mid tint between the current fragment and the never-walked remainder, which
   *  is what makes the chain BUILD as you walk instead of re-lighting one lonely
   *  fragment per step. Must be a stable identity across renders (a useMemo) —
   *  it feeds the layout memo. Omitted / empty = nothing walked yet. */
  visitedSeqs?: ReadonlySet<number>;
  /** Optional per-step comment handler: enables a Comment button in the caption bar
   *  that arms an anchor for the current call — the fragment's FIRST call in
   *  fragment mode (system-design use only; omitted for the read-only
   *  test-scenario views). */
  onCommentStep?: ((edge: SequencedCall) => void) | undefined;
}): ReactNode {
  const t = useTokens();
  const [stepIndex, setStepIndex] = useState(() => clampStep(initialStep, dv.edges.length));
  // Restart the walk whenever the selected view changes (reset-on-prop-change
  // during render — the React-recommended alternative to a setState effect),
  // landing on the caller's requested step rather than always step 1.
  const [prevKey, setPrevKey] = useState(resetKey);
  if (prevKey !== resetKey) {
    setPrevKey(resetKey);
    setStepIndex(clampStep(initialStep, dv.edges.length));
  }
  const safeStep = Math.min(Math.max(stepIndex, 0), Math.max(dv.edges.length - 1, 0));

  // What the diagram lights. Fragment mode takes every call the driving
  // walkthrough's current step authored (both surfaces of a dual-entry step
  // light together — that is the point); otherwise it is the one call the
  // internal step-through stands on.
  const fragmentMode = focusStepNodeId !== undefined;
  const currentCall = dv.edges[safeStep];
  const focusedCalls = useMemo(
    () =>
      focusStepNodeId !== undefined
        ? dv.edges.filter((e) => e.stepNodeId === focusStepNodeId)
        : currentCall !== undefined
          ? [currentCall]
          : [],
    [dv.edges, focusStepNodeId, currentCall]
  );
  // Fragment mode's call-less steps (an unrealized node, a control-flow step by
  // design, the multi-root entry chooser) light no CURRENT fragment. They no
  // longer blank the diagram (founder QA round 4): the visited trail stays lit
  // beneath them, and only a step reached with nothing walked yet — the start
  // node, the entry chooser — darkens everything, which is then the honest
  // picture rather than a contradiction of its own caption. A resolved decider
  // (change 3 addendum) still lights on top of whatever the trail shows.
  const callLess = fragmentMode && focusedCalls.length === 0;
  const focusNodeId = callLess ? focusDecider?.id : undefined;
  // Outside fragment mode the trail is meaningless — the reader is paging calls
  // one at a time, not walking a route — so it is pinned empty there.
  const visited = fragmentMode ? (visitedSeqs ?? NO_VISITED) : NO_VISITED;
  const hasTrail = visited.size > 0;

  const { nodes, edges, colors, usedLayers, placed } = useMemo(
    () =>
      build(dv, t, focusedCalls, focalComponentId, statusBySeq, focusNodeId, fragmentMode, visited),
    [dv, t, focusedCalls, focalComponentId, statusBySeq, focusNodeId, fragmentMode, visited]
  );

  // Recenter the camera on what is lit (only the endpoints that actually got a
  // node — an unresolved id would frame nothing). Deduped: a fragment's calls
  // routinely share an endpoint. Only consumed by the self-driven step-through's
  // per-step FocusNodes below — fragment mode's camera never moves per step.
  const focusIds = useMemo(
    () => [...new Set(focusedCalls.flatMap((c) => [c.from, c.to]))].filter((id) => placed.has(id)),
    [focusedCalls, placed]
  );

  if (dv.participants.length === 0 && dv.persons.length === 0) {
    return <FlowEmpty label="No call chain to render yet." t={t} />;
  }

  return (
    <Box>
      {fragmentMode ? (
        <FragmentBar
          calls={focusedCalls}
          dv={dv}
          focusDecider={focusDecider}
          focusStepKind={focusStepKind}
          focusStepNodeId={focusStepNodeId}
          hasTrail={hasTrail}
          statusBySeq={statusBySeq}
          t={t}
          onCommentStep={onCommentStep}
        />
      ) : (
        <StepBar
          detailBySeq={detailBySeq}
          dv={dv}
          setStepIndex={setStepIndex}
          statusBySeq={statusBySeq}
          stepIndex={safeStep}
          t={t}
          onCommentStep={onCommentStep}
        />
      )}
      <UnresolvedChips ids={dv.unresolved} t={t} />
      {/* STILL CAMERA in fragment mode (founder QA round 3): keying the canvas to
          `resetKey` remounts ReactFlow whenever the VIEW changes, so its own
          `fitView` prop (FlowCanvas) fits the whole diagram once — the same
          initial-fit mechanism every flow lens uses on mount — and then nothing
          moves the camera again while the walkthrough steps (no FocusNodes here).
          The self-driven step-through keeps its own key (stable across steps) and
          its per-step FocusNodes recenter, unchanged. */}
      <FlowCanvas
        edges={edges}
        height={height}
        key={fragmentMode ? `fragment:${resetKey}` : 'step-through'}
        nodes={nodes}
        t={t}
      >
        <LayerLegend colors={colors} t={t} usedLayers={usedLayers} />
        {!fragmentMode && <FocusNodes dep={String(safeStep)} nodeIds={focusIds} />}
      </FlowCanvas>
    </Box>
  );
}

/**
 * The warning row for call endpoints that resolve to NEITHER a System component
 * nor a use-case actor (adapters' `unresolved`): a dangling id in the authored
 * call chain. Surfaced as chips above the canvas — those calls can be drawn no
 * line, and a silently missing arrow would read as a complete chain.
 */
function UnresolvedChips({ ids, t }: { ids: string[]; t: Tokens }): ReactNode {
  if (ids.length === 0) return null;
  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'center',
        flexWrap: 'wrap',
        gap: 0.75,
        mb: 1,
        p: 0.75,
        border: `1.5px solid ${t.awaitingFg}`,
        borderRadius: 1,
        bgcolor: t.paper,
      }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 9,
          fontWeight: 700,
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
          color: t.awaitingFg,
        }}
      >
        Unresolved endpoints
      </Typography>
      {ids.map((id) => (
        <Chip
          key={id}
          label={id}
          size="small"
          sx={{
            height: 18,
            fontFamily: t.mono,
            fontSize: 9,
            fontWeight: 700,
            bgcolor: 'transparent',
            color: t.awaitingFg,
            border: `1.5px solid ${t.awaitingFg}`,
          }}
        />
      ))}
      <Typography sx={{ fontFamily: t.body, fontSize: 11, color: t.muted }}>
        named by a call but neither a component nor an actor — those calls are not drawn.
      </Typography>
    </Box>
  );
}
