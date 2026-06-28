/**
 * gen:api — generate src/api/schema.ts from the server's OpenAPI document.
 *
 * The server OAS is merged from per-manager surfaces (system-design,
 * project-design, construction, operations, project). Those surfaces reuse the
 * SAME operationId across managers (e.g. GetSessionState exists on both the
 * construction and system-design surfaces; RequestArtifactDraft /
 * SubmitReviewDecision on both system-design and project-design). openapi-typescript
 * keys its `operations` interface by operationId, so colliding ids emit duplicate
 * interface members (TS2300 "Duplicate identifier") and `paths` resolves to a
 * merged/broken operation type.
 *
 * We cannot change the server OAS (it is generated). So this wrapper makes every
 * operationId unique in an in-memory copy of the YAML (duplicates get a numeric
 * suffix) BEFORE handing it to openapi-typescript. The SPA consumes the typed
 * `paths` (keyed by URL) + `components`, never the operation names, so the suffixes
 * are invisible to callers — `paths` and `operations` simply stay internally
 * consistent.
 */
import { readFileSync, writeFileSync, mkdtempSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const oasPath = join(here, '..', '..', 'server', 'api', 'openapi.yaml');
const outPath = join(here, '..', 'src', 'api', 'schema.ts');

const source = readFileSync(oasPath, 'utf8');

// Uniquify duplicate `operationId:` values. Each operationId line belongs to
// exactly one path+method block, so renaming the value in place keeps the path's
// reference and the generated operations entry consistent.
const seen = new Map();
const deduped = source
  .split('\n')
  .map((line) => {
    const m = /^(\s*operationId:\s*)(\S+)\s*$/.exec(line);
    if (m === null) return line;
    const [, prefix, id] = m;
    const count = seen.get(id) ?? 0;
    seen.set(id, count + 1);
    return count === 0 ? line : `${prefix}${id}_${String(count + 1)}`;
  })
  .join('\n');

const tmp = join(mkdtempSync(join(tmpdir(), 'aiarch-oas-')), 'openapi.yaml');
writeFileSync(tmp, deduped);

execFileSync('npx', ['openapi-typescript', tmp, '-o', outPath], {
  stdio: 'inherit',
  cwd: join(here, '..'),
});
