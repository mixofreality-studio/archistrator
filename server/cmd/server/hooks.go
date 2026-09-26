package main

// hooks.go is the hand composition-root POLICY seam the generated composition
// root (main.gen.go, composegen) delegates to: the genuinely-compositional
// decisions the deployment model cannot express — profile resolution, the
// Temporal logger, the authorization PDP + token validator + dev config, the
// manager logging wrap + composition-root-only route mounts, the variant-arg
// tuples for the GitHub-App-backed variants, the post-construction dry-run stub
// swaps, the conditional-worker gates, and the func/scalar manager-dep resolvers.
//
// The struct is built ONCE in main() from the resolved *Config; the no-arg hooks
// (repo resolvers, construction-transition ports, repoBase) close over that
// stored cfg + logger, and the GitHub-App satellite (AppClient) + the
// sourcecontrol catalog surface are built once here and reused across the hooks
// that need them.
//
// SIX REVIEWED RESIDUALS of the fixed generated seam (all dev-profile-only;
// cloud with DRYRUN=false is unaffected):
//
//  1. SHARED-ARTIFACT DRY-RUN CONTAMINATION. FinalizeArtifactAccess swaps the
//     profile-built artifactAccess for the in-memory dry-run stub when
//     CONSTRUCTION_DRYRUN=true. That artifactAccess local is SHARED — the
//     generated body threads the SAME value into BOTH the construction Manager
//     and the operations Manager — so in a dry-run dev/dogfood boot the operations
//     Manager also receives the dry-run artifact stub (the old hand run() gave
//     operations the real/nil store). This is accepted for the dry-run dev
//     profiles (operations artifact IO is not exercised in the UC1 slice); cloud
//     (DRYRUN=false) is identity, so operations keeps the real store there.
//
//  1b. SHARED-PIPELINE DRY-RUN CONTAMINATION (the same shape as #1).
//     FinalizeAgenticJobAccess swaps agenticJobAccess for the
//     in-memory dry-run stub when CONSTRUCTION_DRYRUN=true. That local is ALSO
//     shared — the generated body threads it into constructionManager AND BOTH
//     design managers (projectDesignManager/systemDesignManager,
//     main.gen.go:459/471), so a dry-run boot hands the design managers the
//     dry-run pipeline stub too. Benign: the design managers only ever READ
//     agenticJobAccess to report status — they never DISPATCH a
//     construction run (only constructionManager does), so the stub is inert
//     there. Cloud (DRYRUN=false) is identity.
//
//  2. SEPARATE GIT-SUBSTRATE INSTANCES PER SECONDARY CONTRACT (B6). Once
//     constructionTransitionAccess/gitActivityStatusAccess/designSessionAccess
//     became their OWN deployment bindings (sharing projectstate's goPackage +
//     git substrate with projectStateAccess, but composegen has no notion of a
//     binding "providing" a second contract), the generated body constructs
//     THREE separate *GitStore-backed instances alongside projectStateAccess's
//     own (NewGitLocal/GitHubConstructionTransitionAccess etc., gitadapter.go /
//     designsession.go) — four total instead of the pre-B6 hand-wired two (one
//     shared psa's GitConstructionPorts served both ports). All four address the
//     SAME repo and GitStore holds no connection state, so this is an
//     instance-count residual, not a correctness one.
//
//  4. POSTGRES POOL DIAL — PROFILE-GATED (local-first-init-funnel Task 2).
//     main.gen.go's Postgres pool (postgresinfra.NewPool(ctx, cfg.PostgresURL),
//     which PINGS — a real dial, not a lazy handle) is built ONLY when
//     `profile == "cloud"`, so a local boot no longer dials it. The gate comes
//     from composegen's writePostgres (framework-go-app-generator, RELEASED as
//     v0.8.1 — platform commit e3ce3da0, tagged and pinned in server/go.mod):
//     pool construction is no longer decided purely by consumesPostgres() ("does
//     ANY declared binding, in ANY profile, use the postgres substrate", a
//     GENERATION-TIME decision) but by the postgres infra key's declared
//     `profiles` list evaluated against the RUNTIME-resolved `profile` value,
//     mirroring how each RA's own per-profile switch arm already works. With the
//     release pinned, a plain `GOWORK=off go run ./cmd/appgen` regen reproduces
//     the gated main.gen.go byte-for-byte — the old regen-reverts-gating hazard
//     (regenerating against the un-patched v0.8.0 silently reintroduced the
//     unconditional dial) is closed. Confirmed via a real local boot with
//     Temporal reachable and Postgres NOT reachable: the process reaches
//     "http server listening" cleanly, with zero "postgres"/"5432" mentions in
//     the boot log. NEVER hand-edit main.gen.go — regenerate via cmd/appgen.
//
//  5. WORKER PROVIDER SELECTION (resolveWorkerProvider below) — claude-local
//     provider (local-first-init-funnel Task 3): framework-go-infrastructure-llm
//     claudecli.go (llm.ClaudeCLIClient / llm.NewClaudeCLIClient /
//     llm.PreflightClaudeCLI — the claude-local Worker Provider), RELEASED as
//     v0.2.0 (platform commit 2a389d31, tagged) and pinned in server/go.mod, so
//     both consumers — cmd/server (this file's resolveWorkerProvider) and
//     cmd/archistrator (preflight.go's `llm.PreflightClaudeCLI()` call, the
//     `archistrator serve` boot-time claude-CLI check) — compile clean under
//     `GOWORK=off` against the published module alone; the former workspace-only
//     dependency (undefined llm.* symbols against the pre-claudecli v0.1.0 pin)
//     is gone. NEVER stub ClaudeCLIClient's logic locally in the app — the
//     provider belongs in the platform module (see claudecli.go's own doc
//     comment for why), not duplicated here.
//
//  6. CLAUDE-LOCAL PROVIDER TOOL-TURN OMISSION. The llm.ClaudeCLIClient (the
//     claude-local Worker Provider, framework-go-infrastructure-llm/claudecli.go)
//     implements Generate ONLY — GenerateToolTurn and GenerateWithTools are
//     intentionally omitted. The reason: headless `claude --mcp-config` runs its
//     own internal agentic loop and cannot yield one blocking turn to an external
//     caller-driven loop. No live callers of those contracts exist in this repo
//     since the 2026-06 agentic pivot (the construction executor does not call
//     Generate{ToolTurn,WithTools} — it shells `claude` directly with
//     --mcp-config). Full analysis in
//     framework-go-infrastructure-llm/claudecli.go's own doc comment. This is
//     intentional, not a gap: callers requiring those methods remain anchored to
//     llm.AnthropicClient (cloud, or explicit key override on local).

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	github "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	keycloak "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-keycloak"
	llm "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-llm"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	tlog "go.temporal.io/sdk/log"

	"github.com/mixofreality-studio/archistrator/server/internal/client/web"
	managerbilling "github.com/mixofreality-studio/archistrator/server/internal/manager/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/merchantgateway"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// appHooks implements the generated Hooks interface. Built once by newAppHooks.
type appHooks struct {
	logger *slog.Logger
	// embeddedSPA overrides the build-tag-gated spaFS seam (spa_embed.go /
	// spa_stub.go) that ExtraMounts mounts on the local profile; nil means
	// spaFS. Set only by tests, which need an SPA to mount without a
	// `localdist` build.
	embeddedSPA func() (fs.FS, bool)
	// config is the resolved *Config the no-arg hooks (repo resolvers,
	// construction-transition ports, repoBase, escalation/mode scalars) close over —
	// the generated Hooks signatures for those seams take no cfg argument.
	config *Config

	// appClient is the shared GitHub-App satellite the sourcecontrol +
	// agenticjob variant-arg hooks spread into their generated DI ctors;
	// nil when the App identity is unconfigured (repo-less dev / local profile).
	appClient *github.AppClient
	// scCatalog is the sourcecontrol catalog surface backing the projectstate CLOUD
	// ports + the design PR-rail repo resolvers; nil when repo-less.
	scCatalog sourcecontrol.CatalogAccess
	// scAccess + realPipeline are the github-creds-gated RAs the LOCAL profile's
	// binding arms do NOT construct (sourceControlAccess + agenticJobAccess
	// are cloud-arm-only in the deployment model, but their real presence is gated on
	// the GitHub App creds — ORTHOGONAL to the projectstate substrate profile). The
	// Finalize hooks select them in the local profile so a local-projectstate boot
	// WITH github creds (the agentic systemtests) still drives the design-dispatch +
	// PR rail, exactly as the hand run() did. nil when repo-less (rail dormant).
	scAccess     sourcecontrol.SourceControlAccess
	realPipeline agenticjob.AgenticJobAccess

	// localPipeline is the local-first-init-funnel Task 6 construction executor —
	// headless `claude` shelled directly against the on-disk repo, with NO GitHub
	// creds involved. Built whenever the profile is LOCAL (regardless of DRYRUN, so
	// a later toggle to DRYRUN=false takes effect without a restart-time rebuild);
	// FinalizeAgenticJobAccess below selects it for a local boot WITHOUT
	// GitHub creds (realPipeline, built above when creds ARE configured, keeps
	// priority — "creds present keeps the existing behavior"). nil on the cloud
	// profile, mirroring scAccess/realPipeline's repo-less-dormant pattern.
	localPipeline agenticjob.AgenticJobAccess

	// workerProvider is the selected LLM Worker Provider (local-first-init-funnel
	// Task 3): local profile with no ANTHROPIC_API_KEY → llm.ClaudeCLIClient
	// (headless `claude -p` on the user's own subscription, platform module
	// framework-go-infrastructure-llm/claudecli.go); any profile WITH the key set
	// → llm.AnthropicClient (the cloud transport, anthropic.go). nil when neither
	// applies (cloud profile, no key — LLM-consuming seams stay dormant, mirroring
	// scAccess/realPipeline's repo-less-dormant pattern above). NOT YET consumed by
	// any Manager/Engine: the cloud coauthor draft path dispatches an agentic
	// GitHub-Actions job instead of a synchronous provider call, and the local
	// construction executor (Task 6) is a SEPARATE mechanism that shells `claude`
	// directly with --mcp-config, not through this field. Selection + the boot-time
	// `claude --version` preflight land with this task per its brief; the eventual
	// consumer plugs in without touching this file's selection policy.
	workerProvider workerGenerateProvider
}

