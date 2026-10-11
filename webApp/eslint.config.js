// Strict baseline + layered import-boundary gate, from the shared platform package.
// Layers: routes → containers → (components | hooks) → api, with contracts +
// utilities as universal leaves. mcpShell → containers|api directly. `components`
// is pure (no hooks/api) except for a shrinking legacy allowlist — see
// LEGACY_COMPONENTS_HOOKS_FILES in eslint.platform.config.js. See the package README.
import archWeb from './eslint.platform.config.js';

export default [
  ...archWeb({
    tsconfigRootDir: import.meta.dirname,
    // src/contracts/schema.ts is generated from ../server/api/openapi.yaml — do not lint.
    // dist-preview/ is the preview build's output (vite.preview.config.ts).
    ignores: ['src/contracts/schema.ts', 'dist-preview'],
  }),
  {
    // A missing hook dependency is a stale-closure bug, not a style nit: the
    // recommended preset only WARNS, and the lint gate passes on warnings, so a
    // mutant that dropped a memo's dependency got past it (integration review,
    // mutant K11; fix I). The app had no such warning when this was raised.
    files: ['src/**/*.{ts,tsx}'],
    rules: { 'react-hooks/exhaustive-deps': 'error' },
  },
  {
    // Scenario tests only (deterministic component testing, spec T2): the
    // webApp is tested by the scenario specs generated from its committed test
    // plan (uitests/generated/webClient), so a hand-written test file is refused.
    files: ['src/**/*.test.{ts,tsx}'],
    rules: {
      'no-restricted-syntax': [
        'error',
        {
          selector: 'Program',
          message:
            'scenario tests only; see docs/superpowers/specs/2026-10-02-deterministic-component-testing-design.md (T2). Hand-written test files are not added; the generated scenario specs replace them.',
        },
      ],
    },
  },
];
