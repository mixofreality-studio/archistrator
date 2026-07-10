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
import { toGlossaryView } from '../contracts/adapters';
import type { ArtifactModelEnvelope, GlossaryItem } from '../contracts/types';
import { useTokens } from '../utilities/theme/ThemeContext';
import { CommentableList } from './comments/CommentableList';
import { glossaryItemAnchor } from './comments/CommentContext';

// The Four Questions, in canonical order; anything else sinks to the end.
const CATEGORY_ORDER = ['Who', 'What', 'How', 'Where', 'Uncategorized'];

/**
 * Normalize a raw category tag to its display label. The "How" question is refined
 * by The Method into distinct sub-kinds (How-activity vs How-resource-access); we
 * KEEP that distinction as "How · Activity" / "How · Resource Access" rather than
 * collapsing every How-* onto one bucket, while still tolerating a plain "How".
 * Blank/absent sinks to "Uncategorized".
 */
function normalizeCategory(c: string | undefined): string {
  if (c === undefined || c.trim() === '') return 'Uncategorized';
  const trimmed = c.trim();
  if (trimmed === 'How') return 'How';
  // How-activity / How resource access / How-resource-access → "How · <Sub>".
  const howMatch = /^How[\s-]+(.+)$/i.exec(trimmed);
  if (howMatch?.[1] !== undefined) {
    const sub = howMatch[1]
      .split(/[\s-]+/)
      .map((w) => (w.length > 0 ? w.charAt(0).toUpperCase() + w.slice(1) : w))
      .join(' ');
    return `How · ${sub}`;
  }
  return trimmed;
}

/** The Four-Questions bucket a (possibly refined) label ranks under. */
function categoryBase(c: string): string {
  return c.startsWith('How') ? 'How' : c;
}

function categoryRank(c: string): number {
  const i = CATEGORY_ORDER.indexOf(categoryBase(c));
  // Refined How sub-labels share the "How" rank but sort after the plain bucket.
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

  // Term → index in the ORIGINAL model items array, so a per-term comment anchors
  // to `$.items[n]` regardless of the filtered/regrouped display order.
  const originalIndex = useMemo(() => {
    const m = new Map<string, number>();
    items.forEach((it, i) => m.set(it.term, i));
    return m;
  }, [items]);

  const categories = useMemo(() => {
    const counts = new Map<string, number>();
    for (const it of items) {
      const c = normalizeCategory(it.category);
      counts.set(c, (counts.get(c) ?? 0) + 1);
    }
    return [...counts.entries()].sort(
      (a, b) => categoryRank(a[0]) - categoryRank(b[0]) || a[0].localeCompare(b[0])
    );
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
      sections: [...g.entries()].sort(
        (a, b) => categoryRank(a[0]) - categoryRank(b[0]) || a[0].localeCompare(b[0])
      ),
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
              <CommentableList
                ariaLabel={`${cat} glossary terms`}
                getAnchor={(it) => ({
                  kind: 'node',
                  label: it.term,
                  source: `Glossary · ${it.term}`,
                  jsonPath: glossaryItemAnchor(originalIndex.get(it.term) ?? 0),
                })}
                getKey={(it) => it.term}
                getLabel={(it) => it.term}
                getLabelKind={() => 'term'}
                items={entries}
                renderItem={(it) => (
                  <Box sx={{ maxWidth: 760 }}>
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
                )}
              />
            </Box>
          ))
        )}
      </Box>
    </Paper>
  );
}
