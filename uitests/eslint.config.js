// @ts-check
import js from '@eslint/js';
import globals from 'globals';
import tseslint from 'typescript-eslint';
import { defineConfig, globalIgnores } from 'eslint/config';

/**
 * Selector-discipline ESLint config for the archistrator UI-test harness.
 *
 * This package is black-box by construction (see README "What makes it
 * un-cheatable"): every assertion must select DOM elements only by published
 * `data-testid` (`page.getByTestId(TESTID.x)`) or accessible role/label
 * (`getByRole` / `getByLabel`) — never a raw CSS selector, and never a
 * hand-typed testid string that bypasses the single `TESTID` map. Two
 * `no-restricted-syntax` rules enforce that mechanically (not just by review):
 *
 *   1. "no locator() escape hatch" — Playwright's `.locator(<css|xpath>)`
 *      accepts an arbitrary selector and is exactly the un-cheatable-ness
 *      hole this harness must not use for element selection. It is banned
 *      outright. The handful of cases that legitimately need it — a
 *      structural assertion with no testid/role to select by, e.g. counting
 *      react-flow's generated DOM nodes/edges — get an explicit
 *      `eslint-disable-next-line no-restricted-syntax -- <reason>` at the
 *      call site rather than a blanket carve-out here.
 *
 *   2. "no inline testid string" — bans a bare kebab-case string literal
 *      passed straight to `getByTestId(...)`. testid strings must come from
 *      the single published `TESTID` map in tests/support/testids.ts — that
 *      file is the ONE allowed source, so a renamed testid fails one place
 *      instead of silently drifting at N call sites. testids.ts itself is
 *      exempted below (it IS that source).
 */
const NO_LOCATOR = {
  selector: "CallExpression[callee.property.name='locator']",
  message:
    'Do not select elements with .locator(<css|xpath>) — use page.getByTestId(TESTID.x), ' +
    'getByRole(...), or getByLabel(...) instead. If this is a genuine structural assertion ' +
    'with no testid/role to select by (e.g. counting react-flow DOM nodes), justify the escape ' +
    'with `eslint-disable-next-line no-restricted-syntax -- <reason>` rather than removing this rule.',
};

const NO_INLINE_TESTID = {
  selector:
    "CallExpression[callee.property.name='getByTestId'] > Literal[value=/^[a-z][a-z0-9]*(-[a-z0-9]+)*$/]",
  message:
    'Do not hand-type a data-testid string — import it from TESTID in tests/support/testids.ts ' +
    '(the single allowed source of testid strings) so a rename fails one place, not every call site.',
};

export default defineConfig([
  globalIgnores([
    'node_modules',
    'test-results',
    'playwright-report',
    'blob-report',
    'testdata',
  ]),

  {
    files: ['**/*.ts'],
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      globals: globals.node,
    },
    rules: {
      'no-restricted-syntax': ['error', NO_LOCATOR, NO_INLINE_TESTID],
    },
  },

  // tests/support/testids.ts IS the canonical source of testid strings (and issues
  // no Playwright selector calls at all) — exempt it from both rules above.
  {
    files: ['tests/support/testids.ts'],
    rules: {
      'no-restricted-syntax': 'off',
    },
  },
]);
