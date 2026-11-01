// Test the real module's behavior with a DOM fixture; this does not assert
// browser layout or prove that local model weights can generate an answer.
import assert from 'node:assert/strict';
import {installAgentStatus,modelReplyLabel,retrievalReplyHint,retrievalStatusText} from '../web/src/ui/AgentStatus.js';

class Element {
 constructor(){this.listeners=new Map();this.removed=false;this.textContent='';}
 addEventListener(event,fn){this.listeners.set(event,fn);}
 after(element){this.afterElement=element;}
 remove(){this.removed=true;}
 fire(event){return this.listeners.get(event)?.();}
 set innerHTML(_){throw new Error('Service-supplied text must not become HTML');}
}
function fixture(fetcher){
 const nodes=new Map([['npc-profile',new Element()]]),modals=[];
 const doc={getElementById:id=>nodes.get(id),createElement:()=>new Element()};
 const modal=html=>{
  modals.push(html);
  for(const [,id] of html.matchAll(/\bid="([^"]+)"/g))nodes.set(id,new Element());
 };
 const dispose=installAgentStatus({doc,fetcher,modal});
 return {nodes,modals,dispose,button:nodes.get('npc-profile').afterElement};
}

let complete,calls=0;
const ready=fixture((path,options)=>{
 calls++;assert.equal(path,'/api/agents/status');
 assert.equal(options.credentials,'same-origin');assert.equal(options.cache,'no-store');
 return new Promise(resolve=>{complete=resolve;});
});
assert.equal(ready.button.type,'button');assert.equal(ready.button.textContent,'Проверить ИИ');
const pending=ready.button.fire('click');
assert.equal(calls,1);assert.equal(ready.button.disabled,true);assert.equal(ready.button.textContent,'Проверка ИИ…');
const malicious='<img src=x onerror=alert(1)>';
complete({ok:true,json:async()=>({ready:true,model:malicious,message:malicious})});
await pending;
assert.equal(ready.button.disabled,false);assert.equal(ready.button.textContent,'Проверить ИИ');
assert.equal(ready.nodes.get('model-status-title').textContent,'Модель готова к запросам');
assert.equal(ready.nodes.get('model-status-message').textContent,malicious);
assert.equal(ready.nodes.get('model-status-name').textContent,'Модель на хосте: '+malicious);
assert.ok(!ready.modals.join('').includes(malicious),'untrusted model metadata must remain plain text');
assert.match(ready.modals[0],/установленной модели/);assert.match(ready.modals[0],/Источник конкретного ответа/);
ready.dispose();assert.equal(ready.button.removed,true);

for(const [code,message] of [
 ['disabled','Перезапустите игру с OLLAMA_ENABLED=true'],
 ['unavailable','Запустите Ollama'],
 ['model_missing','ollama pull configured-model']
]){
 const ui=fixture(async()=>({ok:true,json:async()=>({ready:false,code,message,model:'configured-model'})}));
 await ui.button.fire('click');
 assert.equal(ui.nodes.get('model-status-title').textContent,'Модель требует внимания');
 assert.equal(ui.nodes.get('model-status-message').textContent,message);
 assert.equal(ui.button.disabled,false);
}
for(const [status,pattern] of [[401,/Сначала войдите/],[503,/восстановления связи/]]){
 const ui=fixture(async()=>({ok:false,status,json:()=>{throw new Error('failed response must not be parsed as readiness');}}));
 await ui.button.fire('click');
 assert.match(ui.nodes.get('model-status-error').textContent,pattern);
 assert.equal(ui.button.disabled,false);assert.equal(ui.button.textContent,'Проверить ИИ');
}
const disconnected=fixture(async()=>{throw new Error('Connection unavailable');});
await disconnected.button.fire('click');
assert.equal(disconnected.nodes.get('model-status-error').textContent,'Connection unavailable');
assert.equal(disconnected.button.disabled,false);
const invalid=fixture(async()=>({ok:true,json:async()=>{throw new SyntaxError('Invalid response');}}));
await invalid.button.fire('click');assert.equal(invalid.button.disabled,false);
assert.match(invalid.nodes.get('model-status-error').textContent,/Invalid response/);
assert.equal(installAgentStatus({doc:{getElementById:()=>null},fetcher:()=>{throw new Error('missing UI must not probe');},modal:()=>{}}),undefined);

