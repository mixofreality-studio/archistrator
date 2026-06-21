package constructionpipeline

// actions_http_client.go is the concrete ghActionsClient — the ONLY place this RA
// speaks to GitHub Actions, by delegating to the github satellite's AppClient
// (framework-go-infrastructure-github). It is the C-CP-R analog of the former
// argo_http_client.go: the seam realisation that holds the infrastructure
// connection + auth and confines every GitHub-Actions wire detail.
//
// AUTH (the reworked §6 Auth model — internal, surface-preserving): this client
// holds the GitHub App identity (App id + RSA private key, via the satellite
// AppClient) and the target installation. It mints/refreshes the short-lived
// INSTALLATION TOKEN INTERNALLY (App-JWT → MintInstallationToken) and presents it
// on every Actions call. The token is NEVER threaded through the RA's contract
// surface and the RA NEVER calls a sibling RA to obtain it (NoSideways). This is the
// exact discipline the Argo path used (a k8s ServiceAccount token acquired inside
// the package) — re-expressed for GitHub. A short token cache avoids minting on
// every call; an expired/rejected token is re-minted on the next call and surfaces
// as fwra.Auth if the App lacks permission.

import (
	"context"
	"strings"
	"sync"
	"time"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ActionsConfig carries the construction-pipeline GitHub binding the composition
// root supplies. Owner/Repo/WorkflowFile/Ref identify WHERE the construction
// workflow runs; the App identity authenticates to it.
type ActionsConfig struct {
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
	// Owner is the repo owner (user or org) the construction workflow lives under.
	Owner string
	// Repo is the repo name.
	Repo string
	// WorkflowFile is the aiarch construction workflow file name (e.g.
	// "aiarch-construct.yml") the RA dispatches. It MUST stamp
	// run-name: aiarch-cp-${{ inputs.idempotency_token }} so the launched run carries
	// the dedup name (see github satellite RunNamePrefix / DispatchInputKeyIdempotencyToken).
	WorkflowFile string
	// Ref is the git ref to dispatch against (e.g. "main").
	Ref string
}

// appClient is the satellite surface this seam depends on — declared as an
// interface so the seam realisation is unit-testable against a satellite fake if
// ever needed, and so the dependency is explicit. The satellite *AppClient
// satisfies it.
type appClient interface {
	FindInstallation(ctx context.Context, account string) (int64, error)
	MintInstallationToken(ctx context.Context, installationID int64) (string, time.Time, error)
	DispatchWorkflow(ctx context.Context, owner, repo, workflowFile, ref string, inputs map[string]string, instToken string) error
	ListRunsByName(ctx context.Context, owner, repo, workflowFile, runName, instToken string) ([]fwgithub.WorkflowRun, error)
	GetRun(ctx context.Context, owner, repo string, runID int64, instToken string) (fwgithub.WorkflowRun, error)
	CancelRun(ctx context.Context, owner, repo string, runID int64, instToken string) error
}

// ghActionsRESTClient is the concrete ghActionsClient over the github satellite.
type ghActionsRESTClient struct {
	app          appClient
	owner        string
	repo         string
	workflowFile string
	ref          string

	mu             sync.Mutex
	installationID int64
	token          string
	tokenExpiry    time.Time
}

var _ ghActionsClient = (*ghActionsRESTClient)(nil)

// tokenRefreshSkew re-mints the installation token a little before its hard expiry.
const tokenRefreshSkew = 60 * time.Second

// NewActionsClient builds the concrete GitHub-Actions seam from the App identity +
// repo binding. It validates config eagerly (a missing field / bad key is a
// configuration error surfaced as fwra.ContractMisuse) but performs no network IO;
// the installation token is minted lazily on first use.
func NewActionsClient(cfg ActionsConfig) (*ghActionsRESTClient, error) {
	if strings.TrimSpace(cfg.Owner) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "NewActionsClient: empty Owner")
	}
	if strings.TrimSpace(cfg.Repo) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "NewActionsClient: empty Repo")
	}
	if strings.TrimSpace(cfg.WorkflowFile) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "NewActionsClient: empty WorkflowFile")
	}
	ref := strings.TrimSpace(cfg.Ref)
	if ref == "" {
		ref = "main"
	}
	app, err := fwgithub.NewAppClient(cfg.AppID, cfg.PrivateKeyPEM, cfg.APIBaseURL)
	if err != nil {
		return nil, err
	}
	return &ghActionsRESTClient{
		app:            app,
		owner:          cfg.Owner,
		repo:           cfg.Repo,
		workflowFile:   cfg.WorkflowFile,
		ref:            ref,
		installationID: cfg.InstallationID,
	}, nil
}

