/**
 * The production bundle must never ship the preview's fixture transport or its
 * network guard (design-renderer-data.md §2′.5 item 3). The browser SPA and the
 * MCP app are built into dist/ (the nginx image and the Go localdist embed ship
 * exactly that); the preview is built into dist-preview/, which neither ships.
 *
 *   node scripts/check-prod-bundle.mjs            dist/ contains NO marker (run by `build`)
 *   node scripts/check-prod-bundle.mjs --preview  dist-preview/ contains EVERY marker
 *                                                 (run by `build:preview`)
 *
 * A symbol name would not do: the minifier renames `fixtureOpsClient`. The
 * markers are string literals the minifier keeps, one in each preview-only
 * module (src/api/fixtureOps.ts and src/previewShell/networkGuard.ts). The
 * --preview run is the POSITIVE CONTROL: it proves the markers survive
 * minification, so the production run cannot pass just because it looks for
 * something that never appears in any bundle.
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, extname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const webApp = join(here, '..');

export const MARKERS = [
  // The fixture transport's miss error (src/api/fixtureOps.ts sets this.name).
  'FixtureMissError',
  // The preview network guard's message prefix (src/previewShell/networkGuard.ts).
  'archistrator-preview-network-guard',
];

const TEXT = new Set(['.js', '.mjs', '.cjs', '.html', '.css', '.json', '.map', '.txt']);

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });
}

function fail(message) {
  console.error(`check-prod-bundle: FAIL — ${message}`);
  process.exit(1);
}

const preview = process.argv.includes('--preview');
const dir = join(webApp, preview ? 'dist-preview' : 'dist');
if (!existsSync(dir)) fail(`${relative(webApp, dir)}/ does not exist; build it first`);

const files = walk(dir);
if (!files.some((f) => f.endsWith('index.html'))) {
  fail(`${relative(webApp, dir)}/ has no index.html; this is not a finished build`);
}
if (!files.some((f) => extname(f) === '.js')) {
  fail(`${relative(webApp, dir)}/ has no JavaScript; this is not a finished build`);
}

const hits = new Map(MARKERS.map((m) => [m, []]));
for (const file of files) {
  if (!TEXT.has(extname(file))) continue;
  const text = readFileSync(file, 'utf8');
  for (const marker of MARKERS) {
    if (text.includes(marker)) hits.get(marker).push(relative(webApp, file));
  }
}

if (preview) {
  const missing = MARKERS.filter((m) => hits.get(m).length === 0);
  if (missing.length > 0) {
    fail(
      `the preview bundle lacks ${missing.join(', ')}. The production check greps for these ` +
        'markers, so it would pass vacuously; keep them in the preview-only modules.'
    );
  }
  console.log(`check-prod-bundle: ok — dist-preview/ carries every marker (${MARKERS.join(', ')})`);
} else {
  const shipped = MARKERS.filter((m) => hits.get(m).length > 0);
  if (shipped.length > 0) {
    fail(
      'the production bundle ships preview-only code:\n' +
        shipped.map((m) => `  ${m} in ${hits.get(m).join(', ')}`).join('\n') +
        '\nOnly src/previewShell/ may import src/api/fixtureOps.ts or the network guard.'
    );
  }
  console.log(`check-prod-bundle: ok — dist/ (${files.length} files) carries no preview marker`);
}
