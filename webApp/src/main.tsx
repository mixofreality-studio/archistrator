import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '@fontsource/space-grotesk/400.css';
import '@fontsource/space-grotesk/500.css';
import '@fontsource/space-grotesk/600.css';
import '@fontsource/space-grotesk/700.css';
import '@fontsource/space-mono/400.css';
import '@fontsource/space-mono/700.css';
import '@fontsource/inter/400.css';
import '@fontsource/inter/500.css';
import '@fontsource/inter/600.css';
import '@fontsource/inter/700.css';
import '@fontsource/playfair-display/700.css';
import '@fontsource/playfair-display/800.css';
import '@fontsource/playfair-display/900.css';
import '@fontsource/jetbrains-mono/400.css';
import '@fontsource/jetbrains-mono/500.css';
import '@fontsource/jetbrains-mono/700.css';
import { createBrowserHistory } from '@tanstack/react-router';
import { ApiError } from './contracts/errors';
import { restOps } from './api/client';
import { OpsClientProvider } from './api/opsContext';
import { fetchCapabilities } from './hooks/useCapabilities';
import { createAppRouter } from './routes/router';
import './index.css';
import App from './App';

const ops = restOps;

// Browser history is what createRouter defaulted to when the router was a module
// singleton; the factory takes it explicitly so the preview shell can pass memory
// history instead (routes/router.tsx).
const router = createAppRouter(createBrowserHistory(), {
  fetchCapabilities: () => fetchCapabilities(ops),
});

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 30,
      retry: (failureCount: number, error: Error): boolean => {
        // Don't retry auth failures or "not started yet" 404s.
        if (error instanceof ApiError && (error.status === 401 || error.status === 404)) {
          return false;
        }
        return failureCount < 1;
      },
    },
  },
});

const rootElement = document.getElementById('root');
if (rootElement === null) {
  throw new Error('Root element not found');
}

createRoot(rootElement).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <OpsClientProvider value={{ ops, transport: 'rest' }}>
        <App router={router} />
      </OpsClientProvider>
    </QueryClientProvider>
  </StrictMode>
);
