package main

// session.go holds the AMBIENT SESSION CONTEXT + the working-tree read/write core of
// the aiarch project-state MCP server. Per the agentic-managers spec (§Construction
// application) this binary IS ProjectStateAccess code operating DIRECTLY on the
// checked-out working tree (the session branch) — .aiarch/state/project.json + the
// research corpus files. It never talks to a git remote for reads; it decodes/encodes
// project.json through the SAME projectstate codec the server uses on read-back, so a
// draft that survives putDraftModel is byte-for-byte what the server would accept.
//
// The session context (project id, artifact kind, job mode, target branch, state root)
// is resolved ONCE from process env — the workflow template passes these as env on the
// MCP server process, templated from the design-job dispatch inputs. The agent NEVER
// supplies a slot number or an artifact kind: the ambient kind fixes the slot.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// Job-mode values — the discriminator the workflow template passes as AIARCH_JOB_MODE,
// mirroring the design workflow's job_mode dispatch input. Draft mode exposes the full
// authoring tool set (incl. putDraftModel); critique mode exposes read verbs +
// setCritiqueVerdict and NEVER putDraftModel; answer mode (question-comments) exposes read
// verbs + respondToReviewComment and NEITHER putDraftModel NOR setCritiqueVerdict.
const (
	jobModeDraft    = "draft"
	jobModeCritique = "critique"
	jobModeAnswer   = "answer"
	// jobModeConstruct is the Phase-3 CONSTRUCTION job mode. Unlike the design modes it
	// is NOT keyed to a Phase-1/2 artifact kind + slot: a construction task works on a
	// COMPONENT + ACTIVITY (AIARCH_COMPONENT_ID / AIARCH_ACTIVITY_ID), writing the flat
	// construction targets (.serviceContracts / .phaseArtifacts / .testingState) through
	// the composed construction verbs (constructverbs.go). AIARCH_ARTIFACT_KIND is
	// therefore OPTIONAL under this mode.
	jobModeConstruct = "construct"
)

// Ambient-context env var names. The workflow template stamps these onto the MCP
// server process from the dispatch inputs (artifact_kind, job_mode, target_branch) and
// the per-project repo (project id = repo name, per the name-as-identity model). The
// agent supplies none of them.
const (
	envProjectID    = "AIARCH_PROJECT_ID"
	envArtifactKind = "AIARCH_ARTIFACT_KIND"
	envJobMode      = "AIARCH_JOB_MODE"
	envTargetBranch = "AIARCH_TARGET_BRANCH"
	envStateRoot    = "AIARCH_STATE_ROOT"
	// envComponentID / envActivityID are the CONSTRUCTION ambient context (job mode
	// "construct"), the analogue of AIARCH_ARTIFACT_KIND for the design modes: a
	// construction task works on a component + activity, not a Phase-1/2 artifact slot.
	// The construct workflow stamps them from its component_id / activity_id dispatch
	// inputs. The agent supplies neither.
	envComponentID = "AIARCH_COMPONENT_ID"
	envActivityID  = "AIARCH_ACTIVITY_ID"
)

// statePathPrefix + projectFile mirror the projectstate git substrate's reserved
// subtree (.aiarch/state/project.json). The binary operates on the same on-disk paths
// the server commits, so a draft it writes is exactly what the server reads back.
const (
	statePathPrefix = ".aiarch/state"
	projectFile     = "project.json"
)

