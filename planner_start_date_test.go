package main

import (
	"bytes"
	"encoding/json"
	"image/png"
	"io"
	"net/http/httptest"
	"testing"
	"time"
)

func startDateFixture(t *testing.T) (*App, PlannerRequest) {
	t.Helper()
	a, _ := testApp(t)
	people := []string{newID(), newID()}
	for _, id := range people {
		if err := a.db.Exec("INSERT INTO people(id,name,color) VALUES(?,?,?)", id, "Test teenager", "#3999aa"); err != nil {
			t.Fatal(err)
		}
	}
	choices := []PlannerChoice{}
	for _, v := range []struct{ rule, minutes, points, times string }{{"daily", "15", "4", "7"}, {"weekly_window", "30", "8", "1"}, {"biweekly_window", "15", "4", "1"}} {
		id := newID()
		if err := a.db.Exec("INSERT INTO task_templates(id,title,repeat_rule,minutes,points,times_per_week) VALUES(?,?,?,?,?,?)", id, v.rule, v.rule, v.minutes, v.points, v.times); err != nil {
			t.Fatal(err)
		}
		choices = append(choices, PlannerChoice{TemplateID: id})
	}
	return a, PlannerRequest{People: people, Choices: choices, StartDate: "2026-10-29", RotateWeeks: 1}
}

func TestPlannerAcceptsEveryStartWeekday(t *testing.T) {
	a, input := startDateFixture(t)
	for _, date := range []string{"2026-10-26", "2026-10-27", "2026-10-28", "2026-10-29", "2026-10-30", "2026-10-31", "2026-11-01", "2026-03-28", "2026-12-31"} {
		t.Run(date, func(t *testing.T) {
			input.StartDate = date
			p, err := a.planPreview(input)
			if err != nil {
				t.Fatal(err)
			}
			if p.StartDate != date || len(p.Assignments) != 9 {
				t.Fatalf("unexpected preview: %+v", p)
			}
			start, _ := time.Parse("2006-01-02", date)
			for _, task := range p.Assignments {
				if task.DueDate < date || task.DueDate > start.AddDate(0, 0, 6).Format("2006-01-02") {
					t.Fatalf("first occurrence outside chosen first seven days: %+v", task)
				}
			}
			want := weekStart(start).AddDate(0, 0, 7).Format("2006-01-02")
			if p.FirstRotationDate != want {
				t.Fatalf("rotation: got %s, want %s", p.FirstRotationDate, want)
			}
		})
	}
	for _, bad := range []string{"", "2026-02-30", "29-10-2026"} {
		input.StartDate = bad
		if _, err := a.planPreview(input); err == nil {
			t.Fatalf("accepted invalid date %q", bad)
		}
	}
	rows, err := a.db.Query("SELECT id FROM tasks")
	if err != nil || len(rows) != 0 {
		t.Fatalf("preview wrote data: %v %v", rows, err)
	}
}

func TestPlannerMidweekPersistenceAndRotation(t *testing.T) {
	a, input := startDateFixture(t)
	p, err := a.planPreview(input)
	if err != nil {
		t.Fatal(err)
	}
	points, minutes := 0, 0
	for _, id := range input.People {
		points += p.FirstWeekTotals[id]
		minutes += p.FirstWeekMinutes[id]
	}
	if points != 28 || minutes != 105 || p.FirstWeekEnd != "2026-11-01" {
		t.Fatalf("first partial week incorrect: %+v", p)
	}
	server := httptest.NewServer(securityHeaders(a.mux))
	defer server.Close()
	cookie, csrf := authenticated(t, server)
	payload, _ := json.Marshal(input)
	status, saved, _ := req(t, server, "POST", "/api/planner/apply", string(payload), cookie, csrf)
	if status != 200 || saved["start_date"] != "2026-10-29" {
		t.Fatalf("save: %d %v", status, saved)
	}
	rows, err := a.db.Query("SELECT id,rotation_start FROM tasks")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row["rotation_start"] != input.StartDate {
			t.Fatalf("start was not persisted: %v", row)
		}
	}
	if err := a.db.Init(); err != nil {
		t.Fatal(err)
	}
	for _, day := range []string{"2026-10-25", "2026-10-26", "2026-10-27", "2026-10-28"} {
		tasks, err := a.tasks(day)
		if err != nil || len(tasks) != 0 {
			t.Fatalf("tasks before start on %s: %v %v", day, tasks, err)
		}
	}
	tasks, err := a.tasks("2026-10-29")
	if err != nil || len(tasks) != 3 {
		t.Fatalf("chosen day: %v %v", tasks, err)
	}
	var weekly Task
	for _, task := range tasks {
		if task.RepeatRule == "weekly_window" {
			weekly = task
		}
	}
	if weekly.ID == "" {
		t.Fatal("weekly task missing on selected start date")
	}
	status, _, _ = req(t, server, "POST", "/api/tasks/"+weekly.ID+"/toggle", `{"day":"2026-10-28","done":true}`, cookie, csrf)
	if status != 400 {
		t.Fatalf("completion before start should fail: %d", status)
	}
	status, _, _ = req(t, server, "POST", "/api/tasks/"+weekly.ID+"/toggle", `{"day":"2026-10-29","done":true}`, cookie, csrf)
	if status != 200 {
		t.Fatalf("completion on start day: %d", status)
	}
	for _, tc := range []struct {
		day           string
		changed, done bool
	}{{"2026-11-01", false, true}, {"2026-11-02", true, false}, {"2026-11-08", true, false}, {"2026-11-09", false, false}} {
		tasks, err := a.tasks(tc.day)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, task := range tasks {
			if task.ID == weekly.ID {
				found = true
				if (task.AssignedTo != weekly.AssignedTo) != tc.changed || task.Done != tc.done {
					t.Fatalf("rotation/completion %s: %+v", tc.day, task)
				}
			}
		}
		if !found {
			t.Fatalf("weekly task absent %s", tc.day)
		}
	}
	code, week, _ := req(t, server, "GET", "/api/tasks/week?start=2026-10-26", "", cookie, "")
	if code != 200 {
		t.Fatal(week)
	}
	for _, entry := range week["entries"].([]any) {
		if entry.(map[string]any)["day"].(string) < input.StartDate {
			t.Fatalf("API returned occurrence before start: %v", entry)
		}
	}
	before, _ := time.ParseInLocation("2006-01-02", "2026-10-28", a.loc)
	pending, err := a.pendingTasks(before, 7, true)
	if err != nil || len(pending) != 0 {
		t.Fatalf("reminders before start: %v %v", pending, err)
	}
}

