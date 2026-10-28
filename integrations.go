package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
			return out, fmt.Errorf("HA calendar %s returned HTTP %d", entity, resp.StatusCode)
		}
		var payload []struct {
			Start       string `json:"start"`
			End         string `json:"end"`
			Summary     string `json:"summary"`
			Location    string `json:"location"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return out, err
		}
		for _, e := range payload {
			st := toISO(e.Start, a.loc)
			en := toISO(e.End, a.loc)
			if st == "" || en == "" {
				continue
			}
			out = append(out, Event{ID: "ha:" + entity + ":" + sha(e.Summary + e.Start + e.End)[:16], Title: e.Summary, Start: st, End: en, Kind: "kalender", Location: e.Location, Notes: e.Description, External: true})
		}
	}
	return out, nil
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
	if a.cfg.HAURL == "" || a.cfg.HAToken == "" {
		return
	}
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
