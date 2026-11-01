#!/usr/bin/env python3
"""Real shipped Linux host + HTTP/WebSocket checks. No browser automation."""
import asyncio, contextlib, hashlib, http.cookiejar, json, os, socket, subprocess, tempfile, time, urllib.error, urllib.request
from pathlib import Path
import websockets
ROOT=Path(__file__).resolve().parent.parent
checks=[]
def passed(name): checks.append(name); print('PASS:',name,flush=True)
class Client:
 def __init__(self, base):
  self.base=base;self.jar=http.cookiejar.CookieJar();self.http=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar));self.counter=0;self.ws=None
 def api(self,path,body=None):
  req=urllib.request.Request(self.base+path,data=json.dumps(body).encode() if body is not None else None,headers={'Content-Type':'application/json','Accept':'text/html,application/json','Origin':self.base})
  with self.http.open(req,timeout=8) as r: return r.status,r.read()
 async def connect(self):
  self.ws=await websockets.connect(self.base.replace('http','ws')+'/ws',origin=self.base,additional_headers={'Cookie':'; '.join(c.name+'='+c.value for c in self.jar)},max_size=2**22)
  return await self.call('player.init')
 async def call(self,kind,payload=None,rid=None,error=False):
  self.counter+=1;rid=rid or 'ui-test-'+str(self.counter)
  await self.ws.send(json.dumps({'protocol_version':1,'type':kind,'request_id':rid,'payload':payload or {}}))
  async with asyncio.timeout(20):
   while True:
    message=json.loads(await self.ws.recv())
    if message.get('request_id')==rid:
     if error: assert message['type']=='error',message
     else: assert message['type']!='error',message
     return message['payload']
 async def close(self):
  if self.ws: await self.ws.close();self.ws=None
