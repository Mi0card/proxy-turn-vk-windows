'use strict';

// ── Состояния ─────────────────────────────────────────────────────────────────────

const state = {
  tunnelRunning:  false,
  tunnelPaused:   false,
  socksRunning:   false,
  logEntries:     [],   // [{ts, msg, lv}] — общий (туннель + маршрутизация + прокси)
  connEntries:    [],   // [{ts, msg, lv}] — подключения (→ host:port)
  rulesetGroups:  { geosite: [], geoip: [] }, // [{ts, msg, lv}] — маршрутизация
  logFilter:      'all',
  activeLogTab:   'general',
  lastPingMs:      null,
  activeTab:       'connect',
  activeWorkers:  0,
  totalWorkers:   0,
  connProfiles:   [],   // [{name, vk, srv, sec, n, listen, captcha_mode, obfs_mode, fingerprint}]
  activeProfile:  '',
  loadedProfile:  null, // профиль, поля которого сейчас в форме (снимок для dirty-проверки)
};

// ── Иконки (inline SVG для динамической разметки) ───────────────────────────

const ICO = {
  clipboard: '<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="8" y="2" width="8" height="4" rx="1"/><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/></svg>',
  close:   '<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18M6 6l12 12"/></svg>',
  trash:   '<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/></svg>',
  up:      '<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m18 15-6-6-6 6"/></svg>',
  down:    '<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>',
  first:   '<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 3h14"/><path d="m18 13-6-6-6 6"/><path d="M12 7v14"/></svg>',
  last:    '<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 21h14"/><path d="m6 11 6 6 6-6"/><path d="M12 3v14"/></svg>',
  moon:    '<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/></svg>',
  sun:     '<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/></svg>',
};

// Заголовки вкладок (шапка контента)
const TAB_META = {
  connect: { title: 'Подключение',  sub: 'Настройка туннеля WireGuard over VK TURN' },
  proxy:   { title: 'Прокси',       sub: 'SOCKS5 + HTTP прокси и системный прокси' },
  routing: { title: 'Маршрутизация', sub: 'Правила распределения трафика' },
  logs:    { title: 'Логи',         sub: 'События туннеля, прокси и маршрутизации' },
  wg:      { title: 'WireGuard',    sub: 'Конфигурация WireGuard' },
  deploy:  { title: 'Deploy',       sub: 'Установка wdtt-server на VPS' },
  settings:{ title: 'Настройки',    sub: 'Поведение окна и системный трей' },
  help:    { title: 'Справка',      sub: 'Инструкции и флаги wdtt-client' },
};

function setBtnLabel(btn, text) {
  if (!btn) return;
  const label = btn.querySelector('.btn-label');
  if (label) label.textContent = text;
  else btn.textContent = text;
}

// ── Toast (баннер ошибок вне вкладки Логи) ───────────────────────────────────

function showToast(msg, lv = 'error') {
  const box = document.getElementById('toast-box');
  if (!box) return;
  const el = document.createElement('div');
  el.className = 'toast toast-' + lv;
  el.textContent = msg;
  box.appendChild(el);
  setTimeout(() => el.classList.add('toast-out'), 4000);
  setTimeout(() => el.remove(), 4400);
}

// ── Busy-состояние кнопок (индикатор длительной операции) ────────────────────

function setBusy(btn, busy, busyText) {
  if (!btn) return;
  const label = btn.querySelector('.btn-label');
  const getTxt = () => label ? label.textContent : btn.textContent;
  const setTxt = (t) => { if (label) label.textContent = t; else btn.textContent = t; };
  if (busy) {
    if (!btn.dataset.label) btn.dataset.label = getTxt();
    setTxt(busyText || '…');
    btn.disabled = true;
    btn.classList.add('is-busy');
  } else {
    if (btn.dataset.label) setTxt(btn.dataset.label);
    btn.disabled = false;
    btn.classList.remove('is-busy');
  }
  // Обновляем статус бадж для отображения загрузки
  if (btn.id === 'btn-connect' || btn.id === 'btn-pause' || btn.id === 'btn-stop' || 
      btn.id === 'btn-pstart' || btn.id === 'btn-pstop' || btn.id === 'btn-deploy' || btn.id === 'btn-undeploy') {
    const statusEl = document.getElementById('status-badge');
    if (statusEl && busy) {
      statusEl.innerHTML = '<span class="dot"></span> Загрузка...';
    }
  }
}

// ── Инициализация ──────────────────────────────────────────────────────────────────────

window.addEventListener('load', async () => {
  initTheme();

  const sysProxySupported = await window.go.main.App.SystemProxySupported();
  if (!sysProxySupported) {
    const row = document.getElementById('sysproxy-row');
    if (row) row.style.display = 'none';
  }
  // Вкладки
  document.querySelectorAll('.tab').forEach(btn => {
    btn.addEventListener('click', () => switchTab(btn.dataset.tab));
  });

  // Подписка на события от Go
  window.runtime.EventsOn('log', onLog);
  window.runtime.EventsOn('tunnel:status', onTunnelStatus);
  window.runtime.EventsOn('tunnel:workers', onWorkers);
  window.runtime.EventsOn('tunnel:captcha', onCaptcha);
  window.runtime.EventsOn('tunnel:captcha:done', onCaptchaDone);
  window.runtime.EventsOn('socks:status', onSocksStatus);
  window.runtime.EventsOn('socks:stats', onSocksStats);
  window.runtime.EventsOn('socks:log', onSocksLog);
  window.runtime.EventsOn('tunnel:stats', onTunnelStats);
  window.runtime.EventsOn('tunnel:ping',  onTunnelPing);
  window.runtime.EventsOn('deploy:log', onDeployLog);
  window.runtime.EventsOn('tunnel:wgconfig', onWgConfig);
  window.runtime.EventsOn('sysproxy:status', onSysProxyStatus);
  window.runtime.EventsOn('ruleset:progress', onRulesetProgress);


  // Загружаем конфиг и заполняем поля
  const cfg = await window.go.main.App.GetConfig();
  loadConfig(cfg);

  // Инициализируем профили подключения
  await initProfiles();

  // Восстанавливаем состояние кнопок прокси (если прокси уже запущен).
  try {
    const socksOn = await window.go.main.App.SocksStatus();
    if (socksOn) onSocksStatus(true);
  } catch (e) { /* SocksStatus недоступен — оставляем дефолт */ }

  // Статус бинарника — показываем только предупреждение, если он не найден.
  const exeExists = await window.go.main.App.GetClientExeExists();
  const binEl = document.getElementById('bin-status');
  if (!exeExists) {
    binEl.textContent = '⚠  wdtt-client.exe не найден';
    binEl.className = 'bin-status err';
  } else {
    binEl.textContent = '';
    binEl.className = 'bin-status';
  }

  // Справка
  const ver = await window.go.main.App.GetVersion();
  document.getElementById('help-text').textContent = helpText(ver);
  const sv = document.getElementById('sidebar-version');
  if (sv) sv.textContent = 'v' + ver;

  log('WinDTT v' + ver + ' запущен', 'success');
  
  // Добавляем события для поля ввода правил
  const input = document.getElementById('rr-new');
  if (input) {
    input.addEventListener('input', onRuleInput);
    input.addEventListener('focus', openRuleSuggest);
    input.addEventListener('keydown', handleRuleSuggestKeydown);
    
    // Закрываем дропдаун при потере фокуса
    input.addEventListener('blur', function() {
      // Небольшая задержка чтобы позволить клику на элемент дропдауна сработать
      setTimeout(closeRuleSuggest, 150);
    });
    
    // Закрываем дропдаун при клике вне его
    document.addEventListener('mousedown', function(e) {
      const suggestBox = document.getElementById('rr-suggest');
      const input = document.getElementById('rr-new');
      
      if (suggestBox && input && 
          !suggestBox.contains(e.target) && 
          !input.contains(e.target)) {
        closeRuleSuggest();
      }
    });
  }

  // «Скачивать правила через туннель» — сохраняем сразу при изменении.
  const rvt = document.getElementById('rr-via-tunnel');
  if (rvt) rvt.addEventListener('change', saveConfig);
});

// ── Вкладки ──────────────────────────────────────────────────────────────────────

function switchTab(name) {
  state.activeTab = name;
  document.querySelectorAll('.tab').forEach(b => b.classList.remove('active'));
  document.querySelectorAll('.tab-panel').forEach(p => { p.classList.remove('active'); p.classList.add('hidden'); });
  document.querySelector(`[data-tab="${name}"]`).classList.add('active');
  const panel = document.getElementById('tab-' + name);
  panel.classList.remove('hidden');
  panel.classList.add('active');

  // Обновляем заголовок и подзаголовок в шапке
  const meta = TAB_META[name] || { title: name, sub: '' };
  const hTitle = document.getElementById('header-title');
  const hSub = document.getElementById('header-subtitle');
  if (hTitle) hTitle.textContent = meta.title;
  if (hSub) hSub.textContent = meta.sub;

  // При переходе на логи — прокручиваем в конец
  if (name === 'logs') {
    const box = state.activeLogTab === 'conn'
      ? document.getElementById('conn-log-box')
      : document.getElementById('general-log-box');
    if (box) box.scrollTop = box.scrollHeight;
  }
  // При открытии роутинга — загружаем правила
  if (name === 'routing' && !state.routingLoaded) {
    state.routingLoaded = true;
    loadRoutingTab();
  }
}

