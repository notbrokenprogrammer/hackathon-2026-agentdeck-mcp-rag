import {roomAt} from '../interior/DeckLayout.js';
const $ = id => document.getElementById(id);
const defaults = {accent: 'cyan', motion: 'system', quality: 'balanced', lights: true};
const colors = {cyan: '#83ded5', blue: '#9ccdf1', amber: '#efc284'};
export function installFlightDeck({getShip, modal, toast, help, crew}) {
  let preferences;
  try { preferences = {...defaults, ...JSON.parse(localStorage.getItem('lastcrew-ui') || '{}')}; } catch { preferences = {...defaults}; }
  function apply() {
    document.documentElement.style.setProperty('--accent', colors[preferences.accent] || colors.cyan);
    document.documentElement.dataset.motion = preferences.motion;
    const ship = getShip();
    if (ship) {
      ship.setLighting?.(preferences.lights !== false);
      if(ship.setQuality)ship.setQuality(preferences.quality);
      else {ship.renderer.setPixelRatio(Math.min(devicePixelRatio, preferences.quality === 'economy' ? 1 : 1.7));ship.resize();}
    }
    const lights = $('toggle-lights');
    lights.setAttribute('aria-pressed', String(preferences.lights !== false));
    lights.textContent = preferences.lights !== false ? '☀ Свет включён' : '☀ Включить свет';
    lights.title = 'Рабочее освещение в вашем окне игры';
  }
  function save() { try { localStorage.setItem('lastcrew-ui', JSON.stringify(preferences)); } catch { toast('Настройки применены только для этой вкладки'); } apply(); }
  function settings() {
    modal(`<span class="eyebrow">ПЕРСОНАЛЬНЫЙ ТЕРМИНАЛ</span><h2>Настройте свой мостик</h2><p>Оформление меняется только у вас. Игровые ресурсы и решения остаются общими.</p><div class="settings-section"><h3>Цвет интерфейса</h3><div class="swatch-list">${Object.entries({cyan:'Ледяная бирюза',blue:'Орбитальный синий',amber:'Тёплый янтарь'}).map(([id,label])=>`<button data-accent="${id}" aria-pressed="${preferences.accent===id}" style="--swatch:${colors[id]}"><i></i>${label}</button>`).join('')}</div></div><div class="settings-row"><div><h3>Анимации интерфейса</h3><p>Уменьшите движение, если оно отвлекает.</p></div><select id="motion-setting" aria-label="Анимации интерфейса"><option value="system">Как в системе</option><option value="reduce">Минимум движения</option></select></div><div class="settings-row"><div><h3>Разрешение 3D</h3><p>Экономный режим уменьшает разрешение и отключает тени.</p></div><select id="quality-setting" aria-label="Разрешение 3D"><option value="balanced">Сбалансированное</option><option value="economy">Экономное</option></select></div><p class="source-origin">Настройки сохраняются в этом браузере. Звук и голосовой ввод в этой версии не используются.</p>`);
    document.querySelectorAll('[data-accent]').forEach(b=>b.onclick=()=>{preferences.accent=b.dataset.accent;save();document.querySelectorAll('[data-accent]').forEach(x=>x.setAttribute('aria-pressed',String(x===b)));});
    for(const [id,key] of [['motion-setting','motion'],['quality-setting','quality']]) { $(id).value=preferences[key]; $(id).onchange=()=>{preferences[key]=$(id).value;save();}; }
  }
  document.querySelector('.theme-toggle').onclick = settings;
  $('settings-button').onclick = settings;
  $('toggle-lights').onclick = () => { preferences.lights = preferences.lights === false; save(); };
  $('expand-comms').onclick = () => {
    const expanded = $('comms-panel').classList.toggle('expanded');
    $('expand-comms').setAttribute('aria-expanded', String(expanded));
    $('expand-comms').setAttribute('aria-label', expanded ? 'Уменьшить окно общения' : 'Развернуть окно общения');
    $('expand-comms').title = expanded ? 'Уменьшить окно' : 'Развернуть окно';
    $('expand-comms').textContent = expanded ? '↙' : '⤢';
    getShip()?.controls.unlock();
  };
  $('entry-briefing').onclick = help;
  $('entry-controls').onclick = help;
  $('entry-crew').onclick = crew;
  $('collapse-quest').onclick = () => {
    const collapsed = document.querySelector('.mission-card').classList.toggle('collapsed');
    $('collapse-quest').setAttribute('aria-expanded', String(!collapsed));
    $('collapse-quest').setAttribute('aria-label', collapsed ? 'Развернуть задачу' : 'Свернуть задачу');
    $('collapse-quest').textContent = collapsed ? '+' : '−';
  };
  let lastLocation = '';
  const timer = setInterval(() => {
    const ship = getShip();
    if(!ship?.active) return;
    const {x,z} = ship.player;
    const area=roomAt(x,z);const id=area?.id||'corridor';
    if(id === lastLocation) return;
    lastLocation=id;
    $('location-label').textContent='Вы здесь · '+(area?.name||'переход');
    document.querySelectorAll('[data-room]').forEach(b=>{
      b.classList.toggle('current-room', b.dataset.room===id);
      if(b.dataset.room===id)b.setAttribute('aria-current','location');else b.removeAttribute('aria-current');
    });
  },500);
  window.addEventListener('pagehide',()=>clearInterval(timer),{once:true});
  apply();
  return {apply, settings};
}
