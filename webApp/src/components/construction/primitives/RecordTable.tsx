import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
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
    <Box
      component="table"
      sx={{
        width: '100%',
        borderCollapse: 'collapse',
        tableLayout: 'fixed',
        border: `1px solid ${t.line}`,
        borderRadius: 1,
        overflow: 'hidden',
      }}
    >
      <Box component="thead" sx={{ bgcolor: t.paperAlt }}>
        <Box component="tr">
          {columns.map((c) => (
            <Box
              component="th"
              key={c.key}
              scope="col"
              sx={{
                textAlign: 'left',
                fontFamily: t.mono,
                fontSize: 9.5,
                fontWeight: 400,
                letterSpacing: '0.06em',
                color: t.muted,
                p: 0.8,
                borderBottom: `1px solid ${t.line}`,
              }}
            >
              {c.label.toUpperCase()}
            </Box>
          ))}
        </Box>
      </Box>
      <Box component="tbody">
        {rows.map((r, i) => (
          <Box component="tr" key={i}>
            {columns.map((c) => (
              <Box
                component="td"
                key={c.key}
                sx={{
                  p: 0.8,
                  fontFamily: t.body,
                  fontSize: 12,
                  color: t.ink,
                  borderBottom: i < rows.length - 1 ? `1px solid ${t.line}` : 'none',
                }}
              >
                {r[c.key] ?? ''}
              </Box>
            ))}
          </Box>
        ))}
      </Box>
    </Box>
  );
}
