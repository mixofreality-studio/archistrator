/// <reference types="node" />
/**
 * THE ACTIVITY EXPERIENCE MOUNTS THE THREE CROSS-SLOT PROVIDERS (stage 4b2 Task 9).
 *
 * The Activity Experience renders `ArtifactRenderer`, and `ArtifactRenderer`'s views
 * read three CROSS-SLOT contexts. None of the three was mounted here: both existing
 * mount points are the HomeBase route and the stage-6 MCP widget container — the two
 * screens this experience replaced. Every consumer degrades to undefined SILENTLY by
 * design (`CommittedSlotsContext.tsx`: "the joins render nothing instead of
 * crashing"), which is why a COMMITTED `operationalConcepts` slot — three
 * environments, five infrastructure entries, sixteen bindings — rendered as a
 * DISABLED Deployment toggle for a whole wave.
 *
 * WHY THIS READS THE SOURCE rather than mounting the component: `npm test` is
 * `node --test 'src/**\/*.test.ts'` over a harness with no DOM and no JSX loader —
 * there is no renderer in this repo's node suite at all (the house idiom for a claim
 * about a container's wiring is the source scan; see `hooks/statusDecides.test.ts`,
 * `api/opsSeam.test.ts`). The BEHAVIOURAL proof that the lens comes back is the
 * preview pair in `uitests/tests/preview/activity-renderers.spec.ts`, which drives a
 * real browser over a fixture whose `operationalConcepts` slot is committed.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const container = readFileSync(
  new URL('./ActivityExperienceContainer.tsx', import.meta.url),
  'utf8'
);
/** The mount point whose nesting this one copies — two agreeing opinions, not three. */
const mcpContainer = readFileSync(
  new URL('./McpSystemDesignContainer.tsx', import.meta.url),
  'utf8'
);

/** The order the three providers nest in, outermost first, in one source file. */
function nesting(source: string): string[] {
  return [
    ...source.matchAll(
      /<(StructureFindingsProvider|DeploymentHealthProvider|CommittedSlotsProvider)\b/g
    ),
  ].flatMap((m) => (m[1] === undefined ? [] : [m[1]]));
}

void test('mounts CommittedSlotsProvider so the Deployment lens is available on a committed operationalConcepts slot', () => {
  // ArchitectureView reads useCommittedSlotEnvelope('operationalConcepts') and
  // disables the Deployment toggle when the envelope is undefined. With no provider
  // the envelope is ALWAYS undefined, committed slot or not.
  assert.match(container, /<CommittedSlotsProvider slots=\{project\?\.slots\}>/);
});

void test('mounts StructureFindingsProvider so the architecture diagram carries its design-health tint', () => {
  assert.match(container, /<StructureFindingsProvider findings=\{designHealth\?\.findings\}>/);
  // Fed by the same hook both prior mount points call — not a second copy of the rules.
  assert.match(container, /const \{ data: designHealth \} = useDesignHealth\(projectId\);/);
});

void test('mounts DeploymentHealthProvider so the deployment lens can tint an observed node', () => {
  assert.match(container, /<DeploymentHealthProvider healthByKey=\{deploymentHealth\}>/);
  // Dormant by default, exactly as at the other two mounts: no operations capability
  // and no derived operated-app id means an absent overlay, never a red diagram.
  assert.match(
    container,
    /useDeploymentHealth\(\s*operatedAppId \?\? '',\s*operationsEnabled\(useCapabilities\(\)\)\s*\)/
  );
});

void test('feeds the providers the SAME project head-state the container already reads', () => {
  // A provider that fetched its own copy of the head-state would be a SECOND read of
  // a 1.17 MB aggregate on every render of a screen that polls every two seconds.
  assert.equal((container.match(/useProject\(/g) ?? []).length, 1, 'head-state read once');
  assert.equal((container.match(/useDesignHealth\(/g) ?? []).length, 1, 'design health read once');
  assert.equal(
    (container.match(/useDeploymentHealth\(/g) ?? []).length,
    1,
    'deployment health read once'
  );
  // And the provider is handed that ONE read's value, not a re-derivation: `slots`
  // (the `project?.slots ?? []` the bodies take) would hide a loading head-state
  // behind an empty array, which reads as "no slot was ever committed".
  assert.match(container, /<CommittedSlotsProvider slots=\{project\?\.slots\}>/);
});

void test('nests the three in the SAME order as the mount point it copies, outside the history CommentProvider', () => {
  assert.deepEqual(nesting(container), nesting(mcpContainer));
  // A read-only revision still gets its lenses: only the comment affordances are
  // suppressed in the past, so the three wrap the disabled CommentProvider, not the
  // other way round.
  const providerAt = container.indexOf('<StructureFindingsProvider');
  const historyAt = container.indexOf('<CommentProvider enabled={false}>');
  assert.ok(providerAt > 0 && historyAt > 0);
  assert.ok(
    providerAt > historyAt,
    'the history CommentProvider is built first and wrapped by the three'
  );
  assert.match(container, /<CommittedSlotsProvider slots=\{project\?\.slots\}>\{commentScoped\}</);
});
