#!/usr/bin/env python3
"""Exercise midnight in the actual UI/backend using disposable fictional data.

Only browser time is simulated (yesterday -> real server today). No production
URL or data directory can be supplied. Backend dates are NOT mocked.
"""
from __future__ import annotations
import argparse
from datetime import datetime, timedelta, timezone, time as day_time
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
from zoneinfo import ZoneInfo
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parents[1]
ZONE = ZoneInfo('Europe/Copenhagen')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--artifacts', type=Path)
    args = parser.parse_args()
    if args.artifacts:
        args.artifacts.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='fh-midnight-test-') as directory:
        work = Path(directory)
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
                        raise RuntimeError('Isolated server exited')
                    try:
                        urllib.request.urlopen(origin+'/healthz', timeout=1).close(); break
                    except OSError:
                        time.sleep(.1)
                else:
                    raise RuntimeError('Isolated server did not start')
                client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
                csrf = ''
                def api(path, body=None):
                    req = urllib.request.Request(origin+path, data=None if body is None else json.dumps(body).encode(), headers={'Content-Type':'application/json','X-CSRF-Token':csrf})
                    with client.open(req, timeout=10) as response:
                        return json.load(response)
                csrf = api('/api/login', {'password':password})['csrf']
                people = [api('/api/people', {'name':name,'color':color,'position':i}) for i,(name,color) in enumerate([('Maja','#8158b1'),('Noah','#2086b5')])]
                today = datetime.now(ZONE).date()
                yesterday = today-timedelta(days=1)
                midnight = datetime.combine(today, day_time(), ZONE).astimezone(timezone.utc)
                before = midnight-timedelta(seconds=2)
                for date, title in [(yesterday,'Kun i gaar'),(today,'Kun i dag')]:
                    api('/api/tasks', {'title':title,'assigned_to':people[0]['id'],'due_date':date.isoformat(),'repeat_rule':'none'})
                    start = datetime.combine(date, day_time(hour=12), ZONE)
                    api('/api/events', {'title':title+' aftale','start_at':start.isoformat(),'end_at':(start+timedelta(hours=1)).isoformat(),'person_id':people[0]['id']})
                template = api('/api/templates', {'title':'Faelles opvask','repeat_rule':'daily','points':4,'minutes':15,'times_per_week':7})
                plan = {'start_date':yesterday.isoformat(),'week_start':(today.weekday()+1)%7,'choices':[{'template_id':template['id'],'people':[p['id'] for p in people],'starter':people[0]['id'],'mode':'rotate','repeat_rule':'daily','rotate_weeks':1}]}
                preview = api('/api/quickstart/preview',plan)
                plan.update(preview_hash=preview['preview_hash'],request_id=secrets.token_hex(16))
                api('/api/quickstart/apply',plan)
                count = len(api('/api/tasks/all'))
                errors=[]
                with sync_playwright() as pw:
                    browser = pw.chromium.launch(executable_path=os.environ.get('FH_TEST_CHROMIUM'))
                    def fresh(tablet=False, zone='Europe/Copenhagen'):
                        context=browser.new_context(viewport={'width':1024 if tablet else 1280,'height':900},locale='da-DK',timezone_id=zone)
                        page=context.new_page();page.on('pageerror',lambda e:errors.append(str(e)))
                        page.clock.install(time=before-timedelta(seconds=1));page.clock.pause_at(before)
                        page.goto(origin+('/tablet' if tablet else '/'),wait_until='domcontentloaded')
                        if tablet:
                            pair=api('/api/tablets',{'name':'Midnat demotablet','can_complete':True})
                            page.locator('#tabletCode').fill(pair['pair_code']);page.locator('#tabletPairForm button').click();expect(page.locator('#tabletApp')).to_be_visible()
                        else:
                            page.locator('#password').fill(password);page.locator('#loginForm button').click();expect(page.locator('#app')).to_be_visible()
                            expect(page.locator('#taskList')).to_contain_text('Kun i gaar')
                        return context,page
                    # Main dashboard: daily tasks, appointments and rotating owner change, no navigation.
                    context,page=fresh()
                    expect(page.locator('#taskList')).to_contain_text('Kun i gaar')
                    expect(page.locator('#taskList .task-row').filter(has_text='Faelles opvask')).to_contain_text('Maja')
                    navigation=[];page.on('framenavigated',lambda f:navigation.append(f.url) if f==page.main_frame else None)
                    page.clock.run_for(1999);expect(page.locator('#taskList')).to_contain_text('Kun i gaar')
                    page.clock.run_for(1)
                    expect(page.locator('#taskList')).to_contain_text('Kun i dag');expect(page.locator('#taskList')).not_to_contain_text('Kun i gaar')
                    expect(page.locator('#calendar')).to_contain_text('Kun i dag aftale')
                    expect(page.locator('#taskList .task-row').filter(has_text='Faelles opvask')).to_contain_text('Noah')
                    assert not navigation, 'Midnight must not reload the page or log out'
                    if args.artifacts:page.screenshot(path=str(args.artifacts/'midnight-dashboard.png'),full_page=True)
                    context.close()
                    # Task page receives its own fresh week data without a click.
                    context,page=fresh();page.locator('[data-page="opgaver"]').click()
                    expect(page.locator('#tasksPageList')).to_contain_text('Kun i gaar');page.clock.run_for(2000)
                    expect(page.locator('#tasksPageList')).to_contain_text('Kun i dag');expect(page.locator('#tasksPageList')).not_to_contain_text('Kun i gaar')
                    context.close()
                    # Editing survives midnight. Closing applies the pending date immediately.
                    context,page=fresh();page.locator('#addTaskBtn').click();expect(page.locator('#entryForm')).to_be_visible()
                    page.locator('#entryForm [name="title"]').fill('Ikke gemt kladde');old_date=page.locator('#entryForm [name="due_date"]').input_value()
                    page.clock.run_for(2000)
                    expect(page.locator('#entryForm [name="title"]')).to_have_value('Ikke gemt kladde')
                    expect(page.locator('#entryForm [name="due_date"]')).to_have_value(old_date)
                    page.locator('#closeModal').click();expect(page.locator('#taskList')).to_contain_text('Kun i dag');context.close()
                    # Intentional past-date browsing is not discarded.
                    context,page=fresh();previous_heading=page.locator('#currentDate').inner_text()
                    page.locator('#prevBtn').click();expect(page.locator('#currentDate')).not_to_have_text(previous_heading)
                    heading=page.locator('#currentDate').inner_text()
                    page.clock.run_for(2000);expect(page.locator('#currentDate')).to_have_text(heading)
                    page.locator('#todayBtn').click();expect(page.locator('#taskList')).to_contain_text('Kun i dag');context.close()
                    # Offline at midnight recovers without a reload.
                    context,page=fresh();expect(page.locator('#taskList')).to_contain_text('Kun i gaar');context.set_offline(True)
                    page.clock.run_for(2000);expect(page.locator('#connection')).to_contain_text('Offline')
                    context.set_offline(False);page.evaluate("window.dispatchEvent(new Event('online'))")
                    expect(page.locator('#taskList')).to_contain_text('Kun i dag');context.close()
                    # Tablet uses Copenhagen date even on a device in another timezone.
                    context,display=fresh(tablet=True,zone='America/Los_Angeles')
                    expect(display.locator('#tabletTasks')).to_contain_text('Kun i gaar')
                    display.clock.run_for(2000)
                    expect(display.locator('#tabletTasks')).to_contain_text('Kun i dag');expect(display.locator('#tabletTasks')).not_to_contain_text('Kun i gaar')
                    expect(display.locator('#tabletTasks .task-row').filter(has_text='Faelles opvask')).to_contain_text('Noah')
                    row=display.locator('#tabletTasks .task-row').filter(has_text='Kun i dag')
                    with display.expect_response(lambda r:'/toggle' in r.url) as tick:
                        row.locator('input').check()
                    assert tick.value.request.post_data_json['day']==today.isoformat()
                    assert tick.value.status==200
                    if args.artifacts:
                        display.screenshot(path=str(args.artifacts/'midnight-tablet.png'),full_page=True)
                        display.set_viewport_size({'width':390,'height':844})
                        assert display.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
                        display.screenshot(path=str(args.artifacts/'midnight-phone.png'),full_page=True)
                    context.close()
                    # Simulate resumed page with a clock jump (missed timer, BFCache-style wake).
                    context,page=fresh();expect(page.locator('#taskList')).to_contain_text('Kun i gaar')
                    page.clock.set_system_time(midnight+timedelta(hours=7))
                    page.evaluate("window.dispatchEvent(new Event('pageshow'))")
                    expect(page.locator('#taskList')).to_contain_text('Kun i dag');context.close()
                    assert len(api('/api/tasks/all'))==count, 'Rollover must not create task copies'
                    assert not errors, errors
                    browser.close()
                print('PASS: Danish midnight, dashboard/tasks/tablet, rotation, drafts, browsing, offline recovery, wake, another device timezone and no task copies. Fictional data only.')
            finally:
                server.terminate()
                try:server.wait(timeout=5)
                except subprocess.TimeoutExpired:server.kill();server.wait()

if __name__=='__main__':
    main()
