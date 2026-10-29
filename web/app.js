/* FamilieHub: zero-dependency, iPadOS 15-compatible frontend. */
(function () {
  'use strict';
  var S = {csrf:'', account:null, accounts:[], view:'week', focus:new Date(), filter:'', people:[], events:[], tasks:[], templates:[], allTasks:[], settingsOpen:false, settingsTab:'templates', settingsQuery:'', page:'kalender', taskPeriod:'week', taskFocus:new Date(), taskFilter:'', taskStatus:'open', weekEntries:[], todayEvents:[], todayDate:'', loading:0, socket:null};
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
  // Category colours apply only when no tagged family member takes precedence.
  var categoryColors={sport:'#2086b5',skole:'#8158b1',arbejde:'#718096',hjemme:'#dc8a29',familie:'#cf578f',fritid:'#2a9d8f',privat:'#798087',legeaftale:'#e86a3c'};
  function colorsOf(ev){
    var ids=ev.person_ids&&ev.person_ids.length?ev.person_ids:(ev.person_id?[ev.person_id]:[]),colors=[];
    ids.forEach(function(id){var p=member(id);if(p&&colors.indexOf(p.color)===-1)colors.push(p.color)});
    if(!colors.length)colors.push(categoryColors[ev.category]||'#8ba5a8');
    return colors;
  }
  function tint(hex,alpha){var v=hex.substring(1);return 'rgba('+parseInt(v.substring(0,2),16)+','+parseInt(v.substring(2,4),16)+','+parseInt(v.substring(4,6),16)+','+alpha+')'}
  function matchesPersonFilter(ev){return !S.filter||ev.person_id===S.filter||ev.pickup_by===S.filter||(ev.person_ids||[]).indexOf(S.filter)!==-1}
  function stripeOf(colors){if(colors.length===1)return colors[0];var stops=[];colors.forEach(function(c,i){var a=100*i/colors.length,b=100*(i+1)/colors.length;stops.push(c+' '+a+'%');stops.push(c+' '+b+'%')});return 'linear-gradient(to bottom,'+stops.join(',')+')'}
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
    try{var d=await api('/api/session');if(d.authenticated){S.csrf=d.csrf;S.account=d.account||{role:'admin',username:'Administrator'};hide('login');show('app');await load();syncPage();listen();scheduleSaver()}else{show('login')}}catch(e){show('login');$('loginError').textContent=e.message}
  }
  function range(){var start=S.view==='week'?startWeek(S.focus):localMidnight(S.focus),end=addDays(start,S.view==='week'?7:1);return {start:start,end:end}}
  async function load(){
    var token=++S.loading,r=range(),day=dayString(S.focus);
    try{var p=new URLSearchParams({start:r.start.toISOString(),end:r.end.toISOString(),day:day});var data=await api('/api/state?'+p.toString());if(token!==S.loading)return;
      S.people=data.people||[];S.events=data.events||[];S.todayEvents=data.today_events||[];S.todayDate=data.today_date||dayString(new Date());S.tasks=data.tasks||[];render();setOnline(true);
      $('warning').textContent=data.warning||'';data.warning?show('warning'):hide('warning');$('updated').textContent='Opdateret '+formatDate(new Date(),{hour:'2-digit',minute:'2-digit'});
    }catch(e){setOnline(false);$('warning').textContent='Kunne ikke hente data: '+e.message;show('warning')}
  }
  function listen(){if(S.socket)S.socket.close();S.socket=new EventSource('/api/stream');S.socket.addEventListener('changed',function(){load();if(S.page==='opgaver')loadTasksPage().catch(function(){});if(S.settingsOpen)refreshSettingsData().catch(function(){})});S.socket.onerror=function(){setOnline(false)};S.socket.addEventListener('connected',function(){setOnline(true)})}
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
  function filteredEvents(){return S.events.filter(matchesPersonFilter)}
  function matchesDay(ev,date){var st=new Date(ev.start_at),en=new Date(ev.end_at),start=localMidnight(date),end=addDays(start,1);return st<end&&en>start}
  function eventCard(ev){
    var node=el('button',{'class':'event-card','type':'button','title':ev.external?'Ekstern kalender: kun læsning':'Tryk for at redigere'});
    var colors=colorsOf(ev);
    node.style.setProperty('--event-stripe',stripeOf(colors));
    node.style.backgroundColor=tint(colors[0],0.09);
    var meta=el('span',{'class':'event-meta'});
    meta.appendChild(el('span',{'class':'event-time'},clock(ev.start_at)));
    if(ev.external)meta.appendChild(el('span',{'class':'external-symbol','aria-label':'Ekstern kalender'},'↗'));
    if(ev.category)meta.appendChild(el('span',{'class':'event-category'},'#'+ev.category));
    node.appendChild(meta);
    node.appendChild(el('span',{'class':'event-name'},ev.title));
    node.addEventListener('click',function(){openEvent(ev)});return node;
  }
  function renderCalendar(){var root=$('calendar');root.replaceChildren();root.className='calendar '+(S.view==='week'?'week-view':'day-view');var days=S.view==='week'?7:1;var start=range().start,events=filteredEvents();
    for(var i=0;i<days;i++){(function(date){var column=el('div',{'class':'day-column'+(dayString(date)===dayString(new Date())?' is-today':'')});var heading=el('button',{'class':'day-heading','type':'button'});heading.appendChild(el('span',{'class':'weekday'},formatDate(date,{weekday:'short'})));heading.appendChild(el('strong',null,String(date.getDate())));heading.addEventListener('click',function(){S.focus=new Date(date);setView('day')});column.appendChild(heading);
      var items=events.filter(function(e){return matchesDay(e,date)}).sort(function(a,b){return a.start_at.localeCompare(b.start_at)});
      if(!items.length){var empty=el('div',{'class':'day-empty'},'Ingen aftaler');column.appendChild(empty)}
      items.forEach(function(e){column.appendChild(eventCard(e))});root.appendChild(column)
    })(addDays(start,i))}
  }
  function repeatLabel(rule){return ({daily:'Hver dag',weekdays:'Hverdage (mandag–fredag)',weekly:'Hver uge (én dag)',biweekly:'Hver anden uge (én dag)',once_week:'Én hel uge',weekly_window:'Hver uge (hele ugen)',biweekly_window:'Hver anden uge (hele ugen)'})[rule]||''}
  function renderTasks(){var root=$('taskList');root.replaceChildren();var tasks=S.tasks.filter(function(t){return !S.filter||t.assigned_to===S.filter}).sort(function(a,b){return Number(a.done)-Number(b.done)}).slice(0,4);if(!tasks.length){root.appendChild(el('p',{'class':'empty-state'},'Ingen opgaver på denne dag. Tryk “+ Opgave” for at oprette en.'));return}
    tasks.forEach(function(t){var row=el('div',{'class':'task-row'});row.style.borderLeft='4px solid '+colorOf(t.assigned_to);row.style.paddingLeft='9px';var check=el('input',{type:'checkbox','aria-label':'Fuldfør '+t.title});check.checked=t.done;check.addEventListener('change',async function(){try{await api('/api/tasks/'+t.id+'/toggle','POST',{day:dayString(S.focus),done:check.checked});load()}catch(e){check.checked=!check.checked;alert(e.message)}});row.appendChild(check);
      var text=el('div',{'class':'task-text'});var title=el('strong',{'class':t.done?'strike':''},t.title);text.appendChild(title);var meta=el('small');var owner=el('span',{'class':'task-person-label'},t.assigned_to?nameOf(t.assigned_to):'Fælles');owner.style.backgroundColor=tint(colorOf(t.assigned_to),0.13);owner.style.color=colorOf(t.assigned_to);meta.appendChild(owner);if(repeatLabel(t.repeat_rule))meta.appendChild(document.createTextNode(' · '+repeatLabel(t.repeat_rule)));text.appendChild(meta);row.appendChild(text);
      var edit=el('button',{'class':'text-button','type':'button'},'Redigér');edit.addEventListener('click',function(){openTask(t)});row.appendChild(edit);root.appendChild(row)
    })
  }
  function renderToday(){var root=$('todayList');root.replaceChildren();var es=S.todayEvents.filter(matchesPersonFilter);$('todayDateLabel').textContent=S.todayDate?formatDate(new Date(S.todayDate+'T12:00:00'),{day:'numeric',month:'short'}):'';
    if(!es.length){root.appendChild(el('p',{'class':'empty-state'},'Ingen aftaler i dag.'));return}
    es.sort(function(a,b){return a.start_at.localeCompare(b.start_at)}).slice(0,6).forEach(function(e){var row=el('div',{'class':'activity-row'});var dot=el('span',{'class':'activity-dot'});dot.style.background=colorsOf(e)[0];row.appendChild(dot);var box=el('div');box.appendChild(el('strong',null,e.title));var extra=clock(e.start_at)+'–'+clock(e.end_at);if(e.pickup_by)extra+=' · '+nameOf(e.pickup_by)+' henter';else if(e.person_id)extra+=' · '+nameOf(e.person_id);box.appendChild(el('small',null,extra));row.appendChild(box);root.appendChild(row)})
  }
  function openModal(title,html){$('modalTitle').textContent=title;$('modalBody').innerHTML=html;$('formError').textContent='';show('modal');var first=$('modalBody').querySelector('input:not([type="hidden"])');if(first)first.focus()}
  function closeModal(){hide('modal');$('modalBody').replaceChildren()}
  function personOptions(selected,emptyText){return '<option value="">'+esc(emptyText||'Ingen')+'</option>'+S.people.map(function(p){return '<option value="'+esc(p.id)+'"'+(selected===p.id?' selected':'')+'>'+esc(p.name)+'</option>'}).join('')}
  function field(label,html){return '<label class="field">'+esc(label)+html+'</label>'}
  async function saveOrError(fn){try{await fn();closeModal();await load();if(S.page==='opgaver')await loadTasksPage()}catch(e){$('formError').textContent=e.message}}
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
  function templateOptions(){return '<option value="">Vælg en skabelon fra arkivet…</option>'+S.templates.map(function(t){return '<option value="'+esc(t.id)+'">'+esc(t.title)+' · '+esc(repeatLabel(t.repeat_rule)||'En gang')+'</option>'}).join('')}
  async function openTask(t){
    var editing=!!t;
    try{S.templates=await api('/api/templates')}catch(err){S.templates=[]}
    var html='<form id="entryForm" class="form-grid">'+
      (!editing?'<label class="field field-wide">Fra opgavearkivet<select id="templatePicker" name="template_id">'+templateOptions()+'</select></label>':'')+
      field('Opgave','<input name="title" maxlength="150" required value="'+esc(t?t.title:'')+'">')+
      field('Ansvarlig','<select name="assigned_to">'+personOptions(t?t.assigned_to:'','Fælles')+'</select>')+
      field('Startdato / forfalder','<input name="due_date" type="date" required value="'+esc(t?t.due_date:dayString(S.focus))+'">')+
      field('Gentagelse',repeatSelectHTML())+
      '<p class="muted field-wide">Hverdage gentages mandag–fredag og afkrydses for hver dag. En opgave for hele ugen krydses kun af én gang. Vælg startdatoen for at bestemme, hvilke uger den skal gælde.</p>'+
      '<div class="form-actions">'+(editing?'<button type="button" id="deleteBtn" class="danger">Slet</button>':'')+
      '<button type="button" id="saveTemplateBtn" class="minor">Gem i arkiv</button><button type="submit" class="primary">Gem opgave</button></div></form>';
    openModal(editing?'Redigér opgave':'Ny opgave',html);
    var form=$('entryForm').elements;form.repeat_rule.value=t?t.repeat_rule:'none';
    if(!editing)$('templatePicker').addEventListener('change',function(){var selected=S.templates.find(function(x){return x.id===$('templatePicker').value});if(!selected)return;form.title.value=selected.title;form.assigned_to.value=selected.assigned_to;form.repeat_rule.value=selected.repeat_rule});
    function fromForm(){return {id:'',title:form.title.value,assigned_to:form.assigned_to.value,due_date:form.due_date.value,repeat_rule:form.repeat_rule.value,done:false}}
    $('entryForm').addEventListener('submit',function(evt){evt.preventDefault();saveOrError(async function(){await api(editing?'/api/tasks/'+t.id:'/api/tasks',editing?'PUT':'POST',fromForm());if(S.settingsOpen)await refreshSettingsData()})});
    $('saveTemplateBtn').addEventListener('click',async function(){var values=fromForm();try{await api('/api/templates','POST',{id:'',title:values.title,assigned_to:values.assigned_to,repeat_rule:values.repeat_rule});S.templates=await api('/api/templates');$('formError').textContent='Gemt i opgavearkivet. Du kan stadig gemme selve opgaven.';if(!editing){var old=$('templatePicker').value;$('templatePicker').innerHTML=templateOptions();$('templatePicker').value=old}}catch(err){$('formError').textContent=err.message}});
    if(editing)$('deleteBtn').addEventListener('click',function(){if(confirm('Slet denne opgave og dens afkrydsninger?'))saveOrError(async function(){await api('/api/tasks/'+t.id,'DELETE');if(S.settingsOpen)await refreshSettingsData()})});
  }
  function repeatSelectHTML(){return '<select name="repeat_rule"><option value="none">En gang (valgt dag)</option><option value="daily">Hver dag</option><option value="weekdays">Hverdage (mandag–fredag)</option><option value="weekly">Hver uge (én dag)</option><option value="biweekly">Hver anden uge (én dag)</option><option value="once_week">Én hel uge</option><option value="weekly_window">Hver uge (hele ugen)</option><option value="biweekly_window">Hver anden uge (hele ugen)</option></select>'}
  async function refreshSettingsData(){
    var data=await Promise.all([api('/api/templates'),api('/api/tasks/all')]);
    S.templates=data[0]||[];S.allTasks=data[1]||[];
    if(S.settingsOpen)renderSettings();
  }
  function openSettingsPage(){location.hash='#indstillinger';syncPage()}
  function closeSettingsPage(){location.hash='#kalender';syncPage()}
  function selectSettingsTab(){S.settingsTab='templates';renderSettings()}
  function renderSettings(){
    var isArchive=true,items=S.templates;
    $('settingsSectionTitle').textContent='Opgavearkiv ('+items.length+')';
    $('settingsDescription').textContent='Genbrug en skabelon fra + Opgave. Arkivet er ikke aktive opgaver.';
    $('settingsAddBtn').textContent='+ Ny skabelon';
    var term=$('settingsSearch').value.toLocaleLowerCase('da-DK').trim(),filtered=items.filter(function(t){return (t.title+' '+nameOf(t.assigned_to)+' '+repeatLabel(t.repeat_rule)).toLocaleLowerCase('da-DK').indexOf(term)!==-1});
    var root=$('settingsList');root.replaceChildren();
    if(!filtered.length){root.appendChild(el('p',{'class':'empty-state'},term?'Ingen opgaver matcher din søgning.':isArchive?'Arkivet er tomt. Tilføj din første skabelon.':'Ingen opgaver oprettet endnu.'));return}
    filtered.forEach(function(t){
      var row=el('div',{'class':'settings-item'});
      var dot=el('i',{'class':'dot'});dot.style.background=colorOf(t.assigned_to);
      var txt=el('div',{'class':'settings-item-info'});
      txt.appendChild(el('strong',null,t.title));
      txt.appendChild(el('small',null,(t.assigned_to?nameOf(t.assigned_to):'Fælles')+' · '+(repeatLabel(t.repeat_rule)||'En gang')+(!isArchive?' · Fra '+t.due_date.split('-').reverse().join('.'):'')));
      var actions=el('div',{'class':'settings-item-actions'});
      var edit=el('button',{'class':'text-button','type':'button'},'Redigér');edit.addEventListener('click',function(){if(isArchive)openTemplate(t);else openTask(t)});actions.appendChild(edit);
      var del=el('button',{'class':'text-button settings-delete','type':'button'},'Slet');del.addEventListener('click',async function(){if(!confirm('Slet '+(isArchive?'skabelonen':'opgaven')+' "'+t.title+'"?'+(isArchive?' Eksisterende opgaver påvirkes ikke.':' Tilhørende afkrydsninger slettes også.')))return;try{await api(isArchive?'/api/templates/'+t.id:'/api/tasks/'+t.id,'DELETE');await refreshSettingsData();await load()}catch(e){alert(e.message)}});actions.appendChild(del);
      row.appendChild(dot);row.appendChild(txt);row.appendChild(actions);root.appendChild(row);
    });
  }

  // Navigation uses URL fragments so the browser's Back button and the title work.
  function syncPage(){
    if(!S.csrf)return;
    var page=(location.hash||'#kalender').substring(1).toLowerCase();
    if(['kalender','opgaver','indstillinger'].indexOf(page)===-1)page='kalender';
    S.page=page;
    S.settingsOpen=page==='indstillinger';
    ['dashboardView','tasksPage','settingsPage'].forEach(function(id){
      var desired={dashboardView:'kalender',tasksPage:'opgaver',settingsPage:'indstillinger'}[id];
      if(page===desired)show(id);else hide(id);
    });
    document.querySelectorAll('.main-tab').forEach(function(link){
      var active=link.getAttribute('data-page')===page;
      link.classList.toggle('active',active);
      if(active)link.setAttribute('aria-current','page');else link.removeAttribute('aria-current');
    });
    if(page==='opgaver')loadTasksPage().catch(function(err){$('tasksPageList').replaceChildren(el('p',{'class':'error'},'Kunne ikke hente opgaver: '+err.message))});
    if(page==='indstillinger'){refreshSettingsData().catch(function(err){$('settingsList').replaceChildren(el('p',{'class':'error'},'Kunne ikke hente arkivet: '+err.message))});loadCalendarOwners();renderAccountSettings();loadAISettings();loadDirectCalendars();loadEmailSettings();loadPushSettings()}
  }
  function taskWeekStart(){return dayString(startWeek(S.taskFocus))}
  async function loadTasksPage(){
    if(S.page!=='opgaver')return;
    var data=await Promise.all([
      api('/api/tasks/week?start='+encodeURIComponent(taskWeekStart())),
      api('/api/tasks/all')
    ]);
    S.weekEntries=data[0].entries||[];
    S.allTasks=data[1]||[];
    renderTasksPage();
  }
  function renderTaskPeopleFilters(){
    var root=$('taskPeopleFilters');root.replaceChildren();
    var all=el('button',{'type':'button','class':'chip'+(!S.taskFilter?' selected':'')},'Alle');
    all.addEventListener('click',function(){S.taskFilter='';renderTasksPage()});root.appendChild(all);
    S.people.forEach(function(p){
      var chip=el('button',{'type':'button','class':'chip'+(S.taskFilter===p.id?' selected':'')});
      var dot=el('i',{'class':'dot'});dot.style.background=p.color;chip.appendChild(dot);
      chip.appendChild(document.createTextNode(p.name));
      chip.addEventListener('click',function(){S.taskFilter=S.taskFilter===p.id?'':p.id;renderTasksPage()});
      root.appendChild(chip);
    });
  }
  function taskDisplayDate(day){return formatDate(new Date(day+'T12:00:00'),{weekday:'short',day:'numeric',month:'short'})}
  function renderTasksPage(){
    var period=S.taskPeriod,all=period==='all';
    document.querySelectorAll('[data-period]').forEach(function(b){b.classList.toggle('active',b.getAttribute('data-period')===period)});
    document.querySelectorAll('[data-status]').forEach(function(b){b.classList.toggle('active',b.getAttribute('data-status')===S.taskStatus)});
    $('tasksDateNavigation').classList.toggle('hidden',all);
    $('taskStatusFilters').classList.toggle('hidden',all);
    renderTaskPeopleFilters();
    var entries=all?S.allTasks.slice():S.weekEntries.slice();
    if(period==='today')entries=entries.filter(function(t){return t.day===dayString(S.taskFocus)});
    if(period==='week'){
      var seen={};
      entries=entries.filter(function(t){
        var span=['once_week','weekly_window','biweekly_window'].indexOf(t.repeat_rule)!==-1;
        if(!span)return true;
        if(seen[t.id])return false;
        seen[t.id]=true;return true;
      });
    }
    if(S.taskFilter)entries=entries.filter(function(t){return t.assigned_to===S.taskFilter});
    if(!all&&S.taskStatus!=='all')entries=entries.filter(function(t){return S.taskStatus==='done'?t.done:!t.done});
    entries.sort(function(a,b){
      var da=all?a.due_date:a.day,db=all?b.due_date:b.day;
      return da.localeCompare(db)||a.title.localeCompare(b.title,'da');
    });
    var title=all?'Alle opgaver':period==='today'?
      'Opgaver '+formatDate(S.taskFocus,{day:'numeric',month:'long'}):
      'Uge '+weekNumber(startWeek(S.taskFocus))+' · '+formatDate(startWeek(S.taskFocus),{month:'long',year:'numeric'});
    $('tasksPeriodTitle').textContent=title;
    $('tasksCount').textContent=entries.length+' '+(entries.length===1?'opgave':'opgaver');
    var root=$('tasksPageList');root.replaceChildren();
    if(!entries.length){
      root.appendChild(el('p',{'class':'empty-state'},'Ingen opgaver med de valgte filtre.'));
      return;
    }
    entries.forEach(function(t){
      var card=el('article',{'class':'task-page-card'});
      var ownerColor=colorOf(t.assigned_to);
      var stripe=el('span',{'class':'task-page-stripe'});stripe.style.background=ownerColor;card.appendChild(stripe);
      if(!all){
        var check=el('input',{'type':'checkbox','aria-label':'Fuldfør '+t.title});check.checked=!!t.done;
        check.addEventListener('change',async function(){
          check.disabled=true;
          try{
            await api('/api/tasks/'+t.id+'/toggle','POST',{day:t.day,done:check.checked});
            await Promise.all([load(),loadTasksPage()]);
          }catch(e){check.checked=!check.checked;alert(e.message)}
          check.disabled=false;
        });card.appendChild(check);
      }
      var content=el('div',{'class':'task-page-content'});
      var titleNode=el('strong',{'class':!all&&t.done?'strike':''},t.title);content.appendChild(titleNode);
      var meta=el('div',{'class':'task-page-meta'});
      var person=el('span',{'class':'task-owner'},t.assigned_to?nameOf(t.assigned_to):'Fælles');
      person.style.backgroundColor=tint(ownerColor,0.13);person.style.color=ownerColor;
      meta.appendChild(person);
      if(t.repeat_rule!=='none')meta.appendChild(el('span',{'class':'task-repetition'},repeatLabel(t.repeat_rule)));
      if(all)meta.appendChild(el('span',{'class':'task-date'},'Fra '+t.due_date.split('-').reverse().join('.')));
      else if(period==='week'){
        var isWhole=['once_week','weekly_window','biweekly_window'].indexOf(t.repeat_rule)!==-1;
        meta.appendChild(el('span',{'class':'task-date'},isWhole?'Hele ugen':taskDisplayDate(t.day)));
      }
      content.appendChild(meta);card.appendChild(content);
      var edit=el('button',{'class':'text-button task-edit','type':'button'},'Redigér');
      edit.addEventListener('click',function(){openTask(t)});card.appendChild(edit);
      if(all){
        var del=el('button',{'class':'text-button settings-delete','type':'button'},'Slet');
        del.addEventListener('click',async function(){
          if(!confirm('Slet opgaven "'+t.title+'" og dens afkrydsninger?'))return;
          try{await api('/api/tasks/'+t.id,'DELETE');await load();await loadTasksPage()}catch(e){alert(e.message)}
        });card.appendChild(del);
      }
      root.appendChild(card);
    });
  }
  function openTemplate(t){
    var editing=!!t;
    var html='<form id="entryForm" class="form-grid">'+field('Opgave','<input name="title" maxlength="150" required value="'+esc(t?t.title:'')+'">')+
      field('Foreslået ansvarlig','<select name="assigned_to">'+personOptions(t?t.assigned_to:'','Fælles / vælg senere')+'</select>')+
      field('Gentagelse',repeatSelectHTML())+
      '<p class="muted field-wide">Skabeloner opretter ikke automatisk opgaver. Vælg dem under + Opgave og angiv startdato.</p>'+
      '<div class="form-actions"><button type="submit" class="primary">Gem skabelon</button></div></form>';
    openModal(editing?'Redigér skabelon':'Ny skabelon',html);$('entryForm').elements.repeat_rule.value=t?t.repeat_rule:'none';
    $('entryForm').addEventListener('submit',function(evt){evt.preventDefault();var f=evt.currentTarget.elements;saveOrError(async function(){await api(editing?'/api/templates/'+t.id:'/api/templates',editing?'PUT':'POST',{id:'',title:f.title.value,assigned_to:f.assigned_to.value,repeat_rule:f.repeat_rule.value});await refreshSettingsData()})});
  }
  function openPeople(){var html='<p class="muted">Tilføj familiemedlemmer. Push er valgfri: find telefonens <code>notify.mobile_app_...</code> under HA → Udviklerværktøjer → Handlinger. Indtast kun <code>mobile_app_...</code>. En <code>device_tracker.*</code> kan ikke sende push.</p><div id="personRows"></div><button id="newPersonBtn" class="primary wide">+ Familiemedlem</button>';openModal('Familiemedlemmer',html);var root=$('personRows');S.people.forEach(function(p){var row=el('div',{'class':'person-row'});var dot=el('i',{'class':'dot'});dot.style.background=p.color;row.appendChild(dot);row.appendChild(el('strong',null,p.name));var b=el('button',{'class':'text-button'},'Redigér');b.addEventListener('click',function(){openPerson(p)});row.appendChild(b);root.appendChild(row)});$('newPersonBtn').addEventListener('click',function(){openPerson()})}
  function openPerson(p){var editing=!!p;var html='<form id="entryForm" class="form-grid">'+field('Navn','<input name="name" maxlength="60" required value="'+esc(p?p.name:'')+'">')+field('Farve','<input name="color" type="color" value="'+esc(p?p.color:'#2a8390')+'">')+field('HA notify-service (valgfri)','<input name="notify_service" placeholder="mobile_app_xxx" value="'+esc(p?p.notify_service:'')+'">')+
   '<div class="form-actions">'+(editing?'<button id="deleteBtn" type="button" class="danger">Slet</button>':'')+(editing&&p.notify_service?'<button id="testBtn" type="button" class="minor">Test push</button>':'')+'<button type="submit" class="primary">Gem</button></div></form>';
   openModal(editing?'Redigér person':'Tilføj person',html);$('entryForm').addEventListener('submit',function(evt){evt.preventDefault();var f=evt.currentTarget.elements;saveOrError(function(){return api(editing?'/api/people/'+p.id:'/api/people',editing?'PUT':'POST',{id:'',name:f.name.value,color:f.color.value,notify_service:f.notify_service.value,position:0})})});
   if(editing){$('deleteBtn').addEventListener('click',function(){if(confirm('Slet '+p.name+'? Opgaver og aftaler beholder deres historik.'))saveOrError(function(){return api('/api/people/'+p.id,'DELETE')})});if(p.notify_service)$('testBtn').addEventListener('click',async function(){try{await api('/api/notifications/test','POST',{person_id:p.id});$('formError').textContent='Testnotifikation sendt.'}catch(e){$('formError').textContent=e.message}})}
  }
  async function settings(){var status;try{status=await api('/api/integrations')}catch(e){alert(e.message);return}var calendarText=!status.calendar_entities.length?'Ingen kalendere valgt':status.calendar_ok?'Kalenderadgang OK':status.calendar_error||'Ingen kalendertest';openModal('Integrationer','<div class="integration-list"><p><strong>Home Assistant API:</strong> '+(status.ha_online?'Forbundet':status.ha_configured?'Kan ikke forbindes':'Ikke konfigureret')+'</p>'+(status.ha_error?'<p class="error">'+esc(status.ha_error)+'</p>':'')+'<p><strong>HA-kalendere:</strong> '+esc(status.calendar_entities.join(', ')||'Ingen')+'</p><p><strong>Kalendertest:</strong> '+esc(calendarText)+'</p><p class="muted">Konfigurér HA_URL, HA_TOKEN og HA_CALENDARS under Yggdrasil → FamilieHub → Settings. Genstart FamilieHub for at indlæse ændringer. Kalender-entity-id starter med calendar. Ingen tokens vises her.</p></div>')}
  $('loginForm').addEventListener('submit',async function(ev){ev.preventDefault();$('loginError').textContent='';try{var d=await api('/api/login','POST',{username:$('username').value.trim(),password:$('password').value});S.csrf=d.csrf;S.account=d.account||{role:'admin',username:'Administrator'};$('password').value='';hide('login');show('app');await load();syncPage();listen();scheduleSaver()}catch(e){$('loginError').textContent=e.message}});
  $('logoutBtn').addEventListener('click',async function(){try{await api('/api/logout','POST',{})}catch(e){}if(S.socket)S.socket.close();S.csrf='';S.account=null;hideSaver();scheduleSaver();hide('app');show('login')});
  $('dayBtn').addEventListener('click',function(){setView('day')});$('weekBtn').addEventListener('click',function(){setView('week')});
  $('prevBtn').addEventListener('click',function(){navigate(-1)});$('nextBtn').addEventListener('click',function(){navigate(1)});$('todayBtn').addEventListener('click',function(){S.focus=new Date();load()});
  $('addEventBtn').addEventListener('click',function(){openEvent()});$('addTaskBtn').addEventListener('click',function(){openTask()});$('settingsPeopleBtn').addEventListener('click',openPeople);$('settingsHABtn').addEventListener('click',settings);$('settingsBtn').addEventListener('click',settings);
  $('settingsAddBtn').addEventListener('click',function(){openTemplate()});
  $('settingsSearch').addEventListener('input',renderSettings);
  $('tasksAddBtn').addEventListener('click',function(){openTask()});
  document.querySelectorAll('[data-period]').forEach(function(button){button.addEventListener('click',function(){S.taskPeriod=button.getAttribute('data-period');S.taskStatus='open';loadTasksPage()})});
  document.querySelectorAll('[data-status]').forEach(function(button){button.addEventListener('click',function(){S.taskStatus=button.getAttribute('data-status');renderTasksPage()})});
  $('tasksPrevBtn').addEventListener('click',function(){S.taskFocus=addDays(S.taskFocus,S.taskPeriod==='week'?-7:-1);loadTasksPage()});
  $('tasksNextBtn').addEventListener('click',function(){S.taskFocus=addDays(S.taskFocus,S.taskPeriod==='week'?7:1);loadTasksPage()});
  $('tasksCurrentBtn').addEventListener('click',function(){S.taskFocus=new Date();loadTasksPage()});
  window.addEventListener('hashchange',syncPage);
  $('closeModal').addEventListener('click',closeModal);$('modal').addEventListener('click',function(e){if(e.target.hasAttribute('data-close'))closeModal()});
  document.addEventListener('keydown',function(e){if(e.key==='Escape')closeModal()});
  var touchX=0,touchY=0;$('calendar').addEventListener('touchstart',function(e){if(e.touches.length===1){touchX=e.touches[0].clientX;touchY=e.touches[0].clientY}},{passive:true});
  $('calendar').addEventListener('touchend',function(e){if(e.changedTouches.length!==1||$('modal').classList.contains('hidden')===false)return;var dx=e.changedTouches[0].clientX-touchX,dy=e.changedTouches[0].clientY-touchY;if(window.innerWidth>700&&Math.abs(dx)>70&&Math.abs(dy)<55)navigate(dx<0?1:-1)},{passive:true});
  document.addEventListener('visibilitychange',function(){if(!document.hidden&&S.csrf)load()});setInterval(function(){if(!document.hidden&&S.csrf)load()},60000);

  // The calendar owner is a fallback only: explicit person tags override it.
  async function loadCalendarOwners(){
    var root=$('calendarOwnerList');root.replaceChildren();
    $('calendarOwnerError').textContent='';
    try{
      var entries=await api('/api/calendar-owners');
      if(!entries.length){root.appendChild(el('p',{'class':'muted'},'Ingen HA-kalendere konfigureret. Tilføj calendar.* i Yggdrasil → HA_CALENDARS.'));return}
      entries.forEach(function(item){
        var row=el('div',{'class':'calendar-owner-row'});
        var name=el('div',{'class':'calendar-owner-name'});name.appendChild(el('strong',null,item.entity_id));
        name.appendChild(el('small',null,'Farve fra standardpersonen, medmindre aftalen har #Person-tag'));
        row.appendChild(name);
        var input=el('select',{'aria-label':'Standardperson for '+item.entity_id});
        input.innerHTML=personOptions(item.person_id,'Fælles / neutral farve');
        input.addEventListener('change',async function(){
          input.disabled=true;
          try{
            await api('/api/calendar-owners','PUT',{entity_id:item.entity_id,person_id:input.value});
            item.person_id=input.value;
            $('calendarOwnerError').textContent='Gemt';
            await load();
          }catch(e){input.value=item.person_id;$('calendarOwnerError').textContent=e.message}
          input.disabled=false;
        });
        row.appendChild(input);root.appendChild(row);
      });
    }catch(e){root.appendChild(el('p',{'class':'error'},'Kalenderfarver kunne ikke indlæses: '+e.message))}
  }

  // Each display has independent light/dark and idle-screen preferences.
  var displayPrefs={theme:'auto',nightFrom:'21:00',nightTo:'07:00',saverDelay:10};
  var saverTimer=null,saverVisible=false;
  function validClock(value){return typeof value==='string'&&/^\d\d:\d\d$/.test(value)&&Number(value.slice(0,2))<24&&Number(value.slice(3))<60}
  function restoreDisplaySettings(){
    try{
      var saved=JSON.parse(localStorage.getItem('familiehub-display-v1')||'{}');
      if(['auto','light','dark'].indexOf(saved.theme)!==-1)displayPrefs.theme=saved.theme;
      if(validClock(saved.nightFrom))displayPrefs.nightFrom=saved.nightFrom;
      if(validClock(saved.nightTo))displayPrefs.nightTo=saved.nightTo;
      if([0,5,10,20,30].indexOf(Number(saved.saverDelay))!==-1)displayPrefs.saverDelay=Number(saved.saverDelay);
    }catch(e){}
    $('themeMode').value=displayPrefs.theme;
    $('nightFrom').value=displayPrefs.nightFrom;
    $('nightTo').value=displayPrefs.nightTo;
    $('saverDelay').value=String(displayPrefs.saverDelay);
    applyTheme();scheduleSaver();
  }
  function applyTheme(){
    var dark=displayPrefs.theme==='dark';
    if(displayPrefs.theme==='auto'){
      var now=new Date(),n=now.getHours()*60+now.getMinutes();
      var from=Number(displayPrefs.nightFrom.slice(0,2))*60+Number(displayPrefs.nightFrom.slice(3));
      var to=Number(displayPrefs.nightTo.slice(0,2))*60+Number(displayPrefs.nightTo.slice(3));
      dark=from>to?(n>=from||n<to):(from<to&&n>=from&&n<to);
    }
    document.documentElement.setAttribute('data-theme',dark?'dark':'light');
    var meta=document.querySelector('meta[name="theme-color"]');if(meta)meta.setAttribute('content',dark?'#10181b':'#182d39');
  }
  function saveDisplaySettings(){
    displayPrefs.theme=$('themeMode').value;
    displayPrefs.nightFrom=validClock($('nightFrom').value)?$('nightFrom').value:'21:00';
    displayPrefs.nightTo=validClock($('nightTo').value)?$('nightTo').value:'07:00';
    displayPrefs.saverDelay=Number($('saverDelay').value);
    try{localStorage.setItem('familiehub-display-v1',JSON.stringify(displayPrefs))}catch(e){}
    applyTheme();scheduleSaver();
  }
  function updateSaver(){
    var now=new Date();
    $('saverClock').textContent=formatDate(now,{hour:'2-digit',minute:'2-digit',hour12:false,timeZone:'Europe/Copenhagen'});
    $('saverDate').textContent=formatDate(now,{weekday:'long',day:'numeric',month:'long',timeZone:'Europe/Copenhagen'});
    var next=S.todayEvents.filter(function(e){return new Date(e.end_at)>now}).sort(function(a,b){return a.start_at.localeCompare(b.start_at)})[0];
    $('saverNext').textContent=next?next.title+' · '+clock(next.start_at):'Ingen flere aftaler i dag';
  }
  function showSaver(){
    if(!S.csrf||!$('login').classList.contains('hidden')||!$('modal').classList.contains('hidden')){scheduleSaver();return}
    saverVisible=true;updateSaver();show('screensaver');
  }
  function hideSaver(){
    if(saverVisible){saverVisible=false;hide('screensaver')}
  }
  function scheduleSaver(){
    if(saverTimer!==null)clearTimeout(saverTimer);
    saverTimer=null;
    if(displayPrefs.saverDelay>0&&S.csrf&&!document.hidden&&!saverVisible){
      saverTimer=setTimeout(showSaver,displayPrefs.saverDelay*60000);
    }
  }
  function activity(){
    if(saverVisible)hideSaver();
    scheduleSaver();
  }
  ['themeMode','nightFrom','nightTo','saverDelay'].forEach(function(id){$(id).addEventListener('change',saveDisplaySettings)});
  $('saverPreviewBtn').addEventListener('click',showSaver);
  $('screensaver').addEventListener('click',activity);
  $('screensaver').addEventListener('touchstart',activity,{passive:true});
  document.addEventListener('keydown',activity);
  document.addEventListener('pointerdown',activity,{passive:true});
  document.addEventListener('wheel',activity,{passive:true});
  setInterval(function(){applyTheme();if(saverVisible)updateSaver()},60000);

  function accountIsAdmin(){return !!S.account&&S.account.role==='admin'}
  function renderAccountSettings(){
    $('accountsAddBtn').classList.toggle('hidden',!accountIsAdmin());
    $('accountsAdmin').classList.toggle('hidden',!accountIsAdmin());
    $('accountSelf').classList.toggle('hidden',accountIsAdmin());
    if(accountIsAdmin()){loadAccounts()}else{
      $('accountSelfInfo').textContent='Du er logget ind som '+(S.account?S.account.username:'medlem')+'. Du kan selv ændre din adgangskode.';
    }
  }
  async function loadAccounts(){
    if(!accountIsAdmin())return;
    var root=$('accountsList');root.replaceChildren();
    try{
      S.accounts=await api('/api/accounts');
      $('accountsError').textContent='';
      if(!S.accounts.length){root.appendChild(el('p',{'class':'empty-state'},'Ingen personlige brugerkonti endnu. Klik + Opret bruger.'));return}
      S.accounts.forEach(function(account){
        var row=el('div',{'class':'settings-item'});
        var color=el('i',{'class':'dot'});color.style.background=colorOf(account.person_id);
        row.appendChild(color);
        var info=el('div',{'class':'settings-item-info'});
        info.appendChild(el('strong',null,account.username));
        info.appendChild(el('small',null,(account.person_id?nameOf(account.person_id):'Intet familiemedlem tilknyttet')+' · '+(account.enabled?'Aktiv':'Deaktiveret')));
        row.appendChild(info);
        var actions=el('div',{'class':'settings-item-actions'});
        var edit=el('button',{'class':'text-button','type':'button'},'Redigér');
        edit.addEventListener('click',function(){openAccountEditor(account)});
        actions.appendChild(edit);
        var reset=el('button',{'class':'text-button','type':'button'},'Nulstil kode');
        reset.addEventListener('click',function(){resetAccountPassword(account)});
        actions.appendChild(reset);
        var del=el('button',{'class':'text-button settings-delete','type':'button'},'Slet');
        del.addEventListener('click',async function(){
          if(!confirm('Slet brugeren '+account.username+'? Familiemedlemmets opgaver og aftaler bliver ikke slettet.'))return;
          try{await api('/api/accounts/'+account.id,'DELETE');await loadAccounts()}catch(err){$('accountsError').textContent=err.message}
        });actions.appendChild(del);
        row.appendChild(actions);root.appendChild(row);
      });
    }catch(e){root.appendChild(el('p',{'class':'error'},'Kunne ikke hente brugere: '+e.message))}
  }
  function openAccountEditor(account){
    if(!accountIsAdmin())return;
    var editing=!!account;
    var html='<form id="entryForm" class="form-grid">'+
      field('Brugernavn','<input name="username" maxlength="32" minlength="3" autocapitalize="none" autocomplete="off" spellcheck="false" required pattern="[A-Za-z][A-Za-z0-9._-]{2,31}" value="'+esc(account?account.username:'')+'" placeholder="fx anton">')+
      field('Familiemedlem','<select name="person_id">'+personOptions(account?account.person_id:'','Vælg familiemedlem (valgfrit)')+'</select>')+
      (!editing?field('Midlertidig adgangskode','<input type="password" name="password" required minlength="12" maxlength="128" autocomplete="new-password">'):'')+
      '<label class="field field-wide account-enabled"><span>Adgang til FamilieHub</span><span><input name="enabled" type="checkbox" '+(!account||account.enabled?'checked':'')+'> Konto aktiv</span></label>'+
      '<p class="muted field-wide">Brugeren kan logge ind fra andre enheder med sit brugernavn og adgangskode. Send adgangskoden privat.</p>'+
      '<div class="form-actions"><button type="submit" class="primary">'+(editing?'Gem bruger':'Opret bruger')+'</button></div></form>';
    openModal(editing?'Redigér bruger':'Opret bruger',html);
    $('entryForm').addEventListener('submit',function(ev){
      ev.preventDefault();
      var f=ev.currentTarget.elements;
      var data={username:f.username.value.trim().toLowerCase(),person_id:f.person_id.value,enabled:f.enabled.checked,password:editing?'':f.password.value};
      saveOrError(async function(){
        await api(editing?'/api/accounts/'+account.id:'/api/accounts',editing?'PUT':'POST',data);
        await loadAccounts();
      });
    });
  }
  function resetAccountPassword(account){
    if(!accountIsAdmin())return;
    openModal('Nulstil adgangskode for '+account.username,
      '<form id="entryForm" class="form-grid">'+field('Ny adgangskode','<input type="password" name="password" required minlength="12" maxlength="128" autocomplete="new-password">')+
      '<p class="muted field-wide">Alle tidligere login-sessioner for '+esc(account.username)+' bliver logget af.</p>'+
      '<div class="form-actions"><button type="submit" class="primary">Gem ny adgangskode</button></div></form>');
    $('entryForm').addEventListener('submit',function(ev){
      ev.preventDefault();
      var pass=ev.currentTarget.elements.password.value;
      saveOrError(function(){return api('/api/accounts/'+account.id+'/password','POST',{password:pass})});
    });
  }
  function changeMyPassword(){
    if(accountIsAdmin()){alert('Administratorens adgangskode ændres i Yggdrasil under APP_PASSWORD.');return}
    openModal('Skift min adgangskode',
      '<form id="entryForm" class="form-grid">'+
      field('Nuværende adgangskode','<input type="password" name="old_password" required autocomplete="current-password">')+
      field('Ny adgangskode','<input type="password" name="new_password" required minlength="12" maxlength="128" autocomplete="new-password">')+
      '<p class="muted field-wide">Når du gemmer, logges du af på alle enheder. Log derefter ind med din nye adgangskode.</p>'+
      '<div class="form-actions"><button type="submit" class="primary">Skift adgangskode</button></div></form>');
    $('entryForm').addEventListener('submit',async function(ev){
      ev.preventDefault();
      var f=ev.currentTarget.elements;
      try{
        await api('/api/account/password','POST',{old_password:f.old_password.value,new_password:f.new_password.value});
        closeModal();
        if(S.socket)S.socket.close();S.socket=null;
        S.csrf='';S.account=null;hideSaver();scheduleSaver();hide('app');show('login');
        $('password').value='';$('loginError').textContent='Adgangskoden er ændret. Log ind igen.';
      }catch(e){$('formError').textContent=e.message}
    });
  }
  $('accountsAddBtn').addEventListener('click',function(){openAccountEditor()});
  $('changeMyPasswordBtn').addEventListener('click',changeMyPassword);

  function aiStatus(message){$('aiConfigStatus').textContent=message}
  function aiShowBaseURL(){
    $('aiBaseField').classList.toggle('hidden',$('aiProvider').value!=='compatible');
  }
  async function loadAISettings(){
    if(!accountIsAdmin()){$('aiSettingsPanel').classList.add('hidden');return}
    $('aiSettingsPanel').classList.remove('hidden');
    try{
      var cfg=await api('/api/ai/config');
      $('aiProvider').value=cfg.provider||'off';
      $('aiModel').value=cfg.model||'';
      $('aiBaseURL').value=cfg.base_url||'';
      $('aiKey').value='';
      $('aiClearKey').checked=false;
      $('aiReminders').checked=!!cfg.reminders_enabled;
      $('aiReminderTime').value=cfg.reminder_time||'18:00';
      aiShowBaseURL();
      aiStatus(cfg.key_set?'API-nøgle er gemt krypteret på serveren.':'Ingen API-nøgle gemt. For lokal Ollama kan nøglefeltet være tomt.');
    }catch(e){aiStatus('Kunne ikke hente AI-indstillinger: '+e.message)}
  }
  $('aiProvider').addEventListener('change',aiShowBaseURL);
  $('aiSaveBtn').addEventListener('click',async function(){
    var b=$('aiSaveBtn');b.disabled=true;aiStatus('Gemmer…');
    try{
      var payload={
        provider:$('aiProvider').value,
        model:$('aiModel').value.trim(),
        base_url:$('aiProvider').value==='compatible'?$('aiBaseURL').value.trim():'',
        api_key:$('aiKey').value,
        clear_key:$('aiClearKey').checked,
        reminders_enabled:$('aiReminders').checked,
        reminder_time:$('aiReminderTime').value
      };
      var cfg=await api('/api/ai/config','PUT',payload);
      $('aiKey').value='';$('aiClearKey').checked=false;
      aiStatus('Gemt. '+(cfg.key_set?'API-nøgle er klar.':'Ingen API-nøgle gemt.')+' Påmindelser: '+(cfg.reminders_enabled?'til':'fra')+'.');
    }catch(e){aiStatus('Kunne ikke gemme: '+e.message)}
    b.disabled=false;
  });
  $('aiTestBtn').addEventListener('click',async function(){
    var b=$('aiTestBtn');b.disabled=true;aiStatus('Tester forbindelsen til AI-udbyderen…');
    try{var d=await api('/api/ai/test','POST',{});aiStatus('Forbindelsen virker: '+d.text)}
    catch(e){aiStatus('Test fejlede: '+e.message)}
    b.disabled=false;
  });
  document.querySelectorAll('[data-ai-mode]').forEach(function(b){
    b.addEventListener('click',async function(){
      var status=$('aiAssistStatus'),result=$('aiAssistResult');
      status.textContent='AI arbejder på forslaget…';result.classList.add('hidden');b.disabled=true;
      try{
        var d=await api('/api/ai/assist','POST',{mode:b.getAttribute('data-ai-mode')});
        result.textContent=d.text;result.classList.remove('hidden');
        status.textContent='AI-forslag – kontrollér selv oplysningerne. Intet er oprettet eller ændret.';
      }catch(e){status.textContent='Kunne ikke hente AI-forslag: '+e.message}
      b.disabled=false;
    });
  });

  async function loadDirectCalendars(){
    var root=$('directCalendarList');root.replaceChildren();
    if(!accountIsAdmin()){$('directCalendarPanel').classList.add('hidden');return}
    $('directCalendarPanel').classList.remove('hidden');
    try{
      var sources=await api('/api/direct-calendars');
      $('directCalendarStatus').textContent=sources.length?sources.length+' kalenderforbindelser':'Ingen direkte kalendere endnu.';
      sources.forEach(function(source){
        var row=el('div',{'class':'settings-item direct-calendar-item'});
        var owner=el('i',{'class':'dot'});owner.style.background=colorOf(source.person_id);row.appendChild(owner);
        var info=el('div',{'class':'settings-item-info'});
        info.appendChild(el('strong',null,source.name));
        var provider={google:'Google Kalender',microsoft:'Microsoft 365 / Outlook',icloud:'iCloud CalDAV',ics:'ICS-abonnement'}[source.provider]||source.provider;
        info.appendChild(el('small',null,provider+' · '+(source.person_id?nameOf(source.person_id):'Fælles')+' · '+(source.connected?'Forbundet':'Afventer forbindelse')));
        row.appendChild(info);
        var buttons=el('div',{'class':'settings-item-actions'});
        if(source.provider==='google'||source.provider==='microsoft'){
          var connect=el('button',{'type':'button','class':'minor'},source.connected?'Forbind igen':'Forbind konto');
          connect.addEventListener('click',async function(){
            connect.disabled=true;
            try{
              var auth=await api('/api/direct-calendars/'+source.id+'/authorize','POST',{});
              window.location.assign(auth.authorization_url);
            }catch(e){$('directCalendarStatus').textContent='OAuth-fejl: '+e.message;connect.disabled=false}
          });
          buttons.appendChild(connect);
        }
        var del=el('button',{'class':'text-button settings-delete','type':'button'},'Fjern');
        del.addEventListener('click',async function(){
          if(!confirm('Fjern kalenderen "'+source.name+'"? Dine opgaver og lokale aftaler bliver ikke slettet.'))return;
          try{await api('/api/direct-calendars/'+source.id,'DELETE');await loadDirectCalendars();await load()}
          catch(e){$('directCalendarStatus').textContent=e.message}
        });
        buttons.appendChild(del);row.appendChild(buttons);root.appendChild(row);
      });
    }catch(e){$('directCalendarStatus').textContent='Kunne ikke hente kalenderforbindelser: '+e.message}
  }
  function openDirectCalendar(){
    if(!accountIsAdmin())return;
    var html='<form id="entryForm" class="form-grid">'+
      field('Udbyder','<select name="provider" id="dcProvider"><option value="google">Google Kalender</option><option value="microsoft">Microsoft 365 / Outlook</option><option value="icloud">iCloud CalDAV</option><option value="ics">ICS (skrivebeskyttet)</option></select>')+
      field('Visningsnavn','<input name="name" maxlength="80" required placeholder="Fx Antons kalender">')+
      field('Standardperson / farve','<select name="person_id">'+personOptions('','Fælles / neutral')+'</select>')+
      '<div id="dcURLField" class="field field-wide hidden"><label>Kalenderadresse (HTTPS)</label><input name="url" type="url" placeholder="https://.../calendar.ics eller /caldav/"></div>'+
      '<div id="dcUsernameField" class="field hidden"><label>Apple ID / brugernavn</label><input name="username" autocomplete="off"></div>'+
      '<div id="dcClientField" class="field hidden"><label>OAuth Client ID</label><input name="client_id" autocomplete="off" maxlength="300"></div>'+
      '<div class="field" id="dcSecretField"><label id="dcSecretLabel">OAuth Client Secret</label><input name="secret" type="password" autocomplete="new-password" maxlength="4096"></div>'+
      '<p id="dcHelp" class="muted field-wide"></p>'+
      '<div class="form-actions"><button type="submit" class="primary">Opret forbindelse</button></div></form>';
    openModal('Forbind kalender',html);
    var f=$('entryForm').elements;
    function update(){
      var p=f.provider.value,oauth=p==='google'||p==='microsoft';
      $('dcURLField').classList.toggle('hidden',oauth);
      $('dcUsernameField').classList.toggle('hidden',p!=='icloud');
      $('dcClientField').classList.toggle('hidden',!oauth);
      $('dcSecretField').classList.toggle('hidden',p==='ics');
      $('dcSecretLabel').textContent=p==='icloud'?'App-specifik adgangskode':'OAuth Client Secret';
      $('dcHelp').textContent=oauth?'Opret en webapp i udbyderens udviklerportal. Registrér redirect URL: '+window.location.origin+'/api/direct-calendars/oauth/callback. Klik derefter Forbind konto på den oprettede forbindelse.':p==='icloud'?'Indtast den præcise CalDAV-kalenders HTTPS-adresse og en app-specifik adgangskode fra Apple. Ingen Apple ID-adgangskode her.':'Indsæt en privat eller offentlig HTTPS .ics-adresse. Dette er skrivebeskyttet.';
      f.url.required=!oauth;f.client_id.required=oauth;f.secret.required=p!=='ics';f.username.required=p==='icloud';
    }
    f.provider.addEventListener('change',update);update();
    $('entryForm').addEventListener('submit',async function(e){
      e.preventDefault();
      try{
        var d={provider:f.provider.value,name:f.name.value,person_id:f.person_id.value,url:f.url.value,username:f.username.value,client_id:f.client_id.value,secret:f.secret.value};
        await api('/api/direct-calendars','POST',d);
        closeModal();await loadDirectCalendars();await load();
      }catch(error){$('formError').textContent=error.message}
    });
  }
  $('directCalendarAddBtn').addEventListener('click',openDirectCalendar);

  async function loadEmailSettings(){
    if(!accountIsAdmin()){$('emailSettingsPanel').classList.add('hidden');return}
    $('emailSettingsPanel').classList.remove('hidden');
    try{
      var cfg=await api('/api/email/config');
      $('smtpHost').value=cfg.host||'';
      $('smtpPort').value=cfg.port||587;
      $('smtpUsername').value=cfg.username||'';
      $('smtpFrom').value=cfg.from||'';
      $('smtpPassword').value='';$('smtpClearPassword').checked=false;
      $('smtpStatus').textContent=cfg.secret_set?'SMTP-adgangskode er gemt krypteret.':'Ingen SMTP-adgangskode gemt.';
      var root=$('smtpRecipients');root.replaceChildren();
      S.people.forEach(function(p){
        var row=el('div',{'class':'settings-item'});
        var title=el('strong',null,p.name);row.appendChild(title);
        var field=el('input',{'type':'email','placeholder':'Modtageradresse','aria-label':'E-mail til '+p.name});
        var saved=(cfg.recipients||[]).find(function(x){return x.person_id===p.id});
        field.value=saved?saved.email:'';
        row.appendChild(field);
        var button=el('button',{'type':'button','class':'minor'},'Gem adresse');
        button.addEventListener('click',async function(){
          button.disabled=true;
          try{
            await api('/api/email/recipient','PUT',{person_id:p.id,email:field.value.trim()});
            $('smtpStatus').textContent='Modtager for '+p.name+' er gemt.';
          }catch(e){$('smtpStatus').textContent=e.message}
          button.disabled=false;
        });row.appendChild(button);root.appendChild(row);
      });
    }catch(e){$('smtpStatus').textContent='Kunne ikke hente SMTP: '+e.message}
  }
  $('smtpSaveBtn').addEventListener('click',async function(){
    var button=$('smtpSaveBtn');button.disabled=true;
    try{
      var cfg=await api('/api/email/config','PUT',{
        host:$('smtpHost').value.trim(),port:Number($('smtpPort').value),username:$('smtpUsername').value.trim(),
        from:$('smtpFrom').value.trim(),password:$('smtpPassword').value,
        clear_password:$('smtpClearPassword').checked
      });
      $('smtpPassword').value='';$('smtpClearPassword').checked=false;
      $('smtpStatus').textContent='SMTP-konfigurationen er gemt.'+(cfg.secret_set?' Adgangskoden er beskyttet.':'');
    }catch(e){$('smtpStatus').textContent='Kunne ikke gemme SMTP: '+e.message}
    button.disabled=false;
  });

  function canPush(){
    return ('serviceWorker' in navigator)&&('PushManager' in window)&&('Notification' in window)&&window.isSecureContext;
  }
  function loadPushSettings(){
    var select=$('pushPerson');
    select.replaceChildren();
    var account=S.account||{};
    var choices=account.role==='admin'?S.people:S.people.filter(function(p){return p.id===account.person_id});
    choices.forEach(function(p){
      var opt=document.createElement('option');opt.value=p.id;opt.textContent=p.name;select.appendChild(opt);
    });
    if(account.person_id)select.value=account.person_id;
    if(!canPush()){
      $('pushStatus').textContent='Web Push understøttes ikke af denne browser eller dette iPadOS. Brug e-mailnotifikationer i stedet.';
      $('pushEnableBtn').disabled=true;$('pushDisableBtn').disabled=true;return;
    }
    if(!choices.length){
      $('pushStatus').textContent='En administrator skal knytte din login-konto til et familiemedlem, før du kan aktivere push.';
      $('pushEnableBtn').disabled=true;return;
    }
    $('pushEnableBtn').disabled=false;
    var installed=window.navigator.standalone||(window.matchMedia&&window.matchMedia('(display-mode: standalone)').matches);
    $('pushStatus').textContent=installed?'Du kan aktivere pushnotifikationer på denne enhed.':'På iPhone/iPad skal du først vælge Del → Føj til hjemmeskærm og åbne FamilieHub derfra.';
  }
  function decodeVAPID(key){
    var base64=key.replace(/-/g,'+').replace(/_/g,'/');
    var padding='='.repeat((4-base64.length%4)%4);
    var raw=atob(base64+padding);
    return Uint8Array.from(raw,function(c){return c.charCodeAt(0)});
  }
  $('pushEnableBtn').addEventListener('click',async function(){
    if(!canPush())return;
    // Browser permission must be requested immediately in response to this click.
    var permission=await Notification.requestPermission();
    if(permission!=='granted'){$('pushStatus').textContent='Notifikationstilladelse blev ikke givet.';return}
    var button=$('pushEnableBtn');button.disabled=true;
    try{
      var config=await api('/api/push/key');
      var registration=await navigator.serviceWorker.register('/sw.js',{scope:'/'});
      var ready=await navigator.serviceWorker.ready;
      var old=await ready.pushManager.getSubscription();
      var subscription=old||await ready.pushManager.subscribe({userVisibleOnly:true,applicationServerKey:decodeVAPID(config.public_key)});
      var info=subscription.toJSON();
      await api('/api/push/subscribe','POST',{endpoint:info.endpoint,keys:info.keys,person_id:$('pushPerson').value});
      $('pushStatus').textContent='Pushnotifikationer er aktive på denne enhed.';
    }catch(e){$('pushStatus').textContent='Push kunne ikke aktiveres: '+e.message}
    button.disabled=false;
  });
  $('pushDisableBtn').addEventListener('click',async function(){
    if(!canPush())return;
    try{
      var ready=await navigator.serviceWorker.ready;
      var subscription=await ready.pushManager.getSubscription();
      if(subscription){
        await api('/api/push/unsubscribe','POST',{endpoint:subscription.endpoint});
        await subscription.unsubscribe();
      }
      $('pushStatus').textContent='Pushnotifikationer er slået fra på denne enhed.';
    }catch(e){$('pushStatus').textContent='Push kunne ikke fjernes: '+e.message}
  });
  function loadBuild(){fetch('/api/version',{cache:'no-store',credentials:'same-origin'}).then(function(r){return r.json()}).then(function(v){var commit=(v.commit||'dev').substring(0,8);$('buildVersion').textContent=' · Build '+commit}).catch(function(){})}
  restoreDisplaySettings();
  loadBuild();
  init();
})();
