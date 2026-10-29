package main

// Opt-in OAuth 2.1 / PKCE for the household's task-only MCP server. The existing
// administrator session approves each connection; passwords never reach an MCP
// client. Opaque access/refresh tokens and authorization codes are stored hashed.
import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kw-aficionado/familiehub/internal/store"
)

const mcpRead = "tasks:read"
const mcpWrite = "tasks:write"
const mcpTime = "2006-01-02T15:04:05Z"

func mcpNow() string { return time.Now().UTC().Format(mcpTime) }
func mcpSecret() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure random unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func mcpPKCE(s string) string {
	b := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func mcpHas(scope, wanted string) bool {
	for _, s := range strings.Fields(scope) {
		if s == wanted {
			return true
		}
	}
	return false
}
func mcpScope(raw string) (string, bool) {
	if raw == "" {
		return mcpRead, true
	}
	read, write := false, false
	for _, s := range strings.Fields(raw) {
		switch s {
		case mcpRead:
			read = true
		case mcpWrite:
			write = true
		default:
			return "", false
		}
	}
	if !read {
		return "", false
	}
	if write {
		return mcpRead + " " + mcpWrite, true
	}
	return mcpRead, true
}

type mcpConfig struct {
	Enabled    bool   `json:"enabled"`
	PublicURL  string `json:"public_url"`
	AllowWrite bool   `json:"allow_write"`
}

func (a *App) initMCP() error {
	for _, sql := range []string{
		`CREATE TABLE IF NOT EXISTS mcp_settings(id INTEGER PRIMARY KEY CHECK(id=1),enabled TEXT NOT NULL DEFAULT '0',public_url TEXT NOT NULL DEFAULT '',allow_write TEXT NOT NULL DEFAULT '0')`,
		`CREATE TABLE IF NOT EXISTS mcp_clients(id TEXT PRIMARY KEY,name TEXT NOT NULL,redirects TEXT NOT NULL,created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS mcp_used_consents(nonce TEXT PRIMARY KEY,expires_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS mcp_codes(hash TEXT PRIMARY KEY,client_id TEXT NOT NULL,redirect_uri TEXT NOT NULL,challenge TEXT NOT NULL,resource TEXT NOT NULL,scope TEXT NOT NULL,epoch TEXT NOT NULL,expires_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS mcp_grants(id TEXT PRIMARY KEY,client_id TEXT NOT NULL,scope TEXT NOT NULL,resource TEXT NOT NULL,epoch TEXT NOT NULL,created_at TEXT NOT NULL,expires_at TEXT NOT NULL,revoked TEXT NOT NULL DEFAULT '0')`,
		`CREATE TABLE IF NOT EXISTS mcp_tokens(hash TEXT PRIMARY KEY,grant_id TEXT NOT NULL,kind TEXT NOT NULL,expires_at TEXT NOT NULL,used TEXT NOT NULL DEFAULT '0',FOREIGN KEY(grant_id) REFERENCES mcp_grants(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS mcp_tokens_grant ON mcp_tokens(grant_id)`,
		`CREATE TABLE IF NOT EXISTS mcp_previews(id TEXT PRIMARY KEY,grant_id TEXT NOT NULL,payload TEXT NOT NULL,created_at TEXT NOT NULL,expires_at TEXT NOT NULL,result TEXT NOT NULL DEFAULT '',FOREIGN KEY(grant_id) REFERENCES mcp_grants(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS mcp_audit(id TEXT PRIMARY KEY,grant_id TEXT NOT NULL,action TEXT NOT NULL,task_count INTEGER NOT NULL,created_at TEXT NOT NULL)`,
	} {
		if err := a.db.Exec(sql); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) mcpRoutes() {
	if err := a.initMCP(); err != nil {
		panic(fmt.Sprintf("MCP migration: %v", err))
	}
	a.mux.HandleFunc("POST /mcp", a.mcpHandle)
	a.mux.HandleFunc("GET /mcp", a.mcpHandle)
	a.mux.HandleFunc("DELETE /mcp", a.mcpHandle)
	a.mux.HandleFunc("GET /.well-known/oauth-protected-resource", a.mcpResourceMetadata)
	a.mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", a.mcpResourceMetadata)
	a.mux.HandleFunc("GET /.well-known/oauth-authorization-server", a.mcpAuthMetadata)
	a.mux.HandleFunc("POST /oauth/register", a.mcpRegister)
	a.mux.HandleFunc("GET /oauth/authorize", a.mcpAuthorizePage)
	a.mux.HandleFunc("POST /oauth/token", a.mcpToken)
	a.mux.HandleFunc("GET /api/mcp/config", a.withAdmin(a.mcpConfigGet))
	a.mux.HandleFunc("PUT /api/mcp/config", a.withAdmin(a.mcpConfigPut))
	a.mux.HandleFunc("POST /api/mcp/authorize", a.withAdmin(a.mcpConsent))
	a.mux.HandleFunc("POST /api/mcp/revoke", a.withAdmin(a.mcpRevoke))
}

func (a *App) loadMCP() (mcpConfig, error) {
	c := mcpConfig{}
	rows, err := a.db.Query("SELECT enabled,public_url,allow_write FROM mcp_settings WHERE id=1")
	if err != nil {
		return c, err
	}
	if len(rows) > 0 {
		c.Enabled = rows[0]["enabled"] == "1"
		c.PublicURL = rows[0]["public_url"]
		c.AllowWrite = rows[0]["allow_write"] == "1"
	}
	return c, nil
}

// Only an explicitly configured HTTPS origin is trusted, never Host or forwarded
// headers. No user info, credentials, fragments, queries or deployment subpaths.
func validMCPOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.RawPath != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return false
	}
	return true
}
func (a *App) mcpEpoch() string { return mac(a.key, "mcp-admin-v1:"+sha(a.cfg.Password)) }
func (a *App) mcpAvailable(w http.ResponseWriter, r *http.Request) (mcpConfig, bool) {
	c, err := a.loadMCP()
	if err != nil {
		apiError(w, 500, "MCP configuration unavailable")
		return c, false
	}
	if !c.Enabled || !validMCPOrigin(c.PublicURL) {
		apiError(w, 404, "MCP is disabled")
		return c, false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != c.PublicURL {
		apiError(w, 403, "Origin not allowed")
		return c, false
	}
	return c, true
}

// Rate limiting is per peer and purpose; it does not trust X-Forwarded-For.
func (a *App) mcpRate(key string, limit int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	// Bound additional MCP keys without discarding the app's login rate limits.
	if len(a.attempts) > 2048 {
		for k, times := range a.attempts {
			if strings.HasPrefix(k, "mcp:") && (len(times) == 0 || now.Sub(times[len(times)-1]) > time.Minute) {
				delete(a.attempts, k)
			}
		}
	}
	key = "mcp:" + key
	recent := []time.Time{}
	for _, t := range a.attempts[key] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= limit {
		return false
	}
	a.attempts[key] = append(recent, now)
	return true
}
func mcpOAuthError(w http.ResponseWriter, status int, kind, description string) {
	writeJSON(w, status, map[string]string{"error": kind, "error_description": description})
}

func (a *App) mcpConfigGet(w http.ResponseWriter, r *http.Request) {
	c, err := a.loadMCP()
	if err != nil {
		apiError(w, 500, "Could not read MCP settings")
		return
	}
	grants, err := a.db.Query("SELECT g.id,c.name,g.scope,g.created_at,g.expires_at,g.revoked FROM mcp_grants g JOIN mcp_clients c ON c.id=g.client_id ORDER BY g.created_at DESC LIMIT 100")
	if err != nil {
		apiError(w, 500, "Could not read MCP connections")
		return
	}
	audit, err := a.db.Query("SELECT id,grant_id,action,task_count,created_at FROM mcp_audit ORDER BY created_at DESC LIMIT 50")
	if err != nil {
		apiError(w, 500, "Could not read MCP audit log")
		return
	}
	if grants == nil {
		grants = store.Rows{}
	}
	if audit == nil {
		audit = store.Rows{}
	}
	writeJSON(w, 200, map[string]any{"config": c, "suggested_url": strings.TrimRight(a.cfg.AppURL, "/"), "endpoint": c.PublicURL + "/mcp", "grants": grants, "audit": audit})
}
func (a *App) mcpConfigPut(w http.ResponseWriter, r *http.Request) {
	var c mcpConfig
	if !readJSON(w, r, &c) {
		return
	}
	c.PublicURL = strings.TrimRight(strings.TrimSpace(c.PublicURL), "/")
	if c.Enabled && (!validMCPOrigin(c.PublicURL) || strings.TrimRight(a.cfg.AppURL, "/") != c.PublicURL) {
		apiError(w, 400, "Brug en offentlig HTTPS-adresse uden sti. Saet APP_URL til samme adresse i Yggdrasil og genstart foerst.")
		return
	}
	if len(c.PublicURL) > 250 {
		apiError(w, 400, "Address is too long")
		return
	}
	err := a.db.Transaction(func(tx *store.Tx) error {
		old, e := tx.Query("SELECT public_url FROM mcp_settings WHERE id=1")
		if e != nil {
			return e
		}
		if !c.Enabled || (len(old) > 0 && old[0]["public_url"] != c.PublicURL) {
			if e = tx.Exec("UPDATE mcp_grants SET revoked='1'"); e != nil {
				return e
			}
			if e = tx.Exec("DELETE FROM mcp_codes"); e != nil {
				return e
			}
		}
		enabled, write := "0", "0"
		if c.Enabled {
			enabled = "1"
		}
		if c.AllowWrite {
			write = "1"
		}
		return tx.Exec("INSERT INTO mcp_settings(id,enabled,public_url,allow_write) VALUES(1,?,?,?) ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled,public_url=excluded.public_url,allow_write=excluded.allow_write", enabled, c.PublicURL, write)
	})
	if err != nil {
		apiError(w, 500, "Could not save MCP settings")
		return
	}
	writeJSON(w, 200, c)
}
func (a *App) mcpRevoke(w http.ResponseWriter, r *http.Request) {
	var v struct {
		ID string `json:"id"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	if !validID(v.ID) {
		apiError(w, 400, "Invalid connection ID")
		return
	}
	if err := a.db.Exec("UPDATE mcp_grants SET revoked='1' WHERE id=?", v.ID); err != nil {
		apiError(w, 500, "Could not revoke connection")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) mcpResourceMetadata(w http.ResponseWriter, r *http.Request) {
	c, ok := a.mcpAvailable(w, r)
	if !ok {
		return
	}
	scopes := []string{mcpRead}
	if c.AllowWrite {
		scopes = append(scopes, mcpWrite)
	}
	writeJSON(w, 200, map[string]any{"resource": c.PublicURL + "/mcp", "authorization_servers": []string{c.PublicURL}, "scopes_supported": scopes, "bearer_methods_supported": []string{"header"}, "resource_name": "FamilieHub opgaver"})
}
func (a *App) mcpAuthMetadata(w http.ResponseWriter, r *http.Request) {
	c, ok := a.mcpAvailable(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"issuer": c.PublicURL, "authorization_endpoint": c.PublicURL + "/oauth/authorize", "token_endpoint": c.PublicURL + "/oauth/token", "registration_endpoint": c.PublicURL + "/oauth/register", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{mcpRead, mcpWrite}, "authorization_response_iss_parameter_supported": true})
}

var mcpCallbackID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var mcpChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var mcpVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

func validMCPRedirect(s string) bool {
	u, e := url.Parse(s)
	if e != nil || u.Scheme != "https" || u.Host != "chatgpt.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	if u.Path == "/connector_platform_oauth_redirect" {
		return true
	}
	return strings.HasPrefix(u.Path, "/connector/oauth/") && mcpCallbackID.MatchString(strings.TrimPrefix(u.Path, "/connector/oauth/"))
}
func (a *App) mcpRegister(w http.ResponseWriter, r *http.Request) {
	_, ok := a.mcpAvailable(w, r)
	if !ok {
		return
	}
	if !a.mcpRate("register:"+remoteIP(r), 10) {
		mcpOAuthError(w, 429, "temporarily_unavailable", "Registration rate limit")
		return
	}
	var v struct {
		Name          string   `json:"client_name"`
		Redirects     []string `json:"redirect_uris"`
		Method        string   `json:"token_endpoint_auth_method"`
		GrantTypes    []string `json:"grant_types"`
		ResponseTypes []string `json:"response_types"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || json.NewDecoder(r.Body).Decode(&v) != nil {
		mcpOAuthError(w, 400, "invalid_client_metadata", "Expected JSON client metadata")
		return
	}
	if len(v.Redirects) < 1 || len(v.Redirects) > 4 || len(v.Name) > 100 || (v.Method != "" && v.Method != "none") {
		mcpOAuthError(w, 400, "invalid_client_metadata", "Public PKCE clients only")
		return
	}
	for _, s := range v.Redirects {
		if !validMCPRedirect(s) {
			mcpOAuthError(w, 400, "invalid_redirect_uri", "Only official ChatGPT HTTPS callbacks are supported")
			return
		}
	}
	for _, s := range v.GrantTypes {
		if s != "authorization_code" && s != "refresh_token" {
			mcpOAuthError(w, 400, "invalid_client_metadata", "Unsupported grant")
			return
		}
	}
	for _, s := range v.ResponseTypes {
		if s != "code" {
			mcpOAuthError(w, 400, "invalid_client_metadata", "Unsupported response type")
			return
		}
	}
	if strings.TrimSpace(v.Name) == "" {
		v.Name = "ChatGPT"
	}
	id := newID()
	raw, _ := json.Marshal(v.Redirects)
	err := a.db.Transaction(func(tx *store.Tx) error {
		rows, e := tx.Query("SELECT COUNT(*) AS n FROM mcp_clients")
		if e != nil {
			return e
		}
		n, _ := strconv.Atoi(rows[0]["n"])
		if n >= 100 {
			return errors.New("client limit")
		}
		return tx.Exec("INSERT INTO mcp_clients(id,name,redirects,created_at) VALUES(?,?,?,?)", id, v.Name, string(raw), mcpNow())
	})
	if err != nil {
		mcpOAuthError(w, 503, "temporarily_unavailable", "Registration unavailable")
		return
	}
	writeJSON(w, 201, map[string]any{"client_id": id, "client_id_issued_at": time.Now().Unix(), "client_name": v.Name, "redirect_uris": v.Redirects, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}})
}

type mcpAuthorization struct {
	Client    string `json:"client_id"`
	Redirect  string `json:"redirect_uri"`
	Resource  string `json:"resource"`
	Scope     string `json:"scope"`
	Challenge string `json:"code_challenge"`
	State     string `json:"state"`
	Nonce     string `json:"nonce"`
	Expires   string `json:"expires_at"`
}

func (a *App) mcpClientMatches(v mcpAuthorization) (string, bool) {
	rows, e := a.db.Query("SELECT name,redirects FROM mcp_clients WHERE id=?", v.Client)
	if e != nil || len(rows) != 1 {
		return "", false
	}
	var redirects []string
	if json.Unmarshal([]byte(rows[0]["redirects"]), &redirects) != nil {
		return "", false
	}
	for _, s := range redirects {
		if s == v.Redirect && validMCPRedirect(s) {
			return rows[0]["name"], true
		}
	}
	return "", false
}
func (a *App) mcpAuthorizePage(w http.ResponseWriter, r *http.Request) {
	c, ok := a.mcpAvailable(w, r)
	if !ok {
		return
	}
	if !a.mcpRate("authorize:"+remoteIP(r), 40) {
		apiError(w, 429, "Too many authorization requests")
		return
	}
	q := r.URL.Query()
	v := mcpAuthorization{Client: q.Get("client_id"), Redirect: q.Get("redirect_uri"), Resource: q.Get("resource"), Scope: q.Get("scope"), Challenge: q.Get("code_challenge"), State: q.Get("state"), Nonce: mcpSecret(), Expires: time.Now().UTC().Add(10 * time.Minute).Format(mcpTime)}
	name, valid := a.mcpClientMatches(v)
	if !valid || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || !mcpChallenge.MatchString(v.Challenge) || v.Resource != c.PublicURL+"/mcp" || len(v.State) > 1024 || v.State == "" {
		apiError(w, 400, "Invalid OAuth request. Restart the connection in ChatGPT.")
		return
	}
	scope, valid := mcpScope(v.Scope)
	if !valid {
		mcpOAuthError(w, 400, "invalid_scope", "Unsupported scope")
		return
	}
	v.Scope = scope
	if mcpHas(v.Scope, mcpWrite) && !c.AllowWrite {
		mcpOAuthError(w, 400, "invalid_scope", "Enable changes in FamilieHub MCP settings first")
		return
	}
	raw, _ := json.Marshal(v)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	sealed := payload + "." + mac(a.key, "mcp-consent:"+payload)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	_ = mcpConsentTemplate.Execute(w, struct {
		Request, Name string
		Write         bool
	}{sealed, name, mcpHas(v.Scope, mcpWrite)})
}

var mcpConsentTemplate = template.Must(template.New("mcp-consent").Parse(`<!doctype html><html lang="da"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Forbind FamilieHub</title><link rel="stylesheet" href="/style.css"><script src="/mcp.js" defer></script><body><main class="login-screen"><section class="login-card"><h1>Forbind FamilieHub</h1><p>Klienten <strong>{{.Name}}</strong> vil l&aelig;se familiemedlemmer, opgaver og planer. Godkend kun, hvis du selv startede forbindelsen i ChatGPT.</p><p>Returadresse: chatgpt.com. Kalenderforbindelser, adgangskoder og API-n&oslash;gler deles ikke.</p><div id="mcpConsent" data-request="{{.Request}}"><form id="mcpLoginForm"><label>Administratoradgangskode<input type="password" id="mcpLoginPassword" autocomplete="current-password" required></label><button class="primary" type="submit">Log ind i FamilieHub</button></form><div id="mcpConsentActions" class="hidden">{{if .Write}}<label><input type="checkbox" id="mcpConsentWrite"> Tillad ogs&aring; rettelser af opgaver efter forh&aring;ndsvisning</label>{{end}}<p>Adgangen g&aelig;lder i h&oslash;jst 30 dage og kan til enhver tid tilbagekaldes under Indstillinger.</p><button type="button" id="mcpApprove" class="primary">Godkend forbindelse</button><button type="button" id="mcpDeny" class="minor">Afvis</button></div><p id="mcpConsentStatus" class="error" role="status"></p></div></section></main></body></html>`))

func (a *App) mcpConsent(w http.ResponseWriter, r *http.Request) {
	c, ok := a.mcpAvailable(w, r)
	if !ok {
		return
	}
	var body struct {
		Request    string `json:"request"`
		Approve    bool   `json:"approve"`
		AllowWrite bool   `json:"allow_write"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	parts := strings.Split(body.Request, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(mac(a.key, "mcp-consent:"+parts[0]))) {
		apiError(w, 400, "Invalid consent request")
		return
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	var v mcpAuthorization
	if e != nil || json.Unmarshal(raw, &v) != nil || v.Expires <= mcpNow() || v.Resource != c.PublicURL+"/mcp" {
		apiError(w, 400, "Consent expired. Start again in ChatGPT.")
		return
	}
	if _, ok := a.mcpClientMatches(v); !ok {
		apiError(w, 400, "OAuth client no longer valid")
		return
	}
	if !c.AllowWrite || !body.AllowWrite {
		v.Scope = mcpRead
	}
	u, _ := url.Parse(v.Redirect)
	q := u.Query()
	q.Set("state", v.State)
	q.Set("iss", c.PublicURL)
	code := mcpSecret()
	err := a.db.Transaction(func(tx *store.Tx) error {
		if e := tx.Exec("DELETE FROM mcp_used_consents WHERE expires_at<?", mcpNow()); e != nil {
			return e
		}
		if e := tx.Exec("INSERT INTO mcp_used_consents(nonce,expires_at) VALUES(?,?)", sha(v.Nonce), v.Expires); e != nil {
			return e
		}
		if !body.Approve {
			return nil
		}
		return tx.Exec("INSERT INTO mcp_codes(hash,client_id,redirect_uri,challenge,resource,scope,epoch,expires_at) VALUES(?,?,?,?,?,?,?,?)", sha(code), v.Client, v.Redirect, v.Challenge, v.Resource, v.Scope, a.mcpEpoch(), time.Now().UTC().Add(5*time.Minute).Format(mcpTime))
	})
	if err != nil {
		apiError(w, 409, "Consent already used or unavailable; restart connection")
		return
	}
	if body.Approve {
		q.Set("code", code)
	} else {
		q.Set("error", "access_denied")
	}
	u.RawQuery = q.Encode()
	writeJSON(w, 200, map[string]string{"redirect_url": u.String()})
}

func (a *App) mcpToken(w http.ResponseWriter, r *http.Request) {
	c, ok := a.mcpAvailable(w, r)
	if !ok {
		return
	}
	if !a.mcpRate("token:"+remoteIP(r), 60) {
		mcpOAuthError(w, 429, "temporarily_unavailable", "Token rate limit")
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		mcpOAuthError(w, 415, "invalid_request", "Expected form data")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if r.ParseForm() != nil {
		mcpOAuthError(w, 400, "invalid_request", "Invalid form")
		return
	}
	f := r.PostForm
	if f.Get("resource") != "" && f.Get("resource") != c.PublicURL+"/mcp" {
		mcpOAuthError(w, 400, "invalid_target", "Resource mismatch")
		return
	}
	access, refresh := mcpSecret(), mcpSecret()
	now := mcpNow()
	grant, scope, expires := "", "", ""
	invalid := errors.New("invalid grant")
	replay := false
	err := a.db.Transaction(func(tx *store.Tx) error {
		switch f.Get("grant_type") {
		case "authorization_code":
			if !mcpVerifier.MatchString(f.Get("code_verifier")) || f.Get("resource") != c.PublicURL+"/mcp" {
				return invalid
			}
			rows, e := tx.Query("SELECT * FROM mcp_codes WHERE hash=?", sha(f.Get("code")))
			if e != nil {
				return e
			}
			if len(rows) != 1 {
				return invalid
			}
			v := rows[0]
			if v["expires_at"] <= now || v["client_id"] != f.Get("client_id") || v["redirect_uri"] != f.Get("redirect_uri") || v["resource"] != c.PublicURL+"/mcp" || v["epoch"] != a.mcpEpoch() || !hmac.Equal([]byte(v["challenge"]), []byte(mcpPKCE(f.Get("code_verifier")))) {
				return invalid
			}
			if e = tx.Exec("DELETE FROM mcp_codes WHERE hash=?", sha(f.Get("code"))); e != nil {
				return e
			}
			grant, scope, expires = newID(), v["scope"], time.Now().UTC().Add(30*24*time.Hour).Format(mcpTime)
			if e = tx.Exec("INSERT INTO mcp_grants(id,client_id,scope,resource,epoch,created_at,expires_at) VALUES(?,?,?,?,?,?,?)", grant, v["client_id"], scope, v["resource"], v["epoch"], now, expires); e != nil {
				return e
			}
		case "refresh_token":
			rows, e := tx.Query("SELECT t.grant_id,t.used,t.expires_at AS token_expiry,g.* FROM mcp_tokens t JOIN mcp_grants g ON g.id=t.grant_id WHERE t.hash=? AND t.kind='refresh'", sha(f.Get("refresh_token")))
			if e != nil {
				return e
			}
			if len(rows) != 1 {
				return invalid
			}
			v := rows[0]
			if v["client_id"] != f.Get("client_id") || v["token_expiry"] <= now || v["expires_at"] <= now || v["revoked"] != "0" || v["epoch"] != a.mcpEpoch() || v["resource"] != c.PublicURL+"/mcp" {
				return invalid
			}
			if v["used"] != "0" {
				replay = true
				return tx.Exec("UPDATE mcp_grants SET revoked='1' WHERE id=?", v["grant_id"])
			}
			grant, scope, expires = v["grant_id"], v["scope"], v["expires_at"]
			if f.Get("scope") != "" && f.Get("scope") != scope {
				return invalid
			}
			if e = tx.Exec("UPDATE mcp_tokens SET used='1' WHERE hash=?", sha(f.Get("refresh_token"))); e != nil {
				return e
			}
		default:
			return invalid
		}
		accessExpiry := time.Now().UTC().Add(15 * time.Minute).Format(mcpTime)
		if expires < accessExpiry {
			accessExpiry = expires
		}
		if e := tx.Exec("INSERT INTO mcp_tokens(hash,grant_id,kind,expires_at) VALUES(?,?,'access',?)", sha(access), grant, accessExpiry); e != nil {
			return e
		}
		return tx.Exec("INSERT INTO mcp_tokens(hash,grant_id,kind,expires_at) VALUES(?,?,'refresh',?)", sha(refresh), grant, expires)
	})
	if err != nil || replay {
		mcpOAuthError(w, 400, "invalid_grant", "Authorization expired, revoked, already used or invalid. Reconnect in ChatGPT.")
		return
	}
	writeJSON(w, 200, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 900, "refresh_token": refresh, "scope": scope})
}

type mcpIdentity struct{ Grant, Scope, Resource string }

func (a *App) mcpIdentity(r *http.Request, c mcpConfig) (mcpIdentity, bool) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) != 43 {
		return mcpIdentity{}, false
	}
	rows, e := a.db.Query("SELECT g.id,g.scope,g.resource,g.epoch FROM mcp_tokens t JOIN mcp_grants g ON g.id=t.grant_id WHERE t.hash=? AND t.kind='access' AND t.expires_at>? AND g.expires_at>? AND g.revoked='0'", sha(parts[1]), mcpNow(), mcpNow())
	if e != nil || len(rows) != 1 || rows[0]["resource"] != c.PublicURL+"/mcp" || rows[0]["epoch"] != a.mcpEpoch() {
		return mcpIdentity{}, false
	}
	v := rows[0]
	return mcpIdentity{v["id"], v["scope"], v["resource"]}, true
}
func (a *App) mcpChallengeResponse(w http.ResponseWriter, c mcpConfig) {
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+c.PublicURL+`/.well-known/oauth-protected-resource/mcp", scope="tasks:read"`)
	apiError(w, 401, "Connect to FamilieHub using OAuth")
}

// Tool arguments are strict even though future-compatible MCP envelopes allow
// extra protocol metadata. This prevents accidental broad or ambiguous writes.
func mcpDecode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("Invalid arguments")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("Expected one JSON object")
	}
	return nil
}
