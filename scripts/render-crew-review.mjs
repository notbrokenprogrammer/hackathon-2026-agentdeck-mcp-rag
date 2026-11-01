// Offline renders of the actual runtime models. Not browser screenshots.
import fs from 'node:fs';
import {registerHooks,createRequire} from 'node:module';
const require=createRequire(process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES?process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES+'/package.json':import.meta.url);
const {createCanvas,ImageData}=require('@napi-rs/canvas');
globalThis.document={createElement:()=>createCanvas(1,1)};globalThis.ImageData=ImageData;
registerHooks({resolve(s,c,next){if(s==='three')return{url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true};return next(s,c);}});
const THREE=await import('../web/vendor/three.module.js');
const {CosmonautModels}=await import('../web/src/crew/CosmonautModel.js');
const {CREW,BridgeCrew}=await import('../web/src/crew/BridgeCrew.js');
const {SoftwareRenderer}=await import('../web/src/render/SoftwareRenderer.js');
const factory=new CosmonautModels(),out=new URL('../docs/crew-review/',import.meta.url);fs.mkdirSync(out,{recursive:true});
const sheet=createCanvas(1600,1020),sc=sheet.getContext('2d');sc.fillStyle='#15212b';sc.fillRect(0,0,1600,1020);
const metrics=[];
for(const [i,profile]of CREW.entries()){
 const {root}=factory.create(profile),scene=new THREE.Scene();scene.background=new THREE.Color(0x26333e);scene.add(root,new THREE.HemisphereLight(0xf5eee4,0x688091,2.6));
 const canvas=createCanvas(720,1000),renderer=new SoftwareRenderer(canvas);renderer.setSize(720,1000);
 const camera=new THREE.PerspectiveCamera(33,.72,.08,30);camera.position.set(-.75,1.35,-3.5);camera.lookAt(0,1,0);renderer.last=-1000;renderer.render(scene,camera);
 fs.writeFileSync(new URL(profile.id+'-full.png',out),canvas.toBuffer('image/png'));sc.drawImage(canvas,i*400,420,400,556);
 renderer.setSize(720,720);camera.aspect=1;camera.fov=30;camera.updateProjectionMatrix();camera.position.set(-.33,1.73,-1.28);camera.lookAt(0,1.67,0);renderer.last=-1000;renderer.render(scene,camera);
 fs.writeFileSync(new URL(profile.id+'-portrait.png',out),canvas.toBuffer('image/png'));sc.drawImage(canvas,i*400,0,400,400);
 sc.fillStyle='#e4edf0';sc.font='20px sans-serif';sc.fillText(profile.short,i*400+20,1000);
 const turn=createCanvas(1440,720),tc=turn.getContext('2d');
 for(const [index,angle]of[0,Math.PI/2,Math.PI].entries()){
  root.rotation.y=angle;renderer.setSize(480,720);camera.aspect=2/3;camera.fov=34;camera.updateProjectionMatrix();camera.position.set(0,1.2,-3.45);camera.lookAt(0,.98,0);renderer.last=-1000;renderer.render(scene,camera);tc.drawImage(canvas,index*480,0);
 }
 fs.writeFileSync(new URL(profile.id+'-turnaround.png',out),turn.toBuffer('image/png'));
 let triangles=0,meshes=0;root.traverse(o=>{if(o.isMesh){meshes++;triangles+=(o.geometry.index?.count||o.geometry.attributes.position.count)/3;}});metrics.push({id:profile.id,meshes,triangles});
}
fs.writeFileSync(new URL('crew-lineup.png',out),sheet.toBuffer('image/png'));
fs.writeFileSync(new URL('models.json',out),JSON.stringify({kind:'offline rendering of runtime procedural 3D; not photogrammetry or WebGL benchmark',metrics},null,2));factory.dispose();

// Also exercise the actual streamed root replacement in the completed interior.
const {ShipInterior}=await import('../web/src/interior/ShipInterior.js');
const {LivingWorld}=await import('../web/src/world/LivingWorld.js');
const {layout}=await import('../web/src/interior/DeckLayout.js');
const canvas=createCanvas(1440,900),renderer=new SoftwareRenderer(canvas);renderer.setSize(1440,900);
const ship={scene:new THREE.Scene(),renderer,player:new THREE.Vector3(),camera:new THREE.PerspectiveCamera(61,1.6,.08,300),remote:new Map(),yaw:0};
ship.scene.add(new THREE.HemisphereLight(0xe4edf8,0x687582,2.6));ship.interior=new ShipInterior(ship);ship.livingWorld=new LivingWorld(ship);
ship.livingWorld.setState({settings:{npc_count:0},objects:layout.gates.filter(g=>g.door).map(g=>({...g,kind:'door',open:false}))});ship.livingWorld.animate(.016);
const manifest=JSON.parse(fs.readFileSync(new URL('../world/manifest.json',import.meta.url)));
const raw={images:[],textures:[],materials:[],geometries:[],object:new Map()};
for(const resource of manifest.resources){const data=JSON.parse(fs.readFileSync(new URL('../world/'+resource.id+'.json',import.meta.url)));if(data.kind==='object')raw.object.set(resource.id,data.body);else raw[data.kind].push(data.body);}
const loader=new THREE.ObjectLoader(),images=loader.parseImages(raw.images,()=>{}),textures=loader.parseTextures(raw.textures,images),materials=loader.parseMaterials(raw.materials,textures),geometries=loader.parseGeometries(raw.geometries,{}),crew=new BridgeCrew(ship.scene);
for(const chunk of manifest.chunks.filter(c=>c.npc)){const group=loader.parseObject(raw.object.get(chunk.low.object),geometries,materials,textures,{});ship.scene.add(group);crew.register(group);}
for(const [name,position,target]of[['bridge-crew',[2.2,1.7,-4.0],[3,1.30,-2.7]],['engineer-room',[15.4,1.7,-.1],[13,1.3,2]],['greenhouse-crew',[-21.2,1.7,-15.6],[-20,1.3,-14]]]){
 ship.player.set(...position);ship.camera.position.copy(ship.player);ship.camera.lookAt(...target);ship.interior.update('explore',ship.player);renderer.last=-1000;renderer.render(ship.scene,ship.camera);fs.writeFileSync(new URL(name+'.png',out),canvas.toBuffer('image/png'));
}
crew.dispose();ship.livingWorld.dispose();ship.interior.dispose();console.log(JSON.stringify(metrics));
