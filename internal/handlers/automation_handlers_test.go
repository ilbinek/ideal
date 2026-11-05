package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"arma3modmanager/internal/database"
)

func newAutomationServer(t *testing.T) int {
	t.Helper()
	t.Setenv("IDEAL_DATA_DIR", t.TempDir())
	if err := database.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	branches, err := database.GetBranches()
	if err != nil || len(branches) == 0 {
		t.Fatalf("branches: %v", err)
	}
	id, err := database.CreateServer("Main", "main", branches[0].ID, nil, 42302, 42303, "Server", "")
	if err != nil {
		t.Fatal(err)
	}
	return int(id)
}

func callAutomation(t *testing.T, fn http.HandlerFunc, method string, id int, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.SetPathValue("id", strconv.Itoa(id))
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec
}

func TestPutAutomationValidatesFirst(t *testing.T) {
	id := newAutomationServer(t)

	// Every field here is fine except the last slot, and none of it may be saved.
	bad := []string{
		`{"autostart":true,"restartOnCrash":true,"restartSlots":[{"m":60,"d":127},{"m":1440,"d":127}]}`,
		`{"autostart":true,"restartSlots":[{"m":60,"d":0}]}`,
		`{"autostart":true,"restartSlots":[{"m":60,"d":128}]}`,
		`{"autostart":true,"warnMinutes":[0]}`,
		`{"autostart":true,"warnMinutes":[121]}`,
		`{"autostart":true,"scheduleEnabled":true,"restartSlots":[]}`,
		`not json`,
	}
	for _, body := range bad {
		rec := callAutomation(t, PutServerAutomationHandler, http.MethodPut, id, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", body, rec.Code)
		}
	}
	a, err := database.GetServerAutomation(id)
	if err != nil {
		t.Fatal(err)
	}
	if a.Autostart || a.RestartOnCrash {
		t.Fatalf("rejected request was partly saved: %+v", a)
	}
}

func TestPutAutomationSavesAndNormalizes(t *testing.T) {
	id := newAutomationServer(t)
	rec := callAutomation(t, PutServerAutomationHandler, http.MethodPut, id,
		`{"autostart":true,"restartOnCrash":true,"scheduleEnabled":true,"warnMinutes":[1,15,5,5],"restartSlots":[{"m":360,"d":127}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	a, err := database.GetServerAutomation(id)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Autostart || !a.RestartOnCrash || !a.ScheduleEnabled {
		t.Fatalf("%+v", a)
	}
	if len(a.WarnMinutes) != 3 || a.WarnMinutes[0] != 15 || a.WarnMinutes[2] != 1 {
		t.Fatalf("warnings %v", a.WarnMinutes)
	}
	if a.LastSlotUnix == 0 {
		t.Fatal("saving must mark past slots as handled, so none fires right away")
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if _, ok := resp["rcon"]; !ok {
		t.Fatalf("response %v", resp)
	}
}

func TestAutomationResponseHasNoPassword(t *testing.T) {
	id := newAutomationServer(t)
	rec := callAutomation(t, GenerateRConHandler, http.MethodPost, id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("generate: status %d: %s", rec.Code, rec.Body)
	}
	for _, body := range []string{rec.Body.String(), callAutomation(t, GetServerAutomationHandler, http.MethodGet, id, "").Body.String()} {
		if strings.Contains(strings.ToLower(body), "password") {
			t.Fatalf("response mentions a password: %s", body)
		}
		if !strings.Contains(body, `"configured":true`) {
			t.Fatalf("expected RCON to be configured: %s", body)
		}
	}
}

func TestPutAutomationSettingsValidates(t *testing.T) {
	newAutomationServer(t)
	call := func(body string) int {
		req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
		rec := httptest.NewRecorder()
		PutAutomationSettingsHandler(rec, req)
		return rec.Code
	}
	for _, body := range []string{
		`{"bootDelaySec":-1,"bootGapSec":30,"warningTemplate":"x"}`,
		`{"bootDelaySec":30,"bootGapSec":3601,"warningTemplate":"x"}`,
		`{"bootDelaySec":30,"bootGapSec":30,"warningTemplate":"   "}`,
		`{"bootDelaySec":30,"bootGapSec":30,"warningTemplate":"` + strings.Repeat("x", maxTemplateLength+1) + `"}`,
	} {
		if code := call(body); code != http.StatusBadRequest {
			t.Errorf("%.60s: status %d", body, code)
		}
	}
	if code := call(`{"paused":true,"bootDelaySec":5,"bootGapSec":6,"warningTemplate":"Back soon"}`); code != http.StatusOK {
		t.Fatalf("valid request: %d", code)
	}
	s, err := database.GetAutomationSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !s.Paused || s.BootDelaySec != 5 || s.BootGapSec != 6 || s.WarningTemplate != "Back soon" {
		t.Fatalf("%+v", s)
	}
}
