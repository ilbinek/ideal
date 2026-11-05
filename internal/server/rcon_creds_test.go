package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"arma3modmanager/internal/database"
)

func writeBE(t *testing.T, path, content string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-age)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

func TestReadCredsActiveOverBase(t *testing.T) {
	dir := t.TempDir()
	writeBE(t, filepath.Join(dir, "beserver_x64.cfg"), "RConPassword old\nRConPort 1\n", time.Hour)
	writeBE(t, filepath.Join(dir, "beserver_x64_active_abc.cfg"), "RConPassword new\nRConPort 2\n", time.Minute)
	writeBE(t, filepath.Join(dir, "beserver_active_other.cfg"), "RConPassword x86\nRConPort 3\n", 0)

	path, isBase := findBEConfig(dir, "beserver_x64")
	if isBase || !strings.Contains(path, "active_abc") {
		t.Fatalf("path %q base %v", path, isBase)
	}

	// Generating afterwards makes the base newest again, and it should win.
	writeBE(t, filepath.Join(dir, "beserver_x64.cfg"), "RConPassword gen\nRConPort 4\n", 0)
	if path, isBase := findBEConfig(dir, "beserver_x64"); !isBase {
		t.Fatalf("newest base lost to %q", path)
	}
}

func TestReadCredsCRLF(t *testing.T) {
	cfg := parseBEConfig("RConPassword  pass word\r\nRConPort\t2310\r\n\r\n// RConIP 1.2.3.4\r\nRESTRICTRCON 1\r\n")
	creds, ok := credsFromConfig(cfg)
	if !ok {
		t.Fatalf("cfg %v", cfg)
	}
	if creds.Password != "pass word" || creds.Port != 2310 || creds.Host != "127.0.0.1" {
		t.Fatalf("creds %+v", creds)
	}
	if cfg["restrictrcon"] != "1" {
		t.Fatalf("restrict %q", cfg["restrictrcon"])
	}

	for _, content := range []string{"", "RConPassword x\n", "RConPort 2310\n", "RConPassword x\nRConPort nope\n"} {
		if _, ok := credsFromConfig(parseBEConfig(content)); ok {
			t.Errorf("incomplete config accepted: %q", content)
		}
	}
}

func TestReadCredsBattleyeOff(t *testing.T) {
	cases := map[string]bool{
		"":                                    true,
		"hostname = \"x\";\n":                 true,
		"BattlEye = 1;\n":                     true,
		"BattlEye = 0;\n":                     false,
		"  battleye=0;\r\n":                   false,
		"// BattlEye = 0;\nBattlEye = 1;\n":   true,
		"BattlEyeLicense = 0;\nBattlEye=1;\n": true,
	}
	for content, want := range cases {
		if got := battleyeEnabledInCfg(content); got != want {
			t.Errorf("%q: got %v", content, got)
		}
	}
}

func TestReadCredsCustomBepath(t *testing.T) {
	instance := t.TempDir()
	if got := battleyeConfigDir(instance, `-limitFPS=60 -BEpath=custom\be`); got != filepath.Join(instance, `custom\be`) {
		t.Fatalf("relative: %q", got)
	}
	abs := filepath.Join(instance, "elsewhere")
	if got := battleyeConfigDir(instance, "-bepath="+abs); got != abs {
		t.Fatalf("absolute: %q", got)
	}
	// No -bepath: an existing folder is matched whatever its case.
	lower := filepath.Join(instance, "profiles", "battleye")
	if err := os.MkdirAll(lower, 0755); err != nil {
		t.Fatal(err)
	}
	got := battleyeConfigDir(instance, "-limitFPS=60")
	if !strings.EqualFold(got, lower) {
		t.Fatalf("existing folder: %q", got)
	}
	if got := battleyeConfigDir(t.TempDir(), ""); !strings.HasSuffix(got, filepath.Join("profiles", "BattlEye")) {
		t.Fatalf("default: %q", got)
	}
}

func TestBEConfigStemFollowsBinary(t *testing.T) {
	if got := beConfigStem("arma3server_x64.exe"); got != "beserver_x64" {
		t.Fatal(got)
	}
	if got := beConfigStem("arma3server"); got != "beserver" {
		t.Fatal(got)
	}
}

