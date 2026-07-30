package main

// promptsurface_test.go — the prompt-surface ↔ tool-registry gate (F-QA2-23).
//
// DEFECT CLASS. The materialized method-assets prompt surface (the repo-root
// .claude/commands/*.md, .claude/skills/**/*.md, and .claude/agents/*.md — the same
// files method-assets installs into operated repos) names aiarch-state MCP tools the
// agents MUST call (getDraftSlot, putDraftModel, getCritique, ...). The GH design/
// construction jobs install THIS binary at a pinned commit
// (sourcecontrol.StateMcpModulePin). When the prompts gained getCritique while the pin
// predated the tool, every non-round-0 design job bailed: the agent called a tool the
// binary did not register and gave up. Nothing caught the skew.
//
// THIS GATE protects the SOURCE-TREE side: every tool name the prompt surface
// references must exist in the registry this source tree registers (composed verbs in
// tools.go/constructverbs.go + the eligible raw generated catalog). The RUNTIME side —
// the pin actually pointing at a pushed commit that carries the tool — cannot be
// checked without the network; it is covered by the pin discipline documented on
// sourcecontrol.StateMcpModulePin and enforced in shape by
// sourcecontrol.TestStateMcpPinIsFullCommitSHA. Together: when a prompt starts naming
// a new tool, this test forces the tool to exist at HEAD, and the pin doc forces the
// pin bump to a pushed commit that has it.
//
// The INVERSE direction is deliberately NOT required: the registry may (and does)
// carry tools no prompt names. The MCP handshake delivers the full tool list with
// descriptions to the agent at runtime (tools/list), so the raw generated read surface
// is discoverable without being named in prose; prompts only name the tools an agent
// MUST call. A registry-only tool is therefore never a defect — asserting registry ⊆
// prompts would just force prose churn for every generated catalog entry.
//
// KNOWN LIMIT: the gate checks existence, not per-job-mode availability (a critique-
// mode prompt naming a draft-only verb would pass). Mode-scoping skew has not occurred
// in QA; extend here if it ever does.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// registryToolNames is the canonical name set of every tool ANY job mode registers,
// derived from the SAME data buildServer registers from: the composed-verb registry
// (union over all modes) plus the eligible raw generated catalog (rawToolEligible —
// the exact predicate registerRawReadTools applies).
func registryToolNames() map[string]bool {
	names := map[string]bool{}
	for _, v := range composedVerbs(&Session{}) {
		names[v.name] = true
	}
	for _, tool := range projectstate.InternalToolCatalog() {
		if rawToolEligible(tool) {
			names[tool.Name] = true
		}
	}
	return names
}

// promptSurfaceIgnore is the CURATED ignore list for backtick-extracted candidates
// that match the tool-name shape but are prose, not aiarch-state tool references.
// Explicit mcp__aiarch-state__* references are NEVER ignorable. Every entry must
// still occur in the prompt surface — a stale entry fails the test (keep it curated).
var promptSurfaceIgnore = map[string]string{
	"getX":                 "property-operation placeholder in senior-developer.md contract doctrine",
	"setX":                 "property-operation placeholder in senior-developer.md contract doctrine",
	"getDesignHealth":      "render-on-read Design Health VIEW op on the webClient/customer surface, not an aiarch-state agent tool; named in doctrine describing where the live tier runs",
	"submitReviewDecision": "example Client resume-call verb in the-method-architecture SKILL.md dynamic-view doctrine",
}

var (
	// explicitToolRef matches the fully-qualified MCP grant form (agent frontmatter
	// `tools:` lists). Always a tool reference — no shape filter, no ignore list.
	explicitToolRef = regexp.MustCompile(`mcp__aiarch-state__([A-Za-z][A-Za-z0-9]*)`)
	// backtickToken matches a single backtick-quoted lowerCamel token (optionally
	// written as a call, e.g. `publishDraft()`), the form prompt prose uses.
	backtickToken = regexp.MustCompile("`([a-z][A-Za-z0-9]*)(?:\\(\\))?`")
	// toolNameShape is the known-verbs pattern a backtick candidate must match to
	// count as a tool reference: a composed-verb verb prefix followed by an
	// UpperCamel remainder. Composed verbs are hand-named verbs (doctrine rule 3),
	// so new verbs keep this shape; extend the prefix list when a new verb prefix
	// is coined. Raw generated tools (<component><Operation>) are referenced via
	// the explicit mcp__ form, which needs no shape filter.
	toolNameShape = regexp.MustCompile(`^(get|put|set|list|read|write|record|respond|publish|propose|submit|create|update|delete|commit|amend|answer)[A-Z][A-Za-z0-9]*$`)
)

