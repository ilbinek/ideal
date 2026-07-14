package app

import (
	"testing"

	"github.com/tbdevent/gosteam/pkg/vdf"
)

func TestDepotMatchesFilters(t *testing.T) {
	section := map[string]any{
		"config": map[string]any{
			"oslist":   "windows,linux",
			"osarch":   "64",
			"language": "english",
		},
	}
	opts := ResolveOptions{}
	if !depotMatchesFilters(section, "windows", "64", "english", opts) {
		t.Fatal("expected windows/64/english to match")
	}
	if depotMatchesFilters(section, "macos", "64", "english", opts) {
		t.Fatal("expected macos to be filtered out")
	}
	if depotMatchesFilters(section, "windows", "32", "english", opts) {
		t.Fatal("expected 32-bit to be filtered out")
	}
	opts.AllPlatforms = true
	if !depotMatchesFilters(section, "macos", "64", "english", opts) {
		t.Fatal("AllPlatforms should skip oslist filter")
	}
}

func TestManifestGID(t *testing.T) {
	manifests := map[string]any{
		"public": map[string]any{
			"gid": "1234567890123456789",
		},
		"profiling": map[string]any{
			"gid": "999",
		},
	}
	if g := manifestGID(manifests, "public"); g != 1234567890123456789 {
		t.Fatalf("public gid = %d", g)
	}
	if g := manifestGID(manifests, "profiling"); g != 999 {
		t.Fatalf("profiling gid = %d", g)
	}
	if g := manifestGID(manifests, "missing"); g != 0 {
		t.Fatalf("missing gid = %d, want 0", g)
	}
}

func TestFindDepotsRootAndFiltersFromFixture(t *testing.T) {
	src := []byte(`
"appinfo"
{
	"appid"		"233780"
	"common"
	{
		"name"		"Arma 3 Server"
	}
	"depots"
	{
		"branches"
		{
			"public" { "buildid" "1" }
			"profiling" { "buildid" "2" }
		}
		"233781"
		{
			"config"
			{
				"oslist"	"windows"
				"osarch"	"64"
			}
			"manifests"
			{
				"public" { "gid" "111" }
				"profiling" { "gid" "222" }
			}
		}
		"233782"
		{
			"config"
			{
				"oslist"	"linux"
			}
			"manifests"
			{
				"public" { "gid" "333" }
			}
		}
		"233783"
		{
			"config"
			{
				"language"	"german"
			}
			"manifests"
			{
				"public" { "gid" "444" }
			}
		}
		"workshopdepot"		"107411"
	}
}
`)
	tree, err := vdf.ParseText(src)
	if err != nil {
		t.Fatal(err)
	}
	depots := findDepotsRoot(tree)
	if depots == nil {
		t.Fatal("no depots root")
	}
	if name := appDisplayName(tree); name != "Arma 3 Server" {
		t.Fatalf("name = %q", name)
	}

	win := vdf.Child(depots, "233781")
	if !depotMatchesFilters(win, "windows", "64", "english", ResolveOptions{}) {
		t.Fatal("windows depot should match")
	}
	linux := vdf.Child(depots, "233782")
	if depotMatchesFilters(linux, "windows", "64", "english", ResolveOptions{}) {
		t.Fatal("linux depot should not match windows")
	}
	man := vdf.Child(win, "manifests")
	if g := manifestGID(man, "profiling"); g != 222 {
		t.Fatalf("profiling gid = %d", g)
	}
}
