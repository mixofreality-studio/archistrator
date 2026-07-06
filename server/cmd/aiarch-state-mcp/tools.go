package main

// tools.go registers the aiarch-state MCP tool surface, SCOPED BY JOB MODE. These tool
// descriptions ARE the new prompt surface — a fresh agent must be able to author a valid
// Method artifact from them alone, so each is written to stand on its own. Draft mode gets
// the full authoring set (incl. putDraftModel); critique mode gets read verbs +
// setCritiqueVerdict and NEVER putDraftModel (a critic must not rewrite the model).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ---- tool input types (the SDK infers each tool's JSON Schema from these) ----

type emptyInput struct{}

type getResearchSourceInput struct {
	Path string `json:"path" jsonschema:"the repo-relative path of a research source, exactly as listResearchSources reported it"`
}

type getCommittedSlotInput struct {
	Kind string `json:"kind" jsonschema:"the Method artifact kind to read the committed basis model of (e.g. mission, volatilities, coreUseCases, system)"`
}

type putDraftModelInput struct {
	Model map[string]any `json:"model" jsonschema:"the complete typed model object for THIS design job's artifact kind — validated through the full server codec and the Method CI rules before it is accepted"`
}

type respondToReviewCommentInput struct {
	ID       string `json:"id" jsonschema:"the id of the open review-ledger comment you are responding to (see getReviewThread)"`
	Response string `json:"response" jsonschema:"how you addressed the comment in this redraft, or a concise reasoned pushback if you disagree"`
}

type setCritiqueVerdictInput struct {
	Verdict string `json:"verdict" jsonschema:"exactly 'approve' or 'revise'"`
	Notes   string `json:"notes" jsonschema:"on 'revise', the concrete revision guidance the architect should apply; empty on 'approve'"`
}

type publishDraftInput struct {
	Message string `json:"message" jsonschema:"a short commit message describing what you drafted or changed"`
}

// composedVerb is one hand-written agent-facing tool — a COMPOSED VERB (doctrine
// rule 3: invariants compile into composed verbs). modes lists the job modes that
// include it under FALLBACK (job-mode) scoping; register adds it to the server.
type composedVerb struct {
	name     string
	modes    []string
	register func(*mcp.Server)
}

// buildServer constructs the MCP server and registers the effective tool set. It is
// the single wiring point exercised by the rig test over stdio.
//
// SCOPING (agentic-managers spec item 5). When the session carries a resolved tool
// allowlist (AIARCH_TOOL_ALLOWLIST — the manager resolved it from archistrator's OWN
// System dynamics), the server registers EXACTLY those tools: manifest-scoping. When
// it does not (the BOOTSTRAP case, until the dynamics document palettes), the server
// falls back to the hand-curated per-mode composed-verb set and logs a WARN — strict
// manifest-scoping flips on automatically once a palette (hence an allowlist) exists.
//
// The composed verbs always sit ON TOP of the raw generated internal surface
// (projectstate.InternalToolCatalog): an allowlisted raw RA/Engine tool with no
// composed shadow is registered from the generated catalog; an agent-hidden raw op
// (e.g. raw CommitArtifact — merge authority stays with the server rail) is NEVER
// registered even when named.
func buildServer(s *Session) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "aiarch-state",
		Title:   "aiarch project-state",
		Version: "0.1.0",
	}, nil)

	verbs := composedVerbs(s)

	if len(s.Allowlist) > 0 {
		registerFromAllowlist(srv, s, verbs)
	} else {
		warnf("no tool allowlist provided (AIARCH_TOOL_ALLOWLIST empty); falling back to the job-mode %q composed-verb set", s.Mode)
		for _, v := range verbs {
			if containsStr(v.modes, s.Mode) {
				v.register(srv)
			}
		}
	}
	return srv
}

