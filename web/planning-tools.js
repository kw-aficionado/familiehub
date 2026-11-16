/* Explicit shared chores and non-destructive review of existing task series. */
(function(root){
  'use strict';
  var weekdays=['Søndag','Mandag','Tirsdag','Onsdag','Torsdag','Fredag','Lørdag'];
  function requestID(){var bytes=new Uint8Array(16);root.crypto.getRandomValues(bytes);return Array.from(bytes).map(function(x){return x.toString(16).padStart(2,'0')}).join('')}
  function sourceLabel(event){var id=event.source_calendar||'';return id.indexOf('calendar.')===0?'Home Assistant · '+id:id.indexOf('direct:')===0?'Direkte kalenderforbindelse':event.external?'Ekstern kalender':'Lokal aftale'}
  function setup(c){
    var S=c.S,$=c.$,el=c.el,api=c.api,nameOf=c.nameOf,esc=c.esc;
    function weekdayOptions(value){return [1,2,3,4,5,6,0].map(function(day){return '<option value="'+day+'"'+(day===value?' selected':'')+'>'+weekdays[day]+'</option>'}).join('')}
    function field(label,input){var box=el('label',{'class':'field'},label);box.appendChild(input);return box}
    function status(text){$('quickstartStatus').textContent=text}
    function reveal(node){if(root.FamilyHubSettings)root.FamilyHubSettings.reveal(node)}
    function firstDay(){return Number.isInteger(S.weekStart)?S.weekStart:1}
    async function refresh(){
      try{var settings=await api('/api/schedule/settings');S.weekStart=settings.week_start;$('familyWeekStart').value=String(settings.week_start);$('familyWeekStart').disabled=!c.isAdmin();$('weekStartSave').disabled=!c.isAdmin()}
      catch(e){status(e.message)}
    }
    $('familyWeekStart').innerHTML=weekdayOptions(1);
    $('weekStartSave').addEventListener('click',async function(){
      var b=$('weekStartSave');b.disabled=true;
      try{var result=await api('/api/schedule/settings','PUT',{week_start:Number($('familyWeekStart').value)});S.weekStart=result.week_start;status('Gemt. Ugen starter '+weekdays[result.week_start].toLowerCase()+'. Eksisterende turnusser og historik er uændrede.');await c.load()}
      catch(e){status(e.message)}b.disabled=!c.isAdmin();
    });

    // A matching title is only a possible overlap: homework and school bags may
    // deliberately belong to every child. Nothing is automatically deleted.
    function approvalControls(parent,preview,start){
      var choices=[],allow=el('input',{type:'checkbox'}),section=el('div',{'class':'plan-conflicts'});
      var conflicts=preview.conflicts||[];
      if(conflicts.length){
        section.appendChild(el('h4',null,'Gennemgå '+conflicts.length+' eksisterende opgaveserier'));
        section.appendChild(el('p',null,'Samme navn kan være et overlap eller en personlig opgave til hvert barn. Intet stoppes automatisk. Markér kun de gamle serier, som den nye plan skal erstatte fra '+start+'. Afkrydsningshistorikken bevares.'));
        conflicts.forEach(function(t){
          var check=el('input',{type:'checkbox','data-replace-task':t.id});check.disabled=!c.isAdmin();
          var label=el('label',{'class':'plan-conflict-row'});label.appendChild(check);
          var people=t.rotation_members&&t.rotation_members.length?t.rotation_members.map(nameOf).join(' / '):nameOf(t.assigned_to);
          label.appendChild(el('span',null,t.title+' · '+people+' · '+c.repeatLabel(t.repeat_rule)+' · fra '+t.due_date+' · '+t.completed+' afkrydsninger · serie '+t.id.slice(0,8)));
          section.appendChild(label);choices.push(check);
        });
        if(!c.isAdmin())section.appendChild(el('p',{'class':'muted'},'Kun administrator kan stoppe eksisterende serier.'));
        var retain=el('label',{'class':'plan-conflict-row'});retain.appendChild(allow);retain.appendChild(el('span',null,'Jeg har gennemgået listen. Behold de øvrige serier, selv om de har samme opgavenavn.'));section.appendChild(retain);
        parent.appendChild(section);
      }
      return function(){return {replace_task_ids:choices.filter(function(e){return e.checked}).map(function(e){return e.getAttribute('data-replace-task')}),allow_overlap:allow.checked}};
    }
    function personChecks(people){var box=el('div',{'class':'quick-people'});people.forEach(function(p){var label=el('label'),check=el('input',{type:'checkbox','data-person-id':p.id});label.appendChild(check);label.appendChild(el('span',null,p.name));box.appendChild(label)});return box}
    var quickDraft=null;
    async function openQuickstart(){
      try{S.templates=await api('/api/templates')}catch(e){status(e.message);return}
      if(!S.people.length||!S.templates.length){status('Opret familiemedlemmer og tilføj arkivopgaver først.');return}
      quickDraft=null;
      c.openModal('Quickstart – hvem deler opgaverne?',
        '<form id="quickstartForm" class="form-grid">'+
        '<label class="field">Planen starter<input id="quickStartDate" type="date" required></label>'+
        '<label class="field">Ugen og opgavebyttet starter<select id="quickWeekStart">'+weekdayOptions(firstDay())+'</select></label>'+
        '<p class="muted field-wide">Vælg kun de opgaver, du vil oprette. En fælles turnus opretter én opgaveserie, ikke én kopi til hver deltager. Vælg fx ét barn som startperson for opvask og det andet for borddækning. Personlige opgaver til hver deltager holdes adskilt.</p>'+
        '<div id="quickChoices" class="field-wide"></div><div class="form-actions"><button type="submit" class="primary">Vis de første 4 uger</button></div></form><div id="quickPreview" aria-live="polite"></div>');
      $('quickStartDate').value=root.FamilyHubPlannerDates.today();
      var entries=[];
      S.templates.forEach(function(t){
        var article=el('fieldset',{'class':'quick-task'}),legend=el('legend'),label=el('label'),chosen=el('input',{type:'checkbox','data-quick-template':t.id});
        label.appendChild(chosen);label.appendChild(el('strong',null,t.title));legend.appendChild(label);article.appendChild(legend);
        var controls=el('div',{'class':'quick-task-controls hidden'});
        var members=personChecks(S.people);controls.appendChild(el('p',{'class':'muted'},t.points+' point / '+t.minutes+' minutter pr. gang. Hvem deltager?'));controls.appendChild(members);
        var mode=el('select',{'data-quick-mode':t.id});mode.innerHTML='<option value="rotate">Én fælles opgave på skift</option><option value="each">Personlig opgave til hver deltager</option>';
        var starter=el('select',{'data-quick-starter':t.id}),repeat=el('select',{'data-quick-repeat':t.id}),rotate=el('select',{'data-quick-rotation':t.id}),weekday=el('select',{'data-quick-weekday':t.id});
        repeat.innerHTML='<option value="daily">Hver dag</option><option value="weekdays">Hverdage (mandag–fredag)</option><option value="weekly_window">Én gang i løbet af hver uge</option><option value="weekly">Hver uge på en valgt dag</option><option value="biweekly_window">Én gang i løbet af hver anden uge</option><option value="biweekly">Hver anden uge på en valgt dag</option><option value="none">Kun på startdatoen</option><option value="once_week">Kun i startugen</option>';
        repeat.value=t.repeat_rule||'daily';
        rotate.innerHTML='<option value="1">Hver uge</option><option value="2">Hver anden uge</option><option value="4">Hver fjerde uge</option>';
        weekday.innerHTML=weekdayOptions(firstDay());
        var starterField=field('Hvem starter?',starter),rotateField=field('Byt ansvar',rotate),dayField=field('Ugedag',weekday);
        controls.appendChild(field('Fordeling',mode));controls.appendChild(starterField);controls.appendChild(field('Gentagelse',repeat));controls.appendChild(dayField);controls.appendChild(rotateField);article.appendChild(controls);$('quickChoices').appendChild(article);
        function selectedPeople(){return Array.from(members.querySelectorAll('input:checked')).map(function(e){return e.getAttribute('data-person-id')})}
        function update(){
          controls.classList.toggle('hidden',!chosen.checked);
          var ids=selectedPeople(),previous=starter.value;starter.replaceChildren();starter.appendChild(el('option',{value:''},'Vælg startperson'));
          ids.forEach(function(id){starter.appendChild(el('option',{value:id},nameOf(id)))});starter.value=ids.indexOf(previous)!==-1?previous:ids.length===1?ids[0]:'';
          starterField.classList.toggle('hidden',mode.value==='each');
          rotateField.classList.toggle('hidden',mode.value==='each'||ids.length<2||repeat.value==='none'||repeat.value==='once_week');
          dayField.classList.toggle('hidden',['weekly','biweekly'].indexOf(repeat.value)===-1);
        }
        [chosen,mode,repeat].forEach(function(e){e.addEventListener('change',update)});members.addEventListener('change',update);update();
        entries.push(function(){return chosen.checked?{template_id:t.id,people:selectedPeople(),starter:starter.value,mode:mode.value,repeat_rule:repeat.value,weekday:Number(weekday.value),rotate_weeks:mode.value==='each'?0:Number(rotate.value)}:null});
      });
      function invalidate(){quickDraft=null;$('quickPreview').replaceChildren()}
      $('quickstartForm').addEventListener('input',invalidate);$('quickstartForm').addEventListener('change',invalidate);
      $('quickstartForm').addEventListener('submit',async function(event){
        event.preventDefault();$('formError').textContent='';$('quickPreview').replaceChildren();
        var draft={start_date:$('quickStartDate').value,week_start:Number($('quickWeekStart').value),choices:entries.map(function(fn){return fn()}).filter(Boolean),request_id:requestID()};quickDraft=draft;
        try{
          var preview=await api('/api/quickstart/preview','POST',draft);
          if(quickDraft!==draft||!$('quickPreview'))return;
          draft.preview_hash=preview.preview_hash;
          var parent=$('quickPreview');parent.appendChild(el('h3',null,'Gennemgå din turnus'));
          parent.appendChild(el('p',null,'Starter '+preview.start_date+'. Ugestart: '+weekdays[preview.week_start]+'. '+(preview.first_rotation_date?'Første bytte: '+preview.first_rotation_date+'.':'Ingen automatisk rotation.')));
          if(preview.warning)parent.appendChild(el('p',{'class':'warning'},preview.warning));
          preview.weeks.forEach(function(w,i){
            var block=el('details',{'class':'quick-week'});block.open=i===0;block.appendChild(el('summary',null,'Uge '+(i+1)+' · '+w.start+' – '+w.end));
            block.appendChild(el('p',{'class':'muted'},Object.keys(w.points).map(function(id){return nameOf(id)+': '+w.points[id]+' point / '+w.minutes[id]+' min.'}).join(' · ')||'Ingen forekomster i denne uge.'));
            w.assignments.forEach(function(t){block.appendChild(el('p',null,nameOf(t.person_id)+' · '+t.title+' · '+t.count+' gange'))});parent.appendChild(block);
          });
          var approval=approvalControls(parent,preview,draft.start_date),message=el('p',{'role':'status','class':'planner-save-status'});parent.appendChild(message);
          var save=el('button',{'class':'primary',type:'button'},'Godkend og gem turnus');parent.appendChild(save);
          save.addEventListener('click',async function(){
            if(quickDraft!==draft){message.textContent='Beregn forslaget igen.';return}
            Object.assign(draft,approval());save.disabled=true;message.textContent='Gemmer turnus…';
            try{var result=await api('/api/quickstart/apply','POST',draft);c.receipt(result,draft);reveal($('plannerStatus'));await c.load()}
            catch(e){message.textContent=e.message;message.classList.add('error');save.disabled=false}
          });parent.scrollIntoView({block:'nearest'});
        }catch(e){$('formError').textContent=e.message;$('formError').scrollIntoView({block:'nearest'})}
      });
    }
    $('quickstartOpenBtn').addEventListener('click',openQuickstart);

    async function openDiagnostics(){
      var data;
      try{data=await api('/api/planner/diagnostics')}catch(e){status(e.message);return}
      c.openModal('Gennemgå opgaveserier',
        '<p>Samme titel betyder ikke nødvendigvis en dublet. Lektier og skoletasker kan være personlige opgaver til hvert barn. Et stop bevarer gamle afkrydsninger og fjerner kun fremtidige forekomster.</p>'+
        '<label class="field">Stop valgte serier fra<input id="retireDate" type="date"></label><div id="taskDiagnostics"></div><p id="retireStatus" role="status"></p><button type="button" id="retireSave" class="danger">Gennemgå og stop valgte serier</button>');
      $('retireDate').value=data.day;$('retireDate').min=data.day;
      var groups={},selected=[];
      data.tasks.forEach(function(t){var key=t.title.trim().toLocaleLowerCase('da-DK');if(!groups[key])groups[key]=[];groups[key].push(t)});
      Object.keys(groups).forEach(function(key){
        var tasks=groups[key],active=tasks.filter(function(t){return t.active_today}).length;
        var block=el('details',{'class':'quick-week'});block.open=active>1;
        block.appendChild(el('summary',null,tasks[0].title+' · '+tasks.length+' serier'+(active>1?' · '+active+' aktive i dag (muligt overlap)':'')));
        tasks.forEach(function(t){
          var row=el('label',{'class':'plan-conflict-row'}),check=el('input',{type:'checkbox'});check.disabled=!c.isAdmin()||!!t.inactive_from;row.appendChild(check);
          var title=t.title+' · '+(t.rotation_members&&t.rotation_members.length?'Turnus '+t.rotation_members.map(nameOf).join(' / '):'Fast: '+nameOf(t.assigned_to));
          row.appendChild(el('span',null,title+' · ansvar på '+data.day+': '+nameOf(t.owner_today)+' · '+(c.repeatLabel(t.repeat_rule)||'Én gang')+' · fra '+t.due_date+' · '+t.completed+' afkrydsninger · '+(t.plan_id?'plan '+t.plan_id.slice(0,8):'manuelt oprettet')+' · serie '+t.id.slice(0,8)+(t.inactive_from?' · stoppet fra '+t.inactive_from:'')));
          block.appendChild(row);selected.push({check:check,task:t});
        });$('taskDiagnostics').appendChild(block);
      });
      if(!data.tasks.length)$('taskDiagnostics').textContent='Ingen opgaveserier.';
      $('retireSave').disabled=!c.isAdmin();
      $('retireSave').addEventListener('click',async function(){
        var chosen=selected.filter(function(x){return x.check.checked}),from=$('retireDate').value;
        if(!chosen.length){$('retireStatus').textContent='Vælg mindst én opgaveserie.';return}
        if(!confirm('Stop disse '+chosen.length+' serier fra '+from+'?\n\n'+chosen.map(function(x){return x.task.title+' · '+nameOf(x.task.assigned_to)+' · '+x.task.id.slice(0,8)}).join('\n')+'\n\nAfkrydsningshistorik slettes ikke.'))return;
        var snapshots={};chosen.forEach(function(x){snapshots[x.task.id]=x.task.snapshot});$('retireSave').disabled=true;
        try{var result=await api('/api/tasks/retire','POST',{ids:chosen.map(function(x){return x.task.id}),from:from,snapshots:snapshots});c.closeModal();status(result.stopped+' serier stoppet fra '+result.from+'. Historikken er bevaret.');await c.load()}
        catch(e){$('retireStatus').textContent=e.message;$('retireSave').disabled=false}
      });
    }
    $('taskDiagnosticsBtn').addEventListener('click',openDiagnostics);
    return {refresh:refresh,openQuickstart:openQuickstart,approvalControls:approvalControls,requestID:requestID};
  }
  var api={setup:setup,requestID:requestID,sourceLabel:sourceLabel};
  if(typeof module==='object'&&module.exports)module.exports=api;else root.FamilyHubPlanningTools=api;
})(typeof window==='object'?window:this);
