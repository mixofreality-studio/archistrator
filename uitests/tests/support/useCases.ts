/**
 * tagUseCase(id) — annotate a spec as exercising a specific Method core use
 * case (the `.coreUseCases` slot in project.json / see the-method-core-use-
 * cases). Two purposes, both served by the SAME call:
 *
 *   1. RUNTIME — pushes a real Playwright test annotation (visible in the
 *      HTML report / trace viewer) so a human reading a run can see which
 *      core use case a given test maps to.
 *   2. STATIC — tests/meta/use-case-coverage.spec.ts does NOT run this
 *      function; it greps the spec SOURCE for literal `tagUseCase('<id>')`
 *      call sites (see its glob+regex scan) to build the "which core use
 *      cases have >=1 tagged UI spec" coverage matrix. This is why `id` MUST
 *      be a string LITERAL at the call site, not a computed/interpolated
 *      value — the static scan cannot evaluate expressions.
 *
 * Call it once per test (or in a `describe`'s `beforeEach`, which tags every
 * test in that block) — as close to the top of the test as practical, right
 * after/alongside any other setup.
 */
import { test } from '@playwright/test';

/** The Playwright annotation `type` used for use-case tags. */
export const USE_CASE_ANNOTATION_TYPE = 'use-case';

export function tagUseCase(id: string): void {
  test.info().annotations.push({ type: USE_CASE_ANNOTATION_TYPE, description: id });
}
