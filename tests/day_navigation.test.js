'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const test = require('node:test');

const source = fs.readFileSync(path.join(__dirname, '../web/app.js'), 'utf8');
const html = fs.readFileSync(path.join(__dirname, '../web/index.html'), 'utf8');
const today = '2026-11-02';
const tomorrow = '2026-11-03';

// Exercise the actual frontend functions against a small DOM, without adding
// production dependencies or starting the unrelated integrations and timers.
class Element {
  constructor(tag = 'div') {
    this.tagName = tag;
    this.attributes = {};
    this.className = '';
    this.children = [];
    this.listeners = {};
    this.style = {setProperty() {}};
    this._text = '';
    this.classList = {
      contains: name => this.className.split(/\s+/).includes(name),
      add: name => this.classList.toggle(name, true),
      remove: name => this.classList.toggle(name, false),
      toggle: (name, force) => {
        const names = new Set(this.className.split(/\s+/).filter(Boolean));
        const add = force === undefined ? !names.has(name) : force;
        if (add) names.add(name); else names.delete(name);
        this.className = [...names].join(' ');
        return add;
      }
    };
  }
  setAttribute(name, value) {
    this.attributes[name] = String(value);
    if (name === 'class') this.className = String(value);
  }
  getAttribute(name) { return name === 'class' ? this.className : this.attributes[name]; }
  removeAttribute(name) { delete this.attributes[name]; }
  appendChild(child) { this.children.push(child); return child; }
  replaceChildren(...children) { this.children = children; this._text = ''; }
  addEventListener(name, listener) { this.listeners[name] = listener; }
  get textContent() { return this._text + this.children.map(child => child.textContent).join(''); }
  set textContent(text) { this._text = String(text); this.children = []; }
}

function boot() {
  const nodes = new Map();
  const node = id => {
    if (!nodes.has(id)) nodes.set(id, new Element());
    return nodes.get(id);
  };
  const withAttribute = (name, values) => values.map(value => {
    const element = new Element('button');
    element.setAttribute(name, value);
    return element;
  });
  const selectors = {
    '[data-period]': withAttribute('data-period', ['today', 'week', 'all']),
    '[data-status]': withAttribute('data-status', ['open', 'all', 'done']),
    '.main-tab': withAttribute('data-page', ['kalender', 'opgaver', 'indstillinger'])
  };
  const requests = [];
  const data = {entries: [], tasks: []};
  const context = vm.createContext({
    window: {FamilyHubPlannerDates: {today: () => today}},
    location: {hash: '#kalender'},
    document: {
      getElementById: node,
      createElement: tag => new Element(tag),
      createTextNode: text => { const element = new Element('#text'); element.textContent = text; return element; },
      querySelectorAll: selector => selectors[selector] || []
    },
    URLSearchParams,
    fetch: async url => {
      requests.push(url);
      return {ok: true, status: 200, json: async () => url === '/api/tasks/all' ? data.tasks : {entries: data.entries}};
    },
    alert() {},
    console
  });
  const end = source.indexOf('  var plannerDraft=null;');
  assert.ok(end > 0, 'frontend navigation section exists');
  vm.runInContext(source.slice(0, end) + '\nglobalThis.app={S,range,renderTasks,renderTasksPage,updateCalendarViewButtons,openDayTasks,selectTaskPeriod,loadTasksPage,syncPage,dayString};\n})();', context);
  return {app: context.app, context, node, data, requests, selectors};
}

function task(id, day, options = {}) {
  return {id, title: 'Opgave ' + id, day, due_date: day, assigned_to: '', repeat_rule: 'daily', done: false, ...options};
}
function cards(node) { return node.children.filter(child => child.tagName === 'article'); }


test('home and tasks start on the family day with all daily statuses visible', () => {
  const {app} = boot();
  assert.equal(app.S.view, 'day');
  assert.equal(app.S.taskPeriod, 'today');
  assert.equal(app.S.taskStatus, 'all');
  assert.equal(app.dayString(app.S.focus), today);
  assert.equal(app.dayString(app.S.taskFocus), today);
  const range = app.range();
  assert.equal((range.end - range.start) / 86400000, 1);
  assert.match(html, /id="dayBtn" class="segment active" aria-pressed="true"/);
  assert.match(html, /id="weekBtn" class="segment" aria-pressed="false"/);
  assert.match(html, /class="task-period active" data-period="today"/);
  assert.match(html, /id="showDayTasksBtn"/);
  assert.match(source, /\$\('showDayTasksBtn'\)\.addEventListener\('click',openDayTasks\)/);
});

test('manual week selection remains available and selected button state is correct', () => {
  const {app, node} = boot();
  app.updateCalendarViewButtons();
  assert.equal(node('dayBtn').getAttribute('aria-pressed'), 'true');
  app.S.view = 'week';
  app.updateCalendarViewButtons();
  assert.equal(node('dayBtn').classList.contains('active'), false);
  assert.equal(node('weekBtn').classList.contains('active'), true);
  assert.equal(node('weekBtn').getAttribute('aria-pressed'), 'true');
  assert.equal((app.range().end - app.range().start) / 86400000, 7);
});

