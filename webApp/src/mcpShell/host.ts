/**
 * The MCP host bridge singleton and its boot sequence.
 *
 * Constructs the ext-apps `App`, registers the tool-lifecycle listeners BEFORE
 * `connect()` (per the create-mcp-app skill — a one-shot notification fired during
 * the handshake is missed if its first listener is added afterwards), and captures
 * the latest tool input/result so the shell can render the pushed screen instantly.
 *
 * The `app` singleton is exported so `main.tsx` can hand it DOWN to the view
 * container as a prop — the lint DAG bars a `containers/` file from importing
 * `mcpShell/`, so the container never imports this module; it receives `app` and
 * reads live results off the `mcp-tool-result` window event instead.
 */
import { App, type McpUiToolResultNotification } from '@modelcontextprotocol/ext-apps';
import type { ThemeKey } from '../utilities/theme/themes';

/** Window event carrying every pushed tool result (initial + agent re-invocations). */
export const TOOL_RESULT_EVENT = 'mcp-tool-result';

/** Map the host's coarse light/dark signal onto a concrete design-language key. */
export function themeKeyFromHost(theme: 'light' | 'dark' | undefined): ThemeKey {
  return theme === 'dark' ? 'mor' : 'retro';
}

export const app = new App({ name: 'archistrator', version: '0.1.0' });

let latestToolArgs: Record<string, unknown> = {};
let latestToolResult: McpUiToolResultNotification['params'] | undefined;

let resolveFirstResult: (() => void) | undefined;
const firstResult = new Promise<void>((resolve) => {
  resolveFirstResult = resolve;
});

// Registered before connect() so no handshake-adjacent notification is missed.
app.addEventListener('toolinput', (params) => {
  latestToolArgs = params.arguments ?? {};
});
app.addEventListener('toolresult', (params) => {
  latestToolResult = params;
  window.dispatchEvent(new CustomEvent<McpUiToolResultNotification['params']>(TOOL_RESULT_EVENT, { detail: params }));
  resolveFirstResult?.();
});

/**
 * Complete the host handshake, then wait (briefly) for the first tool result so the
 * initial render can seed the cache and paint the screen immediately. The race falls
 * through after a short timeout so a tool that pushes no result (or a cold reload)
 * still renders its loading/fetch path rather than hanging.
 */
export async function bootHost(): Promise<void> {
  await app.connect();
  await Promise.race([firstResult, new Promise<void>((resolve) => setTimeout(resolve, 1500))]);
}

/** The most recent complete tool arguments (path + query + body, flattened). */
export function currentToolArgs(): Record<string, unknown> {
  return latestToolArgs;
}

/** The first tool result's structured payload, for synchronous cache seeding. */
export function firstToolStructuredContent(): Record<string, unknown> | undefined {
  return latestToolResult?.structuredContent;
}
