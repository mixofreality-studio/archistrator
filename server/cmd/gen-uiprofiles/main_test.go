package main

import (
	"strings"
	"testing"
)

// Prettier measures a line in characters. Profile copy carries "—" (three bytes in
// UTF-8), so a byte count breaks lines prettier keeps whole and the generated file
// stops being prettier-stable (fix-B review M5).
func TestWriteProp_MeasuresCharactersNotBytes(t *testing.T) {
	// 4 indent + "k" + ": " + quoted literal (2 + 80 + 10) + "," = exactly 100
	// characters, but 120 bytes: ten em dashes at three bytes each.
	literal := tsString(strings.Repeat("a", 80) + strings.Repeat("—", 10))
	var b strings.Builder
	writeProp(&b, "    ", "k", literal)
	want := "    k: " + literal + ",\n"
	if b.String() != want {
		t.Errorf("a 100-character line was broken:\n%q\nwant\n%q", b.String(), want)
	}

	// One character more is over the width, and breaks the way prettier does.
	longer := tsString(strings.Repeat("a", 81) + strings.Repeat("—", 10))
	b.Reset()
	writeProp(&b, "    ", "k", longer)
	wantBroken := "    k:\n      " + longer + ",\n"
	if b.String() != wantBroken {
		t.Errorf("a 101-character line was not broken:\n%q\nwant\n%q", b.String(), wantBroken)
	}
}
