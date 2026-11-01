// Controlled comparison of the former two-at-a-time request strategy and the
// bounded worker pool. Uses real bridge assets with artificial transport delay;
// it measures loading, not network throughput or browser rendering performance.
import fs from 'node:fs';
import {registerHooks} from 'node:module';
registerHooks({resolve(s,c,next){return s==='three'?{url:new URL('../web/vendor/three.module.js',import.meta.url).href,shortCircuit:true}:next(s,c);}});
const THREE=await import('../web/vendor/three.module.js');
const {StreamingWorld}=await import('../web/src/world/StreamingWorld.js');
const root=new URL('../',import.meta.url),manifest=JSON.parse(fs.readFileSync(new URL('world/manifest.json',root))),plan=JSON.parse(fs.readFileSync(new URL('docs/reference-interior-review/plans.json',root))).bridge;
if(plan.version!==manifest.version)throw new Error('Regenerate reference review plans after building the world.');
const latency=Number(process.argv.find(a=>a.startsWith('--latency='))?.slice(10)??10);
if(!Number.isFinite(latency)||latency<0||latency>1000)throw new Error('Use --latency=0..1000 milliseconds.');
const payloads=new Map(plan.resources.map(r=>[r.id,fs.readFileSync(new URL('world/'+r.id+'.json',root))]));
globalThis.window={dispatchEvent(){}};
const results=[];
for(const strategy of ['paired-2','pool-6']){
 const ship={scene:new THREE.Scene(),mode:'explore',player:new THREE.Vector3(),crew:{register(){},unregister(){}},interior:{attachChunk(){},detachChunk(){}},schematicGroup:new THREE.Group(),installSchematic(){},installAvatarTemplate(){},updateState(){}};
 let requests=0,inFlight=0,peakRequests=0;
 globalThis.fetch=async url=>{if(url==='/api/world/interest')return new Response(JSON.stringify(plan));requests++;peakRequests=Math.max(peakRequests,++inFlight);await new Promise(resolve=>setTimeout(resolve,latency));inFlight--;return new Response(payloads.get(url.split('/').at(-1)));};
 const stream=new StreamingWorld(ship);stream.meta={...manifest,limit:Math.floor(manifest.world_bytes*.30)};
 if(strategy==='paired-2')stream.download=async function(resources,budget,query){for(let i=0;i<resources.length;i+=2){const results=await Promise.allSettled(resources.slice(i,i+2).map(r=>this.get(r,budget,query)));const failure=results.find(r=>r.status==='rejected');if(failure)throw failure.reason;}};
 const start=performance.now();await stream.load();
 results.push({strategy,load_ms:+(performance.now()-start).toFixed(1),requests,peak_requests:peakRequests,resident_bytes:stream.C,peak_bytes:stream.peak,assembly_yields:stream.loadYields});
 for(const kind of ['geometries','materials','textures'])for(const asset of Object.values(stream.parsed[kind]))asset.dispose?.();
}
console.log(JSON.stringify({scope:'real bridge plan, local assets, artificial per-request latency; both strategies use current verification and parser; not browser FPS',latency_ms:latency,world_version:manifest.version,results},null,2));
