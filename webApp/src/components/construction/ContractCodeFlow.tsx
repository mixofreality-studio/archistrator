/**
 * ContractCodeFlow — the Code / interface tab of ServiceContractView.
 *
 * Renders a single «interface» xyflow node listing each op. Clicking an op row
 * shifts the interface node right and renders:
 *   - request struct nodes on the LEFT (from op.inputs, or derived from signature)
 *   - response struct nodes on the RIGHT (from op.outputs, or derived from signature)
 * connected by directed edges. Clicking the same op again collapses the expansion.
 *
 * Fallback when op.inputs/op.outputs are empty: parses the signature to extract
 * input/output type names and renders them as struct nodes with empty fields +
 * a muted note "(fields not detailed in this contract)". Type names are always
 * present in the signature — so clicking always expands something real.
 */
import { useMemo, useState, type ReactNode } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  Panel,
  MarkerType,
  Handle,
  Position,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import type { ContractOp, ContractStruct } from '../../api/types';
import type { Tokens } from '../../theme/themes';
import { useTokens } from '../../theme/ThemeContext';

// ---------------------------------------------------------------------------
// Signature parser — derive fallback struct names from an op signature string.
//
// Recognises patterns such as:
//   foo(intent: DesignPhaseIntent) → DesignPhaseAck
//   bar(ctx: Context, cmd: BuildCommand) → (BuildResult, error)
//   baz() → error
//
// Returns { inputNames, outputNames } as string arrays (possibly empty when
// the signature can't be parsed, but op.inputs/outputs will cover that case).
// ---------------------------------------------------------------------------

function parseSignature(sig: string): { inputNames: string[]; outputNames: string[] } {
  // Extract input types: content inside the outermost parens after the func name.
  const parenMatch = /\(([^)]*)\)/.exec(sig);
  const inputNames: string[] = [];
  const parenContent = parenMatch?.[1] ?? '';
  if (parenContent.trim().length > 0) {
    // Each param looks like "name: Type" or just "Type".
    for (const param of parenContent.split(',')) {
      const colonIdx = param.indexOf(':');
      const typePart = colonIdx >= 0 ? param.slice(colonIdx + 1) : param;
      const name = typePart.trim().replace(/^\*/, '').replace(/\[\]/, '');
      if (name.length > 0 && name !== 'ctx' && name !== 'context.Context') {
        inputNames.push(name);
      }
    }
  }

  // Extract output types: everything after → or after the closing paren if the
  // signature uses Go's bare return style.
  const arrowIdx = sig.indexOf('→');
  const outputNames: string[] = [];
  if (arrowIdx >= 0) {
    let returnPart = sig.slice(arrowIdx + 1).trim();
    // Strip surrounding parens if present: "(Foo, error)" → "Foo, error"
    if (returnPart.startsWith('(') && returnPart.endsWith(')')) {
      returnPart = returnPart.slice(1, -1);
    }
    for (const part of returnPart.split(',')) {
      const name = part.trim().replace(/^\*/, '').replace(/\[\]/, '');
      if (name.length > 0) {
        outputNames.push(name);
      }
    }
  }

  return { inputNames, outputNames };
}

/** Build ContractStruct[] for display — real data or signature-derived fallback. */
function resolveStructs(
  structs: ContractStruct[] | undefined,
  fallbackNames: string[]
): ContractStruct[] {
  if (structs !== undefined && structs.length > 0) return structs;
  return fallbackNames.map((name): ContractStruct & { _fallback?: boolean } => ({
    name,
    fields: [],
    _fallback: true,
  }));
}

// ---------------------------------------------------------------------------
// Edge helper
// ---------------------------------------------------------------------------

function makeEdge(
  sourceId: string,
  targetId: string,
  label: string,
  t: Tokens,
  isError = false
): Edge {
  const stroke = isError ? t.dangerFg : t.ink;
  return {
    id: `${sourceId}-${targetId}`,
    source: sourceId,
    target: targetId,
    label,
    type: 'smoothstep',
    style: { stroke, strokeWidth: 1.5, strokeDasharray: isError ? '6 3' : undefined },
    labelStyle: { fontFamily: t.mono, fontSize: 9, fontWeight: 700, fill: stroke },
    labelBgStyle: { fill: t.paper, fillOpacity: 0.96 },
    labelBgPadding: [4, 2] as [number, number],
    labelBgBorderRadius: 3,
    markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
  };
}

// ---------------------------------------------------------------------------
// Struct vertical stacking helper
// ---------------------------------------------------------------------------

function stackY(count: number, i: number): number {
  const PITCH = 200;
  return (i - (count - 1) / 2) * PITCH;
}

// ---------------------------------------------------------------------------
// InterfaceNode — clickable op rows
// ---------------------------------------------------------------------------

