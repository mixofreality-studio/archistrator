/* eslint-disable react-refresh/only-export-components -- provider + hook colocated */
/**
 * Delivery channel for CROSS-SLOT reads: an artifact step view joining against
 * ANOTHER committed artifact of the same project head-state (the Mission view's
 * "realized by" reverse join onto the Deployment & Operations objectiveLinks, the
 * Glossary's term-usage joins across Behaviors/Volatilities/Use Cases/System).
 *
 * The StructureFindingsContext idiom: the components layer stays pure (no
 * src/hooks import), the ORCHESTRATORS that already fetch the project head-state
 * (SystemDesignContainer, McpSystemDesignContainer, the HomeBase route — each
 * holding useProject data) mount the provider with the head-state slots, and the
 * views deep inside ArtifactRenderer read them via useCommittedSlotEnvelope.
 *
 * Defensive by construction: with no provider mounted (tests, an unwired shell)
 * or an undefined value (project still loading) consumers get undefined — the
 * joins render nothing instead of crashing. This is also the honest older-state
 * degradation path: a slot that was never committed simply isn't found.
 */
import { createContext, useContext, type ReactNode } from 'react';
import type { ArtifactKindFull, ArtifactModelEnvelope, ArtifactSlotView } from '../contracts/types';

const Ctx = createContext<ArtifactSlotView[] | undefined>(undefined);

export function CommittedSlotsProvider({
  slots,
  children,
}: {
  /** The project head-state slots (useProject data), undefined while loading —
   *  consumers then see no committed envelopes. */
  slots: ArtifactSlotView[] | undefined;
  children: ReactNode;
}): ReactNode {
  return <Ctx.Provider value={slots}>{children}</Ctx.Provider>;
}

/** The committed model envelope of one head-state slot in scope; undefined when
 *  absent (no provider / project loading / slot never committed). The same
 *  `slots.find(kind).model` join the step views already use for the System slot. */
export function useCommittedSlotEnvelope(
  kind: ArtifactKindFull
): ArtifactModelEnvelope | undefined {
  const slots = useContext(Ctx);
  return slots?.find((s) => s.kind === kind)?.model;
}