// Свернуть / развернуть сайдбар
function toggleSidebar() {
  document.body.classList.toggle('sidebar-collapsed');
}

// ── Конфиг ────────────────────────────────────────────────────────────────────

function loadConfig(cfg) {
  // Устанавливаем только непустые значения — не затираем HTML-дефолты
  if (cfg.vk)       setVkHashes(cfg.vk);
  if (cfg.srv)      setVal('srv',        cfg.srv);
  if (cfg.sec)      setVal('sec',        cfg.sec);
  if (cfg.n)        setVal('n-workers',  cfg.n);
  if (cfg.listen)   setVal('listen',     cfg.listen);
  if (cfg.fingerprint) {
    const fp = document.getElementById('fingerprint');
    if (fp) for (let o of fp.options) if (o.value === cfg.fingerprint) o.selected = true;
  }
  if (cfg.px_host)  setVal('px-host',    cfg.px_host);
  if (cfg.px_socks_port) setVal('px-port', cfg.px_socks_port);
  if (cfg.px_http_port) setVal('px-http-port', cfg.px_http_port);
  if (cfg.px_user)  setVal('px-user',    cfg.px_user);
  if (cfg.px_pass)  setVal('px-pass',    cfg.px_pass);

  const cm = document.getElementById('captcha-mode');
  if (cfg.captcha_mode) {
    for (let o of cm.options) if (o.value === cfg.captcha_mode) o.selected = true;
  }

  const om = document.getElementById('obfs-mode');
  if (cfg.obfs_mode && om) {
    for (let o of om.options) if (o.value === cfg.obfs_mode) o.selected = true;
  }

  const rvt = document.getElementById('rr-via-tunnel');
  if (rvt && cfg.rules_via_tunnel !== undefined) rvt.checked = !!cfg.rules_via_tunnel;

  const rdp = document.getElementById('rr-default-policy');
  if (rdp) rdp.value = cfg.routing_default || 'proxy';

  if (cfg.dns) setVal('rr-dns', cfg.dns);

  // px_use_auth: undefined → оставляем HTML-дефолт (checked)
  if (cfg.px_use_auth !== undefined) {
    document.getElementById('px-use-auth').checked = cfg.px_use_auth !== false;
  }
  const mtr = document.getElementById('minimize-tray');
  if (mtr && cfg.tray_on_exit !== undefined) mtr.checked = !!cfg.tray_on_exit;
  const mtm = document.getElementById('minimize-tray-on-min');
  if (mtm && cfg.tray_on_minimize !== undefined) mtm.checked = !!cfg.tray_on_minimize;
  toggleAuth();
}

function collectConfig() {
  return {
    vk:           getVkHashes(),
    fingerprint:  document.getElementById('fingerprint')?.value || 'firefox',
    srv:          getVal('srv'),
    sec:          getVal('sec'),
    n:            getVal('n-workers') || '9',
    listen:       getVal('listen') || '127.0.0.1:9000',
    captcha_mode: document.getElementById('captcha-mode').value,
    obfs_mode:    document.getElementById('obfs-mode')?.value || 'audio',
    device_id:    '',   // заполнится на Go-стороне
    px_host:      getVal('px-host') || '127.0.0.1',
    px_socks_port: getVal('px-port') || '1080',
    px_http_port: getVal('px-http-port') || '1081',
    px_use_auth:  document.getElementById('px-use-auth').checked,
    px_user:      getVal('px-user'),
    px_pass:      getVal('px-pass'),
    rules_via_tunnel: document.getElementById('rr-via-tunnel').checked,
    routing_default:  document.getElementById('rr-default-policy')?.value || 'proxy',
    dns:             getVal('rr-dns') || '',
    tray_on_exit: document.getElementById('minimize-tray')?.checked || false,
    tray_on_minimize: document.getElementById('minimize-tray-on-min')?.checked || false,
    theme:        document.body.classList.contains('light') ? 'light' : 'dark',
  };
}

async function saveConfig() {
  const cfg = collectConfig();
  await window.go.main.App.SaveConfig(cfg);
}

// ── Профили подключения ───────────────────────────────────────────────────────

function collectProfile() {
  const setSel = (id, fallback) => {
    const el = document.getElementById(id);
    return el ? el.value : fallback;
  };
  return {
    name: '',
    vk:           getVkHashes(),
    srv:          getVal('srv'),
    sec:          getVal('sec'),
    n:            getVal('n-workers') || '9',
    listen:       getVal('listen') || '127.0.0.1:9000',
    captcha_mode: setSel('captcha-mode', 'auto'),
    obfs_mode:    setSel('obfs-mode', 'audio'),
    fingerprint:  setSel('fingerprint', 'firefox'),
  };
}

// Применяет профиль к форме (поля подключения).
function applyProfile(p) {
  if (!p) return;
  setVkHashes(p.vk);
  const direct = (id, v) => { const el = document.getElementById(id); if (el) el.value = v || ''; };
  direct('srv', p.srv);
  direct('sec', p.sec);
  direct('n-workers', p.n);
  direct('listen', p.listen);
  const setOpt = (id, v) => {
    const el = document.getElementById(id);
    if (!el) return;
    el.value = v || '';
  };
  setOpt('captcha-mode', p.captcha_mode);
  setOpt('obfs-mode', p.obfs_mode);
  setOpt('fingerprint', p.fingerprint);
}

// Снимок текущей формы в виде «профиля» (без имени) для dirty-проверки.
function formSnapshot() {
  const p = collectProfile();
  delete p.name;
  return p;
}

function profilesEqual(a, b) {
  if (!a || !b) return false;
  return ['vk','srv','sec','n','listen','captcha_mode','obfs_mode','fingerprint']
    .every(k => (a[k] || '') === (b[k] || ''));
}

// Есть ли несохранённые изменения относительно загруженного профиля?
function isProfileDirty() {
  return state.loadedProfile && !profilesEqual(formSnapshot(), state.loadedProfile);
}

function setLoadedProfile(p) {
  state.loadedProfile = p ? Object.assign({}, p) : null;
}

async function initProfiles() {
  const profiles = await window.go.main.App.GetProfiles();
  const active = await window.go.main.App.GetActiveProfile();
  state.connProfiles = profiles || [];
  state.activeProfile = active || '';

  const sel = document.getElementById('conn-profile');
  if (sel) {
    sel.addEventListener('change', onProfileChange);
  }
  document.getElementById('btn-prof-save').addEventListener('click', saveProfileFromForm);
  document.getElementById('btn-prof-add').addEventListener('click', addProfile);
  document.getElementById('btn-prof-rename').addEventListener('click', renameProfile);
  document.getElementById('btn-prof-del').addEventListener('click', deleteProfile);
  document.getElementById('profile-modal-ok').addEventListener('click', confirmProfileModal);

  renderProfiles();

  // Автозагрузка активного профиля (чтобы «Подключить» сразу использовал его).
  const ap = state.connProfiles.find(p => p.name === state.activeProfile);
  if (ap) {
    applyProfile(ap);
    setLoadedProfile(ap);
  }
}

function renderProfiles() {
  const sel = document.getElementById('conn-profile');
  if (!sel) return;
  sel.innerHTML = '';
  if (state.connProfiles.length === 0) {
    const o = document.createElement('option');
    o.value = '';
    o.textContent = 'Профиль не выбран';
    sel.appendChild(o);
  } else {
    state.connProfiles.forEach(p => {
      const o = document.createElement('option');
      o.value = p.name;
      o.textContent = p.name;
      sel.appendChild(o);
    });
  }
  if (state.connProfiles.length) {
    sel.value = state.activeProfile && state.connProfiles.some(p => p.name === state.activeProfile)
      ? state.activeProfile
      : state.connProfiles[0].name;
  } else {
    sel.value = '';
  }
}

async function confirmDiscard() {
  if (!isProfileDirty()) return true;
  return window.go.main.App.ConfirmDialog(
    'Профиль изменён',
    'В форме есть несохранённые изменения. Продолжить без сохранения?'
  );
}

async function onProfileChange() {
  const sel = document.getElementById('conn-profile');
  const name = sel.value;
  if (name && name !== (state.loadedProfile && state.loadedProfile.name)) {
    if (!(await confirmDiscard())) {
      sel.value = state.loadedProfile ? state.loadedProfile.name : '';
      return;
    }
  }
  const p = state.connProfiles.find(x => x.name === name) || null;
  applyProfile(p);
  setLoadedProfile(p);
  if (name) {
    state.activeProfile = name;
    await window.go.main.App.SetActiveProfile(name);
  }
}

