package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arma3modmanager/internal/database"
)

func TestPickDuplicatePorts(t *testing.T) {
	src := database.Server{ID: 1, GamePort: 2302, SteamPort: 2303}
	all := []database.Server{
		src,
		{ID: 2, GamePort: 2402, SteamPort: 2403}, // blocks +100
		{ID: 4, GamePort: 2490, SteamPort: 2505}, // steam port blocks +200 (2502..2506)
		{ID: 3, GamePort: 2600, SteamPort: 2601}, // 2600..2604 blocks +300 (2602..2606)
	}
	g, s, err := PickDuplicatePorts(src, all)
	if err != nil {
		t.Fatal(err)
	}
	if g != 2702 || s != 2703 {
		t.Fatalf("got %d/%d", g, s)
	}
	if _, _, err := PickDuplicatePorts(database.Server{GamePort: 65500, SteamPort: 65501}, nil); err == nil {
		t.Fatal("expected error near port limit")
	}
}

func TestSetServerCfgHostname(t *testing.T) {
	in := "// cfg\nhostname = \"Old \"\"quoted\"\" name\";\npassword = \"x\";\n"
	out := setServerCfgHostname(in, `New "test"`)
	want := "// cfg\nhostname = \"New \"\"test\"\"\";\npassword = \"x\";\n"
	if out != want {
		t.Fatalf("got %q", out)
	}
	if got := setServerCfgHostname("password = \"x\";", "n"); got != "password = \"x\";" {
		t.Fatalf("no hostname line changed: %q", got)
	}
}

