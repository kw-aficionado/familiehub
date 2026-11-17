package main

import (
	"github.com/kw-aficionado/familiehub/internal/store"
	"time"
)

// Called only inside a database transaction. Shared by accounts and tablets.
func writeTaskCompletion(tx *store.Tx, id, day string, done bool) error {
	rows, err := tx.Query("SELECT due_date,repeat_rule,rotation_start,week_start,inactive_from FROM tasks WHERE id=?", id)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return badPlan(404, "Opgaven findes ikke")
	}
	due, err := time.Parse("2006-01-02", rows[0]["due_date"])
	if err != nil {
		return err
	}
	selected, _ := time.Parse("2006-01-02", day)
	if !plannerTaskHasStarted(rows[0]["rotation_start"], day) || taskIsStopped(rows[0], day) {
		return badPlan(400, "Opgaven er ikke aktiv på den valgte dato")
	}
	visible, completionKey := taskOccurrenceOn(due, selected, rows[0]["repeat_rule"], parseNumber(rows[0]["week_start"], 1))
	if !visible {
		return badPlan(400, "Opgaven er ikke aktiv på den valgte dag")
	}
	if done {
		return tx.Exec("INSERT OR IGNORE INTO completions(task_id,day) VALUES(?,?)", id, completionKey)
	}
	return tx.Exec("DELETE FROM completions WHERE task_id=? AND day=?", id, completionKey)
}
