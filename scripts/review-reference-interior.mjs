// Recreate the exact server plans and offline frames on macOS, Linux or Windows.
import {spawnSync} from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
const root=path.resolve(import.meta.dirname,'..'),output=path.join(root,'docs/reference-interior-review');
fs.mkdirSync(output,{recursive:true});
for(const [command,args,env]of[
 ['go',['test','./internal/game','-run','TestReferenceWorldCoverage','-count=1','-v'],{LASTCREW_REVIEW_PLANS:path.join(output,'plans.json')}],
 [process.execPath,['scripts/test-streamed-interior.mjs','--render'],{}],
]){
 const result=spawnSync(command,args,{cwd:root,env:{...process.env,...env},stdio:'inherit'});
 if(result.error)throw result.error;if(result.status!==0)process.exit(result.status||1);
}
const gallery=path.join(root,'docs/interior-review');
fs.mkdirSync(gallery,{recursive:true});
for(const name of fs.readdirSync(output))if(name.endsWith('.png')||['plans.json','streaming-render.json','streaming-check.json','lighting-pixels.json'].includes(name))fs.copyFileSync(path.join(output,name),path.join(gallery,name));
