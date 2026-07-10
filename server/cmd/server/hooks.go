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
// TWO REVIEWED RESIDUALS of the fixed generated seam (both dev-profile-only;
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
//  2. SEPARATE CONSTRUCTION-PORTS psa INSTANCE. The
//     ConstructionManagerConstructionTransition/GitActivityStatus hooks take no
//     args, so they cannot see the projectStateAccess the generated body
//     constructs; this file builds its OWN projectStateAccess (identical profile
//     switch) to extract the git construction ports. For a git store both
//     instances address the SAME repo, so head-state stays consistent — the
//     duplication is an instance-count residual, not a correctness one.

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	github "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	keycloak "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-keycloak"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	tlog "go.temporal.io/sdk/log"

	"github.com/mixofreality-studio/archistrator/server/internal/client/web"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// appHooks implements the generated Hooks interface. Built once by newAppHooks.
type appHooks struct {
	logger *slog.Logger
	// config is the resolved *Config the no-arg hooks (repo resolvers,
	// construction-transition ports, repoBase, escalation/mode scalars) close over —
	// the generated Hooks signatures for those seams take no cfg argument.
	config *Config

	// appClient is the shared GitHub-App satellite the sourcecontrol +
	// constructionpipeline variant-arg hooks spread into their generated DI ctors;
	// nil when the App identity is unconfigured (repo-less dev / local profile).
	appClient *github.AppClient
	// scCatalog is the sourcecontrol catalog surface backing the projectstate CLOUD
	// ports + the design PR-rail repo resolvers; nil when repo-less.
	scCatalog sourcecontrol.SourceControlCatalogAccess

	// construction-transition + git-activity-status ports, extracted from a
	// composition-root-owned projectStateAccess (residual #2). nil for a non-git
	// projectStateAccess (never today — projectStateAccess is git-only).
	constructionTransition projectstate.ConstructionTransitionAccess
	gitActivityStatus      projectstate.GitActivityStatusAccess
}

// newAppHooks builds the composition-root hook state from the resolved *Config: the
// shared GitHub-App satellite + sourcecontrol catalog (when the App identity is
// configured), and the git construction-transition ports (from a psa mirroring the
// generated profile switch). Fail-fast: a malformed App key / catalog build surfaces
// here so main() exits before the boot walk.
func newAppHooks(cfg *Config, logger *slog.Logger) (*appHooks, error) {
	h := &appHooks{logger: logger, config: cfg}

	// Shared GitHub-App satellite + catalog surface — built once when the App
	// identity + account are configured (the CLOUD profile). Nil otherwise: a dev
	// server with no GitHub creds runs repo-less (design rail dormant), exactly as
	// the hand run() did.
	if cfg.GithubAppAppID != "" && cfg.GithubAppPrivateKeyPEM != "" && cfg.GithubAppAccount != "" {
		app, err := github.NewAppClient(cfg.GithubAppAppID, cfg.GithubAppPrivateKeyPEM, cfg.GithubAppAPIBaseURL)
		if err != nil {
			return nil, err
		}
		h.appClient = app
		scCatalog, _, err := sourcecontrol.NewGitHubSourceControl(app, cfg.GithubAppAccount, cfg.GithubAppAppSlug, true /* repoPrivate */)
		if err != nil {
			return nil, err
		}
		h.scCatalog = scCatalog
		logger.Info("sourceControlAccess (github) ready", "account", cfg.GithubAppAccount, "apiBaseURL", cfg.GithubAppAPIBaseURL)
	} else {
		logger.Warn("sourceControlAccess NOT configured — projects are created repo-less (set ARCHISTRATOR_GITHUB_APP_ID + ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM + ARCHISTRATOR_GITHUB_ACCOUNT for live GitHub repo provisioning)")
	}

	// Construction-transition + git-activity-status ports (residual #2): build a
	// projectStateAccess mirroring the generated profile switch and extract its git
	// ports. This instance is SEPARATE from the managers' psa; consistent because
	// both address the same git repo.
	psa, err := h.designProjectState(cfg)
	if err != nil {
		return nil, err
	}
	if psa != nil {
		if t, s, ok := projectstate.GitConstructionPorts(psa); ok {
			h.constructionTransition, h.gitActivityStatus = t, s
			logger.Info("constructionManager → git substrate (shares the design head-state store; status cascade live)")
		}
	}

	return h, nil
}

