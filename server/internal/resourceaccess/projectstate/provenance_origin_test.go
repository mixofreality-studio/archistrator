package projectstate

import (
	"bytes"
	"encoding/json"
	"testing"
)

// The whole design: a dropped or absent stamp must fail SUSPICIOUS, never blessed.
func TestRecordOrigin_ZeroValueIsSynthesized(t *testing.T) {
	var zero RecordOrigin
	if zero != OriginSynthesized {
		t.Fatalf("zero RecordOrigin = %q, want %q — a missing stamp must never read as observed", zero, OriginSynthesized)
	}
}

func TestAttemptProvenance_ZeroStructIsSynthesized(t *testing.T) {
	var p AttemptProvenance
	if p.Origin != OriginSynthesized {
		t.Errorf("zero AttemptProvenance.Origin = %q, want %q", p.Origin, OriginSynthesized)
	}
}

func TestAttemptProvenance_DecodingAbsentOriginIsSynthesized(t *testing.T) {
	var p AttemptProvenance
	if err := json.Unmarshal([]byte(`{"generator":"x"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Origin != OriginSynthesized {
		t.Errorf("absent origin decoded to %q, want %q", p.Origin, OriginSynthesized)
	}
}

func TestAttemptProvenance_EncodingEmitsOriginKey(t *testing.T) {
	// Finding 1: No omitempty on Origin means zero value MUST be emitted on the wire.
	// If someone added omitempty, synthesized origins would be silently dropped.
	p := AttemptProvenance{} // zero value: Origin = OriginSynthesized
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The "origin" key must be present in the JSON, even though its value is empty string.
	if !bytes.Contains(data, []byte(`"origin":`)) {
		t.Errorf("marshaled JSON = %s, want to contain \"origin\" key", data)
	}
}

func TestAttemptProvenance_ValidateRejectsUnknownOrigin(t *testing.T) {
	p := AttemptProvenance{Origin: RecordOrigin("observed-ish")}
	if err := p.Validate(); err == nil {
		t.Error("Validate() accepted an unknown origin, want error")
	}
}

func TestAttemptProvenance_ValidateAcceptsKnownOrigins(t *testing.T) {
	// Finding 3: Positive cases for Validate().
	cases := []struct {
		name string
		p    AttemptProvenance
	}{
		{"zero value (synthesized)", AttemptProvenance{}},
		{"observed", AttemptProvenance{Origin: OriginObserved}},
		{"backfilled with basis", AttemptProvenance{Origin: OriginBackfilled, Basis: "serviceContracts[artifactAccess]"}},
	}
	for _, c := range cases {
		if err := c.p.Validate(); err != nil {
			t.Errorf("%s: Validate() = %v, want nil", c.name, err)
		}
	}
}

func TestAttemptProvenance_ValidateRequiresBasisForBackfilled(t *testing.T) {
	p := AttemptProvenance{Origin: OriginBackfilled}
	if err := p.Validate(); err == nil {
		t.Error("Validate() accepted backfilled with no basis, want error")
	}
	p.Basis = "serviceContracts[artifactAccess]"
	if err := p.Validate(); err != nil {
		t.Errorf("Validate() rejected backfilled with a basis: %v", err)
	}
}

// Contagion: a value derived from any synthesized input is itself synthesized.
func TestWorstOrigin_Contagion(t *testing.T) {
	cases := []struct {
		name string
		in   []RecordOrigin
		want RecordOrigin
	}{
		{"all observed", []RecordOrigin{OriginObserved, OriginObserved}, OriginObserved},
		{"one backfilled", []RecordOrigin{OriginObserved, OriginBackfilled}, OriginBackfilled},
		{"one synthesized wins", []RecordOrigin{OriginObserved, OriginBackfilled, OriginSynthesized}, OriginSynthesized},
		{"empty is observed", nil, OriginObserved},
	}
	for _, c := range cases {
		if got := WorstOrigin(c.in...); got != c.want {
			t.Errorf("%s: WorstOrigin(%v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestWorstOrigin_UnknownOriginRanksAsSynthesized(t *testing.T) {
	// Finding 2: Unknown origins (those not in the closed enum) must rank as badly as synthesized.
	// If someone changed originRank's default branch to rank unknown as trustworthy,
	// this test would catch it.
	got := WorstOrigin(OriginObserved, RecordOrigin("who-knows"))
	// The unknown origin becomes the worst (has rank 0, same as synthesized), so it's returned.
	want := RecordOrigin("who-knows")
	if got != want {
		t.Errorf("WorstOrigin(OriginObserved, \"who-knows\") = %q, want %q", got, want)
	}
	// Verify that the unknown origin is suspicious (fails validation) — the critical property.
	p := AttemptProvenance{Origin: got}
	if err := p.Validate(); err == nil {
		t.Error("unknown origin should fail Validate(), proving it's treated as dangerous")
	}
}