interface InterfaceNodeData {
  component: string;
  ops: ContractOp[];
  activeOp: string | null;
  onPick: (sig: string) => void;
  [key: string]: unknown;
}

function InterfaceNode({ data }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as InterfaceNodeData;
  return (
    <>
      <Handle position={Position.Left} style={{ opacity: 0 }} type="target" />
      <Handle position={Position.Right} style={{ opacity: 0 }} type="source" />
      <Box
        sx={{
          minWidth: 340,
          maxWidth: 520,
          bgcolor: t.paperAlt,
          border: `1.5px solid ${t.line}`,
          borderLeft: `5px solid ${t.accent}`,
          borderRadius: '10px',
          overflow: 'hidden',
        }}
      >
        {/* header */}
        <Box sx={{ px: 1.4, py: 0.9, bgcolor: t.paper, borderBottom: `1.5px solid ${t.line}` }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 8.5, color: t.muted, letterSpacing: '0.06em' }}>
            «interface» [Component]
          </Typography>
          <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 14, color: t.ink, lineHeight: 1.15 }}>
            {d.component}
          </Typography>
        </Box>
        {/* ops */}
        {d.ops.map((op, i) => {
          const active = op.signature === d.activeOp;
          return (
            <Box
              key={`${op.signature}-${String(i)}`}
              sx={{
                px: 1.4,
                py: 0.7,
                cursor: 'pointer',
                borderBottom: i === d.ops.length - 1 ? 'none' : `1px solid ${t.line}`,
                borderLeft: `3px solid ${active ? t.accent : 'transparent'}`,
                bgcolor: active ? t.awaitingBg : 'transparent',
                '&:hover': { bgcolor: active ? t.awaitingBg : t.paper },
              }}
              onClick={() => { d.onPick(op.signature); }}
            >
              <Typography
                sx={{
                  fontFamily: t.mono,
                  fontSize: 10.5,
                  fontWeight: active ? 700 : 600,
                  color: active ? t.awaitingFg : t.ink,
                  lineHeight: 1.25,
                  wordBreak: 'break-word',
                }}
              >
                {op.signature}
              </Typography>
              <Typography sx={{ fontFamily: t.mono, fontSize: 8.5, color: t.muted, mt: 0.1 }}>
                {op.stereotype}
              </Typography>
              {op.note !== undefined && op.note.length > 0 ? (
                <Typography sx={{ fontFamily: t.body, fontSize: 10.5, color: t.ink, lineHeight: 1.4, mt: 0.25 }}>
                  {op.note}
                </Typography>
              ) : null}
            </Box>
          );
        })}
      </Box>
    </>
  );
}

// ---------------------------------------------------------------------------
// StructNode — field list for request / response / error
// ---------------------------------------------------------------------------

interface StructNodeData {
  struct: ContractStruct & { _fallback?: boolean };
  role: 'input' | 'output';
  [key: string]: unknown;
}

function StructNode({ data }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as StructNodeData;
  // Detect error structs by name convention ("error", "Error", ending in "Error", "Err")
  const isErr = /^error$/i.test(d.struct.name) || d.struct.name.endsWith('Error') || d.struct.name.endsWith('Err');
  const color = isErr ? t.dangerFg : d.role === 'input' ? t.accent2 : t.committedDot;
  return (
    <>
      <Handle position={Position.Left} style={{ opacity: 0 }} type="target" />
      <Handle position={Position.Right} style={{ opacity: 0 }} type="source" />
      <Box
        sx={{
          width: 224,
          bgcolor: t.paperAlt,
          border: `1.5px solid ${isErr ? t.dangerFg : t.line}`,
          borderLeft: `5px solid ${color}`,
          borderRadius: '10px',
          overflow: 'hidden',
        }}
      >
        <Box sx={{ px: 1.3, py: 0.75, bgcolor: t.paper, borderBottom: `1.5px solid ${isErr ? t.dangerFg : t.line}` }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 8, color: isErr ? t.dangerFg : t.muted, letterSpacing: '0.06em' }}>
            {d.role === 'input' ? '«struct» request →' : isErr ? '«error» ← fail path' : '«struct» ← response'}
          </Typography>
          <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: isErr ? t.dangerFg : t.ink, lineHeight: 1.15 }}>
            {d.struct.name}
          </Typography>
        </Box>
        <Box sx={{ px: 1.3, py: 0.6 }}>
          {d.struct.fields.length > 0 ? d.struct.fields.map((f) => (
            <Box key={f.name} sx={{ py: 0.25 }}>
              <Box sx={{ display: 'flex', gap: 0.6, alignItems: 'baseline', flexWrap: 'wrap' }}>
                <Typography sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 700, color: isErr ? t.dangerFg : t.ink }}>
                  {f.name}
                </Typography>
                {f.type.length > 0 ? (
                  <Typography sx={{ fontFamily: t.mono, fontSize: 10, color }}>{f.type}</Typography>
                ) : null}
              </Box>
              {f.note !== undefined && f.note.length > 0 ? (
                <Typography sx={{ fontFamily: t.body, fontSize: 9, color: t.muted, lineHeight: 1.25 }}>{f.note}</Typography>
              ) : null}
            </Box>
          )) : (
            <Typography sx={{ fontFamily: t.body, fontSize: 9.5, color: t.muted, lineHeight: 1.35, fontStyle: 'italic' }}>
              (fields not detailed in this contract)
            </Typography>
          )}
        </Box>
      </Box>
    </>
  );
}

