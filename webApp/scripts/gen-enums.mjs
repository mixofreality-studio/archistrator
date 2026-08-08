/**
 * gen-enums — extract src/contracts/enums.gen.ts from the server's OpenAPI
 * document's `x-enum-varnames` (the Go const names backing each int/string
 * enum schema).
 *
 * Derivation rule (per enum, independently):
 *   1. Strip the schema's manager-namespace prefix (SystemDesign / ProjectDesign
 *      / Construction / Operations) to get the enum's local Go type name
 *      (e.g. `SystemDesignArtifactKind` -> `ArtifactKind`).
 *   2. Split that local type name into PascalCase words (acronyms like `SDP`
 *      stay one word).
 *   3. For every contiguous run of those words (longest first), check whether
 *      it prefixes the varname. Strip the longest matching run, lowerFirst the
 *      remainder -> candidate app string.
 *
 * This reproduces webApp/src/contracts/enums.ts's hand tables exactly for most
 * enums (KindMission -> "mission", StageDrafting -> "drafting", etc.) but NOT
 * all — some hand tables intentionally collapse ordinals (e.g. PipelinePhase
 * folds Cancelled into "failed") or use a different casing convention (e.g.
 * AutoscalerMode keeps PascalCase "Auto"/"Manual"). Those are enumerated in
 * NON_MECHANICAL below with a reason; for them this generator emits ONLY the
 * raw Go varnames (ordinal-indexed), never a derived app string, so nobody
 * mistakes the derivation for the real (hand-maintained) mapping.
 *
 * Enums whose enum+varnames arrays are byte-identical across multiple manager
 * namespaces (e.g. SystemDesignArtifactKind == ProjectDesignArtifactKind) are
 * folded into ONE logical table — see DEDUPE_GROUPS.
 */
import yaml from 'js-yaml';

const MANAGER_PREFIXES = ['SystemDesign', 'ProjectDesign', 'Construction', 'Operations'];

/** Logical output name for each OAS schema name (post manager-prefix strip),
 * chosen to match the existing hand type name in src/contracts/types.ts where
 * one exists, for minimal import churn in Task 3. */
const OUTPUT_NAMES = {
  // deduped groups (see DEDUPE_GROUPS) key on the first-seen member; either
  // alias resolves to the same logical name.
  SystemDesignArtifactKind: 'ArtifactKind',
  ProjectDesignArtifactKind: 'ArtifactKind',
  SystemDesignReviewDecision: 'ReviewDecision',
  ProjectDesignReviewDecision: 'ReviewDecision',
  SystemDesignSeverity: 'Severity',
  ProjectDesignSeverity: 'Severity',
  // ActiveRole / ActiveStep are byte-identical across both design managers
  // (see DEDUPE_GROUPS) — the drafting sub-step the UI's role line reads.
  SystemDesignActiveRole: 'ActiveRole',
  ProjectDesignActiveRole: 'ActiveRole',
  SystemDesignActiveStep: 'ActiveStep',
  ProjectDesignActiveStep: 'ActiveStep',
  // distinct per-manager enums
  SystemDesignSessionStage: 'SessionStage',
  ProjectDesignSessionStage: 'ProjectSessionStage',
  ProjectDesignSDPDecision: 'SDPDecision',
  SystemDesignPhase: 'ProjectPhase',
  SystemDesignArtifactStage: 'ArtifactStage',
  SystemDesignFailureReason: 'FailureReason',
  SystemDesignActivityType: 'ActivityType',
  SystemDesignActivityBuildStatus: 'ActivityBuildStatus',
  SystemDesignActivityConstructionPhase: 'ActivityConstructionPhase',
  SystemDesignCICheckState: 'CICheckState',
  SystemDesignTestingVariant: 'TestingVariant',
  ConstructionConstructionStage: 'ConstructionStage',
  ConstructionOverrideKind: 'OverrideKind',
  ConstructionPhaseDecision: 'PhaseDecision',
  ConstructionPipelinePhase: 'PipelinePhase',
  OperationsAutoscaleAction: 'AutoscaleAction',
  OperationsAutoscalerMode: 'AutoscalerMode',
  OperationsDesiredStateReason: 'DesiredStateReason',
  OperationsHealthState: 'HealthState',
  OperationsPatchKind: 'PatchKind',
  OperationsRuntimeStatusSeam: 'RuntimeStatusSeam',
  // episode capture-seam enums (byte-identical across all three managers —
  // see DEDUPE_GROUPS); new/unwired, no hand table to verify against.
  ConstructionEpisodeKind: 'EpisodeKind',
  ProjectDesignEpisodeKind: 'EpisodeKind',
  SystemDesignEpisodeKind: 'EpisodeKind',
  ConstructionEpisodeOutcome: 'EpisodeOutcome',
  ProjectDesignEpisodeOutcome: 'EpisodeOutcome',
  SystemDesignEpisodeOutcome: 'EpisodeOutcome',
};

