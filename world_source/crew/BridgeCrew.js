import * as THREE from 'three';
export const CREW=[
{id:'savinykh',name:'Виктор Петрович Савиных',short:'Виктор Савиных',role:'Капитан · у штурвала',initials:'ВС',color:0xe6c799,position:[4.2,0,-3],hair:0xbec4c0,suit:0x33475e,height:1.02},
{id:'dzhanibekov',name:'Владимир Александрович Джанибеков',short:'Владимир Джанибеков',role:'Инженер · реактор',initials:'ВД',color:0xe5a779,position:[13,0,2],hair:0x645e59,suit:0x5d5b54,height:1.04},
{id:'ryazansky',name:'Сергей Николаевич Рязанский',short:'Сергей Рязанский',role:'Биолог · оранжерея',initials:'СР',color:0x94cba7,position:[-4,0,-16],hair:0x635b51,suit:0x3f665d,height:1.08},
{id:'krikalev',name:'Сергей Константинович Крикалёв',short:'Сергей Крикалёв',role:'Связь и навигация · мостик',initials:'СК',color:0xa5bfe8,position:[-5,0,-3],hair:0x3e4348,suit:0x49546b,height:1.03}];
export class BridgeCrew{
constructor(scene){this.scene=scene;this.characters=[];this.materials=new Map();this.ray=new THREE.Raycaster();for(const p of CREW){const group=this.character(p);group.position.set(...p.position);if(p.id==='dzhanibekov')group.rotation.y=-.7;if(p.id==='ryazansky')group.rotation.y=.25;scene.add(group);this.characters.push({profile:p,group,baseRotation:group.rotation.y});}}
material(color,metalness=.1,roughness=.65){const key=[color,metalness,roughness].join(':');if(!this.materials.has(key))this.materials.set(key,new THREE.MeshStandardMaterial({color,metalness,roughness,flatShading:true}));return this.materials.get(key);}
mesh(geo,mat,x,y,z,parent){const o=new THREE.Mesh(geo,mat);o.position.set(x,y,z);parent.add(o);return o;}
box(parent,w,h,d,x,y,z,mat){return this.mesh(new THREE.BoxGeometry(w,h,d),mat,x,y,z,parent);}
sphere(parent,r,x,y,z,mat,sx=1,sy=1,sz=1){const o=this.mesh(new THREE.SphereGeometry(r,12,10),mat,x,y,z,parent);o.scale.set(sx,sy,sz);return o;}
rod(parent,a,b,radius,mat){const dir=new THREE.Vector3().subVectors(b,a);const o=this.mesh(new THREE.CylinderGeometry(radius*.92,radius,dir.length(),10),mat,0,0,0,parent);o.position.copy(a).add(b).multiplyScalar(.5);o.quaternion.setFromUnitVectors(new THREE.Vector3(0,1,0),dir.normalize());return o;}
character(p){
const g=new THREE.Group(),body=new THREE.Group();g.add(body);body.scale.y=p.height;g.userData.body=body;
const suit=this.material(p.suit),dark=this.material(0x192630),boots=this.material(0x151f26,.2),skin=this.material(p.id==='ryazansky'?0xd3b49a:0xd4b39d,0),trim=this.material(p.color,.2),silver=this.material(0xa0aeb5,.65,.4),hair=this.material(p.hair);
const shadow=this.mesh(new THREE.CircleGeometry(.57,32),new THREE.MeshBasicMaterial({color:0x020609,transparent:true,opacity:.28,depthWrite:false}),0,.012,0,g);shadow.rotation.x=-Math.PI/2;
for(const x of [-.155,.155]){const leg=this.mesh(new THREE.CylinderGeometry(.12,.105,.65,10),suit,x,.48,.015,body);this.sphere(body,.125,x,.3,-.015,suit,1,.9,1);this.box(body,.25,.19,.4,x,.105,-.075,boots);this.box(body,.26,.035,.41,x,.025,-.08,dark);this.box(body,.16,.035,.02,x,.75,-.105,trim);}
const points=[[.23,.76],[.26,.9],[.29,1.14],[.32,1.35],[.21,1.43]].map(([r,y])=>new THREE.Vector2(r,y));const jacket=this.mesh(new THREE.LatheGeometry(points,16),suit,0,0,0,body);jacket.scale.z=.72;
this.box(body,.48,.06,.35,0,.79,0,dark);this.box(body,.08,.06,.025,0,.79,-.19,silver);
this.box(body,.014,.5,.018,0,1.1,-.226,silver);
for(const x of [-.16,.16]){this.box(body,.16,.15,.035,x,1.15,-.23,suit);this.box(body,.16,.025,.045,x,1.225,-.24,trim);}
this.box(body,.095,.065,.015,.16,1.34,-.22,this.material(0xdde6e8));this.box(body,.095,.014,.018,.16,1.34,-.23,this.material(0xb56e63));
this.mesh(new THREE.CylinderGeometry(.105,.11,.13,12),skin,0,1.48,0,body);
this.mesh(new THREE.CylinderGeometry(.145,.155,.065,12),dark,0,1.435,0,body);
const head=new THREE.Group();head.position.y=1.72;body.add(head);g.userData.head=head;
this.sphere(head,.235,0,0,0,skin,.86,1.13,.92);this.sphere(head,.073,0,-.145,-.06,skin,.9,.7,1);
for(const side of [-1,1]){this.sphere(head,.06,side*.202,-.015,.012,skin,.55,1,.65);this.sphere(head,.03,side*.076,.035,-.204,this.material(0xe7e2d8),1,.6,.3);this.sphere(head,.014,side*.076,.034,-.214,this.material(0x39444c),1,.85,.5);const brow=this.box(head,.07,.019,.025,side*.078,.086,-.21,hair);brow.rotation.z=side*.1;}
this.sphere(head,.052,0,-.025,-.216,skin,.65,.9,1);this.box(head,.072,.009,.009,0,-.11,-.19,this.material(0x9f7770,0));
const cap=this.mesh(new THREE.SphereGeometry(.24,16,10,0,Math.PI*2,0,Math.PI*.43),hair,0,.018,.01,head);cap.scale.set(.87,1.08,.94);
for(const x of [-.19,.19]){this.box(head,.025,.12,.1,x,.035,.012,hair);}
if(p.id==='savinykh'){for(const x of [-.077,.077]){const frame=this.mesh(new THREE.TorusGeometry(.058,.007,5,16),silver,x,.035,-.222,head);frame.scale.y=.7;}this.box(head,.044,.01,.01,0,.035,-.226,silver);for(const x of [-.28,.28]){this.box(body,.17,.04,.17,x,1.375,0,trim);for(let i=0;i<3;i++)this.box(body,.017,.012,.16,x-.045+i*.043,1.402,0,dark);}}
if(p.id==='krikalev'){this.mesh(new THREE.TorusGeometry(.224,.012,6,20),dark,0,.01,.02,head).rotation.y=Math.PI/2;this.sphere(head,.054,-.214,-.01,0,dark,.45,1,1);this.rod(head,new THREE.Vector3(-.22,-.04,-.01),new THREE.Vector3(-.13,-.11,-.24),.009,silver);}
const armGroups=[];
for(const side of [-1,1]){const arm=new THREE.Group();body.add(arm);this.sphere(arm,.135,side*.32,1.33,0,suit,1,.95,.92);const shoulder=new THREE.Vector3(side*.33,1.3,0),elbow=new THREE.Vector3(side*.39,1.08,-.12),hand=new THREE.Vector3(side*.34,.96,-.3);if(p.id==='savinykh'){hand.set(side*.43,1.12,-.59);elbow.set(side*.42,1.12,-.2);}if(p.id==='krikalev'||p.id==='ryazansky'||p.id==='dzhanibekov'){hand.set(side*.19,1.12,-.36);}
this.rod(arm,shoulder,elbow,.1,suit);this.rod(arm,elbow,hand,.08,suit);this.sphere(arm,.079,hand.x,hand.y,hand.z,skin,1,.95,.85);this.box(arm,.13,.04,.13,side*.34,1.06,-.18,dark);armGroups.push(arm);if(side===-1)this.box(arm,.02,.12,.12,-.435,1.27,0,trim);}
g.userData.arms=armGroups;
if(p.id==='savinykh'){const wheel=new THREE.Group();wheel.position.set(0,1.12,-.62);body.add(wheel);this.mesh(new THREE.TorusGeometry(.43,.03,8,40),silver,0,0,0,wheel);for(let i=0;i<6;i++){const a=i*Math.PI/3;this.rod(wheel,new THREE.Vector3(),new THREE.Vector3(Math.cos(a)*.43,Math.sin(a)*.43,0),.015,silver);}this.sphere(wheel,.07,0,0,0,trim);this.rod(body,new THREE.Vector3(0,.06,-.72),new THREE.Vector3(0,1.12,-.62),.055,silver);g.userData.wheel=wheel;}
else if(p.id==='ryazansky'){this.mesh(new THREE.CylinderGeometry(.035,.035,.22,10),new THREE.MeshStandardMaterial({color:0xb9e9cc,transparent:true,opacity:.65,roughness:.2}),.16,1.19,-.36,body);this.mesh(new THREE.CylinderGeometry(.038,.038,.035,10),trim,.16,1.315,-.36,body);const pad=this.box(body,.22,.28,.025,-.12,1.14,-.39,dark);pad.rotation.x=.5;}
else{const pad=this.box(body,.34,.25,.035,0,1.11,-.4,dark);pad.rotation.x=.45;const screen=this.box(body,.29,.18,.012,0,1.12,-.425,new THREE.MeshStandardMaterial({color:p.color,emissive:p.color,emissiveIntensity:.35}));screen.rotation.x=.45;for(let j=0;j<3;j++)this.box(body,.16,.008,.016,-.04,1.16-j*.04,-.435,dark);}
// Mission patches, stitching and equipment remain local procedural geometry.
for(const side of [-1,1]){
 this.box(body,.018,.46,.012,side*.205,1.07,-.222,trim);
 this.box(body,.18,.035,.02,side*.155,.35,-.1,dark);
}
this.box(body,.11,.13,.025,-.14,1.34,-.23,trim);
this.box(body,.075,.018,.027,-.14,1.35,-.25,silver);
if(p.id==='dzhanibekov'){
 for(let i=0;i<3;i++){this.box(body,.045,.19,.045,.2-i*.09,.76,-.21,silver);this.box(body,.065,.07,.06,.2-i*.09,.85,-.22,trim);}
 this.box(body,.15,.23,.11,-.29,.85,.03,dark);
}
if(p.id==='krikalev'){
 this.box(body,.12,.18,.05,-.17,1.17,-.26,dark);
 this.box(body,.03,.17,.03,-.2,1.32,-.25,silver);
}
const ring=this.mesh(new THREE.TorusGeometry(.55,.018,5,36),new THREE.MeshBasicMaterial({color:p.color,transparent:true,opacity:.65}),0,.025,0,g);ring.rotation.x=-Math.PI/2;g.userData.ring=ring;
const canvas=document.createElement('canvas');canvas.width=512;canvas.height=112;const c=canvas.getContext('2d');c.fillStyle='#0e1a26dd';c.beginPath();c.roundRect(4,4,504,104,15);c.fill();c.fillStyle='#dbe9ef';c.font='500 28px system-ui';c.textAlign='center';c.fillText(p.short,256,45);c.font='21px system-ui';c.fillStyle='#90bca5';c.fillText(p.role,256,80);const label=new THREE.Sprite(new THREE.SpriteMaterial({map:new THREE.CanvasTexture(canvas),depthTest:false,transparent:true,opacity:.95}));label.position.set(0,2.4,0);label.scale.set(3.4,.74,1);g.add(label);
g.traverse(o=>o.userData.agentID=p.id);return g;}
setTalking(id){this.talking=id;clearTimeout(this.talkTimer);this.talkTimer=setTimeout(()=>this.talking=null,10000);}
pick(event,canvas,camera){const r=canvas.getBoundingClientRect();this.ray.setFromCamera(new THREE.Vector2((event.clientX-r.left)/r.width*2-1,-(event.clientY-r.top)/r.height*2+1),camera);return this.ray.intersectObjects(this.characters.map(x=>x.group),true)[0]?.object.userData.agentID||null;}
nearest(position){let best=null,distance=3;for(const p of this.characters){const d=p.group.position.distanceTo(new THREE.Vector3(position.x,0,position.z));if(d<distance){best=p.profile.id;distance=d;}}return best;}
animate(t){for(let i=0;i<this.characters.length;i++){const g=this.characters[i].group,selected=this.talking===this.characters[i].profile.id;g.userData.body.position.y=Math.sin(t*1.4+i)*.008;g.userData.head.rotation.y=Math.sin(t*.35+i)*.035;g.userData.head.rotation.x=selected?Math.sin(t*3)*.04:Math.sin(t*.6+i)*.018;g.userData.ring.material.opacity=selected?.8+Math.sin(t*4)*.15:.4;if(g.userData.wheel)g.userData.wheel.rotation.z=Math.sin(t*.35)*.035;if(selected)g.userData.arms[1].rotation.x=Math.sin(t*2)*.025;}}
}
