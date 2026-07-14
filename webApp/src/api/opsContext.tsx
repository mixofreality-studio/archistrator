/* eslint-disable react-refresh/only-export-components -- provider + hook colocated */
/**
 * React context that hands hooks a transport-blind OpsClient. Whichever shell
 * mounts the SPA (standalone browser vs. an MCP-hosted app) supplies the
 * matching OpsClient impl (restOpsClient / mcpOpsClient, see ops.gen.ts) once
 * at the root; every hook below just calls `useOpsClient().ops.call(...)`.
 */
import { createContext, useContext, type ReactNode } from 'react';
import type { OpsClient } from './ops.gen';

export interface OpsCtx {
  ops: OpsClient;
  transport: 'rest' | 'mcp';
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
