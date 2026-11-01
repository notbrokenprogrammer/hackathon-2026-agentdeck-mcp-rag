// Dependency-free DOM fixture. Tests interaction logic, not browser layout.
import assert from 'node:assert/strict';
import {AuthScreen} from '../web/src/ui/AuthScreen.js';
class Element {
 constructor(){this.value='';this.type='text';this.hidden=false;this.disabled=false;this.dataset={};this.attrs={};this.listeners={};this.children=[];this.classes=new Set();this.classList={toggle:(name,on)=>on?this.classes.add(name):this.classes.delete(name)};}
 setAttribute(k,v){this.attrs[k]=v;} removeAttribute(k){delete this.attrs[k];}
 addEventListener(k,v){this.listeners[k]=v;} closest(){return this;}
 focus(){globalThis.focused=this;} querySelectorAll(){return this.children;}
 click(){if(!this.disabled)this.onclick?.();}
}
const nodes=new Map();
globalThis.document={getElementById(id){if(!nodes.has(id))nodes.set(id,new Element());return nodes.get(id);}};
const $=document.getElementById;
globalThis.location={pathname:'/register',search:'?room=ABC123',protocol:'https:'};
globalThis.history={replaceState(a,b,path){this.path=path;}};
const stored=new Map(),events=new Map();
globalThis.localStorage={getItem:key=>stored.get(key),setItem:(key,value)=>stored.set(key,value)};
globalThis.window={scrollY:0,addEventListener:(name,callback)=>events.set(name,callback)};
const mobileHome=new Element(),mobileGuide=new Element(),mobileTheme=new Element();
mobileGuide.dataset.entryTrigger='entry-controls';mobileTheme.dataset.entryTrigger='fizma-theme-toggle';
document.querySelectorAll=selector=>selector==='[data-entry-trigger]'?[mobileGuide,mobileTheme]:selector==='#fizma-mobile-menu a'?[mobileHome]:[];
document.querySelector=selector=>selector==='.fizma-header'?$('entry-header'):null;
$('fizma-mobile-menu').hidden=true;
$('password').type='password';$('password-strength').dataset={};$('password-strength').children=Array.from({length:4},()=>new Element());
let calls=0,result=null,complete;
const network={authenticate:()=>{calls++;return new Promise(resolve=>complete=resolve);}};
const ui=new AuthScreen(network,u=>result=u);
assert.equal($('auth-screen').dataset.theme,'dark');assert.equal($('fizma-session-mode').textContent,'secure');
$('fizma-theme-toggle').click();assert.equal($('auth-screen').dataset.theme,'light');assert.equal(stored.get('lastcrew-entry-mode'),'light');assert.match($('fizma-theme-toggle').attrs['aria-label'],/тёмную/);
$('fizma-menu-toggle').click();assert.equal($('fizma-mobile-menu').hidden,false);assert.equal($('fizma-menu-toggle').attrs['aria-expanded'],'true');assert.match($('fizma-menu-toggle').innerHTML,/M6 6l12 12/);
mobileHome.click();assert.equal($('fizma-mobile-menu').hidden,true);assert.equal($('fizma-menu-toggle').attrs['aria-expanded'],'false');
let guides=0;$('entry-controls').onclick=()=>guides++;
$('fizma-menu-toggle').click();mobileGuide.click();assert.equal(guides,1);assert.equal($('fizma-mobile-menu').hidden,true);
$('fizma-menu-toggle').click();mobileTheme.click();assert.equal($('auth-screen').dataset.theme,'dark');assert.equal($('fizma-mobile-menu').hidden,true);
window.scrollY=19;events.get('scroll')();assert.ok($('entry-header').classes.has('scrolled'));
window.scrollY=0;events.get('scroll')();assert.ok(!$('entry-header').classes.has('scrolled'));
assert.equal($('confirm-field').hidden,false);
$('username').value='bad name';$('password').value='secret123';$('password-confirm').value='secret123';
await ui.submit();assert.equal(calls,0);assert.equal($('username').attrs['aria-invalid'],'true');
$('username').value='navigator';$('password-confirm').value='typo';
await ui.submit();assert.equal(calls,0);assert.equal($('password-confirm').attrs['aria-invalid'],'true');
$('password-confirm').value='secret123';const first=ui.submit();await ui.submit();assert.equal(calls,1);assert.equal($('auth-submit').disabled,true);
ui.setMode(false);assert.equal(ui.register,true,'cannot change operation while request is in flight');
complete({username:'navigator'});await first;assert.deepEqual(result,{username:'navigator'});assert.equal($('password').value,'');assert.equal($('password-confirm').value,'');assert.equal($('auth-submit').disabled,false);
ui.setMode(false);assert.equal(history.path,'/login?room=ABC123');assert.equal($('confirm-field').hidden,true);
$('password-toggle').onclick();assert.equal($('password').type,'text');assert.equal($('password-toggle').attrs['aria-pressed'],'true');
$('password-toggle').onclick();assert.equal($('password').type,'password');
$('password').value='😀'.repeat(20);await ui.submit();assert.equal(calls,1,'reject passwords longer than bcrypt limit');
$('password').value='validPassword';network.authenticate=async()=>{throw Object.assign(new Error('duplicate'),{status:409});};await ui.submit();assert.match($('username-error').textContent,/занят/);assert.equal($('auth-submit').disabled,false);
network.authenticate=async()=>{throw Object.assign(new Error('unauthorized'),{status:401});};await ui.submit();assert.match($('auth-error').textContent,/не совпали/);
network.authenticate=async()=>{throw new TypeError('Failed to fetch');};await ui.submit();assert.match($('auth-error').textContent,/Нет связи/);
console.log(JSON.stringify({passed:true,checks:['entry theme persistence and desktop/mobile controls','mobile HOME, guide and theme close menu','menu icon and accessible state','header scroll styling','HTTPS session label','invalid callsign','confirmation mismatch','duplicate submit prevented','mode change blocked while submitting','credentials cleared','invitation preserved','password visibility','bcrypt byte limit','duplicate callsign feedback','wrong credentials feedback','offline feedback'],scope:'DOM fixture; no browser layout or rendering assertions'}));
