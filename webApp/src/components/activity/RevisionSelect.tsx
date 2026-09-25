/**
 * The revision select of the Activity Experience body — the same control, in the
 * same place, on a dispatch task and on the review that gates it, because a
 * revision is ONE thing seen from both: the episode that produced draft N, and
 * the round that judged it (lifecycleGraphTypes.ts).
 *
 * A dropdown, per the house convention for counts that are not fixed. It renders
 * as a select only when there is something to choose: a task on its first
 * revision shows a static `REVISION 1` eyebrow in the same spot, and a task that
 * has never run says so. Pure and props-only; the option text is `revisionLine`,
 * which the graph's revision menu (a shortcut to this control) shares.
 */
import type { ReactNode } from 'react';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import Typography from '@mui/material/Typography';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { latestRevision, revisionLine, type LifecycleRevision } from './lifecycleGraphTypes.ts';

export function RevisionSelect({
  revisions,
  value,
  onChange,
}: {
  revisions: readonly LifecycleRevision[];
  /** The revision on screen. */
  value: number;
  onChange: (revision: number) => void;
}): ReactNode {
  const t = useTokens();
  const latest = latestRevision({ revisions });

  if (revisions.length <= 1) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.ActivityLifecycle.REVISION_SELECT}
        sx={{
          fontFamily: t.mono,
          fontSize: 10.5,
          letterSpacing: '0.18em',
          color: t.muted,
          whiteSpace: 'nowrap',
        }}
      >
        {revisions.length === 0 ? 'NO REVISION YET' : `REVISION ${String(latest)}`}
      </Typography>
    );
  }

  const historical = value < latest;
  return (
    <Select
      data-testid={UI_IDENTIFIERS.ActivityLifecycle.REVISION_SELECT}
      inputProps={{ 'aria-label': 'Revision' }}
      renderValue={(n) =>
        `Revision ${String(n)} of ${String(latest)}${n === latest ? ' · latest' : ' · read-only'}`
      }
      size="small"
      sx={{
        fontFamily: t.mono,
        fontWeight: 700,
        fontSize: 12,
        color: historical ? t.awaitingFg : t.ink,
        bgcolor: historical ? t.awaitingBg : t.paper,
        '& .MuiOutlinedInput-notchedOutline': {
          borderColor: historical ? t.awaitingFg : t.line,
        },
      }}
      value={value}
      onChange={(e) => {
        onChange(e.target.value);
      }}
    >
      {[...revisions]
        .sort((a, b) => b.n - a.n)
        .map((r) => (
          <MenuItem
            data-testid={UI_IDENTIFIERS.ActivityLifecycle.revisionOption(r.n)}
            key={r.n}
            sx={{ fontFamily: t.mono, fontSize: 12 }}
            value={r.n}
          >
            {revisionLine(r, r.n === latest)}
          </MenuItem>
        ))}
    </Select>
  );
}
