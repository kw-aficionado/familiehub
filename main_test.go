package main

import (
	"bytes"
	"context"
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
func TestHomeAssistantCalendarOfficialPayload(t *testing.T) {
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path == "/api/" {
			io.WriteString(w, `{"message":"API running."}`)
			return
		}
		if r.URL.Path == "/api/calendars/calendar.familie" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[
                {"summary":"Legeaftale", "start":{"dateTime":"2026-10-28T15:00:00+01:00"},"end":{"dateTime":"2026-10-28T17:00:00+01:00"}},
                {"summary":"Fri", "start":{"date":"2026-10-29"},"end":{"date":"2026-10-30"}}
            ]`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ha.Close()
	a, _ := testApp(t)
	a.cfg.HAURL = ha.URL
	a.cfg.HAToken = "test-token"
	a.cfg.HACalendars = []string{"calendar.familie"}
	start, _ := time.Parse(time.RFC3339, "2026-10-28T00:00:00Z")
	end := start.Add(72 * time.Hour)
	events, err := a.haEvents(context.Background(), start, end)
	if err != nil || len(events) != 2 {
		t.Fatalf("official REST events missing: %+v, err %v", events, err)
	}
	if events[0].Start != "2026-10-28T15:00:00+01:00" || events[1].Start != "2026-10-29T00:00:00+01:00" {
		t.Fatalf("HA times parsed incorrectly: %+v", events)
	}
	s := httptest.NewServer(a.mux)
	defer s.Close()
	cookie, _ := authenticated(t, s)
	code, diagnostics, _ := req(t, s, "GET", "/api/integrations", "", cookie, "")
	if code != 200 || diagnostics["ha_online"] != true || diagnostics["calendar_ok"] != true {
		t.Fatalf("diagnostics: %v", diagnostics)
	}
}

func TestHomeAssistantDiagnosticsUnauthorizedAndInvalidCalendar(t *testing.T) {
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	defer ha.Close()
	a, _ := testApp(t)
	a.cfg.HAURL = ha.URL
	a.cfg.HAToken = "bad"
	a.cfg.HACalendars = []string{"calendar.familie"}
	s := httptest.NewServer(a.mux)
	defer s.Close()
	cookie, _ := authenticated(t, s)
	_, v, _ := req(t, s, "GET", "/api/integrations", "", cookie, "")
	if v["ha_online"] != false || !strings.Contains(v["ha_error"].(string), "401") {
		t.Fatalf("unauthorized should show 401: %v", v)
	}
	if strings.Contains(v["ha_error"].(string), "bad") {
		t.Fatalf("token leaked in error: %v", v)
	}
	a.cfg.HACalendars = []string{"sensor.not_a_calendar"}
	_, err := a.haEvents(context.Background(), time.Now(), time.Now().Add(time.Hour))
	if err == nil || !strings.Contains(haFriendlyError(err), "calendar.*") {
		t.Fatalf("invalid calendar: %v", err)
	}
}

func TestPersonNotifyFieldGuidance(t *testing.T) {
	a, _ := testApp(t)
	server := httptest.NewServer(a.mux)
	defer server.Close()
	cookie, csrf := authenticated(t, server)
	code, v, _ := req(t, server, "POST", "/api/people", `{"name":"Anton","color":"#ff8800","notify_service":"device_tracker.anton_iphone_12","position":0}`, cookie, csrf)
	if code != 400 || !strings.Contains(v["error"].(string), "device_tracker") {
		t.Fatalf("should explain notify field: %d %v", code, v)
	}
	code, v, _ = req(t, server, "POST", "/api/people", `{"name":"Anton","color":"#ff8800","notify_service":"","position":0}`, cookie, csrf)
	if code != 200 {
		t.Fatalf("notify empty should be accepted: %d %v", code, v)
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

func TestWeeklyAndBiweeklyChores(t *testing.T) {
	app, _ := testApp(t)
	ids := map[string]string{}
	for _, tc := range []struct{ title, rule string }{
		{"Kun denne uge", "once_week"},
		{"Hel uge hver uge", "weekly_window"},
		{"Hel uge hver anden uge", "biweekly_window"},
		{"Én dag hver anden uge", "biweekly"},
	} {
		id := newID()
		ids[tc.title] = id
		if err := app.db.Exec("INSERT INTO tasks(id,title,assigned_to,due_date,repeat_rule) VALUES(?,?,?,?,?)", id, tc.title, "", "2026-10-28", tc.rule); err != nil {
			t.Fatal(err)
		}
	}
	items := func(date string) map[string]Task {
		tasks, err := app.tasks(date)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]Task{}
		for _, task := range tasks {
			m[task.Title] = task
		}
		return m
	}
	inFirstWeek := items("2026-10-26")
	if len(inFirstWeek) != 3 {
		t.Fatalf("expected 3 whole-week tasks on Monday, got %+v", inFirstWeek)
	}
	if len(items("2026-10-27")) != 3 {
		t.Fatal("whole-week task not visible before selected Wednesday")
	}
	if len(items("2026-10-28")) != 4 {
		t.Fatal("all tasks should be visible on anchor Wednesday")
	}
	nextWeek := items("2026-11-02")
	if len(nextWeek) != 1 || nextWeek["Hel uge hver uge"].Title == "" {
		t.Fatalf("wrong weekly tasks for next week: %+v", nextWeek)
	}
	weekAfter := items("2026-11-09")
	if len(weekAfter) != 2 || weekAfter["Hel uge hver anden uge"].Title == "" {
		t.Fatalf("biweekly window should repeat after two weeks: %+v", weekAfter)
	}
	if _, ok := items("2026-11-04")["Én dag hver anden uge"]; ok {
		t.Fatal("biweekly single-day task must skip following Wednesday")
	}
	if _, ok := items("2026-11-11")["Én dag hver anden uge"]; !ok {
		t.Fatal("biweekly single-day task must repeat after 14 days")
	}

	srv := httptest.NewServer(app.mux)
	defer srv.Close()
	cookie, csrf := authenticated(t, srv)
	id := ids["Hel uge hver anden uge"]
	code, _, _ := req(t, srv, "POST", "/api/tasks/"+id+"/toggle", `{"day":"2026-10-28","done":true}`, cookie, csrf)
	if code != 200 {
		t.Fatalf("toggle week task: %d", code)
	}
	for _, d := range []string{"2026-10-26", "2026-10-29", "2026-11-01"} {
		if !items(d)["Hel uge hver anden uge"].Done {
			t.Fatalf("completion not shared in week for %s", d)
		}
	}
	if items("2026-11-09")["Hel uge hver anden uge"].Done {
		t.Fatal("completion leaked into next biweekly occurrence")
	}
	code, _, _ = req(t, srv, "POST", "/api/tasks/"+id+"/toggle", `{"day":"2026-11-02","done":true}`, cookie, csrf)
	if code != 400 {
		t.Fatalf("inactive week should reject completion, got %d", code)
	}
	code, _, _ = req(t, srv, "POST", "/api/tasks/"+id+"/toggle", `{"day":"2026-10-31","done":false}`, cookie, csrf)
	if code != 200 || items("2026-10-28")["Hel uge hver anden uge"].Done {
		t.Fatal("unchecking must reset completion for the whole week")
	}
	// Validate new choices through the public API, not just an internal database insert.
	code, _, _ = req(t, srv, "POST", "/api/tasks", `{"title":"Ny ugepligt","assigned_to":"","due_date":"2026-10-28","repeat_rule":"weekly_window","done":false,"id":""}`, cookie, csrf)
	if code != 200 {
		t.Fatalf("create weekly-window task over API: %d", code)
	}
}

func TestRecurrenceAcrossNewYear(t *testing.T) {
	anchor, err := time.Parse("2006-01-02", "2026-12-30")
	if err != nil {
		t.Fatal(err)
	}
	dates := []struct {
		date string
		want bool
	}{
		{"2026-12-28", true},
		{"2027-01-03", true},
		{"2027-01-04", false},
		{"2027-01-10", false},
		{"2027-01-11", true},
		{"2027-01-17", true},
		{"2027-01-18", false},
	}
	for _, tc := range dates {
		target, _ := time.Parse("2006-01-02", tc.date)
		got, _ := taskOccurrence(anchor, target, "biweekly_window")
		if got != tc.want {
			t.Fatalf("%s: biweekly_window got %t want %t", tc.date, got, tc.want)
		}
	}
}

func TestExternalCalendarHashtagRecognition(t *testing.T) {
	people := []Person{
		{ID: "anton-id", Name: "Anton", Color: "#fca311"},
		{ID: "alma-id", Name: "Alma", Color: "#db472f"},
		{ID: "anne-id", Name: "Anne Marie", Color: "#1188aa"},
	}
	tests := []struct {
		name, input, title, category, person string
		all                                  []string
	}{
		{name: "person and category", input: "#Anton #Sport Badminton", title: "Badminton", category: "sport", person: "anton-id", all: []string{"anton-id"}},
		{name: "two participants", input: "#Anton #Alma Biograf", title: "Biograf", person: "anton-id", all: []string{"anton-id", "alma-id"}},
		{name: "case insensitive", input: "#aNtOn #SKOLE Terminsprøve", title: "Terminsprøve", category: "skole", person: "anton-id", all: []string{"anton-id"}},
		{name: "name with spaces", input: "Aftensmad #AnneMarie", title: "Aftensmad", person: "anne-id", all: []string{"anne-id"}},
		{name: "category only", input: "#Sport Fodboldkamp", title: "Fodboldkamp", category: "sport"},
		{name: "unknown stays", input: "#Anton #Fremtid Svømning", title: "#Fremtid Svømning", person: "anton-id", all: []string{"anton-id"}},
		{name: "no tag", input: "Almas træning", title: "Almas træning"},
		{name: "partial person not recognized", input: "#Antonia Musik", title: "#Antonia Musik"},
		{name: "URL fragment is not a tag", input: "https://example.test/#Sport", title: "https://example.test/#Sport"},
		{name: "duplicate person", input: "#Alma #Alma #Hjemme Hjemmeopgave", title: "Hjemmeopgave", category: "hjemme", person: "alma-id", all: []string{"alma-id"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := decorateExternalEvent(Event{Title: tc.input, External: true}, people)
			if result.Title != tc.title || result.PersonID != tc.person || result.Category != tc.category {
				t.Fatalf("want title=%q person=%q category=%q, got %+v", tc.title, tc.person, tc.category, result)
			}
			if len(result.PersonIDs) != len(tc.all) {
				t.Fatalf("person ID count: want %v, got %v", tc.all, result.PersonIDs)
			}
			for i, id := range tc.all {
				if result.PersonIDs[i] != id {
					t.Fatalf("person IDs want %v, got %v", tc.all, result.PersonIDs)
				}
			}
		})
	}
}

func TestTaggedHAEventsInState(t *testing.T) {
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/calendars/calendar.familie" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"summary":"#Anton #Alma #Sport Badminton","start":{"dateTime":"2026-10-28T15:00:00+01:00"},"end":{"dateTime":"2026-10-28T17:00:00+01:00"}},{"summary":"Fælles aftale","start":{"date":"2026-10-29"},"end":{"date":"2026-10-30"}}]`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ha.Close()
	a, _ := testApp(t)
	a.cfg.HAURL = ha.URL
	a.cfg.HAToken = "private-test-token"
	a.cfg.HACalendars = []string{"calendar.familie"}
	server := httptest.NewServer(securityHeaders(a.mux))
	defer server.Close()
	cookie, csrf := authenticated(t, server)
	personIDs := []string{}
	for _, name := range []string{"Anton", "Alma"} {
		payload, _ := json.Marshal(Person{Name: name, Color: "#abcd12"})
		code, body, _ := req(t, server, "POST", "/api/people", string(payload), cookie, csrf)
		if code != 200 {
			t.Fatalf("create %s: %v", name, body)
		}
		personIDs = append(personIDs, body["id"].(string))
	}
	code, state, _ := req(t, server, "GET", "/api/state?day=2026-10-28&start=2026-10-28T00:00:00Z&end=2026-10-30T00:00:00Z", "", cookie, "")
	if code != 200 || state["warning"] != "" {
		t.Fatalf("state: %v", state)
	}
	events := state["events"].([]any)
	if len(events) != 2 {
		t.Fatalf("events: %v", events)
	}
	tagged := events[0].(map[string]any)
	if tagged["title"] != "Badminton" || tagged["category"] != "sport" || tagged["person_id"] != personIDs[0] {
		t.Fatalf("bad tag translation: %v", tagged)
	}
	ids := tagged["person_ids"].([]any)
	if len(ids) != 2 || ids[0] != personIDs[0] || ids[1] != personIDs[1] {
		t.Fatalf("people not assigned: %v", ids)
	}
	plain := events[1].(map[string]any)
	if plain["title"] != "Fælles aftale" || plain["person_id"] != "" {
		t.Fatalf("untagged should be unchanged: %v", plain)
	}
}

func TestBuildVersionAndStaticCache(t *testing.T) {
	a, _ := testApp(t)
	s := httptest.NewServer(securityHeaders(a.mux))
	defer s.Close()
	code, body, _ := req(t, s, "GET", "/api/version", "", "", "")
	if code != 200 || body["commit"] != buildCommit || body["version"] != "0.1.1" {
		t.Fatalf("unexpected build metadata: %v", body)
	}
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		resp, err := http.Get(s.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if !strings.Contains(resp.Header.Get("Cache-Control"), "no-store") {
			t.Errorf("%s should not be cacheable: %v", path, resp.Header)
		}
	}
}
