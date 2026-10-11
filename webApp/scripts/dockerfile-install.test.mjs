/**
 * The production image runs `npm ci` from a layer that holds only the
 * manifests (so the dependency layer caches across source edits). `npm ci`
 * runs the root install lifecycle hooks, so every local script a hook names
 * must already be COPY'd into that layer, or the image build fails at
 * `npm ci` (release.yml builds and pushes the webapp image from this file).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const webApp = join(dirname(fileURLToPath(import.meta.url)), '..');
const pkg = JSON.parse(readFileSync(join(webApp, 'package.json'), 'utf8'));
const dockerfile = readFileSync(join(webApp, 'Dockerfile'), 'utf8').split('\n');
const dockerignore = readFileSync(join(webApp, '.dockerignore'), 'utf8')
  .split('\n')
  .map((l) => l.trim())
  .filter((l) => l && !l.startsWith('#'));

const INSTALL_HOOKS = ['preinstall', 'install', 'postinstall', 'prepare'];

/** Local files the root install hooks execute, e.g. `scripts/copy-trace-viewer.mjs`. */
function installHookFiles() {
  const files = [];
  for (const hook of INSTALL_HOOKS) {
    const cmd = pkg.scripts?.[hook];
    if (!cmd) continue;
    for (const m of cmd.matchAll(/(?:^|\s)((?:\.\/)?scripts\/[\w./-]+)/g)) {
      files.push(m[1].replace(/^\.\//, ''));
    }
  }
  return files;
}

/** COPY sources (relative to the context) that precede the first `npm ci`. */
function sourcesCopiedBeforeInstall() {
  const sources = [];
  for (const line of dockerfile) {
    const t = line.trim();
    if (/^RUN\s+npm\s+ci\b/.test(t)) return { sources, foundInstall: true };
    const copy = t.match(/^COPY\s+(?!--from)(.+)$/);
    if (copy) {
      const parts = copy[1].split(/\s+/).filter((p) => !p.startsWith('--'));
      sources.push(...parts.slice(0, -1).map((p) => p.replace(/^\.\//, '').replace(/\/$/, '')));
    }
  }
  return { sources, foundInstall: false };
}

test('every script an install hook runs is in the image before `npm ci`', () => {
  const { sources, foundInstall } = sourcesCopiedBeforeInstall();
  assert.ok(foundInstall, 'Dockerfile has no `RUN npm ci` step');
  const missing = installHookFiles().filter(
    (f) => !sources.some((s) => s === '.' || s === f || f.startsWith(`${s}/`))
  );
  assert.deepEqual(missing, [], `COPY these before \`RUN npm ci\`: ${missing.join(', ')}`);
});

test('the build context excludes the local trace-viewer copy, so the image keeps the one its own install made', () => {
  assert.ok(
    dockerignore.some((l) => l.replace(/^\/|\/$/g, '') === 'public/trace-viewer'),
    '.dockerignore must list public/trace-viewer'
  );
});
