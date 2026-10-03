/**
 * The ONE read that is not an op: a test run's files (spec §6.3).
 *
 * `GET /api/v1/delivery/test-run/{run}/file?path=` streams BYTES — a Playwright
 * video, a trace zip, the venue's `go test -json` output — for the browser to
 * play, for the self-hosted trace viewer to open, and for the results panel to
 * filter one scenario's output out of. A byte stream has no place in the OpsClient
 * catalogue (`ops.gen.ts` is generated from the OAS's JSON operations, and the MCP
 * transport could not carry a video either), so the SPA addresses it by URL:
 * `<video src>` and `window.open` fetch it themselves, and this module fetches
 * the text stream for the Output tab. It is the api layer, where the bare
 * `fetch` the rest of the app is forbidden is the point (eslint
 * `no-restricted-globals`, src/api/** override).
 *
 * Auth is the edge's (session cookie, same origin), exactly as every op call:
 * the request carries no token of its own.
 */
import { ApiError } from '../contracts/errors';
import { config } from '../utilities/config.ts';

/** The file endpoint's URL for one artifact path of one run. */
export function testRunFileUrl(runId: string, path: string): string {
  return `${config.apiBaseUrl}/api/v1/delivery/test-run/${encodeURIComponent(runId)}/file?path=${encodeURIComponent(path)}`;
}

/** The file as text — the `go test -json` stream the Output tab filters. */
export async function fetchTestRunFileText(url: string): Promise<string> {
  const res = await fetch(url, { credentials: 'same-origin' });
  if (!res.ok) {
    throw new ApiError(
      res.status,
      'test_run_file',
      `test-run file ${url}: HTTP ${String(res.status)}`
    );
  }
  return res.text();
}
