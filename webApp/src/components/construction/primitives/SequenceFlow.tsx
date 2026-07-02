import { type ReactNode, useMemo } from 'react';
import { Handle, Position, MarkerType, type Edge, type Node, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { FlowCanvas } from '../../flow/flowShared';
import { useTokens } from '../../../theme/ThemeContext';
import type { Tokens } from '../../../theme/themes';

/** One black-box step: a transport-agnostic manager operation call. */
export interface SequenceStep {
  seq: number;
  component: string;
  operation: string;
  note: string;
  status?: string; // '' | 'red' | 'green'
}

/** 'plan' → every call shown as a red target (N-STP); 'run' → coloured by status (N-IT). */
export type SequenceMode = 'plan' | 'run';

const COL_W = 240;
const HEAD_W = 200;
const HEAD_Y = 0;
const FIRST_ROW_Y = 78;
const ROW_H = 76;
const centerX = (col: number): number => col * COL_W + HEAD_W / 2;

// --- Participant header (a lifeline top). The Test harness is the actor/driver. ---
function SeqHead({ data }: NodeProps): ReactNode {
  const d = data as unknown as { label: string; actor: boolean; t: Tokens };
  const t = d.t;
  return (
    <Box
      sx={{
        width: HEAD_W, px: 1, py: 0.6, textAlign: 'center',
        bgcolor: d.actor ? t.accent : t.paperAlt,
        color: d.actor ? t.accentText : t.ink,
        border: `1.5px solid ${d.actor ? t.accent : t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        fontFamily: t.mono, fontSize: 11.5, fontWeight: 800,
      }}
    >
      {d.actor ? '⧉ ' : ''}{d.label}
      <Handle id="b" position={Position.Bottom} style={{ opacity: 0 }} type="source" />
    </Box>
  );
}

// --- Invisible lifeline anchor with 4 handles (lifeline: t/b, message: l/r). ---
function SeqAnchor(): ReactNode {
  return (
    <Box sx={{ width: 6, height: 6 }}>
      <Handle id="t" position={Position.Top} style={{ opacity: 0 }} type="target" />
      <Handle id="b" position={Position.Bottom} style={{ opacity: 0 }} type="source" />
      <Handle id="l" position={Position.Left} style={{ opacity: 0 }} type="target" />
      <Handle id="r" position={Position.Right} style={{ opacity: 0 }} type="source" />
    </Box>
  );
}

const nodeTypes = { seqHead: SeqHead, seqAnchor: SeqAnchor };

/**
 * A black-box system-test SEQUENCE diagram. A single Test harness lifeline (the
 * component that sequences the calls) drives ordered operation calls out to the
 * target components — time flows down. The managers never call each other; the
 * Test drives every call. Transport-agnostic (component.operation, not routes).
 */
export function SequenceFlow({ steps, mode = 'plan' }: { steps: SequenceStep[]; mode?: SequenceMode }): ReactNode {
  const t = useTokens();
  const { nodes, edges, height } = useMemo(() => {
    // participants: Test (the driver) first, then each distinct component in order.
    const participants = ['Test'];
    for (const s of steps) {
      if (!participants.includes(s.component)) participants.push(s.component);
    }
    const colOf = (name: string): number => participants.indexOf(name);
    const n = steps.length;

    const ns: Node[] = [];
    // headers
    participants.forEach((p, i) => {
      ns.push({
        id: `head:${String(i)}`,
        type: 'seqHead',
        position: { x: i * COL_W, y: HEAD_Y },
        data: { label: i === 0 ? 'Test harness' : p, actor: i === 0, t },
        draggable: false,
        selectable: false,
      });
    });
    // anchors: one per (participant, row) so lifelines run full height and
    // message arrows have endpoints on both lifelines.
    participants.forEach((_, i) => {
      for (let r = 0; r < n; r++) {
        ns.push({
          id: `a:${String(i)}:${String(r)}`,
          type: 'seqAnchor',
          position: { x: centerX(i) - 3, y: FIRST_ROW_Y + r * ROW_H },
          data: {},
          draggable: false,
          selectable: false,
        });
      }
    });

    const es: Edge[] = [];
    // lifelines (dashed vertical): head → first anchor → … → last anchor.
    participants.forEach((_, i) => {
      es.push({
        id: `ll:${String(i)}:h`,
        source: `head:${String(i)}`,
        sourceHandle: 'b',
        target: `a:${String(i)}:0`,
        targetHandle: 't',
        type: 'straight',
        style: { stroke: t.line, strokeWidth: 1.5, strokeDasharray: '4 4' },
      });
      for (let r = 0; r < n - 1; r++) {
        es.push({
          id: `ll:${String(i)}:${String(r)}`,
          source: `a:${String(i)}:${String(r)}`,
          sourceHandle: 'b',
          target: `a:${String(i)}:${String(r + 1)}`,
          targetHandle: 't',
          type: 'straight',
          style: { stroke: t.line, strokeWidth: 1.5, strokeDasharray: '4 4' },
        });
      }
    });
    // messages: Test lifeline → target component lifeline, one per step (time down).
    steps.forEach((s, r) => {
      const j = colOf(s.component);
      const green = mode === 'run' && s.status === 'green';
      const color = green ? t.committedDot : t.dangerFg;
      es.push({
        id: `m:${String(r)}`,
        source: `a:0:${String(r)}`,
        sourceHandle: 'r',
        target: `a:${String(j)}:${String(r)}`,
        targetHandle: 'l',
        type: 'straight',
        label: `${String(s.seq)}. ${s.operation}()${green ? '  ✓' : ''}`,
        labelStyle: { fontFamily: t.mono, fontSize: 10.5, fontWeight: 700, fill: color },
        labelBgStyle: { fill: t.paper, fillOpacity: 0.95 },
        labelBgPadding: [5, 3] as [number, number],
        style: { stroke: color, strokeWidth: 1.75 },
        markerEnd: { type: MarkerType.ArrowClosed, color },
      });
    });

    return { nodes: ns, edges: es, height: Math.max(200, FIRST_ROW_Y + n * ROW_H + 30) };
  }, [steps, mode, t]);

  if (steps.length === 0) return null;
  return (
    <>
      <FlowCanvas edges={edges} height={height} nodeTypes={nodeTypes} nodes={nodes} t={t} />
      <Box sx={{ display: 'flex', gap: 2, mt: 0.5 }}>
        <Legend color={t.dangerFg} label={mode === 'plan' ? 'target (must pass)' : 'failing'} t={t} />
        {mode === 'run' ? <Legend color={t.committedDot} label="passing" t={t} /> : null}
      </Box>
    </>
  );
}

function Legend({ color, label, t }: { color: string; label: string; t: Tokens }): ReactNode {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
      <Box sx={{ width: 14, height: 3, bgcolor: color }} />
      <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>{label}</Typography>
    </Box>
  );
}
