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
  slotCommitted: 'Committed',
  activityStarted: 'Started',
  activityCompleted: 'Finished',
  phaseExited: 'Step done',
};

const DIFF_UNAVAILABLE =
  'A before-and-after comparison of this change isn’t available yet: the history records when each change landed and who made it, not a full snapshot of the project at every step.';

/** A recorded RFC3339 instant, in the reader's own locale; the raw value if unparseable. */
function formatAt(iso: string): string {
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return iso;
  return new Date(ms).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

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
        <Typography sx={{ fontSize: 14, fontWeight: 700, color: t.ink, mb: 0.5 }}>
          No dated changes to show yet
        </Typography>
        <Typography sx={{ fontSize: 13.5, color: t.muted, lineHeight: 1.5 }}>
          Each change appears here, newest first, once it is recorded with a date: an artifact being
          committed, or an activity starting, finishing one of its steps, or finishing altogether.
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
                  <Typography sx={{ fontWeight: 700, fontSize: 12, color: t.muted, minWidth: 72 }}>
                    {KIND_LABEL[e.kind]}
                  </Typography>
                  <Typography sx={{ fontSize: 13.5, color: t.ink, flexGrow: 1, minWidth: 0 }}>
                    {e.title}
                  </Typography>
                  <Typography
                    component="time"
                    dateTime={e.at}
                    sx={{ fontSize: 12, color: t.muted, flexShrink: 0 }}
                  >
                    {formatAt(e.at)}
                  </Typography>
                </Box>
              </Box>
            );
          })}
        </Box>
      </Paper>

      {chosen !== undefined ? (
        <Paper data-testid={UI_IDENTIFIERS.StateHistory.DETAIL} sx={{ p: 2.5 }}>
          <Typography
            component="h2"
            sx={{ color: t.ink, fontFamily: t.display, fontWeight: 700, fontSize: 16 }}
          >
            {chosen.title}
          </Typography>
          <Typography sx={{ fontSize: 12.5, color: t.muted, mt: 0.5 }}>
            {KIND_LABEL[chosen.kind]} · {formatAt(chosen.at)}
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
      ) : (
        <Paper sx={{ p: 2.5, borderStyle: 'dashed', display: { xs: 'none', md: 'block' } }}>
          <Typography sx={{ fontSize: 13, color: t.muted }}>
            Select a change on the left to see who made it and what it recorded.
          </Typography>
        </Paper>
      )}
    </Box>
  );
}