const nodeTypes = { iface: InterfaceNode, struct: StructNode };

// ---------------------------------------------------------------------------
// ContractCodeFlow
// ---------------------------------------------------------------------------

export function ContractCodeFlow({
  component,
  ops,
  height = 420,
  t,
}: {
  component: string;
  ops: ContractOp[];
  height?: number;
  t: Tokens;
}): ReactNode {
  const [activeOp, setActiveOp] = useState<string | null>(null);

  const pick = (sig: string): void => {
    setActiveOp((cur) => (cur === sig ? null : sig));
  };

  // Stable callback ref — wrapped so it can be passed via node data without
  // triggering infinite re-renders. The nodeTypes are module-level constants.
  const { nodes, edges } = useMemo((): { nodes: Node[]; edges: Edge[] } => {
    const expanded = activeOp !== null;
    const IFACE_X = expanded ? 320 : 0;

    const ns: Node[] = [
      {
        id: 'iface',
        type: 'iface',
        position: { x: IFACE_X, y: 0 },
        data: { component, ops, activeOp, onPick: pick },
        draggable: false,
        selectable: false,
      },
    ];
    const es: Edge[] = [];

    if (expanded) {
      const op = ops.find((o) => o.signature === activeOp);
      if (op !== undefined) {
        const { inputNames, outputNames } = parseSignature(op.signature);
        const inputStructs = resolveStructs(op.inputs, inputNames);
        const outputStructs = resolveStructs(op.outputs, outputNames);

        // LEFT — request struct(s)
        inputStructs.forEach((s, i) => {
          const id = `in-${s.name}-${String(i)}`;
          ns.push({
            id,
            type: 'struct',
            position: { x: -300, y: stackY(inputStructs.length, i) },
            data: { struct: s, role: 'input' },
            draggable: false,
            selectable: false,
          });
          es.push(makeEdge(id, 'iface', `${op.signature.split('(')[0] ?? 'call'}(…)`, t, false));
        });

        // RIGHT — response struct(s)
        outputStructs.forEach((s, i) => {
          const id = `out-${s.name}-${String(i)}`;
          const isErr = /^error$/i.test(s.name) || s.name.endsWith('Error') || s.name.endsWith('Err');
          ns.push({
            id,
            type: 'struct',
            position: { x: IFACE_X + 380, y: stackY(outputStructs.length, i) },
            data: { struct: s, role: 'output' },
            draggable: false,
            selectable: false,
          });
          es.push(makeEdge('iface', id, isErr ? 'returns error' : 'returns', t, isErr));
        });
      }
    }

    return { nodes: ns, edges: es };
  }, [component, ops, activeOp, t]);

  const activeMethod = ops.find((o) => o.signature === activeOp);

  return (
    <Box
      sx={{
        height,
        width: '100%',
        border: `1.5px solid ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        bgcolor: t.bg,
      }}
    >
      <ReactFlow
        fitView
        edges={edges}
        fitViewOptions={{ padding: 0.25 }}
        maxZoom={1.5}
        minZoom={0.3}
        nodeTypes={nodeTypes}
        nodes={nodes}
        nodesConnectable={false}
        nodesDraggable={false}
        proOptions={{ hideAttribution: true }}
      >
        <Background color={t.line} gap={22} size={1} />
        <Controls showInteractive={false} />
        <Panel position="top-left">
          <Box
            sx={{
              p: 1,
              bgcolor: t.paper,
              border: `1.5px solid ${t.line}`,
              borderRadius: t.radius / 8 + 0.5,
              maxWidth: 260,
            }}
          >
            <Typography
              sx={{
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: 9,
                letterSpacing: '0.12em',
                textTransform: 'uppercase',
                color: t.muted,
                mb: 0.3,
              }}
            >
              C4 · Code level (Go)
            </Typography>
            <Typography sx={{ fontFamily: t.body, fontSize: 10, color: t.ink, lineHeight: 1.35 }}>
              {activeMethod !== undefined ? (
                <>
                  <b>{activeMethod.signature.split('(')[0]}</b> · request (left) → interface (middle) → response (right). Click again to collapse.
                </>
              ) : (
                <>Click an op to expand its request / response structs.</>
              )}
            </Typography>
          </Box>
        </Panel>
      </ReactFlow>
    </Box>
  );
}
