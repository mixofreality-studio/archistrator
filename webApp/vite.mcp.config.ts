import { fileURLToPath } from 'node:url';
import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';

// The MCP App build. Emits a CLASSIC IIFE bundle (single self-contained chunk) plus
// one CSS file, both with FIXED, hash-free names, into the SAME `dist/` the SPA
// build publishes. The Go server serves a tiny stub HTML that <script src>'s
// `mcp-app.js` + <link>'s `mcp-app.css`; a classic (non-module) script keeps that
// tag CORS-exempt inside the host's sandboxed iframe (spec §3.4).
//
// WHY lib mode (not an HTML entry, as the task sketch showed): a Vite HTML-entry
// build with `format:'iife'` INLINES CSS into the JS (verified: no mcp-app.css is
// emitted) and injects the entry as `<script type="module">`. Library mode with
// `cssCodeSplit:false` extracts a real `mcp-app.css`, and the small plugin below
// emits a classic-script `mcp-app.html` stub — so all three gate invariants hold:
// IIFE js, a separate css file, and no module script in the built html. The source
// `mcp-app.html` at the repo root remains the `vite dev` entry.

/** Emit a classic-script stub HTML (Vite lib mode processes no HTML of its own). */
function emitMcpStubHtml(): Plugin {
  return {
    name: 'emit-mcp-stub-html',
    generateBundle() {
      this.emitFile({
        type: 'asset',
        fileName: 'mcp-app.html',
        source: `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>archistrator · MCP</title>
    <link rel="stylesheet" href="/mcp-app.css" />
  </head>
  <body>
    <div id="root"></div>
    <script src="/mcp-app.js"></script>
  </body>
</html>
`,
      });
    },
  };
}

export default defineConfig({
  plugins: [react(), emitMcpStubHtml()],
  // Lib mode does NOT define process.env.NODE_ENV the way app builds do (library
  // consumers usually handle it); React references it, so an undefined `process`
  // crashes the iframe at boot (T11 finding F-T11-2). Inline it here.
  define: {
    'process.env.NODE_ENV': JSON.stringify('production'),
  },
  build: {
    outDir: 'dist',
    emptyOutDir: false,
    cssCodeSplit: false,
    lib: {
      entry: fileURLToPath(new URL('./src/mcpShell/main.tsx', import.meta.url)),
      formats: ['iife'],
      name: 'ArchistratorMcpApp',
      fileName: () => 'mcp-app.js',
      cssFileName: 'mcp-app',
    },
    rollupOptions: {
      output: {
        inlineDynamicImports: true,
        assetFileNames: (asset) =>
          asset.name?.endsWith('.css') === true ? 'mcp-app.css' : 'mcp-assets/[name][extname]',
      },
    },
  },
});
