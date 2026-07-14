/**
 * Top-level error boundary for the MCP shell. The host loads the built bundle as a
 * classic cross-origin script, which mutes `window.onerror` (spec §3.4) — so a
 * render-time throw would otherwise vanish silently. This boundary is the error
 * channel: it renders a minimal fallback and reports the failure to the host via
 * `app.sendLog` (which the host may record for debugging but never surfaces to the
 * model / conversation).
 */
import { Component, type ErrorInfo, type ReactNode } from 'react';
import type { App } from '@modelcontextprotocol/ext-apps';

interface Props {
  app: App;
  children: ReactNode;
}

interface State {
  error: Error | null;
}

export class McpErrorBoundary extends Component<Props, State> {
  override state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    void this.props.app.sendLog({
      level: 'error',
      logger: 'archistrator-mcp',
      data: `MCP view crashed: ${error.message}\n${info.componentStack ?? ''}`,
    });
  }

  override render(): ReactNode {
    if (this.state.error !== null) {
      return (
        <p style={{ fontFamily: 'monospace', fontSize: 13, padding: 16 }}>
          This view hit an error and can’t be displayed. Ask the agent to re-run the tool.
        </p>
      );
    }
    return this.props.children;
  }
}
