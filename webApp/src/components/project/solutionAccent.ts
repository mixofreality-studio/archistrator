/**
 * Presentation helper: a stable accent colour per solution option, shared by every
 * Phase-2 renderer. Lives in the components layer (not in a data adapter) because it
 * maps a domain kind onto theme tokens — presentation, not data shaping.
 */
import type { Tokens } from '../../utilities/theme/themes';
import type { ProjectArtifactKind } from '../../contracts/types';

export function solutionAccentColor(t: Tokens, kind: ProjectArtifactKind): string {
  if (kind === 'decompressedSolution') return t.committedDot;
  if (kind === 'compressedSolution') return t.accent;
  if (kind === 'normalSolution') return t.accent2;
  return t.muted;
}
