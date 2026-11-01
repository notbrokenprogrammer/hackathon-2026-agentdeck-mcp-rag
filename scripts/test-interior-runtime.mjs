import assert from 'node:assert/strict';
import {registerHooks} from 'node:module';
registerHooks({resolve(s,c,next){return s==='three'?{url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true}:next(s,c);}});
const THREE=await import('../web/vendor/three.module.js');
const {ShipInterior}=await import('../web/src/interior/ShipInterior.js');

const ship={scene:new THREE.Scene(),renderer:{isSoftware:true},player:new THREE.Vector3(0,1.7,4),mode:'explore',livingWorld:{objects:new Map()},lastState:{reactor_active:false,cooling_repaired:false,greenhouse_active:false}};
const interior=new ShipInterior(ship,{streamed:true});
const geometry=new THREE.BoxGeometry(1,1,1),reactor=new THREE.MeshStandardMaterial();reactor.userData.system='reactor';
function chunk(area,material,role='equipment'){
 const root=new THREE.Group();root.userData={areas:[area],role};root.add(new THREE.Mesh(geometry,material));return root;
}
function state(s){ship.lastState=s;interior.updateState(s);}
const first=chunk('bridge',reactor),shared=chunk('reactor',reactor);
interior.attachChunk(first);interior.attachChunk(shared);assert.equal(reactor.emissiveIntensity,.16);
let revisits=0;const traverse=first.traverse;first.traverse=function(callback){revisits++;return traverse.call(this,callback);};
const plain=new THREE.MeshStandardMaterial(),additional=[];
for(let n=0;n<100;n++){const root=chunk('bridge',plain);additional.push(root);interior.attachChunk(root);}
assert.equal(revisits,0,'loading later chunks must not repeatedly traverse previous chunks');
state({reactor_active:true,cooling_repaired:true,greenhouse_active:true});
assert.equal(reactor.emissiveIntensity,.85);assert.equal(reactor.color.getHex(),0x8fcbdc);assert.equal(revisits,0,'mission updates use registered materials instead of walking geometry');
const grow=new THREE.MeshStandardMaterial();grow.userData.system='grow';
const late=chunk('bio',grow);interior.attachChunk(late);assert.equal(grow.emissiveIntensity,1,'late chunk inherits the current mission state');
let changes=0;const originalApply=interior.applySystemState;interior.applySystemState=function(...args){changes++;return originalApply.apply(this,args);};
for(let n=0;n<1000;n++)state({...ship.lastState,unrelated_counter:n});
assert.equal(changes,0,'unrelated snapshots do not rewrite material state');
const unchangedSnapshotWrites=changes;
interior.detachChunk(first);state({reactor_active:false});assert.equal(reactor.emissiveIntensity,.16,'another chunk retains the shared system material');
interior.detachChunk(shared);state({reactor_active:true});assert.equal(reactor.emissiveIntensity,.16,'unloaded system materials are released from the registry');

const roof=chunk('cryo',plain,'ceiling');interior.attachChunk(roof);
assert.equal(roof.parent.visible,false,'closed adjacent room remains hidden');
const visible=interior.visibleRooms;let visibilityWrites=0;
const observed=additional[0];let visibility=observed.visible;Object.defineProperty(observed,'visible',{configurable:true,get(){return visibility;},set(value){visibilityWrites++;visibility=value;}});
for(let n=0;n<1000;n++)interior.update('explore',{x:1+n/10000,z:4});
assert.equal(interior.visibleRooms,visible);assert.equal(visibilityWrites,0,'movement inside one unchanged room does not rewrite all object visibility');
ship.livingWorld.objects.set('door-bridge',{progress:.5});interior.update('explore',ship.player);assert.ok(interior.visibleRooms.has('west'));assert.ok(!interior.visibleRooms.has('cryo'));
ship.livingWorld.objects.set('door-cryo',{progress:.5});interior.update('explore',ship.player);assert.ok(interior.visibleRooms.has('cryo'));assert.equal(roof.parent.visible,true);
const openVisibility=interior.visibleRooms;ship.livingWorld.objects.get('door-cryo').progress=.9;interior.update('explore',ship.player);assert.equal(interior.visibleRooms,openVisibility,'animation within an open passage does not rebuild connectivity');
ship.livingWorld.objects.get('door-cryo').progress=0;interior.update('explore',ship.player);assert.equal(roof.parent.visible,false,'closing a passage invalidates cached visibility');
ship.mode='overview';interior.update(ship.mode,ship.player);assert.equal(roof.visible,false);assert.equal(roof.parent.visible,true);
const lateRoof=chunk('bridge',plain,'ceiling');interior.attachChunk(lateRoof);assert.equal(lateRoof.visible,false,'late ceiling stays hidden in overview');
ship.mode='explore';interior.update(ship.mode,ship.player);assert.equal(roof.visible,true);assert.equal(lateRoof.visible,true);assert.equal(roof.parent.visible,false);
interior.update('explore',{x:-16,z:0});assert.equal(roof.parent.visible,true,'crossing into another room invalidates cached visibility');

