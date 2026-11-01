import * as THREE from 'three';
import {layout} from './DeckLayout.js';

// Built from the supplied room references. Footprints come from the live layout,
// never the older four-room diagram. Groups stay intact until spatial partition.
const TAU=Math.PI*2;
function geometry(i,key,create){key='reference:'+key;if(!i.geometries.has(key))i.geometries.set(key,create());return i.geometries.get(key);}
function material(i,key,options,system){
 if(!i.materials.has(key))i.materials.set(key,new THREE.MeshStandardMaterial(options));
 const m=i.mat(key);if(system)m.userData.system=system;return m;
}
function rounded(shape,w,h,r){
 shape.moveTo(-w/2+r,-h/2);shape.lineTo(w/2-r,-h/2);shape.quadraticCurveTo(w/2,-h/2,w/2,-h/2+r);
 shape.lineTo(w/2,h/2-r);shape.quadraticCurveTo(w/2,h/2,w/2-r,h/2);shape.lineTo(-w/2+r,h/2);
 shape.quadraticCurveTo(-w/2,h/2,-w/2,h/2-r);shape.lineTo(-w/2,-h/2+r);shape.quadraticCurveTo(-w/2,-h/2,-w/2+r,-h/2);shape.closePath();
}
function roundedGeometry(i,w,h,d,r=.12,border=0){
 return geometry(i,`rounded:${w}:${h}:${d}:${r}:${border}`,()=>{
  const s=new THREE.Shape();rounded(s,w,h,r);
  if(border){const hole=new THREE.Path();rounded(hole,w-border*2,h-border*2,Math.max(.02,r-border));s.holes.push(hole);}
  const geo=new THREE.ExtrudeGeometry(s,{depth:d,bevelEnabled:false,curveSegments:5});geo.translate(0,0,-d/2);return geo;
 });
}
function roundedPart(i,g,mat,x,y,z,w,h,d,r=.12,rotation,border=0){return i.part(g,roundedGeometry(i,w,h,d,r,border),mat,x,y,z,1,1,1,rotation);}
function ring(i,g,mat,x,y,z,outer,inner,h){
 const geo=geometry(i,`ring:${outer}:${inner}:${h}`,()=>new THREE.LatheGeometry([
  new THREE.Vector2(inner,-h/2),new THREE.Vector2(outer-.025,-h/2),new THREE.Vector2(outer,-h/2+.025),
  new THREE.Vector2(outer,h/2-.025),new THREE.Vector2(outer-.025,h/2),new THREE.Vector2(inner,h/2),new THREE.Vector2(inner,-h/2)
 ],48));return i.part(g,geo,mat,x,y,z,1,1,1);
}
function torus(i,g,mat,x,y,z,r,t,rotation){
 return i.part(g,geometry(i,`torus:${r}:${t}`,()=>new THREE.TorusGeometry(r,t,6,32)),mat,x,y,z,1,1,1,rotation);
}
function item(i,area,colliderID,name,rotation=0){
 const collider=layout.obstacles.find(o=>o.id===colliderID);if(!collider)throw new Error('Unknown equipment footprint '+colliderID);
 const g=new THREE.Group();g.name=name;g.position.set(collider.x,0,collider.z);g.rotation.y=rotation;
 Object.assign(g.userData,{area,interiorRole:'equipment',colliderID});i.rooms.get(area).add(g);return g;
}
function cosmetic(i,area,name){const g=new THREE.Group();g.name=name;Object.assign(g.userData,{area,interiorRole:'equipment',cosmeticOverhead:true});i.rooms.get(area).add(g);return g;}
function vent(i,g,x,y,z,w=.7,h=.2,rotation=0){
 const p=new THREE.Group();p.position.set(x,y,z);p.rotation.y=rotation;g.add(p);
 roundedPart(i,p,'metal',0,0,0,w,h,.035,.04);i.box(p,'rubber',0,0,.022,w-.055,h-.055,.018);
 for(let n=0;n<5;n++)i.box(p,'metal',0,-h*.32+n*h*.16,.04,w-.085,.012,.024,[.2,0,0]);
}
function handle(i,g,x,y,z,w=.35,rotation=0){
 const p=new THREE.Group();p.position.set(x,y,z);p.rotation.y=rotation;g.add(p);
 roundedPart(i,p,'dark',0,0,0,w+.1,.16,.028,.035);
 for(const side of [-1,1])i.pipe(p,'metal',[side*w/2,-.025,.025],[side*w/2,.025,.045],.018);
 i.pipe(p,'metal',[-w/2,.025,.045],[w/2,.025,.045],.025);
}
function statusStrip(i,g,x,y,z,w,system='cyan'){
 const mesh=i.box(g,system,x,y,z,w,.022,.025);mesh.name=system+' status strip';mesh.userData.system=system;return mesh;
}
function display(i,g,x,y,z,w,h,title,system,angle=-.12){
 const screen=i.screen(g,x,y,z,w,h,title,angle);screen.name=title+' recessed display';screen.userData.decorative=true;
 if(system)screen.userData.system=system;return screen;
}

