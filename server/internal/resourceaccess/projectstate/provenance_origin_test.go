package projectstate

import (
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

func TestAttemptProvenance_ValidateRejectsUnknownOrigin(t *testing.T) {
	p := AttemptProvenance{Origin: RecordOrigin("observed-ish")}
	if err := p.Validate(); err == nil {
		t.Error("Validate() accepted an unknown origin, want error")
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
