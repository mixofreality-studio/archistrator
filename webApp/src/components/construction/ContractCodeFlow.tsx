/**
 * ContractCodeFlow — the Code / interface tab of ServiceContractView.
 *
 * Renders a single «interface» xyflow node listing each op. Clicking an op row
 * shifts the interface node right and renders:
 *   - the request structs as one COLUMN on the LEFT (from op.inputs, or derived
 *     from the signature)
 *   - the response and error structs as one COLUMN on the RIGHT
 * connected by directed edges. Clicking the same op again collapses the expansion.
 *
 * Fallback when op.inputs/op.outputs are empty: parses the signature to extract
 * input/output type names and renders them as struct cards with empty fields +
 * a muted note "(fields not detailed in this contract)". Type names are always
 * present in the signature — so clicking always expands something real.
 *
 * FOCUS VIEW ONLY, AND ONLY WITH ROOM (designer checks B1 and N1). The pane lists
 * the signatures instead (ContractSignatureList); the focus view draws this only
 * where the widest expansion fits at CODE_MIN_ZOOM (contractCode.ts). A fit never
 * zooms out past CODE_MIN_ZOOM.
 *
 * WHY COLUMNS, NOT ONE NODE PER STRUCT (N1). The structs used to be stacked at a
 * fixed 200px pitch: an op with 10 params made a ~1.9k px column on a canvas of
 * a few hundred, and a 17-field struct overran the pitch and overlapped the next.
 * Each column is now one node whose cards stack in normal flow — they cannot
 * overlap — and the canvas grows to the expansion's measured height, so every
 * card lands on it. Each card carries its own edge handle.
 *
 * Nothing overlays the cards: the legend is a caption above the canvas, and the
 * zoom controls are gone (their fit button ignored the 0.9 floor).
 */
