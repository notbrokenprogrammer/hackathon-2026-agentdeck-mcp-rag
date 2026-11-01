import * as THREE from 'three';
import {layout,walls} from './DeckLayout.js';
import {applySurfaceMaterials} from './SurfaceMaterials.js';

// Authored geometry, baked into the normal ObjectLoader world resources.
// The current seven-room layout takes precedence over the old four-room plan.
export const observationWindows = [
  {area:'bridge',axis:'z',fixed:-7,min:2.20,max:7.65,bottom:1.02,top:3.30},
  {area:'bridge',axis:'x',fixed:8,min:-6.65,max:-2.35,bottom:1.02,top:3.30},
  {area:'bio',axis:'x',fixed:-10,min:-21.15,max:-14.30,bottom:1.32,top:3.12},
  {area:'habitat',axis:'z',fixed:21,min:-17.5,max:-12.5,bottom:1.20,top:3.05},
];

function tagged(parent,name,area,role='detail') {
  const g=new THREE.Group();g.name=name;
  g.userData={area,areas:Array.isArray(area)?area:[area],interiorRole:role};
  parent.add(g);return g;
}
function random(seed) {let n=seed>>>0;return()=>{n=(1664525*n+1013904223)>>>0;return n/4294967296;};}

function tuneMaterials(i) {
  i.mat('white').metalness=.08;i.mat('white').roughness=.57;
  i.mat('pearl').metalness=.19;i.mat('pearl').roughness=.48;
  i.mat('floor').color.set(0xaeb7bd);i.mat('floor').roughness=.49;i.mat('floor').metalness=.4;
  i.mat('metal').roughness=.34;i.mat('metal').metalness=.78;
  i.mat('rubber').roughness=.94;i.mat('rubber').metalness=0;
  i.mat('glass').opacity=.085;i.mat('glass').roughness=.12;i.mat('glass').side=THREE.DoubleSide;
  for(const [name,color]of[['warm-light',0xffdfb1],['titanium',0x8d959b],['seal',0x192127]]) {
    if(i.materials.has(name))continue;
    i.materials.set(name,new THREE.MeshStandardMaterial({color,roughness:name==='seal'?.91:.43,metalness:name==='titanium'?.8:.12,...(name==='warm-light'?{emissive:color,emissiveIntensity:1.15}:{})}));
  }
  i.mat('warm-light').userData.system='power';i.mat('light').userData.system='power';
  for(const [key,system]of[['reactor','reactor'],['cooling','cooling'],['plant','greenhouse'],['grow','grow-lamp']])i.mat(key).userData.system=system;
  applySurfaceMaterials(i);
}

function intersectingAreas(horizontal,fixed,lo,hi) {
  return layout.areas.filter(a=>Math.abs(Math.abs((horizontal?a.z:a.x)-fixed)-(horizontal?a.d:a.w)/2)<.02 &&
    Math.min((horizontal?a.x:a.z)+(horizontal?a.w:a.d)/2,hi)>Math.max((horizontal?a.x:a.z)-(horizontal?a.w:a.d)/2,lo)+.001).map(a=>a.id);
}

