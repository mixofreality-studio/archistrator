import { type ReactNode, useMemo } from 'react';
import { Handle, Position, type Edge, type Node, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { FlowCanvas } from '../../flow/flowShared';
import { flowEdge } from '../../flow/flowLayout';
import { useTokens } from '../../../theme/ThemeContext';
import type { Tokens } from '../../../theme/themes';

/** One black-box step: a transport-agnostic manager operation call. */
export interface SequenceStep {
  seq: number;
  component: string;
  operation: string;
  note: string;
}

// --- Custom react-flow node for one operation-call step (module-local) ---
type StepData = SequenceStep & { t: Tokens };

function StepNode({ data }: NodeProps): ReactNode {
  const d = data as unknown as StepData;
  const t = d.t;
  return (
    <Box
      sx={{
        px: 1, py: 0.75, minWidth: 220, maxWidth: 300,
        bgcolor: t.paper, border: `1.5px solid ${t.line}`, borderRadius: t.radius / 8 + 0.5,
        borderLeft: `4px solid ${t.accent}`,
      }}
    >
      <Handle position={Position.Top} style={{ opacity: 0 }} type="target" />
      <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.6 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 800, color: t.accent }}>
          {String(d.seq)}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>{d.component}</Typography>
      </Box>
      <Typography sx={{ fontFamily: t.mono, fontSize: 12.5, fontWeight: 700, color: t.ink, lineHeight: 1.2 }}>
        {d.operation}()
      </Typography>
      {d.note.length > 0 ? (
        <Typography sx={{ fontFamily: t.body, fontSize: 10.5, color: t.muted, mt: 0.2, lineHeight: 1.3 }}>
          {d.note}
        </Typography>
      ) : null}
      <Handle position={Position.Bottom} style={{ opacity: 0 }} type="source" />
    </Box>
  );
}

const nodeTypes = { step: StepNode };
const ROW = 118;

/**
 * A vertical black-box operation-call ladder: each step is a {component,
 * operation} manager call, chained top-to-bottom by sequence. Transport-agnostic
 * (no HTTP routes) — the same sequence drives REST/MCP/gRPC test clients.
 */
export function SequenceFlow({ steps }: { steps: SequenceStep[] }): ReactNode {
  const t = useTokens();
  const { nodes, edges } = useMemo(() => {
    const ns: Node[] = steps.map((s, i) => ({
      id: String(s.seq),
      type: 'step',
      position: { x: 0, y: i * ROW },
      data: { ...s, t },
      draggable: false,
    }));
    const es: Edge[] = steps.slice(1).map((s, i) =>
      flowEdge(`e-${String(steps[i]?.seq)}-${String(s.seq)}`, String(steps[i]?.seq), String(s.seq), '', t)
    );
    return { nodes: ns, edges: es };
  }, [steps, t]);

  if (steps.length === 0) return null;
  return <FlowCanvas edges={edges} height={Math.max(180, steps.length * ROW + 40)} nodeTypes={nodeTypes} nodes={nodes} t={t} />;
}
