/**
 * The project's STATE HISTORY (use case view-the-project-state-log; spec §12 B7):
 * every recorded change the head state carries, newest first (contracts/
 * stateHistory.ts), and — the use case's [drill into a change] branch — the one the
 * reader picks, with what it recorded and where.
 *
 * The typed diff of a change against its parent commit is the use case's last step,
 * and the read model does not carry state AS OF a commit, so a drilled entry says so
 * plainly (`StateHistory.DIFF_UNAVAILABLE`) instead of drawing a diff it cannot know.
 *
 * Pure: the entries arrive as props; the selection is this view's own.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import type { StateHistoryEntry } from '../contracts/stateHistory';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

const KIND_LABEL: Record<StateHistoryEntry['kind'], string> = {
  slotCommitted: 'COMMIT',
  activityStarted: 'START',
  activityCompleted: 'DONE',
  phaseExited: 'PHASE',
};

const DIFF_UNAVAILABLE =
  'The typed diff of this change against the state before it is not on the read model: the head state records when and by whom a change landed, not the state as of each commit.';

export function StateHistoryView({
  entries,
  loading,
}: {
  entries: readonly StateHistoryEntry[];
  loading: boolean;
}): ReactNode {
  const t = useTokens();
  const [selected, setSelected] = useState<string | undefined>(undefined);
  const chosen = entries.find((e) => e.key === selected);

  if (loading) {
    return (
      <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
        Reading the project state…
      </Typography>
    );
  }
  if (entries.length === 0) {
    return (
      <Paper data-testid={UI_IDENTIFIERS.StateHistory.EMPTY} sx={{ p: 3, borderStyle: 'dashed' }}>
        <Typography sx={{ fontSize: 13.5, color: t.ink, lineHeight: 1.5 }}>
          Nothing has been recorded yet: no artifact is committed and no activity has started.
        </Typography>
      </Paper>
    );
  }

  return (
    <Box
      sx={{
        display: 'grid',
        gridTemplateColumns: { xs: '1fr', md: 'minmax(0, 1.2fr) minmax(0, 1fr)' },
        gap: 2,
        alignItems: 'start',
      }}
    >
      <Paper sx={{ p: 0, overflow: 'hidden' }}>
        <Box
          aria-label="Recorded changes, newest first"
          component="ol"
          data-testid={UI_IDENTIFIERS.StateHistory.LIST}
          sx={{ listStyle: 'none', m: 0, p: 0 }}
        >
          {entries.map((e) => {
            const isChosen = e.key === selected;
            return (
              <Box component="li" key={e.key} sx={{ borderBottom: `1px solid ${t.line}` }}>
                <Box
                  aria-pressed={isChosen}
                  component="button"
                  data-testid={UI_IDENTIFIERS.StateHistory.entry(e.key)}
                  sx={{
                    all: 'unset',
                    boxSizing: 'border-box',
                    width: '100%',
                    display: 'flex',
                    alignItems: 'baseline',
                    gap: 1.5,
                    px: 2,
                    py: 1.25,
                    cursor: 'pointer',
                    bgcolor: isChosen ? t.awaitingBg : 'transparent',
                    '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: -2 },
                  }}
                  type="button"
                  onClick={() => {
                    setSelected(isChosen ? undefined : e.key);
                  }}
                >
                  <Typography
                    sx={{
                      fontFamily: t.mono,
                      fontWeight: 700,
                      fontSize: 10,
                      letterSpacing: '0.12em',
                      color: t.muted,
                      minWidth: 52,
                    }}
                  >
                    {KIND_LABEL[e.kind]}
                  </Typography>
                  <Typography sx={{ fontSize: 13.5, color: t.ink, flexGrow: 1, minWidth: 0 }}>
                    {e.title}
                  </Typography>
                  <Typography
                    component="time"
                    dateTime={e.at}
                    sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted, flexShrink: 0 }}
                  >
                    {e.at}
                  </Typography>
                </Box>
              </Box>
            );
          })}
        </Box>
      </Paper>

      {chosen !== undefined ? (
        <Paper data-testid={UI_IDENTIFIERS.StateHistory.DETAIL} sx={{ p: 2.5 }}>
          <Typography sx={{ color: t.ink, fontFamily: t.display, fontWeight: 700, fontSize: 16 }}>
            {chosen.title}
          </Typography>
          <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.muted, mt: 0.5 }}>
            {chosen.address} · {chosen.at}
          </Typography>
          {chosen.facts.length > 0 ? (
            <Box component="dl" sx={{ m: 0, mt: 1.5, display: 'grid', gap: 0.75 }}>
              {chosen.facts.map((f) => (
                <Box key={f.label} sx={{ display: 'flex', gap: 1 }}>
                  <Typography component="dt" sx={{ fontSize: 12.5, color: t.muted, minWidth: 150 }}>
                    {f.label}
                  </Typography>
                  <Typography component="dd" sx={{ m: 0, fontSize: 12.5, color: t.ink }}>
                    {f.value}
                  </Typography>
                </Box>
              ))}
            </Box>
          ) : null}
          <Typography
            data-testid={UI_IDENTIFIERS.StateHistory.DIFF_UNAVAILABLE}
            sx={{ mt: 2, fontSize: 12.5, color: t.muted, lineHeight: 1.5 }}
          >
            {DIFF_UNAVAILABLE}
          </Typography>
        </Paper>
      ) : null}
    </Box>
  );
}
