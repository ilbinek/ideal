package server

import "testing"

func TestValidateMemoryLimitMB(t *testing.T) {
	if v, err := ValidateMemoryLimitMB(0); err != nil || v != 0 {
		t.Fatalf("0: got %d %v", v, err)
	}
	if _, err := ValidateMemoryLimitMB(128); err == nil {
		t.Fatal("expected error for 128")
	}
	if v, err := ValidateMemoryLimitMB(1024); err != nil || v != 1024 {
		t.Fatalf("1024: got %d %v", v, err)
	}
	if _, err := ValidateMemoryLimitMB(-1); err == nil {
		t.Fatal("expected error for -1")
	}
}
