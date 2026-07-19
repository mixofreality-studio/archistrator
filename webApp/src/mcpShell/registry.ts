/**
 * (view id | tool name) → view container. Per the ratified spec ("the same view
 * ids drive the registry"), each entry carries TWO keys mapped to the same
 * container:
 *  - the webApp view-registry id (project.json's `ui.view`, mirrored onto the
 *    tool's `_meta.ui.view` by mcpemit — see server cmd/clientgen/internal/
 *    mcpemit/mcpemit.go) — the PRIMARY resolution path (resolveView.ts);
 *  - the mcpemit-stamped tool name — the FALLBACK path, for any host that
 *    doesn't forward `_meta` through `toolInfo.tool`.
 * An unknown tool/view falls through to the shell's graceful "no view
 * registered" message (see main.tsx).
 */
import type { ComponentType } from 'react';
import {
  McpSystemDesignContainer,
  type McpViewProps,
} from '../containers/McpSystemDesignContainer';

export const VIEW_REGISTRY: Record<string, ComponentType<McpViewProps>> = {
  'system-design-session': McpSystemDesignContainer,
  systemDesignGetSessionState: McpSystemDesignContainer,
};
