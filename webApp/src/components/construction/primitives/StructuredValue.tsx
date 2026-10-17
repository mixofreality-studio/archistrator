/**
 * A bound value drawn as labelled fields — never as a JSON dump.
 *
 * valueTree.readValue turns the binding's JSON/text string into a tree; this
 * draws it: a record is a two-column list of field → value (nested records
 * indented under a rule), a list is numbered items, a scalar or text is one line
 * in the body face, and an absent value says "none". Body font throughout: a
 * value is something the reviewer reads, not code they would type.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import type { Tokens } from '../../../utilities/theme/themes';
import { isInline, readValue, type ValueTree } from './valueTree.ts';

/** How deep a value nests before the rest is summarised (a reviewer aid, not a dump). */
const MAX_DEPTH = 4;

function Inline({ v, t }: { v: ValueTree; t: Tokens }): ReactNode {
  switch (v.kind) {
    case 'empty':
      return (
        <Box component="span" sx={{ color: t.muted, fontStyle: 'italic' }}>
          none
        </Box>
      );
    case 'scalar':
      return (
        <Box component="span" sx={{ fontWeight: 600 }}>
          {v.text}
        </Box>
      );
    case 'text':
      return <Box component="span">{v.text}</Box>;
    case 'list':
      return <Box component="span">{`${String(v.items.length)} items`}</Box>;
    case 'record':
      return <Box component="span">{`${String(v.fields.length)} fields`}</Box>;
  }
}

function Tree({ v, t, depth }: { v: ValueTree; t: Tokens; depth: number }): ReactNode {
  if (isInline(v) || depth >= MAX_DEPTH) return <Inline t={t} v={v} />;
  if (v.kind === 'list') {
    return (
      <Box
        component="ol"
        sx={{ m: 0, pl: 2.25, display: 'flex', flexDirection: 'column', gap: 0.25 }}
      >
        {v.items.map((item, i) => (
          // A bound list has no identity but its position.
          <li key={String(i)}>
            <Tree depth={depth + 1} t={t} v={item} />
          </li>
        ))}
      </Box>
    );
  }
  if (v.kind !== 'record') return null;
  return (
    <Box
      component="dl"
      sx={{
        m: 0,
        display: 'grid',
        gridTemplateColumns: 'max-content minmax(0,1fr)',
        columnGap: 1.25,
        rowGap: 0.25,
        ...(depth > 0 ? { pl: 1, borderLeft: `2px solid ${t.line}` } : {}),
      }}
    >
      {v.fields.map((f) => (
        <Box key={f.key} sx={{ display: 'contents' }}>
          <Box component="dt" sx={{ color: t.muted, fontWeight: 600 }}>
            {f.key}
          </Box>
          <Box component="dd" sx={{ m: 0, minWidth: 0, overflowWrap: 'anywhere' }}>
            <Tree depth={depth + 1} t={t} v={f.value} />
          </Box>
        </Box>
      ))}
    </Box>
  );
}

export function StructuredValue({
  raw,
  t,
  tone,
}: {
  /** The binding's value string (JSON or plain text). */
  raw: string;
  t: Tokens;
  /** Colour override for the whole value (an expected error reads in danger). */
  tone?: string | undefined;
}): ReactNode {
  return (
    <Typography
      component="div"
      sx={{
        fontFamily: t.body,
        fontSize: 12.5,
        lineHeight: 1.45,
        color: tone ?? t.ink,
        minWidth: 0,
        overflowWrap: 'anywhere',
      }}
    >
      <Tree depth={0} t={t} v={readValue(raw)} />
    </Typography>
  );
}
