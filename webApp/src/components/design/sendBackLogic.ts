/**
 * Pure disabled-state logic for GatePanel's "Send back" button, split out of
 * GatePanel.tsx so it is unit-testable headlessly (see sendBackLogic.test.ts) —
 * GatePanel.tsx is JSX, outside this repo's plain `node --test 'src/**\/*.test.ts'`
 * harness (no jsdom/RTL; see roleLine.ts for the established pattern of pulling
 * pure UI logic out into a React-free module for exactly this reason).
 *
 * SPA default (`allowEmptySendBack: false`): stays disabled until at least one
 * client-accumulated comment exists, so a redraft always carries guidance.
 * MCP (`allowEmptySendBack: true`, see McpSystemDesignContainer) has no
 * client-side comment accumulator — its composer collects + enforces
 * non-empty feedback AFTER this click, so the click itself must stay reachable
 * at commentCount === 0 (see GatePanel's own doc comment for the full rationale).
 */
export function sendBackDisabled(
  pending: boolean,
  commentCount: number,
  allowEmptySendBack: boolean
): boolean {
  return pending || (!allowEmptySendBack && commentCount === 0);
}
