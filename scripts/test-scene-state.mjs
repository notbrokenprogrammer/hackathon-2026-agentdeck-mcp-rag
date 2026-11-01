import fs from 'node:fs';
import assert from 'node:assert/strict';
import {registerHooks} from 'node:module';
const root=new URL('../',import.meta.url);
registerHooks({resolve(specifier,context,next){
 if(specifier==='three') return {url:new URL('web/vendor/three.module.js',root).href,shortCircuit:true};
 if(specifier.startsWith('three/addons/')) return {url:new URL('web/vendor/'+specifier.slice('three/addons/'.length),root).href,shortCircuit:true};
 return next(specifier,context);
}});
const THREE=await import('../web/vendor/three.module.js');
const {ShipScene}=await import('../web/src/scene.js');
const manifest=JSON.parse(fs.readFileSync(new URL('world/manifest.json',root)));
const raw={images:[],textures:[],materials:[],geometries:[],object:new Map()};
for(const r of manifest.resources){const d=JSON.parse(fs.readFileSync(new URL('world/'+r.id+'.json',root)));if(d.kind==='object')raw.object.set(r.id,d.body);else raw[d.kind].push(d.body);}
const loader=new THREE.ObjectLoader(),images=loader.parseImages(raw.images,()=>{}),textures=loader.parseTextures(raw.textures,images),materials=loader.parseMaterials(raw.materials,textures),geometries=loader.parseGeometries(raw.geometries,{});
const roots=new Map();for(const c of manifest.chunks)roots.set(c.low.object,loader.parseObject(raw.object.get(c.low.object),geometries,materials,textures,{}));
const {ShipInterior}=await import('../web/src/interior/ShipInterior.js');
const context={scene:new THREE.Scene(),renderer:{isSoftware:true},player:new THREE.Vector3(-15,1.7,0),mode:'explore'};
context.interior=new ShipInterior(context,{streamed:true});for(const root of roots.values())context.interior.attachChunk(root);
const sample=type=>{let found;for(const o of roots.values())o.traverse(m=>{if(m.isMesh&&m.material?.userData.system===type)found=m;});assert.ok(found,type);return found;};
const reactor=sample('reactor'),cooling=sample('cooling'),plant=sample('plant'),lamp=sample('grow'),power=sample('power');
ShipScene.prototype.updateState.call(context,{reactor_active:false,cooling_repaired:false,greenhouse_active:false});
assert.equal(reactor.material.emissiveIntensity,.16);assert.equal(plant.material.color.getHex(),0x71856a);assert.equal(lamp.material.emissiveIntensity,.18);
ShipScene.prototype.updateState.call(context,{reactor_active:true,cooling_repaired:true,greenhouse_active:true});
assert.equal(reactor.material.emissiveIntensity,.85);assert.equal(reactor.material.color.getHex(),0x8fcbdc);assert.equal(cooling.material.color.getHex(),0x7fc6cc);assert.equal(plant.material.color.getHex(),0x6d9b7e);assert.equal(lamp.material.emissiveIntensity,1);assert.equal(power.material.emissiveIntensity,1.1);
// The work-light switch must update a stationary room and newly streamed lamps.
context.ambientLight=new THREE.HemisphereLight();context.sunLight=new THREE.DirectionalLight();
context.setLighting=enabled=>ShipScene.prototype.setLighting.call(context,enabled);
context.setLighting(false);
assert.equal(context.interior.workLight.visible,false);assert.equal(power.material.emissiveIntensity,.08);
const darkAmbient=context.ambientLight.intensity;
ShipScene.prototype.updateState.call(context,{reactor_active:true});
assert.equal(power.material.emissiveIntensity,.08,'mission state cannot turn the personal switch back on');
const lateLamp=power.material.clone();lateLamp.userData.system='power';
const lateRoot=new THREE.Group();lateRoot.userData.areas=['cryo'];lateRoot.add(new THREE.Mesh(new THREE.BoxGeometry(),lateLamp));
context.interior.attachChunk(lateRoot);assert.equal(lateLamp.emissiveIntensity,.08,'late lamp inherits off state');
context.universe={settings:{light_intensity:1.5}};context.setLighting(true);
assert.ok(context.ambientLight.intensity>darkAmbient*5);assert.equal(context.interior.workLight.visible,true);
assert.ok(Math.abs(power.material.emissiveIntensity-1.65)<1e-10);assert.equal(lateLamp.emissiveIntensity,power.material.emissiveIntensity);
assert.equal(context.interior.workLight.intensity,135);
ShipScene.prototype.updateState.call(context,{reactor_active:false});
assert.ok(Math.abs(power.material.emissiveIntensity-1.65)<1e-10,'work lights remain usable while the reactor is inactive');
context.interior.update('overview',context.player);assert.equal(context.interior.workLight.visible,false);
context.interior.update('explore',context.player);assert.equal(context.interior.workLight.visible,true);
context.interior.detachChunk(lateRoot);lateLamp.dispose();
const sizes=[[1366,698],[1920,1010],[390,784]];
for(const[w,h]of sizes){let actual;const c={container:{clientWidth:w,clientHeight:h},renderer:{setSize(...args){actual=args}},camera:new THREE.PerspectiveCamera()};ShipScene.prototype.resize.call(c);assert.deepEqual(actual,[w,h,false]);assert.equal(c.camera.aspect,w/h);}
console.log(JSON.stringify({passed:true,checks:['reactor power and cooling colors','greenhouse recovery','grow lamps','switchable work lighting, late arrivals, shared brightness and reactor independence','canvas resize and camera aspect at three sizes'],note:'geometry/material and sizing logic only; not visual browser QA'}));
