package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// F-QA2-46: a 5xx written by the (generated, log-free) handler surface must leave a
// server-side WARN trace carrying method, path, status, and the error-envelope body.
func TestLog5xxResponsesLogsServerFaults(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"design session unavailable: signal response lost","code":"unavailable"}`))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system-design/submit-review-decision/p1", nil)
	log5xxResponses(logger, inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (middleware must not alter the response)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "signal response lost") {
		t.Fatalf("response body altered: %q", rec.Body.String())
	}
	out := buf.String()
	for _, want := range []string{
		"level=WARN",
		"method=POST",
		"path=/api/v1/system-design/submit-review-decision/p1",
		"status=503",
		"signal response lost",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q; got: %s", want, out)
		}
	}
}

func TestLog5xxResponsesSilentOnSuccessAndClientErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"explicit 200", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}},
		{"implicit 200 (Write without WriteHeader)", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}},
		{"client error 409", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"open comments","code":"failed_precondition"}`))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, nil))
			rec := httptest.NewRecorder()
			log5xxResponses(logger, tc.handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))
			if buf.Len() != 0 {
				t.Fatalf("unexpected log output: %s", buf.String())
			}
		})
	}
}

// Pins the ExtraMounts capture-and-shadow mechanism (hooks.go): the generated
// composition root mounts the API surface at "/" BEFORE ExtraMounts runs; the hook
// captures that handler via ServeMux.Handler and shadows the more specific
// "/api/v1/" pattern with the logging wrap. /healthz (and every non-/api/v1 route)
// must keep routing to the unwrapped surface.
func TestLog5xxShadowMountRoutesApiThroughMiddleware(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	genLike := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"unavailable","code":"unavailable"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError) // a health 500 must NOT be wrapped/logged
	})

	root := http.NewServeMux()
	root.Handle("/", genLike) // main.gen.go's mount, before the hook runs

	// The exact capture-and-shadow lines from ExtraMounts.
	if apiSurface, pat := root.Handler(httptest.NewRequest(http.MethodGet, "/api/v1/", nil)); pat != "" {
		root.Handle("/api/v1/", log5xxResponses(logger, apiSurface))
	} else {
		t.Fatal("capture returned no handler for /api/v1/")
	}

	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/system-design/submit-review-decision/p1", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("api status = %d, want 503", rec.Code)
	}
	if !strings.Contains(buf.String(), "status=503") {
		t.Fatalf("shadow mount did not log the api 5xx; log: %s", buf.String())
	}

	buf.Reset()
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("healthz status = %d, want 500 passthrough", rec.Code)
	}
	if buf.Len() != 0 {
		t.Fatalf("non-/api/v1 route was wrapped: %s", buf.String())
	}
}

func TestLog5xxResponsesBoundsTheBodySnippet(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	huge := strings.Repeat("x", fault5xxBodyCap*4)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(huge))
	})

	rec := httptest.NewRecorder()
	log5xxResponses(logger, inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))

	if got := rec.Body.Len(); got != len(huge) {
		t.Fatalf("response body truncated by middleware: %d != %d", got, len(huge))
	}
	// The logged snippet is capped: the full 4x-cap body must NOT appear in the log.
	if strings.Contains(buf.String(), huge) {
		t.Fatalf("log carries the unbounded body")
	}
	if !strings.Contains(buf.String(), strings.Repeat("x", fault5xxBodyCap)) {
		t.Fatalf("log missing the capped snippet; got: %s", buf.String())
	}
}
