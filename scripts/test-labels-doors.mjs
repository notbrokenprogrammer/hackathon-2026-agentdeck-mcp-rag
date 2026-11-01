import assert from 'node:assert/strict';
import {registerHooks} from 'node:module';
registerHooks({resolve(s,c,next){if(s==='three')return{url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true};return next(s,c);}});
const THREE=await import('../web/vendor/three.module.js');
const {CrewLabels}=await import('../web/src/crew/CrewLabels.js');
const {ShipInterior}=await import('../web/src/interior/ShipInterior.js');
const {LivingWorld}=await import('../web/src/world/LivingWorld.js');
const {layout,doorBlocks}=await import('../web/src/interior/DeckLayout.js');

class Element{
 constructor(doc){this.ownerDocument=doc;this.style={};this.dataset={};this.children=[];this.hidden=false;}
 append(child){this.children.push(child);child.parent=this;}
 setAttribute(){}
 remove(){if(this.parent)this.parent.children=this.parent.children.filter(c=>c!==this);}
}
const doc={createElement:()=>new Element(doc)},container=new Element(doc);container.clientWidth=1000;container.clientHeight=700;
const labels=new CrewLabels(container),head=new THREE.Group(),actor=new THREE.Group();head.position.y=1.68;actor.add(head);
const character={profile:{id:'test-crew',short:'Виктор Савиных',role:'Капитан'},group:actor,rig:{head},loaded:true};
const scene=new THREE.Scene(),camera=new THREE.PerspectiveCamera(65,1000/700,.1,200);
scene.add(actor);actor.position.set(0,0,-4);camera.position.set(0,1.7,2);camera.lookAt(0,2,-4);
const blocked=new THREE.Group();scene.add(blocked);
const wall=new THREE.Mesh(new THREE.BoxGeometry(3,3.8,.28),new THREE.MeshStandardMaterial());wall.position.set(0,1.9,0);blocked.add(wall);
const update=(roots=[],options={})=>labels.update(camera,[character],roots,{pixelRatio:2,...options});
update();const nameplate=labels.entries.get('test-crew');assert.equal(nameplate.hidden,false);assert.ok(nameplate.style.font.includes('14px'));assert.equal(nameplate.children[0].textContent,'Виктор Савиных');
assert.ok(!nameplate.style.transform.includes('scale('),'text must not be enlarged as a bitmap');
const positions=nameplate.style.transform.match(/translate\(([-\d.]+)px, ([-\d.]+)px\)/);assert.ok(positions);for(const n of positions.slice(1))assert.equal(Number(n)*2,Math.round(Number(n)*2),'snap to physical pixels');
update([blocked]);assert.equal(nameplate.hidden,true,'opaque wall hides the name');
blocked.visible=false;update([blocked]);assert.equal(nameplate.hidden,false,'a culled wall must not hide visible names');blocked.visible=true;
wall.material.transparent=true;wall.material.opacity=.1;wall.material.depthWrite=false;update([blocked]);assert.equal(nameplate.hidden,false,'window glass does not erase names');
character.loaded=false;update();assert.equal(nameplate.hidden,true);character.loaded=true;
actor.visible=false;update();assert.equal(nameplate.hidden,true);actor.visible=true;
actor.position.z=-40;update();assert.equal(nameplate.hidden,true,'out-of-range names are hidden');
let traversals=0;const expensive={updateMatrixWorld(){traversals++;},traverseVisible(){traversals++;}};
update([expensive]);assert.equal(traversals,0,'offscreen mentors do not traverse the ship');
actor.position.z=5;update();assert.equal(nameplate.hidden,true,'names behind camera are hidden');
update([expensive]);assert.equal(traversals,0,'behind-camera mentors do not traverse the ship');
actor.position.z=-4;update([], {active:false});assert.equal(nameplate.hidden,true,'inactive game hides labels');
wall.material.transparent=false;wall.material.opacity=1;wall.material.depthWrite=true;
let unnecessaryRaycasts=0;const behindWall=new THREE.Mesh(wall.geometry,wall.material);behindWall.raycast=()=>unnecessaryRaycasts++;blocked.add(behindWall);
update([blocked]);assert.equal(nameplate.hidden,true);assert.equal(unnecessaryRaycasts,0,'occlusion stops at the first blocking surface');blocked.remove(behindWall);
update();assert.equal(labels.entries.size,1,'updates reuse one DOM plate');labels.dispose();assert.equal(container.children.length,0);
wall.geometry.dispose();wall.material.dispose();

