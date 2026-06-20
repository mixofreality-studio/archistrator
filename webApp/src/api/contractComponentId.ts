/**
 * Resolves a ServiceContract's camelCase component name to the kebab-case
 * component id used in the system-design slot's dynamicViews participants array.
 *
 * Resolution order:
 *   1. Direct camelCase → kebab-case conversion (e.g. "webClient" → "web-client").
 *   2. Case-insensitive match against each component's `.id` (kebab).
 *   3. Case-insensitive match against each component's `.name` converted to kebab.
 *
 * Returns undefined when no component can be matched (honest-empty — no fabrication).
 */
import type { C4Component } from './adapters';

/**
 * Converts a camelCase string to kebab-case.
 * "webClient" → "web-client"
 * "settlementManager" → "settlement-manager"
 * "artifactValidationEngine" → "artifact-validation-engine"
 */
export function camelToKebab(s: string): string {
  return s
    .replace(/([A-Z])/g, (c) => `-${c.toLowerCase()}`)
    .replace(/^-/, '');
}

/**
 * Resolves `contractComponent` (camelCase) to the matching id in `components`.
 * Returns undefined when no match is found.
 */
export function resolveContractComponentId(
  contractComponent: string,
  components: C4Component[]
): string | undefined {
  if (contractComponent.length === 0 || components.length === 0) return undefined;

  // 1. Direct camelCase → kebab conversion.
  const kebab = camelToKebab(contractComponent);
  if (components.some((c) => c.id === kebab)) return kebab;

  // 2. Case-insensitive match on id.
  const lower = kebab.toLowerCase();
  const byId = components.find((c) => c.id.toLowerCase() === lower);
  if (byId !== undefined) return byId.id;

  // 3. Case-insensitive match on name converted to kebab.
  const nameMatch = components.find(
    (c) => camelToKebab(c.name).toLowerCase() === lower
  );
  if (nameMatch !== undefined) return nameMatch.id;

  return undefined;
}
