package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"

	"github.com/mixofreality-studio/archistrator/server/internal/client/web"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
)

// fakeTestRunFiles is the Manager half of the streaming endpoint, recorded.
type fakeTestRunFiles struct {
	gotProject delivery.ProjectID
	gotRun     string
	gotPath    string
	file       delivery.TestRunFile
	err        error
}

func (f *fakeTestRunFiles) ReadTestRunFile(_ fwmanager.Context, projectID delivery.ProjectID, runID, filePath string) (delivery.TestRunFile, error) {
	f.gotProject, f.gotRun, f.gotPath = projectID, runID, filePath
	return f.file, f.err
}

// mountTestRunFileRoute composes the route exactly as production does: the generated
// server at "/", then ExtraMounts over it, with a dev principal so the auth boundary
// admits the request and the PDP decides on it.
func mountTestRunFileRoute(t *testing.T, reader delivery.TestRunFileReader) *httptest.Server {
	t.Helper()
	root := http.NewServeMux()
	root.Handle("/", web.NewServer(web.DevConfig{}, nil))
	h := newTestAppHooks()
	h.testRunFiles = reader
	h.ExtraMounts(root, &Config{}, web.DevConfig{Enabled: true, Principal: devPrincipal()}, nil, WebManagers{})
	ts := httptest.NewServer(root)
	t.Cleanup(ts.Close)
	return ts
}

func getBody(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url) //nolint:gosec // test server URL
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(body)
}

// GET /api/v1/delivery/test-run/{runID}/file?projectId=&path= streams the file the
// Manager resolves, under the Manager's Content-Type, with the anti-framing headers
// every route carries.
func TestTestRunFileEndpoint_StreamsTheManagersFile(t *testing.T) {
	reader := &fakeTestRunFiles{file: delivery.TestRunFile{Bytes: []byte("webm-bytes"), MIMEType: "video/webm"}}
	ts := mountTestRunFileRoute(t, reader)
	resp, body := getBody(t, ts.URL+"/api/v1/delivery/test-run/9/file?projectId=p&path=billingManager%2FP2.webm")
	if resp.StatusCode != http.StatusOK || body != "webm-bytes" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/webm" {
		t.Errorf("Content-Type %q, want video/webm", ct)
	}
	if reader.gotProject != "p" || reader.gotRun != "9" || reader.gotPath != "billingManager/P2.webm" {
		t.Errorf("manager called with %q %q %q", reader.gotProject, reader.gotRun, reader.gotPath)
	}
	assertFrameDenied(t, "test-run file", resp.Header)
}

// The selectors are required, a Manager refusal maps to its HTTP status, and a
// composition with no reader answers 503 rather than panicking.
func TestTestRunFileEndpoint_RefusalsMapToStatuses(t *testing.T) {
	reader := &fakeTestRunFiles{}
	ts := mountTestRunFileRoute(t, reader)
	for _, tc := range []struct {
		name  string
		query string
		err   error
		want  int
	}{
		{"no path", "?projectId=p", nil, http.StatusBadRequest},
		{"no projectId", "?path=x", nil, http.StatusBadRequest},
		{"unknown run", "?projectId=p&path=x", fwmanager.New(fwmanager.NotFound, "no run"), http.StatusNotFound},
		{"escaping path", "?projectId=p&path=..%2Fetc", fwmanager.New(fwmanager.ContractMisuse, "bad path"), http.StatusBadRequest},
		{"artifact gone", "?projectId=p&path=x", fwmanager.New(fwmanager.Infrastructure, "zip"), http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader.err = tc.err
			resp, body := getBody(t, ts.URL+"/api/v1/delivery/test-run/9/file"+tc.query)
			if resp.StatusCode != tc.want {
				t.Fatalf("status %d body %q, want %d", resp.StatusCode, body, tc.want)
			}
		})
	}
	unwired := mountTestRunFileRoute(t, nil)
	resp, body := getBody(t, unwired.URL+"/api/v1/delivery/test-run/9/file?projectId=p&path=x")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("no reader wired: status %d body %q, want 503", resp.StatusCode, body)
	}
}

// Unauthenticated requests are denied at the auth boundary (no dev principal, nil
// validator = deny-all), before the Manager is reached.
func TestTestRunFileEndpoint_DeniesUnauthenticated(t *testing.T) {
	reader := &fakeTestRunFiles{file: delivery.TestRunFile{Bytes: []byte("x"), MIMEType: "text/plain"}}
	root := http.NewServeMux()
	root.Handle("/", web.NewServer(web.DevConfig{}, nil))
	h := newTestAppHooks()
	h.testRunFiles = reader
	h.ExtraMounts(root, &Config{}, web.DevConfig{}, nil, WebManagers{})
	ts := httptest.NewServer(root)
	defer ts.Close()
	resp, _ := getBody(t, ts.URL+"/api/v1/delivery/test-run/9/file?projectId=p&path=x")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
	if reader.gotRun != "" {
		t.Fatal("the Manager must not be reached by an unauthenticated request")
	}
}
