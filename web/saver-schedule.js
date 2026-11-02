/* Pure display preferences and Copenhagen-time screensaver rules. */
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.FamilyHubSaverSchedule = api;
})(typeof window !== 'undefined' ? window : this, function () {
  'use strict';
  function validClock(value) {
    return typeof value === 'string' && /^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(value);
  }
  function normalize(saved) {
    if (!saved || typeof saved !== 'object' || Array.isArray(saved)) saved = {};
    var prefs = {theme:'auto', nightFrom:'21:00', nightTo:'07:00', saverDelay:10,
      saverMode:'scheduled', saverFrom:'20:00', saverTo:'07:00'};
    if (['auto','light','dark'].indexOf(saved.theme) !== -1) prefs.theme = saved.theme;
    ['nightFrom','nightTo','saverFrom','saverTo'].forEach(function (key) {
      if (validClock(saved[key])) prefs[key] = saved[key];
    });
    if ((typeof saved.saverDelay === 'number' || typeof saved.saverDelay === 'string') &&
        /^(0|5|10|20|30)$/.test(String(saved.saverDelay))) prefs.saverDelay = Number(saved.saverDelay);
    if (['scheduled','always'].indexOf(saved.saverMode) !== -1) prefs.saverMode = saved.saverMode;
    // Equal times must not silently turn into an unrestricted full-day schedule.
    if (prefs.saverFrom === prefs.saverTo) { prefs.saverFrom = '20:00'; prefs.saverTo = '07:00'; }
    return prefs;
  }
  function minutes(value) { return Number(value.slice(0,2)) * 60 + Number(value.slice(3)); }
  function allowed(now, prefs) {
    if (!now || typeof now.getTime !== 'function' || !Number.isFinite(now.getTime()) ||
        !prefs || !(prefs.saverDelay > 0)) return false;
    if (prefs.saverMode === 'always') return true;
    if (prefs.saverMode !== 'scheduled' || !validClock(prefs.saverFrom) ||
        !validClock(prefs.saverTo) || prefs.saverFrom === prefs.saverTo) return false;
    var parts = new Intl.DateTimeFormat('en-GB', {timeZone:'Europe/Copenhagen',
      hour:'2-digit', minute:'2-digit', hour12:false}).formatToParts(now);
    var hour = 0, minute = 0;
    parts.forEach(function (part) {
      if (part.type === 'hour') hour = Number(part.value) % 24;
      if (part.type === 'minute') minute = Number(part.value);
    });
    var current = hour * 60 + minute, from = minutes(prefs.saverFrom), to = minutes(prefs.saverTo);
    return from > to ? current >= from || current < to : current >= from && current < to;
  }
  return {validClock:validClock, normalize:normalize, allowed:allowed};
});