func TestShiftRConPort(t *testing.T) {
	in := "RConPassword secret\r\nRConPort 2306\r\nRConIP 0.0.0.0\r\n"
	if got := shiftRConPort(in, 100); got != "RConPassword secret\r\nRConPort 2406\r\nRConIP 0.0.0.0\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRelocatePath(t *testing.T) {
	oldRoot := filepath.Join("data", "servers", "a")
	newRoot := filepath.Join("data", "servers", "b")
	if got := relocatePath(filepath.Join(oldRoot, "custom_keys", "k.bikey"), oldRoot, newRoot); got != filepath.Join(newRoot, "custom_keys", "k.bikey") {
		t.Fatalf("inside: %s", got)
	}
	outside := filepath.Join("elsewhere", "k.bikey")
	if got := relocatePath(outside, oldRoot, newRoot); got != outside {
		t.Fatalf("outside: %s", got)
	}
	sibling := filepath.Join("data", "servers", "ab", "k.bikey")
	if got := relocatePath(sibling, oldRoot, newRoot); got != sibling {
		t.Fatalf("sibling: %s", got)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// TestDuplicateServerEndToEnd runs the full copy against a throwaway data dir.
func TestDuplicateServerEndToEnd(t *testing.T) {
	data := t.TempDir()
	t.Setenv("IDEAL_DATA_DIR", data)
	if err := database.Init(); err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	branches, err := database.GetBranches()
	if err != nil || len(branches) == 0 {
		t.Fatalf("branches: %v", err)
	}
	srcID64, err := database.CreateServer("Main", "main", branches[0].ID, nil, 2302, 2303, "Server", "")
	if err != nil {
		t.Fatal(err)
	}
	srcID := int(srcID64)
	if err := database.UpdateServer(srcID, "Main", branches[0].ID, nil, 2302, 2303, "Server", "", "-limitFPS=60", []string{"gm"}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateServerMemoryLimitMB(srcID, 4096); err != nil {
		t.Fatal(err)
	}
	slots := []database.RestartSlot{{Minute: 360, Days: 127}}
	if err := database.SaveServerAutomation(srcID, true, true, true, []int{10, 2}, slots, 555); err != nil {
		t.Fatal(err)
	}
	if err := database.SetCrashState(srcID, []int64{1, 2}, database.OwedCrash, 99); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordManualStop(srcID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SyncHeadlessSlots(srcID, 2, "Main", "main"); err != nil {
		t.Fatal(err)
	}
	user, err := database.CreateSteamUser("76561190000000001", "Tester", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateUserPermissions(user.ID, false, false, false, []int{srcID}); err != nil {
		t.Fatal(err)
	}

	modDir := filepath.Join(t.TempDir(), "@mymod")
	write(t, filepath.Join(modDir, "addons", "x.pbo"), "pbo")
	if err := database.ReplaceServerMods(srcID, []database.ServerMod{
		{CustomPath: modDir, Label: "client", Enabled: true},
		{CustomPath: modDir, Label: "opt", Enabled: true, Optional: true},
		{CustomPath: modDir, Label: "srv", Enabled: false, IsServerMod: true},
	}); err != nil {
		t.Fatal(err)
	}

	srcDir := InstanceDir("main")
	keyPath := filepath.Join(srcDir, "custom_keys", "extra.bikey")
	write(t, keyPath, "key")
	if _, err := database.AddServerKey(srcID, "extra.bikey", keyPath); err != nil {
		t.Fatal(err)
	}
	outsideKey := filepath.Join(t.TempDir(), "shared.bikey")
	write(t, outsideKey, "key")
	if _, err := database.AddServerKey(srcID, "shared.bikey", outsideKey); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(srcDir, "server.cfg"), "hostname = \"Main public\";\npassword = \"pw\";\n")
	write(t, filepath.Join(srcDir, "basic.cfg"), "MaxMsgSend = 128;\n")
	write(t, filepath.Join(srcDir, "mpmissions", "op.Altis.pbo"), "mission")
	write(t, filepath.Join(srcDir, "profiles", "home", "Server", "Server.Arma3Profile"), "difficulty")
	write(t, filepath.Join(srcDir, "profiles", "BattlEye", "beserver_x64.cfg"), "RConPassword x\nRConPort 2310\n")
	write(t, filepath.Join(srcDir, "profiles", "BattlEye", "beserver_x64_active_123.cfg"), "runtime")
	write(t, filepath.Join(srcDir, "profiles", "arma3server_x64_2026.rpt"), "log")
	write(t, filepath.Join(srcDir, "profiles", "DataCache", "c.bin"), "cache")
	write(t, filepath.Join(srcDir, "logs", "session.log"), "log")
	write(t, filepath.Join(srcDir, "mods", "stale"), "x")
	write(t, filepath.Join(srcDir, "userconfig", "cba_settings.sqf"), "settings")

	res, err := DuplicateServer(srcID, "Main test")
	if err != nil {
		t.Fatal(err)
	}
	if res.Slug != "main-test" || res.GamePort != 2402 || res.SteamPort != 2403 {
		t.Fatalf("result: %+v", res)
	}

	dup, err := database.GetServer(int(res.ID))
	if err != nil {
		t.Fatal(err)
	}
	if dup.Name != "Main test" || dup.Status != database.ServerStatusStopped || dup.ExtraLaunchParams != "-limitFPS=60" ||
		dup.MemoryLimitMB != 4096 || dup.HeadlessCount != 2 || len(dup.EnabledCdlcs) != 1 || dup.EnabledCdlcs[0] != "gm" {
		t.Fatalf("server row: %+v", dup)
	}
	auto, err := database.GetServerAutomation(int(res.ID))
	if err != nil {
		t.Fatal(err)
	}
	if auto.Autostart || auto.RestartOnCrash || auto.ScheduleEnabled {
		t.Fatalf("a copy must not start or restart itself: %+v", auto)
	}
	if len(auto.RestartSlots) != 1 || auto.RestartSlots[0].Minute != 360 || len(auto.WarnMinutes) != 2 || auto.WarnMinutes[0] != 10 {
		t.Fatalf("schedule should be copied: %+v", auto)
	}
	if auto.UserStopped || auto.OwedStart != database.OwedNone || auto.GaveUpAt != 0 || len(auto.CrashHistory) != 0 || auto.LastSlotUnix != 0 {
		t.Fatalf("runtime state should start clean: %+v", auto)
	}
	hcs, _ := database.GetHeadlessClients(int(res.ID))
	if len(hcs) != 2 || !strings.HasPrefix(hcs[0].ProfileName, "Maintest-HC") {
		t.Fatalf("headless: %+v", hcs)
	}
	mods, _ := database.GetServerMods(int(res.ID))
	if len(mods) != 3 || mods[0].Label != "client" || !mods[1].Optional || !mods[2].IsServerMod || mods[2].Enabled {
		t.Fatalf("mods: %+v", mods)
	}
	ids, _ := database.GetUserServerIDs(user.ID)
	if len(ids) != 2 {
		t.Fatalf("admin servers: %v", ids)
	}
	keys, _ := database.GetServerKeys(int(res.ID))
	dstDir := InstanceDir(res.Slug)
	byName := map[string]string{}
	for _, k := range keys {
		byName[k.Filename] = k.SourcePath
	}
	if byName["extra.bikey"] != filepath.Join(dstDir, "custom_keys", "extra.bikey") || byName["shared.bikey"] != outsideKey {
		t.Fatalf("keys: %+v", keys)
	}

	cfg, _ := os.ReadFile(filepath.Join(dstDir, "server.cfg"))
	if !strings.Contains(string(cfg), `hostname = "Main test";`) || !strings.Contains(string(cfg), `password = "pw";`) {
		t.Fatalf("server.cfg: %s", cfg)
	}
	be, _ := os.ReadFile(filepath.Join(dstDir, "profiles", "BattlEye", "beserver_x64.cfg"))
	if !strings.Contains(string(be), "RConPort 2410") {
		t.Fatalf("beserver: %s", be)
	}
	for _, rel := range []string{
		"basic.cfg", "mpmissions/op.Altis.pbo", "profiles/home/Server/Server.Arma3Profile",
		"custom_keys/extra.bikey", "userconfig/cba_settings.sqf",
	} {
		if !exists(filepath.Join(dstDir, rel)) {
			t.Errorf("missing copied %s", rel)
		}
	}
	for _, rel := range []string{
		"profiles/BattlEye/beserver_x64_active_123.cfg", "profiles/arma3server_x64_2026.rpt",
		"profiles/DataCache", "logs/session.log", "mods/stale",
	} {
		if exists(filepath.Join(dstDir, rel)) {
			t.Errorf("should not copy %s", rel)
		}
	}

	// Original untouched.
	orig, _ := os.ReadFile(filepath.Join(srcDir, "server.cfg"))
	if !strings.Contains(string(orig), `hostname = "Main public";`) {
		t.Fatalf("original server.cfg changed: %s", orig)
	}
	if s, _ := database.GetServer(srcID); s.GamePort != 2302 || s.Name != "Main" {
		t.Fatalf("original row changed: %+v", s)
	}

	// Second copy gets the next free range and a distinct slug.
	res2, err := DuplicateServer(srcID, "Main test")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Slug == res.Slug || res2.GamePort != 2502 {
		t.Fatalf("second copy: %+v", res2)
	}
}
