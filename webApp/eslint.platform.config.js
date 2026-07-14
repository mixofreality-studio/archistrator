// VENDORED COPY of @mixofreality-studio/archistrator-platform-eslint-config-web@0.1.0
// (archistrator-platform/framework-web-eslint-config/index.js), byte-identical below
// this header. WHY: the package is not yet published to npm (founder npm login
// required), and a file:../../ link cannot resolve in CI or the release Docker
// build. EARMARK: publish the package, swap webApp back to the npm dependency,
// and delete this file. Do NOT edit locally — edit the platform copy and re-vendor.
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

// The six element types and the folders that carry them. mode:'folder' matches a
// file by an ancestor folder path; App.tsx is the app shell, classified as routes.
export const ELEMENTS = [
  { type: 'routes', mode: 'folder', pattern: 'src/routes' },
  { type: 'routes', mode: 'full', pattern: 'src/App.tsx' },
  { type: 'components', mode: 'folder', pattern: 'src/components' },
  { type: 'hooks', mode: 'folder', pattern: 'src/hooks' },
  { type: 'api', mode: 'folder', pattern: 'src/api' },
  { type: 'contracts', mode: 'folder', pattern: 'src/contracts' },
  { type: 'utilities', mode: 'folder', pattern: 'src/utilities' },
];

// The downward-only import DAG (eslint-plugin-boundaries v6 object selectors).
// Sideways (same-type) is allowed within every layer. Only `hooks` may reach `api`
// (the IO client). `contracts` and `utilities` are universal leaves. No upward edges.
export const BOUNDARY_RULES = [
  { from: { type: 'routes' }, allow: { to: { type: ['routes', 'components', 'hooks', 'contracts', 'utilities'] } } },
  { from: { type: 'components' }, allow: { to: { type: ['components', 'hooks', 'contracts', 'utilities'] } } },
  { from: { type: 'hooks' }, allow: { to: { type: ['hooks', 'api', 'contracts', 'utilities'] } } },
  { from: { type: 'api' }, allow: { to: { type: ['api', 'contracts', 'utilities'] } } },
  { from: { type: 'utilities' }, allow: { to: { type: ['utilities', 'contracts'] } } },
  { from: { type: 'contracts' }, allow: { to: { type: ['contracts'] } } },
];

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
        'boundaries/dependencies': ['error', { default: 'disallow', rules: BOUNDARY_RULES }],
        'boundaries/no-unknown': 'error',
        'boundaries/no-unknown-files': 'error',
      },
    },
  );
}
