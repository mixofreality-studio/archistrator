/// <reference types="node" />
/**
 * Unit tests for constructionRoleLine — the pure copy mapping behind the
 * Construction Tracker's in-flight-activity role line. Run with `npm run test`
 * (Node's built-in test runner over TypeScript via native type stripping; see
 * ../design/roleLine.test.ts for the same convention).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { constructionRoleLine, humanizeWorkerClass } from './constructionRoleLine.ts';

void test('humanizeWorkerClass: kebab slug -> sentence-case display name', () => {
  assert.equal(humanizeWorkerClass('junior-developer'), 'Junior developer');
  assert.equal(humanizeWorkerClass('qa-engineer'), 'Qa engineer');
  assert.equal(humanizeWorkerClass('senior-developer'), 'Senior developer');
});

void test('humanizeWorkerClass: empty slug -> "Someone" (no fabricated role)', () => {
  assert.equal(humanizeWorkerClass(''), 'Someone');
});

void test('requirements phase -> "scoping"', () => {
  assert.deepEqual(
    constructionRoleLine('junior-developer', 'requirements', 'Build the web client'),
    {
      seed: 'junior-developer',
      text: 'Junior developer is scoping Build the web client',
    }
  );
});

void test('detailed_design phase -> "designing"', () => {
  assert.deepEqual(
    constructionRoleLine('senior-developer', 'detailed_design', 'Design the auth service'),
    {
      seed: 'senior-developer',
      text: 'Senior developer is designing Design the auth service',
    }
  );
});

void test('test_plan phase -> "planning tests for"', () => {
  assert.deepEqual(constructionRoleLine('test-engineer', 'test_plan', 'System test harness'), {
    seed: 'test-engineer',
    text: 'Test engineer is planning tests for System test harness',
  });
});

void test('construction phase -> "constructing"', () => {
  assert.deepEqual(
    constructionRoleLine('junior-developer', 'construction', 'Build the web client'),
    {
      seed: 'junior-developer',
      text: 'Junior developer is constructing Build the web client',
    }
  );
});

void test('integration phase -> "integrating"', () => {
  assert.deepEqual(
    constructionRoleLine('junior-developer', 'integration', 'Build the web client'),
    {
      seed: 'junior-developer',
      text: 'Junior developer is integrating Build the web client',
    }
  );
});

void test('undefined phase -> honest fallback that omits the phase verb', () => {
  assert.deepEqual(constructionRoleLine('junior-developer', undefined, 'Build the web client'), {
    seed: 'junior-developer',
    text: 'Junior developer is working on Build the web client',
  });
});

void test('unrecognized phase string -> same honest fallback (never guesses a verb)', () => {
  assert.deepEqual(
    constructionRoleLine('junior-developer', 'some-future-phase', 'Build the web client'),
    {
      seed: 'junior-developer',
      text: 'Junior developer is working on Build the web client',
    }
  );
});
