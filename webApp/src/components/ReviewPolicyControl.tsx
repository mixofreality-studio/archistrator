/**
 * Review-policy preset control (project home page): the "how much do you want
 * to approve?" dial. Three presets — vibes / checkpoints / full — map onto the
 * construction SetReviewPolicy op (server-validated closed vocabulary); the
 * committed value reads back from the project view (reviewPolicy.preset), so
 * the control always shows server truth, never a local echo. PURE component
 * (components-layer rule): the route owns the mutation (useSetReviewPolicy)
 * and threads its state in via props.
 *
 * The deploy/spend/schema risk floor is NOT a preset property: a construction
 * dispatch (and its merge) of any activity whose contract touches deploy,
 * spend, or schema always requires explicit approval, under every preset,
 * vibes included. That is why the note below is permanent — there is no
 * dismiss affordance by design.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Radio from '@mui/material/Radio';
import CircularProgress from '@mui/material/CircularProgress';
import LockOutlinedIcon from '@mui/icons-material/LockOutlined';
import type { ReviewPreset } from '../contracts/types';
import { ErrorAlert } from './shared/ErrorAlert';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

const PRESETS: { value: ReviewPreset; title: string; line: string }[] = [
  {
    value: 'vibes',
    title: 'Vibes',
    line: 'Auto-approve every step — finished work merges on its own.',
  },
  {
    value: 'checkpoints',
    title: 'Checkpoints',
    line: 'Approve the key gates: contract commit, construction dispatch, and merge.',
  },
  {
    value: 'full',
    title: 'Full review',
    line: 'Approve every phase of every activity before it advances or merges.',
  },
];

export function ReviewPolicyControl({
  preset,
  pending,
  error,
  onChoose,
}: {
  /** The committed preset from the project read; undefined = not set yet. */
  preset?: string | undefined;
  /** True while the set-review-policy mutation is in flight. */
  pending: boolean;
  /** The mutation's error, if any (null when clean). */
  error: Error | null;
  /** Invoked with the chosen preset; the route runs the mutation. */
  onChoose: (preset: ReviewPreset) => void;
}): ReactNode {
  const t = useTokens();

  const choose = (value: ReviewPreset): void => {
    if (pending || value === preset) return;
    onChoose(value);
  };

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.HomeBase.REVIEW_POLICY}
      sx={{ p: { xs: 2, md: 2.5 }, mb: 3, display: 'flex', flexDirection: 'column', gap: 1 }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
        <Typography sx={{ color: t.ink }} variant="h6">
          Review policy
        </Typography>
        {pending ? <CircularProgress size={16} /> : null}
      </Box>
      <ErrorAlert error={error} />
      <Box
        aria-label="Review policy preset"
        role="radiogroup"
        sx={{ display: 'flex', gap: 1.5, flexWrap: 'wrap' }}
      >
        {PRESETS.map((p) => {
          const selected = p.value === preset;
          return (
            <Box
              aria-checked={selected}
              data-testid={UI_IDENTIFIERS.HomeBase.reviewPolicyOption(p.value)}
              key={p.value}
              role="radio"
              sx={{
                flex: '1 1 220px',
                minWidth: 0,
                display: 'flex',
                alignItems: 'flex-start',
                gap: 0.5,
                p: 1.25,
                borderRadius: 1,
                border: `1.5px solid ${selected ? t.accent : t.line}`,
                bgcolor: selected ? t.paperAlt : 'transparent',
                cursor: pending ? 'progress' : 'pointer',
                opacity: pending && !selected ? 0.6 : 1,
                '&:hover': { bgcolor: t.paperAlt },
              }}
              tabIndex={0}
              onClick={() => {
                choose(p.value);
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  choose(p.value);
                }
              }}
            >
              <Radio checked={selected} size="small" sx={{ p: 0.5, mt: -0.25 }} tabIndex={-1} />
              <Box sx={{ minWidth: 0 }}>
                <Typography sx={{ color: t.ink, fontWeight: 700, fontSize: 14 }}>
                  {p.title}
                </Typography>
                <Typography sx={{ color: t.muted, fontSize: 12.5, lineHeight: 1.45 }}>
                  {p.line}
                </Typography>
              </Box>
            </Box>
          );
        })}
      </Box>
      {/* Permanent risk-floor note — deliberately NOT dismissible: the floor
          applies under every preset and the control must never suggest otherwise. */}
      <Typography
        data-testid={UI_IDENTIFIERS.HomeBase.REVIEW_POLICY_FLOOR_NOTE}
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 0.75,
          fontFamily: t.mono,
          fontSize: 12,
          color: t.muted,
        }}
      >
        <LockOutlinedIcon sx={{ fontSize: 14 }} />
        Deploy, spend, and schema changes always require your approval — on every preset.
      </Typography>
    </Paper>
  );
}
