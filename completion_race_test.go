package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// A concurrent checkbox and retirement must be serializable: either completion
// wins and retirement is refused, or retirement wins and completion is refused.
func TestCompletionAndRetirementCannotHideCompletedHistory(t *testing.T) {
	a, _ := testApp(t)
	today := time.Now().In(a.loc).Format("2006-01-02")
	for n := 0; n < 40; n++ {
		id := newID()
		if e := a.db.Exec("INSERT INTO tasks(id,title,assigned_to,due_date,repeat_rule) VALUES(?,?,'',?,'daily')", id, "Concurrent test", today); e != nil {
			t.Fatal(e)
		}
		rows, e := a.db.Query("SELECT "+taskColumns+" FROM tasks WHERE id=?", id)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(map[string]any{"ids": []string{id}, "from": today, "snapshots": map[string]string{id: jsonHash(taskFromRow(rows[0]))}})
		toggle := httptest.NewRequest("POST", "/api/tasks/"+id+"/toggle", strings.NewReader(`{"day":"`+today+`","done":true}`))
		toggle.SetPathValue("id", id)
		retire := httptest.NewRequest("POST", "/api/tasks/retire", strings.NewReader(string(raw)))
		toggle.Header.Set("Content-Type", "application/json")
		retire.Header.Set("Content-Type", "application/json")
		tw, rw := httptest.NewRecorder(), httptest.NewRecorder()
		ready := make(chan struct{})
		var done sync.WaitGroup
		done.Add(2)
		go func() { defer done.Done(); <-ready; a.toggle(tw, toggle) }()
		go func() { defer done.Done(); <-ready; a.retireTasks(rw, retire) }()
		close(ready)
		done.Wait()
		if !((tw.Code == 200 && rw.Code == 409) || (tw.Code == 400 && rw.Code == 200)) {
			t.Fatalf("completion=%d retirement=%d: %s %s", tw.Code, rw.Code, tw.Body, rw.Body)
		}
		inconsistent, e := a.db.Query("SELECT t.id FROM tasks t JOIN completions c ON t.id=c.task_id WHERE t.id=? AND t.inactive_from<>'' AND c.day>=t.inactive_from", id)
		if e != nil || len(inconsistent) > 0 {
			t.Fatalf("hidden completion: %v %v", inconsistent, e)
		}
	}
}
