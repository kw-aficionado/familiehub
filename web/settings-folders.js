/* Native, keyboard-accessible sections; open/closed state is per browser. */
(function(root){
 'use strict';
 var key='familiehub-settings-sections-v1';
 function readState(storage){try{var v=JSON.parse(storage.getItem(key)||'{}');return v&&typeof v==='object'&&!Array.isArray(v)?v:{}}catch(e){return {}}}
 function sectionKey(panel){return panel.id||panel.getAttribute('aria-label')||(panel.querySelector('h2')||{}).textContent||''}
 function install(document,storage){
  var settings=document.getElementById('settingsPage');if(!settings)return;
  var state=readState(storage);
  function persist(){try{storage.setItem(key,JSON.stringify(state))}catch(e){}}
  function refresh(){
   settings.querySelectorAll('.settings-panel').forEach(function(panel){
    var existing=panel.closest('details.settings-section');if(existing){existing.hidden=panel.classList.contains('hidden');return;}
    var id=sectionKey(panel),heading=panel.querySelector('h2');if(!id||!heading)return;
    var details=document.createElement('details'),summary=document.createElement('summary');
    details.className='settings-section';details.hidden=panel.classList.contains('hidden');details.setAttribute('data-section',id);details.open=state[id]!==false;
    summary.textContent=heading.textContent;details.appendChild(summary);
    panel.parentNode.insertBefore(details,panel);details.appendChild(panel);
    details.addEventListener('toggle',function(){state[id]=details.open;persist()});
   });
  }
  refresh();
  ['settingsExpandAll','settingsCollapseAll'].forEach(function(id){var button=document.getElementById(id);if(button)button.addEventListener('click',function(){settings.querySelectorAll('details.settings-section').forEach(function(d){d.open=id==='settingsExpandAll';state[d.getAttribute('data-section')]=d.open});persist()})});
  if(root.MutationObserver)new root.MutationObserver(refresh).observe(settings,{childList:true,subtree:true,attributes:true,attributeFilter:['class']});
 }
 function reveal(node){for(var p=node;p;p=p.parentElement){if(p.tagName==='DETAILS')p.open=true}}
 var api={install:install,readState:readState,sectionKey:sectionKey,reveal:reveal};
 if(typeof module==='object'&&module.exports)module.exports=api;
 else{root.FamilyHubSettings=api;document.addEventListener('DOMContentLoaded',function(){var storage;try{storage=root.localStorage}catch(e){storage={getItem:function(){return null},setItem:function(){}}}install(document,storage)})}
})(typeof window==='object'?window:this);
