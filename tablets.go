package main

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kw-aficionado/familiehub/internal/store"
)

const tabletCookie = "fh_tablet"
const tabletIdleLifetime = 30 * 24 * time.Hour
const tabletAbsoluteLifetime = 180 * 24 * time.Hour
const tabletPairLifetime = 10 * time.Minute

type tabletDisplay struct {
	Theme      string `json:"theme"`
	NightFrom  string `json:"nightFrom"`
	NightTo    string `json:"nightTo"`
	SaverMode  string `json:"saverMode"`
	SaverFrom  string `json:"saverFrom"`
	SaverTo    string `json:"saverTo"`
	SaverDelay int    `json:"saverDelay"`
}
type tabletConfig struct {
	Name         string         `json:"name"`
	CanComplete  bool           `json:"can_complete"`
	PersonFilter string         `json:"person_filter"`
	Display      *tabletDisplay `json:"display"`
}
type tabletDevice struct {
	ID string `json:"id"`
	tabletConfig
	CreatedAt       string `json:"created_at"`
	LastSeen        string `json:"last_seen,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	AbsoluteExpires string `json:"absolute_expires,omitempty"`
	PairExpires     string `json:"pair_expires,omitempty"`
	RevokedAt       string `json:"revoked_at,omitempty"`
	Status          string `json:"status"`
}
type tabletSession struct {
	Device tabletDevice
	Token  string
}

func defaultTabletDisplay() *tabletDisplay {
	return &tabletDisplay{Theme: "auto", NightFrom: "21:00", NightTo: "07:00", SaverMode: "scheduled", SaverFrom: "20:00", SaverTo: "07:00", SaverDelay: 10}
}
func tabletClock(s string) bool {
	t, e := time.Parse("15:04", s)
	return e == nil && t.Format("15:04") == s
}
func (a *App) validateTablet(v *tabletConfig) error {
	v.Name = strings.TrimSpace(v.Name)
	if n := utf8.RuneCountInString(v.Name); n < 1 || n > 80 || strings.ContainsAny(v.Name, "\x00\r\n") {
		return badPlan(400, "Navnet skal have 1-80 tegn")
	}
	if v.PersonFilter != "" {
		rows, e := a.db.Query("SELECT id FROM people WHERE id=?", v.PersonFilter)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return badPlan(400, "Familiemedlemmet findes ikke")
		}
	}
	if v.Display == nil {
		v.Display = defaultTabletDisplay()
	}
	d := v.Display
	if (d.Theme != "auto" && d.Theme != "light" && d.Theme != "dark") || (d.SaverMode != "scheduled" && d.SaverMode != "always") {
		return badPlan(400, "Ugyldig sk\u00e6rmtilstand")
	}
	if !tabletClock(d.NightFrom) || !tabletClock(d.NightTo) || !tabletClock(d.SaverFrom) || !tabletClock(d.SaverTo) || d.SaverFrom == d.SaverTo {
		return badPlan(400, "V\u00e6lg gyldige og forskellige start- og sluttider")
	}
	switch d.SaverDelay {
	case 0, 5, 10, 20, 30:
	default:
		return badPlan(400, "V\u00e6lg en gyldig inaktivitetstid")
	}
	return nil
}
func (a *App) tabletRoutes() {
	a.mux.HandleFunc("GET /tablet", func(w http.ResponseWriter, r *http.Request) {
		b, e := assets.ReadFile("web/tablet.html")
		if e != nil {
			http.Error(w, "Sk\u00e6rmen kunne ikke indl\u00e6ses", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
	a.mux.HandleFunc("GET /tablet/{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tablet", http.StatusTemporaryRedirect)
	})
	a.mux.HandleFunc("GET /api/tablets", a.withAdmin(a.tabletsList))
	a.mux.HandleFunc("POST /api/tablets", a.withAdmin(a.tabletsCreate))
	a.mux.HandleFunc("PUT /api/tablets/{id}", a.withAdmin(a.tabletsUpdate))
	a.mux.HandleFunc("POST /api/tablets/{id}/pair", a.withAdmin(a.tabletsRepair))
	a.mux.HandleFunc("POST /api/tablets/{id}/revoke", a.withAdmin(a.tabletsRevoke))
	a.mux.HandleFunc("POST /api/tablet/pair", a.tabletPair)
	a.mux.HandleFunc("GET /api/tablet/session", a.tabletSessionGet)
	a.mux.HandleFunc("GET /api/tablet/state", a.tabletState)
	a.mux.HandleFunc("POST /api/tablet/tasks/{id}/toggle", a.tabletToggle)
	a.mux.HandleFunc("POST /api/tablet/disconnect", a.tabletDisconnect)
}
func tabletError(w http.ResponseWriter, err error) {
	var invalid *planError
	if errors.As(err, &invalid) {
		apiError(w, invalid.code, invalid.message)
	} else {
		apiError(w, 500, "Tablet-adgangen kunne ikke behandles. Pr\u00f8v igen.")
	}
}
func tabletRecord(r map[string]string, now time.Time) tabletDevice {
	d := tabletDevice{ID: r["id"], CreatedAt: r["created_at"], LastSeen: r["last_seen"], ExpiresAt: r["idle_expires"], AbsoluteExpires: r["absolute_expires"], PairExpires: r["pair_expires"], RevokedAt: r["revoked_at"]}
	d.Name = r["name"]
	d.CanComplete = r["can_complete"] == "1"
	d.PersonFilter = r["person_filter"]
	d.Display = defaultTabletDisplay()
	_ = json.Unmarshal([]byte(r["display_json"]), d.Display)
	at := now.UTC().Format(time.RFC3339)
	switch {
	case d.RevokedAt != "":
		d.Status = "Sp\u00e6rret"
	case d.ExpiresAt > at && d.AbsoluteExpires > at:
		d.Status = "Forbundet"
	case d.PairExpires > at:
		d.Status = "Afventer parring"
	default:
		d.Status = "Udl\u00f8bet - opret en ny kode"
	}
	return d
}
func (a *App) tabletsList(w http.ResponseWriter, r *http.Request) {
	rows, e := a.db.Query("SELECT d.*,s.last_seen,s.idle_expires,s.absolute_expires FROM tablet_devices d LEFT JOIN tablet_sessions s ON s.device_id=d.id ORDER BY d.created_at,d.id")
	if e != nil {
		tabletError(w, e)
		return
	}
	out := []tabletDevice{}
	for _, row := range rows {
		out = append(out, tabletRecord(row, a.timeNow()))
	}
	writeJSON(w, 200, map[string]any{"devices": out, "tablet_path": "/tablet"})
}
func pairCode() (string, error) {
	b := make([]byte, 8)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	code := base32.NewEncoding("ABCDEFGHJKLMNPQRSTUVWXYZ23456789").WithPadding(base32.NoPadding).EncodeToString(b)[:12]
	return code[:4] + "-" + code[4:8] + "-" + code[8:], nil
}
func normalizePairCode(v string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\t", "", "\n", "").Replace(strings.TrimSpace(v)))
}

// Public tablet credentials require HTTPS. Plain HTTP is allowed only for
// explicitly configured loopback development, never inferred from proxy headers.
func (a *App) tabletTransportReady(r *http.Request) bool {
	u, e := url.Parse(a.cfg.AppURL)
	if e != nil || u.User != nil {
		return false
	}
	if r.TLS != nil || u.Scheme == "https" && u.Host != "" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}
func (a *App) tabletsCreate(w http.ResponseWriter, r *http.Request) {
	if !a.tabletTransportReady(r) {
		apiError(w, 400, "Tablet-adgang kr\u00e6ver APP_URL med HTTPS")
		return
	}
	var v tabletConfig
	if !readJSON(w, r, &v) {
		return
	}
	if e := a.validateTablet(&v); e != nil {
		tabletError(w, e)
		return
	}
	code, e := pairCode()
	if e != nil {
		tabletError(w, e)
		return
	}
	id := newID()
	now := a.timeNow()
	expiry := now.Add(tabletPairLifetime).UTC().Format(time.RFC3339)
	display, _ := json.Marshal(v.Display)
	allowed := "0"
	if v.CanComplete {
		allowed = "1"
	}
	e = a.db.Transaction(func(tx *store.Tx) error {
		rows, e := tx.Query("SELECT COUNT(*) AS n FROM tablet_devices")
		if e != nil {
			return e
		}
		if parseNumber(rows[0]["n"], 0) >= 100 {
			return badPlan(400, "Der er allerede 100 tablets. Genbrug en eksisterende enhed med en ny kode.")
		}
		return tx.Exec("INSERT INTO tablet_devices(id,name,can_complete,person_filter,display_json,created_at,pair_hash,pair_expires) VALUES(?,?,?,?,?,?,?,?)", id, v.Name, allowed, v.PersonFilter, string(display), now.UTC().Format(time.RFC3339), mac(a.key, "tablet-pair:"+normalizePairCode(code)), expiry)
	})
	if e != nil {
		tabletError(w, e)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "pair_code": code, "pair_expires": expiry, "tablet_path": "/tablet"})
}
func (a *App) tabletsUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		apiError(w, 400, "Ugyldigt enheds-id")
		return
	}
	var v tabletConfig
	if !readJSON(w, r, &v) {
		return
	}
	if e := a.validateTablet(&v); e != nil {
		tabletError(w, e)
		return
	}
	display, _ := json.Marshal(v.Display)
	allowed := "0"
	if v.CanComplete {
		allowed = "1"
	}
	e := a.db.Transaction(func(tx *store.Tx) error {
		rows, e := tx.Query("SELECT id FROM tablet_devices WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(rows) != 1 {
			return badPlan(404, "Enheden findes ikke")
		}
		return tx.Exec("UPDATE tablet_devices SET name=?,can_complete=?,person_filter=?,display_json=? WHERE id=?", v.Name, allowed, v.PersonFilter, string(display), id)
	})
	if e != nil {
		tabletError(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) tabletsRepair(w http.ResponseWriter, r *http.Request) {
	if !a.tabletTransportReady(r) {
		apiError(w, 400, "Tablet-adgang kr\u00e6ver APP_URL med HTTPS")
		return
	}
	id := r.PathValue("id")
	if !validID(id) {
		apiError(w, 400, "Ugyldigt enheds-id")
		return
	}
	var v struct{}
	if !readJSON(w, r, &v) {
		return
	}
	code, e := pairCode()
	if e != nil {
		tabletError(w, e)
		return
	}
	expiry := a.timeNow().Add(tabletPairLifetime).UTC().Format(time.RFC3339)
	e = a.db.Transaction(func(tx *store.Tx) error {
		rows, e := tx.Query("SELECT id FROM tablet_devices WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(rows) != 1 {
			return badPlan(404, "Enheden findes ikke")
		}
		if e = tx.Exec("DELETE FROM tablet_sessions WHERE device_id=?", id); e != nil {
			return e
		}
		return tx.Exec("UPDATE tablet_devices SET revoked_at='',pair_hash=?,pair_expires=? WHERE id=?", mac(a.key, "tablet-pair:"+normalizePairCode(code)), expiry, id)
	})
	if e != nil {
		tabletError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "pair_code": code, "pair_expires": expiry, "tablet_path": "/tablet"})
}
func (a *App) tabletsRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		apiError(w, 400, "Ugyldigt enheds-id")
		return
	}
	var v struct{}
	if !readJSON(w, r, &v) {
		return
	}
	e := a.db.Transaction(func(tx *store.Tx) error {
		rows, e := tx.Query("SELECT id FROM tablet_devices WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(rows) != 1 {
			return badPlan(404, "Enheden findes ikke")
		}
		if e = tx.Exec("DELETE FROM tablet_sessions WHERE device_id=?", id); e != nil {
			return e
		}
		return tx.Exec("UPDATE tablet_devices SET revoked_at=?,pair_hash='',pair_expires='' WHERE id=?", a.timeNow().UTC().Format(time.RFC3339), id)
	})
	if e != nil {
		tabletError(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// JSON + custom header prevents cross-origin form login; Origin and Fetch
// Metadata add defense in depth. Proxy headers never determine origin or IP.
func (a *App) tabletSameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	allowed, _ := url.Parse(a.cfg.AppURL)
	got, e := url.Parse(origin)
	if e != nil || got.Scheme == "" || got.Host == "" || got.User != nil || got.Path != "" || got.RawQuery != "" || got.Fragment != "" {
		return false
	}
	return allowed != nil && strings.EqualFold(got.Scheme, allowed.Scheme) && strings.EqualFold(got.Host, allowed.Host)
}
func (a *App) tabletSetCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	maxAge := int(expires.Sub(a.timeNow()).Seconds())
	if token == "" {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{Name: tabletCookie, Value: token, Path: "/api/tablet", HttpOnly: true, Secure: r.TLS != nil || strings.HasPrefix(a.cfg.AppURL, "https://"), SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: maxAge})
}
func tabletToken(r *http.Request) string {
	c, e := r.Cookie(tabletCookie)
	if e != nil || len(c.Value) != 64 {
		return ""
	}
	b, e := hex.DecodeString(c.Value)
	if e != nil || len(b) != 32 {
		return ""
	}
	return c.Value
}
func (a *App) tabletPair(w http.ResponseWriter, r *http.Request) {
	if !a.tabletTransportReady(r) {
		apiError(w, 400, "Tablet-adgang kr\u00e6ver APP_URL med HTTPS")
		return
	}
	if r.Header.Get("X-FH-Pairing") != "1" || !a.tabletSameOrigin(r) {
		apiError(w, 403, "Parring skal ske p\u00e5 FamilieHubs egen tablet-side")
		return
	}
	var v struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	now := a.timeNow()
	if wait := a.pairingAdmission(remoteIP(r), now); wait > 0 {
		tooManyRequests(w, wait)
		return
	}
	code := normalizePairCode(v.Code)
	if len(code) != 12 {
		apiError(w, 400, "Koden er forkert eller udl\u00f8bet")
		return
	}
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		tabletError(w, e)
		return
	}
	token := hex.EncodeToString(b)
	// Pairing deliberately exits any ordinary account session on THIS browser,
	// preventing a hidden administrator cookie from bypassing tablet restrictions.
	ordinarySession := a.sessionID(r)
	oldToken := tabletToken(r)
	expiry := now.Add(tabletIdleLifetime)
	absolute := now.Add(tabletAbsoluteLifetime)
	e := a.db.Transaction(func(tx *store.Tx) error {
		rows, e := tx.Query("SELECT id FROM tablet_devices WHERE pair_hash=? AND pair_expires>? AND revoked_at=''", mac(a.key, "tablet-pair:"+code), now.UTC().Format(time.RFC3339))
		if e != nil {
			return e
		}
		if len(rows) != 1 {
			return badPlan(400, "Koden er forkert eller udl\u00f8bet")
		}
		id := rows[0]["id"]
		if e = tx.Exec("DELETE FROM tablet_sessions WHERE device_id=? OR token_hash=?", id, mac(a.key, "tablet-session:"+oldToken)); e != nil {
			return e
		}
		if e = tx.Exec("INSERT INTO tablet_sessions(device_id,token_hash,paired_at,last_seen,idle_expires,absolute_expires) VALUES(?,?,?,?,?,?)", id, mac(a.key, "tablet-session:"+token), now.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339), expiry.UTC().Format(time.RFC3339), absolute.UTC().Format(time.RFC3339)); e != nil {
			return e
		}
		if e = tx.Exec("UPDATE tablet_devices SET pair_hash='',pair_expires='' WHERE id=?", id); e != nil {
			return e
		}
		if ordinarySession != "" {
			return tx.Exec("DELETE FROM sessions WHERE id=?", sha(ordinarySession))
		}
		return nil
	})
	if e != nil {
		tabletError(w, e)
		return
	}
	a.tabletSetCookie(w, r, token, expiry)
	if ordinarySession != "" {
		a.cookie(w, "", time.Unix(0, 0))
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) tabletInTx(tx *store.Tx, token string, now time.Time) (tabletDevice, error) {
	if token == "" {
		return tabletDevice{}, badPlan(401, "Tablet-adgangen er udl\u00f8bet eller fjernet. Bed administratoren om en ny kode.")
	}
	rows, e := tx.Query("SELECT d.*,s.last_seen,s.idle_expires,s.absolute_expires FROM tablet_devices d JOIN tablet_sessions s ON d.id=s.device_id WHERE s.token_hash=? AND d.revoked_at='' AND s.idle_expires>? AND s.absolute_expires>?", mac(a.key, "tablet-session:"+token), now.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	if e != nil {
		return tabletDevice{}, e
	}
	if len(rows) != 1 {
		return tabletDevice{}, badPlan(401, "Tablet-adgangen er udl\u00f8bet eller fjernet. Bed administratoren om en ny kode.")
	}
	return tabletRecord(rows[0], now), nil
}
func (a *App) tabletAuthenticate(w http.ResponseWriter, r *http.Request) (tabletSession, bool) {
	token := tabletToken(r)
	now := a.timeNow()
	var device tabletDevice
	var renewal time.Time
	e := a.db.Transaction(func(tx *store.Tx) error {
		var e error
		device, e = a.tabletInTx(tx, token, now)
		if e != nil {
			return e
		}
		last, _ := time.Parse(time.RFC3339, device.LastSeen)
		if now.Sub(last) >= 5*time.Minute {
			renewal = now.Add(tabletIdleLifetime)
			absolute, _ := time.Parse(time.RFC3339, device.AbsoluteExpires)
			if renewal.After(absolute) {
				renewal = absolute
			}
			if e = tx.Exec("UPDATE tablet_sessions SET last_seen=?,idle_expires=? WHERE device_id=?", now.UTC().Format(time.RFC3339), renewal.UTC().Format(time.RFC3339), device.ID); e != nil {
				return e
			}
			device.LastSeen = now.UTC().Format(time.RFC3339)
			device.ExpiresAt = renewal.UTC().Format(time.RFC3339)
		}
		return nil
	})
	if e != nil {
		var invalid *planError
		if errors.As(e, &invalid) && invalid.code == 401 {
			a.tabletSetCookie(w, r, "", time.Unix(0, 0))
		}
		tabletError(w, e)
		return tabletSession{}, false
	}
	if !renewal.IsZero() {
		a.tabletSetCookie(w, r, token, renewal)
	}
	return tabletSession{Device: device, Token: token}, true
}
func (a *App) tabletSessionGet(w http.ResponseWriter, r *http.Request) {
	s, ok := a.tabletAuthenticate(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"device": s.Device, "csrf": mac(a.key, "tablet-csrf:"+s.Token)})
}
func (a *App) tabletState(w http.ResponseWriter, r *http.Request) {
	s, ok := a.tabletAuthenticate(w, r)
	if !ok {
		return
	}
	day := r.URL.Query().Get("day")
	if day == "" {
		day = a.timeNow().In(a.loc).Format("2006-01-02")
	}
	if !validDay(day) {
		apiError(w, 400, "Ugyldig dato")
		return
	}
	st, _ := time.ParseInLocation("2006-01-02", day, a.loc)
	en := st.AddDate(0, 0, 1)
	people, e := a.people()
	if e != nil {
		tabletError(w, e)
		return
	}
	owners, e := a.calendarOwnerMap()
	if e != nil {
		tabletError(w, e)
		return
	}
	events, e := a.events(st, en)
	if e != nil {
		tabletError(w, e)
		return
	}
	issues := []string{}
	if a.cfg.HAURL != "" && a.cfg.HAToken != "" && len(a.cfg.HACalendars) > 0 {
		external, err := a.haEvents(r.Context(), st, en)
		if err != nil {
			issues = append(issues, "Home Assistant-kalender kunne ikke opdateres")
		} else {
			events = append(events, applyCalendarOwners(external, people, owners)...)
		}
	}
	direct, failures := a.directEvents(r.Context(), st, en, people)
	events = append(events, direct...)
	if len(failures) > 0 {
		issues = append(issues, "En direkte kalender kunne ikke opdateres")
	}
	tasks, e := a.tasks(day)
	if e != nil {
		tabletError(w, e)
		return
	}
	// Explicit projections: never send notification services, event notes,
	// connection identifiers, private calendar URLs or plan diagnostics to a wall display.
	safePeople := []map[string]any{}
	for _, p := range people {
		safePeople = append(safePeople, map[string]any{"id": p.ID, "name": p.Name, "color": p.Color})
	}
	safeEvents := []map[string]any{}
	for _, ev := range events {
		safeEvents = append(safeEvents, map[string]any{"id": sha(ev.ID), "title": ev.Title, "start_at": ev.Start, "end_at": ev.End, "person_id": ev.PersonID, "person_ids": ev.PersonIDs, "pickup_by": ev.PickupBy})
	}
	safeTasks := []map[string]any{}
	for _, t := range tasks {
		safeTasks = append(safeTasks, map[string]any{"id": t.ID, "title": t.Title, "assigned_to": t.AssignedTo, "done": t.Done, "repeat_rule": t.RepeatRule})
	}
	// Recheck after external I/O so revocation while a calendar is loading cannot
	// release a new response after the final authorization check.
	e = a.db.Transaction(func(tx *store.Tx) error {
		var err error
		s.Device, err = a.tabletInTx(tx, s.Token, a.timeNow())
		return err
	})
	if e != nil {
		tabletError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"device": s.Device, "csrf": mac(a.key, "tablet-csrf:"+s.Token), "day": day, "today": a.timeNow().In(a.loc).Format("2006-01-02"), "people": safePeople, "events": safeEvents, "tasks": safeTasks, "warning": strings.Join(issues, ". ")})
}
func (a *App) tabletCSRF(r *http.Request, token string) bool {
	return token != "" && a.tabletSameOrigin(r) && hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(mac(a.key, "tablet-csrf:"+token)))
}
func (a *App) tabletToggle(w http.ResponseWriter, r *http.Request) {
	token := tabletToken(r)
	if !a.tabletCSRF(r, token) {
		apiError(w, 403, "Ugyldigt sikkerhedstoken")
		return
	}
	var v struct {
		Day  string `json:"day"`
		Done bool   `json:"done"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	id := r.PathValue("id")
	if !validID(id) || !validDay(v.Day) {
		apiError(w, 400, "Ugyldig opgave eller dato")
		return
	}
	e := a.db.Transaction(func(tx *store.Tx) error {
		now := a.timeNow()
		d, e := a.tabletInTx(tx, token, now)
		if e != nil {
			return e
		}
		if !d.CanComplete {
			return badPlan(403, "Denne tablet har kun visningsadgang")
		}
		if v.Day != now.In(a.loc).Format("2006-01-02") {
			return badPlan(400, "Tabletten kan kun afkrydse dagens opgaver")
		}
		return writeTaskCompletion(tx, id, v.Day, v.Done)
	})
	if e != nil {
		tabletError(w, e)
		return
	}
	a.broadcast()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) tabletDisconnect(w http.ResponseWriter, r *http.Request) {
	token := tabletToken(r)
	if !a.tabletCSRF(r, token) {
		apiError(w, 403, "Ugyldigt sikkerhedstoken")
		return
	}
	var v struct{}
	if !readJSON(w, r, &v) {
		return
	}
	e := a.db.Exec("DELETE FROM tablet_sessions WHERE token_hash=?", mac(a.key, "tablet-session:"+token))
	if e != nil {
		tabletError(w, e)
		return
	}
	a.tabletSetCookie(w, r, "", time.Unix(0, 0))
	writeJSON(w, 200, map[string]bool{"ok": true})
}
