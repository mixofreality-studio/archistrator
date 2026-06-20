/**
 * App root: theme token state + session gate + router. The QueryClientProvider is
 * mounted in main.tsx so the client outlives re-renders. ThemeProvider holds the
 * active design-language tokens (read by AppTheme inside the router's root route).
 * UserProvider probes /api/userinfo (GTD parity) and only renders the router once
 * an edge session is confirmed.
 */
import type { ReactNode } from 'react';
import { RouterProvider } from '@tanstack/react-router';
import { ThemeProvider } from './theme/ThemeContext';
import { AppTheme } from './theme/AppTheme';
import { UserProvider } from './auth/UserContext';
import { router } from './navigation/router';

export default function App(): ReactNode {
  return (
    <ThemeProvider>
      <AppTheme>
        <UserProvider>
          <RouterProvider router={router} />
        </UserProvider>
      </AppTheme>
    </ThemeProvider>
  );
}
