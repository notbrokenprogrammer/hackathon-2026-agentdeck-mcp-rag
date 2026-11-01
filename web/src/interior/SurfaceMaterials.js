import * as THREE from 'three';

// Small, deterministic, locally authored surface tiles. Their serialized bytes
// are ordinary manifest resources shared by every room and both geometry LODs.
// Painted grain stays subtle; structural seams and fasteners use authored geometry.
export function applySurfaceMaterials(interior) {
 const texture=(size,pixel,colorSpace=THREE.SRGBColorSpace)=>{
  const data=new Uint8Array(size*size*4);
  for(let y=0;y<size;y++)for(let x=0;x<size;x++){
   const k=(y*size+x)*4,value=Math.round(Math.max(0,Math.min(255,pixel(x,y,size))));
   data[k]=data[k+1]=data[k+2]=value;data[k+3]=255;
  }
  const map=new THREE.DataTexture(data,size,size);
  map.colorSpace=colorSpace;map.wrapS=map.wrapT=THREE.RepeatWrapping;
  map.magFilter=THREE.LinearFilter;map.minFilter=THREE.LinearMipmapLinearFilter;
  map.generateMipmaps=true;map.anisotropy=4;map.needsUpdate=true;interior.textures.push(map);return map;
 };
 const noise=(x,y)=>((x*73+y*151+x*y*17)%29)/28-.5;
 // Panel seams, captive screws and latches are modelled at their true scale.
 // A generic painted bolt on every UV face stretched to palm-sized smudges.
 const panels=texture(64,(x,y)=>249+noise(x,y)*2.5);
 const deck=texture(128,(x,y,n)=>{
  const edge=Math.min(x,y,n-1-x,n-1-y);
  let shade=242+noise(x,y)*4+Math.sin(y*1.3)*.8;
  if(edge===0)shade=202;
  else if(edge===1)shade=249;
  // Fine non-slip grain and sparse, shallow scuffs in the walking surface.
  if((x*31+y*17)%257===0)shade-=7;
  return shade;
 });
 const brushed=texture(32,(x,y)=>245+Math.sin(y*2.1)*4+noise(x,y)*3);
 for(const [keys,map,bump]of[
  [['white','pearl'],panels,.003],
  [['floor'],deck,.006],
  [['metal','titanium'],brushed,.001]
 ]){
  // Height and roughness are linear data; sRGB albedo must never serve as both.
  const height=map.clone();height.colorSpace=THREE.NoColorSpace;height.needsUpdate=true;interior.textures.push(height);
  const roughness=texture(32,(x,y)=>244+noise(x,y)*9,THREE.NoColorSpace);
  for(const key of keys){
   const material=interior.mat(key);if(!material)continue;
   material.map=map;material.roughnessMap=roughness;material.bumpMap=height;material.bumpScale=bump;
   material.userData.staticSurface=true;material.needsUpdate=true;
  }
 }
}
