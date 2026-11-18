package main

// Followup is a ledger of occurrences, never additional task series. A snapshot
// fixes the original deadline, responsibility and effort even across rotations.
import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kw-aficionado/familiehub/internal/store"
)

type FollowupSettings struct {
	From     string `json:"from"`
	RedAfter int    `json:"red_after"`
}
type FollowupOccurrence struct {
	TaskID        string `json:"task_id"`
	Key           string `json:"key"`
	Title         string `json:"title"`
	OriginalOwner string `json:"original_owner"`
	AssignedTo    string `json:"assigned_to"`
	StartsOn      string `json:"starts_on"`
	DueOn         string `json:"due_on"`
	Policy        string `json:"policy"`
	Points        int    `json:"points"`
	Minutes       int    `json:"minutes"`
	Status        string `json:"status"`
	PerformedOn   string `json:"performed_on"`
	PerformedBy   string `json:"performed_by"`
	DeferredUntil string `json:"deferred_until"`
	HelpRequested bool   `json:"help_requested"`
	Version       int    `json:"version"`
	DelayDays     int    `json:"delay_days"`
	Severity      string `json:"severity"`
}
type FollowupRoutine struct {
	TaskIDs    []string `json:"task_ids"`
	TaskID     string   `json:"task_id"`
	Title      string   `json:"title"`
	AssignedTo string   `json:"assigned_to"`
	Missing    int      `json:"missing"`
	LastDue    string   `json:"last_due"`
}
type FollowupSummary struct {
	PersonID string         `json:"person_id"`
	Counts   map[string]int `json:"counts"`
	Points   map[string]int `json:"points"`
}
type FollowupBoard struct {
	Settings    FollowupSettings     `json:"settings"`
	Today       string               `json:"today"`
	ReportFrom  string               `json:"report_from"`
	ReportUntil string               `json:"report_until"`
	Rest        []FollowupOccurrence `json:"rest"`
	Routines    []FollowupRoutine    `json:"routines"`
	Review      []FollowupOccurrence `json:"review,omitempty"`
	Summary     []FollowupSummary    `json:"summary,omitempty"`
	Warning     string               `json:"warning,omitempty"`
}