// Session is the resolved ambient context + the runtime state of one MCP server
// process. It is created once from env (newSessionFromEnv) and threaded into every
// tool handler. The runtime flags (wroteState, published) enforce publishDraft's
// exactly-once + no-empty-publish semantics.
type Session struct {
	ProjectID    projectstate.ProjectID
	Kind         projectstate.ArtifactKind
	Mode         string
	StateRoot    string
	TargetBranch string

	// ComponentID / ActivityID are the CONSTRUCTION ambient context (Mode == construct).
	// They fix which component's contract / which activity's phase artifact the composed
	// construction verbs write, so the agent never chooses a target. Empty in the design
	// modes (which are keyed by Kind instead).
	ComponentID string
	ActivityID  string

	// wroteState is set by any state-mutating verb (putDraftModel / setCritiqueVerdict /
	// respondToReviewComment) so publishDraft can refuse a no-op publish (nothing drafted
	// AND a clean tree) — the F17c "job went green having committed nothing" guard.
	wroteState bool
	// published latches after the first successful publishDraft so a second call is a
	// clear no-op rather than a duplicate commit (exactly-once semantics).
	published bool

	// git is the git runner (injected in tests). Defaults to runGit over the real binary.
	git func(root string, args ...string) (string, error)
}

// newSessionFromEnv resolves the ambient session context from process env. artifact
// kind and job mode are REQUIRED (the dispatch always sets them); project id defaults
// to the repo's committed project.json id and finally to the state-root dir name;
// state root defaults to the process working directory (the checked-out repo).
func newSessionFromEnv(getenv func(string) string, wd string) (*Session, error) {
	mode := strings.TrimSpace(getenv(envJobMode))
	if mode == "" {
		mode = jobModeDraft
	}
	if mode != jobModeDraft && mode != jobModeCritique && mode != jobModeAnswer && mode != jobModeConstruct {
		return nil, fmt.Errorf("%s=%q is not a known job mode (want %q, %q, %q, or %q)", envJobMode, mode, jobModeDraft, jobModeCritique, jobModeAnswer, jobModeConstruct)
	}

	// Artifact kind is the design modes' ambient slot selector; the CONSTRUCT mode is
	// keyed by component + activity instead, so kind is OPTIONAL there.
	var kind projectstate.ArtifactKind
	kindStr := strings.TrimSpace(getenv(envArtifactKind))
	if kindStr == "" {
		if mode != jobModeConstruct {
			return nil, fmt.Errorf("%s is required (the ambient artifact kind for this design job) but was empty", envArtifactKind)
		}
	} else {
		k, err := parseArtifactKind(kindStr)
		if err != nil {
			return nil, err
		}
		kind = k
	}

	root := strings.TrimSpace(getenv(envStateRoot))
	if root == "" {
		root = wd
	}

	s := &Session{
		Kind:         kind,
		Mode:         mode,
		StateRoot:    root,
		TargetBranch: strings.TrimSpace(getenv(envTargetBranch)),
		ComponentID:  strings.TrimSpace(getenv(envComponentID)),
		ActivityID:   strings.TrimSpace(getenv(envActivityID)),
		git:          runGit,
	}

	// Project id: explicit env wins; else the committed project.json id; else the repo
	// dir name (the name-as-identity fallback). A read failure here is non-fatal — the
	// id only matters for diagnostics and is not consulted by the cross-artifact rules.
	if id := strings.TrimSpace(getenv(envProjectID)); id != "" {
		s.ProjectID = projectstate.ProjectID(id)
	} else if proj, _, rerr := s.readProject(); rerr == nil && proj.ID != "" {
		s.ProjectID = proj.ID
	} else {
		s.ProjectID = projectstate.ProjectID(filepath.Base(root))
	}
	return s, nil
}

// parseArtifactKind maps the env value (the dispatch stamps artifactKindString(kind) =
// the PascalCase String() form, e.g. "Volatilities") back to the ArtifactKind. It also
// accepts the camelCase wire name and the raw ordinal so the binary is robust to either
// convention the caller uses.
func parseArtifactKind(v string) (projectstate.ArtifactKind, error) {
	for _, k := range projectstate.AllArtifactKinds() {
		if v == k.String() || v == k.WireName() {
			return k, nil
		}
	}
	if n, err := strconv.Atoi(v); err == nil {
		k := projectstate.ArtifactKind(n)
		if _, ok := projectstate.NewModelForKind(k); ok {
			return k, nil
		}
	}
	return 0, fmt.Errorf("%q is not a recognized artifact kind (want a Method artifact name like %q or %q)",
		v, projectstate.KindVolatilities.String(), projectstate.KindVolatilities.WireName())
}

