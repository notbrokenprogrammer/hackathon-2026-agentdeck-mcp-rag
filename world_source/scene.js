import {SoftwareRenderer} from './render/SoftwareRenderer.js';
import {BridgeCrew} from './crew/BridgeCrew.js';
import * as THREE from 'three';
import {PlayerControls} from './controls/PlayerControls.js';
export class ShipScene {
 constructor(container){
 this.container=container;this.canvas=container.querySelector('canvas');this.player=new THREE.Vector3(-15,1.7,0);this.yaw=0;this.mode='overview';this.target=new THREE.Vector3();this.azimuth=.8;this.overviewRadius=72;this.overviewHeight=46;this.remote=new Map();this.clock=new THREE.Clock();this.active=false;
 this.scene=new THREE.Scene();this.camera=new THREE.PerspectiveCamera(43,1,.1,500);try{this.renderer=new THREE.WebGLRenderer({canvas:this.canvas,antialias:true,alpha:true});}catch{this.renderer=new SoftwareRenderer(this.canvas);const label=document.createElement('span');label.className='render-mode';label.textContent='Совместимый режим';this.container.append(label);}this.renderer.setPixelRatio(Math.min(devicePixelRatio,1.7));this.renderer.outputColorSpace=THREE.SRGBColorSpace;this.renderer.toneMapping=THREE.ACESFilmicToneMapping;this.renderer.toneMappingExposure=1.3;
 this.scene.add(new THREE.HemisphereLight(0xc6e1f6,0x172536,2.6));const sun=new THREE.DirectionalLight(0xdbe6ff,3);sun.position.set(20,40,15);this.scene.add(sun);const rim=new THREE.PointLight(0x71e7df,90,70);rim.position.set(0,10,0);this.scene.add(rim);
 this.floorMat=new THREE.MeshStandardMaterial({color:0x344454,roughness:.6,metalness:.55});this.wallMat=new THREE.MeshStandardMaterial({color:0x657b8d,roughness:.4,metalness:.6});this.detailMat=new THREE.MeshStandardMaterial({color:0x182635,roughness:.5,metalness:.7});
 this.build();this.detailShip();this.crew=new BridgeCrew(this.scene);this.controls=new PlayerControls(this.camera,this.canvas);this.controls.walkSpeed=5;this.controls.sprintSpeed=9;
 this.canvas.addEventListener('click',e=>{if(this.mode==='explore'&&this.active)this.controls.lock();if(this.mode==='overview'){const id=this.crew.pick(e,this.canvas,this.camera);if(id)window.dispatchEvent(new CustomEvent('crew-select',{detail:{agentID:id}}));}});
 let drag=null;this.canvas.addEventListener('pointerdown',e=>{if(this.mode==='overview'){drag={x:e.clientX,a:this.azimuth};this.canvas.setPointerCapture(e.pointerId);}});this.canvas.addEventListener('pointermove',e=>{if(drag)this.azimuth=drag.a+(e.clientX-drag.x)*.006;});this.canvas.addEventListener('pointerup',()=>drag=null);this.canvas.addEventListener('pointercancel',()=>drag=null);
 this.observer=new ResizeObserver(()=>this.resize());this.observer.observe(container);this.resize();this.frame=()=>this.animate();this.frame();this.fps=0;this.lastFps=performance.now();this.frames=0;
 }
 box(w,h,d,x,y,z,mat=this.detailMat,parent=this.scene){const o=new THREE.Mesh(new THREE.BoxGeometry(w,h,d),mat);o.position.set(x,y,z);parent.add(o);return o;}
 glow(color){return new THREE.MeshStandardMaterial({color,emissive:color,emissiveIntensity:1.5,roughness:.3,metalness:.3});}
 build(){
 const starPositions=new Float32Array(1200*3);let seed=718;const rnd=()=>{seed=(seed*1664525+1013904223)>>>0;return seed/4294967296;};for(let i=0;i<starPositions.length;i++)starPositions[i]=(rnd()-.5)*350;const sg=new THREE.BufferGeometry();sg.setAttribute('position',new THREE.BufferAttribute(starPositions,3));this.scene.add(new THREE.Points(sg,new THREE.PointsMaterial({color:0xb1c2d8,size:.25,transparent:true,opacity:.65})));
 // Cutaway deck: geometry and lights are generated locally, no external assets.
 const rooms=[[-16,0,12,12,0x80dfdb],[0,0,16,14,0xb3a4e8],[17,0,12,12,0xecc18b],[0,-17,14,12,0x99c4a5]];
 this.box(47,.65,7,0,-.55,0,this.floorMat);this.box(7,.65,30,0,-.55,-7,this.floorMat);
 for(const [x,z,w,d,color] of rooms){
 this.box(w,.6,d,x,-.3,z,this.floorMat);const glow=this.glow(color);
 this.box(w,.13,.12,x,.05,z-d/2,glow);this.box(w,.13,.12,x,.05,z+d/2,glow);this.box(.12,.13,d,x-w/2,.05,z,glow);this.box(.12,.13,d,x+w/2,.05,z,glow);
 // Low cutaway walls preserve visibility in overview and circulation in walk mode.
 this.box(w,1,.35,x,.5,z-d/2,this.wallMat);this.box(.35,1,d,x+w/2,.5,z,this.wallMat);
 for(let i=-Math.floor(w/2)+1;i<w/2;i+=2)this.box(.025,.02,d-1,x+i,.035,z,this.detailMat);
 for(let j=-Math.floor(d/2)+1;j<d/2;j+=2)this.box(w-1,.02,.025,x,.04,z+j,this.detailMat);
 }
 const blue=this.glow(0x75d8e3),amber=this.glow(0xefb77b),green=this.glow(0x8ccfac);
 // Cryogenic berths.
 for(let i=0;i<3;i++){this.box(1.8,.65,3.8,-19+i*3,.5,-2.8,this.wallMat);this.box(1.55,.12,2.9,-19+i*3,.9,-2.8,new THREE.MeshStandardMaterial({color:0x73c5cb,emissive:0x244c5e,metalness:.7,roughness:.2}));this.box(1.6,.1,.15,-19+i*3,1,-.9,blue);}
 // Bridge consoles and central command table.
 this.box(5,1,2.8,0,.7,-2,this.wallMat);this.box(4.6,.08,2.4,0,1.25,-2,new THREE.MeshStandardMaterial({color:0x588f9d,emissive:0x31585e,metalness:.7}));
 for(let i=0;i<4;i++){this.box(2.4,1.1,1.3,-5+i*3.3,.7,-5.3,this.detailMat);const monitor=this.box(1.9,.9,.08,-5+i*3.3,1.6,-5.1,blue);monitor.rotation.x=-.3;}
 // Reactor with a bright energy core and concentric containment rings.
 this.core=new THREE.Group();this.core.position.set(17,1.7,0);this.scene.add(this.core);const core=new THREE.Mesh(new THREE.CylinderGeometry(1.05,1.05,2,24),amber);core.userData.system='reactor';this.core.add(core);
 for(let j=0;j<3;j++){const ring=new THREE.Mesh(new THREE.TorusGeometry(2.15,.16,8,48),this.wallMat);ring.rotation.x=Math.PI/2;ring.position.y=j*.9-.9;this.core.add(ring);}
 for(let i=0;i<4;i++){const a=i*Math.PI/2;this.box(.4,3,.4,17+Math.sin(a)*2.8,1.5,Math.cos(a)*2.8,this.wallMat);}
 this.reactorLight=new THREE.PointLight(0xefaf70,35,17);this.reactorLight.position.set(17,3,0);this.scene.add(this.reactorLight);
 // Hydroponic rows: individually visible plants with shared geometry/materials.
 const leafGeo=new THREE.ConeGeometry(.4,1,5);const leafMat=new THREE.MeshStandardMaterial({color:0x83b999,roughness:.8});
 for(let r=0;r<3;r++){this.box(9,.5,1.9,0,.4,-20+r*3,this.wallMat);this.box(8.7,.05,1.55,0,.7,-20+r*3,this.detailMat);for(let n=0;n<7;n++){const plant=new THREE.Mesh(leafGeo,leafMat);plant.position.set(-3.6+n*1.2,1.2,-20+r*3);plant.userData.system='greenhouse';this.scene.add(plant);}this.box(9,.06,.08,0,2.8,-20+r*3,green);}
 // Exterior solar wings.
 const solar=new THREE.MeshStandardMaterial({color:0x273d63,metalness:.65,roughness:.45,side:THREE.DoubleSide});
 for(const side of [-1,1]){this.box(12,.15,1,side*17,-1,11,this.wallMat);for(let j=0;j<5;j++){this.box(2.2,.1,6,side*(11+j*2.5),-.85,11,solar);for(let k=0;k<6;k++)this.box(2.1,.02,.025,side*(11+j*2.5),-.78,8.5+k,blue);}}
 // Holographic mentor pedestal.
 const holo=new THREE.Mesh(new THREE.CylinderGeometry(.8,.9,.2,32),this.wallMat);holo.position.set(4,.15,3);this.scene.add(holo);this.box(.45,1.2,.45,4,1.2,3,new THREE.MeshStandardMaterial({color:0x92d9ec,emissive:0x427a91,transparent:true,opacity:.65,wireframe:true}));
 this.avatar=this.makeAvatar(0x7de6df);this.scene.add(this.avatar);this.avatar.position.copy(this.player);this.avatar.position.y=.15;
 const geometry=new THREE.SphereGeometry(15,48,32);const canvas=document.createElement('canvas');canvas.width=256;canvas.height=128;const c=canvas.getContext('2d');const bands=['#9a897a','#c1ad92','#88786c','#c7b398','#b19b80','#816b5f'];for(let y=0;y<128;y++){c.fillStyle=bands[Math.floor(y/7)%bands.length];c.fillRect(0,y,256,1);}const texture=new THREE.CanvasTexture(canvas);this.planet=new THREE.Mesh(geometry,new THREE.MeshBasicMaterial({map:texture,color:0xc3aa8c}));this.planet.position.set(-50,5,-75);this.scene.add(this.planet);
 }

