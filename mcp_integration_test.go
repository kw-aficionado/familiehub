package main

import (
	"net/http/httptest"
	"testing"
)

// These checks use the existing session/CSRF implementation, not a new auth bypass.
func TestMCPIntegrationUsesExistingAdminAndCSRF(t *testing.T) {
	app, _ := testApp(t)
	server := httptest.NewServer(securityHeaders(app.mux))
	defer server.Close()
	code, _, _ := req(t, server, "GET", "/api/mcp/config", "", "", "")
	if code != 401 {
		t.Fatalf("unauthenticated MCP admin access: %d", code)
	}
	cookie, csrf := authenticated(t, server)
	code, config, _ := req(t, server, "GET", "/api/mcp/config", "", cookie, "")
	if code != 200 || config["enabled"] != false {
		t.Fatalf("default MCP config: %d %v", code, config)
	}
	code, _, _ = req(t, server, "PUT", "/api/mcp/config", `{"enabled":true,"public_url":"https://family.example.test"}`, cookie, "")
	if code != 403 {
		t.Fatalf("MCP write without CSRF: %d", code)
	}
	code, _, _ = req(t, server, "PUT", "/api/mcp/config", `{"enabled":true,"public_url":"https://family.example.test"}`, cookie, csrf)
	if code != 200 {
		t.Fatalf("admin cannot enable MCP: %d", code)
	}
	code, _, _ = req(t, server, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, cookie, csrf)
	if code != 401 {
		t.Fatalf("browser cookie must not authorize MCP: %d", code)
	}
	code, _, _ = req(t, server, "POST", "/api/accounts", `{"username":"mcpviewer","password":"long-mcp-test-password","person_id":"","enabled":true}`, cookie, csrf)
	if code != 200 {
		t.Fatalf("create member: %d", code)
	}
	code, login, response := req(t, server, "POST", "/api/login", `{"username":"mcpviewer","password":"long-mcp-test-password"}`, "", "")
	if code != 200 {
		t.Fatalf("member login: %d", code)
	}
	memberCookie := response.Cookies()[0].Name + "=" + response.Cookies()[0].Value
	code, _, _ = req(t, server, "POST", "/api/mcp/clients", `{"name":"unauthorized","redirect_uri":"https://client.example.test/callback","scope":"family:read"}`, memberCookie, login["csrf"].(string))
	if code != 403 {
		t.Fatalf("member may not authorize external access: %d", code)
	}
}
