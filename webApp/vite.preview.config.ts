import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import {
  SURFACE,
  buildFixtureSchema,
  loadOas,
  validateFixtureTree,
} from './scripts/fixture-schema.mjs';

// The PREVIEW build (design-renderer-data.md §2′.1): the same <App/> as the SPA,
// booted by src/previewShell/main.tsx over the fixture transport, with
// ?screen=<id>&state=<id> choosing the fixture. A sibling of vite.mcp.config.ts.
//
//   - `base: './'`, so the bundle can be served from any origin root or path prefix.
//   - outDir `dist-preview/`, NEVER `dist/`: the nginx image and the Go localdist
//     embed ship dist/ only, and `npm run build` asserts dist/ carries no preview
//     code (scripts/check-prod-bundle.mjs).
//   - Fixtures are bundled at build time (import.meta.glob through the
//     `@preview-fixtures` alias), so a preview needs no fetch. The root is
//     ARCHISTRATOR_PREVIEW_FIXTURES (relative to webApp/), defaulting to
//     preview/fixtures, where the U-SPA-web-client Design phase will record the
//     designed fixtures (§2′.5 item 4). The uitests build it over their own
//     test-local fixtures: ARCHISTRATOR_PREVIEW_FIXTURES=../uitests/preview-fixtures.
//   - Every fixture is validated against the OAS-generated schema when the build
//     starts; one invalid fixture fails the build. The schema ships beside the
//     bundle as fixtures.schema.json.
//
// Serve a build with `npx vite preview -c vite.preview.config.ts`. `vite dev` over
// this config is not a supported target: the preview's network guard refuses the
// Vite client's HMR WebSocket, as it refuses every other connection.

const here = dirname(fileURLToPath(import.meta.url));
const fixturesRoot = resolve(
  here,
  process.env['ARCHISTRATOR_PREVIEW_FIXTURES'] ?? 'preview/fixtures'
);

// The response headers the future Go preview listener sets (§2′.3), for `vite
// preview`. frame-ancestors is 'none' here; the listener will name archistrator's
// public origin instead.
const PREVIEW_CSP = [
  "default-src 'self'",
  "script-src 'self'",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data: blob:",
  "font-src 'self' data:",
  "connect-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
  "object-src 'none'",
  "frame-src 'none'",
  "worker-src 'none'",
  "frame-ancestors 'none'",
].join('; ');

function previewFixtures(): Plugin {
  return {
    name: 'archistrator-preview-fixtures',
    apply: 'build',
    // After Vite's own HTML plugin, which emits preview.html in its generateBundle;
    // a normal-order plugin runs first and would find nothing to rename.
    enforce: 'post',
    buildStart() {
      const { files, errors } = validateFixtureTree(fixturesRoot);
      if (errors.length > 0) {
        this.error(`preview fixtures fail the OAS-generated schema:\n  ${errors.join('\n  ')}`);
      }
      this.info(`${String(files.length)} fixture state(s) from ${fixturesRoot}/${SURFACE}`);
    },
    generateBundle(_options, bundle) {
      // The entry is preview.html at the source root; the bundle serves it as index.html.
      const html = bundle['preview.html'];
      if (html?.type === 'asset') {
        delete bundle['preview.html'];
        this.emitFile({ type: 'asset', fileName: 'index.html', source: html.source });
      }
      this.emitFile({
        type: 'asset',
        fileName: 'fixtures.schema.json',
        source: `${JSON.stringify(buildFixtureSchema(loadOas()), null, 2)}\n`,
      });
    },
  };
}

export default defineConfig({
  base: './',
  plugins: [react(), previewFixtures()],
  resolve: { alias: { '@preview-fixtures': fixturesRoot } },
  build: {
    outDir: 'dist-preview',
    emptyOutDir: true,
    rollupOptions: { input: resolve(here, 'preview.html') },
  },
  preview: {
    headers: {
      'Content-Security-Policy': PREVIEW_CSP,
      'Referrer-Policy': 'no-referrer',
      'X-Content-Type-Options': 'nosniff',
      'Cross-Origin-Resource-Policy': 'same-origin',
    },
  },
});
