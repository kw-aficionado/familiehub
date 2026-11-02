'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const test = require('node:test');
const rules = require('../web/saver-schedule.js');
const source = fs.readFileSync(path.join(__dirname, '../web/app.js'), 'utf8');
const html = fs.readFileSync(path.join(__dirname, '../web/index.html'), 'utf8');
const defaults = () => rules.normalize();
const allowed = (iso, prefs = defaults()) => rules.allowed(new Date(iso), prefs);

test('new and existing devices default to the independent 20:00-07:00 window', () => {
  assert.deepEqual(defaults(), {theme:'auto', nightFrom:'21:00', nightTo:'07:00', saverDelay:10,
    saverMode:'scheduled', saverFrom:'20:00', saverTo:'07:00'});
  const old = rules.normalize({theme:'light', nightFrom:'22:30', nightTo:'08:00', saverDelay:0});
  assert.equal(old.theme, 'light'); assert.equal(old.nightFrom, '22:30');
  assert.equal(old.nightTo, '08:00'); assert.equal(old.saverDelay, 0);
  assert.equal(old.saverFrom, '20:00'); assert.equal(old.saverTo, '07:00');
});

test('malformed preferences cannot enable unrestricted or immediate activation', () => {
  for (const saved of [null, [], 10, 'bad', {saverMode:'anything', saverDelay:null, saverFrom:'25:00', saverTo:'bad'}]) {
    assert.deepEqual(rules.normalize(saved), defaults());
  }
  assert.equal(rules.normalize({saverDelay:'20'}).saverDelay, 20);
  assert.equal(rules.normalize({saverDelay:true}).saverDelay, 10);
  const equal = rules.normalize({saverFrom:'12:00', saverTo:'12:00'});
  assert.equal(equal.saverFrom, '20:00'); assert.equal(equal.saverTo, '07:00');
});

test('default overnight boundaries use Copenhagen time, including midnight', () => {
  assert.equal(allowed('2026-11-02T18:59:59Z'), false); // 19:59 CET
  assert.equal(allowed('2026-11-02T19:00:00Z'), true);
  assert.equal(allowed('2026-11-02T23:00:00Z'), true);
  assert.equal(allowed('2026-11-03T05:59:59Z'), true);
  assert.equal(allowed('2026-11-03T06:00:00Z'), false);
  assert.equal(allowed('2026-11-03T11:00:00Z'), false);
});

test('summer and DST transition nights follow Denmark rather than the device timezone', () => {
  assert.equal(allowed('2026-07-01T17:59:59Z'), false);
  assert.equal(allowed('2026-07-01T18:00:00Z'), true); // 20:00 CEST
  assert.equal(allowed('2026-07-02T05:00:00Z'), false); // 07:00 CEST
  assert.equal(allowed('2026-03-29T00:30:00Z'), true);
  assert.equal(allowed('2026-03-29T01:30:00Z'), true);
  assert.equal(allowed('2026-10-25T00:30:00Z'), true);
  assert.equal(allowed('2026-10-25T01:30:00Z'), true);
});

test('a custom same-day window includes the start and excludes the end', () => {
  const prefs = {...defaults(), saverFrom:'09:00', saverTo:'17:00'};
  assert.equal(allowed('2026-11-02T07:59:59Z', prefs), false);
  assert.equal(allowed('2026-11-02T08:00:00Z', prefs), true);
  assert.equal(allowed('2026-11-02T15:59:59Z', prefs), true);
  assert.equal(allowed('2026-11-02T16:00:00Z', prefs), false);
});

test('all-day is explicit; never, invalid dates and invalid windows stay disabled', () => {
  assert.equal(allowed('2026-11-02T11:00:00Z', {...defaults(), saverMode:'always'}), true);
  assert.equal(allowed('2026-11-02T21:00:00Z', {...defaults(), saverDelay:0}), false);
  assert.equal(allowed('2026-11-02T11:00:00Z', {...defaults(), saverMode:'always', saverDelay:0}), false);
  assert.equal(rules.allowed(new Date(NaN), defaults()), false);
  assert.equal(rules.allowed(null, defaults()), false);
  assert.equal(allowed('2026-11-02T20:00:00Z', {...defaults(), saverFrom:'20:00', saverTo:'20:00'}), false);
});

