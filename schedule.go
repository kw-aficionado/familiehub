package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/kw-aficionado/familiehub/internal/store"
)

type scheduleReader interface {
	Query(string, ...string) (store.Rows, error)
}

type ScheduleSettings struct {
	WeekStart int `json:"week_start"`
}

func scheduleSettings(q scheduleReader) (ScheduleSettings, error) {
	s := ScheduleSettings{WeekStart: 1}
	rows, err := q.Query("SELECT value FROM planner_meta WHERE key='week_start'")
	if err != nil {
		return s, err
	}
	if len(rows) > 0 {
		v, e := strconv.Atoi(rows[0]["value"])
		if e == nil && v >= 0 && v <= 6 {
			s.WeekStart = v
		}
	}
	return s, nil
}
func (a *App) scheduleSettingsGet(w http.ResponseWriter, r *http.Request) {
	s, e := scheduleSettings(a.db)
	if e != nil {
		apiError(w, 500, "Ugestart kunne ikke hentes")
		return
	}
	writeJSON(w, 200, s)
}
func (a *App) scheduleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var v struct {
		WeekStart *int `json:"week_start"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	if v.WeekStart == nil || *v.WeekStart < 0 || *v.WeekStart > 6 {
		apiError(w, 400, "Vælg en ugedag")
		return
	}
	if e := a.db.Exec("INSERT INTO planner_meta(key,value) VALUES('week_start',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", strconv.Itoa(*v.WeekStart)); e != nil {
		apiError(w, 500, "Ugestart kunne ikke gemmes")
		return
	}
	a.broadcast()
	writeJSON(w, 200, ScheduleSettings{WeekStart: *v.WeekStart})
}

// Dates are parsed at UTC midnight: elapsed calendar days do not change at DST.
func weekStartOn(t time.Time, first int) time.Time {
	if first < 0 || first > 6 {
		first = 1
	}
	return t.AddDate(0, 0, -((int(t.Weekday()) - first + 7) % 7))
}
func taskOccurrenceOn(due, target time.Time, rule string, first int) (bool, string) {
	switch rule {
	case "once_week", "weekly_window", "biweekly_window":
		anchor, week := weekStartOn(due, first), weekStartOn(target, first)
		delta := int(week.Sub(anchor).Hours()/24) / 7
		if delta < 0 {
			return false, ""
		}
		visible := rule == "weekly_window" || (rule == "once_week" && delta == 0) || (rule == "biweekly_window" && delta%2 == 0)
		return visible, week.Format("2006-01-02")
	default:
		return taskOccurrence(due, target, rule)
	}
}
func rotatedOwnerOn(owner, members, start string, weeks int, day string, first int) string {
	if members == "" || weeks < 1 || !validDay(start) || !validDay(day) || day < start {
		return owner
	}
	var ids []string
	if json.Unmarshal([]byte(members), &ids) != nil || len(ids) < 2 {
		return owner
	}
	index := -1
	for i, id := range ids {
		if id == owner {
			index = i
			break
		}
	}
	if index < 0 {
		return owner
	}
	anchor, _ := time.Parse("2006-01-02", start)
	target, _ := time.Parse("2006-01-02", day)
	step := int(weekStartOn(target, first).Sub(weekStartOn(anchor, first)).Hours()/24) / 7 / weeks
	return ids[(index+step)%len(ids)]
}
func taskIsStopped(row map[string]string, day string) bool {
	return row["inactive_from"] != "" && day >= row["inactive_from"]
}
func taskFromRow(row map[string]string) Task {
	t := Task{ID: row["id"], Title: row["title"], AssignedTo: row["assigned_to"], BaseAssignedTo: row["assigned_to"], DueDate: row["due_date"], RepeatRule: row["repeat_rule"], PlanID: row["plan_id"], RotationStart: row["rotation_start"], RotationWeeks: parseNumber(row["rotation_weeks"], 0), WeekStart: parseNumber(row["week_start"], 1), InactiveFrom: row["inactive_from"], TemplateID: row["template_id"], FollowupPolicy: row["followup_policy"]}
	if row["rotation_members"] != "" {
		_ = json.Unmarshal([]byte(row["rotation_members"]), &t.RotationMembers)
	}
	return t
}

const taskColumns = "id,title,assigned_to,due_date,repeat_rule,rotation_members,rotation_start,rotation_weeks,plan_id,week_start,inactive_from,template_id,followup_policy,effort_points,effort_minutes"

func validWeekStart(v *int, q scheduleReader) (int, error) {
	if v != nil {
		if *v < 0 || *v > 6 {
			return 0, fmt.Errorf("Ugestart skal være en ugedag")
		}
		return *v, nil
	}
	s, e := scheduleSettings(q)
	return s.WeekStart, e
}
