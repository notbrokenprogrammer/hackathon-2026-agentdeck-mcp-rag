// Real model geometry and streaming lifecycle; no renderer, DOM, network or npm dependencies.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {createHash} from 'node:crypto';
import {registerHooks} from 'node:module';

const project=new URL('../',import.meta.url);
registerHooks({resolve(specifier,context,next){
 if(specifier==='three')return{url:new URL('web/vendor/three.module.js',project).href,shortCircuit:true};
 return next(specifier,context);
}});
const THREE=await import('../web/vendor/three.module.js');
const {CREW,BridgeCrew}=await import('../web/src/crew/BridgeCrew.js');
const {CosmonautModels,PORTRAITS}=await import('../web/src/crew/CosmonautModel.js');
const {walkable}=await import('../web/src/interior/DeckLayout.js');

function descendants(root,predicate){const found=[];root.traverse(o=>{if(predicate(o))found.push(o);});return found;}
function checkModel(model,profile){
 const {root,rig}=model;
 for(const name of ['body','head','arm0','arm1','leg0','leg1']){
  assert.ok(rig[name]?.isGroup,profile.id+': missing animated joint '+name);
  assert.equal(rig[name].userData.rig,name);
  assert.equal(descendants(root,o=>o===rig[name]).length,1,'joint must be owned exactly once');
 }
 assert.ok(rig.ring?.isMesh,'selection ring remains controllable');
 root.updateMatrixWorld(true);
 const bounds=new THREE.Box3().setFromObject(rig.body),height=bounds.max.y-bounds.min.y;
 assert.ok(height>1.8&&height<2.05,profile.id+': adult height is '+height);
 assert.ok(bounds.min.y>=-.005&&bounds.min.y<.02,profile.id+': feet must rest on the deck');
 assert.ok(bounds.max.x-bounds.min.x<1.1,'neutral arms fit the doorway scale');
 let triangles=0,meshes=0;
 root.traverse(o=>{
  if(!o.isMesh)return;
  meshes++;
  assert.equal(o.userData.agentID,profile.id,'every rendered piece remains an interaction target');
  const geometry=o.geometry,positions=geometry.getAttribute('position'),normals=geometry.getAttribute('normal');
  assert.ok(positions?.count>0&&positions.array.every(Number.isFinite),'finite nonempty vertices');
  assert.ok(normals?.count===positions.count&&normals.array.every(Number.isFinite),'finite matching normals');
  for(let i=0;i<normals.count;i++)assert.ok(Math.abs(Math.hypot(normals.getX(i),normals.getY(i),normals.getZ(i))-1)<.01,'unit-length normals');
  assert.ok(o.matrixWorld.elements.every(Number.isFinite),'finite joint transform');
  triangles+=(geometry.index?.count??positions.count)/3;
 });
 assert.ok(triangles<80000,profile.id+': portrait exceeds the 80k triangle budget: '+triangles);
 assert.ok(meshes<=60,profile.id+': portrait exceeds 60 draws before canvas patches: '+meshes);
 const faces=descendants(root,o=>o.name==='sculpted-face');
 assert.equal(faces.length,1,'one unique 3D portrait surface');
 const face=faces[0],vertices=face.geometry.getAttribute('position').array;
 const signature=createHash('sha256').update(Buffer.from(vertices.buffer,vertices.byteOffset,vertices.byteLength)).digest('hex');
 const faceBounds=new THREE.Box3().setFromObject(face);
 assert.ok(faceBounds.max.z-faceBounds.min.z>.2,'head has volume in side/rear views');
 const point=rig.head.getWorldPosition(new THREE.Vector3());point.x+=.065;point.y-=.025;
 const ray=new THREE.Raycaster(new THREE.Vector3(point.x,point.y,-2),new THREE.Vector3(0,0,1));
 const hits=ray.intersectObject(root,true);
 assert.ok(hits.length>0,'portrait is raycastable');
 assert.equal(hits[0].object.userData.agentID,profile.id);
 return{id:profile.id,height_m:+height.toFixed(3),feet_y:+bounds.min.y.toFixed(4),triangles,meshes,signature};
}

assert.equal(CREW.length,4);assert.deepEqual(new Set(CREW.map(p=>p.id)),new Set(Object.keys(PORTRAITS)));
for(const profile of CREW)assert.ok(walkable(profile.position[0],profile.position[2],.48),profile.id+': suit must clear walls and large furniture at its home position');
const factory=new CosmonautModels(),metrics=[];
try{for(const profile of CREW)metrics.push(checkModel(factory.create(profile),profile));}
finally{factory.dispose();}
assert.equal(new Set(metrics.map(m=>m.signature)).size,4,'all four heads need different sculpted geometry');

