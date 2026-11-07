package internal_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	projectstatefake "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate/fake"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	sourcecontrolfake "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol/fake"
)

// The sourceControlAccess ProjectRepo verb replaced the composition-root closure and the
// derived repoBase string the delivery Manager used to be handed (hooks.go
// DeliveryManagerRepo / DeliveryManagerRepoBase, Task 2.11e). These tests live here, not beside
// the components, for the reason delivery_views_test.go gives: a component package holds only its
// generated scenarios file (TestFileLayout).

// githubSource builds the GitHub realisation over a throwaway App key, the way the composition
// root does when the App identity is configured (hooks.buildGithubApp). Nothing is dialled:
// ProjectRepo is a name-as-identity derivation.
func githubSource(t *testing.T, apiBaseURL string) sourcecontrol.SourceControlAccess {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	app, err := fwgithub.NewAppClient("12345", pemKey, apiBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	_, access, err := sourcecontrol.NewGitHubSourceControl(app, "acme", "acme-app", apiBaseURL, true)
	if err != nil {
		t.Fatal(err)
	}
	return access
}

func localSource() sourcecontrol.SourceControlAccess {
	return sourcecontrol.NewGitLocalSourceControlAccess("file:///never-opened")
}

// TestProjectRepo_TruthTable pins the resolver exactly as the hooks pinned it:
//
//   - catalog (GitHub App identity configured): the project's own "account|account/<project>" ref
//     and the repo's WEB url on the API's web host (github.com, or the GHES root with /api/v3
//     stripped);
//   - local (GitLocal): the deterministic "local|local/<project>" ref, and no web url, because a
//     filesystem venue has no web host;
//   - a resolution the component cannot make (an empty project id) is a ContractMisuse error,
//     which the Manager reads as "this project has no venue", as the closure's catalog-miss arm
//     did (TestProjectRepo_ManagerReadsTheVerb pins the none arm end to end).
func TestProjectRepo_TruthTable(t *testing.T) {
	cases := []struct {
		name    string
		subject func(t *testing.T) sourcecontrol.SourceControlAccess
		project sourcecontrol.ProjectID
		want    sourcecontrol.ProjectRepoInfo
		wantErr bool
	}{
		{
			name:    "catalog: github.com default",
			subject: func(t *testing.T) sourcecontrol.SourceControlAccess { return githubSource(t, "") },
			project: "todo-app",
			want:    sourcecontrol.ProjectRepoInfo{Repo: "acme|acme/todo-app", WebURL: "https://github.com/acme/todo-app", Found: true},
		},
		{
			name:    "catalog: api.github.com root maps to the web host",
			subject: func(t *testing.T) sourcecontrol.SourceControlAccess { return githubSource(t, "https://api.github.com") },
			project: "todo-app",
			want:    sourcecontrol.ProjectRepoInfo{Repo: "acme|acme/todo-app", WebURL: "https://github.com/acme/todo-app", Found: true},
		},
		{
			name: "catalog: GHES api root strips /api/v3",
			subject: func(t *testing.T) sourcecontrol.SourceControlAccess {
				return githubSource(t, "https://ghe.example/api/v3/")
			},
			project: "todo-app",
			want:    sourcecontrol.ProjectRepoInfo{Repo: "acme|acme/todo-app", WebURL: "https://ghe.example/acme/todo-app", Found: true},
		},
		{
			name:    "catalog: empty project id is contract misuse",
			subject: func(t *testing.T) sourcecontrol.SourceControlAccess { return githubSource(t, "") },
			project: "",
			wantErr: true,
		},
		{
			name:    "local: deterministic GitLocal ref, no web url",
			subject: func(*testing.T) sourcecontrol.SourceControlAccess { return localSource() },
			project: "todo-app",
			want:    sourcecontrol.ProjectRepoInfo{Repo: sourcecontrol.GitLocalRepoRefForProject("todo-app"), WebURL: "", Found: true},
		},
		{
			name:    "local: empty project id is contract misuse",
			subject: func(*testing.T) sourcecontrol.SourceControlAccess { return localSource() },
			project: "",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.subject(t).ProjectRepo(fwra.Context{}, tc.project)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ProjectRepo(%q) = %+v, want a ContractMisuse error", tc.project, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ProjectRepo(%q): %v", tc.project, err)
			}
			if got != tc.want {
				t.Fatalf("ProjectRepo(%q) = %+v, want %+v", tc.project, got, tc.want)
			}
		})
	}
}

