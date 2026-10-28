package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func arrayRequest(t *testing.T, s *httptest.Server, path, cookie string) (int, []map[string]any) {
	t.Helper()
	r, err := http.NewRequest("GET", s.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		return resp.StatusCode, nil
	}
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("JSON array response: %s: %v", raw, err)
	}
	return resp.StatusCode, items
}

func TestTaskArchiveAndAllChores(t *testing.T) {
	a, _ := testApp(t)
	s := httptest.NewServer(securityHeaders(a.mux))
	defer s.Close()

	if status, _ := arrayRequest(t, s, "/api/templates", ""); status != http.StatusUnauthorized {
		t.Fatalf("templates must require login: %d", status)
	}
	if status, _ := arrayRequest(t, s, "/api/tasks/all", ""); status != http.StatusUnauthorized {
		t.Fatalf("all chores must require login: %d", status)
	}
	cookie, csrf := authenticated(t, s)

	status, _, _ := req(t, s, "POST", "/api/templates", `{"title":"Tømme opvaskemaskine","assigned_to":"","repeat_rule":"weekly_window","id":""}`, cookie, "")
	if status != http.StatusForbidden {
		t.Fatalf("templates need CSRF: %d", status)
	}
	status, _, _ = req(t, s, "POST", "/api/templates", `{"title":"Test","assigned_to":"unknown","repeat_rule":"none","id":""}`, cookie, csrf)
	if status != 400 {
		t.Fatalf("unknown member should be rejected: %d", status)
	}
	status, _, _ = req(t, s, "POST", "/api/templates", `{"title":"Test","assigned_to":"","repeat_rule":"every-minute","id":""}`, cookie, csrf)
	if status != 400 {
		t.Fatalf("unsupported repeat rule should be rejected: %d", status)
	}

	status, member, _ := req(t, s, "POST", "/api/people", `{"name":"Anton","color":"#ddaa00","notify_service":"","position":0}`, cookie, csrf)
	if status != 200 {
		t.Fatalf("person: %d", status)
	}
	id := member["id"].(string)
	add := `{"title":"Tømme opvaskemaskine","assigned_to":"` + id + `","repeat_rule":"biweekly_window","id":""}`
	status, tpl, _ := req(t, s, "POST", "/api/templates", add, cookie, csrf)
	if status != 200 {
		t.Fatalf("template create: %d %v", status, tpl)
	}
	tplID := tpl["id"].(string)
	if len(tplID) != 24 {
		t.Fatalf("bad ID: %q", tplID)
	}
	status, list := arrayRequest(t, s, "/api/templates", cookie)
	if status != 200 || len(list) != 1 || list[0]["repeat_rule"] != "biweekly_window" {
		t.Fatalf("template list: %d %v", status, list)
	}

	// Changing the archive must not affect active tasks and vice versa.
	status, task, _ := req(t, s, "POST", "/api/tasks", `{"id":"","title":"Vasketøj","assigned_to":"`+id+`","due_date":"2026-10-28","repeat_rule":"weekly_window","done":false}`, cookie, csrf)
	if status != 200 {
		t.Fatalf("chore create: %d %v", status, task)
	}
	status, all := arrayRequest(t, s, "/api/tasks/all", cookie)
	if status != 200 || len(all) != 1 || all[0]["title"] != "Vasketøj" {
		t.Fatalf("all tasks: %d %v", status, all)
	}
	status, _, _ = req(t, s, "PUT", "/api/templates/"+tplID, `{"id":"","title":"Skraldespand","assigned_to":"`+id+`","repeat_rule":"daily"}`, cookie, csrf)
	if status != 200 {
		t.Fatalf("edit template: %d", status)
	}
	status, list = arrayRequest(t, s, "/api/templates", cookie)
	if status != 200 || len(list) != 1 || list[0]["title"] != "Skraldespand" {
		t.Fatalf("edited templates: %d %v", status, list)
	}
	status, _, _ = req(t, s, "DELETE", "/api/templates/"+tplID, "", cookie, csrf)
	if status != 200 {
		t.Fatalf("delete template: %d", status)
	}
	status, list = arrayRequest(t, s, "/api/templates", cookie)
	if status != 200 || len(list) != 0 {
		t.Fatalf("deleted templates: %v", list)
	}
	status, all = arrayRequest(t, s, "/api/tasks/all", cookie)
	if status != 200 || len(all) != 1 {
		t.Fatalf("deleting template should not delete chores: %v", all)
	}
	// A task not scheduled for the selected day is still in the all-tasks list.
	due := all[0]["due_date"].(string)
	if !strings.HasPrefix(due, "2026-") {
		t.Fatalf("task date missing: %v", all)
	}
}