async function saveProfileFromForm() {
  const sel = document.getElementById('conn-profile');
  const name = sel.value;
  if (!name) {
    showToast('Сначала выберите или добавьте профиль', 'warn');
    return;
  }
  const p = collectProfile();
  p.name = name;
  await window.go.main.App.SaveProfile(p);
  state.activeProfile = name;
  await window.go.main.App.SetActiveProfile(name);
  const existing = state.connProfiles.find(x => x.name === name);
  if (existing) Object.assign(existing, p);
  else state.connProfiles.push(p);
  setLoadedProfile(p);
  showToast('Профиль сохранён', 'success');
  await saveConfig();
}

async function addProfile() {
  const sel = document.getElementById('conn-profile');
  const currentName = sel.value;
  if (currentName && !(await confirmDiscard())) return;
  state.modalMode = 'add';
  document.getElementById('profile-modal-title').textContent = 'Название нового профиля';
  document.getElementById('profile-modal-input').value = '';
  openProfileModal();
}

async function renameProfile() {
  const sel = document.getElementById('conn-profile');
  const name = sel.value;
  if (!name) { showToast('Сначала выберите профиль', 'warn'); return; }
  if (!(await confirmDiscard())) return;
  state.modalMode = 'rename';
  state.renameFrom = name;
  document.getElementById('profile-modal-title').textContent = 'Переименовать профиль';
  document.getElementById('profile-modal-input').value = name;
  openProfileModal();
}

async function deleteProfile() {
  const sel = document.getElementById('conn-profile');
  const name = sel.value;
  if (!name) { showToast('Сначала выберите профиль', 'warn'); return; }
  const ok = await window.go.main.App.ConfirmDialog('Удалить профиль', `Удалить профиль «${name}»?`);
  if (!ok) return;
  await window.go.main.App.DeleteProfile(name);
  state.connProfiles = state.connProfiles.filter(p => p.name !== name);
  if (state.activeProfile === name) state.activeProfile = '';
  state.loadedProfile = null;
  renderProfiles();
  if (state.connProfiles.length === 0) {
    // Очищаем форму подключения
    setVkHashes('');
    const direct = (id) => { const el = document.getElementById(id); if (el) el.value = ''; };
    direct('srv'); direct('sec'); direct('n-workers'); direct('listen');
    direct('captcha-mode'); direct('obfs-mode'); direct('fingerprint');
  }
  await window.go.main.App.SetActiveProfile(state.activeProfile);
  showToast('Профиль удалён', 'success');
}

function openProfileModal() {
  document.getElementById('profile-modal').style.display = 'flex';
  const inp = document.getElementById('profile-modal-input');
  inp.focus();
  inp.select();
}

function closeProfileModal() {
  document.getElementById('profile-modal').style.display = 'none';
}

async function confirmProfileModal() {
  const inp = document.getElementById('profile-modal-input');
  const name = inp.value.trim();
  closeProfileModal();
  if (!name) { showToast('Введите имя профиля', 'warn'); return; }

  if (state.modalMode === 'add') {
    if (state.connProfiles.some(p => p.name === name)) {
      showToast('Профиль с таким именем уже существует', 'error');
      return;
    }
    const p = collectProfile();
    p.name = name;
    await window.go.main.App.SaveProfile(p);
    state.connProfiles.push(p);
    state.activeProfile = name;
    await window.go.main.App.SetActiveProfile(name);
    setLoadedProfile(p);
    renderProfiles();
    showToast('Профиль добавлен', 'success');
  } else if (state.modalMode === 'rename') {
    const oldName = state.renameFrom;
    if (name === oldName) return;
    if (state.connProfiles.some(p => p.name === name)) {
      showToast('Профиль с таким именем уже существует', 'error');
      return;
    }
    await window.go.main.App.RenameProfile(oldName, name);
    const p = state.connProfiles.find(x => x.name === oldName);
    if (p) {
      p.name = name;
      if (state.activeProfile === oldName) state.activeProfile = name;
      if (state.loadedProfile && state.loadedProfile.name === oldName) state.loadedProfile.name = name;
    }
    renderProfiles();
    showToast('Профиль переименован', 'success');
  }
  await saveConfig();
}

// ── Парсим wdtt:// ─────────────────────────────────────────────────────────────

async function parseWdtt() {
  const link = getVal('wlink');
  const r = await window.go.main.App.ParseWdtt(link);
  if (r.ok) {
    setVal('srv', r.server);
    setVkHashes(r.hash);
    setVal('sec', r.secret);
    log('wdtt:// → ' + r.server, 'success');
  } else {
    log('Неверный формат wdtt://', 'error');
  }
}

// ── Туннель ────────────────────────────────────────────────────────────────────

async function connect() {
  const vk  = getVkHashes();
  const srv = getVal('srv').trim();
  const sec = getVal('sec').trim();
  const n   = getVal('n-workers').trim() || '9';
  const lst = getVal('listen').trim()    || '127.0.0.1:9000';
  const cm  = document.getElementById('captcha-mode').value;
  const fp  = document.getElementById('fingerprint')?.value || 'firefox';
  const om  = document.getElementById('obfs-mode')?.value || 'audio';

  if (!vk)  { log('Введите VK хеш!',    'error'); return; }
  if (!srv) { log('Введите адрес VPS!', 'error'); return; }
  if (!sec) { log('Введите Secret!',    'error'); return; }

  // Нормализуем хеши: убираем URL-префиксы если есть
  const hash = vk.split(',').map(h => {
    h = h.trim();
    return h.includes('/') ? h.split('/').pop() : h;
  }).filter(Boolean).join(',');

  const btn = document.getElementById('btn-connect');
  setBusy(btn, true, '…  Подключение');
  try {
    // device_id генерируется на Go стороне при первом запуске
    const cfg = await window.go.main.App.GetConfig();
    const deviceID = cfg.device_id || '';

    log('Подключение → ' + srv, 'info');
    const err = await window.go.main.App.TunnelStart(hash, srv, sec, n, lst, cm, deviceID, fp, om);
    if (err) {
      log('Ошибка: ' + err, 'error');
      showToast('Не удалось подключиться: ' + err);
      btn.disabled = false;
      return;
    }
    await saveConfig();
  } catch (e) {
    log('Ошибка: ' + e, 'error');
    showToast('Не удалось подключиться: ' + e);
    btn.disabled = false;
  } finally {
    setBusy(btn, false);
    // Состояние кнопки по факту запуска туннеля (setBusy(false) её переключает).
    updateTunnelUI();
  }
}

async function pauseResume() {
  const btn = document.getElementById('btn-pause');
  btn.disabled = true;
  try {
    if (state.tunnelPaused) {
      await window.go.main.App.TunnelResume();
      log('Туннель возобновлён.', 'info');
    } else {
      await window.go.main.App.TunnelPause();
      log('Туннель на паузе.', 'warn');
    }
  } catch (e) {
    log('Ошибка: ' + e, 'error');
    showToast('Не удалось переключить паузу: ' + e);
  } finally {
    btn.disabled = false; // фактическое состояние (текст/disabled) поправит updateTunnelUI()
  }
}

async function stop() {
  const btn = document.getElementById('btn-stop');
  setBusy(btn, true, '…  Остановка');
  try {
    await window.go.main.App.TunnelStop();
    document.getElementById('captcha-panel').style.display = 'none';
    log('Туннель остановлен.', 'warn');
  } catch (e) {
    log('Ошибка остановки: ' + e, 'error');
    showToast('Не удалось остановить туннель: ' + e);
  } finally {
    setBusy(btn, false);
    // Состояние кнопки по факту остановки туннеля (setBusy(false) её переключает).
    updateTunnelUI();
  }
}

async function sendCaptcha() {
  const token = getVal('captcha-token').trim();
  if (!token) { log('Вставьте токен капчи!', 'error'); return; }
  try {
    await window.go.main.App.TunnelSendCaptcha(token);
    setVal('captcha-token', '');
    document.getElementById('captcha-panel').style.display = 'none';
    log('Токен капчи отправлен.', 'info');
  } catch (e) {
    log('Ошибка отправки капчи: ' + e, 'error');
    showToast('Не удалось отправить токен капчи: ' + e);
  }
}

// ── События туннеля ─────────────────────────────────────────────────────────────

function onTunnelStatus(s) {
  state.tunnelRunning = s.running;
  state.tunnelPaused  = s.paused;
  updateTunnelUI();
}

function onWorkers(w) {
  state.activeWorkers = w.active;
  state.totalWorkers  = w.total;
  updateWorkersUI();
}

function onCaptcha(msg) {
  document.getElementById('captcha-panel').style.display = '';
  if (msg === 'webview') {
    log('Требуется капча — должно открыться отдельное окно VK. Реши её там; поле ниже — запасной вариант, если окно не появилось.', 'warn');
    showToast('Требуется капча — открылось отдельное окно VK', 'warn');
  } else {
    log('Требуется капча! Вставьте токен.', 'warn');
  }
}

function onCaptchaDone() {
  document.getElementById('captcha-panel').style.display = 'none';
  log('Капча обработана.', 'info');
}

