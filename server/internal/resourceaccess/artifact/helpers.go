package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// helpers.go holds the INFRASTRUCTURE-PRIVATE address/branch/meta encoding for the
// content-addressable construction-output store. It imports NO git package — git
// vocabulary is confined to the github satellite (gitstore.go delegates to it).
// These helpers only shape the opaque content-address string and the on-store
// path/branch/meta encoding the RA controls.

// metaFile is the sidecar path inside each output's commit carrying the MIMEType
// hint (git stores bytes only — the advisory MIMEType must be persisted to be
// faithfully returned on retrieve).
const metaFile = "meta.json"

// addrSeparator joins the opaque commit-identity token and the content path inside
// the content-address string. A colon separates the hex commit token (never
// contains ':') from the content path: parseAddress splits on the FIRST ':'
// (SplitN, 2), so a path containing ':' still round-trips. A colon — not a NUL — is
// used deliberately: the address is persisted into projectStateAccess head-state
// (git JSON / Postgres JSONB), Temporal payloads, and logs, and must be safe text.
const addrSeparator = ":"

// branchFor derives the deterministic, INFRASTRUCTURE-PRIVATE content-derived
// branch name from a hash of the output's content+MIMEType:
// "aiarch/output/{contentHash}". There is no logical (project, kind, variant)
// addressing — content is the key (artifactAccess.md decision 3). Distinct content
// lands on distinct branches (never contends); identical content lands on one
// branch whose tip the dedup probe collapses to a single address. The branch name
// is never exposed on the contract (artifactAccess.md §6).
func branchFor(content ConstructionOutput) string {
	h := sha256.New()
	h.Write([]byte(content.MIMEType))
	h.Write([]byte{0})
	h.Write(content.Bytes)
	return "aiarch/output/" + hex.EncodeToString(h.Sum(nil))
}

// contentPathFor derives the content file path from the advisory MIMEType. The
// extension is best-effort (the MIMEType is advisory per artifactAccess.md §3); an
// unknown/empty MIMEType yields a bare "output" with no extension. Deterministic so
// the same MIMEType always maps to the same path.
func contentPathFor(mimeType string) string {
	return "output" + extFor(mimeType)
}

// extFor maps a MIMEType to a file extension. Conservative: only a small set of
// construction-relevant types is recognised; everything else falls back to ""
// (no extension). Correctness does not depend on this — retrieve reads the path
// recorded in the address, not a re-derived one.
func extFor(mimeType string) string {
	switch normalizeMIME(mimeType) {
	case "text/markdown":
		return ".md"
	case "text/plain":
		return ".txt"
	case "text/x-go", "text/x-gosrc":
		return ".go"
	case "application/json":
		return ".json"
	case "application/yaml", "text/yaml", "application/x-yaml":
		return ".yaml"
	case "application/octet-stream":
		return ".bin"
	default:
		return ""
	}
}

// normalizeMIME strips any parameters (e.g. "; charset=utf-8") and lowercases.
func normalizeMIME(mimeType string) string {
	m := mimeType
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = m[:i]
	}
	return strings.ToLower(strings.TrimSpace(m))
}

// metaDoc is the on-infrastructure JSON shape of the sidecar meta file. A dedicated
// wire struct keeps the infrastructure encoding decoupled from the port value
// object.
type metaDoc struct {
	MIMEType string `json:"mimeType"`
}

func encodeMeta(mimeType string) []byte {
	b, _ := json.Marshal(metaDoc{MIMEType: mimeType})
	return b
}

func decodeMeta(b []byte) string {
	var d metaDoc
	if err := json.Unmarshal(b, &d); err != nil {
		return ""
	}
	return d.MIMEType
}

// makeAddress builds the opaque content-address string from the satellite's opaque
// commit-identity token and a content path (artifactAccess.md decision 2 — a plain
// string, no wrapper type). Callers never parse the result — only parseAddress does.
func makeAddress(commitToken, contentPath string) string {
	return commitToken + addrSeparator + contentPath
}

// parseAddress splits an opaque content address back into the commit-identity token
// and content path. A malformed address (not produced by makeAddress) is a caller
// pre-condition violation => fwra.ContractMisuse.
func parseAddress(address string) (commitToken, contentPath string, err error) {
	if address == "" {
		return "", "", fwra.New(fwra.ContractMisuse, "artifact: empty content address")
	}
	parts := strings.SplitN(address, addrSeparator, 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fwra.New(fwra.ContractMisuse, "artifact: malformed content address")
	}
	return parts[0], parts[1], nil
}

// commitMessage embeds the caller-supplied idempotencyKey in the commit message
// (artifactAccess.md §6). The key is opaque to this layer.
func commitMessage(key fwra.IdempotencyKey) string {
	return "aiarch: " + string(key)
}
