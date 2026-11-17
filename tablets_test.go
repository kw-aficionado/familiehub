package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type tabletReply struct {
	status   int
	data     map[string]any
	recorder *httptest.ResponseRecorder
}

func tabletCall(a *App, method, path string, body any, cookie, csrf string, headers map[string]string) tabletReply {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	r.RemoteAddr = "192.0.2.20:1234"
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if path == "/api/tablet/pair" {
		r.Header.Set("X-FH-Pairing", "1")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	securityHeaders(a.mux).ServeHTTP(w, r)
	data := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &data)
	return tabletReply{w.Code, data, w}
}
func tabletExpect(t *testing.T, r tabletReply, code int) {
	t.Helper()
	if r.status != code {
		t.Fatalf("got %d want %d: %s", r.status, code, r.recorder.Body.String())
	}
}
func replyCookie(t *testing.T, r tabletReply, name string) string {
	t.Helper()
	for _, c := range r.recorder.Result().Cookies() {
		if c.Name == name && c.Value != "" {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatalf("missing cookie %s", name)
	return ""
}
func tabletAdmin(t *testing.T, a *App) (string, string) {
	t.Helper()
	r := tabletCall(a, "POST", "/api/login", map[string]string{"password": a.cfg.Password}, "", "", nil)
	tabletExpect(t, r, 200)
	return replyCookie(t, r, "fh_session"), r.data["csrf"].(string)
}
func createTestTablet(t *testing.T, a *App, admin, csrf string, complete bool) (string, string) {
	t.Helper()
	r := tabletCall(a, "POST", "/api/tablets", tabletConfig{Name: "Test kitchen", CanComplete: complete}, admin, csrf, nil)
	tabletExpect(t, r, 201)
	return r.data["id"].(string), r.data["pair_code"].(string)
}
func pairTestTablet(t *testing.T, a *App, code string) (string, string) {
	t.Helper()
	r := tabletCall(a, "POST", "/api/tablet/pair", map[string]string{"code": code}, "", "", nil)
	tabletExpect(t, r, 200)
	cookie := replyCookie(t, r, tabletCookie)
	s := tabletCall(a, "GET", "/api/tablet/session", nil, cookie, "", nil)
	tabletExpect(t, s, 200)
	return cookie, s.data["csrf"].(string)
}

func TestTabletPairingPrivilegesAndSafeProjection(t *testing.T) {
	a, _ := testApp(t)
	admin, csrf := tabletAdmin(t, a)
	id, code := createTestTablet(t, a, admin, csrf, true)
	cookie, tcsrf := pairTestTablet(t, a, strings.ToLower(code))
	if cookie == admin || tcsrf == csrf {
		t.Fatal("ordinary and tablet auth must be separate")
	}
	rows, _ := a.db.Query("SELECT pair_hash,pair_expires FROM tablet_devices WHERE id=?", id)
	if rows[0]["pair_hash"] != "" || rows[0]["pair_expires"] != "" {
		t.Fatal("code was not consumed")
	}
	rows, _ = a.db.Query("SELECT token_hash FROM tablet_sessions WHERE device_id=?", id)
	if strings.Contains(cookie, rows[0]["token_hash"]) {
		t.Fatal("raw token stored")
	}
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/pair", map[string]string{"code": code}, "", "", nil), 400)
	for _, path := range []string{"/api/accounts", "/api/direct-calendars", "/api/ai/config", "/api/tablets", "/api/templates", "/api/planner/diagnostics", "/api/mcp/config"} {
		tabletExpect(t, tabletCall(a, "GET", path, nil, cookie, "", nil), 401)
	}
	for _, path := range []string{"/api/people", "/api/events", "/api/tasks", "/api/planner/apply", "/api/quickstart/apply", "/api/tablets"} {
		tabletExpect(t, tabletCall(a, "POST", path, map[string]any{}, cookie, tcsrf, nil), 401)
	}
	person := newID()
	if e := a.db.Exec("INSERT INTO people(id,name,color,notify_service) VALUES(?,?,?,?)", person, "Fictional child", "#3999aa", "secret_notify_service"); e != nil {
		t.Fatal(e)
	}
	now := a.timeNow().In(a.loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, a.loc)
	if e := a.db.Exec("INSERT INTO events(id,title,start_at,end_at,notes) VALUES(?,?,?,?,?)", newID(), "Fictional activity", start.UTC().Format(time.RFC3339), start.Add(time.Hour).UTC().Format(time.RFC3339), "private-calendar-link-do-not-project"); e != nil {
		t.Fatal(e)
	}
	r := tabletCall(a, "GET", "/api/tablet/state", nil, cookie, "", nil)
	tabletExpect(t, r, 200)
	text := r.recorder.Body.String()
	for _, secret := range []string{"secret_notify_service", "private-calendar-link", "notify_service", "notes", "source_calendar", "token_hash", "pair_hash"} {
		if strings.Contains(text, secret) {
			t.Fatalf("tablet response exposed %s", secret)
		}
	}
	if len(r.data["events"].([]any)) != 1 || len(r.data["people"].([]any)) != 1 {
		t.Fatal(text)
	}
	tabletExpect(t, tabletCall(a, "GET", "/api/tablet/state?day=wrong", nil, cookie, "", nil), 400)
	tabletExpect(t, tabletCall(a, "GET", "/api/tablet/session", nil, "fh_tablet=forged", "", nil), 401)
	// An ordinary admin session is not automatically a tablet session.
	tabletExpect(t, tabletCall(a, "GET", "/api/tablet/session", nil, admin, "", nil), 401)
}
func TestTabletAdminAndPairingCSRF(t *testing.T) {
	a, _ := testApp(t)
	admin, csrf := tabletAdmin(t, a)
	tabletExpect(t, tabletCall(a, "POST", "/api/tablets", tabletConfig{Name: "Kitchen"}, admin, "", nil), 403)
	id, code := createTestTablet(t, a, admin, csrf, false)
	for _, headers := range []map[string]string{{"X-FH-Pairing": ""}, {"Origin": "https://evil.example"}, {"Origin": "null"}, {"Sec-Fetch-Site": "cross-site"}} {
		tabletExpect(t, tabletCall(a, "POST", "/api/tablet/pair", map[string]string{"code": code}, "", "", headers), 403)
	}
	// Real ordinary member, not merely an unauthenticated request.
	response := tabletCall(a, "POST", "/api/accounts", map[string]any{"username": "member", "password": "fictional-long-password", "enabled": true}, admin, csrf, nil)
	tabletExpect(t, response, 200)
	member := tabletCall(a, "POST", "/api/login", map[string]string{"username": "member", "password": "fictional-long-password"}, "", "", nil)
	tabletExpect(t, member, 200)
	mc := replyCookie(t, member, "fh_session")
	mt := member.data["csrf"].(string)
	tabletExpect(t, tabletCall(a, "GET", "/api/tablets", nil, mc, "", nil), 403)
	for _, path := range []string{"/api/tablets", "/api/tablets/" + id + "/pair", "/api/tablets/" + id + "/revoke"} {
		tabletExpect(t, tabletCall(a, "POST", path, map[string]any{}, mc, mt, nil), 403)
	}
	// Claim clears only this browser's ordinary login, preserving other logins.
	other, _ := tabletAdmin(t, a)
	r := tabletCall(a, "POST", "/api/tablet/pair", map[string]string{"code": code}, admin, "", map[string]string{"Origin": a.cfg.AppURL})
	tabletExpect(t, r, 200)
	tabletExpect(t, tabletCall(a, "GET", "/api/accounts", nil, admin, "", nil), 401)
	tabletExpect(t, tabletCall(a, "GET", "/api/accounts", nil, other, "", nil), 200)
	cleared := false
	for _, cookie := range r.recorder.Result().Cookies() {
		if cookie.Name == "fh_session" && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("ordinary browser cookie not cleared")
	}
}
func TestTabletCookieFlagsAndCodeExpiry(t *testing.T) {
	a, _ := testApp(t)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	a.cfg.AppURL = "https://family.example"
	admin, csrf := tabletAdmin(t, a)
	_, code := createTestTablet(t, a, admin, csrf, false)
	now = now.Add(tabletPairLifetime)
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/pair", map[string]string{"code": code}, "", "", nil), 400)
	_, code = createTestTablet(t, a, admin, csrf, false)
	r := tabletCall(a, "POST", "/api/tablet/pair", map[string]string{"code": code}, "", "", nil)
	tabletExpect(t, r, 200)
	for _, c := range r.recorder.Result().Cookies() {
		if c.Name == tabletCookie {
			if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/api/tablet" || c.MaxAge != int(tabletIdleLifetime.Seconds()) {
				t.Fatalf("unsafe cookie: %+v", c)
			}
		}
	}
	if strings.Contains(r.recorder.Body.String(), "token") || strings.Contains(r.recorder.Body.String(), code) {
		t.Fatal("secret returned to JS")
	}
}
func TestTabletSingleUsePairingIsAtomic(t *testing.T) {
	a, _ := testApp(t)
	admin, csrf := tabletAdmin(t, a)
	_, code := createTestTablet(t, a, admin, csrf, false)
	var wg sync.WaitGroup
	statuses := make(chan int, 12)
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- tabletCall(a, "POST", "/api/tablet/pair", map[string]string{"code": code}, "", "", nil).status
		}()
	}
	wg.Wait()
	close(statuses)
	successes := 0
	for status := range statuses {
		if status == 200 {
			successes++
		} else if status != 400 {
			t.Fatalf("unexpected status %d", status)
		}
	}
	if successes != 1 {
		t.Fatalf("%d successful claims for one code", successes)
	}
}
func TestTabletCompletionRequiresScopeTodayAndCSRF(t *testing.T) {
	a, _ := testApp(t)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	admin, csrf := tabletAdmin(t, a)
	id, code := createTestTablet(t, a, admin, csrf, false)
	cookie, tcsrf := pairTestTablet(t, a, code)
	task := newID()
	if e := a.db.Exec("INSERT INTO tasks(id,title,due_date,repeat_rule) VALUES(?,?,?,?)", task, "Test task", "2026-11-16", "daily"); e != nil {
		t.Fatal(e)
	}
	path := "/api/tablet/tasks/" + task + "/toggle"
	body := map[string]any{"day": "2026-11-17", "done": true}
	tabletExpect(t, tabletCall(a, "POST", path, body, cookie, "", nil), 403)
	tabletExpect(t, tabletCall(a, "POST", path, body, cookie, csrf, nil), 403)
	tabletExpect(t, tabletCall(a, "POST", path, body, cookie, tcsrf, nil), 403)
	tabletExpect(t, tabletCall(a, "PUT", "/api/tablets/"+id, tabletConfig{Name: "Kitchen", CanComplete: true}, admin, csrf, nil), 200)
	tabletExpect(t, tabletCall(a, "POST", path, body, cookie, tcsrf, map[string]string{"Origin": "https://evil.example"}), 403)
	tabletExpect(t, tabletCall(a, "POST", path, map[string]any{"day": "2026-11-16", "done": true}, cookie, tcsrf, nil), 400)
	tabletExpect(t, tabletCall(a, "POST", path, map[string]any{"day": "2026-11-18", "done": true}, cookie, tcsrf, nil), 400)
	tabletExpect(t, tabletCall(a, "POST", path, body, cookie, tcsrf, nil), 200)
	rows, _ := a.db.Query("SELECT day FROM completions WHERE task_id=?", task)
	if len(rows) != 1 || rows[0]["day"] != "2026-11-17" {
		t.Fatal(rows)
	}
	body["done"] = false
	tabletExpect(t, tabletCall(a, "POST", path, body, cookie, tcsrf, nil), 200)
	rows, _ = a.db.Query("SELECT day FROM completions WHERE task_id=?", task)
	if len(rows) != 0 {
		t.Fatal("uncheck failed")
	}
	_ = a.db.Exec("UPDATE tasks SET inactive_from='2026-11-17' WHERE id=?", task)
	tabletExpect(t, tabletCall(a, "POST", path, body, cookie, tcsrf, nil), 400)
}
func TestTabletRevocationRepairAndDisconnectAreIsolated(t *testing.T) {
	a, _ := testApp(t)
	admin, csrf := tabletAdmin(t, a)
	id, code := createTestTablet(t, a, admin, csrf, true)
	cookie, tcsrf := pairTestTablet(t, a, code)
	_, code2 := createTestTablet(t, a, admin, csrf, false)
	cookie2, _ := pairTestTablet(t, a, code2)
	tabletExpect(t, tabletCall(a, "POST", "/api/tablets/"+id+"/revoke", map[string]any{}, admin, csrf, nil), 200)
	tabletExpect(t, tabletCall(a, "GET", "/api/tablet/state", nil, cookie, "", nil), 401)
	tabletExpect(t, tabletCall(a, "GET", "/api/tablet/state", nil, cookie2, "", nil), 200)
	task := newID()
	_ = a.db.Exec("INSERT INTO tasks(id,title,due_date) VALUES(?,?,?)", task, "Test", a.timeNow().In(a.loc).Format("2006-01-02"))
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/tasks/"+task+"/toggle", map[string]any{"day": a.timeNow().In(a.loc).Format("2006-01-02"), "done": true}, cookie, tcsrf, nil), 401)
	repaired := tabletCall(a, "POST", "/api/tablets/"+id+"/pair", map[string]any{}, admin, csrf, nil)
	tabletExpect(t, repaired, 200)
	newer, nt := pairTestTablet(t, a, repaired.data["pair_code"].(string))
	tabletExpect(t, tabletCall(a, "GET", "/api/tablet/session", nil, cookie, "", nil), 401)
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/disconnect", map[string]any{}, newer, nt, nil), 200)
	tabletExpect(t, tabletCall(a, "GET", "/api/tablet/session", nil, newer, "", nil), 401)
	tabletExpect(t, tabletCall(a, "GET", "/api/tablet/session", nil, cookie2, "", nil), 200)
	tabletExpect(t, tabletCall(a, "GET", "/api/accounts", nil, admin, "", nil), 200)
}
func TestTabletRenewalHasIdleAndAbsoluteExpiry(t *testing.T) {
	a, _ := testApp(t)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	initial := now
	a.now = func() time.Time { return now }
	admin, csrf := tabletAdmin(t, a)
	_, code := createTestTablet(t, a, admin, csrf, false)
	cookie, _ := pairTestTablet(t, a, code)
	// Request activity renews the cookie, with no timer-only access beyond expiry.
	now = initial.Add(20 * 24 * time.Hour)
	r := tabletCall(a, "GET", "/api/tablet/session", nil, cookie, "", nil)
	tabletExpect(t, r, 200)
	if len(r.recorder.Result().Cookies()) != 1 {
		t.Fatal("cookie not renewed")
	}
	for n := 40; n < 180; n += 20 {
		now = initial.Add(time.Duration(n) * 24 * time.Hour)
		tabletExpect(t, tabletCall(a, "GET", "/api/tablet/session", nil, cookie, "", nil), 200)
	}
	now = initial.Add(tabletAbsoluteLifetime)
	tabletExpect(t, tabletCall(a, "GET", "/api/tablet/session", nil, cookie, "", nil), 401)
	now = initial
	_, code = createTestTablet(t, a, admin, csrf, false)
	cookie, _ = pairTestTablet(t, a, code)
	now = initial.Add(tabletIdleLifetime)
	tabletExpect(t, tabletCall(a, "GET", "/api/tablet/session", nil, cookie, "", nil), 401)
}
func TestTabletPersistsAcrossRestartAndValidatesSettings(t *testing.T) {
	a, _ := testApp(t)
	admin, csrf := tabletAdmin(t, a)
	_, code := createTestTablet(t, a, admin, csrf, false)
	cookie, _ := pairTestTablet(t, a, code)
	if err := a.db.Init(); err != nil {
		t.Fatal(err)
	}
	second := &App{db: a.db, cfg: a.cfg, key: a.key, client: a.client, loc: a.loc, mux: http.NewServeMux(), listeners: map[chan string]struct{}{}}
	second.routes()
	tabletExpect(t, tabletCall(second, "GET", "/api/tablet/session", nil, cookie, "", nil), 200)
	for _, v := range []tabletConfig{{Name: ""}, {Name: "Kitchen", PersonFilter: "missing"}, {Name: "Kitchen", Display: &tabletDisplay{Theme: "evil"}}} {
		tabletExpect(t, tabletCall(a, "POST", "/api/tablets", v, admin, csrf, nil), 400)
	}
	d := defaultTabletDisplay()
	d.SaverTo = d.SaverFrom
	tabletExpect(t, tabletCall(a, "POST", "/api/tablets", tabletConfig{Name: "Bad hours", Display: d}, admin, csrf, nil), 400)
	d = defaultTabletDisplay()
	d.SaverDelay = 1
	tabletExpect(t, tabletCall(a, "POST", "/api/tablets", tabletConfig{Name: "Bad delay", Display: d}, admin, csrf, nil), 400)
}

