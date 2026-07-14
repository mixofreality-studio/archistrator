/**
 * toolName → view container. Keys MUST match the mcpemit-stamped tool names the
 * server annotates with a `ui://` resource (project.json ui annotations); the shell
 * looks the rendered app up by `hostContext.toolInfo.tool.name`. An unknown tool
 * falls through to the shell's graceful "no view registered" message.
 */
import type { ComponentType } from 'react';
import { McpSystemDesignContainer, type McpViewProps } from '../containers/McpSystemDesignContainer';

export const VIEW_REGISTRY: Record<string, ComponentType<McpViewProps>> = {
  systemDesignGetSessionState: McpSystemDesignContainer,
};
