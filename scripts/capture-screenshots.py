#!/usr/bin/env python3
"""Capture the real frontend + Go backend using ONLY a temporary demo database.

No server URL or existing DATA_DIR is accepted. A new loopback-only server,
random administrator password and fictional family are created on every run.
Requires Go/CGO/SQLite, Python 3.10+, Playwright and Pillow.
"""
from __future__ import annotations

import hashlib
import http.cookiejar
import io
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.request
from datetime import datetime, timedelta, timezone
from zoneinfo import ZoneInfo

from PIL import Image
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "docs" / "screenshots"
ZONE = ZoneInfo("Europe/Copenhagen")


def main() -> None:
    OUTPUT.mkdir(parents=True, exist_ok=True)
    now = datetime.now(ZONE)
    today = now.date()
    monday = today - timedelta(days=today.weekday())
    images: dict[str, dict] = {}
    with tempfile.TemporaryDirectory(prefix="familiehub-demo-") as temporary:
        work = Path(temporary)
        binary = work / "familiehub"
        subprocess.run(["go", "build", "-ldflags", "-X main.buildCommit=demo", "-o", str(binary), "."], cwd=ROOT, check=True)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        origin = f"http://127.0.0.1:{port}"
        password = secrets.token_urlsafe(32)
        environment = os.environ.copy()
        environment.update(APP_PASSWORD=password, APP_URL=origin, DATA_DIR=str(work / "data"), HA_URL="", HA_TOKEN="", HA_CALENDARS="", TZ="Europe/Copenhagen")
        with (work / "server.log").open("w+") as log:
            server = subprocess.Popen([str(binary), "-listen", f"127.0.0.1:{port}"], cwd=ROOT, env=environment, stdout=log, stderr=subprocess.STDOUT)
            try:
                deadline = time.monotonic() + 20
                while True:
                    if server.poll() is not None:
                        raise RuntimeError("The isolated demo server exited before startup")
                    try:
                        with urllib.request.urlopen(origin + "/healthz", timeout=1) as response:
                            if response.status == 200:
                                break
                    except OSError:
                        if time.monotonic() > deadline:
                            raise RuntimeError("The isolated demo server did not become ready")
                        time.sleep(0.1)
                jar = http.cookiejar.CookieJar()
                client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
                csrf = ""

                def api(path: str, body: dict | None = None, method: str | None = None):
                    # All requests are tied to the newly spawned local server.
                    assert path.startswith("/api/")
                    data = None if body is None else json.dumps(body).encode()
                    request = urllib.request.Request(origin + path, data=data, method=method or ("POST" if body is not None else "GET"), headers={"Content-Type": "application/json", "X-CSRF-Token": csrf})
                    with client.open(request, timeout=10) as response:
                        return json.load(response)

                csrf = api("/api/login", {"username": "admin", "password": password})["csrf"]
                people = []
                for position, (name, color) in enumerate([("Maja", "#8158b1"), ("Noah", "#2086b5"), ("Sofie", "#cf578f"), ("Jonas", "#2a9d8f")]):
                    people.append(api("/api/people", {"name": name, "color": color, "position": position, "notify_service": ""}))
                api("/api/planner/seed", {})
                templates = api("/api/templates")
                wanted = {"Tømme og fylde opvaskemaskine", "Rydde op efter aftensmad", "Tørre køkkenbord og spisebord", "Dække bord", "Tømme små skraldespande", "Vande planter", "Rengøre badeværelse", "Gå ud med skrald", "Støvsuge fællesarealer"}
                plan = {"people": [p["id"] for p in people[:2]], "choices": [{"template_id": t["id"], "times_per_week": t["times_per_week"]} for t in templates if t["title"] in wanted], "start_date": today.isoformat(), "rotate_weeks": 1, "preferences": {}, "avoid": {}}
                api("/api/planner/apply", plan)
                entries = api("/api/tasks/week?start=" + monday.isoformat())["entries"]
                current = [entry for entry in entries if entry["day"] == today.isoformat()]
                assert len(current) > 4, "Demo must exercise the full daily checklist"
                api("/api/tasks/" + current[0]["id"] + "/toggle", {"day": today.isoformat(), "done": True})
                for title, hour, duration, person, location in [
                    ("Skole og projektarbejde", 8, 360, 0, "Skolen"),
                    ("Fodboldtræning", 16, 90, 1, "Sportshallen"),
                    ("Hent indkøb", 17, 30, 2, "Supermarkedet"),
                    ("Fælles aftensmad", 18, 60, 3, "Hjemme"),
                ]:
                    start = datetime.combine(today, datetime.min.time(), tzinfo=ZONE).replace(hour=hour)
                    api("/api/events", {"title": title, "start_at": start.isoformat(), "end_at": (start + timedelta(minutes=duration)).isoformat(), "kind": "aftale", "person_id": people[person]["id"], "pickup_by": "", "location": location, "notes": "Fiktive demodata til dokumentation."})
                for offset, title, person in [(0, "Musikundervisning", 0), (4, "Filmaften", 1), (5, "Fælles gåtur", 2), (6, "Familiebrunch", 3)]:
                    start = datetime.combine(monday + timedelta(days=offset), datetime.min.time(), tzinfo=ZONE).replace(hour=11)
                    api("/api/events", {"title": title, "start_at": start.isoformat(), "end_at": (start + timedelta(hours=1)).isoformat(), "kind": "aftale", "person_id": people[person]["id"], "pickup_by": "", "location": "", "notes": "Fiktive demodata."})

                with sync_playwright() as playwright:
                    browser = playwright.chromium.launch()
                    browser_version = browser.version
                    context = browser.new_context(viewport={"width": 1280, "height": 1000}, device_scale_factor=1, locale="da-DK", timezone_id="Europe/Copenhagen", reduced_motion="reduce")
                    errors: list[str] = []
                    page = context.new_page()
                    page.on("pageerror", lambda error: errors.append(str(error)))

                    def login(target) -> None:
                        target.goto(origin, wait_until="domcontentloaded")
                        target.locator("#loginForm").wait_for(state="visible")
                        target.locator("#username").fill("admin")
                        target.locator("#password").fill(password)
                        target.locator("#loginForm button").click()
                        expect(target.locator("#app")).to_be_visible()
                        expect(target.locator("#viewTitle")).to_have_text("Dagsoverblik")
                        target.wait_for_function("document.querySelectorAll('#taskList .task-row').length === 4")

                    def capture(name: str, target=page, locator: str | None = None, full_page: bool = False) -> None:
                        target.evaluate("document.activeElement && document.activeElement.blur()")
                        target.evaluate("document.fonts.ready")
                        raw = target.locator(locator).screenshot(animations="disabled") if locator else target.screenshot(full_page=full_page, animations="disabled")
                        image = Image.open(io.BytesIO(raw)).convert("RGB")
                        destination = OUTPUT / name
                        image.save(destination, format="WEBP", quality=88, method=6)
                        images[name] = {"width": image.width, "height": image.height, "sha256": hashlib.sha256(destination.read_bytes()).hexdigest()}
                        print(f"Captured {name}: {image.width}x{image.height}", flush=True)

                    login(page)
                    expect(page.locator("#dayBtn")).to_have_attribute("aria-pressed", "true")
                    capture("kalender.webp")
                    page.locator("#showDayTasksBtn").click()
                    expect(page.locator("#tasksPage")).to_be_visible()
                    expect(page.locator("#tasksPageList .task-page-card")).to_have_count(len(current))
                    assert page.locator('#tasksPageList input[type="checkbox"]:checked').count() == 1
                    capture("opgaver.webp", full_page=True)
                    page.locator('a.main-tab[data-page="indstillinger"]').click()
                    expect(page.locator("#settingsPage")).to_be_visible()
                    expect(page.locator("#settingsList .settings-item")).to_have_count(15)
                    assert page.locator("#settingsPage #plannerPanel").count() == 1
                    page.locator("#settingsSearch").fill("bord")
                    expect(page.locator("#settingsList .settings-item")).to_have_count(2)
                    page.evaluate("window.scrollTo(0, 0)")
                    capture("indstillinger.webp")
                    page.locator("#plannerOpenBtn").click()
                    expect(page.locator("#plannerStartDate")).to_have_value(today.isoformat())
                    capture("planlaegger.webp", locator=".dialog")
                    with page.expect_response(lambda r: "/api/planner/preview" in r.url and r.request.method == "POST") as preview_response:
                        page.locator('#plannerForm button[type="submit"]').click()
                    assert preview_response.value.status == 200
                    expect(page.locator("#plannerPreview h3")).to_have_text("Forslag til opgaveplan")
                    page.locator("#plannerPreview").scroll_into_view_if_needed()
                    capture("planforslag.webp", locator=".dialog")
                    page.locator("#closeModal").click()
                    display = page.locator('section[aria-label="Skærm og nattilstand"]')
                    display.scroll_into_view_if_needed()
                    expect(page.locator("#saverFrom")).to_have_value("20:00")
                    expect(page.locator("#saverTo")).to_have_value("07:00")
                    capture("skaermindstillinger.webp", locator='section[aria-label="Skærm og nattilstand"]')
                    page.locator("#saverPreviewBtn").click()
                    expect(page.locator("#screensaver")).to_be_visible()
                    capture("nattilstand.webp")
                    page.locator("#screensaver").click()
                    expect(page.locator("#screensaver")).not_to_be_visible()
                    phone_context = browser.new_context(viewport={"width": 390, "height": 844}, device_scale_factor=1, is_mobile=True, has_touch=True, locale="da-DK", timezone_id="Europe/Copenhagen", reduced_motion="reduce")
                    phone = phone_context.new_page()
                    phone.on("pageerror", lambda error: errors.append(str(error)))
                    login(phone)
                    assert phone.evaluate("document.documentElement.scrollWidth <= window.innerWidth + 1"), "Daily mobile view must fit the viewport"
                    capture("iphone.webp", target=phone)
                    phone.locator("#weekBtn").click()
                    expect(phone.locator("#calendar .day-column")).to_have_count(7)
                    phone.locator("#calendar .day-column").last.scroll_into_view_if_needed()
                    capture("iphone-sondag.webp", target=phone)
                    assert not errors, "Browser JavaScript errors: " + "; ".join(errors)
                    phone_context.close()
                    context.close()
                    browser.close()
                source_files = sorted([*ROOT.glob("web/*"), *ROOT.glob("*.go"), *ROOT.glob("internal/**/*.go")])
                source_hashes = {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest() for path in source_files if path.is_file()}
                manifest = {"schema_version": 1, "captured_at": datetime.now(timezone.utc).isoformat(), "fixture_date": today.isoformat(), "timezone": "Europe/Copenhagen", "fixture_kind": "fictional family in a new temporary SQLite database", "backend": "real Go app, loopback only, no external integrations", "browser": "Chromium " + browser_version, "source_hashes": source_hashes, "images": images}
                (OUTPUT / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
                print("Verified real login, daily checklist, archive placement, planner preview, screensaver settings and mobile navigation.", flush=True)
            finally:
                server.terminate()
                try:
                    server.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    server.kill()
                    server.wait()


if __name__ == "__main__":
    main()
