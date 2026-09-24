/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { activityEpisodesManager } from './activityEpisodesManager.ts';

void test('the two system-design activities read the systemDesign ledger', () => {
  assert.equal(activityEpisodesManager('requirements'), 'systemDesign');
  assert.equal(activityEpisodesManager('architecture'), 'systemDesign');
});

void test('project design reads its own ledger, not system design’s', () => {
  assert.equal(activityEpisodesManager('projectDesign'), 'projectDesign');
});

void test('every construction type — and an unknown one — reads the construction ledger', () => {
  for (const type of [
    'service',
    'frontend',
    'testing',
    'deployment',
    'documentation',
    'uiDesign',
    'integration',
    'someTypeThisBuildHasNeverHeardOf',
  ]) {
    assert.equal(activityEpisodesManager(type), 'construction', type);
  }
});
