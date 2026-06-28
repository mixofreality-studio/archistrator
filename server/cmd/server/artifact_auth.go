package main

// artifact_auth.go is the COMPOSITION-ROOT auth wiring for the git-backed
// artifactAccess (the generated GitArtifactAccess / NewGitArtifactAccess in
// internal/resourceaccess/artifact). The generated constructor takes the satellite
// *GitBlobStore plus a per-call auth resolver `func(ctx) (GitAuth, error)`; the
// two deployment profiles' resolvers live HERE (outside internal/, free to import
// the framework github satellite), so the artifact RA's only public surface stays
// the generated contract (interface + models + struct + constructor).
//
// LOCAL profile  — the user's on-disk git repo over a file:// remote needs no HTTP
//                  credential (GitAuth{Local:true}).
// CLOUD profile  — the user's GitHub repo; the short-lived INSTALLATION TOKEN is
//                  minted INTERNALLY (App-JWT -> MintInstallationToken) and cached
//                  to expiry. The credential is NEVER threaded through the RA's
//                  contract surface and the RA NEVER calls a sibling RA to obtain
//                  it (NoSideways) — the discipline the artifactAccess contract
//                  §6 / Non-goal #11 prescribes, re-homed to the composition root
//                  alongside the generated DI constructor.

import (
	"context"
	"strings"
	"sync"
	"time"

	githubinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// localGitAuth is the LOCAL/embedded profile resolver: a file:// remote needs no
// HTTP credential.
func localGitAuth() func(ctx context.Context) (githubinfra.GitAuth, error) {
	return func(context.Context) (githubinfra.GitAuth, error) {
		return githubinfra.GitAuth{Local: true}, nil
	}
}

// tokenRefreshSkew re-mints the installation token a little before its hard expiry.
const tokenRefreshSkew = 60 * time.Second

// cloudGitAuth is the CLOUD profile resolver state. It mints/refreshes the
// installation token internally (App-JWT -> MintInstallationToken), caching it to
// expiry. Thread-safe.
type cloudGitAuth struct {
	app   *githubinfra.AppClient
	owner string

	mu             sync.Mutex
	installationID int64
	token          string
	tokenExpiry    time.Time
}

// resolve returns a non-local GitAuth carrying a valid installation token, minting
// or refreshing it internally. A cached token is reused until shortly before expiry.
func (c *cloudGitAuth) resolve(ctx context.Context) (githubinfra.GitAuth, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-tokenRefreshSkew)) {
		return githubinfra.GitAuth{Token: c.token}, nil
	}
	if c.installationID == 0 {
		id, err := c.app.FindInstallation(ctx, c.owner)
		if err != nil {
			return githubinfra.GitAuth{}, err
		}
		c.installationID = id
	}
	tok, exp, err := c.app.MintInstallationToken(ctx, c.installationID)
	if err != nil {
		return githubinfra.GitAuth{}, err
	}
	c.token = tok
	c.tokenExpiry = exp
	return githubinfra.GitAuth{Token: tok}, nil
}

// newCloudArtifactStore builds the cloud-profile GitArtifactAccess: a satellite
// *GitBlobStore over the project's construction repo plus the internal
// token-minting auth resolver. It validates config eagerly (a missing field / bad
// key surfaces as fwra.ContractMisuse) but performs no network IO; the installation
// token is minted lazily on first use. installationID 0 ⇒ discovered on first call.
func newCloudArtifactStore(repoURL, owner, appID, privateKeyPEM, apiBaseURL string, installationID int64) (*githubinfra.GitBlobStore, func(ctx context.Context) (githubinfra.GitAuth, error), error) {
	if strings.TrimSpace(repoURL) == "" {
		return nil, nil, fwra.New(fwra.ContractMisuse, "artifact cloud: empty RepoURL")
	}
	if strings.TrimSpace(owner) == "" {
		return nil, nil, fwra.New(fwra.ContractMisuse, "artifact cloud: empty Owner")
	}
	blob, err := githubinfra.NewGitBlobStore(repoURL)
	if err != nil {
		return nil, nil, err
	}
	app, err := githubinfra.NewAppClient(appID, privateKeyPEM, apiBaseURL)
	if err != nil {
		return nil, nil, err
	}
	ca := &cloudGitAuth{app: app, owner: owner, installationID: installationID}
	return blob, ca.resolve, nil
}
