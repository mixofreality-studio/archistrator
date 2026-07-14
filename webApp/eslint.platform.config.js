// VENDORED COPY of @mixofreality-studio/archistrator-platform-eslint-config-web@0.1.0
// (archistrator-platform/framework-web-eslint-config/index.js), byte-identical below
// this header. WHY: the package is not yet published to npm (founder npm login
// required), and a file:../../ link cannot resolve in CI or the release Docker
// build. EARMARK: publish the package, swap webApp back to the npm dependency,
// and delete this file. Do NOT edit locally — edit the platform copy and re-vendor.
// LOCAL DIVERGENCE (2026-07-14, task-6-brief.md): the containers/mcpShell elements,
// the pure-components DAG, and the XSS/fetch bans below have NOT yet been mirrored
// back into archistrator-platform/framework-web-eslint-config. Task 12 earmark:
// port this diff to the platform repo and re-vendor.
// Reusable strict ESLint flat config + layered import-boundary gate for
// archistrator TS web apps. See the archistrator spec
// docs/superpowers/specs/2026-07-04-ts-layer-enforcement-design.md.
//
// Usage (app eslint.config.js):
//   import archWeb from '@mixofreality-studio/archistrator-platform-eslint-config-web';
//   export default archWeb({
//     tsconfigRootDir: import.meta.dirname,
//     ignores: ['src/contracts/schema.ts'], // generated
//   });

import js from '@eslint/js';
import globals from 'globals';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import react from 'eslint-plugin-react';
import jsxA11y from 'eslint-plugin-jsx-a11y';
import boundaries from 'eslint-plugin-boundaries';
import tseslint from 'typescript-eslint';
import eslintConfigPrettier from 'eslint-config-prettier';

// The eight element types and the folders that carry them. mode:'folder' matches a
// file by an ancestor folder path; App.tsx is the app shell, classified as routes.
export const ELEMENTS = [
  { type: 'routes', mode: 'folder', pattern: 'src/routes' },
  { type: 'routes', mode: 'full', pattern: 'src/App.tsx' },
  { type: 'containers', mode: 'folder', pattern: 'src/containers' },
  { type: 'mcpShell', mode: 'folder', pattern: 'src/mcpShell' },
  { type: 'components', mode: 'folder', pattern: 'src/components' },
  { type: 'hooks', mode: 'folder', pattern: 'src/hooks' },
  { type: 'api', mode: 'folder', pattern: 'src/api' },
  { type: 'contracts', mode: 'folder', pattern: 'src/contracts' },
  { type: 'utilities', mode: 'folder', pattern: 'src/utilities' },
];

// The downward-only import DAG (eslint-plugin-boundaries v6 object selectors).
// Sideways (same-type) is allowed within every layer. `contracts` and `utilities`
// are universal leaves. No upward edges.
//
// routes:     ['routes', 'containers', 'components', 'hooks', 'contracts', 'utilities']  // transitional: components/hooks until migration completes (see LEGACY_COMPONENTS_HOOKS_FILES below)
// containers: ['containers', 'components', 'hooks', 'contracts', 'utilities']              // containers orchestrate: may reach both components (render) and hooks (IO)
// mcpShell:   ['mcpShell', 'containers', 'api', 'contracts', 'utilities']                   // the MCP UI host talks to containers + the api layer directly
// components: ['components', 'contracts', 'utilities']                                      // pure — hooks/api REMOVED (Task 6)
// hooks:      ['hooks', 'api', 'contracts', 'utilities']                                     // only hooks (and now mcpShell) may reach api
// api:        ['api', 'contracts', 'utilities']
export const BOUNDARY_RULES = [
  { from: { type: 'routes' }, allow: { to: { type: ['routes', 'containers', 'components', 'hooks', 'contracts', 'utilities'] } } },
  { from: { type: 'containers' }, allow: { to: { type: ['containers', 'components', 'hooks', 'contracts', 'utilities'] } } },
  { from: { type: 'mcpShell' }, allow: { to: { type: ['mcpShell', 'containers', 'api', 'contracts', 'utilities'] } } },
  { from: { type: 'components' }, allow: { to: { type: ['components', 'contracts', 'utilities'] } } },
  { from: { type: 'hooks' }, allow: { to: { type: ['hooks', 'api', 'contracts', 'utilities'] } } },
  { from: { type: 'api' }, allow: { to: { type: ['api', 'contracts', 'utilities'] } } },
  { from: { type: 'utilities' }, allow: { to: { type: ['utilities', 'contracts'] } } },
  { from: { type: 'contracts' }, allow: { to: { type: ['contracts'] } } },
];

// LEGACY: components-layer files that still import hooks directly, pre-dating the
// pure-components rule (Task 6 of docs/superpowers/sdd/task-6-brief.md). This list
// only shrinks — a file leaves it when it stops reaching into src/hooks (typically
// by moving its data-fetching orchestration up into a src/containers/ wrapper).
// Task 8 removes the pilot-screen entries when those screens are containerized.
export const LEGACY_COMPONENTS_HOOKS_FILES = [
  'src/components/AppShell.tsx',
  'src/components/CreateProjectDialog.tsx',
  'src/components/OperationalConceptsView.tsx',
  'src/components/ProjectMenu.tsx',
  'src/components/VolatilityMap.tsx',
  'src/components/construction/PolicyPanel.tsx',
  'src/components/operations/DeploymentsTab.tsx',
  'src/components/operations/ScalingCostTab.tsx',
];

