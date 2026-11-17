package aauth

import (
	"strings"
	"testing"
)

// protocol, Interaction Code Format.
func TestInteractionCodeGeneration(t *testing.T) {
	seen := map[string]bool{}
	counts := map[byte]int{}
	for i := 0; i < 2000; i++ {
		code, err := NewInteractionCode(MinInteractionCodeSymbols)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 8 {
			t.Fatalf("length %d", len(code))
		}
		for j := 0; j < len(code); j++ {
			if !strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", rune(code[j])) {
				t.Fatalf("symbol %q outside the alphabet", code[j])
			}
			counts[code[j]]++
		}
		if seen[code] {
			t.Fatalf("repeat after %d codes: %s", i, code)
		}
		seen[code] = true
	}
	// Every symbol turns up: the drawing covers the alphabet.
	if len(counts) != 32 {
		t.Errorf("symbols seen %d", len(counts))
	}
	if _, err := NewInteractionCode(7); err == nil {
		t.Error("fewer than 40 bits accepted")
	}
	long, err := NewInteractionCode(16)
	if err != nil || len(long) != 16 {
		t.Errorf("%q %v", long, err)
	}
}

func TestInteractionCodeFormat(t *testing.T) {
	if got := FormatInteractionCode("A1B2C3D4"); got != "A1B2-C3D4" {
		t.Errorf("got %s", got)
	}
	if got := FormatInteractionCode("A1B2-C3D4"); got != "A1B2-C3D4" {
		t.Errorf("idempotent: %s", got)
	}
	if got := FormatInteractionCode("ABCDEFGHJK"); got != "ABCD-EFGH-JK" {
		t.Errorf("got %s", got)
	}
}

func TestInteractionCodeNormalization(t *testing.T) {
	for in, want := range map[string]string{
		"A1B2-C3D4":   "A1B2C3D4",
		"a1b2-c3d4":   "A1B2C3D4",
		"A1B2C3D4":    "A1B2C3D4",
		"A-1-B-2C3D4": "A1B2C3D4",
		"X7K2-M9P4":   "X7K2M9P4",
		"IABCDEFG":    "1ABCDEFG", // I folds to 1
		"iABCDEFG":    "1ABCDEFG",
		"LABCDEFG":    "1ABCDEFG", // L folds to 1
		"lABCDEFG":    "1ABCDEFG",
		"OABCDEFG":    "0ABCDEFG", // O folds to 0
		"oABCDEFG":    "0ABCDEFG",
	} {
		got, err := NormalizeInteractionCode(in)
		if err != nil || got != want {
			t.Errorf("%s: %s %v, want %s", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ABC", "ABCDEFGU", "ABCD EFGH", "ABCD_EFGH", "ABCDEFG!", "ÄBCDEFGH", "--------"} {
		if _, err := NormalizeInteractionCode(bad); err == nil {
			t.Errorf("%q normalized", bad)
		}
	}
	if !InteractionCodesEqual("A1B2-C3D4", "a1b2c3d4") {
		t.Error("hyphen and case")
	}
	if !InteractionCodesEqual("I0L1-OABC", "10110ABC") {
		t.Error("aliases")
	}
	if InteractionCodesEqual("A1B2-C3D4", "A1B2-C3D5") {
		t.Error("different codes")
	}
	if InteractionCodesEqual("A1B2-C3D4", "A1B2-C3D4A") {
		t.Error("different lengths")
	}
	if InteractionCodesEqual("", "") || InteractionCodesEqual("bad!", "bad!") {
		t.Error("invalid input compared equal")
	}
}

func TestDirectedSub(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	a, err := DirectedSub(key, "person-1", "https://resource.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 {
		t.Errorf("length %d: %s", len(a), a)
	}
	again, _ := DirectedSub(key, "person-1", "https://resource.example")
	if a != again {
		t.Error("not stable")
	}
	// A pure function with a pinned output, so a change to the derivation is caught: it would change
	// every person's identifier at every resource.
	if a != "1dde638643609c87f24c58c2d9075bc7" {
		t.Errorf("derivation changed: %s", a)
	}
	for name, other := range map[string][3]string{
		"another audience": {string(key), "person-1", "https://other.example"},
		"another person":   {string(key), "person-2", "https://resource.example"},
		"another key":      {strings.Repeat("j", 32), "person-1", "https://resource.example"},
	} {
		got, err := DirectedSub([]byte(other[0]), other[1], other[2])
		if err != nil {
			t.Fatal(err)
		}
		if got == a {
			t.Errorf("%s collided", name)
		}
	}
	// Unambiguous encoding: moving a boundary between the two fields changes the output.
	x, _ := DirectedSub(key, "ab", "c")
	y, _ := DirectedSub(key, "a", "bc")
	if x == y {
		t.Error("field boundary is ambiguous")
	}
	if _, err := DirectedSub([]byte("short"), "p", "a"); err == nil {
		t.Error("short key accepted")
	}
	if _, err := DirectedSub(key, "", "a"); err == nil {
		t.Error("empty person accepted")
	}
	if _, err := DirectedSub(key, "p", ""); err == nil {
		t.Error("empty audience accepted")
	}
}
