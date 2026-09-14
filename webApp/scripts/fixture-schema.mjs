/**
 * The preview fixtures' JSON Schema, GENERATED from the server OAS
 * (design-renderer-data.md §2′.1, "Sync is enforced mechanically"), and the
 * validator every fixture file must pass.
 *
 * One fixture file is one screen state:
 *
 *   { "route": "/project/archistrator/construction",
 *     "ops": { "<OpId>": { "result": <the op's 200 body> }
 *                      | { "error": { "status": 500, "code": "…", "message": "…" } }
 *                      | { "pending": true } } }
 *
 * The `ops` keys are exactly the OpsClient's OpIds (op-bindings.mjs, the same
 * derivation gen-ops.mjs emits), so a fixture cannot answer an op the transport
 * does not have. Each `result` is checked against that op's OAS 200 response
 * schema (a composition route's against composition-routes.mjs), so a fixture
 * that drifts from the contract fails. The OAS component schemas are carried
 * over as draft-07 `definitions`.
 *
 * Used by: vite.preview.config.ts (validates every fixture when the preview
 * build starts, and emits the schema as dist-preview/fixtures.schema.json) and
 * fixture-schema.test.mjs.
 *
 * EARMARK (design §2′.1, P2): app-generator's webgen emits fixtures.schema.json
 * from the OAS for every generated app; this script is its archistrator-only
 * prototype.
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import yaml from 'js-yaml';
import Ajv from 'ajv';
import { BUNDLE_MARKERS } from './bundle-markers.mjs';
import { COMPOSITION_ROUTES } from './composition-routes.mjs';
import { opBindings } from './op-bindings.mjs';

const here = dirname(fileURLToPath(import.meta.url));

export const OAS_PATH = join(here, '..', '..', 'server', 'api', 'openapi.yaml');

/** archistrator's one UI surface (its slot-5 client with uiSurface). */
export const SURFACE = 'web-client';

/** Screen and state ids are kebab (design §2′.4). */
const KEBAB = /^[a-z0-9]+(-[a-z0-9]+)*$/;

export function loadOas(path = OAS_PATH) {
  return yaml.load(readFileSync(path, 'utf8'));
}

const OAS_REF = '#/components/schemas/';

function draft07Ref(ref) {
  return typeof ref === 'string' && ref.startsWith(OAS_REF)
    ? `#/definitions/${ref.slice(OAS_REF.length)}`
    : ref;
}

/**
 * OAS 3.1 → draft-07: component refs move to `#/definitions/`, and a `$ref`
 * stands ALONE — its sibling keywords are dropped.
 *
 * That is the reading the app itself compiles against: openapi-typescript
 * (src/contracts/schema.ts) ignores a `$ref`'s siblings too. Strict 3.1 would
 * apply them, and this OAS's emitter writes every nullable ref as
 * `anyOf: [{ $ref, type: ["null"] }, { type: "null" }]`, whose first branch would
 * then admit only null (ModelUseCase.activity, for one: schema.ts types it
 * `ModelActivityDiagram | null`, and the live wire carries real diagrams there).
 * Validating fixtures against the stricter reading would reject the real
 * contract; validating against the TS reading keeps one contract, not two.
 *
 * For the same reason an OAS `oneOf` is read as `anyOf`: openapi-typescript
 * renders it as a TS union, which is "any of". Strict "exactly one" rejects real
 * data wherever branches overlap. The artifact-slot model is such a union of 14
 * kinds with no discriminator, and an empty `{ items: [] }` matches four of them
 * (ModelGlossary, ModelScrubbedRequirements, ModelStandardCheck,
 * ModelVolatilities); the retired standardCheck slot carries exactly that.
 */
function rewriteRefs(node) {
  if (Array.isArray(node)) return node.map(rewriteRefs);
  if (node === null || typeof node !== 'object') return node;
  if ('$ref' in node) return { $ref: draft07Ref(node.$ref) };
  const out = {};
  for (const [key, value] of Object.entries(node)) {
    out[key === 'oneOf' ? 'anyOf' : key] = rewriteRefs(value);
  }
  return out;
}

function resultSchema(doc, opId, binding) {
  if (binding.tool === null) return COMPOSITION_ROUTES[opId].result;
  const operation = doc.paths[binding.path][binding.method.toLowerCase()];
  const schema = operation.responses?.['200']?.content?.['application/json']?.schema;
  // A void op (204) has no body; its fixture's `result` is never read, so any value passes.
  return schema === undefined ? {} : rewriteRefs(schema);
}

