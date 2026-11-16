'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const {webcrypto} = require('node:crypto');
const tools = require('../web/planning-tools.js');
const sections = require('../web/settings-folders.js');

test('calendar source labels distinguish HA and direct without exposing addresses', () => {
  assert.equal(tools.sourceLabel({external:true,source_calendar:'calendar.demo'}), 'Home Assistant · calendar.demo');
  assert.equal(tools.sourceLabel({external:true,source_calendar:'direct:abc'}), 'Direkte kalenderforbindelse');
  assert.equal(tools.sourceLabel({external:true,source_calendar:'https://example.invalid/private-token'}), 'Ekstern kalender');
  assert.equal(tools.sourceLabel({}), 'Lokal aftale');
});

test('independent approval IDs are generated using browser crypto', () => {
  const window = {crypto:webcrypto};
  vm.runInNewContext(fs.readFileSync('web/planning-tools.js','utf8'), {window, Uint8Array});
  const a = window.FamilyHubPlanningTools.requestID();
  const b = window.FamilyHubPlanningTools.requestID();
  assert.match(a,/^[a-f0-9]{32}$/); assert.notEqual(a,b);
});

test('collapsed settings recover safely from blocked or corrupt storage', () => {
  for (const value of ['null','[]','invalid','1']) {
    assert.deepEqual(sections.readState({getItem(){return value}}), {});
  }
  assert.deepEqual(sections.readState({getItem(){throw Error('blocked')}}), {});
  assert.equal(sections.readState({getItem(){return '{"plannerPanel":false}'}}).plannerPanel, false);
});

test('section identifiers use stable panel IDs before changing counts or labels', () => {
  assert.equal(sections.sectionKey({id:'plannerPanel'}),'plannerPanel');
  assert.equal(sections.sectionKey({getAttribute(){return 'Calendar sources'}}),'Calendar sources');
  assert.equal(sections.sectionKey({getAttribute(){return null},querySelector(){return {textContent:'Archive'}}}),'Archive');
});

test('revealing a receipt opens every collapsed ancestor', () => {
  const outer={tagName:'DETAILS',open:false,parentElement:null};
  const inner={tagName:'DETAILS',open:false,parentElement:outer};
  sections.reveal({tagName:'P',parentElement:inner});
  assert.equal(inner.open,true); assert.equal(outer.open,true);
});

test('Quickstart is in Settings and does not replace the daily assistant or planner', () => {
  const html=fs.readFileSync('web/index.html','utf8');
  assert.ok(html.indexOf('id="quickstartPanel"')>html.indexOf('id="settingsPage"'));
  for(const id of ['quickstartOpenBtn','taskDiagnosticsBtn','familyWeekStart','settingsExpandAll','settingsCollapseAll']) {
    assert.equal(html.split('id="'+id+'"').length-1,1);
  }
  assert.ok(html.indexOf('/planning-tools.js')<html.indexOf('/app.js'));
  assert.match(html,/AI-familieassistent/);
  const source=fs.readFileSync('web/planning-tools.js','utf8');
  assert.match(source,/Personlig opgave til hver deltager/);
  assert.match(source,/replace_task_ids/);
  assert.match(source,/preview_hash=preview.preview_hash/);
});
