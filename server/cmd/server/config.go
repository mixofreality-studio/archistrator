package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/davidmarne/archistrator/server/internal/client/web"
	"github.com/davidmarne/archistrator-platform/framework-go/utilities/security"
)

// config is the composition root's env-loaded configuration: the infra endpoints
// (Temporal, Postgres, Gitea) plus the workerAccess provider (Anthropic in
// production, Ollama for systemtests), the HTTP listen port, and the auth dev flag.
// Loaded in main; never read from anywhere else (no ambient env reads deeper in
// the tree). See .env.example for the documented variables.
type config struct {
	// HTTP
	ListenAddr      string
	ShutdownTimeout time.Duration

	// Temporal (in-cluster frontend; the Temporal cloud profile is wired to the
	// in-cluster Temporal — operational-concepts.md §18).
	TemporalHostPort  string
	TemporalNamespace string

	// Keycloak access-token validation (the server validates the bearer token
	// itself; Envoy forwards the Authorization header unchanged — GTD parity).
	// JWKSURL is the realm's certs endpoint (typically the INTERNAL cluster URL);
	// Issuer is the EXTERNAL issuer string the token's `iss` must match. When
	// JWKSURL is empty the validator is NOT constructed (local dev / systemtests):
	// dev mode injects a principal, otherwise the auth surface denies every
	// request.
	KeycloakJWKSURL string
	KeycloakIssuer  string

	// Postgres (projectStateAccess head-state — the LEGACY substrate; superseded by
	// the git path below when configured, retained as the credential-less fallback).
	PostgresURL string

	// projectStateAccess GIT substrate (I-GIT-DESIGN; the 2026-06-09 git-only pivot).
	// The UC1/UC2 design artifacts persist as JSON in the per-project Git repo under
	// .aiarch/state/ (gitstore.go), NOT Postgres. Two profiles behind the unchanged
	// no-cred ProjectStateAccess the Managers consume (the credential is bound in a
	// composition-root adapter, projectstate_git_adapter.go):
	//   - CLOUD: per-project repo at <WebHost>/<GitHubAccount>/<projectName>.git, where
	//     <projectName> IS the project identity (name-as-identity, C-PA-AD 2026-06-15 — the
	//     "aiarch-<id>" prefix is dropped); auth is the GitHub App installation token minted
	//     in-seam by sourceControlAccess.
	//     The project CATALOG is discovered by enumerating the App installation's
	//     aiarch-project repos (founder ruling 2026-06-14 — no registry index repo).
	//     Selected when the GitHub App identity + account are configured (same gate as
	//     sourceControlAccess) and ProjectStateGitLocal is false.
	//   - LOCAL: ProjectStateGitLocal=true selects a file:// on-disk repo (no credential),
	//     with ProjectStateGitRepoURL the per-project repo file:// URL. The catalog is
	//     discovered by scanning that on-disk repo. Used by the embedded profile and the
	//     I-GIT-DESIGN local-git proof.
	// When NEITHER git profile applies (no GitHub creds AND not local) the Managers
	// fall back to the Postgres store, so a credential-less dev server still runs.
	ProjectStateGitLocal   bool
	ProjectStateGitRepoURL string

	// artifactAccess construction-output store (C-AA-R rework; Gitea removed per the
	// 2026-06-09 git-only pivot). The backing store is the per-project construction
	// git repo — the SAME repo projectStateAccess fronts. Two profiles behind the
	// unchanged contract surface:
	//   - CLOUD: ArtifactRepoURL is the git-HTTP clone URL of the user's GitHub
	//     construction repo; auth is the GitHub App installation token minted
	//     INTERNALLY (the GitHubApp* identity below, shared with constructionPipeline).
	//     ArtifactRepoOwner is the repo owner (installation discovery).
	//   - LOCAL/embedded: ArtifactRepoLocal=true selects a file:// on-disk repo
	//     (ArtifactRepoURL is then the file:// URL) with no credential.
	// Constructed only when ArtifactRepoURL is set; nil otherwise (the construction
	// slice then stages no outputs — acceptable for the empty-session runtime state).
	ArtifactRepoURL   string
	ArtifactRepoOwner string
	ArtifactRepoLocal bool

	// Construction (UC3) — constructionPipelineAccess fronts the USER'S GitHub
	// Actions (the 2026-06-09 pivot; C-CP-R rework, Argo removed). The RA dispatches
	// the aiarch construction workflow file in the user's repo via the GitHub App
	// identity and observes/cancels the resulting workflow runs. These are the App
	// identity (numeric App id + RSA private key PEM), the optional GitHub API base
	// URL (empty == github.com; a GHE host or a test fake overrides it), the optional
	// pre-resolved installation id (0 == discover by owner on first use), the target
	// repo (owner/name), the construction workflow file the RA dispatches, and the
	// git ref to dispatch against. ConstructionTaskQueue is the construction
	// Manager's one Temporal task queue (unchanged).
	GitHubAppID              string
	GitHubAppPrivateKeyPEM   string
	GitHubAPIBaseURL         string
	GitHubInstallationID     int64
	ConstructionRepoOwner    string
	ConstructionRepoName     string
	ConstructionWorkflowFile string
	ConstructionRef          string
	ConstructionTaskQueue    string

	// ConstructionDryRun (ARCHISTRATOR_CONSTRUCTION_DRYRUN) registers the UC3
	// construction Worker with IN-MEMORY stubs for its external effects
	// (constructionPipelineAccess / artifactAccess / workerAccess) instead of the real
	// GitHub-Actions + LLM deps. The real ConstructActivityWorkflow + self-cascading
	// pump run end-to-end against instant-success stubs, so "Begin construction" drains
	// the committed network and the tracker animates eligible→running→done WITHOUT
	// firing any GitHub Actions run or LLM call. Default false (the real deps gate the
	// Worker as before). NEVER the production default — a config-gated demo/dogfood mode.
	ConstructionDryRun bool

	// sourceControlAccess (project-birth repo provisioning + the PR-merge rail).
	// GitHubAccount is the org login under which per-project repos are adopted
	// (name-as-identity: the repo name IS the project identity, no "aiarch-" prefix —
	// C-PA-AD 2026-06-15) and installations discovered; it defaults to
	// ConstructionRepoOwner (the user's account, shared with constructionPipeline /
	// artifactAccess) so the GitHub App identity is configured ONCE. GitHubAppSlug is
	// the App's slug, used only as the merge-restriction/bypass actor in the PR rail's
	// branch protection (off the provision path). sourceControlAccess is constructed
	// only when the GitHub App identity (app id + private key) AND an account are
	// configured; nil otherwise (a dev server with no GitHub creds provisions no repo).
	GitHubAccount string
	GitHubAppSlug string

	// WorkerAccess provider selection: "anthropic" (production default) or "ollama"
	// (systemtests only — testcontainers / docker-compose). The Anthropic→Ollama
	// swap is the encapsulated Worker volatility; both build the same WorkerAccess
	// port (worker.AnthropicWorker / worker.OllamaWorker).
	WorkerProvider string

	// Anthropic (production workerAccess generic typed worker). APIKey is required
	// when WorkerProvider == "anthropic"; BaseURL is optional (empty = SDK default).
	// The per-WorkerClass models map the logical drafting/critique roles to concrete
	// Claude models inside the seam.
	AnthropicAPIKey         string
	AnthropicBaseURL        string
	AnthropicModel          string // default / fallback model
	AnthropicArchitectModel string // DraftWorkerClass ("architect")
	AnthropicCritiqueModel  string // CritiqueWorkerClass ("productManager")

	// Ollama (test-only workerAccess provider). Used when WorkerProvider == "ollama".
	OllamaBaseURL string
	OllamaModel   string

	// Replay (test-only workerAccess decorator). Used when WorkerProvider ==
	// "replay". ReplayDir holds the on-disk cassettes; ReplayMode is "strict"
	// (miss = loud error, offline CI default) or "record_on_miss" (miss → generate
	// via the delegate + write). ReplayDelegate ("ollama"|"anthropic") selects the
	// real provider that serves misses in record_on_miss mode.
	ReplayDir      string
	ReplayMode     string
	ReplayDelegate string

	// Auth dev mode (clearly gated; MUST be off behind Envoy).
	Dev web.DevConfig
}