function updateTunnelUI() {
  const { tunnelRunning: r, tunnelPaused: p } = state;
  const statusEl   = document.getElementById('status-badge');
  const btnConnect = document.getElementById('btn-connect');
  const btnPause   = document.getElementById('btn-pause');
  const btnStop    = document.getElementById('btn-stop');

  const dot = '<span class="dot"></span> ';

  if (r && p) {
    statusEl.innerHTML = dot + 'Туннель пауза';
    statusEl.className = 'badge paused';
    setBtnLabel(btnPause, 'Продолжить');
  } else if (r) {
    statusEl.innerHTML = dot + 'Туннель';
    statusEl.className = 'badge connected';
    setBtnLabel(btnPause, 'Пауза');
  } else {
    statusEl.innerHTML = dot + 'Туннель выкл';
    statusEl.className = 'badge disconnected';
    state.activeWorkers = 0;
    updateWorkersUI();
    state.lastPingMs = null;
    const tb = document.getElementById('traffic-badge');
    if (tb) { tb.textContent = ''; tb.className = 'badge hidden'; }
    const sb2 = document.getElementById('speed-badge');
    if (sb2) { sb2.textContent = ''; sb2.className = 'badge hidden'; }
  }

  btnConnect.disabled = r;
  btnPause.disabled   = !r;
  btnStop.disabled    = !r;
}

function updateWorkersUI() {
  const el = document.getElementById('workers-badge');
  if (state.totalWorkers > 0 && state.tunnelRunning) {
    el.textContent = state.activeWorkers + '/' + state.totalWorkers + ' воркеров';
    el.className = state.activeWorkers > 0 ? 'badge connected' : 'badge paused';
  } else {
    el.className = 'badge hidden';
  }
}

// ── SOCKS5 ────────────────────────────────────────────────────────────────────

async function proxyStart() {
  const host     = getVal('px-host')      || '127.0.0.1';
  const s5port   = getVal('px-port')      || '1080';
  const httpPort = getVal('px-http-port') || '1081';
  const ua       = document.getElementById('px-use-auth').checked;
  const u        = ua ? getVal('px-user') : '';
  const pw       = ua ? getVal('px-pass') : '';

  if (s5port === httpPort) { log('SOCKS5 и HTTP порты должны быть разными!', 'error'); return; }
  if (ua && !u) { log('Введите логин!', 'error'); return; }

  const btn = document.getElementById('btn-pstart');
  setBusy(btn, true, '…  Запуск');
  try {
    const err = await window.go.main.App.ProxyStart(host, s5port, httpPort, u, pw, ua);
    if (err) {
      log('Ошибка запуска: ' + err, 'error');
      showToast('Прокси: ' + err);
      btn.disabled = false;
      return;
    }

    document.getElementById('btn-pstop').disabled = false;

    const via = state.tunnelRunning ? ' (через туннель)' : '';
    let hint = `SOCKS5: ${host}:${s5port}${via}\nHTTP:   ${host}:${httpPort}${via}`;
    if (ua) hint += `\nЛогин: ${u}`;
    const hintEl = document.getElementById('proxy-hint');
    hintEl.textContent = hint;
    hintEl.style.display = '';

    await saveConfig();
  } catch (e) {
    log('Ошибка запуска: ' + e, 'error');
    showToast('Прокси: ' + e);
    btn.disabled = false;
  } finally {
    setBusy(btn, false);
    // Состояние кнопок по факту запуска прокси (setBusy(false) их перевключает).
    const running = state.socksRunning;
    document.getElementById('btn-pstart').disabled = running;
    document.getElementById('btn-pstop').disabled  = !running;
  }
}

async function proxyStop() {
  try {
    await window.go.main.App.SocksStop();
  } catch (e) {
    log('Ошибка остановки: ' + e, 'error');
    showToast('Не удалось остановить прокси: ' + e);
  } finally {
    document.getElementById('btn-pstart').disabled = false;
    document.getElementById('btn-pstop').disabled  = true;
    document.getElementById('proxy-hint').style.display = 'none';
  }
}

// ── System Proxy UI ──────────────────────────────────────────────────────────

async function onSysProxyToggle() {
  const cb = document.getElementById('px-sysproxy');
  try {
    if (cb.checked) {
      const sErr = await window.go.main.App.SystemProxyEnable();
      if (sErr) {
        log('System proxy: ' + sErr, 'error');
        showToast('Системный прокси: ' + sErr);
        cb.checked = false;
      }
    } else {
      await window.go.main.App.SystemProxyDisable();
    }
  } catch (e) {
    log('System proxy: ' + e, 'error');
    showToast('Системный прокси: ' + e);
    cb.checked = false;
  }
}

function onSysProxyStatus(on) {
  const banner = document.getElementById('sysproxy-banner');
  if (banner) banner.style.display = on ? '' : 'none';
  // Синхронизируем чекбокс с Go-состоянием (напр. при выходе из приложения)
  const cb = document.getElementById('px-sysproxy');
  if (cb) cb.checked = on;
}

// Отдельный лог для подключений прокси (→ host:port)
function connLog(msg, lv = 'info') {
  const ts = new Date().toLocaleTimeString('ru', {hour12: false});
  const entry = {ts, msg, lv};
  state.connEntries.push(entry);
  // Всегда добавляем в DOM — бокс может быть скрыт но данные актуальны
  appendConnLine(entry);
}

function appendConnLine({ts, msg, lv}) {
  const box = document.getElementById('conn-log-box');
  const line = document.createElement('div');
  line.innerHTML = `<span class="log-ts">[${ts}]</span> <span class="log-${lv}">${escHtml(msg)}</span>`;
  const stick = isNearBottom(box);
  box.appendChild(line);
  if (stick && state.activeTab === 'logs' && state.activeLogTab === 'conn') {
    box.scrollTop = box.scrollHeight;
  }
}

function onTunnelStats(s) {
  const trafficEl = document.getElementById('traffic-badge');
  const speedEl   = document.getElementById('speed-badge');
  if (!trafficEl || !speedEl) return;
  const mb  = s.trafficMB || 0;
  const kbs = s.speedKBs  || 0;
  const traffic = mb >= 1024
    ? (mb / 1024).toFixed(2) + ' ГБ'
    : mb.toFixed(2) + ' МБ';
  const speed = kbs >= 1024
    ? (kbs / 1024).toFixed(1) + ' МБ/с'
    : kbs.toFixed(0) + ' КБ/с';
  trafficEl.textContent = 'трафик ' + traffic;
  trafficEl.className = 'badge connected';
  speedEl.textContent = 'скорость ' + speed;
  speedEl.className = 'badge connected';
}

function onSocksStatus(running) {
  state.socksRunning = running;
  const el = document.getElementById('proxy-status-label');
  const sb = document.getElementById('socks-badge');
  const btnStart = document.getElementById('btn-pstart');
  const btnStop  = document.getElementById('btn-pstop');
  if (running) {
    el.textContent = '● Локальный прокси включен';
    el.className = 'proxy-status connected';
    sb.innerHTML = '<span class="dot"></span> Локальный прокси';
    sb.className = 'badge connected';
    if (btnStart) btnStart.disabled = true;
    if (btnStop)  btnStop.disabled  = false;
  } else {
    el.textContent = '● Локальный прокси выключен';
    el.className = 'proxy-status disconnected';
    sb.className = 'badge hidden';
    if (btnStart) btnStart.disabled = false;
    if (btnStop)  btnStop.disabled  = true;
  }
}

function onTunnelPing(ms) {
  state.lastPingMs = ms;
  updateStatusBadge();
}

function updateStatusBadge() {
  const sb = document.getElementById('status-badge');
  if (!sb) return;
  if (state.tunnelRunning) {
    const ping = state.lastPingMs != null ? ` ${state.lastPingMs}ms` : '';
    sb.innerHTML = '<span class="dot"></span> Туннель' + ping;
    sb.className = 'badge connected';
  }
}

function onSocksStats(stats) {
  const sb = document.getElementById('socks-badge');
  if (sb && state.socksRunning) {
    sb.innerHTML = stats.active > 0
      ? `<span class="dot"></span> Локальный прокси (${stats.active} соед.)`
      : '<span class="dot"></span> Локальный прокси';
    sb.className = 'badge connected';
  }
}

// Получаем события подключений прокси
function onSocksLog(entry) {
  connLog(entry.msg, entry.lv);
}

// ── Лог маршрутизации (ruleset) ───────────────────────────────────────────

// Маршрутизация пишется в общий лог (вкладка «Общий»).
function routingLog(msg, lv = 'info') {
  log(msg, lv);
}

function toggleAuth() {
  const show = document.getElementById('px-use-auth').checked;
  document.getElementById('auth-fields').style.display = show ? '' : 'none';
}

function genPass() {
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%';
  let p = '';
  for (let i = 0; i < 18; i++) p += chars[Math.floor(Math.random() * chars.length)];
  setVal('px-pass', p);
  const el = document.getElementById('px-pass');
  el.type = 'text';
  log('Новый пароль SOCKS5 сгенерирован.', 'info');
}

function genDeployPass() {
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
  let p = '';
  for (let i = 0; i < 16; i++) p += chars[Math.floor(Math.random() * chars.length)];
  setVal('d-tunnel-pwd', p);
}

// ── Роутинг (ruleset-based routing) ───────────────────────────────────────

// Текущий список правил в редакторе (до сохранения).
let routingDraft = [];

