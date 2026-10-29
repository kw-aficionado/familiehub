package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kw-aficionado/familiehub/internal/store"
)

//go:embed web/*
var assets embed.FS

var ident = regexp.MustCompile(`^[a-z0-9_]+$`)
var color = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// buildCommit is set at Docker build time; it never contains credentials.
var buildCommit = "dev"

type Person struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Color         string `json:"color"`
	NotifyService string `json:"notify_service"`
	Position      int    `json:"position"`
}
type Event struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Start     string   `json:"start_at"`
	End       string   `json:"end_at"`
	Kind      string   `json:"kind"`
	PersonID  string   `json:"person_id"`
	PickupBy  string   `json:"pickup_by"`
	Location  string   `json:"location"`
	Notes     string   `json:"notes"`
	External  bool     `json:"external"`
	PersonIDs []string `json:"person_ids,omitempty"`
	Category  string   `json:"category,omitempty"`
	SourceCalendar string `json:"source_calendar,omitempty"`
}
type Task struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	AssignedTo string `json:"assigned_to"`
	DueDate    string `json:"due_date"`
	RepeatRule string `json:"repeat_rule"`
	Done       bool   `json:"done"`
}
type Config struct {
	AppURL, Password, HAURL, HAToken string
	HACalendars                      []string
	DataDir                          string
}
type App struct {
	db        *store.DB
	cfg       Config
	key       []byte
	client    *http.Client
	mux       *http.ServeMux
	mu        sync.Mutex
	listeners map[chan string]struct{}
	attempts  map[string][]time.Time
	loc       *time.Location
}

