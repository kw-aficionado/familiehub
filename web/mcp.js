/* Optional MCP management and OAuth consent, with no third-party dependencies. */
(function () {
  'use strict';
  var $ = function (id) { return document.getElementById(id); };
  function node(tag, text, attrs) {
    var n = document.createElement(tag);
    if (text !== undefined && text !== null) n.textContent = text;
    Object.keys(attrs || {}).forEach(function (k) { n.setAttribute(k, attrs[k]); });
    return n;
  }
  var csrf = '';
  async function request(path, method, body) {
    var headers = {}, opts = {method: method || 'GET', credentials: 'same-origin', headers: headers, cache: 'no-store'};
    if (body !== undefined) { headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
    if (method && method !== 'GET') headers['X-CSRF-Token'] = csrf;
    var response = await fetch(path, opts), data = await response.json().catch(function () { return null; });
    if (!response.ok || data === null) throw new Error(data && data.error ? data.error : 'Forbindelsen fejlede (HTTP ' + response.status + ').');
    return data;
  }
  async function session() { var s = await request('/api/session'); csrf = s.csrf || ''; return s; }

  if ($('mcpConsent')) {
    var message = $('mcpConsentStatus');
    async function checkLogin() {
      try {
        var s = await session(), admin = s.authenticated && s.account && s.account.role === 'admin';
        $('mcpLoginForm').classList.toggle('hidden', !!admin);
        $('mcpConsentActions').classList.toggle('hidden', !admin);
        message.textContent = admin ? '' : 'Godkend med FamilieHubs administratorlogin. Adgangskoden sendes kun til denne FamilieHub-server.';
      } catch (e) { message.textContent = e.message; }
    }
    $('mcpLoginForm').addEventListener('submit', async function (e) {
      e.preventDefault(); var button = e.currentTarget.querySelector('button'); button.disabled = true;
      try { await request('/api/login', 'POST', {username: 'admin', password: $('mcpLoginPassword').value}); $('mcpLoginPassword').value = ''; await checkLogin(); }
      catch (err) { message.textContent = err.message; }
      finally { button.disabled = false; }
    });
    async function consent(approve) {
      $('mcpApprove').disabled = true; $('mcpDeny').disabled = true;
      try {
        var result = await request('/api/mcp/authorize', 'POST', {
          request: $('mcpConsent').getAttribute('data-request'), approve: approve,
          allow_write: !!($('mcpConsentWrite') && $('mcpConsentWrite').checked)
        });
        // The server validates and signs the exact registered callback; do not
        // accept any client-supplied return URL from the browser query string.
        var target = new URL(result.redirect_url);
        if (target.protocol !== 'https:' || target.host !== 'chatgpt.com') throw new Error('Ugyldig returadresse. Start forbindelsen igen.');
        window.location.assign(target.href);
      } catch (err) { message.textContent = err.message; $('mcpApprove').disabled = false; $('mcpDeny').disabled = false; }
    }
    $('mcpApprove').addEventListener('click', function () { consent(true); });
    $('mcpDeny').addEventListener('click', function () { consent(false); });
    checkLogin(); return;
  }

  var settings = $('settingsPage');
  if (!settings) return;
  var panel = node('section', null, {id: 'mcpSettingsPanel', 'class': 'settings-panel ai-settings-panel hidden', 'aria-label': 'MCP-adgang til ChatGPT'});
  panel.innerHTML = '<h2>MCP-adgang til ChatGPT</h2>' +
    '<p class="muted">Giv en godkendt ChatGPT-forbindelse adgang til at l\u00e6se og rette opgaver. Adgangen er sl\u00e5et fra som standard. Der deles ingen API-n\u00f8gler, kalenderforbindelser eller loginoplysninger.</p>' +
    '<form id="mcpSettingsForm" class="display-form">' +
    '<label class="field ai-check"><input type="checkbox" id="mcpEnabled"> Aktiv\u00e9r MCP</label>' +
    '<label class="field ai-check"><input type="checkbox" id="mcpAllowWrite"> Tillad rettelser efter forh\u00e5ndsvisning</label>' +
    '<label class="field field-wide">Offentlig HTTPS-adresse<input id="mcpPublicURL" type="url" maxlength="250" placeholder="https://familie.dit-domaene.dk"></label>' +
    '<p class="muted field-wide">Adressen skal v\u00e6re tilg\u00e6ngelig udefra via HTTPS og svare til APP_URL i Yggdrasil. En privat LAN- eller Tailscale-IP er ikke nok til en direkte forbindelse fra ChatGPT. Ekspon\u00e9r ikke appen uden HTTPS.</p>' +
    '<div class="form-actions"><button class="primary" type="submit">Gem MCP-indstillinger</button></div></form>' +
    '<p id="mcpSettingsStatus" role="status" class="muted"></p>' +
    '<label class="field">MCP-serveradresse<input id="mcpEndpoint" type="text" readonly></label>' +
    '<p class="muted">I ChatGPT: Plugins \u2192 + \u2192 Add custom MCP server. Brug adressen ovenfor og OAuth med automatisk klientregistrering (DCR). Godkend forbindelsen p\u00e5 FamilieHubs egen loginside. Klient-ID og klienthemmelighed skal normalt v\u00e6re tomme.</p>' +
    '<h3>Godkendte forbindelser</h3><div id="mcpConnections" class="settings-list"></div>' +
    '<h3>Seneste rettelser</h3><div id="mcpAudit" class="settings-list"></div>';
  settings.appendChild(panel);
  var busy = false;
  function render(data) {
    var c = data.config;
    $('mcpEnabled').checked = c.enabled; $('mcpAllowWrite').checked = c.allow_write;
    $('mcpPublicURL').value = c.public_url || (/^https:\/\//.test(data.suggested_url) ? data.suggested_url : '');
    $('mcpEndpoint').value = c.enabled ? data.endpoint : '';
    $('mcpSettingsStatus').textContent = c.enabled ? 'MCP er aktiv. Nye forbindelser kr\u00e6ver dit administratorlogin og dit samtykke.' : 'MCP er sl\u00e5et fra. Ingen eksterne MCP-kald kan l\u00e6se eller rette opgaver.';
    var root = $('mcpConnections'); root.replaceChildren();
    var active = (data.grants || []).filter(function (g) { return g.revoked === '0' && new Date(g.expires_at) > new Date(); });
    if (!active.length) root.appendChild(node('p', 'Ingen aktive forbindelser.', {'class': 'muted'}));
    active.forEach(function (g) {
      var row = node('div', null, {'class': 'settings-item'}), info = node('div', null, {'class': 'settings-item-info'});
      info.appendChild(node('strong', g.name));
      info.appendChild(node('small', g.scope + ' \u00b7 Udl\u00f8ber ' + new Date(g.expires_at).toLocaleDateString('da-DK')));
      row.appendChild(info);
      var revoke = node('button', 'Tilbagekald adgang', {type: 'button', 'class': 'danger'}); row.appendChild(revoke);
      revoke.addEventListener('click', async function () {
        if (!confirm('Tilbagekald denne forbindelses adgang til FamilieHub?')) return;
        revoke.disabled = true;
        try { await request('/api/mcp/revoke', 'POST', {id: g.id}); await refresh(); }
        catch (e) { $('mcpSettingsStatus').textContent = e.message; revoke.disabled = false; }
      });
      root.appendChild(row);
    });
    var log = $('mcpAudit'); log.replaceChildren();
    if (!(data.audit || []).length) log.appendChild(node('p', 'Ingen opgaver er rettet via MCP.', {'class': 'muted'}));
    (data.audit || []).forEach(function (e) {
      log.appendChild(node('p', new Date(e.created_at).toLocaleString('da-DK') + ' \u00b7 ' + (e.action === 'move_plan' ? 'Plan flyttet' : 'Opgaver rettet') + ' \u00b7 ' + e.task_count + ' opgaver', {'class': 'muted'}));
    });
  }
  async function refresh() {
    if (busy || location.hash !== '#indstillinger') return;
    busy = true;
    try {
      var s = await session();
      if (!s.authenticated || !s.account || s.account.role !== 'admin') { panel.classList.add('hidden'); return; }
      panel.classList.remove('hidden');
      render(await request('/api/mcp/config'));
    } catch (e) { $('mcpSettingsStatus').textContent = e.message; }
    finally { busy = false; }
  }
  $('mcpSettingsForm').addEventListener('submit', async function (e) {
    e.preventDefault(); var button = e.currentTarget.querySelector('button'); button.disabled = true;
    try {
      await request('/api/mcp/config', 'PUT', {enabled: $('mcpEnabled').checked, public_url: $('mcpPublicURL').value.trim(), allow_write: $('mcpAllowWrite').checked});
      await refresh();
    } catch (err) { $('mcpSettingsStatus').textContent = err.message; }
    finally { button.disabled = false; }
  });
  window.addEventListener('hashchange', refresh);
  if ($('app')) new MutationObserver(function () { if (!$('app').classList.contains('hidden')) refresh(); }).observe($('app'), {attributes: true, attributeFilter: ['class']});
  refresh();
})();