import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useState,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
} from 'react';
import {
  ReactFlow,
  Background,
  MarkerType,
  Handle,
  Position,
  useReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import type { ContractOp } from '../../contracts/types';
import type { Tokens } from '../../utilities/theme/themes';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { useComments, contractOpAnchor } from '../comments/CommentContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { flowInstanceId } from '../flow/flowInstanceId.ts';
import {
  codeCanvasHeightFor,
  CODE_IFACE_W,
  CODE_INPUT_GAP,
  CODE_MIN_ZOOM,
  CODE_OUTPUT_GAP,
  CODE_STRUCT_W,
  isErrorStructName,
  paramOf,
  parseSignature,
  resolveStructs,
  type ResolvedStruct,
} from './contractCode.ts';

// ---------------------------------------------------------------------------
// Edge helper
// ---------------------------------------------------------------------------

function makeEdge(
  edge: { source: string; target: string; sourceHandle?: string; targetHandle?: string },
  label: string,
  t: Tokens,
  isError = false
): Edge {
  const stroke = isError ? t.dangerFg : t.ink;
  return {
    id: `${edge.source}:${edge.sourceHandle ?? ''}-${edge.target}:${edge.targetHandle ?? ''}`,
    ...edge,
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
// InterfaceNode — clickable op rows
// ---------------------------------------------------------------------------

interface InterfaceNodeData {
  component: string;
  ops: ContractOp[];
  activeOp: string | null;
  /** Toggle an op's request/response expansion (keyboard path — Enter/Space). */
  onToggleOp: (signature: string) => void;
  /** Arm an anchored comment on an op (contractOpAnchor). */
  onCommentOp: (signature: string) => void;
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
          width: CODE_IFACE_W,
          boxSizing: 'border-box',
          bgcolor: t.paperAlt,
          border: `1.5px solid ${t.line}`,
          borderLeft: `5px solid ${t.accent}`,
          borderRadius: '10px',
          overflow: 'hidden',
        }}
      >
        {/* header */}
        <Box sx={{ px: 1.4, py: 0.9, bgcolor: t.paper, borderBottom: `1.5px solid ${t.line}` }}>
          <Typography
            sx={{ fontFamily: t.mono, fontSize: 8.5, color: t.muted, letterSpacing: '0.06em' }}
          >
            «interface» [Component]
          </Typography>
          <Typography
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 14,
              color: t.ink,
              lineHeight: 1.15,
            }}
          >
            {d.component}
          </Typography>
        </Box>
        {/* ops */}
        {d.ops.map((op, i) => {
          const active = op.signature === d.activeOp;
          // data-op is read by the ReactFlow onNodeClick handler to know which
          // op row was clicked (all rows live inside the single interface node).
          // The row is also a real keyboard button (role/tabIndex/aria-expanded):
          // Enter/Space toggles the request/response expansion without the mouse.
          return (
            <Box
              aria-expanded={active}
              aria-label={`${op.signature} — toggle request/response`}
              data-op={op.signature}
              key={`${op.signature}-${String(i)}`}
              role="button"
              sx={{
                position: 'relative',
                px: 1.4,
                py: 0.7,
                pr: 4,
                cursor: 'pointer',
                borderBottom: i === d.ops.length - 1 ? 'none' : `1px solid ${t.line}`,
                borderLeft: `3px solid ${active ? t.accent : 'transparent'}`,
                bgcolor: active ? t.awaitingBg : 'transparent',
                '& .contract-op-comment': { opacity: 0, transition: 'opacity 120ms' },
                '&:hover .contract-op-comment, &:focus-visible .contract-op-comment': {
                  opacity: 1,
                },
                '&:hover': { bgcolor: active ? t.awaitingBg : t.paper },
                '&:focus-visible': {
                  outline: `2px solid ${t.accent}`,
                  outlineOffset: -2,
                  '& .contract-op-comment': { opacity: 1 },
                },
              }}
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  d.onToggleOp(op.signature);
                } else if (e.key === 'c' || e.key === 'C') {
                  e.preventDefault();
                  d.onCommentOp(op.signature);
                }
              }}
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
                <Typography
                  sx={{
                    fontFamily: t.body,
                    fontSize: 10.5,
                    color: t.ink,
                    lineHeight: 1.4,
                    mt: 0.25,
                  }}
                >
                  {op.note}
                </Typography>
              ) : null}
              <Tooltip title="Comment on this operation">
                <IconButton
                  aria-label={`Comment on ${op.signature}`}
                  className="contract-op-comment"
                  data-testid={UI_IDENTIFIERS.Comments.listItemComment(op.signature)}
                  size="small"
                  sx={{
                    position: 'absolute',
                    top: 4,
                    right: 4,
                    width: 20,
                    height: 20,
                    color: t.accentText,
                    bgcolor: t.accent,
                    border: `1.5px solid ${t.line}`,
                    borderRadius: 1,
                    '&:hover': { bgcolor: t.accent2 },
                  }}
                  tabIndex={-1}
                  onClick={(e) => {
                    e.stopPropagation();
                    d.onCommentOp(op.signature);
                  }}
                >
                  <ChatBubbleOutlineIcon sx={{ fontSize: 12 }} />
                </IconButton>
              </Tooltip>
            </Box>
          );
        })}
      </Box>
    </>
  );
}

// ---------------------------------------------------------------------------
// ColumnNode — one side's struct cards, stacked in normal flow
// ---------------------------------------------------------------------------

type Role = 'input' | 'output';

interface ColumnNodeData {
  role: Role;
  structs: ResolvedStruct[];
  [key: string]: unknown;
}

/** The handle id of one card in a column: `in-0`, `out-1`. */
function cardHandle(role: Role, i: number): string {
  return `${role === 'input' ? 'in' : 'out'}-${String(i)}`;
}

function ColumnNode({ data }: NodeProps): ReactNode {
  const d = data as ColumnNodeData;
  return (
    <Box
      sx={{
        width: CODE_STRUCT_W,
        boxSizing: 'border-box',
        display: 'flex',
        flexDirection: 'column',
        gap: 2,
      }}
    >
      {d.structs.map((s, i) => (
        <StructCard
          handleId={cardHandle(d.role, i)}
          key={`${s.name}-${String(i)}`}
          role={d.role}
          struct={s}
        />
      ))}
    </Box>
  );
}

