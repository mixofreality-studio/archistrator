/**
 * The activity-list artifact — coding + noncoding activities in 5-day quanta,
 * grouped by worker class, scannable, computed-vs-authored aware. Ported visual
 * design from ux-mock ActivityList, bound to the real typed ActivityList model via
 * api/projectAdapters.toActivityListView.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Collapse from '@mui/material/Collapse';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import type { ProjectArtifactModelEnvelope } from '../../contracts/types';
import { toActivityListView, type ActivityRowView } from '../../contracts/projectAdapters';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { AuthoredBadge } from './computed';
import { SectionHeading } from './SectionHeading';
import { CommentableList } from '../comments/CommentableList';
import { activityAnchor } from '../comments/CommentContext';

// Phone width keeps two columns (activity · risk) and folds effort + kind into a
// line under the title; four fixed columns crushed the title to ~20px and the
// ids overprinted the effort column.
const ROW_GRID = { xs: '1fr 56px', sm: '1fr 110px 140px 96px' } as const;
const WIDE_ONLY = { display: { xs: 'none', sm: 'block' } } as const;

function RiskChip({ row, t }: { row: ActivityRowView; t: Tokens }): ReactNode {
  const high = row.riskBucket >= 8;
  const mid = row.riskBucket >= 3 && row.riskBucket < 8;
  const fg = high ? t.accentText : mid ? t.awaitingFg : t.muted;
  const bg = high ? t.accent : mid ? t.awaitingBg : 'transparent';
  return (
    <Box
      sx={{
        fontFamily: t.mono,
        fontSize: 10.5,
        fontWeight: 700,
        color: fg,
        bgcolor: bg,
        border: `1.5px solid ${high ? t.accent : t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        px: 0.8,
        py: 0.1,
        justifySelf: 'start',
        minWidth: 28,
        textAlign: 'center',
        whiteSpace: 'nowrap',
      }}
    >
      {String(row.riskBucket)}
    </Box>
  );
}

function Stat({
  t,
  label,
  value,
  sub,
}: {
  t: Tokens;
  label: string;
  value: string;
  sub: string;
}): ReactNode {
  return (
    <Box>
      <Typography
        sx={{ fontFamily: t.mono, fontSize: 10.5, letterSpacing: '0.12em', color: t.muted }}
      >
        {label}
      </Typography>
      <Typography
        sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 26, color: t.ink, lineHeight: 1.1 }}
      >
        {value}
      </Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>{sub}</Typography>
    </Box>
  );
}

export function ActivityListView({
  envelope,
}: {
  envelope: ProjectArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const view = toActivityListView(envelope);
  // The reader's toggles only; a group nobody has touched takes the default
  // (the first two open). Seeding a Set from `view` in useState's initialiser
  // captured an EMPTY view whenever the slot arrived after the first render — so
  // every group started collapsed and the list read as headers with nothing in them.
  const [toggled, setToggled] = useState<ReadonlyMap<string, boolean>>(() => new Map());
  const isGroupOpen = (id: string, index: number): boolean => toggled.get(id) ?? index < 2;
  const toggle = (id: string, index: number): void => {
    setToggled((m) => new Map(m).set(id, !isGroupOpen(id, index)));
  };

  if (view.totalActivities === 0) {
    return (
      <Paper sx={{ p: 3 }}>
        <SectionHeading
          hint="The activity list appears once the architect drafts it: one activity per component to build, plus the noncoding work (requirements, architecture, test planning), each estimated in 5-day units."
          t={t}
        >
          Activity list — not drafted yet
        </SectionHeading>
      </Paper>
    );
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, maxWidth: 1040 }}>
      <SectionHeading
        adornment={<AuthoredBadge t={t} />}
        hint="Every piece of work the plan schedules: one coding activity per component (its detailed design and construction are phases inside it, not separate activities), plus the noncoding work. The architect estimates each in 5-day units; the totals below are summed from those estimates."
        t={t}
      >
        Activity list
      </SectionHeading>
      <Paper sx={{ p: 2, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 3 }}>
        <Stat
          label="ACTIVITIES"
          sub={`${String(view.codingCount)} coding · ${String(view.noncodingCount)} noncoding`}
          t={t}
          value={String(view.totalActivities)}
        />
        <Stat
          label="EFFORT"
          sub="person-days"
          t={t}
          value={view.totalPersonDays.toLocaleString()}
        />
        <Stat
          label="QUANTA"
          sub="5-day estimation units"
          t={t}
          value={(view.totalPersonDays / 5).toFixed(1)}
        />
      </Paper>

      <Typography sx={{ fontFamily: t.body, fontSize: 13, color: t.muted, lineHeight: 1.5 }}>
        Grouped by who does the work (worker class). Risk is the architect&rsquo;s relative
        uncertainty for each activity on a Fibonacci scale — 1, 2, 3, 5, 8, 13 — where higher means
        less certain. A filled left bar marks coding work; a grey bar marks noncoding work.
      </Typography>

      {view.groups.map((g, gi) => {
        const isOpen = isGroupOpen(g.group, gi);
        return (
          <Paper key={g.group} sx={{ overflow: 'hidden' }}>
            <Box
              aria-expanded={isOpen}
              aria-label={`${g.group} — ${String(g.count)} activities, ${String(g.totalDays)} person-days of effort`}
              role="button"
              sx={{
                display: 'flex',
                flexWrap: 'wrap',
                alignItems: 'center',
                gap: 1.5,
                px: 2,
                py: 1.25,
                cursor: 'pointer',
                bgcolor: t.paperAlt,
                borderBottom: isOpen ? `1.5px solid ${t.line}` : 'none',
                '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: -2 },
              }}
              tabIndex={0}
              onClick={() => {
                toggle(g.group, gi);
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  toggle(g.group, gi);
                }
              }}
            >
              <Typography
                sx={{
                  fontFamily: t.display,
                  fontWeight: 700,
                  fontSize: 16,
                  color: t.ink,
                  flexGrow: 1,
                  minWidth: 0,
                }}
              >
                {g.group}
              </Typography>
              <Chip
                label={`${String(g.count)} activities`}
                size="small"
                sx={{ bgcolor: t.paper, color: t.ink }}
              />
              <Chip
                label={`${String(g.totalDays)} person-days`}
                size="small"
                sx={{ bgcolor: t.committedBg, color: t.committedFg }}
              />
              <ExpandMoreIcon
                sx={{
                  color: t.muted,
                  transform: isOpen ? 'rotate(180deg)' : 'none',
                  transition: '120ms',
                }}
              />
            </Box>
            <Collapse in={isOpen}>
              <Box>
                <Box
                  sx={{
                    display: 'grid',
                    gridTemplateColumns: ROW_GRID,
                    gap: 1,
                    px: 3,
                    py: 0.75,
                    borderBottom: `1.5px solid ${t.line}`,
                  }}
                >
                  {['ACTIVITY', 'EFFORT', 'KIND', 'RISK (1–13)'].map((h) => (
                    <Typography
                      aria-hidden
                      key={h}
                      sx={{
                        fontFamily: t.mono,
                        fontSize: 10.5,
                        letterSpacing: '0.1em',
                        color: t.muted,
                        ...(h === 'EFFORT' || h === 'KIND' ? WIDE_ONLY : {}),
                      }}
                    >
                      {h}
                    </Typography>
                  ))}
                </Box>
                <CommentableList
                  ariaLabel={`${g.group} activities`}
                  getAnchor={(r) => ({
                    kind: 'node',
                    label: r.title,
                    source: `Activity List · ${r.name}`,
                    jsonPath: activityAnchor(r.name),
                  })}
                  getKey={(r, i) => `${r.name}-${String(i)}`}
                  getLabel={(r) => r.title}
                  getLabelKind={() => 'activity'}
                  items={g.rows}
                  renderItem={(r) => (
                    <Box
                      sx={{
                        display: 'grid',
                        gridTemplateColumns: ROW_GRID,
                        gap: 1,
                        alignItems: 'center',
                      }}
                    >
                      <Box
                        sx={{
                          borderLeft: `4px solid ${r.coding ? t.accent2 : t.muted}`,
                          pl: 0.75,
                          minWidth: 0,
                        }}
                      >
                        <Typography
                          sx={{ fontFamily: t.body, fontSize: 13, color: t.ink, lineHeight: 1.25 }}
                        >
                          {r.title}
                        </Typography>
                        <Typography
                          sx={{
                            fontFamily: t.mono,
                            fontSize: 11,
                            color: t.muted,
                            overflowWrap: 'anywhere',
                          }}
                        >
                          {r.name}
                        </Typography>
                        <Typography
                          sx={{
                            fontFamily: t.mono,
                            fontSize: 11,
                            color: t.muted,
                            display: { xs: 'block', sm: 'none' },
                          }}
                        >
                          {`${String(r.effortDays)} days · ${r.coding ? 'coding' : 'noncoding'}`}
                        </Typography>
                      </Box>
                      <Typography
                        sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, ...WIDE_ONLY }}
                      >
                        {r.effortDays} days
                      </Typography>
                      <Typography
                        sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, ...WIDE_ONLY }}
                      >
                        {r.coding ? 'coding' : 'noncoding'}
                      </Typography>
                      <RiskChip row={r} t={t} />
                    </Box>
                  )}
                />
              </Box>
            </Collapse>
          </Paper>
        );
      })}
    </Box>
  );
}
