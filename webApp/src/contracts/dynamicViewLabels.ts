/**
 * Pure label logic for the Architecture Dynamic-view picker (F-QA2-51): a drafted
 * DynamicView can carry a blank title, which used to render blank MUI Select
 * options. Every picker label now resolves through the fallback chain
 *
 *   title → linked use case's name (useCaseId) → positional "Untitled view N"
 *
 * Kept import-free of adapters (type-only imports) so it is directly unit-testable
 * under node --test (adapters.ts' extensionless imports don't resolve there).
 */
import type { CoreUseCases } from './types';

/**
 * use-case id → name index from the (narrowed) committed CoreUseCases model.
 * Empty map when the model is absent — the label fallback then skips straight to
 * the positional placeholder.
 */
export function indexUseCaseNames(model: CoreUseCases | undefined): Map<string, string> {
  const index = new Map<string, string>();
  for (const d of model?.decisions ?? []) {
    if (d.useCase.name.trim().length > 0) index.set(d.useCase.id, d.useCase.name.trim());
  }
  return index;
}

/**
 * Display-label fallback chain for one dynamic view. `index` is the view's
 * position in the model's FULL dynamicViews order (1-based in the placeholder),
 * so the placeholder stays stable across per-component filtering.
 */
export function dynamicViewLabel(
  view: { title: string; useCaseId: string },
  index: number,
  nameById: Map<string, string>
): string {
  const title = view.title.trim();
  if (title.length > 0) return title;
  const ucName = nameById.get(view.useCaseId);
  if (ucName !== undefined) return ucName;
  return `Untitled view ${String(index + 1)}`;
}
