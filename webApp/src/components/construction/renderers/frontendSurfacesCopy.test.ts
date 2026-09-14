/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { NO_SURFACES_LABEL, noSurfacesSentence } from './frontendSurfacesCopy.ts';

void test('NO SURFACES RECORDED names the client (polish 8)', () => {
  assert.equal(NO_SURFACES_LABEL, 'NO SURFACES RECORDED');
  assert.equal(
    noSurfacesSentence('web-client'),
    "web-client's UI design records no surfaces, so there is nothing to preview. Surfaces are recorded with the UI design; the console does not guess routes."
  );
  assert.match(noSurfacesSentence(undefined), /^This client's UI design records no surfaces/);
});