// projectFilePath is the absolute on-disk path of the aggregate document in the checkout.
func (s *Session) projectFilePath() string {
	return filepath.Join(s.StateRoot, statePathPrefix, projectFile)
}

// readProject reads + decodes the checked-out project.json through the strict server
// codec. It returns the decoded aggregate AND the raw bytes (some callers want both).
// A missing / empty / undecodable document is an error — the design rail always creates
// the project before drafting, so a decode failure here is a genuine fault the agent
// must surface.
func (s *Session) readProject() (projectstate.Project, []byte, error) {
	raw, err := os.ReadFile(s.projectFilePath())
	if err != nil {
		return projectstate.Project{}, nil, fmt.Errorf("read %s: %w", filepath.Join(statePathPrefix, projectFile), err)
	}
	proj, ok, err := projectstate.DecodeProjectJSON(raw, s.ProjectID)
	if err != nil {
		return projectstate.Project{}, raw, fmt.Errorf("the committed project state does not decode: %w", err)
	}
	if !ok {
		return projectstate.Project{}, raw, fmt.Errorf("no project state found at %s", filepath.Join(statePathPrefix, projectFile))
	}
	return proj, raw, nil
}

// writeProjectBytes writes the (already-encoded) canonical project.json to disk.
func (s *Session) writeProjectBytes(b []byte) error {
	return os.WriteFile(s.projectFilePath(), b, 0o600)
}

// slotAccessors is the binary's local mirror of the projectstate slotTable (which is
// unexported) over the EXPORTED Project slot fields — so the ambient kind alone selects
// the slot, never a positional guess by the agent (the F68 fix, made structural). A
// table (not a switch) so the exhaustive gate proves every ArtifactKind has an entry.
var slotAccessors = map[projectstate.ArtifactKind]func(*projectstate.Project) *projectstate.ArtifactSlot{
	projectstate.KindMission:              func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.Mission },
	projectstate.KindGlossary:             func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.Glossary },
	projectstate.KindScrubbedRequirements: func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.ScrubbedRequirements },
	projectstate.KindVolatilities:         func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.Volatilities },
	projectstate.KindCoreUseCases:         func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.CoreUseCases },
	projectstate.KindSystem:               func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.SystemDesign },
	projectstate.KindOperationalConcepts:  func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.OperationalConcepts },
	projectstate.KindStandardCheck:        func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.StandardCheck },
	projectstate.KindPlanningAssumptions:  func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.PlanningAssumptions },
	projectstate.KindActivityList:         func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.ActivityList },
	projectstate.KindNetwork:              func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.Network },
	projectstate.KindNormalSolution:       func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.NormalSolution },
	projectstate.KindSubcriticalSolution:  func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.SubcriticalSolution },
	projectstate.KindCompressedSolution:   func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.CompressedSolution },
	projectstate.KindDecompressedSolution: func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.DecompressedSolution },
	projectstate.KindRiskModel:            func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.RiskModel },
	projectstate.KindSdpReview:            func(p *projectstate.Project) *projectstate.ArtifactSlot { return &p.SdpReview },
}

// slotFor returns the ArtifactSlot pointer for kind on the decoded aggregate; an
// unrecognized kind (out-of-range ordinal) reports false, exactly as the former
// switch default did.
func slotFor(p *projectstate.Project, kind projectstate.ArtifactKind) (*projectstate.ArtifactSlot, bool) {
	acc, ok := slotAccessors[kind]
	if !ok {
		return nil, false
	}
	return acc(p), true
}

// marshalModel renders a typed ArtifactModel to indented JSON for a read tool's output.
func marshalModel(m projectstate.ArtifactModel) (string, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
