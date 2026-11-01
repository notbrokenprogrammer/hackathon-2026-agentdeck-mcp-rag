import assert from 'node:assert/strict';
import {registerHooks} from 'node:module';
registerHooks({resolve(s,c,next){if(s==='three')return{url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true};if(s.startsWith('three/addons/'))return{url:new URL('../web/vendor/'+s.slice('three/addons/'.length),import.meta.url).href,shortCircuit:true};return next(s,c);}});
const {FrameBudget}=await import('../web/src/render/FrameBudget.js');
const {SoftwareRenderer}=await import('../web/src/render/SoftwareRenderer.js');
const {ShipScene}=await import('../web/src/scene.js');
const {PlayerControls}=await import('../web/src/controls/PlayerControls.js');
const {walkable}=await import('../web/src/interior/DeckLayout.js');
const THREE=await import('../web/vendor/three.module.js');

const budget=new FrameBudget();
for(const [w,h,dpr] of [[1366,768,2],[1920,1080,2],[3840,2160,2],[390,844,3]]){
 const ratio=budget.pixelRatio(w,h,dpr);assert.ok(w*h*ratio*ratio<=1920*1080+1);assert.ok(ratio<=1.5);
}
assert.equal(budget.pixelRatio(1280,720,1),1,'native low-DPI canvas remains native');
budget.setQuality('economy');assert.ok(1920*1080*budget.pixelRatio(1920,1080,2)**2<=1280*720+1);
budget.setQuality('balanced');
assert.equal(budget.sample(1000,1000),false,'one loading/resume stall cannot lower quality');
for(let t=1;t<3000;t+=40)budget.sample(40,t);
assert.equal(budget.scale,.9,'sustained slow frames lower raster resolution');
for(let t=3000;t<30000;t+=40)budget.sample(40,t);
assert.ok(budget.scale>=.6,'adaptive lower bound');
for(let t=30000;t<100000;t+=16)budget.sample(16,t);
assert.equal(budget.scale,1,'sustained recovery restores full requested quality');
budget.reset();assert.equal(budget.samples,0,'pause discards timing samples');
const isolatedStall=new FrameBudget();isolatedStall.sample(400,1);for(let t=17;t<3000;t+=16)isolatedStall.sample(16,t);assert.equal(isolatedStall.scale,1,'one long GC/loading stall does not lower quality');
const verySlow=new FrameBudget({software:true});for(let t=1;t<4000;t+=400)verySlow.sample(400,t);assert.equal(verySlow.scale,.9,'persistent frames over250ms still adapt instead of being discarded');

const context={createImageData:(w,h)=>({data:new Uint8ClampedArray(w*h*4)})},canvas={getContext:()=>context};
const software=new SoftwareRenderer(canvas);
software.setSize(1440,900);assert.deepEqual([canvas.width,canvas.height],[1440,900],'offline review stays full resolution by default');
const full=software.buffer;software.setSize(1440,900);assert.equal(software.buffer,full,'unchanged resize reuses buffers');
const softwareBudget=new FrameBudget({software:true});
software.setPixelRatio(softwareBudget.pixelRatio(1920,1080,2));software.setSize(1920,1080);
assert.deepEqual([canvas.width,canvas.height],[640,360],'interactive software render caps pixels without changing camera/CSS size');
assert.equal(software.depth.length,640*360);
softwareBudget.setQuality('economy');assert.equal(softwareBudget.pixelRatio(1920,1080,2),.25);
const qualityScene={frameBudget:new FrameBudget(),renderer:{shadowMap:{enabled:true}},resize(){}};
ShipScene.prototype.setQuality.call(qualityScene,'economy');assert.equal(qualityScene.renderer.shadowMap.enabled,false,'economy skips the shadow pass');
ShipScene.prototype.setQuality.call(qualityScene,'balanced');assert.equal(qualityScene.renderer.shadowMap.enabled,true,'balanced restores shadows');

let draws=0,matrixWork=0,labelHides=0;
globalThis.requestAnimationFrame=()=>1;globalThis.document={hidden:false};
const scene={disposed:false,active:false,mode:'explore',clock:{getDelta:()=>1/60},frameBudget:new FrameBudget(),frame:()=>{},crew:{updateLabels(_camera,_roots,options){assert.equal(options.active,false);labelHides++;}},interior:{update(){matrixWork++;}},renderer:{render(){draws++;}},camera:new THREE.PerspectiveCamera()};
for(let i=0;i<60;i++)ShipScene.prototype.animate.call(scene);
assert.equal(draws,0);assert.equal(matrixWork,0);assert.equal(labelHides,1,'inactive frames hide labels once, without scene/DOM work');
scene.active=true;document.hidden=true;ShipScene.prototype.animate.call(scene);assert.equal(matrixWork,0,'hidden tab cannot simulate held movement');