/** Groups of OAS schema names known (verified by one-off comparison against
 * server/*.go iota order) to carry byte-identical enum + x-enum-varnames
 * arrays. Only the first member of each group is emitted; the rest are
 * asserted (at generation time) to still match, so drift trips a build error
 * instead of silently forking the tables. */
const DEDUPE_GROUPS = [
  ['SystemDesignArtifactKind', 'ProjectDesignArtifactKind'],
  ['SystemDesignReviewDecision', 'ProjectDesignReviewDecision'],
  ['SystemDesignSeverity', 'ProjectDesignSeverity'],
  ['SystemDesignActiveRole', 'ProjectDesignActiveRole'],
  ['SystemDesignActiveStep', 'ProjectDesignActiveStep'],
  ['ConstructionEpisodeKind', 'ProjectDesignEpisodeKind', 'SystemDesignEpisodeKind'],
  ['ConstructionEpisodeOutcome', 'ProjectDesignEpisodeOutcome', 'SystemDesignEpisodeOutcome'],
];

/** Logical output names for which the mechanical derivation does NOT
 * reproduce webApp/src/contracts/enums.ts (or adapters.ts) exactly. Reason is
 * emitted as a header comment. These get raw-varname tables only. */
const NON_MECHANICAL = {
  ProjectSessionStage:
    'StageAssemblingSDP derives to "assemblingSDP" (lowerFirst only lowercases the ' +
    'leading letter); the hand table uses "assemblingSdp". Casing convention diff, not a bug.',
  PipelinePhase:
    'Mechanical derivation gives ordinal 5 (PipelineCancelled) -> "cancelled", but ' +
    'enums.ts pipelinePhaseFromOrdinal deliberately folds ordinal 5 into the same app ' +
    'value as ordinal 4 ("failed") — "the app has no distinct cancelled state". Deliberate ' +
    'product simplification, not a bug.',
  AutoscalerMode:
    'Mechanical derivation gives lowerCamel ("auto"/"manual"), but enums.ts ' +
    'autoscalerModeFromOrdinal returns PascalCase ("Auto"/"Manual"/"Unknown"). Casing ' +
    'convention diff, not a bug.',
  RuntimeStatusSeam:
    'Mechanical derivation gives ("unknown"/"pending"/"healthy"/"degraded"/"withdrawn"), ' +
    'but enums.ts runtimePhaseFromOrdinal returns PascalCase AND renames ordinal 2 ' +
    '(RuntimeStatusHealthy) to "Running". Casing + semantic rename, not a bug.',
  ActivityBuildStatus:
    'Mechanical derivation gives ("inConstruction"/"inReview"/"integrated"/"failed"), but ' +
    'adapters.ts buildStatusRowFromOrdinal returns kebab-case ("in-construction"/"in-review"/' +
    '"integrated") and folds ordinal 3 (BuildFailed) into "in-construction" ("no terminal-fail ' +
    'row state"). Casing convention + deliberate collapse, not a bug.',
  CICheckState:
    'Mechanical derivation gives ("pending"/"success"/"failure"), but enums.ts ' +
    'ciStatusFromOrdinal returns ("in_progress"/"success"/"failed") — different words ' +
    'entirely for 2 of 3 members. Semantic naming diff, not a bug.',
  TestingVariant:
    'Local type name is "TestingVariant" but the Go consts use the short prefix "Test" ' +
    '(TestVariantPlan, ...) — "Test" is not a whole-word run of "TestingVariant", so no ' +
    'candidate strips and the mechanical derivation falls through to the full lowerFirst ' +
    'varname ("testVariantPlan"). enums.ts testingVariantFromOrdinal instead returns short ' +
    'forms ("plan"/"harness"/"perf"/"systemTest"/"qaProcess"). Not mechanically derivable.',
};

