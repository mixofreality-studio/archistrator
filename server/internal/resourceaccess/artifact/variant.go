package artifact

// variant.go holds the deployment-profile VARIANT CONSTRUCTORS for artifactAccess —
// the composition-root policy that used to live in cmd/server (buildArtifactAccess +
// artifact_auth.go) folded into the owning package. Each variant assembles the
// generated GitArtifactAccess (contract.gen.go) over the satellite *GitBlobStore plus
// the profile-specific auth resolver:
//
//	LOCAL  — the user's on-disk git repo over a file:// remote needs no HTTP
//	         credential (GitAuth{Local:true}).
//	CLOUD  — the user's GitHub repo; the short-lived INSTALLATION TOKEN is minted
//	         INTERNALLY (App-JWT -> MintInstallationToken) and cached to expiry. The
//	         credential is NEVER threaded through the RA's contract surface and the RA
//	         NEVER calls a sibling RA to obtain it (NoSideways) — the discipline the
//	         artifactAccess contract §6 / Non-goal #11 prescribes.
//	DRY-RUN — an in-memory stub for the local dogfood/demo profile
//	         (ARCHISTRATOR_CONSTRUCTION_DRYRUN=true); nothing is committed.
//
// These live in the RA package (not internal/, but the RA that owns the contract):
// the git satellite is sanctioned infrastructure plumbing, not a sibling RA, so
// importing it here keeps the no-sideways discipline intact.

import (
	"context"
	"strings"
	"sync"
	"time"

	githubinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// NewLocalGitArtifactAccess builds the LOCAL-profile artifactAccess: a satellite
// *GitBlobStore over the on-disk construction repo plus the no-credential file://
// auth resolver. No network IO at construction.
func NewLocalGitArtifactAccess(repoURL string) (ArtifactAccess, error) {
	blob, err := githubinfra.NewGitBlobStore(repoURL)
	if err != nil {
		return nil, err
	}
	return NewGitArtifactAccess(blob, localGitAuth()), nil
}

// NewGitHubArtifactAccess builds the CLOUD-profile artifactAccess: a satellite
// *GitBlobStore over the user's GitHub construction repo plus the internal
// token-minting auth resolver (App-JWT -> MintInstallationToken, cached to expiry).
// Config is validated eagerly (a missing field / bad key surfaces as
// fwra.ContractMisuse) but no network IO happens; the installation token is minted
// lazily on first use. installationID 0 ⇒ discovered on first call.
func NewGitHubArtifactAccess(repoURL, owner, appID, privateKeyPEM, apiBaseURL string, installationID int64) (ArtifactAccess, error) {
	blob, authResolver, err := newCloudArtifactStore(repoURL, owner, appID, privateKeyPEM, apiBaseURL, installationID)
	if err != nil {
		return nil, err
	}
	return NewGitArtifactAccess(blob, authResolver), nil
}

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

// newCloudArtifactStore builds the cloud-profile satellite *GitBlobStore over the
// project's construction repo plus the internal token-minting auth resolver. It
// validates config eagerly (a missing field / bad key surfaces as
// fwra.ContractMisuse) but performs no network IO; the installation token is minted
// lazily on first use. installationID 0 ⇒ discovered on first call.
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

// ---------------------------------------------------------------------------
// DRY-RUN — artifact.ArtifactAccess stub. Store returns a deterministic fake content
// address; Retrieve returns a minimal valid output. Nothing is committed. Backs the
// UC3 construction Worker when ARCHISTRATOR_CONSTRUCTION_DRYRUN=true (local dogfood /
// demo profile only). Construction dispatches real work via the GH-Actions pipeline
// (agentic-everywhere); there is no server-side LLM worker seam.
// ---------------------------------------------------------------------------

// NewDryRunArtifactAccess returns the in-memory dry-run artifactAccess stub.
func NewDryRunArtifactAccess() ArtifactAccess {
	return dryRunArtifacts{}
}

type dryRunArtifacts struct{}

var _ ArtifactAccess = dryRunArtifacts{}

func (dryRunArtifacts) StoreConstructionOutput(rc fwra.Context, _ ConstructionOutput) (string, error) {
	return "dryrun-addr:" + string(rc.IdempotencyKey), nil
}

func (dryRunArtifacts) RetrieveConstructionOutput(_ fwra.Context, _ string) (ConstructionOutput, error) {
	return ConstructionOutput{Bytes: []byte("dry-run construction output"), MIMEType: "text/plain"}, nil
}

func (dryRunArtifacts) RetrieveOutputTree(_ fwra.Context, contentAddress string) (OutputTree, error) {
	return OutputTree{Root: contentAddress, Entries: map[string]string{}}, nil
}
