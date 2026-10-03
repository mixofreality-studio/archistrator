/// <reference types="node" />
/**
 * THE ORPHANED-IDENTIFIER GUARD — every leaf key in UI_IDENTIFIERS is referenced
 * by something, or this test names it.
 *
 * The `Activity` block states the doctrine in its own comment: "an id declared
 * here and never placed on an element is the same failure as an id asserted by a
 * test and never declared." Nothing enforced it. A deletion wave retires a
 * surface, the ids it placed stay behind, and they then read as live, selectable
 * hooks that nothing can select — until a reviewer happens to notice two of them.
 *
 * WHAT "REFERENCED" MEANS HERE, EXACTLY. This test reads the SOURCES AS TEXT and
 * looks for the literal token `<Namespace>.<key>` anywhere under `webApp/src`.
 * It is a textual match and nothing more:
 *   • it cannot see a key composed at runtime — `UI_IDENTIFIERS[ns][key]`, or a
 *     name assembled from a variable, reads here as an orphan (WAIVERS is for
 *     exactly that);
 *   • it does not prove the referencing code is REACHABLE — an id named only by
 *     a module nothing imports still counts as referenced;
 *   • it does not prove an element carries the id, only that the constant is named.
 * It is worth gating anyway, because the failure it does catch is the one that
 * keeps happening, and the opposite failure it cannot catch — a live element that
 * LOST its hook — is only ever found by triaging the list this test prints.
 *
 * ONLY THE SPA VOUCHES. The hand-written uitests that used to count as a reference
 * site (through their flat TESTID alias map) are gone with the system test plan
 * (deterministic component testing, spec §7): the generated scenario specs select by
 * the testid strings the scenario bindings carry, not by these constants. An id a
 * test named but no element ever carried was always an orphan; now it reads as one.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';

const HERE = import.meta.dirname;
const WEBAPP_SRC = join(HERE, '..', '..');
const IDS_FILE = join(HERE, 'UIIdentifiers.ts');

/** `Namespace.key` → why it may be declared with no textual reference. */
const WAIVERS: Readonly<Record<string, string>> = {};

/** Every `.ts`/`.tsx` under `dir`. */
function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name);
    return e.isDirectory() ? sources(p) : /\.tsx?$/.test(e.name) ? [p] : [];
  });
}

/** Every leaf key of the constant tree, as `Namespace.key`, read off the source. */
function leafKeys(src: string): string[] {
  const out: string[] = [];
  let ns: string | undefined;
  for (const line of src.split('\n')) {
    const open = /^ {2}(\w+): \{$/.exec(line);
    if (open?.[1] !== undefined) ns = open[1];
    else if (/^ {2}\},?$/.test(line)) ns = undefined;
    else {
      const leaf = /^ {4}(\w+):/.exec(line);
      if (leaf?.[1] !== undefined && ns !== undefined) out.push(`${ns}.${leaf[1]}`);
    }
  }
  return out;
}

void test('every UI_IDENTIFIERS leaf is referenced by the SPA', () => {
  const idsSrc = readFileSync(IDS_FILE, 'utf8');

  // Everything that may REFERENCE an id. The constant's own file is a declaration,
  // not a reference; THIS file names ids in its prose and would otherwise vouch
  // for them.
  const corpus = sources(WEBAPP_SRC)
    .filter((f) => f !== IDS_FILE && f !== import.meta.filename)
    .map((f) => readFileSync(f, 'utf8'))
    .join('\n');

  const orphans = leafKeys(idsSrc).filter(
    (id) => !Object.hasOwn(WAIVERS, id) && !corpus.includes(id)
  );

  assert.deepEqual(
    orphans,
    [],
    `${String(orphans.length)} UI_IDENTIFIERS leaf(s) are declared but referenced nowhere under ` +
      `${relative(dirname(WEBAPP_SRC), WEBAPP_SRC)}. Delete the id if its UI is ` +
      `gone; if its element is still rendered, it LOST its hook — put the hook back. Only an id ` +
      `referenced in a way a textual scan cannot see belongs in WAIVERS, with its reason:\n  ` +
      orphans.join('\n  ')
  );
});
