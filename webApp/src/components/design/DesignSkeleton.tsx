/**
 * Themed loading placeholders for the full-screen design experiences (Phase-1
 * System Design + Phase-2 Project Design). While the project head-state query is
 * in flight we do NOT yet know the committed/locked status of any step, the active
 * artifact, or its stage — so instead of a bare centered spinner (which forced the
 * surrounding chrome to guess and then contradict itself: a "NOT DRAFTED" chip, a
 * step-1 stepper) we sketch the upcoming layout with neutral skeletons.
 *
 * Three pieces, all theme-token driven so they read as part of the neobrutalist
 * design system (bordered cards, mono labels) rather than stock MUI:
 *   • SkeletonSpine        — neutral placeholder pips for the progress rail; no
 *                            check / lock / number / active-highlight, so it makes
 *                            no claim about which step is done or current.
 *   • SkeletonContentCard  — a bordered card sketching the artifact body; reused
 *                            on its own for the (post head-state) session-loading
 *                            window where the header is already truthful.
 *   • DesignExperienceSkeleton — the whole loading layout inside ExperienceChrome:
 *                            skeleton spine + skeleton artifact header (title, a
 *                            neutral chip-shaped placeholder instead of a definitive
 *                            StageChip, sub-line) + a SkeletonContentCard.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Skeleton from '@mui/material/Skeleton';

import { ExperienceChrome } from './ExperienceChrome';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

/** Neutral placeholder rail: N muted pips joined by short rails, no status. */
export function SkeletonSpine({ steps }: { steps: number }): ReactNode {
  const t = useTokens();
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', flexWrap: 'nowrap' }}>
      {Array.from({ length: steps }, (_, i) => (
        <Box key={i} sx={{ display: 'flex', alignItems: 'center', flexShrink: 0 }}>
          {i > 0 && <Box sx={{ width: 18, height: 2, bgcolor: t.line }} />}
          <Skeleton
            animation="wave"
            height={22}
            sx={{ bgcolor: t.line, opacity: 0.5, m: 0.4 }}
            variant="circular"
            width={22}
          />
        </Box>
      ))}
    </Box>
  );
}

/** A bordered card sketching the artifact body: heading bar + prose lines + block. */
export function SkeletonContentCard({ t }: { t: Tokens }): ReactNode {
  return (
    <Paper sx={{ p: { xs: 2.5, md: 4 } }}>
      <Skeleton animation="wave" height={30} sx={{ bgcolor: t.line, opacity: 0.5 }} variant="rectangular" width="45%" />
      <Box sx={{ mt: 3, display: 'flex', flexDirection: 'column', gap: 1.25 }}>
        {['92%', '86%', '96%', '70%'].map((w, i) => (
          <Skeleton animation="wave" height={14} key={i} sx={{ bgcolor: t.line, opacity: 0.35 }} variant="rectangular" width={w} />
        ))}
      </Box>
      <Skeleton animation="wave" height={160} sx={{ bgcolor: t.line, opacity: 0.28, mt: 3 }} variant="rectangular" width="100%" />
      <Box sx={{ mt: 3, display: 'flex', flexDirection: 'column', gap: 1.25 }}>
        {['88%', '94%', '60%'].map((w, i) => (
          <Skeleton animation="wave" height={14} key={i} sx={{ bgcolor: t.line, opacity: 0.35 }} variant="rectangular" width={w} />
        ))}
      </Box>
    </Paper>
  );
}

/**
 * The complete loading layout for a design experience, rendered inside the shared
 * ExperienceChrome so the phase header / accent strip / close affordance stay put.
 */
export function DesignExperienceSkeleton({
  phaseNum,
  phaseTitle,
  projectName,
  steps,
  onClose,
}: {
  phaseNum: number;
  phaseTitle: string;
  projectName?: string | undefined;
  steps: number;
  onClose: () => void;
}): ReactNode {
  const t = useTokens();
  return (
    <ExperienceChrome
      phaseNum={phaseNum}
      phaseTitle={phaseTitle}
      projectName={projectName}
      spine={<SkeletonSpine steps={steps} />}
      onClose={onClose}
    >
      <Box
        data-testid={UI_IDENTIFIERS.DesignExperience.LOADING_SKELETON}
        sx={{ flexGrow: 1, minWidth: 0, overflowY: 'auto', px: { xs: 2, md: 4 }, py: 3 }}
      >
        {/* artifact header placeholder — title + a neutral chip-shaped block (NOT a
            definitive StageChip) + the mono sub-line */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 0.5 }}>
          <Skeleton animation="wave" height={40} sx={{ bgcolor: t.line, opacity: 0.5 }} variant="rectangular" width={300} />
          <Skeleton animation="wave" height={26} sx={{ bgcolor: t.line, opacity: 0.4, borderRadius: `${String(t.radius)}px` }} variant="rectangular" width={128} />
        </Box>
        <Skeleton animation="wave" height={14} sx={{ bgcolor: t.line, opacity: 0.35, mb: 2 }} variant="rectangular" width={200} />

        <SkeletonContentCard t={t} />
      </Box>
    </ExperienceChrome>
  );
}