// workerGenerateProvider is the common surface both llm Worker Provider clients
// expose — llm.AnthropicClient.Generate (anthropic.go) and
// llm.ClaudeCLIClient.Generate (claudecli.go) — so this composition root can
// select between them without a caller needing to know which one it got. Local
// to this file: composegen has no declared "workerAccess"-shaped binding in
// project.json (it was removed entirely in the 2026-06 agentic pivot — see
// project.json's "workerAccess removed entirely" decision), so there is no
// generated Hooks interface method for this seam; it is a hand policy decision
// exactly like resolveProfile below.
type workerGenerateProvider interface {
	Generate(ctx context.Context, req llm.AnthropicGenerateRequest) (llm.AnthropicGenerateResponse, error)
}

// configureGithubApp builds the shared GitHub-App satellite — the source-control
// catalog/access pair and, when the construction repo is configured, the
// GitHub-Actions agenticJobAccess — onto h.
//
// Carved out of newAppHooks so the boot walk reads as a list of steps rather than
// one long branch, and so the three credential states (complete / partial /
// absent) are a switch over exactly that question instead of an if-else chain.
// Fail-fast semantics are unchanged: any construction error returns and main()
// exits before the boot walk.
func (h *appHooks) configureGithubApp(cfg *Config, logger *slog.Logger) error {
	complete := cfg.GithubAppAppID != "" && cfg.GithubAppPrivateKeyPEM != "" && cfg.GithubAppAccount != ""
	partial := cfg.GithubAppAppID != "" || cfg.GithubAppPrivateKeyPEM != "" || cfg.GithubAppAccount != ""

	switch {
	case complete:
		return h.buildGithubApp(cfg, logger)
	case partial:
		warnPartialGithubAppCreds(logger, cfg)
	default:
		logger.Warn("sourceControlAccess NOT configured — projects are created repo-less (set ARCHISTRATOR_GITHUB_APP_ID + ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM + ARCHISTRATOR_GITHUB_ACCOUNT for live GitHub repo provisioning)")
	}
	return nil
}

// buildGithubApp is the complete-credentials arm of configureGithubApp.
func (h *appHooks) buildGithubApp(cfg *Config, logger *slog.Logger) error {
	app, err := github.NewAppClient(cfg.GithubAppAppID, cfg.GithubAppPrivateKeyPEM, cfg.GithubAppAPIBaseURL)
	if err != nil {
		return err
	}
	h.appClient = app
	scCatalog, scAccess, err := sourcecontrol.NewGitHubSourceControl(app, cfg.GithubAppAccount, cfg.GithubAppAppSlug, true /* repoPrivate */)
	if err != nil {
		return err
	}
	h.scCatalog, h.scAccess = scCatalog, scAccess
	logger.Info("sourceControlAccess (github) ready", "account", cfg.GithubAppAccount, "apiBaseURL", cfg.GithubAppAPIBaseURL)

	// The github-creds-gated agenticJobAccess the LOCAL profile's binding arm does
	// not build (FinalizeAgenticJobAccess selects it in local profile). Built ONCE
	// here so a construction-error fails fast; the CLOUD arm builds its own via the
	// hook-args, so this is unused in cloud.
	if cfg.ConstructionRepoOwner == "" || cfg.ConstructionRepoName == "" {
		return nil
	}
	pipeline, err := agenticjob.NewGitHubActionsAgenticJobAccess(
		app, cfg.ConstructionRepoOwner, cfg.ConstructionRepoName, cfg.ConstructionWorkflowFile, cfg.ConstructionRef, parseInt64(cfg.GithubAppInstallationID))
	if err != nil {
		return err
	}
	h.realPipeline = pipeline
	return nil
}

// resolveWorkerProvider selects and builds the LLM Worker Provider per the
// local-first deployment thesis (docs/superpowers/plans/2026-07-19-local-first-init-funnel.md,
// Task 3): an ANTHROPIC_API_KEY in the environment always wins (cloud transport
// — the only option for the cloud profile, and an explicit opt-in override for a
// local dev box that wants it); otherwise the local profile falls back to
// claude-local and preflights it — `claude --version` must run cleanly before
// the server accepts traffic, with a friendly, actionable error naming the
// install command when it does not. The cloud profile with no key returns
// (nil, nil): LLM-consuming seams stay dormant, exactly as sourceControlAccess
// does when repo-less.
//
// ANTHROPIC_API_KEY is read directly (not through *Config): configgen only
// generates a Config field for a component's declared deployment-model env var,
// and no "workerAccess"-shaped binding exists in project.json to generate one
// from (see workerGenerateProvider's doc comment) — this hand env read mirrors
// the WEBAPP_ORIGIN/WEBAPP_ASSET_VERSION precedent in ExtraMounts below.
func resolveWorkerProvider(cfg *Config, logger *slog.Logger) (workerGenerateProvider, error) {
	if apiKey := getenvString("ANTHROPIC_API_KEY", ""); apiKey != "" {
		logger.Info("workerProvider (anthropic) ready")
		return llm.NewAnthropicClient(apiKey, "", 0), nil
	}
	if resolveProfile(cfg) != "local" {
		logger.Warn("workerProvider NOT configured — no ANTHROPIC_API_KEY on the cloud profile; LLM-consuming seams stay dormant until one is set")
		return nil, nil //nolint:nilnil // optional dependency absent (cloud, no key) → (nil, nil) is intentional, mirrors scAccess/realPipeline above
	}
	if err := llm.PreflightClaudeCLI(); err != nil {
		return nil, fmt.Errorf("workerProvider: %w", err)
	}
	logger.Info("workerProvider (claude-local) ready — headless claude on the user's own subscription")
	return llm.NewClaudeCLIClient(0), nil
}

// newAppHooks builds the composition-root hook state from the resolved *Config: the
// shared GitHub-App satellite + sourcecontrol catalog (when the App identity is
// configured), and the git construction-transition ports (from a psa mirroring the
// generated profile switch). Fail-fast: a malformed App key / catalog build surfaces
// here so main() exits before the boot walk.
func newAppHooks(cfg *Config, logger *slog.Logger) (*appHooks, error) {
	h := &appHooks{logger: logger, config: cfg}

	// LLM Worker Provider selection + boot-time preflight — fails fast (before
	// the boot walk) on the local profile with no `claude` on PATH, exactly like
	// the GitHub-App key validation below.
	workerProvider, err := resolveWorkerProvider(cfg, logger)
	if err != nil {
		return nil, err
	}
	h.workerProvider = workerProvider

	// Shared GitHub-App satellite + catalog surface — built once when the App
	// identity + account are configured (the CLOUD profile). Nil otherwise: a dev
	// server with no GitHub creds runs repo-less (design rail dormant), exactly as
	// the hand run() did.
	if err := h.configureGithubApp(cfg, logger); err != nil {
		return nil, err
	}

	// LOCAL profile: build the local construction executor (Task 6) whenever the
	// profile is local, independent of DRYRUN — FinalizeAgenticJobAccess
	// decides whether it is actually SELECTED. A missing aiarch-state-mcp binary /
	// repo config only fails the BOOT when DRYRUN=false genuinely needs this arm
	// (mirrors realPipeline/scAccess's repo-less-dormant pattern above): a
	// DRYRUN=true dev/demo boot must not be blocked by packaging this has not been
	// staged for yet.
	if resolveProfile(cfg) == "local" {
		pipeline, err := newLocalPipeline(cfg, logger)
		switch {
		case err == nil:
			h.localPipeline = pipeline
		case !cfg.ConstructionDryRun:
			return nil, err
		default:
			logger.Warn("localPipeline (local construction executor) NOT ready — non-fatal because ARCHISTRATOR_CONSTRUCTION_DRYRUN=true", "cause", err)
		}
	}

	return h, nil
}

