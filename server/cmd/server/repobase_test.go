package main

import "testing"

// TestConstructionRepoBase covers the composition-root mapping from the construction-repo
// config (owner/name + the GitHub API base host) onto the WEB base the webClient git-row
// projection composes each prUrl from (D-PA-GIT-PRURL-ruling R1). It pins: the github.com
// default (web host github.com, NOT api.github.com), GHES API-root → web-host stripping,
// and the unconfigured-repo empty result (which makes the projection omit prUrl).
func TestConstructionRepoBase(t *testing.T) {
	cases := []struct {
		name       string
		apiBaseURL string
		owner      string
		repo       string
		want       string
	}{
		{"github.com default", "", "acme", "proj", "https://github.com/acme/proj"},
		{"github.com explicit api root ignored for web host", "https://api.github.com", "acme", "proj", "https://api.github.com/acme/proj"},
		{"GHES api root strips /api/v3", "https://ghe.example/api/v3", "acme", "proj", "https://ghe.example/acme/proj"},
		{"GHES api root trailing slash", "https://ghe.example/api/v3/", "acme", "proj", "https://ghe.example/acme/proj"},
		{"unconfigured owner", "", "", "proj", ""},
		{"unconfigured repo", "", "acme", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := constructionRepoBase(tc.apiBaseURL, tc.owner, tc.repo)
			if got != tc.want {
				t.Fatalf("constructionRepoBase(%q,%q,%q) = %q, want %q", tc.apiBaseURL, tc.owner, tc.repo, got, tc.want)
			}
		})
	}
}
