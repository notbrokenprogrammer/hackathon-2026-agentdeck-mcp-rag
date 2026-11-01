import assert from 'node:assert/strict';
import {UniversePanel,findSpawnPoint,describeDoor} from '../web/src/ui/UniversePanel.js';
import {layout,contains,walkable} from '../web/src/interior/DeckLayout.js';

// A dependency-free HTML/DOM fixture for event wiring and dialog lifecycle.
// It does not claim CSS layout, browser accessibility or rendering coverage.
const decode=s=>String(s).replace(/&(?:amp|lt|gt|quot|#39);/g,c=>({'&amp;':'&','&lt;':'<','&gt;':'>','&quot;':'"','&#39;':"'"}[c]));
class Element {
  constructor(tag='div',attrs={}){this.tagName=tag.toUpperCase();this.attrs=attrs;this.children=[];this.listeners={};this.parent=null;this.open=false;this._text='';this.classList={toggle:(name,on)=>{const names=new Set(this.className.split(/\s+/));on?names.add(name):names.delete(name);this.className=[...names].join(' ');}};}
  get id(){return this.attrs.id||'';}set id(v){this.attrs.id=v;}
  get className(){return this.attrs.class||'';}set className(v){this.attrs.class=v;}
  get dataset(){return Object.fromEntries(Object.entries(this.attrs).filter(([key])=>key.startsWith('data-')).map(([key,value])=>[key.slice(5).replace(/-([a-z])/g,(_,letter)=>letter.toUpperCase()),value]));}
  setAttribute(key,value){this.attrs[key]=String(value);}removeAttribute(key){delete this.attrs[key];}
  addEventListener(type,fn){this.listeners[type]=fn;}
  append(...elements){for(const el of elements){el.parent=this;this.children.push(el);}}
  prepend(el){el.parent=this;this.children.unshift(el);}
  remove(){if(this.parent)this.parent.children=this.parent.children.filter(child=>child!==this);}
  get nextElementSibling(){const index=this.parent?.children.indexOf(this);return this.parent?.children[index+1];}
  set innerHTML(html){this.children=[];this._text='';const stack=[this];for(const token of html.match(/<[^>]+>|[^<]+/g)||[]){if(token.startsWith('</')){stack.pop();continue;}if(token.startsWith('<')){const tag=token.match(/^<([\w-]+)/)?.[1];if(!tag)continue;const attrs={};const raw=token.slice(tag.length+1,-1);for(const match of raw.matchAll(/([\w-]+)(?:="([^"]*)"|='([^']*)'|=([^\s>]+))?/g))attrs[match[1]]=decode(match[2]??match[3]??match[4]??'');const el=new Element(tag,attrs);stack.at(-1).append(el);if(!['input','br','hr','img','meta','link'].includes(tag)&&!token.endsWith('/>'))stack.push(el);}else stack.at(-1)._text+=decode(token);}}
  get textContent(){return this._text+this.children.map(child=>child.textContent).join('');}set textContent(value){this._text=String(value);this.children=[];}
  get value(){if(this._value!==undefined)return this._value;if(this.tagName==='SELECT'){const options=this.querySelectorAll('option');return (options.find(option=>'selected' in option.attrs)||options[0])?.value||'';}return this.attrs.value||'';}set value(value){this._value=String(value);}
  matches(selector){if(selector.startsWith('#'))return this.id===selector.slice(1);if(selector.startsWith('.'))return this.className.split(/\s+/).includes(selector.slice(1));if(selector.startsWith('['))return selector.slice(1,-1) in this.attrs;return this.tagName===selector.toUpperCase();}
  querySelectorAll(selector){return this.children.flatMap(child=>[...(child.matches(selector)?[child]:[]),...child.querySelectorAll(selector)]);}
  querySelector(selector){return this.querySelectorAll(selector)[0]||null;}
  showModal(){this.open=true;}close(){this.open=false;this.listeners.close?.();}focus(){this.focused=true;}
}
const body=new Element('body');body.innerHTML='<div class="header-actions"></div>';
globalThis.document={body,createElement:tag=>new Element(tag),querySelector:selector=>body.querySelector(selector)};
const listeners=new Map(),calls=[],sceneCalls=[];
let room={code:'ABC123',generation:1,owner:'alice'},me={id:'alice'};
let state={room_code:'ABC123',generation:1,version:1,settings:{ambient_color:'#78e8ca',light_intensity:1,npc_count:64,simulation_speed:1},agents:[{id:'savinykh',behavior:'helpful'},{id:'dzhanibekov',behavior:'helpful'},{id:'ryazansky',behavior:'helpful'},{id:'krikalev',behavior:'helpful'}],objects:[{id:'crate-1',kind:'crate',name:'<test crate>',x:-13,y:0,z:2,held_by:''},{id:'door-1',kind:'door',name:'Door',x:-7,y:0,z:0,open:false}],last_event:{text:'Ready'}};
const emit=(type,data)=>{for(const fn of listeners.get(type)||[])fn(data);};
const clone=value=>JSON.parse(JSON.stringify(value));
const network={ready:true,on(type,fn){if(!listeners.has(type))listeners.set(type,new Set());listeners.get(type).add(fn);return()=>listeners.get(type).delete(fn);},async request(type,payload){calls.push({type,payload});if(type==='universe.get')return clone(state);if(type==='telemetry.get')return {room_code:'ABC123',generation:1,at:Date.now(),clients:[{player_id:'alice',name:'Alice',online:true,fps:58,ping_ms:12,frame_ms:17,draw_calls:100,triangles:1500,renderer:'webgl',hidden:false,stale:false,updated_at:Date.now()}]};if(type==='universe.admin'){if(payload.expected_version!==state.version)throw Object.assign(new Error('stale world'),{code:'stale_version'});if(payload.settings)Object.assign(state.settings,payload.settings);if(payload.agent)Object.assign(state.agents.find(agent=>agent.id===payload.agent.id),payload.agent);state.version++;emit('universe.state',clone(state));return clone(state);}return {message:'Done',changed:true};}};
const ship={setUniverse(...args){sceneCalls.push(args);},controls:{unlock(){}},focusWorldObject(id){this.focused=id;}};
const panel=new UniversePanel({network,ship:()=>ship,room:()=>room,user:()=>me,toast(){}});
await panel.onRoom();assert.equal(panel.state.version,1);assert.equal(panel.button.hidden,false);assert.equal(sceneCalls.at(-1)[1],'alice');
const sceneCount=sceneCalls.length;
emit('universe.state',{...clone(state),room_code:'OTHER',version:500});
emit('universe.state',{...clone(state),generation:2,version:500});
emit('universe.state',{version:500,settings:state.settings});
assert.equal(panel.state.version,1);assert.equal(sceneCalls.length,sceneCount,'foreign and unscoped pushes never reach the scene');
await panel.open();assert.equal(panel.dialog.open,true);assert.equal(panel.dialog.querySelectorAll('[data-world-action]').length,2);assert.equal(panel.dialog.querySelectorAll('[data-universe-tab]').length,4);
assert.ok(panel.body.textContent.includes('<test crate>'),'escaped object name is displayed as literal text');
assert.equal(panel.body.querySelector('test'),null,'an object name cannot create HTML elements');
const fixedDoor={id:'door-cryo',kind:'door',name:'Крио',x:-10,y:0,z:0,open:true,motion_at:Date.now()-layout.doorDuration+200};
state.objects.push(fixedDoor);state.version++;emit('universe.state',clone(state));
const actionFor=id=>panel.body.querySelectorAll('[data-world-action]').find(button=>button.dataset.objectId===id);
assert.ok(!panel.body.querySelectorAll('[data-world-remove]').some(button=>button.dataset.worldRemove==='door-cryo'),'architectural door cannot be offered for deletion');
assert.ok(panel.body.querySelectorAll('[data-world-remove]').some(button=>button.dataset.worldRemove==='door-1'),'owner can still delete a placed door');
assert.match(panel.body.textContent,/Часть корпуса/);
assert.match(actionFor('door-cryo').textContent,/Открывается/);assert.ok('disabled' in actionFor('door-cryo').attrs);
await new Promise(resolve=>setTimeout(resolve,300));
assert.equal(actionFor('door-cryo').textContent,'Закрыть');assert.ok(!('disabled' in actionFor('door-cryo').attrs),'animation end re-enables the door without another server push');
fixedDoor.locked='Нет питания';state.version++;emit('universe.state',clone(state));
assert.match(actionFor('door-cryo').textContent,/Заблокирована: Нет питания/);assert.ok('disabled' in actionFor('door-cryo').attrs);
assert.equal(describeDoor({open:false,motion_at:100},200).status,'Закрывается…');
assert.equal(describeDoor({open:false,motion_at:100},2000).status,'Закрыта');
assert.equal(describeDoor({open:true,motion_at:100},2000).status,'Открыта');
delete fixedDoor.locked;
const tab=name=>{panel.dialog.querySelectorAll('[data-universe-tab]').find(button=>button.dataset.universeTab===name).onclick();};
const settle=()=>new Promise(resolve=>setImmediate(resolve));
tab('settings');
const compartments=layout.areas.filter(area=>area.kind==='room');
assert.equal(compartments.length,7);
assert.deepEqual(panel.body.querySelector('#world-spawn-room').querySelectorAll('option').map(option=>option.value),compartments.map(area=>area.id));
for(const area of compartments) {
  for(const kind of ['door','crate','plant','beacon']) {
    const point=findSpawnPoint(area.id,state.objects,kind);
    assert.ok(point,area.id+' has a safe position for '+kind);
    const clearance=kind==='door'?1.6:kind==='plant'?1:.8;
    assert.ok(contains(area,point.x,point.z,clearance),'object stays in selected compartment');
    assert.ok(walkable(point.x,point.z,clearance),'object footprint avoids architectural collisions');
    assert.ok(!layout.gates.some(gate=>Math.hypot(point.x-gate.x,point.z-gate.z)<clearance+1.2),'object avoids doorways');
    const shifted=findSpawnPoint(area.id,[...state.objects,{id:'occupied',kind,x:point.x,z:point.z}],kind);
    assert.ok(!shifted||shifted.x!==point.x||shifted.z!==point.z,'a second object avoids the occupied location');
  }
  panel.body.querySelector('#world-spawn-room').value=area.id;
  panel.body.querySelector('#world-spawn-kind').value='crate';
  panel.body.querySelector('#universe-spawn-form').onsubmit({preventDefault(){}});await settle();
  const sent=calls.findLast(call=>call.type==='universe.admin').payload.spawn;
  assert.ok(contains(area,sent.x,sent.z),'actual form submits selected room '+area.id);
  assert.ok(walkable(sent.x,sent.z,.8));
}
assert.equal(findSpawnPoint('missing',[]),null,'unknown room never spawns elsewhere');
panel.renderSettings(); // Reopen the settings draft after the seven spawn revisions.
const field=panel.body.querySelector('#world-npc');field.value='90';
await panel.onRoom();assert.equal(panel.body.querySelector('#world-npc'),field,'ordinary room snapshots preserve edited form nodes');
panel.body.querySelector('#universe-settings-form').onsubmit({preventDefault(){}});await settle();assert.equal(state.settings.npc_count,90);
// While the owner edits NPC count, another player changes lighting through an agent.
const draft=panel.body.querySelector('#world-npc'),renderedVersion=state.version;draft.value='100';
state.settings.light_intensity=.3;state.version++;emit('universe.state',clone(state));
assert.equal(panel.body.querySelector('#world-npc'),draft,'draft remains visible until submitted');
assert.equal(panel.body.querySelector('#world-light').value,'1');
const beforeConflict=calls.length;
panel.body.querySelector('#universe-settings-form').onsubmit({preventDefault(){}});await settle();
assert.equal(calls[beforeConflict].payload.expected_version,renderedVersion);
assert.deepEqual(calls.slice(beforeConflict).map(call=>call.type),['universe.admin','universe.get'],'conflicting settings refresh once without replay');
assert.equal(state.settings.light_intensity,.3,'a stale form cannot overwrite newer lighting');assert.equal(state.settings.npc_count,90);
assert.notEqual(panel.body.querySelector('#world-npc'),draft,'a conflict rebuilds settings from the accepted snapshot');
assert.equal(panel.body.querySelector('#world-light').value,'0.3');assert.match(panel.dialog.querySelector('#universe-feedback').textContent,/ещё раз/);
panel.body.querySelector('#world-npc').value='100';panel.body.querySelector('#universe-settings-form').onsubmit({preventDefault(){}});await settle();
assert.equal(state.settings.npc_count,100);assert.equal(state.settings.light_intensity,.3);
tab('agents');const behavior=panel.body.querySelector('[data-world-behavior]');behavior.value='patrol';behavior.onchange();await settle();assert.equal(state.agents[0].behavior,'patrol');
tab('metrics');await settle();assert.ok(panel.body.textContent.includes('Alice'));assert.ok(panel.body.textContent.includes('58'));assert.ok(panel.body.textContent.includes('12 мс'));
const metrics=panel.metrics;
emit('telemetry.state',{...clone(metrics),room_code:'OTHER',clients:[]});emit('telemetry.state',{...clone(metrics),generation:2,clients:[]});
assert.equal(panel.metrics,metrics,'buffered telemetry from another room or mission is ignored');
const request=network.request;network.request=async type=>type==='telemetry.get'?{...clone(metrics),room_code:'OTHER',clients:[]}:request(type);
await panel.fetchMetrics();assert.equal(panel.metrics,metrics,'telemetry request responses are also scoped');network.request=request;
tab('objects');const input=panel.body.querySelector('#universe-command-text');input.value='Добавь растения';panel.body.querySelector('#universe-command-form').onsubmit({preventDefault(){}});await settle();assert.equal(calls.at(-1).type,'universe.command');assert.equal(calls.at(-1).payload.agent_id,'savinykh');
room={...room,owner:'bob'};await panel.onRoom();assert.equal(panel.dialog.querySelectorAll('[data-universe-tab]').length,1,'lost ownership removes admin UI');
assert.equal(panel.dialog.querySelectorAll('[data-world-remove]').length,0);
room=null;await panel.onRoom();assert.equal(panel.dialog.open,false);assert.equal(panel.button.hidden,true);assert.equal(panel.state,null);assert.equal(sceneCalls.at(-1)[0],null);
emit('universe.state',clone(state));emit('telemetry.state',clone(metrics));assert.equal(panel.state,null);assert.equal(panel.metrics,null,'late messages cannot restore logged-out state');
panel.dispose();assert.equal(body.querySelector('#universe-dialog'),null);assert.equal(body.querySelector('#universe-button'),null);
assert.ok([...listeners.values()].every(set=>set.size===0));
console.log('Universe DOM fixture: seven safe room spawns, architectural door protection, moving/locked states and timer, all tabs, live metrics, settings preservation, commands, ownership change, safe text, logout reset, disposal — passed (no browser layout assertions)');
