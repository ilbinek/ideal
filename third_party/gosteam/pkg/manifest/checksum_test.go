package manifest

import "testing"

func TestAdler32Seed0(t *testing.T) {
	// Seed-0 Adler32 of "abc": s1=294, s2=586 => 586<<16 | 294.
	if got := Adler32([]byte("abc")); got != 586<<16|294 {
		t.Fatalf("Adler32(abc) = %#x, want %#x", got, uint32(586<<16|294))
	}
	if got := Adler32(nil); got != 0 {
		t.Fatalf("Adler32(nil) = %#x, want 0", got)
	}
}
