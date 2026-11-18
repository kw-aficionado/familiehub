package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kw-aficionado/familiehub/internal/store"
)

func followupFixture(t *testing.T) (*App, string, string) {
	t.Helper()
	a, _ := testApp(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	p1, p2 := newID(), newID()
	for i, id := range []string{p1, p2} {
		if e := a.db.Exec("INSERT INTO people(id,name,color) VALUES(?,?,?)", id, fmt.Sprint("Test child ", i), "#298393"); e != nil {
			t.Fatal(e)
		}
	}
	return a, p1, p2
}
func followupEnable(t *testing.T, a *App, from string) {
	t.Helper()
	if e := a.db.Exec("INSERT INTO planner_meta(key,value) VALUES('followup_from',?)", from); e != nil {
		t.Fatal(e)
	}
}
func followupTask(t *testing.T, a *App, owner, from, rule, policy string) string {
	t.Helper()
	id := newID()
	if e := a.db.Exec("INSERT INTO tasks(id,title,assigned_to,due_date,repeat_rule,rotation_start,followup_policy,effort_points,effort_minutes) VALUES(?,?,?,?,?,?,?,?,?)", id, "Fictional chore", owner, from, rule, from, policy, "4", "15"); e != nil {
		t.Fatal(e)
	}
	return id
}
func boardFor(t *testing.T, a *App) FollowupBoard {
	t.Helper()
	var b FollowupBoard
	e := a.db.Transaction(func(tx *store.Tx) error { var e error; b, e = followupBoardTx(tx, a.timeNow(), true); return e })
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func actionFor(o FollowupOccurrence, action string) followupAction {
	return followupAction{TaskID: o.TaskID, Key: o.Key, Version: o.Version, Action: action, RequestID: newID()}
}
func applyFor(a *App, v followupAction) (FollowupOccurrence, error) {
	var o FollowupOccurrence
	e := a.db.Transaction(func(tx *store.Tx) error {
		var e error
		o, e = applyFollowupAction(tx, v, "test-admin", true, "", false, a.timeNow())
		return e
	})
	return o, e
}
func occurrenceFor(t *testing.T, a *App, id, key string) FollowupOccurrence {
	t.Helper()
	rows, e := a.db.Query("SELECT * FROM followup_occurrences WHERE task_id=? AND occurrence_key=?", id, key)
	if e != nil || len(rows) != 1 {
		t.Fatalf("missing occurrence %s %s: %v %v", id, key, rows, e)
	}
	return followupRow(rows[0])
}
func TestFollowupIsOptInAndCreatesNoTaskCopies(t *testing.T) {
	a, p, _ := followupFixture(t)
	followupTask(t, a, p, "2026-11-16", "daily", "carry")
	if b := boardFor(t, a); b.Settings.From != "" || len(b.Rest) != 0 {
		t.Fatal(b)
	}
	rows, _ := a.db.Query("SELECT * FROM followup_occurrences")
	if len(rows) != 0 {
		t.Fatal("unrequested activation")
	}
	followupEnable(t, a, "2026-11-17")
	for i := 0; i < 3; i++ {
		b := boardFor(t, a)
		if len(b.Rest) != 1 || b.Rest[0].Key != "2026-11-17" {
			t.Fatal(b)
		}
	}
	rows, _ = a.db.Query("SELECT id FROM tasks")
	if len(rows) != 1 {
		t.Fatal("followup duplicated task series")
	}
	rows, _ = a.db.Query("SELECT * FROM followup_occurrences")
	if len(rows) != 2 {
		t.Fatalf("expected 17th/18th, got %d", len(rows))
	}
}
func TestFollowupPoliciesAndIndependentPersonalTasks(t *testing.T) {
	a, p1, p2 := followupFixture(t)
	followupEnable(t, a, "2026-11-16")
	carry := followupTask(t, a, p1, "2026-11-16", "none", "carry")
	routine := followupTask(t, a, p1, "2026-11-16", "daily", "routine")
	followupTask(t, a, p2, "2026-11-16", "daily", "routine") // same title, different personal series
	date := followupTask(t, a, p1, "2026-11-16", "daily", "date")
	b := boardFor(t, a)
	if len(b.Rest) != 1 || b.Rest[0].TaskID != carry || len(b.Routines) != 2 {
		t.Fatalf("bad policies: %+v", b)
	}
	for _, g := range b.Routines {
		if g.Missing != 2 {
			t.Fatal(g)
		}
	}
	old := occurrenceFor(t, a, routine, "2026-11-16")
	o, e := applyFor(a, actionFor(old, "complete"))
	if e != nil || o.PerformedOn != "2026-11-18" {
		t.Fatalf("%+v %v", o, e)
	}
	today, _ := a.tasks("2026-11-18")
	for _, task := range today {
		if task.ID == routine && task.Done {
			t.Fatal("old completion also completed today")
		}
	}
	priorDate := occurrenceFor(t, a, date, "2026-11-16")
	if priorDate.Status != "open" {
		t.Fatal("date-specific miss silently completed")
	}
}
func TestFollowupRotationDeadlineAndCustomWeeks(t *testing.T) {
	a, p1, p2 := followupFixture(t)
	followupEnable(t, a, "2026-11-09")
	id := followupTask(t, a, p1, "2026-11-09", "weekly_window", "carry")
	members, _ := json.Marshal([]string{p1, p2})
	a.db.Exec("UPDATE tasks SET rotation_members=?,rotation_weeks=1 WHERE id=?", string(members), id)
	b := boardFor(t, a)
	if len(b.Rest) != 1 {
		t.Fatalf("week not expired correctly: %+v", b.Rest)
	}
	old := b.Rest[0]
	if old.DueOn != "2026-11-15" || old.OriginalOwner != p1 || old.AssignedTo != p1 {
		t.Fatal(old)
	}
	thisWeek := occurrenceFor(t, a, id, "2026-11-16")
	if thisWeek.AssignedTo != p2 || thisWeek.DueOn != "2026-11-22" {
		t.Fatal(thisWeek)
	}
	// A Sunday start has a Saturday deadline and is a distinct persisted window.
	id2 := followupTask(t, a, p2, "2026-11-11", "weekly_window", "carry")
	a.db.Exec("UPDATE tasks SET week_start=0 WHERE id=?", id2)
	boardFor(t, a)
	o := occurrenceFor(t, a, id2, "2026-11-08")
	if o.DueOn != "2026-11-14" || o.StartsOn != "2026-11-11" {
		t.Fatal(o)
	}
}
func TestFollowupPureOccurrenceBoundaries(t *testing.T) {
	r := map[string]string{"id": newID(), "due_date": "2026-03-27", "rotation_start": "2026-03-27", "assigned_to": "person", "repeat_rule": "weekdays", "week_start": "1"}
	for _, tc := range []struct {
		day     string
		visible bool
	}{{"2026-03-26", false}, {"2026-03-27", true}, {"2026-03-28", false}, {"2026-03-29", false}, {"2026-03-30", true}} {
		o, ok := newFollowupOccurrence(r, nil, tc.day)
		if ok != tc.visible {
			t.Fatalf("%s: %+v %v", tc.day, o, ok)
		}
	}
	r["repeat_rule"] = "biweekly_window"
	r["due_date"] = "2026-12-28"
	r["rotation_start"] = "2026-12-30"
	for _, tc := range []struct {
		day, key, due string
		visible       bool
	}{{"2026-12-29", "", "", false}, {"2026-12-30", "2026-12-28", "2027-01-03", true}, {"2027-01-05", "", "", false}, {"2027-01-12", "2027-01-11", "2027-01-17", true}} {
		o, ok := newFollowupOccurrence(r, nil, tc.day)
		if ok != tc.visible || ok && (o.Key != tc.key || o.DueOn != tc.due) {
			t.Fatalf("%s: %+v %v", tc.day, o, ok)
		}
	}
	if followupDay(time.Date(2026, 7, 1, 22, 30, 0, 0, time.UTC)) != "2026-07-02" {
		t.Fatal("not Copenhagen")
	}
}
func TestFollowupSnapshotsEffortPolicyAndOwner(t *testing.T) {
	a, p1, p2 := followupFixture(t)
	followupEnable(t, a, "2026-11-16")
	id := followupTask(t, a, p1, "2026-11-16", "daily", "carry")
	boardFor(t, a)
	a.db.Exec("UPDATE tasks SET assigned_to=?,effort_points=9,followup_policy='date' WHERE id=?", p2, id)
	boardFor(t, a)
	old := occurrenceFor(t, a, id, "2026-11-16")
	if old.Points != 4 || old.OriginalOwner != p1 || old.Policy != "carry" {
		t.Fatal("immutable fields changed", old)
	}
	a.now = func() time.Time { return time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC) }
	boardFor(t, a)
	next := occurrenceFor(t, a, id, "2026-11-19")
	if next.Points != 9 || next.OriginalOwner != p2 || next.Policy != "date" {
		t.Fatal(next)
	}
}
func TestFollowupActionsAreDistinctAndAudited(t *testing.T) {
	a, p1, p2 := followupFixture(t)
	followupEnable(t, a, "2026-11-16")
	id := followupTask(t, a, p1, "2026-11-16", "none", "carry")
	o := boardFor(t, a).Rest[0]
	v := actionFor(o, "help")
	o, e := applyFor(a, v)
	if e != nil || !o.HelpRequested {
		t.Fatal(o, e)
	}
	v = actionFor(o, "defer")
	v.Date = "2026-11-20"
	o, e = applyFor(a, v)
	if e != nil || o.DueOn != "2026-11-16" || o.DeferredUntil != v.Date {
		t.Fatal(o, e)
	}
	pending, e := a.pendingTasks(a.timeNow(), 7, true)
	if e != nil || len(pending) != 0 {
		t.Fatal("deferred reminder", pending, e)
	}
	v = actionFor(o, "reassign")
	v.PersonID = p2
	o, e = applyFor(a, v)
	if e != nil || o.OriginalOwner != p1 || o.AssignedTo != p2 {
		t.Fatal(o, e)
	}
	v = actionFor(o, "complete")
	v.Date = "2026-11-17"
	v.PersonID = p2
	o, e = applyFor(a, v)
	if e != nil || o.Status != "completed" || o.PerformedBy != p2 {
		t.Fatal(o, e)
	}
	b := boardFor(t, a)
	if len(b.Rest) != 0 || len(b.Summary) != 1 || b.Summary[0].Points["late"] != 4 || b.Summary[0].PersonID != p1 {
		t.Fatal("stats count days not occurrence", b)
	}
	audit, _ := a.db.Query("SELECT * FROM followup_audit WHERE task_id=?", id)
	if len(audit) != 4 {
		t.Fatalf("want 4 audit actions, got %d", len(audit))
	}
}
func TestFollowupExcuseAndReplacementNeverCountAsDone(t *testing.T) {
	for _, action := range []string{"excuse", "supersede"} {
		t.Run(action, func(t *testing.T) {
			a, p, _ := followupFixture(t)
			followupEnable(t, a, "2026-11-16")
			id := followupTask(t, a, p, "2026-11-16", "daily", "carry")
			o := boardFor(t, a).Rest[0]
			v := actionFor(o, action)
			if _, e := applyFor(a, v); e == nil {
				t.Fatal("missing reason accepted")
			}
			v.Note = "Not needed this time"
			o, e := applyFor(a, v)
			if e != nil {
				t.Fatal(e)
			}
			completed, _ := a.db.Query("SELECT day FROM completions WHERE task_id=? AND day=?", id, o.Key)
			if len(completed) != 0 {
				t.Fatal("waiver became completion")
			}
			e = a.db.Transaction(func(tx *store.Tx) error { return writeTaskCompletionAt(tx, id, o.Key, true, a.timeNow(), "test") })
			if e == nil {
				t.Fatal("checkbox overwrote reviewed outcome")
			}
			tomorrow := occurrenceFor(t, a, id, "2026-11-18")
			if tomorrow.Status != "open" {
				t.Fatal("series changed")
			}
		})
	}
}
func TestFollowupLegacyCompletionTimeIsNotInvented(t *testing.T) {
	a, p, _ := followupFixture(t)
	id := followupTask(t, a, p, "2026-11-16", "none", "carry")
	a.db.Exec("INSERT INTO completions(task_id,day) VALUES(?,?)", id, "2026-11-16")
	followupEnable(t, a, "2026-11-16")
	b := boardFor(t, a)
	if len(b.Rest) != 0 || b.Summary[0].Counts["completed_unknown"] != 1 {
		t.Fatal(b)
	}
}
func TestFollowupCurrentCheckboxKeepsPreviousMisses(t *testing.T) {
	a, p, _ := followupFixture(t)
	followupEnable(t, a, "2026-11-16")
	id := followupTask(t, a, p, "2026-11-16", "daily", "routine")
	if e := a.db.Transaction(func(tx *store.Tx) error {
		return writeTaskCompletionAt(tx, id, "2026-11-18", true, a.timeNow(), "test")
	}); e != nil {
		t.Fatal(e)
	}
	b := boardFor(t, a)
	if b.Routines[0].Missing != 2 {
		t.Fatal(b)
	}
	o := occurrenceFor(t, a, id, "2026-11-18")
	if o.PerformedOn != "2026-11-18" || o.Status != "completed" {
		t.Fatal(o)
	}
	if e := a.db.Transaction(func(tx *store.Tx) error {
		return writeTaskCompletionAt(tx, id, "2026-11-18", false, a.timeNow(), "test")
	}); e != nil {
		t.Fatal(e)
	}
	if occurrenceFor(t, a, id, "2026-11-18").Status != "open" {
		t.Fatal("undo not recorded")
	}
}
func TestFollowupIdempotentConcurrentActionAndConflict(t *testing.T) {
	a, p, _ := followupFixture(t)
	followupEnable(t, a, "2026-11-16")
	id := followupTask(t, a, p, "2026-11-16", "none", "carry")
	o := boardFor(t, a).Rest[0]
	v := actionFor(o, "complete")
	var wg sync.WaitGroup
	errors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := applyFor(a, v); errors <- e }()
	}
	wg.Wait()
	close(errors)
	for e := range errors {
		if e != nil {
			t.Fatal(e)
		}
	}
	audit, _ := a.db.Query("SELECT id FROM followup_audit WHERE task_id=?", id)
	if len(audit) != 1 {
		t.Fatal("duplicate audit/completion")
	}
	v.Note = "different request"
	if _, e := applyFor(a, v); e == nil {
		t.Fatal("reused request id accepted for different content")
	}
	v = actionFor(o, "excuse")
	v.Note = "stale"
	if _, e := applyFor(a, v); e == nil {
		t.Fatal("stale decision accepted")
	}
}
func TestFollowupConcurrentCheckboxAndExcusePreserveOneOutcome(t *testing.T) {
	a, p, _ := followupFixture(t)
	followupEnable(t, a, "2026-11-16")
	for i := 0; i < 15; i++ {
		id := followupTask(t, a, p, "2026-11-16", "none", "carry")
		boardFor(t, a)
		o := occurrenceFor(t, a, id, "2026-11-16")
		v := actionFor(o, "excuse")
		v.Note = "testing concurrency"
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); applyFor(a, v) }()
		go func() {
			defer wg.Done()
			a.db.Transaction(func(tx *store.Tx) error {
				return writeTaskCompletionAt(tx, id, "2026-11-16", true, a.timeNow(), "test")
			})
		}()
		wg.Wait()
		o = occurrenceFor(t, a, id, "2026-11-16")
		rows, _ := a.db.Query("SELECT day FROM completions WHERE task_id=?", id)
		if o.Status == "completed" && len(rows) != 1 || o.Status == "excused" && len(rows) != 0 {
			t.Fatal("mixed state", o, len(rows))
		}
	}
}
func TestFollowupAPIPermissionsTabletScopeAndRevocation(t *testing.T) {
	a, p, _ := followupFixture(t)
	admin, csrf := tabletAdmin(t, a)
	tabletExpect(t, tabletCall(a, "PUT", "/api/followup/settings", FollowupSettings{From: "2026-11-16", RedAfter: 2}, admin, "", nil), 403)
	tabletExpect(t, tabletCall(a, "PUT", "/api/followup/settings", FollowupSettings{From: "2026-11-16", RedAfter: 2}, admin, csrf, nil), 200)
	tabletExpect(t, tabletCall(a, "PUT", "/api/followup/settings", FollowupSettings{From: "2026-11-17", RedAfter: 2}, admin, csrf, nil), 409)
	id := followupTask(t, a, p, "2026-11-16", "none", "carry")
	o := boardFor(t, a).Rest[0]
	device, code := createTestTablet(t, a, admin, csrf, true)
	cookie, token := pairTestTablet(t, a, code)
	v := actionFor(o, "complete")
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/followup/action", v, cookie, token, nil), 403) // can_complete does not imply can_followup
	tabletExpect(t, tabletCall(a, "PUT", "/api/tablets/"+device, tabletConfig{Name: "Test", CanComplete: true, CanFollowup: true}, admin, csrf, nil), 200)
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/followup/action", v, cookie, "", nil), 403)
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/followup/action", v, cookie, token, map[string]string{"Origin": "https://evil.invalid"}), 403)
	forbidden := v
	forbidden.Action = "excuse"
	forbidden.Note = "not allowed"
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/followup/action", forbidden, cookie, token, nil), 403)
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/followup/action", v, cookie, token, nil), 200)
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/followup/action", v, cookie, token, nil), 200)
	tabletExpect(t, tabletCall(a, "POST", "/api/tablets/"+device+"/revoke", map[string]string{}, admin, csrf, nil), 200)
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/followup/action", v, cookie, token, nil), 401)
	if occurrenceFor(t, a, id, o.Key).Status != "completed" {
		t.Fatal("completion lost")
	}
	tabletExpect(t, tabletCall(a, "GET", "/api/followup", nil, "", "", nil), 401)
}
func TestFollowupTabletRejectsRoutineHistoryFutureAndBackdating(t *testing.T) {
	a, p, _ := followupFixture(t)
	followupEnable(t, a, "2026-11-16")
	id := followupTask(t, a, p, "2026-11-16", "daily", "routine")
	boardFor(t, a)
	admin, csrf := tabletAdmin(t, a)
	created := tabletCall(a, "POST", "/api/tablets", tabletConfig{Name: "Test", CanFollowup: true}, admin, csrf, nil)
	tabletExpect(t, created, 201)
	cookie, token := pairTestTablet(t, a, created.data["pair_code"].(string))
	o := occurrenceFor(t, a, id, "2026-11-16")
	v := actionFor(o, "complete")
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/followup/action", v, cookie, token, nil), 403)
	carry := followupTask(t, a, p, "2026-11-16", "daily", "carry")
	boardFor(t, a)
	o = occurrenceFor(t, a, carry, "2026-11-18")
	v = actionFor(o, "complete")
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/followup/action", v, cookie, token, nil), 403)
	o = occurrenceFor(t, a, carry, "2026-11-16")
	v = actionFor(o, "complete")
	v.Date = "2026-11-16"
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/followup/action", v, cookie, token, nil), 403)
}
func TestFollowupCannotDeleteOrRewriteTrackedSeries(t *testing.T) {
	a, p, _ := followupFixture(t)
	followupEnable(t, a, "2026-11-16")
	id := followupTask(t, a, p, "2026-11-16", "none", "carry")
	boardFor(t, a)
	admin, csrf := tabletAdmin(t, a)
	tabletExpect(t, tabletCall(a, "DELETE", "/api/tasks/"+id, nil, admin, csrf, nil), 409)
	tabletExpect(t, tabletCall(a, "PUT", "/api/tasks/"+id, Task{Title: "changed", AssignedTo: p, DueDate: "2026-11-20", RepeatRule: "none"}, admin, csrf, nil), 409)
	// Additive migrations may be rerun without resetting records or permissions.
	if e := a.db.Init(); e != nil {
		t.Fatal(e)
	}
	if len(boardFor(t, a).Rest) != 1 {
		t.Fatal("migration erased followup")
	}
}
func TestFollowupInvalidActionRollsBackAndHistoryStaysPrivate(t *testing.T) {
	a, p, _ := followupFixture(t)
	followupEnable(t, a, "2026-11-16")
	id := followupTask(t, a, p, "2026-11-16", "none", "carry")
	o := boardFor(t, a).Rest[0]
	v := actionFor(o, "complete")
	v.PersonID = newID()
	if _, e := applyFor(a, v); e == nil {
		t.Fatal("invalid performer accepted")
	}
	rows, _ := a.db.Query("SELECT * FROM completions WHERE task_id=?", id)
	if len(rows) > 0 {
		t.Fatal("partial completion survived rollback")
	}
	v = actionFor(o, "complete")
	v.Date = "2026-11-19"
	if _, e := applyFor(a, v); e == nil {
		t.Fatal("future completion accepted")
	}
	e := a.db.Transaction(func(tx *store.Tx) error {
		_, e := applyFollowupAction(tx, actionFor(o, "excuse"), "member", false, p, false, a.timeNow())
		return e
	})
	if e == nil {
		t.Fatal("member can excuse")
	}
	admin, csrf := tabletAdmin(t, a)
	r := tabletCall(a, "POST", "/api/accounts", map[string]any{"username": "child", "password": "a-fixture-long-password", "person_id": p, "enabled": true}, admin, csrf, nil)
	tabletExpect(t, r, 200)
	login := tabletCall(a, "POST", "/api/login", map[string]string{"username": "child", "password": "a-fixture-long-password"}, "", "", nil)
	tabletExpect(t, login, 200)
	cookie := replyCookie(t, login, "fh_session")
	tabletExpect(t, tabletCall(a, "GET", "/api/followup/history?task_id="+id+"&key="+o.Key, nil, cookie, "", nil), 403)
	response := tabletCall(a, "GET", "/api/followup", nil, cookie, "", nil)
	tabletExpect(t, response, http.StatusOK)
	if strings.Contains(response.recorder.Body.String(), "\"review\"") {
		t.Fatal("adult review exposed")
	}
}

