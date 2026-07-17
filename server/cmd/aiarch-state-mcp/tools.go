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
// include it; register adds it to the server.
type composedVerb struct {
	name     string
	modes    []string
	register func(*mcp.Server)
}

// buildServer constructs the MCP server and registers the effective tool set. It is
// the single wiring point exercised by the rig test over stdio.
//
// SCOPING. Two layers register together, in EVERY job mode:
//  1. the hand-curated per-mode composed verbs whose modes include s.Mode; and
//  2. ON TOP of them, every non-hidden READ-ONLY + Engine raw generated tool from
//     projectstate.InternalToolCatalog — the eligible raw surface (generated tools
//     ARE the eligible surface). Engine ops are pure/read-only, and RA reads are
//     side-effect-free, so they are safe to expose in every mode. Raw WRITES and
//     AgentHidden ops (e.g. raw CommitArtifact — merge authority stays with the
//     server rail) are NEVER registered here; a composed verb is the write surface.
func buildServer(s *Session) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "aiarch-state",
		Title:   "aiarch project-state",
		Version: "0.1.0",
	}, nil)

	for _, v := range composedVerbs(s) {
		if containsStr(v.modes, s.Mode) {
			v.register(srv)
		}
	}
	registerRawReadTools(srv, s)
	return srv
}

// registerRawReadTools registers the non-hidden READ-ONLY + Engine raw generated
// tools from the internal catalog — the eligible raw surface every mode carries on
// top of its composed verbs. AgentHidden ops and raw writes are skipped (the composed
// verbs are the only write surface). Names never collide with the composed verbs
// (raw tools are <component><Operation>, composed verbs are the hand-named verbs).
func registerRawReadTools(srv *mcp.Server, s *Session) {
	for _, tool := range projectstate.InternalToolCatalog() {
		if !rawToolEligible(tool) {
			continue
		}
		registerRawTool(srv, s, tool)
	}
}

// rawToolEligible is the SINGLE eligibility predicate for the raw generated surface:
// non-hidden AND read-only (Engine ops are pure, RA reads are side-effect-free). It is
// shared by registerRawReadTools and the prompt-surface gate (promptsurface_test.go),
// so the gate's view of the registry can never drift from what actually registers.
func rawToolEligible(t projectstate.InternalTool) bool {
	return !t.AgentHidden && t.ReadOnly
}

