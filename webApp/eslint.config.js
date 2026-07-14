// Strict baseline + layered import-boundary gate, from the shared platform package.
// Layers: routes → containers → (components | hooks) → api, with contracts +
// utilities as universal leaves. mcpShell → containers|api directly. `components`
// is pure (no hooks/api) except for a shrinking legacy allowlist — see
// LEGACY_COMPONENTS_HOOKS_FILES in eslint.platform.config.js. See the package README.
import archWeb from './eslint.platform.config.js';

export default archWeb({
  tsconfigRootDir: import.meta.dirname,
  // src/contracts/schema.ts is generated from ../server/api/openapi.yaml — do not lint.
  ignores: ['src/contracts/schema.ts'],
});
