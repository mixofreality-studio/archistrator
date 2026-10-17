/**
 * The Phase-2 views' section heading: a REAL heading element (h3 by default) in
 * the mono caps style the views already used for their section titles. Before
 * this, every title on the Project Design body was a styled <p>, so a screen
 * reader's heading list showed the page title and nothing under it — the
 * options, the curves, the decision, the activity list and the network were not
 * navigable as sections at all.
 *
 * An optional `hint` sits under the title in body type: the one-sentence "what
 * is this and how do I read it" a first-time reader needs, said once per section
 * rather than as a tooltip on a non-focusable badge.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import type { Tokens } from '../../utilities/theme/themes';

export function SectionHeading({
  t,
  children,
  level = 3,
  adornment,
  hint,
  color,
}: {
  t: Tokens;
  children: ReactNode;
  level?: 3 | 4;
  /** Rendered beside the title (a COMPUTED / AUTHORED badge). */
  adornment?: ReactNode;
  /** One plain-language sentence under the title. */
  hint?: ReactNode;
  color?: string;
}): ReactNode {
  return (
    <Box sx={{ minWidth: 0 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
        <Typography
          component={level === 3 ? 'h3' : 'h4'}
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: level === 3 ? 13 : 12,
            letterSpacing: '0.08em',
            textTransform: 'uppercase',
            color: color ?? t.ink,
            m: 0,
          }}
        >
          {children}
        </Typography>
        {adornment}
      </Box>
      {hint !== undefined ? (
        <Typography
          sx={{
            fontFamily: t.body,
            fontSize: 13,
            lineHeight: 1.5,
            color: color ?? t.muted,
            mt: 0.5,
          }}
        >
          {hint}
        </Typography>
      ) : null}
    </Box>
  );
}
