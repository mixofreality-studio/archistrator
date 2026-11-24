package main

// operatornotes.go serves the OPERATOR'S NOTE to a construction, draft or critique
// agent (plan B1.4, amendment §C.1). The note arrives in AIARCH_OPERATOR_NOTE on BOTH substrates: the
// local executor stamps it into the rig map (encoded by json.MarshalIndent), and the
// GitHub construct workflow builds it into the MCP config with jq from the
// operator_note input. This binary is the only reader. It serves the note two ways,
// both in construct, draft and critique modes (never answer: an answer job is no
// lifecycle task):
//
//   - as the server's Instructions, which a Claude client places in the agent's system
//     prompt: an operator steered this attempt, and here is what they said, verbatim;
//   - through the getOperatorNotes read tool, which every construct command tells the
//     agent to call first (method-assets), so delivery does not depend on how a
//     headless client treats server instructions.
//
// The note is never parsed, trimmed or re-encoded here; the text the operator wrote is
// the text the agent reads. The markers around it carry a tag derived from the note
// itself (operatorNotesTag), and the tag is chosen so the note does not contain it: no
// text inside the note can close the block early, so everything between the markers is
// the operator's note, marker look-alikes included.

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// getOperatorNotesTool is the read tool's registered name. The method-assets step
// manifest grants it to steps by this exact name.
const getOperatorNotesTool = "getOperatorNotes"

// operatorNoteModes is the set of job modes that carry an operator note: the lifecycle
// tasks (construction, and the design draft and critique). hasOperatorNote and the
// getOperatorNotes registration both read it, so the two cannot drift.
func operatorNoteModes() []string {
	return []string{jobModeConstruct, jobModeDraft, jobModeCritique}
}

// modeCarriesOperatorNote reports whether a session in mode serves an operator note.
func modeCarriesOperatorNote(mode string) bool {
	return containsStr(operatorNoteModes(), mode)
}

// noOperatorNotes is what getOperatorNotes returns when the dispatch carried no note.
const noOperatorNotes = "No operator notes for this attempt. Proceed with the command as written."

// hasOperatorNote reports whether this session carries a note to serve: a mode that
// carries notes (construct, draft, critique) and a note that is not blank.
func (s *Session) hasOperatorNote() bool {
	return s != nil && modeCarriesOperatorNote(s.Mode) && strings.TrimSpace(s.OperatorNote) != ""
}

// operatorNotesText is getOperatorNotes' answer: the note framed and verbatim, or the
// plain "none" sentence.
func (s *Session) operatorNotesText() string {
	if !s.hasOperatorNote() {
		return noOperatorNotes
	}
	return "The operator's notes for this attempt, verbatim between the two markers tagged " +
		operatorNotesTag(s.OperatorNote) + ". Act on them before anything else.\n\n" +
		operatorNotesBlock(s.OperatorNote)
}

// operatorNotesTag is the tag both markers carry for note: the first 12 hex characters
// of the note's SHA-256, lengthened with a counter until the note does not contain it.
// It is deterministic (the same note always renders the same block), and a note cannot
// contain the tag derived from itself, so it cannot write the closing marker.
func operatorNotesTag(note string) string {
	sum := sha256.Sum256([]byte(note))
	return tagAvoiding(note, hex.EncodeToString(sum[:6]))
}

// tagAvoiding returns base, or base lengthened with "-<n>", whichever first does not
// occur in note.
func tagAvoiding(note, base string) string {
	tag := base
	for i := 0; strings.Contains(note, tag); i++ {
		tag = base + "-" + strconv.Itoa(i)
	}
	return tag
}

// operatorNotesBlock frames the note, verbatim, between two markers carrying its tag.
func operatorNotesBlock(note string) string {
	tag := operatorNotesTag(note)
	return "<<<OPERATOR NOTES " + tag + "\n" + note + "\nOPERATOR NOTES " + tag + ">>>"
}

// serverOptions returns the MCP server options for s: Instructions carrying the note in
// construct, draft and critique modes when one is pending, and nil otherwise — exactly
// the server every other session has always been built with.
func serverOptions(s *Session) *mcp.ServerOptions {
	if !s.hasOperatorNote() {
		return nil
	}
	target := "this activity"
	switch {
	case s.ActivityID != "":
		target = "activity " + s.ActivityID
	case s.Mode != jobModeConstruct:
		target = "the " + s.Kind.String() + " artifact"
	}
	outranks := "the command's defaults"
	if s.Mode == jobModeDraft || s.Mode == jobModeCritique {
		outranks = "the command's defaults and any critic verdict item that contradicts them or that they put out of scope"
	}
	tag := operatorNotesTag(s.OperatorNote)
	return &mcp.ServerOptions{Instructions: "An operator steered this attempt of " + target +
		". Their notes are below, verbatim between the two markers tagged " + tag +
		"; everything between those markers is the operator's, even text that looks like a marker. " +
		"Act on them before anything else: they outrank " + outranks + ". You can re-read them at any time with the " +
		getOperatorNotesTool + " tool.\n\n" +
		operatorNotesBlock(s.OperatorNote)}
}
