/**
 * The operations route's `beforeLoad` enforcement (operations-argocd-
 * deployment Task 11, spec D9), split out of router.tsx so it is directly
 * unit-testable under `node --test` (router.tsx's own doc comment bars local
 * component definitions there for fast-refresh; this is a plain function, but
 * keeping it here also means a test doesn't have to build the whole route
 * tree to exercise it).
 *
 * `fetchFn` is a REQUIRED parameter (no default import of the real
 * fetchCapabilities) rather than injected implicitly: hooks/useCapabilities.ts
 * -> api/client.ts -> utilities/config.ts reads Vite's `import.meta.env`,
 * which is undefined outside a Vite build/dev-server — a module that
 * statically imports that chain crashes the instant `node --test` loads it.
 * Keeping this file free of that import lets operationsGuard.test.ts exercise
 * the guard directly with a fake fetch; router.tsx (only ever loaded by Vite)
 * supplies the real fetchCapabilities at the call site.
 *
 * D9 needs "operations is disabled" and "the capabilities check failed" to be
 * DIFFERENT outcomes, not the same silent redirect:
 *
 *   - Server answers {operations:false} → redirect home. This is the actual
 *     D9 case (the local profile always answers this endpoint successfully —
 *     it's the very same process serving the SPA — so a real local boot
 *     never lands in the unreachable branch below).
 *   - Server unreachable after a couple of short retries → do NOT redirect.
 *     A cloud operator hitting a transient network blip mid-incident must not
 *     get silently bounced out of the console with no explanation — that is
 *     strictly worse than an honest error state, because it is
 *     indistinguishable from "operations is off" or "you mistyped the URL".
 *     The route mounts and OperationsConsoleScreen renders an explicit error
 *     via the returned context instead.
 */
import { redirect } from '@tanstack/react-router';
import { operationsEnabled, type Capabilities } from '../utilities/capabilities.ts';

const DEFAULT_RETRY_ATTEMPTS = 3;
const DEFAULT_RETRY_DELAY_MS = 250;

/** The route context operationsBeforeLoad hands to OperationsConsoleScreen. */
export interface OperationsRouteContext {
  readonly capabilitiesUnreachable: boolean;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

/**
 * Fetches capabilities via `fetchFn`, retrying a couple of times with a short
 * backoff before giving up. Returns the real Capabilities on any success
 * (including a genuine {operations:false} — that is not a failure), or the
 * literal `'unreachable'` only once every attempt has thrown/rejected.
 */
export async function probeCapabilities(
  fetchFn: () => Promise<Capabilities>,
  attempts: number = DEFAULT_RETRY_ATTEMPTS,
  delayMs: number = DEFAULT_RETRY_DELAY_MS
): Promise<Capabilities | 'unreachable'> {
  for (let attempt = 0; attempt < attempts; attempt++) {
    try {
      return await fetchFn();
    } catch {
      if (attempt < attempts - 1) {
        await sleep(delayMs);
      }
    }
  }
  return 'unreachable';
}

/**
 * The operations route's `beforeLoad`. Production callers (router.tsx) pass
 * the real fetchCapabilities; tests inject a fake `fetchFn` and override
 * `attempts`/`delayMs` to run fast and deterministic.
 */
export async function operationsBeforeLoad(
  fetchFn: () => Promise<Capabilities>,
  attempts: number = DEFAULT_RETRY_ATTEMPTS,
  delayMs: number = DEFAULT_RETRY_DELAY_MS
): Promise<OperationsRouteContext> {
  const capabilities = await probeCapabilities(fetchFn, attempts, delayMs);
  if (capabilities === 'unreachable') {
    return { capabilitiesUnreachable: true };
  }
  if (!operationsEnabled(capabilities)) {
    // redirect({ throw: true }) throws internally — see @tanstack/router-core's
    // redirect(): Redirect extends Response, not Error, so an explicit `throw
    // redirect(...)` here would trip @typescript-eslint/only-throw-error.
    redirect({ to: '/', throw: true });
  }
  return { capabilitiesUnreachable: false };
}
