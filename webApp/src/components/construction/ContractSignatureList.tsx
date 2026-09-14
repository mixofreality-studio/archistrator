/**
 * The Code tab as an HTML SIGNATURE LIST — what the pane shows (designer check
 * on renderers S1, B1).
 *
 * The code diagram cannot be read in the pane: its interface node is 560px and
 * an expanded op ~1100px, so in a 480–820px pane it fit to ~6px signatures and
 * re-fit to ~4px on expand. So below 900px the tab is this list instead: every
 * op, 12px mono, wrapping; a row expands INLINE into its request / response /
 * error tables — the same structs the canvas draws as nodes, from the same
 * reading (contractCode.opStructsFor). "Open diagram in focus view" sits at the
 * top; the canvas draws there.
 *
 * Each row keeps the canvas's comment anchor (contractOpAnchor), so a Design
 * Review can still pin feedback to one operation.
 */
import { useState, type ReactElement } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import ChevronRightRoundedIcon from '@mui/icons-material/ChevronRightRounded';
import OpenInFullRoundedIcon from '@mui/icons-material/OpenInFullRounded';

import type { ContractOp } from '../../contracts/types';
import type { Tokens } from '../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { useComments, contractOpAnchor } from '../comments/CommentContext';
import { opStructsFor, type ResolvedStruct } from './contractCode.ts';

export function ContractSignatureList({
  component,
  ops,
  t,
  count,
  onOpenFocus,
  needsRoomNote,
}: {
  component: string;
  ops: ContractOp[];
  t: Tokens;
  /** "10 ops" — said on the row the focus button sits on. */
  count: string;
  /** Open the focus view, where the diagram draws. Absent inside the focus view. */
  onOpenFocus?: (() => void) | undefined;
  /** In the focus view with no room for the canvas: say why it is a list. */
  needsRoomNote?: string | undefined;
}): ReactElement {
  const { setAnchor, enabled } = useComments();
  const [open, setOpen] = useState<ReadonlySet<number>>(() => new Set());
  const toggle = (i: number): void => {
    setOpen((cur) => {
      const next = new Set(cur);
      if (next.has(i)) next.delete(i);
      else next.add(i);
      return next;
    });
  };
  const comment = (sig: string): void => {
    setAnchor({
      kind: 'node',
      label: sig,
      source: `${component} · contract op`,
      jsonPath: contractOpAnchor(component, sig),
    });
  };

  return (
    <Box
      data-testid={UI_IDENTIFIERS.ServiceContract.SIGNATURE_LIST}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1, minWidth: 0 }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
        {onOpenFocus !== undefined ? (
          <Button
            data-testid={UI_IDENTIFIERS.ServiceContract.OPEN_FOCUS}
            size="small"
            startIcon={<OpenInFullRoundedIcon sx={{ fontSize: 14 }} />}
            sx={{ fontFamily: t.mono, fontSize: 11, fontWeight: 700, textTransform: 'none' }}
            variant="outlined"
            onClick={onOpenFocus}
          >
            Open diagram in focus view
          </Button>
        ) : null}
        <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.4 }}>
          {`«interface» · ${count} · open one for its request, response and error`}
        </Typography>
      </Box>
      {needsRoomNote !== undefined ? (
        <Typography
          data-testid={UI_IDENTIFIERS.ServiceContract.CANVAS_NEEDS_ROOM}
          sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.45 }}
        >
          {needsRoomNote}
        </Typography>
      ) : null}
      <Box
        component="ul"
        sx={{
          m: 0,
          p: 0,
          listStyle: 'none',
          border: `1.5px solid ${t.line}`,
          borderLeft: `5px solid ${t.accent}`,
          borderRadius: '10px',
          bgcolor: t.paperAlt,
          overflow: 'hidden',
          minWidth: 0,
        }}
      >
        {ops.map((op, i) => {
          const expanded = open.has(i);
          return (
            <Box
              component="li"
              key={`${op.signature}-${String(i)}`}
              sx={{
                borderBottom: i === ops.length - 1 ? 'none' : `1px solid ${t.line}`,
                bgcolor: expanded ? t.paper : 'transparent',
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'flex-start', minWidth: 0 }}>
                <Box
                  aria-expanded={expanded}
                  aria-label={`${op.signature} — show request, response and error`}
                  component="button"
                  data-op={op.signature}
                  data-testid={UI_IDENTIFIERS.ServiceContract.opRow(i)}
                  sx={{
                    flexGrow: 1,
                    minWidth: 0,
                    display: 'flex',
                    alignItems: 'flex-start',
                    gap: 0.5,
                    px: 1,
                    py: 0.75,
                    border: 'none',
                    bgcolor: 'transparent',
                    textAlign: 'left',
                    cursor: 'pointer',
                    color: 'inherit',
                    font: 'inherit',
                    '&:hover': { bgcolor: t.paper },
                    '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: -2 },
                  }}
                  type="button"
                  onClick={() => {
                    toggle(i);
                  }}
                >
                  <ChevronRightRoundedIcon
                    sx={{
                      fontSize: 16,
                      mt: '1px',
                      color: t.muted,
                      flexShrink: 0,
                      transform: expanded ? 'rotate(90deg)' : 'none',
                      transition: 'transform 120ms',
                    }}
                  />
                  <Box sx={{ minWidth: 0 }}>
                    <Typography
                      data-testid={UI_IDENTIFIERS.ServiceContract.OP_SIGNATURE}
                      sx={{
                        fontFamily: t.mono,
                        fontSize: 12,
                        fontWeight: 600,
                        color: t.ink,
                        lineHeight: 1.4,
                        wordBreak: 'break-word',
                      }}
                    >
                      {op.signature}
                    </Typography>
                    {op.stereotype.length > 0 ? (
                      <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
                        {op.stereotype}
                      </Typography>
                    ) : null}
                    {op.note !== undefined && op.note.length > 0 ? (
                      <Typography
                        sx={{
                          fontFamily: t.body,
                          fontSize: 11.5,
                          color: t.ink,
                          lineHeight: 1.45,
                          mt: 0.25,
                        }}
                      >
                        {op.note}
                      </Typography>
                    ) : null}
                  </Box>
                </Box>
                {enabled ? (
                  <Tooltip title="Comment on this operation">
                    <IconButton
                      aria-label={`Comment on ${op.signature}`}
                      data-testid={UI_IDENTIFIERS.Comments.listItemComment(op.signature)}
                      size="small"
                      sx={{ m: 0.5, color: t.muted, flexShrink: 0 }}
                      onClick={() => {
                        comment(op.signature);
                      }}
                    >
                      <ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />
                    </IconButton>
                  </Tooltip>
                ) : null}
              </Box>
              {expanded ? <OpStructTables op={op} t={t} /> : null}
            </Box>
          );
        })}
      </Box>
    </Box>
  );
}

