import assert from 'node:assert/strict';
import {registerHooks,createRequire} from 'node:module';
const root=new URL('../',import.meta.url);
registerHooks({resolve(specifier,context,next){if(specifier==='three')return{url:new URL('web/vendor/three.module.js',root).href,shortCircuit:true};return next(specifier,context);}});
const require=createRequire(process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES?process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES+'/package.json':import.meta.url);
const {createCanvas}=require('@napi-rs/canvas');
const THREE=await import('../web/vendor/three.module.js');
const {SoftwareRenderer}=await import('../web/src/render/SoftwareRenderer.js');
const canvas=createCanvas(160,160),renderer=new SoftwareRenderer(canvas);renderer.setSize(160,160);
const scene=new THREE.Scene(),camera=new THREE.PerspectiveCamera(50,1,.1,100);camera.position.z=3;
const plane=new THREE.Mesh(new THREE.PlaneGeometry(2,2),new THREE.MeshStandardMaterial({color:0x506070}));scene.add(plane);
function sample(){renderer.last=-1e9;renderer.render(scene,camera);const p=canvas.getContext('2d').getImageData(77,81,1,1).data;assert.equal(p[3],255);return Array.from(p).slice(0,3);}
const unlit=sample();const light=new THREE.HemisphereLight(0xff0000,0xff0000,2.6);scene.add(light);const red=sample();light.color.set(0x0000ff);light.groundColor.set(0x0000ff);const blue=sample();
assert.ok(red[0]>blue[0]+15,'red ambient changes actual red pixel channel');assert.ok(blue[2]>red[2]+15,'blue ambient changes actual blue pixel channel');
light.intensity=.26;const dim=sample();assert.ok(dim.reduce((a,b)=>a+b,0)<blue.reduce((a,b)=>a+b,0)*.8,'lower intensity visibly dims geometry');
scene.remove(light);assert.deepEqual(sample(),unlit,'no-light legacy shading retained');
plane.material=new THREE.MeshBasicMaterial({color:0x506070});const basic=sample();scene.add(light);light.intensity=0;assert.deepEqual(sample(),basic,'unlit UI-style geometry unaffected');
plane.material=new THREE.MeshStandardMaterial({color:0x506070,emissive:0x00ff00,emissiveIntensity:.5});const emission=sample();assert.ok(emission[1]>0&&emission[0]===0&&emission[2]===0,'emissive equipment remains visible at zero ambient');
console.log(JSON.stringify({passed:true,checks:['ambient color changes rendered pixels','ambient intensity dims rendered pixels','legacy scene without light retained','basic material independent of light','emission persists in darkness'],pixels:{unlit,red,blue,dim}}));

// Actual pixels, not an implementation mirror: the camera cuts through a large
// triangle. The visible remainder must survive clipping and occlude the blue rear.
scene.clear();camera.position.set(0,0,0);camera.lookAt(0,0,-1);
const rear=new THREE.Mesh(new THREE.PlaneGeometry(20,20),new THREE.MeshBasicMaterial({color:0x0000ff}));rear.position.z=-3;scene.add(rear);
const geometry=new THREE.BufferGeometry();geometry.setAttribute('position',new THREE.Float32BufferAttribute([-2,-2,-2,2,-2,-2,0,2,.1],3));geometry.computeVertexNormals();scene.add(new THREE.Mesh(geometry,new THREE.MeshBasicMaterial({color:0xff0000,side:THREE.DoubleSide})));
const clipped=sample();assert.ok(clipped[0]>200&&clipped[2]<20,'near-clipped foreground must occlude rear geometry');
console.log('Canvas near-plane clipping and depth occlusion — passed');

// Surface tiles have negative/extruded UVs; screens use a flipped data texture.
// Check actual pixels against a four-colour tile, including its repeat seam.
scene.clear();
const tile=new THREE.DataTexture(new Uint8Array([
 255,0,0,255, 0,255,0,255,
 0,0,255,255, 255,255,0,255
]),2,2);
const boardGeometry=new THREE.PlaneGeometry(2,2),board=new THREE.Mesh(boardGeometry,new THREE.MeshBasicMaterial({color:0xffffff,map:tile}));board.position.z=-2;scene.add(board);
const tileSample=(u,v)=>{const uv=boardGeometry.attributes.uv;for(let i=0;i<uv.count;i++)uv.setXY(i,u,v);return sample();};
tile.wrapS=tile.wrapT=THREE.RepeatWrapping;
assert.deepEqual(tileSample(.25,.25),[255,0,0],'data texture keeps authored row orientation');
assert.deepEqual(tileSample(-.25,.25),[0,255,0],'negative extruded UV repeats instead of stretching the border');
assert.deepEqual(tileSample(1.25,.25),[255,0,0],'positive UV beyond one repeats');
tile.wrapS=THREE.ClampToEdgeWrapping;assert.deepEqual(tileSample(-.25,.25),[255,0,0],'screen edge stays clamped');
tile.wrapS=THREE.MirroredRepeatWrapping;assert.deepEqual(tileSample(-.25,.25),[255,0,0]);assert.deepEqual(tileSample(1.25,.25),[0,255,0]);
tile.wrapS=THREE.RepeatWrapping;tile.repeat.set(2,2);tile.offset.set(.55,.55);assert.deepEqual(tileSample(.1,.1),[255,255,0],'texture repeat and offset applied before wrapping');
tile.repeat.set(1,-1);tile.offset.set(0,1);assert.deepEqual(tileSample(.25,.25),[0,0,255],'serialized screen flip remains correct');
tile.flipY=true;assert.deepEqual(tileSample(.25,.25),[255,0,0],'explicit flipY applied once');
tile.flipY=false;tile.repeat.set(1,1);tile.offset.set(0,0);tile.magFilter=THREE.LinearFilter;
const seam=tileSample(0,.25);assert.ok(seam[0]>=127&&seam[0]<=128&&seam[1]>=127&&seam[1]<=128&&seam[2]===0,'linear repeat blends the two sides of the tile seam');
tile.wrapS=THREE.ClampToEdgeWrapping;assert.deepEqual(tileSample(0,.25),[255,0,0],'linear screen sampler cannot wrap into the opposite edge');
console.log('Canvas UV transforms, negative/repeated/mirrored UVs, screen orientation and bilinear tile seams — passed');

// A discarded texel must not paint or write depth over the opaque rear plane.
scene.clear();scene.add(rear,board);board.material.alphaTest=.5;
board.material.map=new THREE.DataTexture(new Uint8Array([255,0,0,64]),1,1);
assert.deepEqual(sample(),[0,0,255],'alpha-tested texture exposes the rear surface');
board.material.alphaTest=0;board.material.map=new THREE.DataTexture(new Uint8Array([255,0,0,0]),1,1);
assert.deepEqual(sample(),[0,0,255],'fully transparent texel preserves the rear surface');
console.log('Canvas alpha-tested and transparent texture cutouts — passed');
