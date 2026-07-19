/**
 * The single dispatcher that renders any typed ArtifactModelEnvelope by its string
 * `kind`, choosing the right client-side renderer:
 *
 *   glossary      → GlossaryView      (search + category filter, toGlossaryView)
 *   volatilities  → VolatilityMap     (two axis lanes, toVolatilityView)
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
import { toMarkdown } from '../contracts/adapters';
import { assertNever } from '../contracts/exhaustive';
import type { ArtifactModelEnvelope, ServiceContracts } from '../contracts/types';
import { Prose } from './Prose';
import { GlossaryView } from './GlossaryView';
import { MissionView } from './MissionView';
import { ScrubbedRequirementsView } from './ScrubbedRequirementsView';
import { StandardCheckView } from './StandardCheckView';
import { VolatilityMap } from './VolatilityMap';
import { ArchitectureView } from './flow/ArchitectureView';
import { OperationalConceptsView } from './OperationalConceptsView';
import { UseCaseCarousel } from './usecase/UseCaseCarousel';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

export function ArtifactRenderer({
  envelope,
  title,
  height,
  serviceContracts,
  useCasesEnvelope,
}: {
  envelope: ArtifactModelEnvelope | undefined;
  /** Human label used as the prose comment source / fallback. */
  title?: string;
  /** Optional diagram canvas height override (experience uses taller canvases). */
  height?: number;
  /** When present, the Architecture view drills into established component contracts. */
  serviceContracts?: ServiceContracts | undefined;
  /** The committed coreUseCases envelope, when available: lets the Architecture
   *  view label blank-titled dynamic views by their linked use case (F-QA2-51). */
  useCasesEnvelope?: ArtifactModelEnvelope | undefined;
}): ReactNode {
  return (
    <Box
      data-artifact-kind={envelope?.kind}
      data-testid={UI_IDENTIFIERS.DesignExperience.ARTIFACT_RENDER}
    >
      {renderBody(envelope, title, height, serviceContracts, useCasesEnvelope)}
    </Box>
  );
}

function renderBody(
  envelope: ArtifactModelEnvelope | undefined,
  title: string | undefined,
  height: number | undefined,
  serviceContracts: ServiceContracts | undefined,
  useCasesEnvelope: ArtifactModelEnvelope | undefined
): ReactNode {
  const kind = envelope?.kind;
  switch (kind) {
    case 'mission':
      return <MissionView envelope={envelope} />;
    case 'glossary':
      return <GlossaryView envelope={envelope} {...(height !== undefined ? { height } : {})} />;
    case 'scrubbedRequirements':
      return <ScrubbedRequirementsView envelope={envelope} />;
    case 'standardCheck':
      return <StandardCheckView envelope={envelope} />;
    case 'volatilities':
      return <VolatilityMap envelope={envelope} />;
    case 'system':
      return (
        <ArchitectureView
          envelope={envelope}
          useCasesEnvelope={useCasesEnvelope}
          {...(height !== undefined ? { height } : {})}
          {...(serviceContracts !== undefined ? { serviceContracts } : {})}
        />
      );
    case 'coreUseCases':
      return <UseCaseCarousel envelope={envelope} />;
    case 'operationalConcepts':
      return (
        <OperationalConceptsView envelope={envelope} {...(height !== undefined ? { height } : {})} />
      );
    // Every Phase-2 kind (planningAssumptions / activityList / network / the 4
    // solution kinds / riskModel / sdpReview), plus an absent envelope/kind,
    // project to markdown via toMarkdown — same as the prior fall-through default.
    case 'planningAssumptions':
    case 'activityList':
    case 'network':
    case 'normalSolution':
    case 'decompressedSolution':
    case 'subcriticalSolution':
    case 'compressedSolution':
    case 'riskModel':
    case 'sdpReview':
    case undefined: {
      const markdown = toMarkdown(envelope);
      return (
        <Prose
          markdown={markdown.length > 0 ? markdown : '_No content yet._'}
          source={title ?? kind}
          {...(kind !== undefined ? { artifactKind: kind } : {})}
        />
      );
    }
    default:
      return assertNever(kind);
  }
}
