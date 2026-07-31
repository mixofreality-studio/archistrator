/**
 * A DynamicViewModel (an ordered call chain — a system use-case sequence OR a
 * black-box test scenario) as a top-down layered C4 view with a Structurizr-style
 * STEP-THROUGH: participants are placed by Method layer (shared computeLayout, with
 * the row-label gutter and Utilities side bar), and the ordered calls are walked one
 * at a time. Each step highlights exactly one relationship (bold edge + glowing
 * endpoints, EVERY other participant muted to the static graph's hover opacity)
 * and surfaces that single call's text in a caption bar above the diagram — the
 * only place relationship labels appear. No labels sit on the lines; no lines
 * are drawn to the Utilities bar.
 *
 * FRAGMENT MODE (`focusStepNodeId`, founder QA round 2): the walk can instead be
 * driven from outside by a use-case walkthrough. The chain then lights the whole
 * FRAGMENT its current activity step realizes — every call of that step at once,
 * everything else muted — and the internal Prev/Next paging gives way to a
 * caption listing those calls. The stepper lives on the other side of the split;
 * this pane follows. A call-less step (nothing to light — an unrealized node, a
 * control-flow node by design, or the multi-root entry chooser) MUTES THE WHOLE
 * DIAGRAM instead, and the caption differentiates a real gap from "by design"
 * using `focusStepKind` (founder QA round 3). The camera fits ONCE to the whole
 * diagram when the view changes (`resetKey`) and never moves again while
 * stepping — the founder does not want the canvas panning/zooming per step, only
 * muting; the self-paced step-through keeps its own per-step recenter.
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
  layerColors,
  computeLayout,
  decorativeNodes,
  c4Node,
  personNode,
  flowEdge,
  sortByLayoutPosition,
} from './flowLayout';
import { LayerLegend, FlowCanvas, FlowEmpty, FocusNodes } from './flowShared';
import { fragmentCallLessHeading } from './fragmentCaption';

/** Per-call status for the test views: 'red' = target/failing, 'green' = passing. */
export type StepStatus = 'red' | 'green';

function statusColor(status: StepStatus | undefined, t: Tokens): string | undefined {
  if (status === undefined) return undefined;
  return status === 'green' ? t.committedDot : t.dangerFg;
}

