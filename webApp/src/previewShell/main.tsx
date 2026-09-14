/**
 * PREVIEW shell entry (design-renderer-data.md §2′.1, §2′.5): the SAME <App/> the
 * browser SPA boots (main.tsx), in preview mode with fixture data. It differs from
 * main.tsx in exactly three seams:
 *
 *   - the transport: fixtureOpsClient over the chosen state's fixture, marked
 *     `transport: 'fixture'`, instead of restOpsClient;
 *   - the history: a memory history seeded at the fixture's route, so a preview
 *     never reads or writes the page URL, instead of browser history;
 *   - the network and navigation: installPreviewGuards (the FIRST import below)
 *     makes every other request throw and refuses any second tab or window (a
 *     nested preview), and PreviewAlarm shows every miss and refusal.
 *
 * `?screen=<id>&state=<id>` picks the fixture (fixtureRegistry.ts); an unknown or
 * missing pair renders an honest error page, never a guess. The QueryClient keeps
 * the SPA's staleTime but never retries, so an error state shows at once.
 *
 * Built by vite.preview.config.ts into dist-preview/, never dist/.
 */
// FIRST: nothing may evaluate before the guards are in place.
import './installPreviewGuards';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory } from '@tanstack/react-router';
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
import '../index.css';
import App from '../App';
import { fixtureOpsClient } from '../api/fixtureOps';
import { OpsClientProvider } from '../api/opsContext';
import { fetchCapabilities } from '../hooks/useCapabilities';
import { createAppRouter } from '../routes/router';
import { PreviewAlarm } from './PreviewAlarm';
import { PreviewErrorPage } from './PreviewErrorPage';
import { resolvePreviewState, statesFromModules } from './fixtureRegistry';
import { raisePreviewIncident } from './previewIncidents';

const modules: Record<string, unknown> = import.meta.glob('@preview-fixtures/web-client/*/*.json', {
  eager: true,
  import: 'default',
});
const resolution = resolvePreviewState(window.location.search, statesFromModules(modules));

const rootElement = document.getElementById('root');
if (rootElement === null) {
  throw new Error('Root element not found');
}
const root = createRoot(rootElement);

if (resolution.kind !== 'ok') {
  document.title = 'archistrator · preview · no such state';
  root.render(<PreviewErrorPage resolution={resolution} />);
} else {
  const { screen, state, fixture } = resolution;
  document.title = `Preview · ${screen} · ${state} · fixture data`;

  const ops = fixtureOpsClient(fixture.ops, {
    onMiss: (opId) => {
      raisePreviewIncident({ kind: 'fixture-miss', detail: opId });
    },
  });
  const router = createAppRouter(createMemoryHistory({ initialEntries: [fixture.route] }), {
    fetchCapabilities: () => fetchCapabilities(ops),
  });
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { staleTime: 1000 * 30, retry: false, refetchOnWindowFocus: false },
    },
  });

  root.render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <OpsClientProvider value={{ ops, transport: 'fixture' }}>
          <App router={router} />
        </OpsClientProvider>
      </QueryClientProvider>
      <PreviewAlarm />
    </StrictMode>
  );
}
