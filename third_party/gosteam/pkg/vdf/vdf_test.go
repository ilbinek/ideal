package vdf

import (
	"bytes"
	"testing"
)

func TestParseBinaryAndFind(t *testing.T) {
	var b bytes.Buffer
	obj := func(name string) { b.WriteByte(typeNone); b.WriteString(name); b.WriteByte(0) }
	str := func(name, val string) {
		b.WriteByte(typeString)
		b.WriteString(name)
		b.WriteByte(0)
		b.WriteString(val)
		b.WriteByte(0)
	}
	end := func() { b.WriteByte(typeEnd) }

	// appinfo { common { name "Arma 3" } depots { workshopdepot "107411" } }
	obj("appinfo")
	obj("common")
	str("name", "Arma 3")
	end()
	obj("depots")
	str("workshopdepot", "107411")
	end()
	end()

	tree, err := ParseBinary(b.Bytes())
	if err != nil {
		t.Fatalf("ParseBinary: %v", err)
	}
	depot, ok := FindUint32(tree, "workshopdepot")
	if !ok || depot != 107411 {
		t.Fatalf("FindUint32(workshopdepot) = %d, %v; want 107411, true", depot, ok)
	}
	if _, ok := FindUint32(tree, "doesnotexist"); ok {
		t.Fatal("expected miss for unknown key")
	}
}

func TestParseText(t *testing.T) {
	src := []byte(`
"appinfo"
{
	"appid"		"107410"
	// a comment
	"common"
	{
		"name"		"Arma 3"  [$WIN64]
	}
	"depots"
	{
		"workshopdepot"		"107411"
		"branches"
		{
			"public" { "buildid" "123" }
		}
	}
}
`)
	tree, err := ParseText(src)
	if err != nil {
		t.Fatalf("ParseText: %v", err)
	}
	depot, ok := FindUint32(tree, "workshopdepot")
	if !ok || depot != 107411 {
		t.Fatalf("FindUint32(workshopdepot) = %d, %v; want 107411, true", depot, ok)
	}

	root := Child(tree, "appinfo")
	if root == nil {
		t.Fatal("Child(appinfo) missing")
	}
	common := Child(root, "common")
	name, ok := String(common, "name")
	if !ok || name != "Arma 3" {
		t.Fatalf("String(name) = %q, %v", name, ok)
	}
	depots := Child(root, "depots")
	branches := Child(depots, "branches")
	public := Child(branches, "public")
	if build, ok := Uint32(public, "buildid"); !ok || build != 123 {
		t.Fatalf("Uint32(buildid) = %d, %v", build, ok)
	}
}
