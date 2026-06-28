package web

import (
	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
)

// This file is the HTTP/JSON wire binding for the project CATALOG facet
// (projectManager): the request/response DTOs for CreateProject / ListProjects /
// GetProject and the typed-state → wire mappers. The catalog is FLAT (projectId is
// minted by the server on create, then carried as a path segment everywhere else),
// so unlike the phase DTOs these shapes never carry a body projectId.

// --- request DTOs ----------------------------------------------------------

// createProjectRequest is the body of POST /api/v1/projects. NAME-AS-IDENTITY
// (C-PM-Δ, 2026-06-15): Name is the user-supplied repo name == the project identity
// (the user creates the empty repo first; aiarch adopts it). The owner is derived
// from the authenticated principal (ownerScopeFor) — never a caller-supplied field.
//
// CORRECTION (2026-06-15, founder ruling): the old optional AgenticToken field was
// REMOVED. aiarch does NO secret management. The user's CLAUDE_CODE_OAUTH_TOKEN is
// provisioned by the Claude Code GitHub App when the USER runs /install-github-app on
// their repo (an OAuth-flow Actions secret, not an API-uploadable value) — a user
// onboarding prerequisite, never carried through aiarch.
type createProjectRequest struct {
	Name string `json:"name"`
}

// --- response DTOs ---------------------------------------------------------

// createProjectResponse echoes the freshly minted projectId the SPA scopes its
// subsequent phase routes under.
type createProjectResponse struct {
	ProjectID string `json:"projectId"`
}

// projectSummaryResponse is one catalog row for the landing grid (ListProjects).
// It mirrors project.ProjectSummary with stable camelCase json keys and the phase
// rendered as a wire string.
type projectSummaryResponse struct {
	ProjectID      string `json:"projectId"`
	Name           string `json:"name"`
	Owner          string `json:"owner"`
	Phase          string `json:"phase"`
	CommittedCount int    `json:"committedCount"`
	TotalCount     int    `json:"totalCount"`
	UpdatedAt      string `json:"updatedAt"`
}