func validFollowupPolicy(s string) bool {
	return s == "" || s == "date" || s == "routine" || s == "carry"
}
func followupSettings(q scheduleReader) (FollowupSettings, error) {
	s := FollowupSettings{RedAfter: 2}
	rows, e := q.Query("SELECT key,value FROM planner_meta WHERE key IN ('followup_from','followup_red_after')")
	for _, r := range rows {
		if r["key"] == "followup_from" {
			s.From = r["value"]
		}
		if r["key"] == "followup_red_after" {
			s.RedAfter = parseNumber(r["value"], 2)
		}
	}
	return s, e
}
func followupDay(now time.Time) string {
	loc, _ := time.LoadLocation("Europe/Copenhagen")
	return now.In(loc).Format("2006-01-02")
}
func followupDate(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
func isWholeWeek(rule string) bool {
	return rule == "weekly_window" || rule == "biweekly_window" || rule == "once_week"
}
func followupRow(r map[string]string) FollowupOccurrence {
	return FollowupOccurrence{TaskID: r["task_id"], Key: r["occurrence_key"], Title: r["title"], OriginalOwner: r["original_owner"], AssignedTo: r["assigned_to"], StartsOn: r["starts_on"], DueOn: r["due_on"], Policy: r["policy"], Points: parseNumber(r["points"], 3), Minutes: parseNumber(r["minutes"], 15), Status: r["status"], PerformedOn: r["performed_on"], PerformedBy: r["performed_by"], DeferredUntil: r["deferred_until"], HelpRequested: r["help_requested"] == "1", Version: parseNumber(r["version"], 1)}
}
func effectiveFollowupPolicy(task, template map[string]string) string {
	if task["followup_policy"] != "" {
		return task["followup_policy"]
	}
	if template["followup_policy"] != "" {
		return template["followup_policy"]
	}
	rule := task["repeat_rule"]
	if template["repeat_rule"] != "" {
		rule = template["repeat_rule"]
	}
	if rule == "daily" || rule == "weekdays" {
		return "routine"
	}
	return "carry"
}

// Build one immutable occurrence snapshot from a visible calendar date. Partial
// weeks retain their actual deadline; week-start is the series' saved setting.
func newFollowupOccurrence(row, template map[string]string, day string) (FollowupOccurrence, bool) {
	o := FollowupOccurrence{}
	if !validDay(day) || !validDay(row["due_date"]) || day < row["due_date"] || !plannerTaskHasStarted(row["rotation_start"], day) || taskIsStopped(row, day) {
		return o, false
	}
	due, target := followupDate(row["due_date"]), followupDate(day)
	first := parseNumber(row["week_start"], 1)
	visible, key := taskOccurrenceOn(due, target, row["repeat_rule"], first)
	if !visible {
		return o, false
	}
	deadline := day
	start := key
	if isWholeWeek(row["repeat_rule"]) {
		deadline = followupDate(key).AddDate(0, 0, 6).Format("2006-01-02")
		if row["due_date"] > start {
			start = row["due_date"]
		}
	}
	if row["rotation_start"] > start {
		start = row["rotation_start"]
	}
	owner := rotatedOwnerOn(row["assigned_to"], row["rotation_members"], row["rotation_start"], parseNumber(row["rotation_weeks"], 0), day, first)
	points, minutes := parseNumber(row["effort_points"], 0), parseNumber(row["effort_minutes"], 0)
	if points < 1 {
		points = parseNumber(template["points"], 3)
	}
	if minutes < 1 {
		minutes = parseNumber(template["minutes"], 15)
	}
	return FollowupOccurrence{TaskID: row["id"], Key: key, Title: row["title"], OriginalOwner: owner, AssignedTo: owner, StartsOn: start, DueOn: deadline, Policy: effectiveFollowupPolicy(row, template), Points: points, Minutes: minutes, Status: "open", Version: 1}, true
}
func insertFollowup(tx *store.Tx, o FollowupOccurrence, completed bool, now time.Time) error {
	status := "open"
	if completed {
		status = "completed"
	} // Legacy ticks have unknown performed-at, not invented timestamps.
	return tx.Exec("INSERT OR IGNORE INTO followup_occurrences(task_id,occurrence_key,title,original_owner,assigned_to,starts_on,due_on,policy,points,minutes,status,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", o.TaskID, o.Key, o.Title, o.OriginalOwner, o.AssignedTo, o.StartsOn, o.DueOn, o.Policy, fmt.Sprint(o.Points), fmt.Sprint(o.Minutes), status, now.UTC().Format(time.RFC3339))
}
func followupTemplates(q scheduleReader) (map[string]map[string]string, error) {
	rows, e := q.Query("SELECT id,repeat_rule,followup_policy,points,minutes FROM task_templates")
	out := map[string]map[string]string{}
	for _, r := range rows {
		out[r["id"]] = r
	}
	return out, e
}

// Incremental materialization, serialized with completion and policy changes.
// At most one year per series per pass after a very long offline period; the
// cursor resumes rather than dropping any older occurrences.
func syncFollowup(tx *store.Tx, now time.Time) (bool, error) {
	s, e := followupSettings(tx)
	if e != nil || s.From == "" {
		return false, e
	}
	today := followupDay(now)
	if today < s.From {
		return false, nil
	}
	templates, e := followupTemplates(tx)
	if e != nil {
		return false, e
	}
	rows, e := tx.Query("SELECT t.*,c.through_day FROM tasks t LEFT JOIN followup_cursors c ON t.id=c.task_id ORDER BY t.id")
	if e != nil {
		return false, e
	}
	doneRows, e := tx.Query("SELECT task_id,day FROM completions WHERE day>=?", followupDate(s.From).AddDate(0, 0, -7).Format("2006-01-02"))
	if e != nil {
		return false, e
	}
	done := map[string]bool{}
	for _, r := range doneRows {
		done[r["task_id"]+":"+r["day"]] = true
	}
	pending := false
	for _, r := range rows {
		start := s.From
		for _, v := range []string{r["due_date"], r["rotation_start"]} {
			if v > start {
				start = v
			}
		}
		if r["through_day"] != "" {
			next := followupDate(r["through_day"]).AddDate(0, 0, 1).Format("2006-01-02")
			if next > start {
				start = next
			}
		}
		end := today
		if r["inactive_from"] != "" && r["inactive_from"] <= end {
			end = followupDate(r["inactive_from"]).AddDate(0, 0, -1).Format("2006-01-02")
		}
		if start > end {
			continue
		}
		until := followupDate(start).AddDate(0, 0, 365).Format("2006-01-02")
		if end > until {
			end = until
			pending = true
		}
		for d := followupDate(start); d.Format("2006-01-02") <= end; d = d.AddDate(0, 0, 1) {
			o, ok := newFollowupOccurrence(r, templates[r["template_id"]], d.Format("2006-01-02"))
			if !ok {
				continue
			}
			if e = insertFollowup(tx, o, done[o.TaskID+":"+o.Key], now); e != nil {
				return pending, e
			}
		}
		if e = tx.Exec("INSERT INTO followup_cursors(task_id,through_day) VALUES(?,?) ON CONFLICT(task_id) DO UPDATE SET through_day=excluded.through_day", r["id"], end); e != nil {
			return pending, e
		}
	}
	return pending, nil
}
func followupClassification(o FollowupOccurrence, today string) string {
	switch o.Status {
	case "completed":
		if o.PerformedOn == "" {
			return "completed_unknown"
		}
		if o.PerformedOn > o.DueOn {
			return "late"
		}
		return "on_time"
	case "excused", "superseded":
		return o.Status
	}
	if o.DeferredUntil >= today {
		return "deferred"
	}
	return "missing"
}
func followupSeverity(o *FollowupOccurrence, today string, redAfter int) {
	due := o.DueOn
	if o.DeferredUntil > due {
		due = o.DeferredUntil
	}
	o.DelayDays = int(followupDate(today).Sub(followupDate(due)).Hours() / 24)
	if o.DelayDays < 0 {
		o.DelayDays = 0
	}
	o.Severity = "orange"
	if o.DelayDays >= redAfter || o.HelpRequested {
		o.Severity = "red"
	}
	if o.DeferredUntil >= today {
		o.Severity = "deferred"
	}
}
func followupBoardTx(tx *store.Tx, now time.Time, admin bool) (FollowupBoard, error) {
	today := followupDay(now)
	from := followupDate(today).AddDate(0, 0, -7).Format("2006-01-02")
	b := FollowupBoard{Today: today, ReportFrom: from, ReportUntil: followupDate(today).AddDate(0, 0, -1).Format("2006-01-02"), Rest: []FollowupOccurrence{}, Routines: []FollowupRoutine{}, Review: []FollowupOccurrence{}, Summary: []FollowupSummary{}}
	var e error
	b.Settings, e = followupSettings(tx)
	if e != nil || b.Settings.From == "" {
		return b, e
	}
	pending, e := syncFollowup(tx, now)
	if e != nil {
		return b, e
	}
	if pending {
		b.Warning = "Historikken indl\u00e6ses trinvist efter lang tids frav\u00e6r. Opdat\u00e9r igen; intet er slettet."
	}
	if b.Settings.From > b.ReportFrom {
		b.ReportFrom = b.Settings.From
	}
	rows, e := tx.Query("SELECT * FROM followup_occurrences WHERE (status='open' AND (policy='carry' OR help_requested=1)) OR due_on>=? ORDER BY due_on,task_id,occurrence_key", from)
	if e != nil {
		return b, e
	}
	// Older planners represent a daily template as weekday shards. Group only
	// by known plan/template identity (and owner), never by matching titles.
	sources, e := tx.Query("SELECT id,plan_id,template_id FROM tasks")
	if e != nil {
		return b, e
	}
	groupKeys := map[string]string{}
	groupIDs := map[string][]string{}
	for _, source := range sources {
		key := source["id"]
		if source["plan_id"] != "" && source["template_id"] != "" {
			key = "plan:" + source["plan_id"] + ":" + source["template_id"]
		}
		groupKeys[source["id"]] = key
		groupIDs[key] = append(groupIDs[key], source["id"])
	}
	groups := map[string]*FollowupRoutine{}
	totals := map[string]*FollowupSummary{}
	for _, r := range rows {
		o := followupRow(r)
		if o.DueOn >= today {
			continue
		} // Day/week deadlines expire at next Copenhagen midnight.
		followupSeverity(&o, today, b.Settings.RedAfter)
		if o.Status == "open" && (o.Policy == "carry" || o.HelpRequested) {
			b.Rest = append(b.Rest, o)
		}
		if o.Status == "open" && o.Policy == "routine" && o.DueOn >= from {
			key := groupKeys[o.TaskID] + ":" + o.AssignedTo
			g := groups[key]
			if g == nil {
				g = &FollowupRoutine{TaskIDs: groupIDs[groupKeys[o.TaskID]], TaskID: o.TaskID, Title: o.Title, AssignedTo: o.AssignedTo}
				groups[key] = g
			}
			g.Missing++
			if o.DueOn > g.LastDue {
				g.LastDue = o.DueOn
			}
		}
		if admin && o.DueOn >= from {
			b.Review = append(b.Review, o)
			t := totals[o.OriginalOwner]
			if t == nil {
				t = &FollowupSummary{PersonID: o.OriginalOwner, Counts: map[string]int{}, Points: map[string]int{}}
				totals[o.OriginalOwner] = t
			}
			key := followupClassification(o, today)
			t.Counts[key]++
			t.Points[key] += o.Points
		}
	}
	for _, g := range groups {
		b.Routines = append(b.Routines, *g)
	}
	sort.Slice(b.Routines, func(i, j int) bool {
		return b.Routines[i].TaskID+b.Routines[i].AssignedTo < b.Routines[j].TaskID+b.Routines[j].AssignedTo
	})
	for _, t := range totals {
		b.Summary = append(b.Summary, *t)
	}
	sort.Slice(b.Summary, func(i, j int) bool { return b.Summary[i].PersonID < b.Summary[j].PersonID })
	return b, nil
}
func (a *App) followupActor(r *http.Request) string {
	u, _ := a.accountIdentity(r)
	if u.Admin {
		return "administrator"
	}
	return u.AccountID
}
func (a *App) followupRoutes() {
	a.mux.HandleFunc("GET /api/followup", a.withAuth(a.followupGet))
	a.mux.HandleFunc("PUT /api/followup/settings", a.withAdmin(a.followupSettingsPut))
	a.mux.HandleFunc("GET /api/followup/policies", a.withAdmin(a.followupPoliciesGet))
	a.mux.HandleFunc("PUT /api/followup/policies/{id}", a.withAdmin(a.followupPolicyPut))
	a.mux.HandleFunc("POST /api/followup/action", a.withAuth(a.followupActionPost))
	a.mux.HandleFunc("GET /api/followup/history", a.withAdmin(a.followupHistory))
	a.mux.HandleFunc("POST /api/tablet/followup/action", a.tabletFollowupAction)
}
func (a *App) followupGet(w http.ResponseWriter, r *http.Request) {
	u, _ := a.accountIdentity(r)
	var b FollowupBoard
	e := a.db.Transaction(func(tx *store.Tx) error { var e error; b, e = followupBoardTx(tx, a.timeNow(), u.Admin); return e })
	if e != nil {
		planResponseError(w, e)
		return
	}
	writeJSON(w, 200, b)
}
func (a *App) followupSettingsPut(w http.ResponseWriter, r *http.Request) {
	var v FollowupSettings
	if !readJSON(w, r, &v) {
		return
	}
	today := followupDay(a.timeNow())
	earliest := followupDate(today).AddDate(0, 0, -90).Format("2006-01-02")
	if !validDay(v.From) || v.From > today || v.RedAfter < 1 || v.RedAfter > 30 {
		apiError(w, 400, "V\u00e6lg start i dag eller h\u00f8jst 90 dage tilbage og 1-30 dage til r\u00f8d markering")
		return
	}
	e := a.db.Transaction(func(tx *store.Tx) error {
		old, e := followupSettings(tx)
		if e != nil {
			return e
		}
		if old.From == "" && v.From < earliest {
			return badPlan(400, "Start kan h\u00f8jst ligge 90 dage tilbage")
		}
		if old.From != "" && old.From != v.From {
			return badPlan(409, "Opf\u00f8lgning er allerede startet. Startdatoen kan ikke \u00e6ndres og skjule historik.")
		}
		if e = tx.Exec("INSERT INTO planner_meta(key,value) VALUES('followup_from',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", v.From); e != nil {
			return e
		}
		if e = tx.Exec("INSERT INTO planner_meta(key,value) VALUES('followup_red_after',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", strconv.Itoa(v.RedAfter)); e != nil {
			return e
		}
		_, e = syncFollowup(tx, a.timeNow())
		return e
	})
	if e != nil {
		planResponseError(w, e)
		return
	}
	a.broadcast()
	writeJSON(w, 200, v)
}
func (a *App) followupPoliciesGet(w http.ResponseWriter, r *http.Request) {
	rows, e := a.db.Query("SELECT " + taskColumns + " FROM tasks ORDER BY title,id")
	if e != nil {
		planResponseError(w, e)
		return
	}
	templates, e := followupTemplates(a.db)
	if e != nil {
		planResponseError(w, e)
		return
	}
	out := []map[string]any{}
	today := followupDay(a.timeNow())
	for _, row := range rows {
		if taskIsStopped(row, today) {
			continue
		}
		out = append(out, map[string]any{"id": row["id"], "title": row["title"], "assigned_to": row["assigned_to"], "policy": row["followup_policy"], "effective_policy": effectiveFollowupPolicy(row, templates[row["template_id"]]), "repeat_rule": row["repeat_rule"]})
	}
	writeJSON(w, 200, out)
}
func (a *App) followupPolicyPut(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Policy string `json:"policy"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	id := r.PathValue("id")
	if !validID(id) || !validFollowupPolicy(v.Policy) {
		apiError(w, 400, "Ugyldig opgave eller regel")
		return
	}
	e := a.db.Transaction(func(tx *store.Tx) error {
		if _, e := syncFollowup(tx, a.timeNow()); e != nil {
			return e
		}
		rows, e := tx.Query("SELECT id FROM tasks WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(rows) != 1 {
			return badPlan(404, "Opgaven findes ikke")
		}
		return tx.Exec("UPDATE tasks SET followup_policy=? WHERE id=?", v.Policy, id)
	})
	if e != nil {
		planResponseError(w, e)
		return
	}
	a.broadcast()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func followupAudit(tx *store.Tx, o FollowupOccurrence, action, actor, note string, now time.Time) error {
	detail, _ := json.Marshal(struct {
		Occurrence FollowupOccurrence `json:"occurrence"`
		Note       string             `json:"note"`
	}{o, note})
	return tx.Exec("INSERT INTO followup_audit(task_id,occurrence_key,action,actor,recorded_at,detail) VALUES(?,?,?,?,?,?)", o.TaskID, o.Key, action, actor, now.UTC().Format(time.RFC3339), string(detail))
}

// Normal checkboxes use the same ledger, without granting permission to excuse
// or rewrite a reviewed outcome. Unknown legacy completion time stays unknown.
func recordFollowupCompletion(tx *store.Tx, row map[string]string, day, key string, done bool, now time.Time, actor string) error {
	s, e := followupSettings(tx)
	if e != nil || s.From == "" || day < s.From || day > followupDay(now) {
		return e
	}
	templates, e := followupTemplates(tx)
	if e != nil {
		return e
	}
	o, visible := newFollowupOccurrence(row, templates[row["template_id"]], day)
	if !visible {
		return nil
	}
	prior, e := tx.Query("SELECT day FROM completions WHERE task_id=? AND day=?", row["id"], key)
	if e != nil {
		return e
	}
	if e = insertFollowup(tx, o, len(prior) > 0, now); e != nil {
		return e
	}
	entries, e := tx.Query("SELECT * FROM followup_occurrences WHERE task_id=? AND occurrence_key=?", row["id"], key)
	if e != nil {
		return e
	}
	if len(entries) != 1 {
		return fmt.Errorf("missing followup")
	}
	o = followupRow(entries[0])
	if o.Status == "excused" || o.Status == "superseded" {
		return badPlan(409, "Denne forekomst er afsluttet ved opf\u00f8lgning, ikke udf\u00f8rt. Se voksenoverblikket.")
	}
	if done && o.Status == "completed" || !done && o.Status == "open" {
		return nil
	}
	action := "reopen"
	status, performed := "open", ""
	if done {
		action = "complete"
		status = "completed"
		performed = followupDay(now)
	}
	o.Status = status
	o.PerformedOn = performed
	o.HelpRequested = false
	o.Version++
	if e = tx.Exec("UPDATE followup_occurrences SET status=?,performed_on=?,performed_by='',help_requested=0,version=version+1,updated_at=? WHERE task_id=? AND occurrence_key=?", status, performed, now.UTC().Format(time.RFC3339), o.TaskID, o.Key); e != nil {
		return e
	}
	return followupAudit(tx, o, action, actor, "", now)
}

type followupAction struct {
	TaskID    string `json:"task_id"`
	Key       string `json:"key"`
	Version   int    `json:"version"`
	Action    string `json:"action"`
	Date      string `json:"date,omitempty"`
	PersonID  string `json:"person_id,omitempty"`
	Note      string `json:"note,omitempty"`
	RequestID string `json:"request_id"`
}

func applyFollowupAction(tx *store.Tx, v followupAction, actor string, admin bool, personID string, tablet bool, now time.Time) (FollowupOccurrence, error) {
	result := FollowupOccurrence{}
	if !validID(v.TaskID) || !validDay(v.Key) || v.Version < 1 || len(v.RequestID) < 16 || len(v.RequestID) > 100 || len([]rune(v.Note)) > 300 {
		return result, badPlan(400, "Ugyldig opf\u00f8lgning")
	}
	if !admin && (v.Action != "complete" && v.Action != "help") {
		return result, badPlan(403, "Denne handling kr\u00e6ver administrator")
	}
	if !admin && (v.Date != "" || v.PersonID != "" || v.Note != "") {
		return result, badPlan(403, "Dato, ansvar og begrundelse kr\u00e6ver administrator")
	}
	if _, e := syncFollowup(tx, now); e != nil {
		return result, e
	}
	rows, e := tx.Query("SELECT * FROM followup_occurrences WHERE task_id=? AND occurrence_key=?", v.TaskID, v.Key)
	if e != nil {
		return result, e
	}
	if len(rows) != 1 {
		return result, badPlan(404, "Forekomsten findes ikke i opf\u00f8lgningen")
	}
	o := followupRow(rows[0])
	today := followupDay(now)
	if !admin && !tablet && (personID == "" || personID != o.AssignedTo) {
		return result, badPlan(403, "Brug din tilknyttede personlige konto til egne restopgaver")
	}
	fingerprint := jsonHash(struct {
		V     followupAction
		Actor string
	}{v, actor})
	receipts, e := tx.Query("SELECT request_hash,response FROM followup_receipts WHERE request_id=?", v.RequestID)
	if e != nil {
		return result, e
	}
	if len(receipts) > 0 {
		if receipts[0]["request_hash"] != fingerprint {
			return result, badPlan(409, "Foresp\u00f8rgslen tilh\u00f8rer en anden handling")
		}
		e = json.Unmarshal([]byte(receipts[0]["response"]), &result)
		return result, e
	}
	// Tablet writes must target an actually displayed, overdue carry/help item,
	// not arbitrary history, future records or adult decisions.
	if tablet && (o.DueOn >= today || (o.Policy != "carry" && !o.HelpRequested) || o.DeferredUntil > today) {
		return result, badPlan(403, "Tabletten kan kun afslutte synlige restopgaver")
	}
	if o.Version != v.Version {
		return result, badPlan(409, "Opgaven er \u00e6ndret. Opdat\u00e9r overblikket f\u00f8r du pr\u00f8ver igen.")
	}
	if o.Status != "open" {
		return result, badPlan(409, "Opgaven er allerede afsluttet")
	}
	if o.StartsOn > today || o.DueOn >= today {
		return result, badPlan(400, "Brug dagens almindelige afkrydsning inden fristen")
	}
	switch v.Action {
	case "complete":
		performed := today
		if v.Date != "" {
			performed = v.Date
		}
		if !validDay(performed) || performed < o.StartsOn || performed > today {
			return result, badPlan(400, "V\u00e6lg en faktisk udf\u00f8relsesdato mellem opgavens start og i dag")
		}
		o.Status = "completed"
		o.PerformedOn = performed
		o.PerformedBy = v.PersonID
		o.HelpRequested = false
		if e = tx.Exec("INSERT OR IGNORE INTO completions(task_id,day) VALUES(?,?)", o.TaskID, o.Key); e != nil {
			return result, e
		}
	case "excuse", "supersede":
		if strings.TrimSpace(v.Note) == "" {
			return result, badPlan(400, "Skriv en kort begrundelse")
		}
		o.Status = "excused"
		if v.Action == "supersede" {
			o.Status = "superseded"
		}
		o.HelpRequested = false
	case "defer":
		if !validDay(v.Date) || v.Date < today || v.Date > followupDate(today).AddDate(0, 0, 365).Format("2006-01-02") {
			return result, badPlan(400, "V\u00e6lg ny frist fra i dag og h\u00f8jst et \u00e5r frem")
		}
		o.DeferredUntil = v.Date
	case "reassign":
		if v.PersonID == "" {
			return result, badPlan(400, "V\u00e6lg en ansvarlig")
		}
		o.AssignedTo = v.PersonID
	case "help":
		o.HelpRequested = true
	default:
		return result, badPlan(400, "Ukendt handling")
	}
	if v.PersonID != "" {
		p, e := tx.Query("SELECT id FROM people WHERE id=?", v.PersonID)
		if e != nil {
			return result, e
		}
		if len(p) != 1 {
			return result, badPlan(400, "Personen findes ikke")
		}
	}
	o.Version++
	if e = tx.Exec("UPDATE followup_occurrences SET assigned_to=?,status=?,performed_on=?,performed_by=?,deferred_until=?,help_requested=?,version=?,updated_at=? WHERE task_id=? AND occurrence_key=?", o.AssignedTo, o.Status, o.PerformedOn, o.PerformedBy, o.DeferredUntil, boolText(o.HelpRequested), fmt.Sprint(o.Version), now.UTC().Format(time.RFC3339), o.TaskID, o.Key); e != nil {
		return result, e
	}
	if e = followupAudit(tx, o, v.Action, actor, v.Note, now); e != nil {
		return result, e
	}
	encoded, _ := json.Marshal(o)
	e = tx.Exec("INSERT INTO followup_receipts(request_id,request_hash,response) VALUES(?,?,?)", v.RequestID, fingerprint, string(encoded))
	return o, e
}
func boolText(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
func (a *App) followupActionPost(w http.ResponseWriter, r *http.Request) {
	var v followupAction
	if !readJSON(w, r, &v) {
		return
	}
	u, _ := a.accountIdentity(r)
	actor := "account:" + a.followupActor(r)
	var o FollowupOccurrence
	e := a.db.Transaction(func(tx *store.Tx) error {
		var e error
		o, e = applyFollowupAction(tx, v, actor, u.Admin, u.PersonID, false, a.timeNow())
		return e
	})
	if e != nil {
		planResponseError(w, e)
		return
	}
	a.broadcast()
	writeJSON(w, 200, o)
}
func (a *App) tabletFollowupAction(w http.ResponseWriter, r *http.Request) {
	token := tabletToken(r)
	if !a.tabletCSRF(r, token) {
		apiError(w, 403, "Ugyldigt sikkerhedstoken")
		return
	}
	var v followupAction
	if !readJSON(w, r, &v) {
		return
	}
	var o FollowupOccurrence
	e := a.db.Transaction(func(tx *store.Tx) error {
		now := a.timeNow()
		d, e := a.tabletInTx(tx, token, now)
		if e != nil {
			return e
		}
		if !d.CanFollowup {
			return badPlan(403, "Tabletten har ikke tilladelse til restopgaver")
		}
		o, e = applyFollowupAction(tx, v, "tablet:"+d.ID, false, "", true, now)
		return e
	})
	if e != nil {
		tabletError(w, e)
		return
	}
	a.broadcast()
	writeJSON(w, 200, o)
}
func (a *App) followupHistory(w http.ResponseWriter, r *http.Request) {
	id, key := r.URL.Query().Get("task_id"), r.URL.Query().Get("key")
	if !validID(id) || !validDay(key) {
		apiError(w, 400, "Ugyldig forekomst")
		return
	}
	rows, e := a.db.Query("SELECT action,actor,recorded_at,detail FROM followup_audit WHERE task_id=? AND occurrence_key=? ORDER BY id", id, key)
	if e != nil {
		planResponseError(w, e)
		return
	}
	if rows == nil {
		rows = store.Rows{}
	}
	writeJSON(w, 200, rows)
}

// Reuse the existing one-digest-per-person notification settings. No provider or
// notification switch is enabled by this feature. Deferrals and waivers matter.
func (a *App) followupPending(now time.Time, daysBack int, includeToday bool) ([]aiPending, error) {
	if daysBack < 1 || daysBack > 14 {
		daysBack = 7
	}
	today := followupDay(now)
	from := followupDate(today).AddDate(0, 0, -daysBack).Format("2006-01-02")
	result := []aiPending{}
	e := a.db.Transaction(func(tx *store.Tx) error {
		if _, e := syncFollowup(tx, now); e != nil {
			return e
		}
		rows, e := tx.Query("SELECT * FROM followup_occurrences WHERE status='open' AND due_on<=? ORDER BY due_on,task_id", today)
		if e != nil {
			return e
		}
		groups := map[string]*aiPending{}
		for _, r := range rows {
			o := followupRow(r)
			if o.AssignedTo == "" || o.DeferredUntil > today {
				continue
			}
			if o.DueOn == today && !includeToday {
				continue
			}
			if o.DueOn < today && ((o.Policy == "date" && !o.HelpRequested) || (o.Policy == "routine" && o.DueOn < from)) {
				continue
			}
			key := o.TaskID + ":" + o.AssignedTo
			g := groups[key]
			if g == nil {
				g = &aiPending{TaskID: o.TaskID, Title: o.Title, PersonID: o.AssignedTo, Due: o.DueOn}
				groups[key] = g
			}
			g.Missed++
			if o.DueOn > g.Due {
				g.Due = o.DueOn
			}
		}
		for _, g := range groups {
			result = append(result, *g)
		}
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].PersonID+result[i].TaskID < result[j].PersonID+result[j].TaskID })
	return result, e
}
