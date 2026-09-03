'use strict';

const state = { snapshots: [], active: null, activeAppID: '', snapshot: null, compare: null, view: 'snapshots', lastCompareTo: '', collapsedEnv: {} };
const MASK = '••••••••••••';
const TOKEN = new URLSearchParams(location.search).get('t') || '';
const $ = (id) => document.getElementById(id);
let editing = null;

// ---- api ----

async function api(path, options = {}) {
  const method = options.method || 'GET';
  if (TOKEN && method !== 'GET') {
    options = { ...options, headers: { ...(options.headers || {}), 'X-Auth-Token': TOKEN } };
  }
  const res = await fetch(path, options);
  if (res.status === 204) return null;
  const type = res.headers.get('content-type') || '';
  if (!res.ok) {
    let message = `请求失败 (${res.status})`;
    try {
      if (type.includes('application/json')) {
        const body = await res.json();
        if (body && body.error && body.error.message) message = body.error.message;
      } else {
        const text = await res.text();
        if (text) message = text;
      }
    } catch (_) { /* keep default message */ }
    throw new Error(message);
  }
  if (type.includes('application/json')) return res.json();
  return res;
}

function jsonOptions(method, body) {
  return { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) };
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function relTime(iso) {
  const t = new Date(iso);
  if (isNaN(t.getTime())) return '';
  const diff = Date.now() - t.getTime();
  const min = Math.floor(diff / 60000);
  if (min < 1) return '刚刚';
  if (min < 60) return `${min} 分钟前`;
  const hours = Math.floor(min / 60);
  if (hours < 24) return `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days} 天前`;
  return t.toLocaleDateString('zh-CN');
}

// ---- render: rail ----

function renderRail() {
  const list = $('snapshot-list');
  list.textContent = '';
  if (!state.snapshots.length) {
    const d = document.createElement('div');
    d.className = 'item';
    d.textContent = '暂无快照';
    list.appendChild(d);
    return;
  }
  const groups = new Map();
  for (const s of state.snapshots) {
    if (!groups.has(s.name)) groups.set(s.name, []);
    groups.get(s.name).push(s);
  }
  for (const [env, refs] of groups) {
    const g = document.createElement('div');
    g.className = 'rail-group' + (state.collapsedEnv[env] ? ' collapsed' : '');
    const gTitle = document.createElement('div');
    gTitle.className = 'group-title';
    gTitle.title = state.collapsedEnv[env] ? '点击展开' : '点击折叠';
    const arrow = document.createElement('span');
    arrow.className = 'arrow';
    arrow.textContent = state.collapsedEnv[env] ? '▸' : '▾';
    const envName = document.createElement('span');
    envName.className = 'env-name';
    envName.textContent = env;
    const gCount = document.createElement('span');
    gCount.className = 'g-count';
    gCount.textContent = `${refs.length}`;
    gTitle.appendChild(arrow);
    gTitle.appendChild(envName);
    gTitle.appendChild(gCount);
    gTitle.addEventListener('click', () => {
      state.collapsedEnv[env] = !state.collapsedEnv[env];
      renderRail();
    });
    g.appendChild(gTitle);
    for (const s of refs) {
      const d = document.createElement('div');
      d.className = 'item' + (s.name === state.active && (s.app_id || '') === (state.activeAppID || '') ? ' active' : '');
      const top = document.createElement('div');
      top.className = 'rail-row';
      const name = document.createElement('span');
      name.textContent = s.app_id ? s.app_id : s.name;
      const count = document.createElement('span');
      count.className = 'count';
      count.textContent = `${s.total} 项`;
      top.appendChild(name);
      top.appendChild(count);
      d.appendChild(top);
      const sub = document.createElement('div');
      sub.className = 'rail-sub';
      sub.textContent = relTime(s.captured_at) ? `更新于 ${relTime(s.captured_at)}` : '';
      d.appendChild(sub);
      const del = document.createElement('button');
      del.type = 'button';
      del.className = 'rail-del';
      del.textContent = '删除';
      del.title = '删除快照';
      del.addEventListener('click', (e) => {
        e.stopPropagation();
        openDeleteSnapshot(s);
      });
      d.appendChild(del);
      d.addEventListener('click', () => selectSnapshot(s.name, s.app_id));
      g.appendChild(d);
    }
    list.appendChild(g);
  }
}

function refLabel(name, appID) {
  return appID ? `${name} (${appID})` : name;
}

// ---- view switching ----

function switchView(name) {
  state.view = name;
  if (name !== 'snapshots') {
    state.active = null;
    state.activeAppID = '';
    state.snapshot = null;
    renderRail();
    renderContext();
  }
  for (const v of ['snapshots', 'aes', 'db', 'settings']) {
    const el = $(`view-${v}`);
    if (el) el.hidden = v !== name;
    const nav = $(`nav-${v}`);
    if (nav) nav.classList.toggle('active', v === name);
  }
  const titles = { aes: 'AES 加解密', db: '数据库隧道', settings: '设置' };
  $('breadcrumb').innerHTML = name === 'snapshots'
    ? '<b>配置工作台</b>'
    : `<b>${titles[name] || name}</b>`;
  if (name === 'aes') loadAESEntries();
  if (name === 'db') loadDB();
  if (name === 'settings') loadSettings();
}

// ---- AES tools ----

let aesEntries = [];

async function loadAESEntries() {
  try {
    const data = await api('/api/aes/config');
    aesEntries = data.entries || [];
    populateAESSelect($('aes-list-select'));
    populateAESSelect($('reveal-list-select'));
    return data.path || '';
  } catch (err) {
    aesEntries = [];
    return '';
  }
}

function populateAESSelect(sel) {
  const current = sel.value;
  sel.textContent = '';
  const none = document.createElement('option');
  none.value = '';
  none.textContent = '— 选择已保存的 key/iv —';
  sel.appendChild(none);
  for (const e of aesEntries) {
    const opt = document.createElement('option');
    opt.value = e.name;
    opt.textContent = e.name;
    sel.appendChild(opt);
  }
  if ([...sel.options].some((o) => o.value === current)) sel.value = current;
}

async function loadSelectedAESEntry(target) {
  const name = $(target).value;
  if (!name) return;
  const e = aesEntries.find((x) => x.name === name);
  if (!e) return;
  $('aes-name').value = e.name;
  $('aes-key').value = e['secret-key'];
  $('aes-iv').value = e.iv;
  setAESInputsLocked(true);
}

function setAESInputsLocked(locked) {
  $('aes-key').disabled = locked;
  $('aes-iv').disabled = locked;
}

async function runAES(op) {
  const key = $('aes-key').value.trim();
  const iv = $('aes-iv').value.trim();
  const text = $('aes-input').value;
  if (!key || !iv) { showAESError('请填写 key 和 iv。'); return; }
  if (!text) { showAESError('请填写输入内容。'); return; }
  try {
    const data = await api('/api/aes/transform', jsonOptions('POST', { op, key, iv, text }));
    $('aes-output').value = data.result;
    showAESError('');
  } catch (err) { showAESError(err.message); }
}

async function genAESKey() {
  try {
    const data = await api('/api/aes/gen-key', jsonOptions('POST', { bytes: 16, iv_bytes: 12 }));
    $('aes-key').value = data.key;
    $('aes-iv').value = data.iv;
    setAESInputsLocked(false);
    showAESError('已生成 key/iv。填写名称后点击「保存到列表」即可存入 aes.json。');
  } catch (err) { showAESError(err.message); }
}

async function saveAESConfig() {
  const name = $('aes-name').value.trim();
  const key = $('aes-key').value.trim();
  const iv = $('aes-iv').value.trim();
  if (!name) { showAESError('请填写名称。'); return; }
  if (!key || !iv) { showAESError('请填写 key 和 iv。'); return; }
  try {
    await api('/api/aes/config', jsonOptions('POST', { name, 'secret-key': key, iv }));
    showAESError('已保存到 aes.json。');
    await loadAESEntries();
    $('aes-list-select').value = name;
  } catch (err) { showAESError(err.message); }
}