/** Logical output names with no existing hand-table counterpart to verify
 * against (new/unwired enums). The mechanical derivation is emitted as
 * best-effort — internally consistent, but unverified against product code. */
const UNVERIFIED_MECHANICAL = new Set([
  'FailureReason',
  'ActivityConstructionPhase',
  'DesiredStateReason',
  'HealthState',
  'PatchKind',
  'EpisodeKind',
  'EpisodeOutcome',
]);

function splitWords(name) {
  return name.match(/[A-Z]+(?=[A-Z][a-z0-9]|$)|[A-Z][a-z0-9]*|[a-z0-9]+/g) ?? [name];
}

function contiguousRuns(words) {
  const runs = new Set();
  for (let i = 0; i < words.length; i += 1) {
    for (let j = i + 1; j <= words.length; j += 1) {
      runs.add(words.slice(i, j).join(''));
    }
  }
  return [...runs].sort((a, b) => b.length - a.length);
}

function lowerFirst(s) {
  return s.length === 0 ? s : s.charAt(0).toLowerCase() + s.slice(1);
}

function deriveAppStrings(localName, varnames) {
  const candidates = contiguousRuns(splitWords(localName));
  return varnames.map((v) => {
    const hit = candidates.find((c) => v.startsWith(c) && v.length > c.length);
    return lowerFirst(hit ? v.slice(hit.length) : v);
  });
}

function stripManagerPrefix(schemaName) {
  const prefix = MANAGER_PREFIXES.find((p) => schemaName.startsWith(p));
  return prefix === undefined ? schemaName : schemaName.slice(prefix.length);
}

function tsIdent(varnames, quote = "'") {
  return varnames.map((v) => `${quote}${v}${quote}`).join(', ');
}

function emitConstArray(name, values, quote = "'") {
  return `export const ${name} = [${tsIdent(values, quote)}] as const;\n`;
}

/**
 * @param {string} oasSource raw openapi.yaml text (pre operationId-dedup —
 *   the enum schemas are unaffected by that rewrite)
 * @returns {string} the enums.gen.ts file contents
 */
export function generateEnumsModule(oasSource) {
  const doc = yaml.load(oasSource);
  const schemas = doc?.components?.schemas ?? {};

  const enumSchemas = Object.entries(schemas)
    .filter(([, s]) => s && typeof s === 'object' && Array.isArray(s['x-enum-varnames']))
    .map(([name, s]) => ({
      name,
      local: stripManagerPrefix(name),
      values: s.enum,
      varnames: s['x-enum-varnames'],
      isString: s.type === 'string',
    }))
    .sort((a, b) => a.name.localeCompare(b.name));

  // Fold dedupe groups into one entry (emitted under the first member), after
  // asserting every member of the group is still byte-identical.
  const byName = new Map(enumSchemas.map((e) => [e.name, e]));
  const suppressed = new Set();
  for (const group of DEDUPE_GROUPS) {
    const [first, ...rest] = group;
    const base = byName.get(first);
    if (base === undefined) {
      throw new Error(`gen-enums: dedupe group member missing from OAS: ${first}`);
    }
    for (const other of rest) {
      const cand = byName.get(other);
      if (cand === undefined) {
        throw new Error(`gen-enums: dedupe group member missing from OAS: ${other}`);
      }
      const same =
        JSON.stringify(cand.values) === JSON.stringify(base.values) &&
        JSON.stringify(cand.varnames) === JSON.stringify(base.varnames);
      if (!same) {
        throw new Error(
          `gen-enums: ${first} and ${other} were folded as duplicates but no longer match — ` +
            `split them back out in gen-enums.mjs (DEDUPE_GROUPS/OUTPUT_NAMES) and re-verify ` +
            `against src/contracts/enums.ts.`
        );
      }
      suppressed.add(other);
    }
  }

  const logical = enumSchemas
    .filter((e) => !suppressed.has(e.name))
    .map((e) => {
      const outputName = OUTPUT_NAMES[e.name];
      if (outputName === undefined) {
        throw new Error(
          `gen-enums: no OUTPUT_NAMES entry for new enum schema ${e.name} — add one ` +
            `(and classify it in NON_MECHANICAL / UNVERIFIED_MECHANICAL as appropriate).`
        );
      }
      const group = DEDUPE_GROUPS.find((g) => g[0] === e.name);
      return { ...e, outputName, sources: group ?? [e.name] };
    })
    .sort((a, b) => a.outputName.localeCompare(b.outputName));

  const blocks = logical.map((e) => emitBlock(e));

  const header = `// Code generated by gen-enums.mjs (webApp/scripts/gen-enums.mjs, invoked via
// npm run gen:api) from api/openapi.yaml x-enum-varnames. DO NOT EDIT.
//
// Every OpenAPI component schema carrying x-enum-varnames becomes one block
// below. Enums byte-identical across manager namespaces (e.g.
// SystemDesignArtifactKind / ProjectDesignArtifactKind) are folded into one
// logical table — see the "Sources:" line on each block. The dedup guard
// (DEDUPE_GROUPS, asserted byte-identical at generation time) lives in
// gen-enums.mjs, not here.
//
// Where the Go varname -> app-string derivation (strip the shared type-name
// prefix, lowerFirst the remainder) reproduces src/contracts/enums.ts's hand
// tables exactly, this file exports both the raw varnames and the derived
// app strings. Where it does NOT (a deliberate collapse/casing choice in the
// hand code, not a bug — see the NOTE on the block), this file exports ONLY
// the raw Go varnames; keep a small hand mapping for those.
`;

  return header + '\n' + blocks.join('\n');
}

