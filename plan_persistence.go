package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/kw-aficionado/familiehub/internal/store"
)

type planTask struct {
	Title       string   `json:"title"`
	TemplateID  string   `json:"template_id"`
	PersonID    string   `json:"person_id"`
	DueDate     string   `json:"due_date"`
	RepeatRule  string   `json:"repeat_rule"`
	Members     []string `json:"members"`
	RotateWeeks int      `json:"rotate_weeks"`
	WeekStart   int      `json:"week_start"`
	Points      int      `json:"points"`
	Minutes     int      `json:"minutes"`
}
type PlanConflict struct {
	Task
	Completed   int    `json:"completed"`
	OwnerToday  string `json:"owner_today"`
	ActiveToday bool   `json:"active_today"`
	Snapshot    string `json:"snapshot"`
}
type planBundle struct {
	Tasks             []planTask     `json:"tasks"`
	StartDate         string         `json:"start_date"`
	FirstRotationDate string         `json:"first_rotation_date"`
	Conflicts         []PlanConflict `json:"conflicts"`
	PreviewHash       string         `json:"preview_hash"`
}
type planApproval struct {
	RequestID      string   `json:"request_id,omitempty"`
	PreviewHash    string   `json:"preview_hash,omitempty"`
	AllowOverlap   bool     `json:"allow_overlap,omitempty"`
	ReplaceTaskIDs []string `json:"replace_task_ids,omitempty"`
}
type planReceipt struct {
	OK                bool   `json:"ok"`
	PlanID            string `json:"plan_id"`
	Created           int    `json:"created"`
	StartDate         string `json:"start_date"`
	FirstRotationDate string `json:"first_rotation_date,omitempty"`
	Reused            bool   `json:"reused"`
	Stopped           int    `json:"stopped"`
}
type planError struct {
	code    int
	message string
}

