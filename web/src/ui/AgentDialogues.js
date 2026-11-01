export const DIALOGUE_MODES = Object.freeze({
  articles: {
    label: 'По статьям',
    description: 'Ответ по материалам базы со ссылками. Если источников нет, наставник сообщит об этом.',
    placeholder: 'Вопрос по статьям и материалам…',
    busy: 'Ищу материалы и готовлю ответ по статьям… Первый запрос может занять до минуты.',
    intro: 'Задайте вопрос по материалам базы. Под ответом будут доступны найденные источники.',
    suggestions: [['Опыт со световыми часами','Покажи опыт со световыми часами'],['«Салют-7»','Что произошло с «Салютом-7»?'],['Растения в космосе','Как выращивают растения в космосе?'],['Время на орбите','Как меняется ход времени на орбите?']]
  },
  mission: {
    label: 'Поручения',
    description: 'Агент выполняет поручения через сервер. Операции расходуют ресурсы и циклы; права вашей роли сохраняются.',
    placeholder: 'Например: «Восстанови охлаждение»…',
    busy: 'Проверяю поручение и жду результат сервера…',
    intro: 'Могу выполнить операцию корабля, создать опыт или показать следующий шаг. Начните с «Что делать дальше?» либо поручите одно конкретное действие. Выполнение засчитывается только после подтверждения сервера.',
    suggestions: [['Что делать дальше?','Что делать дальше?'],['Охлаждение','Восстанови охлаждение'],['Реактор','Запусти реактор'],['Световые часы','Покажи опыт со световыми часами']]
  },
  casual: {
    label: 'Неформальное общение',
    description: 'Свободный разговор с локальной моделью. Поиск по статьям и ссылки не используются.',
    placeholder: 'Поговорить с наставником…',
    busy: 'Наставник готовит ответ для свободного разговора… Первый запрос может занять до минуты.',
    intro: 'Можно просто поговорить, обсудить настроение или идеи. Это разговор с ИИ-персонажем, без поиска по статьям.',
    suggestions: [['Как настроение?','Как настроение?'],['Командный дух','Как поддержать командный дух?'],['О чём поговорим?','Предложи тему для разговора'],['Немного юмора','Расскажи добрую космическую шутку']]
  }
});
export const dialogueMode = (value, fallback='articles') => Object.hasOwn(DIALOGUE_MODES,value) ? value : fallback;
export const messageDialogueMode = (item, fallback) => dialogueMode(item?.reply?.dialogue_mode,dialogueMode(item?.dialogue_mode,dialogueMode(item?.mode,fallback)));

/** Dialogues are scoped to expedition, mentor and explicit conversation mode. */
export class AgentDialogues {
  constructor({network,onChange=()=>{},storage}) {
    this.network=network;this.onChange=onChange;
    try { this.storage=storage??globalThis.localStorage;this.mode=dialogueMode(this.storage?.getItem('lastcrew-dialogue-mode')); } catch { this.mode='articles'; }
    this.agent='savinykh';this.scope='';this.epoch=0;
    this.histories=new Map();this.loaded=new Set();this.loading=new Set();this.pending=new Set();this.errors=new Map();
  }
  key(agent=this.agent,mode=this.mode){return agent+':'+mode;}
  get rows(){return this.histories.get(this.key())||[];}
  get busy(){return this.pending.has(this.key());}
  get historyLoading(){return this.loading.has(this.key());}
  get historyError(){return this.errors.get(this.key())||'';}
  get canSend(){return Boolean(this.scope)&&this.loaded.has(this.key())&&!this.busy&&!this.historyLoading;}
  setScope(scope){
    if(this.scope===scope)return;
    this.scope=scope;this.epoch++;
    for(const collection of [this.histories,this.loaded,this.loading,this.pending,this.errors])collection.clear();
    this.onChange();
  }
  select(agent,mode=this.mode){
    this.agent=agent;this.mode=dialogueMode(mode);
    try { this.storage?.setItem('lastcrew-dialogue-mode',this.mode); } catch { /* Tab-only preference is sufficient. */ }
    this.onChange();return this.loadHistory();
  }
  async loadHistory(){
    const {agent,mode,epoch}=this,key=this.key();
    if(!this.scope||this.loaded.has(key)||this.loading.has(key))return;
    this.loading.add(key);this.errors.delete(key);this.onChange();
    try {
      const response=await this.network.request('dialogue.history',{agent_id:agent,dialogue_mode:mode});
      if(epoch!==this.epoch)return;
      const grouped=new Map([[key,[]]]);
      for(const item of Array.isArray(response.messages)?response.messages:[]) {
        // The endpoint filters explicit modes. Never load the legacy unfiltered history.
        const itemMode=messageDialogueMode(item,mode),destination=this.key(agent,itemMode);
        if(!grouped.has(destination))grouped.set(destination,[]);
        grouped.get(destination).push({...item,dialogue_mode:itemMode});
      }
      for(const [destination,rows] of grouped)if(destination===key||!this.histories.has(destination))this.histories.set(destination,rows);
      this.loaded.add(key);
    } catch(error) {
      if(epoch===this.epoch)this.errors.set(key,'Не удалось загрузить историю «'+DIALOGUE_MODES[mode].label+'»: '+error.message);
    } finally {
      if(epoch===this.epoch){this.loading.delete(key);this.onChange();}
    }
  }
  async send(text){
    text=text.trim();if(!text||!this.canSend)return false;
    const {agent,mode,epoch}=this,key=this.key();
    const rows=this.histories.get(key)||[];
    rows.push({role:'user',text,dialogue_mode:mode});this.histories.set(key,rows);
    this.pending.add(key);this.onChange();
    try {
      const reply=await this.network.request('agent.talk',{agent_id:agent,text,dialogue_mode:mode});
      if(epoch!==this.epoch)return false;
      const replyMode=dialogueMode(reply.dialogue_mode,mode),destination=this.key(agent,replyMode);
      const target=this.histories.get(destination)||[];
      target.push({role:'assistant',text:reply.text,reply,dialogue_mode:replyMode});this.histories.set(destination,target);
    } catch(error) {
      if(epoch===this.epoch)rows.push({role:'system',text:DIALOGUE_MODES[mode].label+': '+error.message,dialogue_mode:mode});
    } finally {
      if(epoch===this.epoch){this.pending.delete(key);this.onChange();}
    }
    return true;
  }
}

export function installDialogueModeSelector({dialogues,doc=document}) {
  const root=doc.getElementById('dialogue-mode-selector');
  const buttons=[...doc.querySelectorAll('[data-dialogue-mode]')];
  for(const button of buttons)button.onclick=()=>dialogues.select(dialogues.agent,button.dataset.dialogueMode);
  doc.getElementById('dialogue-history-retry').onclick=()=>dialogues.loadHistory();
  return ({team=false}={})=>{
    root.hidden=team;
    for(const button of buttons)button.setAttribute('aria-pressed',String(button.dataset.dialogueMode===dialogues.mode));
    doc.getElementById('dialogue-mode-description').textContent=DIALOGUE_MODES[dialogues.mode].description;
    doc.getElementById('dialogue-history-retry').hidden=team||!dialogues.historyError;
  };
}
