package main

// git.go holds publishDraft — the ONLY verb that touches git. It stages the managed
// state subtree (.aiarch/state), commits it, and pushes the session branch, with
// exactly-once + no-empty-publish semantics that close the F17c "job went green having
// committed nothing" failure mode.

import (
	"fmt"
	"os/exec"
	"strings"
)

// runGit executes git in the given repo root and returns trimmed combined output. It is
// the production git runner injected into Session.git (tests inject a fake).
func runGit(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// publishDraft stages .aiarch/state, commits it, and pushes the session branch — the
// single persistence point that replaces the agent's hand-run git commit/push. Its
// contract:
//
//   - EXACTLY ONCE: a second call after a successful publish is a clear no-op (never a
//     duplicate commit) so a retrying agent cannot double-commit.
//   - NO EMPTY PUBLISH: it refuses when NO state-mutating verb ran in this process AND
//     the working tree has no pending change under .aiarch/state — turning "the agent
//     drafted nothing" into a hard error the agent sees, not a silent green job (F17c).
//
// The push targets the ambient target branch (the session branch the workflow refreshed
// and checked out); the commit uses the run's git identity (configured by the workflow's
// refresh step) with a defensive fallback so a missing identity never wedges the publish.
func (s *Session) publishDraft(message string) (string, error) {
	if s.published {
		return "Already published in this session — no changes were committed a second time (publishDraft is exactly-once).", nil
	}

	// No-empty-publish guard: nothing drafted this process AND a clean state subtree.
	if !s.wroteState {
		dirty, derr := s.stateSubtreeDirty()
		if derr != nil {
			return "", derr
		}
		if !dirty {
			if s.Mode == jobModeCritique {
				return "", fmt.Errorf("refusing to publish: no critique verdict was recorded this session (call setCritiqueVerdict first) and the working tree has no pending state change")
			}
			return "", fmt.Errorf("refusing to publish: no draft was recorded this session (call putDraftModel first) and the working tree has no pending state change")
		}
	}

	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = fmt.Sprintf("aiarch design: %s (%s)", s.Kind.WireName(), s.Mode)
	}

	if _, err := s.git(s.StateRoot, "add", "--", statePathPrefix); err != nil {
		return "", err
	}

	// Nothing actually staged (e.g. the drafted content equals what is already committed):
	// treat as a clean no-op rather than failing the commit with "nothing to commit".
	if staged, err := s.git(s.StateRoot, "status", "--porcelain", "--", statePathPrefix); err == nil && strings.TrimSpace(staged) == "" {
		s.published = true
		return "No net change to the committed state — nothing to publish.", nil
	}

	if _, err := s.git(s.StateRoot,
		"-c", "user.name=aiarch-state-mcp",
		"-c", "user.email=aiarch-state-mcp@users.noreply.github.com",
		"commit", "-m", msg, "--", statePathPrefix,
	); err != nil {
		return "", err
	}

	if s.TargetBranch != "" {
		if _, err := s.git(s.StateRoot, "push", "origin", "HEAD:"+s.TargetBranch); err != nil {
			return "", err
		}
	}

	s.published = true
	branch := s.TargetBranch
	if branch == "" {
		branch = "(local; no target branch configured)"
	}
	return fmt.Sprintf("Published the %s %s onto %s.", s.Kind.WireName(), s.Mode, branch), nil
}

// stateSubtreeDirty reports whether the working tree has any pending change under
// .aiarch/state (staged or unstaged). Used by the no-empty-publish guard.
func (s *Session) stateSubtreeDirty() (bool, error) {
	out, err := s.git(s.StateRoot, "status", "--porcelain", "--", statePathPrefix)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}