// extractionSentinels are core verbs the prompt surface is KNOWN to reference. If the
// scanner stops finding any of them, the extraction regexes (not the prompts) have
// rotted, and the gate would be silently vacuous — so their absence is a test failure.
var extractionSentinels = []string{
	"getDraftSlot", "getCritique", "getReviewThread", "putDraftModel",
	"publishDraft", "setCritiqueVerdict", "respondToReviewComment", "recordServiceContract",
}

// TestPromptSurfaceToolReferencesExistInRegistry is the F-QA2-23 gate: every
// aiarch-state tool name the materialized prompt surface references must exist in the
// tool registry of THIS source tree. Failure means either (a) a prompt references a
// tool that must be added to tools.go/constructverbs.go (or the generated catalog)
// FIRST, or (b) a new piece of prose happens to match the tool-name shape and belongs
// in promptSurfaceIgnore.
func TestPromptSurfaceToolReferencesExistInRegistry(t *testing.T) {
	registry := registryToolNames()
	refs, ignoredSeen := scanPromptSurface(t, promptSurfaceFiles(t))

	// The gate: prompt-referenced tools must exist in the registry.
	var missing []string
	for name := range refs {
		if !registry[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("prompt surface references aiarch-state tool %q which the current source tree does NOT register\n"+
			"  referenced by:\n    %s\n"+
			"  fix: register the tool (tools.go composed verbs / the generated catalog) BEFORE the prompts ship it,\n"+
			"  then bump sourcecontrol.StateMcpModulePin to a PUSHED commit that carries it;\n"+
			"  or, if this token is prose that merely looks like a tool name, add it to promptSurfaceIgnore",
			name, strings.Join(refs[name], "\n    "))
	}

	// Extraction sanity: the scanner must still find the sentinel verbs, or the gate
	// has gone vacuous.
	for _, s := range extractionSentinels {
		if len(refs[s]) == 0 {
			t.Errorf("extraction sanity: sentinel tool %q was not found anywhere in the prompt surface — "+
				"either the prompts genuinely dropped it (update extractionSentinels) or the extraction regexes rotted", s)
		}
	}

	// Keep the ignore list curated: an entry no prompt contains anymore is stale.
	for token := range promptSurfaceIgnore {
		if !ignoredSeen[token] {
			t.Errorf("promptSurfaceIgnore entry %q no longer occurs in the prompt surface — remove the stale entry", token)
		}
	}
}

// scanPromptSurface extracts every aiarch-state tool reference from the given prompt
// files. It returns refs (tool name -> referencing files, de-duplicated per file) and
// ignoredSeen (the promptSurfaceIgnore entries actually encountered — used to keep the
// ignore list curated).
func scanPromptSurface(t *testing.T, files []string) (map[string][]string, map[string]bool) {
	t.Helper()
	refs := map[string][]string{}    // tool name -> referencing files
	ignoredSeen := map[string]bool{} // ignore-list entries actually encountered
	addRef := func(name, file string) {
		if len(refs[name]) == 0 || refs[name][len(refs[name])-1] != file {
			refs[name] = append(refs[name], file)
		}
	}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read prompt file %s: %v", file, err)
		}
		text := string(body)
		for _, m := range explicitToolRef.FindAllStringSubmatch(text, -1) {
			addRef(m[1], file)
		}
		for _, m := range backtickToken.FindAllStringSubmatch(text, -1) {
			token := m[1]
			if !toolNameShape.MatchString(token) {
				continue
			}
			if _, ok := promptSurfaceIgnore[token]; ok {
				ignoredSeen[token] = true
				continue
			}
			addRef(token, file)
		}
	}
	return refs, ignoredSeen
}

// promptSurfaceFiles returns every materialized prompt file: .claude/commands/*.md,
// .claude/agents/*.md, and every .md under .claude/skills/ (recursively), rooted at
// the repository root (found by walking up from the test's working directory).
func promptSurfaceFiles(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	var files []string
	for _, dir := range []string{"commands", "agents"} {
		matches, err := filepath.Glob(filepath.Join(root, ".claude", dir, "*.md"))
		if err != nil {
			t.Fatalf("glob .claude/%s: %v", dir, err)
		}
		files = append(files, matches...)
	}
	skillsRoot := filepath.Join(root, ".claude", "skills")
	err := filepath.WalkDir(skillsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", skillsRoot, err)
	}
	if len(files) == 0 {
		t.Fatal("found no prompt files under .claude/ — the repo layout moved; update promptSurfaceFiles")
	}
	sort.Strings(files)
	return files
}

// repoRoot walks up from the test working directory to the directory that carries the
// materialized .claude/commands tree (the repository root).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for range 10 {
		if st, err := os.Stat(filepath.Join(dir, ".claude", "commands")); err == nil && st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate the repo root (a directory containing .claude/commands) above the test working directory")
	return ""
}
