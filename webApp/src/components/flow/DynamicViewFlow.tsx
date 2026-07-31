/**
 * A DynamicViewModel (an ordered call chain — a system use-case sequence OR a
 * black-box test scenario) as a top-down layered C4 view with a Structurizr-style
 * STEP-THROUGH: participants are placed by Method layer (shared computeLayout, with
 * the row-label gutter and Utilities side bar), and the ordered calls are walked one
 * at a time. Each step highlights exactly one relationship (bold edge + glowing
 * endpoints, everything else quiet) and surfaces that single call's text in a
 * caption bar above the diagram — the only place relationship labels appear. No
 * labels sit on the lines; no lines are drawn to the Utilities bar.
 *
 * Decoupled from the System envelope: callers pass a prebuilt `dv` (system views via
 * toDynamicView; test scenarios via testScenarioToDynamicView) plus a `resetKey`
 * that restarts the walk when the selected view changes. An optional `statusBySeq`
 * colours each call red (target / failing) or green (passing) — the test views' own
 * run status, or (Architecture) the owning step's CC findings via callStatus.ts.
 * Reuses the shared C4 node, colours, decoration, legend and canvas chrome.
 *
 * PEOPLE: a realized chain's endpoints include the use case's actors, which are not
 * System components. They are laid out in their own `person` row above the Clients
 * (flowLayout's FlowLayer) and drawn with PersonNode; calls touching them are drawn
 * like any other. An endpoint that resolves to NEITHER a component nor an actor is
 * surfaced as an "unresolved" warning chip above the canvas rather than silently
 * dropping the call's line.
 */
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
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

/** Per-call status for the test views: 'red' = target/failing, 'green' = passing. */
export type StepStatus = 'red' | 'green';

function statusColor(status: StepStatus | undefined, t: Tokens): string | undefined {
  if (status === undefined) return undefined;
  return status === 'green' ? t.committedDot : t.dangerFg;
}

function build(
  dv: DynamicViewModel,
  t: Tokens,
  stepIndex: number,
  focalComponentId: string | undefined,
  statusBySeq: Map<number, StepStatus> | undefined
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
  const current = dv.edges[stepIndex];
  const isEndpoint = (id: string): boolean => id === current?.from || id === current?.to;

  // People first (top row), then the components in the layout's visual reading
  // order (row top→down, then x) so DOM/tab order matches what the eye sees.
  const nodes: Node[] = dv.persons.map((p) => {
    const base = personNode(p, layout.pos.get(p.id) ?? { x: 0, y: 0 }, colors.person);
    return isEndpoint(p.id)
      ? { ...base, style: { filter: `drop-shadow(0 0 6px ${t.accent})` } }
      : base;
  });

  nodes.push(
    ...sortByLayoutPosition(dv.participants, layout).map((c) => {
      // Dynamic lens: names + layer tags only. The current call's detail lives in the
      // step caption rail, so the node bodies stay compact (no volatility prose) — this
      // keeps heights stable and stops tall cards overlapping their neighbours.
      const base = c4Node(c, layout.pos.get(c.id) ?? { x: 0, y: 0 }, colors, {
        showEncapsulates: false,
      });
      const isFocal = focalComponentId !== undefined && c.id === focalComponentId;
      if (isEndpoint(c.id) || isFocal) {
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
      const isCurrent = r.seq === current?.seq;
      const stroke = statusColor(statusBySeq?.get(r.seq), t);
      return flowEdge(`${String(r.seq)}-${r.from}-${r.to}`, r.from, r.to, r.label, t, {
        variant: isCurrent ? 'focus' : 'muted',
        dashed: r.mode !== 'sync', // queued / pub-sub calls render dashed
        ...(stroke !== undefined ? { stroke, opacity: isCurrent ? 1 : 0.4 } : {}),
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
  onCommentStep,
  onStepChange,
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
  /** Optional per-step comment handler: enables a Comment button in the caption bar
   *  that arms an anchor for the current call (system-design use only; omitted for
   *  the read-only test-scenario views). */
  onCommentStep?: ((edge: SequencedCall) => void) | undefined;
  /** Optional notification of where the walk now stands — the current call, or
   *  undefined when the view has no calls. Fired on mount, on every step, and
   *  after a `resetKey` restart, so a companion surface (the Architecture lens'
   *  activity-diagram trace) can follow along. Keep the handler stable
   *  (useCallback) — it is an effect dependency. */
  onStepChange?: ((call: SequencedCall | undefined) => void) | undefined;
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

  const { nodes, edges, colors, usedLayers, placed } = useMemo(
    () => build(dv, t, safeStep, focalComponentId, statusBySeq),
    [dv, t, safeStep, focalComponentId, statusBySeq]
  );

  // Recenter the camera on the current call's two endpoints as you step (only the
  // endpoints that actually got a node — an unresolved id would frame nothing).
  const currentCall = dv.edges[safeStep];
  const focusIds = useMemo(
    () =>
      currentCall !== undefined
        ? [currentCall.from, currentCall.to].filter((id) => placed.has(id))
        : [],
    [currentCall, placed]
  );

  // Publish the walk's position to whoever is following along (the Architecture
  // lens' side-by-side activity trace). Declared above the empty-view early
  // return so the hook order is stable, and keyed on the call OBJECT — the
  // memoized `dv` hands back the same reference until the model itself changes,
  // so a listener that mirrors it into state settles immediately.
  useEffect(() => {
    onStepChange?.(currentCall);
  }, [onStepChange, currentCall]);

  if (dv.participants.length === 0 && dv.persons.length === 0) {
    return <FlowEmpty label="No call chain to render yet." t={t} />;
  }

  return (
    <Box>
      <StepBar
        detailBySeq={detailBySeq}
        dv={dv}
        setStepIndex={setStepIndex}
        statusBySeq={statusBySeq}
        stepIndex={safeStep}
        t={t}
        onCommentStep={onCommentStep}
      />
      <UnresolvedChips ids={dv.unresolved} t={t} />
      <FlowCanvas edges={edges} height={height} nodes={nodes} t={t}>
        <LayerLegend colors={colors} t={t} usedLayers={usedLayers} />
        <FocusNodes dep={String(safeStep)} nodeIds={focusIds} />
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
