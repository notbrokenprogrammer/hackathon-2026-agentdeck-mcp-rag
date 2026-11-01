// Deterministic rendering of the actual runtime enhancements and live-world geometry.
// This is model QA, not a browser screenshot or a hardware performance benchmark.
import fs from 'node:fs';
import {registerHooks,createRequire} from 'node:module';
const require=createRequire(process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES?process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES+'/package.json':import.meta.url);
const {createCanvas,ImageData}=require('@napi-rs/canvas');
const root=new URL('../',import.meta.url);
registerHooks({resolve(specifier,context,next){if(specifier==='three')return{url:new URL('web/vendor/three.module.js',root).href,shortCircuit:true};return next(specifier,context);}});
const THREE=await import('../web/vendor/three.module.js');
const {SoftwareRenderer}=await import('../web/src/render/SoftwareRenderer.js');
const {BridgeCrew}=await import('../web/src/crew/BridgeCrew.js');
const {LivingWorld}=await import('../web/src/world/LivingWorld.js');
globalThis.document={createElement:()=>createCanvas(1,1)};globalThis.ImageData=ImageData;
const manifest=JSON.parse(fs.readFileSync(new URL('world/manifest.json',root))),raw={images:[],textures:[],materials:[],geometries:[],object:new Map()};
for(const r of manifest.resources){const d=JSON.parse(fs.readFileSync(new URL('world/'+r.id+'.json',root)));if(d.kind==='object')raw.object.set(r.id,d.body);else raw[d.kind].push(d.body);}
const loader=new THREE.ObjectLoader(),images=loader.parseImages(raw.images,()=>{}),textures=loader.parseTextures(raw.textures,images),materials=loader.parseMaterials(raw.materials,textures),geometries=loader.parseGeometries(raw.geometries,{});
const scene=new THREE.Scene(),crew=new BridgeCrew(scene);for(const chunk of manifest.chunks){const group=loader.parseObject(raw.object.get(chunk.low.object),geometries,materials,textures,{});scene.add(group);crew.register(group);}
crew.animate(2.2,new THREE.Vector3(-15,1.7,0));
const jobs=[];
function capture(name,scene,width,height,position,target,fov=43){const canvas=createCanvas(width,height),renderer=new SoftwareRenderer(canvas);renderer.setSize(width,height);const camera=new THREE.PerspectiveCamera(fov,width/height,.1,500);camera.position.set(...position);camera.lookAt(...target);renderer.last=-1000;renderer.render(scene,camera);const ctx=canvas.getContext('2d');ctx.globalCompositeOperation='destination-over';ctx.fillStyle='#0b1621';ctx.fillRect(0,0,width,height);ctx.globalCompositeOperation='source-over';ctx.font='20px sans-serif';ctx.fillStyle='#a9d1d3';ctx.fillText(name,22,34);return canvas;}
for(const c of crew.characters){const group=c.group.clone(true);group.position.set(0,0,0);group.rotation.y=-.18;group.children.filter(x=>x.isSprite).forEach(x=>group.remove(x));const portrait=new THREE.Scene();portrait.add(group);jobs.push(capture(c.profile.short,portrait,450,700,[-.48,1.48,-4.6],[0,1.08,0],32));}
const fakeShip={scene,renderer:{isSoftware:true},player:new THREE.Vector3(-15,1.7,0),camera:new THREE.PerspectiveCamera(),remote:new Map(),yaw:0};const world=new LivingWorld(fakeShip);world.setState({settings:{npc_count:64,simulation_speed:1},objects:[{id:'door',kind:'door',name:'Шлюз',x:-8,y:0,z:0,open:false},{id:'crate',kind:'crate',name:'Контейнер',x:-13,y:0,z:2},{id:'plant',kind:'plant',name:'Биомодуль',x:2,y:0,z:-16},{id:'beacon',kind:'beacon',name:'Маяк',x:16,y:0,z:2}]},'self');world.animate(.1);
jobs.push(capture('LIVE DECK / 64 REACTIVE RESIDENTS',scene,1600,1000,[34,39,41],[-1,0,-6]));
const propSheet=createCanvas(1600,650),propCtx=propSheet.getContext('2d');
for(const [i,kind]of ['door','crate','plant','beacon'].entries()){
 const props=new THREE.Scene(),item=world.makeObject({id:kind,kind,name:kind,x:0,y:0,z:0});props.add(item.group);
 const label=['Шлюз · две подвижные створки','Контейнер · взять / положить','Биомодуль · новые растения','Маяк · вращающийся модуль'][i];
 const camera=kind==='door'?[-11,4.1,-4.8]:[-2.5,2.1,-3.7],target=kind==='door'?[0,1.35,0]:[0,.75,0];
 propCtx.drawImage(capture(label,props,400,650,camera,target,kind==='door'?35:32),i*400,0);
}
jobs.push(propSheet);
const out=new URL('docs/sunday/',root);fs.mkdirSync(out,{recursive:true});
const sheet=createCanvas(1800,700),ctx=sheet.getContext('2d');for(let i=0;i<4;i++)ctx.drawImage(jobs[i],i*450,0);
fs.writeFileSync(new URL('crew-upgrade.png',out),sheet.toBuffer('image/png'));
fs.writeFileSync(new URL('living-deck.png',out),jobs[4].toBuffer('image/png'));
fs.writeFileSync(new URL('interactive-objects.png',out),jobs[5].toBuffer('image/png'));
world.dispose();crew.dispose();
console.log(JSON.stringify({rendered:['docs/sunday/crew-upgrade.png','docs/sunday/living-deck.png','docs/sunday/interactive-objects.png'],note:'Deterministic software geometry review, not browser or hardware WebGL QA.'}));