function boot({iso = '2026-11-02T18:45:00Z', saved = {}, storageError = false} = {}) {
  let current = Date.parse(iso), counter = 0;
  const timers = new Map(), nodes = new Map(), documentListeners = {};
  let stored = typeof saved === 'string' ? saved : JSON.stringify(saved);
  const node = id => {
    if (!nodes.has(id)) {
      const classes = new Set(['login','modal','screensaver'].includes(id) ? ['hidden'] : []);
      nodes.set(id, {value:'', textContent:'', attributes:{}, listeners:{},
        classList:{contains: x => classes.has(x), add: x => classes.add(x), remove: x => classes.delete(x),
          toggle(x, on) {if (on) classes.add(x); else classes.delete(x);}},
        setAttribute(name, value) {this.attributes[name] = value;},
        addEventListener(name, fn) {this.listeners[name] = fn;}});
    }
    return nodes.get(id);
  };
  class FakeDate extends Date {
    constructor(...args) {super(...(args.length ? args : [current]));}
    static now() {return current;}
  }
  const document = {hidden:false, documentElement:node('html'), querySelector: () => node('themeMeta'),
    addEventListener(name, fn) {documentListeners[name] = fn;}};
  const state = {csrf:'test-session', todayEvents:[]};
  const context = vm.createContext({Date:FakeDate, window:{FamilyHubSaverSchedule:rules}, document,
    localStorage:{getItem() {if(storageError) throw Error('blocked'); return stored;},
      setItem(key, value) {if(storageError) throw Error('blocked'); stored = value;}},
    S:state, $:node, show:id => node(id).classList.remove('hidden'), hide:id => node(id).classList.add('hidden'),
    formatDate:(date, options) => new Intl.DateTimeFormat('da-DK', options).format(date), clock:() => '20:00',
    setTimeout(fn, delay) {const id = ++counter; timers.set(id, {fn, due:current + delay}); return id;},
    clearTimeout(id) {timers.delete(id);}, setInterval() {return 0;}});
  const begin = source.indexOf('  // Each display has independent light/dark and idle-screen preferences.');
  const end = source.indexOf('  function accountIsAdmin()', begin);
  assert.ok(begin >= 0 && end > begin, 'exercise the real application controller');
  vm.runInContext(source.slice(begin, end) + '\nglobalThis.controller={restoreDisplaySettings,saveDisplaySettings,scheduleSaver,activity,showSaver,hideSaver,visible:()=>saverVisible,prefs:()=>displayPrefs};', context);
  const app = context.controller;
  app.restoreDisplaySettings();
  function advance(iso) {
    const until = Date.parse(iso);
    assert.ok(until >= current, 'time advances forward');
    let iterations = 0;
    for (;;) {
      const next = [...timers.entries()].filter(([,timer]) => timer.due <= until).sort((a,b) => a[1].due - b[1].due)[0];
      if (!next) break;
      assert.ok(++iterations < 20000, 'no busy scheduling loop');
      const [id, timer] = next;
      current = timer.due; timers.delete(id); timer.fn();
    }
    current = until;
  }
  function change(values) {
    for (const [key,value] of Object.entries(values)) node(key).value = String(value);
    app.saveDisplaySettings();
  }
  return {app,node,advance,change,document,documentListeners,state,timers,stored:() => JSON.parse(stored)};
}

test('an already idle screen stays awake before 20:00 and enters exactly at 20:00', () => {
  const t = boot();
  t.advance('2026-11-02T18:59:59Z'); assert.equal(t.app.visible(), false);
  t.advance('2026-11-02T19:00:00Z'); assert.equal(t.app.visible(), true);
  assert.equal(t.node('screensaver').classList.contains('hidden'), false);
});

test('opening at 20:00 still requires the selected idle delay; minute checks do not reset it', () => {
  const t = boot({iso:'2026-11-02T19:00:00Z'});
  t.advance('2026-11-02T19:09:59Z'); assert.equal(t.app.visible(), false);
  t.advance('2026-11-02T19:10:00Z'); assert.equal(t.app.visible(), true);
});

test('user activity restarts the idle clock and dismisses an active screensaver', () => {
  const t = boot({iso:'2026-11-02T19:00:00Z'});
  t.advance('2026-11-02T19:08:00Z'); t.app.activity();
  t.advance('2026-11-02T19:17:59Z'); assert.equal(t.app.visible(), false);
  t.advance('2026-11-02T19:18:00Z'); assert.equal(t.app.visible(), true);
  t.node('screensaver').listeners.click(); assert.equal(t.app.visible(), false);
  t.advance('2026-11-02T19:27:59Z'); assert.equal(t.app.visible(), false);
});

test('the automatic screen dismisses itself at 07:00 with no user interaction', () => {
  const t = boot({iso:'2026-11-03T05:40:00Z'});
  t.advance('2026-11-03T05:59:59Z'); assert.equal(t.app.visible(), true);
  t.advance('2026-11-03T06:00:00Z'); assert.equal(t.app.visible(), false);
  t.advance('2026-11-03T08:00:00Z'); assert.equal(t.app.visible(), false);
});