assert.equal(modelReplyLabel({mode:'model',grounding:'sources'}),'Локальная модель · по материалам');
assert.equal(modelReplyLabel({mode:'model',grounding:'conversation'}),'Локальная модель · диалог');
assert.equal(modelReplyLabel({mode:'model',grounding:'model_knowledge'}),'Локальная модель · без источников');
assert.equal(modelReplyLabel({mode:'knowledge_base',model:'configured-model',grounding:'sources'}),'Документы и правила · резервный режим');
assert.equal(modelReplyLabel({mode:'rules'}),'Расчёт сервера');
assert.equal(modelReplyLabel({mode:'world-command'}),'Изменение окружения');
assert.equal(modelReplyLabel({mode:'unavailable',dialogue_mode:'casual',status:'model_unavailable'}),'Локальная модель недоступна');
assert.equal(modelReplyLabel({mode:'knowledge_base',dialogue_mode:'articles',status:'no_sources'}),'Подходящие источники не найдены');
assert.match(modelReplyLabel({mode:'knowledge_base',dialogue_mode:'articles',status:'no_sources',retrieval:{mode:'bm25_fallback'}}),/источники не найдены.*BM25/);
assert.match(modelReplyLabel({mode:'model',dialogue_mode:'articles',retrieval:{mode:'hybrid'}}),/гибридный поиск/);
assert.match(modelReplyLabel({mode:'model',retrieval:{mode:'bm25'}}),/поиск BM25/);
assert.match(modelReplyLabel({mode:'knowledge_base',retrieval:{mode:'bm25_fallback'}}),/BM25 · резервный поиск/);
assert.doesNotMatch(modelReplyLabel({mode:'model',dialogue_mode:'casual',retrieval:{mode:'hybrid'}}),/гибридный|RAG/,'casual conversation cannot claim retrieval');
assert.doesNotMatch(modelReplyLabel({mode:'model',grounding:'sources',retrieval:{configured:true,ready:true}}),/гибридный/,'configured readiness alone is never actual hybrid provenance');
const missing={configured:true,ready:false,code:'embedding_model_missing',message:'Модель embeddings не установлена',model:'nomic-embed-text',count:0,total:12};
const embeddingUI=fixture(async()=>({ok:true,json:async()=>({ready:true,model:'qwen3:4b',retrieval:missing})}));
await embeddingUI.button.fire('click');
assert.equal(embeddingUI.nodes.get('model-status-title').textContent,'Модель готова к запросам','generation readiness is independent of embedding readiness');
assert.match(embeddingUI.nodes.get('retrieval-status-title').textContent,/BM25.*недоступна/);
assert.match(embeddingUI.nodes.get('retrieval-status-message').textContent,/0 из 12/);
assert.match(embeddingUI.nodes.get('retrieval-status-action').textContent,/ollama pull nomic-embed-text/);
const fallbackReply={dialogue_mode:'articles',retrieval:{mode:'bm25_fallback',embedding_model:'nomic-embed-text',degraded_reason:'embedding_model_missing'}};
assert.match(retrievalReplyHint(fallbackReply),/ollama pull nomic-embed-text/);
assert.equal(retrievalReplyHint({...fallbackReply,dialogue_mode:'casual'}),'');
assert.equal(retrievalReplyHint({retrieval:{mode:'hybrid'}}),'');
assert.match(retrievalReplyHint({retrieval:{mode:'bm25_fallback',degraded_reason:'embedding_timeout'}}),/не успел/);
assert.doesNotMatch(retrievalStatusText({...missing,model:'x; curl attacker'}).action,/ollama pull/,'untrusted model names cannot become shell instructions');
assert.equal(retrievalStatusText({...missing,ready:true,code:'ready'}).title,'Семантический индекс готов');
assert.equal(retrievalStatusText({configured:false,code:'disabled'}).title,'Поиск по словам (BM25)');
assert.match(retrievalStatusText().title,/не получен/);
const untrustedRetrieval=fixture(async()=>({ok:true,json:async()=>({ready:true,retrieval:{...missing,message:malicious,model:malicious}})}));
await untrustedRetrieval.button.fire('click');
assert.ok(untrustedRetrieval.nodes.get('retrieval-status-message').textContent.startsWith(malicious));
assert.ok(!untrustedRetrieval.modals.join('').includes(malicious));
console.log(JSON.stringify({passed:true,checks:['authenticated same-origin status request','busy state and cleanup','readiness separate from actual inference','untrusted status remains text','disabled/offline/missing-model messages','unauthorized and service errors','network and invalid-JSON recovery','actual hybrid vs BM25 and degraded retrieval','missing embedding model actionable hint','casual unavailable and no-source labels','index readiness never claims actual hybrid answer'],scope:'DOM fixture; no real Ollama weights or browser rendering'}));
