import * as THREE from 'three';
import {CosmonautModels} from './CosmonautModel.js';
import {CrewLabels} from './CrewLabels.js';
import {walkable,roomAt} from '../interior/DeckLayout.js';
export const CREW=[
{id:'savinykh',name:'Виктор Петрович Савиных',short:'Виктор Савиных',role:'Капитан · у штурвала',initials:'ВС',color:0xe6c799,position:[3,0,-2.7],hair:0xbec4c0,suit:0x33475e,height:1.02},
{id:'dzhanibekov',name:'Владимир Александрович Джанибеков',short:'Владимир Джанибеков',role:'Инженер · реактор',initials:'ВД',color:0xe5a779,position:[13,0,2],hair:0x645e59,suit:0x5d5b54,height:1.04},
{id:'ryazansky',name:'Сергей Николаевич Рязанский',short:'Сергей Рязанский',role:'Биолог · оранжерея',initials:'СР',color:0x94cba7,position:[-20,0,-14],hair:0x635b51,suit:0x3f665d,height:1.08},
{id:'krikalev',name:'Сергей Константинович Крикалёв',short:'Сергей Крикалёв',role:'Связь и навигация · мостик',initials:'СК',color:0xa5bfe8,position:[-6.7,0,-2],hair:0x3e4348,suit:0x49546b,height:1.03}];
export class BridgeCrew {
 constructor(scene,container) {
  this.models=new CosmonautModels();this.scene=scene;this.ray=new THREE.Raycaster();this.labels=new CrewLabels(container);
  this.characters=CREW.map(profile=>{const group=new THREE.Group();group.position.set(...profile.position);return{profile,group,loaded:false,marker:null,behavior:'helpful'};});
 }
 register(root){const found=[];root.traverse(o=>{if(o.userData.npcRoot)found.push(o);});for(const o of found){const entry=this.characters.find(x=>x.profile.id===o.userData.npcRoot);if(!entry)continue;if(entry.loaded&&entry.group===o){entry.rig=entry.model?.rig||entry.rig;continue;}entry.group=o;o.userData={...o.userData};o.position.set(...entry.profile.position);entry.loaded=true;entry.baseRotation=o.userData.crewBaseRotation??o.rotation.y;o.userData.crewBaseRotation=entry.baseRotation;o.rotation.y=entry.baseRotation;entry.home=o.position.clone();entry.rig={};o.traverse(part=>{if(part.userData.rig)entry.rig[part.userData.rig]=part;});this.enhance(entry);if(entry.marker)entry.marker.visible=false;}}
 enhance(c){
  if(c.group.userData.bakedPortrait){
   c.model={root:c.group.children.find(o=>o.userData.portrait===c.profile.id),rig:c.rig};
   c.group.userData.enhanced=true;c.group.userData.portrait=c.profile.id;return;
  }
  if(c.model?.root.parent===c.group){c.rig=c.model.rig;return;}
  // The streamed root remains the authoritative identity / AOI anchor. Replace
  // its visual children with a reusable portrait model, retaining its behaviors.
  if(!c.model)c.model=this.models.create(c.profile);
  c.group.clear();c.group.add(c.model.root);
  c.rig=c.model.rig;c.group.userData.enhanced=true;
  c.group.userData.portrait=c.profile.id;
 }
 unregister(root){root.traverse(o=>{const entry=this.characters.find(x=>x.profile.id===o.userData.npcRoot);if(entry&&entry.group===o){if(o.userData.bakedPortrait)entry.model=null;entry.group=new THREE.Group();entry.group.position.set(...entry.profile.position);entry.loaded=false;entry.rig={};if(entry.marker)entry.marker.visible=true;}});}
 setBehaviors(agents=[]){for(const c of this.characters)c.behavior=agents.find(a=>a.id===c.profile.id)?.behavior||'helpful';}
 updateLabels(camera,occluders,options){this.labels.update(camera,this.characters,occluders,options);}
 setTalking(id){this.talking=id;clearTimeout(this.timer);this.timer=setTimeout(()=>this.talking=null,10000);}
 pick(event,canvas,camera){const r=canvas.getBoundingClientRect();this.ray.setFromCamera(new THREE.Vector2((event.clientX-r.left)/r.width*2-1,-(event.clientY-r.top)/r.height*2+1),camera);return this.ray.intersectObjects(this.characters.flatMap(x=>[x.group,...(x.marker?[x.marker]:[])]),true)[0]?.object.userData.agentID||null;}
 nearest(position){let best=null,distance=3;for(const c of this.characters){const d=Math.hypot(c.group.position.x-position.x,c.group.position.z-position.z);if(d<distance){distance=d;best=c.profile.id;}}return best;}
 animate(t,player,reducedMotion=false){
  const dt=Math.max(0,Math.min(.1,t-(this.lastAnimationTime??t-.016)));this.lastAnimationTime=t;
  for(let i=0;i<this.characters.length;i++){
   const c=this.characters[i];if(!c.loaded)continue;const rig=c.rig,passive=c.behavior==='passive',selected=this.talking===c.profile.id&&!passive,patrol=c.behavior==='patrol'&&!selected;
   if(c.home){c.group.position.copy(c.home);if(patrol&&!reducedMotion){const x=c.home.x+Math.sin(t*.45+i)*.6,z=c.home.z+Math.cos(t*.45+i)*.4;if(walkable(x,z,.28))c.group.position.set(x,c.home.y,z);c.group.rotation.y=Math.atan2(-Math.cos(t*.45+i),Math.sin(t*.45+i));}else c.group.rotation.y=c.baseRotation;}
   if(rig.body)rig.body.position.y=reducedMotion?0:Math.sin(t*1.4+i)*.004;
   if(rig.head){rig.head.rotation.x=reducedMotion?0:selected?Math.sin(t*2.2)*.035:Math.sin(t*.6+i)*.009;let yaw=reducedMotion?0:Math.sin(t*.35+i)*.04;
    if(!reducedMotion&&player&&!passive&&Math.hypot(player.x-c.group.position.x,player.z-c.group.position.z)<4.5&&roomAt(player.x,player.z)?.id===roomAt(c.group.position.x,c.group.position.z)?.id){const angle=Math.atan2(c.group.position.x-player.x,c.group.position.z-player.z)-c.group.rotation.y;yaw=THREE.MathUtils.clamp(Math.atan2(Math.sin(angle),Math.cos(angle)),-.65,.65);}
    rig.head.rotation.y=reducedMotion?0:THREE.MathUtils.lerp(rig.head.rotation.y,yaw,Math.min(1,dt*5));}
   if(rig.ring)rig.ring.material.opacity=selected&&!reducedMotion?.8+Math.sin(t*4)*.15:.4;
   for(let j=0;j<2;j++){const arm=rig['arm'+j],leg=rig['leg'+j],elbow=rig['elbow'+j],knee=rig['knee'+j],side=j?1:-1,step=Math.sin(t*3+i)*side;if(arm){arm.rotation.x=reducedMotion||passive?0:patrol?step*.19:selected?-.10+Math.sin(t*1.7+j)*.08:Math.sin(t*.9+j)*.012;arm.rotation.z=selected&&!reducedMotion?side*(.025+Math.sin(t*1.4)*.025):0;}if(leg)leg.rotation.x=patrol&&!reducedMotion?-step*.20:0;if(knee)knee.rotation.x=patrol&&!reducedMotion?Math.max(0,step)*.25:0;if(elbow)elbow.rotation.x=-.10+(selected&&!reducedMotion?-.25+Math.sin(t*1.7+j)*.08:0);}
   if(rig.wheel)rig.wheel.rotation.z=reducedMotion?0:Math.sin(t*.45)*.1;
  }
 }
 dispose(){clearTimeout(this.timer);this.labels.dispose();this.models.dispose();}
}
