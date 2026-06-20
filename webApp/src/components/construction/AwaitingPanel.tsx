/**
 * The honest pre-build / awaiting state. The construction pump that populates live
 * sessions is gated on a live Argo cluster (R-CPR) that is not provisioned yet, so
 * the session endpoint commonly returns a quiet/awaiting view (or 404). This panel
 * renders that informatively — it is the EXPECTED state today, not an error.
 */
import type { ReactNode } from 'react';
import Paper from '@mui/material/Paper';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import HourglassEmptyIcon from '@mui/icons-material/HourglassEmpty';
import { useTokens } from '../../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

export function AwaitingPanel({ title, detail }: { title: string; detail: string }): ReactNode {
  const t = useTokens();
  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Construction.AWAITING}
      sx={{ p: 5, textAlign: 'center', borderStyle: 'dashed', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 1.25 }}
    >
      <HourglassEmptyIcon sx={{ fontSize: 30, color: t.muted }} />
      <Typography sx={{ fontFamily: t.mono, color: t.ink, fontSize: 14, fontWeight: 700 }}>{title}</Typography>
      <Box sx={{ maxWidth: 560 }}>
        <Typography sx={{ color: t.muted, fontSize: 12.5, lineHeight: 1.6 }} variant="body2">
          {detail}
        </Typography>
      </Box>
    </Paper>
  );
}
