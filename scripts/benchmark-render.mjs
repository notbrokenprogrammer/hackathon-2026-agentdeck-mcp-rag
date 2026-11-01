// CPU fallback benchmark using real, already generated server interest resources.
// Does not start a server, create users or modify saves. Not a GPU/FPS benchmark.
import fs from 'node:fs';
import {createHash} from 'node:crypto';
import {registerHooks,createRequire} from 'node:module';
const root=new URL('../',import.meta.url);
registerHooks({resolve(s,c,next){if(s==='three')return{url:new URL('web/vendor/three.module.js',root).href,shortCircuit:true};return next(s,c);}});
const require=createRequire(process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES?process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES+'/package.json':import.meta.url);
const {createCanvas,ImageData}=require('@napi-rs/canvas');
globalThis.document={createElement:()=>createCanvas(1,1)};globalThis.ImageData=ImageData;
const THREE=await import('../web/vendor/three.module.js');
const {SoftwareRenderer}=await import('../web/src/render/SoftwareRenderer.js');
const {FrameBudget}=await import('../web/src/render/FrameBudget.js');
const manifest=JSON.parse(fs.readFileSync(new URL('world/manifest.json',root)));
const plan=JSON.parse(fs.readFileSync(new URL('docs/reference-interior-review/plans.json',root))).bridge;
if(plan.version!==manifest.version)throw new Error('Regenerate plans using npm run review:interior after rebuilding the world.');
const raw={images:[],textures:[],materials:[],geometries:[],object:new Map()};
for(const resource of plan.resources){const data=JSON.parse(fs.readFileSync(new URL('world/'+resource.id+'.json',root)));if(data.kind==='object')raw.object.set(resource.id,data.body);else raw[data.kind].push(data.body);}
const loader=new THREE.ObjectLoader(),images=loader.parseImages(raw.images,()=>{}),textures=loader.parseTextures(raw.textures,images),materials=loader.parseMaterials(raw.materials,textures),geometries=loader.parseGeometries(raw.geometries,{});
const scene=new THREE.Scene();scene.background=new THREE.Color(0x050b14);scene.add(new THREE.HemisphereLight(0xe4edf8,0x687582,2.1));
for(const chunk of [plan.background,...plan.chunks]){if(!chunk)continue;const object=loader.parseObject(raw.object.get(chunk.object),geometries,materials,textures,{});object.visible=!object.userData.areas?.length||object.userData.areas.includes('bridge');scene.add(object);}
const viewport={width:1440,height:900},camera=new THREE.PerspectiveCamera(67,viewport.width/viewport.height,.06,300);
camera.position.set(plan.position.x,1.7,plan.position.z);camera.lookAt(2.6,1.9,-5.5);
const frames=[];
for(const [name,ratio] of [['full-resolution',1],['interactive-budget',new FrameBudget({software:true}).pixelRatio(viewport.width,viewport.height,2)]]){
 const canvas=createCanvas(1,1),renderer=new SoftwareRenderer(canvas);renderer.setPixelRatio(ratio);renderer.setSize(viewport.width,viewport.height);const milliseconds=[];
 for(let i=0;i<8;i++){renderer.last=-1e9;const start=performance.now();renderer.render(scene,camera);if(i>=2)milliseconds.push(performance.now()-start);}
 const ordered=[...milliseconds].sort((a,b)=>a-b),pixels=canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height).data;
 frames.push({name,width:canvas.width,height:canvas.height,pixel_ratio:ratio,median_ms:ordered[3],min_ms:ordered[0],samples_ms:milliseconds,rgba_sha256:createHash('sha256').update(pixels).digest('hex'),...renderer.info.render});
 renderer.dispose();
}
console.log(JSON.stringify({scope:'CPU raster of serialized bridge-plan resources; not browser/WebGL or game FPS',world_version:manifest.version,world_bytes:manifest.world_bytes,viewport,warmup_frames:2,measured_frames:6,frame_interval_bypassed:true,node:process.version,platform:process.platform,architecture:process.arch,frames},null,2));