func TestGenerateNeverOverwrites(t *testing.T) {
	existing := "RConPassword keepme\nRestrictRCon 1\nMaxPing 200"
	out := appendBESettings(existing, parseBEConfig(existing), "", 2350)
	if !strings.HasPrefix(out, existing+"\n") {
		t.Fatalf("existing content changed: %q", out)
	}
	cfg := parseBEConfig(out)
	if cfg["rconpassword"] != "keepme" || cfg["restrictrcon"] != "1" || cfg["maxping"] != "200" {
		t.Fatalf("cfg %v", cfg)
	}
	if cfg["rconport"] != "2350" {
		t.Fatalf("missing port not added: %v", cfg)
	}
	if strings.Count(out, "RConPassword") != 1 {
		t.Fatalf("password duplicated: %q", out)
	}
}

func TestGenerateAndLoadEndToEnd(t *testing.T) {
	t.Setenv("IDEAL_DATA_DIR", t.TempDir())
	if err := database.Init(); err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	branches, err := database.GetBranches()
	if err != nil || len(branches) == 0 {
		t.Fatalf("branches: %v", err)
	}
	game := freePort(t)
	id64, err := database.CreateServer("Main", "main", branches[0].ID, nil, game, freePort(t), "Server", "")
	if err != nil {
		t.Fatal(err)
	}
	id := int(id64)
	srv, err := database.GetServer(id)
	if err != nil {
		t.Fatal(err)
	}

	status, err := GetRConStatus(id)
	if err != nil {
		t.Fatal(err)
	}
	if status.Configured {
		t.Fatalf("nothing generated yet: %+v", status)
	}

	first, err := GenerateRConConfig(id)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Status.Configured || first.RequiresRestart {
		t.Fatalf("result %+v", first)
	}
	if p := first.Status.Port; p < game+rconPortSearchFrom || p > game+rconPortSearchTo {
		t.Fatalf("port %d outside search range of game port %d", p, game)
	}
	if first.Status.RestrictRcon != "0" {
		t.Fatalf("restrict %q", first.Status.RestrictRcon)
	}

	_, creds, usable := loadRCon(srv)
	if !usable || len(creds.Password) != rconPasswordLength || creds.Host != generatedRConIP {
		t.Fatalf("usable=%v host=%s len=%d", usable, creds.Host, len(creds.Password))
	}

	// Generating again changes nothing.
	again, err := GenerateRConConfig(id)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status.Port != first.Status.Port {
		t.Fatalf("port moved: %d -> %d", first.Status.Port, again.Status.Port)
	}
	if _, creds2, _ := loadRCon(srv); creds2.Password != creds.Password {
		t.Fatal("password was replaced")
	}

	// The written config must stay inside this instance's profiles folder.
	dir, stem := serverBEPaths(srv)
	if !strings.HasPrefix(dir, filepath.Join(InstanceDir(srv.Slug), "profiles")) {
		t.Fatalf("config dir %q", dir)
	}

	// After a crash only an active copy remains; the next start restores the base.
	base := filepath.Join(dir, stem+".cfg")
	active := filepath.Join(dir, stem+"_active_x.cfg")
	if err := os.Rename(base, active); err != nil {
		t.Fatal(err)
	}
	if err := restoreBEConfig(srv); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(base)
	if err != nil {
		t.Fatalf("base not restored: %v", err)
	}
	if !strings.Contains(string(data), creds.Password) {
		t.Fatal("restored base lost the password")
	}

	// BattlEye off makes RCON unusable even with a full config.
	if err := os.MkdirAll(InstanceDir(srv.Slug), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(InstanceDir(srv.Slug), "server.cfg"), []byte("BattlEye = 0;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if status, _, usable := loadRCon(srv); usable || status.BattleyeEnabled || !status.Configured {
		t.Fatalf("usable=%v status=%+v", usable, status)
	}
}

func TestPickRConPortAvoidsServerPorts(t *testing.T) {
	t.Setenv("IDEAL_DATA_DIR", t.TempDir())
	if err := database.Init(); err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	branches, err := database.GetBranches()
	if err != nil || len(branches) == 0 {
		t.Fatalf("branches: %v", err)
	}
	game := freePort(t)
	id1, err := database.CreateServer("A", "a", branches[0].ID, nil, game, game+1, "Server", "")
	if err != nil {
		t.Fatal(err)
	}
	// A second server whose game ports sit right where the search begins.
	if _, err := database.CreateServer("B", "b", branches[0].ID, nil, game+rconPortSearchFrom, game+rconPortSearchFrom+1, "Server", ""); err != nil {
		t.Fatal(err)
	}
	srv, err := database.GetServer(int(id1))
	if err != nil {
		t.Fatal(err)
	}
	port, err := pickRConPort(srv)
	if err != nil {
		t.Fatal(err)
	}
	if port >= game+rconPortSearchFrom && port < game+rconPortSearchFrom+armaPortSpan {
		t.Fatalf("port %d collides with server B", port)
	}
}
