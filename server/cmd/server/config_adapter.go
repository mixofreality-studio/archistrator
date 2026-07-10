package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/mixofreality-studio/archistrator/server/internal/client/web"
)

// config is the composition root's env-loaded configuration: the infra endpoints
// (Temporal, Postgres, GitHub App, Keycloak) plus the HTTP listen port, the auth
// dev principal, and the profile-scalar knobs. Loaded once in main; never read
// from anywhere else (no ambient env reads deeper in the tree). See .env.example
// for the documented variables.
//
// The raw env READS + defaults now come from the GENERATED loader (config.gen.go,
// emitted by framework-go-app-generator/configgen from project.json's deployment
// model). This adapter reconstructs the historical `config` surface the builders
// consume unchanged, layering back the behaviors configgen deliberately leaves to
// the composition root (enumerated in loadConfig): PEM `_FILE` resolution, the
// installation-id int64 coercion, the GitHubAccount chained default, the dev
// principal, and the unconditional/conditional required-var checks.
type config struct {
	// HTTP
	ListenAddr      string
	ShutdownTimeout time.Duration

	// Temporal (in-cluster frontend).
	TemporalHostPort  string
	TemporalNamespace string

	// Keycloak access-token validation. Empty JWKSURL ⇒ validator NOT constructed
	// (local dev / systemtests) — dev mode injects a principal, else deny-all.
	KeycloakJWKSURL string
	KeycloakIssuer  string

	// Postgres (projectStateAccess head-state legacy substrate + the always-on
	// operatedSystemState/usage store). Required unconditionally (see loadConfig).
	PostgresURL string

	// projectStateAccess GIT substrate (LOCAL file:// vs CLOUD GitHub App).
	ProjectStateGitLocal   bool
	ProjectStateGitRepoURL string

	// artifactAccess construction-output store (per-project git repo; nil when
	// ArtifactRepoURL unset).
	ArtifactRepoURL   string
	ArtifactRepoOwner string
	ArtifactRepoLocal bool

	// Construction (UC3) — constructionPipelineAccess fronts the user's GitHub
	// Actions via the App identity.
	GitHubAppID              string
	GitHubAppPrivateKeyPEM   string
	GitHubAPIBaseURL         string
	GitHubInstallationID     int64
	ConstructionRepoOwner    string
	ConstructionRepoName     string
	ConstructionWorkflowFile string
	ConstructionRef          string
	ConstructionTaskQueue    string

	// ConstructionDryRun registers the UC3 Worker with in-memory stubs.
	ConstructionDryRun bool

	// OperationsDryRun selects the operatedRuntimeAccess LOCAL/dry-run profile.
	OperationsDryRun bool

	// OperatedRuntimeGitOpsRepoURL is the REAL operatedRuntime profile's GitOps target.
	OperatedRuntimeGitOpsRepoURL string

	// ConstructionEscalationTimeout bounds an escalated activity's wait. 0 == wait-forever.
	ConstructionEscalationTimeout time.Duration

	// ConstructionInterventionMode: "tiered" (default) or "escalate-everything".
	ConstructionInterventionMode string

	// sourceControlAccess (project-birth repo provisioning + PR-merge rail).
	// GitHubAccount defaults to ConstructionRepoOwner (chained default, below).
	GitHubAccount string
	GitHubAppSlug string

	// Auth dev mode (MUST be off behind Envoy).
	Dev web.DevConfig
}

// loadConfig reads the environment via the GENERATED loader and reconstructs the
// historical config surface. Every deviation from a straight field copy is a
// HAND-KEPT behavior configgen does not (yet) model:
//
//  1. GitHubAppPrivateKeyPEM: `_FILE` resolution + inline-vs-path detection (envSecret).
//  2. GitHubInstallationID: string→int64 coercion (envInt64 semantics).
//  3. GitHubAccount: chained default env(ACCOUNT, env(CONSTRUCTION_REPO_OWNER)).
//  4. Dev: DEV_SUBJECT/DEV_ROLES principal (not modeled) + the AuthDevMode toggle.
//  5. PostgresURL is required UNCONDITIONALLY (all profiles incl. test) — configgen
//     scopes it to cloud+local, so the check stays hand.
//  6. Construction creds are required only when !ConstructionDryRun.
func loadConfig() (config, error) {
	gen, err := LoadConfig()
	if err != nil {
		return config{}, err
	}

	cfg := config{
		ListenAddr:                    gen.ListenAddr,
		ShutdownTimeout:               gen.ShutdownTimeout,
		TemporalHostPort:              gen.TemporalHostPort,
		TemporalNamespace:             gen.TemporalNamespace,
		KeycloakJWKSURL:               gen.KeycloakJWKSURL,
		KeycloakIssuer:                gen.KeycloakIssuer,
		PostgresURL:                   gen.PostgresURL,
		ProjectStateGitLocal:          gen.ProjectStateGitLocal,
		ProjectStateGitRepoURL:        gen.ProjectStateGitRepoURL,
		ArtifactRepoURL:               gen.ArtifactRepoURL,
		ArtifactRepoOwner:             gen.ArtifactRepoOwner,
		ArtifactRepoLocal:             gen.ArtifactRepoLocal,
		GitHubAppID:                   gen.GithubAppAppID,
		GitHubAPIBaseURL:              gen.GithubAppAPIBaseURL,
		ConstructionRepoOwner:         gen.ConstructionRepoOwner,
		ConstructionRepoName:          gen.ConstructionRepoName,
		ConstructionWorkflowFile:      gen.ConstructionWorkflowFile,
		ConstructionRef:               gen.ConstructionRef,
		ConstructionTaskQueue:         gen.ConstructionTaskQueue,
		ConstructionDryRun:            gen.ConstructionDryRun,
		OperationsDryRun:              gen.OperationsDryRun,
		OperatedRuntimeGitOpsRepoURL:  gen.OperatedRuntimeGitOpsRepoURL,
		ConstructionEscalationTimeout: gen.ConstructionEscalationTimeout,
		ConstructionInterventionMode:  gen.ConstructionInterventionMode,
		GitHubAppSlug:                 gen.GithubAppAppSlug,

		// (1) PEM: re-resolve via envSecret (the generated string field is the raw,
		// unresolved value; envSecret adds _FILE + inline-vs-path handling).
		GitHubAppPrivateKeyPEM: envSecret("ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM", ""),
		// (2) installation id: string → int64.
		GitHubInstallationID: parseInt64(gen.GithubAppInstallationID),
		// (3) account chained default: fall back to the construction-repo owner so
		// the GitHub App identity is configured once.
		GitHubAccount: firstNonEmpty(gen.GithubAppAccount, gen.ConstructionRepoOwner),
	}

	// (4) dev principal.
	cfg.Dev = web.DevConfig{
		Enabled:   gen.AuthDevMode,
		Principal: devPrincipal(),
	}

	// (5) Postgres is a hard dependency in every profile.
	if cfg.PostgresURL == "" {
		return config{}, fmt.Errorf("ARCHISTRATOR_POSTGRES_URL is required")
	}

	// (6) DRYRUN=false: require all construction creds so the server fails fast at
	// startup rather than silently dispatching to nowhere.
	if !cfg.ConstructionDryRun {
		if err := cfg.validateConstructionCreds(); err != nil {
			return config{}, err
		}
	}

	return cfg, nil
}