const ERROR_SCHEMA = {
  type: 'object',
  required: ['status'],
  additionalProperties: false,
  properties: {
    status: { type: 'integer', minimum: 400, maximum: 599 },
    code: { type: 'string' },
    message: { type: 'string' },
  },
};

export function buildFixtureSchema(doc) {
  const ops = {};
  for (const [opId, binding] of Object.entries(opBindings(doc))) {
    ops[opId] = {
      description: `${binding.method} ${binding.path}`,
      oneOf: [
        {
          type: 'object',
          required: ['result'],
          additionalProperties: false,
          properties: { result: resultSchema(doc, opId, binding) },
        },
        {
          type: 'object',
          required: ['error'],
          additionalProperties: false,
          properties: { error: ERROR_SCHEMA },
        },
        {
          type: 'object',
          required: ['pending'],
          additionalProperties: false,
          properties: { pending: { const: true } },
        },
      ],
    };
  }
  return {
    $schema: 'http://json-schema.org/draft-07/schema#',
    title: 'archistrator preview fixture (one screen state)',
    description:
      'Generated from server/api/openapi.yaml by webApp/scripts/fixture-schema.mjs. ' +
      'Each ops key is an OpsClient OpId; each answer is a result, an error, or pending.',
    type: 'object',
    required: ['route', 'ops'],
    additionalProperties: false,
    properties: {
      $schema: { type: 'string' },
      route: { type: 'string', pattern: '^/' },
      title: { type: 'string' },
      note: { type: 'string' },
      ops: { type: 'object', additionalProperties: false, properties: ops },
    },
    definitions: rewriteRefs(doc.components?.schemas ?? {}),
  };
}

/** A compiled ajv validator for one fixture file. */
export function compileFixtureValidator(doc = loadOas()) {
  // OAS formats such as int64 are not JSON Schema formats; ignore unknown ones.
  const ajv = new Ajv({ allErrors: true, jsonPointers: true, unknownFormats: 'ignore' });
  return ajv.compile(buildFixtureSchema(doc));
}

const MAX_ERRORS_PER_FILE = 12;

function describeErrors(errors) {
  const lines = (errors ?? []).map((e) => `${e.dataPath === '' ? '/' : e.dataPath} ${e.message}`);
  const unique = [...new Set(lines)];
  return unique.length > MAX_ERRORS_PER_FILE
    ? [...unique.slice(0, MAX_ERRORS_PER_FILE), `…and ${unique.length - MAX_ERRORS_PER_FILE} more`]
    : unique;
}

/**
 * Validate every fixture under `<root>/<surface>/<screen>/<state>.json`.
 * Returns the files seen and one message per problem (layout, JSON or schema).
 * A missing `<root>/<surface>` is not an error: it is a build with no states.
 */
export function validateFixtureTree(root, { surface = SURFACE, validate } = {}) {
  const files = [];
  const errors = [];
  const dir = join(root, surface);
  if (!existsSync(dir)) return { files, errors };
  const check = validate ?? compileFixtureValidator();
  const rel = (p) => relative(root, p);
  for (const screen of readdirSync(dir).sort()) {
    const screenDir = join(dir, screen);
    if (!statSync(screenDir).isDirectory() || !KEBAB.test(screen)) {
      errors.push(`${rel(screenDir)}: expected a kebab <screen> directory under ${surface}/`);
      continue;
    }
    for (const name of readdirSync(screenDir).sort()) {
      const path = join(screenDir, name);
      const state = name.replace(/\.json$/, '');
      if (!name.endsWith('.json') || !statSync(path).isFile() || !KEBAB.test(state)) {
        errors.push(`${rel(path)}: expected a kebab <state>.json file`);
        continue;
      }
      files.push(path);
      const text = readFileSync(path, 'utf8');
      // Fixtures are bundled into the preview: a bundle marker in their text
      // would satisfy check-prod-bundle's positive control on its own.
      const marker = BUNDLE_MARKERS.find((m) => text.includes(m));
      if (marker !== undefined) {
        errors.push(
          `${rel(path)}: contains the bundle marker "${marker}". Fixture data must not ` +
            '(it would mask the preview bundle check; see scripts/bundle-markers.mjs)'
        );
        continue;
      }
      let data;
      try {
        data = JSON.parse(text);
      } catch (err) {
        errors.push(`${rel(path)}: not JSON (${err.message})`);
        continue;
      }
      if (!check(data)) {
        for (const line of describeErrors(check.errors)) errors.push(`${rel(path)}: ${line}`);
      }
    }
  }
  return { files, errors };
}
