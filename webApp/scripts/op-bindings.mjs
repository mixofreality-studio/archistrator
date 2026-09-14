/**
 * The ONE derivation of the OpsClient's op table from the server OAS, shared by
 * gen-ops.mjs (which emits it as OP_BINDINGS in src/api/ops.gen.ts) and
 * fixture-schema.mjs (which keys the preview fixtures' JSON Schema by it). One
 * implementation, so a fixture can never name an op the transport cannot bind.
 *
 * Every server OAS path has the mechanical shape
 * `/api/v1/<mgr>/<op>[/{param}...]`; opId = camel(mgr) + Pascal(op), which is
 * also the server's MCP tool name. The hand-declared composition routes
 * (composition-routes.mjs) are bound beside them with `tool: null`.
 */
import { COMPOSITION_ROUTES } from './composition-routes.mjs';

// openapi-fetch's client exposes one method per HTTP verb (GET/POST/PUT/PATCH/
// DELETE); the server OAS currently only uses GET and POST, but all five are
// listed here so a future PUT/PATCH/DELETE endpoint does not need this
// generator to change, only regeneration.
const HTTP_METHODS = ['get', 'post', 'put', 'patch', 'delete'];

function words(kebab) {
  return kebab.split('-').filter((w) => w.length > 0);
}

function pascalCase(kebab) {
  return words(kebab)
    .map((w) => w[0].toUpperCase() + w.slice(1))
    .join('');
}

function camelCase(kebab) {
  const p = pascalCase(kebab);
  return p.length > 0 ? p[0].toLowerCase() + p.slice(1) : p;
}

function sortedByOpId(bindings) {
  return Object.fromEntries(Object.entries(bindings).sort(([a], [b]) => a.localeCompare(b)));
}

/**
 * Derive the {method, path, tool} binding for every operation in the OAS.
 * opId = camel(mgr) + Pascal(op); duplicate opIds (a derivation collision)
 * abort the build rather than silently overwriting one binding with another.
 */
export function deriveBindings(doc) {
  const paths = doc.paths ?? {};
  const bindings = {};
  for (const [pathTemplate, methods] of Object.entries(paths)) {
    const segments = pathTemplate.split('/').filter((s) => s.length > 0);
    // segments: ['api', 'v1', '<mgr>', '<op>', ...optional '{param}' segments]
    if (segments.length < 4 || segments[0] !== 'api' || segments[1] !== 'v1') {
      throw new Error(
        `gen-ops: path does not match the /api/v1/<mgr>/<op>[/{...}] shape: ${pathTemplate}`
      );
    }
    const [, , mgr, op] = segments;
    const opId = camelCase(mgr) + pascalCase(op);
    for (const method of HTTP_METHODS) {
      if (!(method in methods)) continue;
      if (opId in bindings) {
        throw new Error(
          `gen-ops: duplicate derived opId "${opId}" (from ${pathTemplate} ${method.toUpperCase()}) — ` +
            'mgr/op derivation collided with an earlier binding'
        );
      }
      bindings[opId] = { method: method.toUpperCase(), path: pathTemplate, tool: opId };
    }
  }
  // Sort by opId so the emitted table's order is stable across regenerations
  // regardless of the OAS's own key order.
  return sortedByOpId(bindings);
}

/**
 * Bind the hand-declared composition routes beside the OAS ops. `tool: null`
 * marks a route with no MCP tool. A composition opId that collides with a
 * derived one aborts the build, as a derived collision does.
 */
export function withCompositionRoutes(bindings) {
  const merged = { ...bindings };
  for (const [opId, route] of Object.entries(COMPOSITION_ROUTES)) {
    if (opId in merged) {
      throw new Error(`gen-ops: composition route "${opId}" collides with a derived OAS binding`);
    }
    merged[opId] = { method: route.method, path: route.path, tool: null };
  }
  return sortedByOpId(merged);
}

/** Every binding the OpsClient carries: the OAS ops plus the composition routes. */
export function opBindings(doc) {
  return withCompositionRoutes(deriveBindings(doc));
}