function OpStructTables({ op, t }: { op: ContractOp; t: Tokens }): ReactElement {
  const structs = opStructsFor(op);
  return (
    <Box
      data-testid={UI_IDENTIFIERS.ServiceContract.OP_STRUCTS}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1, px: 1.5, pb: 1.25, pl: 4 }}
    >
      <StructGroup
        color={t.accent2}
        empty="No request."
        label="REQUEST"
        structs={structs.request}
        t={t}
      />
      <StructGroup
        color={t.committedDot}
        empty="No response value."
        label="RESPONSE"
        structs={structs.response}
        t={t}
      />
      <StructGroup
        color={t.dangerFg}
        empty="No error is declared."
        label="ERROR"
        structs={structs.error}
        t={t}
      />
    </Box>
  );
}

function StructGroup({
  label,
  structs,
  color,
  empty,
  t,
}: {
  label: string;
  structs: ResolvedStruct[];
  color: string;
  empty: string;
  t: Tokens;
}): ReactElement {
  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 10,
          fontWeight: 700,
          letterSpacing: '0.08em',
          color,
          mb: 0.25,
        }}
      >
        {label}
      </Typography>
      {structs.length === 0 ? (
        <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted }}>{empty}</Typography>
      ) : (
        structs.map((s, i) => <StructTable key={`${s.name}-${String(i)}`} struct={s} t={t} />)
      )}
    </Box>
  );
}

function StructTable({ struct, t }: { struct: ResolvedStruct; t: Tokens }): ReactElement {
  const cell = {
    fontFamily: t.mono,
    fontSize: 11.5,
    color: t.ink,
    py: 0.25,
    pr: 1.5,
    verticalAlign: 'top',
    wordBreak: 'break-word',
  } as const;
  return (
    <Box sx={{ mb: 0.5, minWidth: 0 }}>
      <Typography sx={{ fontFamily: t.mono, fontSize: 12, fontWeight: 700, color: t.ink }}>
        {struct.name}
      </Typography>
      {struct.fields.length === 0 ? (
        <Typography
          sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, fontStyle: 'italic' }}
        >
          {struct._fallback === true
            ? '(named by the signature; fields not detailed in this contract)'
            : '(no fields)'}
        </Typography>
      ) : (
        <Box component="table" sx={{ borderCollapse: 'collapse', width: '100%' }}>
          <Box component="tbody">
            {struct.fields.map((f) => (
              <Box component="tr" key={f.name}>
                <Box component="td" sx={{ ...cell, fontWeight: 700, whiteSpace: 'nowrap' }}>
                  {f.name}
                </Box>
                <Box component="td" sx={{ ...cell, color: t.muted }}>
                  {f.type}
                </Box>
                <Box component="td" sx={{ ...cell, fontFamily: t.body, color: t.muted }}>
                  {f.note ?? ''}
                </Box>
              </Box>
            ))}
          </Box>
        </Box>
      )}
    </Box>
  );
}
