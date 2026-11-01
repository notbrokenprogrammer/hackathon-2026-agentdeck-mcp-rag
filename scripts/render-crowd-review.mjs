// Offline view of the actual resident meshes; not a browser screenshot.
import fs from 'node:fs';
import {registerHooks,createRequire} from 'node:module';
registerHooks({resolve(s,c,next){return s==='three'?{url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true}:next(s,c);}});
const require=createRequire(process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES?process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES+'/package.json':import.meta.url);
const {createCanvas}=require('@napi-rs/canvas');
const THREE=await import('../web/vendor/three.module.js');
const {LivingWorld}=await import('../web/src/world/LivingWorld.js');
const {SoftwareRenderer}=await import('../web/src/render/SoftwareRenderer.js');
const canvas=createCanvas(1440,520),renderer=new SoftwareRenderer(canvas);renderer.setSize(1440,520);
const ship={scene:new THREE.Scene(),renderer,player:new THREE.Vector3(100,1.7,100),remote:new Map(),camera:new THREE.PerspectiveCamera(35,1440/520,.1,50)};
ship.scene.background=new THREE.Color(0x24333f);ship.scene.add(new THREE.HemisphereLight(0xf5eee4,0x728596,2.6));
const world=new LivingWorld(ship);world.setState({settings:{npc_count:8,simulation_speed:0},objects:[]});
for(const npc of world.npcs){npc.x=(npc.id-3.5)*.8;npc.z=0;}
world.animate(.016,true);ship.camera.position.set(0,1.3,-7.5);ship.camera.lookAt(0,1,0);
renderer.last=-Infinity;renderer.render(ship.scene,ship.camera);
fs.writeFileSync(new URL('../docs/crew-review/residents.png',import.meta.url),canvas.toBuffer('image/png'));
console.log(JSON.stringify({residents:8,batches:world.parts.length,scope:'offline rendering of actual resident geometry'}));world.dispose();
