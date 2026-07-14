/**
 * Pure resolution of which VIEW_REGISTRY key identifies the current MCP tool
 * render. Per the ratified spec ("the same view ids drive the registry"), the
 * PRIMARY source is the view id carried on the tool's own `_meta.ui.view` —
 * stamped by mcpemit from the service contract's `op.UI.View` (see server
 * cmd/clientgen/internal/mcpemit/mcpemit.go), which is the same id project.json
 * declares as the artifact's `ui.view`. That keeps tool name and view id from
 * being two independent (and driftable) sources of truth for the same routing
 * decision.
 *
 * FALLING BACK to the tool name covers any host that doesn't forward `_meta`
 * through `toolInfo.tool` — VIEW_REGISTRY carries both keys mapped to the same
 * container for exactly that reason.
 *
 * Kept free of any import on the view-container modules (which are JSX, and
 * this repo's plain `node --test 'src/**\/*.test.ts'` harness has no JSX
 * transform) so the resolution logic is unit-testable headlessly — see
 * resolveView.test.ts.
 */
export type ViewResolution =
  | { key: string; resolvedBy: 'view' }
  | { key: string; resolvedBy: 'toolName' }
  | { key: undefined; resolvedBy: 'none' };

/** Read the `ui.view` string out of a tool's `_meta` record, if present and well-typed. */
function viewIdFromToolMeta(toolMeta: Record<string, unknown> | undefined): string | undefined {
  const ui = toolMeta?.['ui'];
  if (typeof ui !== 'object' || ui === null) return undefined;
  const view = (ui as Record<string, unknown>)['view'];
  return typeof view === 'string' && view.length > 0 ? view : undefined;
}

/**
 * @param toolMeta the current tool's `_meta` record (`hostContext.toolInfo.tool._meta`)
 * @param toolName the current tool's name (`hostContext.toolInfo.tool.name`)
 * @param hasKey   whether VIEW_REGISTRY carries a given key (injected so this stays
 *                 dependency-free of the registry's JSX-importing module)
 */
export function resolveViewKey(
  toolMeta: Record<string, unknown> | undefined,
  toolName: string | undefined,
  hasKey: (key: string) => boolean
): ViewResolution {
  const viewId = viewIdFromToolMeta(toolMeta);
  if (viewId !== undefined && hasKey(viewId)) {
    return { key: viewId, resolvedBy: 'view' };
  }
  if (toolName !== undefined && toolName.length > 0 && hasKey(toolName)) {
    return { key: toolName, resolvedBy: 'toolName' };
  }
  return { key: undefined, resolvedBy: 'none' };
}