function bulkheads(i) {
  i.bulkheads=tagged(i.root,'Pressure hull / shared structural walls',[],'shell');
  const lines=new Map();
  for(const b of walls) {
    const horizontal=b.w>b.d,fixed=horizontal?b.z:b.x,middle=horizontal?b.x:b.z,length=horizontal?b.w:b.d;
    const key=(horizontal?'z:':'x:')+fixed.toFixed(5);
    if(!lines.has(key))lines.set(key,{horizontal,fixed,intervals:[]});
    lines.get(key).intervals.push([middle-length/2,middle+length/2]);
  }
  for(const {horizontal,fixed,intervals}of lines.values()) {
    intervals.sort((a,b)=>a[0]-b[0]);const merged=[];
    for(const range of intervals){const last=merged.at(-1);if(last&&range[0]<=last[1]+.001)last[1]=Math.max(last[1],range[1]);else merged.push([...range]);}
    const apertures=observationWindows.filter(w=>w.axis===(horizontal?'z':'x')&&Math.abs(w.fixed-fixed)<.001);
    for(const [lo,hi]of merged) {
      const cuts=[lo,hi,...apertures.flatMap(w=>[w.min,w.max]).filter(v=>v>lo&&v<hi)].sort((a,b)=>a-b);
      for(let n=0;n<cuts.length-1;n++) {
        const a=cuts[n],b=cuts[n+1],window=apertures.find(w=>(a+b)/2>w.min&&(a+b)/2<w.max);
        const heights=window?[[0,window.bottom],[window.top,layout.height]]:[[0,layout.height]];
        const pieces=Math.ceil((b-a)/2.4);
        for(let p=0;p<pieces;p++)for(const [bottom,top]of heights) {
          const l=a+(b-a)*p/pieces,r=a+(b-a)*(p+1)/pieces,mid=(l+r)/2;
          const areas=intersectingAreas(horizontal,fixed,l,r),g=tagged(i.bulkheads,'Structural wall',areas,'shell');
          i.box(g,'pearl',horizontal?mid:fixed,(bottom+top)/2,horizontal?fixed:mid,horizontal?r-l:layout.wall,top-bottom,horizontal?layout.wall:r-l).name='structural-wall';
        }
      }
    }
  }
  for(const gate of layout.gates){const g=tagged(i.bulkheads,'Shared lintel / '+gate.id,gate.areas,'shell');const m=i.box(g,'pearl',gate.x,(layout.height+layout.doorHeight)/2,gate.z,gate.rotation?layout.doorWidth:layout.wall,layout.height-layout.doorHeight,gate.rotation?layout.wall:layout.doorWidth);m.name='door-lintel';m.userData.gateID=gate.id;}
}

function grille(i,g,x,y,z,width,height,rotation=0) {
  const unit=new THREE.Group();unit.position.set(x,y,z);unit.rotation.y=rotation;g.add(unit);
  i.bevel(unit,'titanium',0,0,0,width+.10,height+.08,.065);
  i.box(unit,'seal',0,0,.039,width,height,.018);
  const rows=Math.max(4,Math.round(height/.045));
  for(let j=0;j<rows;j++)i.box(unit,'metal',0,-height/2+(j+.5)*height/rows,.057,width-.035,.012,.032,[.27,0,0]);
  for(const sx of[-1,1])for(const sy of[-1,1])i.cylinder(unit,'dark',sx*(width/2+.012),sy*(height/2+.006),.043,.013,.012,[Math.PI/2,0,0]);
}

