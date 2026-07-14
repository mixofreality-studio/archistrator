package main

// reads.go holds the READ-ONLY verbs: the drafting agent's basis access. They decode the
// checked-out project.json through the strict server codec and render typed models /
// thread / research as JSON or plain text. None mutate state.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// listResearchSources lists the committed research corpus (title → repo-relative path)
// the drafting agent draws on for the Mission. An empty corpus is reported plainly.
func (s *Session) listResearchSources() (string, error) {
	proj, _, err := s.readProject()
	if err != nil {
		return "", err
	}
	if proj.Research.IsZero() {
		return "No research sources are committed for this project.", nil
	}
	var b strings.Builder
	b.WriteString("Research sources (read the full text with getResearchSource(path)):\n")
	for _, src := range proj.Research.Sources {
		fmt.Fprintf(&b, "- %s → %s\n", src.Title, src.Path)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// getResearchSource returns the full text of one research source file, addressed by the
// repo-relative path listResearchSources reports. The path is confined to the checkout
// (no traversal, no absolute escape) so the tool cannot read outside the project repo.
func (s *Session) getResearchSource(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("getResearchSource requires the repo-relative path of a research source (see listResearchSources)")
	}
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the project repository", path)
	}
	full := filepath.Join(s.StateRoot, clean)
	// The path is confined to the checkout — traversal/absolute escape is rejected above
	// (gosec G304 excluded for this tool in .golangci.yml).
	b, err := os.ReadFile(full)

	if err != nil {
		return "", fmt.Errorf("read research source %q: %w", path, err)
	}
	return string(b), nil
}

// getCommittedSlot returns the committed typed model for ANY artifact kind — the
// drafting agent's read-only basis access to its predecessors (e.g. the System draft
// reads the committed CoreUseCases). It reports plainly when the kind is not yet
// committed so the agent knows a prerequisite is missing rather than guessing.
func (s *Session) getCommittedSlot(kindStr string) (string, error) {
	kind, err := parseArtifactKind(strings.TrimSpace(kindStr))
	if err != nil {
		return "", err
	}
	proj, _, err := s.readProject()
	if err != nil {
		return "", err
	}
	slot, ok := slotFor(&proj, kind)
	if !ok {
		return "", fmt.Errorf("no slot for artifact kind %s", kind)
	}
	if slot.Status != projectstate.ReviewCommitted || slot.Model == nil {
		return fmt.Sprintf("The %s artifact is not committed yet — no basis model to read.", kind.WireName()), nil
	}
	return marshalModel(slot.Model)
}

// getDraftSlot returns the current draft model for the AMBIENT kind (whatever status), or
// a plain message when nothing is drafted yet — the amendment / redraft baseline.
func (s *Session) getDraftSlot() (string, error) {
	proj, _, err := s.readProject()
	if err != nil {
		return "", err
	}
	slot, ok := slotFor(&proj, s.Kind)
	if !ok {
		return "", fmt.Errorf("no slot for artifact kind %s", s.Kind)
	}
	if slot.Model == nil {
		return fmt.Sprintf("No %s draft exists yet on this branch — you are drafting it from scratch.", s.Kind.WireName()), nil
	}
	return marshalModel(slot.Model)
}

// getCritique returns the ambient slot's PM-critique verdict + notes — the read-back
// carrier setCritiqueVerdict writes (state.go:170-204). On a redraft after a "revise"
// verdict, this is the only place the guidance is reachable under thin dispatch.
func (s *Session) getCritique() (string, error) {
	proj, _, err := s.readProject()
	if err != nil {
		return "", err
	}
	slot, ok := slotFor(&proj, s.Kind)
	if !ok {
		return "", fmt.Errorf("no slot for artifact kind %s", s.Kind)
	}
	if slot.CritiqueVerdict == "" {
		return "No critique has been recorded on this slot.", nil
	}
	b, err := json.MarshalIndent(struct {
		Verdict string `json:"verdict"`
		Notes   string `json:"notes"`
	}{Verdict: slot.CritiqueVerdict, Notes: slot.CritiqueNotes}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// getReviewThread returns the ambient slot's durable review ledger as JSON (each entry's
// id / anchor / text / status / response). Empty when no reviewer comments exist. The
// drafting agent MUST respond to every OPEN entry via respondToReviewComment.
func (s *Session) getReviewThread() (string, error) {
	proj, _, err := s.readProject()
	if err != nil {
		return "", err
	}
	slot, ok := slotFor(&proj, s.Kind)
	if !ok {
		return "", fmt.Errorf("no slot for artifact kind %s", s.Kind)
	}
	if len(slot.ReviewThread) == 0 {
		return "The review thread is empty — no reviewer comments to address.", nil
	}
	b, err := json.MarshalIndent(slot.ReviewThread, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