test('custom settings save per device without changing the independent theme settings', () => {
  const t = boot({iso:'2026-11-02T19:00:00Z',saved:{theme:'dark',nightFrom:'23:00',nightTo:'08:30',saverDelay:5}});
  t.change({saverFrom:'22:00',saverTo:'06:30'});
  assert.equal(t.stored().saverFrom, '22:00'); assert.equal(t.stored().nightFrom, '23:00');
  assert.equal(t.stored().saverDelay, 5); assert.equal(t.stored().theme, 'dark');
  t.advance('2026-11-02T20:59:59Z'); assert.equal(t.app.visible(), false);
  t.advance('2026-11-02T21:00:00Z'); assert.equal(t.app.visible(), true);
  const next = boot({iso:'2026-11-02T21:30:00Z',saved:t.stored()});
  assert.equal(next.node('saverFrom').value, '22:00'); assert.equal(next.node('saverTo').value, '06:30');
});

test('equal/invalid window inputs display an error and retain the prior preferences', () => {
  const t = boot();
  t.change({saverFrom:'20:00',saverTo:'20:00'});
  assert.equal(t.app.prefs().saverTo, '07:00');
  assert.equal(t.node('saverScheduleStatus').classList.contains('error'), true);
  t.change({saverFrom:'',saverTo:'07:00'});
  assert.equal(t.app.prefs().saverFrom, '20:00');
});

test('the all-day option and Aldrig work, including legacy disabled displays', () => {
  const t = boot({iso:'2026-11-02T11:00:00Z'});
  t.change({saverMode:'always',saverDelay:5});
  assert.equal(t.node('saverFromField').classList.contains('hidden'), true);
  t.advance('2026-11-02T11:05:00Z'); assert.equal(t.app.visible(), true);
  t.change({saverDelay:0}); assert.equal(t.app.visible(), false); assert.equal(t.timers.size, 0);
  t.advance('2026-11-03T00:00:00Z'); assert.equal(t.app.visible(), false);
  const old = boot({saved:{saverDelay:0}});
  old.advance('2026-11-03T00:00:00Z'); assert.equal(old.app.visible(), false);
});

test('manual preview works during the day even with automatic activation disabled', () => {
  const t = boot({iso:'2026-11-02T11:00:00Z',saved:{saverDelay:0}});
  t.node('saverPreviewBtn').listeners.click(); assert.equal(t.app.visible(), true);
  t.node('screensaver').listeners.click(); assert.equal(t.app.visible(), false);
  assert.equal(t.timers.size, 0);
});

test('hidden tabs cancel pending activation and resume with a fresh idle period', () => {
  const t = boot({iso:'2026-11-02T19:00:00Z'});
  t.advance('2026-11-02T19:08:00Z');
  t.document.hidden = true; t.documentListeners.visibilitychange();
  assert.equal(t.timers.size, 0);
  t.advance('2026-11-02T19:30:00Z'); assert.equal(t.app.visible(), false);
  t.document.hidden = false; t.documentListeners.visibilitychange();
  t.advance('2026-11-02T19:39:59Z'); assert.equal(t.app.visible(), false);
  t.advance('2026-11-02T19:40:00Z'); assert.equal(t.app.visible(), true);
});

test('login and modal screens cannot be covered automatically or via preview', () => {
  const t = boot({iso:'2026-11-02T19:00:00Z'});
  t.node('modal').classList.remove('hidden');
  t.advance('2026-11-02T19:15:00Z'); assert.equal(t.app.visible(), false);
  t.node('saverPreviewBtn').listeners.click(); assert.equal(t.app.visible(), false);
  t.node('modal').classList.add('hidden'); t.app.activity();
  t.advance('2026-11-02T19:25:00Z'); assert.equal(t.app.visible(), true);
  t.state.csrf = ''; t.app.scheduleSaver();
  assert.equal(t.app.visible(), false); assert.equal(t.timers.size, 0);
  t.node('saverPreviewBtn').listeners.click(); assert.equal(t.app.visible(), false);
});

test('storage errors use safe defaults and report when preferences are only temporary', () => {
  const corrupt = boot({saved:'not JSON'});
  assert.equal(corrupt.app.prefs().saverFrom, '20:00');
  const blocked = boot({storageError:true});
  blocked.change({saverFrom:'21:30'});
  assert.equal(blocked.app.prefs().saverFrom, '21:30');
  assert.equal(blocked.node('saverScheduleStatus').classList.contains('error'), true);
});

test('HTML includes all settings and loads the scheduler before the main application', () => {
  for (const id of ['saverMode','saverFrom','saverTo','saverFromField','saverToField','saverScheduleStatus']) assert.ok(html.includes('id="'+id+'"'), id);
  assert.ok(html.indexOf('src="/saver-schedule.js') >= 0);
  assert.ok(html.indexOf('src="/saver-schedule.js') < html.indexOf('src="/app.js'));
});
