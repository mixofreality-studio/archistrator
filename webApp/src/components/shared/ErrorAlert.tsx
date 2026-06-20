import type { ReactNode } from 'react';
import Alert from '@mui/material/Alert';
import { ApiError } from '../../api/client';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

export function ErrorAlert({ error }: { error: Error | null }): ReactNode {
  if (error === null) return null;
  const detail =
    error instanceof ApiError
      ? `${error.message} (${error.code}, HTTP ${String(error.status)})`
      : error.message;
  return (
    <Alert data-testid={UI_IDENTIFIERS.Common.ERROR_ALERT} severity="error" sx={{ my: 1 }}>
      {detail}
    </Alert>
  );
}
