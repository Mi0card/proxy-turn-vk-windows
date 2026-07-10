'use strict';

// ── State ─────────────────────────────────────────────────────────────────────

const state = {
  tunnelRunning:  false,
  tunnelPaused:   false,
  socksRunning:   false,
  logEntries:     [],   // [{ts, msg, lv}] — туннель
  socksEntries:   [],   // [{ts, msg, lv}] — SOCKS5
  logFilter:      'all',
  activeLogTab:   'tunnel',
  lastPingMs:      null,
  activeTab:       'connect',
  activeWorkers:  0,
  totalWorkers:   0,
};

// ── Init ──────────────────────────────────────────────────────────────────────

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
  window.runtime.EventsOn('socks:status', onSocksStatus);
  window.runtime.EventsOn('socks:stats', onSocksStats);
  window.runtime.EventsOn('socks:log', onSocksLog);
  window.runtime.EventsOn('tunnel:stats', onTunnelStats);
  window.runtime.EventsOn('tunnel:ping',  onTunnelPing);
  window.runtime.EventsOn('deploy:log', onDeployLog);
  window.runtime.EventsOn('tunnel:wgconfig', onWgConfig);
  window.runtime.EventsOn('sysproxy:status', onSysProxyStatus);


  // Загружаем конфиг и заполняем поля
  const cfg = await window.go.main.App.GetConfig();
  loadConfig(cfg);

  // Статус бинарника
  const exeExists = await window.go.main.App.GetClientExeExists();
  const exePath   = await window.go.main.App.GetClientExePath();
  const binEl = document.getElementById('bin-status');
  if (exeExists) {
    binEl.textContent = '✔  ' + exePath;
    binEl.className = 'bin-status ok';
  } else {
    binEl.textContent = '⚠  wdtt-client.exe не найден';
    binEl.className = 'bin-status err';
  }

  // Справка
  const ver = await window.go.main.App.GetVersion();
  document.getElementById('help-text').textContent = helpText(ver);

  log('WinDTT v' + ver + ' запущен', 'success');
});

// ── Tabs ──────────────────────────────────────────────────────────────────────

function switchTab(name) {
  state.activeTab = name;
  document.querySelectorAll('.tab').forEach(b => b.classList.remove('active'));
  document.querySelectorAll('.tab-panel').forEach(p => { p.classList.remove('active'); p.classList.add('hidden'); });
  document.querySelector(`[data-tab="${name}"]`).classList.add('active');
  const panel = document.getElementById('tab-' + name);
  panel.classList.remove('hidden');
  panel.classList.add('active');
  // При переходе на логи — прокручиваем в конец
  if (name === 'logs') {
    const box = state.activeLogTab === 'socks'
      ? document.getElementById('socks-log-box')
      : document.getElementById('log-box');
    if (box) box.scrollTop = box.scrollHeight;
  }
}

// ── Config ────────────────────────────────────────────────────────────────────

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

  // px_use_auth: undefined → оставляем HTML-дефолт (checked)
  if (cfg.px_use_auth !== undefined) {
    document.getElementById('px-use-auth').checked = cfg.px_use_auth !== false;
  }
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
    theme:        document.body.classList.contains('light') ? 'light' : 'dark',
  };
}

async function saveConfig() {
  const cfg = collectConfig();
  await window.go.main.App.SaveConfig(cfg);
}

// ── Parse wdtt:// ─────────────────────────────────────────────────────────────

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

// ── Tunnel ────────────────────────────────────────────────────────────────────

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

  // device_id генерируется на Go стороне при первом запуске
  const cfg = await window.go.main.App.GetConfig();
  let deviceID = cfg.device_id || '';

  log('Подключение → ' + srv, 'info');
  const err = await window.go.main.App.TunnelStart(hash, srv, sec, n, lst, cm, deviceID, fp, om);
  if (err) { log('Ошибка: ' + err, 'error'); return; }
  await saveConfig();
}

function pauseResume() {
  if (state.tunnelPaused) {
    window.go.main.App.TunnelResume();
    log('Туннель возобновлён.', 'info');
  } else {
    window.go.main.App.TunnelPause();
    log('Туннель на паузе.', 'warn');
  }
}

function stop() {
  window.go.main.App.TunnelStop();
  document.getElementById('captcha-panel').style.display = 'none';
  log('Туннель остановлен.', 'warn');
}

function sendCaptcha() {
  const token = getVal('captcha-token').trim();
  if (!token) { log('Вставьте токен капчи!', 'error'); return; }
  window.go.main.App.TunnelSendCaptcha(token);
  setVal('captcha-token', '');
  document.getElementById('captcha-panel').style.display = 'none';
  log('Токен капчи отправлен.', 'info');
}

