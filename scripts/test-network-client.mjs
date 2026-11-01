// Real client modules with a fake browser transport; no TCP or secure context.
import assert from 'node:assert/strict';
import {webcrypto} from 'node:crypto';
import {NetworkManager} from '../web/src/net/NetworkManager.js';
import {invitationLinks,readRoomInvitation,createInvitePanel} from '../web/src/net/InviteLinks.js';

assert.equal(readRoomInvitation('?room=abc123'), 'ABC123');
assert.equal(readRoomInvitation('?room=invalid'), '');
assert.equal(readRoomInvitation('?room=%3Cscript%3E'), '');
assert.deepEqual(invitationLinks('http://192.168.1.25:8080',
  ['http://10.8.0.1:8080', 'http://192.168.1.25:8080'], 'ABC123'),
  ['http://192.168.1.25:8080/?room=ABC123', 'http://10.8.0.1:8080/?room=ABC123']);
assert.deepEqual(invitationLinks('http://localhost:8080',
  ['http://127.0.0.1:8080', 'http://[::1]:8080', 'http://0.0.0.0:8080', 'javascript:alert(1)',
    'http://user:pass@192.168.1.25:8080', 'http://192.168.1.25:8080'], 'ABC123'),
  ['http://192.168.1.25:8080/?room=ABC123']);
assert.deepEqual(invitationLinks('https://ship.example', [], 'ABC123'), ['https://ship.example/?room=ABC123']);
assert.deepEqual(invitationLinks('http://localhost:8080', ['http://192.168.1.25:8080'], 'ABC123',
  'https://bright-ship.trycloudflare.com'), ['https://bright-ship.trycloudflare.com/?room=ABC123']);
assert.deepEqual(invitationLinks('https://ship.example', ['http://192.168.1.25:8080'], 'ABC123',
  'javascript:alert(1)'), ['https://ship.example/?room=ABC123','http://192.168.1.25:8080/?room=ABC123']);
assert.deepEqual(invitationLinks('http://[fd00::25]:8080', null, ''), ['http://[fd00::25]:8080/']);
assert.deepEqual(invitationLinks('http://localhost:8080', [], ''), []);

Object.defineProperty(globalThis, 'crypto', {configurable:true, value:{
  getRandomValues: values => webcrypto.getRandomValues(values),
}});
const timers = new Map();let timerID = 0;
globalThis.setTimeout = globalThis.setInterval = fn => {const id=++timerID;timers.set(id,fn);return id;};
globalThis.clearTimeout = globalThis.clearInterval = id => timers.delete(id);
class Socket {
  static OPEN=1;
  constructor(url){this.url=url;this.readyState=0;this.messages=[];}
  send(message){this.messages.push(JSON.parse(message));}
  close(){this.readyState=3;this.onclose?.();}
  receive(type,payload,request_id=''){this.onmessage({data:JSON.stringify({protocol_version:1,type,payload,request_id})});}
}
globalThis.WebSocket=Socket;
for (const [protocol,host,expected] of [
  ['http:','192.168.1.25:8080','ws://192.168.1.25:8080/ws'],
  ['http:','[fd00::25]:8080','ws://[fd00::25]:8080/ws'],
  ['https:','ship.example','wss://ship.example/ws'],
]) {
  globalThis.location={protocol,host};
  const net=new NetworkManager();
  let authRequest;
  globalThis.fetch=async(path,options)=>{authRequest={path,options};return {ok:true,json:async()=>({user:{id:'alice'}})};};
  await net.authenticate('login','alice','test-password');
  assert.equal(authRequest.path,'/api/auth/login');
  assert.equal(authRequest.options.credentials,'same-origin');
  assert.equal(net.socket.url,expected);
  net.socket.readyState=1;net.socket.onopen();
  assert.equal(net.socket.messages[0].type,'player.init');
  net.socket.receive('player.ready',{user:{id:'alice'},capabilities:{movement:true}});
  const pending=net.request('room.join',{code:'ABC123'});
  const join=net.socket.messages.at(-1);
  assert.equal(join.type,'room.join');
  net.socket.receive('room.state',{code:'ABC123'},join.request_id);
  assert.deepEqual(await pending,{code:'ABC123'});
  net.disconnect();
  assert.equal(timers.size,0);
}

const nodes=new Map(['invite-links','invite-room-code'].map(id=>[id,{children:[],append(x){this.children.push(x);}}]));
globalThis.document={getElementById:id=>nodes.get(id),createElement:()=>({})};
globalThis.location={origin:'http://192.168.1.25:8080',protocol:'http:',port:'8080'};
globalThis.fetch=async()=>{throw new TypeError('discovery unavailable');};
let copied;
await createInvitePanel({getRoom:()=>({code:'ABC123'}),modal(){},copyText:text=>copied=text})();
assert.equal(nodes.get('invite-links').children.length,1);
nodes.get('invite-links').children[0].onclick();
assert.equal(copied,'http://192.168.1.25:8080/?room=ABC123');
nodes.get('invite-links').children=[];
globalThis.location={origin:'http://localhost:8080',protocol:'http:',port:'8080'};
globalThis.fetch=async()=>({ok:true,json:async()=>({urls:['https://bright-ship.trycloudflare.com'],public_url:'https://bright-ship.trycloudflare.com'})});
let inviteHTML='';
await createInvitePanel({getRoom:()=>({code:'ABC123'}),modal:html=>inviteHTML=html,copyText:text=>copied=text})();
assert.equal(nodes.get('invite-links').children.length,1);
nodes.get('invite-links').children[0].onclick();
assert.equal(copied,'https://bright-ship.trycloudflare.com/?room=ABC123');
assert.match(inviteHTML,/HTTPS/);
assert.match(inviteHTML,/интернет на обоих компьютерах/);
console.log(JSON.stringify({passed:true,checks:[
  'invitation code normalized and validated','reachable origin preferred over VPN',
  'localhost and invalid invitations excluded','HTTP LAN, IPv6 and HTTPS WebSocket endpoints',
  'same-origin cookie auth','player handshake and room join response correlation',
  'works without randomUUID or SubtleCrypto','invite survives discovery failure'],
  scope:'client modules and simulated transport; no actual browser, network sockets or multiplayer devices'}));
