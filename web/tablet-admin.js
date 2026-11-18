/* Admin-only management of revocable, separately scoped wall displays. */
(function(root){
 'use strict';
 function setup(c){
  var $=c.$,el=c.el,api=c.api,esc=c.esc,devices=[],generation=0;
  function status(text){$('tabletAdminStatus').textContent=text}
  function date(value){return value?new Intl.DateTimeFormat('da-DK',{dateStyle:'short',timeStyle:'short'}).format(new Date(value)):'Ikke endnu'}
  function revealCode(data){
   c.openModal('Forbind din tablet','<p>\u00c5bn <strong>/tablet</strong> p\u00e5 FamilieHubs adresse p\u00e5 den tablet, du vil forbinde. Indtast engangskoden nedenfor.</p><label class="field">Engangskode<input id="tabletPairCode" readonly autocomplete="off" spellcheck="false" value="'+esc(data.pair_code)+'"></label><p class="muted">Gyldig til '+esc(date(data.pair_expires))+'. Koden kan kun bruges \u00e9n gang. Del den ikke i chat, billeder eller offentlige links.</p><p>Tabletten f\u00e5r ikke administratoradgang. Et eksisterende almindeligt login p\u00e5 tabletten afsluttes ved parring.</p><button id="tabletCodeDone" class="primary" type="button">F\u00e6rdig</button>');
   $('tabletCodeDone').addEventListener('click',c.closeModal);
  }
  function field(label,html){return '<label class="field">'+label+html+'</label>'}
  function openEditor(device){
   var d=root.FamilyHubSaverSchedule.normalize(device&&device.display),p=device||{};
   var options='<option value="">Alle familiemedlemmer</option>'+c.S.people.map(function(person){return '<option value="'+esc(person.id)+'">'+esc(person.name)+'</option>'}).join('');
   c.openModal(device?'Redig\u00e9r tablet':'Tilf\u00f8j tablet',
    '<form id="tabletEditor" class="form-grid">'+field('Navn','<input name="name" required maxlength="80" placeholder="Fx iPad i k\u00f8kkenet" value="'+esc(p.name||'')+'">')+
    field('Standardfilter','<select name="person_filter">'+options+'</select>')+
    '<label class="field field-wide ai-check"><input type="checkbox" name="can_complete" '+(p.can_complete?'checked':'')+'> Tillad afkrydsning af dagens opgaver</label>'+
    '<label class="field field-wide ai-check"><input type="checkbox" name="can_followup" '+(p.can_followup?'checked':'')+'> Tillad afslutning og hj\u00e6lpeanmodning for synlige restopgaver</label>'+
    '<p class="muted field-wide">Alle med adgang til tabletten kan se familiens kalender og opgaver. Filteret er et visningsvalg, ikke en privatlivsgr\u00e6nse. Uden afkrydsning er tabletten skrivebeskyttet.</p>'+
    field('Tema','<select name="theme"><option value="auto">Automatisk</option><option value="light">Lys</option><option value="dark">M\u00f8rk</option></select>')+
    field('Pausesk\u00e6rm','<select name="saverMode"><option value="scheduled">Kun i tidsrummet</option><option value="always">Hele d\u00f8gnet</option></select>')+
    field('M\u00f8rkt tema fra','<input type="time" name="nightFrom" value="'+esc(d.nightFrom)+'" required>')+
    field('Lyst tema fra','<input type="time" name="nightTo" value="'+esc(d.nightTo)+'" required>')+
    field('Pausesk\u00e6rm fra','<input type="time" name="saverFrom" value="'+esc(d.saverFrom)+'" required>')+
    field('Pausesk\u00e6rm indtil','<input type="time" name="saverTo" value="'+esc(d.saverTo)+'" required>')+
    field('Inaktivitet f\u00f8r pausesk\u00e6rm','<select name="saverDelay"><option value="0">Aldrig</option><option value="5">5 minutter</option><option value="10">10 minutter</option><option value="20">20 minutter</option><option value="30">30 minutter</option></select>')+
    '<p class="muted field-wide">Sk\u00e6rmvalgene g\u00e6lder kun denne tablet. Alle klokkesl\u00e6t f\u00f8lger dansk tid. Adgangen fornyes ved brug, men kr\u00e6ver ny parring efter 30 dage uden kontakt eller senest efter 180 dage.</p>'+
    '<p id="tabletFormStatus" class="error field-wide" role="status"></p><div class="form-actions"><button type="submit" class="primary">'+(device?'Gem tablet':'Opret engangskode')+'</button></div></form>');
   var form=$('tabletEditor'),f=form.elements;f.person_filter.value=p.person_filter||'';f.theme.value=d.theme;f.saverMode.value=d.saverMode;f.saverDelay.value=String(d.saverDelay);
   form.addEventListener('submit',async function(event){event.preventDefault();var button=form.querySelector('button[type="submit"]');if(button.disabled)return;button.disabled=true;$('tabletFormStatus').textContent='';
    try{var payload={name:f.name.value.trim(),can_complete:f.can_complete.checked,can_followup:f.can_followup.checked,person_filter:f.person_filter.value,display:{theme:f.theme.value,nightFrom:f.nightFrom.value,nightTo:f.nightTo.value,saverMode:f.saverMode.value,saverFrom:f.saverFrom.value,saverTo:f.saverTo.value,saverDelay:Number(f.saverDelay.value)}};
     var result=await api(device?'/api/tablets/'+device.id:'/api/tablets',device?'PUT':'POST',payload);
     if(device){c.closeModal();status('Tabletten er opdateret. Den henter \u00e6ndringen ved n\u00e6ste opdatering.')}else{revealCode(result);status('Tablet oprettet. Brug engangskoden p\u00e5 den nye tablet.')}
     await refresh();
    }catch(e){if($('tabletFormStatus'))$('tabletFormStatus').textContent=e.message;else status(e.message);button.disabled=false}
   });
  }
  async function refresh(){
   var token=++generation,admin=c.isAdmin();$('tabletsPanel').classList.toggle('hidden',!admin);if(!admin){$('tabletDevices').replaceChildren();return}
   try{var result=await api('/api/tablets');if(token!==generation||!c.isAdmin())return;devices=result.devices||[];var list=$('tabletDevices');list.replaceChildren();
    if(!devices.length)list.appendChild(el('p',{'class':'muted'},'Ingen tablets er forbundet endnu. Opret en engangskode for hver tablet.'));
    devices.forEach(function(device){var card=el('article',{'class':'tablet-device-card'});card.appendChild(el('h3',null,device.name));card.appendChild(el('p',{'class':'muted'},device.status+' \u00b7 '+(device.can_complete?'Kan afkrydse dagens opgaver':'Kun visning')));card.appendChild(el('p',{'class':'muted'},'Senest kontakt: '+date(device.last_seen)+(device.absolute_expires?' \u00b7 Ny parring senest: '+date(device.absolute_expires):'')));
     var actions=el('div',{'class':'ai-actions'}),edit=el('button',{type:'button','class':'minor'},'Redig\u00e9r'),pair=el('button',{type:'button','class':'minor'},'Ny engangskode'),revoke=el('button',{type:'button','class':'danger'},'Fjern adgang');
     edit.addEventListener('click',function(){openEditor(device)});
     pair.addEventListener('click',async function(){if(!confirm('Opret ny kode til '+device.name+'? En eksisterende tablet-forbindelse afbrydes.'))return;pair.disabled=true;try{revealCode(await api('/api/tablets/'+device.id+'/pair','POST',{}));await refresh()}catch(e){status(e.message);pair.disabled=false}});
     revoke.disabled=!!device.revoked_at;revoke.addEventListener('click',async function(){if(!confirm('Sp\u00e6r '+device.name+'? Familiens \u00f8vrige enheder og opgaver bevares.'))return;revoke.disabled=true;try{await api('/api/tablets/'+device.id+'/revoke','POST',{});status('Adgangen til '+device.name+' er fjernet.');await refresh()}catch(e){status(e.message);revoke.disabled=false}});
     actions.appendChild(edit);actions.appendChild(pair);actions.appendChild(revoke);card.appendChild(actions);list.appendChild(card);
    });
   }catch(e){status('Kunne ikke hente tablets: '+e.message)}
  }
  $('tabletAddBtn').addEventListener('click',function(){openEditor(null)});
  $('tabletRefreshBtn').addEventListener('click',refresh);
  root.addEventListener('pagehide',function(){if($('tabletPairCode'))$('tabletPairCode').value=''});
  return {refresh:refresh};
 }
 root.FamilyHubTablets={setup:setup};
})(window);