function showAESError(msg) {
  $('aes-error').textContent = msg;
  $('aes-error').hidden = !msg;
}

function copyAESOutput() {
  const text = $('aes-output').value;
  if (!text) return;
  copyText(text).then(() => showAESError('已复制到剪贴板。')).catch(() => showAESError('复制失败。'));
}

// ---- database tunnels ----

let dbConns = [];

function showDBError(msg) {
  const el = $('db-error');
  el.textContent = msg;
  el.hidden = !msg;
}

async function loadDB() {
  const status = $('db-key-status');
  const initBtn = $('db-init-btn');
  try {
    const ks = await api('/api/db/key');
    if (ks.available) {
      status.textContent = '数据库密钥可用（Keychain 或环境变量）';
      status.className = 'status-line ok';
      initBtn.hidden = true;
    } else {
      throw new Error('unavailable');
    }
  } catch (_) {
    status.textContent = '数据库密钥不可用，点击下方按钮生成本机密钥';
    status.className = 'status-line warn';
    initBtn.hidden = false;
  }
  try {
    const res = await api('/api/db/list');
    dbConns = res.connections || [];
    renderDBTable();
  } catch (e) {
    showDBError(e.message);
  }
}

function renderDBTable() {
  $('db-count').textContent = dbConns.length ? `${dbConns.length} 条` : '';
  const body = $('db-body');
  body.innerHTML = '';
  const empty = $('db-empty');
  if (!dbConns.length) {
    empty.hidden = false;
    empty.textContent = '暂无连接，在左侧填写名称与数据库 URL 并注册；注册后 host 上的 vaulty-keeper serve 会自动为它开启一条本地隧道';
  } else {
    empty.hidden = true;
  }
  for (const c of dbConns) {
    const tr = document.createElement('tr');
    tr.dataset.name = c.name;
    const typeCell = c.broken
      ? `<td><span class="warn-badge" title="无法用当前密钥解密">密钥不匹配</span></td>`
      : `<td>${escapeHtml(c.type)}</td>`;
    const actions = c.broken
      ? `<td class="row-actions"><button class="row-del" type="button" data-db-action="rm" data-name="${escapeHtml(c.name)}">删除</button></td>`
      : `<td class="row-actions">
          <button class="ghost" type="button" data-db-action="test" data-name="${escapeHtml(c.name)}">测试</button>
          <button class="ghost" type="button" data-db-action="connect" data-name="${escapeHtml(c.name)}">连接信息</button>
          <button class="ghost" type="button" data-db-action="regen" data-name="${escapeHtml(c.name)}" title="重新生成隧道 token，旧链接立即失效">重新生成</button>
          <button class="ghost" type="button" data-db-action="show" data-name="${escapeHtml(c.name)}" hidden>查看URL</button>
          <button class="row-del" type="button" data-db-action="rm" data-name="${escapeHtml(c.name)}">删除</button>
        </td>`;
    tr.innerHTML = `<td><b>${escapeHtml(c.name)}</b></td>${typeCell}<td>:${c.port}</td>${actions}`;
    body.appendChild(tr);
  }
  // only offer "查看URL" when plaintext endpoints are enabled
  api('/api/config').then((cfg) => {
    if (cfg.allow_plaintext) {
      body.querySelectorAll('[data-db-action="show"]').forEach((b) => (b.hidden = false));
    }
  }).catch(() => {});
}

// URL encryption: the browser derives a fresh AES-GCM key from the UI's
// per-process ECDH public key, so the database URL never crosses the wire as
// plaintext. The matching private key lives only in the ui process memory.
let dbPubKey = '';

async function fetchDBPubKey() {
  if (dbPubKey) return dbPubKey;
  const res = await api('/api/db/pubkey');
  if (!res || !res.pub) throw new Error('无法获取数据库 URL 加密公钥');
  dbPubKey = res.pub;
  return dbPubKey;
}

function b64ToBuf(b64) {
  const bin = atob(b64);
  const u = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) u[i] = bin.charCodeAt(i);
  return u.buffer;
}

function bufToB64(buf) {
  const u = new Uint8Array(buf);
  let bin = '';
  for (let i = 0; i < u.length; i++) bin += String.fromCharCode(u[i]);
  return btoa(bin);
}

async function encryptDBURL(url) {
  const pubB64 = await fetchDBPubKey();
  const serverPub = await crypto.subtle.importKey('raw', b64ToBuf(pubB64), { name: 'ECDH', namedCurve: 'P-256' }, false, []);
  const eph = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveKey']);
  const aesKey = await crypto.subtle.deriveKey({ name: 'ECDH', public: serverPub }, eph.privateKey, { name: 'AES-GCM', length: 256 }, false, ['encrypt']);
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, aesKey, new TextEncoder().encode(url));
  const ephRaw = await crypto.subtle.exportKey('raw', eph.publicKey);
  return { eph: bufToB64(ephRaw), iv: bufToB64(iv), ct: bufToB64(ct) };
}

function showDBFormResult(msg, cls) {
  const el = $('db-form-result');
  el.textContent = msg || '';
  el.className = 'db-form-result ' + (cls || '');
  el.hidden = !msg;
}

function setDBFormBusy(busy) {
  $('db-add-btn').disabled = busy;
  $('db-test-btn').disabled = busy;
}

async function dbInit() {
  if (!confirm('初始化数据库密钥？（仅当此前从未初始化时才需要；已存在时不要用强制覆盖，否则已有连接将无法解密）')) return;
  try {
    await api('/api/db/init', jsonOptions('POST', { force: false }));
    showDBError('');
    loadDB();
  } catch (e) {
    if (e.message.includes('已存在')) {
      if (confirm('密钥已存在，确认强制覆盖？这会使所有已注册连接无法解密，必须重新注册')) {
        try {
          await api('/api/db/init', jsonOptions('POST', { force: true }));
          loadDB();
        } catch (e2) {
          showDBError(e2.message);
        }
      }
    } else {
      showDBError(e.message);
    }
  }
}

async function dbAdd() {
  const name = $('db-name').value.trim();
  const url = $('db-url').value.trim();
  const port = parseInt($('db-port').value || '0', 10) || 0;
  if (!name || !url) {
    showDBError('名称与数据库 URL 必填');
    return;
  }
  setDBFormBusy(true);
  try {
    const url_enc = await encryptDBURL(url);
    await api('/api/db/connections', jsonOptions('POST', { name, url_enc, port }));
    $('db-name').value = '';
    $('db-url').value = '';
    $('db-port').value = '';
    showDBError('');
    showDBFormResult('', '');
    loadDB();
  } catch (e) {
    showDBError(e.message);
  } finally {
    setDBFormBusy(false);
  }
}

async function dbTestURL() {
  const url = $('db-url').value.trim();
  if (!url) {
    showDBFormResult('请先填写数据库 URL', 'fail');
    return;
  }
  setDBFormBusy(true);
  try {
    const url_enc = await encryptDBURL(url);
    const res = await api('/api/db/test-url', jsonOptions('POST', { url_enc }));
    if (res.ok) {
      let msg = `✅ 连接成功（${res.type}`;
      if (res.user) msg += `，user=${res.user}`;
      if (res.db) msg += `，db=${res.db}`;
      msg += '）';
      showDBFormResult(msg, 'ok');
    } else {
      showDBFormResult('❌ ' + res.error, 'fail');
    }
  } catch (e) {
    showDBFormResult('❌ ' + e.message, 'fail');
  } finally {
    setDBFormBusy(false);
  }
}

function findDBRow(name) {
  return [...$('db-body').rows].find((r) => r.dataset.name === name);
}

function removeDBResultRow(row) {
  const next = row.nextElementSibling;
  if (next && next.classList.contains('db-result-row')) next.remove();
}

function insertDBResultRow(row, ok, text) {
  removeDBResultRow(row);
  const tr = document.createElement('tr');
  tr.className = 'db-result-row';
  const td = document.createElement('td');
  td.colSpan = 4;
  td.className = ok ? 'db-result ok' : 'db-result fail';
  td.textContent = text;
  tr.appendChild(td);
  row.after(tr);
  setTimeout(() => { if (tr.parentNode) tr.remove(); }, 10000);
}

