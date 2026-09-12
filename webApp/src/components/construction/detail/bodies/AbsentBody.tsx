/**
 * THE ABSENT BODY — the unknown body's distinct sibling.
 *
 * "No record" and "this activity does not have that phase" are DIFFERENT FACTS,
 * and the whole of Stage B is a sequence of refusals to collapse pairs like
 * them: activityTree.ts keeps `unknown` apart from `incomplete`,
 * provenanceAxis.ts keeps `unknown` apart from `observed`, and mapConstructionRow
 * keeps "unclassified" apart from "no build evidence". This is the same refusal
 * one level down.
 *
 * A Deployment activity's Figure A-1 profile is Provisioning Spec / Construction
 * / Convergence Verification — no Requirements, no Test Plan. A deep link naming
 * `?p=test_plan` on one is therefore CORRECT ABSENCE, not a gap in the data, and
 * answering it with the unknown body's "no record" sentence would invite a
 * reader to go looking for something that was never scheduled.
 *
 * So this body says so in those words, and then shows what the profile DOES
 * carry — the reader's real question is "then what is there?", and the generated
 * profile answers it without a round trip. There is deliberately no run CTA:
 * there is nothing here to run.
 */
import type { ReactElement } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import { useTokens } from '../../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import type { ProfileAbsence } from './taskBriefing.ts';

export function AbsentBody({ absence }: { absence: ProfileAbsence }): ReactElement {
  const t = useTokens();
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_BODY_ABSENT}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.25 }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        {/* A struck-through box: present in the vocabulary, absent from THIS
            profile. Deliberately not the unknown body's dashed square — the two
            states must not share a mark. */}
        <Box
          sx={{
            width: 9,
            height: 9,
            flexShrink: 0,
            border: `1px solid ${t.line}`,
            backgroundImage: `linear-gradient(to bottom right, transparent calc(50% - 0.5px), ${t.line} calc(50% - 0.5px), ${t.line} calc(50% + 0.5px), transparent calc(50% + 0.5px))`,
          }}
        />
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 12,
            letterSpacing: '0.08em',
            color: t.muted,
            textTransform: 'uppercase',
          }}
        >
          {absence.title} · not carried
        </Typography>
      </Box>

      <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.5 }}>
        {absence.statement}
      </Typography>

      <Box sx={{ mt: 0.5, pt: 1.25, borderTop: `1px solid ${t.line}` }}>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 9.5,
            fontWeight: 700,
            letterSpacing: '0.08em',
            color: t.muted,
            textTransform: 'uppercase',
            mb: 0.75,
          }}
        >
          What this activity does carry
        </Typography>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.4 }}>
          {absence.carried.map((p) => (
            <CarriedRow key={p.name} name={p.name} t={t} weight={p.weight} />
          ))}
        </Box>
      </Box>
    </Box>
  );
}

function CarriedRow({
  name,
  weight,
  t,
}: {
  name: string;
  weight: number;
  t: Tokens;
}): ReactElement {
  return (
    <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1 }}>
      <Typography sx={{ flexGrow: 1, fontFamily: t.body, fontSize: 12, color: t.ink }}>
        {name}
      </Typography>
      <Typography
        sx={{ flexShrink: 0, fontFamily: t.mono, fontSize: 10.5, fontWeight: 700, color: t.muted }}
      >
        {String(weight)}%
      </Typography>
    </Box>
  );
}
