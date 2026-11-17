#!/usr/bin/env python3
"""Exercise tablet pairing against a NEW loopback server and fictional data only.

No existing server URL, database or credentials can be supplied. Optional review
artifacts never contain pairing codes, passwords or live family data.
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
    with tempfile.TemporaryDirectory(prefix='fh-tablet-test-') as temp:
        work = Path(temp)
        subprocess.run(['go', 'build', '-ldflags', '-X main.buildCommit=demo', '-o', str(work/'app'), '.'], cwd=ROOT, check=True)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1',0)); port=sock.getsockname()[1]
        origin=f'http://127.0.0.1:{port}'
        password=secrets.token_urlsafe(32)
        env=dict(os.environ,APP_URL=origin,APP_PASSWORD=password,DATA_DIR=str(work/'data'),HA_URL='',HA_TOKEN='',HA_CALENDARS='')
        with (work/'server.log').open('w+') as log:
            server=subprocess.Popen([str(work/'app'),'-listen',f'127.0.0.1:{port}'],cwd=ROOT,env=env,stdout=log,stderr=log)
            try:
                for _ in range(150):
                    if server.poll() is not None: raise RuntimeError('Isolated server failed')
                    try:
                        urllib.request.urlopen(origin+'/healthz',timeout=1).close();break
                    except OSError:time.sleep(.1)
                else:raise RuntimeError('Isolated server startup timeout')
                client=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
                csrf=''
                def api(path,body=None,method=None):
                    assert path.startswith('/api/')
                    request=urllib.request.Request(origin+path,data=None if body is None else json.dumps(body).encode(),method=method or ('GET' if body is None else 'POST'),headers={'Content-Type':'application/json','X-CSRF-Token':csrf})
                    with client.open(request,timeout=10) as r:return json.load(r)
                csrf=api('/api/login',{'password':password})['csrf']
                people=[api('/api/people',{'name':name,'color':color,'position':i}) for i,(name,color) in enumerate([('Maja','#8158b1'),('Noah','#2086b5')])]
                now=datetime.now(ZoneInfo('Europe/Copenhagen'));today=now.date().isoformat()
                for i,title in enumerate(['D\u00e6kke bord','Pakke skoletaske','Vande planter','Tjekke lektier','T\u00f8mme opvaskemaskine','Rydde op efter maden']):
                    api('/api/tasks',{'title':title,'assigned_to':people[i%2]['id'],'due_date':today,'repeat_rule':'daily'})
                start=now.replace(hour=16,minute=0,second=0,microsecond=0)
                api('/api/events',{'title':'Fodboldtr\u00e6ning','person_id':people[1]['id'],'kind':'aftale','start_at':start.isoformat(),'end_at':(start+timedelta(hours=1)).isoformat(),'notes':'Private notes must not appear on tablet'})
                with sync_playwright() as pw:
                    launch={'executable_path':os.environ['FH_TEST_BROWSER']} if os.environ.get('FH_TEST_BROWSER') else {}
                    browser=pw.chromium.launch(**launch)
                    admin=browser.new_page(viewport={'width':1280,'height':1000},locale='da-DK',timezone_id='Europe/Copenhagen')
                    errors=[];admin.on('pageerror',lambda e:errors.append(str(e)))
                    admin.goto(origin);admin.locator('#password').fill(password);admin.locator('#loginForm button').click()
                    expect(admin.locator('#app')).to_be_visible()
                    admin.locator('[data-page="indstillinger"]').click()
                    expect(admin.locator('#tabletsPanel')).to_be_visible()
                    admin.locator('#tabletAddBtn').click();admin.locator('#tabletEditor [name="name"]').fill('K\u00f8kkenets iPad')
                    admin.locator('#tabletEditor [name="can_complete"]').check()
                    with admin.expect_response(lambda r:r.url.endswith('/api/tablets') and r.request.method=='POST') as created:
                        admin.locator('#tabletEditor button[type="submit"]').click()
                    assert created.value.status==201
                    device=created.value.json();code=device['pair_code']
                    expect(admin.locator('#tabletPairCode')).to_have_value(code)
                    admin.locator('#tabletCodeDone').click()
                    display=browser.new_page(viewport={'width':1024,'height':900},locale='da-DK',timezone_id='America/New_York')
                    display.on('pageerror',lambda e:errors.append(str(e)))
                    display.goto(origin+'/tablet');expect(display.locator('#tabletPairForm')).to_be_visible()
                    display.locator('#tabletCode').fill(code);display.locator('#tabletPairForm button').click()
                    expect(display.locator('#tabletApp')).to_be_visible()
                    expect(display.locator('#tabletName')).to_have_text('K\u00f8kkenets iPad')
                    expect(display.locator('#tabletTasks input')).to_have_count(6)
                    assert not display.locator('#tabletTasks input').first.is_disabled()
                    expect(display.locator('#tabletEvents')).to_contain_text('Fodboldtr\u00e6ning')
                    assert 'Private notes' not in display.locator('body').inner_text()
                    with display.expect_response(lambda r:'/api/tablet/tasks/' in r.url) as completed:
                        display.locator('#tabletTasks input').first.check()
                    assert completed.value.status==200
                    expect(display.locator('#tabletTasks input:checked')).to_have_count(1)
                    assert display.request.get(origin+'/api/accounts').status==401
                    assert display.request.post(origin+'/api/tasks',data={}).status==401
                    assert display.request.get(origin+'/api/tablet/session?token=anything').status==200 # cookie, never URL token
                    display.locator('#tabletFilters button').nth(1).click();expect(display.locator('#tabletTasks input')).to_have_count(3)
                    display.locator('#tabletFilters button').first.click()
                    display.locator('#tabletNext').click();expect(display.locator('#tabletTasks input:not(:disabled)')).to_have_count(0)
                    display.locator('#tabletToday').click();expect(display.locator('#tabletTasks input:not(:disabled)')).to_have_count(6)
                    if args.artifacts:
                        display.screenshot(path=str(args.artifacts/'tablet-day.png'),full_page=True)
                        display.set_viewport_size({'width':390,'height':844})
                        assert display.evaluate('document.documentElement.scrollWidth<=innerWidth+1'), 'Phone has horizontal overflow'
                        display.screenshot(path=str(args.artifacts/'tablet-phone.png'),full_page=True)
                    display.reload();expect(display.locator('#tabletApp')).to_be_visible() # persistent cookie
                    admin.locator('#tabletRefreshBtn').click()
                    card=admin.locator('.tablet-device-card').filter(has=admin.locator('h3',has_text='K\u00f8kkenets iPad'))
                    expect(card).to_contain_text('Forbundet')
                    if args.artifacts:admin.locator('#tabletsPanel').screenshot(path=str(args.artifacts/'tablet-settings.png'))
                    card.get_by_role('button',name='Redig\u00e9r',exact=True).click();admin.locator('#tabletEditor [name="can_complete"]').uncheck()
                    admin.locator('#tabletEditor button[type="submit"]').click();expect(admin.locator('#modal')).not_to_be_visible()
                    display.reload();expect(display.locator('#tabletAccess')).to_contain_text('Kun visning')
                    expect(display.locator('#tabletTasks input:not(:disabled)')).to_have_count(0)
                    # Server enforces updated scope even for a handcrafted request.
                    session=display.request.get(origin+'/api/tablet/session').json();tid=api('/api/tasks/all')[0]['id']
                    rejected=display.request.post(origin+'/api/tablet/tasks/'+tid+'/toggle',data={'day':today,'done':True},headers={'X-CSRF-Token':session['csrf']})
                    assert rejected.status==403
                    # Separate device remains connected when kitchen display is revoked.
                    other=api('/api/tablets',{'name':'Stue','can_complete':False})
                    other_context=browser.new_context()
                    assert other_context.request.post(origin+'/api/tablet/pair',data={'code':other['pair_code']},headers={'X-FH-Pairing':'1'}).status==200
                    admin.once('dialog',lambda dialog:dialog.accept())
                    card.get_by_role('button',name='Fjern adgang').click();expect(card).to_contain_text('Sp\u00e6rret')
                    display.reload();expect(display.locator('#tabletPairForm')).to_be_visible();expect(display.locator('#tabletTasks input')).to_have_count(0)
                    assert other_context.request.get(origin+'/api/tablet/session').status==200
                    assert len(api('/api/tablets')['devices'])==2
                    # Account rate-limit UI shows remaining time and disables repeat submits.
                    blocked=browser.new_page()
                    for _ in range(8):assert blocked.request.post(origin+'/api/login',data={'username':'blocked-user','password':'fictional-wrong-password'}).status==401
                    blocked.goto(origin);blocked.locator('#username').fill('blocked-user');blocked.locator('#password').fill('fictional-wrong-password');blocked.locator('#loginForm button').click()
                    expect(blocked.locator('#loginWait')).to_contain_text('Vent ');expect(blocked.locator('#loginForm button')).to_be_disabled()
                    # Ordinary login remains usable for another account on the SAME IP.
                    assert api('/api/session')['authenticated'] is True
                    assert not errors, 'JavaScript errors: '+str(errors)
                    browser.close()
                print('Tablet browser checks passed: pairing, full daily list, scoped completion, renewal, revocation, phone layout and login countdown. Fictional data only.')
            finally:
                server.terminate()
                try:server.wait(timeout=10)
                except subprocess.TimeoutExpired:server.kill();server.wait()

if __name__=='__main__':main()
