/* eslint-disable react-refresh/only-export-components -- provider + hook colocated */
/**
 * Delivery channel for the Design-Health findings the architecture diagram joins
 * onto itself (findingOverlays) — the CommentContext idiom: the components layer
 * stays pure (no src/hooks import), the ORCHESTRATORS that own the data fetch
 * (SystemDesignContainer, McpSystemDesignContainer, the HomeBase route — each
 * already holding a projectId + useDesignHealth) mount the provider, and the
 * diagram views deep inside ArtifactRenderer read it via useStructureFindings.
 *
 * Defensive by construction: with no provider mounted (tests, an unwired shell)
 * or an undefined value (health still loading / fetch error) consumers get an
 * empty list — the diagram renders overlay-free instead of crashing, and the
 * findings remain visible in the Design Health step.
 */
import { createContext, useContext, type ReactNode } from 'react';
import type { Finding } from '../../contracts/types';

const Ctx = createContext<Finding[] | undefined>(undefined);

export function StructureFindingsProvider({
  findings,
  children,
}: {
  /** The live Design-Health findings (useDesignHealth data), undefined while
   *  loading / on error — consumers then see an empty list. */
  findings: Finding[] | undefined;
  children: ReactNode;
}): ReactNode {
  return <Ctx.Provider value={findings}>{children}</Ctx.Provider>;
}

/** The Design-Health findings in scope; [] when absent (no provider / no data). */
export function useStructureFindings(): Finding[] {
  return useContext(Ctx) ?? [];
}
