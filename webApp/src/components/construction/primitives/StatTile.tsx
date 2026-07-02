import type { ReactNode } from 'react';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import type { Tokens } from '../../../theme/themes';

/** A single headline metric tile — reusable across artifact renderers. */
export function StatTile({
  label,
  value,
  tone = 'neutral',
  t,
}: {
  label: string;
  value: string | number;
  tone?: 'good' | 'bad' | 'neutral';
  t: Tokens;
}): ReactNode {
  const fg = tone === 'good' ? t.committedFg : tone === 'bad' ? t.awaitingFg : t.ink;
  const bg = tone === 'good' ? t.committedBg : tone === 'bad' ? t.awaitingBg : t.paperAlt;
  return (
    <Paper sx={{ p: 1.25, minWidth: 96, bgcolor: bg, border: `1px solid ${t.line}` }}>
      <Typography sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 26, color: fg, lineHeight: 1 }}>
        {value}
      </Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.06em', color: t.muted, mt: 0.35 }}>
        {label.toUpperCase()}
      </Typography>
    </Paper>
  );
}
