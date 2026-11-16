#!/usr/bin/env python3
"""Real-browser regression of shared chores, duplicate review and settings.

Always starts a fresh loopback Go server and a temporary fictional database.
No production URL, database or credentials can be supplied. Optional artifacts
contain screenshots of that fictional database only.
"""
from __future__ import annotations
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.request
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
DISH = 'T\u00f8mme og fylde opvaskemaskine'
TABLE = 'D\u00e6kke bord'


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--artifacts', type=Path)
    args = parser.parse_args()
    if args.artifacts:
        args.artifacts.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='familiehub-browser-') as directory:
        work = Path(directory)
        binary = work / 'familiehub'
        subprocess.run(['go', 'build', '-o', str(binary), '.'], cwd=ROOT, check=True)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        origin = f'http://127.0.0.1:{port}'
        password = secrets.token_urlsafe(32)
        env = dict(os.environ, APP_PASSWORD=password, APP_URL=origin, DATA_DIR=str(work/'data'),
                   HA_URL='', HA_TOKEN='', HA_CALENDARS='', TZ='Europe/Copenhagen')
        with (work/'server.log').open('w+') as log:
            server = subprocess.Popen([str(binary), '-listen', f'127.0.0.1:{port}'], env=env, stdout=log, stderr=log)
            try:
                for _ in range(150):
                    if server.poll() is not None:
                        raise RuntimeError('Test server exited during startup')
                    try:
                        urllib.request.urlopen(origin+'/healthz', timeout=1).close()
                        break
                    except OSError:
                        time.sleep(.1)
                else:
                    raise RuntimeError('Test server startup timed out')
                client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
                csrf = ''

                def api(path: str, body=None):
                    request = urllib.request.Request(origin+path, data=json.dumps(body).encode() if body is not None else None,
                        headers={'Content-Type':'application/json', 'X-CSRF-Token':csrf})
                    with client.open(request, timeout=10) as response:
                        return json.load(response)

                csrf = api('/api/login', {'username':'admin','password':password})['csrf']
                people = [api('/api/people', {'name':name,'color':color,'position':i})
                          for i,(name,color) in enumerate([('Maja','#8158b1'),('Noah','#2086b5')])]
                api('/api/planner/seed', {})
                with sync_playwright() as pw:
                    browser = pw.chromium.launch()
                    page = browser.new_page(viewport={'width':1280,'height':1000},
                        timezone_id='Europe/Copenhagen', locale='da-DK')
                    errors = []
                    approvals = []
                    page.on('pageerror', lambda e: errors.append(str(e)))
                    page.on('request', lambda r: approvals.append(r.post_data_json)
                            if r.url.endswith('/api/quickstart/apply') else None)

                    def screenshot(name: str) -> None:
                        if args.artifacts:
                            page.screenshot(path=str(args.artifacts/name), full_page=True)

                    def choose_shared(swapped=False) -> None:
                        page.locator('#quickstartOpenBtn').click()
                        expect(page.locator('#quickstartForm')).to_be_visible()
                        for index,title in enumerate([DISH,TABLE]):
                            box = page.locator('fieldset.quick-task').filter(has=page.locator('legend',has_text=title))
                            box.locator('legend input').check()
                            box.locator('.quick-people input').nth(0).check()
                            box.locator('.quick-people input').nth(1).check()
                            owner = people[1-index if swapped else index]['id']
                            box.locator('[data-quick-starter]').select_option(owner)
                            box.locator('[data-quick-repeat]').select_option('daily')
                        page.locator('#quickstartForm button[type="submit"]').click()
                        expect(page.locator('#quickPreview h3')).to_have_text('Gennemg\u00e5 din turnus')

                    page.goto(origin,wait_until='domcontentloaded')
                    page.locator('#username').fill('admin')
                    page.locator('#password').fill(password)
                    page.locator('#loginForm button').click()
                    expect(page.locator('#app')).to_be_visible()
                    expect(page.locator('#dayBtn')).to_have_attribute('aria-pressed','true')
                    page.locator('[data-page="indstillinger"]').click()
                    expect(page.locator('#plannerPanel')).to_be_visible()
                    expect(page.locator('details.settings-section').first).to_be_visible()
                    page.locator('#settingsCollapseAll').click()
                    expect(page.locator('#plannerPanel')).not_to_be_visible()
                    page.reload(wait_until='domcontentloaded')
                    expect(page.locator('#settingsPage')).to_be_visible()
                    expect(page.locator('#plannerPanel')).not_to_be_visible()
                    screenshot('settings-collapsed.png')
                    summary = page.locator('details.settings-section > summary').first
                    summary.focus()
                    summary.press('Enter')
                    expect(page.locator('#settingsList')).to_be_visible()
                    page.locator('#settingsExpandAll').click()
                    page.locator('#familyWeekStart').select_option('0')
                    page.locator('#weekStartSave').click()
                    expect(page.locator('#quickstartStatus')).to_contain_text('Gemt')
                    choose_shared()
                    expect(page.locator('#quickPreview')).to_contain_text('Maja')
                    screenshot('quickstart-preview.png')
                    page.get_by_role('button',name='Godkend og gem turnus',exact=True).click()
                    expect(page.locator('#modal')).not_to_be_visible()
                    expect(page.locator('#plannerStatus')).to_contain_text('2 opgaver')
                    assert len(api('/api/tasks/all')) == 2
                    replay = api('/api/quickstart/apply', approvals[-1])
                    assert replay['reused'] is True
                    assert len(api('/api/tasks/all')) == 2
                    page.locator('#plannerViewPlanLink').click()
                    page.locator('[data-period="today"]').click()
                    expect(page.locator('#tasksPageList .task-page-card')).to_have_count(2)
                    screenshot('daily-shared-tasks.png')
                    # Ordinary duplicate: retire exactly that selected series,
                    # preserving the two shared series and their IDs.
                    day = api('/api/planner/diagnostics')['day']
                    duplicate = api('/api/tasks', {'title':DISH,'assigned_to':people[0]['id'],
                        'due_date':day,'repeat_rule':'daily'})
                    page.locator('[data-page="indstillinger"]').click()
                    page.locator('#taskDiagnosticsBtn').click()
                    row = page.locator('#taskDiagnostics .plan-conflict-row').filter(has_text=duplicate['id'][:8])
                    row.locator('input').check()
                    page.once('dialog',lambda dialog: dialog.accept())
                    page.locator('#retireSave').click()
                    expect(page.locator('#modal')).not_to_be_visible()
                    expect(page.locator('#quickstartStatus')).to_contain_text('1 serier stoppet')
                    assert len(api('/api/tasks/all')) == 3
                    # Replace the shared pair from today with the opposite first
                    # owner, using the shown conflict list and a single save.
                    choose_shared(swapped=True)
                    expect(page.locator('#quickPreview [data-replace-task]')).to_have_count(2)
                    for check in page.locator('#quickPreview [data-replace-task]').all():
                        check.check()
                    page.get_by_role('button',name='Godkend og gem turnus',exact=True).click()
                    expect(page.locator('#modal')).not_to_be_visible()
                    diagnostics = api('/api/planner/diagnostics')
                    active = [t for t in diagnostics['tasks'] if t['active_today']]
                    assert len(active) == 2
                    assert next(t for t in active if t['title']==DISH)['owner_today'] == people[1]['id']
                    assert len(diagnostics['tasks']) == 5, 'Replacement retains all old rows'
                    # The saved start date/form changes invalidate the preview.
                    choose_shared()
                    page.locator('#quickWeekStart').select_option('3')
                    expect(page.locator('#quickPreview h3')).to_have_count(0)
                    page.locator('#closeModal').click()
                    page.set_viewport_size({'width':390,'height':844})
                    page.locator('#quickstartOpenBtn').click()
                    expect(page.locator('#quickstartForm')).to_be_visible()
                    assert page.evaluate('document.documentElement.scrollWidth<=window.innerWidth+1')
                    screenshot('quickstart-phone.png')
                    page.locator('#closeModal').click()
                    assert not errors, 'Browser errors: '+'; '.join(errors)
                    browser.close()
                print('PASS: real browser login, settings persistence/keyboard, custom week, shared preview/save/retry, safe retirement/replacement, stale preview and phone layout')
            finally:
                server.terminate()
                try:
                    server.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    server.kill()
                    server.wait()


if __name__ == '__main__':
    main()
