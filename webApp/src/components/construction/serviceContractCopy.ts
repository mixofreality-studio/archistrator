/**
 * ServiceContractView's copy, pure so it is tested without a renderer.
 */

/**
 * The empty-facets line. It said "(Client layer)" for every contract, which
 * mislabelled any engine with no facets; the layer is named only when it is one.
 */
export function facetsEmptyCopy(layer: string): string {
  return layer === 'Client'
    ? 'This contract declares no data/error/idempotency facets (Client layer).'
    : 'This contract declares no data/error/idempotency facets.';
}
