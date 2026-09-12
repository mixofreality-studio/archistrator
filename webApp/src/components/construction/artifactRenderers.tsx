import type { ReactNode } from 'react';
import type { ArtifactModelEnvelope, ProjectStateWithGit } from '../../contracts/types';
import type { Tokens } from '../../utilities/theme/themes';
import type { ArtifactActivityVM } from './ArtifactActivityList';
import type { Classification } from './artifactClassification';
import { SystemTestRunView } from './renderers/SystemTestRunView';
import { TestPlanView } from './renderers/TestPlanView';
import { FrontendArtifactView } from './renderers/FrontendArtifactView';

export interface ArtifactRendererProps {
  vm: ArtifactActivityVM;
  project?: ProjectStateWithGit | undefined;
  systemEnvelope?: ArtifactModelEnvelope | undefined;
  t: Tokens;
}

/**
 * The classification → renderer registry. A missing entry means "no bespoke
 * renderer yet" — ArtifactActivityDetail falls back to the contract view +
 * honest-pointer cards. Populated one type at a time (see the per-type plan).
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