async def main():
 with socket.socket() as sock: sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
 base=f'http://127.0.0.1:{port}';process=None;clients=[]
 with tempfile.TemporaryDirectory(prefix='.frontend-check-',dir=ROOT) as data:
  log=open(Path(data)/'server.log','w')
  def start():
   nonlocal process
   process=subprocess.Popen(['bash',str(ROOT/'Start.sh')],cwd=ROOT,stdout=log,stderr=log,env=dict(os.environ,HOST_ADDR=f'127.0.0.1:{port}',DATA_DIR=data,DEV_WEB_DIR=str(ROOT/'web'),OLLAMA_ENABLED='false'))
   for _ in range(100):
    try:
     with urllib.request.urlopen(base+'/healthz',timeout=1) as r:
      if r.status==200:return
    except (OSError,urllib.error.URLError): time.sleep(.05)
   raise RuntimeError('Host failed: '+(Path(data)/'server.log').read_text()[-2000:])
  def stop():
   nonlocal process
   if process:process.terminate();process.wait(timeout=8);process=None
  try:
   start();passed('Linux host starts with external updated frontend')
   plain=Client(base)
   for asset in ['index.html','css/style.css','css/flight-deck.css','src/main.js','src/ui/AuthScreen.js','src/ui/FlightDeck.js','src/scene.js','src/crew/BridgeCrew.js']:
    status,body=plain.api('/'+asset);assert status==200 and body==(ROOT/'web'/asset).read_bytes(),asset
   for page in ['/register','/login']:
    status,body=plain.api(page);assert b'flight-deck.css' in body
   passed('HTTP bytes match all changed frontend files; auth routes work')
   try:
    await websockets.connect(base.replace('http','ws')+'/ws',origin=base)
    raise AssertionError('Anonymous WebSocket accepted')
   except websockets.exceptions.InvalidStatus as e:assert e.response.status_code==401
   passed('Anonymous WebSocket rejected')
   for name in ['qa_captain','qa_engineer','qa_isolated']:
    c=Client(base);status,body=c.api('/api/auth/register',{'username':name,'password':'disposable-test-only'});assert status==201
    assert json.loads(c.api('/api/auth/me')[1])['user']['username']==name
    await c.connect();clients.append(c)
   a,b,other=clients;passed('Three independent accounts register and restore HTTP sessions')
   room=await a.call('room.create',{'name':'Проверка нового фронтенда'});code=room['code']
   await b.call('room.join',{'code':code});await other.call('room.create',{'name':'Другая партия'})
   await a.call('room.role',{'role':'captain'});await b.call('room.role',{'role':'captain'},error=True);await b.call('room.role',{'role':'engineer'})
   await a.call('room.ready',{'ready':True});await b.call('room.ready',{'ready':True});await a.call('room.start')
   passed('Room join, role occupancy, readiness, start')
   before=await a.call('game.action',{'action':'get_state'});version=before['version']
   preview=await b.call('game.preview',{'action':'repair_cooling','expected_version':version});assert preview['allowed']
   await b.call('game.action',{'action':'repair_cooling','expected_version':version},rid='repair-once')
   await b.call('game.action',{'action':'repair_cooling','expected_version':version},rid='repair-once')
   sa=await a.call('game.action',{'action':'get_state'});sb=await b.call('game.action',{'action':'get_state'})
   assert sa==sb and sa['energy']==preview['after']['energy'] and sa['version']==version+1 and sa['cooling_repaired']
   isolated=await other.call('game.action',{'action':'get_state'});assert not isolated['cooling_repaired']
   passed('Server preview, shared repair, idempotence, room isolation')
   await a.call('chat.send',{'text':'Контур восстановлен. Подтвердите показания.'})
   rb=await b.call('room.get');assert rb['chat'][-1]['text']=='Контур восстановлен. Подтвердите показания.'
   passed('Team chat delivered to second client')
   ratios=[];system_types=set()
   for target in ['cryo','bridge','reactor','bio']:
    await a.call('player.teleport',{'target':target})
    plan=json.loads(a.api('/api/world/interest')[1]);ratio=sum(r['size'] for r in plan['resources'])/plan['world_bytes'];ratios.append(ratio);assert ratio<=.1
    for res in plan['resources']:
     raw=a.api('/api/world/resource/'+res['id'])[1];assert len(raw)==res['size'] and hashlib.sha256(raw).hexdigest()==res['hash']
     value=json.loads(raw)
     if value['kind']=='object':
      def walk(o):
       if o.get('userData',{}).get('system'):system_types.add(o['userData']['system'])
       for child in o.get('children',[]):walk(child)
      walk(value['body'])
   assert {'reactor','cooling','greenhouse','grow-lamp','power'}<=system_types,system_types
   passed('Four deck routes: updated world hashes, live system tags and cache budget <=10%')
   for npc in ['savinykh','dzhanibekov','ryazansky','krikalev']:
    reply=await a.call('agent.talk',{'agent_id':npc,'text':'Что делать дальше после ремонта охлаждения?'})
    assert reply.get('text'),reply
    assert reply.get('mode') in ['rules','knowledge_base'],reply.get('mode')
    assert reply.get('tool_transport')=='MCP stdio',reply.get('tool_transport')
   passed('All four mentors respond in fallback mode through real MCP stdio')
   for c in clients:await c.close()
   stop();start();await a.connect();await b.connect()
   restored=await a.call('game.action',{'action':'get_state'});assert restored==sa
   room_after=await b.call('room.get');assert room_after['code']==code and room_after['started']
   hist=await a.call('dialogue.history',{'agent_id':'savinykh'});assert len(hist['messages'])>=2
   passed('Process restart restores session, room, state, dialogue')
   await b.close();b.api('/api/auth/logout',{})
   try:b.api('/api/auth/me');raise AssertionError('Logout failed')
   except urllib.error.HTTPError as e:assert e.code==401
   b.api('/api/auth/login',{'username':'qa_engineer','password':'disposable-test-only'});await b.connect()
   assert (await b.call('room.get'))['code']==code
   passed('Logout revokes session; login resumes saved room')
   for action in ['start_reactor','use_battery','start_greenhouse','restore_archive','calculate_course']:
    current=await a.call('game.action',{'action':'get_state'})
    await a.call('game.action',{'action':action,'expected_version':current['version']})
   current=await a.call('game.action',{'action':'get_state'})
   solved=await a.call('investigation.solve',{'clues':['damaged_filter','impact_mark','protective_chronology'],'expected_version':current['version']});assert solved['solved']
   current=await a.call('game.action',{'action':'get_state'})
   proposed=await a.call('game.action',{'action':'final_ecosystem','expected_version':current['version']});assert proposed.get('pending')
   await b.call('decision.vote',{'approve':True,'expected_version':current['version']})
   success=await a.call('game.action',{'action':'get_state'});assert success['game_over'] and success['victory']
   passed('Full quest path, investigation and successful two-player final vote')
   await a.call('room.reset');await a.call('room.ready',{'ready':True});await b.call('room.ready',{'ready':True});await a.call('room.start')
   for action in ['use_battery','start_reactor','wait_cycle','wait_cycle']:
    current=await a.call('game.action',{'action':'get_state'});await a.call('game.action',{'action':action,'expected_version':current['version']})
   current=await a.call('game.action',{'action':'get_state'});await a.call('game.action',{'action':'final_emergency','expected_version':current['version']});await b.call('decision.vote',{'approve':True,'expected_version':current['version']})
   failure=await a.call('game.action',{'action':'get_state'});assert failure['game_over'] and not failure['victory']
   passed('Mission reset and failed ending on unsafe reactor path')
   result={'passed':True,'checks':checks,'world_max_ratio':max(ratios),'renderer':'not tested in browser','binary':'original Linux binary, updated external web and world; no recompilation'}
   out=ROOT/'docs/frontend/integration-results.json';out.write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n');print(json.dumps(result,ensure_ascii=False))
  finally:
   for c in clients:
    with contextlib.suppress(Exception):await c.close()
   stop();log.close()
asyncio.run(main())
