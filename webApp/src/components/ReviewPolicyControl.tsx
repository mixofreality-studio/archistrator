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
import RadioGroup from '@mui/material/RadioGroup';
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
        <Typography component="h2" id="review-policy-heading" sx={{ color: t.ink }} variant="h6">
          Review policy
        </Typography>
        {pending ? <CircularProgress size={16} /> : null}
      </Box>
      <ErrorAlert error={error} />
      <Typography id="review-policy-help" sx={{ color: t.muted, fontSize: 13, lineHeight: 1.5 }}>
        {preset === undefined || preset === ''
          ? 'No policy chosen yet — until you pick one, only deploy, spend and schema changes stop for your approval; everything else proceeds on its own.'
          : 'How much of the agents’ work stops for your approval before it moves on.'}
      </Typography>
      {/* Native radios in one named group: the browser gives arrow-key movement,
          one tab stop and the checked state; each whole card is the radio's
          <label>, so a click anywhere on it chooses it. */}
      <RadioGroup
        aria-describedby="review-policy-help"
        aria-labelledby="review-policy-heading"
        name="review-policy-preset"
        sx={{ display: 'flex', flexDirection: 'row', gap: 1.5, flexWrap: 'wrap' }}
        value={preset ?? ''}
        onChange={(_e, value) => {
          const next = PRESETS.find((p) => p.value === value);
          if (next !== undefined) choose(next.value);
        }}
      >
        {PRESETS.map((p) => {
          const selected = p.value === preset;
          return (
            <Box
              component="label"
              data-testid={UI_IDENTIFIERS.HomeBase.reviewPolicyOption(p.value)}
              key={p.value}
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
                '&:focus-within': { outline: `2px solid ${t.accent}`, outlineOffset: 2 },
              }}
            >
              <Radio
                disabled={pending}
                size="small"
                slotProps={{ input: { 'aria-describedby': `review-policy-${p.value}-line` } }}
                sx={{ p: 0.5, mt: -0.25 }}
                value={p.value}
              />
              <Box sx={{ minWidth: 0 }}>
                <Typography sx={{ color: t.ink, fontWeight: 700, fontSize: 14 }}>
                  {p.title}
                </Typography>
                <Typography
                  id={`review-policy-${p.value}-line`}
                  sx={{ color: t.muted, fontSize: 12.5, lineHeight: 1.45 }}
                >
                  {p.line}
                </Typography>
              </Box>
            </Box>
          );
        })}
      </RadioGroup>
      {/* Permanent risk-floor note — deliberately NOT dismissible: the floor
          applies under every preset and the control must never suggest otherwise. */}
      <Typography
        data-testid={UI_IDENTIFIERS.HomeBase.REVIEW_POLICY_FLOOR_NOTE}
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 0.75,
          fontSize: 12.5,
          color: t.muted,
        }}
      >
        <LockOutlinedIcon aria-hidden sx={{ fontSize: 14 }} />
        Deploy, spend, and schema changes always require your approval — on every preset.
      </Typography>
    </Paper>
  );
}
