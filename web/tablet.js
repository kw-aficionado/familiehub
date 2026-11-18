/* Limited family display: separately opted-in today ticks and residual actions. */
(function(){
 'use strict';
 var $=function(id){return document.getElementById(id)},csrf='',state=null,sequence=0,selected='',configuredFilter=null,followToday=true,day=window.FamilyHubPlannerDates.today(),lastActivity=Date.now(),visible=false,online=false;
 var followupTools=null;
 var prefs=window.FamilyHubSaverSchedule.normalize(),wait=window.FamilyHubLoginWait.install($('tabletPairForm').querySelector('button'),$('tabletPairWait'));
 function node(tag,text,cls){var n=document.createElement(tag);if(text!==undefined)n.textContent=text;if(cls)n.className=cls;return n}
 function show(id,yes){$(id).classList.toggle('hidden',!yes)}
 function fmt(date,options){return new Intl.DateTimeFormat('da-DK',Object.assign({timeZone:'Europe/Copenhagen'},options)).format(date)}
 function name(id){var person=state&&state.people.find(function(p){return p.id===id});return person?person.name:'F\u00e6lles'}
 function color(id){var person=state&&state.people.find(function(p){return p.id===id});return person&&/^#[0-9a-f]{6}$/i.test(person.color)?person.color:'#8ba5a8'}
 async function api(path,method,body){var headers={};if(body!==undefined)headers['Content-Type']='application/json';if(method==='POST'){headers['X-CSRF-Token']=csrf;headers['X-FH-Pairing']='1'}var r=await fetch('/api/tablet/'+path,{method:method||'GET',credentials:'same-origin',cache:'no-store',headers:headers,body:body===undefined?undefined:JSON.stringify(body)});var d=await r.json().catch(function(){return null});if(!r.ok||!d){var e=new Error(d&&d.error?d.error:'Forbindelsen til FamilieHub kunne ikke oprettes.');e.status=r.status;e.retryAfter=d&&d.retry_after||r.headers.get('Retry-After');throw e}return d}
 function reset(message){if(followupTools)followupTools.clear();sequence++;csrf='';state=null;online=false;configuredFilter=null;selected='';visible=false;show('tabletSaver',false);show('tabletApp',false);show('tabletPairing',true);$('tabletTasks').replaceChildren();$('tabletEvents').replaceChildren();$('tabletFilters').replaceChildren();$('tabletPairError').textContent=message||'';$('tabletCode').value=''}
 function connection(ok,text){online=ok;$('tabletConnection').textContent=text;$('tabletConnection').className='connection'+(ok?'':' offline');if(!ok)document.querySelectorAll('#tabletTasks input,#tabletFollowup button').forEach(function(n){n.disabled=true})}
 function render(){
  var d=state.device;if(followupTools)followupTools.setBoard(state.followup||null);prefs=window.FamilyHubSaverSchedule.normalize(d.display);if(configuredFilter!==d.person_filter){configuredFilter=d.person_filter;selected=configuredFilter||''}
  if(selected&&!state.people.some(function(p){return p.id===selected}))selected='';if(followupTools)followupTools.render();
  $('tabletName').textContent=d.name;$('tabletDate').textContent=fmt(new Date(day+'T12:00:00Z'),{weekday:'long',day:'numeric',month:'long',year:'numeric'});
  $('tabletAccess').textContent=d.can_complete?'Kan afkrydse dagens opgaver \u00b7 Ingen administratoradgang':'Kun visning \u00b7 Ingen administratoradgang';if(d.can_followup)$('tabletAccess').textContent=(d.can_complete?'Dagens opgaver og restopgaver':'Restopgaver')+' kan afsluttes \u00b7 Ingen administratoradgang';
  $('tabletWarning').textContent=state.warning||'';show('tabletWarning',!!state.warning);
  var filters=$('tabletFilters');filters.replaceChildren();[{id:'',name:'Alle'}].concat(state.people).forEach(function(person){var b=node('button',person.name,'chip'+(selected===person.id?' selected':''));b.type='button';b.setAttribute('aria-pressed',String(selected===person.id));b.addEventListener('click',function(){selected=person.id;render()});filters.appendChild(b)});
  var events=state.events.filter(function(e){return !selected||e.person_id===selected||e.pickup_by===selected||(e.person_ids||[]).indexOf(selected)!==-1}).sort(function(a,b){return a.start_at.localeCompare(b.start_at)}),er=$('tabletEvents');er.replaceChildren();
  if(!events.length)er.appendChild(node('p','Ingen aftaler denne dag.','muted'));
  events.forEach(function(event){var row=node('div',undefined,'activity-row'),dot=node('span',undefined,'activity-dot');dot.style.backgroundColor=color(event.person_id);row.appendChild(dot);var text=node('div');text.appendChild(node('strong',event.title));text.appendChild(node('small',fmt(new Date(event.start_at),{hour:'2-digit',minute:'2-digit'})+'\u2013'+fmt(new Date(event.end_at),{hour:'2-digit',minute:'2-digit'})+' \u00b7 '+name(event.person_id)));row.appendChild(text);er.appendChild(row)});
  var tasks=state.tasks.filter(function(t){return !selected||t.assigned_to===selected}).sort(function(a,b){return Number(a.done)-Number(b.done)||a.title.localeCompare(b.title,'da')}),tr=$('tabletTasks');tr.replaceChildren();$('tabletTaskCount').textContent='('+tasks.length+')';
  if(!tasks.length)tr.appendChild(node('p','Ingen opgaver denne dag.','muted'));
  tasks.forEach(function(task){var row=node('label',undefined,'task-row'+(task.done?' tablet-task-done':'')),check=node('input');check.type='checkbox';check.checked=task.done;check.disabled=!online||!d.can_complete||state.day!==state.today||task.followup_status==='excused'||task.followup_status==='superseded';check.setAttribute('aria-label','Fuldf\u00f8r '+task.title);row.style.borderLeft='4px solid '+color(task.assigned_to);row.style.paddingLeft='12px';row.appendChild(check);var text=node('div',undefined,'task-text');text.appendChild(node('strong',task.title,task.done?'strike':''));text.appendChild(node('small',name(task.assigned_to)));if(followupTools){var badge=followupTools.badge(task);if(badge)text.appendChild(badge)}row.appendChild(text);tr.appendChild(row);
   check.addEventListener('change',async function(){var done=check.checked,chosenDay=state.day;check.disabled=true;try{await api('tasks/'+task.id+'/toggle','POST',{day:chosenDay,done:done});await load()}catch(e){if(e.status===401){reset(e.message);return}check.checked=!done;await load();$('tabletWarning').textContent=e.message;show('tabletWarning',true);}});
  });
  $('tabletUpdated').textContent='Opdateret '+fmt(new Date(),{hour:'2-digit',minute:'2-digit'})+' \u00b7 Ny parring senest '+fmt(new Date(d.absolute_expires),{day:'numeric',month:'short',year:'numeric'});tick();
 }
 async function load(){var n=++sequence;if(followToday)day=window.FamilyHubPlannerDates.today();var requested=day;try{var data=await api('state?day='+encodeURIComponent(requested));if(n!==sequence)return;state=data;csrf=data.csrf;connection(true,'Online');show('tabletPairing',false);show('tabletApp',true);render()}catch(e){if(n!==sequence)return;if(e.status===401){reset(e.message);return}connection(false,'Offline \u2013 pr\u00f8ver igen');if(!state){show('tabletPairing',true);$('tabletPairError').textContent=e.message}else{$('tabletWarning').textContent='Viser senest hentede data. Afkrydsning er sl\u00e5et fra, indtil forbindelsen virker.';show('tabletWarning',true)}}}
 $('tabletPairForm').addEventListener('submit',async function(e){e.preventDefault();if(wait.blocked())return;wait.busy(true);$('tabletPairError').textContent='';try{await api('pair','POST',{code:$('tabletCode').value});$('tabletCode').value='';followToday=true;lastActivity=Date.now();await load()}catch(err){$('tabletPairError').textContent=err.message;if(err.status===429)wait.start(err.retryAfter)}finally{wait.busy(false)}});
 function move(amount){var d=new Date(day+'T12:00:00Z');d.setUTCDate(d.getUTCDate()+amount);day=d.toISOString().slice(0,10);followToday=false;load()}
 $('tabletPrev').addEventListener('click',function(){move(-1)});$('tabletNext').addEventListener('click',function(){move(1)});$('tabletToday').addEventListener('click',function(){followToday=true;load()});
 $('tabletDisconnect').addEventListener('click',async function(){if(!confirm('Afbryd denne tablet? En ny engangskode kr\u00e6ves for at forbinde igen.'))return;try{await api('disconnect','POST',{});reset('Tabletten er afbrudt.')}catch(e){$('tabletWarning').textContent=e.message;show('tabletWarning',true)}});
 function activity(){lastActivity=Date.now();visible=false;show('tabletSaver',false)}
 function tick(){
  var now=new Date(),parts=new Intl.DateTimeFormat('en-GB',{timeZone:'Europe/Copenhagen',hour:'2-digit',minute:'2-digit',hour12:false}).formatToParts(now),h=0,m=0;parts.forEach(function(p){if(p.type==='hour')h=Number(p.value)%24;if(p.type==='minute')m=Number(p.value)});
  function mins(value){return Number(value.slice(0,2))*60+Number(value.slice(3))}
  var from=mins(prefs.nightFrom),to=mins(prefs.nightTo),current=h*60+m,dark=prefs.theme==='dark'||prefs.theme==='auto'&&(from>to?(current>=from||current<to):(current>=from&&current<to));document.documentElement.setAttribute('data-theme',dark?'dark':'light');
  var allowed=!!state&&!document.hidden&&window.FamilyHubSaverSchedule.allowed(now,prefs);
  visible=allowed&&(Date.now()-lastActivity>=prefs.saverDelay*60000);show('tabletSaver',visible);
  if(visible){$('tabletSaverClock').textContent=fmt(now,{hour:'2-digit',minute:'2-digit'});$('tabletSaverDate').textContent=fmt(now,{weekday:'long',day:'numeric',month:'long'})}
 }
 ['pointerdown','keydown','touchstart','wheel'].forEach(function(type){document.addEventListener(type,activity,{passive:true})});
 document.addEventListener('visibilitychange',function(){activity();if(!document.hidden)load()});
 window.addEventListener('pageshow',function(e){if(e.persisted){activity();load()}});
 window.addEventListener('online',load);
 followupTools=window.FamilyHubFollowup.setup({tablet:true,api:api,nameOf:name,filter:function(){return selected},canAct:function(){return online&&state&&state.device.can_followup},afterChange:load});
 setInterval(function(){if(!document.hidden)load()},30000);setInterval(tick,1000);load();
})();
