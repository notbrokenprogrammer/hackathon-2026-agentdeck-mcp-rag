import * as THREE from 'three';

// Individually sculpted game portraits, based on the user-supplied named folders.
// These are full 3D surfaces, not face photographs placed on planes.
export const PORTRAITS = {
 savinykh: {width:.142,depth:.127,jaw:.94,chin:.024,cheek:.010,brow:.004,nose:.047,tip:.020,eyes:.057,eye:0x66796f,skin:0xd4ad91,hair:0x4b3328,hairline:.095,recession:.020,part:-1,mouth:.056,smile:.003,reference:[5,6],era:'СССР',name:'В. САВИНЫХ'},
 dzhanibekov: {width:.145,depth:.130,jaw:1.06,chin:.030,cheek:.004,brow:.009,nose:.057,tip:.022,eyes:.058,eye:0x66766a,skin:0xcba187,hair:0x584338,hairline:.119,recession:.045,part:1,mouth:.052,smile:0,reference:[1,2],era:'СССР',name:'В. ДЖАНИБЕКОВ'},
 krikalev: {width:.130,depth:.124,jaw:.88,chin:.036,cheek:.002,brow:.010,nose:.054,tip:.018,eyes:.053,eye:0x5e6354,skin:0xc9a88e,hair:0x302d28,hairline:.107,recession:.007,part:0,mouth:.046,smile:.001,reference:[3,4],era:'РОССИЯ',name:'С. КРИКАЛЁВ'},
 ryazansky: {width:.148,depth:.128,jaw:.97,chin:.019,cheek:.014,brow:.005,nose:.041,tip:.023,eyes:.057,eye:0x6b8178,skin:0xd9b296,hair:0x54483b,hairline:.155,recession:.028,part:0,mouth:.058,smile:.008,reference:[7],era:'РОССИЯ',name:'С. РЯЗАНСКИЙ'}
};

const v = a => new THREE.Vector3(...a);
const up = new THREE.Vector3(0,1,0);

