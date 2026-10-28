package main

import (
	"bytes"
	"encoding/json"
	"github.com/kw-aficionado/familiehub/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	db, e := store.Open(filepath.Join(dir, "family.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(db.Close)
	if e = db.Init(); e != nil {
		t.Fatal(e)
	}
	loc, _ := time.LoadLocation("Europe/Copenhagen")
	app := &App{db: db, cfg: Config{Password: "this-is-a-sample-long-secret", DataDir: dir, AppURL: "http://localhost:8080"}, key: bytes.Repeat([]byte{17}, 32), client: &http.Client{Timeout: 5 * time.Second}, mux: http.NewServeMux(), listeners: map[chan string]struct{}{}, attempts: map[string][]time.Time{}, loc: loc}
	app.routes()
	return app, dir
}
func req(t *testing.T, s *httptest.Server, method, path, body, cookie, csrf string) (int, map[string]any, *http.Response) {
	t.Helper()
	var in io.Reader
	if body != "" {
		in = strings.NewReader(body)
	}
	r, e := http.NewRequest(method, s.URL+path, in)
	if e != nil {
		t.Fatal(e)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	var parsed map[string]any
	if e = json.Unmarshal(raw, &parsed); e != nil {
		t.Fatalf("bad JSON: %s; %v", raw, e)
	}
	return resp.StatusCode, parsed, resp
}
func authenticated(t *testing.T, s *httptest.Server) (string, string) {
	t.Helper()
	code, d, r := req(t, s, "POST", "/api/login", `{"password":"this-is-a-sample-long-secret"}`, "", "")
	if code != 200 {
		t.Fatalf("login: %d, %v", code, d)
	}
	ck := r.Cookies()[0].Name + "=" + r.Cookies()[0].Value
	return ck, d["csrf"].(string)
}
func TestLoginCalendarAndTasks(t *testing.T) {
	a, dir := testApp(t)
	s := httptest.NewServer(securityHeaders(a.mux))
	defer s.Close()
	code, _, _ := req(t, s, "GET", "/api/state?day=2026-10-28&start=2026-10-28T00:00:00Z&end=2026-11-02T00:00:00Z", "", "", "")
	if code != 401 {
		t.Fatalf("state must be protected, got %d", code)
	}
	cookie, csrf := authenticated(t, s)
	code, _, _ = req(t, s, "POST", "/api/people", `{"name":"Barn 1","color":"#3999aa","notify_service":"","position":0}`, cookie, "")
	if code != 403 {
		t.Fatalf("mutation without CSRF not rejected: %d", code)
	}
	code, p, _ := req(t, s, "POST", "/api/people", `{"name":"Barn 1","color":"#3999aa","notify_service":"","position":0}`, cookie, csrf)
	if code != 200 {
		t.Fatalf("create person: %v", p)
	}
	personID := p["id"].(string)
	event := `{"title":"Leg med Oliver","start_at":"2026-10-28T14:00:00Z","end_at":"2026-10-28T16:00:00Z","kind":"legeaftale","person_id":"` + personID + `","pickup_by":"` + personID + `","location":"Skole","notes":"","external":false,"id":""}`
	code, created, _ := req(t, s, "POST", "/api/events", event, cookie, csrf)
	if code != 200 {
		t.Fatalf("create event: %v", created)
	}
	task := `{"title":"Dække bord","assigned_to":"` + personID + `","due_date":"2026-10-28","repeat_rule":"daily","done":false,"id":""}`
	code, createdTask, _ := req(t, s, "POST", "/api/tasks", task, cookie, csrf)
	if code != 200 {
		t.Fatalf("create task: %v", createdTask)
	}
	tid := createdTask["id"].(string)
	code, _, _ = req(t, s, "POST", "/api/tasks/"+tid+"/toggle", `{"day":"2026-10-28","done":true}`, cookie, csrf)
	if code != 200 {
		t.Fatalf("task completion: %d", code)
	}
	code, state, _ := req(t, s, "GET", "/api/state?day=2026-10-28&start=2026-10-28T00:00:00Z&end=2026-11-02T00:00:00Z", "", cookie, "")
	if code != 200 {
		t.Fatalf("state: %v", state)
	}
	if len(state["people"].([]any)) != 1 || len(state["events"].([]any)) != 1 || len(state["tasks"].([]any)) != 1 {
		t.Fatalf("state counts wrong: %v", state)
	}
	if !state["tasks"].([]any)[0].(map[string]any)["done"].(bool) {
		t.Fatalf("task should be completed")
	}
	next, err := a.tasks("2026-10-29")
	if err != nil || len(next) != 1 || next[0].Done {
		t.Fatalf("daily repeat must be incomplete next day: %+v %v", next, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "family.sqlite")); err != nil {
		t.Fatal(err)
	}
}
func TestHomeAssistantCalendarAndNotification(t *testing.T) {
	calls := 0
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing HA token")
		}
		if strings.Contains(r.URL.Path, "/api/calendars/") {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"summary":"Skolefest","start":"2026-10-28T15:00:00+01:00","end":"2026-10-28T16:00:00+01:00","location":"Skolen"}]`)
		} else if r.URL.Path == "/api/services/notify/mobile_app_test" {
			calls++
			io.WriteString(w, "[]")
		} else if r.URL.Path == "/api/" {
			io.WriteString(w, `{"message":"API running."}`)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer ha.Close()
	a, _ := testApp(t)
	a.cfg.HAURL = ha.URL
	a.cfg.HAToken = "test-token"
	a.cfg.HACalendars = []string{"calendar.familie"}
	srv := httptest.NewServer(a.mux)
	defer srv.Close()
	cookie, csrf := authenticated(t, srv)
	code, state, _ := req(t, srv, "GET", "/api/state?day=2026-10-28&start=2026-10-28T00:00:00Z&end=2026-10-29T00:00:00Z", "", cookie, "")
	if code != 200 {
		t.Fatalf("HA state: %v", state)
	}
	events := state["events"].([]any)
	if len(events) != 1 || events[0].(map[string]any)["external"] != true {
		t.Fatalf("external event missing: %v", events)
	}
	code, person, _ := req(t, srv, "POST", "/api/people", `{"name":"Forælder","color":"#339988","notify_service":"mobile_app_test","position":0}`, cookie, csrf)
	if code != 200 {
		t.Fatal(person)
	}
	b, _ := json.Marshal(map[string]string{"person_id": person["id"].(string)})
	code, v, _ := req(t, srv, "POST", "/api/notifications/test", string(b), cookie, csrf)
	if code != 200 || calls != 1 {
		t.Fatalf("notify: %v %d calls", v, calls)
	}
}
func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.sqlite")
	db, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if e := db.Init(); e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("INSERT INTO people(id,name,color,notify_service,position) VALUES(?,?,?,?,?)", "test", "Mor", "#112233", "", "1"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	again, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	rows, e := again.Query("SELECT name FROM people WHERE id=?", "test")
	if e != nil || len(rows) != 1 || rows[0]["name"] != "Mor" {
		t.Fatalf("persistence: %v %v", rows, e)
	}
}

func TestTaskReminderDedupe(t *testing.T) {
	calls := 0
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/services/notify/mobile_app_test" {
			calls++
			w.WriteHeader(200)
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ha.Close()
	a, _ := testApp(t)
	a.cfg.HAURL = ha.URL
	a.cfg.HAToken = "test-token"
	if e := a.db.Exec("INSERT INTO people(id,name,color,notify_service,position) VALUES(?,?,?,?,?)", "p", "Far", "#123456", "mobile_app_test", "0"); e != nil {
		t.Fatal(e)
	}
	if e := a.db.Exec("INSERT INTO tasks(id,title,assigned_to,due_date,repeat_rule) VALUES(?,?,?,?,?)", "t", "Gå ud med skrald", "p", "2026-10-28", "daily"); e != nil {
		t.Fatal(e)
	}
	when := time.Date(2026, 10, 28, 16, 1, 0, 0, a.loc)
	a.checkRemindersAt(when)
	a.checkRemindersAt(when.Add(time.Minute))
	if calls != 1 {
		t.Fatalf("expected a single reminder, got %d", calls)
	}
}
