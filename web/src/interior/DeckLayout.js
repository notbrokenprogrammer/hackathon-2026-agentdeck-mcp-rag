import layout from './layout.json' with {type: 'json'};
export {layout};
export const contains = (r,x,z,inset=0) => Math.abs(x-r.x)<=r.w/2-inset && Math.abs(z-r.z)<=r.d/2-inset;
export const roomAt = (x,z) => layout.areas.find(r=>contains(r,x,z));
export function wallSegments() {
 const walls=[];
 for(const area of layout.areas) for(const axis of ['x','z']) for(const sign of [-1,1]) {
  const horizontal=axis==='z', center=horizontal?area.x:area.z, length=horizontal?area.w:area.d;
  const fixed=area[axis]+sign*(horizontal?area.d:area.w)/2;
  const gates=layout.gates.filter(g=>g.areas.includes(area.id)&&Math.abs(g[axis]-fixed)<.01).map(g=>horizontal?g.x:g.z).sort((a,b)=>a-b);
  let start=center-length/2;
  for(const end of [...gates.map(v=>v-layout.doorWidth/2),center+length/2]) {
   if(end>start+.01)walls.push({x:horizontal?(start+end)/2:fixed,z:horizontal?fixed:(start+end)/2,w:horizontal?end-start:layout.wall,d:horizontal?layout.wall:end-start,area:area.id});
   start=end+layout.doorWidth;
  }
 }
 return walls;
}
export const walls=wallSegments();
export function circleHits(x,z,r,b) {
 const dx=Math.max(Math.abs(x-b.x)-b.w/2,0),dz=Math.max(Math.abs(z-b.z)-b.d/2,0);
 return dx*dx+dz*dz<r*r;
}
export function walkable(x,z,r=layout.radius) {
 return layout.areas.some(a=>contains(a,x,z)) && !walls.some(b=>circleHits(x,z,r,b)) && !layout.obstacles.some(b=>circleHits(x,z,r,b));
}
export function slideMove(from,to,allowed) {
 const dx=to.x-from.x,dz=to.z-from.z,steps=Math.max(1,Math.ceil(Math.hypot(dx,dz)/.08));
 let x=from.x,z=from.z;
 for(let i=0;i<steps;i++) {
  if(allowed(x+dx/steps,z))x+=dx/steps;
  if(allowed(x,z+dz/steps))z+=dz/steps;
 }
 return {x,y:1.7,z};
}
export function doorProgress(d,now=Date.now()) {
 if(!d.motion_at)return d.open?1:0;
 const t=Math.max(0,Math.min(1,(now-d.motion_at)/layout.doorDuration));
 return (d.motion_from||0)+((d.open?1:0)-(d.motion_from||0))*t;
}
export function doorBlocks(d,x,z,progress=doorProgress(d),radius=layout.radius) {
 const dx=x-d.x,dz=z-d.z,a=d.rotation||0;
 const normal=dx*Math.cos(a)-dz*Math.sin(a),tangent=dx*Math.sin(a)+dz*Math.cos(a);
 // A real shrinking gap between the two leaves. Frame collision belongs to walls.
 return Math.abs(normal)<.15+radius && Math.abs(tangent)<1.3+radius && Math.abs(tangent)+radius>progress*1.34;
}