function shell(i,a,g) {
  const structure=tagged(g,a.name+' / floor structure',a.id,'shell');
  const support=i.box(structure,'rubber',a.x,-.24,a.z,a.w,.32,a.d);support.name='deck-support';support.userData.area=a.id;
  const nx=Math.ceil(a.w/2.15),nz=Math.ceil(a.d/2.15),tw=a.w/nx,td=a.d/nz;
  const fittings=tagged(g,a.name+' / deck fittings',a.id);
  for(let x=0;x<nx;x++)for(let z=0;z<nz;z++) {
    const px=a.x-a.w/2+(x+.5)*tw,pz=a.z-a.d/2+(z+.5)*td;
    const panel=i.box(structure,'floor',px,-.035,pz,tw-.016,.07,td-.016);panel.name='deck-panel';panel.userData.area=a.id;
    if(a.kind==='corridor'||x===0||x===nx-1) {
      // Peripheral service strips are recessed; the broad walking field stays calm.
      const gx=px+(x===0?-.36:.36)*tw,gw=.20;
      i.box(fittings,'seal',gx,.004,pz,gw,.012,td-.19);
      for(let q=0;q<9;q++)i.box(fittings,'titanium',gx,.013,pz-td*.4+q*td*.1,gw-.025,.009,.022);
    }
    for(const sx of[-1,1])for(const sz of[-1,1])if((x+z)%2===0)i.cylinder(fittings,'metal',px+sx*(tw/2-.105),.006,pz+sz*(td/2-.105),.017,.008);
  }
  const roof=tagged(g,'ceiling',a.id,'ceiling');i.ceilings.push(roof);
  // Ceiling tiles meet only at edges, including where two rooms share a wall.
  for(let x=0;x<nx;x++)for(let z=0;z<nz;z++) {
    const px=a.x-a.w/2+(x+.5)*tw,pz=a.z-a.d/2+(z+.5)*td;
    const slab=i.box(roof,'pearl',px,layout.height+.12,pz,tw,.24,td);slab.name='ceiling-slab';slab.userData.area=a.id;
    i.bevel(roof,'white',px,layout.height-.047,pz,tw-.09,.07,td-.09);
    if(z%2===0&&x%2===0){const vent=tagged(roof,'Ceiling air return',a.id,'ceiling');vent.position.set(px,3.69,pz);vent.rotation.x=Math.PI/2;grille(i,vent,0,0,0,Math.min(1,tw-.25),.40);}
  }
  for(const b of walls.filter(w=>w.area===a.id)) {
    const horizontal=b.w>b.d,nx=horizontal?0:Math.sign(a.x-b.x),nz=horizontal?Math.sign(a.z-b.z):0,len=horizontal?b.w:b.d;
    const axis=horizontal?'z':'x',fixed=horizontal?b.z:b.x,mid=horizontal?b.x:b.z;
    const windows=observationWindows.filter(w=>w.area===a.id&&w.axis===axis&&Math.abs(w.fixed-fixed)<.01);
    const interior=tagged(g,'Wall finish / '+axis+'/'+fixed,a.id);
    i.box(interior,'seal',b.x+nx*.151,.14,b.z+nz*.151,horizontal?len:.025,.28,horizontal?.025:len);
    i.box(interior,'blue',b.x+nx*.169,.46,b.z+nz*.169,horizontal?len:.025,.065,horizontal?.025:len);
    i.box(interior,'metal',b.x+nx*.17,3.48,b.z+nz*.17,horizontal?len:.07,.14,horizontal?.07:len);
    const n=Math.max(1,Math.ceil(len/2.3));
    for(let t=0;t<n;t++) {
      const lo=mid-len/2+t*len/n,hi=mid-len/2+(t+1)*len/n,point=(lo+hi)/2;
      if(windows.some(w=>hi>w.min-.2&&lo<w.max+.2))continue;
      const x=horizontal?point:b.x,z=horizontal?b.z:point;
      const panel=i.bevel(interior,'white',x+nx*.154,1.89,z+nz*.154,horizontal?hi-lo-.06:.026,2.68,horizontal?.026:hi-lo-.06);panel.name='wall-finish';panel.userData.area=a.id;
      for(const side of[-1,1])for(const y of[.68,3.06])i.box(interior,'titanium',(horizontal?point+side*(len/n/2-.12):b.x)+nx*.181,y,(horizontal?b.z:point+side*(len/n/2-.12))+nz*.181,horizontal?.044:.012,.044,horizontal?.012:.044);
      if(t%3===1&&len/n>1.15)i.wallDetail(a,interior,b,x,z,a.kind==='room');
      if(t%3===0&&len/n>1.3){const lamp=new THREE.Group();lamp.position.set(x+nx*.19,0,z+nz*.19);lamp.rotation.y=Math.atan2(nx,nz);interior.add(lamp);i.bevel(lamp,'metal',-.52,2.15,0,.12,1.16,.08);i.bevel(lamp,'warm-light',-.52,2.15,.052,.05,.96,.035);}
    }
    // Cove hides the wall/ceiling seam without protruding into head height.
    const cove=tagged(roof,'Chamfered ceiling cove',a.id,'ceiling');
    if(!windows.length){const m=i.bevel(cove,'pearl',b.x+nx*.27,3.53,b.z+nz*.27,horizontal?len:.16,.54,horizontal?.16:len);if(horizontal)m.rotation.x=-nz*Math.PI/4;else m.rotation.z=nx*Math.PI/4;}
  }
  const alongX=a.kind==='corridor'&& !a.id.endsWith('-link');
  const rows=a.kind==='corridor'?Math.ceil((alongX?a.w:a.d)/3.2):Math.ceil(a.d/3.2);
  for(let q=0;q<rows;q++) {
    const x=alongX?a.x-a.w/2+(q+.5)*a.w/rows:a.x,z=alongX?a.z:a.z-a.d/2+(q+.5)*a.d/rows;
    const fixture=tagged(roof,'Recessed ceiling luminaire',a.id,'ceiling');fixture.position.set(x,3.52,z);if(alongX)fixture.rotation.y=Math.PI/2;
    const width=Math.min(a.kind==='room'?4:2.3,(alongX?a.d:a.w)-.6);
    i.bevel(fixture,'seal',0,0,0,width,.12,.36);i.bevel(fixture,'metal',0,-.045,0,width-.10,.09,.29);i.box(fixture,'warm-light',0,-.098,0,width-.26,.026,.14);
    for(const sign of[-1,1])i.box(fixture,'cyan',sign*(width/2-.10),-.099,0,.035,.028,.17);
  }
  // Cable trays follow structure, supported by regular clamps.
  if(a.kind==='room')for(const side of[-1,1]) {
    const x=a.x+side*(a.w/2-.43),z0=a.z-a.d/2+.35,z1=a.z+a.d/2-.35;
    i.pipe(roof,'seal',[x,3.44,z0],[x,3.44,z1],.055);i.pipe(roof,'titanium',[x+side*.13,3.50,z0],[x+side*.13,3.50,z1],.038);
    for(let z=z0+.35;z<z1;z+=2.2)i.bevel(roof,'metal',x+side*.05,3.45,z,.37,.10,.10);
  }
}

