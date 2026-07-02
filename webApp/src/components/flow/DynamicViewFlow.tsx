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
 * colours each call red (target / failing) or green (passing) for the test views.
 * Reuses the shared C4 node, colours, decoration, legend and canvas chrome.
 */
import { useMemo, useState, type ReactNode } from 'react';
import type { Edge, Node } from '@xyflow/react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import type { DynamicViewModel } from '../../api/adapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import {
  type Layer,
  LAYER_ORDER,
  layerColors,
  computeLayout,
  decorativeNodes,
  c4Node,
  flowEdge,
} from './flowLayout';
import { LayerLegend, FlowCanvas, FlowEmpty } from './flowShared';

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
): { nodes: Node[]; edges: Edge[]; colors: Record<Layer, string>; usedLayers: Layer[] } {
  const colors = layerColors(t);
  const layout = computeLayout(dv.participants, dv.edges);
  const layerOf = new Map(dv.participants.map((c) => [c.id, c.layer]));
  const current = dv.edges[stepIndex];

  const nodes: Node[] = dv.participants.map((c) => {
    const base = c4Node(c, layout.pos.get(c.id) ?? { x: 0, y: 0 }, colors);
    const isEndpoint = c.id === current?.from || c.id === current?.to;
    const isFocal = focalComponentId !== undefined && c.id === focalComponentId;
    if (isEndpoint || isFocal) {
      return {
        ...base,
        data: { ...base.data, ...(isFocal ? { color: t.accent } : {}) },
        style: { filter: `drop-shadow(0 0 6px ${t.accent})` },
      };
    }
    return base;
  });
  nodes.push(...decorativeNodes(layout));

  // No utility lines; no labels on any line. Every call is drawn quiet except the
  // current step, which is highlighted — its text lives in the caption bar. When a
  // status map is supplied (test views) each call is tinted red (target) / green
  // (passing) so the whole pass/fail picture reads at a glance while you step.
  const edges: Edge[] = dv.edges
    .filter((r) => layerOf.get(r.to) !== 'utility')
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
  return { nodes, edges, colors, usedLayers };
}

/** The controls + caption for the current call in the sequence. */
function StepBar({
  dv,
  stepIndex,
  setStepIndex,
  statusBySeq,
  t,
}: {
  dv: DynamicViewModel;
  stepIndex: number;
  setStepIndex: (i: number) => void;
  statusBySeq: Map<number, StepStatus> | undefined;
  t: Tokens;
}): ReactNode {
  const total = dv.edges.length;
  const current = dv.edges[stepIndex];
  const nameOf = useMemo(() => new Map(dv.participants.map((c) => [c.id, c.name])), [dv.participants]);
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

  return (
    <Box sx={{ mb: 1.5 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
        <IconButton
          disabled={stepIndex <= 0}
          size="small"
          sx={btnSx}
          onClick={() => { setStepIndex(stepIndex - 1); }}
        >
          <ChevronLeftIcon fontSize="small" />
        </IconButton>
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, minWidth: 90, textAlign: 'center' }}>
          Step {stepIndex + 1} of {total}
        </Typography>
        <IconButton
          disabled={stepIndex >= total - 1}
          size="small"
          sx={btnSx}
          onClick={() => { setStepIndex(stepIndex + 1); }}
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
      </Box>
      <Box sx={{ p: 1.25, border: `1.5px solid ${t.line}`, borderLeft: `3px solid ${captionAccent}`, borderRadius: 1, bgcolor: t.paper }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13, color: t.ink, wordBreak: 'break-word' }}>
          {current.seq}. {current.label}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted, mt: 0.25 }}>
          {nameOf.get(current.from) ?? current.from} → {nameOf.get(current.to) ?? current.to}
        </Typography>
      </Box>
    </Box>
  );
}

export function DynamicViewFlow({
  dv,
  resetKey,
  height = 600,
  focalComponentId,
  statusBySeq,
}: {
  /** The ordered call chain to render (system use case or test scenario). */
  dv: DynamicViewModel;
  /** Changing this restarts the step-through at step 1 (e.g. the picked view/scenario id). */
  resetKey: string;
  height?: number;
  /** Optional component id (kebab-case) to visually emphasize in the diagram. */
  focalComponentId?: string;
  /** Optional per-call status colouring (test views): seq → 'red' | 'green'. */
  statusBySeq?: Map<number, StepStatus>;
}): ReactNode {
  const t = useTokens();
  const [stepIndex, setStepIndex] = useState(0);
  // Restart the walk whenever the selected view changes (reset-on-prop-change
  // during render — the React-recommended alternative to a setState effect).
  const [prevKey, setPrevKey] = useState(resetKey);
  if (prevKey !== resetKey) {
    setPrevKey(resetKey);
    setStepIndex(0);
  }
  const safeStep = Math.min(Math.max(stepIndex, 0), Math.max(dv.edges.length - 1, 0));

  const { nodes, edges, colors, usedLayers } = useMemo(
    () => build(dv, t, safeStep, focalComponentId, statusBySeq),
    [dv, t, safeStep, focalComponentId, statusBySeq]
  );

  if (dv.participants.length === 0) {
    return <FlowEmpty label="No call chain to render yet." t={t} />;
  }

  return (
    <Box>
      <StepBar dv={dv} setStepIndex={setStepIndex} statusBySeq={statusBySeq} stepIndex={safeStep} t={t} />
      <FlowCanvas edges={edges} height={height} nodes={nodes} t={t}>
        <LayerLegend colors={colors} t={t} usedLayers={usedLayers} />
      </FlowCanvas>
    </Box>
  );
}
