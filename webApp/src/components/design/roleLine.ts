/**
 * Pure copy mapping for the generating scene's "role line" — the honest,
 * real-state replacement for the old wall-clock progress ticker (QA F15).
 *
 * Given the drafting sub-step the server set at its last dispatch boundary
 * (`activeRole` / `activeStep` / `round`, mapped from the wire in wire.ts) plus
 * the artifact's noun phrase (METHOD_METADATA[kind].phrase), it returns the
 * RoleAvatar seed + the sentence to render — or `undefined` when there is no
 * live sub-step (role/step = none, incl. old servers that never set it), which
 * is the caller's signal to fall back to today's plain "DRAFTING…" pill.
 *
 * No timers, no inference: the returned line only restates what the server
 * reported. Kept side-effect-free and React-free so it is unit-testable in
 * isolation (see roleLine.test.ts).
 */
import type { ActiveRole, ActiveStep } from '../../contracts/enums.gen';

export interface RoleLine {
  /** RoleAvatar `seed` — a canonical team role id (see RoleAvatar PROP_FOR). */
  seed: 'system-architect' | 'product-manager';
  /** The honest sentence, e.g. "Architect is crafting the glossary". */
  text: string;
}

const SEED_FOR: Readonly<Record<Exclude<ActiveRole, 'none'>, RoleLine['seed']>> = {
  architect: 'system-architect',
  productManager: 'product-manager',
};

/**
 * @param role   who the server said is working the slot (none → no line)
 * @param step   what they are doing (none → no line)
 * @param round  revision round; the (round N) suffix shows only when > 0 on revising
 * @param phrase the artifact noun phrase, e.g. "vision and mission statement"
 */
export function roleLineFor(
  role: ActiveRole,
  step: ActiveStep,
  round: number,
  phrase: string
): RoleLine | undefined {
  if (role === 'none' || step === 'none') return undefined;
  // Only three (role, step) pairs are legal: architect+drafting, productManager+critiquing,
  // architect+revising. Any other combo (e.g. an old/future server misreporting the pair) is
  // dishonest to render as an avatar/caption — fall back to the plain pill instead.
  if (role === 'architect' && step === 'drafting') {
    return { seed: SEED_FOR.architect, text: `Architect is crafting the ${phrase}` };
  }
  if (role === 'productManager' && step === 'critiquing') {
    return { seed: SEED_FOR.productManager, text: `Product manager is reviewing the ${phrase}` };
  }
  if (role === 'architect' && step === 'revising') {
    return {
      seed: SEED_FOR.architect,
      text:
        round > 0
          ? `Architect is revising the ${phrase} (round ${String(round)})`
          : `Architect is revising the ${phrase}`,
    };
  }
  return undefined;
}
