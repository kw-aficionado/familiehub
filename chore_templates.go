package main

import (
	"net/http"
 "strconv"
	"strings"
	"time"
)

// TaskTemplate is a reusable chore definition. Its start date is deliberately
// omitted: every new chore starts on the date chosen in the creation dialog.
type TaskTemplate struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	AssignedTo string `json:"assigned_to"`
	RepeatRule string `json:"repeat_rule"`
 Category string `json:"category"`
 Minutes int `json:"minutes"`
 Points int `json:"points"`
 TimesPerWeek int `json:"times_per_week"`
}

func parseNumber(value string, fallback int) int{n,e:=strconv.Atoi(value);if e!=nil{return fallback};return n}
func validRepeatRule(rule string) bool {
	switch rule {
	case "none", "daily", "weekdays", "weekly", "biweekly", "once_week", "weekly_window", "biweekly_window":
		return true
	default:
		return false
	}
}

func (a *App) allTasks(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query("SELECT id,title,assigned_to,due_date,repeat_rule FROM tasks ORDER BY title COLLATE NOCASE, due_date, id")
	if err != nil {
		apiError(w, 500, "Alle opgaver kunne ikke hentes")
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
	rows, err := a.db.Query("SELECT id,title,assigned_to,repeat_rule,category,minutes,points,times_per_week FROM task_templates ORDER BY title COLLATE NOCASE, id")
	if err != nil {
		apiError(w, 500, "Opgavearkivet kunne ikke hentes")
		return
	}
	templates := []TaskTemplate{}
	for _, row := range rows {
		templates = append(templates, TaskTemplate{
			ID: row["id"], Title: row["title"],
			AssignedTo: row["assigned_to"], RepeatRule: row["repeat_rule"], Category:row["category"], Minutes:parseNumber(row["minutes"],15), Points:parseNumber(row["points"],3), TimesPerWeek:parseNumber(row["times_per_week"],1),
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
 tpl.Category=strings.TrimSpace(tpl.Category)
 if tpl.Category==""{tpl.Category="Diverse"}
 if tpl.Minutes==0{tpl.Minutes=15}
 if tpl.Points==0{tpl.Points=3}
 if tpl.TimesPerWeek==0{
  switch tpl.RepeatRule{case "daily":tpl.TimesPerWeek=7;case "weekdays":tpl.TimesPerWeek=5;default:tpl.TimesPerWeek=1}
 }
 if len([]rune(tpl.Category))>40||tpl.Minutes<1||tpl.Minutes>240||tpl.Points<1||tpl.Points>20||tpl.TimesPerWeek<1||tpl.TimesPerWeek>7{apiError(w,400,"Kontrollér kategori, minutter, point og hyppighed");return}
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
		if err := a.db.Exec("INSERT INTO task_templates(id,title,assigned_to,repeat_rule,category,minutes,points,times_per_week) VALUES(?,?,?,?,?,?,?,?)", tpl.ID, tpl.Title, tpl.AssignedTo, tpl.RepeatRule,tpl.Category,strconv.Itoa(tpl.Minutes),strconv.Itoa(tpl.Points),strconv.Itoa(tpl.TimesPerWeek)); err != nil {
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
		if err := a.db.Exec("UPDATE task_templates SET title=?,assigned_to=?,repeat_rule=?,category=?,minutes=?,points=?,times_per_week=? WHERE id=?", tpl.Title, tpl.AssignedTo, tpl.RepeatRule,tpl.Category,strconv.Itoa(tpl.Minutes),strconv.Itoa(tpl.Points),strconv.Itoa(tpl.TimesPerWeek),tpl.ID); err != nil {
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

 
// TaskOccurrence exposes scheduled tasks for a single day; all data is read from
// the existing tasks and completions tables (no new persistence required).
type TaskOccurrence struct {
 Task
 Day string `json:"day"`
}

func (a *App) tasksWeek(w http.ResponseWriter, r *http.Request) {
 startDate := r.URL.Query().Get("start")
 if !validDay(startDate) {
  apiError(w, http.StatusBadRequest, "Ugyldig startdato for uge")
  return
 }
 start, _ := time.Parse("2006-01-02", startDate)
 weekday := (int(start.Weekday()) + 6) % 7
 start = start.AddDate(0, 0, -weekday)
 entries := []TaskOccurrence{}
 for dayIndex := 0; dayIndex < 7; dayIndex++ {
  day := start.AddDate(0, 0, dayIndex).Format("2006-01-02")
  tasks, err := a.tasks(day)
  if err != nil {
   apiError(w, http.StatusInternalServerError, "Opgaverne for ugen kunne ikke hentes")
   return
  }
  for _, task := range tasks {
   entries = append(entries, TaskOccurrence{Task: task, Day: day})
  }
 }
 writeJSON(w, http.StatusOK, map[string]any{
  "start": start.Format("2006-01-02"), "entries": entries,
 })
}
