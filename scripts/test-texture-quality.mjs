import assert from 'node:assert/strict';
import fs from 'node:fs';
import {registerHooks} from 'node:module';
registerHooks({resolve(s,c,next){if(s==='three')return{url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true};return next(s,c);}});
const THREE=await import('../web/vendor/three.module.js');
const {configureTexture}=await import('../web/src/render/TextureQuality.js');
const manifest=JSON.parse(fs.readFileSync(new URL('../world/manifest.json',import.meta.url)));
const raw={images:[],textures:[]};
for(const resource of manifest.resources)if(raw[resource.kind])raw[resource.kind].push(JSON.parse(fs.readFileSync(new URL('../world/'+resource.id+'.json',import.meta.url))).body);
const loader=new THREE.ObjectLoader(),images=loader.parseImages(raw.images,()=>{}),textures=loader.parseTextures(raw.textures,images);
let repaired=0;
for(const texture of Object.values(textures)){
 const original={source:texture.source,repeat:texture.repeat.clone(),offset:texture.offset.clone(),flipY:texture.flipY,colorSpace:texture.colorSpace,wrapS:texture.wrapS,wrapT:texture.wrapT};
 if(!texture.generateMipmaps)repaired++;
 configureTexture(texture,{capabilities:{getMaxAnisotropy:()=>16}});
 assert.equal(texture.generateMipmaps,true);assert.equal(texture.minFilter,THREE.LinearMipmapLinearFilter);assert.equal(texture.anisotropy,4);
 for(const key of ['source','flipY','colorSpace','wrapS','wrapT'])assert.equal(texture[key],original[key]);
 assert.ok(texture.repeat.equals(original.repeat)&&texture.offset.equals(original.offset));
 const version=texture.version;configureTexture(texture,{capabilities:{getMaxAnisotropy:()=>16}});assert.equal(texture.version,version,'unchanged sampler does not reupload');
 configureTexture(texture,{capabilities:{getMaxAnisotropy:()=>2}});assert.equal(texture.anisotropy,2);
 configureTexture(texture,{isSoftware:true});assert.equal(texture.anisotropy,1);
}
assert.ok(Object.keys(textures).length>0);
console.log(JSON.stringify({passed:true,textures:Object.keys(textures).length,repaired,checks:['actual serialized world textures','mipmapped minification','bounded hardware anisotropy','authored UV/flip/colour space preserved','no redundant texture upload'],scope:'texture loader state; hardware appearance needs browser verification'}));