function StructCard({
  struct,
  role,
  handleId,
}: {
  struct: ResolvedStruct;
  role: Role;
  handleId: string;
}): ReactNode {
  const t = useTokens();
  const isErr = isErrorStructName(struct.name);
  const color = isErr ? t.dangerFg : role === 'input' ? t.accent2 : t.committedDot;
  const param = paramOf(struct);
  const roleLabel =
    role === 'input'
      ? param !== undefined
        ? '«param» request →'
        : '«struct» request →'
      : isErr
        ? '«error» ← fail path'
        : '«struct» ← response';
  return (
    <Box
      data-role={role}
      data-struct={struct.name}
      data-testid={UI_IDENTIFIERS.ServiceContract.STRUCT_CARD}
      sx={{
        position: 'relative',
        width: '100%',
        boxSizing: 'border-box',
        bgcolor: t.paperAlt,
        border: `1.5px solid ${isErr ? t.dangerFg : t.line}`,
        borderLeft: `5px solid ${color}`,
        borderRadius: '10px',
      }}
    >
      {/* The card's own handle, so each edge meets its card, not the column. */}
      {role === 'input' ? (
        <Handle id={handleId} position={Position.Right} style={{ opacity: 0 }} type="source" />
      ) : (
        <Handle id={handleId} position={Position.Left} style={{ opacity: 0 }} type="target" />
      )}
      <Box
        sx={{
          px: 1.3,
          py: 0.75,
          bgcolor: t.paper,
          borderRadius: '10px 10px 0 0',
          borderBottom: `1.5px solid ${isErr ? t.dangerFg : t.line}`,
        }}
      >
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 8,
            color: isErr ? t.dangerFg : t.muted,
            letterSpacing: '0.06em',
          }}
        >
          {roleLabel}
        </Typography>
        {param !== undefined ? (
          // A primitive or alias param: one row, the type never a second header.
          <Box
            data-testid={UI_IDENTIFIERS.ServiceContract.PARAM_ROW}
            sx={{ display: 'flex', gap: 0.75, alignItems: 'baseline', flexWrap: 'wrap' }}
          >
            <Typography
              sx={{
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: 12,
                color: isErr ? t.dangerFg : t.ink,
              }}
            >
              {param.name}
            </Typography>
            <Typography sx={{ fontFamily: t.mono, fontSize: 11, color }}>{param.type}</Typography>
          </Box>
        ) : (
          <Typography
            data-testid={UI_IDENTIFIERS.ServiceContract.STRUCT_NAME}
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 12,
              color: isErr ? t.dangerFg : t.ink,
              lineHeight: 1.15,
            }}
          >
            {struct.name}
          </Typography>
        )}
      </Box>
      {param !== undefined ? null : (
        <Box sx={{ px: 1.3, py: 0.6 }}>
          {struct.fields.length > 0 ? (
            struct.fields.map((f) => (
              <Box key={f.name} sx={{ py: 0.25 }}>
                <Box sx={{ display: 'flex', gap: 0.6, alignItems: 'baseline', flexWrap: 'wrap' }}>
                  <Typography
                    sx={{
                      fontFamily: t.mono,
                      fontSize: 10,
                      fontWeight: 700,
                      color: isErr ? t.dangerFg : t.ink,
                    }}
                  >
                    {f.name}
                  </Typography>
                  {f.type.length > 0 ? (
                    <Typography sx={{ fontFamily: t.mono, fontSize: 10, color }}>
                      {f.type}
                    </Typography>
                  ) : null}
                </Box>
                {f.note !== undefined && f.note.length > 0 ? (
                  <Typography
                    sx={{ fontFamily: t.body, fontSize: 9, color: t.muted, lineHeight: 1.25 }}
                  >
                    {f.note}
                  </Typography>
                ) : null}
              </Box>
            ))
          ) : (
            <Typography
              sx={{
                fontFamily: t.body,
                fontSize: 9.5,
                color: t.muted,
                lineHeight: 1.35,
                fontStyle: 'italic',
              }}
            >
              {struct._fallback === true ? '(fields not detailed in this contract)' : '(no fields)'}
            </Typography>
          )}
        </Box>
      )}
    </Box>
  );
}

const nodeTypes = { iface: InterfaceNode, column: ColumnNode };

