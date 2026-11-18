'use strict';
const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const ui=require('../web/followup-ui.js');
const read=p=>fs.readFileSync(path.join(__dirname,'..',p),'utf8');

class Element{
 constructor(tag='div'){this.tagName=tag.toUpperCase();this.className='';this.children=[];this.listeners={};this.attributes={};this._text='';this.open=false;this.disabled=false;this.classList={add:n=>this.classList.toggle(n,true),remove:n=>this.classList.toggle(n,false),contains:n=>this.className.split(' ').includes(n),toggle:(n,on)=>{const s=new Set(this.className.split(' ').filter(Boolean));if(on)s.add(n);else s.delete(n);this.className=[...s].join(' ');}}}
 set textContent(s){this._text=String(s);this.children=[]}get textContent(){return this._text+this.children.map(c=>c.textContent).join('')}
 appendChild(c){this.children.push(c);return c}replaceChildren(...c){this.children=c;this._text=''}setAttribute(k,v){this.attributes[k]=String(v)}
 addEventListener(k,v){this.listeners[k]=v}querySelector(tag){return this.all(tag)[0]||null}
 all(tag){return this.children.flatMap(c=>(c.tagName.toLowerCase()===tag?[c]:[]).concat(c.all(tag)))}
}
const occurrence=overrides=>Object.assign({task_id:'abc',key:'2026-11-16',title:'Cleaning',original_owner:'a',assigned_to:'a',due_on:'2026-11-16',policy:'carry',status:'open',performed_on:'',help_requested:false,deferred_until:'',delay_days:2,severity:'red',points:8,minutes:30,version:1},overrides);
function boot(allowed=false){
 const host=new Element(),requests=[];let filter='';
 global.document={getElementById:()=>host,createElement:tag=>new Element(tag)};
 const api=ui.setup({tablet:true,api:async(p,m,b)=>{requests.push({p,m,b});return{}},nameOf:id=>({a:'Maja',b:'Noah'}[id]||'Unknown'),filter:()=>filter,canAct:()=>allowed,afterChange:async()=>{}});
 const board={settings:{from:'2026-11-16',red_after:2},today:'2026-11-18',rest:[occurrence()],routines:[]};
 api.setBoard(board);
 return{api,board,host,requests,setFilter:v=>{filter=v;api.render()}};
}
test('completion labels separate late, on-time, unknown and excused outcomes',()=>{
 assert.equal(ui.statusText(occurrence({status:'completed',performed_on:'2026-11-18'})),'Udf\u00f8rt forsinket');
 assert.equal(ui.statusText(occurrence({status:'completed',performed_on:'2026-11-16'})),'Udf\u00f8rt til tiden');
 assert.equal(ui.statusText(occurrence({status:'completed'})),'Udf\u00f8rt (tidspunkt ukendt)');
 assert.equal(ui.statusText(occurrence({status:'excused'})),'Fritaget');
 assert.ok(!ui.statusText(occurrence({status:'superseded'})).includes('Udf'));
});
test('policy choices distinguish history, routine and actual outstanding work',()=>{
 const options=ui.policyOptions();for(const rule of ['date','routine','carry'])assert.ok(options.includes('value="'+rule+'"'));
 assert.ok(options.includes('value=""'));
});
test('tablet residual board shows deadline, responsibility and textual status without granting writes',()=>{
 const{host}=boot();assert.match(host.textContent,/Restopgaver.*1/);assert.match(host.textContent,/Maja/);assert.match(host.textContent,/2026-11-16/);assert.match(host.textContent,/2 dage/);assert.equal(host.all('button').length,0);
});
test('tablet followup scope enables only visible complete/help actions with occurrence keys',async()=>{
 const{host,requests}=boot(true);assert.equal(host.all('button').length,2);
 await host.all('button')[0].listeners.click();
 assert.equal(requests.length,1);assert.equal(requests[0].p,'followup/action');
 assert.equal(requests[0].b.key,'2026-11-16');assert.equal(requests[0].b.action,'complete');assert.equal(requests[0].b.version,1);
 assert.match(requests[0].b.request_id,/^[a-f0-9]{32}$/);assert.equal(requests[0].b.date,undefined);
});
test('deferred display retains original deadline and blocks tablet action before agreed date',()=>{
 const{api,host,board}=boot(true);board.rest=[occurrence({deferred_until:'2026-11-20',severity:'deferred'})];api.setBoard(board);
 assert.match(host.textContent,/2026-11-16/);assert.match(host.textContent,/Udskudt til 2026-11-20/);assert.equal(host.all('button').length,0);
});
test('responsibility filters and collapse state do not hide the count or change ownership',()=>{
 const{api,host,board,setFilter}=boot();board.rest.push(occurrence({task_id:'def',assigned_to:'b',original_owner:'a'}));api.setBoard(board);
 host.querySelector('details').open=false;api.render();assert.equal(host.querySelector('details').open,false);
 setFilter('b');assert.match(host.textContent,/Restopgaver.*1/);assert.match(host.textContent,/Oprindeligt ansvar: Maja/);assert.match(host.textContent,/Noah/);
});
test('routine markers match known plan shard IDs and owner, never matching titles',()=>{
 const{api,board}=boot();board.routines=[{task_id:'abc',task_ids:['abc','def'],title:'Same title',assigned_to:'a',missing:2,last_due:'2026-11-17'}];api.setBoard(board);
 assert.match(api.badge({id:'def',assigned_to:'a'}).textContent,/2/);
 assert.equal(api.badge({id:'other',title:'Same title',assigned_to:'a'}),null);
 assert.equal(api.badge({id:'def',assigned_to:'b'}),null);
});
test('logout clears previous residual data and an inactive board remains hidden',()=>{
 const{api,board,host}=boot();api.clear();assert.equal(host.all('article').length,0);assert.ok(host.classList.contains('hidden'));
 board.settings.from='';api.setBoard(board);assert.equal(host.all('article').length,0);assert.ok(host.classList.contains('hidden'));
});
test('ordinary and tablet pages load followup code and use separate optional scope',()=>{
 assert.match(read('web/index.html'),/id="followupPanel"/);assert.match(read('web/index.html'),/id="homeFollowup"/);
 assert.match(read('web/tablet.html'),/id="tabletFollowup"/);assert.match(read('web/tablet-admin.js'),/name="can_followup"/);
 assert.match(read('web/tablet.js'),/state.device.can_followup/);assert.match(read('web/app.js'),/followupTools.clear\(\)/);
 assert.ok(!read('web/followup-ui.js').includes('innerHTML=o.title'));
});
