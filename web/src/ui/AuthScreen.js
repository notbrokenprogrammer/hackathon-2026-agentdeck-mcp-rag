const $ = id => document.getElementById(id);

/** Real authentication against the existing Go server. Never stores passwords. */
export class AuthScreen {
  constructor(network, onSuccess) {
    this.network = network;
    this.onSuccess = onSuccess;
    this.register = location.pathname === '/register';
    this.busy = false;
    // Preserve FIZMA.zip's split form and terminal; authentication still uses
    // Last Crew's callsign/session API, with the invitation query untouched.
    const entry = $('auth-screen');
    const setTheme = mode => {
      if (!entry?.dataset) return;
      entry.dataset.theme = mode;
      const toggle = $('fizma-theme-toggle');
      toggle?.setAttribute('aria-label', mode === 'dark' ? 'Включить светлую тему' : 'Включить тёмную тему');
      if (toggle) toggle.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true">' + (mode === 'dark' ? '<path d="M20.9 13A9 9 0 0 1 11 3.1 9 9 0 1 0 20.9 13Z"/>' : '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.42 1.42m11.3 11.3 1.42 1.42M2 12h2m16 0h2M4.93 19.07l1.42-1.42m11.3-11.3 1.42-1.42"/>') + '</svg>';
    };
    try { setTheme(localStorage.getItem('lastcrew-entry-mode') === 'light' ? 'light' : 'dark'); } catch { setTheme('dark'); }
    if ($('fizma-theme-toggle')) $('fizma-theme-toggle').onclick = () => {
      const mode = entry.dataset.theme === 'dark' ? 'light' : 'dark';
      setTheme(mode);
      try { localStorage.setItem('lastcrew-entry-mode', mode); } catch { /* Use this tab's preference. */ }
    };
    const setMenu = open => {
      const menu = $('fizma-mobile-menu'), toggle = $('fizma-menu-toggle');
      if (menu) menu.hidden = !open;
      toggle?.setAttribute('aria-expanded', String(open));
      toggle?.setAttribute('aria-label', open ? 'Закрыть меню' : 'Открыть меню');
      if (toggle) toggle.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="' + (open ? 'M6 6l12 12M6 18 18 6' : 'M4 6h16M4 12h16M4 18h16') + '"/></svg>';
    };
    const closeMenu = () => setMenu(false);
    if ($('fizma-menu-toggle')) $('fizma-menu-toggle').onclick = () => setMenu($('fizma-mobile-menu').hidden);
    for (const link of document.querySelectorAll?.('#fizma-mobile-menu a') || []) link.onclick = closeMenu;
    for (const button of document.querySelectorAll?.('[data-entry-trigger]') || []) button.onclick = () => {
      closeMenu(); $(button.dataset.entryTrigger)?.click();
    };
    const header = document.querySelector?.('.fizma-header');
    if (header) {
      const scroll = () => header.classList.toggle('scrolled', window.scrollY > 18);
      window.addEventListener('scroll', scroll, {passive:true}); scroll();
    }
    if ($('fizma-session-mode')) $('fizma-session-mode').textContent = location.protocol === 'https:' ? 'secure' : 'local';
    $('auth-switch').onclick = () => this.setMode(!this.register);
    $('auth-login-tab').onclick = () => this.setMode(false);
    $('auth-register-tab').onclick = () => this.setMode(true);
    $('password-toggle').onclick = () => {
      const visible = $('password').type === 'password';
      for (const id of ['password', 'password-confirm']) $(id).type = visible ? 'text' : 'password';
      $('password-toggle').setAttribute('aria-label', visible ? 'Скрыть пароль' : 'Показать пароль');
      $('password-toggle').setAttribute('aria-pressed', String(visible));
    };
    for (const id of ['username', 'password', 'password-confirm']) {
      $(id).addEventListener('input', () => {
        this.error(id, '');
        $('auth-error').textContent = '';
        if (id === 'password') this.strength();
      });
    }
    $('password').addEventListener('keyup', e => {
      $('caps-warning').hidden = !e.getModifierState?.('CapsLock');
    });
    $('password').addEventListener('blur', () => $('caps-warning').hidden = true);
    $('auth-form').onsubmit = e => { e.preventDefault(); this.submit(); };
    window.addEventListener('popstate', () => { this.register = location.pathname === '/register'; this.render(); });
    this.render();
  }

  setMode(register) {
    if (this.busy) return;
    this.register = register;
    // Keep an invitation code when switching between login and registration.
    history.replaceState({}, '', (register ? '/register' : '/login') + location.search);
    $('password-confirm').value = '';
    // Switching modes must not leave either password field visible.
    for (const id of ['password', 'password-confirm']) $(id).type = 'password';
    $('password-toggle').setAttribute('aria-label', 'Показать пароль');
    $('password-toggle').setAttribute('aria-pressed', 'false');
    this.render();
    $('username').focus({preventScroll: true});
  }

