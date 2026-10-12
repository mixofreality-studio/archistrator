/**
 * The body header shared by BOTH task kinds — what the task is on the left, the
 * revision select on the right, in the same place and with the same look whether
 * you are reading the episode that produced a draft or the review that judged it.
 * A revision is one thing seen from two sides (lifecycleGraphTypes.ts), so the
 * control that moves between revisions must not move around the screen with it.
 *
 * Under the title sits the task's SPEC (TaskSpecPanel): role, command, model,
 * cost, exit criterion, prompt, tools, edit scope and checks — all of it the
 * server's computed AgentTaskSpec, none of it joined in here.
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
import { TaskSpecPanel, type TaskSpecPanelProps } from './TaskSpecPanel';
import type { LifecycleRevision } from './lifecycleGraphTypes.ts';
import { useTokens } from '../../utilities/theme/ThemeContext';

export function TaskHeader({
  title,
  spec,
  revisions,
  revision,
  onRevision,
}: {
  title: string;
  spec: TaskSpecPanelProps;
  revisions: readonly LifecycleRevision[];
  revision: number;
  onRevision: (n: number) => void;
}): ReactNode {
  const t = useTokens();
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

      <TaskSpecPanel {...spec} />
    </Box>
  );
}