function chamferPath(path,w,h,r) {
  path.moveTo(-w/2+r,-h/2);path.lineTo(w/2-r,-h/2);path.lineTo(w/2,-h/2+r);path.lineTo(w/2,h/2-r);path.lineTo(w/2-r,h/2);path.lineTo(-w/2+r,h/2);path.lineTo(-w/2,h/2-r);path.lineTo(-w/2,-h/2+r);path.closePath();
}
function frameGeometry(i,w,h,border,depth) {
  const key=`reference-window:${w}:${h}:${border}:${depth}`;
  if(!i.geometries.has(key)){const shape=new THREE.Shape(),hole=new THREE.Path();chamferPath(shape,w+border*2,h+border*2,.32+border);chamferPath(hole,w,h,.32);shape.holes.push(hole);const geometry=new THREE.ExtrudeGeometry(shape,{depth,steps:1,bevelEnabled:true,bevelThickness:.014,bevelSize:.012,bevelSegments:2,curveSegments:1});geometry.translate(0,0,-depth/2);i.geometries.set(key,geometry);}
  return i.geometries.get(key);
}
function observationWindow(i,w) {
  const a=layout.areas.find(a=>a.id===w.area),horizontal=w.axis==='z',center=(w.min+w.max)/2,normal=horizontal?Math.sign(a.z-w.fixed):Math.sign(a.x-w.fixed);
  const g=tagged(i.rooms.get(w.area),'Laminated observation window / '+w.axis,w.area,'shell');
  g.position.set(horizontal?center:w.fixed,(w.bottom+w.top)/2,horizontal?w.fixed:center);g.rotation.y=horizontal?(normal<0?Math.PI:0):(normal>0?Math.PI/2:-Math.PI/2);
  const width=w.max-w.min,height=w.top-w.bottom;
  // The pressure wall has a rectangular cutout, while the glazing is chamfered.
  // A continuous backing ring fills the four corner wedges behind the visible
  // frames; decorative chamfers alone otherwise leave small holes into space.
  const backingKey=`reference-window-backing:${width}:${height}`;
  if(!i.geometries.has(backingKey)){
    const shape=new THREE.Shape(),hole=new THREE.Path(),margin=.045;
    shape.moveTo(-width/2-margin,-height/2-margin);shape.lineTo(width/2+margin,-height/2-margin);shape.lineTo(width/2+margin,height/2+margin);shape.lineTo(-width/2-margin,height/2+margin);shape.closePath();
    chamferPath(hole,width,height,.32);shape.holes.push(hole);
    const geometry=new THREE.ExtrudeGeometry(shape,{depth:layout.wall,steps:1,bevelEnabled:false,curveSegments:1});geometry.translate(0,0,-layout.wall/2);i.geometries.set(backingKey,geometry);
  }
  i.part(g,i.geometries.get(backingKey),'pearl',0,0,0,1,1,1).name='Pressure seal corner backing';
  for(const [mat,border,depth,z]of[['metal',.19,.60,0],['pearl',.15,.20,.32],['seal',.038,.045,.22],['titanium',.018,.035,.255]])i.part(g,frameGeometry(i,width,height,border,depth),mat,0,0,z,1,1,1);
  const shape=new THREE.Shape();chamferPath(shape,width,height,.32);const glassKey=`reference-glass:${width}:${height}`;
  if(!i.geometries.has(glassKey))i.geometries.set(glassKey,new THREE.ShapeGeometry(shape));
  const glass=i.part(g,i.geometries.get(glassKey),'glass',0,0,.10,1,1,1);glass.name='Observation glass / physical wall collision retained';glass.castShadow=false;
  const fittings=tagged(g,'Observation window fasteners',w.area);
  for(const side of[-1,1]) {
    i.bevel(fittings,'metal',side*(width/2+.13),0,.42,.13,height-.65,.10);i.box(fittings,'warm-light',side*(width/2+.13),0,.477,.042,height-.85,.023);
    for(let x=-width/2+.35;x<width/2-.15;x+=.62)i.cylinder(fittings,'dark',x,side*(height/2+.11),.44,.023,.020,[Math.PI/2,0,0]);
  }
  i.bevel(fittings,'pearl',0,-height/2-.15,.40,width+.23,.17,.39);i.box(fittings,'cyan',0,-height/2-.225,.49,width-.20,.024,.024);
  i.pipe(fittings,'metal',[-width/2+.25,-height/2-.37,.48],[width/2-.25,-height/2-.37,.48],.026);
}

