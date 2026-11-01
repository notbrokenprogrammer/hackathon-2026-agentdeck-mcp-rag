// Compare small, verbatim FIZMA source fixtures; the original archive stays local.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {fileURLToPath} from 'node:url';
import {FIZMA_CURSOR,installCursorGlow} from '../web/src/ui/CursorGlow.js';

const root=fileURLToPath(new URL('../',import.meta.url));
const fixtureRoot=new URL('./fixtures/fizma/',import.meta.url);
const fixtureManifest=JSON.parse(readFileSync(new URL('manifest.json',fixtureRoot),'utf8'));
const sources={};
for(const [name,hash] of Object.entries(fixtureManifest.files)) {
  const bytes=readFileSync(new URL(name,fixtureRoot));
  assert.equal(createHash('sha256').update(bytes).digest('hex'),hash,name+' reference changed');
  sources[name]=bytes.toString('utf8');
}
const fonts=JSON.parse(readFileSync(root+'web/css/fonts/sources.json','utf8'));
const fontCSS=readFileSync(root+'web/css/fonts/geist.css','utf8');
assert.equal((fontCSS.match(/@font-face/g)||[]).length,fonts.length);
assert.ok(!/url\(['"]?https?:/.test(fontCSS),'font requests must stay on the game server');
for(const font of fonts) {
  const face=fontCSS.split('\n').find(line=>line.includes("url('./"+font.file+"')"));
  assert.ok(face?.includes("font-family: '"+font.family+"'"),font.file+' has a valid local family mapping');
  assert.ok(face.includes('font-weight: 100 900')&&face.includes('unicode-range: U+'));
}
assert.equal(fonts.length,11);
for(const font of fonts) {
  const bytes=readFileSync(root+'web/css/fonts/'+font.file);
  assert.equal(bytes.subarray(0,4).toString(),'wOF2');
  assert.equal(bytes.length,font.bytes,font.file+' size differs from the source manifest');
  assert.equal(createHash('sha256').update(bytes).digest('hex'),font.sha256,font.file+' differs from the source manifest');
}
const cursor=sources['components/ui/cursor-glow.tsx'];
const originalSize=cursor.match(/width: isHovering \? "(\d+)px" : "(\d+)px"/);
assert.deepEqual([FIZMA_CURSOR.hover,FIZMA_CURSOR.idle],originalSize.slice(1).map(Number));
assert.equal(FIZMA_CURSOR.coreOpacity,Number(cursor.match(/opacity: isVisible \? (0\.\d+) : 0/)[1]));
assert.match(cursor,/fixed w-8 h-8/);assert.equal(FIZMA_CURSOR.core,8*4);
assert.match(cursor,/hidden lg:block/);assert.equal(FIZMA_CURSOR.breakpoint,1024);
const css=readFileSync(root+'web/css/fizma-entry.css','utf8');
const originalGlow=sources['app/globals.css'].match(/\.cursor-glow \{([\s\S]*?)\n\}/)[1];
const portGlow=css.match(/#auth-screen \.fizma-cursor-glow \{([^}]+)\}/)[1];
const portCore=css.match(/#auth-screen \.fizma-cursor-core \{([^}]+)\}/)[1];
for(const token of ['blur(12px)','transparent 66%','pointer-events: none']) {
  assert.ok(originalGlow.includes(token),token+' missing from reference');
  assert.ok(portGlow.includes(token),token+' differs in port');
}
assert.match(cursor,/filter: "blur\(4px\)"/);assert.ok(portCore.includes('blur(4px)'));
assert.match(cursor,/transparent 70%/);assert.ok(portCore.includes('transparent 70%'));
assert.match(cursor,/opacity 0\.4s ease, width 0\.3s ease, height 0\.3s ease/);
assert.ok(portGlow.includes('opacity .4s ease,width .3s ease,height .3s ease'));
assert.match(cursor,/opacity 0\.2s ease/);assert.ok(portCore.includes('opacity .2s ease'));
assert.match(sources['components/auth/auth-form.tsx'],/max-w-5xl/);
assert.match(sources['components/auth/auth-form.tsx'],/lg:grid-cols-\[0\.9fr_1\.1fr\]/);
assert.ok(css.includes('max-width: 1024px'));assert.ok(css.includes('grid-template-columns: .9fr 1.1fr'));
const emerald=sources['lib/config/themes.ts'].split('  emerald: {')[1].split('  rose: {')[0];
for(const token of ['oklch(0.06 0.015 260)','oklch(0.72 0.16 165)','oklch(0.72 0.16 165 / 0.12)']) {
  assert.ok(emerald.includes(token));assert.ok(css.includes(token));
}
const html=readFileSync(root+'web/index.html','utf8');
const entry=html.slice(html.indexOf('<section id="auth-screen"'),html.indexOf('<div id="application"'));
assert.ok(entry.includes('fizma-terminal-panel')&&entry.includes('fizma-auth-fields'));
assert.ok(!entry.includes('vessel-card')&&!entry.includes('entry-intro'),'old landing must not replace the archive auth form');
for(const id of ['auth-form','username','password','password-confirm','auth-switch','auth-login-tab','auth-register-tab']) {
  assert.equal((html.match(new RegExp('id="'+id+'"','g'))||[]).length,1,id+' must stay unique');
}

class Element {
  constructor(){this.style={};this.listeners=new Map();this.children=[];this.hidden=false;}
  addEventListener(name,fn){this.listeners.set(name,fn);}
  removeEventListener(name){this.listeners.delete(name);}
  fire(name,event={}){this.listeners.get(name)?.(event);}
  setAttribute(){} append(child){this.children.push(child);child.parent=this;}
  remove(){this.parent.children=this.parent.children.filter(child=>child!==this);}
}
const screen=new Element();globalThis.window=new Element();globalThis.document=new Element();
document.body=new Element();document.createElement=()=>new Element();document.pointerLockElement=null;
const frames=new Map();let next=0;
globalThis.requestAnimationFrame=fn=>{const id=++next;frames.set(id,fn);return id;};
globalThis.cancelAnimationFrame=id=>frames.delete(id);
const flush=()=>{const work=[...frames.values()];frames.clear();work.forEach(fn=>fn());};
const glow=installCursorGlow(screen);const [halo,core]=screen.children;
window.fire('mousemove',{clientX:100,clientY:200});window.fire('mousemove',{clientX:140,clientY:210});
assert.equal(frames.size,1,'pointer events coalesce into one frame');flush();
assert.equal(halo.style.left,'140px');assert.equal(core.style.top,'210px');assert.equal(halo.style.opacity,'1');assert.equal(core.style.opacity,'0.15');
document.fire('mouseover',{target:{closest:()=>({})}});assert.equal(halo.style.width,'500px');
document.fire('mouseover',{target:{closest:()=>null}});assert.equal(halo.style.width,'400px');
window.fire('mousemove',{clientX:170,clientY:230});document.body.fire('mouseleave');
assert.equal(frames.size,0);assert.equal(halo.style.opacity,'0');
screen.hidden=true;window.fire('mousemove',{clientX:200,clientY:240});assert.equal(frames.size,0);
screen.hidden=false;document.pointerLockElement={};window.fire('mousemove',{clientX:200,clientY:240});assert.equal(frames.size,0);
document.pointerLockElement=null;glow.dispose();assert.equal(screen.children.length,0);assert.equal(window.listeners.size,0);assert.equal(document.listeners.size,0);assert.equal(document.body.listeners.size,0);
console.log(JSON.stringify({passed:true,reference:'scripts/fixtures/fizma',embeddedFontSubsets:fonts.length,cursor:{...FIZMA_CURSOR,haloBlur:12,coreBlur:4,haloGradientEnd:'66%',coreGradientEnd:'70%'},checks:['reference cursor dimensions, opacity, gradients and transitions','reference emerald palette','reference auth card proportions','original Geist and Geist Mono hashes, including Cyrillic','auth IDs preserved; old landing removed','cursor DOM movement, hover and leave','no glow during game/pointer lock','listener cleanup'],scope:'source comparison and DOM fixture; browser pixel match not measured'}));
