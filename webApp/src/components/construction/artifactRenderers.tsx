import type { ReactNode } from 'react';
import type {
  ArtifactModelEnvelope,
  ConstructionRow,
  ProjectStateWithGit,
  ScenarioView,
  TestRunPanelView,
} from '../../contracts/types';
import type { Tokens } from '../../utilities/theme/themes';
import type { Classification } from './artifactClassification';
import { FrontendArtifactView } from './renderers/FrontendArtifactView';
import { ScenarioPlanView } from './renderers/ScenarioPlanView';
import { TestRunPanel } from './renderers/TestRunPanel';

/**
 * A view-model row joining a ConstructionRow with the activity-list display
 * name — moved here from the retired ArtifactActivityList.tsx, its only
 * remaining consumer after the Artifacts tab's own list/detail rendering was
 * superseded. The surface that builds this row is
 * `components/activity/ArtifactPanel.tsx` now (stage 5 §7.4 deleted the detail
 * pane's ArtifactBody, which used to).
 */
export interface ArtifactActivityVM {
  activityId: string;
  name: string;
  row: ConstructionRow;
}

/** A query's state as a pure renderer takes it — the container owns the hook. */
export interface AsyncArtifactData<T> {
  data: T | undefined;
  error: Error | null;
  isLoading: boolean;
}

/**
 * What the testing lifecycle's two renderers read (spec §6.4), fetched by the
 * container (`hooks/useTestRun.ts`) and handed down as data: `components/` is
 * pure and may not reach `hooks/`.
 */
export interface TestingArtifactData {
  /** The component the activity builds — the projection's subject and the anchor's `testPlan[<comp>]`. */
  component: string;
  scenarios: AsyncArtifactData<ScenarioView[]>;
  testRun: AsyncArtifactData<TestRunPanelView>;
  /** The run's `go test -json` streams by `outputRef`, for the Output tab. */
  outputs: Record<string, AsyncArtifactData<string>>;
}

export interface ArtifactRendererProps {
  vm: ArtifactActivityVM;
  project?: ProjectStateWithGit | undefined;
  systemEnvelope?: ArtifactModelEnvelope | undefined;
  t: Tokens;
  /**
   * The lifecycle phase the task under review belongs to. A `service` or
   * `frontend` activity's artifact depends on it: the scenario plan on
   * `test_plan`, the test run on `integration`, the built UI on `construction`.
   */
  phase?: string | undefined;
  testing?: TestingArtifactData | undefined;
}

type Renderer = (p: ArtifactRendererProps) => ReactNode;

/**
 * A component activity's artifact, by the phase under review. The service
 * contract (`detailed_design`) never reaches here — `taskArtifactFor` places it
 * through the contract JOIN — and `taskArtifactFor.ARTIFACT_PHASES` admits only
 * the phases listed, so a phase outside them is unreachable rather than drawn
 * as an empty frame.
 */
function byPhase(construction: Renderer | undefined): Renderer {
  const ByPhase = (p: ArtifactRendererProps): ReactNode => {
    switch (p.phase) {
      case 'test_plan':
        return <ScenarioPlanView {...p} />;
      case 'integration':
        return <TestRunPanel {...p} />;
      case 'construction':
        return construction?.(p) ?? null;
      case undefined:
      default:
        return null;
    }
  };
  return ByPhase;
}

/**
 * The classification → renderer registry. A missing entry means "no bespoke
 * renderer yet" — `components/activity/ArtifactPanel.tsx`, via
 * `taskArtifactFor.ts`, falls back to the contract view (`service`) or to an
 * explicit "no renderer" statement. Populated one type at a time (see the
 * per-type plan).
 *
 * The system test plan (`testing:plan`) and system testing (`testing:systemTest`)
 * rows are gone with those activities (deterministic component testing, spec §7): every component's
 * own lifecycle renders its scenario plan and its run now.
 */
export const artifactRenderers: Partial<Record<Classification, Renderer>> = {
  service: byPhase(undefined),
  frontend: byPhase(FrontendArtifactView),
  // A uiDesign activity's artifact IS the ui-design concept, which is exactly
  // what FrontendArtifactView renders (its `ui-design` section) — the same
  // renderer, reached by the classification that actually owns that artifact
  // rather than only by the frontend activity that consumes it.
  uiDesign: FrontendArtifactView,
};
