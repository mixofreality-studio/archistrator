/**
 * Containers-layer wiring for the project's STATE HISTORY (use case
 * view-the-project-state-log): reads the head state (`QueryProjectView{summary}`,
 * the use case's web basis) and hands the pure `StateHistoryView` its projection
 * (contracts/stateHistory.ts).
 */
import { useMemo, type ReactNode } from 'react';
import { useProject } from '../hooks/useDeliveryQueries';
import { stateHistoryFrom } from '../contracts/stateHistory';
import { StateHistoryView } from '../components/StateHistoryView';
import { ErrorAlert } from '../components/shared/ErrorAlert';

export function StateHistoryContainer({ projectId }: { projectId: string }): ReactNode {
  const { data: project, isLoading, error } = useProject(projectId);
  const entries = useMemo(() => stateHistoryFrom(project), [project]);
  return (
    <>
      <ErrorAlert error={error} />
      <StateHistoryView entries={entries} loading={isLoading} />
    </>
  );
}
