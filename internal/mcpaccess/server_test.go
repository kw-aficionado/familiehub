package mcpaccess

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kw-aficionado/familiehub/internal/store"
)

const base = "https://family.example.test"
const callback = "https://chatgpt.com/connector_platform_oauth_redirect"
const planID = "aaaaaaaaaaaaaaaaaaaaaaaa"
const otherPlan = "bbbbbbbbbbbbbbbbbbbbbbbb"

type fixture struct {
	s             *Server
	db            *store.DB
	mux           *http.ServeMux
	now           time.Time
	notifications atomic.Int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "family.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Init(); err != nil {
		t.Fatal(err)
	}
	f := &fixture{db: db, mux: http.NewServeMux(), now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	admin := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Test-Admin") != "yes" {
				fail(w, 403, "admin required")
				return
			}
			if r.Method != "GET" && r.Header.Get("X-CSRF-Token") != "test-csrf" {
				fail(w, 403, "CSRF required")
				return
			}
			h(w, r)
		}
	}
	f.s, err = Register(Config{DB: db, Mux: f.mux, Admin: admin, BaseURL: base, Now: func() time.Time { return f.now }, Notify: func() { f.notifications.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}
	// Match the real app's method-specific static catch-all. Go rejects
	// conflicting route patterns at registration time.
	f.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	return f
}
func send(f *fixture, method, path, body, ctype string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", ctype)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}
func jsonBody(v any) string { b, _ := json.Marshal(v); return string(b) }
func decoded(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("invalid response %d: %s", w.Code, w.Body.String())
	}
	return v
}
func adminJSON(f *fixture, method, path string, v any) *httptest.ResponseRecorder {
	return send(f, method, path, jsonBody(v), "application/json", map[string]string{"X-Test-Admin": "yes", "X-CSRF-Token": "test-csrf"})
}
func enable(t *testing.T, f *fixture) {
	t.Helper()
	w := adminJSON(f, "PUT", "/api/mcp/config", settings{Enabled: true, PublicURL: base})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}
func client(t *testing.T, f *fixture, scope string) (string, string) {
	t.Helper()
	w := adminJSON(f, "POST", "/api/mcp/clients", map[string]string{"name": "ChatGPT", "redirect_uri": callback, "scope": scope})
	if w.Code != 201 {
		t.Fatalf("create client: %d %s", w.Code, w.Body.String())
	}
	v := decoded(t, w)
	return v["client_id"].(string), v["client_secret"].(string)
}
func auth(t *testing.T, f *fixture, id, scope string) (authorizationRequest, string) {
	t.Helper()
	verifier := strings.Repeat("V", 64)
	v := authorizationRequest{ClientID: id, RedirectURI: callback, ResponseType: "code", State: "state-123", Scope: scope, Resource: base + "/mcp", Challenge: pkce(verifier), Method: "S256", Allow: true}
	w := adminJSON(f, "POST", "/api/mcp/authorize", v)
	if w.Code != 200 {
		t.Fatalf("consent: %d %s", w.Code, w.Body.String())
	}
	redirect, _ := url.Parse(decoded(t, w)["redirect"].(string))
	if redirect.Query().Get("iss") != base || redirect.Query().Get("state") != "state-123" {
		t.Fatal("issuer/state missing")
	}
	return v, redirect.Query().Get("code")
}
func exchange(f *fixture, id, secret, code string) *httptest.ResponseRecorder {
	values := url.Values{"client_id": {id}, "client_secret": {secret}, "grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {strings.Repeat("V", 64)}, "redirect_uri": {callback}, "resource": {base + "/mcp"}}
	return send(f, "POST", "/mcp/oauth/token", values.Encode(), "application/x-www-form-urlencoded", nil)
}
func grant(t *testing.T, f *fixture, scope string) (string, string, map[string]any) {
	t.Helper()
	id, secret := client(t, f, scope)
	_, code := auth(t, f, id, scope)
	w := exchange(f, id, secret, code)
	if w.Code != 200 {
		t.Fatalf("exchange: %d %s", w.Code, w.Body.String())
	}
	return id, secret, decoded(t, w)
}
func rpc(f *fixture, token, method string, params any) *httptest.ResponseRecorder {
	return send(f, "POST", "/mcp", jsonBody(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}), "application/json", map[string]string{"Authorization": "Bearer " + token, "Accept": "application/json, text/event-stream"})
}
func seed(t *testing.T, f *fixture) {
	t.Helper()
	for _, p := range []string{"one", "two"} {
		if err := f.db.Exec("INSERT INTO people(id,name,color,notify_service) VALUES(?,?,?,?)", p, p, "#112233", "DO_NOT_EXPOSE_NOTIFY"); err != nil {
			t.Fatal(err)
		}
	}
	for i, date := range []string{"2026-11-02", "2026-11-05", "2026-11-08"} {
		id := fmt.Sprintf("%024x", i+1)
		if err := f.db.Exec("INSERT INTO tasks(id,title,assigned_to,due_date,repeat_rule,rotation_members,rotation_start,rotation_weeks,plan_id) VALUES(?,?,?,?,?,?,?,?,?)", id, "Chore "+date, "one", date, "weekly", `["one","two"]`, "2026-11-02", "1", planID); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.Exec("INSERT INTO tasks(id,title,due_date,plan_id) VALUES('untouched','Unrelated','2026-11-02',?)", otherPlan); err != nil {
		t.Fatal(err)
	}
}
func TestDisabledByDefaultAndAdminCSRF(t *testing.T) {
	f := setup(t)
	for _, path := range []string{"/mcp", "/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource/mcp"} {
		w := send(f, "GET", path, "", "", nil)
		if w.Code != 503 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	if w := send(f, "GET", "/api/mcp/config", "", "", nil); w.Code != 403 {
		t.Fatal("public config access")
	}
	if w := send(f, "PUT", "/api/mcp/config", `{"enabled":true,"public_url":"https://family.example.test"}`, "application/json", map[string]string{"X-Test-Admin": "yes"}); w.Code != 403 {
		t.Fatal("missing CSRF allowed")
	}
	enable(t, f)
	w := send(f, "POST", "/mcp", `{}`, "application/json", nil)
	if w.Code != 401 || !strings.Contains(w.Header().Get("WWW-Authenticate"), base+"/.well-known/oauth-protected-resource/mcp") {
		t.Fatal("missing OAuth challenge")
	}
	// Existing browser sessions are not a substitute for OAuth.
	w = send(f, "POST", "/mcp", `{}`, "application/json", map[string]string{"Cookie": "fh_session=administrator", "X-Test-Admin": "yes"})
	if w.Code != 401 {
		t.Fatal("cookie-only MCP allowed")
	}
}
func TestMetadataAndCredentialStorage(t *testing.T) {
	f := setup(t)
	enable(t, f)
	m := decoded(t, send(f, "GET", "/.well-known/oauth-authorization-server", "", "", nil))
	if m["issuer"] != base || m["authorization_response_iss_parameter_supported"] != true {
		t.Fatal(m)
	}
	if _, ok := m["registration_endpoint"]; ok {
		t.Fatal("must not advertise unimplemented DCR")
	}
	id, secret := client(t, f, ReadScope+" "+OfflineScope)
	rows, err := f.db.Query("SELECT * FROM mcp_clients WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["secret_hash"] != digest(secret) || strings.Contains(jsonBody(rows), secret) {
		t.Fatal("client secret stored in plaintext")
	}
	w := adminJSON(f, "GET", "/api/mcp/config", nil)
	if strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), "secret_hash") {
		t.Fatal("credential leaked in settings")
	}
}
func TestOAuthPKCEAndOneUseCode(t *testing.T) {
	f := setup(t)
	enable(t, f)
	id, secret := client(t, f, ReadScope+" "+OfflineScope)
	v, code := auth(t, f, id, ReadScope+" "+OfflineScope)
	values := url.Values{"client_id": {id}, "client_secret": {secret}, "grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {strings.Repeat("X", 64)}, "redirect_uri": {callback}, "resource": {base + "/mcp"}}
	if w := send(f, "POST", "/mcp/oauth/token", values.Encode(), "application/x-www-form-urlencoded", nil); w.Code != 400 {
		t.Fatal("wrong PKCE accepted")
	}
	values.Set("code_verifier", strings.Repeat("V", 64))
	values.Set("resource", "https://other.test/mcp")
	if w := send(f, "POST", "/mcp/oauth/token", values.Encode(), "application/x-www-form-urlencoded", nil); w.Code != 400 {
		t.Fatal("wrong audience accepted")
	}
	w := exchange(f, id, secret, code)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	tokens := decoded(t, w)
	if tokens["refresh_token"] == nil || tokens["expires_in"] != float64(3600) {
		t.Fatal(tokens)
	}
	if w := exchange(f, id, secret, code); w.Code != 400 {
		t.Fatal("code replay accepted")
	}
	v.RedirectURI = "https://attacker.test/callback"
	if w := adminJSON(f, "POST", "/api/mcp/authorize", v); w.Code != 400 {
		t.Fatal("open redirect")
	}
	v.RedirectURI = callback
	v.Method = "plain"
	if w := adminJSON(f, "POST", "/api/mcp/authorize", v); w.Code != 400 {
		t.Fatal("plain PKCE allowed")
	}
	v.Method = "S256"
	v.Scope = ReadScope + " " + WriteScope
	if w := adminJSON(f, "POST", "/api/mcp/authorize", v); w.Code != 400 {
		t.Fatal("scope escalation")
	}
}
func TestOAuthDenialAndExpiry(t *testing.T) {
	f := setup(t)
	enable(t, f)
	id, secret := client(t, f, ReadScope)
	v, code := auth(t, f, id, ReadScope)
	v.Allow = false
	w := adminJSON(f, "POST", "/api/mcp/authorize", v)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	u, _ := url.Parse(decoded(t, w)["redirect"].(string))
	if u.Query().Get("error") != "access_denied" || u.Query().Get("iss") != base || u.Query().Get("code") != "" {
		t.Fatal(u)
	}
	f.now = f.now.Add(6 * time.Minute)
	if w := exchange(f, id, secret, code); w.Code != 400 {
		t.Fatal("expired code accepted")
	}
	_, _, tokens := grant(t, f, ReadScope)
	token := tokens["access_token"].(string)
	f.now = f.now.Add(time.Hour)
	if w := rpc(f, token, "ping", map[string]any{}); w.Code != 401 {
		t.Fatal("expired access token accepted")
	}
}
func TestRefreshRotationReplayRevokesGrant(t *testing.T) {
	f := setup(t)
	enable(t, f)
	id, secret, tokens := grant(t, f, ReadScope+" "+OfflineScope)
	values := url.Values{"client_id": {id}, "client_secret": {secret}, "grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)}, "resource": {base + "/mcp"}}
	w := send(f, "POST", "/mcp/oauth/token", values.Encode(), "application/x-www-form-urlencoded", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	fresh := decoded(t, w)
	if fresh["refresh_token"] == tokens["refresh_token"] {
		t.Fatal("refresh did not rotate")
	}
	w = send(f, "POST", "/mcp/oauth/token", values.Encode(), "application/x-www-form-urlencoded", nil)
	if w.Code != 400 {
		t.Fatal("refresh replay accepted")
	}
	if w := rpc(f, fresh["access_token"].(string), "ping", map[string]any{}); w.Code != 401 {
		t.Fatal("replayed refresh did not revoke family")
	}
}
func TestClientRevocationAndDisableAreImmediate(t *testing.T) {
	f := setup(t)
	enable(t, f)
	id, _, tokens := grant(t, f, ReadScope)
	token := tokens["access_token"].(string)
	if w := rpc(f, token, "ping", map[string]any{}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := adminJSON(f, "DELETE", "/api/mcp/clients/"+id, nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := rpc(f, token, "ping", map[string]any{}); w.Code != 401 {
		t.Fatal("revoked client still authorized")
	}
	_, _, tokens = grant(t, f, ReadScope)
	token = tokens["access_token"].(string)
	w = adminJSON(f, "PUT", "/api/mcp/config", settings{Enabled: false, PublicURL: base})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	enable(t, f)
	if w := rpc(f, token, "ping", map[string]any{}); w.Code != 401 {
		t.Fatal("disable/re-enable restored old authorization")
	}
}
func TestMCPNegotiationScopeAndNoSecretLeak(t *testing.T) {
	f := setup(t)
	seed(t, f)
	enable(t, f)
	_, _, tokens := grant(t, f, ReadScope)
	token := tokens["access_token"].(string)
	w := rpc(f, token, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "test", "version": "1"}})
	v := decoded(t, w)
	if v["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Fatal(v)
	}
	w = rpc(f, token, "tools/list", map[string]any{})
	if strings.Contains(w.Body.String(), "familyhub_apply_move_plan") {
		t.Fatal("read-only client saw write tool")
	}
	w = rpc(f, token, "tools/call", map[string]any{"name": "familyhub_get_overview", "arguments": map[string]any{}})
	if strings.Contains(w.Body.String(), "DO_NOT_EXPOSE_NOTIFY") || !strings.Contains(w.Body.String(), "2026-10-29") {
		t.Fatal(w.Body.String())
	}
	w = rpc(f, token, "tools/call", map[string]any{"name": "familyhub_apply_move_plan", "arguments": map[string]any{"proposal_id": strings.Repeat("a", 64), "confirm": true}})
	if decoded(t, w)["result"].(map[string]any)["isError"] != true {
		t.Fatal("read-only write accepted")
	}
	headers := map[string]string{"Authorization": "Bearer " + token, "Accept": "application/json", "Origin": "https://attacker.test"}
	if w := send(f, "POST", "/mcp", `{}`, "application/json", headers); w.Code != 403 {
		t.Fatal("bad Origin allowed")
	}
	delete(headers, "Origin")
	if w := send(f, "GET", "/mcp", "", "", headers); w.Code != 405 {
		t.Fatal("GET stream should not be advertised")
	}
	if w := send(f, "POST", "/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, "application/json", headers); w.Code != 202 || w.Body.Len() != 0 {
		t.Fatal("bad notification acknowledgement")
	}
	if w := send(f, "POST", "/mcp?access_token="+token, `{}`, "application/json", headers); w.Code != 401 {
		t.Fatal("query-string token accepted")
	}
	w = rpc(f, token, "tools/call", map[string]any{"name": "familyhub_get_overview", "arguments": map[string]any{"sql": "SELECT * FROM ai_settings"}})
	if decoded(t, w)["result"].(map[string]any)["isError"] != true {
		t.Fatal("unknown arguments accepted")
	}
}
func TestMovePlanPreviewApprovalAndIdempotency(t *testing.T) {
	f := setup(t)
	seed(t, f)
	before, _ := planRows(f.db, planID)
	got, err := f.s.previewShift("client:test", shiftRequest{PlanID: planID, NewStart: "2026-10-26"})
	if err != nil {
		t.Fatal(err)
	}
	p := got.(proposal)
	after, _ := planRows(f.db, planID)
	if snapshot(before) != snapshot(after) {
		t.Fatal("preview changed tasks")
	}
	if p.DeltaDays != -7 || len(p.Changes) != 3 || len(p.Warnings) < 2 {
		t.Fatal(p)
	}
	if _, err := f.s.applyShift("client:test", applyRequest{ID: p.ID}); err == nil {
		t.Fatal("unapproved apply accepted")
	}
	if _, err := f.s.applyShift("client:other", applyRequest{ID: p.ID, Confirm: true}); err == nil {
		t.Fatal("another client applied preview")
	}
	receipt, err := f.s.applyShift("client:test", applyRequest{ID: p.ID, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.s.applyShift("client:test", applyRequest{ID: p.ID, Confirm: true})
	if err != nil || jsonBody(again) != jsonBody(receipt) {
		t.Fatal("idempotent replay failed")
	}
	moved, _ := planRows(f.db, planID)
	for i, r := range moved {
		if r["id"] != before[i]["id"] || r["assigned_to"] != before[i]["assigned_to"] || r["repeat_rule"] != before[i]["repeat_rule"] || r["rotation_start"] != "2026-10-26" {
			t.Fatal(r)
		}
	}
	if moved[0]["due_date"] != "2026-10-26" || moved[2]["due_date"] != "2026-11-01" {
		t.Fatal(moved)
	}
	unchanged, _ := planRows(f.db, otherPlan)
	if unchanged[0]["due_date"] != "2026-11-02" {
		t.Fatal("other plan changed")
	}
	audit, _ := f.db.Query("SELECT id FROM mcp_audit")
	if len(audit) != 1 || f.notifications.Load() != 1 {
		t.Fatal("duplicate audit/notification")
	}
}
func TestMoveRejectsCompletedChangedOrExpiredPlans(t *testing.T) {
	for _, scenario := range []string{"completed-before", "completed-after", "edited-after", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			seed(t, f)
			if scenario == "completed-before" {
				_ = f.db.Exec("INSERT INTO completions(task_id,day) VALUES(?,?)", fmt.Sprintf("%024x", 1), "2026-11-02")
			}
			got, err := f.s.previewShift("client:test", shiftRequest{PlanID: planID, NewStart: "2026-10-26"})
			if scenario == "completed-before" {
				if err == nil {
					t.Fatal("completed plan allowed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			p := got.(proposal)
			switch scenario {
			case "completed-after":
				_ = f.db.Exec("INSERT INTO completions(task_id,day) VALUES(?,?)", fmt.Sprintf("%024x", 1), "2026-11-02")
			case "edited-after":
				_ = f.db.Exec("UPDATE tasks SET title='Changed' WHERE id=?", fmt.Sprintf("%024x", 1))
			case "expired":
				f.now = f.now.Add(16 * time.Minute)
			}
			before, _ := planRows(f.db, planID)
			if _, err := f.s.applyShift("client:test", applyRequest{ID: p.ID, Confirm: true}); err == nil {
				t.Fatal("invalid preview applied")
			}
			after, _ := planRows(f.db, planID)
			if snapshot(before) != snapshot(after) {
				t.Fatal("failed apply changed data")
			}
		})
	}
}
func TestAtomicRollbackAndConcurrentApply(t *testing.T) {
	t.Run("rollback", func(t *testing.T) {
		f := setup(t)
		seed(t, f)
		before, _ := planRows(f.db, planID)
		got, _ := f.s.previewShift("admin", shiftRequest{PlanID: planID, NewStart: "2026-10-26"})
		p := got.(proposal)
		if err := f.db.Exec(`CREATE TRIGGER deny_audit BEFORE INSERT ON mcp_audit BEGIN SELECT RAISE(ABORT,'test audit failure'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.applyShift("admin", applyRequest{ID: p.ID, Confirm: true}); err == nil {
			t.Fatal("audit failure ignored")
		}
		after, _ := planRows(f.db, planID)
		if snapshot(before) != snapshot(after) {
			t.Fatal("partial writes survived rollback")
		}
	})
	t.Run("concurrent", func(t *testing.T) {
		f := setup(t)
		seed(t, f)
		got, _ := f.s.previewShift("admin", shiftRequest{PlanID: planID, NewStart: "2026-10-26"})
		p := got.(proposal)
		var wg sync.WaitGroup
		errs := make(chan error, 12)
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := f.s.applyShift("admin", applyRequest{ID: p.ID, Confirm: true})
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		audit, _ := f.db.Query("SELECT id FROM mcp_audit")
		if len(audit) != 1 || f.notifications.Load() != 1 {
			t.Fatal("concurrent replay wrote twice")
		}
	})
}
func TestAuthenticatedMCPMoveEndToEnd(t *testing.T) {
	f := setup(t)
	seed(t, f)
	enable(t, f)
	_, _, tokens := grant(t, f, ReadScope+" "+WriteScope)
	token := tokens["access_token"].(string)
	w := rpc(f, token, "tools/call", map[string]any{"name": "familyhub_list_plans", "arguments": map[string]any{}})
	if !strings.Contains(w.Body.String(), planID) {
		t.Fatal(w.Body.String())
	}
	w = rpc(f, token, "tools/call", map[string]any{"name": "familyhub_preview_move_plan", "arguments": map[string]any{"plan_id": planID, "new_start_date": "2026-10-26"}})
	result := decoded(t, w)["result"].(map[string]any)
	p := result["structuredContent"].(map[string]any)
	w = rpc(f, token, "tools/call", map[string]any{"name": "familyhub_apply_move_plan", "arguments": map[string]any{"proposal_id": p["proposal_id"], "confirm": true}})
	if decoded(t, w)["result"].(map[string]any)["isError"] != false {
		t.Fatal(w.Body.String())
	}
	rows, _ := planRows(f.db, planID)
	if rows[0]["due_date"] != "2026-10-26" {
		t.Fatal(rows)
	}
}
func TestURLValidationAndMalformedInput(t *testing.T) {
	for _, raw := range []string{"http://100.64.0.1:8080", "https://user:pass@host.test", "https://host.test/path", "https://host.test?secret=x", "javascript:alert(1)"} {
		if _, err := validBase(raw); err == nil {
			t.Fatalf("unsafe base accepted: %s", raw)
		}
	}
	for _, raw := range []string{"https://host.test", "http://localhost:8080", "http://127.0.0.1:8080"} {
		if _, err := validBase(raw); err != nil {
			t.Fatal(err)
		}
	}
	f := setup(t)
	enable(t, f)
	seed(t, f)
	for _, date := range []string{"2026-10-29", "2026-02-30", "not-a-date", "2020-01-06", "2026-11-02"} {
		if _, err := f.s.previewShift("admin", shiftRequest{PlanID: planID, NewStart: date}); err == nil {
			t.Fatalf("invalid/no-op start allowed: %s", date)
		}
	}
	if w := adminJSON(f, "POST", "/api/mcp/plans/preview-shift", map[string]any{"plan_id": planID, "new_start_date": "2026-10-26", "sql": "DROP TABLE tasks"}); w.Code != 400 {
		t.Fatal("unknown field accepted")
	}
}
func TestAdminMoveWorksWithMCPDisabled(t *testing.T) {
	f := setup(t)
	seed(t, f)
	w := adminJSON(f, "POST", "/api/mcp/plans/preview-shift", shiftRequest{PlanID: planID, NewStart: "2026-10-26"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	p := decoded(t, w)
	w = adminJSON(f, "POST", "/api/mcp/plans/apply-shift", applyRequest{ID: p["proposal_id"].(string), Confirm: true})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = send(f, "GET", "/mcp/settings", "", "", nil)
	if w.Code != 200 || w.Header().Get("Referrer-Policy") != "no-referrer" || !strings.Contains(w.Body.String(), "mcpShiftForm") {
		t.Fatal("settings page unavailable")
	}
}

func TestBasicClientAuthenticationAndDuplicateFields(t *testing.T) {
	f := setup(t)
	enable(t, f)
	id, secret := client(t, f, ReadScope)
	_, code := auth(t, f, id, ReadScope)
	values := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {strings.Repeat("V", 64)}, "redirect_uri": {callback}, "resource": {base + "/mcp"}}
	r := httptest.NewRequest("POST", "/mcp/oauth/token", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetBasicAuth(id, secret)
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	_, code = auth(t, f, id, ReadScope)
	values.Set("code", code)
	values.Set("client_id", id)
	values.Set("client_secret", secret)
	values.Add("resource", "https://attacker.test")
	if w := send(f, "POST", "/mcp/oauth/token", values.Encode(), "application/x-www-form-urlencoded", nil); w.Code != 400 {
		t.Fatal("duplicate OAuth parameters accepted")
	}
}
func TestTokenRevocationEndpointAndGrantLifetime(t *testing.T) {
	f := setup(t)
	enable(t, f)
	id, secret, tokens := grant(t, f, ReadScope+" "+OfflineScope)
	values := url.Values{"client_id": {id}, "client_secret": {secret}, "token": {tokens["refresh_token"].(string)}}
	w := send(f, "POST", "/mcp/oauth/revoke", values.Encode(), "application/x-www-form-urlencoded", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := rpc(f, tokens["access_token"].(string), "ping", map[string]any{}); w.Code != 401 {
		t.Fatal("token revocation did not revoke access")
	}
	id, secret, tokens = grant(t, f, ReadScope+" "+OfflineScope)
	f.now = f.now.Add(30 * 24 * time.Hour)
	values = url.Values{"client_id": {id}, "client_secret": {secret}, "grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)}, "resource": {base + "/mcp"}}
	if w := send(f, "POST", "/mcp/oauth/token", values.Encode(), "application/x-www-form-urlencoded", nil); w.Code != 400 {
		t.Fatal("grant exceeded maximum 30 days")
	}
}
func TestPaginationAndTransactionPanicRollback(t *testing.T) {
	f := setup(t)
	if err := f.db.Transaction(func(tx *store.Tx) error {
		for i := 0; i < 101; i++ {
			if err := tx.Exec("INSERT INTO task_templates(id,title) VALUES(?,?)", fmt.Sprintf("%024x", i+100), "Example"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := f.s.callTool(principal{Scope: ReadScope}, "familyhub_list_templates", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	v := result.(map[string]any)
	if len(v["templates"].(store.Rows)) != 100 || v["next_offset"] != 100 {
		t.Fatal("bad pagination")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		_ = f.db.Transaction(func(tx *store.Tx) error {
			if err := tx.Exec("DELETE FROM task_templates"); err != nil {
				t.Fatal(err)
			}
			panic("simulated panic")
		})
	}()
	rows, err := f.db.Query("SELECT id FROM task_templates")
	if err != nil || len(rows) != 101 {
		t.Fatal("panic did not roll back")
	}
}
func TestCopenhagenDateAndDSTWeekShift(t *testing.T) {
	f := setup(t)
	seed(t, f)
	// Copenhagen is already on the next day at 23:30 UTC after DST ends.
	f.now = time.Date(2026, 10, 28, 23, 30, 0, 0, time.UTC)
	overview, err := f.s.callTool(principal{Scope: ReadScope}, "familyhub_get_overview", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if overview.(map[string]any)["today"] != "2026-10-29" {
		t.Fatal("wrong Copenhagen date")
	}
	p, err := f.s.previewShift("admin", shiftRequest{PlanID: planID, NewStart: "2026-10-19"})
	if err != nil {
		t.Fatal(err)
	}
	if p.(proposal).DeltaDays != -14 {
		t.Fatal("DST changed week length")
	}
}