function consoleKit(i,g,title,w=3){
 const width=w-.06;
 roundedPart(i,g,'metal',0,.075,0,width-.24,1.12,.15,.14,[-Math.PI/2,0,0]);
 for(const s of [-1,1]){
  i.bevel(g,'dark',s*(width/2-.42),.55,-.08,.34,.96,.74,[0,0,s*.1]);
  i.bevel(g,'pearl',s*(width/2-.28),.66,-.08,.22,.96,.87,[0,0,s*.09]);
  i.box(g,'blue',s*(width/2-.16),.65,-.07,.025,.55,.5);
  i.pipe(g,'rubber',[s*(width/2-.49),.22,-.4],[s*(width/2-.49),.98,-.4],.045);
 }
 i.bevel(g,'metal',0,.74,-.43,width-.55,.58,.22);
 vent(i,g,0,.72,-.557,width-.85,.28,Math.PI);
 roundedPart(i,g,'rubber',0,1.07,0,width,1.19,.085,.17,[-Math.PI/2,0,0]);
 roundedPart(i,g,'pearl',0,1.16,0,width,1.16,.14,.16,[-Math.PI/2,0,0]);
 roundedPart(i,g,'metal',0,1.244,.23,width-.22,.57,.027,.09,[-Math.PI/2,0,0]);
 roundedPart(i,g,'dark',0,1.261,.23,width-.28,.51,.024,.07,[-Math.PI/2,0,0]);
 display(i,g,0,1.76,-.29,1.4,.68,title);
 for(const s of [-1,1]){
  const screen=display(i,g,s*1.09,1.72,-.245,.43,.59,s<0?'CHANNEL / 07':'SYSTEM / LINK');screen.rotation.y=s*-.18;
  i.cylinder(g,'dark',s*1.14,1.3,.35,.072,.045);i.cylinder(g,'metal',s*1.14,1.34,.35,.045,.06);
  i.box(g,'cyan',s*1.14,1.376,.35,.012,.006,.04);
 }
 for(let row=0;row<3;row++)for(let col=0;col<12;col++)i.bevel(g,col===11?'blue':'metal',-.69+col*.115,1.29,.1+row*.105,.082,.026,.07);
 i.bevel(g,'metal',0,1.292,.46,.6,.028,.055);
 for(const s of [-1,1]){
  i.bevel(g,'pearl',s*(width/2-.12),1.3,.14,.13,.13,.8);
  for(const y of [.37,.91])i.cylinder(g,'metal',s*(width/2-.27),y,.37,.027,.025,[Math.PI/2,0,0]);
 }
 vent(i,g,0,.96,.547,1.12,.16);statusStrip(i,g,0,1.12,.586,.92);
 i.label(g,'H7 / FLIGHT CONTROL',0,.78,.56,.78,.1);
}
function chairKit(i,g){
 const base=geometry(i,'chair-base',()=>new THREE.CylinderGeometry(.39,.41,.12,8));i.part(g,base,'metal',0,.06,0,1,1,1);
 for(const s of [-1,1])for(const z of [-.28,.28])i.cylinder(g,'dark',s*.28,.135,z,.042,.025);
 i.cylinder(g,'rubber',0,.3,0,.12,.42);i.cylinder(g,'metal',0,.48,0,.18,.15);
 ring(i,g,'blue',0,.48,0,.19,.16,.055);
 roundedPart(i,g,'pearl',0,.64,0,.79,.78,.16,.13,[-Math.PI/2,0,0]);
 roundedPart(i,g,'fabric',0,.77,-.02,.66,.63,.15,.13,[-Math.PI/2,0,0]);
 for(const x of [-.22,0,.22])i.box(g,'fabricStitch',x,.847,-.02,.008,.006,.49);
 roundedPart(i,g,'pearl',0,1.28,.245,.75,.99,.14,.15,[-.12,0,0]);
 for(let n=0;n<3;n++)roundedPart(i,g,'fabric',0,.99+n*.25,.143+n*.03,.61,.23,.12,.065,[-.12,0,0]);
 roundedPart(i,g,'fabric',0,1.9,.3,.48,.24,.17,.08);
 i.bevel(g,'metal',0,1.25,.352,.12,.85,.045);
 for(const s of [-1,1]){
  i.bevel(g,'metal',s*.34,.94,.12,.055,.43,.09);
  roundedPart(i,g,'dark',s*.335,1.14,.035,.13,.62,.12,.05,[-Math.PI/2,0,0]);
  i.cylinder(g,'metal',s*.36,.74,.15,.06,.045,[0,0,Math.PI/2]);
  i.box(g,'blue',s*.3,1.55,.34,.07,.21,.016);
 }
 i.pipe(g,'rubber',[-.22,.87,-.29],[-.18,1.52,.13],.019);i.bevel(g,'metal',-.2,1.08,-.16,.065,.08,.04);
}
function bridge(i){
 for(let n=0;n<3;n++){
  consoleKit(i,item(i,'bridge','console-'+n,['Communications console','Flight command console','Trajectory console'][n]),['ARCHIVE / COMMS','ORBIT / FLIGHT','TRAJECTORY / CONTROL'][n]);
  chairKit(i,item(i,'bridge','chair-'+n,'Suspended pilot chair '+(n+1)));
 }
 const g=item(i,'bridge','nav-table','Mission planning table');
 roundedPart(i,g,'metal',0,.08,0,1.65,1.02,.16,.14,[-Math.PI/2,0,0]);
 i.bevel(g,'pearl',0,.58,0,1.4,.98,.8);vent(i,g,0,.55,.414,.95,.34);
 roundedPart(i,g,'rubber',0,1.08,0,3.12,1.51,.08,.17,[-Math.PI/2,0,0]);
 roundedPart(i,g,'pearl',0,1.17,0,3.16,1.55,.15,.2,[-Math.PI/2,0,0]);
 display(i,g,0,1.257,0,2.7,1.11,'MISSION / VECTOR',null,-Math.PI/2);
 for(const s of [-1,1])statusStrip(i,g,s*1.45,1.27,.57,.12);
}

