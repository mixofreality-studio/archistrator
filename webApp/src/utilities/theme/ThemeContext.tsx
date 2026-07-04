/* eslint-disable react-refresh/only-export-components -- provider + hooks colocated */
/**
 * Theme context: holds the active design language and persists the choice to
 * localStorage. useTokens() exposes the raw semantic token bag; useThemeSwitch()
 * exposes the setter for the ThemeSwitcher. Default is 'retro'.
 */
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';
import { TOKENS, type ThemeKey, type Tokens } from './themes';

interface Ctx {
  themeKey: ThemeKey;
  setThemeKey: (k: ThemeKey) => void;
  tokens: Tokens;
}

const ThemeCtx = createContext<Ctx | null>(null);

const STORAGE_KEY = 'archistrator.theme';
const DEFAULT_THEME: ThemeKey = 'retro';

function readStoredTheme(): ThemeKey {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    if (stored !== null && stored in TOKENS) {
      return stored as ThemeKey;
    }
  } catch {
    // localStorage unavailable (private mode / SSR) — fall back to default.
  }
  return DEFAULT_THEME;
}

export function useTokens(): Tokens {
  const c = useContext(ThemeCtx);
  if (c === null) throw new Error('useTokens must be used within a ThemeProvider');
  return c.tokens;
}

export function useThemeSwitch(): Ctx {
  const c = useContext(ThemeCtx);
  if (c === null) throw new Error('useThemeSwitch must be used within a ThemeProvider');
  return c;
}

export function ThemeProvider({ children }: { children: ReactNode }): ReactNode {
  const [themeKey, setThemeKey] = useState<ThemeKey>(readStoredTheme);

  const selectTheme = useCallback((k: ThemeKey): void => {
    setThemeKey(k);
    try {
      window.localStorage.setItem(STORAGE_KEY, k);
    } catch {
      // Best-effort persistence; ignore storage failures.
    }
  }, []);

  const tokens = TOKENS[themeKey];
  const value = useMemo<Ctx>(
    () => ({ themeKey, setThemeKey: selectTheme, tokens }),
    [themeKey, selectTheme, tokens]
  );

  return <ThemeCtx.Provider value={value}>{children}</ThemeCtx.Provider>;
}
