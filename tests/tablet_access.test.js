'use strict';
const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const root=path.join(__dirname,'..');
const read=p=>fs.readFileSync(path.join(root,p),'utf8');
const wait=require('../web/login-wait.js');

test('login countdown rounds up and never returns negative or unbounded retry values',()=>{
 assert.equal(wait.seconds('60'),60);assert.equal(wait.seconds(1.1),2);
 for(const value of [null,NaN,-1,'oops',Infinity])assert.equal(wait.seconds(value),0);
 assert.equal(wait.seconds(999999),86400);
 assert.equal(wait.remaining(1500,1000),1);assert.equal(wait.remaining(1000,1500),0);
});
test('actual login countdown disables repeated submission without extending the deadline',()=>{
 let now=1000,interval;class Clock extends Date{static now(){return now}}
 const ctx=vm.createContext({Date:Clock,setInterval:fn=>(interval=fn,1),clearInterval(){interval=null}});
 vm.runInContext(read('web/login-wait.js'),ctx);
 const button={disabled:false},status={textContent:''},w=ctx.FamilyHubLoginWait.install(button,status);
 w.start(300);assert.equal(button.disabled,true);assert.match(status.textContent,/5:00/);
 now+=1000;interval();assert.match(status.textContent,/4:59/);
 w.busy(true);now+=299000;interval();assert.equal(button.disabled,true);w.busy(false);assert.equal(button.disabled,false);assert.equal(status.textContent,'');
});
test('tablet page has separate startup, manifest and no planning or admin controls',()=>{
 const html=read('web/tablet.html'),js=read('web/tablet.js');
 for(const file of ['/tablet.js','/login-wait.js','/saver-schedule.js'])assert.ok(html.includes(file));
 for(const id of ['plannerOpenBtn','tasksAddBtn','settingsPage','addEventBtn','aiKey'])assert.ok(!html.includes('id="'+id+'"'));
 assert.match(js,/\/api\/tablet\//);
 assert.ok(!js.includes('localStorage'));assert.ok(!js.includes('sessionStorage'));
 const manifest=JSON.parse(read('web/tablet.webmanifest'));assert.equal(manifest.start_url,'/tablet');
 assert.match(js,/state.day!==state.today/);assert.match(js,/!d.can_complete/);
 assert.match(js,/window.FamilyHubSaverSchedule.allowed/);
});
test('admin management is inside Settings and uses existing security-aware API',()=>{
 const html=read('web/index.html'),js=read('web/app.js'),admin=read('web/tablet-admin.js');
 assert.ok(html.indexOf('id="tabletsPanel"')>html.indexOf('id="settingsPage"'));
 assert.match(js,/tabletTools\.refresh\(\)/);
 assert.match(admin,/c\.isAdmin\(\)/);assert.match(admin,/\/api\/tablets\//);
 assert.ok(!admin.includes('?token='));assert.ok(!admin.includes('localStorage'));
 assert.match(js,/e.status===429/);assert.match(html,/id="loginWait"/);
});
