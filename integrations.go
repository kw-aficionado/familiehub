package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

func (a *App) haEvents(ctx context.Context, start, end time.Time) ([]Event, error) {
	out := []Event{}
	for _, entity := range a.cfg.HACalendars {
		if !strings.HasPrefix(entity, "calendar.") {
			return out, fmt.Errorf("invalid HA calendar entity %q", entity)
		}
		endpoint := a.cfg.HAURL + "/api/calendars/" + url.PathEscape(entity) + "?start=" + url.QueryEscape(start.Format(time.RFC3339)) + "&end=" + url.QueryEscape(end.Format(time.RFC3339))
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("Authorization", "Bearer "+a.cfg.HAToken)
		resp, err := a.client.Do(req)
		if err != nil {
			return out, err
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if err != nil {
			return out, err
		}
		if resp.StatusCode != 200 {
			return out, &haHTTPError{Code: resp.StatusCode, Entity: entity}
		}
		var payload []struct {
			Start       json.RawMessage `json:"start"`
			End         json.RawMessage `json:"end"`
			Summary     string          `json:"summary"`
			Location    string          `json:"location"`
			Description string          `json:"description"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return out, err
		}
		for _, e := range payload {
			st := toISO(haTimeValue(e.Start), a.loc)
			en := toISO(haTimeValue(e.End), a.loc)
			if st == "" || en == "" {
				continue
			}
			out = append(out, Event{ID: "ha:" + entity + ":" + sha(e.Summary + st + en)[:16], Title: e.Summary, Start: st, End: en, Kind: "kalender", Location: e.Location, Notes: e.Description, External: true, SourceCalendar: entity})
		}
	}
	return out, nil
}

// Recognised hashtags in a Google/iCloud calendar title are display metadata only.
// HA remains read-only and the original calendar event is never modified.
// Unknown tags remain in the visible title. Tags can occur anywhere as whole tokens.
var calendarTagPattern = regexp.MustCompile(`(?i)(^|[[:space:]])#([\p{L}\p{N}_-]+)`)

var calendarCategories = map[string]bool{
	"sport": true, "skole": true, "arbejde": true, "hjemme": true,
	"familie": true, "fritid": true, "privat": true, "legeaftale": true,
}

func decorateExternalEvent(ev Event, people []Person) Event {
	title := ev.Title
	var visible strings.Builder
	previous := 0
	seen := map[string]bool{}
	for _, match := range calendarTagPattern.FindAllStringSubmatchIndex(title, -1) {
		tag := title[match[4]:match[5]]
		recognized := false
		for _, person := range people {
			// #AnneMarie also matches the person named "Anne Marie".
			alias := strings.Join(strings.Fields(person.Name), "")
			if strings.EqualFold(alias, tag) && person.ID != "" {
				recognized = true
				if !seen[person.ID] {
					ev.PersonIDs = append(ev.PersonIDs, person.ID)
					seen[person.ID] = true
				}
			}
		}
		if !recognized && calendarCategories[strings.ToLower(tag)] {
			recognized = true
			if ev.Category == "" {
				ev.Category = strings.ToLower(tag)
			}
		}
		if !recognized {
			continue
		}
		visible.WriteString(title[previous:match[0]])
		// The whole match includes the preceding whitespace when present.
		visible.WriteByte(' ')
		previous = match[1]
	}
	if previous == 0 { // No recognised tags; preserve the event as provided.
		return ev
	}
	visible.WriteString(title[previous:])
	cleaned := strings.Join(strings.Fields(visible.String()), " ")
	if cleaned != "" {
		ev.Title = cleaned
	}
	if len(ev.PersonIDs) > 0 {
		ev.PersonID = ev.PersonIDs[0] // compatibility with existing colour/identity code
	}
	return ev
}

// Home Assistant's REST calendar API uses {"dateTime": "..."} or {"date": "..."}.
// Older test fixtures and third-party adapters sometimes return a string.
func haTimeValue(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var fields struct {
		DateTime string `json:"dateTime"`
		Date     string `json:"date"`
	}
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	if fields.DateTime != "" {
		return fields.DateTime
	}
	return fields.Date
}

type haHTTPError struct {
	Code   int
	Entity string
}

func (e *haHTTPError) Error() string {
	if e.Entity == "" {
		return fmt.Sprintf("HA API HTTP %d", e.Code)
	}
	return fmt.Sprintf("HA calendar %s HTTP %d", e.Entity, e.Code)
}

// Only return safe, actionable diagnostics to an authenticated household user.
// Do not leak tokens or echo potentially sensitive network error strings.
func haFriendlyError(err error) string {
	if err == nil {
		return ""
	}
	var httpErr *haHTTPError
	if errors.As(err, &httpErr) {
		prefix := "Home Assistant API"
		if httpErr.Entity != "" {
			prefix = httpErr.Entity
		}
		switch httpErr.Code {
		case 301, 302, 303, 307, 308:
			return prefix + ": omdirigering – kontrollér HA_URL (skal pege på HA API og ikke et login)"
		case 400, 404:
			if httpErr.Entity != "" {
				return prefix + ": ikke fundet eller ugyldigt kalender-entity-id; kontrollér HA_CALENDARS"
			}
			return prefix + ": API-endpoint ikke fundet; kontrollér HA_URL"
		case 401:
			return prefix + ": HTTP 401 – token er ugyldigt eller udløbet"
		case 403:
			return prefix + ": HTTP 403 – kontoen har ikke adgang til ressourcen"
		default:
			return fmt.Sprintf("%s: HTTP %d – se Home Assistant-loggen", prefix, httpErr.Code)
		}
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "DNS-fejl – Yggdrasil-serveren kan ikke slå HA-værtsnavnet op"
	}
	var x509err x509.UnknownAuthorityError
	if errors.As(err, &x509err) {
		return "TLS-certifikat kan ikke verificeres – brug gyldigt certifikat eller intern HTTP via privat net"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "Forbindelse afvist – kontrollér HA-port, firewall og HA_URL fra Yggdrasil-serveren"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() || errors.Is(err, context.DeadlineExceeded) {
		return "Timeout – Yggdrasil-serveren kan ikke få svar fra Home Assistant"
	}
	if strings.Contains(err.Error(), "certificate") || strings.Contains(err.Error(), "tls:") {
		return "TLS/HTTPS-fejl – kontrollér HA-certifikatet og URL"
	}
	if strings.Contains(err.Error(), "invalid HA calendar entity") {
		return "HA_CALENDARS skal indeholde calendar.* entity-id'er (kommasepareret)"
	}
	if strings.Contains(err.Error(), "unmarshal") || strings.Contains(err.Error(), "JSON") {
		return "Home Assistant returnerede uventede kalenderdata – kontrollér API-adressen og logs"
	}
	return "Netværks- eller API-fejl – se Yggdrasil-containerloggen for detaljer"
}

// A read-only probe. Authentication and calendar permissions are checked separately.
func (a *App) haProbe(ctx context.Context) (bool, string) {
	req, err := http.NewRequestWithContext(ctx, "GET", a.cfg.HAURL+"/api/", nil)
	if err != nil {
		return false, haFriendlyError(err)
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.HAToken)
	resp, err := a.client.Do(req)
	if err != nil {
		return false, haFriendlyError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, haFriendlyError(&haHTTPError{Code: resp.StatusCode})
	}
	return true, ""
}

func toISO(s string, loc *time.Location) string {
	if t, e := time.Parse(time.RFC3339, s); e == nil {
		return t.Format(time.RFC3339)
	}
	if t, e := time.ParseInLocation("2006-01-02", s, loc); e == nil {
		return t.Format(time.RFC3339)
	}
	return ""
}
func (a *App) notify(ctx context.Context, service, title, message string) error {
	if a.cfg.HAURL == "" || a.cfg.HAToken == "" {
		return errors.New("Home Assistant is not configured")
	}
	if !ident.MatchString(service) {
		return errors.New("invalid notification service")
	}
	payload, _ := json.Marshal(map[string]string{"title": title, "message": message})
	req, err := http.NewRequestWithContext(ctx, "POST", a.cfg.HAURL+"/api/services/notify/"+url.PathEscape(service), strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.HAToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Home Assistant notify HTTP %d", resp.StatusCode)
	}
	return nil
}
func (a *App) testNotification(w http.ResponseWriter, r *http.Request) {
	var v struct {
		PersonID string `json:"person_id"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	people, err := a.people()
	if err != nil {
		apiError(w, 500, "Personer kunne ikke læses")
		return
	}
	for _, p := range people {
		if p.ID == v.PersonID {
			if p.NotifyService == "" {
				apiError(w, 400, "Denne person mangler en notify-service")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 7*time.Second)
			defer cancel()
			if err := a.notify(ctx, p.NotifyService, "FamilieHub", "Testbesked fra FamilieHub"); err != nil {
				apiError(w, 502, "Kunne ikke sende notifikation via Home Assistant")
				return
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	apiError(w, 404, "Personen findes ikke")
}
func (a *App) reminderLoop() {
	// Check now and every minute. Only local playdates with an assigned pickup person.
	a.checkReminders()
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for range tick.C {
		a.checkReminders()
	}
}
func (a *App) checkReminders() { a.checkRemindersAt(time.Now().In(a.loc)) }

func (a *App) checkRemindersAt(now time.Time) {
	evs, err := a.events(now.Add(-24*time.Hour), now.Add(24*time.Hour))
	if err != nil {
		return
	}
	people, err := a.people()
	if err != nil {
		return
	}
	byID := map[string]Person{}
	for _, p := range people {
		byID[p.ID] = p
	}
	a.taskReminders(now, byID)
	a.aiReminders(now, byID)
	if a.cfg.HAURL == "" || a.cfg.HAToken == "" {return}
	for _, ev := range evs {
		if ev.PickupBy == "" || ev.Kind != "legeaftale" {
			continue
		}
		p, ok := byID[ev.PickupBy]
		if !ok || p.NotifyService == "" {
			continue
		}
		end, err := time.Parse(time.RFC3339, ev.End)
		if err != nil {
			continue
		}
		due := end.Add(-30 * time.Minute)
		if now.Before(due) || now.After(due.Add(10*time.Minute)) {
			continue
		}
		key := "pickup:" + ev.ID + ":" + end.Format(time.RFC3339)
		r, e := a.db.Query("SELECT reminder_key FROM reminders WHERE reminder_key=?", key)
		if e != nil || len(r) > 0 {
			continue
		}
		msg := fmt.Sprintf("Du skal hente efter '%s' kl. %s", ev.Title, end.In(a.loc).Format("15:04"))
		if ev.Location != "" {
			msg += " · " + ev.Location
		}
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		e = a.notify(ctx, p.NotifyService, "Afhentning om 30 minutter", msg)
		cancel()
		if e != nil {
			continue
		}
		_ = a.db.Exec("INSERT OR IGNORE INTO reminders(reminder_key,sent_at) VALUES(?,?)", key, now.Format(time.RFC3339))
	}
}

// 16:00 reminder, grouped per assigned person. Dedupe persists across restarts.
func (a *App) taskReminders(now time.Time, people map[string]Person) {
	// The opt-in AI/reminder digest replaces the earlier 16:00 notification,
	// so the same unfinished tasks do not generate two alerts in one day.
	if cfg, err := a.loadAIConfig(); err == nil && cfg.RemindersEnabled {
		return
	}
	if now.Hour() != 16 || now.Minute() > 10 {
		return
	}
	day := now.Format("2006-01-02")
	all, err := a.tasks(day)
	if err != nil {
		return
	}
	grouped := map[string][]string{}
	for _, t := range all {
		// A whole-week chore has a single deadline per week. Remind on Sunday.
		if (t.RepeatRule == "once_week" || t.RepeatRule == "weekly_window" || t.RepeatRule == "biweekly_window") && now.Weekday() != time.Sunday {
			continue
		}
		if !t.Done && t.AssignedTo != "" {
			if p, ok := people[t.AssignedTo]; ok && p.NotifyService != "" {
				grouped[p.ID] = append(grouped[p.ID], t.Title)
			}
		}
	}
	for personID, titles := range grouped {
		key := "tasklist:" + personID + ":" + day
		rows, e := a.db.Query("SELECT reminder_key FROM reminders WHERE reminder_key=?", key)
		if e != nil || len(rows) > 0 {
			continue
		}
		p := people[personID]
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		e = a.notify(ctx, p.NotifyService, "Dagens pligter", strings.Join(titles, ", "))
		cancel()
		if e == nil {
			_ = a.db.Exec("INSERT OR IGNORE INTO reminders(reminder_key,sent_at) VALUES(?,?)", key, now.Format(time.RFC3339))
		}
	}
}