func TestFollowupGroupsOnlyKnownDailyPlannerShards(t *testing.T) {
	a, p, _ := followupFixture(t)
	followupEnable(t, a, "2026-11-16")
	plan, tpl := newID(), newID()
	if e := a.db.Exec("INSERT INTO task_templates(id,title,repeat_rule) VALUES(?,?,?)", tpl, "Daily template", "daily"); e != nil {
		t.Fatal(e)
	}
	var ids []string
	for _, day := range []string{"2026-11-16", "2026-11-17", "2026-11-18"} {
		id := followupTask(t, a, p, day, "weekly", "")
		ids = append(ids, id)
		if e := a.db.Exec("UPDATE tasks SET plan_id=?,template_id=? WHERE id=?", plan, tpl, id); e != nil {
			t.Fatal(e)
		}
	}
	b := boardFor(t, a)
	if len(b.Routines) != 1 || b.Routines[0].Missing != 2 || len(b.Routines[0].TaskIDs) != 3 {
		t.Fatalf("shards not grouped: %+v", b.Routines)
	}
	// A separately created plan with the identical template is still independent.
	id := followupTask(t, a, p, "2026-11-16", "weekly", "")
	if e := a.db.Exec("UPDATE tasks SET plan_id=?,template_id=? WHERE id=?", newID(), tpl, id); e != nil {
		t.Fatal(e)
	}
	b = boardFor(t, a)
	if len(b.Routines) != 2 {
		t.Fatalf("separate plans conflated: %+v", b.Routines)
	}
}

