/**
 * The glossary as a searchable, filterable reference — NOT prose. The typed
 * GlossaryItem[] already carries a `category`, so this renders a real widget
 * (search-as-you-type + category filter chips + alphabetized, category-grouped
 * list with sticky subheaders) instead of the flat markdown fallback a ~40-term
 * reference was drowning in. Bound to adapters.toGlossaryView. Recolored from
 * theme tokens so it rides the active theme. Safe-empty.
 */
import { useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import InputAdornment from '@mui/material/InputAdornment';
import Chip from '@mui/material/Chip';
import Stack from '@mui/material/Stack';
import SearchIcon from '@mui/icons-material/Search';
import { toGlossaryView } from '../api/adapters';
import type { ArtifactModelEnvelope } from '../api/types';
import type { GlossaryItem } from '../api/models';
import { useTokens } from '../theme/ThemeContext';

// The Four Questions, in canonical order; anything else sinks to the end.
const CATEGORY_ORDER = ['Who', 'What', 'How', 'Where', 'Uncategorized'];

/** Fold legacy tags (e.g. "How-activity", blank) onto the canonical four. */
function normalizeCategory(c: string | undefined): string {
  if (c === undefined || c.trim() === '') return 'Uncategorized';
  if (c.startsWith('How')) return 'How';
  return c;
}

function categoryRank(c: string): number {
  const i = CATEGORY_ORDER.indexOf(c);
  return i === -1 ? CATEGORY_ORDER.length : i;
}

export function GlossaryView({
  envelope,
  height = 620,
}: {
  envelope: ArtifactModelEnvelope | undefined;
  height?: number;
}): ReactNode {
  const t = useTokens();
  const items = toGlossaryView(envelope);
  const [query, setQuery] = useState('');
  const [activeCat, setActiveCat] = useState<string | null>(null);

  const categories = useMemo(() => {
    const counts = new Map<string, number>();
    for (const it of items) {
      const c = normalizeCategory(it.category);
      counts.set(c, (counts.get(c) ?? 0) + 1);
    }
    return [...counts.entries()].sort((a, b) => categoryRank(a[0]) - categoryRank(b[0]));
  }, [items]);

  const grouped = useMemo(() => {
    const q = query.trim().toLowerCase();
    const matches = items.filter((it) => {
      if (activeCat !== null && normalizeCategory(it.category) !== activeCat) return false;
      if (q === '') return true;
      return it.term.toLowerCase().includes(q) || it.definition.toLowerCase().includes(q);
    });
    const g = new Map<string, GlossaryItem[]>();
    for (const it of matches) {
      const c = normalizeCategory(it.category);
      const arr = g.get(c) ?? [];
      arr.push(it);
      g.set(c, arr);
    }
    for (const arr of g.values()) arr.sort((a, b) => a.term.localeCompare(b.term));
    return {
      total: matches.length,
      sections: [...g.entries()].sort((a, b) => categoryRank(a[0]) - categoryRank(b[0])),
    };
  }, [items, query, activeCat]);

  if (items.length === 0) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No glossary drafted yet.
      </Box>
    );
  }

  return (
    <Paper sx={{ display: 'flex', flexDirection: 'column', overflow: 'hidden', height }}>
      {/* pinned header: search + category filter chips */}
      <Box sx={{ p: 2, borderBottom: `1px solid ${t.line}`, flexShrink: 0 }}>
        <TextField
          fullWidth
          placeholder="Filter terms…"
          size="small"
          slotProps={{
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <SearchIcon sx={{ fontSize: 18, color: t.muted }} />
                </InputAdornment>
              ),
            },
          }}
          sx={{ mb: 1.5 }}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
          }}
        />
        <Stack useFlexGap direction="row" spacing={1} sx={{ flexWrap: 'wrap', gap: 1 }}>
          <Chip
            label={`All · ${String(items.length)}`}
            size="small"
            variant={activeCat === null ? 'filled' : 'outlined'}
            onClick={() => {
              setActiveCat(null);
            }}
          />
          {categories.map(([cat, count]) => (
            <Chip
              key={cat}
              label={`${cat} · ${String(count)}`}
              size="small"
              variant={activeCat === cat ? 'filled' : 'outlined'}
              onClick={() => {
                setActiveCat(activeCat === cat ? null : cat);
              }}
            />
          ))}
        </Stack>
      </Box>

      {/* scrollable grouped list */}
      <Box sx={{ overflowY: 'auto', flexGrow: 1, px: 2, pb: 2 }}>
        {grouped.total === 0 ? (
          <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
            No terms match “{query}”.
          </Box>
        ) : (
          grouped.sections.map(([cat, entries]) => (
            <Box key={cat} sx={{ mb: 1.5 }}>
              <Typography
                sx={{
                  position: 'sticky',
                  top: 0,
                  zIndex: 1,
                  bgcolor: t.paper,
                  py: 1,
                  fontFamily: t.mono,
                  fontSize: 12,
                  letterSpacing: '0.08em',
                  textTransform: 'uppercase',
                  color: t.muted,
                  borderBottom: `1px solid ${t.line}`,
                }}
              >
                {cat} · {entries.length}
              </Typography>
              {entries.map((it) => (
                <Box key={it.term} sx={{ py: 1.25, maxWidth: 760 }}>
                  <Typography
                    component="span"
                    sx={{ fontWeight: 700, fontFamily: t.body, color: t.ink }}
                  >
                    {it.term}
                  </Typography>
                  <Typography
                    component="span"
                    sx={{ color: t.ink, fontFamily: t.body, lineHeight: 1.6 }}
                  >
                    {' — '}
                    {it.definition}
                  </Typography>
                </Box>
              ))}
            </Box>
          ))
        )}
      </Box>
    </Paper>
  );
}
