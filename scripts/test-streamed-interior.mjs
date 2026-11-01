// Real server interest plans + production resource loader; local file transport.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {registerHooks,createRequire} from 'node:module';
registerHooks({resolve(s,c,next){
 if(s==='three')return{url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true};
 if(s.startsWith('three/addons/'))return{url:new URL('../web/vendor/'+s.slice('three/addons/'.length),import.meta.url).href,shortCircuit:true};
 return next(s,c);
}});
const THREE=await import('../web/vendor/three.module.js');
const {StreamingWorld}=await import('../web/src/world/StreamingWorld.js');
const {ShipInterior}=await import('../web/src/interior/ShipInterior.js');
const {BridgeCrew}=await import('../web/src/crew/BridgeCrew.js');
const {LivingWorld}=await import('../web/src/world/LivingWorld.js');
const {layout}=await import('../web/src/interior/DeckLayout.js');
const {ShipScene}=await import('../web/src/scene.js');
const render=process.argv.includes('--render'),out=new URL('../docs/reference-interior-review/',import.meta.url);
let canvas,renderer;
if(render){
 const require=createRequire(process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES?process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES+'/package.json':import.meta.url);
 const {createCanvas,ImageData}=require('@napi-rs/canvas');globalThis.document={createElement:()=>createCanvas(1,1)};globalThis.ImageData=ImageData;
 canvas=createCanvas(1440,900);const {SoftwareRenderer}=await import('../web/src/render/SoftwareRenderer.js');renderer=new SoftwareRenderer(canvas);renderer.setSize(1440,900);
}else renderer={isSoftware:true};
const manifest=JSON.parse(fs.readFileSync(new URL('../world/manifest.json',import.meta.url)));
const plans=JSON.parse(fs.readFileSync(new URL('plans.json',out)));
assert.ok(Object.values(plans).every(p=>p.version===manifest.version),'regenerate plans with TestReferenceWorldCoverage after rebuilding world');
let currentPlan=plans.cryo,requests=0;
globalThis.window={dispatchEvent(){}};
globalThis.fetch=async url=>{
 const parsed=new URL(url,'http://fixture.local');
 if(parsed.pathname==='/api/world/interest')return new Response(JSON.stringify(currentPlan));
 assert.ok(parsed.pathname.startsWith('/api/world/resource/'),'no external requests');
 const id=parsed.pathname.split('/').at(-1);assert.ok(currentPlan.resources.some(r=>r.id===id),'server-authorized resource');requests++;
 return new Response(fs.readFileSync(new URL('../world/'+id+'.json',import.meta.url)));
};
const ship={scene:new THREE.Scene(),renderer,mode:'explore',player:new THREE.Vector3(),remote:new Map(),yaw:0,lastState:{reactor_active:true,cooling_repaired:true,greenhouse_active:true},camera:new THREE.PerspectiveCamera(67,1.6,.06,300)};
ship.scene.background=new THREE.Color(0x050b14);ship.scene.add(new THREE.HemisphereLight(0xe4edf8,0x687582,2.1));
ship.crew=new BridgeCrew(ship.scene);ship.interior=new ShipInterior(ship,{streamed:true});ship.livingWorld=new LivingWorld(ship);
ship.schematicGroup=new THREE.Group();ship.installSchematic=g=>{ship.schematicGroup=g;};ship.installAvatarTemplate=g=>{ship.avatarTemplate=g;g.removeFromParent();};ship.updateState=s=>ship.interior.updateState(s);
ship.livingWorld.setState({settings:{npc_count:0},objects:layout.gates.filter(g=>g.door).map(g=>({...g,kind:'door',open:false}))});ship.livingWorld.animate(.016);
const stream=ship.streamWorld=new StreamingWorld(ship);stream.meta={...manifest,limit:Math.floor(manifest.world_bytes*.30)};
let disposed=0;const observed=new WeakSet(),watch=()=>{for(const kind of ['geometries','materials','textures'])for(const item of Object.values(stream.parsed[kind]))if(!observed.has(item)){observed.add(item);item.addEventListener('dispose',()=>disposed++);}};
const views=[['bridge',[2.6,1.9,-5.5]],['window',[7,2.15,-7]],['window-angle',[7,2.15,-6.5]],['cryo',[-20,1.5,0]],['reactor',[17,1.7,0]],['bio',[-16,1.5,-20]],['corridor',[10,1.75,9]],['research',[-.8,1.4,18]],['cargo',[19,1.5,18]],['habitat',[-18,1.6,18]],['bridge-alt',[4,1.5,-4]],['cryo-alt',[-16,1.5,-3]],['reactor-alt',[17,1.7,0]],['bio-alt',[-17,1.5,-17]],['corridor-alt',[-10,1.75,9]],['research-alt',[-1,1.4,17]],['cargo-alt',[19,1.4,14]],['habitat-alt',[-16,1.5,16]]];
const metrics=[];
for(let pass=0;pass<2;pass++)for(const [name,target]of views){
 currentPlan=plans[name];ship.player.set(currentPlan.position.x,1.7,currentPlan.position.z);await stream.load();watch();
 assert.equal(stream.reserved,0);assert.ok(stream.C<=currentPlan.budget&&stream.peak<=stream.meta.limit);
 assert.equal(stream.objects.size,currentPlan.chunks.length+1);
 const roots=[...stream.objects.values()],ids=new Set();for(const root of roots){assert.ok(root.parent,'resident chunk attached');assert.ok(!ids.has(root.userData.chunk));ids.add(root.userData.chunk);}
 assert.ok(ship.interior.ceilings.length>0);ship.interior.update('overview',ship.player);assert.ok(ship.interior.ceilings.every(r=>!r.visible));ship.interior.update('explore',ship.player);assert.ok(ship.interior.ceilings.every(r=>r.visible));
 ship.crew.animate(0,ship.player,true);ship.livingWorld.animate(.016);ship.interior.update('explore',ship.player);
 for(const c of ship.crew.characters)if(c.loaded){for(const joint of ['head','arm0','arm1','leg0','leg1','ring'])assert.ok(c.rig[joint]);assert.ok(c.group.children.length===1,'no procedural duplicate');c.group.visible=ship.interior.visibleRooms.has(layout.areas.find(a=>Math.abs(c.group.position.x-a.x)<=a.w/2&&Math.abs(c.group.position.z-a.z)<=a.d/2)?.id);}
 if(render&&pass===0){ship.camera.position.copy(ship.player);ship.camera.lookAt(...target);renderer.last=-1000;const start=performance.now();renderer.render(ship.scene,ship.camera);const ms=performance.now()-start;fs.writeFileSync(new URL(name+'.png',out),canvas.toBuffer('image/png'));metrics.push({name,render_ms:+ms.toFixed(1),...renderer.info.render,resident_bytes:stream.C,ratio:stream.C/manifest.world_bytes,loaded_mentors:ship.crew.characters.filter(c=>c.loaded).map(c=>c.profile.id)});}
}
assert.ok(disposed>0,'leaving rooms frees shared GPU resources only when no longer referenced');
assert.equal(ship.crew.models.geometries.size,0,'runtime uses only serialized portrait geometry');
assert.notEqual(stream.positionKey({x:-16,z:6.8}),stream.positionKey({x:-16,z:7.2}),'same-cell doorway triggers an interest refresh');
ship.mode='overview';ship.target=new THREE.Vector3(17,0,0);assert.equal(stream.viewArea().id,'reactor');ship.mode='explore';assert.equal(stream.viewArea(),null);
currentPlan=plans.bridge;ship.player.set(0,1.7,4.5);await stream.load();
for(const open of [false,true]){
 const door=ship.livingWorld.objects.get('door-main');door.data.open=open;ship.livingWorld.animate(.016);ship.interior.update('explore',ship.player);ship.livingWorld.animate(.016);
 if(render){ship.camera.position.copy(ship.player);ship.camera.lookAt(0,1.7,9);renderer.last=-1000;renderer.render(ship.scene,ship.camera);fs.writeFileSync(new URL('door-'+(open?'open':'closed')+'.png',out),canvas.toBuffer('image/png'));}
}
// Exercise the WebGL batching path without claiming a GPU render.
if(render){
 ship.ambientLight=ship.scene.children.find(o=>o.isHemisphereLight);
 ship.sunLight=new THREE.DirectionalLight();
 const brightness=[];
 ship.camera.position.copy(ship.player);ship.camera.lookAt(2.6,1.9,-5.5);
 for(const enabled of [false,true]){
  ShipScene.prototype.setLighting.call(ship,enabled);
  renderer.last=-1000;renderer.render(ship.scene,ship.camera);
  fs.writeFileSync(new URL('lights-'+(enabled?'on':'off')+'.png',out),canvas.toBuffer('image/png'));
  const pixels=canvas.getContext('2d').getImageData(400,660,640,200).data;
  let sum=0;for(let n=0;n<pixels.length;n+=4)sum+=pixels[n]+pixels[n+1]+pixels[n+2];
  brightness.push(sum/(pixels.length/4*3));
 }
 assert.ok(brightness[1]>brightness[0]*1.4,'work-light switch visibly illuminates actual streamed deck pixels');
 fs.writeFileSync(new URL('lighting-pixels.json',out),JSON.stringify({scope:'production light switch and serialized bridge rendered on CPU',off:brightness[0],on:brightness[1],ratio:brightness[1]/brightness[0]},null,2));
}
renderer.isSoftware=false;
const templateRoot=new THREE.Group(),template=ship.avatarTemplate.clone(true);templateRoot.add(template);const avatarParts=template.children.length;ship.interior.batch(templateRoot);assert.equal(template.children.length,avatarParts,'batching must preserve player template hierarchy');
let instances=0,staticMeshesBefore=0,staticMeshesAfter=0;for(const root of ship.interior.chunkRoots){root.traverse(o=>{if(o.isMesh)staticMeshesBefore++;});ship.interior.batch(root);root.traverse(o=>{if(o.isMesh)staticMeshesAfter++;if(o.isInstancedMesh)instances+=o.count;});}assert.ok(instances>0,'static geometry uses instancing');assert.ok(staticMeshesAfter<staticMeshesBefore,'batching reduces static mesh nodes without dropping geometry');
renderer.isSoftware=true;
const result={passed:true,scope:'production loader with server plans and local file transport; no browser or live network',routePasses:2,requests,disposed,peak_bytes:stream.peak,world_bytes:manifest.world_bytes,peak_ratio:stream.peak/manifest.world_bytes,bridgeStaticBatching:{meshes_before:staticMeshesBefore,meshes_after:staticMeshesAfter,instanced_parts:instances,scope:'resident static mesh nodes; not measured GPU draw calls'},frames:metrics};
fs.writeFileSync(new URL(render?'streaming-render.json':'streaming-check.json',out),JSON.stringify(result,null,2));console.log(JSON.stringify(result));
ship.interior.dispose();ship.livingWorld.dispose();ship.crew.dispose();
