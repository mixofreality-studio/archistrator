/**
 * The code-level diagram's two node kinds (codeDiagramModel.ts has the language):
 *
 *   StackNode      one column of class boxes stacked in normal flow (they cannot
 *                  overlap), each box carrying its own edge handle on its inner
 *                  side — and, for a state and the proof that reads it, a dotted
 *                  link down the column.
 *   InterfaceNode  the «interface» class: one row per operation, the highlighted
 *                  row carrying the in/out handles the side columns' edges meet.
 *
 * The rows of an interactive interface (the contract's) are real keyboard
 * buttons; their comment button is the row's SIBLING, never its child (a button
 * inside a button is nested-interactive).
 */
import { Fragment, useLayoutEffect, useRef, type ReactNode } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';

import { useTokens } from '../../../utilities/theme/ThemeContext';
import {
  boxHandle,
  IFACE_IN,
  IFACE_OUT,
  roleColor,
  type ClassBoxSpec,
  type ClassRow,
} from './codeDiagramModel.ts';

const HIDDEN = { opacity: 0, width: 6, height: 6, minWidth: 0, minHeight: 0 } as const;

// ---------------------------------------------------------------------------
// ClassBox
// ---------------------------------------------------------------------------

function RowValue({ row }: { row: ClassRow }): ReactNode {
  const t = useTokens();
  if (row.tone === 'owed') {
    return (
      <Box
        component="span"
        sx={{
          display: 'inline-block',
          px: 0.75,
          border: `1.5px dashed ${t.dangerFg}`,
          borderRadius: 1,
          color: t.dangerFg,
          fontWeight: 700,
          fontSize: 11.5,
        }}
      >
        {row.value ?? 'not bound yet'}
      </Box>
    );
  }
  return (
    <Box
      component="span"
      sx={{
        color: row.tone === 'danger' ? t.dangerFg : row.tone === 'muted' ? t.muted : t.ink,
        fontStyle: row.tone === 'muted' ? 'italic' : 'normal',
      }}
      title={row.valueTip}
    >
      {breakable(row.value ?? '')}
    </Box>
  );
}

/**
 * A value with a line-break opportunity after every `/`, so a path wraps at its
 * segments rather than mid-word (only a segment too long for the box breaks).
 */
function breakable(text: string): ReactNode {
  if (!text.includes('/')) return text;
  const parts = text.split('/');
  return parts.map((p, i) => (
    // Positional by nature: the segments of one string.
    <Fragment key={String(i)}>
      {p}
      {i < parts.length - 1 ? (
        <>
          /<wbr />
        </>
      ) : null}
    </Fragment>
  ));
}

