/**
 * THE UNKNOWN BODY — the majority surface, designed as a feature.
 *
 * Every task nobody has run yet lands here — on a project mid-construction,
 * more often than every other body COMBINED. An empty state would
 * therefore be the dominant impression the construction console makes, which is
 * why this is not one: it is a briefing card that teaches the Method while it
 * waits.
 *
 * WHAT IT REFUSES TO BE
 * ---------------------
 *  - no error tone, and nothing red. Nothing has gone wrong here. `t.dangerFg`
 *    does not appear in this file.
 *  - no spinner. Nothing is loading; there is simply no record, and a spinner
 *    would promise one is on the way.
 *  - no "something went wrong" / "no data" shrug. The two REAL causes are
 *    stated (never run, or ran before per-task history existed) and neither is
 *    guessed between — the surface genuinely cannot tell them apart, and
 *    picking one would be a fabrication in a very quiet voice.
 *
 * Every word of the briefing is COMPOSED from the generated profile (its per-
 * profile labels and exit criteria) — see taskBriefing.ts, which holds the whole rule and
 * is tested without a renderer. Nothing here is authored per task.
 *
 * The ONE action this state offers is the invariant action bar's `↻ Run this
 * task` (Task 4's standing ruling: failure is never terminal, so `run` is present
 * and enabled in every state). The card used to repeat it as a second, inert
 * button — two "Run this task" buttons on one pane, one of which did nothing
 * (designer P1-5). It is not repeated.
 *
 * The heading names what is selected — the task's label, the phase's name, or
 * "This activity" (unknownTitleFor, designer P1-6) — never "This task" for an
 * activity.
 */
import type { ReactElement } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { alpha } from '@mui/material/styles';

import type { ConstructionRow } from '../../../../contracts/types';
import { useTokens } from '../../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import type { LensSelection } from '../../lens/useLensSelection';
import {
  briefingFor,
  noBriefingNoteFor,
  unknownStatementFor,
  unknownTitleFor,
  type Briefing,
} from './taskBriefing.ts';

/** The label column's width — fixed so the four rows read as a table, not prose. */
const LABEL_COLUMN = 86;

export interface UnknownBodyProps {
  row: ConstructionRow | undefined;
  selection: LensSelection;
  /** Overrides the default title when a caller has a better name for the thing. */
  title?: string | undefined;
  /**
   * Replaces the default "no record" sentence. Used by the artifact body when it
   * falls back here for a cut classification: the reason is different (this
   * stage ships no renderer for it) and saying "no record" instead would be
   * false.
   */
  statement?: string | undefined;
}

export function UnknownBody({ row, selection, title, statement }: UnknownBodyProps): ReactElement {
  const t = useTokens();
  const briefing = briefingFor(row, selection);
  return (
    <UnknownCard
      briefing={briefing}
      noBriefingNote={noBriefingNoteFor(row)}
      statement={statement ?? unknownStatementFor(briefing?.scope)}
      t={t}
      title={title ?? unknownTitleFor(briefing, selection)}
    />
  );
}

function UnknownCard({
  briefing,
  noBriefingNote,
  statement,
  title,
  t,
}: {
  briefing: Briefing | undefined;
  noBriefingNote: string;
  statement: string;
  title: string;
  t: Tokens;
}): ReactElement {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_BODY_UNKNOWN}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.25 }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        {/* The SAME hairline dashed square the tree's unknown task rows carry
            (ActivityTreeView's StateGlyph). One mark for one meaning across both
            surfaces — a second glyph here would read as a second state. */}
        <Box
          sx={{
            width: 9,
            height: 9,
            flexShrink: 0,
            border: `1px dashed ${alpha(t.line, 0.55)}`,
          }}
        />
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 12,
            letterSpacing: '0.08em',
            color: t.ink,
            textTransform: 'uppercase',
          }}
        >
          {title}
        </Typography>
      </Box>

      <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted, lineHeight: 1.5 }}>
        {statement}
      </Typography>

      {briefing !== undefined ? (
        <Box
          sx={{
            display: 'flex',
            flexDirection: 'column',
            gap: 0.85,
            mt: 0.5,
            pt: 1.25,
            borderTop: `1px solid ${t.line}`,
          }}
        >
          <BriefingRow label="What it is" t={t} value={briefing.whatItIs} />
          <BriefingRow label="Exit" t={t} value={briefing.exit} />
          <BriefingRow label="Weight" t={t} value={briefing.weight} />
          <BriefingRow label="Retry rule" t={t} value={briefing.retryRule} />
        </Box>
      ) : (
        // No single phase resolved. The card says WHY the table is absent — an
        // unclassified activity (no profile to brief) and a classified one with no
        // current phase yet are different reasons (see noBriefingNoteFor) —
        // rather than showing a plausible-looking one.
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted, lineHeight: 1.5 }}>
          {noBriefingNote}
        </Typography>
      )}
    </Box>
  );
}

function BriefingRow({
  label,
  value,
  t,
}: {
  label: string;
  value: string;
  t: Tokens;
}): ReactElement {
  return (
    <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1 }}>
      <Typography
        sx={{
          flexShrink: 0,
          width: LABEL_COLUMN,
          fontFamily: t.mono,
          fontSize: 9.5,
          fontWeight: 700,
          letterSpacing: '0.08em',
          color: t.muted,
          textTransform: 'uppercase',
          lineHeight: 1.6,
        }}
      >
        {label}
      </Typography>
      <Typography
        sx={{ flexGrow: 1, fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.5 }}
      >
        {value}
      </Typography>
    </Box>
  );
}
