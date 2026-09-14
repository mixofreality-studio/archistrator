package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// In production the SPA is served by nginx (webApp/nginx.conf, baked into the
// archistrator-webapp image), not by this server, so framedenial.go's wrap never
// sees those responses. nginx drops every server-level add_header in a location
// that declares its own add_header, so each location block must carry both
// anti-framing headers itself, with `always` so error responses carry them too.
// This test pins that for every location, including any added later.
func TestNginxConfDeniesFramingInEveryLocation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "webApp", "nginx.conf"))
	if err != nil {
		t.Fatalf("read webApp/nginx.conf: %v", err)
	}
	want := []string{
		`add_header X-Frame-Options "DENY" always;`,
		`add_header Content-Security-Policy "frame-ancestors 'none'" always;`,
	}
	blocks := nginxLocationBlocks(string(raw))
	if len(blocks) == 0 {
		t.Fatal("no location blocks found in webApp/nginx.conf")
	}
	for header, body := range blocks {
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("nginx %s is missing %s", header, w)
			}
		}
	}
}

// nginxLocationBlocks maps each `location ... {` header line to the text of
// its block, with comments stripped.
func nginxLocationBlocks(conf string) map[string]string {
	var lines []string
	for l := range strings.SplitSeq(conf, "\n") {
		if i := strings.Index(l, "#"); i >= 0 {
			l = l[:i]
		}
		lines = append(lines, strings.TrimSpace(l))
	}
	blocks := map[string]string{}
	for i, l := range lines {
		if !strings.HasPrefix(l, "location ") {
			continue
		}
		var body strings.Builder
		depth := 0
		for _, bl := range lines[i:] {
			depth += strings.Count(bl, "{") - strings.Count(bl, "}")
			body.WriteString(bl + "\n")
			if depth == 0 {
				break
			}
		}
		blocks[l] = body.String()
	}
	return blocks
}
