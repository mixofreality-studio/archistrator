package artifact

import (
	"context"
	"strings"
	"sync"
	"time"

	fwgithub "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// gitstore_client.go holds the two PROFILE constructors (cloud / local) and the
// package-internal auth seam. AUTH (the reworked §6 Auth model — internal,
// surface-preserving): the cloud profile holds the GitHub App identity (App id +
// RSA private key, via the satellite AppClient) and the target installation, and
// mints/refreshes the short-lived INSTALLATION TOKEN INTERNALLY (App-JWT ->
// MintInstallationToken). The token is NEVER threaded through the RA's contract
// surface and the RA NEVER calls a sibling RA to obtain it (NoSideways). This is
// the exact discipline C-CP-R's constructionPipelineAccess uses (and the senior
// review ratified) — re-expressed for the git-data path. The local profile needs
// no credential (a `file://` remote).

// --- LOCAL profile -----------------------------------------------------------

// localAuth is the auth source for the LOCAL/embedded profile: the user's on-disk
// git repo over a file:// remote needs no HTTP credential.
type localAuth struct{}

func (localAuth) gitAuth(context.Context) (fwgithub.GitAuth, error) {
	return fwgithub.GitAuth{Local: true}, nil
}

// NewLocalStore builds an artifactAccess Store for the LOCAL/embedded profile over
// the user's on-disk construction git repo reachable at repoURL (a file:// URL).
// No IO at construction; infrastructure failures surface lazily on the first call
// as typed fwra errors.
func NewLocalStore(repoURL string) (*Store, error) {
	blob, err := fwgithub.NewGitBlobStore(repoURL)
	if err != nil {
		return nil, err
	}
	return newStore(blob, localAuth{}), nil
}

// --- CLOUD profile -----------------------------------------------------------

// CloudConfig carries the GitHub binding the composition root supplies for the
// cloud profile: WHICH per-project construction repo, and the App identity that
// authenticates to it. Owner/RepoURL identify the repo; the App identity mints the
// installation token internally.
type CloudConfig struct {
	// RepoURL is the git-HTTP clone URL of the project's construction repo.
	RepoURL string
	// Owner is the repo owner (user or org); used to discover the installation when
	// InstallationID is 0.
	Owner string
	// AppID is the numeric GitHub App id (as a string).
	AppID string
	// PrivateKeyPEM is the App's RSA private key (PEM).
	PrivateKeyPEM string
	// APIBaseURL is the REST root ("" == github.com; a GHE host or a test fake
	// overrides it).
	APIBaseURL string
	// InstallationID is the App installation on the user's account/org. When 0, the
	// client discovers it on first use via FindInstallation(Owner).
	InstallationID int64
}

// appClient is the satellite App surface the cloud auth source depends on — an
// interface so the seam is unit-testable. The satellite *AppClient satisfies it.
type appClient interface {
	FindInstallation(ctx context.Context, account string) (int64, error)
	MintInstallationToken(ctx context.Context, installationID int64) (string, time.Time, error)
}

// tokenRefreshSkew re-mints the installation token a little before its hard expiry.
const tokenRefreshSkew = 60 * time.Second

// cloudAuth is the auth source for the CLOUD profile. It mints/refreshes the
// installation token internally (App-JWT -> MintInstallationToken), caching it to
// expiry. Thread-safe.
type cloudAuth struct {
	app   appClient
	owner string

	mu             sync.Mutex
	installationID int64
	token          string
	tokenExpiry    time.Time
}

func (c *cloudAuth) gitAuth(ctx context.Context) (fwgithub.GitAuth, error) {
	tok, err := c.installationToken(ctx)
	if err != nil {
		return fwgithub.GitAuth{}, err
	}
	return fwgithub.GitAuth{Token: tok}, nil
}

// installationToken returns a valid installation token, minting/refreshing it
// internally. A cached token is reused until shortly before expiry.
func (c *cloudAuth) installationToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-tokenRefreshSkew)) {
		return c.token, nil
	}
	if c.installationID == 0 {
		id, err := c.app.FindInstallation(ctx, c.owner)
		if err != nil {
			return "", err
		}
		c.installationID = id
	}
	tok, exp, err := c.app.MintInstallationToken(ctx, c.installationID)
	if err != nil {
		return "", err
	}
	c.token = tok
	c.tokenExpiry = exp
	return tok, nil
}

// NewCloudStore builds an artifactAccess Store for the CLOUD/remote profile over
// the user's per-project GitHub construction repo. It validates config eagerly (a
// missing field / bad key surfaces as fwra.ContractMisuse) but performs no network
// IO; the installation token is minted lazily on first use.
func NewCloudStore(cfg CloudConfig) (*Store, error) {
	if strings.TrimSpace(cfg.RepoURL) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "artifact.NewCloudStore: empty RepoURL")
	}
	if strings.TrimSpace(cfg.Owner) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "artifact.NewCloudStore: empty Owner")
	}
	blob, err := fwgithub.NewGitBlobStore(cfg.RepoURL)
	if err != nil {
		return nil, err
	}
	app, err := fwgithub.NewAppClient(cfg.AppID, cfg.PrivateKeyPEM, cfg.APIBaseURL)
	if err != nil {
		return nil, err
	}
	return newStore(blob, &cloudAuth{
		app:            app,
		owner:          cfg.Owner,
		installationID: cfg.InstallationID,
	}), nil
}
