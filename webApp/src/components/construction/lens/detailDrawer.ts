/**
 * Whether the detail pane's narrow-screen Drawer (below 1200px) is MODAL.
 *
 * In the list the modal drawer is right: the list is a column the drawer
 * replaces. In the GRAPH lens it is not (designer P2): the canvas is what the
 * operator navigates by, and a modal backdrop over it — plus the focus trap —
 * made every other card unreachable until the pane closed. So the graph gets a
 * NON-modal drawer — MUI's `persistent` variant, which renders no Modal: no
 * backdrop, no focus trap, no scroll lock, and none of the aria-hiding a
 * Modal applies to the rest of the app, so the canvas beside it stays both
 * clickable and visible to assistive technology.
 *
 * Pure — pinned by detailDrawer.test.ts.
 */
import type { LensId } from './useLensSelection.ts';

export function detailDrawerModal(lens: LensId): boolean {
  return lens !== 'graph';
}