function cabinet(i,g,w,d,title,medical=false){
 i.bevel(g,'metal',0,1.35,0,w,2.7,d);
 const count=medical?2:3,slot=w/count;
 for(let n=0;n<count;n++){
  const x=-w/2+slot*(n+.5);
  roundedPart(i,g,'white',x,1.38,d/2-.035,slot-.045,2.5,.055,.06);
  if(medical){
   roundedPart(i,g,'dark',x,1.73,d/2-.001,slot-.18,1.31,.03,.05);
   for(let row=0;row<3;row++){
    i.box(g,'metal',x,1.22+row*.37,d/2+.012,slot-.23,.025,.018);
    for(let k=0;k<3;k++){i.cylinder(g,k===1?'blue':'pearl',x+(k-1)*slot*.2,1.35+row*.37,d/2+.023,.06,.2);i.cylinder(g,'metal',x+(k-1)*slot*.2,1.46+row*.37,d/2+.023,.061,.035);}
   }
   roundedPart(i,g,'canopy',x,1.73,d/2+.085,slot-.19,1.29,.012,.05);
  }
  handle(i,g,x+slot*.23,.94,d/2-.015,.12);
  statusStrip(i,g,x,2.48,d/2+.008,slot-.19,medical?'mint':'blue');
  vent(i,g,x,.38,d/2+.004,slot-.2,.19);
  i.label(g,`${title} / 0${n+1}`,x,2.3,d/2+.012,slot-.24,.1);
 }
}
function cryo(i){
 const canopyGeo=geometry(i,'cryo-curved-canopy',()=>{
  const vertices=[],indices=[],nx=12,nz=12;
  for(let ix=0;ix<=nx;ix++)for(let iz=0;iz<=nz;iz++){
   const u=ix/nx*2-1,v=iz/nz*2-1;vertices.push(u*.94,.34+.12*(1-v*v)+.025*(1-u*u),v*.5);
  }
  for(let x=0;x<nx;x++)for(let z=0;z<nz;z++){const a=x*(nz+1)+z,b=a+nz+1;indices.push(a,b,a+1,b,b+1,a+1);}
  const geo=new THREE.BufferGeometry();geo.setAttribute('position',new THREE.Float32BufferAttribute(vertices,3));geo.setIndex(indices);geo.computeVertexNormals();return geo;
 });
 for(let n=0;n<3;n++){
  const g=item(i,'cryo','pod-'+n,'Inclined cryogenic capsule '+(n+1));
  i.bevel(g,'dark',0,.09,0,2.32,.18,1.55);i.bevel(g,'white',0,.35,0,2.32,.48,1.52);
  for(const s of [-1,1]){i.bevel(g,'metal',s*.93,.34,0,.25,.52,1.49);statusStrip(i,g,s*.88,.17,.77,.18,'mint');}
  vent(i,g,1.14,.36,0,.84,.27,Math.PI/2);
  const cradle=new THREE.Group();cradle.name='Padded inclined bed';cradle.position.set(-.15,1.16,0);cradle.rotation.z=-.82;g.add(cradle);
  roundedPart(i,cradle,'metal',0,-.04,0,2.02,1.3,.18,.24,[-Math.PI/2,0,0]);
  roundedPart(i,cradle,'rubber',0,.065,0,1.91,1.17,.08,.2,[-Math.PI/2,0,0]);
  for(let k=0;k<4;k++)roundedPart(i,cradle,'medicalPad',-.69+k*.44,.155,0,.41,.98,.13,.09,[-Math.PI/2,0,0]);
  roundedPart(i,cradle,'fabric',-.73,.23,0,.34,.62,.13,.1,[-Math.PI/2,0,0]);
  for(const s of [-1,1]){
   roundedPart(i,cradle,'white',0,.22,s*.61,2.04,.12,.18,.05);
   i.box(cradle,'mint',0,.29,s*.585,1.72,.018,.025);
   i.box(cradle,'rubber',-.2,.239,s*.32,.06,.018,.46);
   i.bevel(cradle,'metal',-.2,.258,s*.11,.085,.03,.07);
  }
  const lid=new THREE.Group();lid.name='Canopy lid with hinge';lid.userData.articulated=true;cradle.add(lid);
  roundedPart(i,lid,'pearl',0,.35,0,2.08,1.2,.08,.22,[-Math.PI/2,0,0],.085);
  roundedPart(i,lid,'rubber',0,.356,0,1.94,1.06,.025,.18,[-Math.PI/2,0,0],.025);
  i.part(lid,canopyGeo,'canopy',0,0,0,1,1,1).name='Convex glass canopy';
  for(const s of [-1,1]){i.cylinder(cradle,'metal',-.92,.23,s*.64,.07,.18,[Math.PI/2,0,0]);i.pipe(cradle,'metal',[-.88,.04,s*.65],[-.49,.34,s*.65],.018);}
  const terminal=new THREE.Group();terminal.name='Capsule bedside terminal';terminal.position.set(.83,1.31,-.47);terminal.rotation.y=Math.PI/2;g.add(terminal);
  i.bevel(terminal,'pearl',0,0,0,.43,.73,.15);display(i,terminal,0,.04,.045,.27,.48,'VITALS / 0'+(n+1),'cryo',0);
  i.box(terminal,'blue',0,-.27,.09,.19,.04,.015);
 }
 const cabinetGroup=item(i,'cryo','cryo-cabinet','Medical storage and resuscitation cabinet');
 // Recess the front so bottles, handles and glazing stay inside the old 0.8m footprint.
 const inner=new THREE.Group();inner.position.z=-.105;cabinetGroup.add(inner);cabinet(i,inner,2.65,.58,'MED',true);
}

