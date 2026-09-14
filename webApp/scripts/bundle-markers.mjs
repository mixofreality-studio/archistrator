/**
 * The string markers that identify preview-only code in a bundle
 * (check-prod-bundle.mjs). Each is a string literal in exactly one preview-only
 * module, and string literals survive minification.
 *
 * Fixture DATA must never contain one; fixture-schema.mjs refuses such a
 * fixture. Fixtures are bundled into the preview, so a marker in a fixture's
 * text would satisfy the preview's positive control even after the code
 * carrying it was gone. Mutation testing found exactly that (preview P1, M2c):
 * a fixture note naming FixtureMissError kept the control green with the class
 * renamed away.
 */
export const BUNDLE_MARKERS = [
  // The fixture transport's miss error (src/api/fixtureOps.ts sets this.name).
  'FixtureMissError',
  // The preview network guard's message prefix (src/previewShell/networkGuard.ts).
  'archistrator-preview-network-guard',
];
