import * as THREE from 'three';

// Browser text stays at native CSS resolution in both WebGL and Canvas mode.
// The same camera and opaque scene meshes decide visibility; names cannot show
// through a closed bulkhead merely because their DOM element is on top.
export class CrewLabels {
 constructor(container){
  this.container=container;this.entries=new Map();this.ray=new THREE.Raycaster();
  this.anchor=new THREE.Vector3();this.projected=new THREE.Vector3();this.origin=new THREE.Vector3();this.direction=new THREE.Vector3();this.hits=[];
  const doc=container?.ownerDocument;
  if(!doc?.createElement)return;
  this.layer=doc.createElement('div');this.layer.className='crew-label-layer';this.layer.setAttribute('aria-hidden','true');
  Object.assign(this.layer.style,{position:'absolute',inset:'0',overflow:'hidden',pointerEvents:'none',zIndex:'1'});container.append(this.layer);
 }
 hide(){for(const element of this.entries.values())element.hidden=true;}
 element(profile){
  let element=this.entries.get(profile.id);if(element)return element;
  const doc=this.container.ownerDocument;element=doc.createElement('div');element.className='crew-nameplate';element.dataset.agentId=profile.id;element.hidden=true;
  Object.assign(element.style,{position:'absolute',left:'0',top:'0',maxWidth:'240px',padding:'7px 11px',border:'1px solid rgba(191,216,231,.45)',borderRadius:'7px',background:'rgba(12,28,40,.9)',color:'#f1f6f8',font:'600 14px/1.3 system-ui, -apple-system, sans-serif',whiteSpace:'nowrap',textAlign:'center',boxShadow:'0 3px 12px rgba(0,0,0,.2)',pointerEvents:'none'});
  const name=doc.createElement('span');name.textContent=profile.short;element.append(name);
  const role=doc.createElement('small');role.textContent=profile.role;Object.assign(role.style,{display:'block',marginTop:'2px',font:'400 11px/1.35 system-ui, -apple-system, sans-serif',color:'#b9d0df'});element.append(role);
  this.entries.set(profile.id,element);this.layer.append(element);return element;
 }
 update(camera,characters,occluderRoots=[],{active=true,mode='explore',pixelRatio=globalThis.devicePixelRatio||1}={}){
  if(!this.layer)return;
  if(!active){this.hide();return;}
  const width=this.container.clientWidth,height=this.container.clientHeight;if(!width||!height){this.hide();return;}
  camera.updateMatrixWorld(true);camera.getWorldPosition(this.origin);
  const ratio=Math.max(1,Math.min(4,pixelRatio)),maxDistance=mode==='overview'?40:14;
  const candidates=[];
  this.hide();
  for(const c of characters){
   const element=this.element(c.profile);
   if(!c.loaded||!c.rig?.head)continue;
   let visible=true;for(let p=c.group;p;p=p.parent)if(!p.visible){visible=false;break;}if(!visible)continue;
   c.group.updateMatrixWorld(true);c.rig.head.getWorldPosition(this.anchor);this.anchor.y+=.36;
   const distance=this.anchor.distanceTo(this.origin);if(distance<.8||distance>maxDistance)continue;
   this.projected.copy(this.anchor).applyMatrix4(camera.matrixWorldInverse);if(this.projected.z>=-.05)continue;
   this.projected.copy(this.anchor).project(camera);
   if(this.projected.z< -1||this.projected.z>1||Math.abs(this.projected.x)>.94||Math.abs(this.projected.y)>.92)continue;
   const x=Math.round((this.projected.x+1)*width*.5*ratio)/ratio,y=Math.round((1-this.projected.y)*height*.5*ratio)/ratio;
   candidates.push({element,anchor:this.anchor.clone(),distance,x,y});
  }
  // Most rooms have no mentor on screen. Avoid walking and updating thousands
  // of scene nodes merely to discover that there is no name to draw.
  if(!candidates.length)return;
  const blockers=[];
  for(const root of occluderRoots){root.updateMatrixWorld(true);root.traverseVisible(o=>{if(!o.isMesh||o.userData.labelPassThrough)return;const materials=Array.isArray(o.material)?o.material:[o.material];if(materials.some(m=>m&&m.depthWrite!==false&&!m.transparent&&m.opacity!==0))blockers.push(o);});}
  for(const {element,anchor,distance,x,y} of candidates){
   this.direction.copy(anchor).sub(this.origin).normalize();this.ray.set(this.origin,this.direction);this.ray.near=.03;this.ray.far=distance-.08;
   let blocked=false;
   // Occlusion needs one hit, not a sorted list of every surface behind it.
   for(const mesh of blockers){this.hits.length=0;this.ray.intersectObject(mesh,false,this.hits);if(this.hits.length){blocked=true;break;}}
   if(blocked)continue;
   const transform=`translate(${x}px, ${y}px) translate(-50%, -100%)`;
   if(element.style.transform!==transform)element.style.transform=transform;
   element.hidden=false;
  }
 }
 dispose(){this.layer?.remove();this.entries.clear();}
}
