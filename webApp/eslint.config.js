// Strict baseline + layered import-boundary gate, from the shared platform package.
// Layers: routes → components → hooks → api, with contracts + utilities as universal
// leaves. Only hooks may import the IO client (src/api). See the package README.
import archWeb from '@mixofreality-studio/archistrator-platform-eslint-config-web';

export default archWeb({
  tsconfigRootDir: import.meta.dirname,
  // src/contracts/schema.ts is generated from ../server/api/openapi.yaml — do not lint.
  ignores: ['src/contracts/schema.ts'],
});
