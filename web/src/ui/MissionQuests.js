// Progress is derived exclusively from the persisted authoritative mission state.
// Opening a panel, visiting an NPC or clicking a button never completes a task.
const definitions=[
 {id:'power',title:'Сердце корабля',room:'reactor',mentor:'dzhanibekov',desc:'Восстановите охлаждение, затем запустите реактор. Каждая операция расходует цикл.',steps:[['cooling_repaired','Восстановить охлаждение','repair_cooling'],['reactor_active','Запустить реактор','start_reactor']]},
 {id:'air',title:'Право на вдох',room:'bio',mentor:'ryazansky',desc:'Запустите оранжерею: расход кислорода снизится с 8 до 3 за цикл.',steps:[['greenhouse_active','Запустить оранжерею','start_greenhouse']]},
 {id:'investigation',title:'Что произошло?',room:'bridge',mentor:'savinykh',desc:'Восстановите архив и сопоставьте три найденных свидетельства. Проверка гипотезы не расходует цикл.',steps:[['archive_restored','Восстановить архив','restore_archive'],['investigation_solved','Подтвердить причину аварии','investigation']]},
 {id:'navigation',title:'Путь домой',room:'bridge',mentor:'krikalev',desc:'Рассчитайте безопасный курс для длительного спасательного манёвра.',steps:[['course_calculated','Рассчитать безопасный курс','calculate_course']]},
 {id:'rescue',title:'Спасти экипаж',room:'bridge',mentor:'savinykh',desc:'Выберите доступный манёвр. В совместной игре дождитесь согласия остальных участников.',steps:[['victory','Завершить спасательный манёвр','rescue']]}
];
export function missionQuests(state={}) {
 let previousDone=true;
 return definitions.map((definition,index)=>{
  const checks=definition.steps.map(([field,title,action])=>({title,action,done:field==='victory'?!!state.game_over&&!!state.victory:!!state[field]}));
  const done=checks.every(step=>step.done),available=previousDone;
  const status=done?'completed':state.game_over?(state.victory?'skipped':'failed'):available?'active':'locked';
  previousDone=previousDone&&done;
  return {...definition,index,checks,done,status,next:checks.find(step=>!step.done)};
 });
}
export function completedQuestChanges(previous,next) {
 if(!previous)return [];
 const before=new Set(missionQuests(previous).filter(q=>q.done).map(q=>q.id));
 return missionQuests(next).filter(q=>q.done&&!before.has(q.id));
}
