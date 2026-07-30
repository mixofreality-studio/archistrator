/**
 * The glossary as a searchable, filterable reference — NOT prose. The typed
 * GlossaryItem[] already carries a `category`, so this renders a real widget
 * (search-as-you-type + category filter chips + alphabetized, category-grouped
 * list with sticky subheaders) instead of the flat markdown fallback a ~40-term
 * reference was drowning in. Bound to adapters.toGlossaryView. Recolored from
 * theme tokens so it rides the active theme. Safe-empty.
 *
 * The chip bar aggregates by the Four-Questions BASE (categoryBase), so refined
 * "How · Activity"-style sub-labels roll up under one "How" chip; section
 * headers keep the refined labels. The aggregation/filtering rules are pure
 * logic in glossaryLogic.ts (unit-tested there). Comment anchors key by the
 * item's ORIGINAL array index, so duplicate term names never collide. A
 * visually-hidden polite live region (StageChip pattern, keyed by message)
 * announces the match count when filtering changes it.
 */
import { useMemo, useRef, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import InputAdornment from '@mui/material/InputAdornment';
import IconButton from '@mui/material/IconButton';
import Chip from '@mui/material/Chip';
import Stack from '@mui/material/Stack';
import SearchIcon from '@mui/icons-material/Search';
import ClearIcon from '@mui/icons-material/Close';
import { toGlossaryView } from '../contracts/adapters';
import type { ArtifactModelEnvelope } from '../contracts/types';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';
import { CommentableList } from './comments/CommentableList';
import { glossaryItemAnchor } from './comments/CommentContext';
import { useCommittedSlotEnvelope } from './CommittedSlotsContext';
import { StepLink } from './shared/StepLink';
import {
  buildUsageCorpus,
  chipCategories,
  filterGlossary,
  indexGlossaryItems,
  matchAnnouncement,
  termUsage,
  USAGE_STEP_LABELS,
  type UsageChip,
} from './glossaryLogic';

/**
 * The quiet per-term usage row: where this term is USED across the committed
 * downstream artifacts, one chip-link per step ("Behaviors ×3" — counts per
 * step, never a per-occurrence listing). Nothing renders for an unused term or
 * before the downstream slots commit.
 */
function UsageChips({ index, chips }: { index: number; chips: UsageChip[] }): ReactNode {
  const t = useTokens();
  if (chips.length === 0) return null;
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Glossary.usage(index)}
      sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.6, mt: 0.5 }}
    >
      {chips.map((c) => (
        <StepLink
          key={c.kind}
          kind={c.kind}
          label={`Used in ${String(c.count)} ${USAGE_STEP_LABELS[c.kind]} item${c.count === 1 ? '' : 's'}`}
          sx={{
            px: 0.7,
            py: 0.15,
            borderRadius: 1,
            border: `1px solid ${t.line}`,
            bgcolor: t.paperAlt,
            fontFamily: t.mono,
            fontSize: 10,
            color: t.accent2,
            letterSpacing: '0.02em',
            '&:hover': { borderColor: t.accent, textDecoration: 'underline' },
          }}
          testId={UI_IDENTIFIERS.Glossary.usageLink(index, c.kind)}
          underline="none"
        >
          {USAGE_STEP_LABELS[c.kind]} ×{c.count}
        </StepLink>
      ))}
    </Box>
  );
}

