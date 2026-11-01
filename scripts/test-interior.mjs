import assert from 'node:assert/strict';
import {registerHooks} from 'node:module';
registerHooks({resolve(s,c,next){if(s==='three')return {url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true};return next(s,c);}});
const {layout,walls,walkable,doorBlocks,doorProgress,slideMove,roomAt}=await import('../web/src/interior/DeckLayout.js');
const {ShipInterior}=await import('../web/src/interior/ShipInterior.js');
const THREE=await import('../web/vendor/three.module.js');
const {LivingWorld}=await import('../web/src/world/LivingWorld.js');
const gate=layout.gates.find(g=>g.id==='door-main');
const door={...gate,kind:'door',open:false};
assert.equal(walkable(0,7),true);assert.equal(doorBlocks(door,0,7),true);
const now=Date.now();Object.assign(door,{open:true,motion_at:now,motion_from:0});
assert.equal(doorBlocks(door,0,7,doorProgress(door,now+50)),true);
assert.equal(doorBlocks(door,0,7,doorProgress(door,now+900)),false);
assert.equal(doorBlocks(door,1.1,7,1),true,'shoulder cannot clip frame');
let p={x:0,y:1.7,z:5};p=slideMove(p,{x:0,z:9},(x,z)=>walkable(x,z)&&!doorBlocks({...door,open:false,motion_at:0},x,z));assert.ok(p.z<6.6);
p=slideMove(p,{x:0,z:9},(x,z)=>walkable(x,z)&&!doorBlocks(door,x,z,1));assert.ok(p.z>8.9);
p=slideMove(p,{x:0,z:5},(x,z)=>walkable(x,z)&&!doorBlocks(door,x,z,1));assert.ok(p.z<5.1);
// Flood-fill the actual capsule footprint; prove every gate/room is reachable.
const step=.2,key=(x,z)=>`${x},${z}`,q=[[-75,0]],seen=new Set([key(-75,0)]);let i=0;
while(i<q.length){const [x,z]=q[i++];for(const [dx,dz]of [[1,0],[-1,0],[0,1],[0,-1]]){const a=x+dx,b=z+dz,k=key(a,b);if(!seen.has(k)&&walkable(a*step,b*step)){seen.add(k);q.push([a,b]);}}}
for(const a of layout.areas){assert.ok(q.some(([x,z])=>roomAt(x*step,z*step)?.id===a.id),'unreachable '+a.id);}
for(const g of layout.gates)assert.ok(seen.has(key(Math.round(g.x/step),Math.round(g.z/step))),'unreachable gate '+g.id);
for(const a of layout.areas.filter(a=>a.kind==='room')){assert.ok(walls.filter(w=>w.area===a.id).length>=4);}
// Movement cannot tunnel through a wall even under a deliberately large timestep.
p=slideMove({x:3,z:4},{x:7,z:14},walkable);assert.ok(p.z<6.6);assert.ok(p.x>5,'slides along wall instead of freezing');
assert.equal(walkable(17,0),false);assert.equal(walkable(100,0),false);
// Inspect the actual pre-batch meshes, not a nominal offset constant: the old
// support slab and every floor panel had the same top face and z-fought.
const floorReview=new ShipInterior({scene:new THREE.Scene(),renderer:{isSoftware:true}}),decks=new Map();
floorReview.root.updateMatrixWorld(true);
floorReview.root.traverse(o=>{if(!['deck-support','deck-panel'].includes(o.name))return;const a=decks.get(o.userData.area)||{panels:[]},bounds=new THREE.Box3().setFromObject(o);if(o.name==='deck-support')a.support=bounds;else a.panels.push(bounds);decks.set(o.userData.area,a);});
for(const area of layout.areas){const deck=decks.get(area.id);assert.ok(deck?.support&&deck.panels.length,'missing physical floor in '+area.id);for(const panel of deck.panels){assert.ok(deck.support.max.y<panel.min.y-.005,'support must be recessed below the panel, '+area.id);assert.ok(Math.abs(panel.max.y)<1e-8,'finished floor stays on the collision datum');}for(let i=0;i<deck.panels.length;i++)for(let j=i+1;j<deck.panels.length;j++){const a=deck.panels[i],b=deck.panels[j];assert.ok(Math.min(a.max.x,b.max.x)-Math.max(a.min.x,b.min.x)<=0||Math.min(a.max.z,b.max.z)-Math.max(a.min.z,b.min.z)<=0,'floor panels must not overlap');}}
floorReview.dispose();
const ship={scene:new THREE.Scene(),renderer:{isSoftware:false},player:new THREE.Vector3(0,1.7,4.5),remote:new Map(),camera:new THREE.PerspectiveCamera()};
const interior=new ShipInterior(ship);ship.interior=interior;const world=new LivingWorld(ship);
world.setState({settings:{npc_count:0},objects:layout.gates.filter(g=>g.door).map(g=>({...g,kind:'door',open:false}))});
world.animate(.016);interior.update('explore',ship.player);assert.ok(interior.ceilings.every(c=>c.visible));
interior.update('overview',ship.player);assert.ok(interior.ceilings.every(c=>!c.visible));
let instances=0;interior.root.traverse(o=>{if(o.isInstancedMesh)instances++;if(o.isMesh){assert.ok(o.geometry.attributes.position.array.every(Number.isFinite));}});assert.ok(instances>30);
interior.updateState({reactor_active:true,cooling_repaired:true,greenhouse_active:true});assert.equal(interior.mat('reactor').emissiveIntensity,.85);
// Local sensor holds closing leaves until the server's reopen reaches this client.
const d=world.objects.get('door-main');ship.player.set(0,1.7,7);d.data.open=false;d.data.motion_from=1;d.data.motion_at=Date.now();d.lastProgress=1;world.animate(.016);assert.equal(d.progress,1);
world.dispose();interior.dispose();
console.log(JSON.stringify({passed:true,rooms:layout.areas.filter(a=>a.kind==='room').length,doors:layout.gates.filter(g=>g.door).length,reachableCapsuleCells:seen.size,staticBatches:instances,checks:['bridge-door-corridor-return cycle','animated gap collision','all compartments reachable','wall sweep and sliding','obstacle collision','recessed structural deck and nonoverlapping floor panels','ceiling cutaway','door occupancy sensor','mission material state','finite 3D geometry']}));