function routingPolicyLabel(p) {
  switch (p) {
    case 'block':  return 'block (заблокировать)';
    case 'direct': return 'direct (напрямую)';
    case 'proxy':  return 'proxy (через туннель)';
    default:       return 'proxy (через туннель)';
  }
}

// Разбирает строку правила на {type, value} для отображения.
// types: geosite/geoip (из ruleset:...), domain, domain-suffix, keyword, regex, cidr, ip.
function splitRule(rule) {
  const s = rule || '';
  const m = /^(ruleset:|domain-suffix:|domain:|keyword:|regex:|cidr:|ip:)/i.exec(s);
  if (!m) return {type: s, value: ''};
  const prefix = m[1].toLowerCase();
  if (prefix === 'ruleset:') {
    const rest = s.slice(m[1].length);
    const idx = rest.indexOf('-');
    if (idx < 0) return {type: rest, value: ''};
    return {type: rest.slice(0, idx), value: rest.slice(idx + 1)};
  }
  return {type: prefix.slice(0, -1), value: s.slice(m[1].length)};
}

function routingRuleType(rule) {
  return splitRule(rule).type;
}

function routingRuleGroup(rule) {
  return splitRule(rule).value;
}

function renderRoutingList() {
  const container = document.getElementById('ruleset-list');
  if (!container) return;
  if (!routingDraft.length) {
    container.innerHTML = '<p class="hint">Правил пока нет. Добавьте правило выше.</p>';
    return;
  }
  container.innerHTML = routingDraft.map((rs, i) => `
    <div class="ruleset-item">
      <input type="checkbox" class="rr-enable" data-i="${i}" ${rs.enable ? 'checked' : ''} title="Включить правило">
      <span class="ruleset-type">${escHtml(routingRuleType(rs.rule))}</span>
      <span class="ruleset-group mono">${escHtml(routingRuleGroup(rs.rule) || rs.rule)}</span>
      <select class="sel rr-policy" data-i="${i}" title="Политика">
        <option value="proxy"  ${rs.policy === 'proxy'  ? 'selected' : ''}>proxy</option>
        <option value="direct" ${rs.policy === 'direct' ? 'selected' : ''}>direct</option>
        <option value="block"  ${rs.policy === 'block'  ? 'selected' : ''}>block</option>
      </select>
      <button class="icon-btn rr-first" data-i="${i}" title="В начало">${ICO.first}</button>
      <button class="icon-btn rr-up"   data-i="${i}" title="Вверх">${ICO.up}</button>
      <button class="icon-btn rr-down" data-i="${i}" title="Вниз">${ICO.down}</button>
      <button class="icon-btn rr-last" data-i="${i}" title="В конец">${ICO.last}</button>
      <button class="icon-btn rr-del"  data-i="${i}" title="Удалить">${ICO.close}</button>
    </div>
  `).join('');

  // Обработчики
  container.querySelectorAll('.rr-enable').forEach(el => {
    el.addEventListener('change', () => { const i = +el.dataset.i; if (routingDraft[i]) routingDraft[i].enable = el.checked; });
  });
  container.querySelectorAll('.rr-policy').forEach(el => {
    el.addEventListener('change', () => { const i = +el.dataset.i; if (routingDraft[i]) routingDraft[i].policy = el.value; });
  });
  container.querySelectorAll('.rr-first').forEach(el => {
    el.addEventListener('click', () => { const i = +el.dataset.i; if (i > 0) { const [t] = routingDraft.splice(i, 1); routingDraft.unshift(t); renderRoutingList(); } });
  });
  container.querySelectorAll('.rr-last').forEach(el => {
    el.addEventListener('click', () => { const i = +el.dataset.i; if (i < routingDraft.length - 1) { const [t] = routingDraft.splice(i, 1); routingDraft.push(t); renderRoutingList(); } });
  });
  container.querySelectorAll('.rr-up').forEach(el => {
    el.addEventListener('click', () => { const i = +el.dataset.i; if (i > 0) { const t = routingDraft[i-1]; routingDraft[i-1] = routingDraft[i]; routingDraft[i] = t; renderRoutingList(); } });
  });
  container.querySelectorAll('.rr-down').forEach(el => {
    el.addEventListener('click', () => { const i = +el.dataset.i; if (i < routingDraft.length - 1) { const t = routingDraft[i+1]; routingDraft[i+1] = routingDraft[i]; routingDraft[i] = t; renderRoutingList(); } });
  });
  container.querySelectorAll('.rr-del').forEach(el => {
    el.addEventListener('click', () => { const i = +el.dataset.i; routingDraft.splice(i, 1); renderRoutingList(); });
  });
}

async function loadRoutingTab() {
  const [rulesets, status, groups] = await Promise.all([
    window.go.main.App.GetRulesets(),
    window.go.main.App.GetRulesetStatus(),
    window.go.main.App.GetRulesetGroups(),
  ]);
  routingDraft = (rulesets || []).map(r => ({ rule: r.rule, policy: r.policy || 'proxy', enable: r.enable }));
  renderRoutingList();
  updateRoutingStatus(status);
  state.rulesetGroups = groups;
  populateRuleOptions(groups);
}

function updateRoutingStatus(status) {
  const el = document.getElementById('routing-status');
  if (!el) return;
  // Встроенные правила (domain:/keyword:/regex:/cidr:/ip:) не требуют скачивания.
  const needsRulesets = (routingDraft || []).some(r => /^ruleset:/i.test(r.rule));
  if (status && status.loaded) {
    const when = status.last_update ? new Date(status.last_update).toLocaleString('ru') : 'неизвестно';
    el.textContent = '● Правила загружены (обновлены: ' + when + ')';
    el.className = 'proxy-status connected';
  } else if (routingDraft.length && !needsRulesets) {
    el.textContent = '● Маршрутизация активна (встроенные правила, скачивание не требуется)';
    el.className = 'proxy-status connected';
  } else if (routingDraft.length) {
    el.textContent = '● Правила не загружены — нажмите «Обновить правила», чтобы скачать geosite/geoip';
    el.className = 'proxy-status disconnected';
  } else {
    el.textContent = '● Правил нет — маршрутизация неактивна';
    el.className = 'proxy-status disconnected';
  }
}

function addRoutingRule() {
  const inp = document.getElementById('rr-new');
  let rule = (inp.value || '').trim();
  if (!rule) { showToast('Введите правило!'); return; }
  // Нормализуем правило: если нет известного префикса, добавляем "ruleset:"
  if (!/^(ruleset:|domain-suffix:|domain:|keyword:|regex:|cidr:|ip:)/i.test(rule)) {
    rule = 'ruleset:' + rule;
  }
  routingDraft.push({ rule, policy: 'proxy', enable: true });
  inp.value = '';
  renderRoutingList();
  closeRuleSuggest();
}

async function saveRoutingRules() {
  const configs = routingDraft.map(r => ({ rule: r.rule, policy: r.policy, enable: r.enable }));
  const btn = document.getElementById('btn-rules-save');
  const saved = document.getElementById('rules-saved');
  if (btn) { btn.disabled = true; setBtnLabel(btn, 'Сохраняю…'); }
  if (saved) saved.style.display = 'none';
  const err = await window.go.main.App.SetRulesets(configs, document.getElementById('rr-default-policy')?.value || 'proxy');
  if (err) {
    if (btn) { btn.disabled = false; setBtnLabel(btn, 'Сохранить'); }
    showToast('Маршрутизация: ' + err); routingLog('Маршрутизация: ' + err, 'error'); return;
  }
  // Убеждаемся, что правила загружены и применены к прокси.
  await window.go.main.App.EnsureRulesetsLoaded();
  // Сохраняем и остальные настройки (DNS и т.п.) — SaveConfig на Go-стороне
  // сохраняет переданные правила, т.к. collectConfig не отправляет rulesets.
  await saveConfig();
  routingLog('Маршрутизация: правила маршрутизации сохранены.', 'success');
  showToast('Правила маршрутизации сохранены.', 'success');
  const status = await window.go.main.App.GetRulesetStatus();
  updateRoutingStatus(status);

  // После сохранения обновляем список групп
  const groups = await window.go.main.App.GetRulesetGroups();
  state.rulesetGroups = groups;
  populateRuleOptions(groups);
  renderRuleSuggest();

  // Визуальный индикатор: кнопка «Сохранить» и надпись «Сохранено»
  if (btn) { setBtnLabel(btn, 'Сохранено'); btn.classList.add('saved'); }
  if (saved) saved.style.display = 'inline';
  setTimeout(() => {
    if (btn) { setBtnLabel(btn, 'Сохранить'); btn.classList.remove('saved'); btn.disabled = false; }
    if (saved) saved.style.display = 'none';
  }, 1500);
}

// ── Помощь по автоподсказкам ──────────────────────────────────────────────

// Примеры встроенных правил (не требуют скачивания дата-файлов).
const INLINE_RULE_EXAMPLES = [
  'domain:example.com',
  'domain-suffix:example.com',
  'keyword:example',
  'regex:\\.example\\.com$',
  'cidr:10.0.0.0/8',
  'ip:1.2.3.4',
];