const ship={scene:new THREE.Scene(),renderer:{isSoftware:true},player:new THREE.Vector3(100,1.7,100),remote:new Map(),camera};
const interior=ship.interior=new ShipInterior(ship),world=ship.livingWorld=new LivingWorld(ship);
const meshes=[];interior.root.traverse(o=>{if(o.isMesh)meshes.push(o);});interior.root.updateMatrixWorld(true);
const headers=meshes.filter(o=>o.name==='door-lintel'),ceilings=meshes.filter(o=>o.name==='ceiling-slab'),finishes=meshes.filter(o=>o.name==='wall-finish');
for(const gate of layout.gates)assert.equal(headers.filter(o=>o.userData.gateID===gate.id).length,1,'exactly one lintel for '+gate.id);
assert.equal(headers.length,layout.gates.length);
const bounds=o=>new THREE.Box3().setFromObject(o),overlap=(a,b,axis)=>Math.min(a.max[axis],b.max[axis])-Math.max(a.min[axis],b.min[axis]);
for(let i=0;i<ceilings.length;i++)for(let j=i+1;j<ceilings.length;j++){const a=bounds(ceilings[i]),b=bounds(ceilings[j]);assert.ok(overlap(a,b,'x')<1e-7||overlap(a,b,'z')<1e-7,'neighbouring ceiling slabs must not overlap above a doorway');}
for(let i=0;i<finishes.length;i++)for(let j=i+1;j<finishes.length;j++){
 if(finishes[i].userData.area===finishes[j].userData.area)continue;const a=bounds(finishes[i]),b=bounds(finishes[j]);
 for(const axis of ['x','z']){const tangent=axis==='x'?'z':'x';if(overlap(a,b,tangent)>.01&&overlap(a,b,'y')>.01)for(const side of ['min','max'])assert.ok(Math.abs(a[side][axis]-b[side][axis])>1e-6,'room finishes cannot share the same exposed plane');}
}
const gates=layout.gates.filter(g=>g.door),now=Date.now(),originalNow=Date.now;
const doorState=(progress)=>({settings:{npc_count:0},objects:gates.map(g=>({...g,kind:'door',open:progress>0,motion_at:progress>0&&progress<1?now-progress*layout.doorDuration:0,motion_from:0}))});
Date.now=()=>now;
try{
 for(const progress of [0,.25,.5,1]){
  world.setState(doorState(progress));world.animate(0,true);ship.scene.updateMatrixWorld(true);
  for(const gate of gates){
   const item=world.objects.get(gate.id),angle=gate.rotation||0,normal=new THREE.Vector3(Math.cos(angle),0,-Math.sin(angle)),tangent=new THREE.Vector3(Math.sin(angle),0,Math.cos(angle));
   for(const t of [-.8,-.3,0,.3,.8]){
    let blockedByLeaves=false;
    for(const edge of [-layout.radius,layout.radius]){const origin=new THREE.Vector3(gate.x,1.4,gate.z).addScaledVector(tangent,t+edge).addScaledVector(normal,-.8),ray=new THREE.Raycaster(origin,normal,.01,1.6);blockedByLeaves ||= ray.intersectObjects(item.leaves,true).length>0;}
    const center=new THREE.Vector3(gate.x,0,gate.z).addScaledVector(tangent,t);
    assert.equal(blockedByLeaves,doorBlocks(gate,center.x,center.z,progress),gate.id+': visible gap agrees with capsule collision at '+progress+'/'+t);
   }
   const inverse=new THREE.Matrix4().copy(item.group.matrixWorld).invert(),localBox=o=>{o.geometry.computeBoundingBox();return o.geometry.boundingBox.clone().applyMatrix4(o.matrixWorld).applyMatrix4(inverse);};
   const gaskets=item.leaves.map(leaf=>localBox(leaf.getObjectByName('door-leaf-gasket')));
   assert.ok(gaskets[0].max.z<=gaskets[1].min.z+1e-7,'gaskets do not overlap at the central seam');
   for(const leaf of item.leaves){const seal=localBox(leaf.getObjectByName('door-leaf-gasket')),core=localBox(leaf.getObjectByName('door-leaf-core'));assert.ok(seal.max.y<core.max.y-.005,'seal and leaf top faces are not coplanar');}
  }
 }
 // Test the label layer against all twelve real architectural door meshes.
 const actualLabels=new CrewLabels(container);ship.scene.add(actor);interior.update('overview',ship.player);
 for(const gate of gates){
  const n=new THREE.Vector3(Math.cos(gate.rotation||0),0,-Math.sin(gate.rotation||0));actor.position.set(gate.x,0,gate.z).addScaledVector(n,.9);camera.position.set(gate.x,1.7,gate.z).addScaledVector(n,-.9);camera.lookAt(actor.position.clone().add(new THREE.Vector3(0,2.04,0)));camera.updateMatrixWorld(true);
  for(const progress of [0,1]){world.setState(doorState(progress));world.animate(0,true);actualLabels.update(camera,[character],[interior.root,world.root]);assert.equal(actualLabels.entries.get('test-crew').hidden,progress===0,gate.id+': name obeys the actual '+(progress?'open':'closed')+' passage');}
 }
 actualLabels.dispose();
}finally{Date.now=originalNow;world.dispose();interior.dispose();}
console.log(JSON.stringify({passed:true,doors:gates.length,doorAnimationSamples:4,checks:['native DOM text and DPR snapping','range/frustum/inactive visibility','opaque-wall and glass occlusion','one shared lintel per passage','nonoverlapping ceilings and inward wall finishes','gasket separation','visual gap matches capsule collision','all 12 open/closed doors occlude names']}));
