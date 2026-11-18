/* Occurrence followup shared by the ordinary app and scoped tablet display. */
(function(root){
 'use strict';
 var labels={date:'Kun historik for den oprindelige dag',routine:'Daglig rutine med historikmarkering',carry:'Behold som restopgave',open:'Mangler afkrydsning',completed:'Udf\u00f8rt',excused:'Fritaget',superseded:'Erstattet / ikke n\u00f8dvendig',on_time:'Til tiden',late:'Forsinket',completed_unknown:'Udf\u00f8rt, tidspunkt ukendt',missing:'Mangler',deferred:'Udskudt'};
 function statusText(o){if(o.status==='completed')return o.performed_on?(o.performed_on>o.due_on?'Udf\u00f8rt forsinket':'Udf\u00f8rt til tiden'):'Udf\u00f8rt (tidspunkt ukendt)';return labels[o.status]||'Mangler afkrydsning'}
 function policyOptions(){return '<option value="">Automatisk ud fra gentagelse</option><option value="date">Kun historik for dagen</option><option value="routine">Rutine: vis manglende afkrydsninger</option><option value="carry">Behold som restopgave</option>'}
 function requestID(){var b=new Uint32Array(4);root.crypto.getRandomValues(b);return Array.from(b).map(function(n){return n.toString(16).padStart(8,'0')}).join('')}
 function setup(c){
  var board=null,generation=0,settingsInitialized=false;
  function $(id){return document.getElementById(id)}
  function n(tag,text,cls){var e=document.createElement(tag);if(text!==undefined)e.textContent=text;if(cls)e.className=cls;return e}
  function name(id){return c.nameOf(id)||'Tidligere / ukendt person'}
  function call(path,method,body){return c.api(c.tablet?'followup/'+path:'/api/followup'+(path?'/'+path:''),method,body)}
  function button(label,fn,cls){var b=n('button',label,cls||'minor');b.type='button';b.addEventListener('click',fn);return b}
  function canAct(o){return c.canAct?c.canAct(o):c.isAdmin()||!!(c.S.account&&c.S.account.person_id&&c.S.account.person_id===o.assigned_to)}
  function isAdmin(){return !c.tablet&&c.isAdmin()}
  async function act(o,action,b,status){
   var payload={task_id:o.task_id,key:o.key,version:o.version,action:action,request_id:requestID()};b.disabled=true;status.textContent='Gemmer...';
   try{await call('action','POST',payload);status.textContent='Gemt';await c.afterChange();if(!c.tablet)await refresh()}
   catch(e){status.textContent=e.message;status.classList.add('error');b.disabled=false}
  }
  function detail(o){
   var esc=c.esc,options='<option value="">Ikke angivet</option>'+(c.S.people||[]).map(function(p){return '<option value="'+esc(p.id)+'">'+esc(p.name)+'</option>'}).join('');
   c.openModal('Vurder opgave', '<p><strong>'+esc(o.title)+'</strong></p><p>Oprindelig frist: '+esc(o.due_on)+' \u00b7 Oprindeligt ansvar: '+esc(name(o.original_owner))+'</p>'+
    '<form id="followupActionForm" class="form-grid"><label class="field field-wide">Handling<select name="action"><option value="complete">Udf\u00f8rt, eventuelt glemt afkrydsning</option><option value="defer">Aftal ny frist</option><option value="reassign">Overdrag denne forekomst</option><option value="excuse">Fritag denne gang</option><option value="supersede">Erstattet / ikke l\u00e6ngere n\u00f8dvendig</option></select></label><label class="field" id="followupDateField">Dato<input name="date" type="date" value="'+esc(board.today)+'"></label><label class="field" id="followupPersonField">Hvem udf\u00f8rte den / ny ansvarlig?<select name="person_id">'+options+'</select></label><label class="field field-wide">Begrundelse (p\u00e5kr\u00e6vet ved fritagelse/erstatning)<input name="note" maxlength="300"></label><p class="muted field-wide">Kun denne forekomst \u00e6ndres. Frist, oprindeligt ansvar og tidligere handlinger bevares. Erstatning markerer ikke en anden opgave som udf\u00f8rt.</p><p id="followupActionStatus" class="error field-wide" role="status"></p><div class="form-actions"><button type="submit" class="primary">Godkend og gem</button><button id="followupHistoryBtn" type="button" class="minor">Se historik</button></div></form><div id="followupAudit"></div>');
   var f=$('followupActionForm'),reqID=requestID();
   function fields(){var a=f.elements.action.value;$('followupDateField').classList.toggle('hidden',a!=='complete'&&a!=='defer');$('followupPersonField').classList.toggle('hidden',a!=='complete'&&a!=='reassign')}
   f.elements.action.addEventListener('change',fields);fields();
   f.addEventListener('submit',async function(e){e.preventDefault();var a=f.elements.action.value,b=f.querySelector('button[type="submit"]'),status=$('followupActionStatus');if(b.disabled)return;
    var payload={task_id:o.task_id,key:o.key,version:o.version,action:a,note:f.elements.note.value,request_id:reqID};if(a==='complete'||a==='defer')payload.date=f.elements.date.value;if(a==='complete'||a==='reassign')payload.person_id=f.elements.person_id.value;
    b.disabled=true;status.textContent='Gemmer...';try{await call('action','POST',payload);c.closeModal();await c.afterChange();await refresh()}catch(err){status.textContent=err.message;b.disabled=false}
   });
   $('followupHistoryBtn').addEventListener('click',async function(){var box=$('followupAudit');box.replaceChildren();try{var events=await call('history?task_id='+encodeURIComponent(o.task_id)+'&key='+encodeURIComponent(o.key));if(!events.length)box.appendChild(n('p','Ingen manuelle handlinger endnu.','muted'));events.forEach(function(event){var text=event.recorded_at+' \u00b7 '+event.action+' \u00b7 '+event.actor;try{var d=JSON.parse(event.detail);if(d.note)text+=' \u00b7 '+d.note}catch(e){}box.appendChild(n('p',text,'muted'))})}catch(e){box.textContent=e.message}});
  }
  function card(o,review){
   var row=n('article',undefined,'followup-card');row.setAttribute('data-task-id',o.task_id);row.setAttribute('data-occurrence',o.key);
   var title=n('strong',o.title);row.appendChild(title);
   row.appendChild(n('p',name(o.assigned_to)+' \u00b7 Oprindelig frist '+o.due_on,'muted'));
   if(o.original_owner!==o.assigned_to)row.appendChild(n('small','Oprindeligt ansvar: '+name(o.original_owner),'muted'));
   var text=statusText(o),color='neutral';if(o.status==='open'){color=o.severity||'orange';text=o.deferred_until&&o.deferred_until>=board.today?'Udskudt til '+o.deferred_until:o.delay_days+' '+(o.delay_days===1?'dag':'dage')+' efter fristen';if(o.help_requested)text+=' \u00b7 Hj\u00e6lp \u00f8nskes'}
   row.appendChild(n('span',text,'followup-badge '+color));
   if(o.status==='completed'&&o.performed_on)row.appendChild(n('small','Udf\u00f8rt '+o.performed_on+(o.performed_by?' af '+name(o.performed_by):''),'muted'));
   row.appendChild(n('small',o.points+' point \u00b7 '+o.minutes+' min. (t\u00e6lles kun \u00e9n gang)','muted'));
   var status=n('p','','followup-status');status.setAttribute('role','status');
   if(o.status==='open'&&canAct(o)&&(!c.tablet||!o.deferred_until||o.deferred_until<=board.today)){
    var actions=n('div',undefined,'ai-actions'),done=button('Udf\u00f8rt nu',function(){act(o,'complete',done,status)},'primary');actions.appendChild(done);
    var help=button(o.help_requested?'Hj\u00e6lp er efterspurgt':'Bed om hj\u00e6lp',function(){act(o,'help',help,status)});help.disabled=o.help_requested;actions.appendChild(help);
    if(isAdmin())actions.appendChild(button('Vurder / udskyd',function(){detail(o)}));row.appendChild(actions);
   }else if(isAdmin()&&review){row.appendChild(button('Se historik',function(){detail(o);$('followupActionForm').querySelector('button[type="submit"]').disabled=true;$('followupHistoryBtn').click()}))}
   row.appendChild(status);return row;
  }
  function renderBoard(id,filter){var host=$(id);if(!host)return;var previous=host.querySelector('details'),open=previous?previous.open:true;host.replaceChildren();
   if(!board||!board.settings.from){host.classList.add('hidden');return}host.classList.remove('hidden');
   var items=board.rest.filter(function(o){return !filter||o.assigned_to===filter}),details=n('details');details.open=open;
   details.appendChild(n('summary','Restopgaver \u00b7 '+items.length));details.appendChild(n('p','Aktuel opf\u00f8lgning pr. '+board.today+'. En manglende afkrydsning er ikke n\u00f8dvendigvis manglende udf\u00f8relse.','muted'));
   if(!items.length)details.appendChild(n('p','Ingen restopgaver.','muted'));
   var grid=n('div',undefined,'followup-grid');items.forEach(function(o){grid.appendChild(card(o,false))});details.appendChild(grid);
   var routines=board.routines.filter(function(g){return !filter||g.assigned_to===filter});if(routines.length){var routine=n('details',undefined,'followup-routines');routine.appendChild(n('summary','Rutiners historik \u00b7 '+routines.reduce(function(sum,g){return sum+g.missing},0)+' manglende afkrydsninger (7 dage)'));routines.forEach(function(g){routine.appendChild(n('p',g.title+' \u00b7 '+name(g.assigned_to)+' \u00b7 '+g.missing+' mangler, senest '+g.last_due,'muted'))});details.appendChild(routine)}
   if(board.warning)details.appendChild(n('p',board.warning,'warning'));host.appendChild(details);
  }
  function render(){if(c.tablet){renderBoard('tabletFollowup',c.filter());return}renderBoard('homeFollowup',c.S.filter);renderBoard('tasksFollowup',c.S.taskFilter)}
  function badge(task){if(!board)return null;if(task.followup_status==='excused'||task.followup_status==='superseded')return n('small',labels[task.followup_status],'followup-badge neutral');var groups=board.routines.filter(function(g){return (g.task_id===task.id||(g.task_ids||[]).indexOf(task.id)!==-1)&&g.assigned_to===task.assigned_to});if(!groups.length)return null;return n('small','Mangler '+groups[0].missing+' tidligere afkrydsninger (7 dage)','followup-badge orange')}
  function renderAdmin(){
   if(c.tablet||!$('followupPanel'))return;$('followupPanel').classList.toggle('hidden',!isAdmin());if(!isAdmin()||!board)return;
   var form=$('followupSettings');if(!settingsInitialized){form.elements.from.value=board.settings.from||board.today;form.elements.from.disabled=!!board.settings.from;form.elements.red_after.value=String(board.settings.red_after);settingsInitialized=true}
   $('followupEnabledText').textContent=board.settings.from?'Opf\u00f8lgning fra '+board.settings.from+'. Oprindelige frister og historik bevares.':'V\u00e6lg f\u00f8rst reglerne nedenfor, og aktiv\u00e9r fra en valgt dato. Intet bliver automatisk gjort til restopgaver ved opdatering.';
   var report=$('followupReport');report.replaceChildren();if(!board.settings.from)return;
   report.appendChild(n('h3','Voksenoverblik \u00b7 '+(board.report_from>board.report_until?'Ingen afsluttede frister endnu':board.report_from+' til '+board.report_until)));report.appendChild(n('p','Kun afsluttede frister. Point tilskrives oprindeligt ansvar og t\u00e6lles \u00e9n gang. Fritaget og erstattet er ikke udf\u00f8rt.','muted'));
   var keys=['on_time','late','missing','deferred','excused','superseded','completed_unknown'];
   board.summary.forEach(function(s){var article=n('article',undefined,'followup-summary');article.appendChild(n('strong',name(s.person_id)));keys.forEach(function(k){if(s.counts[k])article.appendChild(n('span',labels[k]+': '+s.counts[k]+' ('+s.points[k]+' point)'))});report.appendChild(article)});
   if(!board.summary.length)report.appendChild(n('p','Ingen afsluttede frister i perioden endnu.','muted'));
   var review=n('details');review.appendChild(n('summary','Gennemg\u00e5 forekomster og historik ('+board.review.length+')'));var list=n('div',undefined,'followup-grid'),visible=0;
   function more(){var end=Math.min(visible+30,board.review.length);for(;visible<end;visible++)list.appendChild(card(board.review[visible],true));loadMore.disabled=visible>=board.review.length;loadMore.textContent=visible>=board.review.length?'Alle viste':'Vis flere'}
   var loadMore=button('Vis flere',more);more();review.appendChild(list);review.appendChild(loadMore);report.appendChild(review);
  }
  async function policies(){if(c.tablet||!isAdmin())return;var items=await call('policies'),host=$('followupPolicies');host.replaceChildren();
   items.forEach(function(task){var row=n('div',undefined,'followup-policy-row');row.appendChild(n('strong',task.title));row.appendChild(n('small',name(task.assigned_to)+' \u00b7 '+task.id.slice(0,8),'muted'));var select=n('select');select.innerHTML=policyOptions();select.value=task.policy;select.setAttribute('aria-label','Regel for '+task.title);row.appendChild(select);var msg=n('span','','muted');msg.setAttribute('role','status');var save=button('Gem regel',async function(){save.disabled=true;try{await call('policies/'+task.id,'PUT',{policy:select.value});msg.textContent='Gemt for nye forekomster. Eksisterende historik bevares.'}catch(e){msg.textContent=e.message}save.disabled=false});row.appendChild(save);row.appendChild(msg);host.appendChild(row)});
  }
  async function refresh(){if(c.tablet)return;var g=++generation;try{var b=await call('');if(g!==generation)return;board=b;render();renderAdmin();if(c.onBoard)c.onBoard()}catch(e){if($('followupSettingsStatus'))$('followupSettingsStatus').textContent='Opf\u00f8lgning kunne ikke opdateres: '+e.message}}
  if(!c.tablet){
   $('followupSettings').addEventListener('submit',async function(e){e.preventDefault();var form=e.currentTarget,status=$('followupSettingsStatus'),b=form.querySelector('button');if(!board)return;if(!board.settings.from&&!confirm('Start opf\u00f8lgning fra '+form.elements.from.value+'? Eksisterende afkrydsninger bevares. Gennemg\u00e5 reglerne f\u00f8rst.'))return;b.disabled=true;try{await call('settings','PUT',{from:form.elements.from.value,red_after:Number(form.elements.red_after.value)});settingsInitialized=false;status.textContent='Opf\u00f8lgning er gemt.';await refresh();await c.afterChange()}catch(err){status.textContent=err.message}b.disabled=false});
   $('followupRefreshBtn').addEventListener('click',function(){refresh();policies().catch(function(e){$('followupSettingsStatus').textContent=e.message})});
  }
  return {refresh:refresh,render:render,renderAdmin:renderAdmin,policies:policies,badge:badge,setBoard:function(b){board=b;render()},clear:function(){board=null;generation++;settingsInitialized=false;render();if(!c.tablet){if($('followupReport'))$('followupReport').replaceChildren();if($('followupPolicies'))$('followupPolicies').replaceChildren();if($('followupPanel'))$('followupPanel').classList.add('hidden')}}};
 }
 var api={setup:setup,policyOptions:policyOptions,statusText:statusText};root.FamilyHubFollowup=api;if(typeof module==='object'&&module.exports)module.exports=api;
})(typeof window==='undefined'?globalThis:window);
