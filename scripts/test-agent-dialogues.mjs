// Test actual dialogue controller, selector events and main.js submit handler.
// No sockets, browser rendering or local model weights are used.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
import {AgentDialogues,DIALOGUE_MODES,messageDialogueMode,installDialogueModeSelector} from '../web/src/ui/AgentDialogues.js';

class Element {
 constructor(mode){this.dataset={dialogueMode:mode};this.attrs={};this.hidden=false;this.value='';this.textContent='';}
 setAttribute(key,value){this.attrs[key]=value;}
 click(){return this.onclick?.();}
}
function fixture(saved='articles') {
 const stored=new Map([['lastcrew-dialogue-mode',saved]]),calls=[],nodes=new Map();
 for(const id of ['dialogue-mode-selector','dialogue-mode-description','dialogue-history-retry'])nodes.set(id,new Element());
 const buttons=Object.keys(DIALOGUE_MODES).map(mode=>new Element(mode));
 const network={request(type,payload){return new Promise((resolve,reject)=>calls.push({type,payload,resolve,reject}));}};
 const dialogues=new AgentDialogues({network,storage:{getItem:key=>stored.get(key),setItem:(key,value)=>stored.set(key,value)}});
 const doc={getElementById:id=>nodes.get(id),querySelectorAll:()=>buttons};
 const render=installDialogueModeSelector({dialogues,doc});
 let team=false;dialogues.onChange=()=>render({team});render();
 return {dialogues,network,calls,nodes,buttons,stored,setTeam(value){team=value;render({team});}};
}
const ui=fixture('unknown-mode'),d=ui.dialogues;
assert.equal(d.mode,'articles','new or invalid preference defaults to articles');
assert.equal(ui.buttons[0].attrs['aria-pressed'],'true');
assert.match(ui.nodes.get('dialogue-mode-description').textContent,/со ссылками/);
d.setScope('ABC123:1');
const firstHistory=d.select('savinykh');
assert.deepEqual(ui.calls[0].payload,{agent_id:'savinykh',dialogue_mode:'articles'});
assert.equal(d.historyLoading,true);assert.equal(d.canSend,false);
assert.equal(await d.send('too early'),false);assert.equal(ui.calls.length,1);
const casualHistory=ui.buttons.find(b=>b.dataset.dialogueMode==='casual').click();
assert.equal(d.mode,'casual');assert.equal(ui.stored.get('lastcrew-dialogue-mode'),'casual');
assert.equal(ui.buttons.find(b=>b.dataset.dialogueMode==='casual').attrs['aria-pressed'],'true');
assert.match(ui.nodes.get('dialogue-mode-description').textContent,/не используются/);
ui.calls[0].resolve({messages:[{role:'user',text:'article question',dialogue_mode:'articles'},{role:'assistant',text:'article answer',reply:{dialogue_mode:'articles'}}]});
await firstHistory;
assert.deepEqual(d.rows,[],'late article history cannot populate the selected casual thread');
assert.equal(d.historyLoading,true);
ui.calls[1].resolve({messages:[{role:'assistant',text:'casual history',mode:'casual'}]});await casualHistory;
assert.deepEqual(d.rows.map(row=>row.text),['casual history']);assert.equal(d.canSend,true);

const casualSend=d.send('How is the crew?');
assert.deepEqual(ui.calls.at(-1).payload,{agent_id:'savinykh',text:'How is the crew?',dialogue_mode:'casual'});
assert.equal(d.busy,true);assert.equal(await d.send('duplicate'),false);
const casualRequest=ui.calls.at(-1);
const nextMentor=d.select('dzhanibekov','articles');
assert.equal(d.busy,false,'another mentor/mode does not display the old busy state');
ui.calls.at(-1).resolve({messages:[]});await nextMentor;
const articleSend=d.send('Explain time dilation');const articleRequest=ui.calls.at(-1);
assert.equal(articleRequest.payload.dialogue_mode,'articles');assert.equal(d.busy,true);
casualRequest.resolve({text:'Casual response',dialogue_mode:'casual',mode:'model',grounding:'conversation'});await casualSend;
assert.deepEqual(d.rows.map(row=>row.text),['Explain time dilation'],'late response keeps captured mentor and mode');
assert.equal(d.busy,true,'one completed request does not clear another thread’s pending state');
assert.equal(d.histories.get('savinykh:casual').at(-1).text,'Casual response');
articleRequest.resolve({text:'Article response',dialogue_mode:'articles',mode:'model',retrieval:{mode:'hybrid'}});await articleSend;
assert.equal(d.busy,false);assert.equal(d.rows.at(-1).reply.retrieval.mode,'hybrid');
await d.select('savinykh','articles');
assert.deepEqual(d.rows.map(row=>row.text),['article question','article answer']);
assert.deepEqual([...ui.stored.keys()],['lastcrew-dialogue-mode'],'conversation text is never placed in local storage');
ui.setTeam(true);assert.equal(ui.nodes.get('dialogue-mode-selector').hidden,true);assert.equal(d.mode,'articles');
ui.setTeam(false);assert.equal(ui.nodes.get('dialogue-mode-selector').hidden,false);