function engineering(i){
 const g=item(i,'reactor','core','Layered fusion reactor and coolant manifold');
 i.cylinder(g,'dark',0,.09,0,1.77,.18);ring(i,g,'metal',0,.22,0,1.73,1.28,.16);
 i.cylinder(g,'metal',0,.57,0,1.38,.7);ring(i,g,'pearl',0,.34,0,1.55,1.2,.17);ring(i,g,'pearl',0,.92,0,1.55,1.2,.18);
 i.cylinder(g,'dark',0,2.94,0,1.35,.51);ring(i,g,'pearl',0,2.72,0,1.54,1.2,.16);ring(i,g,'pearl',0,3.19,0,1.52,1.12,.15);
 const glass=geometry(i,'reactor-glass',()=>new THREE.CylinderGeometry(1.09,1.09,1.61,48,1,true));i.part(g,glass,'canopy',0,1.81,0,1,1,1).name='Containment glass';
 i.cylinder(g,'reactor',0,1.78,0,.24,2.25).userData.system='reactor';
 for(const y of [.87,1.1,1.74,2.4,2.62]){ring(i,g,'metal',0,y,0,.78,.56,.085);ring(i,g,'reactor',0,y+.05,0,.59,.52,.05);}
 for(let n=0;n<8;n++){
  const angle=n*TAU/8,s=Math.sin(angle),c=Math.cos(angle);
  i.cylinder(g,'metal',s*1.25,1.84,c*1.25,.055,2.78);
  i.pipe(g,'cooling',[s*.72,.96,c*.72],[s*.72,2.62,c*.72],.044).userData.system='cooling';
  const panel=new THREE.Group();panel.position.set(s*1.37,0,c*1.37);panel.rotation.y=angle;g.add(panel);
  for(const [y,h] of [[.62,.44],[2.95,.27]]){
   roundedPart(i,panel,'white',0,y,0,.9,h,.13,.055);i.box(panel,'blue',0,y-h/2+.055,.071,.79,.045,.012);
   for(const x of [-.36,.36])i.cylinder(panel,'metal',x,y,.08,.023,.015,[Math.PI/2,0,0]);
  }
  i.box(panel,'dark',0,1.83,-.03,.12,1.62,.12);statusStrip(i,panel,0,2.51,.04,.1,'amber');
 }
 for(const y of [1.09,2.51]){ring(i,g,'metal',0,y,0,1.58,1.29,.1);ring(i,g,'pearl',0,y+.07,0,1.6,1.42,.095);}
 display(i,g,0,1.52,1.52,.71,.41,'CORE / COOLANT','reactor',-.08);
 for(const x of [-.72,.72]){torus(i,g,'blue',x,.6,1.25,.12,.018);i.pipe(g,'metal',[x,.6,1.21],[x,.6,1.35],.045);}
 const rack=item(i,'reactor','engine-rack','Cooling pumps and engineer diagnostic post');
 for(const z of [-1.04,0,1.04]){
  i.cylinder(rack,'metal',.04,1.43,z,.3,2.5);
  for(const y of [.26,.47,2.39,2.65])ring(i,rack,'dark',.04,y,z,.34,.28,.065);
  i.cylinder(rack,'cooling',.04,1.4,z,.309,.15);
  for(const y of [.45,2.36])i.bevel(rack,'pearl',.12,y,z,.58,.13,.7);
  i.pipe(rack,'metal',[.04,2.68,z],[.04,2.92,z],.07);
 }
 i.pipe(rack,'metal',[.04,2.92,-1.1],[.04,2.92,1.1],.07);
 const post=new THREE.Group();post.position.set(-.36,1.65,0);post.rotation.y=-Math.PI/2;rack.add(post);
 display(i,post,0,0,0,1.02,.58,'ENGINEER / DIAGNOSTICS','cooling',0);
 const pipes=cosmetic(i,'reactor','Overhead supply and return loops');
 for(const [z,y] of [[-2.7,3.39],[-3.25,3.57]]){
  const points=[[17.3,y,.1],[17.3,y,z],[21.54,y,z],[21.54,2.92,z]];
  for(let n=0;n<points.length-1;n++)i.pipe(pipes,'metal',points[n],points[n+1],.085);
  for(const x of [18,19.4,20.8]){i.box(pipes,'dark',x,y+.13,z,.1,.1,.25);i.cylinder(pipes,'blue',x,y,z,.105,.06,[0,0,Math.PI/2]);}
 }
}