document.hidden=false;document.documentElement={dataset:{motion:'system'}};
let simulations=0,crewAnimations=0,labelUpdates=0;
const active={disposed:false,active:true,mode:'overview',clock:{getDelta:()=>1/60},frame:()=>{},frameBudget:new FrameBudget(),frames:0,lastFps:performance.now(),motionQuery:{matches:false},camera:new THREE.PerspectiveCamera(),target:new THREE.Vector3(),azimuth:.8,overviewRadius:20,overviewHeight:12,player:new THREE.Vector3(0,1.7,0),yaw:0,avatar:new THREE.Group(),remote:new Map(),interior:{root:new THREE.Group(),update(){matrixWork++;},visibleRooms:new Set(['bridge'])},livingWorld:{root:new THREE.Group(),time:0,animate(){simulations++;}},crew:{characters:[],animate(){crewAnimations++;},updateLabels(){labelUpdates++;}},renderer:{render(){draws++;}}};
for(let i=0;i<20;i++)ShipScene.prototype.animate.call(active);
assert.equal(simulations,20,'active door/world simulation still advances every frame');assert.equal(crewAnimations,20,'crew animation retained');assert.equal(draws,20,'active WebGL render stays per-frame');assert.equal(labelUpdates,1,'expensive label projection/raycast pass is paced separately');

function walker(){
 const camera=new THREE.PerspectiveCamera();camera.position.set(3,1.7,3.7);
 // Use the production velocity/friction update and actual deck capsule sweep;
 // replace only PointerLockControls' DOM-dependent camera translation adapter.
 const controls=Object.assign(Object.create(PlayerControls.prototype),{camera,isLocked:true,velocity:new THREE.Vector3(),direction:new THREE.Vector3(),walkSpeed:5,sprintSpeed:9,playerHeight:1.7,moveForward:true,moveBackward:false,moveLeft:false,moveRight:false,isSprinting:false,controls:{moveRight(distance){camera.position.x+=distance;},moveForward(distance){camera.position.z-=distance;}}});
 return {camera,controls,player:new THREE.Vector3(3,1.7,3.7),previousCameraPosition:new THREE.Vector3(),livingWorld:{walkable:()=>true}};
}
const distances={};
for(const hz of [5,20,60]){const w=walker();for(let i=0;i<hz;i++)ShipScene.prototype.stepMovement.call(w,1/hz);distances[hz]=3.7-w.player.z;assert.ok(walkable(w.player.x,w.player.z));}
assert.ok(distances[20]>4.4&&Math.abs(distances[20]-distances[60])<.05,'20/60fps walking distance differs by less than5cm over1s');
assert.ok(Math.abs(distances[5]-distances[20])<.001,'5fps fallback no longer reduces walking speed to25percent');
const wall=walker();wall.camera.position.set(7,1.7,3.7);wall.controls.moveForward=false;wall.controls.moveRight=true;wall.controls.isSprinting=true;
for(let i=0;i<8;i++)ShipScene.prototype.stepMovement.call(wall,.2);
assert.ok(wall.player.x<7.7&&wall.player.x>7&&walkable(wall.player.x,wall.player.z),'slow sprint frames cannot tunnel through the actual bridge wall');
const resumed=walker();Object.assign(active,resumed,{mode:'explore',stepMovement:ShipScene.prototype.stepMovement,resumeFrame:true,clock:{getDelta:()=>10}});active.livingWorld={...resumed.livingWorld,root:new THREE.Group(),time:0,animate(){}};
ShipScene.prototype.animate.call(active);assert.equal(active.player.z,3.7,'tab resume cannot apply a catch-up leap');

let sizing=0;
const resized={container:{clientWidth:1920,clientHeight:1080},renderer:{setPixelRatio(){},setSize(){sizing++;}},camera:new THREE.PerspectiveCamera(),frameBudget:new FrameBudget()};
ShipScene.prototype.resize.call(resized);ShipScene.prototype.resize.call(resized);assert.equal(sizing,1);assert.equal(resized.camera.aspect,1920/1080);
console.log(JSON.stringify({passed:true,checks:['bounded high-DPI framebuffer','sustained adaptive resolution and recovery','single-stall rejection','full-resolution offline review','Canvas pixel/depth budget','economy skips shadows','idempotent resize','paused/hidden scene skips work','5/20/60fps movement consistency','slow-frame bridge wall collision','no tab-resume leap'],canvasPixels:{before:1920*1080,after:640*360},walkingMetresInOneSecond:distances,note:'scheduler, movement and framebuffer checks; no hardware FPS claim'}));
