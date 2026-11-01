// Exercise real fetch/decode/cache paths with a controllable transport. No GPU
// or browser performance claim; scheduling is asserted without timing races.
import assert from 'node:assert/strict';
import {registerHooks} from 'node:module';
import {createHash} from 'node:crypto';
registerHooks({resolve(s,c,next){return s==='three'?{url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true}:next(s,c);}});
const THREE=await import('../web/vendor/three.module.js');
const {StreamingWorld}=await import('../web/src/world/StreamingWorld.js');
const tick=()=>new Promise(resolve=>setTimeout(resolve,0));
async function until(predicate){for(let i=0;i<100;i++){if(predicate())return;await tick();}assert.fail('asynchronous operation did not settle');}
function fixture(){
 const ship={scene:new THREE.Scene(),player:new THREE.Vector3(),mode:'explore',active:true,abortController:new AbortController(),crew:{register(){},unregister(){}},schematicGroup:new THREE.Group(),updateState(){}};
 const stream=new StreamingWorld(ship);stream.meta={version:'fixture',chunk_size:8,limit:100000,world_bytes:1000000};return {ship,stream};
}
function resource(id){const bytes=Buffer.from(JSON.stringify({body:{uuid:id,type:'Group',children:[]}}));return {bytes,meta:{id,kind:'objects',size:bytes.length,hash:createHash('sha256').update(bytes).digest('hex')}};}
const resources=Array.from({length:18},(_,i)=>resource('resource-'+i));
globalThis.window={dispatchEvent(){}};

// A slow first request cannot block the other five workers. Reservation bytes
// count every request from dispatch until verified data enters the cache.
{
 const {stream}=fixture(),pending=new Map();let started=0;
 globalThis.fetch=(url)=>new Promise(resolve=>{const id=url.split('/').at(-1);started++;pending.set(id,()=>{pending.delete(id);resolve(new Response(resources.find(r=>r.meta.id===id).bytes));});});
 const budget=resources.reduce((sum,r)=>sum+r.meta.size,0);
 const task=stream.download(resources.map(r=>r.meta),budget);
 assert.equal(started,6);assert.equal(pending.size,6);assert.equal(stream.reserved,resources.slice(0,6).reduce((sum,r)=>sum+r.meta.size,0));
 pending.get('resource-1')();await until(()=>started===7);
 assert.ok(pending.has('resource-0'),'slow first download is still in flight');
 while(started<resources.length||pending.size){for(const release of [...pending.values()])release();await tick();}
 await task;
 assert.equal(stream.cache.size,18);assert.equal(stream.C,budget);assert.equal(stream.reserved,0);assert.ok(stream.peak<=budget);
}

// A failed request waits for all outstanding work before rejecting. No worker
// from the failed plan can mutate the cache after the caller starts a retry.
{
 const {stream}=fixture(),pending=new Map();let started=0,settled=false;
 globalThis.fetch=(url)=>new Promise(resolve=>{const id=url.split('/').at(-1);started++;pending.set(id,(status=200)=>{pending.delete(id);resolve(new Response(resources.find(r=>r.meta.id===id).bytes,{status}));});});
 const task=stream.download(resources.map(r=>r.meta),100000).then(()=>assert.fail('failure expected'),error=>{assert.match(error.message,/Область обновилась/);settled=true;});
 pending.get('resource-0')(403);await tick();assert.equal(settled,false);assert.equal(started,6);
 for(const release of [...pending.values()])release();await task;
 assert.equal(stream.reserved,0);assert.equal(started,6);const retained=stream.C;await tick();assert.equal(stream.C,retained);
}

// Both secure-context native SHA and ordinary LAN fallback reject equal-sized
// corrupt bodies; a size mismatch and an over-budget request never enter cache.
const nativeCrypto=globalThis.crypto;
for(const native of [true,false]){
 Object.defineProperty(globalThis,'crypto',{value:native?nativeCrypto:undefined,configurable:true});
 const {stream}=fixture(),entry=resources[0],tampered=Buffer.from(entry.bytes);tampered[tampered.length-2]^=1;
 globalThis.fetch=async()=>new Response(tampered);
 await assert.rejects(stream.get(entry.meta,100000),/Повреждён ресурс мира/);assert.equal(stream.C,0);assert.equal(stream.reserved,0);
 globalThis.fetch=async()=>new Response(entry.bytes.subarray(1));
 await assert.rejects(stream.get(entry.meta,100000),/Размер ресурса/);assert.equal(stream.cache.size,0);assert.equal(stream.reserved,0);
 let calls=0;globalThis.fetch=async()=>{calls++;return new Response(entry.bytes);};
 await assert.rejects(stream.get(entry.meta,entry.meta.size-1),/бюджет/);assert.equal(calls,0);
 await stream.get(entry.meta,100000);assert.equal(stream.C,entry.meta.size);assert.equal(stream.cache.get(entry.meta.id).body.uuid,entry.meta.id);
}
Object.defineProperty(globalThis,'crypto',{value:nativeCrypto,configurable:true});

// Disposing a scene cancels pending fetches and releases their reservations.
{
 const {ship,stream}=fixture();let pending=0;
 globalThis.fetch=(_url,{signal})=>new Promise((_resolve,reject)=>{pending++;signal.addEventListener('abort',()=>reject(new DOMException('aborted','AbortError')),{once:true});});
 const task=stream.download(resources.map(r=>r.meta),100000);assert.equal(pending,6);ship.disposed=true;ship.abortController.abort();
 await assert.rejects(task,{name:'AbortError'});assert.equal(stream.reserved,0);assert.equal(stream.C,0);assert.equal(stream.cache.size,0);
}

