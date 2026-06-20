/**
 * Core Use Cases as a carousel — the activity diagram is the hero. Bound to
 * adapters.toCoreUseCasesView (UseCaseView[]). A compact meta sidebar (name,
 * classification, swim-lanes) flanks a large React-Flow activity diagram. Tabs +
 * prev/next page through the use cases. Recolored from tokens.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Chip from '@mui/material/Chip';
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import { toCoreUseCasesView } from '../../api/adapters';
import type { ArtifactModelEnvelope } from '../../api/types';
import { ActivityFlow } from './ActivityFlow';
import { laneColors } from './laneColors';
import { useTokens } from '../../theme/ThemeContext';

export function UseCaseCarousel({ envelope }: { envelope: ArtifactModelEnvelope | undefined }): ReactNode {
  const t = useTokens();
  const [i, setI] = useState(0);
  const useCases = toCoreUseCasesView(envelope).useCases;

  if (useCases.length === 0) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No core use cases drafted yet.
      </Box>
    );
  }

  const active = Math.min(i, useCases.length - 1);
  const uc = useCases[active];
  if (uc === undefined) return null;
  const colors = laneColors(t, uc.lanes);
  const go = (d: number): void => {
    setI((p) => (p + d + useCases.length) % useCases.length);
  };
  const isCore = uc.classification === 'core';

  return (
    <Box>
      {/* slide tabs */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1.5, flexWrap: 'wrap' }}>
        {useCases.map((u, idx) => (
          <Box
            key={u.id}
            sx={{
              cursor: 'pointer',
              px: 1.25,
              py: 0.5,
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 12,
              border: `1.5px solid ${t.line}`,
              borderRadius: t.radius / 8 + 0.5,
              bgcolor: idx === active ? t.accent : 'transparent',
              color: idx === active ? t.accentText : t.muted,
              boxShadow: idx === active && t.hardShadow ? `2px 2px 0 ${t.shadowColor}` : 'none',
              maxWidth: 220,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
            onClick={() => {
              setI(idx);
            }}
          >
            {u.name}
          </Box>
        ))}
        <Box sx={{ flexGrow: 1 }} />
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
          {active + 1} / {useCases.length}
        </Typography>
        <IconButton size="small" sx={{ border: `1.5px solid ${t.line}`, borderRadius: 1, color: t.ink }} onClick={() => { go(-1); }}>
          <ChevronLeftIcon fontSize="small" />
        </IconButton>
        <IconButton size="small" sx={{ border: `1.5px solid ${t.line}`, borderRadius: 1, color: t.ink }} onClick={() => { go(1); }}>
          <ChevronRightIcon fontSize="small" />
        </IconButton>
      </Box>

      <Paper sx={{ display: 'flex', alignItems: 'stretch', overflow: 'hidden', flexDirection: { xs: 'column', md: 'row' } }}>
        {/* meta sidebar */}
        <Box sx={{ width: { xs: '100%', md: 300 }, flexShrink: 0, p: 3, borderRight: { md: `1.5px solid ${t.line}` }, bgcolor: t.paperAlt }}>
          <Typography sx={{ color: t.muted }} variant="overline">
            {isCore ? 'Core Use Case' : 'Variation'}
          </Typography>
          <Typography sx={{ color: t.ink, lineHeight: 1.1, mt: 0.5, mb: 1.5 }} variant="h4">
            {uc.name}
          </Typography>
          <Chip
            label={isCore ? 'CORE' : 'NON-CORE'}
            size="small"
            sx={{ bgcolor: isCore ? t.committedBg : t.awaitingBg, color: isCore ? t.committedFg : t.awaitingFg, mb: 2 }}
          />
          {!isCore && uc.rejectionReason.length > 0 && (
            <Typography sx={{ color: t.muted, fontSize: 13, lineHeight: 1.6, mb: 3 }}>
              {uc.rejectionReason}
            </Typography>
          )}
          <Typography sx={{ color: t.muted, mb: 1 }} variant="subtitle2">
            SWIMLANES
          </Typography>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75 }}>
            {uc.lanes.map((l) => (
              <Box key={l} sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
                <Box sx={{ width: 11, height: 11, bgcolor: colors[l], border: `1.5px solid ${t.line}`, flexShrink: 0 }} />
                <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.ink }}>{l}</Typography>
              </Box>
            ))}
          </Box>
        </Box>

        {/* diagram hero — React Flow */}
        <Box sx={{ flexGrow: 1, minWidth: 0, p: 1.5 }}>
          <ActivityFlow height={580} uc={uc} useCaseIndex={active} />
        </Box>
      </Paper>
    </Box>
  );
}
