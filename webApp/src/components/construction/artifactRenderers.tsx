import type { ReactNode } from 'react';
import type { ArtifactModelEnvelope, ProjectStateWithGit } from '../../api/types';
import type { Tokens } from '../../theme/themes';
import type { ArtifactActivityVM } from './ArtifactActivityList';
import type { Classification } from './artifactClassification';
import { SystemTestView } from './renderers/SystemTestView';

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
  'testing:systemTest': SystemTestView,
};