async function dbTest(name) {
  const row = findDBRow(name);
  const btn = row && row.querySelector('[data-db-action="test"]');
  if (btn) {
    btn.disabled = true;
    btn.textContent = '测试中…';
  }
  let ok = false;
  let text;
  try {
    const res = await api('/api/db/test', jsonOptions('POST', { name }));
    ok = res.ok;
    let parts = [`${res.name} (${res.type}) :${res.port}`];
    if (res.user) parts.push(`user=${res.user}`);
    if (res.db) parts.push(`db=${res.db}`);
    text = ok ? '✅ ' + parts.join(' ') : '❌ ' + parts.join(' ') + ' — ' + res.error;
  } catch (e) {
    text = '❌ ' + e.message;
  }
  if (row) insertDBResultRow(row, ok, text);
  if (btn) {
    btn.disabled = false;
    btn.textContent = '测试';
  }
}

function showDBConnectDialog(res, regenerated) {
  $('db-connect-head').innerHTML =
    `<b>${escapeHtml(res.name)}</b><span class="tag">${escapeHtml(res.type)}</span><span class="port">:${res.port}</span>`;
  const note = $('db-connect-note');
  if (regenerated) {
    note.textContent = '已重新生成新 token，旧链接已立即失效';
    note.hidden = false;
  } else if (res.note) {
    note.textContent = res.note;
    note.hidden = false;
  } else {
    note.hidden = true;
  }
  let html = '';
  if (res.raw) {
    html += `<div class="db-line"><div class="db-line-label">原始隧道链接（AI / 其他工具可据此自行转换）</div>` +
      `<pre><code>${escapeHtml(res.raw)}</code></pre><button type="button" class="copy-btn" data-copy>复制</button></div>`;
  }
  for (const cl of res.clients) {
    html += `<div class="db-line"><div class="db-line-label">${escapeHtml(cl.label)}</div>` +
      `<pre><code>${escapeHtml(cl.line)}</code></pre><button type="button" class="copy-btn" data-copy>复制</button></div>`;
  }
  html += `<div class="db-connect-footer">token 是 bridge token，不是真实数据库密码。</div>`;
  $('db-connect-body').innerHTML = html;
  openDialog('db-connect-dialog');
}

async function dbConnectInfo(name) {
  try {
    const res = await api(`/api/db/connect?name=${encodeURIComponent(name)}`);
    showDBConnectDialog(res);
  } catch (e) {
    showDBError(e.message);
  }
}

async function dbShow(name) {
  try {
    const res = await api('/api/db/show', jsonOptions('POST', { name }));
    $('db-show-url').textContent = res.url;
    $('db-show-copy-btn').textContent = '复制';
    openDialog('db-show-dialog');
  } catch (e) {
    showDBError(e.message);
  }
}

let deletingDBConn = null;

function dbRemove(name) {
  deletingDBConn = name;
  $('db-delete-name').textContent = name;
  openDialog('db-delete-dialog');
}

async function confirmDBDelete() {
  if (!deletingDBConn) return;
  const name = deletingDBConn;
  try {
    await api(`/api/db/connections/${encodeURIComponent(name)}`, { method: 'DELETE' });
    closeDialog('db-delete-dialog');
    deletingDBConn = null;
    loadDB();
  } catch (err) {
    dialogError('db-delete-dialog', err.message);
  }
}

let regenTarget = null; // { all: true } or { name }

function dbRegen(name) {
  regenTarget = { name };
  $('db-regen-desc').innerHTML =
    `确定重新生成 <code>${escapeHtml(name)}</code> 的隧道 token 吗？旧链接将立即失效，此操作不可撤销（全局 bridge token 不受影响）。`;
  openDialog('db-regen-dialog');
}

function dbRegenAll() {
  regenTarget = { all: true };
  $('db-regen-desc').textContent = '确定重新生成所有连接的隧道 token 吗？所有旧链接将立即失效，此操作不可撤销（全局 bridge token 不受影响）。';
  openDialog('db-regen-dialog');
}

async function confirmDBRegen() {
  if (!regenTarget) return;
  try {
    if (regenTarget.all) {
      const res = await api('/api/db/regen', jsonOptions('POST', { all: true }));
      closeDialog('db-regen-dialog');
      regenTarget = null;
      const n = (res.regenerated || []).length;
      showDBError(n ? `已重新生成 ${n} 个连接的 token，旧链接已失效` : '没有可重新生成 token 的连接');
      loadDB();
    } else {
      const res = await api('/api/db/regen', jsonOptions('POST', { name: regenTarget.name }));
      closeDialog('db-regen-dialog');
      regenTarget = null;
      showDBConnectDialog(res, true);
    }
  } catch (err) {
    dialogError('db-regen-dialog', err.message);
  }
}

// ---- settings ----

async function loadSettings() {
  const status = $('key-status');
  const initBtn = $('init-key-btn');
  try {
    const ks = await api('/api/key');
    if (ks.available) {
      status.textContent = '快照密钥可用（Keychain 或环境变量）。';
      status.className = 'status-line ok';
      initBtn.hidden = true;
    } else {
      throw new Error('unavailable');
    }
  } catch (_) {
    status.textContent = '快照密钥不可用。点击下方按钮生成本机密钥。';
    status.className = 'status-line warn';
    initBtn.hidden = false;
  }
  const sStatus = $('sensitive-key-status');
  const sInit = $('init-sensitive-btn');
  try {
    const sk = await api('/api/sensitive/key');
    if (sk.available) {
      sStatus.textContent = '敏感值密钥可用（Keychain 或环境变量）。敏感值用它加密，显示时用它解密。';
      sStatus.className = 'status-line ok';
      sInit.hidden = true;
    } else {
      throw new Error('unavailable');
    }
  } catch (_) {
    sStatus.textContent = '敏感值密钥不可用。点击下方按钮生成本机密钥。';
    sStatus.className = 'status-line warn';
    sInit.hidden = false;
  }
  try {
    const path = await loadAESEntries();
    const el = $('aes-config-status');
    el.textContent = aesEntries.length ? `已保存 ${aesEntries.length} 条（${path}）。` : `未保存任何条目（${path}）。`;
    el.className = aesEntries.length ? 'status-line ok' : 'status-line';
    renderAESList();
  } catch (err) { showSettingsError(err.message); }
}

function renderAESList() {
  const body = $('aes-list-body');
  body.textContent = '';
  if (!aesEntries.length) {
    const tr = document.createElement('tr');
    const td = document.createElement('td');
    td.className = 'empty';
    td.textContent = '暂无条目';
    td.colSpan = 3;
    tr.appendChild(td);
    body.appendChild(tr);
    return;
  }
  for (const e of aesEntries) {
    const tr = document.createElement('tr');
    const tdName = document.createElement('td');
    tdName.textContent = e.name;
    const tdKey = document.createElement('td');
    tdKey.className = 'masked';
    tdKey.textContent = maskSecret(e['secret-key']);
    const tdIV = document.createElement('td');
    tdIV.className = 'masked';
    tdIV.textContent = maskSecret(e.iv);
    const tdDel = document.createElement('td');
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'reveal-btn';
    btn.textContent = '删除';
    btn.addEventListener('click', () => deleteAESEntry(e.name));
    tdDel.appendChild(btn);
    tr.appendChild(tdName);
    tr.appendChild(tdKey);
    tr.appendChild(tdIV);
    tr.appendChild(tdDel);
    body.appendChild(tr);
  }
}

function maskSecret(s) {
  if (!s) return '';
  if (s.length <= 8) return '••••';
  return `${s.slice(0, 4)}…${s.slice(-4)}`;
}

async function deleteAESEntry(name) {
  try {
    await api(`/api/aes/config?name=${encodeURIComponent(name)}`, { method: 'DELETE' });
    await loadAESEntries();
    renderAESList();
    $('aes-config-status').textContent = aesEntries.length ? `已保存 ${aesEntries.length} 条。` : '未保存任何条目。';
    $('aes-config-status').className = aesEntries.length ? 'status-line ok' : 'status-line';
  } catch (err) { showSettingsError(err.message); }
}

