// SHA-256 fallback for ordinary HTTP LAN origins without SubtleCrypto.
export function sha256(bytes){
 const primes=[];for(let n=2;primes.length<64;n++){let prime=true;for(let d=2;d*d<=n;d++)if(n%d===0){prime=false;break;}if(prime)primes.push(n);}
 const H=primes.slice(0,8).map(n=>(Math.sqrt(n)%1*4294967296)|0),K=primes.map(n=>(Math.cbrt(n)%1*4294967296)|0);const n=Math.ceil((bytes.length+9)/64)*64,padded=new Uint8Array(n);padded.set(bytes);padded[bytes.length]=128;const view=new DataView(padded.buffer);view.setUint32(n-8,Math.floor(bytes.length/536870912));view.setUint32(n-4,(bytes.length*8)>>>0);const words=new Int32Array(64),rot=(v,n)=>(v>>>n)|(v<<(32-n));
 for(let offset=0;offset<n;offset+=64){for(let i=0;i<16;i++)words[i]=view.getInt32(offset+i*4);for(let i=16;i<64;i++){const a=words[i-15],b=words[i-2];words[i]=(words[i-16]+(rot(a,7)^rot(a,18)^(a>>>3))+words[i-7]+(rot(b,17)^rot(b,19)^(b>>>10)))|0;}let[a,b,c,d,e,f,g,h]=H;for(let i=0;i<64;i++){const t1=(h+(rot(e,6)^rot(e,11)^rot(e,25))+((e&f)^(~e&g))+K[i]+words[i])|0,t2=((rot(a,2)^rot(a,13)^rot(a,22))+((a&b)^(a&c)^(b&c)))|0;h=g;g=f;f=e;e=(d+t1)|0;d=c;c=b;b=a;a=(t1+t2)|0;}for(const[i,v]of[a,b,c,d,e,f,g,h].entries())H[i]=(H[i]+v)|0;}
 return H.map(v=>(v>>>0).toString(16).padStart(8,'0')).join('');
}
