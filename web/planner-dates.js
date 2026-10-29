/* Date-only arithmetic for planner input; independent of the device timezone. */
(function (root) {
  'use strict';
  function parse(value) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) throw new Error('Invalid date');
    var d = new Date(value + 'T12:00:00Z');
    if (!isFinite(d.getTime()) || d.toISOString().slice(0, 10) !== value) throw new Error('Invalid date');
    return d;
  }
  function addDays(value, days) {
    var d = parse(value);
    d.setUTCDate(d.getUTCDate() + days);
    return d.toISOString().slice(0, 10);
  }
  function today(now) {
    var values = {};
    new Intl.DateTimeFormat('en-GB', {timeZone:'Europe/Copenhagen', year:'numeric', month:'2-digit', day:'2-digit'}).formatToParts(now || new Date()).forEach(function (p) { values[p.type] = p.value; });
    return values.year + '-' + values.month + '-' + values.day;
  }
  function shortcut(which, value) {
    var d = parse(value);
    if (which === 'today') return value;
    if (which === 'tomorrow') return addDays(value, 1);
    if (which === 'monday') return addDays(value, (8 - d.getUTCDay()) % 7 || 7);
    throw new Error('Unknown shortcut');
  }
  function firstRotation(value, weeks) {
    if (!weeks) return '';
    var d = parse(value);
    return addDays(value, -((d.getUTCDay() + 6) % 7) + weeks * 7);
  }
  var dates = {today:today, shortcut:shortcut, firstRotation:firstRotation};
  if (typeof module === 'object' && module.exports) module.exports = dates;
  else root.FamilyHubPlannerDates = dates;
})(typeof window === 'object' ? window : this);
