/**
 * The single dispatcher that renders any typed ArtifactModelEnvelope by its string
 * `kind`, choosing the right client-side renderer:
 *
 *   volatilities  → VolatilityMap     (two-axis scatter, toVolatilityView)
 *   system        → ArchitectureFlow  (xyflow C4 layered view, toC4View)
 *   coreUseCases  → UseCaseCarousel   (xyflow activity diagrams, toCoreUseCasesView)
 *   prose kinds   → Prose(toMarkdown(...))  (react-markdown)
 *
 * Used in BOTH the System Design experience (rendering the live candidate `draft`)
 * and the home base's ArtifactPane (rendering committed slots). Resilient to an
 * absent envelope/model — each underlying adapter returns a safe empty view, and
 * the prose branch shows a placeholder. The wrapping element carries the
 * `artifact-render` testid so tests can assert a renderer mounted.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import { toMarkdown } from '../api/adapters';
import type { ArtifactModelEnvelope, ServiceContracts } from '../api/types';
import { Prose } from './Prose';
import { VolatilityMap } from './VolatilityMap';
import { ArchitectureView } from './flow/ArchitectureView';
import { OperationalConceptsView } from './OperationalConceptsView';
import { UseCaseCarousel } from './usecase/UseCaseCarousel';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

export function ArtifactRenderer({
  envelope,
  title,
  height,
  serviceContracts,
}: {
  envelope: ArtifactModelEnvelope | undefined;
  /** Human label used as the prose comment source / fallback. */
  title?: string;
  /** Optional diagram canvas height override (experience uses taller canvases). */
  height?: number;
  /** When present, the Architecture view drills into established component contracts. */
  serviceContracts?: ServiceContracts | undefined;
}): ReactNode {
  return (
    <Box
      data-artifact-kind={envelope?.kind}
      data-testid={UI_IDENTIFIERS.DesignExperience.ARTIFACT_RENDER}
    >
      {renderBody(envelope, title, height, serviceContracts)}
    </Box>
  );
}

function renderBody(
  envelope: ArtifactModelEnvelope | undefined,
  title: string | undefined,
  height: number | undefined,
  serviceContracts: ServiceContracts | undefined
): ReactNode {
  const kind = envelope?.kind;
  if (kind === 'volatilities') return <VolatilityMap envelope={envelope} />;
  if (kind === 'system') {
    return (
      <ArchitectureView
        envelope={envelope}
        {...(height !== undefined ? { height } : {})}
        {...(serviceContracts !== undefined ? { serviceContracts } : {})}
      />
    );
  }
  if (kind === 'coreUseCases') return <UseCaseCarousel envelope={envelope} />;
  if (kind === 'operationalConcepts') {
    return (
      <OperationalConceptsView envelope={envelope} {...(height !== undefined ? { height } : {})} />
    );
  }

  // Prose kinds (mission / glossary / scrubbedRequirements / operationalConcepts /
  // standardCheck) and any Phase-2 kind project to markdown via toMarkdown.
  const markdown = toMarkdown(envelope);
  return (
    <Prose
      markdown={markdown.length > 0 ? markdown : '_No content yet._'}
      source={title ?? kind}
      {...(kind !== undefined ? { artifactKind: kind } : {})}
    />
  );
}
