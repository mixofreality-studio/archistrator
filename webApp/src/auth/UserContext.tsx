/* eslint-disable react-refresh/only-export-components -- provider + hook colocated */
/**
 * Session gate (GTD parity). The Envoy edge runs the Keycloak OIDC redirect
 * login, sets the session cookie, and forwards the validated access token to the
 * server; the SPA never runs its own OIDC flow. On mount we probe
 * GET /api/userinfo (same-origin, so the edge session cookie rides along):
 *
 *   - 200   → authenticated; provide the user to the app.
 *   - 401   → no/expired session. Reload the page: a top-level navigation is
 *             answered by the edge with the OIDC redirect (302 to Keycloak),
 *             whereas this `Accept: application/json` probe is answered with 401
 *             by the edge's denyRedirect rule. After login the cookie is set and
 *             the reloaded probe returns 200.
 *   - other → surface an error with a retry button.
 *
 * In dev mode there is no edge; the server injects a dev principal, so the same
 * probe returns 200 with that principal — no special-casing needed here.
 */
import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import Alert from '@mui/material/Alert';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import { config } from '../config';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';
import type { UserInfo } from './userInfo';

interface UserContextValue {
  readonly user: UserInfo;
}

const UserContext = createContext<UserContextValue | null>(null);

export function useUser(): UserInfo {
  const ctx = useContext(UserContext);
  if (ctx === null) {
    throw new Error('useUser must be used within a UserProvider');
  }
  return ctx.user;
}

export function UserProvider({ children }: { children: ReactNode }): ReactNode {
  const [user, setUser] = useState<UserInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // load() performs the probe. It deliberately does NOT set loading/error
  // synchronously up front: the mount path starts in the loading state already,
  // and the retry handler resets that state itself before calling load(). This
  // keeps the effect free of synchronous setState (react-hooks/set-state-in-effect)
  // — every setState below runs in an async continuation after `await`.
  const load = async (): Promise<void> => {
    try {
      const res = await fetch(`${config.apiBaseUrl}/api/userinfo`, {
        headers: { Accept: 'application/json' },
      });
      if (!res.ok) {
        if (res.status === 401) {
          // No/expired edge session — reload so the edge issues the OIDC redirect.
          window.location.reload();
          return;
        }
        throw new Error(`Failed to load user info: ${String(res.status)} ${res.statusText}`);
      }
      setUser((await res.json()) as UserInfo);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  const retry = (): void => {
    setLoading(true);
    setError(null);
    void load();
  };

  useEffect(() => {
    // load() only setStates in an async continuation (after `await fetch`), so
    // there is no synchronous cascading render — the rule's heuristic can't see
    // past the await. Probe once on mount.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, []);

  if (loading) {
    return (
      <Box
        data-testid={UI_IDENTIFIERS.Common.LOADING}
        sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}
      >
        <CircularProgress />
      </Box>
    );
  }

  if (error !== null || user === null) {
    return (
      <Box
        sx={{
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'center',
          alignItems: 'center',
          minHeight: '100vh',
          gap: 2,
          p: 3,
        }}
      >
        <Alert
          data-testid={UI_IDENTIFIERS.Common.ERROR_ALERT}
          severity="error"
          sx={{ maxWidth: 600 }}
        >
          <Typography gutterBottom variant="h6">
            Failed to load your session
          </Typography>
          <Typography sx={{ mb: 2 }} variant="body2">
            {error ?? 'No session available'}
          </Typography>
          <Button variant="contained" onClick={retry}>
            Retry
          </Button>
        </Alert>
      </Box>
    );
  }

  return <UserContext.Provider value={{ user }}>{children}</UserContext.Provider>;
}
