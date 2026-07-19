package main

// projectstate_cloud.go holds the CLOUD-profile projectStateAccess glue that CANNOT fold
// into the projectstate package: it bridges projectStateAccess ↔ sourceControlAccess, and
// projectstate importing sourcecontrol would be a forbidden RA→RA sideways edge (arch_test
// NoSideways). So the sourcecontrol-backed CredentialMinter + ProjectCatalog live at the
// composition root and are passed into projectstate.NewGitHubProjectStateAccess as ports
// (the projectstate RA NEVER calls sourceControlAccess — the cred is a caller-supplied
// parameter, D-SC §1.1 returned-not-recorded). gitWebHost is host-derivation policy shared
// with constructionRepoBase and stays here too.
//
// cmd/server is OUTSIDE internal/, so this file may freely import both concrete RA packages
// and the github satellite; it imports no Temporal.

import (
	"context"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

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

// cloudCredentialMinter is the CLOUD profile projectstate.CredentialMinter: it mints a
// repo-scoped GitHub-App installation token via the concrete sourceControlAccess (which
// owns the deterministic repo-name encoding), then folds it into the provider-neutral
// projectstate.RepoCredential the GitStore consumes. The two RepoCredential value types are
// SHAPE-MATCHED (both {Bytes, ExpiresAt}); the fold is the one place the composition root
// bridges them.
type cloudCredentialMinter struct {
	sc      sourcecontrol.CatalogAccess
	account sourcecontrol.AccountRef
}

var _ projectstate.CredentialMinter = cloudCredentialMinter{}

func (m cloudCredentialMinter) CredentialFor(ctx context.Context, projectID projectstate.ProjectID) (projectstate.RepoCredential, error) {
	cred, err := m.sc.GetInstallationTokenForProject(ctx, m.account, sourcecontrol.ProjectID(projectID.String()))
	if err != nil {
		return projectstate.RepoCredential{}, err
	}
	return projectstate.RepoCredential{Bytes: cred.Bytes, ExpiresAt: cred.ExpiresAt}, nil
}

func (m cloudCredentialMinter) CatalogCredential(ctx context.Context) (projectstate.RepoCredential, error) {
	cred, err := m.sc.GetInstallationTokenForAccount(ctx, m.account)
	if err != nil {
		return projectstate.RepoCredential{}, err
	}
	return projectstate.RepoCredential{Bytes: cred.Bytes, ExpiresAt: cred.ExpiresAt}, nil
}

// cloudProjectCatalog is the CLOUD profile projectstate.ProjectCatalog: it enumerates the
// account's aiarch-project repos via the concrete sourceControlAccess (which owns the
// GitHub installation-repo listing + topic filter), then maps each ProjectRepoRef to the
// projectstate.ProjectCatalogRef the store consumes, carrying the repo NAME as the project
// identity (name-as-identity, C-PA-AD 2026-06-15 — r.ProjectID() returns the WHOLE
// user-supplied repo name) and the description as the display title. The store's
// no-sideways discipline is preserved: this is a composition-root value the store calls as
// a port, not the store reaching into a sibling RA.
type cloudProjectCatalog struct {
	sc      sourcecontrol.CatalogAccess
	account sourcecontrol.AccountRef
}

var _ projectstate.ProjectCatalog = cloudProjectCatalog{}

func (c cloudProjectCatalog) ListProjectRepos(ctx context.Context, _ projectstate.OwnerScope, _ projectstate.RepoCredential) ([]projectstate.ProjectCatalogRef, error) {
	repos, err := c.sc.ListProjectRepos(ctx, c.account)
	if err != nil {
		return nil, err
	}
	out := make([]projectstate.ProjectCatalogRef, 0, len(repos))
	for _, r := range repos {
		// r.ProjectID() is the WHOLE repo name (name-as-identity, A1) — carried verbatim as
		// the string ProjectID. A user-named repo is no longer skipped.
		out = append(out, projectstate.ProjectCatalogRef{
			ProjectID: projectstate.ProjectID(r.ProjectID()),
			Title:     r.Description,
		})
	}
	return out, nil
}