/** One class box: the stereotype and name compartment, then its attribute rows. */
export function ClassBox({
  spec,
  side,
}: {
  spec: ClassBoxSpec;
  side: 'left' | 'right';
}): ReactNode {
  const t = useTokens();
  const color = roleColor(t, spec.role);
  const danger = spec.role === 'error';
  const hasRows = spec.rows.length > 0;
  return (
    <Box
      data-role={spec.role}
      data-struct={spec.dataStruct ?? spec.title}
      data-testid={spec.testId}
      sx={{
        position: 'relative',
        width: '100%',
        boxSizing: 'border-box',
        bgcolor: t.paperAlt,
        border: `1.5px solid ${danger ? t.dangerFg : t.line}`,
        borderTop: `5px solid ${color}`,
        borderStyle: spec.role === 'proof' ? 'dashed' : 'solid',
        borderTopStyle: 'solid',
        borderRadius: '10px',
      }}
    >
      {/* The in/out edge meets the box on its INNER side, level with its title. */}
      <Handle
        id={boxHandle.side(spec.id)}
        isConnectable={false}
        position={side === 'left' ? Position.Right : Position.Left}
        style={{ ...HIDDEN, top: 20 }}
        type={side === 'left' ? 'source' : 'target'}
      />
      {spec.provedBelow === true ? (
        <Handle
          id={boxHandle.proofOut(spec.id)}
          isConnectable={false}
          position={Position.Bottom}
          style={HIDDEN}
          type="source"
        />
      ) : null}
      {spec.provesAbove === true ? (
        <Handle
          id={boxHandle.proofIn(spec.id)}
          isConnectable={false}
          position={Position.Top}
          style={HIDDEN}
          type="target"
        />
      ) : null}
      <Box
        sx={{
          px: 1.25,
          py: 0.6,
          bgcolor: t.paper,
          borderRadius: hasRows ? '6px 6px 0 0' : '6px',
          borderBottom: hasRows ? `1.5px solid ${danger ? t.dangerFg : t.line}` : 'none',
          textAlign: 'center',
        }}
      >
        <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.3 }}>
          {spec.stereotype}
        </Typography>
        <Typography
          data-testid={spec.nameTestId}
          sx={{
            fontFamily: t.body,
            fontWeight: 700,
            fontSize: 13.5,
            lineHeight: 1.25,
            color: danger ? t.dangerFg : t.ink,
            overflowWrap: 'break-word',
          }}
          title={spec.titleTip}
        >
          {spec.title}
        </Typography>
        {!hasRows && spec.emptyText !== undefined ? (
          <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
            {spec.emptyText}
          </Typography>
        ) : null}
      </Box>
      {hasRows ? (
        <Box
          sx={{
            px: 1.25,
            py: 0.5,
            display: 'grid',
            // The name column is as wide as the longest name — names never wrap
            // mid-word; values wrap at word boundaries (a long token breaks only
            // when it cannot fit at all).
            gridTemplateColumns: 'max-content minmax(0,1fr)',
            columnGap: 1.25,
            rowGap: 0.35,
            alignItems: 'baseline',
          }}
        >
          {spec.rows.map((r, i) => {
            const heading = r.section !== undefined && r.section !== spec.rows[i - 1]?.section;
            return (
              <Box data-testid={spec.rowTestId} key={r.key} sx={{ display: 'contents' }}>
                {heading ? (
                  <Typography
                    sx={{
                      gridColumn: '1 / -1',
                      fontFamily: t.body,
                      fontSize: 11,
                      fontWeight: 700,
                      color: t.muted,
                      mt: 0.25,
                    }}
                  >
                    {r.section}
                  </Typography>
                ) : null}
                <Typography
                  sx={{
                    fontFamily: t.body,
                    fontSize: 12.5,
                    fontWeight: 600,
                    color: r.tone === 'danger' || r.tone === 'owed' ? t.dangerFg : t.ink,
                    whiteSpace: 'nowrap',
                  }}
                >
                  {r.name}
                </Typography>
                <Typography
                  component="div"
                  sx={{
                    fontFamily: t.body,
                    fontSize: 12.5,
                    lineHeight: 1.4,
                    minWidth: 0,
                    overflowWrap: 'break-word',
                  }}
                >
                  <RowValue row={r} />
                  {r.note !== undefined && r.note.length > 0 ? (
                    <Box component="span" sx={{ display: 'block', color: t.muted, fontSize: 11.5 }}>
                      {r.note}
                    </Box>
                  ) : null}
                </Typography>
              </Box>
            );
          })}
        </Box>
      ) : null}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// StackNode — one column of class boxes
// ---------------------------------------------------------------------------

export interface StackNodeData {
  side: 'left' | 'right';
  width: number;
  boxes: ClassBoxSpec[];
  [key: string]: unknown;
}

export function StackNode({ data }: NodeProps): ReactNode {
  const d = data as StackNodeData;
  return (
    <Box
      sx={{
        width: d.width,
        boxSizing: 'border-box',
        display: 'flex',
        flexDirection: 'column',
        gap: 1.75,
      }}
    >
      {d.boxes.map((b) => (
        <ClassBox key={b.id} side={d.side} spec={b} />
      ))}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// InterfaceNode — the «interface» class
// ---------------------------------------------------------------------------

export interface InterfaceRow {
  id: string;
  name: string;
  /** One muted line under the name: what it takes and returns. */
  detail?: string | undefined;
  note?: string | undefined;
  active: boolean;
  /** Present on an interactive interface (the contract): the row is a button. */
  interactive?:
    | {
        ariaLabel: string;
        onToggle: () => void;
        onComment: () => void;
        commentLabel: string;
        commentTestId: string;
        dataOp: string;
      }
    | undefined;
}

export interface InterfaceNodeData {
  stereotype: string;
  title: string;
  /** One line under the title (a shared error, the component's layer). */
  subtitle?: string | undefined;
  subtitleTip?: string | undefined;
  width: number;
  rows: InterfaceRow[];
  /** Muted closing line ("+ 9 other operations"). */
  footer?: string | undefined;
  testId?: string | undefined;
  /** Reports the highlighted row's top, so the side columns can sit level with it. */
  onActiveRowTop?: ((top: number) => void) | undefined;
  [key: string]: unknown;
}

function RowBody({ row }: { row: InterfaceRow }): ReactNode {
  const t = useTokens();
  return (
    <>
      <Typography
        sx={{ fontFamily: t.body, fontSize: 13.5, fontWeight: 700, color: t.ink, lineHeight: 1.3 }}
      >
        {row.name}
      </Typography>
      {row.detail !== undefined ? (
        <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted, lineHeight: 1.4 }}>
          {row.detail}
        </Typography>
      ) : null}
      {row.note !== undefined && row.note.length > 0 ? (
        <Typography
          sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.4, mt: 0.25 }}
        >
          {row.note}
        </Typography>
      ) : null}
    </>
  );
}

