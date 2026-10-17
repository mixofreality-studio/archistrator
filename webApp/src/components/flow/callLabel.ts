/**
 * Split an authored call label — `Op(args) — what it does`, the shape the
 * dynamic views record — into its parts, so the caption can show the operation
 * NAME as a short identifier, the gloss as prose and the arguments as a quiet
 * "passes …" line, instead of one monospace signature string that reads as code.
 *
 * Tolerant by design: a label that is not call-shaped (no leading identifier
 * followed by a balanced parenthesis) comes back as `{ op: '', args: '', gloss:
 * label }` — prose, rendered as-is. Pure and React-free (`node --test`-loadable).
 */
export interface CallLabelParts {
  /** The operation name, e.g. `DecideSdpOption` ('' when the label is not call-shaped). */
  op: string;
  /** The text between the outer parentheses, trimmed ('' for none). */
  args: string;
  /** Whatever follows the call — its leading dash/colon stripped — or the whole
   *  label when it is not call-shaped. */
  gloss: string;
}

const IDENT = /^[A-Za-z_][\w.]*$/;

export function splitCallLabel(label: string): CallLabelParts {
  const open = label.indexOf('(');
  const op = open > 0 ? label.slice(0, open).trim() : '';
  if (open <= 0 || !IDENT.test(op)) return { op: '', args: '', gloss: label.trim() };
  let depth = 0;
  let close = -1;
  for (let i = open; i < label.length; i++) {
    const ch = label[i];
    if (ch === '(') depth++;
    else if (ch === ')') {
      depth--;
      if (depth === 0) {
        close = i;
        break;
      }
    }
  }
  if (close < 0) return { op: '', args: '', gloss: label.trim() };
  const args = label.slice(open + 1, close).trim();
  const gloss = label
    .slice(close + 1)
    .replace(/^\s*[—–:-]+\s*/, '')
    .trim();
  return { op, args, gloss };
}