// Parse the shipped AOI resource roots: this exercises the integration with
// existing identities, labels and streamed transforms rather than fake rigs.
const manifest=JSON.parse(fs.readFileSync(new URL('world/manifest.json',project)));
const raw={images:[],textures:[],materials:[],geometries:[],objects:new Map()};
for(const resource of manifest.resources){
 const value=JSON.parse(fs.readFileSync(new URL('world/'+resource.id+'.json',project)));
 if(value.kind==='object')raw.objects.set(resource.id,value.body);else raw[value.kind].push(value.body);
}
const loader=new THREE.ObjectLoader(),images=loader.parseImages(raw.images,()=>{}),textures=loader.parseTextures(raw.textures,images),materials=loader.parseMaterials(raw.materials,textures),geometries=loader.parseGeometries(raw.geometries,{});
const chunks=manifest.chunks.filter(c=>c.npc);
const loadRoots=()=>chunks.map(c=>loader.parseObject(raw.objects.get(c.low.object),geometries,materials,textures,{}));
const scene=new THREE.Scene(),crew=new BridgeCrew(scene);
try{
 let roots=loadRoots();for(const root of roots){scene.add(root);crew.register(root);}
 assert.equal(crew.characters.filter(c=>c.loaded).length,4);
 const cached=new Map(crew.characters.map(c=>[c.profile.id,{model:c.model,rig:c.rig,home:c.home.clone(),baseRotation:c.baseRotation}]));
 const cacheSize=crew.models.geometries.size;
 const assertAttached=()=>{
  for(const c of crew.characters){
   const saved=cached.get(c.profile.id);
   assert.equal(c.loaded,true);if(!c.group.userData.bakedPortrait)assert.ok(c.model===saved.model,'reuse legacy portrait objects across AOI changes');
   if(!c.group.userData.bakedPortrait)assert.ok(c.rig===saved.rig,'registration must preserve the complete animation rig');
   for(const joint of ['head','body','arm0','arm1','leg0','leg1','ring'])assert.ok(c.rig[joint],'baked rig retains '+joint);
   assert.ok(c.rig.ring===c.model.rig.ring,'repeat registration must not lose the selection ring');
   assert.ok(c.model.root.parent===c.group,'model remains attached to the streamed root');
   assert.equal(descendants(c.group,o=>o.isSprite).length,0,'streamed bitmap nameplates are replaced by native text');
   assert.equal(descendants(c.group,o=>o.userData.portrait===c.profile.id&&o!==c.group).length,1,'no duplicated portrait model');
   assert.equal(c.home.distanceTo(saved.home),0,'registration must not move the patrol home');
   assert.equal(c.baseRotation,saved.baseRotation,'registration must not replace the rest rotation');
  }
  assert.equal(crew.models.geometries.size,cacheSize,'reentry must not allocate another full model');
 };
 assertAttached();
 for(const root of roots)crew.register(root);
 assertAttached();
 crew.setBehaviors([{id:'savinykh',behavior:'patrol'}]);crew.animate(1,new THREE.Vector3());
 assert.notEqual(crew.characters.find(c=>c.profile.id==='savinykh').rig.leg0.rotation.x,0,'patrol still animates legs');
 for(const root of roots)crew.register(root);
 assertAttached();
 crew.setBehaviors([]);crew.animate(0,new THREE.Vector3(),true);
 scene.updateMatrixWorld(true);
 for(const c of crew.characters){
  const target=c.rig.head.getWorldPosition(new THREE.Vector3());
  const camera=new THREE.PerspectiveCamera(50,4/3,.05,20);camera.position.copy(target).add(new THREE.Vector3(0,0,-3));camera.lookAt(target);camera.updateMatrixWorld(true);
  const canvas={getBoundingClientRect:()=>({left:0,top:0,width:800,height:600})};
  assert.equal(crew.pick({clientX:400,clientY:300},canvas,camera),c.profile.id,'pick the correct streamed agent');
 }
 // Same cached root, then a newly decoded AOI root, must both restore the model.
 for(const root of roots){crew.unregister(root);scene.remove(root);}
 assert.ok(crew.characters.every(c=>!c.loaded&&Object.keys(c.rig).length===0));
 for(const root of roots){scene.add(root);crew.register(root);}assertAttached();
 for(const root of roots){crew.unregister(root);scene.remove(root);}
 roots=loadRoots();for(const root of roots){scene.add(root);crew.register(root);}assertAttached();
 crew.animate(2,new THREE.Vector3());
 assert.ok(crew.characters.every(c=>c.rig.head.rotation.toArray().slice(0,3).every(Number.isFinite)));
}finally{crew.dispose();}

console.log(JSON.stringify({passed:true,models:metrics.map(({signature,...m})=>m),checks:['four unique volumetric heads','finite vertices and unit normals','adult proportions and grounded boots','animated joints and selection ring','real streamed roots','idempotent registration','AOI unload/reload cache reuse','agent raycast identity','triangle and draw budgets']}));