func (e *planError) Error() string           { return e.message }
func badPlan(code int, message string) error { return &planError{code, message} }
func normalizedTitle(s string) string        { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
func jsonHash(v any) string                  { raw, _ := json.Marshal(v); return sha(string(raw)) }

func planConflicts(q scheduleReader, tasks []planTask, from string) ([]PlanConflict, error) {
	rows, e := q.Query("SELECT " + taskColumns + " FROM tasks ORDER BY id")
	if e != nil {
		return nil, e
	}
	counts, e := q.Query("SELECT task_id,COUNT(*) AS n FROM completions GROUP BY task_id")
	if e != nil {
		return nil, e
	}
	completed := map[string]int{}
	for _, r := range counts {
		completed[r["task_id"]] = parseNumber(r["n"], 0)
	}
	titles := map[string]bool{}
	templates := map[string]bool{}
	for _, t := range tasks {
		titles[normalizedTitle(t.Title)] = true
		if t.TemplateID != "" {
			templates[t.TemplateID] = true
		}
	}
	result := []PlanConflict{}
	for _, r := range rows {
		if taskIsStopped(r, from) || (!titles[normalizedTitle(r["title"])] && !templates[r["template_id"]]) {
			continue
		}
		// Finished one-off series cannot overlap a new plan starting later.
		if r["repeat_rule"] == "none" && r["due_date"] < from {
			continue
		}
		if r["repeat_rule"] == "once_week" {
			due, _ := time.Parse("2006-01-02", r["due_date"])
			if weekStartOn(due, parseNumber(r["week_start"], 1)).AddDate(0, 0, 7).Format("2006-01-02") <= from {
				continue
			}
		}
		result = append(result, PlanConflict{Task: taskFromRow(r), Completed: completed[r["id"]], Snapshot: jsonHash(taskFromRow(r))})
	}
	return result, nil
}
func completeBundle(q scheduleReader, b *planBundle) error {
	var e error
	b.Conflicts, e = planConflicts(q, b.Tasks, b.StartDate)
	if e != nil {
		return e
	}
	// Hash only stable, server-computed information, never a client-supplied list.
	b.PreviewHash = jsonHash(struct {
		Tasks     []planTask
		Start     string
		Conflicts []PlanConflict
	}{b.Tasks, b.StartDate, b.Conflicts})
	return nil
}
func definitionHash(b planBundle, replace []string) string {
	tasks := append([]planTask(nil), b.Tasks...)
	sort.Slice(tasks, func(i, j int) bool { return jsonHash(tasks[i]) < jsonHash(tasks[j]) })
	ids := append([]string(nil), replace...)
	sort.Strings(ids)
	return jsonHash(struct {
		Tasks   []planTask
		Start   string
		Replace []string
	}{tasks, b.StartDate, ids})
}

// All validation, idempotency checks, optional retirement and inserts share one
// SQLite transaction, including reads, so concurrent approvals cannot duplicate.
func (a *App) persistPlan(approval planApproval, admin bool, compile func(scheduleReader) (planBundle, error)) (planReceipt, error) {
	result := planReceipt{}
	if len(approval.RequestID) > 80 {
		return result, badPlan(400, "Ugyldigt forespørgsels-ID")
	}
	err := a.db.Transaction(func(tx *store.Tx) error {
		if _, e := syncFollowup(tx, a.timeNow()); e != nil {
			return e
		}
		b, e := compile(tx)
		if e != nil {
			return badPlan(400, e.Error())
		}
		if len(b.Tasks) < 1 || len(b.Tasks) > 200 {
			return badPlan(400, "Vælg mellem 1 og 200 opgaveserier")
		}
		definition := definitionHash(b, approval.ReplaceTaskIDs)
		key := approval.RequestID
		if key == "" {
			key = "definition:" + definition
		}
		rows, e := tx.Query("SELECT definition_hash,response FROM plan_receipts WHERE request_key=?", key)
		if e != nil {
			return e
		}
		if len(rows) > 0 {
			if rows[0]["definition_hash"] != definition {
				return badPlan(409, "Dette gemmeforsøg tilhører et andet forslag. Beregn planen igen.")
			}
			if json.Unmarshal([]byte(rows[0]["response"]), &result) != nil {
				return fmt.Errorf("invalid receipt")
			}
			if e := receiptMatches(tx, result, b); e != nil {
				return e
			}
			result.Reused = true
			return nil
		}
		// Separate tabs/new request IDs with the same active definition also reuse it.
		rows, e = tx.Query("SELECT r.response FROM plan_receipts r WHERE r.definition_hash=? AND EXISTS(SELECT 1 FROM tasks t WHERE t.plan_id=r.plan_id AND (t.inactive_from='' OR t.inactive_from>?)) ORDER BY r.rowid LIMIT 1", definition, b.StartDate)
		if e != nil {
			return e
		}
		if len(rows) > 0 {
			if json.Unmarshal([]byte(rows[0]["response"]), &result) != nil {
				return fmt.Errorf("invalid receipt")
			}
			if e := receiptMatches(tx, result, b); e != nil {
				return e
			}
			result.Reused = true
			return nil
		}
		if approval.PreviewHash != "" && approval.PreviewHash != b.PreviewHash {
			return badPlan(409, "Opgaverne eller arkivet er ændret siden forslaget. Beregn og godkend igen.")
		}
		replacing := map[string]bool{}
		conflicts := map[string]PlanConflict{}
		for _, t := range b.Conflicts {
			conflicts[t.ID] = t
		}
		for _, id := range approval.ReplaceTaskIDs {
			if !validID(id) || replacing[id] {
				return badPlan(400, "Ugyldige eller gentagne opgave-ID'er")
			}
			if _, ok := conflicts[id]; !ok {
				return badPlan(409, "En valgt gammel opgave er ændret. Beregn forslaget igen.")
			}
			replacing[id] = true
		}
		if len(replacing) > 0 {
			if !admin {
				return badPlan(403, "Kun administrator kan stoppe eksisterende opgaveserier")
			}
			if b.StartDate < time.Now().In(a.loc).Format("2006-01-02") {
				return badPlan(400, "Eksisterende serier må kun stoppes fra i dag eller senere")
			}
			for id := range replacing {
				if e := canRetire(tx, id, b.StartDate); e != nil {
					return e
				}
			}
		}
		if len(conflicts) > len(replacing) && !approval.AllowOverlap {
			return badPlan(409, "Der findes allerede opgaver med samme navn. Gennemgå mulige overlap, før du gemmer.")
		}
		result = planReceipt{OK: true, PlanID: newID(), Created: len(b.Tasks), StartDate: b.StartDate, FirstRotationDate: b.FirstRotationDate, Stopped: len(replacing)}
		for id := range replacing {
			if e = tx.Exec("UPDATE tasks SET inactive_from=? WHERE id=?", b.StartDate, id); e != nil {
				return e
			}
		}
		for _, task := range b.Tasks {
			members := ""
			if task.RotateWeeks > 0 && len(task.Members) > 1 {
				raw, _ := json.Marshal(task.Members)
				members = string(raw)
			}
			policy := ""
			template, e := tx.Query("SELECT followup_policy FROM task_templates WHERE id=?", task.TemplateID)
			if e != nil {
				return e
			}
			if len(template) > 0 {
				policy = template[0]["followup_policy"]
			}
			if e = tx.Exec("INSERT INTO tasks(id,title,assigned_to,due_date,repeat_rule,rotation_members,rotation_start,rotation_weeks,plan_id,week_start,template_id,effort_points,effort_minutes,followup_policy) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)", newID(), task.Title, task.PersonID, task.DueDate, task.RepeatRule, members, b.StartDate, fmt.Sprint(task.RotateWeeks), result.PlanID, fmt.Sprint(task.WeekStart), task.TemplateID, fmt.Sprint(task.Points), fmt.Sprint(task.Minutes), policy); e != nil {
				return e
			}
		}
		raw, _ := json.Marshal(result)
		return tx.Exec("INSERT INTO plan_receipts(request_key,definition_hash,plan_id,created,start_date,response) VALUES(?,?,?,?,?,?)", key, definition, result.PlanID, fmt.Sprint(result.Created), result.StartDate, string(raw))
	})
	return result, err
}
func planResponseError(w http.ResponseWriter, e error) {
	var p *planError
	if errors.As(e, &p) {
		apiError(w, p.code, p.message)
	} else {
		apiError(w, 500, "Planen kunne ikke gemmes; ingen delvise ændringer blev gemt")
	}
}
func canRetire(q scheduleReader, id, from string) error {
	rows, e := q.Query("SELECT due_date,repeat_rule,week_start,inactive_from FROM tasks WHERE id=?", id)
	if e != nil {
		return e
	}
	if len(rows) != 1 {
		return badPlan(404, "Opgaveserien findes ikke")
	}
	r := rows[0]
	if r["inactive_from"] != "" && r["inactive_from"] <= from {
		return badPlan(409, "Opgaveserien er allerede stoppet")
	}
	// A completed whole-week occurrence includes its entire old window. Retiring
	// inside that window would hide completed history, so require the next week.
	threshold := from
	if r["repeat_rule"] == "weekly_window" || r["repeat_rule"] == "biweekly_window" || r["repeat_rule"] == "once_week" {
		day, _ := time.Parse("2006-01-02", from)
		threshold = weekStartOn(day, parseNumber(r["week_start"], 1)).Format("2006-01-02")
	}
	done, e := q.Query("SELECT day FROM completions WHERE task_id=? AND day>=? LIMIT 1", id, threshold)
	if e != nil {
		return e
	}
	if len(done) > 0 {
		return badPlan(409, "En valgt opgave er afkrydset i eller efter den periode. Vælg en senere stopdato; historikken bevares.")
	}
	return nil
}
func (a *App) plannerDiagnostics(w http.ResponseWriter, r *http.Request) {
	day := r.URL.Query().Get("day")
	if day == "" {
		day = time.Now().In(a.loc).Format("2006-01-02")
	}
	if !validDay(day) {
		apiError(w, 400, "Ugyldig dato")
		return
	}
	rows, e := a.db.Query("SELECT " + taskColumns + " FROM tasks ORDER BY title,id")
	if e != nil {
		apiError(w, 500, "Opgaver kunne ikke læses")
		return
	}
	all := []PlanConflict{}
	counts := map[string]int{}
	comps, e := a.db.Query("SELECT task_id,COUNT(*) AS n FROM completions GROUP BY task_id")
	if e != nil {
		apiError(w, 500, "Afkrydsninger kunne ikke læses")
		return
	}
	for _, c := range comps {
		counts[c["task_id"]] = parseNumber(c["n"], 0)
	}
	target, _ := time.Parse("2006-01-02", day)
	for _, row := range rows {
		task := taskFromRow(row)
		due, e := time.Parse("2006-01-02", task.DueDate)
		if e != nil {
			continue
		}
		active, _ := taskOccurrenceOn(due, target, task.RepeatRule, task.WeekStart)
		active = active && plannerTaskHasStarted(task.RotationStart, day) && !taskIsStopped(row, day)
		all = append(all, PlanConflict{Task: task, Snapshot: jsonHash(task), Completed: counts[task.ID], OwnerToday: rotatedOwnerOn(task.AssignedTo, row["rotation_members"], task.RotationStart, task.RotationWeeks, day, task.WeekStart), ActiveToday: active})
	}
	writeJSON(w, 200, map[string]any{"day": day, "tasks": all, "snapshot": jsonHash(all)})
}
func (a *App) retireTasks(w http.ResponseWriter, r *http.Request) {
	var v struct {
		IDs       []string          `json:"ids"`
		From      string            `json:"from"`
		Snapshots map[string]string `json:"snapshots"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	if !validDay(v.From) || v.From < time.Now().In(a.loc).Format("2006-01-02") || len(v.IDs) < 1 || len(v.IDs) > 200 {
		apiError(w, 400, "Vælg opgaver og en stopdato fra i dag eller senere")
		return
	}
	e := a.db.Transaction(func(tx *store.Tx) error {
		seen := map[string]bool{}
		for _, id := range v.IDs {
			if !validID(id) || seen[id] {
				return badPlan(400, "Ugyldige opgave-ID'er")
			}
			seen[id] = true
			rows, e := tx.Query("SELECT "+taskColumns+" FROM tasks WHERE id=?", id)
			if e != nil {
				return e
			}
			if len(rows) != 1 {
				return badPlan(404, "Opgaven findes ikke")
			}
			if v.Snapshots[id] == "" || v.Snapshots[id] != jsonHash(taskFromRow(rows[0])) {
				return badPlan(409, "Opgaven er ændret. Genindlæs oversigten før godkendelse.")
			}
			if e = canRetire(tx, id, v.From); e != nil {
				return e
			}
		}
		for _, id := range v.IDs {
			if e := tx.Exec("UPDATE tasks SET inactive_from=? WHERE id=?", v.From, id); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		planResponseError(w, e)
		return
	}
	a.broadcast()
	writeJSON(w, 200, map[string]any{"ok": true, "stopped": len(v.IDs), "from": v.From})
}

// An approval retry cannot resurrect a deleted/stopped plan or pretend a moved
// plan still has the old dates. Completed checkmarks do not invalidate a receipt.
func receiptMatches(q scheduleReader, r planReceipt, b planBundle) error {
	rows, e := q.Query("SELECT "+taskColumns+" FROM tasks WHERE plan_id=? ORDER BY id", r.PlanID)
	if e != nil {
		return e
	}
	if len(rows) != len(b.Tasks) {
		return badPlan(409, "Planen er allerede gemt, men er senere ændret eller slettet. Genindlæs oversigten; der er ikke oprettet kopier.")
	}
	want := []string{}
	got := []string{}
	for _, t := range b.Tasks {
		t.Points = 0
		t.Minutes = 0
		want = append(want, jsonHash(t))
	}
	for _, row := range rows {
		if row["rotation_start"] != b.StartDate || row["inactive_from"] != "" {
			return badPlan(409, "Planen er allerede gemt, men er flyttet eller stoppet. Genindlæs oversigten; der er ikke oprettet kopier.")
		}
		t := planTask{Title: row["title"], TemplateID: row["template_id"], PersonID: row["assigned_to"], DueDate: row["due_date"], RepeatRule: row["repeat_rule"], RotateWeeks: parseNumber(row["rotation_weeks"], 0), WeekStart: parseNumber(row["week_start"], 1)}
		if row["rotation_members"] != "" {
			_ = json.Unmarshal([]byte(row["rotation_members"]), &t.Members)
		}
		got = append(got, jsonHash(t))
	}
	sort.Strings(want)
	sort.Strings(got)
	if jsonHash(want) != jsonHash(got) {
		return badPlan(409, "Planen er allerede gemt, men indholdet er ændret. Genindlæs oversigten; der er ikke oprettet kopier.")
	}
	return nil
}
