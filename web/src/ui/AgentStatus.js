export function modelReplyLabel(reply) {
 if(reply.mode==='server-command')return ({completed:'✓ Подтверждено сервером',pending:'Ожидает согласия экипажа',failed:'Поручение не выполнено',not_completed:'Задача ещё не выполнена',preview:'Расчёт без выполнения',status:'Состояние миссии'})[reply.command?.status||reply.status]||'Ответ сервера';
 const retrieval=reply.retrieval?.mode;
 const search=({hybrid:'гибридный поиск',bm25:'поиск BM25',bm25_fallback:'BM25 · резервный поиск'})[retrieval];
 if(reply.mode==='unavailable'||reply.status==='model_unavailable')return 'Локальная модель недоступна';
 if(reply.status==='no_sources')return 'Подходящие источники не найдены'+(search?' · '+search:'');
 if(reply.mode==='world-command')return 'Изменение окружения';
 if(reply.mode==='rules')return 'Расчёт сервера';
 if(reply.mode==='model'&&(reply.dialogue_mode==='casual'||reply.grounding==='conversation'))return 'Локальная модель · диалог';
 if(reply.mode==='model'&&reply.grounding==='model_knowledge')return 'Локальная модель · без источников';
 if(search)return (reply.mode==='model'?'Модель + статьи':'Материалы базы')+' · '+search;
 return reply.mode==='model'?'Локальная модель · по материалам':'Документы и правила · резервный режим';
}

const embeddingReasons={
 embedding_model_missing:'На компьютере ведущего не установлена модель embeddings.',
 embedding_service_unavailable:'Служба embeddings недоступна. Ведущему нужно запустить Ollama.',
 embedding_timeout:'Семантический поиск не успел ответить. Выполнен поиск по словам.',
 embedding_response_invalid:'Модель embeddings вернула неподходящий ответ.',
 embedding_cache_invalid:'Семантический индекс требует восстановления.',
 embedding_index_not_ready:'Семантический индекс ещё не готов.'
};
const modelPullHint=model=>typeof model==='string'&&/^[a-zA-Z0-9][a-zA-Z0-9._:/-]{0,199}$/.test(model)?'На компьютере ведущего: ollama pull '+model+'. После установки перезапустите игру.':'Ведущему нужно проверить имя модели embeddings в настройках сервера.';
export function retrievalReplyHint(reply) {
 if(reply.dialogue_mode==='casual'||reply.retrieval?.mode!=='bm25_fallback')return '';
 const retrieval=reply.retrieval;
 return (embeddingReasons[retrieval.degraded_reason]||'Семантический поиск недоступен; ответ найден по словам (BM25).')+(retrieval.degraded_reason==='embedding_model_missing'?' '+modelPullHint(retrieval.embedding_model):'');
}

export function retrievalStatusText(status) {
 if(!status)return {title:'Статус поиска не получен',message:'Сервер не передал состояние индекса. Способ поиска указан под каждым ответом.',action:''};
 const title=status.ready?'Семантический индекс готов':status.configured?'Поиск BM25 · семантика недоступна':'Поиск по словам (BM25)';
 const progress=Number.isFinite(status.count)&&Number.isFinite(status.total)?' Проиндексировано '+status.count+' из '+status.total+' фрагментов.':'';
 return {title,message:(status.message||embeddingReasons[status.code]||'Способ конкретного поиска указан под ответом.')+progress,action:status.code==='embedding_model_missing'?modelPullHint(status.model):''};
}

// Availability is separate from the provenance of each actual answer.
export function installAgentStatus({modal,doc=document,fetcher=fetch}) {
 const anchor=doc.getElementById('npc-profile');if(!anchor)return;
 const button=doc.createElement('button');button.id='agent-status-check';button.type='button';button.className='text-button';button.textContent='Проверить ИИ';button.title='Проверить модель и поиск по статьям на компьютере ведущего';anchor.after(button);
 const check=async()=>{
  button.disabled=true;button.textContent='Проверка ИИ…';
  try{
   const response=await fetcher('/api/agents/status',{credentials:'same-origin',cache:'no-store'});
   if(!response.ok)throw new Error(response.status===401?'Сначала войдите в игру.':'Не удалось проверить модель. Повторите после восстановления связи.');
   const status=await response.json();
   modal('<span class="eyebrow">СВЯЗЬ С НАСТАВНИКАМИ</span><h2 id="model-status-title"></h2><p id="model-status-message"></p><div id="model-status-name" class="dialog-stat"></div><h3 id="retrieval-status-title"></h3><p id="retrieval-status-message"></p><p id="retrieval-status-action"></p><p>Перед вопросом выберите «По статьям» или «Неформальное общение». Первый режим ищет материалы и показывает источники; второй ведёт свободный разговор без поиска. Истории сохраняются отдельно.</p><p>Служба Ollama и модели нужны только на компьютере ведущего. Откройте Ollama или выполните <code>ollama serve</code>. Если модель выключена, перезапустите игру с <code>OLLAMA_ENABLED=true</code>.</p><p class="source-origin">Проверка показывает доступность службы и установленной модели. Источник конкретного ответа указан под сообщением. Готовность индекса не означает, что каждый запрос использовал гибридный поиск.</p>');
   doc.getElementById('model-status-title').textContent=status.ready?'Модель готова к запросам':'Модель требует внимания';
   doc.getElementById('model-status-message').textContent=status.message||'Нет сведений о подключении.';
   doc.getElementById('model-status-name').textContent='Модель на хосте: '+(status.model||'не задана');
   const retrieval=retrievalStatusText(status.retrieval);
   doc.getElementById('retrieval-status-title').textContent=retrieval.title;
   doc.getElementById('retrieval-status-message').textContent=retrieval.message;
   doc.getElementById('retrieval-status-action').textContent=retrieval.action;
  }catch(error){modal('<h2>Проверка ИИ</h2><p id="model-status-error"></p>');doc.getElementById('model-status-error').textContent=error.message;}
  finally{button.disabled=false;button.textContent='Проверить ИИ';}
 };
 button.addEventListener('click',check);return()=>button.remove();
}
