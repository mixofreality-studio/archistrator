/**
 * The read-only banner over a NON-LATEST revision (spec §7.2, R1), and the one
 * way out of it.
 *
 * It sits above BOTH bodies, because "you are reading history" is a fact about
 * the selection, not about the task kind: an older episode and the round that
 * judged an older draft are equally past. Everything that would CHANGE something
 * is gone beneath it — the submit bar is not rendered, the comment provider is
 * disabled, the margin's resolve/reopen are omitted — so this banner is not a
 * warning about disabled controls; there are none to warn about. It says where
 * the reader is, and offers the single navigation back.
 *
 * `role="status"` because it appears as the RESULT of the reader's own
 * navigation (picking a revision), which a polite live region announces without
 * interrupting. Not `alert`: nothing here is wrong.
 *
 * Pure and props-only.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import HistoryIcon from '@mui/icons-material/History';

import { BACK_TO_LATEST, historyBanner } from './activityCopy.ts';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

export function HistoryBanner({
  revision,
  latest,
  onBackToLatest,
}: {
  /** The revision on screen. */
  revision: number;
  /** The task's newest revision — the one the reader is NOT looking at. */
  latest: number;
  onBackToLatest: () => void;
}): ReactNode {
  const t = useTokens();
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Activity.HISTORY_BANNER}
      role="status"
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        flexWrap: 'wrap',
        px: 2,
        py: 1.25,
        bgcolor: t.awaitingBg,
        border: `1.5px solid ${t.awaitingFg}`,
        borderRadius: t.radius / 8 + 0.5,
      }}
    >
      <HistoryIcon sx={{ fontSize: 18, color: t.awaitingFg }} />
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 12,
          letterSpacing: '0.06em',
          color: t.awaitingFg,
        }}
      >
        {historyBanner(revision, latest)}
      </Typography>
      <Box sx={{ flexGrow: 1 }} />
      <Button
        data-testid={UI_IDENTIFIERS.Activity.BACK_TO_LATEST}
        size="small"
        sx={{ fontFamily: t.mono, color: t.ink, borderColor: t.line, textTransform: 'none' }}
        variant="outlined"
        onClick={onBackToLatest}
      >
        {BACK_TO_LATEST}
      </Button>
    </Box>
  );
}
