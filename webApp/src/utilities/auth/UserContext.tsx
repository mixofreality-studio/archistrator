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
 *
 * The probe itself is INJECTED (`fetchUser`), never fetched here: App.tsx passes
 * hooks/useUserInfo.ts's OpsClient call, so the probe rides the same transport
 * seam as every other request and a preview's fixture transport can answer it
 * (design-renderer-data.md §2′.0 P2). A 401 arrives as the transport's ApiError.
 */
import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import Alert from '@mui/material/Alert';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import { ApiError } from '../../contracts/errors';
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

export function UserProvider({
  fetchUser,
  children,
}: {
  /** The session probe: resolves the user, or rejects (an ApiError carries the status). */
  fetchUser: () => Promise<UserInfo>;
  children: ReactNode;
}): ReactNode {
  const [user, setUser] = useState<UserInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // load() performs the probe. It deliberately does NOT set loading/error
  // synchronously up front: the mount path starts in the loading state already,
  // and the retry handler resets that state itself before calling load(). This
  // keeps the effect free of synchronous setState (react-hooks/set-state-in-effect)
  // — every setState below runs in an async continuation after `await`.
  // It is memoized on `fetchUser` (App passes a stable useCallback), so the mount
  // effect below still probes exactly once per transport.
  const load = useCallback(async (): Promise<void> => {
    try {
      setUser(await fetchUser());
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        // No/expired edge session — reload so the edge issues the OIDC redirect.
        window.location.reload();
        return;
      }
      setError(
        err instanceof ApiError
          ? `Failed to load user info: ${String(err.status)} ${err.message}`
          : err instanceof Error
            ? err.message
            : 'Unknown error'
      );
    } finally {
      setLoading(false);
    }
  }, [fetchUser]);

  const retry = (): void => {
    setLoading(true);
    setError(null);
    void load();
  };

  useEffect(() => {
    // Probe once on mount.
    // eslint-disable-next-line react-hooks/set-state-in-effect -- load() only setStates in an async continuation (after `await fetchUser()`), so there is no synchronous cascading render; the rule's heuristic can't see past the await
    void load();
  }, [load]);

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
