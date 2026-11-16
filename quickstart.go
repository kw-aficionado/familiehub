package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

type QuickstartChoice struct {
	TemplateID  string   `json:"template_id"`
	People      []string `json:"people"`
	Starter     string   `json:"starter"`
	Mode        string   `json:"mode"` // rotate: one shared chore; each: one personal chore per member
	RepeatRule  string   `json:"repeat_rule"`
	Weekday     *int     `json:"weekday,omitempty"`
	RotateWeeks int      `json:"rotate_weeks"`
}
type QuickstartRequest struct {
	StartDate string             `json:"start_date"`
	WeekStart *int               `json:"week_start,omitempty"`
	Choices   []QuickstartChoice `json:"choices"`
	planApproval
}
type QuickstartWeek struct {
	Start       string                 `json:"start"`
	End         string                 `json:"end"`
	Points      map[string]int         `json:"points"`
	Minutes     map[string]int         `json:"minutes"`
	Assignments []QuickstartOccurrence `json:"assignments"`
}
type QuickstartOccurrence struct {
	Title    string `json:"title"`
	PersonID string `json:"person_id"`
	Count    int    `json:"count"`
	Points   int    `json:"points"`
	Minutes  int    `json:"minutes"`
}
type QuickstartPreview struct {
	planBundle
	WeekStart int              `json:"week_start"`
	Weeks     []QuickstartWeek `json:"weeks"`
	Warning   string           `json:"warning,omitempty"`
}