async function initKey() {
  try {
    await api('/api/init', jsonOptions('POST', { force: false }));
    $('init-key-btn').hidden = true;
    $('key-status').textContent = '快照密钥已生成。';
    $('key-status').className = 'status-line ok';
  } catch (err) { showSettingsError(err.message); }
}

async function initSensitiveKey() {
  try {
    await api('/api/sensitive/init', jsonOptions('POST', { force: false }));
    $('init-sensitive-btn').hidden = true;
    $('sensitive-key-status').textContent = '敏感值密钥已生成。';
    $('sensitive-key-status').className = 'status-line ok';
  } catch (err) { showSettingsError(err.message); }
}

function showSettingsError(msg) {
  $('settings-error').textContent = msg;
  $('settings-error').hidden = !msg;
}

// ---- render: context / hero ----

function renderContext() {
  const s = state.snapshots.find((x) => x.name === state.active && (x.app_id || '') === (state.activeAppID || ''));
  const secure = $('snapshot-context').querySelector('.secure');
  if (!s) {
    $('context-name').textContent = '未选择快照';
    $('context-meta').textContent = '暂无快照，点击左侧「导入配置快照」开始。';
    $('snapshot-context').querySelector('.env').textContent = '✦';
    secure.hidden = true;
    $('breadcrumb').innerHTML = '<b>配置工作台</b>';
    $('hero-title').textContent = '检查配置状态。';
    return;
  }
  $('context-name').textContent = state.active + (state.activeAppID ? ' / ' + state.activeAppID : '');
  $('context-meta').textContent = `${s.total} 项配置 · ${s.sensitive} 个敏感项 · ${relTime(s.captured_at)}更新`;
  $('snapshot-context').querySelector('.env').textContent = s.name.slice(0, 1);
  secure.hidden = false;
  $('breadcrumb').innerHTML = `<b>配置工作台</b><span class="sep">/</span>${escapeHtml(s.name)}${s.app_id ? `<span class="sep">/</span>${escapeHtml(s.app_id)}` : ''}`;
  $('hero-title').textContent = `检查 ${s.name}${s.app_id ? ` (${s.app_id})` : ''} 的配置状态。`;
}

// ---- render: table ----

function currentItems() {
  if (!state.snapshot) return [];
  const q = $('search-input').value.trim().toLowerCase();
  const items = [...state.snapshot.items];
  if (!q) return items;
  return items.filter((it) => {
    if (it.key.toLowerCase().includes(q)) return true;
    return !it.sensitive && it.value && it.value.toLowerCase().includes(q);
  });
}

function renderTable() {
  const body = $('config-body');
  body.textContent = '';
  const items = currentItems();
  $('config-count').textContent = state.snapshot ? `${items.length} / ${state.snapshot.items.length}` : '';
  if (!items.length) {
    const row = document.createElement('tr');
    const td = document.createElement('td');
    td.colSpan = 2;
    const span = document.createElement('span');
    span.className = 'empty';
    span.textContent = state.snapshot ? '没有匹配的配置项' : '暂无配置，点击左侧「导入配置快照」开始。';
    td.appendChild(span);
    row.appendChild(td);
    body.appendChild(row);
    return;
  }
  for (const it of items) {
    const tr = document.createElement('tr');
    const tdKey = document.createElement('td');
    tdKey.textContent = it.key;
    const tdVal = document.createElement('td');
    if (it.sensitive) {
      tdVal.className = 'masked';
      tdVal.textContent = MASK;
      const i = document.createElement('i');
      i.textContent = `${it.length} 字符`;
      tdVal.appendChild(i);
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'reveal-btn';
      btn.textContent = '显示';
      tdVal.appendChild(btn);
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        openReveal(it);
      });
    } else {
      tdVal.textContent = it.value || '';
    }
    const cmpBtn = document.createElement('button');
    cmpBtn.type = 'button';
    cmpBtn.className = 'reveal-btn';
    cmpBtn.textContent = '对比此 key';
    cmpBtn.title = '查看此 key 在所有快照中的取值';
    tdVal.appendChild(cmpBtn);
    cmpBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      openKeyCompare(it.key);
    });
    tr.appendChild(tdKey);
    tr.appendChild(tdVal);
    tr.addEventListener('click', () => openEntry(it));
    body.appendChild(tr);
  }
}

// ---- snapshot loading ----

function showError(msg) {
  $('error-region').textContent = msg;
  $('error-region').hidden = false;
}

function clearError() {
  $('error-region').textContent = '';
  $('error-region').hidden = true;
}

async function loadSnapshot(name, appID) {
  const requested = name;
  const requestedAppID = appID || '';
  state.active = name;
  state.activeAppID = requestedAppID;
  state.compare = null;
  state.snapshot = null;
  $('compare-result-dialog').close();
  $('search-input').value = '';
  clearError();
  renderRail();
  renderContext();
  const q = state.activeAppID ? `?appid=${encodeURIComponent(state.activeAppID)}` : '';
  try {
    const data = await api(`/api/snapshots/${encodeURIComponent(name)}${q}`);
    if (state.active !== requested || state.activeAppID !== requestedAppID) return;
    state.snapshot = { name: data.name, items: data.items };
  } catch (err) {
    if (state.active !== requested || state.activeAppID !== requestedAppID) return;
    showError(err.message);
  }
  renderTable();
}

function selectSnapshot(name, appID) {
  switchView('snapshots');
  loadSnapshot(name, appID);
}

// Reloads the active snapshot's data and the rail summaries without resetting
// UI state (search text, scroll position, compare panel).
async function reloadSnapshotData() {
  const reqName = state.active;
  if (!reqName) return;
  const q = state.activeAppID ? `?appid=${encodeURIComponent(state.activeAppID)}` : '';
  try {
    const data = await api(`/api/snapshots/${encodeURIComponent(reqName)}${q}`);
    state.snapshot = { name: data.name, items: data.items };
  } catch (err) {
    showError(err.message);
  }
  try {
    const list = await api('/api/snapshots');
    state.snapshots = list.snapshots;
  } catch (_) { /* keep current list */ }
  renderRail();
  renderContext();
  renderTable();
}

async function refreshSnapshots(selectRef) {
  try {
    const data = await api('/api/snapshots');
    state.snapshots = data.snapshots;
  } catch (err) {
    showError(err.message);
  }
  renderRail();
  const target = selectRef || (state.active ? { name: state.active, app_id: state.activeAppID } : state.snapshots[0]);
  if (target && target.name) await loadSnapshot(target.name, target.app_id);
  else {
    state.active = null;
    state.activeAppID = '';
    renderContext();
    renderTable();
  }
}

// ---- dialogs ----

function openDialog(id) {
  clearDialogError(id);
  $(id).showModal();
}

function closeDialog(id) {
  $(id).close();
}

function dialogError(id, msg) {
  const el = $(id).querySelector('.error');
  if (!el) return;
  el.textContent = msg;
  el.hidden = false;
}

function clearDialogError(id) {
  const el = $(id).querySelector('.error');
  if (!el) return;
  el.textContent = '';
  el.hidden = true;
}

// ---- import ----

function openImport() {
  $('import-name').value = '';
  $('import-appid').value = '';
  $('import-text').value = '';
  $('import-preview').hidden = true;
  $('import-preview').textContent = '';
  $('import-dupe').hidden = true;
  $('import-confirm-btn').hidden = true;
  $('import-preview-btn').hidden = false;
  openDialog('import-dialog');
  $('import-name').focus();
}

function checkImportDuplicate() {
  const env = $('import-name').value.trim();
  const appid = $('import-appid').value.trim();
  const el = $('import-dupe');
  if (!env || !appid) {
    el.hidden = true;
    return;
  }
  const dupe = state.snapshots.find((s) => s.name === env && s.app_id === appid);
  if (dupe) {
    el.textContent = `已存在快照：${dupe.name} (${dupe.app_id})，共 ${dupe.total} 项配置。导入会失败，请换用其他环境或应用 ID。`;
    el.hidden = false;
  } else {
    el.hidden = true;
  }
}