// installationToken returns a valid installation token, minting/refreshing it
// internally. Thread-safe; a cached token is reused until shortly before expiry.
func (c *ghActionsRESTClient) installationToken(ctx context.Context) (string, error) {
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

// resolveTarget applies the per-call ghTarget over this client's configured default
// (the construction repo + aiarch-construct.yml). An EMPTY field on the target falls
// back to the configured value, so a ZERO ghTarget reproduces the legacy UC3
// behavior exactly (owner/repo/workflowFile all default), while a per-project DESIGN
// dispatch overrides all three. owner/repo address the per-project repo; workflowFile
// selects aiarch-design.yml. ref stays the client's configured ref (the design branch
// is carried as the dispatch target_branch input, not the workflow ref).
func (c *ghActionsRESTClient) resolveTarget(tgt ghTarget) (owner, repo, workflowFile string) {
	owner, repo, workflowFile = c.owner, c.repo, c.workflowFile
	if tgt.owner != "" {
		owner = tgt.owner
	}
	if tgt.repo != "" {
		repo = tgt.repo
	}
	if tgt.workflowFile != "" {
		workflowFile = tgt.workflowFile
	}
	return owner, repo, workflowFile
}

func (c *ghActionsRESTClient) listRunsByName(ctx context.Context, tgt ghTarget, runName string) ([]ghRun, error) {
	tok, err := c.installationToken(ctx)
	if err != nil {
		return nil, err
	}
	owner, repo, workflowFile := c.resolveTarget(tgt)
	runs, err := c.app.ListRunsByName(ctx, owner, repo, workflowFile, runName, tok)
	if err != nil {
		return nil, err
	}
	out := make([]ghRun, 0, len(runs))
	for _, r := range runs {
		out = append(out, toGHRun(r))
	}
	return out, nil
}

func (c *ghActionsRESTClient) dispatch(ctx context.Context, tgt ghTarget, idempotencyToken, _ string, dispatchInputs map[string]string) error {
	tok, err := c.installationToken(ctx)
	if err != nil {
		return err
	}
	// Merge the caller's optional extra inputs FIRST, then stamp the RA-controlled
	// idempotency token LAST so it wins any key collision (the load-bearing dedup /
	// run-name anchor stays RA-controlled — constructionPipelineAccess.md §0d.6).
	inputs := make(map[string]string, len(dispatchInputs)+1)
	for k, v := range dispatchInputs {
		inputs[k] = v
	}
	inputs[fwgithub.DispatchInputKeyIdempotencyToken] = idempotencyToken
	owner, repo, workflowFile := c.resolveTarget(tgt)
	return c.app.DispatchWorkflow(ctx, owner, repo, workflowFile, c.ref, inputs, tok)
}

func (c *ghActionsRESTClient) getRun(ctx context.Context, tgt ghTarget, runID int64) (ghRun, error) {
	tok, err := c.installationToken(ctx)
	if err != nil {
		return ghRun{}, err
	}
	owner, repo, _ := c.resolveTarget(tgt)
	run, err := c.app.GetRun(ctx, owner, repo, runID, tok)
	if err != nil {
		return ghRun{}, err
	}
	return toGHRun(run), nil
}

func (c *ghActionsRESTClient) cancelRun(ctx context.Context, tgt ghTarget, runID int64) error {
	tok, err := c.installationToken(ctx)
	if err != nil {
		return err
	}
	owner, repo, _ := c.resolveTarget(tgt)
	return c.app.CancelRun(ctx, owner, repo, runID, tok)
}

// toGHRun bridges the satellite's WorkflowRun to the seam's package-internal ghRun.
func toGHRun(r fwgithub.WorkflowRun) ghRun {
	return ghRun{
		id:         r.ID,
		name:       r.Name,
		status:     string(r.Status),
		conclusion: string(r.Conclusion),
	}
}
