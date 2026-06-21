package harness

import (
	"crypto/rand"
	"encoding/hex"
)

// NewProjectID returns a random RFC-4122 v4 UUID string. Zero-dependency (no
// google/uuid) so the harness import graph stays stdlib-only — the smaller the
// graph, the smaller the cheating surface (see go.mod / the constitution test).
// The server parses projectId as a UUID, so the byte layout must be exact.
func NewProjectID() string {
	var b [16]byte
	// crypto/rand.Read never returns a short read / error in practice; the
	// returned values are ignored deliberately.
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx

	var dst [36]byte
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst[:])
}

// ShortID returns a short random hex token (12 chars) for building unique, repo-name-
// safe per-run project names. The systemtests Temporal dev server PERSISTS workflow
// history across runs, and a co-author WorkflowID is {projectID}:{kind} — so a fixed
// project name collides with a stale workflow from a prior run still retrying against a
// now-closed fake. A random suffix isolates each run's WorkflowIDs.
func ShortID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
