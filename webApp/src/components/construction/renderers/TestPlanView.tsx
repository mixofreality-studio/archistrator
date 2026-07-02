import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Typography from '@mui/material/Typography';
import { UI_IDENTIFIERS } from '../../../constants/UIIdentifiers';
import type { ArtifactRendererProps } from '../artifactRenderers';
import { SequenceFlow } from '../primitives/SequenceFlow';

/**
 * The System Test Plan (N-STP) renderer: the black-box operation-sequence
 * scenarios, one react-flow ladder per core use case. Each step is a
 * transport-agnostic manager operation — the same plan the N-STH generator turns
 * into REST/MCP/gRPC test code. Read from project.testingState.systemTestPlan.
 */
export function TestPlanView({ project, t }: ArtifactRendererProps): ReactNode {
  const scenarios = project?.testingState?.systemTestPlan?.scenarios ?? [];

  if (scenarios.length === 0) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.TEST_PLAN_VIEW}
        sx={{ color: t.muted, fontSize: 12.5 }}
      >
        No system-test scenarios authored yet. The plan enumerates a black-box operation sequence per core
        use case; each appears here as a diagram once N-STP is produced.
      </Typography>
    );
  }

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.TEST_PLAN_VIEW}
      sx={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}
    >
      <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink }}>
        {`SYSTEM TEST PLAN · ${String(scenarios.length)} black-box scenario${scenarios.length === 1 ? '' : 's'}`}
      </Typography>
      <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted, lineHeight: 1.4 }}>
        Each scenario is an ordered sequence of manager operations (transport-agnostic — REST / MCP / gRPC are
        generated bindings). N-STH generates the test code from these; N-IT executes it.
      </Typography>

      {scenarios.map((s) => (
        <Box key={s.id} sx={{ display: 'flex', flexDirection: 'column', gap: 0.75 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
            <Chip
              label={s.useCase}
              size="small"
              sx={{ height: 18, fontSize: 9, bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
            />
            <Typography sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 13, color: t.ink }}>
              {s.title}
            </Typography>
            <Box sx={{ flexGrow: 1 }} />
            <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>
              {`${String((s.steps ?? []).length)} calls`}
            </Typography>
          </Box>
          <SequenceFlow steps={s.steps ?? []} />
        </Box>
      ))}
    </Box>
  );
}
