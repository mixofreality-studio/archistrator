//go:build methoddesign

package internal_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// TestMethodProjectStateIsCanonical holds archistrator's own committed
// `.aiarch/state/project.json` to the codec's canonical form: decoding it and
// encoding it again (no fresh updatedAt stamp, no project id supplied — exactly what
// the construct-mode tools do) must reproduce the committed bytes.
//
// Every writer of the document — the git store, aiarch-state-mcp's verbs and
// record-test-run, the migrations — goes through that one codec, so a member the
// codec does not carry is lost on the next write of ANY of them, and a member it
// spells differently churns. A document that is not canonical therefore holds data
// the system cannot keep (DCT plan 5 r2: record-test-run silently dropped four
// off-schema members). Hand edits must leave the document canonical; a member that
// belongs in it belongs in the schema first.
func TestMethodProjectStateIsCanonical(t *testing.T) {
	path := filepath.Join(findRepoRoot(t), ".aiarch", "state", "project.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	p, ok, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil || !ok {
		t.Fatalf("decode %s: ok=%v err=%v", path, ok, err)
	}
	enc, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if bytes.Equal(bytes.TrimSpace(raw), bytes.TrimSpace(enc)) {
		return
	}
	a, b := bytes.Split(raw, []byte("\n")), bytes.Split(enc, []byte("\n"))
	for i := 0; i < len(a) && i < len(b); i++ {
		if !bytes.Equal(a[i], b[i]) {
			t.Fatalf("%s is not in the codec's canonical form: line %d is\n\t%s\nbut the codec writes\n\t%s\n(re-encode it through projectstate.EncodeProjectJSON; a member the codec drops is not part of the schema)",
				path, i+1, bytes.TrimSpace(a[i]), bytes.TrimSpace(b[i]))
		}
	}
	t.Fatalf("%s is not in the codec's canonical form: %d lines committed, %d lines canonical", path, len(a), len(b))
}