func TestTemplatesSchemaUpgradePreservesExistingData(t *testing.T) {
	a, _ := testApp(t)
	if err := a.db.Exec("INSERT INTO tasks(id,title,assigned_to,due_date,repeat_rule) VALUES(?,?,?,?,?)", "persisted-task", "Eksisterende pligt", "", "2026-10-28", "weekly_window"); err != nil {
		t.Fatal(err)
	}
	// Init is idempotent when upgrading an existing data directory.
	if err := a.db.Init(); err != nil {
		t.Fatal(err)
	}
	tasks, err := a.db.Query("SELECT title FROM tasks WHERE id=?", "persisted-task")
	if err != nil || len(tasks) != 1 || tasks[0]["title"] != "Eksisterende pligt" {
		t.Fatalf("tasks lost on upgrade: %v, %v", tasks, err)
	}
	if err := a.db.Exec("INSERT INTO task_templates(id,title,assigned_to,repeat_rule) VALUES(?,?,?,?)", "persisted-template", "Opvask", "", "weekly_window"); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Init(); err != nil {
		t.Fatal(err)
	}
	templates, err := a.db.Query("SELECT title FROM task_templates WHERE id=?", "persisted-template")
	if err != nil || len(templates) != 1 {
		t.Fatalf("template lost on reinit: %v, %v", templates, err)
	}
}

 
func TestWeekTaskOverviewRequiresLoginAndRespectsCompletion(t *testing.T) {
 a, _ := testApp(t)
 s := httptest.NewServer(securityHeaders(a.mux))
 defer s.Close()
 code, _, _ := req(t, s, "GET", "/api/tasks/week?start=2026-10-28", "", "", "")
 if code != http.StatusUnauthorized {t.Fatalf("week endpoint should require login: %d",code)}
 cookie, _ := authenticated(t, s)
 for _, x := range []struct{ id,title,due,rule string }{
   {"whole","Hele ugen","2026-10-28","weekly_window"},
   {"daily","Daglig","2026-10-28","daily"},
   {"later","Senere","2026-11-09","once_week"},
 }{
   if err := a.db.Exec("INSERT INTO tasks(id,title,assigned_to,due_date,repeat_rule) VALUES(?,?,?,?,?)",x.id,x.title,"",x.due,x.rule);err!=nil{t.Fatal(err)}
 }
 if err := a.db.Exec("INSERT INTO completions(task_id,day) VALUES(?,?)","whole","2026-10-26");err!=nil{t.Fatal(err)}
 request, err := http.NewRequest("GET",s.URL+"/api/tasks/week?start=2026-10-28",nil)
 if err!=nil {t.Fatal(err)}
 request.Header.Set("Cookie",cookie)
 response,err:=http.DefaultClient.Do(request)
 if err!=nil{t.Fatal(err)}
 defer response.Body.Close()
 if response.StatusCode!=200{t.Fatalf("week status %d",response.StatusCode)}
 var payload struct{
  Start string `json:"start"`
  Entries []TaskOccurrence `json:"entries"`
 }
 if err:=json.NewDecoder(response.Body).Decode(&payload);err!=nil {t.Fatal(err)}
 if payload.Start!="2026-10-26"{t.Fatalf("expected Monday got %q",payload.Start)}
 count:=map[string]int{}
 for _,x:=range payload.Entries {
  count[x.ID]++
  if x.ID=="whole"&&!x.Done{t.Fatal("completed week task should remain completed each day")}
  if x.ID=="later"{t.Fatal("future one-time task returned")}
 }
 if count["whole"]!=7||count["daily"]!=5{t.Fatalf("wrong weekly occurrences %+v",count)}
}