export function GlossaryView({
  envelope,
  height = 620,
  fill = false,
}: {
  envelope: ArtifactModelEnvelope | undefined;
  height?: number;
  /**
   * Fill the available vertical height of a flex-column parent instead of sitting
   * at the fixed `height`. The full-screen design experience passes this so the
   * committed/draft glossary card grows to the bottom of the scroll area (no dead
   * space below it) while its own grouped list keeps scrolling internally. The
   * outer scroll container owns the short-viewport floor, so a squeezed viewport
   * scrolls the page instead of collapsing the card. The home base (ArtifactPane)
   * leaves this false and keeps the fixed `height`.
   */
  fill?: boolean;
}): ReactNode {
  const t = useTokens();
  const items = toGlossaryView(envelope);
  const [query, setQuery] = useState('');
  const [activeBase, setActiveBase] = useState<string | null>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  // Items paired with their index in the ORIGINAL model array, so a per-term
  // comment anchors to `$.items[n]` regardless of the filtered/regrouped
  // display order — and duplicates keep distinct anchors.
  const entries = useMemo(() => indexGlossaryItems(items), [items]);

  // Chip counts track the live query (text-only, ignoring the active category),
  // so a chip never advertises stale totals while a filter is typed. The "All"
  // chip's count is the query's grand total — the sum of the per-chip counts.
  const categories = useMemo(() => chipCategories(entries, query), [entries, query]);
  const allCount = categories.reduce((n, [, count]) => n + count, 0);

  const grouped = useMemo(
    () => filterGlossary(entries, query, activeBase),
    [entries, query, activeBase]
  );

  // Cross-artifact term-usage joins: the committed downstream slots arrive via
  // CommittedSlotsContext (no provider / not committed → empty corpus → no
  // chips), the corpus + per-term counts are memoized once per glossary render.
  const behaviorsEnvelope = useCommittedSlotEnvelope('scrubbedRequirements');
  const volatilitiesEnvelope = useCommittedSlotEnvelope('volatilities');
  const useCasesEnvelope = useCommittedSlotEnvelope('coreUseCases');
  const systemEnvelope = useCommittedSlotEnvelope('system');
  const usage = useMemo(() => {
    const corpus = buildUsageCorpus({
      scrubbedRequirements: behaviorsEnvelope,
      volatilities: volatilitiesEnvelope,
      coreUseCases: useCasesEnvelope,
      system: systemEnvelope,
    });
    return new Map(entries.map((e) => [e.index, termUsage(e.item.term, corpus)]));
  }, [entries, behaviorsEnvelope, volatilitiesEnvelope, useCasesEnvelope, systemEnvelope]);

  if (items.length === 0) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No glossary drafted yet.
      </Box>
    );
  }

  const announcement = matchAnnouncement(grouped.total);

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Glossary.ROOT}
      sx={{
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        // fill: grow to the parent flex column's height (the parent owns the floor);
        // otherwise sit at the fixed pixel height (home base ArtifactPane).
        ...(fill ? { flexGrow: 1, minHeight: 0 } : { height }),
      }}
    >
      {/* pinned header: search + category filter chips */}
      <Box sx={{ p: 2, borderBottom: `1px solid ${t.line}`, flexShrink: 0 }}>
        <TextField
          fullWidth
          inputRef={searchRef}
          placeholder="Filter terms…"
          size="small"
          slotProps={{
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <SearchIcon sx={{ fontSize: 18, color: t.muted }} />
                </InputAdornment>
              ),
              endAdornment:
                query !== '' ? (
                  <InputAdornment position="end">
                    <IconButton
                      aria-label="Clear filter"
                      data-testid={UI_IDENTIFIERS.Glossary.CLEAR}
                      edge="end"
                      size="small"
                      onClick={() => {
                        setQuery('');
                        searchRef.current?.focus();
                      }}
                    >
                      <ClearIcon sx={{ fontSize: 18, color: t.muted }} />
                    </IconButton>
                  </InputAdornment>
                ) : undefined,
            },
            htmlInput: {
              'aria-label': 'Filter glossary terms',
              'data-testid': UI_IDENTIFIERS.Glossary.SEARCH,
            },
          }}
          sx={{ mb: 1.5 }}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
          }}
          onKeyDown={(e) => {
            // Escape clears an active query; when already empty, let it bubble
            // (so an enclosing dialog/menu can handle its own Escape).
            if (e.key === 'Escape' && query !== '') {
              e.preventDefault();
              setQuery('');
            }
          }}
        />
        <Stack useFlexGap direction="row" spacing={1} sx={{ flexWrap: 'wrap', gap: 1 }}>
          <Chip
            aria-pressed={activeBase === null}
            data-testid={UI_IDENTIFIERS.Glossary.CHIP_ALL}
            label={`All · ${String(allCount)}`}
            size="small"
            variant={activeBase === null ? 'filled' : 'outlined'}
            onClick={() => {
              setActiveBase(null);
            }}
          />
          {categories.map(([base, count]) => (
            <Chip
              aria-pressed={activeBase === base}
              data-testid={UI_IDENTIFIERS.Glossary.chip(base)}
              key={base}
              label={`${base} · ${String(count)}`}
              size="small"
              variant={activeBase === base ? 'filled' : 'outlined'}
              onClick={() => {
                setActiveBase(activeBase === base ? null : base);
              }}
            />
          ))}
        </Stack>
      </Box>

      {/* Visually-hidden polite announcer: the keyed child only remounts (and so
          only announces) when the MESSAGE changes, not on every re-render. */}
      <Box
        aria-live="polite"
        component="span"
        role="status"
        sx={{
          position: 'absolute',
          width: '1px',
          height: '1px',
          margin: '-1px',
          padding: 0,
          overflow: 'hidden',
          clipPath: 'inset(50%)',
          whiteSpace: 'nowrap',
        }}
      >
        <span key={announcement}>{announcement}</span>
      </Box>

      {/* scrollable grouped list */}
      <Box sx={{ overflowY: 'auto', flexGrow: 1, px: 2, pb: 2 }}>
        {grouped.total === 0 ? (
          <Box
            data-testid={UI_IDENTIFIERS.Glossary.EMPTY}
            sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}
          >
            No terms match “{query}”.
          </Box>
        ) : (
          grouped.sections.map(([cat, sectionEntries]) => (
            <Box key={cat} sx={{ mb: 1.5 }}>
              <Typography
                data-testid={UI_IDENTIFIERS.Glossary.section(cat)}
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
                {cat} · {sectionEntries.length}
              </Typography>
              <CommentableList
                ariaLabel={`${cat} glossary terms`}
                getAnchor={(e) => ({
                  kind: 'node',
                  label: e.item.term,
                  source: `Glossary · ${e.item.term}`,
                  jsonPath: glossaryItemAnchor(e.index),
                })}
                getKey={(e) => `${e.item.term}-${String(e.index)}`}
                getLabel={(e) => e.item.term}
                getLabelKind={() => 'term'}
                items={sectionEntries}
                renderItem={(e) => (
                  <Box sx={{ maxWidth: 760 }}>
                    <Typography
                      component="span"
                      sx={{ fontWeight: 700, fontFamily: t.body, color: t.ink }}
                    >
                      {e.item.term}
                    </Typography>
                    <Typography
                      component="span"
                      sx={{ color: t.ink, fontFamily: t.body, lineHeight: 1.6 }}
                    >
                      {' — '}
                      {e.item.definition}
                    </Typography>
                    <UsageChips chips={usage.get(e.index) ?? []} index={e.index} />
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
