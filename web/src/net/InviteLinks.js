export function readRoomInvitation(search) {
  const code = (new URLSearchParams(search).get('room') || '').trim().toUpperCase();
  return /^[A-F0-9]{6}$/.test(code) ? code : '';
}

function sharedOrigin(value) {
  try {
    const url = new URL(value);
    const host = url.hostname.toLowerCase().replace(/^\[|\]$/g, '');
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password ||
        host === 'localhost' || host.endsWith('.localhost') || host === '::1' ||
        host === '::' || host === '0.0.0.0' || /^127\./.test(host)) return null;
    return url.origin;
  } catch { return null; }
}

// A remote player's current origin is already reachable. Prefer it to VPN or
// virtual adapters discovered on the host, and never advertise client localhost.
export function invitationLinks(origin, addresses, code, publicURL = '') {
  const publicOrigin = sharedOrigin(publicURL);
  const origins = (publicOrigin?.startsWith('https:') ? [publicOrigin] : [origin, ...(Array.isArray(addresses) ? addresses : [])])
    .map(sharedOrigin).filter(Boolean);
  return [...new Set(origins)].map(address => {
    const url = new URL('/', address);
    if (/^[A-F0-9]{6}$/.test(code || '')) url.searchParams.set('room', code);
    return url.href;
  });
}

export function createInvitePanel({getRoom, modal, copyText}) {
  return async () => {
    let addresses = [];
    let publicURL = '';
    try {
      const response = await fetch('/api/host', {credentials: 'same-origin', cache: 'no-store'});
      if (response.ok) { const data = await response.json(); addresses = data.urls; publicURL = data.public_url || ''; }
    } catch { /* The address already used by a remote browser remains usable. */ }
    const code = getRoom()?.code || '';
    const links = invitationLinks(location.origin, addresses, code, publicURL);
    const secure = !!sharedOrigin(publicURL)?.startsWith('https:') || location.protocol === 'https:';
    modal('<span class="eyebrow">СОБЕРИТЕ ЭКИПАЖ / '+(secure?'HTTPS':'LAN')+'</span><h2>Пригласить на борт</h2>' +
      '<p>Друзья открывают ссылку в браузере, входят каждый в свой аккаунт и нажимают «Присоединиться». Сервер запускается только на одном компьютере.</p>' +
      '<div id="invite-room-code" class="dialog-stat"></div><div id="invite-links"></div>' +
      '<p class="source-origin">'+(secure?'Другому игроку не нужны установка игры, Go или Ollama. Передайте HTTPS-ссылку и оставьте сервер работающим. В режиме --share нужен интернет на обоих компьютерах; ссылка меняется при новом запуске.':'Эти HTTP-ссылки работают в одной локальной сети. Для HTTPS-подключения из любой сети запустите на хосте go run . --share. Если адресов несколько, выберите IP текущей Wi-Fi или Ethernet сети.')+'</p>');
    document.getElementById('invite-room-code').textContent = code ? 'Код экспедиции: ' + code : 'Создайте экспедицию, чтобы получить код.';
    const root = document.getElementById('invite-links');
    if (!links.length) {
      root.textContent = 'Сетевой адрес не найден. Узнайте IPv4 компьютера в настройках сети и откройте http://IP:' +
        (location.port || (location.protocol === 'https:' ? '443' : '80')) +
        '. Адрес localhost работает только на компьютере с сервером.';
    }
    for (const url of links) {
      const button = document.createElement('button');
      button.className = 'source-chip';
      button.textContent = url + ' ↗';
      button.title = 'Скопировать приглашение';
      button.onclick = () => copyText(url);
      root.append(button);
    }
  };
}