// validateConstructionCreds returns an error naming every missing construction
// credential when ARCHISTRATOR_CONSTRUCTION_DRYRUN=false.
func (c config) validateConstructionCreds() error {
	missing := []string{}
	if c.GitHubAppID == "" {
		missing = append(missing, "ARCHISTRATOR_GITHUB_APP_ID")
	}
	if c.GitHubAppPrivateKeyPEM == "" {
		missing = append(missing, "ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM")
	}
	if c.ConstructionRepoOwner == "" {
		missing = append(missing, "ARCHISTRATOR_CONSTRUCTION_REPO_OWNER")
	}
	if c.ConstructionRepoName == "" {
		missing = append(missing, "ARCHISTRATOR_CONSTRUCTION_REPO_NAME")
	}
	if c.ConstructionWorkflowFile == "" {
		missing = append(missing, "ARCHISTRATOR_CONSTRUCTION_WORKFLOW_FILE")
	}
	if c.ConstructionRef == "" {
		missing = append(missing, "ARCHISTRATOR_CONSTRUCTION_REF")
	}
	// The real-path selection needs the git-forward artifact store too
	// (main.go: pipeline != nil && artifacts != nil). artifacts is constructed
	// only when ArtifactRepoURL is set, so it is required when not dry-run.
	if c.ArtifactRepoURL == "" {
		missing = append(missing, "ARCHISTRATOR_ARTIFACT_REPO_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"ARCHISTRATOR_CONSTRUCTION_DRYRUN=false requires construction creds; missing: %s",
			strings.Join(missing, ", "),
		)
	}
	return nil
}

// devPrincipal builds the dev principal injected in dev mode. Roles are no longer
// load-bearing for authorization; the defaults keep the identity well-formed and
// remain overridable via env for when the Cedar PDP starts consuming roles.
func devPrincipal() security.SecurityPrincipal {
	subject := getenvString("ARCHISTRATOR_DEV_SUBJECT", "dev-architect")
	return security.SecurityPrincipal{
		Kind:     security.PrincipalUser,
		Subject:  subject,
		Username: subject,
		Roles:    strings.Fields(getenvString("ARCHISTRATOR_DEV_ROLES", "drive-phase approve-artifact")),
	}
}

// envSecret reads a multiline secret (e.g. an RSA PEM) that does not survive a
// shell `source .env` as an inline value. Resolution order:
//  1. "<key>_FILE" — a single-line path that sources cleanly; read the file.
//  2. "<key>" — an inline PEM block is used verbatim; a value that instead names a
//     readable file path is read (the common path-in-content-var mistake).
//
// A read error falls through so the downstream fail-fast names the missing
// credential rather than panicking here.
func envSecret(key, def string) string {
	if path := strings.TrimSpace(os.Getenv(key + "_FILE")); path != "" {
		// #nosec G304 G703 -- operator-controlled secret-file path (Docker/K8s secrets convention), not untrusted input
		if b, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if strings.Contains(v, "-----BEGIN") {
			return v
		}
		// #nosec G304 G703 -- operator-controlled secret-file path (Docker/K8s secrets convention), not untrusted input
		if b, err := os.ReadFile(v); err == nil {
			return strings.TrimSpace(string(b))
		}
		return v
	}
	return def
}

// parseInt64 coerces a decimal string to int64 (envInt64 semantics: empty or
// unparseable ⇒ 0).
func parseInt64(v string) int64 {
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// firstNonEmpty returns the first non-empty argument (the chained-default helper).
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
