/* FamilieHub: zero-dependency, iPadOS 15-compatible frontend. */
(function () {
  'use strict';
  var S = {csrf:'', view:'week', focus:new Date(), filter:'', people:[], events:[], tasks:[], loading:0, socket:null};
  var $ = function(id){return document.getElementById(id)};
  function el(tag,props,text){var x=document.createElement(tag);if(props)Object.keys(props).forEach(function(k){x.setAttribute(k,props[k])});if(text!==undefined)x.textContent=text;return x}
  function esc(s){return String(s==null?'':s).replace(/[&<>"']/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]})}
  function pad(n){return String(n).padStart(2,'0')}
  function dayString(d){return d.getFullYear()+'-'+pad(d.getMonth()+1)+'-'+pad(d.getDate())}
  function localMidnight(d){return new Date(d.getFullYear(),d.getMonth(),d.getDate())}
  function addDays(d,n){return new Date(d.getFullYear(),d.getMonth(),d.getDate()+n)}
  function startWeek(d){var w=d.getDay();return addDays(d,-((w+6)%7))}
  function formatDate(d,opt){return new Intl.DateTimeFormat('da-DK',opt).format(d)}
  function clock(s){return formatDate(new Date(s),{hour:'2-digit',minute:'2-digit',hour12:false,timeZone:'Europe/Copenhagen'})}
  function dateTimeInput(d){return dayString(d)+'T'+pad(d.getHours())+':'+pad(d.getMinutes())}
  function cleanInput(s){var d=new Date(s);return isNaN(d.getTime())?'':d.toISOString()}
  function member(id){return S.people.find(function(x){return x.id===id})||null}
  function nameOf(id){var p=member(id);return p?p.name:'Ingen'}
  function colorOf(id){var p=member(id);return p?p.color:'#8ba5a8'}
  function show(id){$(id).classList.remove('hidden')}
  function hide(id){$(id).classList.add('hidden')}
  function errorMessage(x){return x&&x.error?x.error:'Der opstod en fejl. Prøv igen.'}
  function setOnline(v){$('connection').textContent=v?'● Online':'● Offline';$('connection').className='connection '+(v?'':'offline')}
  async function api(path,method,body){
    var opts={method:method||'GET',credentials:'same-origin',headers:{}};
    if(body!==undefined){opts.headers['Content-Type']='application/json';opts.body=JSON.stringify(body)}
    if(opts.method!=='GET')opts.headers['X-CSRF-Token']=S.csrf;
    var resp=await fetch(path,opts);var data=await resp.json().catch(function(){return {error:'Ugyldigt serversvar'}});
    if(resp.status===401&&path!=='/api/login'){hide('app');show('login');throw new Error('Session udløbet. Log ind igen.')}
    if(!resp.ok)throw new Error(errorMessage(data));return data;
  }
  async function init(){
    try{var d=await api('/api/session');if(d.authenticated){S.csrf=d.csrf;hide('login');show('app');await load();listen()}else{show('login')}}catch(e){show('login');$('loginError').textContent=e.message}
  }
  function range(){var start=S.view==='week'?startWeek(S.focus):localMidnight(S.focus),end=addDays(start,S.view==='week'?7:1);return {start:start,end:end}}
  async function load(){
    var token=++S.loading,r=range(),day=dayString(S.focus);
    try{var p=new URLSearchParams({start:r.start.toISOString(),end:r.end.toISOString(),day:day});var data=await api('/api/state?'+p.toString());if(token!==S.loading)return;
      S.people=data.people||[];S.events=data.events||[];S.tasks=data.tasks||[];render();setOnline(true);
      $('warning').textContent=data.warning||'';data.warning?show('warning'):hide('warning');$('updated').textContent='Opdateret '+formatDate(new Date(),{hour:'2-digit',minute:'2-digit'});
    }catch(e){setOnline(false);$('warning').textContent='Kunne ikke hente data: '+e.message;show('warning')}
  }
  function listen(){if(S.socket)S.socket.close();S.socket=new EventSource('/api/stream');S.socket.addEventListener('changed',function(){load()});S.socket.onerror=function(){setOnline(false)};S.socket.addEventListener('connected',function(){setOnline(true)})}
  function navigate(n){S.focus=addDays(S.view==='week'?startWeek(S.focus):S.focus,(S.view==='week'?7:1)*n);load()}
  function setView(v){S.view=v;$('dayBtn').classList.toggle('active',v==='day');$('weekBtn').classList.toggle('active',v==='week');load()}
  function render(){
    var r=range();$('currentDate').textContent=formatDate(S.focus,{weekday:'long',day:'numeric',month:'long',year:'numeric'}).toUpperCase();
    $('viewTitle').textContent=S.view==='week'?('Uge '+weekNumber(startWeek(S.focus))+' · '+formatDate(r.start,{month:'long',year:'numeric'})):'Dagsoverblik';
    $('taskDate').textContent='Opgaver for '+formatDate(S.focus,{weekday:'long',day:'numeric',month:'long'});
    renderFilters();renderCalendar();renderTasks();renderToday();
  }
  function weekNumber(date){var d=new Date(Date.UTC(date.getFullYear(),date.getMonth(),date.getDate()));d.setUTCDate(d.getUTCDate()+4-(d.getUTCDay()||7));var start=new Date(Date.UTC(d.getUTCFullYear(),0,1));return Math.ceil((((d-start)/86400000)+1)/7)}
  function renderFilters(){var root=$('peopleFilters');root.replaceChildren();var all=el('button',{'class':'chip'+(!S.filter?' selected':''),'type':'button'},'Alle');all.addEventListener('click',function(){S.filter='';render()});root.appendChild(all);
    S.people.forEach(function(p){var b=el('button',{'class':'chip'+(S.filter===p.id?' selected':''),'type':'button'});var dot=el('i',{'class':'dot'});dot.style.background=p.color;b.appendChild(dot);b.appendChild(document.createTextNode(p.name));b.addEventListener('click',function(){S.filter=S.filter===p.id?'':p.id;render()});root.appendChild(b)})
  }
  function filteredEvents(){return S.events.filter(function(e){return !S.filter||e.person_id===S.filter||e.pickup_by===S.filter||!e.person_id})}
  function matchesDay(ev,date){var st=new Date(ev.start_at),en=new Date(ev.end_at),start=localMidnight(date),end=addDays(start,1);return st<end&&en>start}
  function eventCard(ev){var node=el('button',{'class':'event-card','type':'button','title':ev.external?'Ekstern kalender: kun læsning':'Tryk for at redigere'});node.style.borderLeftColor=colorOf(ev.person_id);var time=el('span',{'class':'event-time'},clock(ev.start_at));node.appendChild(time);node.appendChild(el('span',{'class':'event-name'},ev.title));if(ev.external)node.appendChild(el('span',{'class':'external-symbol'},'↗'));node.addEventListener('click',function(){openEvent(ev)});return node}
  function renderCalendar(){var root=$('calendar');root.replaceChildren();root.className='calendar '+(S.view==='week'?'week-view':'day-view');var days=S.view==='week'?7:1;var start=range().start,events=filteredEvents();
    for(var i=0;i<days;i++){(function(date){var column=el('div',{'class':'day-column'+(dayString(date)===dayString(new Date())?' is-today':'')});var heading=el('button',{'class':'day-heading','type':'button'});heading.appendChild(el('span',{'class':'weekday'},formatDate(date,{weekday:'short'})));heading.appendChild(el('strong',null,String(date.getDate())));heading.addEventListener('click',function(){S.focus=new Date(date);setView('day')});column.appendChild(heading);
      var items=events.filter(function(e){return matchesDay(e,date)}).sort(function(a,b){return a.start_at.localeCompare(b.start_at)});
      if(!items.length){var empty=el('div',{'class':'day-empty'},'Ingen aftaler');column.appendChild(empty)}
      items.forEach(function(e){column.appendChild(eventCard(e))});root.appendChild(column)
    })(addDays(start,i))}
  }
  function repeatLabel(rule){return ({daily:'Hver dag',weekly:'Hver uge (én dag)',biweekly:'Hver anden uge (én dag)',once_week:'Én hel uge',weekly_window:'Hver uge (hele ugen)',biweekly_window:'Hver anden uge (hele ugen)'})[rule]||''}
  function renderTasks(){var root=$('taskList');root.replaceChildren();var tasks=S.tasks.filter(function(t){return !S.filter||t.assigned_to===S.filter});if(!tasks.length){root.appendChild(el('p',{'class':'empty-state'},'Ingen pligter på denne dag. Tryk “+ Opgave” for at oprette en.'));return}
    tasks.forEach(function(t){var row=el('div',{'class':'task-row'});var check=el('input',{type:'checkbox','aria-label':'Fuldfør '+t.title});check.checked=t.done;check.addEventListener('change',async function(){try{await api('/api/tasks/'+t.id+'/toggle','POST',{day:dayString(S.focus),done:check.checked});load()}catch(e){check.checked=!check.checked;alert(e.message)}});row.appendChild(check);
      var text=el('div',{'class':'task-text'});var title=el('strong',{'class':t.done?'strike':''},t.title);text.appendChild(title);text.appendChild(el('small',null,nameOf(t.assigned_to)+(repeatLabel(t.repeat_rule)?' · '+repeatLabel(t.repeat_rule):'')));row.appendChild(text);
      var edit=el('button',{'class':'text-button','type':'button'},'Redigér');edit.addEventListener('click',function(){openTask(t)});row.appendChild(edit);root.appendChild(row)
    })
  }
  function renderToday(){var root=$('todayList');root.replaceChildren();var es=S.events.filter(function(e){return matchesDay(e,S.focus)&&(!S.filter||e.person_id===S.filter||e.pickup_by===S.filter||!e.person_id)});
    if(!es.length){root.appendChild(el('p',{'class':'empty-state'},'Ingen aftaler på den valgte dag.'));return}
    es.sort(function(a,b){return a.start_at.localeCompare(b.start_at)}).slice(0,6).forEach(function(e){var row=el('div',{'class':'activity-row'});var dot=el('span',{'class':'activity-dot'});dot.style.background=colorOf(e.person_id);row.appendChild(dot);var box=el('div');box.appendChild(el('strong',null,e.title));var extra=clock(e.start_at)+'–'+clock(e.end_at);if(e.pickup_by)extra+=' · '+nameOf(e.pickup_by)+' henter';else if(e.person_id)extra+=' · '+nameOf(e.person_id);box.appendChild(el('small',null,extra));row.appendChild(box);root.appendChild(row)})
  }
  function openModal(title,html){$('modalTitle').textContent=title;$('modalBody').innerHTML=html;$('formError').textContent='';show('modal');var first=$('modalBody').querySelector('input:not([type="hidden"])');if(first)first.focus()}
  function closeModal(){hide('modal');$('modalBody').replaceChildren()}
  function personOptions(selected,emptyText){return '<option value="">'+esc(emptyText||'Ingen')+'</option>'+S.people.map(function(p){return '<option value="'+esc(p.id)+'"'+(selected===p.id?' selected':'')+'>'+esc(p.name)+'</option>'}).join('')}
  function field(label,html){return '<label class="field">'+esc(label)+html+'</label>'}
  async function saveOrError(fn){try{await fn();closeModal();await load()}catch(e){$('formError').textContent=e.message}}
  function openEvent(e){var editing=!!e;if(e&&e.external){openModal('Kalenderaftale','<p><strong>'+esc(e.title)+'</strong></p><p>'+esc(formatDate(new Date(e.start_at),{weekday:'long',day:'numeric',month:'long',hour:'2-digit',minute:'2-digit'}))+'</p><p>'+esc(e.location)+'</p><p class="muted">Ekstern kalenderaftale. Redigér den i den oprindelige kalender.</p>');return}
    var start=e?new Date(e.start_at):new Date(S.focus.getFullYear(),S.focus.getMonth(),S.focus.getDate(),15,0),end=e?new Date(e.end_at):new Date(start.getTime()+60*60*1000);
    var html='<form id="entryForm" class="form-grid">'+field('Titel','<input name="title" maxlength="150" required value="'+esc(e?e.title:'')+'">')+
      field('Start','<input name="start" type="datetime-local" required value="'+esc(dateTimeInput(start))+'">')+
      field('Slut','<input name="end" type="datetime-local" required value="'+esc(dateTimeInput(end))+'">')+
      field('Type','<select name="kind"><option value="aftale">Aftale</option><option value="legeaftale">Legeaftale</option><option value="aktivitet">Aktivitet</option><option value="andet">Andet</option></select>')+
      field('For hvem?','<select name="person_id">'+personOptions(e?e.person_id:'','Hele familien')+'</select>')+
      field('Hvem henter?','<select name="pickup_by">'+personOptions(e?e.pickup_by:'','Ingen')+'</select>')+
      field('Sted','<input name="location" maxlength="200" value="'+esc(e?e.location:'')+'">')+
      field('Bemærkninger','<textarea name="notes" maxlength="2000" rows="3">'+esc(e?e.notes:'')+'</textarea>')+
      '<div class="form-actions">'+(editing?'<button type="button" id="deleteBtn" class="danger">Slet</button>':'')+'<button type="submit" class="primary">Gem aftale</button></div></form>';
    openModal(editing?'Redigér aftale':'Ny aftale',html);$('entryForm').elements.kind.value=e?e.kind:'aftale';
    $('entryForm').addEventListener('submit',function(evt){evt.preventDefault();var form=evt.currentTarget.elements;var s=cleanInput(form.start.value),en=cleanInput(form.end.value);if(!s||!en||new Date(en)<=new Date(s)){$('formError').textContent='Slut skal være efter start.';return}
      saveOrError(function(){return api(editing?'/api/events/'+e.id:'/api/events',editing?'PUT':'POST',{title:form.title.value,start_at:s,end_at:en,kind:form.kind.value,person_id:form.person_id.value,pickup_by:form.pickup_by.value,location:form.location.value,notes:form.notes.value,id:'',external:false})})
    });if(editing)$('deleteBtn').addEventListener('click',function(){if(confirm('Slet denne aftale?'))saveOrError(function(){return api('/api/events/'+e.id,'DELETE')})})
  }
  function openTask(t){var editing=!!t;var html='<form id="entryForm" class="form-grid">'+
    field('Opgave','<input name="title" maxlength="150" required value="'+esc(t?t.title:'')+'">')+
    field('Ansvarlig','<select name="assigned_to">'+personOptions(t?t.assigned_to:'','Fælles')+'</select>')+
    field('Startdato / forfalder','<input name="due_date" type="date" required value="'+esc(t?t.due_date:dayString(S.focus))+'">')+
    field('Gentagelse','<select name="repeat_rule"><option value="none">En gang (valgt dag)</option><option value="daily">Hver dag</option><option value="weekly">Hver uge (én dag)</option><option value="biweekly">Hver anden uge (én dag)</option><option value="once_week">Én hel uge</option><option value="weekly_window">Hver uge (hele ugen)</option><option value="biweekly_window">Hver anden uge (hele ugen)</option></select>')+
    '<p class="muted">Hele ugen betyder mandag–søndag. Pligten ses hver dag i ugen, men afkrydses kun én gang pr. uge. Hver anden uge beregnes fra ugen med startdatoen.</p>'+
    '<div class="form-actions">'+(editing?'<button type="button" id="deleteBtn" class="danger">Slet</button>':'')+'<button type="submit" class="primary">Gem opgave</button></div></form>';
    openModal(editing?'Redigér pligt':'Ny pligt',html);$('entryForm').elements.repeat_rule.value=t?t.repeat_rule:'none';
    $('entryForm').addEventListener('submit',function(evt){evt.preventDefault();var form=evt.currentTarget.elements;saveOrError(function(){return api(editing?'/api/tasks/'+t.id:'/api/tasks',editing?'PUT':'POST',{id:'',title:form.title.value,assigned_to:form.assigned_to.value,due_date:form.due_date.value,repeat_rule:form.repeat_rule.value,done:false})})});
    if(editing)$('deleteBtn').addEventListener('click',function(){if(confirm('Slet denne pligt og dens fuldførelser?'))saveOrError(function(){return api('/api/tasks/'+t.id,'DELETE')})})
  }
  function openPeople(){var html='<p class="muted">Tilføj familiemedlemmer. Push er valgfri: find telefonens <code>notify.mobile_app_...</code> under HA → Udviklerværktøjer → Handlinger. Indtast kun <code>mobile_app_...</code>. En <code>device_tracker.*</code> kan ikke sende push.</p><div id="personRows"></div><button id="newPersonBtn" class="primary wide">+ Familiemedlem</button>';openModal('Familiemedlemmer',html);var root=$('personRows');S.people.forEach(function(p){var row=el('div',{'class':'person-row'});var dot=el('i',{'class':'dot'});dot.style.background=p.color;row.appendChild(dot);row.appendChild(el('strong',null,p.name));var b=el('button',{'class':'text-button'},'Redigér');b.addEventListener('click',function(){openPerson(p)});row.appendChild(b);root.appendChild(row)});$('newPersonBtn').addEventListener('click',function(){openPerson()})}
  function openPerson(p){var editing=!!p;var html='<form id="entryForm" class="form-grid">'+field('Navn','<input name="name" maxlength="60" required value="'+esc(p?p.name:'')+'">')+field('Farve','<input name="color" type="color" value="'+esc(p?p.color:'#2a8390')+'">')+field('HA notify-service (valgfri)','<input name="notify_service" placeholder="mobile_app_xxx" value="'+esc(p?p.notify_service:'')+'">')+
   '<div class="form-actions">'+(editing?'<button id="deleteBtn" type="button" class="danger">Slet</button>':'')+(editing&&p.notify_service?'<button id="testBtn" type="button" class="minor">Test push</button>':'')+'<button type="submit" class="primary">Gem</button></div></form>';
   openModal(editing?'Redigér person':'Tilføj person',html);$('entryForm').addEventListener('submit',function(evt){evt.preventDefault();var f=evt.currentTarget.elements;saveOrError(function(){return api(editing?'/api/people/'+p.id:'/api/people',editing?'PUT':'POST',{id:'',name:f.name.value,color:f.color.value,notify_service:f.notify_service.value,position:0})})});
   if(editing){$('deleteBtn').addEventListener('click',function(){if(confirm('Slet '+p.name+'? Opgaver og aftaler beholder deres historik.'))saveOrError(function(){return api('/api/people/'+p.id,'DELETE')})});if(p.notify_service)$('testBtn').addEventListener('click',async function(){try{await api('/api/notifications/test','POST',{person_id:p.id});$('formError').textContent='Testnotifikation sendt.'}catch(e){$('formError').textContent=e.message}})}
  }
  async function settings(){var status;try{status=await api('/api/integrations')}catch(e){alert(e.message);return}var calendarText=!status.calendar_entities.length?'Ingen kalendere valgt':status.calendar_ok?'Kalenderadgang OK':status.calendar_error||'Ingen kalendertest';openModal('Integrationer','<div class="integration-list"><p><strong>Home Assistant API:</strong> '+(status.ha_online?'Forbundet':status.ha_configured?'Kan ikke forbindes':'Ikke konfigureret')+'</p>'+(status.ha_error?'<p class="error">'+esc(status.ha_error)+'</p>':'')+'<p><strong>HA-kalendere:</strong> '+esc(status.calendar_entities.join(', ')||'Ingen')+'</p><p><strong>Kalendertest:</strong> '+esc(calendarText)+'</p><p class="muted">Konfigurér HA_URL, HA_TOKEN og HA_CALENDARS under Yggdrasil → FamilieHub → Settings. Genstart FamilieHub for at indlæse ændringer. Kalender-entity-id starter med calendar. Ingen tokens vises her.</p></div>')}
  $('loginForm').addEventListener('submit',async function(ev){ev.preventDefault();$('loginError').textContent='';try{var d=await api('/api/login','POST',{password:$('password').value});S.csrf=d.csrf;$('password').value='';hide('login');show('app');await load();listen()}catch(e){$('loginError').textContent=e.message}});
  $('logoutBtn').addEventListener('click',async function(){try{await api('/api/logout','POST',{})}catch(e){}if(S.socket)S.socket.close();S.csrf='';hide('app');show('login')});
  $('dayBtn').addEventListener('click',function(){setView('day')});$('weekBtn').addEventListener('click',function(){setView('week')});
  $('prevBtn').addEventListener('click',function(){navigate(-1)});$('nextBtn').addEventListener('click',function(){navigate(1)});$('todayBtn').addEventListener('click',function(){S.focus=new Date();load()});
  $('addEventBtn').addEventListener('click',function(){openEvent()});$('addTaskBtn').addEventListener('click',function(){openTask()});$('peopleBtn').addEventListener('click',openPeople);$('settingsBtn').addEventListener('click',settings);
  $('closeModal').addEventListener('click',closeModal);$('modal').addEventListener('click',function(e){if(e.target.hasAttribute('data-close'))closeModal()});
  document.addEventListener('keydown',function(e){if(e.key==='Escape')closeModal()});
  var touchX=0,touchY=0;$('calendar').addEventListener('touchstart',function(e){if(e.touches.length===1){touchX=e.touches[0].clientX;touchY=e.touches[0].clientY}},{passive:true});
  $('calendar').addEventListener('touchend',function(e){if(e.changedTouches.length!==1||$('modal').classList.contains('hidden')===false)return;var dx=e.changedTouches[0].clientX-touchX,dy=e.changedTouches[0].clientY-touchY;if(Math.abs(dx)>70&&Math.abs(dy)<55)navigate(dx<0?1:-1)},{passive:true});
  document.addEventListener('visibilitychange',function(){if(!document.hidden&&S.csrf)load()});setInterval(function(){if(!document.hidden&&S.csrf)load()},60000);
  init();
})();