func (a *App) compileQuickstart(q scheduleReader, input QuickstartRequest) (QuickstartPreview, error) {
	p := QuickstartPreview{planBundle: planBundle{StartDate: input.StartDate, Tasks: []planTask{}}, Weeks: []QuickstartWeek{}}
	if !validDay(input.StartDate) || len(input.Choices) < 1 || len(input.Choices) > 50 {
		return p, fmt.Errorf("Vælg startdato og 1–50 opgaver")
	}
	first, e := validWeekStart(input.WeekStart, q)
	if e != nil {
		return p, e
	}
	p.WeekStart = first
	people, e := q.Query("SELECT id FROM people")
	if e != nil {
		return p, e
	}
	valid := map[string]bool{}
	for _, r := range people {
		valid[r["id"]] = true
	}
	rows, e := q.Query("SELECT id,title,minutes,points FROM task_templates")
	if e != nil {
		return p, e
	}
	archive := map[string]map[string]string{}
	for _, r := range rows {
		archive[r["id"]] = r
	}
	start, _ := time.Parse("2006-01-02", input.StartDate)
	used := map[string]bool{}
	usedTitles := map[string]bool{}
	for _, c := range input.Choices {
		tpl, ok := archive[c.TemplateID]
		if !ok || used[c.TemplateID] {
			return p, fmt.Errorf("Vælg hver arkivopgave højst én gang")
		}
		used[c.TemplateID] = true
		titleKey := normalizedTitle(tpl["title"])
		if usedTitles[titleKey] {
			return p, fmt.Errorf("Vælg kun én arkivskabelon pr. opgavenavn. Brug personlige opgaver til flere deltagere.")
		}
		usedTitles[titleKey] = true
		if len(c.People) < 1 || len(c.People) > 10 {
			return p, fmt.Errorf("Vælg mindst én deltager pr. opgave")
		}
		seen := map[string]bool{}
		for _, id := range c.People {
			if !valid[id] || seen[id] {
				return p, fmt.Errorf("Ugyldige eller gentagne deltagere")
			}
			seen[id] = true
		}
		if c.Mode != "rotate" && c.Mode != "each" {
			return p, fmt.Errorf("Vælg fælles turnus eller personlig opgave til hver deltager")
		}
		if !validRepeatRule(c.RepeatRule) {
			return p, fmt.Errorf("Ugyldig gentagelse")
		}
		if c.RotateWeeks != 0 && c.RotateWeeks != 1 && c.RotateWeeks != 2 && c.RotateWeeks != 4 {
			return p, fmt.Errorf("Opgavebytte skal være 0, 1, 2 eller 4 uger")
		}
		if c.Mode == "rotate" && !seen[c.Starter] {
			return p, fmt.Errorf("Vælg en startperson blandt opgavens deltagere")
		}
		if c.Mode == "rotate" && len(c.People) > 1 && c.RotateWeeks == 0 {
			return p, fmt.Errorf("Vælg et bytteinterval, eller kun én fast ansvarlig")
		}
		due := start
		if c.RepeatRule == "weekly" || c.RepeatRule == "biweekly" {
			if c.Weekday == nil || *c.Weekday < 0 || *c.Weekday > 6 {
				return p, fmt.Errorf("Vælg ugedag for ugeopgaven")
			}
			due = due.AddDate(0, 0, (*c.Weekday-int(start.Weekday())+7)%7)
		}
		points, minutes := parseNumber(tpl["points"], 0), parseNumber(tpl["minutes"], 0)
		if points < 1 || points > 20 || minutes < 1 || minutes > 240 {
			return p, fmt.Errorf("Kontrollér point og minutter i arkivet")
		}
		owners := []string{c.Starter}
		rotation := c.RotateWeeks
		if len(c.People) == 1 || c.Mode == "each" || c.RepeatRule == "none" || c.RepeatRule == "once_week" {
			rotation = 0
		}
		if c.Mode == "each" {
			owners = c.People
		}
		for _, owner := range owners {
			task := planTask{Title: tpl["title"], TemplateID: c.TemplateID, PersonID: owner, DueDate: due.Format("2006-01-02"), RepeatRule: c.RepeatRule, WeekStart: first, Points: points, Minutes: minutes, RotateWeeks: rotation}
			if rotation > 0 {
				task.Members = append([]string(nil), c.People...)
			}
			p.Tasks = append(p.Tasks, task)
		}
		if rotation > 0 {
			next := weekStartOn(start, first).AddDate(0, 0, 7*rotation).Format("2006-01-02")
			if p.FirstRotationDate == "" || next < p.FirstRotationDate {
				p.FirstRotationDate = next
			}
		}
	}
	if len(p.Tasks) > 200 {
		return p, fmt.Errorf("Planen må højst indeholde 200 opgaveserier")
	}
	// Four real calendar weeks show exact occurrence counts, including biweekly
	// chores and a partial first week. It is not an estimated flat weekly total.
	for i := 0; i < 4; i++ {
		week := weekStartOn(start, first).AddDate(0, 0, 7*i)
		w := QuickstartWeek{Start: week.Format("2006-01-02"), End: week.AddDate(0, 0, 6).Format("2006-01-02"), Points: map[string]int{}, Minutes: map[string]int{}, Assignments: []QuickstartOccurrence{}}
		entries := map[string]*QuickstartOccurrence{}
		for idx, t := range p.Tasks {
			due, _ := time.Parse("2006-01-02", t.DueDate)
			raw, _ := json.Marshal(t.Members)
			seen := map[string]bool{}
			for day := 0; day < 7; day++ {
				target := week.AddDate(0, 0, day)
				date := target.Format("2006-01-02")
				if date < input.StartDate {
					continue
				}
				visible, key := taskOccurrenceOn(due, target, t.RepeatRule, first)
				if !visible || seen[key] {
					continue
				}
				seen[key] = true
				owner := rotatedOwnerOn(t.PersonID, string(raw), input.StartDate, t.RotateWeeks, date, first)
				k := fmt.Sprint(idx) + ":" + owner
				if entries[k] == nil {
					entries[k] = &QuickstartOccurrence{Title: t.Title, PersonID: owner}
				}
				item := entries[k]
				item.Count++
				item.Points += t.Points
				item.Minutes += t.Minutes
				w.Points[owner] += t.Points
				w.Minutes[owner] += t.Minutes
			}
		}
		keys := []string{}
		for k := range entries {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			w.Assignments = append(w.Assignments, *entries[k])
		}
		p.Weeks = append(p.Weeks, w)
	}
	if input.StartDate < time.Now().In(a.loc).Format("2006-01-02") {
		p.Warning = "Startdatoen ligger før i dag. Ældre forekomster kan blive forfaldne. Gamle serier kan ikke stoppes bagud i tid."
	}
	if e := completeBundle(q, &p.planBundle); e != nil {
		return p, e
	}
	return p, nil
}
func (a *App) quickstartPreview(w http.ResponseWriter, r *http.Request) {
	var v QuickstartRequest
	if !readJSON(w, r, &v) {
		return
	}
	p, e := a.compileQuickstart(a.db, v)
	if e != nil {
		apiError(w, 400, e.Error())
		return
	}
	writeJSON(w, 200, p)
}
func (a *App) quickstartApply(w http.ResponseWriter, r *http.Request) {
	var v QuickstartRequest
	if !readJSON(w, r, &v) {
		return
	}
	if v.PreviewHash == "" || v.RequestID == "" {
		apiError(w, 400, "Beregn og godkend først et Quickstart-forslag")
		return
	}
	user, _ := a.accountIdentity(r)
	result, e := a.persistPlan(v.planApproval, user.Admin, func(q scheduleReader) (planBundle, error) { p, e := a.compileQuickstart(q, v); return p.planBundle, e })
	if e != nil {
		planResponseError(w, e)
		return
	}
	a.broadcast()
	writeJSON(w, 200, result)
}