// Заполняет список подсказок для автоподсказок в поле ввода правил
function populateRuleOptions(groups) {
  const datalist = document.getElementById('ruleset-options');
  if (!datalist) return;
  
  // Очищаем существующие опции
  datalist.innerHTML = '';
  
  // Добавляем опции для geosite групп
  groups.geosite.forEach(group => {
    const option = document.createElement('option');
    option.value = group;
    datalist.appendChild(option);
  });
  
  // Добавляем опции для geoip групп
  groups.geoip.forEach(group => {
    const option = document.createElement('option');
    option.value = group;
    datalist.appendChild(option);
  });

  // Добавляем примеры встроенных правил
  INLINE_RULE_EXAMPLES.forEach(example => {
    const option = document.createElement('option');
    option.value = example;
    datalist.appendChild(option);
  });
}

// ── Дропдаун подсказок ──────────────────────────────────────────────────

let ruleSuggestState = {
  open: false,
  selectedIndex: -1,
  filteredGroups: { geosite: [], geoip: [] }
};

function openRuleSuggest() {
  const input = document.getElementById('rr-new');
  if (!input) return;
  
  ruleSuggestState.open = true;
  const suggestBox = document.getElementById('rr-suggest');
  if (suggestBox) {
    suggestBox.style.display = 'block';
    renderRuleSuggest();
  }
}

function closeRuleSuggest() {
  ruleSuggestState.open = false;
  const suggestBox = document.getElementById('rr-suggest');
  if (suggestBox) {
    suggestBox.style.display = 'none';
  }
  ruleSuggestState.selectedIndex = -1;
}

function renderRuleSuggest() {
  const suggestBox = document.getElementById('rr-suggest');
  if (!suggestBox) return;
  
  const input = document.getElementById('rr-new');
  if (!input) return;
  
  const inputValue = (input.value || '').trim();
  
  // Фильтруем группы по введенному тексту
  let filteredGeosite = [];
  let filteredGeoip = [];
  
  if (inputValue) {
    const searchLower = inputValue.toLowerCase();
    filteredGeosite = state.rulesetGroups.geosite.filter(group => 
      group.toLowerCase().includes(searchLower)
    );
    filteredGeoip = state.rulesetGroups.geoip.filter(group => 
      group.toLowerCase().includes(searchLower)
    );
  } else {
    // Если ничего не введено, показываем все
    filteredGeosite = state.rulesetGroups.geosite;
    filteredGeoip = state.rulesetGroups.geoip;
  }
  
  ruleSuggestState.filteredGroups = {
    geosite: filteredGeosite,
    geoip: filteredGeoip
  };
  
  // Генерируем HTML для подсказок
  let html = '';
  
  if (filteredGeosite.length > 0) {
    html += `<div class="rr-suggest-head">geosite</div>`;
    filteredGeosite.forEach((group, index) => {
      html += `<div class="rr-suggest-item" data-index="${index}" data-type="geosite" data-value="${group}">
        <span class="rr-type">geosite-</span>
        ${group.substring(8)}
      </div>`;
    });
  }
  
  if (filteredGeoip.length > 0) {
    html += `<div class="rr-suggest-head">geoip</div>`;
    filteredGeoip.forEach((group, index) => {
      html += `<div class="rr-suggest-item" data-index="${index}" data-type="geoip" data-value="${group}">
        <span class="rr-type">geoip-</span>
        ${group.substring(6)}
      </div>`;
    });
  }

  // Встроенные правила — фильтруем примеры по введённому тексту.
  const filteredInline = INLINE_RULE_EXAMPLES.filter(e => {
    if (!inputValue) return true;
    return e.toLowerCase().includes(inputValue.toLowerCase());
  });
  if (filteredInline.length > 0) {
    html += `<div class="rr-suggest-head">встроенные (без скачивания)</div>`;
    filteredInline.forEach(example => {
      html += `<div class="rr-suggest-item" data-type="inline" data-value="${escHtml(example)}">
        <span class="rr-type">${escHtml(example.split(':')[0] + ':')}</span>
        ${escHtml(example.split(':').slice(1).join(':'))}
      </div>`;
    });
  }
  
  if (filteredGeosite.length === 0 && filteredGeoip.length === 0 && filteredInline.length === 0) {
    // Если ничего не найдено, показываем сообщение
    if (state.rulesetGroups.geosite.length === 0 && state.rulesetGroups.geoip.length === 0) {
      html = `<div class="rr-suggest-item rr-suggest-empty">
        Правила не загружены — встроенные правила (domain:/keyword:/...) работают без скачивания
      </div>`;
    } else {
      html = `<div class="rr-suggest-item rr-suggest-empty">
        Нет подходящих групп
      </div>`;
    }
  }
  
  suggestBox.innerHTML = html;
  
  // Добавляем обработчики событий для элементов подсказок
  const items = suggestBox.querySelectorAll('.rr-suggest-item');
  items.forEach((item, index) => {
    item.addEventListener('click', function() {
      const value = this.getAttribute('data-value');
      const input = document.getElementById('rr-new');
      if (input) {
        input.value = value;
        input.focus();
        closeRuleSuggest();
      }
    });
  });
}

function handleRuleSuggestKeydown(e) {
  if (!ruleSuggestState.open) return;
  
  const items = document.querySelectorAll('#rr-suggest .rr-suggest-item');
  if (items.length === 0) return;
  
  switch (e.key) {
    case 'ArrowDown':
      e.preventDefault();
      ruleSuggestState.selectedIndex = Math.min(ruleSuggestState.selectedIndex + 1, items.length - 1);
      updateRuleSuggestSelection();
      break;
    case 'ArrowUp':
      e.preventDefault();
      ruleSuggestState.selectedIndex = Math.max(ruleSuggestState.selectedIndex - 1, 0);
      updateRuleSuggestSelection();
      break;
    case 'Enter':
      e.preventDefault();
      if (ruleSuggestState.selectedIndex >= 0 && items[ruleSuggestState.selectedIndex]) {
        items[ruleSuggestState.selectedIndex].click();
      }
      break;
    case 'Escape':
      closeRuleSuggest();
      break;
  }
}

function updateRuleSuggestSelection() {
  const items = document.querySelectorAll('#rr-suggest .rr-suggest-item');
  items.forEach((item, index) => {
    if (index === ruleSuggestState.selectedIndex) {
      item.classList.add('active');
    } else {
      item.classList.remove('active');
    }
  });
}

// ── Работа с полем ввода правил ──────────────────────────────────────────

function onRuleInput() {
  const input = document.getElementById('rr-new');
  if (!input) return;
  
  const value = input.value.trim();
  
  if (value) {
    // Открываем дропдаун при вводе
    if (!ruleSuggestState.open) {
      openRuleSuggest();
    }
    renderRuleSuggest();
  } else {
    // Закрываем дропдаун при очистке
    closeRuleSuggest();
  }
}

// Флаг активного обновления правил — прогресс-бар показываем только когда
// обновление запущено пользователем (а не фоновой загрузкой при старте).
let rulesUpdating = false;

function onRulesetProgress(data) {
  if (!rulesUpdating) return;
  const bar = document.getElementById('rules-progress-bar');
  const label = document.getElementById('rules-progress-label');
  if (!bar || !label) return;
  const pct   = data && typeof data.pct === 'number' ? data.pct : -1;
  const stage = (data && data.stage) || '';
  const stageName = stage === 'geoip'   ? 'geoip.dat'
    : stage === 'geosite' ? 'geosite.dat'
    : 'правил';
  if (pct >= 0) {
    bar.classList.remove('indeterminate');
    bar.style.width = pct + '%';
    label.textContent = `Загрузка ${stageName}… ${pct}%`;
  } else {
    bar.classList.add('indeterminate');
    bar.style.width = '';
    label.textContent = `Загрузка ${stageName}…`;
  }
}

async function updateAllRulesets() {
  const btn = document.getElementById('btn-rules-update');
  setBusy(btn, true, '…  Обновление');
  const bar = document.getElementById('rules-progress');
  const barEl = document.getElementById('rules-progress-bar');
  if (barEl) { barEl.style.width = ''; barEl.classList.add('indeterminate'); }
  const label = document.getElementById('rules-progress-label');
  if (label) label.textContent = 'Загрузка правил…';
  if (bar) bar.style.display = '';
  rulesUpdating = true;
  try {
    const err = await window.go.main.App.UpdateRulesets();
    if (err) { showToast('Маршрутизация: ' + err); routingLog('Маршрутизация: ' + err, 'error'); }
    const status = await window.go.main.App.GetRulesetStatus();
    updateRoutingStatus(status);
    
    // После обновления правил обновляем список групп
    const groups = await window.go.main.App.GetRulesetGroups();
    state.rulesetGroups = groups;
    populateRuleOptions(groups);
    renderRuleSuggest();
  } catch (e) {
    routingLog('Маршрутизация: ' + e, 'error');
    showToast('Маршрутизация: ' + e);
  } finally {
    rulesUpdating = false;
    setBusy(btn, false);
    if (bar) bar.style.display = 'none';
  }
}


// ── Deploy ────────────────────────────────────────────────────────────────────