function build(
  dv: DynamicViewModel,
  t: Tokens,
  focused: readonly SequencedCall[],
  focalComponentId: string | undefined,
  statusBySeq: Map<number, StepStatus> | undefined,
  /** Fragment mode's call-less steps (founder QA round 3): an unrealized
   *  activity node, a control-flow step by design, or the multi-root entry
   *  chooser all light NOTHING, so mute the WHOLE diagram — every node
   *  (including the Utilities bar's usual never-dimmed carve-out) and every
   *  edge — rather than rendering it plain/unmuted, which read as a silent
   *  no-op rather than "no calls happen here". False whenever `focusNodeId`
   *  is set (change 3 below) — the two are mutually exclusive. */
  muteAll: boolean,
  /** Change 3 (founder QA round 3 addendum): a decision/switch call-less step
   *  highlights its DECIDER — one participant id (an actor or a component) —
   *  instead of muting everything. Folded into `focusEndpoints` below so it
   *  gets the exact same glow/dim treatment a real call's endpoint gets;
   *  `muteAll` is false whenever this is set. */
  focusNodeId: string | undefined
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
  // What is lit: ONE call in the step-through, the whole fragment in
  // walkthrough-driven mode. Everything else is muted exactly as the static
  // graph mutes a hovered component's non-neighbours (founder QA round 2 — the
  // glow alone did not read as focus).
  const focusSeqs = new Set(focused.map((c) => c.seq));
  const focusEndpoints = new Set([
    ...focused.flatMap((c) => [c.from, c.to]),
    // Change 3: the decider highlight rides the SAME endpoint set a real
    // call's endpoints use — same glow, same dimming of everyone else, same
    // Utilities carve-out (no calls exist for this step, so no edge is ever
    // "current" — every edge below just goes quietly `muted`).
    ...(focusNodeId !== undefined ? [focusNodeId] : []),
  ]);
  const hasFocus = focusEndpoints.size > 0;
  const isEndpoint = (id: string): boolean => focusEndpoints.has(id);
  // Feeds edges + the person row: a real per-call focus OR the call-less
  // mute-all — mutually exclusive (muteAll is only ever true when `focused`,
  // and therefore `focusEndpoints`, is empty).
  const muted = hasFocus || muteAll;

  // People first (top row), then the components in the layout's visual reading
  // order (row top→down, then x) so DOM/tab order matches what the eye sees.
  const nodes: Node[] = dv.persons.map((p) => {
    const base = personNode(p, layout.pos.get(p.id) ?? { x: 0, y: 0 }, colors.person, {
      dimmed: muted && !isEndpoint(p.id),
    });
    return isEndpoint(p.id)
      ? { ...base, style: { filter: `drop-shadow(0 0 6px ${t.accent})` } }
      : base;
  });

  nodes.push(
    ...sortByLayoutPosition(dv.participants, layout).map((c) => {
      const isFocal = focalComponentId !== undefined && c.id === focalComponentId;
      // Dynamic lens: names + layer tags only. The current call's detail lives in the
      // step caption rail, so the node bodies stay compact (no volatility prose) — this
      // keeps heights stable and stops tall cards overlapping their neighbours.
      // Utilities carry the static graph's carve-out: shared infrastructure in a
      // side bar that receives no lines is never dimmed, it simply exists — EXCEPT
      // under mute-all, which overrides every carve-out (including this one and the
      // focal glow below) so the whole diagram reads as "no calls happen here".
      const base = c4Node(c, layout.pos.get(c.id) ?? { x: 0, y: 0 }, colors, {
        showEncapsulates: false,
        dimmed: muteAll || (muted && !isEndpoint(c.id) && !isFocal && c.layer !== 'utility'),
      });
      if (!muteAll && (isEndpoint(c.id) || isFocal)) {
        return {
          ...base,
          data: { ...base.data, ...(isFocal ? { color: t.accent } : {}) },
          style: { filter: `drop-shadow(0 0 6px ${t.accent})` },
        };
      }
      return base;
    })
  );
  nodes.push(...decorativeNodes(layout));

  const placed = new Set(nodes.map((n) => n.id));

  // No utility lines; no labels on any line. Every call is drawn quiet except the
  // current step, which is highlighted — its text lives in the caption bar. When a
  // status map is supplied each call is tinted red (failing / flagged) or green
  // (passing / clean) so the whole picture reads at a glance while you step. A call
  // with an UNRESOLVED endpoint gets no line (there is no node to attach it to) —
  // the unresolved ids are surfaced as chips above the canvas instead.
  const edges: Edge[] = dv.edges
    .filter((r) => layerOf.get(r.to) !== 'utility' && placed.has(r.from) && placed.has(r.to))
    .map((r) => {
      const isCurrent = focusSeqs.has(r.seq);
      const stroke = statusColor(statusBySeq?.get(r.seq), t);
      return flowEdge(`${String(r.seq)}-${r.from}-${r.to}`, r.from, r.to, r.label, t, {
        variant: isCurrent ? 'focus' : muted ? 'muted' : 'normal',
        dashed: r.mode !== 'sync', // queued / pub-sub calls render dashed
        ...(stroke !== undefined ? { stroke, opacity: isCurrent || !muted ? 1 : 0.4 } : {}),
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
  focusStepNodeId,
  focusStepKind,
  focusDecider,
  statusBySeq,
  onCommentStep,
  t,
}: {
  dv: DynamicViewModel;
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
   *  resolved decider (an actor or the entry Manager) — takes priority over
   *  `fragmentCallLessHeading` when `calls` is empty. Undefined for every
   *  other call-less kind, or when no decider could be resolved. */
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
  const heading =
    first !== undefined
      ? `Step: ${stepLabel ?? first.stepNodeId} — ${String(calls.length)} call${
          calls.length === 1 ? '' : 's'
        }`
      : focusDecider !== undefined
        ? `Decided by ${focusDecider.label}`
        : fragmentCallLessHeading(focusStepNodeId, focusStepKind);

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
                {c.seq}. {c.label}
                <Box component="span" sx={{ color: t.muted }}>
                  {'  ·  '}
                  {nameOf.get(c.from) ?? c.from} → {nameOf.get(c.to) ?? c.to}
                </Box>
              </Typography>
            ))}
          </Box>
        ) : focusStepNodeId !== '' ? (
          <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, mt: 0.5 }}>
            This step authors no calls — the chain stays as it was while you walk past it.
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
  onCommentStep,
}: {
  /** The ordered call chain to render (system use case or test scenario). */
  dv: DynamicViewModel;
  /** Changing this restarts the step-through at `initialStep` (e.g. the picked
   *  view/scenario id). */
  resetKey: string;
  height?: number;
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
  // Fragment mode's call-less steps mute the WHOLE diagram (founder QA round 3)
  // rather than rendering it plain — see build()'s `muteAll` doc — UNLESS the
  // caller resolved a decider (change 3 addendum: a decision/switch node
  // highlights whoever makes the call instead). Never true in the self-driven
  // step-through (there `focusedCalls` is only empty when the view has no
  // edges at all, which renders no StepBar either).
  const callLess = fragmentMode && focusedCalls.length === 0;
  const muteAll = callLess && focusDecider === undefined;
  const focusNodeId = callLess ? focusDecider?.id : undefined;

  const { nodes, edges, colors, usedLayers, placed } = useMemo(
    () => build(dv, t, focusedCalls, focalComponentId, statusBySeq, muteAll, focusNodeId),
    [dv, t, focusedCalls, focalComponentId, statusBySeq, muteAll, focusNodeId]
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