// warnPartialGithubAppCreds fires when an operator has set SOME but not ALL
// three GitHub App identity settings (ARCHISTRATOR_GITHUB_APP_ID /
// ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM / ARCHISTRATOR_GITHUB_ACCOUNT) —
// almost always a misconfiguration (an env-var typo, a partially-completed
// rollout) rather than the intentional repo-less boot (which sets NONE of
// them, and gets the milder "not configured" warning above instead). Names
// exactly which settings are still missing so the operator does not have to
// cross-reference config.gen.go, and states the CONCRETE local-first-init-
// funnel Task 6 consequence: with the App identity incomplete, h.appClient /
// h.scAccess / h.realPipeline all stay nil (the same as the fully-repo-less
// case, exactly as the code path above computes), so on the LOCAL profile
// construction silently falls through to the local executor
// (FinalizeAgenticJobAccess's h.localPipeline arm) instead of the
// GitHub-Actions pipeline the operator likely intended — the kind of silent
// behavior change this warning exists to surface loudly instead of leaving
// the operator to discover it from a construction run's behavior.
func warnPartialGithubAppCreds(logger *slog.Logger, cfg *Config) {
	var missing []string
	if cfg.GithubAppAppID == "" {
		missing = append(missing, "ARCHISTRATOR_GITHUB_APP_ID")
	}
	if cfg.GithubAppPrivateKeyPEM == "" {
		missing = append(missing, "ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM")
	}
	if cfg.GithubAppAccount == "" {
		missing = append(missing, "ARCHISTRATOR_GITHUB_ACCOUNT")
	}
	logger.Warn("sourceControlAccess PARTIALLY configured — GitHub App identity is incomplete; "+
		"treated as repo-less (same as none set), so construction routes to the local executor instead of GitHub Actions",
		"missing", strings.Join(missing, ", "))
}

// stateMCPBinEnvOverride lets an operator pin exactly which cmd/aiarch-state-mcp
// binary the local construction executor attaches via --mcp-config, bypassing
// discovery — the composition-root analog of cmd/archistrator/serverchild.go's
// ARCHISTRATOR_SERVER_BIN override.
const stateMCPBinEnvOverride = "ARCHISTRATOR_STATE_MCP_BIN"

// locateStateMCPBinary resolves the cmd/aiarch-state-mcp binary the local
// construction executor (Task 6) attaches to headless claude via --mcp-config —
// the SAME construct-verb rig the cloud aiarch-construct.yml workflow builds fresh
// from source on every dispatch. This process (archistrator-server) has no `go`
// toolchain guarantee at runtime, so discovery mirrors cmd/archistrator/
// serverchild.go's locateServerBinary precedent exactly: an explicit override, a
// binary named aiarch-state-mcp sitting next to this executable (the shape a
// packaging step is expected to produce, staged alongside archistrator-server),
// then PATH.
func locateStateMCPBinary() (string, error) {
	if override := getenvString(stateMCPBinEnvOverride, ""); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("%s=%s: %w", stateMCPBinEnvOverride, override, err)
		}
		return override, nil
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "aiarch-state-mcp")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if p, err := exec.LookPath("aiarch-state-mcp"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf(
		"could not find the aiarch-state-mcp binary (looked for %s, a sibling of this executable, and PATH) — "+
			"build it with `go build ./cmd/aiarch-state-mcp` or set %s",
		"aiarch-state-mcp", stateMCPBinEnvOverride)
}

// localProjectID derives the AIARCH_PROJECT_ID value the local executor stamps on
// the state-mcp process from the local repo path (name-as-identity, mirroring the
// cloud workflow's github.event.repository.name): the basename of the repo
// directory, ".git" suffix stripped if present (a bare repo's conventional name).
func localProjectID(repoURL string) string {
	path := strings.TrimSuffix(strings.TrimPrefix(repoURL, "file://"), "/")
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".git")
	if base == "" || base == "." || base == "/" {
		return "local"
	}
	return base
}

// localExecRunTimeoutEnv optionally overrides the local executor's per-invocation
// claude budget (agenticjob's 25-minute defaultLocalRunTimeout, which mirrors
// aiarch-construct.yml's job timeout). Same os.Getenv posture as the package's
// other rarely-used operator overrides (e.g. ARCHISTRATOR_LOCAL_EXEC_ALLOW_UNSANDBOXED):
// a Go duration string ("45m", "1h"). Exists because a design-heavy episode (the
// System draft) was observed productively mid-flight at the 25-minute kill on
// bench run-20260811T232159Z-6b2342fa — a bench/operator pacing choice, not a
// per-project setting, so it is not part of the generated Config.
const localExecRunTimeoutEnv = "ARCHISTRATOR_LOCAL_EXEC_RUN_TIMEOUT"

func localExecRunTimeoutFromEnv(logger *slog.Logger) time.Duration {
	raw := strings.TrimSpace(os.Getenv(localExecRunTimeoutEnv))
	if raw == "" {
		return 0 // constructor default (25m)
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		logger.Warn("ignoring invalid "+localExecRunTimeoutEnv, "value", raw)
		return 0
	}
	return d
}

// newLocalPipeline builds the local construction executor (Task 6) over the
// SAME on-disk repo the local projectstate substrate is configured with.
func newLocalPipeline(cfg *Config, logger *slog.Logger) (agenticjob.AgenticJobAccess, error) {
	if cfg.ProjectStateGitRepoURL == "" {
		return nil, fmt.Errorf("localPipeline: ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL is required")
	}
	bin, err := locateStateMCPBinary()
	if err != nil {
		return nil, fmt.Errorf("localPipeline: %w", err)
	}
	pipeline, err := agenticjob.NewLocalExecAgenticJobAccess(
		cfg.ProjectStateGitRepoURL, localProjectID(cfg.ProjectStateGitRepoURL), bin,
		localExecRunTimeoutFromEnv(logger))
	if err != nil {
		return nil, err
	}
	logger.Info("localPipeline (local construction executor) ready", "repoURL", cfg.ProjectStateGitRepoURL, "stateMCPBin", bin)
	return pipeline, nil
}

// projectStateCloudPorts builds the CLOUD projectStateAccess port tuple (the same
// values ProjectStateAccessGitHubArgs returns) from the shared sourcecontrol
// catalog. Caller guarantees h.scCatalog != nil.
func (h *appHooks) projectStateCloudPorts(cfg *Config) (webHost, account string, catalog projectstate.ProjectCatalog, minter projectstate.CredentialMinter) {
	webHost = gitWebHost(cfg.GithubAppAPIBaseURL)
	account = cfg.GithubAppAccount
	acctRef := sourcecontrol.AccountRef(account)
	return webHost, account,
		cloudProjectCatalog{sc: h.scCatalog, account: acctRef},
		cloudCredentialMinter{sc: h.scCatalog, account: acctRef}
}

// resolveProfile maps the loaded config to the active deployment profile. Only two
// profiles boot: LOCAL (ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true — on-disk git,
// dev/systemtests) and CLOUD (the GitHub-App-backed production stack). "test" is a
// C4-doc-only profile, NOT bootable (usage/opsystemstate/projectstate error on its
// default arm). A free function so newAppHooks and the ResolveProfile hook agree.
func resolveProfile(cfg *Config) string {
	if cfg.ProjectStateGitLocal {
		return "local"
	}
	return "cloud"
}

// --- Hooks interface ---------------------------------------------------------

func (h *appHooks) ResolveProfile(cfg *Config) string { return resolveProfile(cfg) }

func (h *appHooks) TemporalLogger(logger *slog.Logger) tlog.Logger {
	return newTemporalLogger(logger)
}

func (h *appHooks) PolicyDecisionPoint() security.PolicyDecisionPoint {
	// INTERIM policy: authentication (JWT validation in the auth middleware) is the
	// only gate — authenticatedOnlyPDP permits any authenticated principal, until
	// the Cedar PDP lands (authz.go).
	return authenticatedOnlyPDP{}
}