async function deploy() {
  const ip   = getVal('d-ip').trim();
  const port = getVal('d-port').trim() || '22';
  const user = getVal('d-user').trim();
  const pwd  = getVal('d-pwd');
  const wg   = getVal('d-wg').trim()   || '51820';
  const wdtt = getVal('d-wdtt').trim() || '56000';

  if (!ip || !user) { deployLog('Введите IP и пользователя!', 'error'); return; }

  const btn = document.getElementById('btn-deploy');
  try {
    deployLog('Получаю fingerprint ' + ip + ':' + port + '...', 'info');
    const fp = await window.go.main.App.DeployGetFingerprint(ip, port);
    if (!fp.ok) {
      deployLog('Не удалось получить fingerprint: ' + (fp.error || ''), 'error');
      showToast('Deploy: не удалось получить fingerprint');
      return;
    }

    const confirmed = await window.go.main.App.ConfirmDialog(
      'Подтверждение подключения',
      `Подключение к ${ip}\n\nSHA-256 fingerprint хост-ключа:\n${fp.fingerprint}\n\nЭто ваш сервер? Подключиться?`
    );
    if (!confirmed) { deployLog('Деплой отменён.', 'warn'); return; }

    clearDeployLog();
    const tunnelPwd = getVal('d-tunnel-pwd') || '';
    if (!tunnelPwd) { deployLog('Укажите пароль туннеля!', 'error'); return; }
    const adminID   = getVal('d-admin-id')   || '';
    const botToken  = getVal('d-bot-token')  || '';

    setBusy(btn, true, '…  Деплой');
    await window.go.main.App.DeployRun(ip, port, user, pwd, wg, wdtt, tunnelPwd, adminID, botToken, fp.fingerprint);
  } catch (e) {
    deployLog('Ошибка деплоя: ' + e, 'error');
    showToast('Deploy: ' + e);
  } finally {
    setBusy(btn, false);
  }
}

async function undeploy() {
  const ip   = getVal('d-ip').trim();
  const port = getVal('d-port').trim() || '22';
  const user = getVal('d-user').trim();
  const pwd  = getVal('d-pwd');
  const wg   = getVal('d-wg').trim()   || '51820';
  const wdtt = getVal('d-wdtt').trim() || '56000';

  if (!ip || !user) { deployLog('Введите IP и пользователя!', 'error'); return; }

  const btn = document.getElementById('btn-undeploy');
  try {
    // Fingerprint: берём сохранённый, или запрашиваем заново.
    const cfg = await window.go.main.App.GetConfig();
    let fingerprint = (cfg && cfg.fingerprint) || '';
    if (!fingerprint) {
      deployLog('Fingerprint не сохранён — получаю с сервера...', 'info');
      const fp = await window.go.main.App.DeployGetFingerprint(ip, port);
      if (!fp.ok) {
        deployLog('Не удалось получить fingerprint: ' + (fp.error || ''), 'error');
        showToast('Undeploy: не удалось получить fingerprint');
        return;
      }
      const ok = await window.go.main.App.ConfirmDialog(
        'Подтверждение',
        `SHA-256 fingerprint:\n${fp.fingerprint}\n\nЭто ваш сервер?`
      );
      if (!ok) { deployLog('Удаление отменено.', 'warn'); return; }
      fingerprint = fp.fingerprint;
    }

    const confirmed = await window.go.main.App.ConfirmDialog('Подтверждение', `Удалить wdtt-server с ${ip}?`);
    if (!confirmed) return;

    clearDeployLog();
    setBusy(btn, true, '…  Удаление');
    await window.go.main.App.UndeployRun(ip, port, user, pwd, wg, wdtt, fingerprint);
  } catch (e) {
    deployLog('Ошибка удаления: ' + e, 'error');
    showToast('Undeploy: ' + e);
  } finally {
    setBusy(btn, false);
  }
}

// ── Импорт / экспорт конфигурации ─────────────────────────────────────────────

async function exportConfig() {
  const content = await window.go.main.App.ExportConfig();
  if (!content) { log('Не удалось сериализовать конфигурацию.', 'error'); return; }
  const ok = await window.go.main.App.SaveConfigDialog(content);
  if (ok) log('Конфигурация экспортирована.', 'success');
}

async function importConfig() {
  const confirmed = await window.go.main.App.ConfirmDialog(
    'Импорт конфигурации',
    'Импорт полностью заменит текущую конфигурацию. Продолжить?'
  );
  if (!confirmed) { log('Импорт отменён.', 'warn'); return; }
  const res = await window.go.main.App.ImportConfig();
  if (!res.ok) {
    log('Ошибка импорта: ' + (res.error || 'неизвестная'), 'error');
    showToast('Не удалось импортировать конфигурацию');
    return;
  }
  log('Конфигурация импортирована.', 'success');
  await applyImportedConfig(res.config);
}

async function applyImportedConfig(cfg) {
  if (cfg) loadConfig(cfg);
  if (cfg && cfg.theme) applyTheme(cfg.theme);
  loadRoutingTab();
}

// ── WireGuard ─────────────────────────────────────────────────────────────────

function onWgConfig(conf) {
  const el = document.getElementById('wg-conf');
  if (el) {
    el.value = conf;
    log('WireGuard конфиг получен — вкладка WireGuard обновлена.', 'success');
  }
}

async function wgSave() {
  const conf = document.getElementById('wg-conf').value;
  if (!conf.trim()) { log('Конфиг пустой!', 'error'); return; }
  const ok = await window.go.main.App.SaveWgConfig(conf);
  if (ok) log('WireGuard конфиг сохранён.', 'success');
}

async function wgOpen() {
  const content = await window.go.main.App.OpenFileDialog('Открыть WireGuard конфиг');
  if (content) {
    document.getElementById('wg-conf').value = content;
    log('Конфиг загружен.', 'info');
  }
}

async function wgCopy() {
  const conf = document.getElementById('wg-conf').value;
  if (!conf.trim()) { log('Конфиг пустой!', 'error'); return; }
  try {
    await navigator.clipboard.writeText(conf);
    const hint = document.getElementById('wg-copy-hint');
    if (hint) {
      hint.style.display = '';
      setTimeout(() => { hint.style.display = 'none'; }, 2000);
    }
  } catch {
    log('Не удалось скопировать — используйте Ctrl+A, Ctrl+C в поле.', 'warn');
  }
}

// ── Лог ───────────────────────────────────────────────────────────────────────

// Находится ли скроллбокс у нижней границы (порог ~40px). Если юзер отмотал
// лог назад — автопрокрутка не срабатывает, чтобы не мешать просмотру.
function isNearBottom(box) {
  return box.scrollHeight - box.scrollTop - box.clientHeight < 40;
}

function log(msg, lv = 'info') {
  const ts = new Date().toLocaleTimeString('ru', {hour12: false});
  const entry = {ts, msg, lv};
  state.logEntries.push(entry);
  const f = state.logFilter;
  if (f === 'all' || lv === f || (f === 'warn' && (lv === 'warn' || lv === 'error'))) {
    appendLogLine(entry);
  }
}

function onLog(entry) {
  log(entry.msg, entry.lv);
}

function appendLogLine({ts, msg, lv}) {
  const box = document.getElementById('general-log-box');
  const line = document.createElement('div');
  line.innerHTML = `<span class="log-ts">[${ts}]</span> <span class="log-${lv}">${escHtml(msg)}</span>`;
  const stick = isNearBottom(box);
  box.appendChild(line);
  // Автопрокрутка только когда вкладка логов активна и юзер был внизу
  if (stick && state.activeTab === 'logs' && state.activeLogTab === 'general') {
    box.scrollTop = box.scrollHeight;
  }
}

function setFilter(f) {
  state.logFilter = f;
  const box = document.getElementById('general-log-box');
  box.innerHTML = '';
  state.logEntries.forEach(e => {
    if (f === 'all' || e.lv === f || (f === 'warn' && (e.lv === 'warn' || e.lv === 'error'))) {
      appendLogLine(e);
    }
  });
  box.scrollTop = box.scrollHeight;
}

async function saveLog() {
  const entries = state.activeLogTab === 'general' ? state.logEntries
    : state.connEntries;
  const lines = entries
    .map(e => `[${e.ts}] [${e.lv.toUpperCase().padEnd(7)}] ${e.msg}`)
    .join('\n');
  const ok = await window.go.main.App.SaveFileDialog(lines);
  if (ok) log('Лог сохранён.', 'success');
}

function clearLog() {
  if (state.activeLogTab === 'general') {
    state.logEntries = [];
    document.getElementById('general-log-box').innerHTML = '';
  } else {
    state.connEntries = [];
    document.getElementById('conn-log-box').innerHTML = '';
  }
}

// ── Тема ─────────────────────────────────────────────────────────────────────

async function initTheme() {
  const saved = await window.go.main.App.GetTheme();
  if (saved) { applyTheme(saved); return; }
  // Тема ещё не выбрана — берём предпочтение ОС.
  const prefersLight = window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches;
  applyTheme(prefersLight ? 'light' : 'dark');
  // Сохраняем предпочтение ОС как пользовательский выбор
  if (window.go?.main?.App?.SetTheme) {
    window.go.main.App.SetTheme(prefersLight ? 'light' : 'dark');
  }
}