  render() {
    const r = this.register;
    $('auth-title').textContent = r ? 'Регистрация в Last Crew' : 'Вход в Last Crew';
    $('auth-code').textContent = r ? 'CREATE ACCOUNT' : 'AUTHORIZATION';
    if ($('auth-code-mobile')) $('auth-code-mobile').textContent = $('auth-code').textContent;
    const terminalStatus = $('fizma-auth-status');
    if (terminalStatus) terminalStatus.textContent = r ? 'waiting for registration' : 'waiting for login';
    $('auth-subtitle').textContent = r ? 'Создай аккаунт, чтобы исследовать корабль, сохранять результаты и играть вместе с экипажем.' : 'Вернись на корабль и продолжи сохранённую экспедицию.';
    $('auth-submit').innerHTML = `<span>${r ? 'СОЗДАТЬ АККАУНТ' : 'ВОЙТИ'}</span><span class="fizma-submit-arrow" aria-hidden="true">→</span>`;
    $('auth-switch-caption').textContent = r ? 'Уже есть аккаунт?' : 'Нет аккаунта?';
    $('auth-switch').textContent = r ? 'Войти' : 'Зарегистрироваться';
    $('password').autocomplete = r ? 'new-password' : 'current-password';
    $('password').placeholder = 'Не менее 6 символов';
    $('password-label-hint').textContent = r ? 'НЕ МЕНЕЕ 6 СИМВОЛОВ' : 'ДОСТУП К КОРАБЛЮ';
    $('auth-login-tab').setAttribute('aria-pressed', String(!r));
    $('auth-register-tab').setAttribute('aria-pressed', String(r));
    $('confirm-field').hidden = !r;
    $('password-confirm').required = r;
    $('password-strength').hidden = !r;
    $('auth-error').textContent = '';
    for (const id of ['username', 'password', 'password-confirm']) this.error(id, '');
    this.strength();
  }

  error(id, message) {
    $(id + '-error').textContent = message;
    $(id).setAttribute('aria-invalid', String(Boolean(message)));
    $(id).closest('.input-shell').classList.toggle('invalid', Boolean(message));
  }

  strength() {
    const value = $('password').value;
    // A length guide, not a claim that the password is cryptographically strong.
    const score = value.length >= 16 ? 4 : value.length >= 12 ? 3 : value.length >= 8 ? 2 : value.length >= 6 ? 1 : 0;
    $('password-strength').dataset.level = score;
    $('password-strength').querySelectorAll('i').forEach((el, i) => el.classList.toggle('filled', i < score));
    $('strength-caption').textContent = !value ? 'Используйте длинный пароль из нескольких слов' : score === 0 ? 'Добавьте ещё символы — нужно не менее 6' : score < 3 ? 'Можно сделать длиннее: 12–16 символов удобнее защитить' : 'Хорошая длина. Не используйте этот пароль в других сервисах';
  }

  async submit() {
    if (this.busy) return;
    const name = $('username').value.trim();
    const password = $('password').value;
    const bytes = new TextEncoder().encode(password).length;
    const errors = [];
    if (!/^[\p{L}\p{N}_-]{3,32}$/u.test(name)) errors.push(['username', 'От 3 до 32 букв или цифр. Допустимы _ и дефис.']);
    if (bytes < 6 || bytes > 72) errors.push(['password', 'Пароль должен занимать от 6 до 72 байт. Для кириллицы лимит символов меньше.']);
    if (this.register && password !== $('password-confirm').value) errors.push(['password-confirm', 'Пароли не совпадают. Проверьте повторный ввод.']);
    for (const id of ['username', 'password', 'password-confirm']) this.error(id, '');
    $('auth-error').textContent = '';
    if (errors.length) {
      errors.forEach(([id, message]) => this.error(id, message));
      $(errors[0][0]).focus();
      return;
    }
    this.busy = true;
    $('auth-form').setAttribute('aria-busy', 'true');
    const buttons = ['auth-submit', 'auth-switch', 'auth-login-tab', 'auth-register-tab'];
    buttons.forEach(id => $(id).disabled = true);
    $('auth-submit').innerHTML = `<span>${this.register ? 'СОЗДАНИЕ...' : 'ВХОД...'}</span><span class="fizma-submit-arrow" aria-hidden="true">→</span>`;
    try {
      const user = await this.network.authenticate(this.register ? 'register' : 'login', name, password);
      $('password').value = '';
      $('password-confirm').value = '';
      this.onSuccess(user);
    } catch (error) {
      if (error.status === 409) {
        this.error('username', 'Этот позывной уже занят. Выберите другой или войдите в аккаунт.');
        $('username').focus();
      } else {
        $('auth-error').textContent = error.status === 401 ? 'Позывной или пароль не совпали. Проверьте данные и попробуйте снова.' : error instanceof TypeError ? 'Нет связи с сервером. Проверьте адрес и подключение к локальной сети.' : error.message;
      }
    } finally {
      this.busy = false;
      $('auth-form').removeAttribute('aria-busy');
      buttons.forEach(id => $(id).disabled = false);
      $('auth-submit').innerHTML = `<span>${this.register ? 'СОЗДАТЬ АККАУНТ' : 'ВОЙТИ'}</span><span class="fizma-submit-arrow" aria-hidden="true">→</span>`;
    }
  }
}
