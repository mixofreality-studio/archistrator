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
 * looks for the literal token `<Namespace>.<key>` anywhere under `webApp/src` or
 * `uitests/tests`. It is a textual match and nothing more:
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
 * THE ALIAS HOP. uitests mostly does not name ids by their own key:
 * `uitests/tests/support/testids.ts` re-exports them flat and camelCase
 * (`constructionListRow: UI_IDENTIFIERS.Construction.listRow`) and the specs say
 * `TESTID.constructionListRow`. So testids.ts is read as an ALIAS MAP, never as a
 * reference site: an id is referenced through it only when some spec uses the
 * alias. Counting the alias definition itself would make this guard blind;
 * ignoring the map would make it a false-positive machine.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';

const HERE = import.meta.dirname;
const WEBAPP_SRC = join(HERE, '..', '..');
const UITESTS = join(WEBAPP_SRC, '..', '..', 'uitests', 'tests');
const IDS_FILE = join(HERE, 'UIIdentifiers.ts');
const ALIAS_FILE = join(UITESTS, 'support', 'testids.ts');

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

void test('every UI_IDENTIFIERS leaf is referenced by the SPA or a uitests spec', () => {
  const idsSrc = readFileSync(IDS_FILE, 'utf8');
  const aliasSrc = readFileSync(ALIAS_FILE, 'utf8');

  // The alias map: `Namespace.key` → the TESTID names the specs would say.
  const aliases = new Map<string, string[]>();
  for (const m of aliasSrc.matchAll(/(\w+):[^\n]*?UI_IDENTIFIERS\.(\w+)\.(\w+)/g)) {
    const id = `${String(m[2])}.${String(m[3])}`;
    aliases.set(id, [...(aliases.get(id) ?? []), String(m[1])]);
  }

  // Everything that may REFERENCE an id. The constant's own file and the alias
  // map are declarations, not references; THIS file names ids in its prose and
  // would otherwise vouch for them.
  const corpus = [...sources(WEBAPP_SRC), ...sources(UITESTS)]
    .filter((f) => f !== IDS_FILE && f !== ALIAS_FILE && f !== import.meta.filename)
    .map((f) => readFileSync(f, 'utf8'))
    .join('\n');

  const orphans = leafKeys(idsSrc).filter(
    (id) =>
      !Object.hasOwn(WAIVERS, id) &&
      !corpus.includes(id) &&
      !(aliases.get(id) ?? []).some((a) => corpus.includes(`TESTID.${a}`))
  );

  assert.deepEqual(
    orphans,
    [],
    `${String(orphans.length)} UI_IDENTIFIERS leaf(s) are declared but referenced nowhere under ` +
      `${relative(dirname(WEBAPP_SRC), WEBAPP_SRC)} or uitests/tests. Delete the id if its UI is ` +
      `gone; if its element is still rendered, it LOST its hook — put the hook back. Only an id ` +
      `referenced in a way a textual scan cannot see belongs in WAIVERS, with its reason:\n  ` +
      orphans.join('\n  ')
  );
});
