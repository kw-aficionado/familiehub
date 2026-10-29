package mcpaccess

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kw-aficionado/familiehub/internal/store"
)

var challengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var verifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

func pkce(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func oauthError(w http.ResponseWriter, status int, code string) {
	out(w, status, map[string]string{"error": code})
}
func (s *Server) authMetadata(w http.ResponseWriter, r *http.Request) {
	c, err := s.ready()
	if err != nil {
		fail(w, 503, "MCP is not enabled with a valid HTTPS URL")
		return
	}
	out(w, 200, map[string]any{
		"issuer": c.PublicURL, "authorization_endpoint": c.PublicURL + "/mcp/authorize", "token_endpoint": c.PublicURL + "/mcp/oauth/token", "revocation_endpoint": c.PublicURL + "/mcp/oauth/revoke",
		"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"code_challenge_methods_supported":      []string{"S256"}, "scopes_supported": []string{ReadScope, WriteScope, OfflineScope},
		"authorization_response_iss_parameter_supported": true,
	})
}
func (s *Server) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	c, err := s.ready()
	if err != nil {
		fail(w, 503, "MCP is not enabled with a valid HTTPS URL")
		return
	}
	out(w, 200, map[string]any{"resource": c.PublicURL + "/mcp", "resource_name": "FamilieHub", "authorization_servers": []string{c.PublicURL}, "scopes_supported": []string{ReadScope, WriteScope}, "bearer_methods_supported": []string{"header"}})
}

type authorizationRequest struct {
	ClientID     string `json:"client_id"`
	RedirectURI  string `json:"redirect_uri"`
	ResponseType string `json:"response_type"`
	State        string `json:"state"`
	Scope        string `json:"scope"`
	Resource     string `json:"resource"`
	Challenge    string `json:"code_challenge"`
	Method       string `json:"code_challenge_method"`
	Allow        bool   `json:"allow"`
}

func authRequest(q url.Values) authorizationRequest {
	return authorizationRequest{ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"), ResponseType: q.Get("response_type"), State: q.Get("state"), Scope: q.Get("scope"), Resource: q.Get("resource"), Challenge: q.Get("code_challenge"), Method: q.Get("code_challenge_method")}
}
func (s *Server) validateAuthorization(v *authorizationRequest) (settings, map[string]string, error) {
	c, err := s.ready()
	if err != nil {
		return c, nil, err
	}
	if v.ResponseType != "code" || v.Method != "S256" || !challengePattern.MatchString(v.Challenge) || len(v.State) < 1 || len(v.State) > 1024 || v.Resource != c.PublicURL+"/mcp" {
		return c, nil, errors.New("Invalid OAuth request: resource, state and PKCE S256 are required")
	}
	rows, err := s.cfg.DB.Query("SELECT id,name,redirect_uri,scope FROM mcp_clients WHERE id=? AND revoked=0", v.ClientID)
	if err != nil || len(rows) != 1 {
		return c, nil, errors.New("Unknown or revoked OAuth client")
	}
	client := rows[0]
	if client["redirect_uri"] != v.RedirectURI {
		return c, nil, errors.New("OAuth callback URL does not exactly match the registered URL")
	}
	v.Scope, err = canonicalScopes(v.Scope)
	if err != nil || !subset(v.Scope, client["scope"]) {
		return c, nil, errors.New("Requested scopes exceed the client's permission")
	}
	return c, client, nil
}
func (s *Server) authorizationInfo(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.RawQuery) > 8192 {
		fail(w, 400, "OAuth request too large")
		return
	}
	v := authRequest(r.URL.Query())
	_, client, err := s.validateAuthorization(&v)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	out(w, 200, map[string]string{"name": client["name"], "scope": v.Scope, "resource": v.Resource, "redirect_uri": v.RedirectURI, "expires_in": "30 days maximum; revoke at any time in FamilieHub"})
}
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	var v authorizationRequest
	if !parseJSON(w, r, &v) {
		return
	}
	c, _, err := s.validateAuthorization(&v)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	destination, _ := url.Parse(v.RedirectURI)
	q := destination.Query()
	q.Set("state", v.State)
	q.Set("iss", c.PublicURL)
	if !v.Allow {
		q.Set("error", "access_denied")
		destination.RawQuery = q.Encode()
		out(w, 200, map[string]string{"redirect": destination.String()})
		return
	}
	code, err := tokenValue()
	if err != nil {
		fail(w, 500, "Could not generate authorization code")
		return
	}
	grant, err := tokenValue()
	if err != nil {
		fail(w, 500, "Could not generate authorization grant")
		return
	}
	now := s.cfg.Now()
	err = s.cfg.DB.Transaction(func(tx *store.Tx) error {
		// Recheck enablement and revocation in the grant transaction. A simultaneous
		// disable or URL change must not create a grant that revives on re-enable.
		settingRows, e := tx.Query("SELECT enabled,public_url FROM mcp_settings WHERE id=1")
		if e != nil {
			return e
		}
		if len(settingRows) != 1 || settingRows[0]["enabled"] != "1" || settingRows[0]["public_url"] != c.PublicURL {
			return errors.New("MCP disabled or reconfigured")
		}
		// Recheck revocation inside the same transaction as the grant.
		clients, err := tx.Query("SELECT id FROM mcp_clients WHERE id=? AND revoked=0", v.ClientID)
		if err != nil {
			return err
		}
		if len(clients) != 1 {
			return errors.New("revoked")
		}
		if err := tx.Exec("DELETE FROM mcp_codes WHERE expires_at<=?", unix(now)); err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO mcp_grants(id,client_id,resource,scope,expires_at) VALUES(?,?,?,?,?)", grant, v.ClientID, v.Resource, v.Scope, unix(now.Add(30*24*time.Hour))); err != nil {
			return err
		}
		return tx.Exec("INSERT INTO mcp_codes(hash,grant_id,redirect_uri,challenge,expires_at) VALUES(?,?,?,?,?)", digest(code), grant, v.RedirectURI, v.Challenge, unix(now.Add(5*time.Minute)))
	})
	if err != nil {
		fail(w, 500, "Could not authorize access")
		return
	}
	q.Set("code", code)
	destination.RawQuery = q.Encode()
	out(w, 200, map[string]string{"redirect": destination.String()})
}

