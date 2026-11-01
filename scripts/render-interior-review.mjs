// Offline images of actual runtime geometry, not browser/hardware acceptance.
import fs from 'node:fs';
import {registerHooks,createRequire} from 'node:module';
const require=createRequire(process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES?process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES+'/package.json':import.meta.url);
const {createCanvas,ImageData}=require('@napi-rs/canvas');
globalThis.document={createElement:()=>createCanvas(1,1)};globalThis.ImageData=ImageData;
registerHooks({resolve(s,c,next){if(s==='three')return {url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true};return next(s,c);}});
const THREE=await import('../web/vendor/three.module.js');
const {ShipInterior}=await import('../web/src/interior/ShipInterior.js');
const {layout}=await import('../web/src/interior/DeckLayout.js');
const {LivingWorld}=await import('../web/src/world/LivingWorld.js');
const {SoftwareRenderer}=await import('../web/src/render/SoftwareRenderer.js');
const canvas=createCanvas(1440,900),renderer=new SoftwareRenderer(canvas);renderer.setSize(1440,900);
const ship={scene:new THREE.Scene(),renderer,player:new THREE.Vector3(0,1.7,4.5),camera:new THREE.PerspectiveCamera(67,1.6,.06,300),remote:new Map(),yaw:0};ship.scene.add(new THREE.HemisphereLight(0xe4edf8,0x687582,2.6));ship.interior=new ShipInterior(ship);
const world=new LivingWorld(ship);ship.livingWorld=world;const objects=layout.gates.filter(g=>g.door).map(g=>({...g,kind:'door',open:false}));world.setState({settings:{npc_count:0},objects});world.animate(.016);ship.interior.update('explore',ship.player);
const out=new URL('../docs/interior-review/',import.meta.url);fs.mkdirSync(out,{recursive:true});
const frames=[['bridge',[3.3,1.7,3.7],[2.6,1.9,-5.5]],['corridor',[-10,1.7,9],[10,1.75,9]],['research',[0,1.7,12.5],[-.8,1.4,18]],['door-closed',[0,1.7,3.8],[0,1.7,7]],['door-open',[0,1.7,3.8],[0,1.7,7]],['cargo',[15.5,1.7,12],[19,1.5,18]],['habitat',[-15,1.7,12.5],[-18,1.6,18]]];
const results=[];
for(const [name,pos,target] of frames){if(name==='door-open'){world.objects.get('door-main').data.open=true;world.animate(.016);}ship.player.set(...pos);ship.interior.update('explore',ship.player);world.animate(.016);ship.interior.update('explore',ship.player);ship.camera.position.set(...pos);ship.camera.lookAt(...target);renderer.last=-1000;const begin=performance.now();renderer.render(ship.scene,ship.camera);const ms=performance.now()-begin;fs.writeFileSync(new URL(name+'.png',out),canvas.toBuffer('image/png'));results.push({name,render_ms:Math.round(ms*10)/10,...renderer.info.render});}
const benchmark=[];
for(const name of ['bridge','corridor','research']){const [,pos,target]=frames.find(f=>f[0]===name);ship.player.set(...pos);ship.camera.position.set(...pos);ship.camera.lookAt(...target);ship.interior.update('explore',ship.player);world.animate(.016);const times=[];for(let i=0;i<12;i++){renderer.last=-1000;const start=performance.now();renderer.render(ship.scene,ship.camera);if(i>1)times.push(performance.now()-start);}times.sort((a,b)=>a-b);benchmark.push({name,samples:times.length,median_ms:Math.round(times[5]*10)/10,p95_ms:Math.round(times[9]*10)/10,...renderer.info.render});}
fs.writeFileSync(new URL('benchmark.json',out),JSON.stringify({platform:process.platform,arch:process.arch,node:process.version,renderer:'SoftwareRenderer + @napi-rs/canvas, offline',resolution:'1440x900',npc_count:0,streamed_crew:false,shadows:false,benchmark},null,2));
fs.writeFileSync(new URL('render-metrics.json',out),JSON.stringify({kind:'offline SoftwareRenderer; not gameplay FPS',resolution:'1440 × 900',npc_count:0,frames:results},null,2));console.log(JSON.stringify(results));
