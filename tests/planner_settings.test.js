'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const test = require('node:test');

const source = fs.readFileSync(path.join(__dirname, '../web/app.js'), 'utf8');
const html = fs.readFileSync(path.join(__dirname, '../web/index.html'), 'utf8');
const readme = fs.readFileSync(path.join(__dirname, '../README.md'), 'utf8');

function section(start, end) {
  const from = source.indexOf(start);
  const to = source.indexOf(end, from);
  assert.ok(from >= 0 && to > from, 'actual frontend section must exist');
  return source.slice(from, to);
}

function boot() {
  const nodes = new Map();
  function node(id) {
    if (!nodes.has(id)) nodes.set(id, {
      textContent: '', hidden: true, listeners: {},
      addEventListener(type, handler) { this.listeners[type] = handler; },
      scrollIntoView() { this.scrolled = true; }
    });
    return nodes.get(id);
  }
  const effects = [];
  const state = {page: 'indstillinger', taskPeriod: 'today', taskFocus: new Date('2026-11-03T12:00:00'), taskFilter: 'old-person', taskStatus: 'open'};
  const context = vm.createContext({
    S: state,
    location: {hash: '#indstillinger'},
    $: node,
    closeModal() { effects.push('close'); },
    show(id) { node(id).hidden = false; },
    syncPage() { effects.push('navigate'); },
    confirm() { return true; },
    api: async (url, method) => {
      effects.push([url, method]);
      return {created: 15};
    },
    refreshSettingsData: async () => { effects.push('refreshArchive'); }
  });
  vm.runInContext(section('  function showSavedPlannerPlan(', '  async function openPlanner('), context);
  vm.runInContext(section("  $('plannerSeedBtn').addEventListener('click',async function(){", '  function openTemplate(t){'), context);
  return {context, state, node, effects};
}

function localDay(date) {
  return [date.getFullYear(), String(date.getMonth() + 1).padStart(2, '0'), String(date.getDate()).padStart(2, '0')].join('-');
}

test('planner appears exactly once in Settings immediately after the task archive', () => {
  const matches = [...html.matchAll(/<section\b[^>]*\bid="plannerPanel"[^>]*>[\s\S]*?<\/section>/g)];
  assert.equal(matches.length, 1);
  const planner = matches[0];
  const settingsAt = html.indexOf('<section id="settingsPage"');
  const tasksAt = html.indexOf('<section id="tasksPage"');
  assert.ok(tasksAt >= 0 && settingsAt > tasksAt && planner.index > settingsAt);
  assert.ok(!html.slice(tasksAt, settingsAt).includes('id="plannerPanel"'));
  const archiveAt = html.indexOf('id="settingsList"', settingsAt);
  assert.match(html.slice(archiveAt, planner.index), /^id="settingsList"[^>]*><\/div>\s*<\/div>\s*$/);
  assert.ok(planner.index < html.indexOf('aria-label="Brugerkonti"', settingsAt));
});

test('planner controls remain unique and the daily AI assistant stays on Tasks', () => {
  for (const id of ['plannerOpenBtn', 'plannerSeedBtn', 'plannerStatus', 'plannerViewPlanLink']) {
    assert.equal(html.split('id="' + id + '"').length - 1, 1, id);
  }
  assert.match(html, /id="plannerViewPlanLink"[^>]*href="#opgaver"/);
  const tasks = html.slice(html.indexOf('<section id="tasksPage"'), html.indexOf('<section id="settingsPage"'));
  assert.match(tasks, /AI-familieassistent/);
  assert.match(tasks, /data-ai-mode="today"/);
  assert.match(tasks, /data-period="today"/);
  assert.match(source, /\$\('plannerOpenBtn'\)\.addEventListener\('click',openPlanner\)/);
  assert.match(readme, /Indstillinger\*\*, og find \*\*AI Opgaveplanlægger\*\* lige under \*\*Opgavearkiv/);
});

