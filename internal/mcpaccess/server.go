// Package mcpaccess adds a narrowly scoped MCP endpoint to FamilieHub.
// It never exposes settings, credentials, calendar contents, or raw SQL.
package mcpaccess

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kw-aficionado/familiehub/internal/store"
)

const (
	ReadScope       = "family:read"
	WriteScope      = "plans:write"
	OfflineScope    = "offline_access"
	protocolVersion = "2025-11-25"
)

// Config.Admin must enforce administrator authentication AND CSRF on writes.
// BaseURL is only a suggested default; MCP remains disabled until configured.
type Config struct {
	DB       *store.DB
	Mux      *http.ServeMux
	Admin    func(http.HandlerFunc) http.HandlerFunc
	Notify   func()
	BaseURL  string
	Location *time.Location
	Now      func() time.Time
}
type Server struct {
	cfg   Config
	mu    sync.Mutex
	rates map[string]rateWindow
}
type rateWindow struct {
	until time.Time
	count int
}
type settings struct {
	Enabled   bool   `json:"enabled"`
	PublicURL string `json:"public_url"`
}

var idPattern = regexp.MustCompile(`^[a-f0-9]{24,64}$`)

func Register(c Config) (*Server, error) {
	if c.DB == nil || c.Mux == nil || c.Admin == nil {
		return nil, errors.New("MCP requires database, mux and admin middleware")
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Notify == nil {
		c.Notify = func() {}
	}
	if c.Location == nil {
		var err error
		c.Location, err = time.LoadLocation("Europe/Copenhagen")
		if err != nil {
			return nil, err
		}
	}
	s := &Server{cfg: c, rates: map[string]rateWindow{}}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	c.Mux.HandleFunc("GET /api/mcp/config", c.Admin(s.configGet))
	c.Mux.HandleFunc("PUT /api/mcp/config", c.Admin(s.configPut))
	c.Mux.HandleFunc("POST /api/mcp/clients", c.Admin(s.clientCreate))
	c.Mux.HandleFunc("DELETE /api/mcp/clients/{id}", c.Admin(s.clientRevoke))
	c.Mux.HandleFunc("GET /api/mcp/authorization", c.Admin(s.authorizationInfo))
	c.Mux.HandleFunc("POST /api/mcp/authorize", c.Admin(s.authorize))
	c.Mux.HandleFunc("GET /api/mcp/plans", c.Admin(s.adminPlans))
	c.Mux.HandleFunc("POST /api/mcp/plans/preview-shift", c.Admin(s.adminPreview))
	c.Mux.HandleFunc("POST /api/mcp/plans/apply-shift", c.Admin(s.adminApply))
	c.Mux.HandleFunc("GET /api/mcp/audit", c.Admin(s.auditGet))
	c.Mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.authMetadata)
	c.Mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.resourceMetadata)
	c.Mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.resourceMetadata)
	c.Mux.HandleFunc("POST /mcp/oauth/token", s.token)
	c.Mux.HandleFunc("POST /mcp/oauth/revoke", s.revokeToken)
	c.Mux.HandleFunc("GET /mcp/authorize", s.authorizationPage)
	c.Mux.HandleFunc("GET /mcp/settings", s.settingsPage)
	// Use method-specific patterns: a methodless /mcp conflicts with
	// FamilieHub's existing "GET /" static-file route in Go 1.23.
	c.Mux.HandleFunc("POST /mcp", s.serveMCP)
	c.Mux.HandleFunc("GET /mcp", s.serveMCP)
	c.Mux.HandleFunc("DELETE /mcp", s.serveMCP)
	return s, nil
}
func (s *Server) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS mcp_settings(id INTEGER PRIMARY KEY CHECK(id=1),enabled INTEGER NOT NULL DEFAULT 0,public_url TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS mcp_clients(id TEXT PRIMARY KEY,name TEXT NOT NULL,secret_hash TEXT NOT NULL,redirect_uri TEXT NOT NULL,scope TEXT NOT NULL,revoked INTEGER NOT NULL DEFAULT 0,created_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS mcp_grants(id TEXT PRIMARY KEY,client_id TEXT NOT NULL,resource TEXT NOT NULL,scope TEXT NOT NULL,expires_at INTEGER NOT NULL,revoked INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS mcp_codes(hash TEXT PRIMARY KEY,grant_id TEXT NOT NULL,redirect_uri TEXT NOT NULL,challenge TEXT NOT NULL,expires_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS mcp_tokens(hash TEXT PRIMARY KEY,grant_id TEXT NOT NULL,kind TEXT NOT NULL,expires_at INTEGER NOT NULL,used INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS mcp_tokens_grant ON mcp_tokens(grant_id)`,
		`CREATE TABLE IF NOT EXISTS mcp_proposals(id TEXT PRIMARY KEY,actor TEXT NOT NULL,plan_id TEXT NOT NULL,snapshot_hash TEXT NOT NULL,payload TEXT NOT NULL,expires_at INTEGER NOT NULL,receipt TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS mcp_audit(id TEXT PRIMARY KEY,at TEXT NOT NULL,actor TEXT NOT NULL,action TEXT NOT NULL,plan_id TEXT NOT NULL,details TEXT NOT NULL)`,
	}
	for _, q := range statements {
		if err := s.cfg.DB.Exec(q); err != nil {
			return fmt.Errorf("MCP migration: %w", err)
		}
	}
	return nil
}
func tokenValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func digest(v string) string  { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
func unix(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }
func number(v string) int64   { n, _ := strconv.ParseInt(v, 10, 64); return n }
func out(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	out(w, status, map[string]string{"error": msg})
}
func parseJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "Expected application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "Invalid JSON data")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400, "Expected one JSON object")
		return false
	}
	return true
}
func validBase(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("Use an HTTPS origin without a path, query or credentials")
	}
	local := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		local = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", errors.New("Remote MCP requires HTTPS; HTTP is allowed only for loopback development")
	}
	if len(raw) > 300 {
		return "", errors.New("URL is too long")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
func validRedirect(raw string) bool {
	if len(raw) > 1000 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	local := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		local = ip.IsLoopback()
	}
	return u.Scheme == "https" || (u.Scheme == "http" && local)
}
func (s *Server) settings() (settings, error) {
	rows, err := s.cfg.DB.Query("SELECT enabled,public_url FROM mcp_settings WHERE id=1")
	if err != nil {
		return settings{}, err
	}
	if len(rows) == 0 {
		return settings{PublicURL: strings.TrimRight(s.cfg.BaseURL, "/")}, nil
	}
	return settings{Enabled: rows[0]["enabled"] == "1", PublicURL: rows[0]["public_url"]}, nil
}
func (s *Server) ready() (settings, error) {
	c, err := s.settings()
	if err != nil {
		return c, err
	}
	if !c.Enabled {
		return c, errors.New("MCP access is disabled")
	}
	c.PublicURL, err = validBase(c.PublicURL)
	return c, err
}
func (s *Server) configGet(w http.ResponseWriter, r *http.Request) {
	c, err := s.settings()
	if err != nil {
		fail(w, 500, "Could not read MCP settings")
		return
	}
	clients, err := s.cfg.DB.Query("SELECT id,name,redirect_uri,scope,revoked,created_at FROM mcp_clients ORDER BY created_at DESC")
	if err != nil {
		fail(w, 500, "Could not read clients")
		return
	}
	if clients == nil {
		clients = store.Rows{}
	}
	out(w, 200, map[string]any{"enabled": c.Enabled, "public_url": c.PublicURL, "endpoint": c.PublicURL + "/mcp", "clients": clients, "read_scope": ReadScope, "write_scope": WriteScope})
}
func (s *Server) configPut(w http.ResponseWriter, r *http.Request) {
	var v settings
	if !parseJSON(w, r, &v) {
		return
	}
	var err error
	if v.Enabled || v.PublicURL != "" {
		v.PublicURL, err = validBase(v.PublicURL)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
	}
	enabled := "0"
	if v.Enabled {
		enabled = "1"
	}
	err = s.cfg.DB.Transaction(func(tx *store.Tx) error {
		old, err := tx.Query("SELECT public_url FROM mcp_settings WHERE id=1")
		if err != nil {
			return err
		}
		if !v.Enabled || (len(old) > 0 && old[0]["public_url"] != v.PublicURL) {
			if err := tx.Exec("UPDATE mcp_grants SET revoked=1"); err != nil {
				return err
			}
		}
		return tx.Exec("INSERT INTO mcp_settings(id,enabled,public_url) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled,public_url=excluded.public_url", enabled, v.PublicURL)
	})
	if err != nil {
		fail(w, 500, "Could not save MCP settings")
		return
	}
	out(w, 200, v)
}
func canonicalScopes(raw string) (string, error) {
	seen := map[string]bool{}
	for _, v := range strings.Fields(raw) {
		if v != ReadScope && v != WriteScope && v != OfflineScope {
			return "", errors.New("Unsupported scope")
		}
		seen[v] = true
	}
	if !seen[ReadScope] {
		return "", errors.New("family:read is required")
	}
	values := []string{}
	for v := range seen {
		values = append(values, v)
	}
	sort.Strings(values)
	return strings.Join(values, " "), nil
}
func hasScope(raw, scope string) bool {
	for _, v := range strings.Fields(raw) {
		if v == scope {
			return true
		}
	}
	return false
}
func subset(want, allowed string) bool {
	for _, v := range strings.Fields(want) {
		if !hasScope(allowed, v) {
			return false
		}
	}
	return true
}
func (s *Server) clientCreate(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Name     string `json:"name"`
		Redirect string `json:"redirect_uri"`
		Scope    string `json:"scope"`
	}
	if !parseJSON(w, r, &v) {
		return
	}
	v.Name = strings.TrimSpace(v.Name)
	if len(v.Name) == 0 || len([]rune(v.Name)) > 60 || !validRedirect(v.Redirect) {
		fail(w, 400, "Supply a name and the exact HTTPS OAuth callback URL from the client")
		return
	}
	scope, err := canonicalScopes(v.Scope)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	id, err := tokenValue()
	if err != nil {
		fail(w, 500, "Could not generate client ID")
		return
	}
	secret, err := tokenValue()
	if err != nil {
		fail(w, 500, "Could not generate client secret")
		return
	}
	err = s.cfg.DB.Transaction(func(tx *store.Tx) error {
		count, err := tx.Query("SELECT COUNT(*) AS n FROM mcp_clients WHERE revoked=0")
		if err != nil {
			return err
		}
		if number(count[0]["n"]) >= 20 {
			return errors.New("limit")
		}
		return tx.Exec("INSERT INTO mcp_clients(id,name,secret_hash,redirect_uri,scope,created_at) VALUES(?,?,?,?,?,?)", id, v.Name, digest(secret), v.Redirect, scope, unix(s.cfg.Now()))
	})
	if err != nil {
		fail(w, 400, "Could not create client (maximum 20 active clients)")
		return
	}
	out(w, 201, map[string]string{"client_id": id, "client_secret": secret, "scope": scope, "redirect_uri": v.Redirect, "notice": "Copy this secret into the MCP client configuration, never into a chat. It is shown only once."})
}
func (s *Server) clientRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !idPattern.MatchString(id) {
		fail(w, 400, "Invalid client ID")
		return
	}
	err := s.cfg.DB.Transaction(func(tx *store.Tx) error {
		if err := tx.Exec("UPDATE mcp_clients SET revoked=1 WHERE id=?", id); err != nil {
			return err
		}
		return tx.Exec("UPDATE mcp_grants SET revoked=1 WHERE client_id=?", id)
	})
	if err != nil {
		fail(w, 500, "Could not revoke access")
		return
	}
	out(w, 200, map[string]bool{"ok": true})
}
func (s *Server) allow(key string, limit int) bool {
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.rates[key]
	if !now.Before(v.until) {
		v = rateWindow{until: now.Add(time.Minute)}
	}
	if v.count >= limit {
		return false
	}
	if len(s.rates) >= 2048 {
		for k, x := range s.rates {
			if !now.Before(x.until) {
				delete(s.rates, k)
			}
		}
		if len(s.rates) >= 2048 {
			if _, ok := s.rates[key]; !ok {
				return false
			}
		}
	}
	v.count++
	s.rates[key] = v
	return true
}
func remote(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
func (s *Server) auditGet(w http.ResponseWriter, r *http.Request) {
	rows, err := s.cfg.DB.Query("SELECT id,at,actor,action,plan_id,details FROM mcp_audit ORDER BY at DESC,id DESC LIMIT 50")
	if err != nil {
		fail(w, 500, "Could not read audit log")
		return
	}
	if rows == nil {
		rows = store.Rows{}
	}
	out(w, 200, rows)
}
