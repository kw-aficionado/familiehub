package main

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoginLimitsAccountNotSharedProxyAndDoesNotExtend(t *testing.T) {
	a, _ := testApp(t)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	for n := 0; n < 8; n++ {
		tabletExpect(t, tabletCall(a, "POST", "/api/login", map[string]string{"username": "other", "password": "wrong-password-long"}, "", "", nil), 401)
	}
	r := tabletCall(a, "POST", "/api/login", map[string]string{"username": "OTHER", "password": "wrong-password-long"}, "", "", nil)
	tabletExpect(t, r, 429)
	if r.recorder.Header().Get("Retry-After") != "300" || r.data["retry_after"] != float64(300) {
		t.Fatal(r.data)
	}
	// Same proxy, different account: normal family access is unaffected.
	tabletExpect(t, tabletCall(a, "POST", "/api/login", map[string]string{"username": "admin", "password": a.cfg.Password}, "", "", nil), 200)
	now = now.Add(4 * time.Minute)
	r = tabletCall(a, "POST", "/api/login", map[string]string{"username": "other", "password": "wrong-password-long"}, "", "", nil)
	tabletExpect(t, r, 429)
	if r.recorder.Header().Get("Retry-After") != "60" {
		t.Fatal(r.recorder.Header())
	}
	now = now.Add(time.Minute)
	tabletExpect(t, tabletCall(a, "POST", "/api/login", map[string]string{"username": "other", "password": "wrong-password-long"}, "", "", nil), 401)
	if loginAccountKey("") != loginAccountKey(" ADMIN ") {
		t.Fatal("admin alias bypass")
	}
}
func TestSuccessfulLoginsDoNotLockAccountAndSprayIsBounded(t *testing.T) {
	a, _ := testApp(t)
	for n := 0; n < 20; n++ {
		tabletExpect(t, tabletCall(a, "POST", "/api/login", map[string]string{"password": a.cfg.Password}, "", "", nil), 200)
	}
	for n := 0; n < 100; n++ {
		tabletExpect(t, tabletCall(a, "POST", "/api/login", map[string]string{"username": "unknown" + strconv.Itoa(n), "password": "wrong-password"}, "", "", nil), 401)
	}
	tabletExpect(t, tabletCall(a, "POST", "/api/login", map[string]string{"username": "fresh-account", "password": "wrong-password"}, "", "", nil), 429)
}
func TestPairRateLimitIndependentAndUnspoofable(t *testing.T) {
	a, _ := testApp(t)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	for n := 0; n < 20; n++ {
		tabletExpect(t, tabletCall(a, "POST", "/api/tablet/pair", map[string]string{"code": "ABCD-EFGH-JKLM"}, "", "", map[string]string{"X-Forwarded-For": "198.51.100." + strconv.Itoa(n)}), 400)
	}
	r := tabletCall(a, "POST", "/api/tablet/pair", map[string]string{"code": "ABCD-EFGH-JKLM"}, "", "", nil)
	tabletExpect(t, r, 429)
	tabletExpect(t, tabletCall(a, "POST", "/api/login", map[string]string{"password": a.cfg.Password}, "", "", nil), 200)
	now = now.Add(5 * time.Minute)
	tabletExpect(t, tabletCall(a, "POST", "/api/tablet/pair", map[string]string{"code": "ABCD-EFGH-JKLM"}, "", "", nil), 400)
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader("{}"))
	req.RemoteAddr = "[2001:db8::5]:443"
	req.Header.Set("X-Forwarded-For", "1.1.1.1")
	if remoteIP(req) != "2001:db8::5" {
		t.Fatal(remoteIP(req))
	}
}
func TestAuthBucketsArePrunedAndBounded(t *testing.T) {
	a, _ := testApp(t)
	now := time.Now()
	for n := 0; n < authMaxBuckets+10; n++ {
		wait, finish := a.loginAdmission(loginAccountKey(strconv.Itoa(n)), "192.0.2."+strconv.Itoa(n), now)
		if wait == 0 {
			finish(false)
		}
	}
	if len(a.auth.buckets) > authMaxBuckets {
		t.Fatalf("unbounded map: %d", len(a.auth.buckets))
	}
	wait, finish := a.loginAdmission("account:new", "192.0.2.1", now.Add(authWindow))
	if wait != 0 {
		t.Fatal(wait)
	}
	finish(true)
	if len(a.auth.buckets) > 2 {
		t.Fatal("expired buckets retained")
	}
}
