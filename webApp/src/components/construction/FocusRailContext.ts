/**
 * The focus view's side panel, as the artifact inside it sees it: whether it is
 * beside the artifact, whether it can collapse, and the switch — so the Code
 * tab's needs-room note can offer "collapse the side panel" and a compact verdict
 * chip can open it again (focusRail.ts). Undefined outside the focus view.
 */
import { createContext, useContext } from 'react';

export interface FocusRailState {
  /** The rail is collapsed (only ever true where it can collapse). */
  collapsed: boolean;
  /** It can collapse here: a window of 600px or more (below, it stacks above). */
  collapsible: boolean;
  setCollapsed: (collapsed: boolean) => void;
}

export const FocusRailContext = createContext<FocusRailState | undefined>(undefined);

export function useFocusRail(): FocusRailState | undefined {
  return useContext(FocusRailContext);
}