func env(k string) string { return strings.TrimSpace(os.Getenv(k)) }
func main() {
	listen := flag.String("listen", "0.0.0.0:8080", "HTTP listen address")
	flag.Parse()
	cfg := Config{AppURL: env("APP_URL"), Password: os.Getenv("APP_PASSWORD"), HAURL: strings.TrimRight(env("HA_URL"), "/"), HAToken: env("HA_TOKEN"), DataDir: env("DATA_DIR")}
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	if len(cfg.Password) < 12 {
		log.Fatal("APP_PASSWORD must be configured (minimum 12 characters)")
	}
	if cfg.HAURL != "" {
		u, err := url.Parse(cfg.HAURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			log.Fatal("HA_URL must be a valid http(s) URL")
		}
	}
	for _, x := range strings.Split(env("HA_CALENDARS"), ",") {
		x = strings.TrimSpace(x)
		if x != "" {
			cfg.HACalendars = append(cfg.HACalendars, x)
		}
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		log.Fatal(err)
	}
	key, err := loadKey(filepath.Join(cfg.DataDir, "session.key"))
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(filepath.Join(cfg.DataDir, "familiehub.sqlite"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err = db.Init(); err != nil {
		log.Fatal(err)
	}
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		log.Fatal(err)
	}
	app := &App{db: db, cfg: cfg, key: key, client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, mux: http.NewServeMux(), listeners: map[chan string]struct{}{}, attempts: map[string][]time.Time{}, loc: loc}
	app.routes()
	go app.reminderLoop()
	server := &http.Server{Addr: *listen, Handler: securityHeaders(app.mux), ReadHeaderTimeout: 8 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("FamilieHub ready on %s", *listen)
	log.Fatal(server.ListenAndServe())
}
func loadKey(path string) ([]byte, error) {
	if b, e := os.ReadFile(path); e == nil {
		if len(b) != 32 {
			return nil, errors.New("invalid existing session key")
		}
		return b, nil
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return nil, e
	}
	if e := os.WriteFile(path, b, 0600); e != nil {
		return nil, e
	}
	return b, nil
}
func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Never serve an old Safari-cached dashboard when the container is updated.
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store, max-age=0")
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		h.ServeHTTP(w, r)
	})
}
func (a *App) routes() {
	web, err := fs.Sub(assets, "web")
	if err != nil {
		panic(err)
	}
	a.mux.Handle("GET /", http.FileServer(http.FS(web)))
	a.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	a.mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"version": "0.1.1", "commit": buildCommit})
	})
	a.mux.HandleFunc("POST /api/login", a.login)
	a.mux.HandleFunc("GET /api/session", a.session)
	a.mux.HandleFunc("POST /api/logout", a.withAuth(a.logout))
	a.mux.HandleFunc("GET /api/account/me", a.withAuth(a.accountMe))
	a.mux.HandleFunc("POST /api/account/password", a.withAuth(a.accountChangePassword))
	a.mux.HandleFunc("GET /api/accounts", a.withAdmin(a.accountsList))
	a.mux.HandleFunc("POST /api/accounts", a.withAdmin(a.accountSave))
	a.mux.HandleFunc("PUT /api/accounts/{id}", a.withAdmin(a.accountSave))
	a.mux.HandleFunc("POST /api/accounts/{id}/password", a.withAdmin(a.accountResetPassword))
	a.mux.HandleFunc("DELETE /api/accounts/{id}", a.withAdmin(a.accountDelete))
	a.mux.HandleFunc("GET /api/state", a.withAuth(a.state))
	a.mux.HandleFunc("GET /api/calendar-owners", a.withAuth(a.calendarOwnersGet))
	a.mux.HandleFunc("GET /api/push/key", a.withAuth(a.pushPublicKey))
	a.mux.HandleFunc("POST /api/push/subscribe", a.withAuth(a.pushSubscribe))
	a.mux.HandleFunc("POST /api/push/unsubscribe", a.withAuth(a.pushUnsubscribe))
	a.mux.HandleFunc("GET /api/email/config", a.withAdmin(a.emailConfigGet))
	a.mux.HandleFunc("PUT /api/email/config", a.withAdmin(a.emailConfigPut))
	a.mux.HandleFunc("PUT /api/email/recipient", a.withAdmin(a.emailRecipientPut))
	a.mux.HandleFunc("GET /api/direct-calendars", a.withAdmin(a.directCalendarsGet))
	a.mux.HandleFunc("POST /api/direct-calendars", a.withAdmin(a.directCalendarsSave))
	a.mux.HandleFunc("DELETE /api/direct-calendars/{id}", a.withAdmin(a.directCalendarsDelete))
	a.mux.HandleFunc("POST /api/direct-calendars/{id}/authorize", a.withAdmin(a.directCalendarAuthorize))
	a.mux.HandleFunc("GET /api/direct-calendars/oauth/callback", a.directCalendarOAuthCallback)
	a.mux.HandleFunc("PUT /api/calendar-owners", a.withAuth(a.calendarOwnersPut))
	a.mux.HandleFunc("GET /api/stream", a.withAuth(a.stream))
	a.mux.HandleFunc("GET /api/integrations", a.withAuth(a.integrations))
	a.mux.HandleFunc("GET /api/ai/config", a.withAdmin(a.aiConfigGet))
	a.mux.HandleFunc("PUT /api/ai/config", a.withAdmin(a.aiConfigPut))
	a.mux.HandleFunc("POST /api/ai/test", a.withAdmin(a.aiTest))
	a.mux.HandleFunc("POST /api/ai/assist", a.withAuth(a.aiAssist))
	a.mux.HandleFunc("POST /api/people", a.withAuth(a.personSave))
	a.mux.HandleFunc("PUT /api/people/{id}", a.withAuth(a.personSave))
	a.mux.HandleFunc("DELETE /api/people/{id}", a.withAuth(a.personDelete))
	a.mux.HandleFunc("POST /api/events", a.withAuth(a.eventSave))
	a.mux.HandleFunc("PUT /api/events/{id}", a.withAuth(a.eventSave))
	a.mux.HandleFunc("DELETE /api/events/{id}", a.withAuth(a.eventDelete))
	a.mux.HandleFunc("GET /api/tasks/all", a.withAuth(a.allTasks))
	a.mux.HandleFunc("GET /api/tasks/week", a.withAuth(a.tasksWeek))
	a.mux.HandleFunc("GET /api/templates", a.withAuth(a.templates))
	a.mux.HandleFunc("POST /api/templates", a.withAuth(a.templateSave))
	a.mux.HandleFunc("PUT /api/templates/{id}", a.withAuth(a.templateSave))
	a.mux.HandleFunc("DELETE /api/templates/{id}", a.withAuth(a.templateDelete))
	a.mux.HandleFunc("POST /api/tasks", a.withAuth(a.taskSave))
	a.mux.HandleFunc("PUT /api/tasks/{id}", a.withAuth(a.taskSave))
	a.mux.HandleFunc("DELETE /api/tasks/{id}", a.withAuth(a.taskDelete))
	a.mux.HandleFunc("POST /api/tasks/{id}/toggle", a.withAuth(a.toggle))
	a.mux.HandleFunc("POST /api/notifications/test", a.withAuth(a.testNotification))
}
func readJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		apiError(w, 415, "Content-Type skal være application/json")
		return false
	}
	d := json.NewDecoder(io.LimitReader(r.Body, 32768))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		apiError(w, 400, "Ugyldige JSON-data")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		apiError(w, 400, "Kun ét JSON-objekt tilladt")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
