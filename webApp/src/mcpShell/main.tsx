/**
 * MCP shell entry. Boots the host bridge (handshake + first tool result), resolves
 * the view from the tool name, and mounts it inside the SAME ThemeProvider/AppTheme
 * pair the SPA uses (seeded from the host's light/dark context), a fresh
 * QueryClient, and the MCP-transport OpsClient. The `app` singleton is handed DOWN
 * to the container as a prop (the lint DAG bars containers from importing mcpShell).
 *
 * Deliberately NO StrictMode: the app owns a live host connection and posts model
 * context from effects; the dev-only double-invoke would double those side effects
 * for no benefit in a host-embedded surface.
 */
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '@fontsource/space-grotesk/400.css';
import '@fontsource/space-mono/400.css';
import '@fontsource/inter/400.css';
import { OpsClientProvider } from '../api/opsContext';
import { mcpOpsClient } from '../api/ops.gen';
import { ThemeProvider } from '../utilities/theme/ThemeContext';
import { AppTheme } from '../utilities/theme/AppTheme';
import { VIEW_REGISTRY } from './registry';
import { resolveViewKey } from './resolveView';
import { McpErrorBoundary } from './McpErrorBoundary';
import { McpThemeSync } from './McpThemeSync';
import { app, bootHost, currentToolArgs, firstToolStructuredContent, themeKeyFromHost } from './host';

async function main(): Promise<void> {
  await bootHost();

  const ctx = app.getHostContext();
  const toolName = ctx?.toolInfo?.tool.name ?? '';
  // Resolve PRIMARILY from the tool's own _meta.ui.view (mcpemit-stamped, mirrors
  // project.json's ui.view — see resolveView.ts), FALLING BACK to the tool name.
  // Logged to the host (app.sendLog) so which path fired is visible per-host —
  // a host-variance datum, not just a debugging aid.
  const resolution = resolveViewKey(
    ctx?.toolInfo?.tool._meta,
    toolName,
    (key) => VIEW_REGISTRY[key] !== undefined,
    [...new Set(Object.values(VIEW_REGISTRY))].length === 1 ? Object.keys(VIEW_REGISTRY).slice(0, 1) : []
  );
  const View = resolution.key !== undefined ? VIEW_REGISTRY[resolution.key] : undefined;
  void app.sendLog({
    level: 'info',
    logger: 'mcpShell.registry',
    data:
      resolution.resolvedBy === 'none'
        ? `no view registered for tool "${toolName}" (no _meta.ui.view either)`
        : `resolved view "${resolution.key}" via ${resolution.resolvedBy} (tool "${toolName}")`,
  });

  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
  });

  const rootElement = document.getElementById('root');
  if (rootElement === null) throw new Error('Root element not found');

  createRoot(rootElement).render(
    <McpErrorBoundary app={app}>
      <ThemeProvider initialThemeKey={themeKeyFromHost(ctx?.theme)}>
        <AppTheme>
          <QueryClientProvider client={queryClient}>
            <OpsClientProvider value={{ ops: mcpOpsClient(app), transport: 'mcp' }}>
              <McpThemeSync app={app} />
              {View !== undefined ? (
                <View
                  app={app}
                  displayMode={ctx?.displayMode}
                  seededResult={firstToolStructuredContent()}
                  toolArgs={currentToolArgs()}
                />
              ) : (
                <p style={{ fontFamily: 'monospace', fontSize: 13, padding: 16 }}>
                  No view registered for {toolName.length > 0 ? toolName : 'this tool'}.
                </p>
              )}
            </OpsClientProvider>
          </QueryClientProvider>
        </AppTheme>
      </ThemeProvider>
    </McpErrorBoundary>
  );
}

void main();
