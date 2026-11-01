// Locally authored schematic artwork, baked into counted world textures.
// These diagrams are labelled explicitly; live mission indicators are separate meshes.
export function drawInstrumentDisplay(ctx,title) {
 const w=768,h=384;
 ctx.fillStyle='#081a28';ctx.fillRect(0,0,w,h);
 ctx.strokeStyle='#183447';ctx.lineWidth=1;
 for(let x=24;x<744;x+=32){ctx.beginPath();ctx.moveTo(x,56);ctx.lineTo(x,326);ctx.stroke();}
 for(let y=56;y<326;y+=32){ctx.beginPath();ctx.moveTo(24,y);ctx.lineTo(744,y);ctx.stroke();}
 ctx.fillStyle='#b6d9e5';ctx.font='22px monospace';ctx.fillText(title,24,34);
 ctx.fillStyle='#7399ac';ctx.font='14px monospace';ctx.fillText('HORIZON–7  /  REFERENCE SCHEMATIC',24,366);
 ctx.strokeStyle='#6ab9c7';ctx.lineWidth=3;
 const line=points=>{ctx.beginPath();points.forEach(([x,y],n)=>n?ctx.lineTo(x,y):ctx.moveTo(x,y));ctx.stroke();};
 const label=(text,x,y)=>{ctx.fillStyle='#a8ced7';ctx.font='16px monospace';ctx.fillText(text,x,y);};
 const circle=(x,y,r)=>{ctx.beginPath();ctx.arc(x,y,r,0,Math.PI*2);ctx.stroke();};
 if(/CORE|COOLANT|ENGINEER/.test(title)){
  ctx.strokeRect(274,95,144,195);
  for(let y=115;y<290;y+=35){ctx.beginPath();ctx.ellipse(346,y,72,13,0,0,Math.PI*2);ctx.stroke();}
  line([[274,126],[148,126],[148,268],[274,268]]);line([[418,126],[568,126],[568,268],[418,268]]);
  for(const x of[148,568]){circle(x,196,25);line([[x-10,196],[x+10,186],[x+10,206],[x-10,196]]);}
  label('PRIMARY LOOP',55,87);label('HEAT EXCHANGER',500,315);label('CORE',325,82);
 }else if(/VITALS|SCAN|SPECTRUM|RESEARCH/.test(title)){
  const spectrum=/SPECTRUM|RESEARCH/.test(title);
  for(let row=0;row<3;row++){
   ctx.strokeStyle=['#75c5b4','#77b5d2','#c5b98a'][row];
   const points=[];for(let x=40;x<725;x+=3){const t=(x+row*41)%130;const pulse=t>55&&t<65?-(t-55)*3:t>=65&&t<75?-30+(t-65)*5:t>=75&&t<85?20-(t-75)*2:Math.sin(x*.06)*3;const y=spectrum?-40*Math.exp(-(((t-65)/17)**2))+Math.sin(x*.11)*4:pulse;points.push([x,126+row*77+y]);}line(points);
  }
  label(spectrum?'SPECTRAL CHANNELS / A · B · C':'BIOMETRIC CHANNELS / ECG · RESP · TEMP',40,80);
 }else if(/BIO/.test(title)){
  for(let n=0;n<3;n++){
   const x=105+n*220;ctx.strokeRect(x-55,225,135,60);line([[x+15,225],[x+15,145]]);
   for(const side of[-1,1]){ctx.beginPath();ctx.ellipse(x+15+side*27,173,34,13,side*.6,0,Math.PI*2);ctx.stroke();}
   label('BED 0'+(n+1),x-30,315);
  }
  line([[50,107],[696,107],[696,255],[680,255]]);label('NUTRIENT SUPPLY / RETURN',50,82);
 }else if(/COMMS|ARCHIVE|CHANNEL|LINK/.test(title)){
  for(let n=0;n<5;n++){
   const y=95+n*46;label('0'+(n+1),35,y+8);line([[95,y],[140,y-12],[178,y+12],[218,y],[270,y]]);
   ctx.fillStyle=n%2?'#284c61':'#376777';ctx.fillRect(305,y-8,120+n*48,16);
   label(['S-BAND','UHF','RELAY','ARCHIVE','INTERNAL'][n],550,y+8);
  }
 }else{
  for(const radius of[60,110,170]){ctx.beginPath();ctx.ellipse(360,193,radius,radius*.57,-.18,0,Math.PI*2);ctx.stroke();}
  ctx.fillStyle='#c5b294';ctx.beginPath();ctx.arc(360,193,26,0,Math.PI*2);ctx.fill();
  ctx.strokeStyle='#c9bb8e';line([[110,285],[240,240],[460,143],[660,89]]);
  for(const [x,y]of[[240,240],[460,143],[660,89]])circle(x,y,7);
  label('JOVIAN TRANSFER',40,80);label('EUROPA',545,244);label('NAV / REFERENCE',485,317);
 }
 ctx.strokeStyle='#345268';ctx.lineWidth=1;ctx.strokeRect(17,52,734,286);
}
