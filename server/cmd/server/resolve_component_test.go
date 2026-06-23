package main

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestResolveComponentID(t *testing.T) {
	contracts := map[string]projectstate.ServiceContract{
		"operatedRuntimeAccess": {Component: "operatedRuntimeAccess"},
		"billingManager":        {Component: "billingManager"},
		"settlementManager":     {Component: "settlementManager"},
		"mcpClient":             {Component: "mcpClient"},
	}
	cases := []struct {
		name     string
		title    string
		produced []projectstate.ProducedArtifact
		want     string
		wantOK   bool
	}{
		{
			name:   "fuzzy title match (no hint)",
			title:  "Build Operated Runtime Access",
			want:   "operatedRuntimeAccess",
			wantOK: true,
		},
		{
			// Parenthetical names settlementManager but the target is billingManager.
			name:   "parenthetical does not steal the fuzzy match",
			title:  "Build Billing Manager (reuses sunk settlementManager skeleton)",
			want:   "billingManager",
			wantOK: true,
		},
		{
			name:   "fuzzy title match mcp",
			title:  "Build MCP Client",
			want:   "mcpClient",
			wantOK: true,
		},
		{
			// produced[] service-contract hint is authoritative even when the title would
			// fuzzy-match a DIFFERENT (or no) contract.
			name:  "produced hint wins over title",
			title: "Some unrelated activity title",
			produced: []projectstate.ProducedArtifact{
				{Kind: "code", Title: "ignored code artifact"},
				{Kind: "service-contract", Title: "operatedRuntimeAccess — service contract"},
			},
			want:   "operatedRuntimeAccess",
			wantOK: true,
		},
		{
			// No contract match AND no hint → sentinel (caller logs + skips dispatch).
			name:   "no match returns sentinel",
			title:  "Wire up the CI gate",
			want:   "",
			wantOK: false,
		},
		{
			// A produced hint that does not name a real key falls through to the (absent)
			// fuzzy title match → sentinel.
			name:  "stale hint with no key falls through to sentinel",
			title: "Wire up the CI gate",
			produced: []projectstate.ProducedArtifact{
				{Kind: "service-contract", Title: "ghostComponent — service contract"},
			},
			want:   "",
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := resolveComponentID(c.title, c.produced, contracts)
			if got != c.want || ok != c.wantOK {
				t.Errorf("resolveComponentID(%q) = (%q, %v), want (%q, %v)", c.title, got, ok, c.want, c.wantOK)
			}
		})
	}
}
