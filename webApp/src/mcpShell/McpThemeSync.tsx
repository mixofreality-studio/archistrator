/**
 * Keeps the app's design tokens in step with the host's light/dark context after
 * boot. The initial theme is seeded once (ThemeProvider `initialThemeKey`, from the
 * handshake host-context); this bridge subscribes to later
 * `host-context-changed` notifications and flips the active token bag when the user
 * toggles their host theme. Renders nothing.
 */
import { useEffect, type ReactNode } from 'react';
import type { App } from '@modelcontextprotocol/ext-apps';
import { useThemeSwitch } from '../utilities/theme/ThemeContext';
import { themeKeyFromHost } from './host';

export function McpThemeSync({ app }: { app: App }): ReactNode {
  const { setThemeKey } = useThemeSwitch();
  useEffect(() => {
    const handler = (params: { theme?: 'light' | 'dark' }): void => {
      if (params.theme !== undefined) setThemeKey(themeKeyFromHost(params.theme));
    };
    app.addEventListener('hostcontextchanged', handler);
    return (): void => {
      app.removeEventListener('hostcontextchanged', handler);
    };
  }, [app, setThemeKey]);
  return null;
}