export function InterfaceNode({ data }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as InterfaceNodeData;
  const activeRef = useRef<HTMLDivElement | null>(null);
  const activeId = d.rows.find((r) => r.active)?.id;
  const report = d.onActiveRowTop;
  useLayoutEffect(() => {
    if (report === undefined || activeRef.current === null) return;
    report(activeRef.current.offsetTop);
  }, [report, activeId]);
  return (
    <Box
      data-testid={d.testId}
      sx={{
        width: d.width,
        boxSizing: 'border-box',
        bgcolor: t.paperAlt,
        border: `1.5px solid ${t.line}`,
        borderTop: `5px solid ${t.accent}`,
        borderRadius: '10px',
        position: 'relative',
      }}
    >
      <Box
        sx={{
          px: 1.25,
          py: 0.75,
          bgcolor: t.paper,
          borderRadius: '6px 6px 0 0',
          borderBottom: `1.5px solid ${t.line}`,
          textAlign: 'center',
        }}
      >
        <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted }}>
          {d.stereotype}
        </Typography>
        <Typography
          sx={{
            fontFamily: t.display,
            fontWeight: 800,
            fontSize: 16,
            color: t.ink,
            lineHeight: 1.2,
          }}
        >
          {d.title}
        </Typography>
        {d.subtitle !== undefined ? (
          <Typography
            sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted }}
            title={d.subtitleTip}
          >
            {d.subtitle}
          </Typography>
        ) : null}
      </Box>
      {d.rows.map((row, i) => {
        const last = i === d.rows.length - 1 && d.footer === undefined;
        const rowSx = {
          px: 1.25,
          py: 0.7,
          borderLeft: `4px solid ${row.active ? t.accent : 'transparent'}`,
          bgcolor: row.active ? t.awaitingBg : 'transparent',
        } as const;
        return (
          <Box
            key={row.id}
            ref={row.active ? activeRef : undefined}
            sx={{
              position: 'relative',
              borderBottom: last ? 'none' : `1px solid ${t.line}`,
              '& .code-row-comment': { opacity: 0, transition: 'opacity 120ms' },
              '&:hover .code-row-comment, &:focus-within .code-row-comment': { opacity: 1 },
            }}
          >
            {row.active ? (
              <>
                <Handle
                  id={IFACE_IN}
                  isConnectable={false}
                  position={Position.Left}
                  style={HIDDEN}
                  type="target"
                />
                <Handle
                  id={IFACE_OUT}
                  isConnectable={false}
                  position={Position.Right}
                  style={HIDDEN}
                  type="source"
                />
              </>
            ) : null}
            {row.interactive !== undefined ? (
              <Box
                aria-expanded={row.active}
                aria-label={row.interactive.ariaLabel}
                data-op={row.interactive.dataOp}
                role="button"
                sx={{
                  ...rowSx,
                  pr: 5.5,
                  cursor: 'pointer',
                  '&:hover': { bgcolor: row.active ? t.awaitingBg : t.paper },
                  '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: -2 },
                }}
                tabIndex={0}
                onClick={row.interactive.onToggle}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    row.interactive?.onToggle();
                  } else if (e.key === 'c' || e.key === 'C') {
                    e.preventDefault();
                    row.interactive?.onComment();
                  }
                }}
              >
                <RowBody row={row} />
              </Box>
            ) : (
              <Box sx={rowSx}>
                <RowBody row={row} />
              </Box>
            )}
            {row.interactive !== undefined ? (
              <Tooltip title="Comment on this operation">
                <IconButton
                  aria-label={row.interactive.commentLabel}
                  className="code-row-comment"
                  data-testid={row.interactive.commentTestId}
                  size="small"
                  sx={{
                    position: 'absolute',
                    top: 6,
                    right: 6,
                    width: 28,
                    height: 28,
                    color: t.accentText,
                    bgcolor: t.accent,
                    border: `1.5px solid ${t.line}`,
                    borderRadius: 1,
                    '&:hover': { bgcolor: t.accent2 },
                  }}
                  tabIndex={-1}
                  onClick={(e) => {
                    e.stopPropagation();
                    row.interactive?.onComment();
                  }}
                >
                  <ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />
                </IconButton>
              </Tooltip>
            ) : null}
          </Box>
        );
      })}
      {d.footer !== undefined ? (
        <Typography sx={{ px: 1.25, py: 0.6, fontFamily: t.body, fontSize: 12, color: t.muted }}>
          {d.footer}
        </Typography>
      ) : null}
    </Box>
  );
}
