/**
 * Pure (JSX-free, xyflow-free) cues for the architecture diagram family, kept in
 * a leaf module so they are unit-testable under `node --test`
 * (architectureCues.test.ts — the volatilityMapLogic pattern).
 */
import type { Layer } from '../../contracts/types';

/** Tooltip / accessible copy for the no-volatility warning badge on a C4 node. */
export const NO_VOLATILITY_WARNING =
  'Encapsulates no identified volatility — functional decomposition smell (Righting Software ch. 2)';

/** The layers the Method REQUIRES to encapsulate a volatility. Clients (
 *  deployment/technology volatility), Resources (storage) and Utilities
 *  (mechanisms) legitimately carry none. */
const VOLATILITY_BEARING_LAYERS: readonly Layer[] = ['manager', 'engine', 'resourceAccess'];

/**
 * True when a Manager/Engine/ResourceAccess component encapsulates NO
 * volatility: the typed `encapsulatesVolatilities` names are empty AND (for
 * states predating the typed field) the `encapsulates` prose is blank. Such a
 * component exists for what it DOES rather than what it hides — the
 * anti-functional-decomposition smell the diagram flags quietly.
 */
export function componentLacksVolatility(c: {
  layer: Layer;
  encapsulates: string;
  encapsulatesVolatilities: readonly string[];
}): boolean {
  if (!VOLATILITY_BEARING_LAYERS.includes(c.layer)) return false;
  return c.encapsulatesVolatilities.length === 0 && c.encapsulates.trim() === '';
}
