package main

import (
	"net/http"
	"strings"
)

// TaskTemplate is a reusable chore definition. Its start date is deliberately
// omitted: every new chore starts on the date chosen in the creation dialog.
type TaskTemplate struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	AssignedTo string `json:"assigned_to"`
	RepeatRule string `json:"repeat_rule"`
}

func validRepeatRule(rule string) bool {
	switch rule {
	case "none", "daily", "weekly", "biweekly", "once_week", "weekly_window", "biweekly_window":
		return true
	default:
		return false
	}
}

func (a *App) allTasks(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query("SELECT id,title,assigned_to,due_date,repeat_rule FROM tasks ORDER BY title COLLATE NOCASE, due_date, id")
	if err != nil {
		apiError(w, 500, "Alle pligter kunne ikke hentes")
		return
	}
	tasks := []Task{}
	for _, row := range rows {
		tasks = append(tasks, Task{
			ID: row["id"], Title: row["title"], AssignedTo: row["assigned_to"],
			DueDate: row["due_date"], RepeatRule: row["repeat_rule"],
		})
	}
	writeJSON(w, 200, tasks)
}

func (a *App) templates(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query("SELECT id,title,assigned_to,repeat_rule FROM task_templates ORDER BY title COLLATE NOCASE, id")
	if err != nil {
		apiError(w, 500, "Opgavearkivet kunne ikke hentes")
		return
	}
	templates := []TaskTemplate{}
	for _, row := range rows {
		templates = append(templates, TaskTemplate{
			ID: row["id"], Title: row["title"],
			AssignedTo: row["assigned_to"], RepeatRule: row["repeat_rule"],
		})
	}
	writeJSON(w, 200, templates)
}

func (a *App) templateSave(w http.ResponseWriter, r *http.Request) {
	var tpl TaskTemplate
	if !readJSON(w, r, &tpl) {
		return
	}
	tpl.Title = strings.TrimSpace(tpl.Title)
	if !bounded(tpl.Title, 150) || !validRepeatRule(tpl.RepeatRule) {
		apiError(w, 400, "Kontrollér navn og gentagelse")
		return
	}
	if tpl.AssignedTo != "" {
		people, err := a.people()
		if err != nil {
			apiError(w, 500, "Familiemedlemmer kunne ikke læses")
			return
		}
		exists := false
		for _, person := range people {
			if person.ID == tpl.AssignedTo {
				exists = true
				break
			}
		}
		if !exists {
			apiError(w, 400, "Den valgte ansvarlige findes ikke")
			return
		}
	}
	if r.Method == http.MethodPost {
		tpl.ID = newID()
		if err := a.db.Exec("INSERT INTO task_templates(id,title,assigned_to,repeat_rule) VALUES(?,?,?,?)", tpl.ID, tpl.Title, tpl.AssignedTo, tpl.RepeatRule); err != nil {
			apiError(w, 500, "Skabelonen kunne ikke oprettes")
			return
		}
	} else {
		tpl.ID = r.PathValue("id")
		if !validID(tpl.ID) {
			apiError(w, 400, "Ugyldigt skabelon-ID")
			return
		}
		if rows, err := a.db.Query("SELECT id FROM task_templates WHERE id=?", tpl.ID); err != nil {
			apiError(w, 500, "Skabelonen kunne ikke læses")
			return
		} else if len(rows) == 0 {
			apiError(w, 404, "Skabelonen findes ikke")
			return
		}
		if err := a.db.Exec("UPDATE task_templates SET title=?,assigned_to=?,repeat_rule=? WHERE id=?", tpl.Title, tpl.AssignedTo, tpl.RepeatRule, tpl.ID); err != nil {
			apiError(w, 500, "Skabelonen kunne ikke gemmes")
			return
		}
	}
	a.broadcast()
	writeJSON(w, 200, tpl)
}

func (a *App) templateDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		apiError(w, 400, "Ugyldigt skabelon-ID")
		return
	}
	if rows, err := a.db.Query("SELECT id FROM task_templates WHERE id=?", id); err != nil {
		apiError(w, 500, "Skabelonen kunne ikke læses")
		return
	} else if len(rows) == 0 {
		apiError(w, 404, "Skabelonen findes ikke")
		return
	}
	if err := a.db.Exec("DELETE FROM task_templates WHERE id=?", id); err != nil {
		apiError(w, 500, "Skabelonen kunne ikke slettes")
		return
	}
	a.broadcast()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
