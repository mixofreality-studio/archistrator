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

/**
 * One `ops` entry as a spec reads it off disk: a result, an error, a pending —
 * or, for `deliveryQueryProjectView` ALONE, one such answer per `ProjectViewKind`.
 *
 * That one op absorbed thirteen per-rail readers in stage 4a, and it answers a
 * different body per kind, so a single answer under its op id could serve only one
 * of the kinds a screen reads. `viewAnswer` / `viewResult` below are how a spec
 * reads it, so no spec has to remember which shape it is looking at.
 */
export interface FixtureAnswer {
  result?: unknown;
  error?: { message?: string };
  pending?: boolean;
  summary?: FixtureAnswer;
  projects?: FixtureAnswer;
  session?: FixtureAnswer;
  pump?: FixtureAnswer;
  designHealth?: FixtureAnswer;
  episodes?: FixtureAnswer;
  timeline?: FixtureAnswer;
}

/** The bits of a fixture file a spec reads to derive what it then asserts. */
export interface FixtureFile {
  route: string;
  ops: Record<string, FixtureAnswer>;
}

/** The one op whose fixture entry is keyed by view kind. */
export const VIEW_OP = 'deliveryQueryProjectView';

/** The seven `ProjectViewKind` selectors the merged read answers. */
export type ViewKind =
  | 'summary'
  | 'projects'
  | 'session'
  | 'pump'
  | 'designHealth'
  | 'episodes'
  | 'timeline';

/** The merged read's answer for one kind, however it answers. */
export function viewAnswer(f: FixtureFile, kind: ViewKind): FixtureAnswer | undefined {
  return f.ops[VIEW_OP]?.[kind];
}

/**
 * The BODY the merged read answers for one kind, unwrapped out of its
 * `DeliveryProjectView` envelope exactly as the SPA unwraps it.
 *
 * It THROWS rather than answering `undefined`: a spec derives its expectations
 * from this, so a fixture that does not carry the kind, or carries it without the
 * envelope, must fail here naming what it wanted — not three assertions later as
 * a count of zero against a count of zero.
 *
 * The member is the kind's own name, which holds for every kind a preview spec
 * reads. `session` is the one kind whose member depends on the SELECTOR
 * (`session` | `projectSession` | `constructionSession`); no preview spec reads it
 * off disk today, and one that needs to should read the member it means.
 */
export function viewResult<T>(f: FixtureFile, kind: ViewKind): T {
  const envelope = viewAnswer(f, kind)?.result as Partial<Record<ViewKind, T>> | undefined;
  const body = envelope?.[kind];
  if (body === undefined) {
    throw new Error(
      `fixture ${f.route}: ops.${VIEW_OP}.${kind}.result.${kind} is missing — ` +
        'the merged read answers a DeliveryProjectView, so the body sits under its own kind',
    );
  }
  return body;
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