 detailShip(){
 const metallic=new THREE.MeshStandardMaterial({color:0x526775,metalness:.65,roughness:.45});
 const dark=new THREE.MeshStandardMaterial({color:0x142737,metalness:.6,roughness:.55});
 const white=new THREE.MeshStandardMaterial({color:0x82959e,metalness:.4,roughness:.55});
 this.statusBeacons=[];this.growLamps=[];
 this.box(49,.4,30,0,-1.05,-6,dark);
 // Recessed outer hull ribs and layered deck edges.
 for(const [x,z,w,d,label,color] of [[-16,0,12,12,'01 / CRYOGENICS',0x91d5e0],[0,0,16,14,'02 / COMMAND',0xb0c6df],[17,0,12,12,'03 / POWER CORE',0xe6b17e],[0,-17,14,12,'04 / BIOSPHERE',0x92cba0]]){
  for(let j=0;j<4;j++){const px=x-w/2+.7+j*(w-1.4)/3;this.box(.17,2.3,.35,px,1.15,z-d/2,metallic);this.box(.65,.14,.7,px,2.25,z-d/2,white);}
  this.box(w,.12,.12,x,2.2,z-d/2,metallic);
  for(let i=0;i<4;i++){const p=this.box(.22,.34,.09,x-w/2+.7+i*.38,1.1,z-d/2+.25,new THREE.MeshStandardMaterial({color,emissive:color,emissiveIntensity:.3}));if(label.includes('POWER')){p.userData.system='cooling';this.statusBeacons.push(p);}}
  const c=document.createElement('canvas');c.width=1024;c.height=128;const ctx=c.getContext('2d');ctx.fillStyle='#142330';ctx.fillRect(0,0,1024,128);ctx.fillStyle='#9cbfc6';ctx.font='40px monospace';ctx.textAlign='center';ctx.fillText(label,512,78);const plate=new THREE.Mesh(new THREE.PlaneGeometry(6,.75),new THREE.MeshBasicMaterial({map:new THREE.CanvasTexture(c),transparent:true,side:THREE.DoubleSide}));plate.position.set(x,.05,z+d/2-1);plate.rotation.x=-Math.PI/2;this.scene.add(plate);
 }
 // Curved storage rails, cable runs and instrument cabinets.
 for(let i=0;i<5;i++){this.box(1.25,1.65,.6,-21+i*1.7,.85,5.1,white);this.box(1.05,1.3,.04,-21+i*1.7,.87,4.77,dark);this.box(.36,.04,.03,-21+i*1.7,.9,4.73,metallic);}
 for(let i=0;i<3;i++){this.box(6,.12,.12,17,.4+i*.22,-4.8,metallic);this.box(.12,2.6,.12,21.6,1.4,-3+i*.3,metallic);}
 for(let i=0;i<8;i++){const a=i*Math.PI/4;this.box(.14,2.2,.14,17+Math.sin(a)*1.25,1.65,Math.cos(a)*1.25,dark);}
 for(let i=0;i<3;i++){this.box(2.5,.85,1.8,20,.45,2.5+i,metallic);this.box(2,.04,1.3,20,.9,2.5+i,dark);}
 // Command displays with a local procedural telemetry texture.
 const c=document.createElement('canvas');c.width=512;c.height=256;const ctx=c.getContext('2d');ctx.fillStyle='#102934';ctx.fillRect(0,0,512,256);ctx.strokeStyle='#71b7c9';ctx.lineWidth=2;for(let j=0;j<5;j++){ctx.beginPath();for(let x=0;x<512;x+=5){const y=40+j*40+Math.sin(x*.035+j)*12; if(x===0)ctx.moveTo(x,y);else ctx.lineTo(x,y);}ctx.stroke();}ctx.fillStyle='#9cd1d8';ctx.font='18px monospace';ctx.fillText('HORIZON / TELEMETRY',20,25);const monitorMat=new THREE.MeshBasicMaterial({map:new THREE.CanvasTexture(c)});
 for(let i=0;i<4;i++){const m=this.box(1.85,.85,.015,-5+i*3.3,1.61,-5.02,monitorMat);m.rotation.x=-.3;}
 // Dedicated grow bars and planted rows; shared material changes after repair.
 this.leafMaterial=new THREE.MeshStandardMaterial({color:0x555f46,roughness:.9});const leafGeo=new THREE.ConeGeometry(.25,.65,5);for(let r=0;r<3;r++){const mat=new THREE.MeshStandardMaterial({color:0x9bcbb3,emissive:0x85b9bd,emissiveIntensity:.18});const lamp=this.box(8,.1,.3,0,2.6,-20+r*3,mat);lamp.userData.system='grow-lamp';this.growLamps.push(lamp);for(let n=0;n<8;n++){const leaf=new THREE.Mesh(leafGeo,this.leafMaterial);leaf.position.set(-3.6+n,1.4,-20+r*3);leaf.rotation.z=(n%2?.2:-.2);leaf.userData.system='greenhouse';this.scene.add(leaf);}}
 // System lighting uses isolated materials so it cannot recolor unrelated equipment.
 for(const [x,z,w] of [[-16,0,10],[0,0,14],[17,0,10],[0,-17,12]]){
  const mat=new THREE.MeshStandardMaterial({color:0x91cdd1,emissive:0x76b8c8,emissiveIntensity:.2,roughness:.35});
  for(const side of [-1,1]){const strip=this.box(w,.045,.09,x,.09,z+side*4.4,mat);strip.userData.system='power';}
 }
 const caution=new THREE.MeshStandardMaterial({color:0xb18e59,roughness:.65});
 for(let i=0;i<7;i++){const mark=this.box(.4,.025,.1,10+i*.4,.07,-2.9,caution);mark.rotation.y=-.6;}
 const buttonMat=new THREE.MeshStandardMaterial({color:0x84adb5,metalness:.3,roughness:.5});
 for(let station=0;station<4;station++)for(let k=0;k<6;k++)this.box(.1,.04,.1,-5+station*3.3-.6+k*.23,1.28,-4.85,buttonMat);
 const stem=new THREE.MeshStandardMaterial({color:0x537265,roughness:1});
 const leaf=new THREE.MeshStandardMaterial({color:0x69765b,roughness:.9,side:THREE.DoubleSide});
 for(let r=0;r<3;r++)for(let n=0;n<5;n++){
  const x=-3+n*1.45,z=-20+r*3;
  const stalk=new THREE.Mesh(new THREE.CylinderGeometry(.025,.04,.65,5),stem);stalk.position.set(x,1.3,z);this.scene.add(stalk);
  for(const side of [-1,1]){const blade=new THREE.Mesh(new THREE.SphereGeometry(.22,6,4),leaf);blade.scale.set(1,.18,1.8);blade.rotation.z=side*.45;blade.position.set(x+side*.14,1.5,z);blade.userData.system='greenhouse';this.scene.add(blade);}
 }
 const coolant=new THREE.MeshStandardMaterial({color:0xdba076,emissive:0xb06d42,emissiveIntensity:.3});
 for(let i=0;i<3;i++){const gauge=this.box(.19,.19,.07,20.8,1.5+i*.3,-4.64,coolant);gauge.userData.system='cooling';}

 }
 makeAvatar(color){const g=new THREE.Group();const m=new THREE.MeshStandardMaterial({color,emissive:color,emissiveIntensity:.3});const body=new THREE.Mesh(new THREE.CylinderGeometry(.25,.3,.85,10),m);body.position.y=.7;g.add(body);const head=new THREE.Mesh(new THREE.SphereGeometry(.25,12,8),m);head.position.y=1.4;g.add(head);const ring=new THREE.Mesh(new THREE.TorusGeometry(.6,.035,6,24),m);ring.rotation.x=Math.PI/2;ring.position.y=.1;g.add(ring);return g;}
 spawn(state){this.player.set(state.position.x,state.position.y,state.position.z);this.yaw=state.yaw||0;if(this.mode==='explore'){this.camera.position.copy(this.player);this.camera.rotation.set(0,this.yaw,0);}}
 setMode(mode){this.controls.unlock();this.mode=mode;if(mode==='explore'){this.camera.fov=70;this.camera.position.copy(this.player);this.camera.rotation.set(0,this.yaw,0);if(this.active)this.controls.lock();}else this.camera.fov=43;this.camera.updateProjectionMatrix();}
 focus(room){const p={cryo:[-16,0],bridge:[0,0],reactor:[17,0],bio:[0,-17]}[room];this.target.set(p[0],0,p[1]);this.overviewRadius=22;this.overviewHeight=18;if(this.mode!=='overview')this.setMode('overview');}
 focusCrew(id){const p=this.crew.characters.find(x=>x.profile.id===id);if(!p)return;this.setMode('overview');this.target.copy(p.group.position).add(new THREE.Vector3(0,1,-.4));this.overviewRadius=5.5;this.overviewHeight=3.2;this.azimuth=3.7;}
 resetOverview(){this.target.set(0,0,0);this.overviewRadius=72;this.overviewHeight=46;this.azimuth=.8;}
 setActive(value){this.active=value;if(!value){this.controls.unlock();this.controls.velocity.set(0,0,0);}}
 snapshots(players,self){const present=new Set();for(const p of players){if(p.id===self)continue;present.add(p.id);let item=this.remote.get(p.id);if(!item){const mesh=this.makeAvatar(0xb9a7ef);this.scene.add(mesh);item={mesh,target:new THREE.Vector3()};this.remote.set(p.id,item);}item.target.set(p.x,.15,p.z);item.mesh.rotation.y=p.yaw;}for(const [id,item] of this.remote){if(!present.has(id)){this.scene.remove(item.mesh);item.mesh.traverse(o=>{o.geometry?.dispose();if(o.material)o.material.dispose();});this.remote.delete(id);}}}
 resize(){const w=this.container.clientWidth,h=this.container.clientHeight;if(!w||!h)return;this.renderer.setSize(w,h,false);this.camera.aspect=w/h;this.camera.updateProjectionMatrix();}
 updateState(s){if(this.growLamps)for(const m of this.growLamps)m.material.emissiveIntensity=s.greenhouse_active?1.6:.18;if(this.leafMaterial)this.leafMaterial.color.set(s.greenhouse_active?0x87bc91:0x555f46);if(this.statusBeacons)for(const m of this.statusBeacons)m.material.color.set(s.cooling_repaired?0x80dfb3:0xec9c66);this.reactorLight.color.set(s.cooling_repaired?0x80dfdb:0xefaf70);this.core.children[0].material.emissiveIntensity=s.reactor_active?1.8:.5;}
 animate(){requestAnimationFrame(this.frame);this.frames++;if(performance.now()-this.lastFps>1000){this.fps=this.frames;this.frames=0;this.lastFps=performance.now();}const dt=Math.min(this.clock.getDelta(),.05);if(this.mode==='overview'){const fit=Math.max(1,1.6/this.camera.aspect);const r=this.overviewRadius*fit;this.camera.position.set(Math.sin(this.azimuth)*r+this.target.x,this.overviewHeight*fit,Math.cos(this.azimuth)*r+this.target.z);this.camera.lookAt(this.target);this.avatar.visible=true;}else{if(this.active)this.controls.update(dt);this.camera.position.x=THREE.MathUtils.clamp(this.camera.position.x,-24,24);this.camera.position.z=THREE.MathUtils.clamp(this.camera.position.z,-24,8);this.player.copy(this.camera.position);this.yaw=this.camera.rotation.y;this.avatar.visible=false;}this.avatar.position.set(this.player.x,.15,this.player.z);this.avatar.rotation.y=this.yaw;for(const item of this.remote.values())item.mesh.position.lerp(item.target,1-Math.exp(-dt*12));if(this.core)this.core.rotation.y+=dt*.1;this.crew.animate(this.clock.elapsedTime);if(this.active)this.renderer.render(this.scene,this.camera);}
}
