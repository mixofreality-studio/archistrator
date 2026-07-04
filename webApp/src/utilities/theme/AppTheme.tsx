/**
 * Mounts the MUI theme root from the active token bag (ThemeContext). Renders the
 * MuiThemeProvider, a CssBaseline, and the body GlobalStyles (background texture +
 * selection colour). The ThemeProvider that holds the token state lives above
 * this in the tree (App.tsx).
 */
import { useMemo, type ReactNode } from 'react';
import { ThemeProvider as MuiThemeProvider } from '@mui/material/styles';
import CssBaseline from '@mui/material/CssBaseline';
import GlobalStyles from '@mui/material/GlobalStyles';
import { useTokens } from './ThemeContext';
import { buildMuiTheme } from './themes';

export function AppTheme({ children }: { children: ReactNode }): ReactNode {
  const tokens = useTokens();
  const muiTheme = useMemo(() => buildMuiTheme(tokens), [tokens]);

  return (
    <MuiThemeProvider theme={muiTheme}>
      <CssBaseline />
      <GlobalStyles
        styles={{
          body: {
            backgroundColor: tokens.bg,
            backgroundImage: tokens.texture,
            backgroundSize: tokens.textureSize,
            transition: 'background-color 200ms ease',
          },
          '::selection': { background: tokens.accent, color: tokens.accentText },
        }}
      />
      {children}
    </MuiThemeProvider>
  );
}
