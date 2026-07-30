/**
 * Required Behaviors as a keyboard-navigable, item-commentable list — NOT flat
 * prose. Each behavior carries a stable `B-NN` id, so it renders as a row in the
 * shared CommentableList with a per-item "Comment on this item" button that anchors
 * `$.items[id="B-NN"]` (survives a redraft that reorders items).
 *
 * Three tiers per row:
 *   • the scrubbed BEHAVIOR text is the main line (B-id + statement, as before),
 *   • `statedAs` — the raw stated ask(s) this behavior was scrubbed/consolidated
 *     from — renders as a subtle expandable "stated as:" provenance line, so the
 *     customer can see their own words survived the scrub, and
 *   • `volatilityHint` — the candidate volatility name(s) this behavior points at —
 *     renders as small inline chip LINKS to the Volatilities step (StepLink; the
 *     former "text only, no router coupling" constraint was lifted with the
 *     navigable-joins pass — chips are now real, keyboard-reachable links).
 * Bound to adapters.toScrubbedRequirementsView. Safe-empty.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import ButtonBase from '@mui/material/ButtonBase';
import Typography from '@mui/material/Typography';
import { toScrubbedRequirementsView } from '../contracts/adapters';
import type { ArtifactModelEnvelope } from '../contracts/types';
import { CommentableList } from './comments/CommentableList';
import { scrubbedRequirementAnchor } from './comments/CommentContext';
import { StepLink } from './shared/StepLink';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

/** The candidate-volatility hints as small chip-shaped links to the Volatilities
 *  step (the chip border signals interactivity; hover underlines). */
function VolatilityHints({ hints }: { hints: string[] }): ReactNode {
  const t = useTokens();
  if (hints.length === 0) return null;
  return (
    <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.6, mt: 0.6 }}>
      {hints.map((h) => (
        <StepLink
          key={h}
          kind="volatilities"
          label={h}
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
          testId={UI_IDENTIFIERS.ScrubbedRequirements.hintLink(h)}
          underline="none"
        >
          {h}
        </StepLink>
      ))}
    </Box>
  );
}

/** The raw stated ask(s) this behavior came from, as an expandable provenance line. */
function StatedAsProvenance({ statedAs }: { statedAs: string[] }): ReactNode {
  const t = useTokens();
  const [open, setOpen] = useState(false);
  if (statedAs.length === 0) return null;
  return (
    <Box sx={{ mt: 0.5 }}>
      <ButtonBase
        aria-expanded={open}
        sx={{
          justifyContent: 'flex-start',
          fontFamily: t.mono,
          fontSize: 10.5,
          fontWeight: 700,
          color: t.muted,
          letterSpacing: '0.03em',
          '&:hover': { color: t.ink },
        }}
        onClick={() => {
          setOpen((o) => !o);
        }}
      >
        {open ? '▾' : '▸'} stated as{statedAs.length > 1 ? ` (${String(statedAs.length)})` : ''}
      </ButtonBase>
      {open ? (
        <Box component="ul" sx={{ listStyle: 'none', m: 0, mt: 0.35, pl: 1.5 }}>
          {statedAs.map((s, i) => (
            <Typography
              component="li"
              key={i}
              sx={{
                fontFamily: t.body,
                fontSize: '0.85rem',
                fontStyle: 'italic',
                color: t.muted,
                lineHeight: 1.5,
                mb: 0.35,
                pl: 1.25,
                borderLeft: `2px solid ${t.line}`,
              }}
            >
              {s}
            </Typography>
          ))}
        </Box>
      ) : null}
    </Box>
  );
}

export function ScrubbedRequirementsView({
  envelope,
}: {
  envelope: ArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const items = toScrubbedRequirementsView(envelope);

  if (items.length === 0) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No required behaviors drafted yet.
      </Box>
    );
  }

  return (
    <CommentableList
      ariaLabel="Required behaviors"
      getAnchor={(item, i) => ({
        kind: 'node',
        label: item.id,
        source: `${item.id} · behavior`,
        jsonPath: scrubbedRequirementAnchor(i, item.id),
      })}
      getKey={(item, i) => item.id || `behavior-${String(i)}`}
      getLabel={(item) => `behavior ${item.id}`}
      items={items}
      renderItem={(item) => (
        <Box>
          <Box sx={{ display: 'flex', gap: 1.25, alignItems: 'baseline' }}>
            <Typography
              component="span"
              sx={{
                fontFamily: t.mono,
                fontSize: 12,
                fontWeight: 700,
                color: t.accent2,
                flexShrink: 0,
              }}
            >
              {item.id}
            </Typography>
            <Typography component="span" sx={{ color: t.ink, fontFamily: t.body, lineHeight: 1.6 }}>
              {item.statement}
            </Typography>
          </Box>
          <StatedAsProvenance statedAs={item.statedAs ?? []} />
          <VolatilityHints hints={item.volatilityHint ?? []} />
        </Box>
      )}
    />
  );
}
