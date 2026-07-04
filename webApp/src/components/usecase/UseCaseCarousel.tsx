/**
 * Core Use Cases as a carousel — the activity diagram is the hero. Bound to
 * adapters.toCoreUseCasesView (UseCaseView[]). A compact meta sidebar (name,
 * classification, swim-lanes) flanks a large React-Flow activity diagram. A
 * labeled Select + prev/next page through the use cases (dynamic, project-state-
 * driven count — can exceed a dozen — so a dropdown replaces the old tab strip).
 * Recolored from tokens.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Chip from '@mui/material/Chip';
import FormControl from '@mui/material/FormControl';
import InputLabel from '@mui/material/InputLabel';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import { toCoreUseCasesView } from '../../api/adapters';
import type { ArtifactModelEnvelope } from '../../api/types';
import { ActivityFlow } from './ActivityFlow';
import { laneColors } from './laneColors';
// Aliased away from a `use*` name so the react-hooks lint heuristic doesn't
// mistake this plain anchor builder for a React hook.
import { useComments, useCaseAnchor as buildUseCaseAnchor } from '../comments/CommentContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { useTokens } from '../../theme/ThemeContext';

export function UseCaseCarousel({ envelope }: { envelope: ArtifactModelEnvelope | undefined }): ReactNode {
  const t = useTokens();
  const { setAnchor } = useComments();
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
      {/* use-case picker */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1.5 }}>
        <FormControl size="small" sx={{ flexGrow: 1, minWidth: 0 }}>
          <InputLabel id="use-case-picker-label" sx={{ fontFamily: t.mono }}>
            Use case
          </InputLabel>
          <Select
            label="Use case"
            labelId="use-case-picker-label"
            sx={{ fontFamily: t.mono, fontSize: 13 }}
            value={active}
            onChange={(e) => {
              setI(e.target.value);
            }}
          >
            {useCases.map((u, idx) => (
              <MenuItem key={u.id} sx={{ fontFamily: t.mono, fontSize: 13 }} value={idx}>
                {u.name}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, flexShrink: 0 }}>
          {active + 1} / {useCases.length}
        </Typography>
        <IconButton aria-label="Previous use case" size="small" sx={{ border: `1.5px solid ${t.line}`, borderRadius: 1, color: t.ink }} onClick={() => { go(-1); }}>
          <ChevronLeftIcon fontSize="small" />
        </IconButton>
        <IconButton aria-label="Next use case" size="small" sx={{ border: `1.5px solid ${t.line}`, borderRadius: 1, color: t.ink }} onClick={() => { go(1); }}>
          <ChevronRightIcon fontSize="small" />
        </IconButton>
        <IconButton
          aria-label={`Comment on use case ${uc.name}`}
          data-testid={UI_IDENTIFIERS.Comments.USECASE_COMMENT}
          size="small"
          sx={{ border: `1.5px solid ${t.line}`, borderRadius: 1, color: t.accentText, bgcolor: t.accent, flexShrink: 0, '&:hover': { bgcolor: t.accent2 } }}
          onClick={() => {
            setAnchor({
              kind: 'node',
              label: uc.name,
              source: `${uc.name} · use case`,
              jsonPath: buildUseCaseAnchor(active),
            });
          }}
        >
          <ChatBubbleOutlineIcon fontSize="small" />
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
