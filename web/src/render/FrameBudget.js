// Bound framebuffer work independently of CSS/UI resolution. Slow frames lower
// only raster resolution; geometry, simulation, interactions and streaming stay.
export class FrameBudget {
 constructor({software=false,quality='balanced'}={}){this.software=software;this.quality=quality;this.scale=1;this.reset();}
 reset(){this.since=0;this.samples=0;this.total=0;this.maximum=0;this.fastWindows=0;}
 setQuality(quality){this.quality=quality==='economy'?'economy':'balanced';this.scale=1;this.reset();}
 pixelRatio(width,height,dpr=1){
  const economy=this.quality==='economy',pixels=this.software?(economy?480*270:640*360):(economy?1280*720:1920*1080);
  const limit=this.software?1:economy?1:1.5;
  return Math.min(Math.max(.1,dpr),limit,Math.sqrt(pixels/Math.max(1,width*height)))*this.scale;
 }
 sample(milliseconds,now){
  // Ignore tab-sized pauses, cap individual slow samples, and discard the
  // single worst sample per window. Persistent slow frames still lower quality.
  if(!Number.isFinite(milliseconds)||milliseconds<=0||milliseconds>=1000)return false;
  milliseconds=Math.min(milliseconds,250);
  if(!this.since)this.since=now;
  this.samples++;this.total+=milliseconds;this.maximum=Math.max(this.maximum,milliseconds);
  if(now-this.since<2500||this.samples<(this.software?8:20))return false;
  const mean=(this.total-this.maximum)/(this.samples-1),slow=this.software?42:24,fast=this.software?28:18;
  let scale=this.scale;
  if(mean>slow){scale=Math.max(.6,scale-.1);this.fastWindows=0;}
  else if(mean<fast){if(++this.fastWindows>=3){scale=Math.min(1,scale+.05);this.fastWindows=0;}}
  else this.fastWindows=0;
  this.since=now;this.samples=0;this.total=0;this.maximum=0;
  const changed=Math.abs(scale-this.scale)>.001;this.scale=scale;return changed;
 }
}
