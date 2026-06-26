// Command server is the archistrator server composition root (designs/aiarch
// codename; BUILD-LOCATION.md). It wires the UC1 system-design slice end to end:
//
//	External → Envoy Gateway [TLS + Keycloak JWT validation] → in-process
//	webClient → systemDesignManager (via its typed ops, which own Temporal) →
//	embedded Temporal Worker in this process.
//
// (operational-concepts.md §1 topology; §18 Temporal cloud profile wired to the
// in-cluster Temporal frontend.)
//
// This file is OUTSIDE internal/, so it is not scanned by the Method arch checker
// (TestMethodLayering) and may freely import Temporal — it is the composition
// root that constructs the Manager's Temporal client and the embedded Worker. The
// Client (internal/client/web) imports no Temporal; the Manager owns it.
//
// Responsibilities (in order): load env config → construct the real RAs against
// the in-cluster infra (Temporal client, Postgres pool, Gitea, and the workerAccess
// provider — Anthropic in production, Ollama in systemtests) →
// construct the security Utility → construct systemDesignManager → register its
// workflows + activities on an embedded Temporal Worker → start the Worker →
// construct the webClient and mount its routes behind the auth middleware → start
// the HTTP server → graceful shutdown on SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.temporal.io/sdk/client"
	temporalotel "go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/client/web"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	enginesettlement "github.com/mixofreality-studio/archistrator/server/internal/engine/settlement"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usagelog"
	workeraccess "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/worker"

	githubinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	keycloak "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-keycloak"
	otelinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-otel"
	postgresinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres"
	temporalprop "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-temporal"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/telemetry"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

// buildDesignProjectState selects the projectStateAccess substrate the UC1/UC2 design
// managers consume (I-GIT-DESIGN). It always returns the no-cred
// projectstate.ProjectStateAccess interface; the credential threading is hidden behind
// the composition-root projectStateGitAdapter (projectstate_git_adapter.go).
//
// Selection (in order):
//  1. LOCAL git  — ProjectStateGitLocal=true: file:// on-disk repo, no credential.
//     The per-project repo URL is taken verbatim from config (the embedded profile and
//     the I-GIT-DESIGN local-git proof point this at a throwaway on-disk repo). The
//     catalog is discovered by scanning the on-disk repo (no registry index).
//  2. CLOUD git  — a wired sourceControlAccess (GitHub App + account): the per-project
//     repos live under the account as <account>/<name>.git, where <name> IS the project
//     identity (name-as-identity, C-PA-AD 2026-06-15 — the "aiarch-<id>" prefix is
//     dropped); the catalog is discovered by enumerating the App installation's
//     aiarch-project repos (founder ruling 2026-06-14 — no registry index repo); the
//     installation token is minted in-seam.
//  3. Postgres   — neither git profile applies: the legacy head-state store, so a
//     credential-less dev server still boots and serves.
func buildDesignProjectState(cfg config, pgStore *projectstate.Store, sc *sourcecontrol.Access, logger *slog.Logger) (projectstate.ProjectStateAccess, error) {
	switch {
	case cfg.ProjectStateGitLocal:
		if cfg.ProjectStateGitRepoURL == "" {
			return nil, fmt.Errorf("ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL is required when ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true")
		}
		locator := gitRepoLocator{
			branch:            "main",
			perProjectRepoURL: func(projectstate.ProjectID) string { return cfg.ProjectStateGitRepoURL },
		}
		store, err := projectstate.NewGitStore(locator, true /* local */)
		if err != nil {
			return nil, err
		}
		// Discover-by-enumeration over the single on-disk project repo (no GitHub
		// installation API in local mode — founder ruling 2026-06-14).
		store = store.WithCatalog(localProjectCatalog{repoURL: cfg.ProjectStateGitRepoURL, branch: "main"})
		logger.Info("projectStateAccess (local git) ready", "repoURL", cfg.ProjectStateGitRepoURL)
		return &projectStateGitAdapter{store: store, minter: localCredentialMinter{}}, nil

	case sc != nil:
		webHost := gitWebHost(cfg.GitHubAPIBaseURL)
		account := cfg.GitHubAccount
		locator := gitRepoLocator{
			branch: "main",
			perProjectRepoURL: func(projectID projectstate.ProjectID) string {
				return cloudPerProjectRepoURL(webHost, account, projectID.String())
			},
		}
		store, err := projectstate.NewGitStore(locator, false /* cloud */)
		if err != nil {
			return nil, err
		}
		// Discover-by-enumeration: list the GitHub App installation's aiarch-project
		// repos (founder ruling 2026-06-14 — the registry index repo is removed).
		store = store.WithCatalog(cloudProjectCatalog{sc: sc, account: sourcecontrol.AccountRef(account)})
		logger.Info("projectStateAccess (github) ready", "account", account, "webHost", webHost)
		return &projectStateGitAdapter{
			store:  store,
			minter: cloudCredentialMinter{sc: sc, account: sourcecontrol.AccountRef(account)},
		}, nil

	default:
		logger.Warn("projectStateAccess (postgres) — git substrate NOT configured; UC1/UC2 design artifacts persist to Postgres, NOT GitHub (set ARCHISTRATOR_GITHUB_APP_ID + ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM + ARCHISTRATOR_GITHUB_ACCOUNT for live GitHub artifact commits, or ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true for on-disk git)")
		return pgStore, nil
	}
}

