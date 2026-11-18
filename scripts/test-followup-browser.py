#!/usr/bin/env python3
"""Real-browser followup test on a NEW loopback app with fictional data only.

No existing server, database, secrets or family calendars can be supplied.
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
from datetime import datetime, timedelta
from zoneinfo import ZoneInfo
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--artifacts', type=Path)
    args = parser.parse_args()
    if args.artifacts:
        args.artifacts.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='fh-followup-test-') as temp:
        work = Path(temp)
        subprocess.run(['go', 'build', '-ldflags', '-X main.buildCommit=demo', '-o', str(work/'app'), '.'], cwd=ROOT, check=True)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0)); port = sock.getsockname()[1]
        origin = f'http://127.0.0.1:{port}'
        password = secrets.token_urlsafe(32)
        env = dict(os.environ, APP_URL=origin, APP_PASSWORD=password, DATA_DIR=str(work/'data'), HA_URL='', HA_TOKEN='', HA_CALENDARS='')
        with (work/'server.log').open('w+') as log:
            server = subprocess.Popen([str(work/'app'), '-listen', f'127.0.0.1:{port}'], cwd=ROOT, env=env, stdout=log, stderr=log)
            try:
                for _ in range(150):
                    if server.poll() is not None:
                        raise RuntimeError('Isolated server failed')
                    try:
                        urllib.request.urlopen(origin+'/healthz', timeout=1).close(); break
                    except OSError:
                        time.sleep(.1)
                else:
                    raise RuntimeError('Isolated server startup timeout')
                client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
                csrf = ''
                def api(path, body=None, method=None):
                    assert path.startswith('/api/')
                    req = urllib.request.Request(origin+path, data=None if body is None else json.dumps(body).encode(), method=method or ('GET' if body is None else 'POST'), headers={'Content-Type':'application/json','X-CSRF-Token':csrf})
                    with client.open(req, timeout=15) as response:
                        return json.load(response)
                csrf = api('/api/login', {'password':password})['csrf']
                people = [api('/api/people', {'name':name, 'color':color, 'position':i}) for i,(name,color) in enumerate([('Maja','#8158b1'),('Noah','#2086b5')])]
                today = datetime.now(ZoneInfo('Europe/Copenhagen')).date()
                prior = (today-timedelta(days=2)).isoformat()
                tomorrow = (today+timedelta(days=1)).isoformat()
                def task(title, owner, rule, policy):
                    return api('/api/tasks', {'title':title,'assigned_to':people[owner]['id'],'due_date':prior,'repeat_rule':rule,'followup_policy':policy})
                cleaning = task('Reng\u00f8re badev\u00e6relse',0,'none','carry')
                laundry = task('Folde vasket\u00f8j',1,'none','carry')
                routine = task('Pakke skoletaske',0,'daily','routine')
                task('Pakke skoletaske',1,'daily','routine')
                day_only = task('D\u00e6kke bord',1,'daily','date')
                assert api('/api/followup')['settings']['from']==''
                with sync_playwright() as pw:
                    browser = pw.chromium.launch()
                    context = browser.new_context(viewport={'width':1280,'height':1000}, locale='da-DK', timezone_id='Europe/Copenhagen')
                    page = context.new_page(); errors=[]
                    page.on('pageerror',lambda error:errors.append(str(error)))
                    page.goto(origin); page.locator('#password').fill(password); page.locator('#loginForm button').click()
                    expect(page.locator('#app')).to_be_visible()
                    expect(page.locator('#homeFollowup')).not_to_be_visible()
                    page.locator('[data-page="indstillinger"]').click()
                    expect(page.locator('#followupPanel')).to_be_visible()
                    page.locator('#followupSettings [name="from"]').fill(prior)
                    page.once('dialog', lambda d:d.accept())
                    page.locator('#followupSettings button[type="submit"]').click()
                    expect(page.locator('#followupEnabledText')).to_contain_text(prior)
                    expect(page.locator('#followupReport .followup-summary')).to_have_count(2)
                    expect(page.locator('#followupSettings [name="from"]')).to_be_disabled()
                    # Day-specific and routine history is not copied into the residual list.
                    page.locator('[data-page="kalender"]').click()
                    expect(page.locator('#homeFollowup .followup-card')).to_have_count(2)
                    expect(page.locator('#homeFollowup')).to_contain_text('4 manglende afkrydsninger')
                    page.locator('[data-page="opgaver"]').click()
                    expect(page.locator('#tasksPageList .task-page-card')).to_have_count(3)
                    expect(page.locator('#tasksFollowup .followup-card')).to_have_count(2)
                    # Marking today's routine does not resolve the earlier dates.
                    task_row = page.locator('#tasksPageList .task-page-card').filter(has_text='Pakke skoletaske').filter(has_text='Maja')
                    task_row.locator('input[type="checkbox"]').check()
                    expect(task_row.locator('input')).to_be_checked()
                    board = api('/api/followup')
                    assert next(g for g in board['routines'] if g['task_id']==routine['id'])['missing']==2
                    # Create a tablet with today permission only: residual rights remain opt-in.
                    device=api('/api/tablets',{'name':'Demotablet','can_complete':True})
                    tablet_context=browser.new_context(viewport={'width':1024,'height':900},locale='da-DK',timezone_id='Europe/Copenhagen')
                    display=tablet_context.new_page();display.on('pageerror',lambda error:errors.append(str(error)))
                    display.goto(origin+'/tablet');display.locator('#tabletCode').fill(device['pair_code']);display.locator('#tabletPairForm button').click()
                    expect(display.locator('#tabletApp')).to_be_visible()
                    expect(display.locator('#tabletFollowup .followup-card')).to_have_count(2)
                    expect(display.locator('#tabletFollowup button')).to_have_count(0)
                    page.locator('[data-page="indstillinger"]').click()
                    page.locator('#tabletRefreshBtn').click()
                    device_card=page.locator('.tablet-device-card').filter(has_text='Demotablet')
                    device_card.get_by_role('button',name='Redig\u00e9r',exact=True).click()
                    expect(page.locator('#tabletEditor [name="can_followup"]')).not_to_be_checked()
                    page.locator('#tabletEditor [name="can_followup"]').check()
                    page.locator('#tabletEditor button[type="submit"]').click();expect(page.locator('#modal')).not_to_be_visible()
                    display.reload();expect(display.locator('#tabletFollowup button')).to_have_count(4)
                    clean_card=display.locator('#tabletFollowup .followup-card[data-task-id="'+cleaning['id']+'"]')
                    clean_card.get_by_role('button',name='Bed om hj\u00e6lp').click()
                    expect(clean_card).to_contain_text('Hj\u00e6lp \u00f8nskes')
                    # Admin defers without changing the original due date or current owner.
                    page.locator('[data-page="kalender"]').click()
                    admin_card=page.locator('#homeFollowup .followup-card[data-task-id="'+cleaning['id']+'"]')
                    admin_card.get_by_role('button',name='Vurder / udskyd').click()
                    page.locator('#followupActionForm [name="action"]').select_option('defer')
                    page.locator('#followupActionForm [name="date"]').fill(tomorrow)
                    page.locator('#followupActionForm button[type="submit"]').click();expect(page.locator('#modal')).not_to_be_visible()
                    display.reload();expect(clean_card).to_contain_text('Udskudt til '+tomorrow)
                    expect(clean_card).to_contain_text('Oprindelig frist '+prior)
                    expect(clean_card.locator('button')).to_have_count(0)
                    laundry_card=display.locator('#tabletFollowup .followup-card[data-task-id="'+laundry['id']+'"]')
                    if args.artifacts:
                        display.screenshot(path=str(args.artifacts/'followup-tablet.png'),full_page=True)
                        display.set_viewport_size({'width':390,'height':844})
                        assert display.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
                        display.screenshot(path=str(args.artifacts/'followup-phone.png'),full_page=True)
                    with display.expect_response(lambda r:r.url.endswith('/api/tablet/followup/action')) as finished:
                        laundry_card.get_by_role('button',name='Udf\u00f8rt nu').click()
                    assert finished.value.status==200
                    payload=finished.value.request.post_data_json
                    result=finished.value.json()
                    assert result['key']==prior and result['performed_on']==today.isoformat()
                    session=display.request.get(origin+'/api/tablet/session').json()
                    retry=display.request.post(origin+'/api/tablet/followup/action',data=payload,headers={'X-CSRF-Token':session['csrf']})
                    assert retry.status==200 and retry.json()['version']==result['version']
                    expect(display.locator('#tabletFollowup .followup-card')).to_have_count(1)
                    # Adult-only waiver: reason required and NOT marked completed.
                    admin_card.get_by_role('button',name='Vurder / udskyd').click()
                    page.locator('#followupActionForm [name="action"]').select_option('excuse')
                    page.locator('#followupActionForm [name="note"]').fill('Ikke n\u00f8dvendigt denne gang')
                    page.locator('#followupActionForm button[type="submit"]').click();expect(page.locator('#modal')).not_to_be_visible()
                    expect(page.locator('#homeFollowup .followup-card')).to_have_count(0)
                    page.locator('[data-page="indstillinger"]').click();page.locator('#followupRefreshBtn').click()
                    expect(page.locator('#followupReport')).to_contain_text('Fritaget: 1')
                    expect(page.locator('#followupReport')).to_contain_text('Forsinket: 1')
                    board=api('/api/followup');clean=next(o for o in board['review'] if o['task_id']==cleaning['id'])
                    assert clean['status']=='excused' and not clean['performed_on']
                    assert len(api('/api/tasks/all'))==5, 'Residual followup created duplicate task series'
                    if args.artifacts:
                        page.locator('#followupPanel').screenshot(path=str(args.artifacts/'followup-adult.png'))
                        page.set_viewport_size({'width':390,'height':844})
                        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
                    page.locator('#logoutBtn').click();expect(page.locator('#login')).to_be_visible()
                    expect(page.locator('#homeFollowup .followup-card')).to_have_count(0)
                    assert not errors, errors
                    tablet_context.close();context.close();browser.close()
                print('PASS followup browser: opt-in policies, routine aggregation, day ticks, scoped tablet rights, help, deferral, late completion, replay, waiver, adult overview, no copies, logout and mobile layout. Fictional data only.')
            finally:
                server.terminate()
                try:server.wait(timeout=10)
                except subprocess.TimeoutExpired:server.kill();server.wait()

if __name__=='__main__':
    main()
