/**
 * The four things every preview spec does, in one place.
 *
 * A preview boots the REAL app over a fixture file (`?screen=<id>&state=<id>`,
 * see tests/preview/preview-shell.spec.ts's header): it answers from fixtures,
 * so nothing but the static bundle may be fetched, and an op no fixture answers
 * is a LOUD miss — the alarm plus an entry in the page's incident log. Every
 * preview assertion is therefore only as good as `incidents(page)` being empty
 * beside it: without that, a screen that rendered half a state over a missing
 * op reads as a pass.
 *
 * These were private to preview-shell.spec until the stage-5 suites needed the
 * same four (Task 12). One definition, so "clean" means the same thing in all
 * three files.
 */
import { readFileSync } from 'node:fs';
import type { Page, Request } from '@playwright/test';

const FIXTURES = new URL('../../preview-fixtures/web-client/', import.meta.url);

/** The bits of a fixture file a spec reads to derive what it then asserts. */
export interface FixtureFile {
  route: string;
  ops: Record<string, { result?: unknown; error?: { message?: string } }>;
}

/** The fixture the preview will boot for `?screen=&state=`, read off disk. */
export function fixture(screen: string, state: string): FixtureFile {
  return JSON.parse(
    readFileSync(new URL(`${screen}/${state}.json`, FIXTURES), 'utf8')
  ) as FixtureFile;
}

export interface Incident {
  kind: 'fixture-miss' | 'network-blocked' | 'navigation-blocked';
  detail: string;
}

/** The preview's own incident log. `null` when the page is not a preview at all. */
export function incidents(page: Page): Promise<Incident[] | null> {
  return page.evaluate(
    () =>
      (
        window as unknown as {
          __ARCHISTRATOR_PREVIEW__?: { incidents: Incident[] };
        }
      ).__ARCHISTRATOR_PREVIEW__?.incidents ?? null
  );
}

/**
 * Every request the page makes that is not the static bundle itself. A preview
 * answers from fixtures, so this must stay empty.
 */
export function watchNetwork(page: Page): string[] {
  const offBundle: string[] = [];
  page.on('request', (req: Request) => {
    const url = new URL(req.url());
    const isBundle =
      url.pathname === '/index.html' ||
      url.pathname.startsWith('/assets/') ||
      url.protocol === 'data:';
    if (!isBundle) offBundle.push(`${req.method()} ${req.url()}`);
  });
  return offBundle;
}

/** Open one fixture state, watching the network from before the navigation. */
export async function openState(page: Page, screen: string, state: string): Promise<string[]> {
  const offBundle = watchNetwork(page);
  await page.goto(`/index.html?screen=${screen}&state=${state}`);
  return offBundle;
}
