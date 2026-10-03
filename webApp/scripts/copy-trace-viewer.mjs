/**
 * copy-trace-viewer — self-host the Playwright trace viewer (spec §6.4).
 *
 * A UI scenario's run records a Playwright trace. The results panel opens it in
 * the trace viewer that ships inside `playwright-core`
 * (`lib/vite/traceViewer`, a static Vite build), copied here into
 * `public/trace-viewer/` so Vite serves it at `/trace-viewer/index.html` from
 * our own origin: the viewer then fetches the trace from the file endpoint with
 * the edge's session cookie, and nothing leaves the deployment. (trace.playwright.dev
 * would need the bytes handed to a third party, and the SPA refuses to be framed,
 * so the viewer opens in a new tab rather than an iframe.)
 *
 * Runs on `postinstall`; the copy is gitignored and rebuilt from the pinned
 * `playwright-core` on every install, so the viewer version is the lockfile's.
 */
import { cpSync, existsSync, mkdirSync, rmSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const webApp = join(here, '..');
const source = join(webApp, 'node_modules', 'playwright-core', 'lib', 'vite', 'traceViewer');
const target = join(webApp, 'public', 'trace-viewer');

if (!existsSync(join(source, 'index.html'))) {
  console.error(
    `copy-trace-viewer: ${source} has no index.html — is playwright-core installed? ` +
      '(the results panel\'s "Open trace" needs it; see spec §6.4)'
  );
  process.exit(1);
}

rmSync(target, { recursive: true, force: true });
mkdirSync(dirname(target), { recursive: true });
cpSync(source, target, { recursive: true });
console.log(`copy-trace-viewer: ${source} → public/trace-viewer/`);
