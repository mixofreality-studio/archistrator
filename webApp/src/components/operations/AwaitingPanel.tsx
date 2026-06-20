/**
 * The honest awaiting / empty state the Operations console degrades to when the
 * read view is quiet or absent (no operated app deployed yet, or a 404). Mirrors
 * the ConstructionConsole's AwaitingPanel idiom — a dashed card, no error tone.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import CloudOffOutlinedIcon from '@mui/icons-material/CloudOffOutlined';
import { useTokens } from '../../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

export function AwaitingPanel({
  title,
  detail,
}: {
  title: string;
  detail: string;
}): ReactNode {
  const t = useTokens();
  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Operations.AWAITING}
      sx={{ p: 4, textAlign: 'center', borderStyle: 'dashed', maxWidth: 720 }}
    >
      <CloudOffOutlinedIcon sx={{ fontSize: 32, color: t.muted, opacity: 0.6 }} />
      <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 18, color: t.ink, mt: 1 }}>
        {title}
      </Typography>
      <Box sx={{ maxWidth: 520, mx: 'auto' }}>
        <Typography sx={{ fontFamily: t.body, fontSize: 13, color: t.muted, mt: 0.5, lineHeight: 1.5 }}>
          {detail}
        </Typography>
      </Box>
    </Paper>
  );
}