func TestPlannerStartWithoutRotationAndLegacyWholeWeek(t *testing.T) {
	a, input := startDateFixture(t)
	input.RotateWeeks = 0
	p, err := a.planPreview(input)
	if err != nil {
		t.Fatal(err)
	}
	if p.FirstRotationDate != "" {
		t.Fatal("non-rotating plan should not announce a swap")
	}
	server := httptest.NewServer(securityHeaders(a.mux))
	defer server.Close()
	cookie, csrf := authenticated(t, server)
	raw, _ := json.Marshal(input)
	code, data, _ := req(t, server, "POST", "/api/planner/apply", string(raw), cookie, csrf)
	if code != 200 {
		t.Fatal(data)
	}
	tasks, err := a.tasks("2026-10-28")
	if err != nil || len(tasks) != 0 {
		t.Fatalf("non-rotating tasks leaked before start: %v %v", tasks, err)
	}
	id := newID()
	if err := a.db.Exec("INSERT INTO tasks(id,title,due_date,repeat_rule) VALUES(?,?,?,?)", id, "Legacy weekly task", "2026-10-29", "weekly_window"); err != nil {
		t.Fatal(err)
	}
	tasks, err = a.tasks("2026-10-26")
	if err != nil || len(tasks) != 1 || tasks[0].ID != id {
		t.Fatalf("legacy manual whole-week behavior changed: %v %v", tasks, err)
	}
}

func TestCalendarRotationFromMidweek(t *testing.T) {
	for _, tc := range []struct {
		start, day string
		weeks      int
		want       string
	}{
		{"2026-10-29", "2026-11-01", 1, "a"}, {"2026-10-29", "2026-11-02", 1, "b"},
		{"2026-10-29", "2026-11-08", 2, "a"}, {"2026-10-29", "2026-11-09", 2, "b"},
		{"2026-12-31", "2027-01-04", 1, "b"}, {"2026-10-24", "2026-10-26", 1, "b"},
	} {
		if got := rotatedOwner("a", `["a","b"]`, tc.start, tc.weeks, tc.day); got != tc.want {
			t.Fatalf("%+v: got %s", tc, got)
		}
	}
}

func TestFamilyHubIconAssets(t *testing.T) {
	a, _ := testApp(t)
	server := httptest.NewServer(securityHeaders(a.mux))
	defer server.Close()
	for _, path := range []string{"/favicon.ico", "/icon.svg", "/apple-touch-icon.png"} {
		resp, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("icon %s: %d %v", path, resp.StatusCode, err)
		}
		if path == "/favicon.ico" && !bytes.HasPrefix(data, []byte{0, 0, 1, 0}) {
			t.Fatal("invalid ICO file")
		}
		if path == "/apple-touch-icon.png" {
			cfg, err := png.DecodeConfig(bytes.NewReader(data))
			if err != nil || cfg.Width != 180 || cfg.Height != 180 {
				t.Fatalf("invalid touch icon: %+v %v", cfg, err)
			}
		}
	}
}
