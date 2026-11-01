import * as THREE from 'three';

const wrapUV=(value,mode)=>mode===THREE.RepeatWrapping?value-Math.floor(value):mode===THREE.MirroredRepeatWrapping?1-Math.abs(((value%2)+2)%2-1):Math.max(0,Math.min(1,value));

// Offline/Canvas fallback. A depth buffer and near-plane clipping are necessary
// inside closed rooms: painter sorting leaves holes in walls and ceilings.
export class SoftwareRenderer {
 constructor(canvas){this.domElement=canvas;this.ctx=canvas.getContext('2d');if(!this.ctx)throw new Error('Canvas renderer unavailable');this.last=0;this.pixelRatio=1;this.frameInterval=1000/30;this.isSoftware=true;this.textureCanvases=new WeakMap();this.info={render:{triangles:0,calls:0},memory:{}};}
 setPixelRatio(ratio){this.pixelRatio=Math.max(.1,Number(ratio)||1);}
 setSize(w,h){const width=Math.max(1,Math.round(w*this.pixelRatio)),height=Math.max(1,Math.round(h*this.pixelRatio));if(this.width===width&&this.height===height)return;this.width=width;this.height=height;this.domElement.width=this.width;this.domElement.height=this.height;this.buffer=this.ctx.createImageData(this.width,this.height);this.depth=new Float32Array(this.width*this.height);}
 texture(map){const source=map?.image;if(!source)return null;if(this.textureCanvases.has(source))return this.textureCanvases.get(source);let result;if(source.data&&!source.getContext)result={data:source.data,width:source.width,height:source.height};else{let c=source;if(!c.getContext){c=document.createElement('canvas');c.width=source.width;c.height=source.height;c.getContext('2d').drawImage(source,0,0);}result=c.getContext('2d').getImageData(0,0,c.width,c.height);}let grayscale=true,opaque=true;for(let i=0;i<result.data.length&&(grayscale||opaque);i+=4){grayscale&&=result.data[i]===result.data[i+1]&&result.data[i]===result.data[i+2];opaque&&=result.data[i+3]===255;}result={data:result.data,width:result.width,height:result.height,grayscale,opaque};this.textureCanvases.set(source,result);return result;}
 render(scene,camera){
  const now=performance.now();if(now-this.last<this.frameInterval||!this.width)return;this.last=now;scene.updateMatrixWorld();camera.updateMatrixWorld();camera.matrixWorldInverse.copy(camera.matrixWorld).invert();
  const vp=new THREE.Matrix4().multiplyMatrices(camera.projectionMatrix,camera.matrixWorldInverse),frustum=new THREE.Frustum().setFromProjectionMatrix(vp),sphere=new THREE.Sphere();
  const w=this.width,h=this.height,pixels=this.buffer.data,depth=this.depth,bg=scene.background?.isColor?scene.background.clone().convertLinearToSRGB():new THREE.Color(0,0,0);depth.fill(Infinity);
  for(let i=0;i<pixels.length;i+=4){pixels[i]=bg.r*255;pixels[i+1]=bg.g*255;pixels[i+2]=bg.b*255;pixels[i+3]=scene.background?255:0;}
  let hemisphere;scene.traverseVisible(o=>{if(o.isHemisphereLight&&!hemisphere)hemisphere=o;});
  const strength=hemisphere?Math.max(0,Math.min(3,hemisphere.intensity/2.6)):1,white=new THREE.Color(0xffffff),light=new THREE.Vector3(.3,.85,.5).normalize(),nm=new THREE.Matrix3();
  const translucent=[],sprites=[],normal=new THREE.Vector3();let triangles=0,calls=0;
  const raster=(v,mat,color,tex)=>{
   const a=v[0],b=v[1],c=v[2];const area=(b.x-a.x)*(c.y-a.y)-(b.y-a.y)*(c.x-a.x);
   if(Math.abs(area)<.05||(mat.side!==THREE.DoubleSide&&(mat.side===THREE.BackSide?area<0:area>0)))return;
   const minX=Math.max(0,Math.floor(Math.min(a.x,b.x,c.x))),maxX=Math.min(w-1,Math.ceil(Math.max(a.x,b.x,c.x))),minY=Math.max(0,Math.floor(Math.min(a.y,b.y,c.y))),maxY=Math.min(h-1,Math.ceil(Math.max(a.y,b.y,c.y)));
   if(minX>maxX||minY>maxY)return;triangles++;
   const inv=1/area,alpha=mat.transparent?mat.opacity:1,cr=color.r*255,cg=color.g*255,cb=color.b*255;
   const map=mat.map,repeatX=map?.wrapS===THREE.RepeatWrapping,repeatY=map?.wrapT===THREE.RepeatWrapping,linear=map?.magFilter===THREE.LinearFilter,offset=linear?.5:0;
   const dx0=(b.y-c.y)*inv,dx1=(c.y-a.y)*inv,dx2=-dx0-dx1,px=minX+.5;
   for(let y=minY;y<=maxY;y++){const py=y+.5;let b0=((b.x-px)*(c.y-py)-(b.y-py)*(c.x-px))*inv,b1=((c.x-px)*(a.y-py)-(c.y-py)*(a.x-px))*inv;
   // Intersect this scanline with the three triangle half-planes. Thin panels
   // used to scan their entire rectangle, most of which is outside the triangle.
   let start=0,end=maxX-minX;
   if(dx0>0)start=Math.max(start,Math.ceil((-.00001-b0)/dx0));else if(dx0<0)end=Math.min(end,Math.floor((-.00001-b0)/dx0));else if(b0<-.00001)continue;
   if(dx1>0)start=Math.max(start,Math.ceil((-.00001-b1)/dx1));else if(dx1<0)end=Math.min(end,Math.floor((-.00001-b1)/dx1));else if(b1<-.00001)continue;
   const base2=1-b0-b1;
   if(dx2>0)start=Math.max(start,Math.ceil((-.00001-base2)/dx2));else if(dx2<0)end=Math.min(end,Math.floor((-.00001-base2)/dx2));else if(base2<-.00001)continue;
   b0+=start*dx0;b1+=start*dx1;
   for(let x=minX+start,stop=minX+end;x<=stop;x++,b0+=dx0,b1+=dx1){
    const b2=1-b0-b1;
    const z=b0*a.z+b1*b.z+b2*c.z,index=y*w+x;if(z>depth[index]+1e-6||z< -1||z>1)continue;
    let r=cr,g=cg,bl=cb,opacity=alpha;
    if(tex){const q=b0*a.q+b1*b.q+b2*c.q,u=wrapUV((b0*a.u+b1*b.u+b2*c.u)/q,map.wrapS);let v=wrapUV((b0*a.v+b1*b.v+b2*c.v)/q,map.wrapT);if(map.flipY)v=1-v;
     const tx=repeatX?u*tex.width-offset:Math.max(0,Math.min(tex.width-1,u*tex.width-offset)),ty=repeatY?v*tex.height-offset:Math.max(0,Math.min(tex.height-1,v*tex.height-offset)),rawX=Math.floor(tx),rawY=Math.floor(ty),ix=repeatX?(rawX+tex.width)%tex.width:rawX,iy=repeatY?(rawY+tex.height)%tex.height:rawY,k=(iy*tex.width+ix)*4;
     if(linear){const x1=repeatX?(ix+1)%tex.width:Math.min(tex.width-1,ix+1),y1=repeatY?(iy+1)%tex.height:Math.min(tex.height-1,iy+1),fx=tx-rawX,fy=ty-rawY,k10=(iy*tex.width+x1)*4,k01=(y1*tex.width+ix)*4,k11=(y1*tex.width+x1)*4;const a=(1-fx)*(1-fy)/255,b=fx*(1-fy)/255,c=(1-fx)*fy/255,d=fx*fy/255;const red=tex.data[k]*a+tex.data[k10]*b+tex.data[k01]*c+tex.data[k11]*d;r*=red;if(tex.grayscale){g*=red;bl*=red;}else{g*=tex.data[k+1]*a+tex.data[k10+1]*b+tex.data[k01+1]*c+tex.data[k11+1]*d;bl*=tex.data[k+2]*a+tex.data[k10+2]*b+tex.data[k01+2]*c+tex.data[k11+2]*d;}if(!tex.opaque)opacity*=tex.data[k+3]*a+tex.data[k10+3]*b+tex.data[k01+3]*c+tex.data[k11+3]*d;}else{r*=tex.data[k]/255;g*=tex.data[k+1]/255;bl*=tex.data[k+2]/255;opacity*=tex.data[k+3]/255;}}
    if(opacity<(mat.alphaTest||0)||opacity===0)continue;
    const k=index*4;if(opacity<1){pixels[k]=r*opacity+pixels[k]*(1-opacity);pixels[k+1]=g*opacity+pixels[k+1]*(1-opacity);pixels[k+2]=bl*opacity+pixels[k+2]*(1-opacity);}else{pixels[k]=r;pixels[k+1]=g;pixels[k+2]=bl;}pixels[k+3]=255;if(mat.depthWrite!==false&&opacity>.98)depth[index]=z;
   }}
  };
  scene.traverseVisible(object=>{
   if(object.isSprite){sprites.push(object);return;}if(!object.isMesh||object.isSkinnedMesh)return;const geo=object.geometry,pos=geo.attributes.position;if(!pos)return;
   if(!geo.boundingSphere)geo.computeBoundingSphere();sphere.copy(geo.boundingSphere).applyMatrix4(object.matrixWorld);if(!frustum.intersectsSphere(sphere))return;
   const mat=Array.isArray(object.material)?object.material[0]:object.material;if(!mat||mat.opacity===0)return;calls++;
   const projection=new THREE.Matrix4().multiplyMatrices(vp,object.matrixWorld),m=projection.elements,uv=geo.attributes.uv,vertices=[];nm.getNormalMatrix(object.matrixWorld);if(mat.map?.matrixAutoUpdate)mat.map.updateMatrix();const tm=mat.map?.matrix.elements;
   for(let i=0;i<pos.count;i++){const x=pos.getX(i),y=pos.getY(i),z=pos.getZ(i),u=uv?.getX(i)||0,v=uv?.getY(i)||0;vertices.push({x:m[0]*x+m[4]*y+m[8]*z+m[12],y:m[1]*x+m[5]*y+m[9]*z+m[13],z:m[2]*x+m[6]*y+m[10]*z+m[14],w:m[3]*x+m[7]*y+m[11]*z+m[15],u:tm?tm[0]*u+tm[3]*v+tm[6]:u,v:tm?tm[1]*u+tm[4]*v+tm[7]:v});}
   const index=geo.index,count=index?index.count:pos.count,normals=geo.attributes.normal,tex=this.texture(mat.map);
   for(let i=0;i<count;i+=3){const ids=[index?index.getX(i):i,index?index.getX(i+1):i+1,index?index.getX(i+2):i+2];let polygon=ids.map(id=>vertices[id]);
    // Clip against near/far planes in homogeneous space, preserving UVs.
    for(const sign of [1,-1]){const clipped=[];for(let j=0;j<polygon.length;j++){const a=polygon[j],b=polygon[(j+1)%polygon.length],da=a.w+sign*a.z,db=b.w+sign*b.z;if(da>=0)clipped.push(a);if((da>=0)!==(db>=0)){const t=da/(da-db),v={};for(const k of ['x','y','z','w','u','v'])v[k]=a[k]+(b[k]-a[k])*t;clipped.push(v);}}polygon=clipped;}
    if(polygon.length<3)continue;
    normal.set(0,0,0);if(normals)for(const id of ids){normal.x+=normals.getX(id);normal.y+=normals.getY(id);normal.z+=normals.getZ(id);}normal.applyMatrix3(nm).normalize();
    const color=(mat.color||white).clone(),shade=mat.isMeshBasicMaterial?1:.62+.45*Math.max(0,normal.dot(light));color.multiplyScalar(shade);
    if(hemisphere&&!mat.isMeshBasicMaterial)color.multiply(hemisphere.groundColor.clone().lerp(hemisphere.color,(normal.y+1)*.5).lerp(white,.4)).multiplyScalar(strength);
    if(mat.emissive)color.add(mat.emissive.clone().multiplyScalar(Math.min(.4,(mat.emissiveIntensity||0)*.25)));color.convertLinearToSRGB();
    const projected=polygon.map(v=>({x:(v.x/v.w+1)*w/2,y:(1-v.y/v.w)*h/2,z:v.z/v.w,q:1/v.w,u:v.u/v.w,v:v.v/v.w}));
    for(let j=1;j<projected.length-1;j++){const triangle=[projected[0],projected[j],projected[j+1]];if(mat.transparent)translucent.push({v:triangle,mat,color,tex,z:triangle.reduce((a,v)=>a+v.z,0)});else raster(triangle,mat,color,tex);}
   }
  });
  translucent.sort((a,b)=>b.z-a.z);for(const f of translucent)raster(f.v,f.mat,f.color,f.tex);this.ctx.putImageData(this.buffer,0,0);
  // Existing crew nameplates retain their authored appearance and obey wall depth.
  for(const sprite of sprites){const image=sprite.material.map?.image;if(!image?.getContext)continue;const pos=sprite.getWorldPosition(new THREE.Vector3()),distance=pos.distanceTo(camera.position);pos.applyMatrix4(vp);const x=(pos.x+1)*w/2,y=(1-pos.y)*h/2;if(pos.z< -1||pos.z>1||x<0||x>=w||y<0||y>=h||pos.z>depth[Math.floor(y)*w+Math.floor(x)]+.001)continue;const scale=sprite.getWorldScale(new THREE.Vector3()),factor=h/(2*Math.tan(THREE.MathUtils.degToRad(camera.fov/2)))/Math.max(distance,1);this.ctx.drawImage(image,x-scale.x*factor/2,y-scale.y*factor/2,scale.x*factor,scale.y*factor);}
  this.info.render={triangles,calls};
 }
 dispose(){this.buffer=null;this.depth=null;this.textureCanvases=new WeakMap();}
}