test('successful save leaves a visible receipt in Settings and does not force another view', () => {
  const {context, state, node, effects} = boot();
  context.showSavedPlannerPlan({created: 7, start_date: '2026-11-09'}, {start_date: '2026-11-03'});
  assert.equal(context.location.hash, '#indstillinger');
  assert.equal(state.taskPeriod, 'today');
  assert.match(node('plannerStatus').textContent, /7 opgaver fra 2026-11-09/);
  assert.ok(!node('plannerStatus').textContent.includes('nedenfor'));
  assert.equal(node('plannerViewPlanLink').hidden, false);
  assert.equal(node('plannerStatus').scrolled, true);
  assert.deepEqual(effects, ['close']);
});

test('saved-plan link restores the saved start week even after other task navigation', () => {
  const {context, state, node} = boot();
  context.showSavedPlannerPlan({created: 7, start_date: '2026-11-09'}, {start_date: '2026-11-03'});
  state.taskFocus = new Date('2026-12-15T12:00:00');
  state.taskFilter = 'unrelated-person';
  state.taskStatus = 'done';
  node('plannerViewPlanLink').onclick();
  assert.equal(localDay(state.taskFocus), '2026-11-09');
  assert.equal(state.taskPeriod, 'week');
  assert.equal(state.taskFilter, '');
  assert.equal(state.taskStatus, 'all');
});

test('receipt captures the preview date for older responses and can be replaced by a later save', () => {
  const {context, state, node} = boot();
  const draft = {start_date: '2026-11-05'};
  context.showSavedPlannerPlan({created: 2}, draft);
  draft.start_date = '2026-12-01';
  node('plannerViewPlanLink').onclick();
  assert.equal(localDay(state.taskFocus), '2026-11-05');
  context.showSavedPlannerPlan({created: 3, start_date: '2026-11-12'}, draft);
  node('plannerViewPlanLink').onclick();
  assert.equal(localDay(state.taskFocus), '2026-11-12');
});

test('saved-plan link also refreshes a task page already selected', () => {
  const {context, node, effects} = boot();
  context.showSavedPlannerPlan({created: 1, start_date: '2026-11-03'}, {});
  context.location.hash = '#opgaver';
  node('plannerViewPlanLink').onclick();
  assert.deepEqual(effects, ['close', 'navigate']);
});

test('standard-task import immediately refreshes the adjacent archive', async () => {
  const {node, effects} = boot();
  await node('plannerSeedBtn').listeners.click();
  assert.deepEqual(effects, [['/api/planner/seed', 'POST'], 'refreshArchive']);
  assert.match(node('plannerStatus').textContent, /15 nye skabeloner/);
});

test('cancelled import performs no request and failed import stays visible in Settings', async () => {
  const {context, node, effects} = boot();
  context.confirm = () => false;
  await node('plannerSeedBtn').listeners.click();
  assert.deepEqual(effects, []);
  context.confirm = () => true;
  context.api = async () => { throw new Error('Ingen forbindelse'); };
  await node('plannerSeedBtn').listeners.click();
  assert.equal(node('plannerStatus').textContent, 'Ingen forbindelse');
  assert.deepEqual(effects, []);
});

test('planner success calls the receipt only after the existing save confirmation', () => {
  const planner = section('  async function openPlanner(', "  $('plannerOpenBtn').addEventListener");
  const saveAt = planner.indexOf("saved=await api('/api/planner/apply','POST',draft)");
  const verifyAt = planner.indexOf('saved.ok!==true');
  const receiptAt = planner.indexOf('showSavedPlannerPlan(saved,draft);');
  assert.ok(saveAt >= 0 && verifyAt > saveAt && receiptAt > verifyAt);
  assert.match(planner.slice(verifyAt, receiptAt), /return;/);
  assert.match(planner, /if\(plannerDraft!==draft\)/);
});
