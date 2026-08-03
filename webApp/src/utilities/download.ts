/**
 * Client-side "download a generated text file" helper — a Blob + a transient
 * `<a download>` anchor, the standard no-server-round-trip pattern. Used by the
 * episodes Export menu (JSON/CSV) so the export never needs a server endpoint of
 * its own (there is no `exportEpisodes` op — cut per the 2026-08-02 facet
 * ruling); any future client-assembled export can reuse this rather than
 * re-inventing the blob/anchor dance.
 */
export function downloadTextFile(filename: string, content: string, mimeType: string): void {
  const blob = new Blob([content], { type: mimeType });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  document.body.removeChild(anchor);
  URL.revokeObjectURL(url);
}