export class CosmonautModels {
 constructor(){this.geometries=new Map();this.materials=new Map();this.textures=[];this.disposed=false;}
 geometry(key,make){if(!this.geometries.has(key))this.geometries.set(key,make());return this.geometries.get(key);}
 mat(key,color,roughness=.7,metalness=0){if(!this.materials.has(key))this.materials.set(key,new THREE.MeshStandardMaterial({color,roughness,metalness}));return this.materials.get(key);}
 mesh(parent,geometry,material,position=[0,0,0],scale=[1,1,1],name=''){
  const mesh=new THREE.Mesh(geometry,material);mesh.position.set(...position);mesh.scale.set(...scale);mesh.name=name;mesh.castShadow=true;mesh.receiveShadow=true;parent.add(mesh);return mesh;
 }
 ellipsoid(parent,material,position,scale,name=''){
  const r=Math.max(...scale),segments=r<.014?[8,6]:r<.045?[12,8]:[20,12];
  return this.mesh(parent,this.geometry('sphere:'+segments.join(':'),()=>new THREE.SphereGeometry(1,...segments)),material,position,scale,name);
 }
 box(parent,material,position,scale){
  const geometry=this.geometry('rounded-box',()=>{
   const s=new THREE.Shape(),r=.09;s.moveTo(-.5+r,-.5);s.lineTo(.5-r,-.5);s.quadraticCurveTo(.5,-.5,.5,-.5+r);s.lineTo(.5,.5-r);s.quadraticCurveTo(.5,.5,.5-r,.5);s.lineTo(-.5+r,.5);s.quadraticCurveTo(-.5,.5,-.5,.5-r);s.lineTo(-.5,-.5+r);s.quadraticCurveTo(-.5,-.5,-.5+r,-.5);
   const g=new THREE.ExtrudeGeometry(s,{depth:.88,bevelEnabled:true,bevelSize:.018,bevelThickness:.06,bevelSegments:2,curveSegments:3});g.translate(0,0,-.44);return g;
  });return this.mesh(parent,geometry,material,position,scale);
 }
 tube(parent,material,points,r=.006,key=''){
  const make=()=>new THREE.TubeGeometry(new THREE.CatmullRomCurve3(points.map(v)),Math.max(8,points.length*4),r,6,false);
  return this.mesh(parent,key?this.geometry(key,make):this.geometry('tube:'+r+':'+points.flat().join(','),make),material);
 }
 rod(parent,material,a,b,r1,r2=r1){
  const direction=v(b).sub(v(a)),length=direction.length();
  const geometry=this.geometry(`rod:${r1}:${r2}:${length.toFixed(5)}`,()=>{
   if(r1>.065&&length>.15){
    const points=[0,.12,.42,.77,1].map((t,i)=>new THREE.Vector2((r1+(r2-r1)*t)*[.94,1.02,1.015,.99,.94][i],(t-.5)*length));
    const geometry=new THREE.LatheGeometry(points,20);geometry.computeVertexNormals();return geometry;
   }
   return new THREE.CylinderGeometry(r2,r1,length,r1<.025?12:24,1);
  });
  const mesh=this.mesh(parent,geometry,material,v(a).add(v(b)).multiplyScalar(.5).toArray());mesh.quaternion.setFromUnitVectors(up,direction.normalize());return mesh;
 }
 torus(parent,material,position,radius,tube,rotation=[Math.PI/2,0,0],scale=[1,1,1]){
  const geometry=this.geometry(`ring:${radius}:${tube}`,()=>new THREE.TorusGeometry(radius,tube,radius<.1?5:8,radius<.1?24:40));
  const mesh=this.mesh(parent,geometry,material,position,scale);mesh.rotation.set(...rotation);return mesh;
 }
 group(parent,name,position=[0,0,0]){const g=new THREE.Group();g.name=name;g.position.set(...position);g.userData.rig=name;parent.add(g);return g;}
 patch(parent,id,text,position,size,kind='name'){
  const backing=this.mat('patch-backing',0x284c76,.92);
  this.box(parent,backing,position,[size[0]+.012,size[1]+.010,.012]);
  if(typeof document==='undefined')return;
  const key=`patch:${id}:${text}:${kind}`;let material=this.materials.get(key);
  if(!material){
   const canvas=document.createElement('canvas');canvas.width=512;canvas.height=Math.round(512*size[1]/size[0]);const c=canvas.getContext('2d');
   c.fillStyle=kind==='mission'?'#123254':'#e4e7dd';c.fillRect(0,0,canvas.width,canvas.height);c.strokeStyle=kind==='mission'?'#cbd5bd':'#2a5582';c.lineWidth=9;c.strokeRect(6,6,500,canvas.height-12);
   if(kind==='flag'){
    const top=canvas.height*.38;c.fillStyle='#e6c982';c.font=`bold ${top*.65}px sans-serif`;c.textAlign='center';c.fillStyle='#234672';c.fillText(text,256,top*.8);
    for(const [i,color]of(text==='СССР'?['#b53634','#b53634','#b53634']:['#e7e9e9','#24509b','#b8383b']).entries()){c.fillStyle=color;c.fillRect(15,top+i*(canvas.height-top-15)/3,482,(canvas.height-top-15)/3+1);}
   }else if(kind==='mission'){
    c.strokeStyle='#cf7052';c.lineWidth=10;c.beginPath();c.ellipse(256,canvas.height*.48,175,canvas.height*.28,-.4,0,Math.PI*2);c.stroke();c.strokeStyle='#9dbacb';c.lineWidth=5;c.beginPath();c.moveTo(120,canvas.height*.78);c.lineTo(256,canvas.height*.15);c.lineTo(390,canvas.height*.78);c.stroke();c.fillStyle='#dbe5df';c.font=`bold ${canvas.height*.13}px sans-serif`;c.textAlign='center';c.fillText('ГОРИЗОНТ • 7',256,canvas.height*.9);
   }else{c.fillStyle='#274c70';c.font=`bold ${canvas.height*.42}px sans-serif`;c.textAlign='center';c.textBaseline='middle';c.fillText(text,256,canvas.height/2,460);}
   const texture=new THREE.CanvasTexture(canvas);texture.colorSpace=THREE.SRGBColorSpace;texture.anisotropy=4;this.textures.push(texture);material=new THREE.MeshStandardMaterial({map:texture,roughness:.92});this.materials.set(key,material);
  }
  const front=this.mesh(parent,this.geometry('patch-plane',()=>new THREE.PlaneGeometry(1,1)),material,[position[0],position[1],position[2]-.007],[size[0],size[1],1]);front.rotation.y=Math.PI;
 }
 head(parent,p,id){
  const skin=this.mat('skin:'+id,p.skin,.9),lip=this.mat('lips:'+id,0xb28b79,.92),crease=this.mat('skin-crease:'+id,new THREE.Color(p.skin).multiplyScalar(.88),.95),hair=this.mat('hair:'+id,p.hair,.99),eyeWhite=this.mat('eye-white',0xc9c5b9,.65),iris=this.mat('iris:'+id,p.eye,.6),pupil=this.mat('pupil',0x17201e,.5);
  const geometry=this.geometry('head:'+id,()=>{
   const points=[],uvs=[],indices=[],rows=28,cols=48;
   for(let row=0;row<=rows;row++){
    const t=row/rows,y=-.177+t*.369,yn=(y-.012)/.19,ring=Math.sqrt(Math.max(.0001,1-yn*yn));
    const jaw=1+(p.jaw-1)*Math.exp(-Math.pow((y+.105)/.055,2));
    for(let col=0;col<=cols;col++){
     const a=col/cols*Math.PI*2,s=Math.sin(a),front=Math.max(0,Math.cos(a));let x=p.width*ring*s*jaw,z=-p.depth*ring*Math.cos(a);
     // Forehead, cheekbone, recessed eye socket and chin are part of the head mesh.
     z-=front**7*(p.cheek*Math.exp(-Math.pow((y+.033)/.037,2))+p.brow*Math.exp(-Math.pow((y-.068)/.026,2))+p.chin*Math.exp(-Math.pow((y+.132)/.027,2)));
     z+=.012*front**7*Math.exp(-Math.pow((y-.025)/.023,2))*Math.exp(-Math.pow((Math.abs(x)-p.eyes)/.027,2));
     z-=.010*front**12*Math.exp(-Math.pow((y+.075)/.045,2));
     points.push(x,y,z);uvs.push(col/cols,t);
     if(row<rows&&col<cols){const a=row*(cols+1)+col,b=a+cols+1;indices.push(a,b,a+1,a+1,b,b+1);}
    }
   }
   const g=new THREE.BufferGeometry();g.setAttribute('position',new THREE.Float32BufferAttribute(points,3));g.setAttribute('uv',new THREE.Float32BufferAttribute(uvs,2));g.setIndex(indices);g.computeVertexNormals();return g;
  });
  const face=this.mesh(parent,geometry,skin,[0,0,0],[1,1,1],'sculpted-face');face.userData.keepSurface=true;
  // Separate concha, helix and tragus give the ears depth in side and rear views.
  for(const side of [-1,1]){
   this.ellipsoid(parent,skin,[side*(p.width-.003),-.010,.006],[.028,.046,.026]);
   this.ellipsoid(parent,crease,[side*(p.width+.011),-.008,-.016],[.012,.026,.009]);
   this.tube(parent,skin,[[side*(p.width+.009),-.045,-.009],[side*(p.width+.025),-.018,-.017],[side*(p.width+.018),.029,-.006],[side*(p.width+.001),.031,-.006]],.005);
   this.ellipsoid(parent,skin,[side*(p.width+.002),-.008,-.022],[.008,.015,.007]);
   const ex=side*p.eyes,ez=-p.depth+.006;
   this.ellipsoid(parent,eyeWhite,[ex,.028,ez],[.022,.0085,.008]);
   this.ellipsoid(parent,iris,[ex,.028,ez-.007],[.007,.007,.0025]);
   this.ellipsoid(parent,pupil,[ex,.028,ez-.0095],[.0034,.0038,.0015]);
   this.ellipsoid(parent,this.mat('eye-glint',0xf0f0df,.2),[ex-.002,.031,ez-.0135],[.0014,.0016,.001]);
   for(const upper of [true,false])this.tube(parent,skin,[[ex-.025,.027,ez+.001],[ex-.013,upper?.038:.020,ez-.007],[ex,.028+(upper?.010:-.009),ez-.010],[ex+.014,upper?.036:.019,ez-.005],[ex+.025,.026,ez+.003]],upper?.0032:.0024);
   this.tube(parent,hair,[[ex-.027,.061,ez+.006],[ex-.009,.066,ez-.002],[ex+.011,.062,ez-.001],[ex+.026,.056,ez+.009]],id==='krikalev'?.005:.004);
   this.tube(parent,crease,[[side*.032,-.049,-.128],[side*.046,-.065,-.122],[side*.057,-.085,-.110]],.0013);
  }
  // A tapered bridge, tip, alae and recessed nostrils; no flat portrait cards.
  this.rod(parent,skin,[0,.061,-.129],[0,-.021,-(.123+p.nose*.53)],.013,.018);
  this.ellipsoid(parent,skin,[0,-.026,-(.115+p.nose*.74)],[p.tip,.017,.022]);
  for(const side of [-1,1]){
   this.ellipsoid(parent,skin,[side*.019,-.035,-.140],[.012,.010,.013]);
   this.ellipsoid(parent,this.mat('nostril',0x725243,.95),[side*.014,-.041,-.148],[.006,.003,.004]);
  }
  this.tube(parent,crease,[[-.007,-.048,-.133],[0,-.061,-.137],[.007,-.048,-.133]],.0015);
  const mouthPoints=[[-p.mouth/2,-.081+p.smile,-.122],[-.014,-.079,-.135],[0,-.082,-.140],[.014,-.079,-.135],[p.mouth/2,-.081+p.smile,-.122]];
  this.tube(parent,lip,mouthPoints,.0026);this.ellipsoid(parent,this.mat('lower-lip:'+id,0xb68875,.9),[0,-.088,-.130],[p.mouth*.39,.005,.006]);
  // Slight forehead lines, concentrated around the brow rather than painted everywhere.
  if(id==='dzhanibekov'||id==='savinykh')for(const y of [.091,.110])this.tube(parent,crease,[[-.047,y,-.109],[0,y+.002,-.119],[.047,y,-.109]],.00065);
  const hairGeo=this.geometry('hair-cap:'+id,()=>{
   const pos=[],uv=[],idx=[],cols=40,rows=10;
   for(let row=0;row<=rows;row++)for(let col=0;col<=cols;col++){
    const a=col/cols*Math.PI*2,front=Math.max(0,Math.cos(a));
    const edge=-.045+front*(p.hairline+.045)+p.recession*front*Math.abs(Math.sin(a*2));
    const y=edge+( .199-edge)*Math.sin(row/rows*Math.PI/2),f=Math.sqrt(Math.max(.0001,1-((y-.012)/.198)**2));
    const ripple=id==='krikalev'?Math.sin(a*24+row*1.8)*.0015:Math.sin(a*16+row*.2)*.0006;
    pos.push((p.width+.005+ripple)*f*Math.sin(a),y,-(p.depth+.005+ripple)*f*Math.cos(a)+.004);uv.push(col/cols,row/rows);
    if(row<rows&&col<cols){const k=row*(cols+1)+col;idx.push(k,k+cols+1,k+1,k+1,k+cols+1,k+cols+2);}
   }
   const g=new THREE.BufferGeometry();g.setAttribute('position',new THREE.Float32BufferAttribute(pos,3));g.setAttribute('uv',new THREE.Float32BufferAttribute(uv,2));g.setIndex(idx);g.computeVertexNormals();return g;
  });this.mesh(parent,hairGeo,hair);
  if(p.part){
   for(let i=0;i<13;i++){
    const x=-.10+i*.015;
    this.tube(parent,this.mat('hair-strand:'+id,0x665043,.98),[[x*.8,p.hairline+.014,-.082],[x*.7+p.part*.016,.184,-.023],[x*.6+p.part*.024,.188,.038]],.0012);
   }
  }
 }
 create(profile){
  const p=PORTRAITS[profile.id];if(!p)throw new Error('Unknown cosmonaut '+profile.id);
  const root=new THREE.Group();root.name='Station crew · '+profile.short;root.userData.portrait=profile.id;root.userData.references=p.reference;
  const body=this.group(root,'body');body.scale.setScalar(profile.height*.97);
  const rig={body},palette={savinykh:[0x344b65,0xc6a66b],dzhanibekov:[0x4c565d,0xd99958],ryazansky:[0x3d6259,0x9cbea2],krikalev:[0x3c536d,0x82b6d2]}[profile.id];
  const cloth=this.mat('duty:'+profile.id,palette[0],.94),accent=this.mat('duty-accent:'+profile.id,palette[1],.8),panel=this.mat('duty-panel',0x23323e,.97),boots=this.mat('duty-boots',0x18232c,.96),metal=this.mat('duty-hardware',0x87969c,.55,.4),skin=this.mat('skin:'+profile.id,p.skin,.9);
  const torso=this.geometry('duty-torso',()=>{const g=new THREE.LatheGeometry([[.174,.81],[.19,.88],[.185,1.04],[.224,1.25],[.245,1.37],[.22,1.42],[.105,1.50]].map(([r,y])=>new THREE.Vector2(r,y)),24);g.scale(1,1,.66);return g;});
  this.mesh(body,torso,cloth);this.ellipsoid(body,cloth,[0,.82,.014],[.19,.105,.125]);
  this.rod(body,skin,[0,1.43,0],[0,1.59,0],.064,.061);
  this.torus(body,panel,[0,1.505,0],.078,.014,[Math.PI/2,0,0],[1,1,.9]);
  rig.head=this.group(body,'head',[0,1.69,0]);rig.head.scale.setScalar(.86);this.head(rig.head,p,profile.id);
  // Tailored duty uniform: shoulder yoke, zipper, rank, pockets and name badge.
  for(const side of [-1,1]){
   this.box(body,panel,[side*.142,1.38,-.133],[.17,.09,.018]);
   this.box(body,accent,[side*.142,1.415,-.145],[.15,.018,.008]);
   this.box(body,panel,[side*.119,1.18,-.143],[.145,.15,.016]);
   this.box(body,cloth,[side*.119,1.245,-.155],[.15,.035,.012]);
   this.box(body,accent,[side*.182,.93,-.105],[.017,.16,.012]);
  }
  this.box(body,panel,[0,1.18,-.153],[.019,.48,.012]);
  this.box(body,metal,[0,1.37,-.165],[.014,.035,.008]);
  this.patch(body,profile.id,p.name,[.105,1.33,-.153],[.18,.037]);
  this.patch(body,profile.id,'',[-.105,1.32,-.154],[.065,.073],'mission');
  for(let i=0;i<(profile.id==='savinykh'?3:2);i++)this.box(body,accent,[-.102+i*.025,1.23,-.169],[.015,.04,.008]);
  this.torus(body,panel,[0,.858,0],.185,.019,[Math.PI/2,0,0],[1,1,.66]);
  this.box(body,metal,[0,.86,-.141],[.052,.039,.016]);
  this.box(body,panel,[.196,.91,.025],[.035,.14,.09]);
  this.box(body,accent,[.217,.945,.023],[.008,.022,.056]);
  this.box(body,panel,[0,1.25,.148],[.32,.08,.016]);
  for(const [index,side]of [-1,1].entries()){
   const arm=rig['arm'+index]=this.group(body,'arm'+index,[side*.25,1.37,0]);
   this.ellipsoid(arm,cloth,[side*.012,-.028,0],[.085,.09,.085]);
   this.rod(arm,cloth,[side*.015,-.03,0],[side*.035,-.265,-.012],.079,.063);
   this.box(arm,accent,[side*.025,-.10,-.077],[.085,.025,.008]);
   const elbow=rig['elbow'+index]=this.group(arm,'elbow'+index,[side*.035,-.265,-.012]);
   elbow.rotation.x=-.1;
   this.ellipsoid(elbow,panel,[0,0,.015],[.064,.058,.064]);
   this.rod(elbow,cloth,[0,-.015,0],[side*.005,-.225,-.015],.061,.045);
   this.rod(elbow,panel,[side*.005,-.215,-.014],[side*.005,-.245,-.016],.047);
   this.ellipsoid(elbow,skin,[side*.005,-.294,-.025],[.044,.065,.028]);
   this.ellipsoid(elbow,skin,[-side*.031,-.278,-.042],[.017,.035,.018]);
   const leg=rig['leg'+index]=this.group(body,'leg'+index,[side*.106,.82,0]);
   this.rod(leg,cloth,[0,-.015,0],[side*.012,-.365,.005],.102,.076);
   this.box(leg,panel,[side*.085,-.14,.005],[.034,.17,.11]);
   const knee=rig['knee'+index]=this.group(leg,'knee'+index,[side*.012,-.365,.005]);
   this.ellipsoid(knee,cloth,[0,0,0],[.077,.074,.078]);
   this.rod(knee,cloth,[0,-.005,0],[0,-.30,.015],.074,.053);
   this.box(knee,panel,[0,-.015,-.072],[.105,.09,.015]);
   this.rod(knee,boots,[0,-.28,.015],[0,-.365,.005],.058,.065);
   this.box(knee,boots,[0,-.375,-.038],[.145,.13,.255]);
   this.box(knee,panel,[0,-.424,-.039],[.15,.025,.26]);
  }
  if(profile.id==='krikalev'){
   this.ellipsoid(rig.head,panel,[-p.width-.018,0,.006],[.017,.034,.028]);
   this.tube(rig.head,metal,[[-p.width-.022,-.018,-.011],[-.12,-.074,-.109],[-.062,-.082,-.155]],.0035);
   this.ellipsoid(rig.head,panel,[-.062,-.082,-.158],[.012,.007,.009]);
  }
  if(profile.id==='dzhanibekov')for(let i=0;i<3;i++)this.rod(body,metal,[-.19+i*.045,.91,-.108],[-.19+i*.045,1.03,-.108],.007);
  const ringMaterial=new THREE.MeshBasicMaterial({color:profile.color,transparent:true,opacity:.24,depthWrite:false});this.materials.set('selection:'+profile.id,ringMaterial);
  rig.ring=this.torus(root,ringMaterial,[0,.022,0],.40,.005);
  root.traverse(o=>o.userData.agentID=profile.id);this.pack(body);
  return {root,rig};
 }
 // Merge rigid pieces per joint and material; joints stay independently animated.
 // Ordinary meshes work with both WebGL and the project's CPU renderer.
 pack(parent){
  for(const child of [...parent.children])if(child.isGroup)this.pack(child);
  const groups=new Map();for(const child of parent.children)if(child.isMesh&&!child.userData.keepSurface){const list=groups.get(child.material)||[];list.push(child);groups.set(child.material,list);}
  for(const [material,list]of groups){
   if(list.length<2)continue;const positions=[],normals=[],uv=[];
   for(const part of list){part.updateMatrix();const geometry=part.geometry.index?part.geometry.toNonIndexed():part.geometry.clone();geometry.applyMatrix4(part.matrix);positions.push(...geometry.attributes.position.array);normals.push(...geometry.attributes.normal.array);uv.push(...geometry.attributes.uv.array);geometry.dispose();}
   const merged=new THREE.BufferGeometry();merged.setAttribute('position',new THREE.Float32BufferAttribute(positions,3));merged.setAttribute('normal',new THREE.Float32BufferAttribute(normals,3));merged.setAttribute('uv',new THREE.Float32BufferAttribute(uv,2));merged.computeBoundingSphere();this.geometries.set('merged:'+merged.uuid,merged);
   const mesh=this.mesh(parent,merged,material);mesh.userData.agentID=list[0].userData.agentID;mesh.userData.mergedParts=list.length;for(const part of list)parent.remove(part);
  }
 }
 dispose(){if(this.disposed)return;this.disposed=true;for(const g of this.geometries.values())g.dispose();for(const m of this.materials.values())m.dispose();for(const t of this.textures)t.dispose();}
}
