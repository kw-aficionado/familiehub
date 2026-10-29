package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kw-aficionado/familiehub/internal/store"
)

const mcpTestOrigin = "https://familie.example.com"
const mcpTestRedirect = "https://chatgpt.com/connector_platform_oauth_redirect"

type mcpTestRig struct {
	a            *App
	s            *httptest.Server
	cookie, csrf string
}

func newMCPTest(t *testing.T) *mcpTestRig {
	t.Helper()
	a, _ := testApp(t)
	a.cfg.AppURL = mcpTestOrigin
	s := httptest.NewServer(securityHeaders(a.mux))
	t.Cleanup(s.Close)
	cookie, csrf := authenticated(t, s)
	code, out, _ := req(t, s, "PUT", "/api/mcp/config", `{"enabled":true,"public_url":"`+mcpTestOrigin+`","allow_write":true}`, cookie, csrf)
	if code != 200 {
		t.Fatalf("enable: %d %v", code, out)
	}
	return &mcpTestRig{a, s, cookie, csrf}
}
func mcpHTTP(t *testing.T, s *httptest.Server, method, path, body string, headers map[string]string) (int, map[string]any, http.Header, string) {
	t.Helper()
	r, e := http.NewRequest(method, s.URL+path, strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	response, e := s.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return response.StatusCode, out, response.Header, string(raw)
}
func mcpForm(t *testing.T, r *mcpTestRig, f url.Values) (int, map[string]any) {
	t.Helper()
	code, out, _, _ := mcpHTTP(t, r.s, "POST", "/oauth/token", f.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	return code, out
}
func mcpInvoke(t *testing.T, r *mcpTestRig, token, method string, params any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	code, out, _, _ := mcpHTTP(t, r.s, "POST", "/mcp", string(raw), map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream", "Authorization": "Bearer " + token, "MCP-Protocol-Version": "2025-11-25"})
	return code, out
}
func mcpTestTool(t *testing.T, r *mcpTestRig, token, name string, args any) map[string]any {
	t.Helper()
	code, out := mcpInvoke(t, r, token, "tools/call", map[string]any{"name": name, "arguments": args})
	if code != 200 {
		t.Fatalf("%s: status %d %v", name, code, out)
	}
	result, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s not tool result: %v", name, out)
	}
	return result
}
func mcpTestValue(t *testing.T, result map[string]any) map[string]any {
	t.Helper()
	if result["isError"] == true {
		t.Fatalf("tool failed: %v", result)
	}
	out, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("structured result missing: %v", result)
	}
	return out
}
func mcpRegisterTest(t *testing.T, r *mcpTestRig) string {
	t.Helper()
	code, out, _ := req(t, r.s, "POST", "/oauth/register", `{"client_name":"ChatGPT test","redirect_uris":["`+mcpTestRedirect+`"],"token_endpoint_auth_method":"none","grant_types":["authorization_code","refresh_token"],"response_types":["code"]}`, "", "")
	if code != 201 {
		t.Fatalf("registration: %d %v", code, out)
	}
	return out["client_id"].(string)
}
func mcpConsentRequest(t *testing.T, r *mcpTestRig, client, scope, verifier string) string {
	t.Helper()
	q := url.Values{"client_id": {client}, "redirect_uri": {mcpTestRedirect}, "response_type": {"code"}, "scope": {scope}, "resource": {mcpTestOrigin + "/mcp"}, "state": {"state-that-must-be-echoed"}, "code_challenge": {mcpPKCE(verifier)}, "code_challenge_method": {"S256"}}
	code, _, _, body := mcpHTTP(t, r.s, "GET", "/oauth/authorize?"+q.Encode(), "", nil)
	if code != 200 {
		t.Fatalf("authorize page: %d %s", code, body)
	}
	match := regexp.MustCompile(`data-request="([^"]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("no signed consent: %s", body)
	}
	return match[1]
}
func mcpApproveTest(t *testing.T, r *mcpTestRig, sealed string, write bool) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"request": sealed, "approve": true, "allow_write": write})
	code, out, _ := req(t, r.s, "POST", "/api/mcp/authorize", string(raw), r.cookie, r.csrf)
	if code != 200 {
		t.Fatalf("consent: %d %v", code, out)
	}
	u, e := url.Parse(out["redirect_url"].(string))
	if e != nil {
		t.Fatal(e)
	}
	if u.Query().Get("state") != "state-that-must-be-echoed" || u.Query().Get("iss") != mcpTestOrigin {
		t.Fatalf("OAuth response state/issuer: %s", u)
	}
	return u.Query().Get("code")
}
func mcpConnectTest(t *testing.T, r *mcpTestRig, write bool) (string, string, string) {
	t.Helper()
	client := mcpRegisterTest(t, r)
	verifier := strings.Repeat("p", 64)
	sealed := mcpConsentRequest(t, r, client, mcpRead+" "+mcpWrite, verifier)
	authCode := mcpApproveTest(t, r, sealed, write)
	status, out := mcpForm(t, r, url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {authCode}, "code_verifier": {verifier}, "redirect_uri": {mcpTestRedirect}, "resource": {mcpTestOrigin + "/mcp"}})
	if status != 200 {
		t.Fatalf("token: %d %v", status, out)
	}
	return out["access_token"].(string), out["refresh_token"].(string), client
}
func mcpSeedPlan(t *testing.T, r *mcpTestRig) string {
	t.Helper()
	ids := []string{newID(), newID()}
	for i, id := range ids {
		if e := r.a.db.Exec("INSERT INTO people(id,name,color) VALUES(?,?,?)", id, []string{"Teen A", "Teen B"}[i], "#123456"); e != nil {
			t.Fatal(e)
		}
	}
	tpl := newID()
	if e := r.a.db.Exec("INSERT INTO task_templates(id,title,repeat_rule,minutes,points,times_per_week) VALUES(?,?,'daily',15,4,7)", tpl, "Opvask"); e != nil {
		t.Fatal(e)
	}
	today, _ := time.Parse("2006-01-02", r.a.mcpToday())
	start := weekStart(today).AddDate(0, 0, 7).Format("2006-01-02")
	raw, _ := json.Marshal(PlannerRequest{People: ids, Choices: []PlannerChoice{{TemplateID: tpl, TimesPerWeek: 7}}, StartDate: start, RotateWeeks: 1})
	status, out, _ := req(t, r.s, "POST", "/api/planner/apply", string(raw), r.cookie, r.csrf)
	if status != 200 {
		t.Fatalf("create test plan: %d %v", status, out)
	}
	return out["plan_id"].(string)
}

func TestMCPDisabledAndMetadata(t *testing.T) {
	a, _ := testApp(t)
	s := httptest.NewServer(securityHeaders(a.mux))
	defer s.Close()
	code, _, _ := req(t, s, "GET", "/mcp", "", "", "")
	if code != 404 {
		t.Fatalf("MCP not off by default: %d", code)
	}
	r := newMCPTest(t)
	code, out, _ := req(t, r.s, "GET", "/.well-known/oauth-protected-resource/mcp", "", "", "")
	if code != 200 || out["resource"] != mcpTestOrigin+"/mcp" {
		t.Fatalf("resource metadata: %v", out)
	}
	code, out, _ = req(t, r.s, "GET", "/.well-known/oauth-authorization-server", "", "", "")
	if code != 200 || out["issuer"] != mcpTestOrigin || out["authorization_response_iss_parameter_supported"] != true {
		t.Fatalf("auth metadata: %v", out)
	}
	code, _, h, _ := mcpHTTP(t, r.s, "POST", "/mcp", `{}`, map[string]string{"Content-Type": "application/json"})
	if code != 401 || !strings.Contains(h.Get("WWW-Authenticate"), "oauth-protected-resource/mcp") {
		t.Fatalf("OAuth challenge: %d %v", code, h)
	}
	code, _, _, _ = mcpHTTP(t, r.s, "GET", "/mcp?access_token=not-accepted", "", map[string]string{"Cookie": r.cookie})
	if code != 401 {
		t.Fatalf("browser session accepted as MCP auth: %d", code)
	}
	code, _, _ = req(t, r.s, "PUT", "/api/mcp/config", `{"enabled":true,"public_url":"http://100.124.77.126:8080","allow_write":true}`, r.cookie, r.csrf)
	if code != 400 {
		t.Fatal("insecure origin accepted")
	}
	code, _, _ = req(t, r.s, "PUT", "/api/mcp/config", `{"enabled":true,"public_url":"https://other.example.com","allow_write":true}`, r.cookie, r.csrf)
	if code != 400 {
		t.Fatal("APP_URL mismatch accepted")
	}
}
func TestMCPOAuthPKCEConsentReplayAndProtocol(t *testing.T) {
	r := newMCPTest(t)
	client := mcpRegisterTest(t, r)
	verifier := strings.Repeat("v", 64)
	sealed := mcpConsentRequest(t, r, client, mcpRead+" "+mcpWrite, verifier)
	raw, _ := json.Marshal(map[string]any{"request": sealed, "approve": true, "allow_write": true})
	code, _, _ := req(t, r.s, "POST", "/api/mcp/authorize", string(raw), r.cookie, "")
	if code != 403 {
		t.Fatal("consent without CSRF")
	}
	authCode := mcpApproveTest(t, r, sealed, true)
	code, _, _ = req(t, r.s, "POST", "/api/mcp/authorize", string(raw), r.cookie, r.csrf)
	if code != 409 {
		t.Fatal("consent replay accepted")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {authCode}, "code_verifier": {strings.Repeat("x", 64)}, "redirect_uri": {mcpTestRedirect}, "resource": {mcpTestOrigin + "/mcp"}}
	code, _ = mcpForm(t, r, form)
	if code != 400 {
		t.Fatal("wrong PKCE accepted")
	}
	form.Set("code_verifier", verifier)
	form.Set("resource", "https://evil.example/mcp")
	code, _ = mcpForm(t, r, form)
	if code != 400 {
		t.Fatal("wrong resource accepted")
	}
	form.Set("resource", mcpTestOrigin+"/mcp")
	code, out := mcpForm(t, r, form)
	if code != 200 {
		t.Fatalf("exchange: %d %v", code, out)
	}
	token := out["access_token"].(string)
	code, _ = mcpForm(t, r, form)
	if code != 400 {
		t.Fatal("code replay accepted")
	}
	code, out = mcpInvoke(t, r, token, "initialize", map[string]string{"protocolVersion": "2025-11-25"})
	if code != 200 || out["result"] == nil {
		t.Fatalf("init: %v", out)
	}
	code, out = mcpInvoke(t, r, token, "tools/list", map[string]any{})
	if code != 200 || len(out["result"].(map[string]any)["tools"].([]any)) != 7 {
		t.Fatalf("tools: %v", out)
	}
	family := mcpTestValue(t, mcpTestTool(t, r, token, "get_family", map[string]any{}))
	if family["timezone"] != "Europe/Copenhagen" {
		t.Fatal(family)
	}
	code, _, _, _ = mcpHTTP(t, r.s, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json", "Origin": "https://evil.example"})
	if code != 403 {
		t.Fatal("hostile Origin accepted")
	}
	code, _, _, _ = mcpHTTP(t, r.s, "POST", "/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"})
	if code != 202 {
		t.Fatalf("notification %d", code)
	}
	code, _, _, _ = mcpHTTP(t, r.s, "GET", "/mcp", "", map[string]string{"Authorization": "Bearer " + token})
	if code != 405 {
		t.Fatalf("stateless GET %d", code)
	}
	hashes, e := r.a.db.Query("SELECT hash FROM mcp_tokens")
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range hashes {
		if v["hash"] == token || len(v["hash"]) != 64 {
			t.Fatal("token stored as plaintext")
		}
	}
	_, _, _, body := mcpHTTP(t, r.s, "GET", "/api/mcp/config", "", map[string]string{"Cookie": r.cookie})
	if strings.Contains(body, token) {
		t.Fatal("token leaked in settings")
	}
}

func TestMCPRejectsMaliciousRedirects(t *testing.T) {
	r := newMCPTest(t)
	for _, redirect := range []string{"https://evil.example/callback", "https://chatgpt.com.evil.example/connector_platform_oauth_redirect", "https://chatgpt.com/connector/oauth/../../bad", "https://chatgpt.com/connector_platform_oauth_redirect?redirect=evil", "http://chatgpt.com/connector_platform_oauth_redirect", "https://chatgpt.com:443/connector_platform_oauth_redirect"} {
		raw, _ := json.Marshal(map[string]any{"redirect_uris": []string{redirect}, "token_endpoint_auth_method": "none"})
		code, _, _ := req(t, r.s, "POST", "/oauth/register", string(raw), "", "")
		if code != 400 {
			t.Fatalf("accepted redirect %s (%d)", redirect, code)
		}
	}
	if !validMCPRedirect("https://chatgpt.com/connector/oauth/abc-123") {
		t.Fatal("callback-ID form rejected")
	}
}
func TestMCPRefreshRotationAndRevoke(t *testing.T) {
	r := newMCPTest(t)
	access, refresh, client := mcpConnectTest(t, r, true)
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {refresh}, "resource": {mcpTestOrigin + "/mcp"}}
	code, out := mcpForm(t, r, form)
	if code != 200 {
		t.Fatalf("refresh: %v", out)
	}
	nextAccess := out["access_token"].(string)
	if access == nextAccess || refresh == out["refresh_token"] {
		t.Fatal("tokens did not rotate")
	}
	code, _ = mcpForm(t, r, form)
	if code != 400 {
		t.Fatal("reused refresh token accepted")
	}
	code, _ = mcpInvoke(t, r, nextAccess, "ping", map[string]any{})
	if code != 401 {
		t.Fatal("refresh replay did not revoke grant")
	}
	access, _, _ = mcpConnectTest(t, r, true)
	rows, e := r.a.db.Query("SELECT grant_id FROM mcp_tokens WHERE hash=?", sha(access))
	if e != nil {
		t.Fatal(e)
	}
	code, _, _ = req(t, r.s, "POST", "/api/mcp/revoke", `{"id":"`+rows[0]["grant_id"]+`"}`, r.cookie, r.csrf)
	if code != 200 {
		t.Fatal(code)
	}
	code, _ = mcpInvoke(t, r, access, "ping", map[string]any{})
	if code != 401 {
		t.Fatal("revoked connection accepted")
	}
}
func TestMCPMovePlanPreservesIDsAndRotations(t *testing.T) {
	r := newMCPTest(t)
	access, _, _ := mcpConnectTest(t, r, true)
	plan := mcpSeedPlan(t, r)
	before, e := r.a.db.Query(mcpTaskSelect + "ORDER BY t.id")
	if e != nil {
		t.Fatal(e)
	}
	today, _ := time.Parse("2006-01-02", r.a.mcpToday())
	start := weekStart(today).Format("2006-01-02")
	preview := mcpTestValue(t, mcpTestTool(t, r, access, "preview_move_plan", map[string]any{"plan_id": plan, "new_start_date": start}))
	unchanged, _ := r.a.db.Query(mcpTaskSelect + "ORDER BY t.id")
	b1, _ := json.Marshal(before)
	b2, _ := json.Marshal(unchanged)
	if string(b1) != string(b2) {
		t.Fatal("preview changed tasks")
	}
	if mcpTestTool(t, r, access, "apply_changes", map[string]any{"preview_id": preview["preview_id"], "confirmed": false})["isError"] != true {
		t.Fatal("unconfirmed write accepted")
	}
	result := mcpTestValue(t, mcpTestTool(t, r, access, "apply_changes", map[string]any{"preview_id": preview["preview_id"], "confirmed": true}))
	if result["updated"] != float64(7) {
		t.Fatal(result)
	}
	after, _ := r.a.db.Query(mcpTaskSelect + "ORDER BY t.id")
	if len(after) != len(before) {
		t.Fatal("task count changed")
	}
	for i, row := range after {
		if row["id"] != before[i]["id"] || row["rotation_start"] != start || row["due_date"] < r.a.mcpToday() {
			t.Fatalf("wrong moved task: %v", row)
		}
	}
	again := mcpTestValue(t, mcpTestTool(t, r, access, "apply_changes", map[string]any{"preview_id": preview["preview_id"], "confirmed": true}))
	if again["already_applied"] != true {
		t.Fatal(again)
	}
	audit, _ := r.a.db.Query("SELECT id FROM mcp_audit")
	if len(audit) != 1 {
		t.Fatalf("retry wrote extra audit: %d", len(audit))
	}
	tasks1, _ := r.a.tasks(r.a.mcpToday())
	tasks2, _ := r.a.tasks(today.AddDate(0, 0, 7).Format("2006-01-02"))
	if len(tasks1) != 1 || len(tasks2) != 1 || tasks1[0].AssignedTo == tasks2[0].AssignedTo {
		t.Fatalf("rotation: %v %v", tasks1, tasks2)
	}
}
func TestMCPWriteScopesConflictsAndHistory(t *testing.T) {
	r := newMCPTest(t)
	access, _, _ := mcpConnectTest(t, r, true)
	readOnly, _, _ := mcpConnectTest(t, r, false)
	plan := mcpSeedPlan(t, r)
	rows, _ := r.a.db.Query("SELECT id,title FROM tasks ORDER BY id")
	id := rows[0]["id"]
	preview := mcpTestValue(t, mcpTestTool(t, r, access, "preview_update_tasks", map[string]any{"updates": []map[string]any{{"id": id, "title": "New title"}}}))
	if mcpTestTool(t, r, readOnly, "apply_changes", map[string]any{"preview_id": preview["preview_id"], "confirmed": true})["isError"] != true {
		t.Fatal("read-only token wrote tasks")
	}
	other, _, _ := mcpConnectTest(t, r, true)
	if mcpTestTool(t, r, other, "apply_changes", map[string]any{"preview_id": preview["preview_id"], "confirmed": true})["isError"] != true {
		t.Fatal("cross-connection preview applied")
	}
	if e := r.a.db.Exec("UPDATE tasks SET title='Changed in app' WHERE id=?", id); e != nil {
		t.Fatal(e)
	}
	if mcpTestTool(t, r, access, "apply_changes", map[string]any{"preview_id": preview["preview_id"], "confirmed": true})["isError"] != true {
		t.Fatal("stale preview applied")
	}
	preview = mcpTestValue(t, mcpTestTool(t, r, access, "preview_update_tasks", map[string]any{"updates": []map[string]any{{"id": id, "title": "New title"}}}))
	if e := r.a.db.Exec("INSERT INTO completions(task_id,day) VALUES(?,?)", id, r.a.mcpToday()); e != nil {
		t.Fatal(e)
	}
	if mcpTestTool(t, r, access, "apply_changes", map[string]any{"preview_id": preview["preview_id"], "confirmed": true})["isError"] != true {
		t.Fatal("completion after preview ignored")
	}
	today, _ := time.Parse("2006-01-02", r.a.mcpToday())
	if mcpTestTool(t, r, access, "preview_move_plan", map[string]any{"plan_id": plan, "new_start_date": weekStart(today).Format("2006-01-02")})["isError"] != true {
		t.Fatal("completed plan not protected")
	}
	if e := r.a.db.Exec("UPDATE mcp_settings SET enabled='0' WHERE id=1"); e != nil {
		t.Fatal(e)
	}
	code, _ := mcpInvoke(t, r, access, "ping", map[string]any{})
	if code != 404 {
		t.Fatal("disabled MCP still accessible")
	}
}
func TestMCPTransactionRollback(t *testing.T) {
	a, _ := testApp(t)
	id := newID()
	sentinel := errors.New("rollback test")
	err := a.db.Transaction(func(tx *store.Tx) error {
		if e := tx.Exec("INSERT INTO people(id,name,color) VALUES(?,?,'#123456')", id, "Rollback"); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	rows, _ := a.db.Query("SELECT id FROM people WHERE id=?", id)
	if len(rows) != 0 {
		t.Fatal("partial transaction committed")
	}
}

func TestMCPAdminOnlyConsentExpiryAndStrictArguments(t *testing.T) {
	r := newMCPTest(t)
	code, _, _ := req(t, r.s, "POST", "/api/accounts", `{"username":"child","password":"another-test-password","person_id":"","enabled":true}`, r.cookie, r.csrf)
	if code != 200 {
		t.Fatal(code)
	}
	code, login, response := req(t, r.s, "POST", "/api/login", `{"username":"child","password":"another-test-password"}`, "", "")
	if code != 200 {
		t.Fatal(code)
	}
	cookie := response.Cookies()[0].Name + "=" + response.Cookies()[0].Value
	memberCSRF := login["csrf"].(string)
	code, _, _ = req(t, r.s, "PUT", "/api/mcp/config", `{"enabled":true,"public_url":"`+mcpTestOrigin+`","allow_write":true}`, cookie, memberCSRF)
	if code != 403 {
		t.Fatal("member enabled MCP")
	}
	client := mcpRegisterTest(t, r)
	sealed := mcpConsentRequest(t, r, client, mcpRead, strings.Repeat("z", 64))
	body, _ := json.Marshal(map[string]any{"request": sealed, "approve": true})
	code, _, _ = req(t, r.s, "POST", "/api/mcp/authorize", string(body), cookie, memberCSRF)
	if code != 403 {
		t.Fatal("member approved a connection")
	}
	access, _, _ := mcpConnectTest(t, r, true)
	if mcpTestTool(t, r, access, "get_family", map[string]any{"password": true})["isError"] != true {
		t.Fatal("unknown arguments accepted")
	}
	if e := r.a.db.Exec("UPDATE mcp_tokens SET expires_at=? WHERE hash=?", "2000-01-01T00:00:00Z", sha(access)); e != nil {
		t.Fatal(e)
	}
	code, _ = mcpInvoke(t, r, access, "ping", map[string]any{})
	if code != 401 {
		t.Fatal("expired access token accepted")
	}
}

func TestMCPAtomicConflictAndWriteDisable(t *testing.T) {
	r := newMCPTest(t)
	access, _, _ := mcpConnectTest(t, r, true)
	_ = mcpSeedPlan(t, r)
	rows, _ := r.a.db.Query("SELECT id,title FROM tasks ORDER BY id")
	updates := []map[string]any{{"id": rows[0]["id"], "title": "First changed"}, {"id": rows[1]["id"], "title": "Second changed"}}
	p := mcpTestValue(t, mcpTestTool(t, r, access, "preview_update_tasks", map[string]any{"updates": updates}))
	if e := r.a.db.Exec("UPDATE tasks SET title='Concurrent edit' WHERE id=?", rows[1]["id"]); e != nil {
		t.Fatal(e)
	}
	if mcpTestTool(t, r, access, "apply_changes", map[string]any{"preview_id": p["preview_id"], "confirmed": true})["isError"] != true {
		t.Fatal("conflict not rejected")
	}
	first, _ := r.a.db.Query("SELECT title FROM tasks WHERE id=?", rows[0]["id"])
	if first[0]["title"] != rows[0]["title"] {
		t.Fatal("part of the conflicting batch was applied")
	}
	p = mcpTestValue(t, mcpTestTool(t, r, access, "preview_update_tasks", map[string]any{"updates": updates}))
	if e := r.a.db.Exec("UPDATE mcp_settings SET allow_write='0' WHERE id=1"); e != nil {
		t.Fatal(e)
	}
	if mcpTestTool(t, r, access, "apply_changes", map[string]any{"preview_id": p["preview_id"], "confirmed": true})["isError"] != true {
		t.Fatal("write disable ignored")
	}
}
