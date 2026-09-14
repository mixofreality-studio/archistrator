/* eslint-disable react-refresh/only-export-components -- provider + hook colocated */
/**
 * React context that hands hooks a transport-blind OpsClient. Whichever shell
 * mounts the SPA (standalone browser, an MCP-hosted app, or the preview build)
 * supplies the matching OpsClient impl (restOpsClient / mcpOpsClient, see
 * ops.gen.ts; fixtureOpsClient, see fixtureOps.ts) once at the root; every hook
 * below just calls `useOpsClient().ops.call(...)`.
 *
 * `transport === 'fixture'` is how a component learns it is rendering inside a
 * preview: a nested preview panel renders a placeholder instead of a frame
 * (design-renderer-data.md §2′.5 item 5).
 */
import { createContext, useContext, type ReactNode } from 'react';
import type { OpsClient } from './ops.gen';

export interface OpsCtx {
  ops: OpsClient;
  transport: 'rest' | 'mcp' | 'fixture';
}

const Ctx = createContext<OpsCtx | null>(null);

export function OpsClientProvider({
  value,
  children,
}: {
  value: OpsCtx;
  children: ReactNode;
}): ReactNode {
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useOpsClient(): OpsCtx {
  const value = useContext(Ctx);
  if (value === null) throw new Error('OpsClientProvider missing');
  return value;
}
