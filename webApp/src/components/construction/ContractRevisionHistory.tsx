/**
 * Timeline of contract revisions — dot + connecting line + metadata row.
 * Latest (first in array) uses committedDot token; earlier entries use accent2.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import HistoryIcon from '@mui/icons-material/History';
import type { ContractRevision } from '../../api/types';
import type { Tokens } from '../../theme/themes';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

function RevisionRow({ r, t, isLatest, isLast }: { r: ContractRevision; t: Tokens; isLatest: boolean; isLast: boolean }): ReactNode {
  const dotColor = isLatest ? t.committedDot : t.accent2;
  return (
    <Box
      data-testid={UI_IDENTIFIERS.ServiceContract.revisionRow(r.rev)}
      sx={{ display: 'flex', gap: 1.5 }}
    >
      {/* timeline spine */}
      <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', flexShrink: 0, width: 16 }}>
        <Box
          sx={{
            width: 11,
            height: 11,
            borderRadius: '50%',
            bgcolor: dotColor,
            border: `2px solid ${t.paper}`,
            boxShadow: `0 0 0 1.5px ${dotColor}`,
            mt: 0.3,
          }}
        />
        {!isLast && <Box sx={{ flexGrow: 1, width: 2, bgcolor: t.line, mt: 0.25 }} />}
      </Box>

      {/* content */}
      <Box sx={{ pb: isLast ? 0 : 1.5, minWidth: 0 }}>
        <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1, flexWrap: 'wrap' }}>
          <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, color: t.ink }}>{r.rev}</Typography>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>
            {r.at}
            {r.by.length > 0 ? ` · ${r.by}` : ''}
          </Typography>
          {r.byActivity !== undefined && r.byActivity.length > 0 ? (
            <Box
              sx={{
                fontFamily: t.mono,
                fontSize: 9,
                color: t.accent,
                border: `1px solid ${t.line}`,
                borderRadius: 0.75,
                px: 0.5,
              }}
            >
              {r.byActivity}
            </Box>
          ) : null}
        </Box>
        {r.summary !== undefined && r.summary.length > 0 ? (
          <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.45, mt: 0.2 }}>
            {r.summary}
          </Typography>
        ) : null}
      </Box>
    </Box>
  );
}

export function ContractRevisionHistory({ revisions, t }: { revisions: ContractRevision[]; t: Tokens }): ReactNode {
  if (revisions.length === 0) return null;

  return (
    <Paper data-testid={UI_IDENTIFIERS.ServiceContract.REVISION_HISTORY} sx={{ p: 0, overflow: 'hidden' }}>
      <Box
        sx={{
          px: 2,
          py: 1.25,
          bgcolor: t.paperAlt,
          borderBottom: `1.5px solid ${t.line}`,
          display: 'flex',
          alignItems: 'center',
          gap: 1,
        }}
      >
        <HistoryIcon sx={{ fontSize: 16, color: t.ink }} />
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, letterSpacing: '0.06em', color: t.ink }}>
          REVISION HISTORY
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>
          · re-cut by multiple activities over time
        </Typography>
      </Box>
      <Box sx={{ p: 2 }}>
        {revisions.map((r, i) => (
          <RevisionRow
            isLast={i === revisions.length - 1}
            isLatest={i === 0}
            key={r.rev}
            r={r}
            t={t}
          />
        ))}
      </Box>
    </Paper>
  );
}
