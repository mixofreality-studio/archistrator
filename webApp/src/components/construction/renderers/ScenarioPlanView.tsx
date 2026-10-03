import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { ArtifactRendererProps } from '../artifactRenderers';
import { ScenarioBrowser } from './ScenarioBrowser';

/**
 * The component's scenario plan (spec §2.4, §3, §6.4 "plan lens") — the `stp`
 * and `stpReview` tasks' artifact: every deterministic scenario the use cases
 * project onto this component, each with the binding the test plan committed
 * for it. The scenarios are DERIVED (framework-go/scenario.ForComponent over
 * the committed architecture); the test engineer only binds values, so the
 * reviewer judges bindings against a projection nobody could invent.
 *
 * Read through `testing.scenarios` (the `scenarios` project view), fetched by
 * the container — never from the row.
 */
export function ScenarioPlanView({ t, testing }: ArtifactRendererProps): ReactNode {
  const q = testing?.scenarios;
  if (q === undefined || q.isLoading) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PLAN_VIEW}
        sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}
      >
        Projecting scenarios…
      </Typography>
    );
  }
  if (q.error !== null) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PLAN_VIEW}
        sx={{ color: t.dangerFg, fontSize: 12.5 }}
      >
        {`Could not project this component's scenarios: ${q.error.message}`}
      </Typography>
    );
  }
  const scenarios = q.data ?? [];
  if (scenarios.length === 0) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PLAN_VIEW}
        sx={{ color: t.muted, fontSize: 12.5 }}
      >
        No use-case path sends a stimulus into this component, so it has no scenarios to bind. A
        component nothing reaches is a finding for the architecture, not for the test plan.
      </Typography>
    );
  }

  const bound = scenarios.filter(
    (s) => s.binding !== undefined && s.binding.skip === undefined
  ).length;
  const skipped = scenarios.filter((s) => s.binding?.skip !== undefined).length;
  const unbound = scenarios.length - bound - skipped;
  const counts = [
    `${String(bound)} bound`,
    ...(skipped > 0 ? [`${String(skipped)} skipped`] : []),
    ...(unbound > 0 ? [`${String(unbound)} unbound`] : []),
  ].join(' · ');

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PLAN_VIEW}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 11,
          letterSpacing: '0.06em',
          color: unbound > 0 ? t.awaitingFg : t.ink,
        }}
      >
        {`SCENARIO PLAN · ${String(scenarios.length)} ${scenarios.length === 1 ? 'scenario' : 'scenarios'} · ${counts}`}
      </Typography>
      <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted, lineHeight: 1.4 }}>
        Each scenario is one path through a core use case, projected onto this component: the
        stimuli that reach it, in order, and what must be observable after each. The plan binds
        concrete inputs and an expectation to every stimulus; the generated test replays exactly
        that, and the testing gate passes only when every scenario runs green at the current
        revision.
      </Typography>
      {/* The plan's own browser follows the `sc` deep link (scenarioLink.ts). */}
      <ScenarioBrowser linked component={testing?.component ?? ''} scenarios={scenarios} t={t} />
    </Box>
  );
}
