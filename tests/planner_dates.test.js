'use strict';
var test = require('node:test');
var assert = require('node:assert/strict');
var dates = require('../web/planner-dates.js');

test('today uses Copenhagen even before UTC midnight', function () {
  assert.equal(dates.today(new Date('2026-10-28T23:30:00Z')), '2026-10-29');
  assert.equal(dates.today(new Date('2026-07-01T22:30:00Z')), '2026-07-02');
});
test('shortcuts are based on the actual day, not next week', function () {
  assert.equal(dates.shortcut('today','2026-10-29'), '2026-10-29');
  assert.equal(dates.shortcut('tomorrow','2026-10-29'), '2026-10-30');
  assert.equal(dates.shortcut('monday','2026-10-29'), '2026-11-02');
  assert.equal(dates.shortcut('monday','2026-11-02'), '2026-11-09');
  assert.equal(dates.shortcut('tomorrow','2026-12-31'), '2027-01-01');
});
test('rotation starts on Monday after the chosen number of calendar weeks', function () {
  assert.equal(dates.firstRotation('2026-10-29',1),'2026-11-02');
  assert.equal(dates.firstRotation('2026-10-29',2),'2026-11-09');
  assert.equal(dates.firstRotation('2026-10-29',4),'2026-11-23');
  assert.equal(dates.firstRotation('2026-10-29',0),'');
  assert.equal(dates.firstRotation('2026-12-31',1),'2027-01-04');
  assert.equal(dates.firstRotation('2026-10-24',1),'2026-10-26');
});
test('invalid input is rejected', function () {
  assert.throws(function () { dates.shortcut('today','2026-02-30'); });
  assert.throws(function () { dates.shortcut('today',''); });
});
