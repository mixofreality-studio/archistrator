/// <reference types="node" />
/**
 * THE MCP DESIGN WIDGET'S GATE IS WIRED TO THE RIGHT TASK (stage 4b2 Task 10, from
 * Task 9's review).
 *
 * `liveDesignGate` itself is well held — inverting its `=== 'awaitingHuman'` fails three
 * of its own tests. Its WIRING was not held by anything: swapping
 * `reviewTaskId: gateRef.taskId` for `draftRef.taskId` — pointing the founder's
 * approve/reject gate at the DRAFT task, which never carries an `awaitingHuman`
 * revision — left `npm run check` at 1223/1223 with typecheck and lint clean. So the
 * gate this wave re-opened could be shut again by a rename in `lifecycles.gen.ts` or a
 * copy-paste slip, with CI green: exactly the defect Task 9 existed to fix, re-openable
 * with no red light.
 *
 * WHY THIS READS THE SOURCE: `npm test` is `node --test 'src/**\/*.test.ts'` over a
 * harness with no DOM and no JSX loader, so there is no way to mount this container at
 * all. The house idiom for a claim about a container's wiring is the source scan —
 * `ActivityExperienceContainer.test.ts`, `hooks/statusDecides.test.ts`,
 * `api/opsSeam.test.ts`. Task 9 used that idiom for ONE of the two containers it
 * touched; this closes the pair. And there is no other net: `mcpShell` is not booted by
 * `previewShell`, so the MCP widget has NO preview surface — unit tests and the
 * compiler are the whole of its coverage.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const container = readFileSync(new URL('./McpSystemDesignContainer.tsx', import.meta.url), 'utf8');

void test('the live gate reads the REVIEW task’s revision and the DRAFT task’s, each on its own ref', () => {
  // The two refs are not interchangeable and the compiler cannot tell them apart —
  // both are `{ activityId, taskId }` strings off the same table.
  assert.match(container, /reviewTaskId:\s*gateRef\.taskId/);
  assert.match(container, /dispatchTaskId:\s*draftRef\.taskId/);
  // And they come from the two inverters, not from each other.
  assert.match(container, /const draftRef = dispatchRefFor\(activeKind\)/);
  assert.match(container, /const gateRef = reviewRefFor\(activeKind\)/);
});

void test('the activity view is read for the GATE’s activity, once, and only when there is one', () => {
  // A second read of the same activity view would double a poll that already runs at
  // the gate cadence on a widget with no preview surface to notice it.
  assert.equal((container.match(/useActivityView\(/g) ?? []).length, 1);
  assert.match(
    container,
    /useActivityView\(\s*projectId,\s*gateRef\.activityId,\s*projectId\.length > 0 && gateRef\.activityId\.length > 0\s*\)/
  );
});

void test('all three facts the screen takes come from that ONE gate, not from the derived session door', () => {
  // The derived door's vocabulary is {unknown, committed, withdrawn, draftFailed}; none
  // of these three can be read off it, and reading any of them from `stage` is how the
  // gate was shut for a whole wave.
  assert.match(container, /awaitingHuman=\{gate\.awaitingHuman\}/);
  assert.match(container, /dispatchRunning=\{gate\.dispatchRunning\}/);
  assert.match(container, /failedRunUrl=\{gate\.failedRunUrl\}/);
});
