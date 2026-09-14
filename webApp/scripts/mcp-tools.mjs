/**
 * The MCP tools the server REALLY registers, read from the server's generated tool
 * tables (server/internal/client/mcp/<mgr>/<mgr>_tools.gen.go, emitted by
 * server/cmd/clientgen/internal/mcpemit). op-bindings.mjs binds an OAS op's
 * `tool` only when its name is in this set, and binds `tool: null` otherwise, so
 * mcpOpsClient REFUSES an op with no tool (ApiError 501 no_mcp_tool) instead of
 * calling a tool that does not exist.
 *
 * Why read the generated Go: the tool name is decided there (mcpemit:
 * `toolName := mgrPrefix + op.Name`), and it is not the name the path yields for
 * every op. /project-design/request-sdp-commit is `projectDesignRequestSDPCommit`
 * on the server, and the path-derived `projectDesignRequestSdpCommit` was bound
 * to a tool that never existed (preview P1b). gen-ops.mjs already reads the
 * server OAS the same way (../server/api/openapi.yaml).
 *
 * Loud on a blind read: no tool directory, or no tool in it, aborts the build
 * rather than binding every op `tool: null`.
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

export const MCP_TOOLS_DIR = join(here, '..', '..', 'server', 'internal', 'client', 'mcp');

// The one registration shape mcpemit emits (with or without a Meta for a UI view).
const ADD_TOOL = /mcp\.AddTool\(srv, &mcp\.Tool\{Name: "([A-Za-z0-9]+)"/g;

/** The set of tool names the server registers. Throws when it reads none. */
export function loadMcpTools(dir = MCP_TOOLS_DIR) {
  if (!existsSync(dir)) {
    throw new Error(`mcp-tools: no generated MCP tool directory at ${dir}`);
  }
  const tools = new Set();
  for (const mgr of readdirSync(dir).sort()) {
    const mgrDir = join(dir, mgr);
    if (!statSync(mgrDir).isDirectory()) continue;
    for (const file of readdirSync(mgrDir).sort()) {
      if (!file.endsWith('_tools.gen.go')) continue;
      for (const match of readFileSync(join(mgrDir, file), 'utf8').matchAll(ADD_TOOL)) {
        tools.add(match[1]);
      }
    }
  }
  if (tools.size === 0) {
    throw new Error(`mcp-tools: read no mcp.AddTool registration under ${dir}`);
  }
  return tools;
}
