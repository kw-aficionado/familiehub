'use strict';
const assert = require('node:assert/strict');
const test = require('node:test');
const {watch} = require('../web/day-rollover.js');
const dates = require('../web/planner-dates.js');
const fs = require('node:fs');
const path = require('node:path');

function boot(iso) {
  let clock=Date.parse(iso), id=0, accept=true;
  const timers=new Map(), calls=[];
  const emitter=()=>({handlers:{},addEventListener(k,f){(this.handlers[k] ||= new Set()).add(f)},
    removeEventListener(k,f){this.handlers[k]?.delete(f)},emit(k){this.handlers[k]?.forEach(f=>f())}});
  const host=emitter(), doc=Object.assign(emitter(),{hidden:false});
  const controller=watch({host,document:doc,dates,now:()=>new Date(clock),
    setTimeout:(f,ms)=>{timers.set(++id,{f,at:clock+ms});return id},clearTimeout:n=>timers.delete(n),
    onChange:(next,previous)=>{calls.push([next,previous]);return accept}});
  function advance(iso){const until=Date.parse(iso);let count=0;
    for(;;){const pair=[...timers.entries()].sort((a,b)=>a[1].at-b[1].at)[0];if(!pair||pair[1].at>until)break;
      assert.ok(++count<5000);clock=pair[1].at;timers.delete(pair[0]);pair[1].f()}
    clock=until;
  }
  return {host,doc,calls,timers,controller,advance,jump:iso=>{clock=Date.parse(iso)},accept:x=>{accept=x}};
}
for(const [season,before,after] of [
  ['winter','2026-11-22T22:59:59.500Z','2026-11-22T23:00:00Z'],
  ['summer','2026-07-01T21:59:59.500Z','2026-07-01T22:00:00Z'],
  ['year boundary','2026-12-31T22:59:59.500Z','2026-12-31T23:00:00Z'],
  ['leap day','2028-02-28T22:59:59.500Z','2028-02-28T23:00:00Z']]){
  test('one transition at Danish midnight: '+season,()=>{
    const t=boot(before);assert.equal(t.calls.length,0);t.advance(after);
    assert.deepEqual(t.calls,[[dates.today(new Date(after)),dates.today(new Date(before))]]);
    t.controller.check();t.host.emit('focus');t.host.emit('pageshow');t.host.emit('online');
    assert.equal(t.calls.length,1);assert.equal(t.timers.size,1);t.controller.stop();
  });
}
test('DST spring day is 23 hours, not a 24-hour countdown',()=>{
  const t=boot('2026-03-28T23:00:00Z');t.advance('2026-03-29T21:59:59.999Z');assert.equal(t.calls.length,0);
  t.advance('2026-03-29T22:00:00Z');assert.deepEqual(t.calls,[['2026-03-30','2026-03-29']]);
});
test('DST autumn day is 25 hours and the repeated hour does not advance the day',()=>{
  const t=boot('2026-10-24T22:00:00Z');t.advance('2026-10-25T22:59:59.999Z');assert.equal(t.calls.length,0);
  t.advance('2026-10-25T23:00:00Z');assert.deepEqual(t.calls,[['2026-10-26','2026-10-25']]);
});
test('sleep stops work; returning skips directly to the actual date',()=>{
  const t=boot('2026-11-20T18:00:00Z');t.doc.hidden=true;t.doc.emit('visibilitychange');assert.equal(t.timers.size,0);
  t.jump('2026-11-23T06:00:00Z');t.controller.check();assert.equal(t.calls.length,0);
  t.doc.hidden=false;t.doc.emit('visibilitychange');assert.deepEqual(t.calls,[['2026-11-23','2026-11-20']]);
  assert.equal(t.timers.size,1);
});
test('restoration, focus and reconnect recover a delayed timer exactly once',()=>{
  for(const event of ['pageshow','focus','online']){
    const t=boot('2026-11-22T20:00:00Z');t.host.emit('pagehide');assert.equal(t.timers.size,0);
    t.jump('2026-11-23T08:00:00Z');t.host.emit(event);t.host.emit(event);assert.equal(t.calls.length,1);assert.equal(t.timers.size,1);
  }
});
test('a pending editor defers the rollover without losing the previous date',()=>{
  const t=boot('2026-11-22T22:59:59Z');t.accept(false);t.advance('2026-11-22T23:00:00Z');
  assert.deepEqual(t.calls,[['2026-11-23','2026-11-22']]);
  t.jump('2026-11-24T08:00:00Z');t.accept(true);assert.equal(t.controller.check(),true);
  assert.deepEqual(t.calls[1],['2026-11-24','2026-11-22']);assert.equal(t.controller.check(),false);
});
test('clock corrections are detected and stop removes all listeners',()=>{
  const t=boot('2026-11-23T08:00:00Z');t.jump('2026-11-22T20:00:00Z');assert.equal(t.controller.check(),true);
  t.controller.stop();t.jump('2026-11-24T08:00:00Z');t.host.emit('pageshow');t.doc.emit('visibilitychange');
  assert.equal(t.calls.length,1);assert.equal(t.timers.size,0);
});
test('both entrypoints load the shared watcher before the application',()=>{
  for(const [file,app] of [['index.html','app.js'],['tablet.html','tablet.js']]){
    const text=fs.readFileSync(path.join(__dirname,'../web',file),'utf8');
    assert.ok(text.indexOf('/planner-dates.js')<text.indexOf('/day-rollover.js'));
    assert.ok(text.indexOf('/day-rollover.js')<text.indexOf('/'+app));
  }
});