func TestFollowupChangingRedThresholdAfterNinetyDays(t *testing.T) {
	a, _, _ := followupFixture(t)
	followupEnable(t, a, "2026-01-01")
	cookie, csrf := tabletAdmin(t, a)
	r := tabletCall(a, "PUT", "/api/followup/settings", FollowupSettings{From: "2026-01-01", RedAfter: 3}, cookie, csrf, nil)
	tabletExpect(t, r, 200)
	r = tabletCall(a, "PUT", "/api/followup/settings", FollowupSettings{From: "2026-11-18", RedAfter: 3}, cookie, csrf, nil)
	tabletExpect(t, r, 409)
}

func TestFollowupManualArchiveTaskCapturesEffort(t *testing.T) {
	a, p, _ := followupFixture(t)
	cookie, csrf := tabletAdmin(t, a)
	tpl := tabletCall(a, "POST", "/api/templates", map[string]any{"title": "Archive chore", "repeat_rule": "daily", "points": 8, "minutes": 30, "followup_policy": "date"}, cookie, csrf, nil)
	tabletExpect(t, tpl, 200)
	task := tabletCall(a, "POST", "/api/tasks", map[string]any{"title": "Archive chore", "assigned_to": p, "due_date": "2026-11-16", "repeat_rule": "daily", "template_id": tpl.data["id"], "followup_policy": "date"}, cookie, csrf, nil)
	tabletExpect(t, task, 200)
	followupEnable(t, a, "2026-11-16")
	b := boardFor(t, a)
	if len(b.Rest) != 0 || len(b.Review) != 2 {
		t.Fatal(b)
	}
	for _, o := range b.Review {
		if o.Points != 8 || o.Minutes != 30 || o.Policy != "date" {
			t.Fatal(o)
		}
	}
}
