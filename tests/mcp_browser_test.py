"""End-to-end MCP settings and OAuth consent against a disposable FamilieHub.

Run after `go build -o /tmp/familiehub .`; requires Playwright Chromium.
All data is synthetic. The browser's HTTPS origin is routed to a loopback test
server: no requests, passwords, or OAuth codes are sent to external services.
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urlsplit, parse_qs
from urllib.request import Request, urlopen

from playwright.sync_api import sync_playwright, expect

ORIGIN = "https://familie.example.com"
PASSWORD = "synthetic-mcp-test-password"
CALLBACK = "https://chatgpt.com/connector_platform_oauth_redirect"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--out", default="/tmp/familiehub-mcp-browser")
    args = parser.parse_args()
    output = Path(args.out)
    output.mkdir(parents=True, exist_ok=True)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    local = f"http://127.0.0.1:{port}"

    def upstream(method: str, path: str, data: bytes | None = None,
                 headers: dict[str, str] | None = None):
        request = Request(local + path, data=data, headers=headers or {}, method=method)
        try:
            response = urlopen(request, timeout=10)
        except HTTPError as error:
            response = error
        with response:
            return response.status, dict(response.headers), response.read()

    def post(path: str, data: dict, form: bool = False):
        encoded = urlencode(data).encode() if form else json.dumps(data).encode()
        kind = "application/x-www-form-urlencoded" if form else "application/json"
        status, _, body = upstream("POST", path, encoded, {"Content-Type": kind})
        assert status in (200, 201), f"HTTP {status} at {path}: {body[:200]!r}"
        return json.loads(body)

    with tempfile.TemporaryDirectory(prefix="familiehub-mcp-ui-") as directory:
        env = dict(os.environ, DATA_DIR=directory, APP_PASSWORD=PASSWORD, APP_URL=ORIGIN)
        process = subprocess.Popen([args.binary, "-listen", f"127.0.0.1:{port}"], env=env,
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            for _ in range(100):
                try:
                    if upstream("GET", "/healthz")[0] == 200:
                        break
                except (URLError, OSError):
                    time.sleep(0.1)
            else:
                raise RuntimeError("Test server did not start")
            with sync_playwright() as playwright:
                browser = playwright.chromium.launch(headless=True, args=["--no-sandbox"])
                context = browser.new_context(viewport={"width": 1024, "height": 900})
                page = context.new_page()
                errors: list[str] = []
                page.on("pageerror", lambda error: errors.append(str(error)))

                def route_request(route):
                    request = route.request
                    url = urlsplit(request.url)
                    if url.hostname == "chatgpt.com":
                        route.fulfill(status=200, content_type="text/plain", body="OAuth callback received (test only)")
                        return
                    if url.hostname != "familie.example.com":
                        route.abort()
                        return
                    if url.path == "/api/stream":
                        route.abort()
                        return
                    headers = {k: v for k, v in request.all_headers().items()
                               if k.lower() not in {"host", "content-length", "accept-encoding"}}
                    path = url.path + ("?" + url.query if url.query else "")
                    status, response_headers, body = upstream(request.method, path,
                                                              request.post_data_buffer, headers)
                    response_headers = {k: v for k, v in response_headers.items()
                                        if k.lower() not in {"content-length", "transfer-encoding", "content-encoding"}}
                    route.fulfill(status=status, headers=response_headers, body=body)

                context.route("**/*", route_request)
                page.goto(ORIGIN + "/#indstillinger")
                page.locator("#username").fill("admin")
                page.locator("#password").fill(PASSWORD)
                page.locator("#loginForm button[type=submit]").click()
                expect(page.locator("#mcpSettingsPanel")).to_be_visible()
                expect(page.locator("#mcpEnabled")).not_to_be_checked()
                page.locator("#mcpPublicURL").fill(ORIGIN)
                page.locator("#mcpEnabled").check()
                page.locator("#mcpAllowWrite").check()
                page.locator("#mcpSettingsForm button[type=submit]").click()
                expect(page.locator("#mcpSettingsStatus")).to_contain_text("MCP er aktiv")
                expect(page.locator("#mcpEndpoint")).to_have_value(ORIGIN + "/mcp")
                page.locator("#mcpSettingsPanel").screenshot(path=str(output / "mcp-settings.png"))

                client = post("/oauth/register", {
                    "client_name": "ChatGPT test <script>window.hacked=true</script>",
                    "redirect_uris": [CALLBACK], "token_endpoint_auth_method": "none",
                })
                verifier = "test-pkce-verifier-" + "a" * 50
                challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).decode().rstrip("=")
                params = {"client_id": client["client_id"], "redirect_uri": CALLBACK,
                          "response_type": "code", "resource": ORIGIN + "/mcp",
                          "scope": "tasks:read tasks:write", "state": "browser-test-state",
                          "code_challenge": challenge, "code_challenge_method": "S256"}
                page.goto(ORIGIN + "/oauth/authorize?" + urlencode(params))
                expect(page.locator("#mcpConsentActions")).to_be_visible()
                expect(page.locator("#mcpConsentWrite")).not_to_be_checked()
                assert page.evaluate("window.hacked") is None, "Client name executed as script"
                page.screenshot(path=str(output / "mcp-consent.png"), full_page=True)
                page.locator("#mcpConsentWrite").check()
                page.locator("#mcpApprove").click()
                page.wait_for_url("https://chatgpt.com/connector_platform_oauth_redirect?**")
                query = parse_qs(urlsplit(page.url).query)
                assert query["state"] == ["browser-test-state"]
                assert query["iss"] == [ORIGIN]
                token = post("/oauth/token", {
                    "grant_type": "authorization_code", "code": query["code"][0],
                    "client_id": client["client_id"], "redirect_uri": CALLBACK,
                    "code_verifier": verifier, "resource": ORIGIN + "/mcp",
                }, form=True)
                raw = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}}).encode()
                status, _, body = upstream("POST", "/mcp", raw, {
                    "Content-Type": "application/json", "Authorization": "Bearer " + token["access_token"],
                    "Accept": "application/json, text/event-stream",
                })
                assert status == 200 and len(json.loads(body)["result"]["tools"]) == 7
                page.goto(ORIGIN + "/#indstillinger")
                expect(page.locator("#mcpConnections button")).to_have_count(1)
                page.on("dialog", lambda dialog: dialog.accept())
                page.locator("#mcpConnections button").click()
                expect(page.locator("#mcpConnections")).to_contain_text("Ingen aktive forbindelser")
                status, _, _ = upstream("POST", "/mcp", raw, {
                    "Content-Type": "application/json", "Authorization": "Bearer " + token["access_token"],
                })
                assert status == 401, "Revoked browser-approved access still works"
                assert not errors, f"Browser errors: {errors}"
                browser.close()
            print("MCP browser tests passed: settings, consent, PKCE, discovery, XSS escaping and revocation")
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)


if __name__ == "__main__":
    main()
