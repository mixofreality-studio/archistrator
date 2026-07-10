package sourcecontrol

// variant.go holds the deployment VARIANT CONSTRUCTOR for sourceControlAccess —
// the composition-root policy that used to live in cmd/server (buildSourceControl)
// folded into the owning package. The shared *fwgithub.AppClient satellite stays
// OUTSIDE (built once at the composition root and shared with
// constructionPipeline / artifactAccess); the variant takes it in.
//
// NewGitHubSourceControl returns BOTH published surfaces the composition root wires:
//   - SourceControlCatalogAccess: the catalog/locator/token surface the projectStateAccess
//     git cred minter + catalog (CLOUD profile) consume;
//   - SourceControlAccess: the generated interface the design Managers' adapters + the
//     PR-rail consume.
//
// The unexported impl satisfies both, so the catalog surface is a type-assertion of the
// generated interface — folded here so the assertion is no longer a caller concern.

import (
	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
)

// NewGitHubSourceControl builds the GitHub-App-backed sourceControlAccess over the shared
// *fwgithub.AppClient and returns both published surfaces (catalog + generated interface).
func NewGitHubSourceControl(client *fwgithub.AppClient, account, appSlug string, repoPrivate bool) (SourceControlCatalogAccess, SourceControlAccess, error) {
	scAccess, err := NewGitHubSourceControlAccess(client, account, appSlug, repoPrivate)
	if err != nil {
		return nil, nil, err
	}
	scConcrete := scAccess.(SourceControlCatalogAccess)
	return scConcrete, scAccess, nil
}