// registerFromAllowlist registers exactly the tools named in the session allowlist:
// a composed verb by name (mode no longer gates — the palette is the authority), or a
// raw generated internal tool from the catalog (refusing agent-hidden ops), or a WARN
// for an unknown name.
func registerFromAllowlist(srv *mcp.Server, s *Session, verbs []composedVerb) {
	composed := make(map[string]composedVerb, len(verbs))
	for _, v := range verbs {
		composed[v.name] = v
	}
	for _, name := range s.Allowlist {
		if v, ok := composed[name]; ok {
			v.register(srv)
			continue
		}
		if tool, ok := projectstate.InternalToolByName(name); ok {
			if tool.AgentHidden {
				warnf("tool allowlist names agent-hidden op %q; refusing to register it (its authority stays on the server rail / a composed verb replaces it)", name)
				continue
			}
			registerRawTool(srv, tool)
			continue
		}
		warnf("tool allowlist names unknown tool %q; skipping", name)
	}
}

// composedVerbs returns the composed-verb registry bound to this session, each with
// its FALLBACK job-mode membership and its register thunk. The tool DESCRIPTIONS are
// the agent's prompt surface, so each stands on its own.
func composedVerbs(s *Session) []composedVerb {
	all := []string{jobModeDraft, jobModeCritique, jobModeAnswer}
	return []composedVerb{
		{name: "listResearchSources", modes: all, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "listResearchSources",
				Description: "List the committed research corpus for this project (each source's title and repo-relative path). " +
					"Use this to discover the source material for the Mission; read a source's full text with getResearchSource.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			}, textHandler(func(context.Context, emptyInput) (string, error) { return s.listResearchSources() }))
		}},
		{name: "getResearchSource", modes: all, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "getResearchSource",
				Description: "Return the full text of one research source, addressed by the repo-relative path listResearchSources reports. " +
					"Confined to this project repository.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			}, textHandler(func(_ context.Context, in getResearchSourceInput) (string, error) {
				return s.getResearchSource(in.Path)
			}))
		}},
		{name: "getCommittedSlot", modes: all, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "getCommittedSlot",
				Description: "Return the committed typed model for ANY Method artifact kind — your read-only basis access to the predecessors this artifact builds on " +
					"(e.g. when drafting the System, read the committed coreUseCases and volatilities). Reports plainly when the kind is not committed yet.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			}, textHandler(func(_ context.Context, in getCommittedSlotInput) (string, error) { return s.getCommittedSlot(in.Kind) }))
		}},
		{name: "getDraftSlot", modes: all, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "getDraftSlot",
				Description: "Return the current draft of THIS design job's artifact (whatever its status) on this branch, or a note that none exists. " +
					"On an amendment or redraft, start from this draft rather than from scratch.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			}, textHandler(func(context.Context, emptyInput) (string, error) { return s.getDraftSlot() }))
		}},
		{name: "getReviewThread", modes: all, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "getReviewThread",
				Description: "Return this artifact's durable review ledger (each reviewer comment's id, anchor, text, status, and your prior response). " +
					"You MUST respond to every OPEN comment before publishing — use respondToReviewComment.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			}, textHandler(func(context.Context, emptyInput) (string, error) { return s.getReviewThread() }))
		}},
		{name: "setCritiqueVerdict", modes: []string{jobModeCritique}, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "setCritiqueVerdict",
				Description: "Record your Product-Manager critique verdict for this artifact: exactly 'approve' or 'revise'. On 'revise', give concrete revision notes " +
					"naming the change the architect should make. This does NOT rewrite the model. A verdict is required; finish by calling publishDraft.",
			}, textHandler(func(_ context.Context, in setCritiqueVerdictInput) (string, error) {
				if err := s.setCritiqueVerdict(in.Verdict, in.Notes); err != nil {
					return "", err
				}
				return "Recorded the critique verdict. Call publishDraft to commit it.", nil
			}))
		}},
		{name: "putDraftModel", modes: []string{jobModeDraft}, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "putDraftModel",
				Description: "Submit the complete typed model for THIS design job's artifact. It is validated through the FULL server codec AND the Method CI rules " +
					"before it is accepted: if it fails, this returns the exact, actionable errors and writes NOTHING — fix them and call putDraftModel again. " +
					"You never choose a slot or a kind; the ambient design job fixes them. When it succeeds, finish with publishDraft.",
			}, textHandler(func(_ context.Context, in putDraftModelInput) (string, error) {
				modelJSON, err := marshalInputModel(in.Model)
				if err != nil {
					return "", err
				}
				if err := s.putDraftModel(modelJSON); err != nil {
					return "", err
				}
				return "The draft passed the server codec and the Method CI rules and was written. Respond to any open review comments, then call publishDraft.", nil
			}))
		}},
		{name: "respondToReviewComment", modes: []string{jobModeDraft, jobModeAnswer}, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "respondToReviewComment",
				Description: "Record your response to one OPEN review-ledger comment (matched by id from getReviewThread). In answer mode this ANSWERS a question in place; " +
					"in draft mode you respond after revising the draft to address a change-request. A change-request left without a response stays open and blocks approval; " +
					"an answered question is marked addressed.",
			}, textHandler(func(_ context.Context, in respondToReviewCommentInput) (string, error) {
				if err := s.respondToReviewComment(in.ID, in.Response); err != nil {
					return "", err
				}
				return "Recorded your response on the review thread.", nil
			}))
		}},
		{name: "publishDraft", modes: all, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "publishDraft",
				Description: "Stage, commit, and push your changes to the project state onto this design job's session branch — the LAST thing you do. " +
					"It is exactly-once (a second call is a no-op) and it refuses to publish when you have recorded nothing this session.",
			}, textHandler(func(_ context.Context, in publishDraftInput) (string, error) { return s.publishDraft(in.Message) }))
		}},
	}
}

