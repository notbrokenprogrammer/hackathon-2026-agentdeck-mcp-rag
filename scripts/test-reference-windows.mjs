import assert from 'node:assert/strict';
import {registerHooks} from 'node:module';
registerHooks({resolve(s,c,next){return s==='three'?{url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true}:next(s,c);}});
const THREE=await import('../web/vendor/three.module.js');
const {ShipInterior}=await import('../web/src/interior/ShipInterior.js');
const {observationWindows}=await import('../web/src/interior/ReferenceArchitecture.js');
const {walkable}=await import('../web/src/interior/DeckLayout.js');
const interior=new ShipInterior({scene:new THREE.Scene(),renderer:{isSoftware:true}},{batch:false});interior.root.updateMatrixWorld(true);
const windows=[];interior.root.traverse(o=>{if(o.name.startsWith('Laminated observation window'))windows.push(o);});
assert.equal(windows.length,observationWindows.length);
let sealedRays=0,clearRays=0;
for(let index=0;index<windows.length;index++){
 const frame=windows[index],spec=observationWindows[index],width=spec.max-spec.min,height=spec.top-spec.bottom;
 const casts=(point,direction)=>{
  const origin=point.clone().addScaledVector(direction,-1).applyMatrix4(frame.matrixWorld),worldDirection=direction.clone().transformDirection(frame.matrixWorld);
  return new THREE.Raycaster(origin,worldDirection,0,2).intersectObject(frame,true);
 };
 for(const sx of[-1,1])for(const sy of[-1,1])for(const side of[-1,1]){
  const point=new THREE.Vector3(sx*(width/2-.02),sy*(height/2-.02),0);
  // Near-normal and oblique shots cover both interior and exterior faces.
  for(const slope of[0,.2]){
   const hits=casts(point,new THREE.Vector3(slope*sx,slope*sy,side).normalize());
   assert.ok(hits.some(h=>!h.object.material.transparent),'unsealed window corner: '+spec.area+'/'+spec.axis+'/'+sx+'/'+sy+'/'+side+'/'+slope);sealedRays++;
  }
 }
 for(const side of[-1,1])for(const slope of[0,.35]){
  const hits=casts(new THREE.Vector3(),new THREE.Vector3(slope,0,side).normalize());
  assert.ok(hits.some(h=>h.object.name==='Observation glass / physical wall collision retained'),'glass missing from clear aperture');
  assert.ok(!hits.some(h=>!h.object.material.transparent),'opaque geometry covers the viewing aperture');clearRays++;
 }
 const midpoint=(spec.min+spec.max)/2;
 assert.equal(walkable(spec.axis==='x'?spec.fixed:midpoint,spec.axis==='z'?spec.fixed:midpoint),false,'glass retains authoritative wall collision');
}
interior.dispose();
console.log(JSON.stringify({passed:true,windows:windows.length,sealedCornerRays:sealedRays,clearApertureRays:clearRays,checks:['both faces and oblique corner closure','transparent viewing aperture','physical glass barrier'],scope:'actual authored 3D ray intersections; no browser reflection claim'}));