// projectSummaryFromManager maps a projectManager ProjectSummary onto the wire row.
func projectSummaryFromManager(s project.ProjectSummary) projectSummaryResponse {
	return projectSummaryResponse{
		ProjectID:      string(s.ProjectID),
		Name:           s.Name,
		Owner:          string(s.Owner),
		Phase:          phaseName(s.Phase),
		CommittedCount: int(s.CommittedCount),
		TotalCount:     int(s.TotalCount),
		UpdatedAt:      s.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

// projectStateResponse is the full typed head-state of one project (GetProject).
// Slots carries one slot per defined ArtifactKind in the stable slot order; each
// slot's typed Model serializes via the shared discriminated envelope (the
// project.ArtifactSlotView MarshalJSON), identical byte-for-byte to the envelope
// the systemDesignManager session read emits — so the SPA decodes a model the same
// way regardless of which read produced it.
//
// GitRows is the per-activity git-forward head-state (D-PA-GIT, C-CW-GIT), keyed by
// ActivityID — the SAME map shape the ux-mock GIT_BY_ID consumer uses
// (Record<string, GitRef>), so U-SPA-GIT's hand-mirrored lookup (gitFor(activityId))
// drops in unchanged. It is OMITTED entirely (omitempty) until the first git Record*
// verb populates it; an activity with no git row is simply absent from the map (the
// honest-empty convention — never a fabricated empty row). The construction-session
// view AND the change-request views both read git head-state from THIS one map,
// keyed by their activity / CR-activity id.
type projectStateResponse struct {
	ProjectID            string                        `json:"projectId"`
	Name                 string                        `json:"name"`
	Owner                string                        `json:"owner"`
	Phase                string                        `json:"phase"`
	Version              uint64                        `json:"version"`
	Research             researchInputDTO              `json:"research"`
	Slots                []project.ArtifactSlotView    `json:"slots"`
	GitRows              map[string]gitRowDTO          `json:"gitRows,omitempty"`
	ConstructionRows     map[string]constructionRowDTO `json:"constructionRows,omitempty"`
	ConstructionProgress *constructionProgressDTO      `json:"constructionProgress,omitempty"`
	ServiceContracts     map[string]serviceContractDTO `json:"serviceContracts,omitempty"`
}

// gitRowDTO is the wire form of one project.ActivityGitStatus — the per-activity git
// head-state row the SPA renders as the GitRowMeta cluster (branch · PR · CI [· +1]).
// The field names match the ux-mock GitRef consumer EXACTLY (data/git.ts) so U-SPA-GIT
// hand-mirrors this struct 1:1 (the codegen gap, [[project_archistrator_webapp_parity_build]]).
//
// PROVIDER-OPACITY (D-PA-GIT-review Ruling 3 + D-PA-GIT-PRURL-ruling, OQ-3): the
// DURABLE row exposes the OPAQUE pullRequestRef (the rail's PullRequestRef.String(),
// "today a PR number"), and the aggregate / projectStateAccess store carry zero
// provider host — that invariant is UNCHANGED. prNumber and prUrl are NOT stored;
// they are READ-TIME PROJECTIONS the webClient constructs at the read boundary
// (D-PA-GIT-PRURL-ruling R1/R2):
//   - prNumber is DERIVED from the opaque ref (strconv.Atoi) — the "the opaque ref is
//     a number" assumption is now isolated to ONE server-side mapper, not the SPA.
//   - prUrl is COMPOSED as <repoBase>/pull/<ref> from the project-wide repoBase the
//     Client holds (threaded from the construction-repo config at composition; see
//     web.go / cmd/server/main.go). The GitHub URL grammar lives server-side with the
//     rest of the provider knowledge; the SPA receives a READY href and drops in
//     unchanged from the ux-mock (which also got a finished prUrl).
//
// Both stay omitempty: a branch-only first touch (ref == "") or an unconfigured
// construction repo (repoBase == "") simply omits them — the honest-empty convention,
// never a fabricated host. The opaque pullRequestRef REMAINS on the wire (it is the
// durable truth; prNumber/prUrl are its projections, not replacements). pullRequestRef
// is "" until the PR opens. ciStatus is the provider-neutral 3-state rollup string the
// SPA maps to its CiStatus union ('in_progress' | 'failed' | 'success'). crLabel/isRevert
// are omitempty (absent for a non-CR, non-revert activity), matching the optional ux-mock
// GitRef fields. updatedAt is the server-resolved last-touch (RFC3339).
type gitRowDTO struct {
	BranchName           string `json:"branchName"`
	PullRequestRef       string `json:"pullRequestRef,omitempty"`
	PrNumber             int    `json:"prNumber,omitempty"`
	PrURL                string `json:"prUrl,omitempty"`
	CIStatus             string `json:"ciStatus"`
	ArchitectureApproved bool   `json:"architectureApproved"`
	Merged               bool   `json:"merged"`
	CRLabel              string `json:"crLabel,omitempty"`
	IsRevert             bool   `json:"isRevert,omitempty"`
	UpdatedAt            string `json:"updatedAt"`
}

// projectStateFromManager maps a projectManager ProjectState onto the wire shape.
// The typed slots are passed through unchanged — their own MarshalJSON owns the
// model-envelope encoding. The git head-state map is projected per-row onto the
// gitRowDTO (a PURE read projection — no business logic, no writes). It is a method on
// *Client so the git-row projection can read the project-wide repoBase the Client holds
// (threaded from the construction-repo config at composition) to compose each row's
// prUrl — pure config + opaque-ref composition, no store read, no Manager call
// (D-PA-GIT-PRURL-ruling R1).
func (c *Client) projectStateFromManager(s project.ProjectState) projectStateResponse {
	rows, prog := constructionFromState(s.ActivityConstruction, s.ConstructionProgress)
	return projectStateResponse{
		ProjectID:            string(s.ProjectID),
		Name:                 s.Name,
		Owner:                string(s.Owner),
		Phase:                phaseName(s.Phase),
		Version:              uint64(s.Version),
		Research:             researchInputFromState(s.Research),
		Slots:                s.Slots,
		GitRows:              gitRowsFromState(s.GitRows),
		ConstructionRows:     rows,
		ConstructionProgress: prog,
		ServiceContracts:     serviceContractsFromState(s),
	}
}

// gitRowsFromState projects the per-activity git head-state map onto the wire DTO map,
// keyed by the SAME ActivityID. An absent/empty head-state map yields nil (omitted from
// the JSON via omitempty) — the honest-empty convention: the SPA sees no gitRows key
// rather than an empty object, exactly as the ux-mock gitFor(id) returns undefined for
// a not-yet-branched activity.
//
// prNumber/prUrl are no longer composed here: they are now read-time projections the
// projectManager OWNS (composed from its repoBase + the opaque ref; the former web
// projectPRRef relocated onto the contract). This layer passes them through verbatim,
// preserving their omitempty semantics (prNumber 0 / prUrl "" simply omit on the wire).
func gitRowsFromState(rows map[string]project.ActivityGitStatus) map[string]gitRowDTO {
	if len(rows) == 0 {
		return nil
	}
	out := make(map[string]gitRowDTO, len(rows))
	for activityID, g := range rows {
		out[activityID] = gitRowDTO{
			BranchName:           g.BranchName,
			PullRequestRef:       g.PullRequestRef,
			PrNumber:             int(g.PrNumber),
			PrURL:                g.PrURL,
			CIStatus:             ciStatusName(g.CICheck),
			ArchitectureApproved: g.ArchApproved,
			Merged:               g.Merged,
			CRLabel:              g.CRLabel,
			IsRevert:             g.IsRevert,
			UpdatedAt:            g.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		}
	}
	return out
}

// ciStatusName renders the typed 3-state project.CICheckState onto the stable wire
// string the ux-mock CiStatus union uses ('in_progress' | 'failed' | 'success').
// Pending (the zero value / unknown) maps to 'in_progress' — the ux-mock's running
// state. This is the ONLY place the CI enum crosses the wire, mirroring the
// constructionStageName / runtimeStatusName enum-mappers.
func ciStatusName(s project.CICheckState) string {
	switch s {
	case project.CICheckSuccess:
		return "success"
	case project.CICheckFailure:
		return "failed"
	default:
		return "in_progress"
	}
}

// researchInputFromState mirrors project.ResearchInput onto the wire research
// DTO (the inverse of setResearchInputRequest.toResearchInput).
func researchInputFromState(r project.ResearchInput) researchInputDTO {
	sources := make([]researchSourceDTO, 0, len(r.Sources))
	for _, s := range r.Sources {
		sources = append(sources, researchSourceDTO{Title: s.Title, Content: s.Content})
	}
	return researchInputDTO{Sources: sources}
}

// phaseName renders a project.Phase onto the wire as a stable string.
func phaseName(p project.Phase) string {
	switch p {
	case project.PhaseSystemDesign:
		return "systemDesign"
	case project.PhaseProjectDesign:
		return "projectDesign"
	case project.PhaseConstruction:
		return "construction"
	default:
		return "unknown"
	}
}