// TestProjectRepo_ManagerReadsTheVerb drives the Manager side through the public QueryProjectView(summary):
// each git row's prUrl is composed from the web url the rail's ProjectRepo answers, a local
// venue has no pull request, and every way of having no venue (no rail bound, a rail that errors,
// a rail that reports no repo) leaves the rows without a link. It is the "none" arm of the venue
// truth table the composition-root closure carried.
func TestProjectRepo_ManagerReadsTheVerb(t *testing.T) {
	const project = "todo-app"
	ps := &projectstatefake.FakeProjectStateAccess{
		ReadProjectFn: func(fwra.Context, projectstate.ProjectID) (projectstate.Project, error) {
			return projectstate.Project{
				ID: project, Name: project,
				ActivityGit: map[string]projectstate.ActivityGitStatus{
					"C-x": {ActivityID: "C-x", BranchName: "activity/C-x", PullRequestRef: "12"},
				},
			}, nil
		},
	}
	fakeRail := func(fn func(sourcecontrol.ProjectID) (sourcecontrol.ProjectRepoInfo, error)) sourcecontrol.SourceControlAccess {
		return &sourcecontrolfake.FakeSourceControlAccess{
			ProjectRepoFn: func(_ fwra.Context, pid sourcecontrol.ProjectID) (sourcecontrol.ProjectRepoInfo, error) {
				return fn(pid)
			},
		}
	}
	cases := []struct {
		name        string
		rail        sourcecontrol.SourceControlAccess
		wantPRURL   string
		wantPRRef   string
		wantNumber  int64
		wantRowKept bool
	}{
		{
			name:      "catalog: prUrl on the project's own repo",
			rail:      githubSource(t, ""),
			wantPRURL: "https://github.com/acme/todo-app/pull/12", wantPRRef: "12", wantNumber: 12, wantRowKept: true,
		},
		{
			name:      "catalog: GHES web host",
			rail:      githubSource(t, "https://ghe.example/api/v3"),
			wantPRURL: "https://ghe.example/acme/todo-app/pull/12", wantPRRef: "12", wantNumber: 12, wantRowKept: true,
		},
		{
			name: "none: no rail bound",
			rail: nil, wantPRURL: "", wantPRRef: "12", wantNumber: 12, wantRowKept: true,
		},
		{
			name: "none: rail cannot resolve the project",
			rail: fakeRail(func(sourcecontrol.ProjectID) (sourcecontrol.ProjectRepoInfo, error) {
				return sourcecontrol.ProjectRepoInfo{}, errors.New("boom")
			}),
			wantPRURL: "", wantPRRef: "12", wantNumber: 12, wantRowKept: true,
		},
		{
			name: "none: rail reports no repo",
			rail: fakeRail(func(sourcecontrol.ProjectID) (sourcecontrol.ProjectRepoInfo, error) {
				return sourcecontrol.ProjectRepoInfo{WebURL: "https://github.com/central/repo"}, nil
			}),
			wantPRURL: "", wantPRRef: "12", wantNumber: 12, wantRowKept: true,
		},
		{
			// A GitLocal venue has no pull request: the activity reads as its branch with no PR, and no
			// plausible-looking link is stamped onto it.
			name: "local: branch row, no pull request",
			rail: localSource(), wantPRURL: "", wantPRRef: "", wantNumber: 0, wantRowKept: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := delivery.NewDeliveryManager(noSessions{}, ps, nil, nil, nil, nil, nil, nil, nil, nil, tc.rail, nil, nil, nil, nil, nil, 0, "")
			pid := project
			view, err := mgr.QueryProjectView(viewCtx(), delivery.ProjectViewQuery{Kind: delivery.ProjectViewSummary, ProjectID: &pid})
			if err != nil {
				t.Fatalf("QueryProjectView(summary): %v", err)
			}
			st := view.Summary
			if st == nil {
				t.Fatalf("view %+v carries no summary", view)
			}
			row, ok := st.GitRows["C-x"]
			if tc.wantRowKept && !ok {
				t.Fatalf("GitRows = %+v, want the stored row for C-x", st.GitRows)
			}
			if ok && (row.PrURL != tc.wantPRURL || row.PullRequestRef != tc.wantPRRef || row.PrNumber != tc.wantNumber) {
				t.Fatalf("row = {prUrl %q, ref %q, number %d}, want {%q, %q, %d}",
					row.PrURL, row.PullRequestRef, row.PrNumber, tc.wantPRURL, tc.wantPRRef, tc.wantNumber)
			}
		})
	}
}