async function runImportPreview() {
  const text = $('import-text').value;
  if (!text.trim()) {
    dialogError('import-dialog', '请先粘贴配置内容。');
    return;
  }
  try {
    const data = await api('/api/import/preview', jsonOptions('POST', { text }));
    renderImportPreview(data);
    $('import-confirm-btn').hidden = false;
    $('import-preview-btn').hidden = true;
  } catch (err) {
    dialogError('import-dialog', err.message);
  }
}

function renderImportPreview(data) {
  const box = $('import-preview');
  box.textContent = '';
  const warns = data.warnings || [];
  if (warns.length) {
    const list = document.createElement('div');
    list.className = 'warn-list';
    for (const w of warns) {
      const el = document.createElement('div');
      el.className = 'warn';
      const badge = document.createElement('span');
      badge.className = 'warn-badge';
      badge.textContent = `L${w.line}`;
      el.appendChild(badge);
      const msg = document.createElement('span');
      msg.textContent = w.message + (w.content ? `: "${w.content}"` : '');
      el.appendChild(msg);
      list.appendChild(el);
    }
    box.appendChild(list);
  }
  for (const it of data.items || []) {
    const row = document.createElement('div');
    row.className = 'row';
    const code = document.createElement('code');
    code.textContent = it.key;
    row.appendChild(code);
    if (it.sensitive) {
      const tag = document.createElement('span');
      tag.className = 'tag';
      tag.textContent = '敏感';
      row.appendChild(tag);
    }
    box.appendChild(row);
  }
  box.hidden = false;
}

async function confirmImport() {
  const name = $('import-name').value.trim();
  const appId = $('import-appid').value.trim();
  const text = $('import-text').value;
  if (!name) {
    dialogError('import-dialog', '请填写环境。');
    return;
  }
  if (!appId) {
    dialogError('import-dialog', '请填写应用 ID。');
    return;
  }
  if (!text.trim()) {
    dialogError('import-dialog', '请先粘贴配置内容。');
    return;
  }
  const dupe = state.snapshots.find((s) => s.name === name && s.app_id === appId);
  if (dupe) {
    dialogError('import-dialog', `快照 ${name} (${appId}) 已存在，无法重复导入。`);
    return;
  }
  try {
    const data = await api('/api/snapshots', jsonOptions('POST', { name, app_id: appId, text }));
    closeDialog('import-dialog');
    await refreshSnapshots({ name: data.name, app_id: data.app_id });
  } catch (err) {
    dialogError('import-dialog', err.message);
  }
}

// ---- entry edit / replace / delete ----

function openEntry(item) {
  editing = item;
  $('entry-key').textContent = item.key;
  $('entry-env').textContent = item.key.slice(0, 1);
  const input = $('entry-value');
  const warn = $('entry-warning');
  if (item.sensitive) {
    $('entry-title').textContent = '替换敏感值';
    input.type = 'password';
    input.value = '';
    input.placeholder = '输入新值以替换…';
    warn.hidden = false;
  } else {
    $('entry-title').textContent = '编辑配置项';
    input.type = 'text';
    input.value = item.value || '';
    input.placeholder = '';
    warn.hidden = true;
  }
  openDialog('entry-dialog');
  input.focus();
}

async function saveEntry() {
  if (!editing) return;
  const key = editing.key;
  const secret = editing.sensitive;
  const value = $('entry-value').value;
  if (secret && !value.trim()) {
    closeDialog('entry-dialog');
    editing = null;
    return;
  }
  try {
    await api(`/api/snapshots/${encodeURIComponent(state.active)}/items/${encodeURIComponent(key)}${snapshotQuery()}`,
      jsonOptions('PUT', { value, secret }));
    closeDialog('entry-dialog');
    editing = null;
    await reloadSnapshotData();
  } catch (err) {
    dialogError('entry-dialog', err.message);
  }
}

function openDelete() {
  if (!editing) return;
  $('delete-key').textContent = editing.key;
  openDialog('delete-dialog');
}

async function confirmDelete() {
  if (!editing) return;
  const key = editing.key;
  try {
    await api(`/api/snapshots/${encodeURIComponent(state.active)}/items/${encodeURIComponent(key)}${snapshotQuery()}`,
      { method: 'DELETE' });
    closeDialog('delete-dialog');
    closeDialog('entry-dialog');
    editing = null;
    await reloadSnapshotData();
  } catch (err) {
    dialogError('delete-dialog', err.message);
  }
}

// ---- compare ----

function openCompare() {
  const select = $('compare-target');
  select.textContent = '';
  $('compare-from').textContent = refLabel(state.active, state.activeAppID) || '—';
  for (const s of state.snapshots) {
    if (s.name === state.active && (s.app_id || '') === (state.activeAppID || '')) continue;
    const opt = document.createElement('option');
    opt.value = `${s.name}\u0000${s.app_id || ''}`;
    opt.textContent = s.app_id ? `${s.name} (${s.app_id})` : s.name;
    select.appendChild(opt);
  }
  if (!select.options.length) {
    showError('没有可对比的其他快照。');
    return;
  }
  if (state.lastCompareTo) {
    const match = [...select.options].find((o) => o.value === state.lastCompareTo);
    if (match) match.selected = true;
  }
  openDialog('compare-dialog');
}

async function confirmCompare() {
  const from = state.active;
  const optVal = $('compare-target').value;
  const sepIdx = optVal.indexOf('\u0000');
  const to = sepIdx >= 0 ? optVal.slice(0, sepIdx) : optVal;
  const toApp = sepIdx >= 0 ? optVal.slice(sepIdx + 1) : '';
  const fromRef = `${from}\u0000${state.activeAppID || ''}`;
  const toRef = `${to}\u0000${toApp}`;
  if (!from || !to || fromRef === toRef) {
    dialogError('compare-dialog', '请选择目标环境。');
    return;
  }
  try {
    const params = new URLSearchParams({ from, to });
    if (state.activeAppID) params.set('from_appid', state.activeAppID);
    if (toApp) params.set('to_appid', toApp);
    const data = await api(`/api/compare?${params.toString()}`);
    state.compare = data;
    state.lastCompareTo = optVal;
    state.compareMeta = {
      fromLabel: refLabel(from, state.activeAppID),
      toLabel: refLabel(to, toApp),
      changes: data.changes || [],
    };
    $('compare-filter').value = '';
    renderCompareRefs(state.compareMeta.fromLabel, state.compareMeta.toLabel, state.compareMeta.changes);
    renderCompare(state.compareMeta.changes);
    closeDialog('compare-dialog');
    openDialog('compare-result-dialog');
  } catch (err) {
    dialogError('compare-dialog', err.message);
  }
}

function renderCompareRefs(fromLabel, toLabel, changes) {
  const bar = $('compare-refs');
  bar.textContent = '';
  const f = document.createElement('span');
  f.className = 'compare-ref from';
  f.textContent = fromLabel;
  const arrow = document.createElement('span');
  arrow.className = 'compare-arrow';
  arrow.textContent = '→';
  const t = document.createElement('span');
  t.className = 'compare-ref to';
  t.textContent = toLabel;
  bar.appendChild(f);
  bar.appendChild(arrow);
  bar.appendChild(t);
  const stat = document.createElement('span');
  stat.className = 'compare-stat';
  const counts = { added: 0, removed: 0, changed: 0 };
  for (const c of changes) counts[c.kind] = (counts[c.kind] || 0) + 1;
  const parts = [];
  if (counts.added) {
    const s = document.createElement('span');
    s.className = 'add';
    s.textContent = `+${counts.added} 新增`;
    parts.push(s);
  }
  if (counts.removed) {
    const s = document.createElement('span');
    s.className = 'del';
    s.textContent = `-${counts.removed} 删除`;
    parts.push(s);
  }
  if (counts.changed) {
    const s = document.createElement('span');
    s.className = 'chg';
    s.textContent = `~${counts.changed} 变更`;
    parts.push(s);
  }
  if (!parts.length) {
    const s = document.createElement('span');
    s.className = 'same';
    s.textContent = '0 处变更';
    parts.push(s);
  }
  for (const p of parts) stat.appendChild(p);
  bar.appendChild(stat);
  bar.hidden = false;
}

