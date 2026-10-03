/**
 * gen-enums.mjs — the ordinal table of a SPARSE int enum is indexed by the wire
 * VALUE, never by position.
 *
 * `TestingVariant` retired ordinals 0 and 3 (deterministic component testing,
 * spec §7) and became `[1, 2, 4]`. The generator used to alias the ordinal table
 * to the varname array, so `ORDINAL_TO_GO_VARNAME[1]` read 'TestVariantPerf' for
 * a Harness row — every testing activity's variant was off by one. The table
 * carries `undefined` holes now, and a dense enum's table is unchanged.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { generateEnumsModule } from './gen-enums.mjs';

const oas = `
openapi: 3.1.0
components:
  schemas:
    DeliveryTestingVariant:
      type: integer
      enum: [1, 2, 4]
      x-enum-varnames: [TestVariantHarness, TestVariantPerf, TestVariantQAProcess]
    DeliveryArtifactKind:
      type: integer
      enum: [0, 1]
      x-enum-varnames: [KindMission, KindGlossary]
`;

void test('a sparse int enum emits a value-indexed ordinal table with holes', () => {
  const out = generateEnumsModule(oas);
  const line = out
    .split('\n')
    .find((l) => l.startsWith('export const TESTING_VARIANT_ORDINAL_TO_GO_VARNAME'));
  assert.ok(line !== undefined, 'the table is emitted');
  assert.match(
    line,
    /= \[undefined, 'TestVariantHarness', 'TestVariantPerf', undefined, 'TestVariantQAProcess'\]/
  );
  assert.match(line, /readonly \(TestingVariantGoVarname \| undefined\)\[\]/);
});

void test('a dense int enum still aliases the varname array', () => {
  const out = generateEnumsModule(oas);
  const line = out
    .split('\n')
    .find((l) => l.startsWith('export const ARTIFACT_KIND_ORDINAL_TO_GO_VARNAME'));
  assert.ok(line !== undefined);
  assert.match(line, /= ARTIFACT_KIND_GO_VARNAMES;$/);
});
