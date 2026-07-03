import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { UI_IDENTIFIERS } from '../../../constants/UIIdentifiers';
import type { ArtifactRendererProps } from '../artifactRenderers';
import { ScenarioBrowser } from './ScenarioBrowser';

/**
 * The System Test Plan (N-STP) renderer: the black-box operation-sequence
 * scenarios, one per core use case. A selector picks a scenario; only that one
 * renders as a sequence diagram (a Test harness lifeline sequences every call —
 * managers never call each other). At plan time every call is a red target that
 * N-IT must prove green. Read from project.testingState.systemTestPlan.
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
        use case; each appears here as a sequence diagram once N-STP is produced.
      </Typography>
    );
  }

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.TEST_PLAN_VIEW}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}
    >
      <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink }}>
        {`SYSTEM TEST PLAN · ${String(scenarios.length)} black-box scenarios`}
      </Typography>
      <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted, lineHeight: 1.4 }}>
        A test harness sequences each call along the use-case call chain — the managers never call each
        other. Every step is a transport-agnostic manager operation (REST / MCP / gRPC are generated
        bindings). At plan time every call is a red target; N-STH generates the test code, and N-IT runs it
        against the real build to turn them green.
      </Typography>
      <ScenarioBrowser mode="plan" scenarios={scenarios} t={t} />
    </Box>
  );
}
