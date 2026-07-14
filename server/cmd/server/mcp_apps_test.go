package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShellStubHTML(t *testing.T) {
	got := shellStubHTML("https://app.example.com", "42")
	if strings.Contains(got, "module") || strings.Contains(got, "crossorigin") {
		t.Error("stub must use classic CORS-exempt tags (no module/crossorigin)")
	}
	for _, want := range []string{
		`<!DOCTYPE html>`,
		// CLASSIC tags — no type="module", no crossorigin: CORS-exempt (spec §3.4)
		`<script src="https://app.example.com/mcp-app.js?v=42"></script>`,
		`<link rel="stylesheet" href="https://app.example.com/mcp-app.css?v=42">`,
		`<div id="root"></div>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stub missing %q\n---\n%s", want, got)
		}
	}
}

func TestDevCORSPreflight(t *testing.T) {
	h := devCORS(true, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest("OPTIONS", "/mcp", nil)
	req.Header.Set("Origin", "http://localhost:8080")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing ACAO header: %v", rr.Header())
	}
	// prod profile: no header
	h = devCORS(false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("prod must not set CORS headers")
	}
}