func loadConfig() (config, error) {
	cfg := config{
		ListenAddr:        env("ARCHISTRATOR_LISTEN_ADDR", ":8080"),
		ShutdownTimeout:   envDuration("ARCHISTRATOR_SHUTDOWN_TIMEOUT", 20*time.Second),
		TemporalHostPort:  env("ARCHISTRATOR_TEMPORAL_HOSTPORT", "temporal-frontend.temporal.svc:7233"),
		TemporalNamespace: env("ARCHISTRATOR_TEMPORAL_NAMESPACE", "default"),
		KeycloakJWKSURL:   env("ARCHISTRATOR_KEYCLOAK_JWKS_URL", ""),
		KeycloakIssuer:    env("ARCHISTRATOR_KEYCLOAK_ISSUER", ""),
		PostgresURL:       env("ARCHISTRATOR_POSTGRES_URL", ""),
		ArtifactRepoURL:   env("ARCHISTRATOR_ARTIFACT_REPO_URL", ""),
		ArtifactRepoOwner: env("ARCHISTRATOR_ARTIFACT_REPO_OWNER", ""),
		ArtifactRepoLocal: envBool("ARCHISTRATOR_ARTIFACT_REPO_LOCAL", false),

		ProjectStateGitLocal:   envBool("ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL", false),
		ProjectStateGitRepoURL: env("ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL", ""),

		GitHubAppID:              env("ARCHISTRATOR_GITHUB_APP_ID", ""),
		GitHubAppPrivateKeyPEM:   env("ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM", ""),
		GitHubAPIBaseURL:         env("ARCHISTRATOR_GITHUB_API_BASE_URL", ""),
		GitHubInstallationID:     envInt64("ARCHISTRATOR_GITHUB_INSTALLATION_ID", 0),
		ConstructionRepoOwner:    env("ARCHISTRATOR_CONSTRUCTION_REPO_OWNER", ""),
		ConstructionRepoName:     env("ARCHISTRATOR_CONSTRUCTION_REPO_NAME", ""),
		ConstructionWorkflowFile: env("ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE", "aiarch-construct.yml"),
		ConstructionRef:          env("ARCHISTRATOR_CONSTRUCTION_REF", "main"),
		ConstructionTaskQueue:    env("ARCHISTRATOR_CONSTRUCTION_TASK_QUEUE", "construction"),
		ConstructionDryRun:       envBool("ARCHISTRATOR_CONSTRUCTION_DRYRUN", false),

		// sourceControlAccess: account defaults to the construction-repo owner so the
		// GitHub App identity is configured once; the App slug has no universal default.
		GitHubAccount: env("ARCHISTRATOR_GITHUB_ACCOUNT", env("ARCHISTRATOR_CONSTRUCTION_REPO_OWNER", "")),
		GitHubAppSlug: env("ARCHISTRATOR_GITHUB_APP_SLUG", ""),

		WorkerProvider: env("ARCHISTRATOR_WORKER_PROVIDER", "anthropic"),

		AnthropicAPIKey:         env("ARCHISTRATOR_ANTHROPIC_API_KEY", ""),
		AnthropicBaseURL:        env("ARCHISTRATOR_ANTHROPIC_BASEURL", ""),
		AnthropicModel:          env("ARCHISTRATOR_ANTHROPIC_MODEL", "claude-opus-4-8"),
		AnthropicArchitectModel: env("ARCHISTRATOR_ANTHROPIC_ARCHITECT_MODEL", "claude-opus-4-8"),
		AnthropicCritiqueModel:  env("ARCHISTRATOR_ANTHROPIC_CRITIQUE_MODEL", "claude-sonnet-4-6"),

		OllamaBaseURL: env("ARCHISTRATOR_OLLAMA_BASEURL", ""),
		OllamaModel:   env("ARCHISTRATOR_OLLAMA_MODEL", "qwen2.5:3b"),

		ReplayDir:      env("ARCHISTRATOR_WORKER_REPLAY_DIR", ""),
		ReplayMode:     env("ARCHISTRATOR_WORKER_REPLAY_MODE", "strict"),
		ReplayDelegate: env("ARCHISTRATOR_WORKER_REPLAY_DELEGATE", "ollama"),
	}

	devEnabled := envBool("ARCHISTRATOR_AUTH_DEV_MODE", false)
	cfg.Dev = web.DevConfig{
		Enabled:   devEnabled,
		Principal: devPrincipal(),
	}

	if cfg.PostgresURL == "" {
		return config{}, fmt.Errorf("ARCHISTRATOR_POSTGRES_URL is required")
	}
	switch cfg.WorkerProvider {
	case "anthropic":
		if cfg.AnthropicAPIKey == "" {
			return config{}, fmt.Errorf("ARCHISTRATOR_ANTHROPIC_API_KEY is required when ARCHISTRATOR_WORKER_PROVIDER=anthropic")
		}
	case "ollama":
		if cfg.OllamaBaseURL == "" {
			return config{}, fmt.Errorf("ARCHISTRATOR_OLLAMA_BASEURL is required when ARCHISTRATOR_WORKER_PROVIDER=ollama")
		}
	case "replay":
		if cfg.ReplayDir == "" {
			return config{}, fmt.Errorf("ARCHISTRATOR_WORKER_REPLAY_DIR is required when ARCHISTRATOR_WORKER_PROVIDER=replay")
		}
		switch cfg.ReplayMode {
		case "strict":
		case "record_on_miss":
			switch cfg.ReplayDelegate {
			case "ollama":
				if cfg.OllamaBaseURL == "" {
					return config{}, fmt.Errorf("ARCHISTRATOR_OLLAMA_BASEURL is required for replay record_on_miss with the ollama delegate")
				}
			case "anthropic":
				if cfg.AnthropicAPIKey == "" {
					return config{}, fmt.Errorf("ARCHISTRATOR_ANTHROPIC_API_KEY is required for replay record_on_miss with the anthropic delegate")
				}
			default:
				return config{}, fmt.Errorf("ARCHISTRATOR_WORKER_REPLAY_DELEGATE must be \"ollama\" or \"anthropic\", got %q", cfg.ReplayDelegate)
			}
		default:
			return config{}, fmt.Errorf("ARCHISTRATOR_WORKER_REPLAY_MODE must be \"strict\" or \"record_on_miss\", got %q", cfg.ReplayMode)
		}
	default:
		return config{}, fmt.Errorf("ARCHISTRATOR_WORKER_PROVIDER must be \"anthropic\", \"ollama\", or \"replay\", got %q", cfg.WorkerProvider)
	}
	return cfg, nil
}

// devPrincipal builds the dev principal injected in dev mode. Roles are no
// longer load-bearing for authorization (the interim authenticatedOnlyPDP
// permits any authenticated principal — see authz.go); the default values are
// kept only so the injected identity is well-formed and remain overridable via
// env for when the Cedar PDP starts consuming roles/claims.
func devPrincipal() security.SecurityPrincipal {
	subject := env("ARCHISTRATOR_DEV_SUBJECT", "dev-architect")
	return security.SecurityPrincipal{
		Kind:     security.PrincipalUser,
		Subject:  subject,
		Username: subject,
		Roles:    strings.Fields(env("ARCHISTRATOR_DEV_ROLES", "drive-phase approve-artifact")),
	}
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt64(key string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
