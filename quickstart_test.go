package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kw-aficionado/familiehub/internal/store"
)

func quickFixture(t *testing.T) (*App, QuickstartRequest) {
	t.Helper()
	a, _ := testApp(t)
	people := []string{newID(), newID()}
	for i, id := range people {
		if e := a.db.Exec("INSERT INTO people(id,name,color) VALUES(?,?,?)", id, []string{"Barn A", "Barn B"}[i], "#123456"); e != nil {
			t.Fatal(e)
		}
	}
	v := QuickstartRequest{StartDate: "2026-11-16", WeekStart: intPointer(1), planApproval: planApproval{RequestID: newID()}}
	for i, title := range []string{"Opvask", "Borddaekning"} {
		id := newID()
		if e := a.db.Exec("INSERT INTO task_templates(id,title,repeat_rule,points,minutes,times_per_week) VALUES(?,?,'daily',?,?,7)", id, title, []string{"4", "1"}[i], []string{"15", "5"}[i]); e != nil {
			t.Fatal(e)
		}
		v.Choices = append(v.Choices, QuickstartChoice{TemplateID: id, People: people, Starter: people[i], Mode: "rotate", RepeatRule: "daily", RotateWeeks: 1})
	}
	return a, v
}
func intPointer(v int) *int { return &v }
func previewQuick(t *testing.T, a *App, v *QuickstartRequest) QuickstartPreview {
	t.Helper()
	p, e := a.compileQuickstart(a.db, *v)
	if e != nil {
		t.Fatal(e)
	}
	v.PreviewHash = p.PreviewHash
	return p
}
func saveQuick(a *App, v QuickstartRequest, admin bool) (planReceipt, error) {
	return a.persistPlan(v.planApproval, admin, func(q scheduleReader) (planBundle, error) { p, e := a.compileQuickstart(q, v); return p.planBundle, e })
}
func TestQuickstartSharedRotationAndBurden(t *testing.T) {
	a, v := quickFixture(t)
	p := previewQuick(t, a, &v)
	if len(p.Tasks) != 2 || p.FirstRotationDate != "2026-11-23" {
		t.Fatalf("preview: %+v", p)
	}
	if p.Weeks[0].Points[v.Choices[0].Starter] != 28 || p.Weeks[0].Points[v.Choices[1].Starter] != 7 {
		t.Fatal("wrong frequency weighted burden")
	}
	if p.Weeks[1].Points[v.Choices[0].Starter] != 7 || p.Weeks[1].Minutes[v.Choices[1].Starter] != 105 {
		t.Fatal("week two did not rotate workload")
	}
	rows, _ := a.db.Query("SELECT id FROM tasks")
	if len(rows) != 0 {
		t.Fatal("preview wrote tasks")
	}
	result, e := saveQuick(a, v, true)
	if e != nil || result.Created != 2 {
		t.Fatalf("save %v %v", result, e)
	}
	for week := 0; week < 4; week++ {
		date, _ := time.Parse("2006-01-02", v.StartDate)
		date = date.AddDate(0, 0, 7*week)
		list, e := a.tasks(date.Format("2006-01-02"))
		if e != nil || len(list) != 2 {
			t.Fatalf("shared chores duplicated: %v %v", list, e)
		}
		for _, task := range list {
			if task.Title == "Opvask" && task.AssignedTo != v.Choices[0].People[week%2] {
				t.Fatalf("wrong starter or week: %+v", task)
			}
		}
	}
	before, _ := a.tasks("2026-11-15")
	if len(before) != 0 {
		t.Fatal("tasks before start")
	}
}
func TestQuickstartEveryWeekStartPreservesExistingPlans(t *testing.T) {
	for first := 0; first < 7; first++ {
		t.Run(time.Weekday(first).String(), func(t *testing.T) {
			a, v := quickFixture(t)
			v.WeekStart = intPointer(first)
			v.StartDate = "2026-11-18"
			p := previewQuick(t, a, &v)
			if _, e := saveQuick(a, v, true); e != nil {
				t.Fatal(e)
			}
			start, _ := time.Parse("2006-01-02", v.StartDate)
			next := weekStartOn(start, first).AddDate(0, 0, 7)
			if p.FirstRotationDate != next.Format("2006-01-02") {
				t.Fatal(p.FirstRotationDate)
			}
			if e := a.db.Exec("INSERT OR REPLACE INTO planner_meta(key,value) VALUES('week_start','2')"); e != nil {
				t.Fatal(e)
			}
			if e := a.db.Init(); e != nil {
				t.Fatal(e)
			}
			tasks, e := a.tasks(next.Format("2006-01-02"))
			if e != nil {
				t.Fatal(e)
			}
			for _, task := range tasks {
				if task.WeekStart != first {
					t.Fatal("global setting rewrote existing series")
				}
				if task.Title == "Opvask" && task.AssignedTo != v.Choices[0].People[1] {
					t.Fatal("wrong rotation boundary")
				}
			}
		})
	}
}
func TestQuickstartPersonalTasksRemainIndependent(t *testing.T) {
	a, v := quickFixture(t)
	v.Choices = v.Choices[:1]
	v.Choices[0].Mode = "each"
	v.Choices[0].Starter = ""
	v.Choices[0].RepeatRule = "weekdays"
	previewQuick(t, a, &v)
	result, e := saveQuick(a, v, true)
	if e != nil || result.Created != 2 {
		t.Fatalf("%+v %v", result, e)
	}
	tasks, _ := a.tasks(v.StartDate)
	if len(tasks) != 2 || tasks[0].AssignedTo == tasks[1].AssignedTo {
		t.Fatal("one personal chore per child is intentional")
	}
	saturday, _ := a.tasks("2026-11-21")
	if len(saturday) != 0 {
		t.Fatal("weekday chores on saturday")
	}
	if e := a.db.Exec("INSERT INTO completions(task_id,day) VALUES(?,?)", tasks[0].ID, v.StartDate); e != nil {
		t.Fatal(e)
	}
	tasks, _ = a.tasks(v.StartDate)
	done := 0
	for _, x := range tasks {
		if x.Done {
			done++
		}
	}
	if done != 1 {
		t.Fatal("personal completions were merged")
	}
}
func TestQuickstartSaveIsIdempotentAcrossConcurrentClients(t *testing.T) {
	a, v := quickFixture(t)
	previewQuick(t, a, &v)
	var wg sync.WaitGroup
	results := make(chan planReceipt, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := v
			if i%2 == 0 {
				req.RequestID = newID()
			}
			r, e := saveQuick(a, req, true)
			results <- r
			errs <- e
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	ids := map[string]bool{}
	for r := range results {
		ids[r.PlanID] = true
	}
	if len(ids) != 1 {
		t.Fatal("concurrent approvals created different plans")
	}
	tasks, _ := a.db.Query("SELECT id FROM tasks")
	if len(tasks) != 2 {
		t.Fatalf("created %d", len(tasks))
	}
	v.Choices[0].Starter = v.Choices[0].People[1]
	if _, e := saveQuick(a, v, true); e == nil {
		t.Fatal("same request ID accepted changed contents")
	}
}
func TestQuickstartStalePreviewAndInvalidInput(t *testing.T) {
	a, v := quickFixture(t)
	previewQuick(t, a, &v)
	if e := a.db.Exec("UPDATE task_templates SET points=9 WHERE id=?", v.Choices[0].TemplateID); e != nil {
		t.Fatal(e)
	}
	if _, e := saveQuick(a, v, true); e == nil {
		t.Fatal("stale preview saved")
	}
	rows, _ := a.db.Query("SELECT id FROM tasks")
	if len(rows) != 0 {
		t.Fatal("partial save")
	}
	for _, mutate := range []func(*QuickstartRequest){
		func(x *QuickstartRequest) { x.StartDate = "2026-02-30" },
		func(x *QuickstartRequest) { x.WeekStart = intPointer(7) },
		func(x *QuickstartRequest) { x.Choices[0].Starter = newID() },
		func(x *QuickstartRequest) {
			x.Choices[0].People = []string{x.Choices[0].People[0], x.Choices[0].People[0]}
		},
		func(x *QuickstartRequest) { x.Choices[0].Mode = "unknown" },
		func(x *QuickstartRequest) { x.Choices[0].RepeatRule = "bogus" },
		func(x *QuickstartRequest) { x.Choices[0].RotateWeeks = 3 },
		func(x *QuickstartRequest) { x.Choices[0].RotateWeeks = 0 },
		func(x *QuickstartRequest) { x.Choices[0].RepeatRule = "weekly"; x.Choices[0].Weekday = nil },
	} {
		raw, _ := json.Marshal(v)
		var copy QuickstartRequest
		_ = json.Unmarshal(raw, &copy)
		mutate(&copy)
		if _, e := a.compileQuickstart(a.db, copy); e == nil {
			t.Fatal("accepted invalid quickstart")
		}
	}
}
func TestQuickstartOverlapRequiresExplicitReviewAndSafeReplacement(t *testing.T) {
	a, v := quickFixture(t)
	v.StartDate = time.Now().In(a.loc).Format("2006-01-02")
	old := newID()
	if e := a.db.Exec("INSERT INTO tasks(id,title,assigned_to,due_date,repeat_rule) VALUES(?,?,?,'2026-01-01','daily')", old, "Opvask", v.Choices[0].People[0]); e != nil {
		t.Fatal(e)
	}
	p := previewQuick(t, a, &v)
	if len(p.Conflicts) != 1 {
		t.Fatalf("conflicts: %+v", p.Conflicts)
	}
	if _, e := saveQuick(a, v, true); e == nil {
		t.Fatal("unreviewed overlap saved")
	}
	v.ReplaceTaskIDs = []string{old}
	if _, e := saveQuick(a, v, false); e == nil {
		t.Fatal("member retired tasks")
	}
	yesterday := time.Now().In(a.loc).AddDate(0, 0, -1).Format("2006-01-02")
	if e := a.db.Exec("INSERT INTO completions(task_id,day) VALUES(?,?)", old, yesterday); e != nil {
		t.Fatal(e)
	}
	previewQuick(t, a, &v)
	r, e := saveQuick(a, v, true)
	if e != nil || r.Stopped != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	rows, _ := a.db.Query("SELECT day FROM completions WHERE task_id=?", old)
	if len(rows) != 1 {
		t.Fatal("history deleted")
	}
	tasks, _ := a.tasks(v.StartDate)
	if len(tasks) != 2 {
		t.Fatal("old/new overlap after replacement")
	}
	historical, _ := a.tasks(yesterday)
	if len(historical) != 1 || !historical[0].Done {
		t.Fatal("historical occurrence hidden")
	}
}
func TestQuickstartCompletedReplacementRollsBackAllWrites(t *testing.T) {
	a, v := quickFixture(t)
	v.StartDate = time.Now().In(a.loc).Format("2006-01-02")
	old := newID()
	_ = a.db.Exec("INSERT INTO tasks(id,title,due_date,repeat_rule) VALUES(?,'Opvask',?,'daily')", old, v.StartDate)
	_ = a.db.Exec("INSERT INTO completions(task_id,day) VALUES(?,?)", old, v.StartDate)
	previewQuick(t, a, &v)
	v.ReplaceTaskIDs = []string{old}
	if _, e := saveQuick(a, v, true); e == nil {
		t.Fatal("completed history retired")
	}
	rows, _ := a.db.Query("SELECT inactive_from FROM tasks")
	if len(rows) != 1 || rows[0]["inactive_from"] != "" {
		t.Fatal("partial replacement")
	}
	receipts, _ := a.db.Query("SELECT * FROM plan_receipts")
	if len(receipts) != 0 {
		t.Fatal("failed write has receipt")
	}
}
func TestLegacyPlannerRepeatedSaveDoesNotDuplicate(t *testing.T) {
	a, input := startDateFixture(t)
	save := func() (planReceipt, error) {
		return a.persistPlan(planApproval{}, true, func(q scheduleReader) (planBundle, error) {
			p, e := a.planPreviewWith(q, input)
			b := bundleFromPlanner(p, input)
			b.Conflicts = p.Conflicts
			b.PreviewHash = p.PreviewHash
			return b, e
		})
	}
	one, e := save()
	if e != nil {
		t.Fatal(e)
	}
	two, e := save()
	if e != nil || !two.Reused || one.PlanID != two.PlanID {
		t.Fatalf("%+v %v", two, e)
	}
	rows, _ := a.db.Query("SELECT id FROM tasks")
	if len(rows) != one.Created {
		t.Fatal("legacy approval duplicates")
	}
}
func TestWholeWeekCustomBoundaryCompletion(t *testing.T) {
	a, v := quickFixture(t)
	v.WeekStart = intPointer(0)
	v.Choices = v.Choices[:1]
	v.Choices[0].RepeatRule = "weekly_window"
	previewQuick(t, a, &v)
	if _, e := saveQuick(a, v, true); e != nil {
		t.Fatal(e)
	}
	tasks, _ := a.tasks(v.StartDate)
	if len(tasks) != 1 || tasks[0].OccurrenceKey != "2026-11-15" {
		t.Fatal(tasks)
	}
	_ = a.db.Exec("INSERT INTO completions(task_id,day) VALUES(?,?)", tasks[0].ID, tasks[0].OccurrenceKey)
	saturday, _ := a.tasks("2026-11-21")
	sunday, _ := a.tasks("2026-11-22")
	if len(saturday) != 1 || !saturday[0].Done || len(sunday) != 1 || sunday[0].Done || sunday[0].AssignedTo == saturday[0].AssignedTo {
		t.Fatal("custom boundary completion/rotation incorrect")
	}
}
func TestQuickstartBiweeklyAndOneOffPreviewCounts(t *testing.T) {
	a, v := quickFixture(t)
	v.Choices = v.Choices[:1]
	v.Choices[0].RepeatRule = "biweekly_window"
	p := previewQuick(t, a, &v)
	for i, w := range p.Weeks {
		want := 0
		if i%2 == 0 {
			want = 1
		}
		if len(w.Assignments) != want {
			t.Fatalf("week %d: %+v", i, w)
		}
	}
	v.Choices[0].RepeatRule = "none"
	p = previewQuick(t, a, &v)
	if len(p.Weeks[0].Assignments) != 1 || len(p.Weeks[1].Assignments) != 0 || p.Tasks[0].RotateWeeks != 0 {
		t.Fatal("one-off repeats or rotates")
	}
}
func TestQuickstartAPIAuthCSRFAndWeekSettings(t *testing.T) {
	a, v := quickFixture(t)
	server := httptest.NewServer(securityHeaders(a.mux))
	defer server.Close()
	cookie, csrf := authenticated(t, server)
	raw, _ := json.Marshal(v)
	code, _, _ := req(t, server, "POST", "/api/quickstart/preview", string(raw), "", "")
	if code != 401 {
		t.Fatal(code)
	}
	code, _, _ = req(t, server, "POST", "/api/quickstart/preview", string(raw), cookie, "")
	if code != 403 {
		t.Fatal(code)
	}
	code, preview, _ := req(t, server, "POST", "/api/quickstart/preview", string(raw), cookie, csrf)
	if code != 200 {
		t.Fatal(preview)
	}
	v.PreviewHash = preview["preview_hash"].(string)
	raw, _ = json.Marshal(v)
	code, saved, _ := req(t, server, "POST", "/api/quickstart/apply", string(raw), cookie, csrf)
	if code != 200 || saved["created"] != float64(2) {
		t.Fatalf("%d %v", code, saved)
	}
	code, _, _ = req(t, server, "PUT", "/api/schedule/settings", `{"week_start":0}`, cookie, csrf)
	if code != 200 {
		t.Fatal(code)
	}
	code, week, _ := req(t, server, "GET", "/api/tasks/week?start=2026-11-18", "", cookie, "")
	if code != 200 || week["start"] != "2026-11-15" {
		t.Fatal(week)
	}
	code, _, _ = req(t, server, "PUT", "/api/schedule/settings", `{"week_start":7}`, cookie, csrf)
	if code != 400 {
		t.Fatal(code)
	}
	code, _, _ = req(t, server, "PUT", "/api/schedule/settings", `{}`, cookie, csrf)
	if code != 400 {
		t.Fatal(code)
	}
}
func TestPendingRotationDoesNotChargeOneChildForAnotherWeek(t *testing.T) {
	a, v := quickFixture(t)
	v.Choices = v.Choices[:1]
	previewQuick(t, a, &v)
	if _, e := saveQuick(a, v, true); e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 11, 24, 18, 0, 0, 0, a.loc)
	items, e := a.pendingTasks(now, 7, true)
	if e != nil || len(items) != 2 {
		t.Fatalf("%+v %v", items, e)
	}
	counts := map[string]int{}
	for _, x := range items {
		counts[x.PersonID] = x.Missed
	}
	if counts[v.Choices[0].People[0]] != 6 || counts[v.Choices[0].People[1]] != 2 {
		t.Fatal(counts)
	}
	rows, _ := a.db.Query("SELECT id FROM tasks")
	_ = a.db.Exec("UPDATE tasks SET inactive_from='2026-11-24' WHERE id=?", rows[0]["id"])
	items, e = a.pendingTasks(now, 7, true)
	if e != nil || len(items) != 0 {
		t.Fatal("stopped series still sends reminders")
	}
}
func TestRetirementEndpointPreservesHistoryAndRejectsStaleSnapshot(t *testing.T) {
	a, v := quickFixture(t)
	v.StartDate = time.Now().In(a.loc).Format("2006-01-02")
	previewQuick(t, a, &v)
	_, e := saveQuick(a, v, true)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(securityHeaders(a.mux))
	defer server.Close()
	cookie, csrf := authenticated(t, server)
	_, d, _ := req(t, server, "GET", "/api/planner/diagnostics", "", cookie, "")
	items := d["tasks"].([]any)
	t0 := items[0].(map[string]any)
	id := t0["id"].(string)
	data := map[string]any{"ids": []string{id}, "from": v.StartDate, "snapshots": map[string]string{id: t0["snapshot"].(string)}}
	payload, _ := json.Marshal(data)
	code, _, _ := req(t, server, "POST", "/api/tasks/retire", string(payload), cookie, "")
	if code != 403 {
		t.Fatal(code)
	}
	_ = a.db.Exec("UPDATE tasks SET title='Changed' WHERE id=?", id)
	code, _, _ = req(t, server, "POST", "/api/tasks/retire", string(payload), cookie, csrf)
	if code != 409 {
		t.Fatal(code)
	}
	rows, _ := a.db.Query("SELECT "+taskColumns+" FROM tasks WHERE id=?", id)
	data["snapshots"] = map[string]string{id: jsonHash(taskFromRow(rows[0]))}
	payload, _ = json.Marshal(data)
	code, _, _ = req(t, server, "POST", "/api/tasks/retire", string(payload), cookie, csrf)
	if code != 200 {
		t.Fatal(code)
	}
	all, _ := a.db.Query("SELECT id FROM tasks")
	if len(all) != 2 {
		t.Fatal("retirement deleted rows")
	}
}
func TestPersistRollsBackOnDatabaseError(t *testing.T) {
	a, v := quickFixture(t)
	previewQuick(t, a, &v)
	if e := a.db.Exec("CREATE TRIGGER fail_insert BEFORE INSERT ON tasks WHEN NEW.title='Borddaekning' BEGIN SELECT RAISE(ABORT,'test-only failure'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e := saveQuick(a, v, true); e == nil {
		t.Fatal("expected failure")
	}
	rows, _ := a.db.Query("SELECT id FROM tasks")
	if len(rows) != 0 {
		t.Fatal("partial tasks")
	}
}
func TestWeekArithmeticAcrossDSTAndYear(t *testing.T) {
	for _, date := range []string{"2026-03-29", "2026-10-25", "2026-12-31", "2027-01-01"} {
		for first := 0; first < 7; first++ {
			d, _ := time.Parse("2006-01-02", date)
			w := weekStartOn(d, first)
			if int(w.Weekday()) != first || w.After(d) || d.Sub(w) >= 7*24*time.Hour {
				t.Fatal(date, first)
			}
		}
	}
}
func TestSharedSeriesCannotBeCorruptedByOldEditor(t *testing.T) {
	a, v := quickFixture(t)
	previewQuick(t, a, &v)
	_, e := saveQuick(a, v, true)
	if e != nil {
		t.Fatal(e)
	}
	tasks, _ := a.tasks(v.StartDate)
	server := httptest.NewServer(securityHeaders(a.mux))
	defer server.Close()
	cookie, csrf := authenticated(t, server)
	payload, _ := json.Marshal(tasks[0])
	code, body, _ := req(t, server, "PUT", "/api/tasks/"+tasks[0].ID, string(payload), cookie, csrf)
	if code != 409 || !strings.Contains(body["error"].(string), "Quickstart") {
		t.Fatal(code, body)
	}
}