function renderCompare(changes) {
  const body = $('compare-body');
  body.textContent = '';
  if (!changes.length) {
    const d = document.createElement('span');
    d.className = 'empty';
    d.textContent = '两个环境配置一致。';
    body.appendChild(d);
    return;
  }
  const fmt = (v) => {
    if (!v || !v.present) return '';
    if (v.sensitive) {
      const fp = v.fingerprint ? ` · 指纹 ${v.fingerprint}` : '';
      return `敏感值 · ${v.length} 字符${fp}`;
    }
    return v.value == null ? '' : v.value;
  };
  const line = (sym, cls, text) => {
    const row = document.createElement('div');
    row.className = 'row ' + cls;
    const s = document.createElement('span');
    s.className = 'sym';
    s.textContent = sym;
    const code = document.createElement('code');
    code.textContent = text;
    row.appendChild(s);
    row.appendChild(code);
    return row;
  };
  for (const c of changes) {
    if (c.kind === 'added') {
      body.appendChild(line('+', 'add', `${c.key} = ${fmt(c.new)}`));
    } else if (c.kind === 'removed') {
      body.appendChild(line('-', 'del', `${c.key} = ${fmt(c.old)}`));
    } else {
      body.appendChild(line('-', 'del', `${c.key} = ${fmt(c.old)}`));
      body.appendChild(line('+', 'add', `${c.key} = ${fmt(c.new)}`));
    }
  }
}

function applyCompareFilter() {
  const q = $('compare-filter').value.trim().toLowerCase();
  const all = state.compareMeta ? state.compareMeta.changes : [];
  const filtered = q ? all.filter((c) => c.key.toLowerCase().includes(q)) : all;
  renderCompare(filtered);
}

function copyText(text) {
  return new Promise((resolve, reject) => {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(resolve).catch(() => {
        legacyCopy(text) ? resolve() : reject(new Error('copy failed'));
      });
      return;
    }
    legacyCopy(text) ? resolve() : reject(new Error('copy failed'));
  });
  function legacyCopy(t) {
    const ta = document.createElement('textarea');
    ta.value = t;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch (_) { ok = false; }
    ta.remove();
    return ok;
  }
}

function copyCompareDiff() {
  const meta = state.compareMeta;
  if (!meta) return;
  const fmtLine = (v) => {
    if (!v || !v.present) return '—';
    if (v.sensitive) {
      const fp = v.fingerprint ? ` · 指纹 ${v.fingerprint}` : '';
      return `敏感值 · ${v.length} 字符${fp}`;
    }
    return v.value == null ? '' : v.value;
  };
  const lines = [`${meta.fromLabel} → ${meta.toLabel}`];
  for (const c of meta.changes) {
    if (c.kind === 'added') lines.push(`+ ${c.key} = ${fmtLine(c.new)}`);
    else if (c.kind === 'removed') lines.push(`- ${c.key} = ${fmtLine(c.old)}`);
    else {
      lines.push(`- ${c.key} = ${fmtLine(c.old)}`);
      lines.push(`+ ${c.key} = ${fmtLine(c.new)}`);
    }
  }
  copyText(lines.join('\n'))
    .then(() => {
      const btn = $('compare-copy-btn');
      btn.textContent = '已复制';
      setTimeout(() => { btn.textContent = '复制差异'; }, 1500);
    })
    .catch(() => {
      const btn = $('compare-copy-btn');
      btn.textContent = '复制失败';
      setTimeout(() => { btn.textContent = '复制差异'; }, 1500);
    });
}

// ---- multi-environment horizontal compare ----

let multiState = null;

function openMultiCompare() {
  const list = $('multi-ref-list');
  list.textContent = '';
  $('multi-compare-error').hidden = true;
  for (const s of state.snapshots) {
    const label = document.createElement('label');
    label.className = 'check-item';
    const cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.value = `${s.name}\u0000${s.app_id || ''}`;
    if (s.name === state.active && (s.app_id || '') === (state.activeAppID || '')) cb.checked = true;
    const txt = document.createElement('span');
    txt.textContent = refLabel(s.name, s.app_id);
    label.appendChild(cb);
    label.appendChild(txt);
    list.appendChild(label);
  }
  openDialog('multi-compare-dialog');
}

async function confirmMultiCompare() {
  const refs = [];
  for (const cb of $('multi-ref-list').querySelectorAll('input[type=checkbox]:checked')) {
    const [name, appID] = cb.value.split('\u0000');
    refs.push({ name, app_id: appID });
  }
  if (refs.length < 2) {
    dialogError('multi-compare-dialog', '请至少选择 2 个快照。');
    return;
  }
  try {
    const data = await api('/api/compare/multi', jsonOptions('POST', { refs }));
    multiState = data;
    renderMultiTable(data);
    closeDialog('multi-compare-dialog');
    openDialog('multi-result-dialog');
  } catch (err) {
    dialogError('multi-compare-dialog', err.message);
  }
}

function multiValueText(v) {
  if (!v || !v.present) return { text: '—', cls: 'absent' };
  if (v.sensitive) {
    const fp = v.fingerprint ? ` · 指纹 ${v.fingerprint}` : '';
    return { text: `敏感值 · ${v.length} 字符${fp}`, cls: 'masked' };
  }
  return { text: v.value == null ? '' : v.value, cls: 'plain' };
}

function stripQuotes(s) {
  if (typeof s !== 'string') return s;
  if (s.length >= 2 && ((s[0] === '"' && s[s.length - 1] === '"') || (s[0] === "'" && s[s.length - 1] === "'"))) {
    return s.slice(1, -1);
  }
  return s;
}

function rowHasDiff(row) {
  const seen = new Set();
  let presentCount = 0;
  for (const v of row.values) {
    if (!v || !v.present) continue;
    presentCount++;
    const t = v.sensitive ? `s:${v.length}:${v.fingerprint || ''}` : `p:${stripQuotes(v.value)}`;
    seen.add(t);
  }
  return seen.size > 1 || (presentCount > 0 && presentCount < row.values.length);
}

function renderMultiTable(data) {
  const wrap = $('multi-table-wrap');
  wrap.textContent = '';
  const table = document.createElement('table');
  table.className = 'multi-table';
  const thead = document.createElement('thead');
  const hrow = document.createElement('tr');
  const thKey = document.createElement('th');
  thKey.textContent = 'KEY';
  hrow.appendChild(thKey);
  for (const ref of data.refs) {
    const th = document.createElement('th');
    th.textContent = refLabel(ref.name, ref.app_id);
    th.title = `${ref.total} 项 · ${ref.sensitive} 个敏感`;
    hrow.appendChild(th);
  }
  thead.appendChild(hrow);
  table.appendChild(thead);
  const tbody = document.createElement('tbody');
  for (const row of data.rows) {
    const tr = document.createElement('tr');
    const tdKey = document.createElement('td');
    tdKey.className = 'k';
    tdKey.textContent = row.key;
    tr.appendChild(tdKey);
    let hasDiff = rowHasDiff(row);
    for (const v of row.values) {
      const td = document.createElement('td');
      const info = multiValueText(v);
      td.textContent = info.text;
      if (info.cls === 'masked') td.classList.add('m');
      if (info.cls === 'absent') td.classList.add('a');
      tr.appendChild(td);
    }
    if (hasDiff) tr.classList.add('diff-row');
    tbody.appendChild(tr);
  }
  table.appendChild(tbody);
  wrap.appendChild(table);
}

function copyMultiCompare() {
  if (!multiState) return;
  const lines = ['KEY\t' + multiState.refs.map((r) => refLabel(r.name, r.app_id)).join('\t')];
  for (const row of multiState.rows) {
    lines.push(row.key + '\t' + row.values.map((v) => {
      if (!v || !v.present) return '—';
      if (v.sensitive) return `敏感值·${v.length}字符${v.fingerprint ? `·${v.fingerprint}` : ''}`;
      return v.value == null ? '' : v.value;
    }).join('\t'));
  }
  copyText(lines.join('\n'))
    .then(() => {
      const btn = $('multi-copy-btn');
      btn.textContent = '已复制';
      setTimeout(() => { btn.textContent = '复制为表格（Tab 分隔）'; }, 1500);
    })
    .catch(() => {
      const btn = $('multi-copy-btn');
      btn.textContent = '复制失败';
      setTimeout(() => { btn.textContent = '复制为表格（Tab 分隔）'; }, 1500);
    });
}

