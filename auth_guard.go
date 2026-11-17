package main

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Fixed windows do not slide on blocked requests. Separate account and generous
// network limits avoid treating a house/proxy as a single eight-attempt account.
// No forwarded address is trusted implicitly.
type authBucket struct {
	Start time.Time
	Count int
}
type authGate struct {
	mu      sync.Mutex
	buckets map[string]authBucket
}

const authWindow = 5 * time.Minute
const authMaxBuckets = 4096

func (a *App) timeNow() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}
func loginAccountKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "admin"
	}
	return "account:" + sha(name)
}
func (g *authGate) prune(now time.Time) {
	if g.buckets == nil {
		g.buckets = make(map[string]authBucket)
	}
	for key, b := range g.buckets {
		if !now.Before(b.Start.Add(authWindow)) {
			delete(g.buckets, key)
		}
	}
}
func (g *authGate) wait(key string, limit int, now time.Time) time.Duration {
	b := g.buckets[key]
	if b.Count >= limit && now.Before(b.Start.Add(authWindow)) {
		return b.Start.Add(authWindow).Sub(now)
	}
	return 0
}
func (g *authGate) add(key string, now time.Time) {
	b := g.buckets[key]
	if b.Count == 0 {
		b.Start = now
	}
	b.Count++
	g.buckets[key] = b
}

// Hold this gate while validating credentials, so concurrent guesses cannot all
// pass the same failed-attempt counter. No request/secret is retained in memory.
func (a *App) loginAdmission(account, ip string, now time.Time) (time.Duration, func(bool)) {
	g := &a.auth
	g.mu.Lock()
	g.prune(now)
	network := "login-ip:" + sha(ip)
	wait := g.wait(account, 8, now)
	if other := g.wait(network, 120, now); other > wait {
		wait = other
	}
	needed := 0
	if _, ok := g.buckets[account]; !ok {
		needed++
	}
	if _, ok := g.buckets[network]; !ok {
		needed++
	}
	if len(g.buckets)+needed > authMaxBuckets && wait == 0 {
		wait = time.Minute
	}

	if wait > 0 {
		g.mu.Unlock()
		return wait, nil
	}
	g.add(network, now)
	return 0, func(valid bool) {
		if valid {
			delete(g.buckets, account)
		} else {
			g.add(account, now)
		}
		g.mu.Unlock()
	}
}
func (a *App) pairingAdmission(ip string, now time.Time) time.Duration {
	g := &a.auth
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prune(now)
	key := "pair-ip:" + sha(ip)
	if wait := g.wait(key, 20, now); wait > 0 {
		return wait
	}
	if len(g.buckets) >= authMaxBuckets {
		if _, ok := g.buckets[key]; !ok {
			return time.Minute
		}
	}
	g.add(key, now)
	return 0
}
func tooManyRequests(w http.ResponseWriter, wait time.Duration) {
	seconds := int((wait + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"error":       "For mange fors\u00f8g. Vent " + strconv.Itoa(seconds) + " sekunder og pr\u00f8v igen.",
		"retry_after": seconds,
	})
}
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return ip.String()
	}
	return host
}
