/**
 * Whether the detail pane's narrow-screen Drawer (below 1200px) is MODAL.
 *
 * In the list the modal drawer is right: the list is a column the drawer
 * replaces. In the GRAPH lens it is not (designer P2): the canvas is what the
 * operator navigates by, and a modal backdrop over it — plus the focus trap —
 * made every other card unreachable until the pane closed. So the graph gets a
 * NON-modal drawer: no backdrop, no focus trap, no scroll lock, and the canvas
 * beside it keeps taking clicks.
 *
 * Pure — pinned by detailDrawer.test.ts.
 */
import type { LensId } from './useLensSelection.ts';

export function detailDrawerModal(lens: LensId): boolean {
  return lens !== 'graph';
}
