// Deterministic render of shipped geometry, not a browser screenshot or FPS benchmark.
import fs from 'node:fs';
import {createRequire} from 'node:module';
import * as THREE from '../web/vendor/three.module.js';
const require=createRequire(process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES?process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES+'/package.json':import.meta.url);
const {createCanvas,ImageData}=require('@napi-rs/canvas');
globalThis.document={createElement:()=>createCanvas(1,1)};globalThis.ImageData=ImageData;
const base=new URL('../',import.meta.url);
const code=fs.readFileSync(new URL('web/src/render/SoftwareRenderer.js',base),'utf8').replace("from 'three'",`from '${new URL('web/vendor/three.module.js',base).href}'`);
const {SoftwareRenderer}=await import('data:text/javascript;base64,'+Buffer.from(code).toString('base64'));
const manifest=JSON.parse(fs.readFileSync(new URL('world/manifest.json',base)));
const raw={images:[],textures:[],materials:[],geometries:[],object:new Map()};
for(const r of manifest.resources){const d=JSON.parse(fs.readFileSync(new URL('world/'+r.id+'.json',base)));if(d.kind==='object')raw.object.set(r.id,d.body);else raw[d.kind].push(d.body);}
const loader=new THREE.ObjectLoader();
const images=loader.parseImages(raw.images,()=>{}), textures=loader.parseTextures(raw.textures,images),materials=loader.parseMaterials(raw.materials,textures),geometries=loader.parseGeometries(raw.geometries,{});
const scene=new THREE.Scene();
for(const c of manifest.chunks){scene.add(loader.parseObject(raw.object.get(c.low.object),geometries,materials,textures,{}));}
const out=new URL('docs/frontend/',base);fs.mkdirSync(out,{recursive:true});
const canvas=createCanvas(1600,1000),renderer=new SoftwareRenderer(canvas);renderer.setSize(1600,1000);
const camera=new THREE.PerspectiveCamera(43,1.6,.1,500);camera.position.set(37,35,44);camera.lookAt(0,0,-6);renderer.last=-1000;renderer.render(scene,camera);
const ctx=canvas.getContext('2d');ctx.globalCompositeOperation='destination-over';ctx.fillStyle='#0b1621';ctx.fillRect(0,0,1600,1000);ctx.globalCompositeOperation='source-over';ctx.fillStyle='#96b7c6';ctx.font='17px sans-serif';ctx.fillText('HORIZON–7 / ПРОЦЕДУРНАЯ ГЕОМЕТРИЯ · CANVAS REVIEW',35,45);
fs.writeFileSync(new URL('ship-model-review.png',out),canvas.toBuffer('image/png'));
const sheet=createCanvas(1600,650),sheetCtx=sheet.getContext('2d');sheetCtx.fillStyle='#0b1621';sheetCtx.fillRect(0,0,1600,650);
const portraits=[];scene.traverse(o=>{if(o.userData.npcRoot)portraits.push(o);});
for(let i=0;i<portraits.length;i++){
 const character=portraits[i].clone(true);character.position.set(0,0,0);character.rotation.y=-.1;character.children.filter(c=>c.isSprite).forEach(c=>character.remove(c));
 const portraitScene=new THREE.Scene();portraitScene.add(character);const c=createCanvas(400,610),r=new SoftwareRenderer(c);r.setSize(400,610);
 const cam=new THREE.PerspectiveCamera(33,400/610,.1,100);cam.position.set(-.4,1.5,-4.4);cam.lookAt(0,1.1,0);r.last=-1000;r.render(portraitScene,cam);sheetCtx.drawImage(c,i*400,30);
 sheetCtx.fillStyle='#9bbfc9';sheetCtx.font='15px sans-serif';sheetCtx.fillText(manifest.crew.find(x=>x.id===character.userData.npcRoot).short,i*400+45,40);
}
fs.writeFileSync(new URL('crew-model-review.png',out),sheet.toBuffer('image/png'));
console.log(JSON.stringify({rendered:['ship-model-review.png','crew-model-review.png'],note:'SoftwareRenderer output from rebuilt assets; no browser, UI, hardware WebGL or performance claims'}));