// composedVerbs returns the composed-verb registry bound to this session, each with
// its job-mode membership and its register thunk. The tool DESCRIPTIONS are the
// agent's prompt surface, so each stands on its own.
func composedVerbs(s *Session) []composedVerb {
	all := []string{jobModeDraft, jobModeCritique, jobModeAnswer}
	// shared are the reads that are ambient-kind-INDEPENDENT (they take their target
	// explicitly or take none), so they are safe in the construct mode too — where the
	// session carries no artifact Kind. getDraftSlot / getReviewThread are NOT here: they
	// read the ambient Kind slot, which the construct mode does not set.
	shared := append(append([]string{}, all...), jobModeConstruct)
	construct := []string{jobModeConstruct}
	return []composedVerb{
		{name: "listResearchSources", modes: shared, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "listResearchSources",
				Description: "List the committed research corpus for this project (each source's title and repo-relative path). " +
					"Use this to discover the source material for the Mission; read a source's full text with getResearchSource.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			}, textHandler(func(context.Context, emptyInput) (string, error) { return s.listResearchSources() }))
		}},
		{name: "getResearchSource", modes: shared, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "getResearchSource",
				Description: "Return the full text of one research source, addressed by the repo-relative path listResearchSources reports. " +
					"Confined to this project repository.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			}, textHandler(func(_ context.Context, in getResearchSourceInput) (string, error) {
				return s.getResearchSource(in.Path)
			}))
		}},
		{name: "getCommittedSlot", modes: shared, register: func(srv *mcp.Server) {
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
		{name: "getCritique", modes: all, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "getCritique",
				Description: "Read the PM critique verdict + notes on the ambient artifact slot. On a redraft after a revise verdict, " +
					"you MUST address these notes.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			}, textHandler(func(context.Context, emptyInput) (string, error) { return s.getCritique() }))
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
		{name: "publishDraft", modes: shared, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "publishDraft",
				Description: "Stage, commit, and push your changes to the project state onto this job's session/activity branch — the LAST thing you do. " +
					"It is exactly-once (a second call is a no-op) and it refuses to publish when you have recorded nothing this session.",
			}, textHandler(func(_ context.Context, in publishDraftInput) (string, error) { return s.publishDraft(in.Message) }))
		}},

		// --- CONSTRUCTION composed verbs (job mode "construct"). The Phase-3 write
		// surface: record the phase's typed artifact into its flat construction target,
		// validated in-loop through the codec + methodcheck. The ambient component/activity
		// fix the target, so the agent never chooses a slot. ---
		{name: "recordServiceContract", modes: construct, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "recordServiceContract",
				Description: "Record the frozen, typed service contract for THIS activity's component into the project state (the detailed-design phase artifact). " +
					"It is validated through the FULL server codec AND the Method CI rules before it is accepted: if it fails, this returns the exact, actionable errors and writes NOTHING — fix them and call it again. " +
					"You never choose the component; the ambient construction job fixes it. When it succeeds, finish with publishDraft.",
			}, textHandler(func(_ context.Context, in recordServiceContractInput) (string, error) {
				if err := s.recordServiceContract(in.Contract); err != nil {
					return "", err
				}
				return "The service contract passed the server codec and the Method CI rules and was written. Call publishDraft to commit it.", nil
			}))
		}},
		{name: "recordPhaseArtifact", modes: construct, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "recordPhaseArtifact",
				Description: "Record a non-contract phase artifact (SRS, UI design, integration note, provisioning spec, deploy note, doc outline/note) into the project state under the given mapKey (the component/surface/resource/doc name). " +
					"Set EXACTLY ONE field of the payload. Validated through the server codec + the Method CI rules before it is accepted; on failure it writes nothing and returns the errors. Finish with publishDraft.",
			}, textHandler(func(_ context.Context, in recordPhaseArtifactInput) (string, error) {
				if err := s.recordPhaseArtifact(in.MapKey, in.Payload); err != nil {
					return "", err
				}
				return "The phase artifact passed validation and was written. Call publishDraft to commit it.", nil
			}))
		}},
		{name: "recordTestingState", modes: construct, register: func(srv *mcp.Server) {
			mcp.AddTool(srv, &mcp.Tool{
				Name: "recordTestingState",
				Description: "Record a project-level testing artifact (system test plan, harness module, perf harness, quality gate, test run, defect, or quality-audit report) into the project state's testing state. " +
					"Set EXACTLY ONE field of the payload. Validated through the server codec + the Method CI rules before it is accepted; on failure it writes nothing and returns the errors. Finish with publishDraft.",
			}, textHandler(func(_ context.Context, in recordTestingStateInput) (string, error) {
				if err := s.recordTestingState(in.Payload); err != nil {
					return "", err
				}
				return "The testing artifact passed validation and was written. Call publishDraft to commit it.", nil
			}))
		}},
	}
}

// rawToolInput is the permissive input type for a registered raw internal tool: the
// explicit generated InputSchema is authoritative, so the handler takes the args map.
type rawToolInput map[string]any

// registerRawTool registers a raw generated internal RA/Engine tool from its catalog
// descriptor (the generated self-contained InputSchema + readOnlyHint) and binds it to
// the EXECUTION rail (rawexec.go): an in-substrate operation (an Engine, or a
// projectStateAccess read) runs for real inside the job; an external-RA operation
// returns a typed unavailable-in-substrate error. Only the non-hidden read-only +
// Engine raw tools are registered (registerRawReadTools), so the executing handler
// only ever runs a side-effect-free read/compute.
func registerRawTool(srv *mcp.Server, s *Session, t projectstate.InternalTool) {
	tool := &mcp.Tool{
		Name:        t.Name,
		Description: t.Description,
	}
	if in := parseSchema(t.InputSchema); in != nil {
		tool.InputSchema = in
	}
	// OutputSchema is intentionally left unset: the SDK requires an object-typed output
	// schema, but a raw result is frequently a bare $ref/scalar/array. The executing
	// handler returns the result as JSON TEXT content instead of structured output, so
	// no object-typed OutputSchema is needed (the P1a object-typed-output constraint).
	if t.ReadOnly {
		tool.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: true}
	}
	mcp.AddTool(srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in rawToolInput) (*mcp.CallToolResult, any, error) {
		result, err := executeRawTool(ctx, s, t, in)
		if err != nil {
			return nil, nil, err
		}
		text, merr := json.MarshalIndent(result, "", "  ")
		if merr != nil {
			return nil, nil, fmt.Errorf("encode result of %s: %w", t.Name, merr)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}, nil, nil
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