// ── Tunnel events ─────────────────────────────────────────────────────────────

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
  log('Требуется капча! Вставьте токен.', 'warn');
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
    btnPause.textContent = '▶  Продолжить';
  } else if (r) {
    statusEl.innerHTML = dot + 'Туннель';
    statusEl.className = 'badge connected';
    btnPause.textContent = '⏸  Пауза';
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

  // Блокируем кнопку сразу — разблокируем только при ошибке
  document.getElementById('btn-pstart').disabled = true;

  const err = await window.go.main.App.ProxyStart(host, s5port, httpPort, u, pw, ua);
  if (err) {
    socksLog('Ошибка запуска: ' + err, 'error');
    document.getElementById('btn-pstart').disabled = false;
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
}

// Псевдоним
const socksStart = proxyStart;

function proxyStop() {
  window.go.main.App.SocksStop();
  document.getElementById('btn-pstart').disabled = false;
  document.getElementById('btn-pstop').disabled  = true;
  document.getElementById('proxy-hint').style.display = 'none';
}

const socksStop = proxyStop;

// ── System Proxy UI ──────────────────────────────────────────────────────────

async function onSysProxyToggle() {
  const cb = document.getElementById('px-sysproxy');

  if (cb.checked) {
    const sErr = await window.go.main.App.SystemProxyEnable();
    if (sErr) {
      socksLog('System proxy: ' + sErr, 'error');
      cb.checked = false;
    }
  } else {
    await window.go.main.App.SystemProxyDisable();
  }
}

function onSysProxyStatus(on) {
  const banner = document.getElementById('sysproxy-banner');
  if (banner) banner.style.display = on ? '' : 'none';
  // Синхронизируем чекбокс с Go-состоянием (напр. при выходе из приложения)
  const cb = document.getElementById('px-sysproxy');
  if (cb) cb.checked = on;
}

// Отдельный лог для SOCKS5 соединений
function socksLog(msg, lv = 'info') {
  const ts = new Date().toLocaleTimeString('ru', {hour12: false});
  const entry = {ts, msg, lv};
  state.socksEntries.push(entry);
  // Всегда добавляем в DOM — бокс может быть скрыт но данные актуальны
  appendSocksLine(entry);
}

function appendSocksLine({ts, msg, lv}) {
  const box = document.getElementById('socks-log-box');
  const line = document.createElement('div');
  line.innerHTML = `<span class="log-ts">[${ts}]</span> <span class="log-${lv}">${escHtml(msg)}</span>`;
  box.appendChild(line);
  if (state.activeTab === 'logs' && state.activeLogTab === 'socks') {
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
  if (running) {
    el.textContent = '● Локальный прокси включен';
    el.className = 'proxy-status connected';
    sb.innerHTML = '<span class="dot"></span> Локальный прокси';
    sb.className = 'badge connected';
  } else {
    el.textContent = '● Локальный прокси выключен';
    el.className = 'proxy-status disconnected';
    sb.className = 'badge hidden';
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

// Получаем отдельные события лога SOCKS5 соединений
function onSocksLog(entry) {
  socksLog(entry.msg, entry.lv);
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

// ── Deploy ────────────────────────────────────────────────────────────────────

async function deploy() {
  const ip   = getVal('d-ip').trim();
  const port = getVal('d-port').trim() || '22';
  const user = getVal('d-user').trim();
  const pwd  = getVal('d-pwd');
  const wg   = getVal('d-wg').trim()   || '51820';
  const wdtt = getVal('d-wdtt').trim() || '56000';

  if (!ip || !user) { deployLog('Введите IP и пользователя!', 'error'); return; }

  deployLog('Получаю fingerprint ' + ip + ':' + port + '...', 'info');
  const fp = await window.go.main.App.DeployGetFingerprint(ip, port);
  if (!fp.ok) {
    deployLog('Не удалось получить fingerprint: ' + (fp.error || ''), 'error');
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
  window.go.main.App.DeployRun(ip, port, user, pwd, wg, wdtt, tunnelPwd, adminID, botToken, fp.fingerprint);
}

async function undeploy() {
  const ip   = getVal('d-ip').trim();
  const port = getVal('d-port').trim() || '22';
  const user = getVal('d-user').trim();
  const pwd  = getVal('d-pwd');
  const wg   = getVal('d-wg').trim()   || '51820';
  const wdtt = getVal('d-wdtt').trim() || '56000';

  if (!ip || !user) { deployLog('Введите IP и пользователя!', 'error'); return; }

  // Fingerprint: берём сохранённый, или запрашиваем заново.
  const cfg = await window.go.main.App.GetConfig();
  let fingerprint = (cfg && cfg.fingerprint) || '';
  if (!fingerprint) {
    deployLog('Fingerprint не сохранён — получаю с сервера...', 'info');
    const fp = await window.go.main.App.DeployGetFingerprint(ip, port);
    if (!fp.ok) { deployLog('Не удалось получить fingerprint: ' + (fp.error || ''), 'error'); return; }
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
  window.go.main.App.UndeployRun(ip, port, user, pwd, wg, wdtt, fingerprint);
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

// ── Log ───────────────────────────────────────────────────────────────────────

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
  const box = document.getElementById('log-box');
  const line = document.createElement('div');
  line.innerHTML = `<span class="log-ts">[${ts}]</span> <span class="log-${lv}">${escHtml(msg)}</span>`;
  box.appendChild(line);
  // Автопрокрутка только когда вкладка логов активна
  if (state.activeTab === 'logs' && state.activeLogTab === 'tunnel') {
    box.scrollTop = box.scrollHeight;
  }
}

function setFilter(f) {
  state.logFilter = f;
  const box = document.getElementById('log-box');
  box.innerHTML = '';
  state.logEntries.forEach(e => {
    if (f === 'all' || e.lv === f || (f === 'warn' && (e.lv === 'warn' || e.lv === 'error'))) {
      appendLogLine(e);
    }
  });
}

async function saveLog() {
  const entries = state.activeLogTab === 'tunnel' ? state.logEntries : state.socksEntries;
  const lines = entries
    .map(e => `[${e.ts}] [${e.lv.toUpperCase().padEnd(7)}] ${e.msg}`)
    .join('\n');
  const ok = await window.go.main.App.SaveFileDialog(lines);
  if (ok) log('Лог сохранён.', 'success');
}

function clearLog() {
  if (state.activeLogTab === 'tunnel') {
    state.logEntries = [];
    document.getElementById('log-box').innerHTML = '';
  } else {
    state.socksEntries = [];
    document.getElementById('socks-log-box').innerHTML = '';
  }
}

// ── Тема ─────────────────────────────────────────────────────────────────────

async function initTheme() {
  const saved = await window.go.main.App.GetTheme();
  applyTheme(saved || 'dark');
}

function applyTheme(theme) {
  if (theme === 'light') {
    document.body.classList.add('light');
    document.getElementById('theme-btn').textContent = '☀️';
  } else {
    document.body.classList.remove('light');
    document.getElementById('theme-btn').textContent = '🌙';
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
    <button class="btn btn-sm" onclick="pasteVkHash(this)">📋</button>
    <button class="btn btn-sm vk-remove" onclick="removeVkHash(this)">✕</button>
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
    btnP.className = 'btn btn-sm'; btnP.textContent = '📋';
    btnP.onclick = function() { pasteVkHash(this); };
    const btnR = document.createElement('button');
    btnR.className = 'btn btn-sm vk-remove'; btnR.textContent = '✕';
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
  box.appendChild(line);
  box.scrollTop = box.scrollHeight;
}

function onDeployLog(entry) {
  const box = document.getElementById('deploy-log-box');
  if (!box) return;
  const line = document.createElement('div');
  line.innerHTML = `<span class="log-ts">[${entry.ts}]</span> <span class="log-${entry.lv}">${escHtml(entry.msg)}</span>`;
  box.appendChild(line);
  box.scrollTop = box.scrollHeight;
}

function clearDeployLog() {
  const box = document.getElementById('deploy-log-box');
  if (box) box.innerHTML = '';
}

// ── Переключение вкладок лога ────────────────────────────────────────────────

function switchLogTab(tab) {
  state.activeLogTab = tab;

  document.getElementById('ltab-tunnel').classList.toggle('active', tab === 'tunnel');
  document.getElementById('ltab-socks').classList.toggle('active',  tab === 'socks');

  document.getElementById('log-box').style.display       = tab === 'tunnel' ? '' : 'none';
  document.getElementById('socks-log-box').style.display = tab === 'socks'  ? '' : 'none';

  // Фильтр показываем только для туннеля
  document.getElementById('log-filter-row').style.display = tab === 'tunnel' ? '' : 'none';
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
  1. Вставьте wdtt:// ссылку → «Разобрать»
     Формат: wdtt://IP:PORT:WG_PORT:LISTEN:SECRET:HASH[,HASH2]
  2. Или заполните поля вручную:
     VK хеш    — хеш(и) VK-звонка через запятую
     VPS адрес — IP:PORT сервера (например 1.2.3.4:56000)
     Secret    — пароль туннеля
  3. Нажмите «▶ Подключить»
  4. После подключения конфиг WireGuard появится
     на вкладке WireGuard — скопируйте в WG клиент

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
PROXY (SOCKS5 + HTTP)
  Запускается на вкладке Прокси.
  Оба протокола стартуют одновременно.
  После получения WireGuard конфига
  трафик автоматически идёт через туннель.

  SOCKS5: настройте в браузере как SOCKS5 прокси
  HTTP:   настройте как HTTP/HTTPS прокси
  
  Firefox: Настройки → Сеть → Ручная настройка прокси
  Chrome:  расширение Proxy SwitchyOmega

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
  Кнопки: 📋 Копировать  💾 Скачать .conf
  Используйте с официальным WireGuard клиентом.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
ТОП-БАР
  Туннель 42ms — статус туннеля и пинг
  трафик 1.24 МБ       — суммарный трафик
  скорость 128 КБ/с    — текущая скорость
  12/24 воркеров       — активных/всего
  Локальный прокси (3 соед.) — статус прокси

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
ФЛАГИ wdtt-client.exe:
  -peer IP:PORT       адрес VPS
  -vk   HASH,...      хеш(и) VK-звонка
  -password SECRET    пароль туннеля
  -n    24            потоков (кратно 12, макс 108)
  -listen 127.0.0.1:9000
  -fingerprint chrome/safari/firefox/ios/android
  -captcha-mode auto/rjs/wv
  -obfs audio/video   тип маскировки трафика`;
}
