import * as THREE from 'three';
import {layout,roomAt} from './DeckLayout.js';
import {buildReferenceArchitecture} from './ReferenceArchitecture.js';
import {buildReferenceEquipment} from './ReferenceEquipment.js';
import {drawInstrumentDisplay} from './InstrumentDisplay.js';

// All geometry is real, reusable 3D. Static parts are batched by room/material;
// ceilings remain physical and are removed only in the tactical cutaway view.
export class ShipInterior {
 constructor(ship,{streamed=false,batch=true}={}) {
  this.ship=ship;this.root=new THREE.Group();this.root.name='HORIZON 7 • LIGHT INTERIOR';ship.scene.add(this.root);
  this.streamed=streamed;if(streamed)ship.scene.background=new THREE.Color(0x050b14);this.chunkRoots=new Set();this.visibleRooms=new Set();
  this.systemMaterials=new Map();this.chunkSystemMaterials=new Map();this.systemState=null;this.visibilityMode=null;this.visibilityArea=null;this.visibilityDoors=-1;
  this.lightingEnabled=true;this.lightIntensity=1;
  this.materials=new Map();this.geometries=new Map();this.rooms=new Map();this.ceilings=[];this.textures=[];this.systems=[];
  this.boxGeo=new THREE.BoxGeometry(1,1,1);this.cylinderGeo=new THREE.CylinderGeometry(1,1,1,24);this.sphereGeo=new THREE.SphereGeometry(1,32,20);
  const shape=new THREE.Shape();shape.moveTo(-.46,-.46);shape.lineTo(.46,-.46);shape.lineTo(.46,.46);shape.lineTo(-.46,.46);shape.closePath();
  this.bevelGeo=new THREE.ExtrudeGeometry(shape,{depth:.92,bevelEnabled:true,bevelThickness:.04,bevelSize:.04,bevelSegments:2,steps:1,curveSegments:1});this.bevelGeo.translate(0,0,-.46);this.bevelGeo.computeVertexNormals();
  for(const [id,color,roughness,metalness] of [['pearl',0xdde3e6,.48,.22],['white',0xf0f1ec,.62,.12],['floor',0x9caab2,.47,.42],['metal',0x718491,.32,.72],['dark',0x23313f,.62,.35],['rubber',0x111e29,.93,.02],['blue',0x23589c,.39,.4],['fabric',0x384d63,.96,0],['green',0x6d9b7e,.8,0],['warm',0xa58c68,.7,.1]]) this.materials.set(id,new THREE.MeshStandardMaterial({color,roughness,metalness}));
  for(const [id,color] of [['light',0xd9efff],['cyan',0x4fb8ff],['amber',0xf3c58a],['mint',0x9de2c3]]) this.materials.set(id,new THREE.MeshStandardMaterial({color,emissive:color,emissiveIntensity:id==='light'?1.1:.65,roughness:.4}));
  this.materials.set('reactor',this.mat('cyan').clone());this.materials.set('cooling',this.mat('cyan').clone());this.materials.set('plant',this.mat('green').clone());this.materials.set('grow',this.mat('light').clone());
  this.materials.set('glass',new THREE.MeshPhysicalMaterial({color:0xaccfea,transparent:true,opacity:.12,roughness:.08,metalness:.1,depthWrite:false}));
  if(streamed){for(const a of layout.areas){const g=new THREE.Group();g.name=a.name;this.root.add(g);this.rooms.set(a.id,g);}}
  else {buildReferenceArchitecture(this);buildReferenceEquipment(this);}
  if(!ship.renderer.isSoftware&&typeof document!=='undefined'){
   const c=document.createElement('canvas');c.width=256;c.height=128;const ctx=c.getContext('2d'),grad=ctx.createLinearGradient(0,0,0,128);grad.addColorStop(0,'#c7d4e0');grad.addColorStop(.35,'#edf0ec');grad.addColorStop(.55,'#73808b');grad.addColorStop(1,'#263441');ctx.fillStyle=grad;ctx.fillRect(0,0,256,128);ctx.fillStyle='#ffffff';for(const x of [25,110,195])ctx.fillRect(x,25,35,5);const tex=new THREE.CanvasTexture(c);tex.colorSpace=THREE.SRGBColorSpace;tex.mapping=THREE.EquirectangularReflectionMapping;const generator=new THREE.PMREMGenerator(ship.renderer);this.environment=generator.fromEquirectangular(tex);ship.scene.environment=this.environment.texture;generator.dispose();tex.dispose();
  }
  if(!streamed&&batch){
   for(const roof of this.ceilings)this.batch(roof);
   for(const group of this.rooms.values())this.batch(group);
   this.batch(this.bulkheads);
  }
  // One shadow caster follows the room, never the camera. No lamp shines through a wall.
  this.workLight=new THREE.SpotLight(0xe7f0ff,90,32,1.43,.65,1.4);this.workLight.castShadow=true;this.workLight.shadow.mapSize.set(1024,1024);this.workLight.shadow.bias=-.0002;this.workLight.shadow.normalBias=.03;this.workLight.shadow.camera.near=.1;this.workLight.shadow.camera.far=30;this.root.add(this.workLight,this.workLight.target);
  if(!streamed)this.registerSystems(this.root);
 }
 // Static authored nodes arrive exclusively through the hashed AOI resources.
 // This controller never generates a second full ship in the running game.
 attachChunk(root){
  if(this.chunkRoots.has(root))return;
  this.chunkRoots.add(root);const areas=root.userData.areas||[];
  (areas.length===1?this.rooms.get(areas[0])||this.root:this.root).add(root);
  const roofs=[];
  if(root.userData.role?.startsWith('ceiling'))roofs.push(root);
  root.traverse(o=>{if(o.userData.interiorRole==='ceiling'&&!roofs.includes(o))roofs.push(o);});this.ceilings.push(...roofs);
  this.batch(root);
  this.updateState(this.ship.lastState||this.systemState||{});this.registerSystems(root);
  this.update(this.ship.mode,this.ship.player);
  // An arriving chunk does not change room connectivity. Initialize only its
  // visibility; revisiting every resident mesh here made loading quadratic.
  root.visible=!areas.length||areas.some(id=>this.visibleRooms.has(id));
  for(const roof of roofs)roof.visible=this.ship.mode!=='overview';
 }
 detachChunk(root){
  if(!this.chunkRoots.delete(root))return;
  for(const material of this.chunkSystemMaterials.get(root)||[]){const entry=this.systemMaterials.get(material);if(entry&&--entry.references===0)this.systemMaterials.delete(material);}this.chunkSystemMaterials.delete(root);
  const removed=new Set();root.traverse(o=>{removed.add(o);if(o.isInstancedMesh)o.dispose();});
  this.ceilings=this.ceilings.filter(o=>!removed.has(o));root.removeFromParent();
 }
 registerSystems(root){
  if(this.chunkSystemMaterials.has(root))return;
  const found=new Map();root.traverse(o=>{for(const material of Array.isArray(o.material)?o.material:o.material?[o.material]:[]){const type=material.userData.system||o.userData.system;if(['reactor','cooling','plant','greenhouse','grow','grow-lamp','power'].includes(type))found.set(material,type);}});
  this.chunkSystemMaterials.set(root,new Set(found.keys()));
  for(const [material,type]of found){const entry=this.systemMaterials.get(material);if(entry)entry.references++;else{this.systemMaterials.set(material,{type,references:1});if(this.systemState)this.applySystemState(material,type,this.systemState);}}
 }
 mat(id){return typeof id==='string'?this.materials.get(id):id;}
 part(g,geo,mat,x,y,z,w,h,d,rotation){const m=new THREE.Mesh(geo,this.mat(mat));m.position.set(x,y,z);m.scale.set(w,h,d);if(rotation)m.rotation.set(...rotation);m.castShadow=true;m.receiveShadow=true;g.add(m);return m;}
 box(g,mat,x,y,z,w,h,d,rotation){return this.part(g,this.boxGeo,mat,x,y,z,w,h,d,rotation);}
 bevel(g,mat,x,y,z,w,h,d,rotation){return this.part(g,this.bevelGeo,mat,x,y,z,w,h,d,rotation);}
 cylinder(g,mat,x,y,z,r,h,rotation){return this.part(g,this.cylinderGeo,mat,x,y,z,r,h,r,rotation);}
 pipe(g,mat,from,to,r=.035){const a=new THREE.Vector3(...from),b=new THREE.Vector3(...to),direction=b.clone().sub(a),center=a.add(b).multiplyScalar(.5);const m=this.cylinder(g,mat,center.x,center.y,center.z,r,direction.length());m.quaternion.setFromUnitVectors(new THREE.Vector3(0,1,0),direction.normalize());return m;}
 screws(g,w,h,z,y=0){for(const x of [-1,1])for(const sy of [-1,1]){this.cylinder(g,'metal',x*(w/2-.07),y+sy*(h/2-.07),z,.023,.018,[Math.PI/2,0,0]);this.box(g,'dark',x*(w/2-.07),y+sy*(h/2-.07),z+.012,.024,.004,.003);}}
 wallDetail(a,g,b,x,z,service=false){
  const horizontal=b.w>b.d,nx=horizontal?0:Math.sign(a.x-b.x),nz=horizontal?Math.sign(a.z-b.z):0;
  const kit=new THREE.Group();kit.name=service?'Recessed service cabinet':'Air return grille';kit.position.set(x+nx*.17,0,z+nz*.17);kit.rotation.y=Math.atan2(nx,nz);g.add(kit);
  if(service){
   this.bevel(kit,'metal',0,1.68,.02,.94,1.24,.07);this.box(kit,'rubber',0,1.68,.061,.82,1.1,.015);
   this.bevel(kit,'white',0,1.68,.085,.72,1,.04);this.box(kit,'blue',0,2.07,.109,.68,.045,.008);
   this.box(kit,'dark',.19,1.68,.115,.07,.25,.025);this.pipe(kit,'metal',[.19,1.59,.14],[.19,1.77,.14],.018);
   this.box(kit,'mint',-.22,1.9,.112,.055,.025,.012);this.box(kit,'amber',-.13,1.9,.112,.025,.025,.012);
   this.label(kit,'ACCESS / 07',-.04,1.37,.112,.43,.075);this.screws(kit,.94,1.24,.065,1.68);
  }
  this.bevel(kit,'metal',0,2.85,.02,1.22,.44,.08);this.box(kit,'rubber',0,2.85,.065,1.08,.31,.018);
  for(let i=0;i<6;i++)this.box(kit,'pearl',0,2.72+i*.052,.086,1.03,.018,.04,[.2,0,0]);
  this.screws(kit,1.22,.44,.075,2.85);
  for(const sx of [-.53,.53]){this.box(kit,'dark',sx,1.04,.025,.075,.12,.08);this.pipe(kit,'metal',[sx,1.04,.06],[sx,1.04,.145],.024);}
  this.pipe(kit,'metal',[-.53,1.04,.145],[.53,1.04,.145],.027);
 }
 group(id,x=0,z=0,angle=0){const g=new THREE.Group();g.position.set(x,0,z);g.rotation.y=angle;(this.rooms.get(id)||this.root).add(g);return g;}
 label(g,text,x,y,z,w=.9,h=.22,rotation=0,dark=false){
  if(typeof document==='undefined')return;
  const key='text:'+text+':'+dark+':'+w/h;let mat=this.materials.get(key);
  if(!mat){const c=document.createElement('canvas');c.width=1024;c.height=Math.max(64,Math.round(1024*h/w));const ctx=c.getContext('2d');ctx.fillStyle=dark?'#132839':'#dbe3e6';ctx.fillRect(0,0,1024,c.height);ctx.fillStyle=dark?'#bde4fa':'#28465f';ctx.font='500 '+Math.round(c.height*.54)+'px sans-serif';ctx.textBaseline='middle';ctx.fillText(text,32,c.height/2,960);const t=new THREE.CanvasTexture(c);t.colorSpace=THREE.SRGBColorSpace;this.textures.push(t);mat=new THREE.MeshStandardMaterial({map:t,roughness:.65,emissive:dark?0x7bbedc:0x000000,emissiveMap:dark?t:null,emissiveIntensity:.25});this.materials.set(key,mat);}
  const m=this.box(g,mat,x,y,z,w,h,.012);m.rotation.y=rotation;return m;
 }
 screen(g,x,y,z,w=1.7,h=.88,title='FLIGHT / NAVIGATION',angle=-.16){
  const frame=new THREE.Group();frame.position.set(x,y,z);frame.rotation.x=angle;g.add(frame);
  this.bevel(frame,'metal',0,0,0,w+.14,h+.14,.15);this.bevel(frame,'rubber',0,0,.084,w+.06,h+.06,.035);
  const key='screen:'+title;let mat=this.materials.get(key);
  if(!mat&&typeof document!=='undefined'){
   const c=document.createElement('canvas');c.width=768;c.height=384;drawInstrumentDisplay(c.getContext('2d'),title);
   const t=new THREE.CanvasTexture(c);t.colorSpace=THREE.SRGBColorSpace;this.textures.push(t);mat=new THREE.MeshStandardMaterial({map:t,emissiveMap:t,emissive:0xffffff,emissiveIntensity:.5,roughness:.35});this.materials.set(key,mat);
  }
  this.box(frame,mat||'dark',0,0,.108,w,h,.012);for(const side of [-1,1])this.cylinder(frame,'metal',side*(w/2+.03),-h/2-.025,.1,.016,.02,[Math.PI/2,0,0]);return frame;
 }
 batch(group){
  if(this.ship.renderer.isSoftware||!group)return;
  const animated=o=>o.userData.playerTemplate||o.userData.schematicRoot||o.userData.npcRoot||o.userData.rig||o.userData.articulated;
  if(animated(group))return;
  group.updateMatrixWorld(true);const batches=new Map(),inverse=new THREE.Matrix4().copy(group.matrixWorld).invert(),transform=new THREE.Matrix4();
  group.traverse(o=>{
   let p=o,protectedNode=false,roof=false;while(p&&p!==group){if(animated(p))protectedNode=true;if(p.name==='ceiling')roof=true;p=p.parent;}
   if(protectedNode||o.isSkinnedMesh||o.morphTargetInfluences?.length)return;
   // Keep local matrices for immutable authored parts. The group itself stays
   // live, so door leaves can still move and chunks can be reparented safely.
   if(o!==group)o.matrixAutoUpdate=false;
   if(roof||!o.isMesh||o.isInstancedMesh||Array.isArray(o.material)||o.material.transparent||(o.material.map&&!o.material.userData.staticSurface))return;
   const key=[o.geometry.uuid,o.material.uuid,o.userData.system||'',o.castShadow,o.receiveShadow,o.renderOrder,o.layers.mask].join(':');const list=batches.get(key)||[];list.push(o);batches.set(key,list);
  });
  for(const list of batches.values()){if(list.length<3)continue;const first=list[0],mesh=new THREE.InstancedMesh(first.geometry,first.material,list.length);mesh.castShadow=first.castShadow;mesh.receiveShadow=first.receiveShadow;mesh.renderOrder=first.renderOrder;mesh.layers.mask=first.layers.mask;if(first.userData.system)mesh.userData.system=first.userData.system;for(const [i,o]of list.entries()){mesh.setMatrixAt(i,transform.multiplyMatrices(inverse,o.matrixWorld));o.parent.remove(o);}mesh.computeBoundingSphere();mesh.matrixAutoUpdate=false;group.add(mesh);}
 }
 update(mode,position){if(!position)return;const area=roomAt(position.x,position.z);
  // Door animation matters to visibility only when a passage opens or closes.
  // Avoid reallocating Sets and walking all chunks on every stationary frame.
  let doors=0;for(let n=0;n<layout.gates.length;n++){const gate=layout.gates[n],door=this.ship.livingWorld?.objects.get(gate.id);if(!gate.door||(door&&door.progress>=.02))doors|=1<<n;}
  if(this.visibilityMode===mode&&this.visibilityArea===area?.id&&this.visibilityDoors===doors)return;
  this.visibilityMode=mode;this.visibilityArea=area?.id;this.visibilityDoors=doors;
  const visible=new Set(mode==='overview'?layout.areas.map(a=>a.id):[area?.id]);
  if(mode!=='overview'){let changed=true;while(changed){changed=false;for(let n=0;n<layout.gates.length;n++){const g=layout.gates[n];if(!(doors&(1<<n)))continue;if(g.areas.some(id=>visible.has(id)))for(const id of g.areas)if(!visible.has(id)){visible.add(id);changed=true;}}}}
  this.visibleRooms=visible;for(const [id,g]of this.rooms)g.visible=visible.has(id);
  for(const root of this.chunkRoots){const areas=root.userData.areas||[];root.visible=!areas.length||areas.some(id=>visible.has(id));}
  for(const roof of this.ceilings)roof.visible=mode!=='overview';
  if(area&&this.litRoom!==area.id){this.litRoom=area.id;this.workLight.position.set(area.x,3.4,area.z);this.workLight.target.position.set(area.x,0,area.z);this.workLight.intensity=(area.kind==='corridor'?40:90)*this.lightIntensity;}
  this.workLight.visible=mode==='explore'&&this.lightingEnabled;if(this.ship.sunLight)this.ship.sunLight.visible=mode==='overview';
 }
 setLighting(enabled,intensity=1){
  enabled=enabled!==false;intensity=Math.max(.2,Math.min(2,Number(intensity)||1));
  if(this.lightingEnabled===enabled&&this.lightIntensity===intensity)return;
  this.lightingEnabled=enabled;this.lightIntensity=intensity;
  // Invalidate only lighting/topology cache; streamed chunks retain their resources.
  this.visibilityMode=null;this.litRoom=null;
  for(const [material,{type}]of this.systemMaterials)if(type==='power')this.applySystemState(material,type,this.systemState||{});
  this.update(this.ship.mode,this.ship.player);
 }
 applySystemState(m,type,s){
  if(type==='reactor'){m.emissiveIntensity=s.reactor_active?.85:.16;m.color.set(s.cooling_repaired?0x8fcbdc:0xc9a47c);m.emissive?.copy(m.color);}
  else if(type==='cooling'){m.color.set(s.cooling_repaired?0x7fc6cc:0xd8b076);m.emissive?.copy(m.color);}
  else if(type==='plant'||type==='greenhouse')m.color.set(s.greenhouse_active?0x6d9b7e:0x71856a);
  else if(type==='grow'||type==='grow-lamp')m.emissiveIntensity=s.greenhouse_active?1:.18;
  // The personal work-light circuit remains available during the reactor mission.
  else if(type==='power')m.emissiveIntensity=this.lightingEnabled?1.1*this.lightIntensity:.08;
 }
 updateState(s){
  const next={reactor_active:!!s.reactor_active,cooling_repaired:!!s.cooling_repaired,greenhouse_active:!!s.greenhouse_active},previous=this.systemState;
  if(previous&&previous.reactor_active===next.reactor_active&&previous.cooling_repaired===next.cooling_repaired&&previous.greenhouse_active===next.greenhouse_active)return;
  this.systemState=next;
  for(const type of ['reactor','cooling','plant','grow'])this.applySystemState(this.mat(type),type,next);
  for(const [material,{type}]of this.systemMaterials)this.applySystemState(material,type,next);
 }
 dispose(){for(const root of [...this.chunkRoots])this.detachChunk(root);this.root.traverse(o=>{if(o.isInstancedMesh)o.dispose();});this.systemMaterials.clear();this.chunkSystemMaterials.clear();this.ship.scene.remove(this.root);for(const m of this.materials.values())m.dispose();for(const t of this.textures)t.dispose();for(const g of [this.boxGeo,this.bevelGeo,this.cylinderGeo,this.sphereGeo,...this.geometries.values()])g.dispose();this.workLight.dispose();this.environment?.dispose();}
}