function leafGeometry(i,type){
 return geometry(i,'leaf:'+type,()=>{
  const vertices=[],indices=[],rows=9;
  for(let n=0;n<=rows;n++){
   const t=n/rows,spread=Math.pow(Math.sin(Math.PI*t),type==='blade'?.8:.55)*(type==='ruffle'?1+.17*Math.sin(t*Math.PI*8):1);
   for(const side of [-1,0,1])vertices.push(side*spread*(type==='blade'?.075:.17),.085*Math.sin(Math.PI*t)+(side===0?.028:0),t*.45);
  }
  for(let n=0;n<rows;n++)for(let j=0;j<2;j++){const a=n*3+j;indices.push(a,a+3,a+1,a+1,a+3,a+4);}
  const geo=new THREE.BufferGeometry();geo.setAttribute('position',new THREE.Float32BufferAttribute(vertices,3));geo.setIndex(indices);geo.computeVertexNormals();return geo;
 });
}
function plant(i,g,x,z,index){
 const p=new THREE.Group();p.name=['Leafy herb','Broadleaf vegetable','Climbing seedling'][index%3];p.position.set(x,1.15,z);p.userData.system='plant';g.add(p);
 i.cylinder(p,'rubber',0,.015,0,.13,.065);i.cylinder(p,'soil',0,.05,0,.108,.025);
 const tall=index%3===2,height=tall?.79:.31;
 i.pipe(p,'green',[0,.07,0],[0,height,0],.013);
 for(let n=0;n<(tall?6:7);n++){
  const angle=n*2.399+index*.71,y=tall?.17+n*.09:.1+n*.018;
  const group=new THREE.Group();group.position.y=y;group.rotation.y=angle;group.rotation.x=-(tall?.15:.26+n*.065);p.add(group);
  const scale=tall?.6+(n%2)*.12:.65+(n%3)*.075;
  const leaf=i.part(group,leafGeometry(i,index%3===0?'ruffle':index%3===1?'broad':'blade'),n===2&&index%4===0?'leafBronze':'plant',0,0,0,scale,scale,scale);
  leaf.name='Veined leaf';leaf.material.side=THREE.DoubleSide;
  i.pipe(group,'leafVein',[0,.025,0],[0,.068*scale,.35*scale],.004);
 }
}
function bio(i){
 for(let n=0;n<3;n++){
  const g=item(i,'bio','grow-'+n,['Herb hydroponic bench','Vegetable hydroponic bench','Propagation and sample station'][n]);
  i.bevel(g,'dark',0,.065,0,1.56,.13,4.96);i.bevel(g,'pearl',0,.57,0,1.53,.94,4.9);
  for(const s of [-1,1]){
   for(let bay=0;bay<3;bay++){
    i.bevel(g,'white',s*.76,.6,-1.58+bay*1.58,.026,.68,1.49);
    i.box(g,'blue',s*.778,.3,-1.58+bay*1.58,.015,.055,1.33);
   }
   i.pipe(g,'metal',[s*.68,1.17,-2.25],[s*.68,1.17,2.25],.035);
   for(const z of [-2.33,2.33]){i.box(g,'metal',s*.7,1.56,z,.045,3.12,.045);i.box(g,'dark',s*.7,1.15,z,.11,.1,.11);}
  }
  roundedPart(i,g,'metal',0,1.08,0,1.56,4.95,.09,.12,[-Math.PI/2,0,0]);
  for(let row=0;row<(n===2?5:7);row++){
   const z=-1.98+row*.58;
   roundedPart(i,g,'rubber',0,1.143,z,1.26,.51,.055,.065,[-Math.PI/2,0,0]);
   for(const s of [-1,1]){plant(i,g,s*.285,z,n*17+row*2+(s===1?1:0));i.pipe(g,'blue',[s*.66,1.2,z],[s*.34,1.2,z],.012);}
  }
  i.pipe(g,'metal',[-.68,1.17,-2.25],[.68,1.17,-2.25],.035);
  i.pipe(g,'metal',[0,.3,-2.25],[0,1.17,-2.25],.04);
  handle(i,g,0,.7,2.42,.55);vent(i,g,0,.36,2.44,.9,.2);
  statusStrip(i,g,0,.96,2.455,.7,'grow');
  roundedPart(i,g,'pearl',0,2.92,0,1.47,4.83,.11,.1,[-Math.PI/2,0,0]);
  for(const x of [-.49,0,.49]){i.box(g,'dark',x,2.855,0,.25,.03,4.59);const light=i.box(g,'grow',x,2.832,0,.15,.025,4.47);light.name='Horticultural LED';light.userData.system='grow';}
  if(n===2){
   roundedPart(i,g,'white',0,1.17,1.7,1.3,1.21,.08,.06,[-Math.PI/2,0,0]);
   display(i,g,0,1.58,1.73,.61,.37,'BIO / SAMPLES','grow');
   for(let tube=0;tube<4;tube++){i.cylinder(g,'metal',-.38+tube*.13,1.29,2.08,.045,.17);i.cylinder(g,'mint',-.38+tube*.13,1.39,2.08,.042,.04);}
  }
 }
}

