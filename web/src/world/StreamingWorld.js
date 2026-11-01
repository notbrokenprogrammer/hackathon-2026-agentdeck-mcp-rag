import * as THREE from 'three';
import {walkable,roomAt} from '../interior/DeckLayout.js';
import {sha256} from './sha256.js';
import {configureTexture} from '../render/TextureQuality.js';

// Keep the connection pool bounded, but do not wait for the slower request of
// every pair before starting the next one. Geometry parsing is also sliced so
// controls and rendering can run while a room is being assembled.
const DOWNLOAD_CONCURRENCY=6,WORK_SLICE_MS=4;

export class StreamingWorld{
 constructor(ship){this.ship=ship;this.scene=ship.scene;this.cache=new Map();this.objects=new Map();this.parsed={images:{},textures:{},materials:{},geometries:{}};this.loader=new THREE.ObjectLoader();this.C=0;this.reserved=0;this.peak=0;this.lastKey='';this.meta=null;this.running=false;this.errors=[];this.scope='';this.decoder=new TextDecoder();this.sliceStart=0;this.loadYields=0;this.signal=ship.abortController?.signal;this.requestTimeoutMs=20000;}
 viewArea(){return this.ship.mode==='overview'?roomAt(this.ship.target?.x??this.ship.player.x,this.ship.target?.z??this.ship.player.z):null;}
 positionKey(position){return Math.floor(position.x/this.meta.chunk_size)+','+Math.floor(position.z/this.meta.chunk_size)+':'+(roomAt(position.x,position.z)?.id||'');}
 async initialize(){this.meta=await this.request('/api/world/meta',async res=>{if(!res.ok)throw new Error('Мир недоступен');return res.json();});this.ship.schematic(this.meta.rooms);if(!this.ship.initialFocus&&this.ship.mode==='overview'){const room=[...this.meta.rooms].sort((a,b)=>Math.hypot(a.x-this.ship.player.x,a.z-this.ship.player.z)-Math.hypot(b.x-this.ship.player.x,b.z-this.ship.player.z))[0];this.ship.focus(room.id);}}
 update(position,force=false){if(this.ship.disposed||!this.ship.active)return;if(!this.meta){if(!this.initializing)this.initializing=this.initialize().then(()=>this.update(position,true)).catch(e=>{this.errors.push(e.message);this.initializing=null;});return;}const view=this.viewArea();const key=this.positionKey(view||position)+':'+(view?.id||'');if(!force&&key===this.lastKey)return;if(this.running){this.pending=true;return;}this.running=true;this.load().catch(e=>{if(e.name!=='AbortError')this.errors.push(e.message);this.lastKey='';}).finally(()=>{this.running=false;if(this.pending){this.pending=false;this.update(this.ship.player,true);}});}
 async load(){const started=performance.now();this.sliceStart=started;this.loadYields=0;const view=this.viewArea()?.id||'',query=view?'?view='+encodeURIComponent(view):'';const plan=await this.request('/api/world/interest'+query,async response=>{if(!response.ok)throw new Error('План загрузки недоступен');return response.json();});if(plan.version!==this.meta.version)throw new Error('Версия мира изменилась. Обновите страницу.');this.lastKey=this.positionKey(plan.position)+':'+view;const desired=new Set(plan.resources.map(x=>x.id));const lods=[plan.background,...plan.chunks].filter(x=>x?.object);const objectIDs=new Set(lods.map(x=>x.object));
 const todo=plan.resources.filter(x=>!this.cache.has(x.id));
 // Retain the previous view until replacements are ready. The hard limit
 // provides bounded transition headroom above the steady-state plan budget.
 if(this.C+todo.reduce((sum,r)=>sum+r.size,0)>this.meta.limit)this.discardUnused(desired,objectIDs);
 const resourcesByID=new Map(plan.resources.map(r=>[r.id,r]));
 // Publish the current room first, while more distant rooms keep downloading.
 // Server chunk order is shell -> local people/equipment -> nearby decoration.
 for(let offset=0;offset<lods.length;offset+=16){
 const batch=lods.slice(offset,offset+16),needed=new Set(batch.flatMap(lod=>lod.deps||[lod.object]));
 await this.download([...needed].filter(id=>!this.cache.has(id)).map(id=>resourcesByID.get(id)),this.meta.limit,query);
 for(const kind of ['images','textures','materials','geometries']){for(const record of this.cache.values()){if(record.kind!==kind||this.parsed[kind][record.id])continue;await this.yieldWork();const data=[record.body];let result;if(kind==='images')result=this.loader.parseImages(data,()=>{});if(kind==='textures'){result=this.loader.parseTextures(data,this.parsed.images);for(const texture of Object.values(result))configureTexture(texture,this.ship.renderer);}if(kind==='materials')result=this.loader.parseMaterials(data,this.parsed.textures);if(kind==='geometries')result=this.loader.parseGeometries(data,{});Object.assign(this.parsed[kind],result);record.body=null;}}
 for(const lod of batch){if(this.objects.has(lod.object))continue;const data=this.cache.get(lod.object)?.body;if(!data)continue;await this.yieldWork();const root=this.loader.parseObject(data,this.parsed.geometries,this.parsed.materials,this.parsed.textures,{});root.userData.chunk=lod.object;
 // A new LOD replaces the matching old chunk in the same task, avoiding a
 // blank frame and avoiding two overlapping copies between cooperative yields.
 for(const[id,old]of this.objects)if(!objectIDs.has(id)&&((root.name&&old.name===root.name)||(root.userData.npcRoot&&old.userData.npcRoot===root.userData.npcRoot)))this.removeObject(id,old);
 if(root.userData.interiorChunk)this.ship.interior.attachChunk(root);else this.scene.add(root);const special=[];root.traverse(o=>{if(o.userData.playerTemplate||o.userData.schematicRoot)special.push(o);});for(const o of special){if(o.userData.playerTemplate)this.ship.installAvatarTemplate(o);if(o.userData.schematicRoot)this.ship.installSchematic(o);}this.objects.set(lod.object,root);this.ship.crew.register(root);
 }
 }
 this.discardUnused(desired,objectIDs);
 this.ship.updateState(this.ship.lastState||{});this.ship.schematicGroup.visible=this.ship.mode==='overview';this.lastLoadMs=performance.now()-started;this.report();
 }
 removeObject(id,node){this.ship.interior?.detachChunk(node);node.removeFromParent();this.ship.crew.unregister(node);this.objects.delete(id);}
 discardUnused(desired,objectIDs){
  for(const[id,node]of this.objects)if(!objectIDs.has(id))this.removeObject(id,node);
  for(const[id,record]of this.cache)if(!desired.has(id)){const item=this.parsed[record.kind]?.[id];if(['geometries','materials','textures'].includes(record.kind))item?.dispose();if(record.kind==='images')item?.data?.close?.();if(this.parsed[record.kind])delete this.parsed[record.kind][id];this.cache.delete(id);this.C-=record.size;}
 }
 async request(url,read){
  const controller=new AbortController();let timedOut=false;
  const cancel=()=>controller.abort();
  if(this.ship.disposed||this.signal?.aborted)cancel();else this.signal?.addEventListener('abort',cancel,{once:true});
  const timer=setTimeout(()=>{timedOut=true;controller.abort();},this.requestTimeoutMs);
  try{
   const response=await fetch(url,{cache:'no-store',signal:controller.signal});
   // Keep the deadline alive through the body stream, not just HTTP headers.
   return await read(response);
  }catch(error){if(timedOut)throw new Error('Превышено время ожидания сервера мира');throw error;}
  finally{clearTimeout(timer);this.signal?.removeEventListener('abort',cancel);}
 }
 async download(resources,budget,query=''){
  let cursor=0,failure;
  const worker=async()=>{while(!failure&&cursor<resources.length){const resource=resources[cursor++];try{await this.get(resource,budget,query);}catch(error){failure=error;}}};
  // Wait for every in-flight reservation to settle even if one request fails.
  // A retry must never race a worker that is still filling the old plan.
  await Promise.all(Array.from({length:Math.min(DOWNLOAD_CONCURRENCY,resources.length)},worker));
  if(failure)throw failure;
 }
 async yieldWork(){
  if(this.ship.disposed||this.signal?.aborted)throw new DOMException('Загрузка остановлена','AbortError');
  if(performance.now()-this.sliceStart<WORK_SLICE_MS)return;
  this.loadYields++;
  if(globalThis.scheduler?.yield)await globalThis.scheduler.yield();else await new Promise(resolve=>setTimeout(resolve,0));
  this.sliceStart=performance.now();
  if(this.ship.disposed||this.signal?.aborted)throw new DOMException('Загрузка остановлена','AbortError');
 }
 async get(resource,budget,query=''){if(this.C+this.reserved+resource.size>Math.min(budget,this.meta.limit))throw new Error('Ресурс не помещается в бюджет мира');this.reserved+=resource.size;this.observe();try{const bytes=await this.request('/api/world/resource/'+encodeURIComponent(resource.id)+query,async response=>{if(!response.ok)throw new Error('Область обновилась; ресурс будет запрошен заново');return new Uint8Array(await response.arrayBuffer());});if(bytes.byteLength!==resource.size)throw new Error('Размер ресурса отличается от manifest');let hash;if(globalThis.crypto?.subtle){const digest=await globalThis.crypto.subtle.digest('SHA-256',bytes);hash=Array.from(new Uint8Array(digest),v=>v.toString(16).padStart(2,'0')).join('');}else{await this.yieldWork();hash=sha256(bytes);}if(hash!==resource.hash)throw new Error('Повреждён ресурс мира');if(this.ship.disposed||this.signal?.aborted)throw new DOMException('Загрузка остановлена','AbortError');const value=JSON.parse(this.decoder.decode(bytes));this.cache.set(resource.id,{...resource,body:value.body});this.C+=resource.size;}finally{this.reserved-=resource.size;this.observe();}}
 observe(){this.peak=Math.max(this.peak,this.C+this.reserved);if(this.meta&&this.C+this.reserved>this.meta.limit)throw new Error('Превышен лимит кеша мира');}
 report(){const stats={world_bytes:this.meta.world_bytes,resident_bytes:this.C,reserved_bytes:this.reserved,peak_bytes:this.peak,ratio:this.C/this.meta.world_bytes,peak_ratio:this.peak/this.meta.world_bytes,limit_ratio:this.meta.limit/this.meta.world_bytes,resources:this.cache.size,chunks:this.objects.size,load_ms:Math.round(this.lastLoadMs||0),load_yields:this.loadYields,errors:this.errors.slice(-4)};this.stats=stats;window.dispatchEvent(new CustomEvent('world-cache',{detail:stats}));}
 walkable(x,z){return walkable(x,z);}
}
