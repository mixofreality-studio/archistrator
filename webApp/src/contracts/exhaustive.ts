/**
 * Exhaustiveness-checking helper for `switch` statements over discriminated unions
 * (e.g. `ArtifactKindFull`, `ProjectArtifactKind`). Lives in `contracts` — not
 * `utilities` — because the boundary DAG only lets the `contracts` layer import
 * `contracts`, and the `contracts`-layer dispatchers (`adapters.ts`) need it too;
 * `components` may import `contracts`, so this one helper also serves the
 * component-level dispatchers (`ArtifactRenderer.tsx`, `ProjectArtifactRenderer.tsx`).
 *
 * Usage: as the `default` arm of a `switch (kind)` whose cases cover every union
 * member. If a new member is added to the union without a matching `case`, `kind`
 * narrows to something other than `never` at the `default` arm and the call site
 * fails to typecheck — the compiler forces the missing case to be handled.
 */
export function assertNever(x: never): never {
  throw new Error(`Unhandled case: ${JSON.stringify(x)}`);
}
