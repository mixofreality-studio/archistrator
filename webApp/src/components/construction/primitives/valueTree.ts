/**
 * A bound test value, read as a TREE a reviewer can scan — pure (no React).
 *
 * The scenario plan binds every input, expectation and probe as a JSON/text
 * string (TestArgView.value: "concrete value as JSON/text, so a harness can emit
 * runnable code"). That string is the harness's input, not the reviewer's: shown
 * verbatim it is a raw JSON dump in a code font. `readValue` parses it once into
 * labelled fields, lists and plain scalars; StructuredValue.tsx draws the tree.
 *
 * Anything that is not JSON (a bare word, a path, a sentence) is plain text — the
 * same thing the reviewer would read, minus the quotes.
 */

export type ValueTree =
  | { kind: 'text'; text: string }
  | { kind: 'scalar'; text: string }
  | { kind: 'empty' }
  | { kind: 'list'; items: ValueTree[] }
  | { kind: 'record'; fields: { key: string; value: ValueTree }[] };

function fromJson(v: unknown): ValueTree {
  if (v === null || v === undefined) return { kind: 'empty' };
  if (typeof v === 'string') return v.length === 0 ? { kind: 'empty' } : { kind: 'text', text: v };
  if (typeof v === 'number' || typeof v === 'boolean') {
    return { kind: 'scalar', text: String(v) };
  }
  if (Array.isArray(v)) {
    return v.length === 0 ? { kind: 'empty' } : { kind: 'list', items: v.map(fromJson) };
  }
  if (typeof v === 'object') {
    const fields = Object.entries(v as Record<string, unknown>).map(([key, value]) => ({
      key,
      value: fromJson(value),
    }));
    return fields.length === 0 ? { kind: 'empty' } : { kind: 'record', fields };
  }
  // bigint / symbol / function cannot come out of JSON.parse; said, not dumped.
  return { kind: 'text', text: typeof v };
}

/** Parse a bound value. JSON becomes a tree; anything else is one line of text. */
export function readValue(raw: string): ValueTree {
  const s = raw.trim();
  if (s.length === 0) return { kind: 'empty' };
  const first = s[0];
  const looksJson =
    first === '{' ||
    first === '[' ||
    first === '"' ||
    s === 'null' ||
    s === 'true' ||
    s === 'false' ||
    /^-?\d+(\.\d+)?([eE][+-]?\d+)?$/.test(s);
  if (!looksJson) return { kind: 'text', text: s };
  try {
    return fromJson(JSON.parse(s) as unknown);
  } catch {
    return { kind: 'text', text: s };
  }
}

/** Whether a tree reads on one line (a scalar, text or empty) — no nesting to lay out. */
export function isInline(v: ValueTree): boolean {
  return v.kind === 'text' || v.kind === 'scalar' || v.kind === 'empty';
}

export function summarize(v: ValueTree): string {
  switch (v.kind) {
    case 'empty':
      return 'none';
    case 'text':
    case 'scalar':
      return v.text;
    case 'list':
      return v.items.map(summarize).join(', ');
    case 'record':
      return v.fields
        .map(
          (f) => `${f.key}: ${isInline(f.value) ? summarize(f.value) : `(${summarize(f.value)})`}`
        )
        .join(' · ');
  }
}

/**
 * A bound value as ONE readable line, for a surface that takes plain text (the
 * call chain's step caption): `notes: Amendment… · comments: none`, never the
 * `{"notes":…}` JSON the harness reads.
 */
export function valueSummary(raw: string): string {
  return summarize(readValue(raw));
}