function habitat(i){
 for(let n=0;n<2;n++){
  const g=item(i,'habitat','bunk-'+n,'Two-level crew sleeping module '+(n+1));
  for(const x of [-1.27,1.27])for(const z of [-.92,.92])i.bevel(g,'metal',x,1.56,z,.08,3.12,.08);
  for(const y of [.55,1.95]){
   roundedPart(i,g,'pearl',0,y,0,2.65,1.94,.2,.1,[-Math.PI/2,0,0]);
   roundedPart(i,g,'fabric',0,y+.18,0,2.4,1.68,.17,.1,[-Math.PI/2,0,0]);
   roundedPart(i,g,'medicalPad',-.84,y+.34,0,.48,1.3,.2,.1,[-Math.PI/2,0,0]);
   i.bevel(g,'linen',.33,y+.285,0,1.22,.055,1.54);for(const z of [-.62,.62])i.box(g,'fabricStitch',.33,y+.316,z,1.15,.008,.012);
   for(const z of [-.9,.9]){i.pipe(g,'metal',[-.98,y+.4,z],[.58,y+.4,z],.025);i.pipe(g,'metal',[.58,y+.12,z],[.58,y+.4,z],.025);}
   i.box(g,'dark',-1.16,y+.6,0,.075,.34,.55);i.box(g,'cyan',-1.111,y+.6,0,.015,.17,.37);
   i.box(g,'light',-1.2,y+.88,0,.06,.035,.8);
  }
  for(let step=0;step<5;step++)i.pipe(g,'metal',[1.27,.3+step*.39,-.48],[1.27,.3+step*.39,.48],.027);
  for(const z of [-.52,.52])i.pipe(g,'metal',[1.27,.2,z],[1.27,2.18,z],.026);
  roundedPart(i,g,'pearl',0,3.16,0,2.65,1.94,.1,.1,[-Math.PI/2,0,0]);
 }
 const lockers=item(i,'habitat','locker','Personal equipment lockers',Math.PI/2);cabinet(i,lockers,2.42,.68,'CREW');
 const sofa=item(i,'habitat','sofa','Crew lounge settee');
 roundedPart(i,sofa,'metal',0,.09,0,2.68,.91,.16,.1,[-Math.PI/2,0,0]);i.bevel(sofa,'pearl',0,.43,0,2.72,.6,.88);
 for(const x of [-.85,0,.85]){roundedPart(i,sofa,'fabric',x,.79,-.035,.79,.72,.18,.11,[-Math.PI/2,0,0]);roundedPart(i,sofa,'fabric',x,1.16,.32,.79,.65,.18,.1,[-.08,0,0]);}
 for(const x of [-1.3,1.3])roundedPart(i,sofa,'pearl',x,.94,0,.14,.88,.27,.06,[-Math.PI/2,0,0]);
 i.bevel(sofa,'metal',0,.55,-.462,.52,.26,.04);handle(i,sofa,0,.55,-.425,.3,Math.PI);
}
function research(i){
 const g=item(i,'research','lab-bed','Medical analysis bed with scanning gantry');
 roundedPart(i,g,'metal',0,.08,0,1.62,2.73,.16,.12,[-Math.PI/2,0,0]);i.bevel(g,'pearl',0,.55,0,1.35,.95,2.5);
 vent(i,g,0,.48,1.265,.94,.37);
 roundedPart(i,g,'pearl',0,1.08,0,2.12,2.93,.18,.2,[-Math.PI/2,0,0]);
 roundedPart(i,g,'medicalPad',0,1.22,.12,1.58,2.38,.15,.17,[-Math.PI/2,0,0]);
 roundedPart(i,g,'fabric',0,1.37,.91,.76,.52,.18,.09,[-Math.PI/2,0,0]);
 roundedPart(i,g,'pearl',0,1.55,-.74,2.08,1.98,.2,.36,undefined,.17);
 roundedPart(i,g,'metal',0,1.55,-.624,1.76,1.66,.04,.25,undefined,.035);
 i.box(g,'mint',0,2.35,-.58,1.3,.024,.05);
 display(i,g,.56,1.57,-1.15,.65,.42,'SCAN / ANALYSIS','cryo',0);
 const desk=item(i,'research','lab-desk','Research workstation and spectrometer',Math.PI);
 i.bevel(desk,'metal',0,.06,0,3.9,.12,1.2);
 for(const x of [-1.45,1.45]){i.bevel(desk,'pearl',x,.55,0,.8,.98,1.15);for(let row=0;row<3;row++){i.bevel(desk,'white',x,.3+row*.25,.586,.69,.22,.025);handle(i,desk,x,.3+row*.25,.571,.3);}}
 roundedPart(i,desk,'pearl',0,1.13,0,3.94,1.24,.15,.12,[-Math.PI/2,0,0]);
 display(i,desk,.8,1.71,-.26,1.35,.66,'RESEARCH / SPECTRUM');
 i.bevel(desk,'dark',.75,1.23,.32,1.35,.04,.38);for(let k=0;k<10;k++)i.box(desk,'metal',.2+k*.115,1.26,.31,.075,.018,.18);
 roundedPart(i,desk,'white',-1.05,1.5,0,1.01,.62,.77,.11);
 roundedPart(i,desk,'dark',-1.05,1.57,.4,.65,.32,.025,.06);i.cylinder(desk,'cyan',-1.05,1.57,.424,.1,.015,[Math.PI/2,0,0]);
 i.label(desk,'SPECTROMETER',-1.05,1.31,.401,.7,.075);
 const storage=item(i,'research','lab-cabinet','Sample and diagnostic storage');const inner=new THREE.Group();inner.position.z=-.08;storage.add(inner);cabinet(i,inner,2.95,.52,'LAB',true);
}
function cargoCrate(i,g,y,w=2.38,d=1.88){
 roundedPart(i,g,'dark',0,y+.08,0,w,d,.16,.12,[-Math.PI/2,0,0]);i.bevel(g,'pearl',0,y+.67,0,w-.04,1.12,d-.04);
 roundedPart(i,g,'rubber',0,y+1.24,0,w,d,.055,.12,[-Math.PI/2,0,0]);roundedPart(i,g,'white',0,y+1.32,0,w,d,.12,.12,[-Math.PI/2,0,0]);
 for(const sx of [-1,1])for(const sz of [-1,1])i.bevel(g,'metal',sx*(w/2-.08),y+.71,sz*(d/2-.08),.15,1.33,.15);
 for(const side of [-1,1]){
  handle(i,g,0,y+.81,side*(d/2-.05),.46,side<0?Math.PI:0);
  handle(i,g,side*(w/2-.05),y+.81,0,.46,side*Math.PI/2);
  for(const x of [-.7,.7]){
   i.bevel(g,'dark',x,y+1.1,side*(d/2-.03),.17,.29,.045);i.bevel(g,'metal',x,y+1.1,side*(d/2-.002),.1,.15,.035);
   i.box(g,'fabric',x,y+.68,side*(d/2+.002),.067,1.27,.025);
  }
  i.box(g,'blue',0,y+.34,side*(d/2-.005),w-.35,.06,.02);
 }
 i.label(g,'H7 / SECURED CARGO',0,y+.55,d/2+.008,.84,.12);
}
function cargo(i){
 for(let n=0;n<4;n++){
  const g=item(i,'cargo','cargo-'+n,'Locked cargo cradle '+(n+1));
  cargoCrate(i,g,0);if(n%2)cargoCrate(i,g,1.4,2.15,1.72);
  for(const x of [-.83,.83])i.box(g,'blue',x,1.39,0,.08,.022,1.82);
 }
 const rail=cosmetic(i,'cargo','Overhead cargo handling rails');
 for(const z of [12.5,19.5]){
  i.bevel(rail,'metal',15.5,3.36,z,12.8,.15,.18);i.box(rail,'amber',15.5,3.267,z,12.5,.022,.025);
  for(const x of [11,20]){i.bevel(rail,'dark',x,3.2,z,.43,.19,.34);i.pipe(rail,'metal',[x,3.1,z],[x,2.84,z],.035);torus(i,rail,'metal',x,2.81,z,.09,.018);}
 }
}

export function buildReferenceEquipment(i){
 material(i,'medicalPad',{color:0x91a0a9,roughness:.94,metalness:0});
 material(i,'fabricStitch',{color:0x748591,roughness:1,metalness:0});
 material(i,'linen',{color:0x72929e,roughness:1,metalness:0});
 material(i,'soil',{color:0x293c30,roughness:1,metalness:0});
 material(i,'leafVein',{color:0x97ae75,roughness:.9,metalness:0},'plant');
 material(i,'leafBronze',{color:0x7d655a,roughness:.83,metalness:0,side:THREE.DoubleSide},'plant');
 if(!i.materials.has('canopy'))i.materials.set('canopy',new THREE.MeshPhysicalMaterial({color:0xaacbdc,transparent:true,opacity:.22,roughness:.12,metalness:.06,depthWrite:false,side:THREE.DoubleSide}));
 for(const name of ['reactor','cooling','plant','grow'])i.mat(name).userData.system=name;
 bridge(i);cryo(i);engineering(i);bio(i);habitat(i);research(i);cargo(i);
 return [...i.rooms.values()].flatMap(room=>room.children.filter(child=>child.userData.interiorRole==='equipment'));
}