// CPU assembly yields to a real task (including hidden tabs), retains parsed
// objects across identical plans, and does not re-download resident resources.
{
 const {stream}=fixture();let requests=0,yields=0;
 const geometry={id:'geometry',kind:'geometries',size:1,hash:''};
 const plan={version:'fixture',position:{x:0,z:0},budget:100000,resources:[geometry,...resources.map(r=>r.meta)],chunks:resources.map(r=>({object:r.meta.id}))};
 stream.cache.set('geometry',{...geometry,body:{uuid:'geometry',type:'BoxGeometry',width:1,height:1,depth:1}});stream.C=1;
 globalThis.document={hidden:true};globalThis.requestAnimationFrame=()=>assert.fail('hidden-tab scheduling must not use requestAnimationFrame');
 globalThis.fetch=async(url)=>{if(url==='/api/world/interest')return new Response(JSON.stringify(plan));requests++;return new Response(resources.find(r=>r.meta.id===url.split('/').at(-1)).bytes);};
 const parse=stream.loader.parseObject.bind(stream.loader);let parses=0;
 stream.loader.parseObject=(...args)=>{parses++;stream.sliceStart=-Infinity;return parse(...args);};
 globalThis.scheduler={async yield(){yields++;await tick();}};
 await stream.load();assert.equal(stream.objects.size,18);assert.equal(requests,18);assert.equal(parses,18);assert.ok(yields>=17);assert.ok(stream.parsed.geometries.geometry);
 const firstRoot=stream.objects.values().next().value;await stream.load();assert.equal(stream.objects.values().next().value,firstRoot);assert.equal(requests,18);assert.equal(parses,18);
 delete globalThis.scheduler;stream.sliceStart=-Infinity;await stream.yieldWork();
}

// A delayed replacement keeps the previous room visible. The first batch is
// attached while the final distant chunk is still waiting on its response.
{
 const {ship,stream}=fixture();
 const make=(id,name)=>{const bytes=Buffer.from(JSON.stringify({body:{uuid:id,type:'Group',name,children:[]}}));return{bytes,meta:{id,kind:'objects',size:bytes.length,hash:createHash('sha256').update(bytes).digest('hex')}};};
 const previous=make('old-room','room'),next=Array.from({length:17},(_,i)=>make('new-'+i,i===0?'room':'distant-'+i));
 const plan=items=>({version:stream.meta.version,position:{x:0,z:0},budget:100000,resources:items.map(r=>r.meta),chunks:items.map(r=>({object:r.meta.id,deps:[r.meta.id]}))});
 globalThis.fetch=async url=>url==='/api/world/interest'?new Response(JSON.stringify(plan([previous]))):new Response(previous.bytes);
 await stream.load();const old=stream.objects.get(previous.meta.id);assert.equal(old.parent,ship.scene);
 globalThis.fetch=async url=>url==='/api/world/interest'?new Response(JSON.stringify(plan(next))):new Response('',{status:503});
 await assert.rejects(stream.load(),/Область обновилась/);assert.equal(old.parent,ship.scene,'failed prefetch preserves existing scene');
 let firstRelease,lastRelease;
 globalThis.fetch=async url=>{
  if(url==='/api/world/interest')return new Response(JSON.stringify(plan(next)));
  const item=next.find(r=>r.meta.id===url.split('/').at(-1));
  if(item===next[0])return new Promise(resolve=>{firstRelease=()=>resolve(new Response(item.bytes));});
  if(item===next[16])return new Promise(resolve=>{lastRelease=()=>resolve(new Response(item.bytes));});
  return new Response(item.bytes);
 };
 const loading=stream.load();await until(()=>firstRelease);assert.equal(old.parent,ship.scene,'old LOD remains until replacement has downloaded');firstRelease();
 await until(()=>lastRelease);assert.equal(old.parent,null);assert.equal(stream.objects.get('new-0').parent,ship.scene);
 assert.equal(stream.objects.size,16,'near chunks become visible before distant download finishes');
 lastRelease();await loading;assert.equal(stream.objects.size,17);assert.ok(!stream.cache.has(previous.meta.id));assert.equal(stream.reserved,0);assert.ok(stream.peak<=stream.meta.limit);
}

// Deadlines include a stalled response BODY after successful HTTP headers.
// Meta/interest retries must recover; resource timeout releases all reservations.
{
 const {ship,stream}=fixture();stream.requestTimeoutMs=5;ship.schematic=()=>{};
 globalThis.fetch=async(_url,{signal})=>{const stalled=()=>new Promise((_resolve,reject)=>signal.addEventListener('abort',()=>reject(new DOMException('aborted','AbortError')),{once:true}));return {ok:true,json:stalled,arrayBuffer:stalled};};
 await assert.rejects(stream.initialize(),/время ожидания/);
 await assert.rejects(stream.load(),/время ожидания/);
 await assert.rejects(stream.download(resources.map(r=>r.meta),100000),/время ожидания/);assert.equal(stream.reserved,0);assert.equal(stream.C,0);
 globalThis.fetch=async url=>url==='/api/world/meta'?new Response(JSON.stringify({...stream.meta,rooms:[]})):new Response(resources[0].bytes);
 await stream.initialize();await stream.get(resources[0].meta,100000);assert.equal(stream.C,resources[0].meta.size);assert.equal(stream.reserved,0);
}
console.log(JSON.stringify({passed:true,checks:['bounded work-conserving request pool','in-flight byte reservations','failed plans settle before retry','native and LAN SHA verification','resource size and budget rejection','scene disposal abort','cooperative parse/attach scheduling including hidden tabs','resident object and resource reuse','meta/interest/resource body deadlines and retry'],scope:'controlled local transport; no browser FPS measurement'}));