function multiCellText(v) {
  if (!v || !v.present) return '—';
  if (v.sensitive) return `敏感值·${v.length}字符${v.fingerprint ? `·${v.fingerprint}` : ''}`;
  return v.value == null ? '' : v.value;
}

function showMultiReport() {
  if (!multiState) return;
  $('multi-report-body').textContent = buildMultiReportText(multiState);
  openDialog('multi-report-dialog');
}

function buildMultiReportText(data) {
  const refsLabel = data.refs.map((r) => refLabel(r.name, r.app_id)).join(' / ');
  const diffRows = data.rows.filter(rowHasDiff);
  const sensitiveDiff = diffRows.filter((r) => r.values.some((v) => v && v.sensitive)).length;
  const lines = [];
  lines.push('横向对比报告');
  lines.push(`对比环境：${refsLabel}`);
  lines.push(`统计：共 ${data.rows.length} 个 key，${diffRows.length} 个存在差异，${data.rows.length - diffRows.length} 个全部一致；敏感值差异 ${sensitiveDiff} 个`);
  lines.push('');
  if (!diffRows.length) {
    lines.push('所有环境配置完全一致。');
  } else {
    lines.push('差异字段：');
    for (const row of diffRows) {
      const cells = data.refs.map((r, i) => `${refLabel(r.name, r.app_id)}: ${multiCellText(row.values[i])}`);
      lines.push(`${row.key} → ${cells.join(' | ')}`);
    }
  }
  return lines.join('\n');
}

function copyMultiReport() {
  if (!multiState) return;
  copyText(buildMultiReportText(multiState))
    .then(() => {
      const btn = $('multi-report-copy-btn');
      btn.textContent = '已复制';
      setTimeout(() => { btn.textContent = '复制报告'; }, 1500);
    })
    .catch(() => {
      const btn = $('multi-report-copy-btn');
      btn.textContent = '复制失败';
      setTimeout(() => { btn.textContent = '复制报告'; }, 1500);
    });
}

