package mcpaccess

import "net/http"

func sendPage(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'; form-action 'self'")
	_, _ = w.Write([]byte(html))
}

const pageStart = `<!doctype html><html lang="da"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>FamilieHub - MCP</title><link rel="stylesheet" href="/mcp-ui.css"><script src="/mcp-ui.js" defer></script></head><body><main class="mcp-page"><a href="/#indstillinger">Tilbage til FamilieHub</a><h1>FamilieHub - MCP-adgang</h1><p id="mcpMessage" role="status" aria-live="polite"></p><section id="mcpLogin" hidden><p>Log ind som administrator i FamilieHub, og vend tilbage til denne fane.</p><a href="/" target="_blank" rel="noopener">&Aring;bn FamilieHub-login</a><button type="button" id="mcpRetry">Kontroll&eacute;r login igen</button></section>`

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	sendPage(w, pageStart+`
<div id="mcpManage" hidden>
<section><h2>Flyt en eksisterende opgaveplan</h2><p>Dette virker ogs&aring;, n&aring;r ekstern MCP-adgang er sl&aring;et fra. Ingen data &aelig;ndres f&oslash;r godkendelse. Planer med afkrydsninger kan ikke flyttes.</p>
<form id="mcpShiftForm"><label>Plan<select id="mcpPlan" required></select></label><label>Ny startuge (mandag)<input id="mcpWeek" type="date" required></label><button type="submit">Vis datoflytning</button></form><div id="mcpShiftPreview" aria-live="polite"></div></section>
<section><h2>Ekstern MCP-forbindelse</h2><p>Sl&aring;et fra som standard. OAuth-adgang godkendes af en administrator og kan tilbagekaldes. En privat IP-adresse alene kan ikke bruges af en ekstern klient.</p>
<form id="mcpSettingsForm"><label>Offentlig HTTPS-adresse uden sti<input id="mcpURL" type="url" placeholder="https://familie.example.dk" maxlength="300"></label><label class="mcp-check"><input id="mcpEnabled" type="checkbox">Aktiv&eacute;r MCP</label><button type="submit">Gem MCP-indstillinger</button></form><p>Serveradresse: <code id="mcpEndpoint"></code></p></section>
<section><h2>Opret adgang til ChatGPT eller anden MCP-klient</h2><p>V&aelig;lg OAuth med et manuelt klient-id og en klienthemmelighed. Kopi&eacute;r den pr&aelig;cise callback-adresse fra MCP-klientens ops&aelig;tning. Del aldrig hemmeligheden i chatten.</p>
<form id="mcpClientForm"><label>Navn<input id="mcpClientName" maxlength="60" value="ChatGPT" required></label><label>OAuth callback / redirect URI<input id="mcpRedirect" type="url" placeholder="Kopi&eacute;r den pr&aelig;cise adresse fra klienten" maxlength="1000" required></label><label class="mcp-check"><input id="mcpWrite" type="checkbox">Tillad flytning af opgaveplaner efter forh&aring;ndsvisning og godkendelse</label><label class="mcp-check"><input id="mcpOffline" type="checkbox" checked>Bevar forbindelsen med roterende refresh tokens (h&oslash;jst 30 dage)</label><button type="submit">Opret OAuth-klient</button></form>
<div id="mcpCredentials" hidden><strong>Vises kun &eacute;n gang</strong><label>Client ID<input id="mcpClientID" readonly></label><label>Client secret<input id="mcpClientSecret" type="password" readonly autocomplete="off"></label><button id="mcpReveal" type="button">Vis/skjul hemmelighed</button><button id="mcpForget" type="button">Jeg har gemt den - fjern fra sk&aelig;rmen</button><p>Inds&aelig;t v&aelig;rdierne i klientens OAuth-felter. FamilieHubs adgangskode forlader ikke FamilieHub.</p></div>
<h3>Forbindelser</h3><div id="mcpClients"></div></section><section><h2>Seneste &aelig;ndringer</h2><div id="mcpAudit"></div></section></div></main></body></html>`)
}
func (s *Server) authorizationPage(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.RawQuery) > 8192 {
		fail(w, 400, "OAuth request too large")
		return
	}
	sendPage(w, pageStart+`<section id="mcpConsent" hidden><h2>Godkend forbindelsen</h2><p id="mcpConsentSummary"></p><p>Denne adgang omfatter kun opgaveplaner, opgavearkiv og familiemedlemmers navne. Ingen adgang til kalenderaftaler, konti, API-n&oslash;gler eller SMTP.</p><p>Adgangen udl&oslash;ber senest efter 30 dage og kan tilbagekaldes tidligere under MCP-indstillinger.</p><button type="button" id="mcpApprove">Godkend adgang</button><button type="button" id="mcpDeny">Afvis</button></section></main></body></html>`)
}