func TestTabletRequiresHTTPSOutsideLoopbackDevelopment(t *testing.T) {
	a, _ := testApp(t)
	admin, csrf := tabletAdmin(t, a)
	a.cfg.AppURL = "http://public.example"
	tabletExpect(t, tabletCall(a, "POST", "/api/tablets", tabletConfig{Name: "Unsafe"}, admin, csrf, nil), 400)
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/pair", map[string]string{"code": "ABCD-EFGH-JKLM"}, "", "", nil), 400)
}
func TestTabletRevokeAndCompletionHaveOneTransactionOrder(t *testing.T) {
	a, _ := testApp(t)
	admin, csrf := tabletAdmin(t, a)
	task := newID()
	day := a.timeNow().In(a.loc).Format("2006-01-02")
	if e := a.db.Exec("INSERT INTO tasks(id,title,due_date) VALUES(?,?,?)", task, "Test", day); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 10; i++ {
		id, code := createTestTablet(t, a, admin, csrf, true)
		cookie, tcsrf := pairTestTablet(t, a, code)
		var wg sync.WaitGroup
		wg.Add(2)
		done := make(chan int, 1)
		revoked := make(chan int, 1)
		go func() {
			defer wg.Done()
			done <- tabletCall(a, "POST", "/api/tablet/tasks/"+task+"/toggle", map[string]any{"day": day, "done": true}, cookie, tcsrf, nil).status
		}()
		go func() {
			defer wg.Done()
			revoked <- tabletCall(a, "POST", "/api/tablets/"+id+"/revoke", map[string]any{}, admin, csrf, nil).status
		}()
		wg.Wait()
		status := <-done
		if status != 200 && status != 401 {
			t.Fatalf("completion raced incorrectly: %d", status)
		}
		if <-revoked != 200 {
			t.Fatal("revoke failed")
		}
		// A request started AFTER revoke has completed can never mutate anything.
		tabletExpect(t, tabletCall(a, "POST", "/api/tablet/tasks/"+task+"/toggle", map[string]any{"day": day, "done": false}, cookie, tcsrf, nil), 401)
	}
}
