import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import type { Tokens } from '../../../theme/themes';

export interface RecordColumn {
  key: string;
  label: string;
}

/** A simple token-driven table for artifact record lists (runs, defects, entries). */
export function RecordTable({
  columns,
  rows,
  t,
}: {
  columns: RecordColumn[];
  rows: Record<string, ReactNode>[];
  t: Tokens;
}): ReactNode {
  return (
    <Box sx={{ border: `1px solid ${t.line}`, borderRadius: 1, overflow: 'hidden' }}>
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: `repeat(${String(columns.length)}, 1fr)`,
          bgcolor: t.paperAlt,
          borderBottom: `1px solid ${t.line}`,
        }}
      >
        {columns.map((c) => (
          <Typography
            key={c.key}
            sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.06em', color: t.muted, p: 0.8 }}
          >
            {c.label.toUpperCase()}
          </Typography>
        ))}
      </Box>
      {rows.map((r, i) => (
        <Box
          key={i}
          sx={{
            display: 'grid',
            gridTemplateColumns: `repeat(${String(columns.length)}, 1fr)`,
            borderBottom: i < rows.length - 1 ? `1px solid ${t.line}` : 'none',
          }}
        >
          {columns.map((c) => (
            <Box key={c.key} sx={{ p: 0.8, fontFamily: t.body, fontSize: 12, color: t.ink }}>
              {r[c.key] ?? ''}
            </Box>
          ))}
        </Box>
      ))}
    </Box>
  );
}
