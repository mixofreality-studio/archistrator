/**
 * THE PANE'S PROVENANCE NOTE — "how do we know?", spelled out where a reader
 * actually goes to check.
 *
 * The LIST lens answers this with a 3px hatched rail and an `≈ RECONSTRUCTED`
 * badge on group headers (provenance.tsx / provenanceAxis.ts). That badge is
 * load-bearing: a 2026-09-09 founder ruling widened the backfill so 21
 * activities render `100% ✓ PASSED` with every lifecycle phase complete, and six
 * of the ten tasks on each — srs, srsReview, stp, stpReview, integration,
 * testing — have no artifact behind them at all. Their whole evidence is the
 * ruling, recorded in the attempt's `provenance.basis` as
 * `founderRuling[2026-09-09]=…`.
 *
 * A reader who distrusts a green row opens the pane. Until this component
 * existed the pane showed `SRS · PASSED` with NO mark whatsoever — the same
 * laundering the list had just fixed, one surface over, on the exact surface
 * built for checking.
 *
 * TWO THINGS THE LIST'S BADGE DOES NOT DO, AND THIS DOES
 * -----------------------------------------------------
 *  1. It quotes the BASIS in the open. On the list the basis lives in a tooltip,
 *     which is right for a dense screen of 300 rows; in the pane the reader has
 *     already asked the question, so making them hover again for the answer is
 *     just a second gate on the same fact.
 *  2. It states the EVIDENCE POINTER, including its absence. Six of ten task
 *     rows per widened activity carry `{kind:'', ref:''}` — there is genuinely
 *     nothing to click, and this says so rather than rendering a dead link that
 *     would imply a record exists and merely failed to load.
 *
 * Rendered for EVERY body, above whichever one is showing, because provenance is
 * an orthogonal axis rather than a state: which body you are looking at must
 * never change whether you are told how the record came to exist.
 *
 * Colour is never the channel here either (see provenanceAxis.ts's header) — the
 * hatch is the house `scanlines` texture drawn in `currentColor`, exactly as the
 * list's rail draws it.
 */
import type { ReactElement } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { alpha } from '@mui/material/styles';

import { useTokens } from '../../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import { scanlines } from '../../../../utilities/theme/textures.ts';
import type { EvidencePointer } from '../detailPaneState.ts';
import type { ProvenanceReading } from '../../provenance';

const HATCH = scanlines('currentColor');

export interface ProvenanceNoteProps {
  reading: ProvenanceReading;
  /** The selected attempt's evidence pointer; absent when no attempt exists. */
  evidence: EvidencePointer | undefined;
}

/**
 * Renders for the `reconstructed` grade only.
 *
 * `recorded` earns no decoration — the absence of a mark IS "we watched this
 * happen", and it is the only grade allowed to be quiet. `unknown` is already
 * spoken for by whichever body is showing (the unknown body's entire copy is
 * about it), so a second block would restate it. Both return null: this note
 * exists to name the one grade a reader would otherwise mistake for fact.
 */
export function ProvenanceNote({ reading, evidence }: ProvenanceNoteProps): ReactElement | null {
  const t = useTokens();
  if (reading.origin !== 'backfilled' && reading.origin !== 'synthesized') return null;

  return (
    <Box
      data-provenance={reading.origin}
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_PROVENANCE_NOTE}
      sx={{
        display: 'flex',
        gap: 1.25,
        mb: 1.5,
        p: 1.25,
        border: `1px solid ${t.line}`,
        borderRadius: `${String(t.radius)}px`,
        bgcolor: t.paperAlt,
      }}
    >
      {/* The same 3px hatch the list's rail draws, in the same one ink. */}
      <Box
        sx={{
          width: 4,
          flexShrink: 0,
          alignSelf: 'stretch',
          color: alpha(t.ink, 0.75),
          backgroundImage: HATCH,
          backgroundSize: '3px 100%',
          backgroundRepeat: 'repeat-y',
          backgroundPosition: 'left top',
        }}
      />
      <Box sx={{ minWidth: 0, display: 'flex', flexDirection: 'column', gap: 0.6 }}>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 10,
            letterSpacing: '0.08em',
            color: t.ink,
          }}
        >
          ≈ RECONSTRUCTED · {reading.origin === 'backfilled' ? 'BACKFILLED' : 'INFERRED'}
        </Typography>
        <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.45 }}>
          This record was WRITTEN FROM the basis below, not observed. Its outcome is an assertion
          about what the work must have been, not a report of what anyone watched happen.
        </Typography>

        {reading.bases.length > 0 ? (
          <Box
            data-testid={UI_IDENTIFIERS.Construction.DETAIL_PROVENANCE_BASIS}
            sx={{ display: 'flex', flexDirection: 'column', gap: 0.4 }}
          >
            {reading.bases.map((basis) => (
              <Typography
                key={basis}
                sx={{
                  fontFamily: t.mono,
                  fontSize: 10.5,
                  color: t.muted,
                  lineHeight: 1.5,
                  wordBreak: 'break-word',
                }}
              >
                basis · {basis}
              </Typography>
            ))}
          </Box>
        ) : (
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted, lineHeight: 1.5 }}>
            basis · none recorded on this attempt.
          </Typography>
        )}

        <EvidenceLine evidence={evidence} t={t} />
      </Box>
    </Box>
  );
}

/**
 * Where to go and check. THREE distinct answers, none of them a link this
 * surface cannot back:
 *
 *   - a real pointer  → named (kind + ref), so the reader knows what to open.
 *   - an EMPTY ref    → "no evidence recorded", said plainly. This is the state
 *                       of six of the ten task rows on every widened activity,
 *                       and it is the single most important line in this file:
 *                       there is nothing to click, and pretending otherwise
 *                       would launder the ruling right back in.
 *   - no attempt      → nothing is said at all; the body above is already about
 *                       the absence of a record.
 */
function EvidenceLine({
  evidence,
  t,
}: {
  evidence: EvidencePointer | undefined;
  t: Tokens;
}): ReactElement | null {
  if (evidence === undefined) return null;
  return (
    <Typography
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_EVIDENCE}
      sx={{
        fontFamily: t.mono,
        fontSize: 10.5,
        color: t.muted,
        lineHeight: 1.5,
        wordBreak: 'break-word',
      }}
    >
      {evidence.present
        ? `evidence · ${evidence.kind} ${evidence.ref}`
        : 'evidence · none recorded — there is nothing to open for this task.'}
    </Typography>
  );
}