/** Every fit — on mount and on expand — stops at CODE_MIN_ZOOM (designer check B1). */
const FIT_OPTIONS = { padding: 0.12, minZoom: CODE_MIN_ZOOM, maxZoom: 1 } as const;

/** Frames to wait, at most, for React Flow to measure newly added nodes. */
const MEASURE_FRAMES = 30;

// Re-frames the canvas whenever `dep` changes so the input | interface | output
// columns come into view (fitView only runs once on mount otherwise). Lives as a
// child of <ReactFlow> so it can use the flow hooks. It waits until React Flow
// has MEASURED every node (a new column is added unmeasured, and bounds taken
// then are the interface's alone), then GROWS the canvas to the drawing's height
// at CODE_MIN_ZOOM — the resize re-runs it — and fits, clamped at CODE_MIN_ZOOM,
// so a drawing is never shrunk to an unreadable size.
function FitViewOnChange({
  dep,
  height,
  baseHeight,
  onHeight,
}: {
  dep: string | null;
  height: number;
  baseHeight: number;
  onHeight: (height: number) => void;
}): null {
  const { fitView, getNodes, getNodesBounds, getInternalNode } = useReactFlow();
  useEffect(() => {
    let raf = 0;
    let frames = 0;
    const step = (): void => {
      const nodes = getNodes();
      const unmeasured = nodes.some((n) => {
        const m = getInternalNode(n.id)?.measured;
        return (m?.width ?? 0) === 0 || (m?.height ?? 0) === 0;
      });
      if (unmeasured && frames < MEASURE_FRAMES) {
        frames += 1;
        raf = requestAnimationFrame(step);
        return;
      }
      const need = codeCanvasHeightFor(getNodesBounds(nodes).height, baseHeight);
      if (need !== height) {
        onHeight(need);
        return;
      }
      void fitView({ ...FIT_OPTIONS, duration: 300 });
    };
    raf = requestAnimationFrame(() => {
      raf = requestAnimationFrame(step);
    });
    return (): void => {
      cancelAnimationFrame(raf);
    };
  }, [dep, height, baseHeight, onHeight, fitView, getNodes, getNodesBounds, getInternalNode]);
  return null;
}

// ---------------------------------------------------------------------------
// ContractCodeFlow
// ---------------------------------------------------------------------------

