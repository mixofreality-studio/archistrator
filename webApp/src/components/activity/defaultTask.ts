/**
 * Which task the Activity Experience opens on when the URL names none
 * (spec §7.2): what needs YOU first, then what broke, then what is moving,
 * then the last thing that finished, then the first task of the lifecycle.
 *
 * Promoted out of the prototype's `defaultNodeId`, where it was an inline
 * `find` chain over fixture data.
 */
import type { LifecycleNode } from './lifecycleGraphTypes.ts';

export function defaultTaskId(nodes: readonly LifecycleNode[]): string | undefined {
  if (nodes.length === 0) return undefined;
  const first = (state: LifecycleNode['state']): LifecycleNode | undefined =>
    nodes.find((n) => n.state === state);
  const lastPassed = [...nodes].reverse().find((n) => n.state === 'done');
  return (
    first('awaitingHuman')?.id ??
    first('failed')?.id ??
    first('running')?.id ??
    lastPassed?.id ??
    nodes[0]?.id
  );
}

/**
 * The task the URL actually selects: the one it names when the activity has
 * it, else the default. A stale deep link opens the activity rather than an
 * empty body.
 */
export function selectedTaskId(
  nodes: readonly LifecycleNode[],
  requested: string | undefined
): string | undefined {
  if (requested !== undefined && nodes.some((n) => n.id === requested)) return requested;
  return defaultTaskId(nodes);
}
