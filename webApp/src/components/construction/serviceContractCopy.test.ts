/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { facetsEmptyCopy } from './serviceContractCopy.ts';

void test('the empty-facets line names the Client layer only for a client contract', () => {
  assert.match(facetsEmptyCopy('Client'), /\(Client layer\)\.$/);
  for (const layer of ['Engine', 'Manager', 'ResourceAccess', 'Utility', '']) {
    assert.doesNotMatch(facetsEmptyCopy(layer), /Client/);
  }
});