// Assert that migrations add scheduling fields, never replace stored rows.
func TestSchedulingMigrationKeepsLegacyTaskAndCompletion(t *testing.T) {
	a, _ := testApp(t)
	id := newID()
	_ = a.db.Exec("INSERT INTO tasks(id,title,due_date,repeat_rule) VALUES(?,'Legacy','2026-11-16','daily')", id)
	_ = a.db.Exec("INSERT INTO completions(task_id,day) VALUES(?,'2026-11-16')", id)
	if e := a.db.Init(); e != nil {
		t.Fatal(e)
	}
	rows, e := a.db.Query("SELECT week_start FROM tasks WHERE id=?", id)
	if e != nil || len(rows) != 1 || rows[0]["week_start"] != "1" {
		t.Fatal(rows, e)
	}
	if e := a.db.Transaction(func(tx *store.Tx) error {
		done, e := tx.Query("SELECT day FROM completions WHERE task_id=?", id)
		if e == nil && len(done) != 1 {
			t.Fatal(done)
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
}

func TestApprovalRetryDoesNotMisreportAnEditedPlan(t *testing.T) {
	a, v := quickFixture(t)
	previewQuick(t, a, &v)
	r, e := saveQuick(a, v, true)
	if e != nil {
		t.Fatal(e)
	}
	_ = a.db.Exec("UPDATE tasks SET rotation_start='2026-11-23' WHERE plan_id=?", r.PlanID)
	if _, e := saveQuick(a, v, true); e == nil {
		t.Fatal("moved plan falsely acknowledged at old date")
	}
	rows, _ := a.db.Query("SELECT id FROM tasks")
	if len(rows) != 2 {
		t.Fatal("edited plan duplicated")
	}
}

func TestDuplicateArchiveTitlesCannotCreateTwoSharedChores(t *testing.T) {
	a, v := quickFixture(t)
	_ = a.db.Exec("UPDATE task_templates SET title='  OPVASK  ' WHERE id=?", v.Choices[1].TemplateID)
	if _, e := a.compileQuickstart(a.db, v); e == nil {
		t.Fatal("duplicate archive title created two shared chores")
	}
	v.Choices = v.Choices[:1]
	v.Choices[0].Mode = "each"
	v.Choices[0].Starter = ""
	if _, e := a.compileQuickstart(a.db, v); e != nil {
		t.Fatal("intentional personal chores rejected", e)
	}
}