// The pre-Task-6 components allowance, scoped only to the files above via a later,
// more-specific flat-config block (see archWeb() below) — new/migrated components
// files get the strict BOUNDARY_RULES.
const LEGACY_COMPONENTS_RULES = BOUNDARY_RULES.map((rule) =>
  rule.from.type === 'components'
    ? { from: { type: 'components' }, allow: { to: { type: ['components', 'hooks', 'contracts', 'utilities'] } } }
    : rule,
);

// The strict code-quality baseline every archistrator TS app shares, ported verbatim
// from the webApp's original eslint.config.js.
const CUSTOM_RULES = {
  '@typescript-eslint/no-explicit-any': 'error',
  '@typescript-eslint/explicit-function-return-type': 'error',
  '@typescript-eslint/explicit-module-boundary-types': 'error',
  '@typescript-eslint/no-non-null-assertion': 'error',
  '@typescript-eslint/consistent-type-imports': 'error',
  '@typescript-eslint/consistent-type-exports': 'error',
  '@typescript-eslint/no-import-type-side-effects': 'error',
  '@typescript-eslint/strict-boolean-expressions': 'error',
  '@typescript-eslint/switch-exhaustiveness-check': 'error',
  '@typescript-eslint/no-unnecessary-condition': 'error',
  '@typescript-eslint/prefer-nullish-coalescing': 'error',
  '@typescript-eslint/prefer-optional-chain': 'error',
  'react/prop-types': 'off',
  'react/jsx-no-leaked-render': 'error',
  'react/hook-use-state': 'error',
  'react/jsx-curly-brace-presence': ['error', { props: 'never', children: 'never' }],
  'react/self-closing-comp': 'error',
  'react/jsx-sort-props': ['error', { callbacksLast: true, shorthandFirst: true }],
};

// XSS + raw-IO bans (MCP Apps spec §8.2). `react/no-danger` catches the JSX
// `dangerouslySetInnerHTML` prop directly (eslint-plugin-react is already loaded
// above for the rest of the react/* rules) — no-restricted-properties can't see
// into a JSX attribute, so we don't use it here. `no-restricted-globals` bans the
// bare `fetch` identifier everywhere except the api layer itself (see the
// src/api/** override in archWeb() below), so all network IO is forced through
// OpsClient.
const XSS_AND_IO_RULES = {
  'react/no-danger': 'error',
  'no-restricted-imports': ['error', {
    paths: [{ name: 'rehype-raw', message: 'Raw HTML rendering in markdown is banned (MCP Apps XSS control — spec §8.2).' }],
  }],
  'no-restricted-globals': ['error', {
    name: 'fetch',
    message: 'Network IO only via the api layer (OpsClient) — spec §8.2.',
  }],
};

/**
 * Build the flat-config array for an archistrator TS web app.
 * @param {object} [options]
 * @param {string} options.tsconfigRootDir - `import.meta.dirname` of the app.
 * @param {string[]} [options.ignores] - extra global ignore globs (e.g. generated files).
 * @returns {import('typescript-eslint').ConfigArray}
 */
export default function archWeb({ tsconfigRootDir, ignores = [] } = {}) {
  return tseslint.config(
    { ignores: ['dist', ...ignores] },
    {
      files: ['src/**/*.{ts,tsx}'],
      extends: [
        js.configs.recommended,
        ...tseslint.configs.strictTypeChecked,
        ...tseslint.configs.stylisticTypeChecked,
        react.configs.flat.recommended,
        react.configs.flat['jsx-runtime'],
        reactHooks.configs.flat.recommended,
        reactRefresh.configs.vite,
        jsxA11y.flatConfigs.strict,
        eslintConfigPrettier,
      ],
      languageOptions: {
        ecmaVersion: 2022,
        globals: globals.browser,
        parserOptions: {
          projectService: true,
          tsconfigRootDir,
        },
      },
      plugins: { boundaries },
      settings: {
        react: { version: 'detect' },
        'import/resolver': {
          typescript: { alwaysTryTypes: true },
          node: { extensions: ['.ts', '.tsx', '.js', '.jsx'] },
        },
        // Entry/ambient files are not part of any layer.
        'boundaries/ignore': ['src/main.tsx', 'src/vite-env.d.ts'],
        'boundaries/elements': ELEMENTS,
      },
      rules: {
        ...CUSTOM_RULES,
        ...XSS_AND_IO_RULES,
        'boundaries/dependencies': ['error', { default: 'disallow', rules: BOUNDARY_RULES }],
        'boundaries/no-unknown': 'error',
        'boundaries/no-unknown-files': 'error',
      },
    },
    {
      // LEGACY components→hooks allowance, scoped to exactly the burn-down list
      // above. This block is later in the array than the strict block, so its
      // 'boundaries/dependencies' value wins for these files only (flat-config
      // last-match-wins per rule key). Remove entries as they're containerized;
      // delete this whole block once LEGACY_COMPONENTS_HOOKS_FILES is empty.
      files: LEGACY_COMPONENTS_HOOKS_FILES,
      rules: {
        'boundaries/dependencies': ['error', { default: 'disallow', rules: LEGACY_COMPONENTS_RULES }],
      },
    },
    {
      // The api layer is the one place the banned `fetch` global is the point.
      files: ['src/api/**/*.{ts,tsx}'],
      rules: { 'no-restricted-globals': 'off' },
    },
    {
      // LEGACY: the session gate probes GET /api/userinfo to decide whether to
      // render the router at all — it runs above/outside the app's OpsClient
      // context (see App.tsx), so it predates and sits outside the api layer by
      // design, not by omission. EARMARK (not tracked by a numbered task yet):
      // fold this probe into src/api once OpsClient supports a pre-render usage.
      files: ['src/utilities/auth/UserContext.tsx'],
      rules: { 'no-restricted-globals': 'off' },
    },
  );
}