// cloudPerProjectRepoURL composes the clone URL of a project's per-project repo in the
// CLOUD profile. Under NAME-AS-IDENTITY (C-PA-AD, 2026-06-15) the project identity IS
// the (user-supplied) repo name, so the URL is <webHost>/<account>/<name>.git — the
// old "aiarch-<id>" prefix is DROPPED. This MUST agree with the repo-name re-derivation
// the per-project credential is scoped to: sourceControlAccess.deterministicRepoName
// collapsed to the identity map in C-SC-AD/A1, so the credential minter scopes the
// installation token to <account>/<name>.git. The locator (this URL) and the credential
// scope therefore address the SAME adopted repo verbatim — no "aiarch-" disagreement.
func cloudPerProjectRepoURL(webHost, account, name string) string {
	return fmt.Sprintf("%s/%s/%s.git", webHost, account, name)
}

// gitWebHost derives the GitHub WEB host (https://github.com, or a GHES web host) the
// per-project repo clone URLs are composed from. Mirrors constructionRepoBase's host
// derivation: github.com by default; for GHES strip the /api/v3 REST suffix off the
// configured API base URL to recover the web host.
func gitWebHost(apiBaseURL string) string {
	host := "https://github.com"
	if base := strings.TrimRight(strings.TrimSpace(apiBaseURL), "/"); base != "" {
		host = strings.TrimSuffix(base, "/api/v3")
	}
	return host
}