// designProjectState builds a projectStateAccess mirroring the generated body's
// profile switch (LOCAL git on-disk, or CLOUD GitHub with the sourcecontrol-backed
// ports). Returns nil only when neither profile applies (unreachable —
// projectStateAccess is git-only; the generated body errors on that default arm).
func (h *appHooks) designProjectState(cfg *Config) (projectstate.ProjectStateAccess, error) {
	switch resolveProfile(cfg) {
	case "local":
		return projectstate.NewGitLocalProjectStateAccess(cfg.ProjectStateGitRepoURL), nil
	case "cloud":
		if h.scCatalog == nil {
			return nil, nil //nolint:nilnil // cloud profile with no App creds is not bootable; the generated projectStateAccess GitHub arm surfaces it. Repo-less design ports are simply unavailable.
		}
		webHost, account, catalog, minter := h.projectStateCloudPorts(cfg)
		return projectstate.NewGitHubProjectStateAccess(webHost, account, catalog, minter)
	default:
		return nil, nil //nolint:nilnil // unreachable: the generated body errors on a profile with no projectStateAccess arm.
	}
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

// WrapManagers decorates the four web/MCP-exposed managers with the composition-root
// logging seam (managerlog.go): every Infrastructure-kind error surfaced to a client
// — through either transport — is logged once server-side with op/projectID/cause.
func (h *appHooks) WrapManagers(managers WebManagers) WebManagers {
	return WebManagers{
		ConstructionManager:  loggingConstructionManager{inner: managers.ConstructionManager, log: h.logger},
		OperationsManager:    loggingOperationsManager{inner: managers.OperationsManager, log: h.logger},
		ProjectDesignManager: loggingProjectDesignManager{inner: managers.ProjectDesignManager, log: h.logger},
		SystemDesignManager:  loggingSystemDesignManager{inner: managers.SystemDesignManager, log: h.logger},
	}
}

// ExtraMounts adds the composition-root-only routes behind the same auth boundary:
// GET /api/userinfo (the SPA session probe — not a manager op) and /mcp (the MCP
// transport over the SAME four wrapped managers the REST handlers use).
func (h *appHooks) ExtraMounts(root *http.ServeMux, cfg *Config, dev web.DevConfig, validator security.Validator, managers WebManagers) {
	root.Handle("GET /api/userinfo", web.AuthMiddleware(dev, validator)(http.HandlerFunc(security.UserInfoHandler)))
	root.Handle("/mcp", newMCPHandler(dev, validator,
		managers.SystemDesignManager, managers.ProjectDesignManager, managers.ConstructionManager, managers.OperationsManager))
}

// ArtifactAccessGitHubCloudArgs supplies the CLOUD artifactAccess ctor args: the
// construction-output repo + the GitHub-App identity (installationID coerced to int64).
func (h *appHooks) ArtifactAccessGitHubCloudArgs(cfg *Config) (string, string, string, string, string, int64) {
	return cfg.ArtifactRepoURL, cfg.ArtifactRepoOwner, cfg.GithubAppAppID, cfg.GithubAppPrivateKeyPEM, cfg.GithubAppAPIBaseURL, parseInt64(cfg.GithubAppInstallationID)
}

// ConstructionPipelineAccessGitHubActionsArgs supplies the shared AppClient + the
// construction repo/workflow settings the GitHub-Actions pipeline dispatches through.
func (h *appHooks) ConstructionPipelineAccessGitHubActionsArgs(cfg *Config) (*github.AppClient, string, string, string, string, int64) {
	return h.appClient, cfg.ConstructionRepoOwner, cfg.ConstructionRepoName, cfg.ConstructionWorkflowFile, cfg.ConstructionRef, parseInt64(cfg.GithubAppInstallationID)
}

// ProjectStateAccessGitHubArgs supplies the CLOUD projectStateAccess ports (webHost +
// account + the sourcecontrol-backed catalog + credential minter — the RA→RA bridge
// the projectstate package cannot import).
func (h *appHooks) ProjectStateAccessGitHubArgs(cfg *Config) (string, string, projectstate.ProjectCatalog, projectstate.CredentialMinter) {
	return h.projectStateCloudPorts(cfg)
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

// FinalizeConstructionPipelineAccess swaps the pipeline for the in-memory dry-run
// stub when CONSTRUCTION_DRYRUN=true (the stubbed pump runs end-to-end with no real
// GitHub Actions run). Identity for cloud.
func (h *appHooks) FinalizeConstructionPipelineAccess(cfg *Config, v constructionpipeline.ConstructionPipelineAccess) constructionpipeline.ConstructionPipelineAccess {
	if cfg.ConstructionDryRun {
		return constructionpipeline.NewDryRunConstructionPipelineAccess()
	}
	return v
}

// FinalizeSourceControlAccess is identity — sourceControlAccess has no dry-run stub
// (the PR rail just goes dormant when the RA is nil).
func (h *appHooks) FinalizeSourceControlAccess(cfg *Config, v sourcecontrol.SourceControlAccess) sourcecontrol.SourceControlAccess {
	return v
}

// registerConstruction is the construction Worker gate (run()'s selectConstructionDeps):
// register when dry-run, or when BOTH external-effect deps are configured (the
// pipeline repo + the artifact store).
func registerConstruction(cfg *Config) bool {
	if cfg.ConstructionDryRun {
		return true
	}
	pipelinePresent := cfg.ConstructionRepoOwner != "" && cfg.ConstructionRepoName != ""
	artifactPresent := cfg.ArtifactRepoURL != ""
	return pipelinePresent && artifactPresent
}

func (h *appHooks) RegisterConstructionManagerWorker(cfg *Config) bool {
	return registerConstruction(cfg)
}

// The design + operations managers register unconditionally: their optional-dormant
// deps (the PR rail / artifact store) simply go dormant when absent — the CoAuthor
// spine + the operations workflows run unchanged (hand run() always registered them).
func (h *appHooks) RegisterOperationsManagerWorker(cfg *Config) bool    { return true }
func (h *appHooks) RegisterProjectDesignManagerWorker(cfg *Config) bool { return true }
func (h *appHooks) RegisterSystemDesignManagerWorker(cfg *Config) bool  { return true }

func (h *appHooks) ConstructionManagerConstructionTransition() projectstate.ConstructionTransitionAccess {
	return h.constructionTransition
}

func (h *appHooks) ConstructionManagerGitActivityStatus() projectstate.GitActivityStatusAccess {
	return h.gitActivityStatus
}

func (h *appHooks) ConstructionManagerEscalationWaitTimeout() time.Duration {
	return h.config.ConstructionEscalationTimeout
}

func (h *appHooks) ConstructionManagerInterventionMode() string {
	return h.config.ConstructionInterventionMode
}

// ProjectDesignManagerRepo / SystemDesignManagerRepo are the design PR-rail repo
// resolvers (run()'s buildDesignRepoResolvers): projectID → per-project RepoRef via
// the sourcecontrol catalog (name-as-identity). nil when repo-less (rail dormant).
func (h *appHooks) ProjectDesignManagerRepo() func(projectID projectdesign.ProjectID) (sourcecontrol.RepoRef, bool) {
	if h.scCatalog == nil {
		return nil
	}
	return func(pid projectdesign.ProjectID) (sourcecontrol.RepoRef, bool) {
		return h.repoForProject(projectstate.ProjectID(pid))
	}
}

func (h *appHooks) SystemDesignManagerRepo() func(projectID systemdesign.ProjectID) (sourcecontrol.RepoRef, bool) {
	if h.scCatalog == nil {
		return nil
	}
	return func(pid systemdesign.ProjectID) (sourcecontrol.RepoRef, bool) {
		return h.repoForProject(projectstate.ProjectID(pid))
	}
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

// SystemDesignManagerRepoBase is the project-wide construction-repo WEB base the
// systemDesignManager composes each git row's clickable prUrl from; "" when the
// construction repo is unconfigured.
func (h *appHooks) SystemDesignManagerRepoBase() string {
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
