package main

// toolproxy.go bridges the stdio mcp.Server `archistrator mcp` exposes to
// Claude Code onto the SAME tool catalog the spawned archistrator-server
// child's own `/mcp` streamable-HTTP mount exposes (server/cmd/server/mcp_mount.go's
// newMCPServer — systemDesign/projectDesign/construction/operations, each
// namespaced, registered on ONE *mcp.Server there): connect an mcp.Client to
// the child, list its tools ONCE at startup, and register each one on the
// local server with a handler that forwards CallTool verbatim to the child's
// session. This reuses the REAL tool catalog byte-for-byte — no tool is
// redeclared or reimplemented here, only relayed — for exactly the same
// "cannot import package main" reason serverchild.go documents: relaying a
// live connection is how a SEPARATE binary shares another binary's exposed
// surface.
//
// MCP Apps (mcp_apps.go's ui://archistrator/shell.html resource) is
// deliberately NOT proxied: the plan doc is explicit that MCP Apps is not a
// surface here (Claude Code doesn't render ui://; the upstream ext-apps#671
// bug blocks claude.ai/Desktop rendering too) — only ListTools/CallTool are
// bridged.
//
// SPA-URL footer (Task-5 review finding 2): the plan's Architecture section
// requires that "tool responses that reference session/project state include
// the SPA URL so the driver surfaces it." mountProxiedTools stays a verbatim
// relay for every other tool; for tools whose NAME falls in the get/start/
// session/project word families (referencesSessionOrProjectState below) it
// appends exactly one trailing text content block — "SPA: <url>" — to a
// successful result. This is name-based, not description- or schema-based:
// the relay never inspects a tool's semantics beyond its name, by design (see
// the package doc above — "no tool is redeclared or reimplemented here").
import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpVersion is the Implementation.Version this binary reports over both the
// local stdio mcp.Server and (via toolproxy) the upstream client connection —
// bumped alongside archistrator releases; a literal for now (no release
// tooling wired to this cmd yet).
const mcpVersion = "0.1.0"

// connectUpstream opens an mcp.Client session to the archistrator-server
// child's streamable-HTTP /mcp mount at http://addr/mcp.
func connectUpstream(ctx context.Context, addr string) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "archistrator-cli", Version: mcpVersion}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp"}
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to archistrator-server /mcp at %s: %w", addr, err)
	}
	return cs, nil
}

// mountProxiedTools lists upstream's tools once and registers each on local
// as a relay: CallTool(name, args) → upstream.CallTool(name, args), result
// returned verbatim EXCEPT for the SPA-URL footer described in the package
// doc above. spaURL is the local stack's render surface (e.g.
// "http://127.0.0.1:8877/") — passed in rather than reconstructed here so
// this function stays ignorant of how the caller resolved the port. Returns
// the number of tools mounted.
func mountProxiedTools(ctx context.Context, local *mcp.Server, upstream *mcp.ClientSession, spaURL string) (int, error) {
	listed, err := upstream.ListTools(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("list upstream tools: %w", err)
	}
	for _, t := range listed.Tools {
		tool := t // capture
		appendFooter := referencesSessionOrProjectState(tool.Name)
		local.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args any
			if req.Params != nil {
				args = req.Params.Arguments
			}
			res, err := upstream.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: args})
			if err != nil {
				return nil, fmt.Errorf("archistrator-server: %s: %w", tool.Name, err)
			}
			if appendFooter && res != nil && !res.IsError {
				res.Content = append(res.Content, &mcp.TextContent{Text: "SPA: " + spaURL})
			}
			return res, nil
		})
	}
	return len(listed.Tools), nil
}

// spaURLWordFamilies is the conservative, name-prefix rule the package doc
// above describes: a tool's camelCase-split name words (see camelWords) are
// checked, case-insensitively, against this fixed set. "get"/"start" are the
// read/begin verb families named in the brief (the catalog's actual verb for
// "begin" is Start — e.g. systemDesignStartSystemDesign — there is no
// literal "begin" in the 35-tool catalog); "session"/"project"/"projects"
// are the state-noun families. This deliberately UNDER-matches rather than
// over-matches: e.g. it does NOT catch systemDesignRequestArtifactDraft even
// though its description mentions "a handle to ... session" — the relay
// only ever looks at the tool NAME, never its description.
var spaURLWordFamilies = map[string]bool{
	"get":      true,
	"start":    true,
	"session":  true,
	"project":  true,
	"projects": true,
}

// referencesSessionOrProjectState reports whether toolName's camelCase words
// include one of spaURLWordFamilies. Matching whole words (not substrings)
// is deliberate: it is what keeps operationsQueryCostProjection from
// falsely matching on "Project" — "Projection" is a distinct whole word.
func referencesSessionOrProjectState(toolName string) bool {
	for _, w := range camelWords(toolName) {
		if spaURLWordFamilies[strings.ToLower(w)] {
			return true
		}
	}
	return false
}

// camelWords splits a lowerCamelCase identifier (archistrator's MCP tool
// naming convention — see mcp_mount.go) into its constituent words, e.g.
// "constructionGetSessionState" → ["construction", "Get", "Session",
// "State"]. A new word starts at each uppercase rune immediately preceded by
// a non-uppercase rune, so runs of capitals (acronyms) stay joined to
// whatever follows them — irrelevant for the word families checked here,
// none of which are acronyms.
func camelWords(s string) []string {
	if s == "" {
		return nil
	}
	var words []string
	start := 0
	runes := []rune(s)
	for i := 1; i < len(runes); i++ {
		if isUpper(runes[i]) && !isUpper(runes[i-1]) {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	words = append(words, string(runes[start:]))
	return words
}

func isUpper(r rune) bool {
	return r >= 'A' && r <= 'Z'
}