export function ContractCodeFlow({
  component,
  ops,
  height: baseHeight = 420,
  t,
}: {
  component: string;
  ops: ContractOp[];
  /** The canvas's least height; it grows to fit an expansion. */
  height?: number;
  t: Tokens;
}): ReactNode {
  const [activeOp, setActiveOp] = useState<string | null>(null);
  const [height, setHeight] = useState(baseHeight);
  const { setAnchor } = useComments();
  // Its own React Flow id: two canvases on one page (the pane and the focus view,
  // or two diagrams) would otherwise share xyflow's default `1` in every DOM id.
  const rfId = flowInstanceId(useId());

  const toggleOp = useCallback((sig: string): void => {
    setActiveOp((cur) => (cur === sig ? null : sig));
  }, []);

  // Arm an anchored comment on a specific op (rides the next phase-gate send-back).
  const commentOp = useCallback(
    (sig: string): void => {
      setAnchor({
        kind: 'node',
        label: sig,
        source: `${component} · contract op`,
        jsonPath: contractOpAnchor(component, sig),
      });
    },
    [component, setAnchor]
  );

  // React Flow's onNodeClick. Defining it also tells React Flow these nodes are
  // interactive, so it keeps pointer-events enabled on them (otherwise a
  // non-selectable, non-draggable node gets pointer-events:none and the pane
  // behind it swallows the click). Rows carry a data-op attribute we read off
  // the event target to know which op was clicked; clicking the active op again
  // collapses it.
  const onNodeClick = (event: ReactMouseEvent, node: Node): void => {
    if (node.id !== 'iface') return;
    const sig = (event.target as HTMLElement).closest('[data-op]')?.getAttribute('data-op');
    if (sig === null || sig === undefined) return;
    setActiveOp((cur) => (cur === sig ? null : sig));
  };

  const { nodes, edges } = useMemo((): { nodes: Node[]; edges: Edge[] } => {
    const expanded = activeOp !== null;
    // Column x-origins. Input at 0, interface cleared to its right, output
    // cleared past the fixed-width interface so it never overlaps the component.
    const IFACE_X = expanded ? CODE_STRUCT_W + CODE_INPUT_GAP : 0;
    const OUTPUT_X = IFACE_X + CODE_IFACE_W + CODE_OUTPUT_GAP;

    const ns: Node[] = [
      {
        id: 'iface',
        type: 'iface',
        position: { x: IFACE_X, y: 0 },
        data: { component, ops, activeOp, onToggleOp: toggleOp, onCommentOp: commentOp },
        draggable: false,
        selectable: false,
      },
    ];
    const es: Edge[] = [];

    const op = expanded ? ops.find((o) => o.signature === activeOp) : undefined;
    if (op !== undefined) {
      const { inputNames, outputNames } = parseSignature(op.signature);
      const inputStructs = resolveStructs(op.inputs, inputNames);
      const outputStructs = resolveStructs(op.outputs, outputNames);
      const call = `${op.signature.split('(')[0] ?? 'call'}(…)`;

      // LEFT — the request column
      if (inputStructs.length > 0) {
        ns.push({
          id: 'col-in',
          type: 'column',
          position: { x: 0, y: 0 },
          data: { role: 'input', structs: inputStructs },
          draggable: false,
          selectable: false,
        });
        // The call is named once, on the first edge: the edges converge on the
        // interface, and a label per card stacked the same words there.
        inputStructs.forEach((_s, i) => {
          es.push(
            makeEdge(
              { source: 'col-in', sourceHandle: cardHandle('input', i), target: 'iface' },
              i === 0 ? call : '',
              t
            )
          );
        });
      }

      // RIGHT — the response and error column
      if (outputStructs.length > 0) {
        ns.push({
          id: 'col-out',
          type: 'column',
          position: { x: OUTPUT_X, y: 0 },
          data: { role: 'output', structs: outputStructs },
          draggable: false,
          selectable: false,
        });
        outputStructs.forEach((s, i) => {
          const isErr = isErrorStructName(s.name);
          es.push(
            makeEdge(
              { source: 'iface', target: 'col-out', targetHandle: cardHandle('output', i) },
              isErr ? 'returns error' : 'returns',
              t,
              isErr
            )
          );
        });
      }
    }

    return { nodes: ns, edges: es };
  }, [component, ops, activeOp, t, toggleOp, commentOp]);

  const activeMethod = ops.find((o) => o.signature === activeOp);

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75, minWidth: 0 }}>
      {/* The legend, above the canvas: nothing sits over the cards. */}
      <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.45 }}>
        <Box component="span" sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.08em' }}>
          C4 · CODE LEVEL (GO)
        </Box>
        {' · '}
        {activeMethod !== undefined ? (
          <>
            <b>{activeMethod.signature.split('(')[0]}</b> · request (left) → interface (middle) →
            response (right). Click it again to collapse.
          </>
        ) : (
          <>Click an op to expand its request / response structs.</>
        )}
      </Typography>
      <Box
        data-canvas-height={String(height)}
        data-testid={UI_IDENTIFIERS.ServiceContract.CODE_CANVAS_FRAME}
        sx={{
          height,
          width: '100%',
          boxSizing: 'border-box',
          border: `1.5px solid ${t.line}`,
          borderRadius: t.radius / 8 + 0.5,
          bgcolor: t.bg,
        }}
      >
        <ReactFlow
          fitView
          edges={edges}
          fitViewOptions={FIT_OPTIONS}
          id={rfId}
          maxZoom={1.5}
          minZoom={0.3}
          nodeTypes={nodeTypes}
          nodes={nodes}
          nodesConnectable={false}
          nodesDraggable={false}
          proOptions={{ hideAttribution: true }}
          onNodeClick={onNodeClick}
        >
          <FitViewOnChange
            baseHeight={baseHeight}
            dep={activeOp}
            height={height}
            onHeight={setHeight}
          />
          <Background color={t.line} gap={22} size={1} />
        </ReactFlow>
      </Box>
    </Box>
  );
}
