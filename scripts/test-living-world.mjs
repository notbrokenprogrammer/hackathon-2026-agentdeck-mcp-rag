import fs from 'node:fs';
import assert from 'node:assert/strict';
import {registerHooks} from 'node:module';
const root = new URL('../', import.meta.url);
registerHooks({resolve(specifier,context,next){
 if(specifier==='three')return{url:new URL('web/vendor/three.module.js',root).href,shortCircuit:true};
 return next(specifier,context);
}});
const THREE=await import('../web/vendor/three.module.js');
const {LivingWorld}=await import('../web/src/world/LivingWorld.js');
const {BridgeCrew}=await import('../web/src/crew/BridgeCrew.js');
const {roomAt}=await import('../web/src/interior/DeckLayout.js');
function ship(software=false){return{scene:new THREE.Scene(),renderer:{isSoftware:software},player:new THREE.Vector3(-15,1.7,0),yaw:0,remote:new Map(),camera:new THREE.PerspectiveCamera()};}
const state={settings:{npc_count:64,simulation_speed:1},objects:[
 {id:'door',kind:'door',name:'Шлюз',x:-8,y:0,z:0,rotation:0,open:false},
 {id:'box',kind:'crate',name:'Контейнер',x:-13,y:0,z:2,rotation:0},
 {id:'plant',kind:'plant',name:'Растение',x:1,y:0,z:-16,rotation:0},
 {id:'beacon',kind:'beacon',name:'Маяк',x:17,y:0,z:2,rotation:0}
]};
const owner=ship(),world=new LivingWorld(owner);world.setState(state,'self');
assert.equal(world.npcs.length,64);assert.equal(world.parts.length,15);assert.ok(world.parts.every(p=>p.mesh.isInstancedMesh&&p.mesh.count===64));
assert.equal(world.walkable(-8,0),false);assert.equal(world.walkable(-7,0),true);
assert.equal(world.nearest(owner.player).action,'pickup');assert.equal(world.nearest(owner.player).object_id,'box');
world.animate(.05);const moving=world.npcs.find(n=>Math.hypot(n.x-owner.player.x,n.z-owner.player.z)>3);const prior={x:moving.x,z:moving.z};
world.reactToSpeech(new THREE.Vector3(moving.x,0,moving.z));world.animate(.05);assert.equal(moving.walking,false);assert.equal(moving.x,prior.x);assert.equal(moving.z,prior.z);
world.speechUntil=0;owner.player.set(moving.x,1.7,moving.z);world.animate(.05);assert.equal(moving.walking,false);
const next=structuredClone(state);next.objects[0].open=true;next.objects[1].held_by='self';world.setState(next,'self');assert.equal(world.walkable(-8,0),true);assert.equal(world.nearest(owner.player).action,'drop');world.animate(.1);assert.ok(world.objects.get('box').target.y>.5);
next.objects[1].held_by='';next.objects[1].x=3;next.objects[1].z=4;world.setState(next,'self');for(let i=0;i<30;i++)world.animate(.1);assert.ok(world.objects.get('box').group.position.distanceTo(new THREE.Vector3(3,0,4))<.01);
next.settings.npc_count=160;world.setState(next,'self');assert.equal(world.npcs.length,160);assert.equal(world.parts.length,15);world.animate(.016);
next.settings.npc_count=0;next.objects=[];world.setState(next,'self');assert.equal(world.npcs.length,0);assert.equal(world.objects.size,0);
const fallback=new LivingWorld(ship(true));fallback.setState(state,'self');fallback.animate(.016);assert.equal(fallback.parts[0].meshes.length,64);assert.ok(fallback.parts[0].meshes[0].matrix.elements.every(Number.isFinite));
// Hidden residents must disappear from the GPU draw count, not merely collapse
// to zero-scale triangles. Opening a room restores every resident and its colour.
world.setState(state,'self');owner.interior={visibleRooms:new Set(['bridge'])};world.animate(.016);
const residents=world.npcs.filter(n=>roomAt(n.x,n.z)?.id==='bridge');assert.ok(residents.length>0&&residents.length<64);
for(const part of world.parts)assert.equal(part.mesh.count,residents.length);
const torso=world.parts.find(p=>p.name==='torso').mesh,color=new THREE.Color();
for(let i=0;i<residents.length;i++){torso.getColorAt(i,color);const expected=new THREE.Color([0x466986,0x686f78,0x4c796a,0x566582][residents[i].id%4]);for(const channel of ['r','g','b'])assert.ok(Math.abs(color[channel]-expected[channel])<1e-6,'compacted instance retains its uniform colour');}
owner.interior.visibleRooms=new Set();world.animate(.016);assert.ok(world.parts.every(p=>p.mesh.count===0));
delete owner.interior;world.animate(.016);assert.ok(world.parts.every(p=>p.mesh.count===64));
fallback.ship.interior={visibleRooms:new Set(['bridge'])};fallback.animate(.016);
for(const npc of fallback.npcs)for(const part of fallback.parts)if(roomAt(npc.x,npc.z)?.id!=='bridge')assert.equal(part.meshes[npc.id].visible,false);
delete fallback.ship.interior;fallback.animate(.016);assert.ok(fallback.parts.find(p=>p.name==='torso').meshes.every(m=>m.visible));
fallback.dispose();world.dispose();
// Exercise the enhancement against the actual streamed models and their real rigs.
const manifest=JSON.parse(fs.readFileSync(new URL('world/manifest.json',root)));const raw={images:[],textures:[],materials:[],geometries:[],object:new Map()};
for(const r of manifest.resources){const d=JSON.parse(fs.readFileSync(new URL('world/'+r.id+'.json',root)));if(d.kind==='object')raw.object.set(r.id,d.body);else raw[d.kind].push(d.body);}
const loader=new THREE.ObjectLoader(),images=loader.parseImages(raw.images,()=>{}),textures=loader.parseTextures(raw.textures,images),materials=loader.parseMaterials(raw.materials,textures),geometries=loader.parseGeometries(raw.geometries,{});
const crew=new BridgeCrew(new THREE.Scene());
for(const chunk of manifest.chunks.filter(c=>c.npc)){const group=loader.parseObject(raw.object.get(chunk.low.object),geometries,materials,textures,{});crew.register(group);}
assert.equal(crew.characters.filter(c=>c.loaded).length,4);
for(const c of crew.characters){assert.ok(c.group.userData.enhanced);assert.ok(c.rig.leg0&&c.rig.leg1);assert.ok(c.rig.arm0.position.y>1);}
crew.setBehaviors([{id:'savinykh',behavior:'patrol'}]);crew.animate(1,new THREE.Vector3());assert.notEqual(crew.characters[0].rig.leg0.rotation.x,0);
crew.setBehaviors([{id:'savinykh',behavior:'passive'}]);crew.animate(2,new THREE.Vector3());assert.equal(crew.characters[0].rig.leg0.rotation.x,0);assert.equal(crew.characters[0].rig.arm0.rotation.x,0);crew.dispose();
console.log(JSON.stringify({passed:true,checks:['64/160 NPC instanced crowd','speech and proximity reactions','closed/open door collision','authoritative pickup and drop','world replacement','Canvas fallback transforms','actual streamed crew enhancement','articulated patrol/passive behaviors']}));
