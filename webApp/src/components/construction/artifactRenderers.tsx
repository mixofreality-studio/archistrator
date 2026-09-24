import type { ReactNode } from 'react';
import type {
  ArtifactModelEnvelope,
  ConstructionRow,
  ProjectStateWithGit,
} from '../../contracts/types';
import type { Tokens } from '../../utilities/theme/themes';
import type { Classification } from './artifactClassification';
import { SystemTestRunView } from './renderers/SystemTestRunView';
import { TestPlanView } from './renderers/TestPlanView';
import { FrontendArtifactView } from './renderers/FrontendArtifactView';

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

export interface ArtifactRendererProps {
  vm: ArtifactActivityVM;
  project?: ProjectStateWithGit | undefined;
  systemEnvelope?: ArtifactModelEnvelope | undefined;
  t: Tokens;
}

/**
 * The classification → renderer registry. A missing entry means "no bespoke
 * renderer yet" — `components/activity/ArtifactPanel.tsx`, via
 * `taskArtifactFor.ts`, falls back to the contract view (`service`) or to an
 * explicit "no renderer" statement. Populated one type at a time (see the
 * per-type plan).
 */
export const artifactRenderers: Partial<
  Record<Classification, (p: ArtifactRendererProps) => ReactNode>
> = {
  'testing:plan': TestPlanView,
  'testing:systemTest': SystemTestRunView,
  frontend: FrontendArtifactView,
  // A uiDesign activity's artifact IS the ui-design concept, which is exactly
  // what FrontendArtifactView renders (its `ui-design` section) — the same
  // renderer, reached by the classification that actually owns that artifact
  // rather than only by the frontend activity that consumes it.
  uiDesign: FrontendArtifactView,
};