function corridors(i) {
  for(const a of layout.areas.filter(a=>a.kind==='corridor')) {
    const g=tagged(i.rooms.get(a.id),'Pressure passage structural frames',a.id),alongX=!a.id.endsWith('-link');
    const length=alongX?a.w:a.d,width=alongX?a.d:a.w;
    for(let t=-length/2+.45;t<length/2-.2;t+=3.35) {
      const frame=new THREE.Group();frame.position.set(a.x+(alongX?t:0),0,a.z+(alongX?0:t));if(alongX)frame.rotation.y=Math.PI/2;g.add(frame);
      for(const side of[-1,1]){i.bevel(frame,'metal',side*(width/2-.11),1.64,0,.14,3.22,.15);i.bevel(frame,'pearl',side*(width/2-.18),3.33,0,.18,.68,.19,[0,0,side*.60]);i.box(frame,'cyan',side*(width/2-.20),.19,.09,.045,.055,.035);}
      i.bevel(frame,'metal',0,3.58,0,width-.6,.14,.18);
    }
    for(const side of[-1,1])for(let t=-length/2+.3;t<length/2-.3;t+=1.9) {
      const x=a.x+(alongX?t:side*(width/2-.5)),z=a.z+(alongX?side*(width/2-.5):t);
      i.box(g,'blue',x,.012,z,alongX?1.65:.08,.02,alongX?.08:1.65);i.box(g,'light',x,.028,z,alongX?.72:.035,.014,alongX?.035:.72);
    }
  }
  const main=i.rooms.get('corridor');
  for(const x of[-9,8]){i.label(main,'← КРИО   /   МОСТИК ↑   /   РЕАКТОР →',x,2.62,7.19,6,.28);i.label(main,'ЖИЛОЙ ←   /   ИССЛЕДОВАНИЯ   /   ГРУЗ →',x,2.62,10.81,6,.28,Math.PI);}
}

