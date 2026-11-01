export class NetworkManager {
 constructor(){this.socket=null;this.currentUser=null;this.capabilities=null;this.isConnected=false;this.ready=false;this.listeners=new Map();this.pending=new Map();this.sequence=0;this.requestId=0;this.nonce=Array.from(crypto.getRandomValues(new Uint32Array(3)),x=>x.toString(16)).join('-');this.generation=0;this.stopped=true;this.attempt=0;this.pings=new Map();}
 on(type,fn){if(!this.listeners.has(type))this.listeners.set(type,new Set());this.listeners.get(type).add(fn);return()=>this.listeners.get(type)?.delete(fn);}
 emit(type,...data){for(const fn of this.listeners.get(type)||[])fn(...data);}
 async api(path,body){const response=await fetch('/api/auth/'+path,{method:body?'POST':'GET',credentials:'same-origin',headers:body?{'Content-Type':'application/json'}:{},body:body?JSON.stringify(body):undefined});const data=await response.json().catch(()=>({}));if(!response.ok){const e=new Error(data.message||'Сервер недоступен');e.status=response.status;throw e;}return data;}
 async authenticate(mode,username,password){const d=await this.api(mode,{username,password});this.currentUser=d.user;this.stopped=false;this.connect();return d.user;}
 async resume(){try{const d=await this.api('me');this.currentUser=d.user;this.stopped=false;this.connect();return d.user;}catch(e){if(e.status!==401)this.emit('notice',e.message);return null;}}
 connect(){if(this.stopped)return;if(this.socket&&this.socket.readyState<2)return;clearTimeout(this.retryTimer);const gen=++this.generation;this.emit('status','connecting');const ws=new WebSocket(`${location.protocol==='https:'?'wss':'ws'}://${location.host}/ws`);this.socket=ws;
 const timeout=setTimeout(()=>{if(gen===this.generation&&!this.ready)ws.close();},7000);
 ws.onopen=()=>{if(gen!==this.generation)return;this.isConnected=true;this.sequence=0;this.send('player.init',{client_time:Date.now()});};
 ws.onmessage=e=>{if(gen!==this.generation)return;let msg;try{msg=JSON.parse(e.data);}catch{return;}if(msg.protocol_version!==1)return;
 if(msg.type==='player.ready'){clearTimeout(timeout);this.currentUser=msg.payload.user;this.capabilities=msg.payload.capabilities;this.ready=true;this.attempt=0;this.emit('status','online');this.ping();this.heartbeat=setInterval(()=>this.ping(),5000);}
 if(msg.type==='pong'&&this.pings.has(msg.request_id)){this.emit('latency',Math.round(performance.now()-this.pings.get(msg.request_id)));this.pings.delete(msg.request_id);}
 const pending=this.pending.get(msg.request_id);if(pending){clearTimeout(pending.timer);this.pending.delete(msg.request_id);if(msg.type==='error')pending.reject(Object.assign(new Error(msg.payload.message),{code:msg.payload.code}));else pending.resolve(msg.payload);}
 else if(msg.type==='error')this.emit('notice',msg.payload.message);
 this.emit(msg.type,msg.payload,msg);};
 ws.onclose=()=>{clearTimeout(timeout);if(gen!==this.generation)return;clearInterval(this.heartbeat);this.isConnected=false;this.ready=false;this.capabilities=null;this.pings.clear();this.rejectPending('Соединение потеряно. Проверьте состояние перед повтором операции.');if(!this.stopped){this.emit('status','offline');this.scheduleReconnect();}};
 ws.onerror=()=>this.emit('status','offline');
 }
 ping(){const now=performance.now();for(const[id,at]of this.pings)if(now-at>15000)this.pings.delete(id);const id=this.send('ping',{timestamp:Date.now()});if(id)this.pings.set(id,now);}
 scheduleReconnect(){clearTimeout(this.retryTimer);const delay=Math.min(1000*2**Math.min(this.attempt++,4),12000);this.retryTimer=setTimeout(async()=>{if(this.stopped)return;try{const me=await this.api('me');this.currentUser=me.user;this.connect();}catch(e){if(e.status===401){this.disconnect();this.emit('expired');}else{this.emit('status','offline');this.scheduleReconnect();}}},delay);}
 send(type,payload={}){if(!this.socket||this.socket.readyState!==WebSocket.OPEN)return null;const id=this.nonce+'-'+String(++this.requestId);this.socket.send(JSON.stringify({protocol_version:1,type,request_id:id,payload}));return id;}
 request(type,payload={}){return new Promise((resolve,reject)=>{if(!this.ready){reject(new Error('Дождитесь подключения к кораблю'));return;}const id=this.send(type,payload);if(!id){reject(new Error('Нет соединения'));return;}const timer=setTimeout(()=>{this.pending.delete(id);reject(new Error('Ответ не получен. Проверьте состояние корабля перед повтором.'));},type==='agent.talk'?85000:12000);this.pending.set(id,{resolve,reject,timer});});}
 movement(position,yaw){if(this.ready&&this.capabilities?.movement)this.send('player.input',{seq:++this.sequence,x:position.x,y:position.y,z:position.z,yaw});}
 reconnectNow(){if(this.socket&&this.socket.readyState<2)return;this.stopped=false;this.connect();}
 rejectPending(message){for(const p of this.pending.values()){clearTimeout(p.timer);p.reject(new Error(message));}this.pending.clear();}
 disconnect(){this.stopped=true;this.generation++;clearTimeout(this.retryTimer);clearInterval(this.heartbeat);this.socket?.close(1000,'logout');this.socket=null;this.ready=false;this.isConnected=false;this.pings.clear();this.rejectPending('Сеанс завершён');this.emit('status','offline');}
 async logout(){this.disconnect();await this.api('logout',{});this.currentUser=null;}
}
