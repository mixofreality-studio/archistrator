package main

// operatornotes.go serves the OPERATOR'S NOTE to a construction agent (plan B1.4,
// amendment §C.1). The note arrives in AIARCH_OPERATOR_NOTE on BOTH substrates: the
// local executor stamps it into the rig map (encoded by json.MarshalIndent), and the
// GitHub construct workflow builds it into the MCP config with jq from the
// operator_note input. This binary is the only reader. It serves the note two ways,
// both in construct mode only:
//
//   - as the server's Instructions, which a Claude client places in the agent's system
//     prompt: an operator steered this attempt, and here is what they said, verbatim;
//   - through the get_operator_notes read tool, which every construct command tells
//     the agent to call first (method-assets), so delivery does not depend on how a
//     headless client treats server instructions.
//
// The note is never parsed, trimmed or re-encoded here; the text the operator wrote is
// the text the agent reads. The markers around it are fixed strings, so a note can
// quote them without changing what the agent is told.

import (
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// getOperatorNotesTool is the construct-mode read tool's registered name. The
// method-assets step manifest grants it to every construct step by this exact name.
const getOperatorNotesTool = "get_operator_notes"

// noOperatorNotes is what get_operator_notes returns when the dispatch carried no note.
const noOperatorNotes = "No operator notes for this attempt. Proceed with the command as written."

// hasOperatorNote reports whether this session carries a note to serve: construct mode
// and a note that is not blank.
func (s *Session) hasOperatorNote() bool {
	return s != nil && s.Mode == jobModeConstruct && strings.TrimSpace(s.OperatorNote) != ""
}

// operatorNotesText is get_operator_notes' answer: the note framed and verbatim, or the
// plain "none" sentence.
func (s *Session) operatorNotesText() string {
	if !s.hasOperatorNote() {
		return noOperatorNotes
	}
	return "The operator's notes for this attempt, verbatim between the markers. Act on them before anything else.\n\n" +
		operatorNotesBlock(s.OperatorNote)
}

// operatorNotesBlock frames the note between fixed markers, verbatim.
func operatorNotesBlock(note string) string {
	return "<<<OPERATOR NOTES\n" + note + "\nOPERATOR NOTES>>>"
}

// serverOptions returns the MCP server options for s: Instructions carrying the note in
// construct mode when one is pending, and nil otherwise — exactly the server every other
// session has always been built with.
func serverOptions(s *Session) *mcp.ServerOptions {
	if !s.hasOperatorNote() {
		return nil
	}
	target := "this activity"
	if s.ActivityID != "" {
		target = "activity " + s.ActivityID
	}
	return &mcp.ServerOptions{Instructions: "An operator steered this attempt of " + target +
		". Their notes are below, verbatim between the markers. Act on them before anything else: " +
		"they outrank the command's defaults. You can re-read them at any time with the " + getOperatorNotesTool + " tool.\n\n" +
		operatorNotesBlock(s.OperatorNote)}
}
