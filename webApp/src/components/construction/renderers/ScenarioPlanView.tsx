import { useMemo, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { ArtifactRendererProps } from '../artifactRenderers';
import { scenarioModels } from '../../../contracts/adapters';
import { ScenarioBrowser } from './ScenarioBrowser';
import { planKeyFor, type ScenarioContext } from './scenarioPresentation';
import { scenarioPlanState } from './scenarioSelection.ts';

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
export function ScenarioPlanView({
  t,
  testing,
  project,
  systemEnvelope,
  useCasesEnvelope,
}: ArtifactRendererProps): ReactNode {
  const q = testing?.scenarios;
  // What each scenario is read against: the use cases (name, trigger, actors,
  // diagram), the system (real callers, layers, call chain), the contracts (params).
  const context: ScenarioContext = useMemo(
    () => ({
      ...scenarioModels(systemEnvelope, useCasesEnvelope),
      contracts: project?.serviceContracts,
    }),
    [systemEnvelope, useCasesEnvelope, project?.serviceContracts]
  );
  const state = q === undefined ? 'loading' : scenarioPlanState(q);
  if (q === undefined || state === 'loading') {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PLAN_VIEW}
        sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted }}
      >
        Projecting scenarios…
      </Typography>
    );
  }
  if (state === 'error') {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PLAN_VIEW}
        sx={{ color: t.dangerFg, fontSize: 12.5 }}
      >
        {`Could not project this component's scenarios: ${q.error?.message ?? ''}`}
      </Typography>
    );
  }
  if (state === 'unread') {
    // The container never fired the read: the activity names no component, so
    // there is nothing to project onto. Not the same as a projection of nothing.
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PLAN_VIEW}
        sx={{ color: t.muted, fontSize: 12.5 }}
      >
        This activity names no component, so no scenarios were projected. The scenario plan is a
        component&apos;s: the activity&apos;s componentId must resolve before it can be read.
      </Typography>
    );
  }
  const scenarios = q.data ?? [];
  if (state === 'empty') {
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
      <Typography sx={{ fontFamily: t.body, fontSize: 13, color: t.muted, lineHeight: 1.5 }}>
        Each scenario follows one path through a core use case and lists every call that path makes
        into this component (a <b>stimulus</b>), with the real inputs and the result expected. For
        each state that must hold afterwards, the plan names how the test reads it back — its{' '}
        <b>proof</b>. A scenario is <b>bound</b> once all of that is filled in. The generated test
        replays exactly this, and the testing gate passes only when every scenario passes at the
        current revision.
      </Typography>
      {/* The plan's own browser follows the `sc` deep link (scenarioLink.ts). */}
      <ScenarioBrowser
        linked
        component={planKeyFor(project?.serviceContracts, testing?.component ?? '')}
        context={context}
        scenarios={scenarios}
        t={t}
      />
    </Box>
  );
}