function screamingSnake(name) {
  return name
    .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1_$2')
    .toUpperCase();
}

function emitBlock(e) {
  const SNAKE = screamingSnake(e.outputName);
  const sourcesLine = `// Sources: ${e.sources.join(', ')}${e.sources.length > 1 ? ' (identical; folded)' : ''}`;
  const out = [
    `// --- ${e.outputName} ${'-'.repeat(Math.max(0, 68 - e.outputName.length))}`,
    sourcesLine,
  ];

  if (e.isString) {
    // String-valued enum: the wire value IS the app string already.
    out.push(
      '// String-valued enum — the wire value is already the app string (no ordinal indirection).'
    );
    out.push(emitConstArray(`${SNAKE}_VALUES`, e.values));
    out.push(`export type ${e.outputName} = (typeof ${SNAKE}_VALUES)[number];\n`);
    out.push(emitConstArray(`${SNAKE}_GO_VARNAMES`, e.varnames));
    return out.join('\n');
  }

  out.push(emitConstArray(`${SNAKE}_GO_VARNAMES`, e.varnames));
  out.push(`export type ${e.outputName}GoVarname = (typeof ${SNAKE}_GO_VARNAMES)[number];\n`);
  out.push(
    `export const ${SNAKE}_ORDINAL_TO_GO_VARNAME: readonly ${e.outputName}GoVarname[] = ${SNAKE}_GO_VARNAMES;\n`
  );

  const nonMechanicalReason = NON_MECHANICAL[e.outputName];
  if (nonMechanicalReason !== undefined) {
    out.push(`// NOT mechanically derivable to an app string: ${nonMechanicalReason}`);
    return out.join('\n');
  }

  const appStrings = deriveAppStrings(e.local, e.varnames);
  if (UNVERIFIED_MECHANICAL.has(e.outputName)) {
    out.push(
      '// NOTE: no existing hand-authored app-string table to verify against (unwired/new ' +
        'enum) — derived mechanically, unverified.'
    );
  }
  out.push(emitConstArray(`${SNAKE}_APP_STRINGS`, appStrings));
  out.push(`export type ${e.outputName} = (typeof ${SNAKE}_APP_STRINGS)[number];\n`);
  out.push(
    `export const ${SNAKE}_ORDINAL_TO_APP: readonly ${e.outputName}[] = ${SNAKE}_APP_STRINGS;\n`
  );
  const entries = appStrings.map((s, i) => `  ${s}: ${i},`).join('\n');
  out.push(
    `export const ${SNAKE}_APP_TO_ORDINAL: Readonly<Record<${e.outputName}, number>> = {\n${entries}\n};`
  );
  return out.join('\n');
}