func run(logger *slog.Logger) error { //nolint:gocognit,gocyclo,maintidx,nestif // server bootstrap wiring
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Dev.Enabled {
		logger.Warn("AUTH DEV MODE ENABLED — a dev principal is injected on every request and the access token is NOT validated. MUST be off in any IdP-fronted deployment.")
	}

	// Root context cancelled on the first SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- Telemetry (traces/metrics/logs → in-cluster collector) ----------------
	// Composition-root binding satellite→facade: the OTLP-backed Provider is built
	// by the framework-go-infrastructure-otel SATELLITE (it owns all the OTLP/gRPC/
	// collector coupling, reads the standard OTEL_* env, and returns a no-op Provider
	// when OTEL_EXPORTER_OTLP_ENDPOINT is unset — telemetry off for dev/systemtests);
	// the opaque utilities/telemetry FACADE then installs it as the OTel globals + a
	// W3C propagator and bridges slog. A backend swap touches only the satellite.
	// MUST run BEFORE the Temporal client dial and the HTTP handler wrap below so both
	// pick up the installed globals. service.name comes from OTEL_SERVICE_NAME (set in
	// the deployment); there is no server build-version constant today, so
	// ServiceVersion is left empty (not reported).
	telemetryProvider, err := otelinfra.NewProvider(ctx, otelinfra.Options{ServiceName: "archistrator-server"})
	if err != nil {
		return err
	}
	shutdownTelemetry := telemetry.Install(telemetryProvider, telemetry.Options{ServiceName: "archistrator-server", Logger: logger})
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			logger.Error("telemetry shutdown failed", "err", err)
		}
	}()

	// --- Resources / ResourceAccess against the in-cluster infra ---------------

	// Temporal control-plane client (the Manager owns it; the embedded Worker polls
	// the system-design task queue). Lazy dial so a transient Temporal blip at boot
	// does not crash the process before the Worker's own backoff kicks in.
	// The security-principal context propagator flows the validated principal from
	// the HTTP request, through workflows, into activities. Registered on the
	// client; the embedded Worker (built from this same client) inherits it.
	// OTel tracing interceptor + metrics handler bind the Temporal control plane to
	// the installed global TracerProvider/MeterProvider (both default to the OTel
	// globals telemetry.Setup installed; when telemetry is off the globals are the
	// SDK no-ops, so this is inert). The interceptor traces workflow/activity spans;
	// the metrics handler exports the SDK's worker/client metrics over OTLP.
	tracingInterceptor, err := temporalotel.NewTracingInterceptor(temporalotel.TracerOptions{})
	if err != nil {
		return err
	}
	tc, err := client.DialContext(ctx, client.Options{
		HostPort:           cfg.TemporalHostPort,
		Namespace:          cfg.TemporalNamespace,
		Logger:             newTemporalLogger(logger),
		ContextPropagators: []workflow.ContextPropagator{temporalprop.NewPrincipalPropagator()},
		Interceptors:       []interceptor.ClientInterceptor{tracingInterceptor},
		MetricsHandler:     temporalotel.NewMetricsHandler(temporalotel.MetricsHandlerOptions{}),
	})
	if err != nil {
		return err
	}
	defer tc.Close()
	logger.Info("temporal client dialed", "hostPort", cfg.TemporalHostPort, "namespace", cfg.TemporalNamespace)

	// Postgres-backed projectStateAccess (head-state aggregate).
	pool, err := postgresinfra.NewPool(ctx, cfg.PostgresURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	ps, err := projectstate.NewStore(ctx, pool)
	if err != nil {
		return err
	}
	logger.Info("projectStateAccess (postgres) ready")

	// Postgres-backed usageAccess (append-only usage_log ledger, C-UA).
	// Constructed at boot so its constructor-applied DDL reconciles the schema
	// on every deploy (R-PG-US convention); billingManager (UC5 period close)
	// is the reader-to-come — no Manager consumes the port yet.
	if _, err := usagelog.NewStore(ctx, pool); err != nil {
		return err
	}
	logger.Info("usageAccess (postgres) ready")

	// git-backed artifactAccess (content-addressable store for Phase-3 construction
	// outputs; C-AA-R rework — Gitea removed per the 2026-06-09 git-only pivot). The
	// backing store is the per-project construction git repo. Two profiles behind the
	// unchanged contract surface: LOCAL (file:// on-disk repo, no credential) and
	// CLOUD (the user's GitHub repo; the installation token is minted INTERNALLY from
	// the GitHubApp* identity — never threaded through the RA surface, no sibling-RA
	// call). Constructed only when a repo URL is configured; the constructionManager's
	// StoreConstructionOutput/RetrieveConstructionOutput route through it. Nil when
	// unconfigured (the construction slice then stages no outputs — acceptable for the
	// empty-session runtime state).
	var artifacts *artifact.Store
	if cfg.ArtifactRepoURL != "" { //nolint:nestif
		if cfg.ArtifactRepoLocal {
			artifacts, err = artifact.NewLocalStore(cfg.ArtifactRepoURL)
			if err != nil {
				return err
			}
			logger.Info("artifactAccess (local git) ready", "repoURL", cfg.ArtifactRepoURL)
		} else {
			artifacts, err = artifact.NewCloudStore(artifact.CloudConfig{
				RepoURL:        cfg.ArtifactRepoURL,
				Owner:          cfg.ArtifactRepoOwner,
				AppID:          cfg.GitHubAppID,
				PrivateKeyPEM:  cfg.GitHubAppPrivateKeyPEM,
				APIBaseURL:     cfg.GitHubAPIBaseURL,
				InstallationID: cfg.GitHubInstallationID,
			})
			if err != nil {
				return err
			}
			logger.Info("artifactAccess (github) ready", "repoURL", cfg.ArtifactRepoURL)
		}
	}

	// constructionPipelineAccess (UC3) — fronts the USER'S GitHub Actions (C-CP-R
	// rework; Argo removed per the 2026-06-09 pivot). It dispatches the aiarch
	// construction workflow in the user's repo via the GitHub App identity and
	// observes/cancels the runs. Constructed only when the construction repo is
	// configured; nil otherwise (the pump then never submits a pipeline — acceptable
	// empty-session state, and the pump is unwired anyway pending the schedulerClient).
	var pipeline *constructionpipeline.Access
	if cfg.ConstructionRepoOwner != "" && cfg.ConstructionRepoName != "" {
		actionsClient, acErr := constructionpipeline.NewActionsClient(constructionpipeline.ActionsConfig{
			AppID:          cfg.GitHubAppID,
			PrivateKeyPEM:  cfg.GitHubAppPrivateKeyPEM,
			APIBaseURL:     cfg.GitHubAPIBaseURL,
			InstallationID: cfg.GitHubInstallationID,
			Owner:          cfg.ConstructionRepoOwner,
			Repo:           cfg.ConstructionRepoName,
			WorkflowFile:   cfg.ConstructionWorkflowFile,
			Ref:            cfg.ConstructionRef,
		})
		if acErr != nil {
			return acErr
		}
		pipeline, err = constructionpipeline.New(actionsClient)
		if err != nil {
			return err
		}
		logger.Info("constructionPipelineAccess (github-actions) ready",
			"owner", cfg.ConstructionRepoOwner, "repo", cfg.ConstructionRepoName, "workflow", cfg.ConstructionWorkflowFile)
	}

	// sourceControlAccess (project-birth repo ADOPT + managed-scaffold seating + the PR-merge
	// rail; C-SC). It is the ONLY component permitted to perform GitHub-App-lifecycle
	// operations. The projectManager drives its adopt-then-seat surface synchronously at
	// project birth (C-PM-Δ): AdoptProjectRepo → CommitManagedFiles (the design workflow +
	// the go-test gate scaffold), BEFORE
	// projectStateAccess.CreateProject, so a project is never cataloged without an
	// adopted, scaffold-seated repo (founder acceptance #2). Name-as-identity: the user
	// supplies the repo name, which IS the project identity. (2026-06-15 correction: the
	// CLAUDE_CODE_OAUTH_TOKEN Actions secret is user-provisioned via the Claude Code
	// GitHub App — aiarch does no secret management.)
	//
	// Constructed only when the GitHub App identity (app id + private key) AND an
	// account (org login) are configured — the SAME App credentials constructionPipeline
	// / artifactAccess already use. Nil otherwise: a dev server with no GitHub creds runs
	// repo-less (the projectManager then skips adopt+seating and CreateProject still
	// works), exactly as the construction Worker stays dormant when its deps are absent —
	// we do NOT hard-crash a credential-free dev stack.
	var sourceControl project.SourceControlAccess
	var scConcrete *sourcecontrol.Access // retained for the projectStateAccess git cred minter (CLOUD profile)
	if cfg.GitHubAppID != "" && cfg.GitHubAppPrivateKeyPEM != "" && cfg.GitHubAccount != "" {
		ghClient, scErr := githubinfra.NewAppClient(cfg.GitHubAppID, cfg.GitHubAppPrivateKeyPEM, cfg.GitHubAPIBaseURL)
		if scErr != nil {
			return scErr
		}
		scAccess, scErr := sourcecontrol.New(ghClient, cfg.GitHubAccount, cfg.GitHubAppSlug, true /* repoPrivate */)
		if scErr != nil {
			return scErr
		}
		scConcrete = scAccess
		sourceControl = sourceControlAdapter{inner: scAccess}
		logger.Info("sourceControlAccess (github) ready", "account", cfg.GitHubAccount, "apiBaseURL", cfg.GitHubAPIBaseURL)
	} else {
		logger.Warn("sourceControlAccess NOT configured — projects are created repo-less (set ARCHISTRATOR_GITHUB_APP_ID + ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM + ARCHISTRATOR_GITHUB_ACCOUNT for live GitHub repo provisioning)")
	}

	// projectStateAccess SUBSTRATE SELECTION (I-GIT-DESIGN). The UC1/UC2 design
	// managers persist head-state to the per-project GIT repo when a git profile
	// applies, else fall back to the Postgres store (kept above as the credential-less
	// dev fallback). The selection presents the SAME no-cred projectstate.ProjectStateAccess:
	//   - LOCAL  (ProjectStateGitLocal=true): file:// on-disk repos, no credential.
	//   - CLOUD  (GitHub App + account configured): the user's GitHub repos, with the
	//            installation token minted in-seam by sourceControlAccess.
	//   - else   the Postgres store (a dev server with neither git profile still runs).
	// The construction/usage RAs keep the Postgres `ps` — only the design managers swap.
	designProjectState, err := buildDesignProjectState(cfg, ps, scConcrete, logger)
	if err != nil {
		return err
	}

	// --- Engines ---------------------------------------------------------------

	// Phase-3 (construction) Engines — pure, deterministic, Temporal-free. The
	// constructionManager calls them DIRECTLY in-workflow by value (handOffEngine /
	// interventionEngine / reviewEngine). reviewEngine is the hand-run seam given a
	// deterministic Go realisation (see internal/engine/review).
	handOffEngine := handoff.New()
	interventionEngine := intervention.New()
	reviewEngine := review.New()

	// Phase-2 estimate Engines — pure, deterministic, Temporal-free. The
	// projectDesignManager calls them by value in its SDP-assembly workflow
	// (projectDesignManager.md §6.3; estimationEngine / operationEstimationEngine /
	// settlementEngine contracts).
	estimator := estimation.New()
	operationEstimator := operationestimation.New()
	settlementEstimator := enginesettlement.New()

	// --- Utility ---------------------------------------------------------------

	// security Utility: authorization PDP + webhook verifier + service identity.
	// INTERIM policy: authentication (JWT validation in the auth middleware) is
	// the only gate — authenticatedOnlyPDP permits any authenticated principal.
	// This replaces the framework's deny-by-default role stub until the Cedar PDP
	// lands (see authz.go).
	sec := security.New(security.WithPolicyDecisionPoint(authenticatedOnlyPDP{}))

	// Access-token validator (authN). Constructed only when a JWKS URL is
	// configured; in dev mode or with no IdP (systemtests) it is nil and the auth
	// middleware injects a dev principal or denies, respectively.
	var tokenValidator security.Validator
	if !cfg.Dev.Enabled && cfg.KeycloakJWKSURL != "" {
		tokenValidator, err = keycloak.NewValidator(ctx, keycloak.Config{
			JWKSURL: cfg.KeycloakJWKSURL,
			Issuer:  cfg.KeycloakIssuer,
			Leeway:  3 * time.Second,
		})
		if err != nil {
			return err
		}
		logger.Info("keycloak access-token validator ready", "issuer", cfg.KeycloakIssuer, "jwksURL", cfg.KeycloakJWKSURL)
	}

	// --- Manager + embedded Temporal Worker ------------------------------------

	manager := systemdesign.NewManager(tc, designProjectState)
	projectDesignManager := projectdesign.NewManager(tc)
	// Thin projectManager (catalog + cross-phase typed read) over projectStateAccess,
	// plus the optional sourceControlAccess it drives at project birth to provision the
	// backing repo BEFORE the head-state row (architecture.dsl:581; nil ⇒ repo-less).
	// Wired into web.NewClient below as the catalog entry port. Uses the git substrate
	// (designProjectState) so CreateProject + the catalog read land in the per-project
	// git repos when configured (I-GIT-DESIGN). The estimator is the
	// constructionEstimationEngine the Manager calls at READ time to populate the
	// network computed block (compute-at-read CPM + criticality bands, founder gate
	// 2026-06-19).
	projectManager := project.NewManager(designProjectState, sourceControl, estimator)

	// PR-rail wiring for the design Managers (I-DESIGN-DISPATCH §2c). The SAME concrete
	// *sourcecontrol.Access that backs project birth + the construction rail backs the
	// design rail (it structurally satisfies each design Manager's SourceControlRail
	// consumer port). The Repo resolver maps a projectID → its deterministic per-project
	// RepoRef via RepoRefForProject (name-as-identity). When sourceControlAccess is NOT
	// configured (scConcrete == nil) BOTH are nil and the design rail is DORMANT — the
	// CoAuthor spine runs unchanged (read-back/stage on main, no branch/PR ops), exactly
	// as before this activity. The branch-aware read-back rides through the existing
	// projectStateGitAdapter / gitRepoLocator (which now implements ProjectRepoOnBranch),
	// so a non-empty session-branch override resolves a per-branch GitStore handle.
	var (
		designRailSD systemdesign.SourceControlRail
		designRepoSD func(systemdesign.ProjectID) (sourcecontrol.RepoRef, bool)
		designRailPD projectdesign.SourceControlRail
		designRepoPD func(projectdesign.ProjectID) (sourcecontrol.RepoRef, bool)
	)
	if scConcrete != nil {
		railAccount := sourcecontrol.AccountRef(cfg.GitHubAccount)
		// The concrete RA is now RA-context-based; the design Managers' SourceControlRail
		// mirrors are plain-ctx, so bridge via the composition-root railAdapter (which
		// builds fwra.Context at the boundary). One adapter satisfies both rails (identical
		// method sets).
		scRail := railAdapter{inner: scConcrete}
		designRailSD = scRail
		designRailPD = scRail
		repoFor := func(projectID projectstate.ProjectID) (sourcecontrol.RepoRef, bool) {
			ref, rerr := scConcrete.RepoRefForProject(railAccount, sourcecontrol.ProjectID(projectID.String()))
			if rerr != nil {
				logger.Warn("design PR rail: could not resolve RepoRef for project; rail dormant for this project", "projectID", projectID, "err", rerr)
				return sourcecontrol.RepoRef(""), false
			}
			return ref, true
		}
		designRepoSD = repoFor
		designRepoPD = repoFor
		logger.Info("design PR rail (github) ready — agentic design drafts use branch→PR→read-back→+1→merge", "account", cfg.GitHubAccount)
	} else {
		logger.Warn("design PR rail NOT configured — agentic design read-back/commit run on main with no PR (set the GitHub App config to enable the branch→PR→merge design model)")
	}

	// Phase-1 (system-design) Worker. The UC1 agentic pivot (D-MSD-Δ) dispatches the
	// draft/PM-critique as claude-code-action DESIGN jobs over the FROZEN
	// constructionPipelineAccess (the design Manager is a NEW caller of the same RA the
	// construction pump uses), so it takes the projectStateAccess (read-back + thin
	// writes) + the constructionPipelineAccess adapter. workerAccess +
	// artifactValidationEngine are no longer wired into this Manager. The OPTIONAL PR rail
	// (§2b) is threaded in (nil when sourceControlAccess is unconfigured).
	w := worker.New(tc, systemdesign.TaskQueue, worker.Options{})
	systemdesign.RegisterWorker(w, designProjectState, designPipelineAdapter{inner: pipeline}, designRailSD, designRepoSD)
	if err := w.Start(); err != nil {
		return err
	}
	defer w.Stop()
	logger.Info("embedded temporal worker started", "taskQueue", systemdesign.TaskQueue)

	// Phase-2 (project-design) Worker — one Worker per Manager task queue
	// (operational-concepts.md lines 311/324). It polls the project-design queue and
	// runs the projectDesignManager's three workflows + activities. The UC2 agentic
	// pivot (D-MPD-Δ) dispatches Phase-2 plan-DRAFTING as claude-code-action DESIGN
	// jobs over the FROZEN constructionPipelineAccess (the design Manager is a NEW
	// caller of the same RA the construction pump uses); the three estimate Engines
	// STAY server-side in-workflow. workerAccess + artifactValidationEngine are no
	// longer wired into this Manager.
	wpd := worker.New(tc, projectdesign.TaskQueue, worker.Options{})
	projectdesign.RegisterWorker(wpd, estimator, operationEstimator, settlementEstimator, designProjectState, designProjectDesignPipelineAdapter{inner: pipeline}, designRailPD, designRepoPD)
	if err := wpd.Start(); err != nil {
		return err
	}
	defer wpd.Stop()
	logger.Info("embedded temporal worker started", "taskQueue", projectdesign.TaskQueue)

	// Phase-3 (construction) Manager — the UC3 façade. It holds only the Temporal
	// client (it owns Temporal); the webClient relays superviseConstruction intents
	// (GetSessionState / PauseProject / OverrideActivity) to it. The embedded
	// construction Worker is registered ONLY when its real downstream deps are
	// configured (artifactAccess + constructionPipelineAccess present): without a
	// live Argo cluster the pump has nothing to drive, and the console still renders
	// with empty/quiet sessions.
	constructionManager := construction.NewManager(tc)

	// constructionManager projectState SUBSTRATE (the load-bearing fix). The per-activity
	// construction head-state (status + the transition records) lives in the SAME Project
	// aggregate as the design slots, so the construction Manager MUST share the substrate
	// the design Managers use — otherwise it cannot see a git-substrate project (the
	// dogfooded `archistrator` project lives in the git-local store, not Postgres, and the
	// pump was inert because this dep pointed at Postgres). When the git substrate is
	// active, point construction at the git store (sharing the design head-state store via
	// the same credentialMinter); else keep the Postgres `ps` (the legacy composition).
	// constructionGitStatus lights up the per-activity construction-status records
	// (RecordActivityStarted/Completed) that drive the pump's eligibility cascade.
	constructionPS := construction.ProjectStateAccess(ps)
	var constructionGitStatus construction.GitActivityStatusAccess
	if gitAdapter, ok := designProjectState.(*projectStateGitAdapter); ok {
		constructionPS = constructionProjectStateAdapter{store: gitAdapter.store, minter: gitAdapter.minter}
		constructionGitStatus = gitAdapter.store // the concrete *GitStore satisfies the git head-state seam
		logger.Info("constructionManager projectState → git substrate (shares the design head-state store; status cascade live)")
	}

	// The three EXTERNAL-effect construction deps. Real GitHub-Actions pipeline + artifact
	// store + LLM worker by default (registered only when configured); the dry-run profile
	// swaps in instant in-memory stubs so the pump cascades with no GitHub Actions / LLM
	// (config-gated demo/dogfood; never the prod default — construction_dryrun.go).
	registerConstruction := false
	var (
		constructionPipeline  construction.ConstructionPipelineAccess
		constructionArtifacts construction.ArtifactAccess
		constructionWorkers   workeraccess.WorkerAccess
	)
	switch {
	case cfg.ConstructionDryRun:
		constructionPipeline = dryRunPipeline{}
		constructionArtifacts = dryRunArtifacts{}
		constructionWorkers = dryRunWorker{}
		registerConstruction = true
		logger.Warn("construction Worker DRY-RUN mode — pipeline/artifact/worker effects are STUBBED (no GitHub Actions run, no LLM call); the real pump + per-activity lifecycle + head-state cascade run end-to-end")
	case pipeline != nil && artifacts != nil:
		constructionPipeline = pipelineAdapter{inner: pipeline}
		constructionArtifacts = artifactAdapter{inner: artifacts}
		// Construction does NOT use a server-side LLM. The real work (and review)
		// runs in GitHub Actions via claude-code-action on the user's token; the
		// server only dispatches + observes the pipeline. dryRunWorker is a no-LLM
		// stub that satisfies the legacy GenerateWork/review seam with a valid
		// ConstructionOutput so the workflow advances to the real GH-Actions dispatch.
		// (Removing the GenerateWork/review steps entirely is the Plan 3 follow-up.)
		constructionWorkers = dryRunWorker{}
		registerConstruction = true
	}

	if registerConstruction {
		// Intervention regime: Tiered{RetryBudget:2} by default (autonomous retry,
		// escalate after budget), or EscalateEverything for supervised mode. The engine
		// policy (fed to the Engine via the adapter) and the Manager mirror are derived
		// from the same config so they stay in lock-step.
		engPolicy, mgrPolicy := constructionInterventionPolicy(cfg.ConstructionInterventionMode)
		deps := construction.WireDeps(
			handoffAdapter{inner: handOffEngine},
			interventionAdapter{inner: interventionEngine, policy: engPolicy},
			reviewAdapter{inner: reviewEngine},
			constructionPS,
			constructionPipeline,
			constructionArtifacts,
			constructionWorkers, // the generic typed worker (adapted to the unexported seam in-package)
			nextEligibleActivity,
			construction.HandOffPolicy{},
			mgrPolicy,
		)
		// Bound the escalation wait so a cancelled/failed GH-Actions run or an
		// unanswered escalation FAILS the activity (head-state EscalationTimedOut)
		// instead of hanging forever. 0 == wait-forever (supervised mode).
		deps.EscalationWaitTimeout = cfg.ConstructionEscalationTimeout
		// Light up the construction-status head-state slice so the pump's eligibility
		// cascade advances (RecordActivityStarted/Completed). The PR rail (first arg) +
		// per-project repo resolver (third arg) stay nil — the status records are
		// INDEPENDENT of the branch→PR→merge lifecycle (gitforward.go startedCred), so the
		// local/dry-run profile cascades without any GitHub rail.
		if constructionGitStatus != nil {
			deps = deps.WithGitForward(nil, constructionGitStatus, nil)
		}
		wc := worker.New(tc, construction.TaskQueue, worker.Options{})
		construction.RegisterWorker(wc, deps)
		if err := wc.Start(); err != nil {
			return err
		}
		defer wc.Stop()
		logger.Info("embedded temporal worker started", "taskQueue", construction.TaskQueue, "dryRun", cfg.ConstructionDryRun)

		// TODO(schedulerClient): register the nextActivity (30s) + replanSweep (5m)
		// Temporal Schedules. construction.RegisterSchedules needs a
		// construction.DurableExecutionAccess, whose RegisterSchedule signature names
		// the UNEXPORTED construction.scheduleSpec — so no composition-root adapter can
		// satisfy it (frozen-deps.go seam gap; see log/C-MCN-reconcile.md). Schedule
		// registration belongs to the unbuilt schedulerClient. The console's manual
		// "Begin construction" (POST .../construction/begin → ExecuteNextActivity, which
		// self-cascades via ContinueAsNew) supersedes the schedule for the dry-run.
	} else {
		logger.Warn("construction Worker NOT registered — set ARCHISTRATOR_CONSTRUCTION_DRYRUN=true for the stubbed pump, or configure artifactAccess + constructionPipelineAccess for the real one; the UC3 pump is dormant (GetSessionState still answers with empty/quiet sessions)")
	}

	// --- Client + HTTP server --------------------------------------------------

	// UC4 (operations) Manager — the operateDeliveredSystem façade. Like the
	// construction Manager it holds only the Temporal client (it owns Temporal); the
	// webClient relays operate intents (Deploy / Scale / UpdateAutoscalerPolicy →
	// DeployAfterConstruction, Withdraw → WithdrawSystem, QueryCostProjection) to it.
	// The operatedStateReconcile Schedule + the operations Worker are scheduler/worker
	// concerns wired separately (the console's deploy/withdraw/cost ops are accepted as
	// durable workflow starts regardless).
	operationsManager := operations.NewManager(tc)

	// repoBase is the project-wide construction-repo WEB base the webClient's git-row
	// read projection composes each clickable prUrl from (<repoBase>/pull/<opaqueRef>;
	// D-PA-GIT-PRURL-ruling R1). It is computed ONCE here from the same construction-repo
	// config the constructionPipelineAccess already uses (cfg.ConstructionRepoOwner /
	// cfg.ConstructionRepoName + the GitHub WEB host). The host is github.com by default;
	// for GHES it is derived from cfg.GitHubAPIBaseURL (an API root like
	// https://ghe.host/api/v3) by stripping the /api/v3 REST suffix to recover the web
	// host. When the construction repo is unconfigured, repoBase == "" and the projection
	// simply omits prUrl (no fabricated host). This is a Client-held CONFIG value — NOT a
	// store read, NOT an aggregate field; the durable git head-state stays provider-opaque.
	repoBase := constructionRepoBase(cfg.GitHubAPIBaseURL, cfg.ConstructionRepoOwner, cfg.ConstructionRepoName)

	webClient := web.NewClient(manager, projectDesignManager, projectManager, constructionManager, operationsManager, sec, repoBase)
	// otelhttp wraps the whole route tree: it starts a server span per request
	// (extracting any inbound W3C trace context) and records http.server.* metrics
	// against the installed global providers. Inert when telemetry is off (the
	// globals are no-ops). The span name is the request method + matched route.
	handler := otelhttp.NewHandler(
		webClient.Routes(web.AuthMiddleware(cfg.Dev, tokenValidator)),
		"archistrator-server",
	)
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// --- Wait for shutdown signal or fatal server error ------------------------

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received; draining")
	case err := <-serverErr:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http graceful shutdown failed", "err", err)
		return err
	}
	logger.Info("http server stopped cleanly")
	return nil
}

// constructionRepoBase composes the project-wide construction-repo WEB base
// (<host>/<owner>/<repo>) the webClient's git-row read projection turns into each
// clickable prUrl (<repoBase>/pull/<opaqueRef>; D-PA-GIT-PRURL-ruling R1). It returns ""
// when the construction repo is unconfigured (owner or name empty) — the projection then
// omits prUrl rather than fabricating a host.
//
// apiBaseURL is the GitHub REST API root config (cfg.GitHubAPIBaseURL): empty for
// github.com, or a GHES API root like https://ghe.example/api/v3. The WEB host differs
// from the API host: for github.com it is https://github.com (NOT api.github.com), and
// for GHES it is the API root with the trailing /api/v3 REST suffix stripped. This is the
// single place that maps the API-host config to the web-host the PR URL needs.
func constructionRepoBase(apiBaseURL, owner, repo string) string {
	if owner == "" || repo == "" {
		return ""
	}
	host := "https://github.com"
	if base := strings.TrimRight(strings.TrimSpace(apiBaseURL), "/"); base != "" {
		// GHES: strip the REST suffix (/api/v3) to recover the web host.
		host = strings.TrimSuffix(base, "/api/v3")
	}
	return host + "/" + owner + "/" + repo
}