const failedSend=d.send('question that fails');ui.calls.at(-1).reject(new Error('connection lost'));await failedSend;
assert.match(d.rows.at(-1).text,/По статьям: connection lost/);assert.equal(d.canSend,true);
const pendingOld=d.send('old expedition');const oldRequest=ui.calls.at(-1);
d.setScope('OTHER:2');const newHistory=d.select('savinykh','articles');const newRequest=ui.calls.at(-1);
oldRequest.resolve({text:'must not leak',dialogue_mode:'articles'});await pendingOld;
assert.deepEqual(d.rows,[]);assert.equal(d.historyLoading,true);
newRequest.reject(new Error('offline'));await newHistory;
assert.match(d.historyError,/По статьям/);assert.equal(d.canSend,false);assert.equal(ui.nodes.get('dialogue-history-retry').hidden,false);
const retry=ui.nodes.get('dialogue-history-retry').click();ui.calls.at(-1).resolve({messages:[]});await retry;
assert.equal(d.canSend,true);assert.equal(ui.nodes.get('dialogue-history-retry').hidden,true);
const lateHistory=d.select('krikalev','casual');const lateRequest=ui.calls.at(-1);d.setScope('');
lateRequest.resolve({messages:[{text:'old account'}]});await lateHistory;assert.equal(d.histories.size,0);assert.equal(d.canSend,false);
assert.equal(messageDialogueMode({mode:'casual'},'articles'),'casual');
assert.equal(messageDialogueMode({mode:'casual',dialogue_mode:'casual',reply:{dialogue_mode:'articles'}},'casual'),'articles');
assert.equal(fixture('casual').dialogues.mode,'casual');
const denied=new AgentDialogues({network:ui.network,storage:{getItem(){throw new Error('blocked');},setItem(){throw new Error('blocked');}}});
assert.equal(denied.mode,'articles');await denied.select('savinykh','casual');assert.equal(denied.mode,'casual');

// Execute the production form handler in isolation: team requests stay unchanged,
// while mentor requests are delegated to the mode-aware controller above.
const source=readFileSync(new URL('../web/src/main.js',import.meta.url),'utf8');
const submitSource=source.split("$('chat-form').onsubmit=")[1].split("$('chat-input').addEventListener")[0];
const input=new Element(),submitted=[],notices=[];
let resolveTeam;
const context=vm.createContext({chatMode:'team',teamBusy:false,$:()=>input,ship:{reactToSpeech(){}},renderChat(){},toast:text=>notices.push(text),network:{request(type,payload){submitted.push({type,payload});return new Promise(resolve=>resolveTeam=resolve);}},dialogues:{canSend:true,async send(text){submitted.push({type:'controller',text});}}});
new vm.Script('handler='+submitSource).runInContext(context);
input.value='team message';const teamSend=context.handler({preventDefault(){}});
assert.equal(submitted[0].type,'chat.send');assert.equal(JSON.stringify(submitted[0].payload),'{"text":"team message"}');
assert.equal(input.value,'');context.chatMode='npc';input.value='new private draft';resolveTeam({});await teamSend;
assert.equal(input.value,'new private draft','late team response does not erase a mentor draft');
await context.handler({preventDefault(){}});assert.equal(submitted.at(-1).type,'controller');assert.equal(submitted.at(-1).text,'new private draft');
context.dialogues.canSend=false;input.value='unsent';await context.handler({preventDefault(){}});assert.equal(input.value,'unsent');
assert.equal(notices.length,0);
const html=readFileSync(new URL('../web/index.html',import.meta.url),'utf8');
assert.ok(html.indexOf('id="dialogue-mode-selector"')<html.indexOf('id="chat-input"'),'mode selection appears before the question field');
for(const mode of ['articles','casual','mission'])assert.ok(html.includes('data-dialogue-mode="'+mode+'"'));
assert.notEqual(DIALOGUE_MODES.articles.busy,DIALOGUE_MODES.casual.busy);
console.log(JSON.stringify({passed:true,checks:['explicit request/history mode','persistent mode preference only','separate mentor/mode histories','late history and reply routing','scope/logout invalidation','per-thread busy and duplicate prevention','mode-specific errors and history retry','accessible selector and team exclusion','production form: unchanged team payload and private draft retention','no secret or message storage'],scope:'controller and DOM/client fixtures, no browser rendering or inference'}));

const commands=fixture('mission');commands.dialogues.setScope('ROOM:1');
const commandHistory=commands.dialogues.select('dzhanibekov','mission');commands.calls.at(-1).resolve({messages:[]});await commandHistory;
const issued=commands.dialogues.send('Восстанови охлаждение');
assert.equal(commands.calls.at(-1).payload.dialogue_mode,'mission');
commands.calls.at(-1).resolve({dialogue_mode:'mission',mode:'server-command',text:'Выполнено',command:{status:'completed',action:'repair_cooling'}});await issued;
assert.equal(commands.dialogues.rows.at(-1).reply.command.status,'completed');
assert.equal(commands.dialogues.histories.has('dzhanibekov:articles'),false);
