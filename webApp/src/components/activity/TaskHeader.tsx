/**
 * The body header shared by BOTH task kinds — what the task is on the left, the
 * revision select on the right, in the same place and with the same look whether
 * you are reading the episode that produced a draft or the review that judged it.
 * A revision is one thing seen from two sides (lifecycleGraphTypes.ts), so the
 * control that moves between revisions must not move around the screen with it.
 *
 * Under the title sit the three FACTS of the task — worker class, command, exit
 * criterion. None of them is on the wire: all three are joined in from
 * `lifecycles.gen.ts` by `taskFactsFor`, and a fact the join did not find is
 * OMITTED rather than rendered as an empty value. A labelled blank reads as "this
 * task has no exit criterion", which is a claim, and a false one.
 *
 * Ported from the prototype's `TaskHeader.tsx`, minus its node-kind eyebrow: the
 * graph's pip already says which kind a task is, by icon, and the body's own
 * furniture says it again below.
 *
 * Pure and props-only.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import { RevisionSelect } from './RevisionSelect';
import type { TaskFacts } from './activityViewToGraph.ts';
import type { LifecycleRevision } from './lifecycleGraphTypes.ts';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

function Fact({ label, value }: { label: string; value: string }): ReactNode {
  const t = useTokens();
  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography
        sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.14em', color: t.muted }}
      >
        {label}
      </Typography>
      <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13, color: t.ink }}>
        {value}
      </Typography>
    </Box>
  );
}

export function TaskHeader({
  title,
  facts,
  revisions,
  revision,
  onRevision,
}: {
  title: string;
  facts: TaskFacts;
  revisions: readonly LifecycleRevision[];
  revision: number;
  onRevision: (n: number) => void;
}): ReactNode {
  const t = useTokens();
  // Nothing joined: an activity type this build's `lifecycles.gen.ts` predates,
  // or a task id the table does not carry. An empty bordered frame would read as
  // "this task has no facts"; no frame reads as what it is.
  const anyFact =
    facts.workerClass !== undefined ||
    facts.command !== undefined ||
    facts.exitCriterion !== undefined;
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
      <Box sx={{ display: 'flex', alignItems: 'flex-end', gap: 2, flexWrap: 'wrap' }}>
        <Box sx={{ minWidth: 0, flexGrow: 1 }}>
          <Typography
            component="h2"
            sx={{
              fontFamily: t.display,
              fontWeight: 800,
              fontSize: 26,
              lineHeight: 1.15,
              color: t.ink,
              m: 0,
            }}
          >
            {title}
          </Typography>
        </Box>
        <RevisionSelect revisions={revisions} value={revision} onChange={onRevision} />
      </Box>

      {anyFact ? (
        <Box
          data-testid={UI_IDENTIFIERS.Activity.TASK_FACTS}
          sx={{
            display: 'flex',
            alignItems: 'flex-start',
            gap: 3,
            flexWrap: 'wrap',
            px: 2,
            py: 1.5,
            bgcolor: t.paperAlt,
            border: `1.5px solid ${t.line}`,
            borderRadius: t.radius / 8 + 0.5,
          }}
        >
          {facts.workerClass !== undefined ? (
            <Fact label="WORKER CLASS" value={facts.workerClass} />
          ) : null}
          {facts.command !== undefined ? (
            <Fact label="COMMAND" value={`/${facts.command}`} />
          ) : null}
          {facts.exitCriterion !== undefined ? (
            <Box sx={{ minWidth: 240, flexBasis: 320, flexGrow: 1 }}>
              <Typography
                sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.14em', color: t.muted }}
              >
                EXIT CRITERION
              </Typography>
              <Typography sx={{ fontSize: 13.5, color: t.ink, lineHeight: 1.45 }}>
                {facts.exitCriterion}
              </Typography>
            </Box>
          ) : null}
        </Box>
      ) : null}
    </Box>
  );
}