// parseTokenForm rejects query-string credentials and ambiguous repeated fields.
func (s *Server) tokenForm(w http.ResponseWriter, r *http.Request) (settings, string, bool) {
	c, err := s.ready()
	if err != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return c, "", false
	}
	if !s.allow("oauth:"+remote(r), 60) {
		w.Header().Set("Retry-After", "60")
		oauthError(w, 429, "temporarily_unavailable")
		return c, "", false
	}
	if r.URL.RawQuery != "" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		oauthError(w, 400, "invalid_request")
		return c, "", false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != c.PublicURL {
		oauthError(w, 403, "access_denied")
		return c, "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := r.ParseForm(); err != nil {
		oauthError(w, 400, "invalid_request")
		return c, "", false
	}
	for _, values := range r.PostForm {
		if len(values) != 1 {
			oauthError(w, 400, "invalid_request")
			return c, "", false
		}
	}
	clientID, secret := r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	if user, pass, ok := r.BasicAuth(); ok {
		if clientID != "" || secret != "" {
			oauthError(w, 400, "invalid_request")
			return c, "", false
		}
		var err error
		clientID, err = url.QueryUnescape(user)
		if err != nil {
			oauthError(w, 400, "invalid_request")
			return c, "", false
		}
		secret, err = url.QueryUnescape(pass)
		if err != nil {
			oauthError(w, 400, "invalid_request")
			return c, "", false
		}
	}
	rows, err := s.cfg.DB.Query("SELECT secret_hash FROM mcp_clients WHERE id=? AND revoked=0", clientID)
	expected := strings.Repeat("0", 64)
	if err == nil && len(rows) == 1 {
		expected = rows[0]["secret_hash"]
	}
	match := subtle.ConstantTimeCompare([]byte(digest(secret)), []byte(expected)) == 1
	if err != nil || len(rows) != 1 || !match || len(secret) != 64 {
		w.Header().Set("WWW-Authenticate", `Basic realm="FamilieHub MCP"`)
		oauthError(w, 401, "invalid_client")
		return c, "", false
	}
	return c, clientID, true
}

