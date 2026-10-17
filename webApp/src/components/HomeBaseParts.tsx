/**
 * Presentational pieces for the home base: the economics strip. Pure
 * presentation — the screen reads the committed SDP review and hands it figures.
 * (`TocRow` lived here too; the home base's own ArtifactNav replaced it and it had
 * no caller left, so it went.)
 *
 * `PhaseCard` lived here too, one per Method phase, with a committed/total
 * progress bar and a `resume →` / `open console →` button into that phase's own
 * rail. Stage 5 §7.4 replaced all three with the home base's ONE plan card (Task
 * 11) and deleted the rails they opened (Task 13), so it went with
 * `toPhaseCards`, the adapter that fed it.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import type { Tokens } from '../utilities/theme/themes';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

/** What the economics strip shows, read off the committed SDP review (if any). */
export interface EconomicsFigures {
  /** The option the figures come from, in words ("Decompressed-normal", …). */
  optionLabel: string;
  /** Whether that option was CHOSEN at SDP review, or is only the recommendation. */
  chosen: boolean;
  buildCost: string;
  monthlyRunCost: string;
  perCycleNet: string;
}

/**
 * The ECONOMICS strip: what building and running this system costs, from the
 * committed SDP review — the option management chose, else the architect's
 * recommendation (said so). Before that review exists the strip says WHEN the
 * figures appear instead of showing dashes with no explanation.
 *
 * It used to carry a third metric, "revenue share", read off the committed
 * planningAssumptions slot. Revenue share left the vocabulary with stage 4b2
 * (founder ruling) — the platform bills a usage-based hosting fee for operating a
 * delivered system and nothing else — so the metric is gone rather than rendering 0%.
 */
export function EconomicsStrip({ figures }: { figures: EconomicsFigures | undefined }): ReactNode {
  const t = useTokens();
  return (
    <Paper
      aria-labelledby="economics-heading"
      component="section"
      data-testid={UI_IDENTIFIERS.HomeBase.ECONOMICS_STRIP}
      sx={{
        p: 2,
        mb: 4,
        display: 'flex',
        alignItems: 'center',
        gap: 3,
        flexWrap: 'wrap',
        bgcolor: t.paperAlt,
      }}
    >
      <Box>
        <Typography
          component="h2"
          id="economics-heading"
          sx={{ color: t.ink, fontWeight: 700 }}
          variant="subtitle1"
        >
          Economics
        </Typography>
        <Typography sx={{ color: t.muted, fontSize: 12 }}>
          {figures === undefined
            ? 'Appears once the delivery plan (SDP review) is committed'
            : `${figures.optionLabel} option${figures.chosen ? ' (chosen)' : ' (recommended)'}`}
        </Typography>
      </Box>
      {figures === undefined ? null : (
        <>
          <Metric
            hint="one-time, to design and build"
            label="Build cost"
            t={t}
            value={figures.buildCost}
          />
          <Metric
            hint="projected cost to run it each month"
            label="Monthly running cost"
            t={t}
            value={figures.monthlyRunCost}
          />
          <Metric
            hint="expected net per billing cycle once operating"
            label="Net per cycle"
            t={t}
            value={figures.perCycleNet}
          />
        </>
      )}
    </Paper>
  );
}

function Metric({
  t,
  label,
  value,
  hint,
}: {
  t: Tokens;
  label: string;
  value: string;
  hint: string;
}): ReactNode {
  return (
    <Box>
      <Typography sx={{ fontSize: 12, fontWeight: 600, color: t.muted }}>{label}</Typography>
      <Typography
        sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 22, lineHeight: 1.1, color: t.ink }}
      >
        {value}
      </Typography>
      <Typography sx={{ fontSize: 11.5, color: t.muted }}>{hint}</Typography>
    </Box>
  );
}
