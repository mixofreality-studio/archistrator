/**
 * The scenario deep link, handed from the detail pane (which reads the URL) to
 * the system test plan's scenario browser (which renders deep inside the
 * artifact renderer dispatch). A context, so the dispatch's renderer props stay
 * the same for every kind. Absent — outside the construction console — the
 * browser keeps its own local pick.
 */
import { createContext } from 'react';

export interface ScenarioLink {
  /** `sc` from the URL; undefined when the link names none. */
  scenarioId: string | undefined;
  /** Record a pick in the URL (a replace, not a history entry). */
  onScenarioChange: (scenarioId: string) => void;
}

export const ScenarioLinkContext = createContext<ScenarioLink | undefined>(undefined);
