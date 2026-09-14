/**
 * The Code tab's form (designer check B1) and the op → struct reading it shares
 * with the canvas, pinned without a renderer.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ContractOp } from '../../contracts/types.ts';
import {
  CODE_CANVAS_MIN_WIDTH,
  CODE_EXPANDED_WIDTH,
  CODE_MIN_ZOOM,
  codeCanvasHeightFor,
  codeTabModeFor,
  isErrorStructName,
  opStructsFor,
  paramOf,
  parseSignature,
} from './contractCode.ts';

void test('the pane always lists: no pane width draws the canvas', () => {
  // The pane is 360–820px; even a width past the threshold is not the focus view.
  for (const w of [0, 360, 480, 520, 820, 1200]) {
    assert.equal(codeTabModeFor(w, false), 'list', `pane at ${String(w)}`);
  }
});

void test('the canvas needs the widest expansion at 0.9, plus its gutters and frame (N1)', () => {
  // request 224 + gap 300 + interface 560 + gap 150 + response 224.
  assert.equal(CODE_EXPANDED_WIDTH, 1458);
  // ⌈1458 × 0.9⌉ = 1313, + 2 × 14 gutter + 3 frame.
  assert.equal(CODE_CANVAS_MIN_WIDTH, 1344);
  assert.ok(CODE_CANVAS_MIN_WIDTH >= CODE_EXPANDED_WIDTH * CODE_MIN_ZOOM + 3);
});

void test('the focus view draws the canvas only with room for it', () => {
  assert.equal(codeTabModeFor(0, true), 'list', 'unmeasured lists');
  assert.equal(codeTabModeFor(476, true), 'list', 'the focus column at a 500 window');
  assert.equal(codeTabModeFor(732, true), 'list', 'the focus column at a 1100 window');
  // S2 drew here, and the expanded structs landed 0% / 22% on the canvas.
  assert.equal(codeTabModeFor(912, true), 'list', 'the focus column at a 1280 window');
  assert.equal(codeTabModeFor(998, true), 'list', 'the focus column at a 1366 window');
  assert.equal(codeTabModeFor(1232, true), 'list', 'the focus column at a 1600 window');
  assert.equal(codeTabModeFor(1343, true), 'list');
  assert.equal(codeTabModeFor(1344, true), 'canvas');
  assert.equal(codeTabModeFor(1392, true), 'canvas', 'the focus column at a 1760 window');
});

void test('a primitive or alias param is one row, never a struct with its type as a header', () => {
  assert.deepEqual(paramOf({ name: 'string', fields: [{ name: 'tickID', type: 'string' }] }), {
    name: 'tickID',
    type: 'string',
  });
  assert.equal(
    paramOf({ name: 'fwm.Error', fields: [{ name: 'fault', type: 'fwm.Error' }] })?.name,
    'fault'
  );
  // A real struct — one field of another type, several fields, or none — stays a struct.
  assert.equal(paramOf({ name: 'GetRequest', fields: [{ name: 'id', type: 'ID' }] }), undefined);
  assert.equal(
    paramOf({
      name: 'PumpResult',
      fields: [
        { name: 'dispatched', type: 'bool' },
        { name: 'ActivityID', type: 'ActivityID' },
      ],
    }),
    undefined
  );
  assert.equal(paramOf({ name: 'string', fields: [] }), undefined);
});

void test('a fit never zooms the code diagram below 0.9', () => {
  assert.equal(CODE_MIN_ZOOM, 0.9);
});

void test('the canvas grows to the expansion at 0.9, never below its base', () => {
  // A short drawing keeps the caller's height.
  assert.equal(codeCanvasHeightFor(300, 780), 780);
  // A 10-param request column, ~1400px tall: ⌈1400 × 0.9⌉ + 28 gutter + 3 frame.
  assert.equal(codeCanvasHeightFor(1400, 780), 1291);
});

void test('the signature names the request and the response, error apart', () => {
  const sig = 'ExecuteNextActivity(projectID: ProjectID, tickID: string) → (PumpResult, fwm.Error)';
  assert.deepEqual(parseSignature(sig), {
    inputNames: ['ProjectID', 'string'],
    outputNames: ['PumpResult', 'fwm.Error'],
  });
  const op: ContractOp = { signature: sig, stereotype: '«command»' };
  const structs = opStructsFor(op);
  assert.deepEqual(
    structs.request.map((s) => s.name),
    ['ProjectID', 'string']
  );
  assert.deepEqual(
    structs.response.map((s) => s.name),
    ['PumpResult']
  );
  assert.deepEqual(
    structs.error.map((s) => s.name),
    ['fwm.Error']
  );
  assert.ok(structs.request.every((s) => s._fallback === true && s.fields.length === 0));
});

void test('recorded structs win over the signature, and still split out the errors', () => {
  const op: ContractOp = {
    signature: 'Get(id: ID) → (View, error)',
    stereotype: '«query»',
    inputs: [{ name: 'GetRequest', fields: [{ name: 'id', type: 'ID' }] }],
    outputs: [
      { name: 'View', fields: [{ name: 'name', type: 'string' }] },
      { name: 'NotFoundError', fields: [] },
    ],
  };
  const structs = opStructsFor(op);
  assert.deepEqual(
    structs.request.map((s) => s.name),
    ['GetRequest']
  );
  assert.deepEqual(
    structs.response.map((s) => s.name),
    ['View']
  );
  assert.deepEqual(
    structs.error.map((s) => s.name),
    ['NotFoundError']
  );
  assert.equal(structs.request[0]?._fallback, undefined);
});

void test('error structs are named by convention', () => {
  for (const n of ['error', 'Error', 'fwm.Error', 'NotFoundErr'])
    assert.ok(isErrorStructName(n), n);
  for (const n of ['PumpResult', 'ErrorBudget', 'string']) assert.ok(!isErrorStructName(n), n);
});
