/**
 * App root: theme token state + session gate + router. The QueryClientProvider is
 * mounted in main.tsx so the client outlives re-renders. ThemeProvider holds the
 * active design-language tokens (read by AppTheme inside the router's root route).
 * UserProvider probes /api/userinfo (GTD parity) and only renders the router once
 * an edge session is confirmed.
 *
 * The SAME App boots in two shells: main.tsx (the browser SPA, REST transport,
 * browser history) and previewShell/main.tsx (the preview build, fixture
 * transport, memory history). Each shell builds its own router and OpsClient;
 * App reads the probe through the OpsClient, so it never knows which is live.
 */
import type { ReactNode } from 'react';
import { RouterProvider } from '@tanstack/react-router';
import { ThemeProvider } from './utilities/theme/ThemeContext';
import { AppTheme } from './utilities/theme/AppTheme';
import { UserProvider } from './utilities/auth/UserContext';
import { useUserInfoFetcher } from './hooks/useUserInfo';
import type { AppRouter } from './routes/router';

export default function App({ router }: { router: AppRouter }): ReactNode {
  const fetchUser = useUserInfoFetcher();
  return (
    <ThemeProvider>
      <AppTheme>
        <UserProvider fetchUser={fetchUser}>
          <RouterProvider router={router} />
        </UserProvider>
      </AppTheme>
    </ThemeProvider>
  );
}