func newID() string { b := make([]byte, 12); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func validDay(s string) bool {
	t, e := time.Parse("2006-01-02", s)
	return e == nil && t.Format("2006-01-02") == s
}
func validTime(s string) bool      { _, e := time.Parse(time.RFC3339, s); return e == nil }
func bounded(s string, n int) bool { return len(strings.TrimSpace(s)) > 0 && len([]rune(s)) <= n }
func (a *App) broadcast() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for c := range a.listeners {
		select {
		case c <- "update":
		default:
		}
	}
}
func mac(key []byte, value string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}
func sha(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func (a *App) sessionID(r *http.Request) string {
	c, e := r.Cookie("fh_session")
	if e != nil {
		return ""
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 || len(parts[0]) != 48 {
		return ""
	}
	if !hmac.Equal([]byte(parts[1]), []byte(mac(a.key, parts[0]))) {
		return ""
	}
	row, e := a.db.Query("SELECT id FROM sessions WHERE id=? AND expires_at>?", sha(parts[0]), time.Now().UTC().Format(time.RFC3339))
	if e != nil || len(row) == 0 {
		return ""
	}
	return parts[0]
}
func (a *App) cookie(w http.ResponseWriter, value string, expire time.Time) {
	http.SetCookie(w, &http.Cookie{Name: "fh_session", Value: value, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(a.cfg.AppURL, "https://"), SameSite: http.SameSiteLaxMode, Expires: expire, MaxAge: int(time.Until(expire).Seconds())})
}
func (a *App) session(w http.ResponseWriter, r *http.Request) {
	user, ok := a.accountIdentity(r)
	if !ok {
		writeJSON(w, 200, map[string]any{"authenticated": false})
		return
	}
	id := a.sessionID(r)
	writeJSON(w, 200, map[string]any{"authenticated": true, "csrf": mac(a.key, "csrf:"+id), "account": identityResponse(user)})
}
func remoteIP(r *http.Request) string {
	p := r.RemoteAddr
	if idx := strings.LastIndex(p, ":"); idx > 0 {
		return p[:idx]
	}
	return p
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	ip := remoteIP(r)
	now := time.Now()
	a.mu.Lock()
	recent := a.attempts[ip][:0]
	for _, t := range a.attempts[ip] {
		if now.Sub(t) < 5*time.Minute {
			recent = append(recent, t)
		}
	}
	blocked := len(recent) >= 8
	a.attempts[ip] = append(recent, now)
	a.mu.Unlock()
	if blocked {
		apiError(w, 429, "For mange loginforsøg. Prøv igen senere.")
		return
	}
	username := strings.ToLower(strings.TrimSpace(body.Username))
	accountID := ""
	var valid bool
	if username == "" || username == "admin" {
		// Backward compatible administrator account: same APP_PASSWORD in Rune.
		aHash := sha(a.cfg.Password)
		valid = hmac.Equal([]byte(aHash), []byte(sha(body.Password)))
	} else if accountUsernamePattern.MatchString(username) {
		rows, err := a.db.Query("SELECT id,password_hash,enabled FROM accounts WHERE username=? COLLATE NOCASE", username)
		if err == nil && len(rows)>0 && rows[0]["enabled"] == "1" {
			valid = checkAccountPassword(body.Password, rows[0]["password_hash"])
			if valid {accountID=rows[0]["id"]}
		}
	}
	if !valid {
		apiError(w, 401, "Forkert brugernavn eller adgangskode")
		return
	}
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		apiError(w, 500, "Session kunne ikke oprettes")
		return
	}
	id := hex.EncodeToString(b)
	expire := now.Add(30 * 24 * time.Hour).UTC()
	if e := a.db.Exec("INSERT INTO sessions(id,expires_at,account_id) VALUES(?,?,?)", sha(id), expire.Format(time.RFC3339), accountID); e != nil {
		apiError(w, 500, "Session kunne ikke gemmes")
		return
	}
	a.cookie(w, id+"."+mac(a.key, id), expire)
	user := identity{Username:"Administrator",Admin:true}
	if accountID!="" {
		rows, err := a.db.Query("SELECT username,person_id FROM accounts WHERE id=?",accountID)
		if err!=nil || len(rows)==0 {apiError(w,500,"Brugeren blev ikke fundet");return}
		user=identity{AccountID:accountID,Username:rows[0]["username"],PersonID:rows[0]["person_id"]}
	}
	writeJSON(w, 200, map[string]any{"authenticated": true, "csrf": mac(a.key, "csrf:"+id), "account":identityResponse(user)})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie("fh_session"); e == nil {
		parts := strings.Split(c.Value, ".")
		if len(parts) == 2 {
			_ = a.db.Exec("DELETE FROM sessions WHERE id=?", sha(parts[0]))
		}
	}
	a.cookie(w, "", time.Unix(0, 0))
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) withAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := a.accountIdentity(r)
		if !ok {
			apiError(w, 401, "Log ind for at fortsætte")
			return
		}
		id := a.sessionID(r)
		if r.Method != "GET" && r.Method != "HEAD" {
			provided := r.Header.Get("X-CSRF-Token")
			expected := mac(a.key, "csrf:"+id)
			if !hmac.Equal([]byte(provided), []byte(expected)) {
				apiError(w, 403, "Ugyldigt sikkerhedstoken")
				return
			}
		}
		h(w, r)
	}
}
func (a *App) state(w http.ResponseWriter, r *http.Request) {
 start, end, day := r.URL.Query().Get("start"), r.URL.Query().Get("end"), r.URL.Query().Get("day")
 if !validTime(start) || !validTime(end) || !validDay(day) {
  apiError(w, 400, "Ugyldigt dato-interval")
  return
 }
 st, en := mustTime(start), mustTime(end)
 if !en.After(st) || en.Sub(st) > 45*24*time.Hour {
  apiError(w, 400, "Maksimalt 45 dage pr. forespørgsel")
  return
 }
 people, err := a.people()
 if err != nil { apiError(w, 500, "Kunne ikke læse personer"); return }
 owners, err := a.calendarOwnerMap()
 if err != nil { apiError(w, 500, "Kalenderindstillinger kunne ikke læses"); return }
 ev, err := a.events(st,en)
 if err != nil { apiError(w, 500, "Kunne ikke læse kalenderen"); return }
 tasks,err := a.tasks(day)
 if err != nil { apiError(w, 500, "Kunne ikke læse opgaver"); return }
 warning := ""
 withHA := a.cfg.HAURL != "" && a.cfg.HAToken != "" && len(a.cfg.HACalendars)>0
 if withHA {
  external,e := a.haEvents(r.Context(),st,en)
  if e!=nil {warning="Home Assistant-kalendere: "+haFriendlyError(e);log.Printf("calendar integration error: %v",e)} else {
   ev=append(ev,applyCalendarOwners(external,people,owners)...)
  }
 }
 direct,issues:=a.directEvents(r.Context(),st,en,people)
 ev=append(ev,direct...)
 if len(issues)>0 {if warning!=""{warning+=" · "};warning+=strings.Join(issues," · ")}
 // Today's family panel must always use the actual Copenhagen calendar day,
 // independently of the date currently selected on the dashboard.
 now:=time.Now().In(a.loc)
 today:=now.Format("2006-01-02")
 todayStart:=time.Date(now.Year(),now.Month(),now.Day(),0,0,0,0,a.loc)
 todayEnd:=todayStart.AddDate(0,0,1)
 var todayEvents []Event
 if !st.After(todayStart) && !en.Before(todayEnd) {
  for _,e:=range ev {
   startAt,e1:=time.Parse(time.RFC3339,e.Start)
   endAt,e2:=time.Parse(time.RFC3339,e.End)
   if e1==nil && e2==nil && startAt.Before(todayEnd) && endAt.After(todayStart) {todayEvents=append(todayEvents,e)}
  }
 } else {
  todayEvents,err=a.events(todayStart,todayEnd)
  if err!=nil {apiError(w,500,"Kunne ikke læse dagens aftaler");return}
  if withHA {
   external,e:=a.haEvents(r.Context(),todayStart,todayEnd)
   if e!=nil {
    if warning=="" {warning="Home Assistant-kalendere: "+haFriendlyError(e)}
   } else {todayEvents=append(todayEvents,applyCalendarOwners(external,people,owners)...)}
  }
  directToday,_:=a.directEvents(r.Context(),todayStart,todayEnd,people)
  todayEvents=append(todayEvents,directToday...)
 }
 if todayEvents==nil {todayEvents=[]Event{}}
 writeJSON(w,200,map[string]any{"people":people,"events":ev,"today_events":todayEvents,"today_date":today,"tasks":tasks,"warning":warning})
}
func mustTime(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }
func (a *App) people() ([]Person, error) {
	rows, e := a.db.Query("SELECT id,name,color,notify_service,position FROM people ORDER BY position,name")
	if e != nil {
		return nil, e
	}
	out := []Person{}
	for _, r := range rows {
		n, _ := strconv.Atoi(r["position"])
		out = append(out, Person{ID: r["id"], Name: r["name"], Color: r["color"], NotifyService: r["notify_service"], Position: n})
	}
	return out, nil
}
func (a *App) events(start, end time.Time) ([]Event, error) {
	rows, e := a.db.Query("SELECT id,title,start_at,end_at,kind,person_id,pickup_by,location,notes FROM events WHERE start_at<? AND end_at>? ORDER BY start_at", end.UTC().Format(time.RFC3339), start.UTC().Format(time.RFC3339))
	if e != nil {
		return nil, e
	}
	out := []Event{}
	for _, r := range rows {
		out = append(out, Event{ID: r["id"], Title: r["title"], Start: r["start_at"], End: r["end_at"], Kind: r["kind"], PersonID: r["person_id"], PickupBy: r["pickup_by"], Location: r["location"], Notes: r["notes"]})
	}
	return out, nil
}

