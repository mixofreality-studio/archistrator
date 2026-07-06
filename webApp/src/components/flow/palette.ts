/**
 * Agentic sub-workflow tool-palette helpers. A palette entry is an internal MCP tool
 * NAME (e.g. "reviewProposeReviews", "projectStateReadProject") — the bounded set a
 * component's agent MAY call at run time. The server's tool catalog carries each
 * tool's target component + read-only flag, but that catalog is not shipped to the
 * SPA; we recover both from the documented naming convention instead:
 *
 *   toolName = <componentPrefix><OpName>   where componentPrefix is the component id
 *   minus its trailing Method-layer word ("access" | "engine" | ...), canonicalized.
 *
 * So we resolve a tool's target by longest-prefix match against the view's actual
 * participants (robust: a valid palette is ⊆ the owner's static deps, which are
 * participants), and read-only-ness from the op verb. Both are best-effort hints that
 * the real catalog will supersede once it reaches the client.
 */

/** Lowercase + strip '-', '_', spaces so kebab / camel / spaced ids all coincide. */
export function canonicalId(s: string): string {
  return s.toLowerCase().replace(/[-_ ]/g, '');
}

// Method-layer suffix words a component id may end with; stripped to get the tool prefix.
const LAYER_SUFFIXES = ['access', 'engine', 'manager', 'client', 'store', 'resource'];

/** The tool-name prefix a component contributes: its canonical id minus a trailing
 *  layer-suffix word (e.g. "review-engine" → "review", "projectStateAccess" →
 *  "projectstate"). */
export function componentToolPrefix(componentId: string): string {
  const c = canonicalId(componentId);
  for (const suf of LAYER_SUFFIXES) {
    if (c.length > suf.length && c.endsWith(suf)) return c.slice(0, c.length - suf.length);
  }
  return c;
}

/** Resolves a palette tool to the participant component it targets, by longest matching
 *  tool-prefix. Returns the participant id, or undefined when no participant matches. */
export function resolvePaletteTarget(
  tool: string,
  participants: readonly { id: string }[]
): string | undefined {
  const ct = canonicalId(tool);
  let bestId: string | undefined;
  let bestLen = 0;
  for (const p of participants) {
    const prefix = componentToolPrefix(p.id);
    if (prefix.length > 0 && ct.startsWith(prefix) && prefix.length > bestLen) {
      bestId = p.id;
      bestLen = prefix.length;
    }
  }
  return bestId;
}

// Op verbs that name a read-only operation (query side). Anything else is treated as
// mutating. Convention-derived; matches the server catalog's ReadOnly flag in practice.
const READ_VERBS = [
  'read',
  'get',
  'list',
  'retrieve',
  'resolve',
  'compute',
  'recompute',
  'propose',
  'derive',
  'load',
  'fetch',
  'find',
  'project',
  'view',
  'describe',
  'plan',
  'estimate',
  'lint',
  'check',
  'validate',
];

/** Whether a palette tool is read-only, from its op verb. `targetId` (when known) lets
 *  us slice off the component prefix so the verb is the first camel-hump of the op. */
export function paletteToolReadOnly(tool: string, targetId: string | undefined): boolean {
  const ct = canonicalId(tool);
  const prefix = targetId !== undefined ? componentToolPrefix(targetId) : '';
  const op = ct.startsWith(prefix) ? ct.slice(prefix.length) : ct;
  return READ_VERBS.some((v) => op.startsWith(v));
}