// rawToolInput is the permissive input type for a registered raw internal tool: the
// explicit generated InputSchema is authoritative, so the handler takes the args map.
type rawToolInput map[string]any

// registerRawTool registers a raw generated internal RA/Engine tool from its catalog
// descriptor: the generated JSON Schemas (self-contained) and the readOnlyHint. Its
// handler is a BOOTSTRAP stub — the internal surface EXISTS and is scopable now; the
// construction rail (the next priority) wires the executing handler. This keeps the
// design job honest: a raw tool only ever appears when a palette explicitly names it.
func registerRawTool(srv *mcp.Server, t projectstate.InternalTool) {
	tool := &mcp.Tool{
		Name:        t.Name,
		Description: t.Description + " NOTE: catalog-registered from the generated internal surface; not executable in a design job (the construction rail wires the handler).",
	}
	if in := parseSchema(t.InputSchema); in != nil {
		tool.InputSchema = in
	}
	// OutputSchema is intentionally left unset: the SDK requires an object-typed
	// output schema, but a raw result is often a bare $ref/scalar, and this bootstrap
	// stub returns no structured output. The generated catalog still carries the full
	// OutputSchema for documentation + the construction rail's executing handler.
	if t.ReadOnly {
		tool.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: true}
	}
	mcp.AddTool(srv, tool, func(context.Context, *mcp.CallToolRequest, rawToolInput) (*mcp.CallToolResult, any, error) {
		return nil, nil, fmt.Errorf("raw internal tool %q (%s.%s) is catalog-registered but not executable in this design job; it becomes executable when the construction rail wires the handler", t.Name, t.Component, t.Operation)
	})
}

// parseSchema decodes a raw JSON Schema node into the MCP SDK's schema type, or nil
// on an unparseable/empty node (the SDK then infers from the Go input type).
func parseSchema(raw json.RawMessage) *jsonschema.Schema {
	if len(raw) == 0 {
		return nil
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	return &s
}

// warnf writes a WARN line to STDERR. stdout is the MCP stdio transport, so diagnostics
// must never go there.
func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "aiarch-state-mcp: WARN "+format+"\n", args...)
}

func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// textHandler adapts a plain (input)->(text, error) function into the SDK's typed tool
// handler. A returned error surfaces to the agent as an IsError tool result whose text is
// the error message (so validation failures are self-correctable), and a success returns
// the text as tool content.
func textHandler[In any](fn func(context.Context, In) (string, error)) mcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		out, err := fn(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}}, nil, nil
	}
}