// weekStart returns Monday of the ISO week, using UTC calendar dates.
// This avoids daylight-saving changes when calculating week intervals.
func weekStart(t time.Time) time.Time {
	return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7))
}

// taskOccurrence reports visibility and the completion key for a selected date.
// Whole-week tasks share one completion key (the Monday of that week), so
// checking a task on Tuesday also marks it complete on Sunday of that week.
func taskOccurrence(due, target time.Time, rule string) (bool, string) {
	dayKey := target.Format("2006-01-02")
	switch rule {
	case "once_week", "weekly_window", "biweekly_window":
		anchorWeek, targetWeek := weekStart(due), weekStart(target)
		weeks := int(targetWeek.Sub(anchorWeek).Hours()) / (7 * 24)
		if weeks < 0 || (rule == "once_week" && weeks != 0) || (rule == "biweekly_window" && weeks%2 != 0) {
			return false, ""
		}
		return true, targetWeek.Format("2006-01-02")
	}
	if target.Before(due) {
		return false, ""
	}
	days := int(target.Sub(due).Hours()) / 24
	switch rule {
	case "daily":
		return true, dayKey
	case "weekdays":
		// Each weekday is a separate occurrence and has its own completion.
		// Saturday/Sunday are never due, even when the start date is a weekend.
		return target.Weekday() >= time.Monday && target.Weekday() <= time.Friday, dayKey
	case "weekly":
		return days%7 == 0, dayKey
	case "biweekly":
		return days%14 == 0, dayKey
	default:
		return days == 0, dayKey
	}
}

