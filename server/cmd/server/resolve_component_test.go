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
	cases := []struct{ title, fallback, want string }{
		{"Build Operated Runtime Access", "C-OR", "operatedRuntimeAccess"},
		// Parenthetical names settlementManager but the target is billingManager.
		{"Build Billing Manager (reuses sunk settlementManager skeleton)", "C-BM", "billingManager"},
		{"Build MCP Client", "C-CM", "mcpClient"},
		// No contract match → fall back to the activity id.
		{"Wire up the CI gate", "N-CI", "N-CI"},
	}
	for _, c := range cases {
		if got := resolveComponentID(c.title, c.fallback, contracts); got != c.want {
			t.Errorf("resolveComponentID(%q) = %q, want %q", c.title, got, c.want)
		}
	}
}
