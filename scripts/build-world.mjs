import {createRequire,registerHooks} from 'node:module';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
const root=path.resolve(import.meta.dirname,'..');
const require=createRequire(process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES?process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES+'/package.json':import.meta.url);
const {createCanvas}=require('@napi-rs/canvas');
globalThis.document={createElement:()=>createCanvas(1,1)};
globalThis.HTMLCanvasElement=createCanvas(1,1).constructor;
registerHooks({resolve(s,c,next){return s==='three'?{url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true}:next(s,c);}});
const THREE=await import('../web/vendor/three.module.js');
const {ShipInterior}=await import('../web/src/interior/ShipInterior.js');
const {layout}=await import('../web/src/interior/DeckLayout.js');
const {CREW}=await import('../web/src/crew/BridgeCrew.js');
const {CosmonautModels}=await import('../web/src/crew/CosmonautModel.js');
const ship={scene:new THREE.Scene(),renderer:{isSoftware:true}};
const interior=new ShipInterior(ship,{batch:false});
interior.root.updateMatrixWorld(true);
// Preserve every authored surface, spatially partitioning before batching.
// Shell and ceiling stay separate so AOI always closes the occupied room.
const groups=new Map();
const groupFor=(key,metadata)=>{
 if(!groups.has(key)){const group=new THREE.Group();group.name=key;group.userData={interiorChunk:true,...metadata};groups.set(key,group);}
 return groups.get(key);
};
const background=new THREE.Group();background.name='Orbital environment and player template';background.userData.interiorChunk=true;
interior.root.traverse(o=>{
 if(!o.isMesh&&!o.isPoints)return;
 let role='detail',areas=[],isBackground=false,equipment=null;
 for(let p=o;p&&p!==interior.root;p=p.parent){
  if(p.userData.background)isBackground=true;
  if(p.userData.colliderID)equipment=p;
  if(p.userData.interiorRole==='ceiling')role='ceiling';
  else if(p.userData.interiorRole==='shell'&&role!=='ceiling')role='shell';
  if(!areas.length){const a=p.userData.areas||p.userData.area;areas=Array.isArray(a)?a:a?[a]:[];}
 }
 const center=new THREE.Vector3();o.getWorldPosition(center);
 if(role==='ceiling'&&o.name!=='ceiling-slab')role='ceiling-detail';
 if(equipment){role='equipment';equipment.getWorldPosition(center);}
 const cx=Math.floor(center.x/8),cz=Math.floor(center.z/8);
 const target=isBackground?background:groupFor(`${areas.join('+')}/${cx},${cz}/${role}${equipment?'/'+equipment.userData.colliderID:''}`,{areas,role,cx,cz,...(equipment?{colliderID:equipment.userData.colliderID}:{})});
 const copy=o.clone(false);o.matrixWorld.decompose(copy.position,copy.quaternion,copy.scale);target.add(copy);
});
// Bake existing portraits and animation rigs into the counted resources.
const models=new CosmonautModels();
for(const profile of CREW){
 const group=new THREE.Group();group.position.set(...profile.position);
 const area=layout.areas.find(a=>Math.abs(profile.position[0]-a.x)<=a.w/2&&Math.abs(profile.position[2]-a.z)<=a.d/2);
 group.userData={npcRoot:profile.id,bakedPortrait:true,areas:area?[area.id]:[]};const model=models.create(profile);for(const [name,node]of Object.entries(model.rig))node.userData.rig=name;group.add(model.root);
 groups.set('npc:'+profile.id,group);
}
const player=new THREE.Group();player.userData.playerTemplate=true;
const playerMat=new THREE.MeshStandardMaterial({color:0xdfe4e6,roughness:.7}),playerGeo=new THREE.BoxGeometry(1,1,1);
for(const [position,scale]of [[[0,.9,0],[.48,.66,.28]],[[0,1.43,0],[.3,.32,.3]],[[-.32,.9,0],[.14,.65,.16]],[[.32,.9,0],[.14,.65,.16]],[[-.14,.32,0],[.19,.64,.22]],[[.14,.32,0],[.19,.64,.22]]]){const mesh=new THREE.Mesh(playerGeo,playerMat);mesh.position.set(...position);mesh.scale.set(...scale);player.add(mesh);}background.add(player);
const schematic=new THREE.Group();schematic.userData.schematicRoot=true;
for(const a of layout.areas){const mesh=new THREE.Mesh(new THREE.BoxGeometry(a.w,.08,a.d),new THREE.MeshBasicMaterial({color:0x7b9fa8,wireframe:true,transparent:true,opacity:.18}));mesh.position.set(a.x,-.5,a.z);schematic.add(mesh);}
for(const profile of CREW){const marker=new THREE.Mesh(new THREE.ConeGeometry(.38,1,5),new THREE.MeshBasicMaterial({color:profile.color,wireframe:true}));marker.position.set(profile.position[0],.8,profile.position[2]);marker.userData={proxyFor:profile.id,agentID:profile.id};schematic.add(marker);}background.add(schematic);
const assets=new Map(),files=new Map(),hash=b=>crypto.createHash('sha256').update(b).digest('hex');
function resource(kind,body){
 if(kind==='geometries'&&body.data){body=JSON.parse(JSON.stringify(body));for(const attr of Object.values(body.data.attributes||{}))if(attr.type?.startsWith('Float'))attr.array=attr.array.map(n=>Math.round(n*100000)/100000);}
 const content={...body};delete content.uuid;
 const id=kind+'_'+hash(JSON.stringify(content)).slice(0,24),bytes=Buffer.from(JSON.stringify({kind,body:{...body,uuid:id}}));
 if(!assets.has(id)){files.set(id,bytes);assets.set(id,{id,size:bytes.length,hash:hash(bytes),kind});}return id;
}
const textureCache=new Map(),materialCache=new Map();
const lodGeometries=new Map();
function staticGeometry(source,low){
 const key=source.uuid+':static:'+low;if(lodGeometries.has(key))return lodGeometries.get(key);
 let shape=source;
 if(low&&source.type==='ExtrudeGeometry'&&source.parameters.options.bevelEnabled){
  shape=new THREE.ExtrudeGeometry(source.parameters.shapes,{...source.parameters.options,bevelEnabled:false,curveSegments:3});
  source.computeBoundingBox();shape.computeBoundingBox();
  const target=source.boundingBox.getSize(new THREE.Vector3()),size=shape.boundingBox.getSize(new THREE.Vector3()),center=source.boundingBox.getCenter(new THREE.Vector3());
  shape.center();shape.scale(target.x/size.x,target.y/size.y,target.z/size.z);shape.translate(center.x,center.y,center.z);
 }
 const result=new THREE.BufferGeometry().copy(shape);lodGeometries.set(key,result);return result;
}
// Vertex clustering removes sub-pixel portrait details in the distant LOD,
// retaining closed surfaces and articulated joints rather than dropping faces.
function portraitGeometry(source,low,mapped,head){
 const key=source.uuid+':'+low+':'+mapped+':'+head;if(lodGeometries.has(key))return lodGeometries.get(key);
 const cell=head?(low?.010:.004):(low?.035:.018),position=source.attributes.position,uv=mapped?source.attributes.uv:null;
 const cells=new Map(),vertices=[],uvs=[],indices=[],mapping=[];
 for(let n=0;n<position.count;n++){
  const p=[position.getX(n),position.getY(n),position.getZ(n)],key=p.map(v=>Math.round(v/cell)).join(',')+(uv?':'+Math.round(uv.getX(n)*32)+','+Math.round(uv.getY(n)*32):'');
  if(!cells.has(key)){cells.set(key,vertices.length/3);vertices.push(...p);if(uv)uvs.push(uv.getX(n),uv.getY(n));}mapping.push(cells.get(key));
 }
 const count=source.index?.count||position.count;
 for(let n=0;n<count;n+=3){const face=[0,1,2].map(k=>mapping[source.index?source.index.getX(n+k):n+k]);if(new Set(face).size===3)indices.push(...face);}
 const result=new THREE.BufferGeometry();result.setAttribute('position',new THREE.Float32BufferAttribute(vertices,3));if(uv)result.setAttribute('uv',new THREE.Float32BufferAttribute(uvs,2));result.setIndex(indices);result.computeVertexNormals();lodGeometries.set(key,result);return result;
}
function texture(source,low,orbital,portrait){
 const key=source.uuid+':'+low+':'+orbital+':'+portrait;if(textureCache.has(key))return textureCache.get(key);
 const image=source.image;if(!image?.getContext)return source;
 const maximum=orbital?256:portrait?(low?48:128):low?96:384,scale=Math.min(1,maximum/Math.max(image.width,image.height));
 const canvas=createCanvas(Math.max(1,Math.round(image.width*scale)),Math.max(1,Math.round(image.height*scale)));
 canvas.getContext('2d').drawImage(image,0,0,canvas.width,canvas.height);
 const data=canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height);
 const result=new THREE.DataTexture(new Uint8Array(data.data),canvas.width,canvas.height);
 result.colorSpace=source.colorSpace;result.magFilter=THREE.LinearFilter;result.minFilter=THREE.LinearMipmapLinearFilter;result.generateMipmaps=true;result.anisotropy=4;result.repeat.y=-1;result.offset.y=1;result.needsUpdate=true;textureCache.set(key,result);return result;
}
function material(mat,low,orbital,portrait){
 const key=mat.uuid+':'+low+':'+orbital+':'+portrait;if(materialCache.has(key))return materialCache.get(key);
 const copy=mat.clone();for(const key of Object.keys(copy))if(copy[key]?.isTexture)copy[key]=texture(copy[key],low,orbital,portrait);
 materialCache.set(key,copy);return copy;
}
function tree(group,low,orbital=false){
 const clone=group.clone(true);clone.traverse(o=>{if(o.material)o.material=Array.isArray(o.material)?o.material.map(m=>material(m,low,orbital,group.userData.bakedPortrait)):material(o.material,low,orbital,group.userData.bakedPortrait);if(group.userData.bakedPortrait&&o.isMesh){let head=false;for(let p=o;p;p=p.parent)if(p.userData.rig==='head')head=true;o.geometry=portraitGeometry(o.geometry,low,!!o.material.map,head);}else if(o.geometry&&(o.geometry.type==='ExtrudeGeometry'||o.geometry.type==='ShapeGeometry'))o.geometry=staticGeometry(o.geometry,low);});
 if(low&&['equipment','detail','ceiling-detail'].includes(group.userData.role)){
  clone.updateMatrixWorld(true);const small=[];clone.traverse(o=>{if(!o.isMesh||(o.material.map&&!o.material.userData.staticSurface))return;const size=new THREE.Box3().setFromObject(o).getSize(new THREE.Vector3());if(Math.max(size.x,size.y,size.z)<.11)small.push(o);});for(const mesh of small)mesh.removeFromParent();
 }
 clone.updateMatrixWorld(true);const json=clone.toJSON(),remap=new Map(),deps=[];
 for(const kind of ['images','textures','materials','geometries'])for(const item of json[kind]||[]){
  const copy=JSON.parse(JSON.stringify(item));
  if(kind==='textures')copy.image=remap.get(copy.image)||copy.image;
  if(kind==='materials')for(const key of Object.keys(copy))if(typeof copy[key]==='string'&&remap.has(copy[key]))copy[key]=remap.get(copy[key]);
  const id=resource(kind,copy);remap.set(item.uuid,id);deps.push(id);
 }
 let serial=0;
 const rewrite=o=>{o.uuid='node-'+serial++;if(o.geometry)o.geometry=remap.get(o.geometry);if(o.material)o.material=Array.isArray(o.material)?o.material.map(x=>remap.get(x)):remap.get(o.material);for(const child of o.children||[])rewrite(child);};rewrite(json.object);
 const object=resource('object',json.object);return{object,deps:[...new Set([...deps,object])]};
}
const chunks=[];
for(const [key,g]of groups){
 const npc=key.startsWith('npc:')?key.slice(4):'',p=npc?g.position:null;
 chunks.push({cx:p?Math.floor(p.x/8):g.userData.cx,cz:p?Math.floor(p.z/8):g.userData.cz,npc,areas:g.userData.areas||[],role:g.userData.role||'npc',low:tree(g,true),high:tree(g,false)});
}
const bg=tree(background,true,true),worldBytes=[...assets.values()].reduce((n,a)=>n+a.size,0);
const manifest={version:hash(JSON.stringify([...assets.keys()])).slice(0,16),chunk_size:8,world_bytes:worldBytes,resources:[...assets.values()],chunks,background:bg,rooms:layout.areas.filter(a=>a.kind==='room'),crew:CREW};
// Publish only after all geometry has serialized successfully.
for(const [id,bytes]of files)fs.writeFileSync(path.join(root,'world',id+'.json'),bytes);
fs.writeFileSync(root+'/world/manifest.json',JSON.stringify(manifest));
for(const filename of fs.readdirSync(root+'/world'))if(filename.endsWith('.json')&&filename!=='manifest.json'&&!assets.has(filename.slice(0,-5)))fs.unlinkSync(root+'/world/'+filename);
console.log(JSON.stringify({resources:assets.size,chunks:chunks.length,world_bytes:worldBytes,budget:Math.floor(worldBytes*.25),limit:Math.floor(worldBytes*.30)}));