func (a *App) tasks(day string) ([]Task, error) {
	rows, err := a.db.Query("SELECT id,title,assigned_to,due_date,repeat_rule FROM tasks ORDER BY title")
	if err != nil {
		return nil, err
	}
	target, err := time.Parse("2006-01-02", day)
	if err != nil {
		return nil, err
	}
	out := []Task{}
	for _, r := range rows {
		due, err := time.Parse("2006-01-02", r["due_date"])
		if err != nil {
			continue
		}
		visible, completionKey := taskOccurrence(due, target, r["repeat_rule"])
		if !visible {
			continue
		}
		completed, err := a.db.Query("SELECT day FROM completions WHERE task_id=? AND day=?", r["id"], completionKey)
		if err != nil {
			return nil, err
		}
		out = append(out, Task{ID: r["id"], Title: r["title"], AssignedTo: r["assigned_to"], DueDate: r["due_date"], RepeatRule: r["repeat_rule"], Done: len(completed) > 0})
	}
	return out, nil
}
func (a *App) personSave(w http.ResponseWriter, r *http.Request) {
	var p Person
	if !readJSON(w, r, &p) {
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	if !bounded(p.Name, 60) || !color.MatchString(p.Color) || (p.NotifyService != "" && !ident.MatchString(p.NotifyService)) {
		if !bounded(p.Name, 60) || !color.MatchString(p.Color) {
			apiError(w, 400, "Udfyld et navn og vælg en gyldig farve")
		} else {
			apiError(w, 400, "Notify-service skal være f.eks. mobile_app_anton_iphone (uden notify. eller device_tracker.). Lad feltet stå tomt, hvis du ikke ved det endnu.")
		}
		return
	}
	if r.Method == "POST" {
		p.ID = newID()
		e := a.db.Exec("INSERT INTO people(id,name,color,notify_service,position) VALUES(?,?,?,?,?)", p.ID, p.Name, p.Color, p.NotifyService, strconv.Itoa(p.Position))
		if e != nil {
			apiError(w, 500, "Kunne ikke oprette person")
			return
		}
	} else {
		p.ID = r.PathValue("id")
		if !validID(p.ID) {
			apiError(w, 400, "Ugyldigt ID")
			return
		}
		e := a.db.Exec("UPDATE people SET name=?,color=?,notify_service=?,position=? WHERE id=?", p.Name, p.Color, p.NotifyService, strconv.Itoa(p.Position), p.ID)
		if e != nil {
			apiError(w, 500, "Kunne ikke gemme person")
			return
		}
	}
	a.broadcast()
	writeJSON(w, 200, p)
}
func validID(s string) bool {
	if len(s) != 24 {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
func (a *App) personDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		apiError(w, 400, "Ugyldigt ID")
		return
	}
	if e := a.db.Exec("DELETE FROM people WHERE id=?", id); e != nil {
		apiError(w, 500, "Kunne ikke slette person")
		return
	}
	a.broadcast()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) eventSave(w http.ResponseWriter, r *http.Request) {
	var ev Event
	if !readJSON(w, r, &ev) {
		return
	}
	ev.Title = strings.TrimSpace(ev.Title)
	s, e := time.Parse(time.RFC3339, ev.Start)
	end, ee := time.Parse(time.RFC3339, ev.End)
	if !bounded(ev.Title, 150) || e != nil || ee != nil || !end.After(s) || end.Sub(s) > 14*24*time.Hour || len([]rune(ev.Kind)) > 30 || len([]rune(ev.Location)) > 200 || len([]rune(ev.Notes)) > 2000 {
		apiError(w, 400, "Kontrollér titel, datoer og feltlængder")
		return
	}
	ev.Start = s.UTC().Format(time.RFC3339)
	ev.End = end.UTC().Format(time.RFC3339)
	if ev.Kind == "" {
		ev.Kind = "aftale"
	}
	if r.Method == "POST" {
		ev.ID = newID()
		e = a.db.Exec("INSERT INTO events(id,title,start_at,end_at,kind,person_id,pickup_by,location,notes,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", ev.ID, ev.Title, ev.Start, ev.End, ev.Kind, ev.PersonID, ev.PickupBy, ev.Location, ev.Notes, time.Now().UTC().Format(time.RFC3339))
	} else {
		ev.ID = r.PathValue("id")
		if !validID(ev.ID) {
			apiError(w, 400, "Ugyldigt ID")
			return
		}
		e = a.db.Exec("UPDATE events SET title=?,start_at=?,end_at=?,kind=?,person_id=?,pickup_by=?,location=?,notes=? WHERE id=?", ev.Title, ev.Start, ev.End, ev.Kind, ev.PersonID, ev.PickupBy, ev.Location, ev.Notes, ev.ID)
	}
	if e != nil {
		apiError(w, 500, "Kunne ikke gemme aftalen")
		return
	}
	a.broadcast()
	writeJSON(w, 200, ev)
}
func (a *App) eventDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		apiError(w, 400, "Ugyldigt ID")
		return
	}
	if e := a.db.Exec("DELETE FROM events WHERE id=?", id); e != nil {
		apiError(w, 500, "Kunne ikke slette aftalen")
		return
	}
	a.broadcast()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) taskSave(w http.ResponseWriter, r *http.Request) {
	var t Task
	if !readJSON(w, r, &t) {
		return
	}
	t.Title = strings.TrimSpace(t.Title)
	if !bounded(t.Title, 150) || !validDay(t.DueDate) || !validRepeatRule(t.RepeatRule) {
		apiError(w, 400, "Kontrollér titel, dato og gentagelse")
		return
	}
	var e error
	if r.Method == "POST" {
		t.ID = newID()
		e = a.db.Exec("INSERT INTO tasks(id,title,assigned_to,due_date,repeat_rule) VALUES(?,?,?,?,?)", t.ID, t.Title, t.AssignedTo, t.DueDate, t.RepeatRule)
	} else {
		t.ID = r.PathValue("id")
		if !validID(t.ID) {
			apiError(w, 400, "Ugyldigt ID")
			return
		}
		e = a.db.Exec("UPDATE tasks SET title=?,assigned_to=?,due_date=?,repeat_rule=? WHERE id=?", t.Title, t.AssignedTo, t.DueDate, t.RepeatRule, t.ID)
	}
	if e != nil {
		apiError(w, 500, "Kunne ikke gemme opgaven")
		return
	}
	a.broadcast()
	writeJSON(w, 200, t)
}
func (a *App) taskDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		apiError(w, 400, "Ugyldigt ID")
		return
	}
	if e := a.db.Exec("DELETE FROM tasks WHERE id=?", id); e != nil {
		apiError(w, 500, "Kunne ikke slette opgaven")
		return
	}
	a.broadcast()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) toggle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		apiError(w, 400, "Ugyldigt ID")
		return
	}
	var v struct {
		Day  string `json:"day"`
		Done bool   `json:"done"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	if !validDay(v.Day) {
		apiError(w, 400, "Ugyldig dato")
		return
	}
	rows, err := a.db.Query("SELECT due_date,repeat_rule FROM tasks WHERE id=?", id)
	if err != nil {
		apiError(w, 500, "Kunne ikke læse opgaven")
		return
	}
	if len(rows) == 0 {
		apiError(w, 404, "Opgaven findes ikke")
		return
	}
	due, err := time.Parse("2006-01-02", rows[0]["due_date"])
	if err != nil {
		apiError(w, 500, "Ugyldig startdato for opgaven")
		return
	}
	selected, _ := time.Parse("2006-01-02", v.Day)
	visible, completionKey := taskOccurrence(due, selected, rows[0]["repeat_rule"])
	if !visible {
		apiError(w, 400, "Opgaven er ikke aktiv på den valgte dag")
		return
	}
	if v.Done {
		if err := a.db.Exec("INSERT OR IGNORE INTO completions(task_id,day) VALUES(?,?)", id, completionKey); err != nil {
			apiError(w, 500, "Opgaven kunne ikke afsluttes")
			return
		}
	} else {
		if err := a.db.Exec("DELETE FROM completions WHERE task_id=? AND day=?", id, completionKey); err != nil {
			apiError(w, 500, "Opgaven kunne ikke genåbnes")
			return
		}
	}
	a.broadcast()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) stream(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		apiError(w, 500, "Streaming understøttes ikke")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := make(chan string, 1)
	a.mu.Lock()
	a.listeners[ch] = struct{}{}
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.listeners, ch); a.mu.Unlock() }()
	fmt.Fprint(w, "event: connected\ndata: ok\n\n")
	f.Flush()
	keep := time.NewTicker(25 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			fmt.Fprint(w, "event: changed\ndata: update\n\n")
			f.Flush()
		case <-keep.C:
			fmt.Fprint(w, ": ping\n\n")
			f.Flush()
		}
	}
}
func (a *App) integrations(w http.ResponseWriter, r *http.Request) {
	configured := a.cfg.HAURL != "" && a.cfg.HAToken != ""
	result := map[string]any{
		"ha_configured":     configured,
		"ha_online":         false,
		"calendar_entities": a.cfg.HACalendars,
		"ha_error":          "",
		"calendar_error":    "",
		"calendar_ok":       false,
	}
	if a.cfg.HAURL == "" {
		result["ha_error"] = "HA_URL mangler i Yggdrasil-serverens indstillinger"
	} else if a.cfg.HAToken == "" {
		result["ha_error"] = "HA_TOKEN mangler i Yggdrasil-serverens indstillinger"
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
		defer cancel()
		connected, diagnosis := a.haProbe(ctx)
		result["ha_online"] = connected
		result["ha_error"] = diagnosis
		if connected && len(a.cfg.HACalendars) != 0 {
			start := time.Now().In(a.loc)
			_, err := a.haEvents(ctx, start, start.Add(7*24*time.Hour))
			if err != nil {
				result["calendar_error"] = haFriendlyError(err)
			} else {
				result["calendar_ok"] = true
			}
		}
	}
	writeJSON(w, 200, result)
}
