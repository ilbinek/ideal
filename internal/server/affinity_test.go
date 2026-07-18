package server

import (
	"errors"
	"testing"
)

func TestNormalizeAffinityExpandsPairs(t *testing.T) {
	topo := CPUTopology{
		LogicalCount: 8,
		Pairs:        [][]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}},
	}
	got, err := NormalizeAffinity([]int{0, 5}, topo)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 1, 4, 5}
	if FormatAffinity(got) != FormatAffinity(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestNormalizeAffinityEmpty(t *testing.T) {
	topo := CPUTopology{LogicalCount: 4, Pairs: [][]int{{0, 1}, {2, 3}}}
	got, err := NormalizeAffinity(nil, topo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestNormalizeAffinityUnknownCPU(t *testing.T) {
	topo := CPUTopology{LogicalCount: 2, Pairs: [][]int{{0, 1}}}
	_, err := NormalizeAffinity([]int{9}, topo)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFormatAffinity(t *testing.T) {
	cpus := ParseAffinity("5,0,1,0")
	if FormatAffinity(cpus) != "0,1,5" {
		t.Fatalf("got %q", FormatAffinity(cpus))
	}
}

func TestAffinityConflictError(t *testing.T) {
	err := &AffinityConflictError{ServerName: "Alpha", CPUs: []int{0, 1}}
	var conflict *AffinityConflictError
	if !errors.As(err, &conflict) {
		t.Fatal("errors.As failed")
	}
}
