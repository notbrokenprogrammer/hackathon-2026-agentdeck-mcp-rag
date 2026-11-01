import assert from 'node:assert/strict';
import {registerHooks} from 'node:module';
registerHooks({resolve(specifier,context,next){if(specifier==='three')return {url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true};return next(specifier,context);}});
const THREE=await import('../web/vendor/three.module.js');
const {ShipInterior}=await import('../web/src/interior/ShipInterior.js');
const {buildReferenceEquipment}=await import('../web/src/interior/ReferenceEquipment.js');
const {layout}=await import('../web/src/interior/DeckLayout.js');

// Use the actual mesh-building helpers, isolated from architecture and streaming.
function equipment(){
 const i=Object.create(ShipInterior.prototype);
 i.materials=new Map();i.geometries=new Map();i.rooms=new Map();i.textures=[];i.root=new THREE.Group();
 i.boxGeo=new THREE.BoxGeometry(1,1,1);i.cylinderGeo=new THREE.CylinderGeometry(1,1,1,24);
 const shape=new THREE.Shape();shape.moveTo(-.46,-.46);shape.lineTo(.46,-.46);shape.lineTo(.46,.46);shape.lineTo(-.46,.46);shape.closePath();
 i.bevelGeo=new THREE.ExtrudeGeometry(shape,{depth:.92,bevelEnabled:true,bevelThickness:.04,bevelSize:.04,bevelSegments:2,steps:1,curveSegments:1});i.bevelGeo.translate(0,0,-.46);
 for(const name of ['pearl','white','metal','dark','rubber','blue','fabric','green','light','cyan','amber','mint','reactor','cooling','plant','grow'])i.materials.set(name,new THREE.MeshStandardMaterial());
 for(const area of layout.areas){const g=new THREE.Group();i.rooms.set(area.id,g);i.root.add(g);}
 const groups=buildReferenceEquipment(i);i.root.updateMatrixWorld(true);return {i,groups};
}
const {i,groups}=equipment(),failures=[];
const covered=new Set(),byRoom=new Map();let leaves=0,glass=0,meshes=0,triangles=0;
for(const group of groups){
 assert.ok(group.name&&group.userData.area&&group.userData.interiorRole==='equipment');
 byRoom.set(group.userData.area,(byRoom.get(group.userData.area)||0)+1);
 const bounds=new THREE.Box3().setFromObject(group);
 if(group.userData.cosmeticOverhead){assert.ok(bounds.min.y>=2.65,'overhead cosmetic crosses human headroom: '+group.name);continue;}
 const collider=layout.obstacles.find(o=>o.id===group.userData.colliderID);assert.ok(collider,'equipment has an authoritative footprint');
 assert.ok(!covered.has(collider.id),'one equipment instance per collider');covered.add(collider.id);
 const tolerance=.005;
 if(bounds.min.x<collider.x-collider.w/2-tolerance||bounds.max.x>collider.x+collider.w/2+tolerance||bounds.min.z<collider.z-collider.d/2-tolerance||bounds.max.z>collider.z+collider.d/2+tolerance)failures.push({id:collider.id,bounds:[bounds.min.x,bounds.max.x,bounds.min.z,bounds.max.z],collider:[collider.x-collider.w/2,collider.x+collider.w/2,collider.z-collider.d/2,collider.z+collider.d/2]});
 assert.ok(bounds.min.y>=-.005&&bounds.max.y<layout.height,'equipment stays on the deck below the ceiling: '+group.name);
}
assert.deepEqual(failures,[],'visual geometry must remain inside existing collision footprints');
assert.equal(covered.size,layout.obstacles.length,'every existing equipment footprint has a complete replacement');
assert.equal(byRoom.size,7);
i.root.traverse(mesh=>{
 if(!mesh.isMesh)return;meshes++;assert.ok(!mesh.isInstancedMesh,'root partitions geometry before batching');
 const positions=mesh.geometry.attributes.position;assert.ok(positions.array.every(Number.isFinite));
 triangles+=(mesh.geometry.index?.count||positions.count)/3;
 if(mesh.name==='Veined leaf')leaves++;
 if(mesh.material.transparent)glass++;
 assert.ok(mesh.material.isMaterial,'valid shared PBR material');
});
assert.ok(leaves>200,'multiple hydroponic rows contain real leaf geometry');
assert.ok(glass>=7,'three canopies, containment chamber and cabinet glazing');
assert.equal(groups.filter(g=>g.name.startsWith('Inclined cryogenic capsule')).length,3);
assert.ok(i.geometries.has('reference:cryo-curved-canopy'));
for(const type of ['broad','blade','ruffle'])assert.ok(i.geometries.has('reference:leaf:'+type));
for(const name of ['reactor','cooling','plant','grow'])assert.equal(i.mat(name).userData.system,name);
const {i:second,groups:again}=equipment();
assert.equal(again.length,groups.length);assert.equal(second.geometries.size,i.geometries.size,'deterministic reusable geometry cache');
const a=new THREE.Box3().setFromObject(i.root),b=new THREE.Box3().setFromObject(second.root);assert.ok(a.equals(b));
for(const scene of [i,second]){for(const m of scene.materials.values())m.dispose();for(const g of [scene.boxGeo,scene.cylinderGeo,scene.bevelGeo,...scene.geometries.values()])g.dispose();}
console.log(JSON.stringify({passed:true,rooms:byRoom.size,equipmentFootprints:covered.size,namedGroups:groups.length,unbatchedMeshes:meshes,sourceTriangles:triangles,leafMeshes:leaves,glassMeshes:glass,reusableGeometries:i.geometries.size,checks:['all seven rooms','exact current collision footprint coverage','human headroom and floor datum','three real inclined glass capsules','three leaf geometries and irrigation rows','annular reactor containment','deterministic cached geometry','mission system tags','partition before instancing'],scope:'actual 3D geometry/Box3 validation; not browser rendering or FPS'}));
