/**
 * The hand-declared COMPOSITION ROUTES: generator input beside the server OAS.
 *
 * These two GETs are mounted by the Go composition root
 * (server/cmd/server/hooks.go's ExtraMounts, and framework-go's /api/userinfo),
 * not by a manager contract, so the OAS does not carry them and no MCP tool
 * exists for them. They used to be raw `fetch` calls that bypassed the
 * OpsClient seam (design-renderer-data.md §2′.0 P2), which meant a preview
 * build could not intercept them. Declaring them here lets gen-ops.mjs bind
 * them into OP_BINDINGS like any other op (with `tool: null`), and lets
 * fixture-schema.mjs type their fixtures, so they ride the ONE transport seam.
 *
 * `result` is the JSON Schema of the 200 body. It mirrors the hand-written TS
 * types the app reads (utilities/auth/userInfo.ts's UserInfo and
 * hooks/useOperatedAppId.ts's OperatedAppIdResponse); keep them in step.
 */
export const COMPOSITION_ROUTES = {
  compositionGetOperatedAppId: {
    method: 'GET',
    path: '/api/v1/projects/{projectID}/operated-app-id',
    result: {
      type: 'object',
      required: ['operatedAppId'],
      properties: { operatedAppId: { type: 'string' } },
    },
  },
  compositionGetUserinfo: {
    method: 'GET',
    path: '/api/userinfo',
    result: {
      type: 'object',
      required: ['kind', 'sub'],
      properties: {
        kind: { type: 'string' },
        sub: { type: 'string' },
        preferred_username: { type: 'string' },
        email: { type: 'string' },
        name: { type: 'string' },
        roles: { type: 'array', items: { type: 'string' } },
        organizations: {
          type: 'array',
          items: {
            type: 'object',
            required: ['id', 'name'],
            properties: { id: { type: 'string' }, name: { type: 'string' } },
          },
        },
      },
    },
  },
};
