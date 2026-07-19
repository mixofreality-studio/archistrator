package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// config_adapter.go is the composition root's config-resolution seam. The raw env
// READS + defaults come from the GENERATED loader (config.gen.go, emitted by
// framework-go-app-generator/configgen from project.json's deployment model);
// this file layers back the behaviors configgen deliberately leaves to the
// composition root and runs the required-var validations, IN PLACE on the
// generated *Config that RunGenerated + the Hooks (hooks.go) consume.
//
// Step-8 A2: the former hand `config` adapter struct + `loadConfig()` are gone —
// the generated composition root threads the generated *Config directly, so there
// is no second config shape to keep in sync. The transforms configgen does not
// model live here as in-place mutations of *Config; the int64 installation-id
// coercion moved to the variant-args hooks (parseInt64), which is where the typed
// value is actually consumed.

// loadResolvedConfig loads the generated *Config and applies the composition-root
// transforms configgen leaves to the root, plus the required-var validations:
//
//  1. GithubAppPrivateKeyPEM: `_FILE` resolution + inline-vs-path detection (envSecret).
//  2. GithubAppAccount: chained default env(ACCOUNT) → env(CONSTRUCTION_REPO_OWNER).
//  3. PostgresURL is required on every profile EXCEPT local (configgen scopes the
//     generated requiredEnvByProfile table to cloud-only, but the composition root
//     resolves the active profile from other config fields, which configgen cannot
//     see — so this profile-conditional check stays hand). The local profile has
//     zero Postgres-backed BINDINGS: project state is git, usageAccess's local arm
//     is the permanent no-op (usage.NewNoOpUsageAccess — Task 2, local-first-init-
//     funnel), and operatedSystemStateAccess's local arm is likewise the permanent
//     no-op (operatedsystemstate.NewNoOpOperatedSystemStateAccess — Task 2b:
//     "Postgres exists only for usage metering + operate/deploy head-state, neither
//     of which local mode does"). This validation is about the REQUIRED-env
//     contract only — it does NOT mean local never attempts a Postgres network
//     dial; see hooks.go's residual #4 for the known generator-level gap that still
//     makes it try.
//  4. ProjectStateGitRepoURL is required when ProjectStateGitLocal=true (the boot-time
//     git-local guard — reproduces the pre-appgen hand buildDesignProjectState check).
//     4b. LOCAL profile: ArtifactRepoURL defaults to ProjectStateGitRepoURL when unset
//     (local-first-init-funnel Task 6) — local mode's "zero external deps beyond
//     git+claude" thesis would otherwise be broken by requiring a SEPARATE
//     artifact repo just to register the construction Worker; the SAME on-disk
//     repo project state already lives in is a reasonable, zero-config default.
//  5. Construction creds are required only when !ConstructionDryRun, and (Task 6)
//     only the GitHub-Actions-specific ones on a NON-local profile — the local
//     construction executor needs no GitHub App / construction-repo config at all.
//  6. GithubAppAppSlug is required on a CLOUD-profile server whose GitHub App rail is
//     configured (the allowed_bots guard — see validateGithubAppSlug).
//
// GithubAppInstallationID stays the raw string on *Config; the two GitHub
// variant-args hooks coerce it to int64 via parseInt64 at the point of use.
func loadResolvedConfig() (*Config, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	// (1) PEM: re-resolve via envSecret (the generated string field is the raw,
	// unresolved value; envSecret adds _FILE + inline-vs-path handling).
	cfg.GithubAppPrivateKeyPEM = envSecret("ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM", "")
	// (2) account chained default: fall back to the construction-repo owner so the
	// GitHub App identity is configured once.
	cfg.GithubAppAccount = firstNonEmpty(cfg.GithubAppAccount, cfg.ConstructionRepoOwner)

	// (3) Postgres is a hard dependency everywhere except the local profile — the
	// local profile has zero Postgres-backed bindings (see the doc comment above).
	if resolveProfile(cfg) != "local" && cfg.PostgresURL == "" {
		return nil, fmt.Errorf("ARCHISTRATOR_POSTGRES_URL is required")
	}

	// (4) LOCAL git profile requires its repo URL — configgen's binding-setting
	// threading (projectStateAccess→projectStateGitRepoURL) has no arm-presence
	// check of its own, so an empty URL under PROJECT_STATE_GIT_LOCAL=true would
	// otherwise surface as an opaque on-disk-repo error deep in
	// projectstate.NewGitLocalProjectStateAccess. Fail fast here instead, at the
	// same boot-time-required-var checkpoint as the Postgres guard above (old
	// error text — this reproduces the pre-appgen hand run()'s
	// buildDesignProjectState guard verbatim).
	if cfg.ProjectStateGitLocal && cfg.ProjectStateGitRepoURL == "" {
		return nil, fmt.Errorf("ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL is required when ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true")
	}

	// (4b) LOCAL profile: default the artifact store to the SAME on-disk repo as
	// project state (see the doc comment above).
	if cfg.ProjectStateGitLocal && cfg.ArtifactRepoURL == "" {
		cfg.ArtifactRepoURL = cfg.ProjectStateGitRepoURL
	}

	// (5) DRYRUN=false: require all construction creds so the server fails fast at
	// startup rather than silently dispatching to nowhere.
	if !cfg.ConstructionDryRun {
		if err := validateConstructionCreds(cfg); err != nil {
			return nil, err
		}
	}

	// (6) CLOUD profile + configured App rail: the App slug is load-bearing, not
	// cosmetic — fail fast rather than seat broken design workflows.
	if err := validateGithubAppSlug(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validateGithubAppSlug fails the boot when a CLOUD-profile server
// (ProjectStateGitLocal=false) has its GitHub App rail configured (App id + PEM +
// account — the exact newAppHooks condition that activates the seated design rail)
// but NO ARCHISTRATOR_GITHUB_APP_SLUG. The slug renders the seated design workflow's
// `allowed_bots:` line (sourcecontrol.DesignWorkflowFile via methodassets); an empty
// slug OMITS the line, and then EVERY claude-code-action draft run the server
// dispatches fails with "Workflow initiated by non-human actor" (observed in
// production QA 2026-07-10). On a git-local dev server the omission stays valid (the
// rail is exercised by humans, if at all), so only the cloud profile fails fast.
func validateGithubAppSlug(c *Config) error {
	appConfigured := c.GithubAppAppID != "" && c.GithubAppPrivateKeyPEM != "" && c.GithubAppAccount != ""
	if !c.ProjectStateGitLocal && appConfigured && c.GithubAppAppSlug == "" {
		return fmt.Errorf(
			"ARCHISTRATOR_GITHUB_APP_SLUG is required on a cloud-profile server with GitHub App creds configured: " +
				"the seated design workflow renders its allowed_bots: line from the App slug, and with the slug empty " +
				"the line is omitted — every bot-dispatched claude-code-action design run then fails with " +
				`"Workflow initiated by non-human actor". Set it to the GitHub App's slug (the app's URL name, ` +
				"e.g. \"archistrator\" for github.com/apps/archistrator)")
	}
	return nil
}

// validateConstructionCreds returns an error naming every missing construction
// credential when ARCHISTRATOR_CONSTRUCTION_DRYRUN=false. The GitHub-Actions-
// specific fields (App id/key, construction repo, workflow file, ref) are only
// required on a NON-local profile (local-first-init-funnel Task 6: the local
// construction executor dispatches via headless claude directly against the
// on-disk repo — it has no GitHub Actions involvement at all, so requiring these
// unconditionally would make DRYRUN=false unreachable for local-without-creds).
// The workflow file + ref carry configgen defaults (aiarch-construct.yml / main),
// so they never appear in the missing set for a bare DRYRUN=false CLOUD server.
func validateConstructionCreds(c *Config) error {
	missing := []string{}
	if resolveProfile(c) != "local" {
		if c.GithubAppAppID == "" {
			missing = append(missing, "ARCHISTRATOR_GITHUB_APP_ID")
		}
		if c.GithubAppPrivateKeyPEM == "" {
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
	}
	// The real-path selection needs the artifact store too
	// (RegisterConstructionManagerWorker gates on it) on EVERY non-dry-run
	// profile, cloud AND local alike — ArtifactRepoURL is constructed only when
	// set (step 4b above defaults it for local, so this only fires for local when
	// ProjectStateGitRepoURL was itself somehow empty, already caught at step 4).
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
func devPrincipal() security.Principal {
	subject := getenvString("ARCHISTRATOR_DEV_SUBJECT", "dev-architect")
	return security.Principal{
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