// TokenValidator constructs the keycloak access-token validator (authN). nil in dev
// mode or with no IdP (systemtests) — the auth middleware then injects a dev
// principal or denies.
func (h *appHooks) TokenValidator(ctx context.Context, cfg *Config) (security.Validator, error) {
	if cfg.AuthDevMode || cfg.KeycloakJWKSURL == "" {
		return nil, nil //nolint:nilnil // optional dependency absent (dev mode / no IdP) → (nil, nil) is intentional; the auth middleware injects a dev principal or denies.
	}
	validator, err := keycloak.NewValidator(ctx, keycloak.Config{
		JWKSURL: cfg.KeycloakJWKSURL,
		Issuer:  cfg.KeycloakIssuer,
		Leeway:  3 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	h.logger.Info("keycloak access-token validator ready", "issuer", cfg.KeycloakIssuer, "jwksURL", cfg.KeycloakJWKSURL)
	return validator, nil
}

func (h *appHooks) DevConfig(cfg *Config) web.DevConfig {
	return web.DevConfig{Enabled: cfg.AuthDevMode, Principal: devPrincipal()}
}

// WrapManagers decorates the two web/MCP-exposed managers with the composition-root
// logging seam (managerlog.go): every Infrastructure-kind error surfaced to a client
// — through either transport — is logged once server-side with op/projectID/cause.
// Each manager then takes its project-scoped authorization guard on top
// (projectScopedDeliveryManager / projectScopedOperationsManager), so a denial is
// answered before any inner work, on either transport.
func (h *appHooks) WrapManagers(managers WebManagers) WebManagers {
	return WebManagers{
		DeliveryManager: projectScopedDeliveryManager{
			DeliveryManager: loggingDeliveryManager{inner: managers.DeliveryManager, log: h.logger},
			security:        security.New(security.WithPolicyDecisionPoint(h.PolicyDecisionPoint())),
		},
		OperationsManager: projectScopedOperationsManager{
			OperationsManager: loggingOperationsManager{inner: managers.OperationsManager, log: h.logger},
		},
	}
}

// ExtraMounts adds the composition-root-only routes behind the same auth boundary:
// GET /api/userinfo (the SPA session probe — not a manager op), GET /api/v1/capabilities
// (the operations-argocd-deployment D9 seam — see below), and /mcp (the MCP
// transport over the SAME four wrapped managers the REST handlers use, plus the
// ui://archistrator/shell.html MCP-Apps resource). On the local profile, also
// mounts the embedded SPA at "/" (local-first-init-funnel Task 4, spa_handler.go)
// — the single-binary `archistrator init` boot serves design (UC1+UC2) AND
// construction from the same process that answers /api + /mcp. Gated on BOTH the
// `localdist` build tag (spaFS, spa_embed.go/spa_stub.go — cloud images never
// carry the tag) AND the runtime profile, so a hypothetical localdist-tagged
// binary run with cloud config never mounts the SPA either. The local profile
// also UNMOUNTS the generated operations routes entirely (D9: local holds no
// deployment credential and must not appear to operate — not a disabled
// console, not a simulated one; see the doc comment on that block below).
//
// WEBAPP_ORIGIN/WEBAPP_ASSET_VERSION are NOT configgen-owned (configgen emits
// config.gen.go from project.json's deployment model, which does not yet declare
// these two settings): read directly here, mirroring config_adapter.go's pattern
// of hand env reads for composition-root-only values (envSecret, devPrincipal).
//
// Every route, including the generated surface main.gen.go bound to root at "/"
// before this hook runs, is registered on an INNER mux. denyFramingOnEveryPath
// (framedenial.go) then binds that mux to root behind the anti-framing wrap, so
// every response carries frame-ancestors 'none' and X-Frame-Options: DENY. Add
// new routes in mountRoutes, never on root directly.
func (h *appHooks) ExtraMounts(root *http.ServeMux, cfg *Config, dev web.DevConfig, validator security.Validator, managers WebManagers) {
	routes := http.NewServeMux()
	if generated, pat := root.Handler(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/"}}); pat != "" {
		routes.Handle("/", generated)
	}
	h.mountRoutes(routes, cfg, dev, validator, managers)
	denyFramingOnEveryPath(root, routes)
}

// mountRoutes is ExtraMounts' route table; root here is the inner mux
// ExtraMounts wraps, already carrying the generated surface at "/".
func (h *appHooks) mountRoutes(root *http.ServeMux, cfg *Config, dev web.DevConfig, validator security.Validator, managers WebManagers) {
	// F-QA2-46: the generated handlers' writeManagerError writes every 5xx with zero
	// server-side logging, and the manager logging wrap above only sees
	// Infrastructure-kind *manager.Error values — so a client-visible 503 (e.g. a
	// lost Temporal signal response) could leave no server trace. Log EVERY /api/v1
	// 5xx at the transport instead: capture the generated API surface the generated
	// composition root mounted at "/" (before this hook runs) and shadow the more
	// specific "/api/v1/" pattern with the SAME handler behind the 5xx-logging
	// middleware (http5xxlog.go). Generated files stay untouched; /healthz, /readyz
	// and the composition-root mounts below stay outside the wrap.
	if apiSurface, pat := root.Handler(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/api/v1/"}}); pat != "" {
		root.Handle("/api/v1/", log5xxResponses(h.logger, apiSurface))
	}
	root.Handle("GET /api/userinfo", web.AuthMiddleware(dev, validator)(http.HandlerFunc(security.UserInfoHandler)))
	webAppOrigin := getenvString("ARCHISTRATOR_WEBAPP_ORIGIN", "http://localhost:5173")
	assetVersion := getenvString("ARCHISTRATOR_WEBAPP_ASSET_VERSION", "dev")
	root.Handle("/mcp", newMCPHandler(dev, validator,
		managers.DeliveryManager, managers.OperationsManager,
		webAppOrigin, assetVersion))

	// GET /api/v1/capabilities — the ONE thing that tells the webApp which
	// deployment profile it is talking to (operations-argocd-deployment Task 11,
	// spec D9). Behind the same auth boundary as /api/userinfo: by the time any
	// route component mounts, UserProvider has already confirmed a session, so
	// the read rides the same cookie/token. The response's zero value
	// ({"operations":false}) is the SAFE direction — an unreachable/erroring read
	// on the client leaves useCapabilities() returning undefined, and
	// webApp/src/utilities/capabilities.ts's operationsEnabled treats undefined
	// (and false) as HIDDEN, never shown-by-default.
	root.Handle("GET /api/v1/capabilities",
		web.AuthMiddleware(dev, validator)(http.HandlerFunc(h.handleCapabilities(cfg))))

	// GET /api/v1/projects/{projectID}/operated-app-id — the D13 derivation, answered
	// server-side so the browser never reimplements it (see OperatedAppIDForProject).
	// Same auth boundary and same composition-root-only status as /api/v1/capabilities;
	// mounted in BOTH profiles because it is a pure derivation that surfaces nothing
	// operational — the local profile's overlay stays dormant on the capability gate
	// (D9), not on this route's absence.
	root.Handle("GET /api/v1/projects/{projectID}/operated-app-id",
		web.AuthMiddleware(dev, validator)(h.handleOperatedAppID()))

	if resolveProfile(cfg) == "local" {
		spaSource := spaFS
		if h.embeddedSPA != nil {
			spaSource = h.embeddedSPA
		}
		mountSPA(root, h.logger, spaSource)

		// D9: the local profile holds no deployment credential and must not
		// APPEAR to operate — not a disabled console, not a simulated one. Hiding
		// the webApp nav entry is not enough on its own (a curious user could
		// still hit the URL directly): the generated operations routes
		// (internal/client/web/operations/operations_handlers.gen.go, all under
		// /api/v1/operations/) are UNMOUNTED here rather than left live-but-hidden.
		// This pattern mirrors the /api/v1/ 5xx-logging shadow above: a MORE
		// SPECIFIC literal pattern registered directly on root wins over both the
		// broader "/api/v1/" pattern and (on a localdist build) the SPA's
		// "/{first}/{rest...}" wildcard (spa_handler.go), so every
		// /api/v1/operations/... request 404s exactly as if the routes were never
		// registered at all — never falls through to genServer's real handlers.
		root.Handle("/api/v1/operations/", http.NotFoundHandler())
	}
}

// capabilitiesResponse is the wire shape GET /api/v1/capabilities answers —
// mirrored by hand on the webApp side (webApp/src/utilities/capabilities.ts's
// Capabilities type) since this composition-root-only route is not generated
// from a .serviceContracts entry.
type capabilitiesResponse struct {
	Operations bool `json:"operations"`
}

// handleCapabilities reports operations:true ONLY on the cloud profile — the
// profile whose operatedRuntimeAccess binding actually holds a real deployment
// credential (operatedRuntimeAccess binds local -> Local (dry-run), cloud ->
// Real). This is the single source of truth resolveProfile already computes;
// no separate config flag to drift from it.
func (h *appHooks) handleCapabilities(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(capabilitiesResponse{Operations: resolveProfile(cfg) == "cloud"}); err != nil {
			h.logger.Warn("capabilities: failed writing response", "err", err)
		}
	}
}

// ===========================================================================
// D13 — the project → operated-app identity.
// ===========================================================================

// operatedAppIDNamespace is the fixed UUID namespace OperatedAppIDForProject derives
// under. A platform constant, not a configuration knob: changing it re-points every
// project at a different operated app and orphans every head-state row already
// registered under the old derivation. Minted once (2026-08-08) and frozen.
var operatedAppIDNamespace = uuid.MustParse("fa098c85-58b6-483e-8506-36045a008da7")

// OperatedAppIDForProject derives an operated app's id from its project's id. This is
// the ONE realization of spec D13 — "an operated app's id IS its project's id: one
// operated app per project, no lookup verb, no stored correlation" — and every consumer
// of that identity goes through it: the deployment-diagram health overlay (via
// handleOperatedAppID below), the RegisterOperatedApp identity guard
// (projectScopedOperationsManager below), and the operator following the deployment
// runbook.
//
// D13's INTENT is that the mapping needs no lookup: given a project, anyone can compute
// its operated app's id with no read and no stored table. Its first MECHANISM — literal
// equality — could not survive contact with the types: a projectstate.ProjectID is a
// free-form string ("archistrator"), an operatedAppId is a uuid.UUID, and passing the
// former where the latter is expected is a 400 on every call, forever. A deterministic
// derivation keeps every property D13 actually wanted (pure function of the project id,
// stable across processes and restarts, no lookup verb, no new contract surface) and is
// type-honest.
//
// UUIDv5 — SHA-1 over a fixed namespace plus the project id — because it is specified,
// stable, and reproducible on both sides of any wire without inventing a format. The
// value is neither a secret nor an authorization boundary: knowing a project id has
// always implied knowing which operated app it deploys to, and authorization stays where
// it already is (the Principal on the call Context).
//
// It lives HERE, at the composition root, rather than in the operations Manager, because
// the encapsulation gate (internal/arch_test.go's TestGeneratedOnlyPublic) admits no
// exported symbol in a generated-contract package beyond its generated surface, and a
// second unexported copy inside the Manager is exactly the drift a single derivation
// exists to prevent.
//
// RELAXATION PATH (unchanged from D13): if a project ever needs more than one
// deployment, a real projectRef → operatedAppId lookup gets added then; the FIRST
// deployment keeps the id this function derives — so nothing already registered moves —
// and additional deployments take fresh ids.
func OperatedAppIDForProject(projectRef string) uuid.UUID {
	return uuid.NewSHA1(operatedAppIDNamespace, []byte(projectRef))
}

// operatedAppIDResponse is the wire shape GET /api/v1/projects/{projectID}/operated-app-id
// answers — mirrored by hand on the webApp side (webApp/src/api/client.ts), like
// capabilitiesResponse above, since this composition-root-only route has no
// .serviceContracts entry.
type operatedAppIDResponse struct {
	OperatedAppID string `json:"operatedAppId"`
}

// handleOperatedAppID answers the derived operated-app id for a project. It exists so
// the browser never has to reproduce the derivation: the deployment diagram's health
// overlay knows a projectId and needs the uuid QueryDeploymentHealth takes, and
// hand-rolling UUIDv5 in TypeScript would be a second implementation of a rule with one
// authority. Pure — it reads nothing, so it cannot fail — and it deliberately does NOT
// assert the project or the operated app exists: it answers "what id WOULD this project's
// deployment have", which is exactly what a caller needs before anything is registered.
// The overlay's own read (QueryDeploymentHealth) is where a not-yet-registered app
// surfaces, with a precondition error naming that fact.
func (h *appHooks) handleOperatedAppID() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("projectID")
		if projectID == "" {
			http.Error(w, "missing projectID", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(operatedAppIDResponse{OperatedAppID: OperatedAppIDForProject(projectID).String()}); err != nil {
			h.logger.Warn("operated-app-id: failed writing response", "err", err)
		}
	}
}

// projectScopedOperationsManager enforces D13 at the one write that creates an operated
// app. RegisterOperatedApp takes an operatedAppId AND a projectRef as independent
// parameters, so nothing in the type system stops a caller seeding a row under an id no
// consumer will ever derive — and because every consumer DERIVES the id rather than
// reading it back from anywhere, such a row is not a visible error but a permanent dead
// end: the console and the health overlay would both address the derived id and find
// nothing, forever.
//
// The guard sits at the composition root rather than inside the Manager façade because
// the derivation does (see OperatedAppIDForProject). It covers every caller of either
// transport: WrapManagers wraps the SAME OperationsManager the REST handlers and the MCP
// tools both call. The embedded interface carries every other op through untouched, so a
// future contract op cannot silently bypass the wrapper by being forgotten here.
type projectScopedOperationsManager struct {
	operations.OperationsManager
}

func (m projectScopedOperationsManager) RegisterOperatedApp(rc fwmanager.Context, operatedAppID uuid.UUID, customerID uuid.UUID, projectRef string, deployableBundleRef string) (operations.Version, error) {
	if want := OperatedAppIDForProject(projectRef); projectRef != "" && operatedAppID != want {
		return 0, fwmanager.New(fwmanager.ContractMisuse,
			fmt.Sprintf("operatedAppId %s is not the id derived from projectRef %q (%s): an operated app's id IS its project's id (spec D13) — one operated app per project, derived, never chosen",
				operatedAppID, projectRef, want))
	}
	return m.OperationsManager.RegisterOperatedApp(rc, operatedAppID, customerID, projectRef, deployableBundleRef)
}

// projectScopedDeliveryManager authorizes the TWO delivery ops that name a project in
// their BODY against the project they name, which no generated handler can do:
// StartProject's ADOPT arm, and every QueryProjectView that carries a projectId.
//
// The other TEN ops carry their projectID in the PATH, and the
// http generator's convention turns a leading ID path param into
// `Authorize(verb, {Kind:"project", ID:<that id>})` — the ONE mechanism binding an
// authorization decision to a project (framework-go-http-generator httpgen/plan.go
// planOp/resourceKindFor). These two ids ride the BODY, so the same convention gives
// each the owner-scoped fallback `{Kind:"deliveryCatalog", ID:principal.Subject}`:
// "may this principal start projects / read project views at all", never "may it touch
// THAT project". A body id names a resource the catalog decision never sees.
//
// StartProject's id MUST ride the body (an ABSENT id is what means CREATE, and
// net/http's mux cannot match an empty path segment — see the op's doc comment).
// QueryProjectView's rides the body because the op folds THIRTEEN former readers into
// one: `projectId` is a member of the ProjectViewQuery object, not a parameter, and a
// query object cannot be a path segment. BOTH are stage-4a REGRESSIONS, not inherited
// gaps — before Task 6 every one of those readers was a path route
// (`system-design/get-project/{projectID}`, `get-session-state/{projectID}`,
// `get-design-health/{projectID}`, `list-episodes-for-artifact/{projectID}`,
// `get-episode-timeline/{projectID}`, `construction/get-pump-status/{projectID}`, …),
// each authorizing `{project, id}`. Unguarded, any principal who may see the catalog
// could read summary/session/pump/designHealth/episodes/timeline for ANY project id.
//
// So the guard re-asks the SAME question the path form asked, with the SAME verb and
// the SAME resource ref, at the composition root — the one place holding the wired PDP
// (PolicyDecisionPoint(); a Manager building its own would get the deny-by-default
// evaluator and refuse everything). It sits here rather than inside the Manager façade
// for the reason the sibling operations guard does: the decision belongs to the
// deployment's policy engine, not to the Method component. It covers BOTH transports,
// since WrapManagers hands the SAME instance to the REST handler and the MCP tools —
// and the MCP surface authorizes nothing of its own, so before this the adopt arm was
// unchecked there too.
//
// RULING (stage 4a pre-final): the generator is NOT changed to read a body id. A path
// id is the convention's identity signal, and teaching it to reach into a request
// wrapper for an optional id — or into a nested query object — would make every
// body-carried id a silent authorization surface, decided by a naming coincidence
// rather than by a reviewed line. Each body id that NAMES A PROJECT gets an explicit
// arm here instead, and the arm is what a reviewer reads. `ReviewDecisionInput.optionId`
// needs none: an option is a member of the project the PATH already named.
//
// The embedded interface carries every other op through untouched, so a future contract
// op cannot silently bypass the wrapper by being forgotten here.
type projectScopedDeliveryManager struct {
	delivery.DeliveryManager
	security security.Security
}

// StartProject denies an adopt whose principal may not act on the named project. A
// CREATE (projectID absent) is left to the handler's catalog decision — there is no
// project yet to name. The fail-closed arm mirrors the generated handlers exactly: an
// UNREACHABLE policy engine denies (security.Authorize's contract), and Unauthorized is
// the façade kind the client layer renders as 403 — the same status the path route gave.
func (m projectScopedDeliveryManager) StartProject(rc fwmanager.Context, owner delivery.OwnerScope, name string, projectID *string, model *delivery.OperatingModel, research *delivery.ResearchInput, start bool) (delivery.StartProjectResult, error) {
	if projectID != nil {
		decision, err := m.security.Authorize(rc.Context, rc.Principal,
			security.Action{Verb: "start-project"},
			security.ResourceRef{Kind: "project", ID: *projectID})
		if err != nil || !decision.Permit {
			return delivery.StartProjectResult{}, fwmanager.New(fwmanager.Unauthorized, "not permitted")
		}
	}
	return m.DeliveryManager.StartProject(rc, owner, name, projectID, model, research, start)
}

// QueryProjectView denies a read whose principal may not act on the project the query
// NAMES. It is the whole read surface: six of the seven kinds (summary, session, pump,
// designHealth, episodes, timeline) address ONE project through query.projectId, and
// each was a `{project, id}`-authorized path route before stage 4a folded them into this
// op.
//
// The `projects` kind is the one that stays CATALOG-scoped, and legitimately so: it
// carries an `owner` and no projectId, it is the catalog listing itself, and the
// handler's own `{deliveryCatalog, principal.Subject}` decision is exactly the question
// it asks. An empty (non-nil) projectId is left to the Manager, which answers the
// ContractMisuse that names the missing selector — a guard that denied it would turn a
// malformed query into a permissions error and tell the caller nothing true.
func (m projectScopedDeliveryManager) QueryProjectView(rc fwmanager.Context, query delivery.ProjectViewQuery) (delivery.ProjectView, error) {
	if query.ProjectID != nil && *query.ProjectID != "" {
		decision, err := m.security.Authorize(rc.Context, rc.Principal,
			security.Action{Verb: "query-project-view"},
			security.ResourceRef{Kind: "project", ID: *query.ProjectID})
		if err != nil || !decision.Permit {
			return delivery.ProjectView{}, fwmanager.New(fwmanager.Unauthorized, "not permitted")
		}
	}
	return m.DeliveryManager.QueryProjectView(rc, query)
}

// ArtifactAccessGitHubCloudArgs supplies the CLOUD artifactAccess ctor args: the
// construction-output repo + the GitHub-App identity (installationID coerced to int64).
func (h *appHooks) ArtifactAccessGitHubCloudArgs(cfg *Config) (string, string, string, string, string, int64) {
	return cfg.ArtifactRepoURL, cfg.ArtifactRepoOwner, cfg.GithubAppAppID, cfg.GithubAppPrivateKeyPEM, cfg.GithubAppAPIBaseURL, parseInt64(cfg.GithubAppInstallationID)
}

// AgenticJobAccessGitHubActionsArgs supplies the shared AppClient + the
// construction repo/workflow settings the GitHub-Actions pipeline dispatches through.
func (h *appHooks) AgenticJobAccessGitHubActionsArgs(cfg *Config) (*github.AppClient, string, string, string, string, int64) {
	return h.appClient, cfg.ConstructionRepoOwner, cfg.ConstructionRepoName, cfg.ConstructionWorkflowFile, cfg.ConstructionRef, parseInt64(cfg.GithubAppInstallationID)
}

// ProjectStateAccessGitHubArgs supplies the CLOUD projectStateAccess ports (webHost +
// account + the sourcecontrol-backed catalog + credential minter — the RA→RA bridge
// the projectstate package cannot import).
func (h *appHooks) ProjectStateAccessGitHubArgs(cfg *Config) (string, string, projectstate.ProjectCatalog, projectstate.CredentialMinter) {
	return h.projectStateCloudPorts(cfg)
}

// ConstructionTransitionAccessGitHubArgs / GitActivityStatusAccessGitHubArgs /
// DesignSessionAccessGitHubArgs (B6) supply the SAME CLOUD ports as
// ProjectStateAccessGitHubArgs above — these three secondary contracts build
// their own git substrate over the identical sourcecontrol-backed catalog +
// credential minter, never raw github-app credentials directly.
func (h *appHooks) ConstructionTransitionAccessGitHubArgs(cfg *Config) (string, string, projectstate.ProjectCatalog, projectstate.CredentialMinter) {
	return h.projectStateCloudPorts(cfg)
}

func (h *appHooks) GitActivityStatusAccessGitHubArgs(cfg *Config) (string, string, projectstate.ProjectCatalog, projectstate.CredentialMinter) {
	return h.projectStateCloudPorts(cfg)
}

func (h *appHooks) DesignSessionAccessGitHubArgs(cfg *Config) (string, string, projectstate.ProjectCatalog, projectstate.CredentialMinter) {
	return h.projectStateCloudPorts(cfg)
}

// ActivityExecutionAccessGitHubArgs supplies the FIFTH facet (stage 3) the same CLOUD
// ports as its four siblings: it is one more contract over the one git substrate, not a
// second store, so it must never be handed raw github-app credentials of its own.
func (h *appHooks) ActivityExecutionAccessGitHubArgs(cfg *Config) (string, string, projectstate.ProjectCatalog, projectstate.CredentialMinter) {
	return h.projectStateCloudPorts(cfg)
}

// ConstructionTransitionAccessGitLocalArgs / GitActivityStatusAccessGitLocalArgs /
// DesignSessionAccessGitLocalArgs (B6) supply the SAME LOCAL repoURL setting
// projectStateAccess's own GitLocal arm reads (cfg.ProjectStateGitRepoURL) — a hook
// purely to REUSE that existing binding-scoped setting rather than declare a
// duplicate one for the same env var.
func (h *appHooks) ConstructionTransitionAccessGitLocalArgs(cfg *Config) string {
	return cfg.ProjectStateGitRepoURL
}

func (h *appHooks) GitActivityStatusAccessGitLocalArgs(cfg *Config) string {
	return cfg.ProjectStateGitRepoURL
}

func (h *appHooks) DesignSessionAccessGitLocalArgs(cfg *Config) string {
	return cfg.ProjectStateGitRepoURL
}

// ActivityExecutionAccessGitLocalArgs reads the SAME binding-scoped setting its four
// siblings read — one on-disk repo, five facets over it.
func (h *appHooks) ActivityExecutionAccessGitLocalArgs(cfg *Config) string {
	return cfg.ProjectStateGitRepoURL
}

// SourceControlAccessGitLocalArgs supplies the GitLocal PR rail's repoURL — the SAME
// on-disk repo the projectstate GitLocal substrate and the local construction executor
// operate on (reusing the existing binding-scoped setting, like the three hooks above).
func (h *appHooks) SourceControlAccessGitLocalArgs(cfg *Config) string {
	return cfg.ProjectStateGitRepoURL
}

// EpisodeAccessLocalFSArgs (Task 8) supplies the LocalFS episodeAccess variant's
// repoURL — the SAME shared repo the GitLocal hooks above and the local construction
// executor already bind to (reusing the existing binding-scoped setting rather than
// declaring a duplicate one for the same env var).
func (h *appHooks) EpisodeAccessLocalFSArgs(cfg *Config) string {
	return cfg.ProjectStateGitRepoURL
}

// SourceControlAccessGitHubArgs supplies the shared AppClient + the App identity the
// sourcecontrol RA is built over.
func (h *appHooks) SourceControlAccessGitHubArgs(cfg *Config) (*github.AppClient, string, string, bool) {
	return h.appClient, cfg.GithubAppAccount, cfg.GithubAppAppSlug, true /* repoPrivate */
}

// FinalizeArtifactAccess swaps the profile-built artifactAccess for the in-memory
// dry-run stub when CONSTRUCTION_DRYRUN=true (residual #1: this contaminates the
// SHARED operations artifact dep in dry-run dev profiles). Identity for cloud.
func (h *appHooks) FinalizeArtifactAccess(cfg *Config, v artifact.ArtifactAccess) artifact.ArtifactAccess {
	if cfg.ConstructionDryRun {
		return artifact.NewDryRunArtifactAccess()
	}
	return v
}

// FinalizeAgenticJobAccess resolves the construction pipeline — THREE
// dispatch arms, in order:
//   - CONSTRUCTION_DRYRUN=true → the in-memory dry-run stub (the stubbed pump runs
//     end-to-end with no real dispatch of any kind);
//   - otherwise v when the profile arm built it (cloud);
//   - otherwise the github-creds-gated real GitHub-Actions pipeline (local profile
//     WITH creds — the agenticJobAccess binding is cloud-arm-only, but
//     its real presence is gated on the App creds, orthogonal to the projectstate
//     profile) — creds present keeps this exact pre-existing behavior;
//   - otherwise the LOCAL construction executor (Task 6: local profile WITHOUT
//     GitHub creds → headless claude, no GitHub Actions involved), or nil on the
//     cloud profile with no creds (the pump stays dormant, same as before Task 6).
func (h *appHooks) FinalizeAgenticJobAccess(cfg *Config, v agenticjob.AgenticJobAccess) agenticjob.AgenticJobAccess {
	if cfg.ConstructionDryRun {
		return agenticjob.NewDryRunAgenticJobAccess()
	}
	if v != nil {
		return v
	}
	if h.realPipeline != nil {
		return h.realPipeline
	}
	return h.localPipeline
}

// FinalizeSourceControlAccess resolves sourceControlAccess CREDS-WIN: the
// github-creds-gated real RA when present (local profile WITH creds — the agentic
// systemtests boot — keeps the REAL GitHub rail, pairing with realPipeline exactly as
// FinalizeAgenticJobAccess pairs), else the profile arm's build (cloud
// GitHub, or the local GitLocal PR rail), else nil (rail dormant — a creds-less cloud
// boot). No dry-run stub — the PR rail simply goes dormant when the RA is nil.
func (h *appHooks) FinalizeSourceControlAccess(_ *Config, v sourcecontrol.SourceControlAccess) sourcecontrol.SourceControlAccess {
	if h.scAccess != nil {
		return h.scAccess
	}
	return v
}

// FinalizeOperatedRuntimeAccess restores the ARCHISTRATOR_OPERATIONS_DRYRUN toggle
// composegen v0.5.1 now gives a typed seam for (A2: a REQUIRED, profile-keyed
// binding needs the same orthogonal per-component override as an optional one).
// The pre-appgen hand run() selected the operatedRuntimeAccess PROFILE purely from
// OperationsDryRun — operationsRuntimeProfile(cfg): DryRun ⇒ RuntimeProfileLocal,
// else RuntimeProfileReal — ORTHOGONAL to the deployment profile (cloud/local)
// that now drives the generated binding's variant switch (main.gen.go: cloud arm
// builds NewRealOperatedRuntimeAccess, local arm builds NewLocalOperatedRuntimeAccess
// directly, so the toggle only has an actual swap to make in the cloud arm).
// Reproduce that: when OperationsDryRun is set AND the deployment profile is
// "cloud" (so v is the Real variant), swap it for the package's own deterministic
// Local constructor (operatedruntime.NewLocalOperatedRuntimeAccess — reused
// verbatim, no new construction logic here). Cloud + DRYRUN=false, and local
// (already Local either way), are both identity.
func (h *appHooks) FinalizeOperatedRuntimeAccess(cfg *Config, v operatedruntime.OperatedRuntimeAccess) operatedruntime.OperatedRuntimeAccess {
	if cfg.OperationsDryRun && resolveProfile(cfg) == "cloud" {
		return operatedruntime.NewLocalOperatedRuntimeAccess()
	}
	return v
}

// The remaining Finalize<Component> hooks (A2/B3: composegen v0.5.1 emits one for
// EVERY constructed binding, required bindings included) have no composition-root
// policy to apply — identity. Each is a required, single-arm stub/profile binding
// with no orthogonal toggle of its own (unlike operatedRuntimeAccess above).

// FinalizeBillingStateAccess is identity — billingStateAccess is the required,
// arm-less stub binding; no composition-root policy applies.
func (h *appHooks) FinalizeBillingStateAccess(_ *Config, v billingstate.BillingStateAccess) billingstate.BillingStateAccess {
	return v
}

// FinalizeMerchantGatewayAccess swaps the generated stub (fwra.Unknown,
// "not implemented" on every op) for the package's own not-configured
// implementation. manager/billing and engine/billing are real, wired components;
// resourceaccess/billingstate is NOT — it's still the generated stub (every op
// returns fwra.Unknown, "not implemented"), earmarked as retry-waste debt in
// docs/bugs/2026-08-09-construction-open-items.md; real state access returns with
// the settlement re-plan (docs/billing-setup.md). This hook only fixes
// merchantGatewayAccess: its generated stub's Unknown kind isn't in
// gatewayActivityOptions' TerminalRA set (manager/billing/billingmanager.go), so
// callers burned a full retry budget before failing anyway with a message that gave
// no path forward. The not-configured implementation fails terminally (fwra.Auth)
// with a message pointing at docs/billing-setup.md. Unconditional — Stripe is not
// configured in ANY profile yet, so there is no cloud/local or dry-run split here
// (unlike FinalizeOperatedRuntimeAccess above).
func (h *appHooks) FinalizeMerchantGatewayAccess(_ *Config, _ merchantgateway.MerchantGatewayAccess) merchantgateway.MerchantGatewayAccess {
	return merchantgateway.NewNotConfiguredMerchantGatewayAccess()
}

// FinalizeOperatedSystemStateAccess is identity — operatedSystemStateAccess is
// profile-switched (cloud: pgx; local: same pgx-backed store, per the corrected
// UC4 finding that it is real in both profiles) with no orthogonal toggle; no
// composition-root policy applies.
func (h *appHooks) FinalizeOperatedSystemStateAccess(_ *Config, v operatedsystemstate.OperatedSystemStateAccess) operatedsystemstate.OperatedSystemStateAccess {
	return v
}

// FinalizeProjectStateAccess is identity — projectStateAccess's dev/cloud split is
// already the deployment-profile switch (local git vs. GitHub-backed); no
// additional orthogonal toggle applies.
func (h *appHooks) FinalizeProjectStateAccess(_ *Config, v projectstate.ProjectStateAccess) projectstate.ProjectStateAccess {
	return v
}

// FinalizeUsageAccess is identity — usageAccess is profile-switched with no
// orthogonal toggle; no composition-root policy applies.
func (h *appHooks) FinalizeUsageAccess(_ *Config, v usage.UsageAccess) usage.UsageAccess {
	return v
}

// FinalizeConstructionTransitionAccess / FinalizeGitActivityStatusAccess /
// FinalizeDesignSessionAccess (B6) are identity — the three secondary git-substrate
// contracts are profile-switched exactly like projectStateAccess, with no
// orthogonal toggle of their own.
func (h *appHooks) FinalizeConstructionTransitionAccess(_ *Config, v projectstate.ConstructionTransitionAccess) projectstate.ConstructionTransitionAccess {
	return v
}

func (h *appHooks) FinalizeGitActivityStatusAccess(_ *Config, v projectstate.GitActivityStatusAccess) projectstate.GitActivityStatusAccess {
	return v
}

func (h *appHooks) FinalizeDesignSessionAccess(_ *Config, v projectstate.DesignSessionAccess) projectstate.DesignSessionAccess {
	return v
}

// FinalizeActivityExecutionAccess is identity for the same reason the other three are:
// the facet is profile-switched exactly like projectStateAccess, with no orthogonal
// toggle of its own.
func (h *appHooks) FinalizeActivityExecutionAccess(_ *Config, v projectstate.ActivityExecutionAccess) projectstate.ActivityExecutionAccess {
	return v
}

// FinalizeEpisodeAccess is identity — the profile arm's build is always the right
// episodeAccess (SP1 capture-seam). Required by the generated Hooks interface the moment
// the three design/construction Managers took the episodeAccess dep; the CONSTRUCTOR
// binding for the LocalFS/NoOp variants is composition-root work that does not live here
// (see main.gen.go's per-profile switch).
func (h *appHooks) FinalizeEpisodeAccess(_ *Config, v episode.EpisodeAccess) episode.EpisodeAccess {
	return v
}

// FinalizeRevenueLedgerAccess is identity — revenueLedgerAccess is the required,
// arm-less stub binding (billingstate.NewRevenueLedgerAccess, a permanent no-op per
// the charge-only R-013 rationale); no composition-root policy applies.
func (h *appHooks) FinalizeRevenueLedgerAccess(_ *Config, v billingstate.RevenueLedgerAccess) billingstate.RevenueLedgerAccess {
	return v
}

// MessageBusTemporalArgs supplies messageBus's ONE constructor argument the
// deployment model cannot express (Task 7c): the ExecutionKind -> KindBinding
// table messagebus.NewTemporalMessageBus's registry resolves every
// RegisterSchedule call against (both the three managers' STARTUP schedules
// below and any future workflow-invoked registerSchedule call, e.g. billing's
// per-customer closeBillingCycle:<id>). composegen threads the dialed `tc` as
// a separate positional arg ahead of this hook call — see the generated
// main.gen.go call site — so this hook supplies ONLY the table.
//
// NOT compiler-linked to the managers' own private executionKind* constants
// (they are unexported; a cross-package reference is impossible) — this table
// is the single hand-maintained source of truth pairing each logical
// ExecutionKind string with its registered Temporal workflow-type name (always
// identical today, since every RegisterSchedule caller passes its own
// executionKind* constant as both) and the owning Manager's TaskQueue. A
// rename of any of these constants must be mirrored here; nothing else
// enforces it.
func (h *appHooks) MessageBusTemporalArgs(_ *Config) map[messagebus.ExecutionKind]messagebus.KindBinding {
	return map[messagebus.ExecutionKind]messagebus.KindBinding{
		// billing: the startup shortfallSweep Schedule (billingmanager.go's
		// executionKindShortfallSweep) + the per-customer closeBillingCycle:<id>
		// Schedule registered at onboarding (executionKindClose).
		"billingShortfallSweep": {WorkflowType: "billingShortfallSweep", TaskQueue: managerbilling.TaskQueue},
		"billingCloseCycle":     {WorkflowType: "billingCloseCycle", TaskQueue: managerbilling.TaskQueue},
		// operations: the startup operatedStateReconcile Schedule
		// (operationsmanager.go's executionKindReconcile).
		"operationsReconcile": {WorkflowType: "operationsReconcile", TaskQueue: operations.TaskQueue},
		// delivery: the two startup Schedules (Task 7c; deliverymanager.go's
		// executionKindPumpSweep/executionKindReplanSweep). The WORKFLOW TYPE names are
		// unchanged at stage 4a (R2 — nineteen replay fixtures replay against them); only
		// the task queue and the Schedule ids moved to the delivery namespace.
		"constructionPumpSweep":   {WorkflowType: "constructionPumpSweep", TaskQueue: delivery.TaskQueue},
		"constructionReplanSweep": {WorkflowType: "constructionReplanSweep", TaskQueue: delivery.TaskQueue},
	}
}

// FinalizeMessageBus is identity, EXCEPT when CONSTRUCTION_DRYRUN=true: it then
// wraps messageBus so the two platform-wide construction Schedules (pump sweep
// 30s / replan sweep 5m — registered via construction.RegisterSchedules at the
// generated main's RegisterConstructionManagerWorker gate, Task 7c) are never
// actually created against the runtime. A dry-run boot's construction pipeline
// is already the in-memory stub (FinalizeAgenticJobAccess above), so there is
// nothing real for the sweeps to drive — a live Schedule ticking every 30s
// against a dry-run/dev/demo boot is pure noise and, on a shared dev Temporal
// namespace, a needless collision risk with a real deployment's identical
// schedule ids. billing/operations RegisterSchedule, and every DeliverSignal
// call from ANY Manager (construction's queued M→M edges included), pass
// through unchanged — see dryRunConstructionScheduleGate. The generated
// main.gen.go call sites (RegisterSchedules for all three managers, plus every
// workflow-invoked MessageBusRegisterSchedule/DeliverSignal Activity) are never
// hand-edited; this hook is the one seam FinalizeMessageBus already gives the
// composition root to intercept what the shared messageBus instance does.
func (h *appHooks) FinalizeMessageBus(cfg *Config, v messagebus.MessageBus) messagebus.MessageBus {
	if !cfg.ConstructionDryRun {
		return v
	}
	return dryRunConstructionScheduleGate{inner: v, kinds: h.constructionExecutionKinds(cfg), logger: h.logger}
}

// constructionExecutionKinds returns the ExecutionKinds bound to construction's
// own TaskQueue in MessageBusTemporalArgs's table above — the SAME table the
// composition root feeds messagebus.NewTemporalMessageBus — so
// dryRunConstructionScheduleGate can never drift from "which kinds are
// construction's" as that table evolves; a future construction Schedule added
// there is picked up automatically.
func (h *appHooks) constructionExecutionKinds(cfg *Config) map[messagebus.ExecutionKind]bool {
	kinds := make(map[messagebus.ExecutionKind]bool)
	for kind, binding := range h.MessageBusTemporalArgs(cfg) {
		if binding.TaskQueue == delivery.TaskQueue {
			kinds[kind] = true
		}
	}
	return kinds
}

// dryRunConstructionScheduleGate wraps messageBus so a RegisterSchedule call
// naming one of construction's own ExecutionKinds is skipped (logged, not
// forwarded) while every other call — billing/operations RegisterSchedule, and
// DeliverSignal for any Manager — passes straight through to inner. Built only
// by FinalizeMessageBus above, only when CONSTRUCTION_DRYRUN=true.
type dryRunConstructionScheduleGate struct {
	inner  messagebus.MessageBus
	kinds  map[messagebus.ExecutionKind]bool
	logger *slog.Logger
}

var _ messagebus.MessageBus = dryRunConstructionScheduleGate{}

// DeliverSignal always passes through — the gate only intercepts Schedule
// registration.
func (g dryRunConstructionScheduleGate) DeliverSignal(rc fwra.Context, targetExecutionID messagebus.ExecutionID, signalName messagebus.SignalName, payload messagebus.ExecutionPayload) error {
	return g.inner.DeliverSignal(rc, targetExecutionID, signalName, payload)
}

// RegisterSchedule skips (returns nil, no runtime call) when spec.ExecutionKind
// is one of construction's own kinds; every other kind (billing/operations)
// delegates to inner unchanged.
func (g dryRunConstructionScheduleGate) RegisterSchedule(rc fwra.Context, scheduleID messagebus.ScheduleID, spec messagebus.ScheduleSpec) error {
	if g.kinds[spec.ExecutionKind] {
		g.logger.Info("messageBus.RegisterSchedule skipped — CONSTRUCTION_DRYRUN=true", "scheduleID", scheduleID, "executionKind", spec.ExecutionKind)
		return nil
	}
	return g.inner.RegisterSchedule(rc, scheduleID, spec)
}

// registerConstruction is the construction Worker gate (run()'s selectConstructionDeps):
// register when dry-run, or when the artifact store is configured AND an executor
// is available for it to dispatch through — the cloud GitHub-Actions repo config
// (cloud profile) OR the local profile itself (Task 6: the local construction
// executor needs no GitHub construction-repo config at all — headless claude runs
// against the on-disk repo directly).
func registerConstruction(cfg *Config) bool {
	if cfg.ConstructionDryRun {
		return true
	}
	if cfg.ArtifactRepoURL == "" {
		return false
	}
	if resolveProfile(cfg) == "local" {
		return true
	}
	return cfg.ConstructionRepoOwner != "" && cfg.ConstructionRepoName != ""
}

// RegisterDeliveryManagerWorker registers UNCONDITIONALLY at stage 4a. Before the
// collapse the construction worker was gated on registerConstruction(cfg) while the two
// design workers registered unconditionally; one worker now carries all eleven workflow
// types, so gating it would take the design rails down with construction. The dry-run /
// no-repo restraint the gate expressed has not been lost — it lives on the SCHEDULES
// (FinalizeMessageBus's dryRunConstructionScheduleGate), which is where "do not actually
// drive construction" belongs. registerConstruction stays as that gate's input.
func (h *appHooks) RegisterDeliveryManagerWorker(_ *Config) bool { return true }

// Operations registers unconditionally too: its optional-dormant deps simply go dormant
// when absent — the operations workflows run unchanged (hand run() always registered them).
func (h *appHooks) RegisterOperationsManagerWorker(_ *Config) bool { return true }

func (h *appHooks) DeliveryManagerEscalationWaitTimeout() time.Duration {
	return h.config.ConstructionEscalationTimeout
}

func (h *appHooks) DeliveryManagerInterventionMode() string {
	return h.config.ConstructionInterventionMode
}

// DeliveryManagerRepo is the ONE repo resolver the merged Manager threads into all
// three moved rails: projectID → the project's own RepoRef via the sourcecontrol
// catalog (name-as-identity). Non-nil retargets every construction dispatch to the
// project repo (aiarch-construct.yml) AND activates the branch→PR rail; nil (a
// repo-less server) keeps the central-repo fallback + a dormant rail.
//
// PER-RAIL BEHAVIOUR IS PRESERVED THROUGH ONE DEP (stage 4a fix round 1). The three
// hooks this replaces did NOT agree: the two DESIGN hooks carried a second arm — when
// no GitHub App catalog is bound AND the profile is "local", resolve every project to
// the deterministic GitLocal RepoRef, which is what activates the local design rail's
// branch → PR → merge lifecycle — while the CONSTRUCTION hook returned nil there
// ("construction keeps its local-merge-job flow this pass"). Collapsing to the
// construction body would have taken the local design rail down; collapsing to the
// design body would have switched construction's local PR rail ON. Neither is
// acceptable, and no contract change is needed to avoid both: this hook returns the
// DESIGN arm (the superset), and the construction rail recognises a GitLocal ref at its
// single use site and treats it as "no per-project venue", which is exactly the nil it
// used to be handed (constructRepoTarget, internal/manager/delivery/constructactivity.go).
// The selection moved; no rail's behaviour did.
func (h *appHooks) DeliveryManagerRepo() func(projectID delivery.ProjectID) (sourcecontrol.RepoRef, bool) {
	if h.scCatalog == nil {
		if h.gitLocalRailBound() {
			return func(pid delivery.ProjectID) (sourcecontrol.RepoRef, bool) {
				return sourcecontrol.GitLocalRepoRefForProject(sourcecontrol.ProjectID(projectstate.ProjectID(pid).String())), true
			}
		}
		return nil
	}
	return func(pid delivery.ProjectID) (sourcecontrol.RepoRef, bool) {
		return h.repoForProject(projectstate.ProjectID(pid))
	}
}

// gitLocalRailBound reports whether the bound sourceControlAccess is the GitLocal PR
// rail: the local profile's binding arm builds it, and the creds-win Finalize keeps it
// only when no GitHub App creds exist (scCatalog/scAccess nil). The design rails' repo
// resolution must then resolve every project to the deterministic local RepoRef so the
// rail lifecycle (branch → PR → merge) activates.
func (h *appHooks) gitLocalRailBound() bool {
	return resolveProfile(h.config) == "local"
}

// repoForProject resolves one project's RepoRef through the sourcecontrol catalog,
// warning + returning ok=false (rail dormant for that project) on a resolution miss.
func (h *appHooks) repoForProject(projectID projectstate.ProjectID) (sourcecontrol.RepoRef, bool) {
	railAccount := sourcecontrol.AccountRef(h.config.GithubAppAccount)
	ref, err := h.scCatalog.RepoRefForProject(railAccount, sourcecontrol.ProjectID(projectID.String()))
	if err != nil {
		h.logger.Warn("design PR rail: could not resolve RepoRef for project; rail dormant for this project", "projectID", projectID, "err", err)
		return sourcecontrol.RepoRef(""), false
	}
	return ref, true
}

// DeliveryManagerRepoBase is the project-wide construction-repo WEB base the
// delivery Manager composes each git row's clickable prUrl from; "" when the
// construction repo is unconfigured.
func (h *appHooks) DeliveryManagerRepoBase() string {
	c := h.config
	return constructionRepoBase(c.GithubAppAPIBaseURL, c.ConstructionRepoOwner, c.ConstructionRepoName)
}

// constructionRepoBase composes the construction-repo WEB base
// (<host>/<owner>/<repo>) — "" when owner or repo is empty. The WEB host differs
// from the API host: github.com (not api.github.com), or the GHES API root with the
// /api/v3 REST suffix stripped.
func constructionRepoBase(apiBaseURL, owner, repo string) string {
	if owner == "" || repo == "" {
		return ""
	}
	host := "https://github.com"
	if base := strings.TrimRight(strings.TrimSpace(apiBaseURL), "/"); base != "" {
		host = strings.TrimSuffix(base, "/api/v3")
	}
	return host + "/" + owner + "/" + repo
}