// The GPU batching path is checked with actual Three geometry and transforms;
// no WebGL context or claimed GPU frame-rate is involved in this test.
ship.renderer.isSoftware=false;
const surface=new THREE.MeshStandardMaterial({map:new THREE.Texture()});surface.userData.staticSurface=true;
const label=new THREE.MeshStandardMaterial({map:new THREE.Texture()});
const group=new THREE.Group();group.userData={areas:['bridge'],role:'equipment'};group.position.set(2,0,0);
for(let n=0;n<4;n++){const m=new THREE.Mesh(geometry,surface);m.position.set(n,1,n*.2);m.castShadow=true;group.add(m);}
const labels=[];for(let n=0;n<3;n++){const m=new THREE.Mesh(geometry,label);m.position.set(n,3,0);labels.push(m);group.add(m);}
const moving=new THREE.Group();moving.userData.articulated=true;group.add(moving);
for(let n=0;n<3;n++){const m=new THREE.Mesh(geometry,surface);m.position.set(n,5,0);moving.add(m);}
const before=new THREE.Box3().setFromObject(group);interior.attachChunk(group);
const batch=group.children.find(o=>o.isInstancedMesh);assert.ok(batch);assert.equal(batch.count,4,'repeated mapped surfaces share one instanced draw');
assert.ok(labels.every(m=>m.parent===group),'label/screen meshes are retained');
assert.equal(moving.children.length,3);assert.ok(moving.children.every(m=>m.matrixAutoUpdate),'articulated equipment remains animated');
assert.ok(labels.every(m=>!m.matrixAutoUpdate),'static local transforms are not recomputed each frame');
const after=new THREE.Box3().setFromObject(group);assert.ok(before.min.distanceTo(after.min)<1e-6&&before.max.distanceTo(after.max)<1e-6,'batching preserves real world-space bounds');
interior.batch(group);assert.equal(group.children.filter(o=>o.isInstancedMesh).length,1);assert.equal(batch.count,4,'repeat batching cannot collapse existing instances');
group.position.x+=5;const moved=new THREE.Box3().setFromObject(group);assert.ok(Math.abs(moved.min.x-after.min.x-5)<1e-6,'the batch root can still move (door leaves/chunk parents)');
const template=new THREE.Group();template.userData.playerTemplate=true;for(let n=0;n<4;n++)template.add(new THREE.Mesh(geometry,surface));interior.batch(template);assert.equal(template.children.length,4);assert.ok(template.children.every(m=>m.matrixAutoUpdate));
let instanceDisposals=0,geometryDisposals=0,materialDisposals=0;
batch.addEventListener('dispose',()=>instanceDisposals++);geometry.addEventListener('dispose',()=>geometryDisposals++);surface.addEventListener('dispose',()=>materialDisposals++);
interior.detachChunk(group);interior.detachChunk(group);assert.equal(instanceDisposals,1,'instance buffers are freed exactly once');assert.equal(geometryDisposals,0);assert.equal(materialDisposals,0,'shared GPU resources remain owned by the streaming loader');
interior.detachChunk(roof);assert.ok(!interior.ceilings.includes(roof),'unloaded ceilings leave the visibility list');
interior.dispose();assert.equal(interior.systemMaterials.size,0);assert.equal(interior.chunkRoots.size,0);
geometry.dispose();for(const m of [reactor,plain,grow,surface,label]){m.map?.dispose();m.dispose();}
console.log(JSON.stringify({passed:true,checks:['linear chunk registration without revisiting loaded trees','state cache with late arrivals and shared-material lifetime','door topology and room invalidation','ceiling cutaway and late arrivals','mapped surface instancing without collapsing labels or animated rigs','stable geometry bounds and movable parent transforms','idempotent instance disposal and streaming resource ownership'],unchangedSnapshotWrites,scope:'actual Three.js objects; no browser or FPS claim'}));