function applyTheme(theme) {
  const btn = document.getElementById('theme-btn');
  if (theme === 'light') {
    document.body.classList.add('light');
    if (btn) btn.innerHTML = ICO.sun;
  } else {
    document.body.classList.remove('light');
    if (btn) btn.innerHTML = ICO.moon;
  }
  // Сохраняем в конфиг Go — localStorage сбрасывается при перезапуске Wails
  if (window.go?.main?.App?.SetTheme) {
    window.go.main.App.SetTheme(theme);
  }
}

function toggleTheme() {
  const isLight = document.body.classList.contains('light');
  applyTheme(isLight ? 'dark' : 'light');
}

// ── VK хеши — динамический список ────────────────────────────────────────────

function addVkHash() {
  const list = document.getElementById('vk-list');
  const row = document.createElement('div');
  row.className = 'vk-hash-row';
  row.innerHTML = `
    <input type="text" class="inp vk-hash" placeholder="e5rA_78w7leYpoKiqtDb...">
    <button class="icon-btn" onclick="pasteVkHash(this)" title="Вставить" aria-label="Вставить хеш из буфера обмена">${ICO.clipboard}</button>
    <button class="icon-btn vk-remove" onclick="removeVkHash(this)" title="Удалить" aria-label="Удалить хеш">${ICO.close}</button>
  `;
  list.appendChild(row);
  updateRemoveButtons();
  row.querySelector('.vk-hash').focus();
}

function removeVkHash(btn) {
  btn.closest('.vk-hash-row').remove();
  updateRemoveButtons();
}

function pasteVkHash(btn) {
  const inp = btn.closest('.vk-hash-row').querySelector('.vk-hash');
  navigator.clipboard.readText().then(t => { if (t) inp.value = t.trim(); }).catch(() => {});
}

function updateRemoveButtons() {
  const rows = document.querySelectorAll('.vk-hash-row');
  rows.forEach(row => {
    const btn = row.querySelector('.vk-remove');
    if (btn) btn.style.display = rows.length > 1 ? '' : 'none';
  });
}

function getVkHashes() {
  return [...document.querySelectorAll('.vk-hash')]
    .map(i => i.value.trim()).filter(Boolean).join(',');
}

function setVkHashes(hashStr) {
  const hashes = (hashStr || '').split(',').map(h => h.trim()).filter(Boolean);
  const list = document.getElementById('vk-list');
  if (!list) return;
  list.innerHTML = '';
  (hashes.length ? hashes : ['']).forEach((h) => {
    const row = document.createElement('div');
    row.className = 'vk-hash-row';
    const inp = document.createElement('input');
    inp.type = 'text'; inp.className = 'inp vk-hash';
    inp.placeholder = 'e5rA_78w7leYpoKiqtDb...';
    inp.value = h; // прямое присвоение — безопасно, не экранирует символы хеша
    const btnP = document.createElement('button');
    btnP.className = 'icon-btn'; btnP.innerHTML = ICO.clipboard;
    btnP.title = 'Вставить'; btnP.setAttribute('aria-label', 'Вставить хеш из буфера обмена');
    btnP.onclick = function() { pasteVkHash(this); };
    const btnR = document.createElement('button');
    btnR.className = 'icon-btn vk-remove'; btnR.innerHTML = ICO.close;
    btnR.title = 'Удалить'; btnR.setAttribute('aria-label', 'Удалить хеш');
    btnR.onclick = function() { removeVkHash(this); };
    row.appendChild(inp); row.appendChild(btnP); row.appendChild(btnR);
    list.appendChild(row);
  });
  updateRemoveButtons();
}

// ── Deploy лог ───────────────────────────────────────────────────────────────

function deployLog(msg, lv = 'info') {
  const box = document.getElementById('deploy-log-box');
  if (!box) return;
  const ts = new Date().toLocaleTimeString('ru', {hour12: false});
  const line = document.createElement('div');
  line.innerHTML = `<span class="log-ts">[${ts}]</span> <span class="log-${lv}">${escHtml(msg)}</span>`;
  const stick = isNearBottom(box);
  box.appendChild(line);
  if (stick) box.scrollTop = box.scrollHeight;
}

function onDeployLog(entry) {
  const box = document.getElementById('deploy-log-box');
  if (!box) return;
  const line = document.createElement('div');
  line.innerHTML = `<span class="log-ts">[${entry.ts}]</span> <span class="log-${entry.lv}">${escHtml(entry.msg)}</span>`;
  const stick = isNearBottom(box);
  box.appendChild(line);
  if (stick) box.scrollTop = box.scrollHeight;
}

function clearDeployLog() {
  const box = document.getElementById('deploy-log-box');
  if (box) box.innerHTML = '';
}

// ── Переключение вкладок лога ────────────────────────────────────────────────

function switchLogTab(tab) {
  state.activeLogTab = tab;

  document.getElementById('ltab-general').classList.toggle('active', tab === 'general');
  document.getElementById('ltab-conn').classList.toggle('active',    tab === 'conn');

  document.getElementById('general-log-box').style.display = tab === 'general' ? '' : 'none';
  document.getElementById('conn-log-box').style.display    = tab === 'conn'    ? '' : 'none';

  // Фильтр показываем только для общего лога
  document.getElementById('log-filter-row').style.display = tab === 'general' ? '' : 'none';
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function getVal(id) { return (document.getElementById(id) || {}).value || ''; }
function setVal(id, v) { const el = document.getElementById(id); if (el && v) el.value = v; }

function togglePass(id, cb) {
  const el = document.getElementById(id);
  if (el) el.type = cb.checked ? 'text' : 'password';
}

async function pasteInput(id) {
  try {
    const text = await navigator.clipboard.readText();
    setVal(id, text.trim());
  } catch {
    // Clipboard недоступен — ничего не делаем
  }
}

function escHtml(s) {
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

// ── Help text ─────────────────────────────────────────────────────────────────
function helpText(ver) {
  return `WinDTT  v${ver}
WireGuard over VK TURN — туннель через звонки VK

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
ПОДКЛЮЧЕНИЕ
  1. Вставьте wdtt:// ссылку → «Разобрать»,
     либо заполните вручную: VK хеш, VPS адрес (IP:PORT), Secret.
  2. Нажмите «▶ Подключить».
  3. Конфиг WireGuard появится на вкладке WireGuard —
     скопируйте его в WireGuard клиент.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
ПРОКСИ (SOCKS5 + HTTP)
  Запускается на вкладке Прокси, оба протокола одновременно.
  После подключения трафик автоматически идёт через туннель.
  Настройте браузер:
    Firefox: Настройки → Сеть → Ручная настройка прокси
    Chrome:  расширение Proxy SwitchyOmega

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
МАРШРУТИЗАЦИЯ (по правилам)
  Задаёт, куда направлять трафик по правилам.
  Встроенные правила (без скачивания):
    domain:<домен>        — точное совпадение домена
    domain-suffix:<домен> — домен и все поддомены
    keyword:<текст>       — подстрока в домене
    regex:<шаблон>        — регулярное выражение по домену
    cidr:<подсеть>        — IP-подсеть (cidr:10.0.0.0/8)
    ip:<адрес>            — конкретный IP (ip:1.2.3.4)
  Правила из дата-файлов (нужно «Обновить правила»):
    ruleset:geosite-<группа>  — домены из geosite
    ruleset:geoip-<группа>    — подсети из geoip
  Примеры:
    domain-suffix:sobaka.com → block
    keyword:youtube          → proxy
    ruleset:geosite-category-ru → proxy
    ruleset:geoip-private       → direct
  Правила можно вводить без префикса "ruleset:"
  (geosite-... / geoip-...) — он добавляется автоматически.
  Политики: block (заблокировать),
    direct (напрямую в обход туннеля),
    proxy (через туннель).
  Правила применяются сверху вниз — первое совпадение.
  Трафик без совпадения идёт по выбору «Трафик по умолчанию»
  (вверху вкладки): proxy / direct / block.
  Кнопка «Обновить правила» скачивает geosite/geoip
  из runetfreedom/russia-v2ray-rules-dat.
  DNS: укажите IP серверов через запятую (1.1.1.1, 8.8.8.8),
    пусто — использовать DNS из конфига сервера.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
DEPLOY
  Устанавливает wdtt-server на ваш VPS.
  Требуется: SSH доступ root, Ubuntu/Debian.
  
  Пароль туннеля — обязателен, запомните его.
  Этот же пароль вводится в поле Secret
  при подключении клиента.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
WIREGUARD
  Конфиг загружается автоматически при подключении.
  Кнопки: Открыть · Копировать · Скачать .conf.
  Используйте с официальным WireGuard клиентом.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
ШАПКА
  Туннель 42ms — статус и пинг туннеля
  трафик 1.24 МБ   — суммарный трафик
  скорость 128 КБ/с — текущая скорость
  12/24 воркеров    — активных/всего
  Локальный прокси (3 соед.) — статус прокси`;
}