var errInvalidGrant = errors.New("invalid_grant")

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	c, clientID, ok := s.tokenForm(w, r)
	if !ok {
		return
	}
	if r.PostForm.Get("resource") != c.PublicURL+"/mcp" {
		oauthError(w, 400, "invalid_target")
		return
	}
	grantType := r.PostForm.Get("grant_type")
	if grantType != "authorization_code" && grantType != "refresh_token" {
		oauthError(w, 400, "unsupported_grant_type")
		return
	}
	access, err := tokenValue()
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	refresh, err := tokenValue()
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	now := s.cfg.Now()
	var scope string
	var expires int64
	replayed := false
	err = s.cfg.DB.Transaction(func(tx *store.Tx) error {
		var grantID string
		settingRows, e := tx.Query("SELECT enabled,public_url FROM mcp_settings WHERE id=1")
		if e != nil {
			return e
		}
		if len(settingRows) != 1 || settingRows[0]["enabled"] != "1" || settingRows[0]["public_url"] != c.PublicURL {
			return errInvalidGrant
		}
		if grantType == "authorization_code" {
			rows, err := tx.Query("SELECT grant_id,redirect_uri,challenge,expires_at FROM mcp_codes WHERE hash=?", digest(r.PostForm.Get("code")))
			if err != nil {
				return err
			}
			if len(rows) != 1 {
				return errInvalidGrant
			}
			row := rows[0]
			verifier := r.PostForm.Get("code_verifier")
			if number(row["expires_at"]) <= now.Unix() || row["redirect_uri"] != r.PostForm.Get("redirect_uri") || !verifierPattern.MatchString(verifier) || subtle.ConstantTimeCompare([]byte(pkce(verifier)), []byte(row["challenge"])) != 1 {
				return errInvalidGrant
			}
			grantID = row["grant_id"]
		} else {
			rows, err := tx.Query("SELECT t.grant_id,t.used,t.expires_at FROM mcp_tokens t JOIN mcp_grants g ON g.id=t.grant_id WHERE t.hash=? AND t.kind='refresh' AND g.client_id=?", digest(r.PostForm.Get("refresh_token")), clientID)
			if err != nil {
				return err
			}
			if len(rows) != 1 {
				return errInvalidGrant
			}
			row := rows[0]
			grantID = row["grant_id"]
			if row["used"] == "1" {
				replayed = true
				return tx.Exec("UPDATE mcp_grants SET revoked=1 WHERE id=?", grantID)
			}
			if number(row["expires_at"]) <= now.Unix() {
				return errInvalidGrant
			}
		}
		rows, err := tx.Query("SELECT g.scope,g.expires_at FROM mcp_grants g JOIN mcp_clients c ON c.id=g.client_id WHERE g.id=? AND g.client_id=? AND g.resource=? AND g.revoked=0 AND c.revoked=0", grantID, clientID, c.PublicURL+"/mcp")
		if err != nil {
			return err
		}
		if len(rows) != 1 || number(rows[0]["expires_at"]) <= now.Unix() {
			return errInvalidGrant
		}
		scope = rows[0]["scope"]
		grantExpiry := number(rows[0]["expires_at"])
		if grantType == "refresh_token" && !hasScope(scope, OfflineScope) {
			return errInvalidGrant
		}
		if requested := r.PostForm.Get("scope"); requested != "" {
			canonical, e := canonicalScopes(requested)
			if e != nil || canonical != scope {
				return errInvalidGrant
			}
		} // no scope escalation or ambiguous refresh narrowing
		if grantType == "authorization_code" {
			if err := tx.Exec("DELETE FROM mcp_codes WHERE hash=?", digest(r.PostForm.Get("code"))); err != nil {
				return err
			}
		} else {
			if err := tx.Exec("UPDATE mcp_tokens SET used=1 WHERE hash=?", digest(r.PostForm.Get("refresh_token"))); err != nil {
				return err
			}
		}
		expiry := now.Add(time.Hour).Unix()
		if expiry > grantExpiry {
			expiry = grantExpiry
		}
		expires = expiry - now.Unix()
		if err := tx.Exec("INSERT INTO mcp_tokens(hash,grant_id,kind,expires_at) VALUES(?,?,'access',?)", digest(access), grantID, unix(time.Unix(expiry, 0))); err != nil {
			return err
		}
		if hasScope(scope, OfflineScope) {
			return tx.Exec("INSERT INTO mcp_tokens(hash,grant_id,kind,expires_at) VALUES(?,?,'refresh',?)", digest(refresh), grantID, unix(time.Unix(grantExpiry, 0)))
		}
		return nil
	})
	if replayed || errors.Is(err, errInvalidGrant) {
		oauthError(w, 400, "invalid_grant")
		return
	}
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	response := map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": expires, "scope": scope}
	if hasScope(scope, OfflineScope) {
		response["refresh_token"] = refresh
	}
	w.Header().Set("Pragma", "no-cache")
	out(w, 200, response)
}
func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	_, clientID, ok := s.tokenForm(w, r)
	if !ok {
		return
	}
	err := s.cfg.DB.Exec("UPDATE mcp_grants SET revoked=1 WHERE client_id=? AND id IN (SELECT grant_id FROM mcp_tokens WHERE hash=?)", clientID, digest(r.PostForm.Get("token")))
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	out(w, 200, map[string]bool{"ok": true})
}

type principal struct{ ClientID, GrantID, Scope string }

func (s *Server) authenticate(r *http.Request, c settings) (principal, bool) {
	// Cookies, API keys from other services and URI tokens are not authentication.
	if r.URL.RawQuery != "" {
		return principal{}, false
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) != 64 {
		return principal{}, false
	}
	rows, err := s.cfg.DB.Query(`SELECT g.client_id,g.id AS grant_id,g.scope FROM mcp_tokens t
 JOIN mcp_grants g ON g.id=t.grant_id JOIN mcp_clients c ON c.id=g.client_id
 WHERE t.hash=? AND t.kind='access' AND t.used=0 AND t.expires_at>?
 AND g.expires_at>? AND g.revoked=0 AND c.revoked=0 AND g.resource=?`, digest(parts[1]), unix(s.cfg.Now()), unix(s.cfg.Now()), c.PublicURL+"/mcp")
	if err != nil || len(rows) != 1 {
		return principal{}, false
	}
	v := rows[0]
	return principal{ClientID: v["client_id"], GrantID: v["grant_id"], Scope: v["scope"]}, true
}
