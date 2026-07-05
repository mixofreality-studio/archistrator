package main

// tools.go registers the aiarch-state MCP tool surface, SCOPED BY JOB MODE. These tool
// descriptions ARE the new prompt surface — a fresh agent must be able to author a valid
// Method artifact from them alone, so each is written to stand on its own. Draft mode gets
// the full authoring set (incl. putDraftModel); critique mode gets read verbs +
// setCritiqueVerdict and NEVER putDraftModel (a critic must not rewrite the model).

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// buildServer constructs the MCP server and registers the tool set for the session's job
// mode. It is the single wiring point exercised by the rig test over stdio.
func buildServer(s *Session) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "aiarch-state",
		Title:   "aiarch project-state",
		Version: "0.1.0",
	}, nil)

	// ---- read verbs (both modes) ----

	mcp.AddTool(srv, &mcp.Tool{
		Name: "listResearchSources",
		Description: "List the committed research corpus for this project (each source's title and repo-relative path). " +
			"Use this to discover the source material for the Mission; read a source's full text with getResearchSource.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, textHandler(func(context.Context, emptyInput) (string, error) { return s.listResearchSources() }))

	mcp.AddTool(srv, &mcp.Tool{
		Name: "getResearchSource",
		Description: "Return the full text of one research source, addressed by the repo-relative path listResearchSources reports. " +
			"Confined to this project repository.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, textHandler(func(_ context.Context, in getResearchSourceInput) (string, error) {
		return s.getResearchSource(in.Path)
	}))

	mcp.AddTool(srv, &mcp.Tool{
		Name: "getCommittedSlot",
		Description: "Return the committed typed model for ANY Method artifact kind — your read-only basis access to the predecessors this artifact builds on " +
			"(e.g. when drafting the System, read the committed coreUseCases and volatilities). Reports plainly when the kind is not committed yet.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, textHandler(func(_ context.Context, in getCommittedSlotInput) (string, error) { return s.getCommittedSlot(in.Kind) }))

	mcp.AddTool(srv, &mcp.Tool{
		Name: "getDraftSlot",
		Description: "Return the current draft of THIS design job's artifact (whatever its status) on this branch, or a note that none exists. " +
			"On an amendment or redraft, start from this draft rather than from scratch.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, textHandler(func(context.Context, emptyInput) (string, error) { return s.getDraftSlot() }))

	mcp.AddTool(srv, &mcp.Tool{
		Name: "getReviewThread",
		Description: "Return this artifact's durable review ledger (each reviewer comment's id, anchor, text, status, and your prior response). " +
			"You MUST respond to every OPEN comment before publishing — use respondToReviewComment.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, textHandler(func(context.Context, emptyInput) (string, error) { return s.getReviewThread() }))

	// ---- mode-specific write verbs ----

	if s.Mode == jobModeCritique {
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
	} else {
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

		mcp.AddTool(srv, &mcp.Tool{
			Name: "respondToReviewComment",
			Description: "Record your response to one OPEN review-ledger comment (matched by id from getReviewThread) after you revise the draft to address it. " +
				"A comment you leave without a response stays open and blocks approval.",
		}, textHandler(func(_ context.Context, in respondToReviewCommentInput) (string, error) {
			if err := s.respondToReviewComment(in.ID, in.Response); err != nil {
				return "", err
			}
			return "Recorded your response on the review thread.", nil
		}))
	}

	// ---- publish (both modes) ----

	mcp.AddTool(srv, &mcp.Tool{
		Name: "publishDraft",
		Description: "Stage, commit, and push your changes to the project state onto this design job's session branch — the LAST thing you do. " +
			"It is exactly-once (a second call is a no-op) and it refuses to publish when you have recorded nothing this session.",
	}, textHandler(func(_ context.Context, in publishDraftInput) (string, error) { return s.publishDraft(in.Message) }))

	return srv
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
