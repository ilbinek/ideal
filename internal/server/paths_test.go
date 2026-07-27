package server

import "testing"

func TestEnsureUniqueSlug_Reserved(t *testing.T) {
	exists := func(string) bool { return false }

	got := EnsureUniqueSlug("servers", exists)
	if got != "servers-2" {
		t.Fatalf("EnsureUniqueSlug(servers) = %q, want servers-2", got)
	}

	got = EnsureUniqueSlug("api", exists)
	if got != "api-2" {
		t.Fatalf("EnsureUniqueSlug(api) = %q, want api-2", got)
	}

	got = EnsureUniqueSlug("dl", exists)
	if got != "dl-2" {
		t.Fatalf("EnsureUniqueSlug(dl) = %q, want dl-2", got)
	}

	got = EnsureUniqueSlug("myserver", exists)
	if got != "myserver" {
		t.Fatalf("EnsureUniqueSlug(myserver) = %q, want myserver", got)
	}
}

func TestIsReservedSlug(t *testing.T) {
	if !IsReservedSlug("servers") {
		t.Fatal("expected servers to be reserved")
	}
	if IsReservedSlug("my-opfor") {
		t.Fatal("did not expect my-opfor to be reserved")
	}
}