function csvEscape(v) {
  if (/[",\n\r]/.test(v)) return '"' + v.replace(/"/g, '""') + '"';
  return v;
}

function copyMultiCSV() {
  if (!multiState) return;
  const lines = [['KEY', ...multiState.refs.map((r) => refLabel(r.name, r.app_id))].map(csvEscape).join(',')];
  for (const row of multiState.rows) {
    const cells = [row.key, ...row.values.map(multiCellText)].map(csvEscape);
    lines.push(cells.join(','));
  }
  copyText(lines.join('\n'))
    .then(() => {
      const btn = $('multi-csv-btn');
      btn.textContent = '已复制';
      setTimeout(() => { btn.textContent = '复制 CSV（逗号分隔）'; }, 1500);
    })
    .catch(() => {
      const btn = $('multi-csv-btn');
      btn.textContent = '复制失败';
      setTimeout(() => { btn.textContent = '复制 CSV（逗号分隔）'; }, 1500);
    });
}

// ---- single key across snapshots ----

function openKeyCompare(key) {
  const appid = state.activeAppID || '';
  $('key-compare-title').textContent = appid
    ? `单 key 对比：${key}（同 appid ${appid}）`
    : `单 key 对比：${key}（无 appid 快照）`;
  const body = $('key-compare-body');
  body.textContent = '';
  const loading = document.createElement('span');
  loading.className = 'empty';
  loading.textContent = '加载中…';
  body.appendChild(loading);
  openDialog('key-compare-dialog');
  loadKeyCompare(key);
}
async function loadKeyCompare(key) {
  const body = $('key-compare-body');
  try {
    const appid = state.activeAppID || '';
    const data = await api(`/api/compare/key?key=${encodeURIComponent(key)}&appid=${encodeURIComponent(appid)}`);
    body.textContent = '';
    if (!data.rows.length) {
      const d = document.createElement('span');
      d.className = 'empty';
      d.textContent = '没有快照包含此 key。';
      body.appendChild(d);
      return;
    }
    for (const row of data.rows) {
      const line = document.createElement('div');
      line.className = 'row';
      const ref = document.createElement('code');
      ref.className = 'key-ref';
      ref.textContent = refLabel(row.name, row.app_id);
      const val = document.createElement('code');
      const info = multiValueText(row.value);
      val.className = info.cls;
      val.textContent = info.text;
      line.appendChild(ref);
      line.appendChild(val);
      body.appendChild(line);
    }
  } catch (err) {
    body.textContent = '';
    const d = document.createElement('span');
    d.className = 'empty';
    d.textContent = err.message;
    body.appendChild(d);
  }
}

// ---- export ----

function openExport() {
  if (!state.active) {
    showError('当前没有已选快照。');
    return;
  }
  $('export-name').textContent = refLabel(state.active, state.activeAppID);
  openDialog('export-dialog');
}

async function confirmExport() {
  const name = state.active;
  if (!name) {
    dialogError('export-dialog', '当前没有已选快照。');
    return;
  }
  try {
    const res = await api(`/api/snapshots/${encodeURIComponent(name)}/export${snapshotQuery()}`,
      jsonOptions('POST', { confirm: true }));
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${name}${state.activeAppID ? `__${state.activeAppID}` : ''}.json`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    closeDialog('export-dialog');
  } catch (err) {
    dialogError('export-dialog', err.message);
  }
}

async function copyExport() {
  const name = state.active;
  if (!name) { dialogError('export-dialog', '当前没有已选快照。'); return; }
  try {
    const res = await api(`/api/snapshots/${encodeURIComponent(name)}/export${snapshotQuery()}`,
      jsonOptions('POST', { confirm: true }));
    const text = await res.text();
    await copyText(text);
    closeDialog('export-dialog');
  } catch (err) {
    dialogError('export-dialog', err.message);
  }
}

// ---- summary (placeholder) ----

function openSummary() {
  openDialog('summary-dialog');
}

// ---- snapshot delete ----

function snapshotQuery() {
  return state.activeAppID ? `?appid=${encodeURIComponent(state.activeAppID)}` : '';
}

let deletingSnapshot = null;

function openDeleteSnapshot(s) {
  deletingSnapshot = s;
  $('snap-delete-name').textContent = s.name;
  $('snap-delete-appid').textContent = s.app_id || '—';
  openDialog('snap-delete-dialog');
}

async function confirmDeleteSnapshot() {
  if (!deletingSnapshot) return;
  const { name, app_id } = deletingSnapshot;
  const q = app_id ? `?appid=${encodeURIComponent(app_id)}` : '';
  try {
    await api(`/api/snapshots/${encodeURIComponent(name)}${q}`, { method: 'DELETE' });
    closeDialog('snap-delete-dialog');
    deletingSnapshot = null;
    if (state.active === name && (state.activeAppID || '') === (app_id || '')) {
      state.active = null;
      state.activeAppID = '';
    }
    await refreshSnapshots();
  } catch (err) {
    dialogError('snap-delete-dialog', err.message);
  }
}

// ---- reveal ----

let revealItem = null;

function openReveal(item) {
  revealItem = item;
  $('reveal-key').textContent = item.key;
  $('reveal-value').textContent = '';
  $('reveal-value').hidden = true;
  $('reveal-error').hidden = true;
  $('reveal-advanced').hidden = true;
  $('reveal-key-input').value = '';
  $('reveal-iv-input').value = '';
  $('reveal-list-select').value = '';
  $('reveal-confirm-btn').hidden = false;
  $('reveal-confirm-btn').textContent = '显示';
  if (aesEntries.length) populateAESSelect($('reveal-list-select'));
  openDialog('reveal-dialog');
}

async function confirmReveal() {
  if (!revealItem) return;
  const key = revealItem.key;
  const aesKey = $('reveal-key-input').value.trim();
  const aesIV = $('reveal-iv-input').value.trim();
  try {
    const data = await api(`/api/snapshots/${encodeURIComponent(state.active)}/reveal${snapshotQuery()}`,
      jsonOptions('POST', { targets: [key], confirm: true, key: aesKey, iv: aesIV }));
    $('reveal-value').textContent = data.values[key] != null ? data.values[key] : '(空)';
    $('reveal-value').hidden = false;
    $('reveal-confirm-btn').hidden = true;
  } catch (err) {
    $('reveal-advanced').hidden = false;
    dialogError('reveal-dialog', err.message);
  }
}

function loadSelectedRevealEntry() {
  const name = $('reveal-list-select').value;
  if (!name) return;
  const e = aesEntries.find((x) => x.name === name);
  if (!e) return;
  $('reveal-key-input').value = e['secret-key'];
  $('reveal-iv-input').value = e.iv;
}

// ---- bulk edit ----

let editLoaded = false;

function openBulkEdit() {
  if (!state.active) { showError('当前没有已选快照。'); return; }
  $('edit-name').textContent = refLabel(state.active, state.activeAppID);
  $('edit-text').value = '';
  $('edit-save-btn').hidden = true;
  editLoaded = false;
  openDialog('edit-dialog');
  loadBulkEdit();
}

async function loadBulkEdit() {
  if (!state.active) return;
  try {
    const data = await api(`/api/snapshots/${encodeURIComponent(state.active)}/edit${snapshotQuery()}`,
      jsonOptions('POST', { confirm: true }));
    $('edit-text').value = data.text;
    $('edit-save-btn').hidden = false;
    editLoaded = true;
  } catch (err) {
    dialogError('edit-dialog', err.message);
  }
}

async function saveBulkEdit() {
  if (!state.active || !editLoaded) return;
  const text = $('edit-text').value;
  try {
    await api(`/api/snapshots/${encodeURIComponent(state.active)}/edit${snapshotQuery()}`,
      jsonOptions('PUT', { text }));
    closeDialog('edit-dialog');
    await reloadSnapshotData();
  } catch (err) {
    dialogError('edit-dialog', err.message);
  }
}

// ---- wiring ----

function wire() {
  document.addEventListener('click', (e) => {
    const close = e.target.closest('[data-close]');
    if (close) {
      close.closest('dialog').close();
      return;
    }
    const act = e.target.closest('[data-action]');
    if (!act) return;
    switch (act.dataset.action) {
      case 'import': openImport(); break;
      case 'compare': openCompare(); break;
      case 'export': openExport(); break;
      case 'focus-search': switchView('snapshots'); $('search-input').focus(); break;
      case 'summary': openSummary(); break;
      case 'bulk-edit': openBulkEdit(); break;
      case 'multi-compare': openMultiCompare(); break;
    }
  });

  $('nav-aes').addEventListener('click', () => switchView('aes'));
  $('nav-db').addEventListener('click', () => switchView('db'));
  $('nav-settings').addEventListener('click', () => switchView('settings'));

  $('db-init-btn').addEventListener('click', dbInit);
  $('db-add-btn').addEventListener('click', dbAdd);
  $('db-test-btn').addEventListener('click', dbTestURL);
  $('db-regen-all-btn').addEventListener('click', dbRegenAll);
  $('db-body').addEventListener('click', (e) => {
    const btn = e.target.closest('[data-db-action]');
    if (!btn) return;
    const name = btn.getAttribute('data-name');
    switch (btn.getAttribute('data-db-action')) {
      case 'test': dbTest(name); break;
      case 'connect': dbConnectInfo(name); break;
      case 'regen': dbRegen(name); break;
      case 'show': dbShow(name); break;
      case 'rm': dbRemove(name); break;
    }
  });
  $('db-connect-dialog').addEventListener('click', (e) => {
    const btn = e.target.closest('[data-copy]');
    if (!btn) return;
    const code = btn.closest('.db-line').querySelector('code');
    if (!code) return;
    copyText(code.textContent)
      .then(() => { btn.textContent = '已复制'; setTimeout(() => { btn.textContent = '复制'; }, 1200); })
      .catch(() => {});
  });
  $('db-show-copy-btn').addEventListener('click', () => {
    const text = $('db-show-url').textContent;
    if (!text) return;
    copyText(text)
      .then(() => { $('db-show-copy-btn').textContent = '已复制'; setTimeout(() => { $('db-show-copy-btn').textContent = '复制'; }, 1200); })
      .catch(() => {});
  });

  $('aes-encrypt-btn').addEventListener('click', () => runAES('encrypt'));
  $('aes-decrypt-btn').addEventListener('click', () => runAES('decrypt'));
  $('aes-gen-key').addEventListener('click', genAESKey);
  $('aes-load-config').addEventListener('click', () => loadSelectedAESEntry('aes-list-select'));
  $('aes-refresh-list').addEventListener('click', loadAESEntries);
  $('aes-save-config').addEventListener('click', saveAESConfig);
  $('aes-copy').addEventListener('click', copyAESOutput);
  $('aes-list-new').addEventListener('click', () => switchView('aes'));

  $('init-key-btn').addEventListener('click', initKey);
  $('init-sensitive-btn').addEventListener('click', initSensitiveKey);

  $('reveal-confirm-btn').addEventListener('click', confirmReveal);
  $('reveal-load-config').addEventListener('click', loadSelectedRevealEntry);
  $('edit-form').addEventListener('submit', (e) => { e.preventDefault(); saveBulkEdit(); });

  $('snap-delete-confirm-btn').addEventListener('click', confirmDeleteSnapshot);
  $('db-delete-confirm-btn').addEventListener('click', confirmDBDelete);
  $('db-regen-confirm-btn').addEventListener('click', confirmDBRegen);

  $('export-copy-btn').addEventListener('click', copyExport);

  $('search-input').addEventListener('input', renderTable);

  $('import-form').addEventListener('submit', (e) => {
    e.preventDefault();
    if ($('import-confirm-btn').hidden) runImportPreview();
    else confirmImport();
  });
  $('import-preview-btn').addEventListener('click', runImportPreview);
  $('import-name').addEventListener('input', checkImportDuplicate);
  $('import-appid').addEventListener('input', checkImportDuplicate);

  $('entry-form').addEventListener('submit', (e) => {
    e.preventDefault();
    saveEntry();
  });
  $('entry-delete-btn').addEventListener('click', openDelete);
  $('delete-confirm-btn').addEventListener('click', confirmDelete);
  $('compare-confirm-btn').addEventListener('click', confirmCompare);
  $('compare-filter').addEventListener('input', applyCompareFilter);
  $('compare-copy-btn').addEventListener('click', copyCompareDiff);
  $('multi-compare-confirm-btn').addEventListener('click', confirmMultiCompare);
  $('multi-copy-btn').addEventListener('click', copyMultiCompare);
  $('multi-report-btn').addEventListener('click', showMultiReport);
  $('multi-report-copy-btn').addEventListener('click', copyMultiReport);
  $('multi-csv-btn').addEventListener('click', copyMultiCSV);
  $('export-confirm-btn').addEventListener('click', confirmExport);
}

async function init() {
  wire();
  switchView('snapshots');
  if (!TOKEN) {
    showError('未携带访问令牌：请用启动时打印的完整 URL（含 ?t=...）打开，否则导入/修改/导出/解密等操作不可用。');
  }
  try {
    const cfg = await api('/api/config');
    if (cfg && !cfg.allow_plaintext) {
      $('plaintext-banner').hidden = false;
    }
  } catch (_) { /* banner is cosmetic */ }
  try {
    const data = await api('/api/snapshots');
    state.snapshots = data.snapshots;
  } catch (err) {
    showError(err.message);
  }
  renderRail();
  const first = state.snapshots[0];
  if (first) await loadSnapshot(first.name, first.app_id);
  else {
    state.active = null;
    state.activeAppID = '';
    renderContext();
    renderTable();
  }
}

document.addEventListener('DOMContentLoaded', init);