test('home preview reports hidden tasks and the full daily list has no four-task cap', () => {
  const {app, node} = boot();
  const entries = Array.from({length: 8}, (_, i) => task(String(i), today, {done: i === 2}));
  app.S.tasks = entries;
  app.renderTasks();
  assert.equal(node('taskList').children.length, 4);
  assert.equal(node('taskPreviewCount').textContent, 'Viser 4 af 8 opgaver.');
  assert.equal(node('showDayTasksBtn').textContent, 'Se alle dagens opgaver');
  app.S.weekEntries = entries.concat(task('tomorrow', tomorrow));
  app.renderTasksPage();
  assert.equal(cards(node('tasksPageList')).length, 8);
  assert.equal(node('tasksCount').textContent, '8 opgaver');
  assert.ok(cards(node('tasksPageList')).some(card => card.children.some(child => child.tagName === 'input' && child.checked)));
  assert.ok(!node('tasksPageList').textContent.includes('tomorrow'));
});

test('home shortcut follows the shown day/person and clears stale status and period', () => {
  const {app, context} = boot();
  app.S.focus = new Date(tomorrow + 'T12:00:00');
  app.S.filter = 'person-selected-on-home';
  app.S.taskPeriod = 'all';
  app.S.taskStatus = 'done';
  app.S.taskFilter = 'old-unrelated-person';
  app.openDayTasks();
  assert.equal(context.location.hash, '#opgaver');
  assert.equal(app.S.taskPeriod, 'today');
  assert.equal(app.dayString(app.S.taskFocus), tomorrow);
  assert.equal(app.S.taskStatus, 'all');
  assert.equal(app.S.taskFilter, app.S.filter);
});

test('Dagens opgaver resets a previously browsed future week to today', async () => {
  const {app, data, requests, node} = boot();
  app.S.page = 'opgaver';
  app.S.taskFocus = new Date('2026-12-21T12:00:00');
  app.S.taskPeriod = 'week';
  app.S.taskStatus = 'done';
  data.entries = [task('today', today), task('other', tomorrow)];
  await app.selectTaskPeriod('today');
  assert.equal(app.dayString(app.S.taskFocus), today);
  assert.equal(app.S.taskStatus, 'all');
  assert.deepEqual(requests, ['/api/tasks/week?start=2026-11-02']);
  assert.equal(cards(node('tasksPageList')).length, 1);
  assert.match(node('tasksPeriodTitle').textContent, /^Dagens opgaver/);
});

test('Alle opgaver is still a separate catalogue and is not fetched for day views', async () => {
  const {app, data, requests, node} = boot();
  app.S.page = 'opgaver';
  data.tasks = [task('past', '2026-10-26'), task('future', '2026-12-01')];
  await app.selectTaskPeriod('all');
  assert.deepEqual(requests, ['/api/tasks/all']);
  assert.equal(node('tasksPeriodTitle').textContent, 'Alle opgaver');
  assert.equal(cards(node('tasksPageList')).length, 2);
  assert.equal(node('taskStatusFilters').classList.contains('hidden'), true);
});

test('whole-week chores appear once per day, and once in the weekly summary', () => {
  const {app, node} = boot();
  const weekly = ['02', '03', '04', '05', '06', '07', '08'].map(day => task('weekly', '2026-11-' + day, {repeat_rule: 'weekly_window'}));
  app.S.weekEntries = weekly.concat(task('daily', today), task('daily', tomorrow));
  app.renderTasksPage();
  assert.equal(cards(node('tasksPageList')).length, 2);
  app.S.taskPeriod = 'week';
  app.renderTasksPage();
  assert.equal(cards(node('tasksPageList')).length, 3);
});

test('daily status and person filters still work after showing the complete checklist', () => {
  const {app, node} = boot();
  app.S.weekEntries = [task('a', today, {assigned_to:'a'}), task('b', today, {assigned_to:'b',done:true}), task('c', tomorrow)];
  app.S.taskStatus = 'done';
  app.renderTasksPage();
  assert.equal(cards(node('tasksPageList')).length, 1);
  app.S.taskStatus = 'all';
  app.S.taskFilter = 'a';
  app.renderTasksPage();
  assert.equal(cards(node('tasksPageList')).length, 1);
  assert.ok(node('tasksPageList').textContent.includes('Opgave a'));
});

test('a previous day response cannot overwrite a newly requested day', async () => {
  const {app, context, node} = boot();
  app.S.page = 'opgaver';
  const pending = [];
  context.fetch = () => new Promise(resolve => pending.push(resolve));
  const first = app.loadTasksPage();
  app.S.taskFocus = new Date(tomorrow + 'T12:00:00');
  const second = app.loadTasksPage();
  pending[1]({ok:true,status:200,json:async () => ({entries:[task('new',tomorrow)]})});
  await second;
  pending[0]({ok:true,status:200,json:async () => ({entries:[task('stale',today)]})});
  await first;
  assert.equal(app.S.weekEntries[0].id, 'new');
  assert.ok(node('tasksPageList').textContent.includes('Opgave new'));
  assert.ok(!node('tasksPageList').textContent.includes('stale'));
});

test('other selected calendar days are clearly labelled and an empty day is empty', () => {
  const {app, node} = boot();
  app.S.focus = new Date(tomorrow + 'T12:00:00');
  app.renderTasks();
  assert.equal(node('showDayTasksBtn').textContent, 'Se alle opgaver denne dag');
  app.S.weekEntries = [task('not-today', tomorrow)];
  app.renderTasksPage();
  assert.equal(node('tasksCount').textContent, '0 opgaver');
  assert.equal(cards(node('tasksPageList')).length, 0);
});
