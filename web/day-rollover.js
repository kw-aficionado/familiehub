/* Shared Copenhagen calendar-day watcher. No reloads, writes or 24-hour offsets. */
(function (root) {
  'use strict';
  function watch(options) {
    var host=options.host||root, doc=options.document||host.document;
    var dates=options.dates||host.FamilyHubPlannerDates;
    var now=options.now||function(){return new Date()};
    var set=options.setTimeout||host.setTimeout.bind(host);
    var clear=options.clearTimeout||host.clearTimeout.bind(host);
    var seen=dates.today(now()), timer=null, stopped=false;
    function check() {
      if(stopped||doc.hidden)return false;
      var next=dates.today(now());
      if(next===seen)return false;
      var previous=seen;
      // Set before the callback so a refresh cannot recursively repeat this change.
      seen=next;
      try {
        if(options.onChange(next,previous)===false){seen=previous;return false}
      } catch(error) {seen=previous;throw error}
      return true;
    }
    function cancel(){if(timer!==null){clear(timer);timer=null}}
    function schedule(){
      cancel();
      if(!stopped&&!doc.hidden){
        // Align to real clock minutes: midnight is not 24 hours after page load.
        // Re-evaluating the date handles DST, clock corrections and days asleep.
        timer=set(run,Math.max(1,60000-(now().getTime()%60000)));
      }
    }
    function run(){try{check()}finally{schedule()}}
    doc.addEventListener('visibilitychange',run);
    ['focus','pageshow','online'].forEach(function(event){host.addEventListener(event,run)});
    host.addEventListener('pagehide',cancel);
    schedule();
    return {check:check,stop:function(){
      stopped=true;cancel();doc.removeEventListener('visibilitychange',run);
      ['focus','pageshow','online'].forEach(function(event){host.removeEventListener(event,run)});
      host.removeEventListener('pagehide',cancel);
    }};
  }
  var api={watch:watch};
  if(typeof module==='object'&&module.exports)module.exports=api;
  else root.FamilyHubDayRollover=api;
})(typeof window==='object'?window:this);