function planetTexture(i,type) {
  if(typeof document==='undefined')return null;
  const c=document.createElement('canvas');c.width=768;c.height=384;
  const ctx=c.getContext('2d'),img=ctx.createImageData(c.width,c.height);
  for(let y=0;y<c.height;y++)for(let x=0;x<c.width;x++) {
    const u=x/c.width*Math.PI*2,v=(y/c.height-.5)*Math.PI;
    let rgb;
    if(type==='jupiter') {
      const wave=.035*Math.sin(u*5+v*17)+.012*Math.sin(u*17-v*13),lat=v+wave;
      const band=Math.sin(lat*30)+.38*Math.sin(lat*71+Math.sin(u*9))+.11*Math.sin(u*91+lat*121);
      const t=Math.max(0,Math.min(1,(band+1.3)/2.6));rgb=[137+100*t,94+137*t,65+159*t];
      const spot=Math.pow((u-3.6)/.43,2)+Math.pow((v+.35)/.13,2);
      if(spot<1){const swirl=.5+.5*Math.sin(Math.sqrt(spot)*24+Math.atan2(v+.35,u-3.6)*2);rgb=[173+30*swirl,88+37*swirl,47+33*swirl];}
      const turbulence=Math.sin(u*67+lat*103+Math.sin(u*19+lat*27)*2)*3;rgb=rgb.map(c=>c+turbulence);
    } else {
      const seam=Math.min(Math.abs(Math.sin(u*13+Math.sin(v*9)*.8)),Math.abs(Math.sin(v*17+u*6+Math.sin(u*8)*.7)));
      const dust=Math.sin(u*93+Math.sin(v*77))*5;
      rgb=seam<.045?[115+dust,87+dust,67+dust]:[204+dust,211+dust,205+dust];
    }
    const shade=.30+.70*Math.pow(Math.max(0,Math.cos(u-2.4)*Math.cos(v)),.3),k=(y*c.width+x)*4;
    for(let n=0;n<3;n++)img.data[k+n]=Math.min(255,rgb[n]*shade);img.data[k+3]=255;
  }
  ctx.putImageData(img,0,0);const tex=new THREE.CanvasTexture(c);tex.colorSpace=THREE.SRGBColorSpace;i.textures.push(tex);return tex;
}

function orbitalSpace(i) {
  const g=i.spaceGroup=tagged(i.root,'Jovian orbital environment',[],'background');g.userData.background=true;
  i.ship.scene.background=new THREE.Color(0x050b14);
  const sphere=new THREE.SphereGeometry(1,48,32);i.geometries.set('reference-orbital-sphere',sphere);
  for(const [name,position,radius,color]of[['jupiter',[72,16,-120],72,0xdcc5ac],['europa',[40,-2,-56],9,0xcad1cc]]) {
    const map=planetTexture(i,name),mat=new THREE.MeshBasicMaterial({color:map?0xffffff:color,map});i.materials.set('orbital-'+name,mat);
    const body=i.part(g,sphere,mat,...position,radius,radius,radius);body.name=name==='jupiter'?'Jupiter / banded gas giant':'Europa / fractured ice';body.castShadow=false;body.receiveShadow=false;body.rotation.z=-.13;
    if(name==='jupiter')i.planet=body;
  }
  const positions=[],rnd=random(7041);
  for(let j=0;j<700;j++){const y=rnd()*2-1,angle=rnd()*Math.PI*2,r=Math.sqrt(1-y*y);positions.push(Math.cos(angle)*r*240,y*240,Math.sin(angle)*r*240);}
  const geometry=new THREE.BufferGeometry();geometry.setAttribute('position',new THREE.Float32BufferAttribute(positions,3));i.geometries.set('reference-stars',geometry);
  const material=new THREE.PointsMaterial({color:0xdde9f3,size:.19,sizeAttenuation:true});i.materials.set('orbital-stars',material);g.add(new THREE.Points(geometry,material));
}

export function buildReferenceArchitecture(i) {
  tuneMaterials(i);bulkheads(i);
  for(const a of layout.areas){const g=tagged(i.root,a.name,a.id,'room');i.rooms.set(a.id,g);shell(i,a,g);}
  for(const window of observationWindows)observationWindow(i,window);
  corridors(i);orbitalSpace(i);
}
