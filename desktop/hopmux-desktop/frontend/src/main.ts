import '@xterm/xterm/css/xterm.css';
import './style.css';
import logoUrl from './assets/logo.png';

import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { Unicode11Addon } from '@xterm/addon-unicode11';

import {
  HostNames, Scan, OpenSession, SendInput, Resize, RescanHost, CloseSession,
  AddServer, GetSettings, SaveSettings, ReadSSHConfig, WriteSSHConfig,
} from '../wailsjs/go/main/App';
import { EventsOn, WindowToggleMaximise } from '../wailsjs/runtime/runtime';

const MONO = "'HopmuxMono', SFMono-Regular, Menlo, monospace";

// ---------- types ----------
interface Agent { Agent: string; SID: string; CWD: string; MTime: number; Title: string; Host: string; }
interface Tmux { Name: string; Windows: string; Attached: boolean; Host: string; }
interface GPU { Index: number; Util: number; MemUsed: number; MemTotal: number; Name: string; }
interface Host {
  Name: string; Reachable: boolean; Scanned: boolean; AuthRequired: boolean; Err: string;
  Tmux: Tmux[]; Agents: Agent[]; GPUs: GPU[]; Hostname: string; Now: number;
}

const hosts = new Map<string, Host>();
let order: string[] = [];
let selected: string | null = null;
let showGPU = false;
let fontSize = 13;

function setFontSize(px: number) {
  fontSize = Math.max(8, Math.min(28, px));
  for (const t of tabs) { t.term.options.fontSize = fontSize; t.fit.fit(); Resize(t.id, t.term.cols, t.term.rows); }
}

// ---------- elements ----------
const $serverList = document.getElementById('server-list')!;
const $pickerHead = document.getElementById('picker-head')!;
const $sessionList = document.getElementById('session-list')!;
const $picker = document.getElementById('picker')!;
const $terminals = document.getElementById('terminals')!;
const $tabbar = document.getElementById('tabbar')!;
const $connText = document.getElementById('conn-text')!;
const $connDot = document.getElementById('conn-dot')!;
(document.getElementById('logo') as HTMLImageElement).src = logoUrl;

// Double-click the title bar to maximize / restore (standard macOS behavior).
document.getElementById('titlebar')!.addEventListener('dblclick', () => WindowToggleMaximise());

// ---------- tabs ----------
interface Tab {
  id: string; title: string; color: string;
  term: Terminal; fit: FitAddon; wrap: HTMLElement; tabEl: HTMLElement;
}
const tabs: Tab[] = [];
let activeTab: string | null = null;

function xtermTheme(light: boolean) {
  return light
    ? { background: '#fbfbfa', foreground: '#1c2128', cursor: '#c25b3a', selectionBackground: '#e2dfd8' }
    : { background: '#0d1017', foreground: '#e6e6e6', cursor: '#d97757', selectionBackground: '#243040',
        red: '#f85149', green: '#3fb950', yellow: '#d29922', blue: '#2bb6c4',
        magenta: '#d97757', cyan: '#2bb6c4', white: '#e6e6e6', brightBlack: '#3b4351' };
}

function newTab(id: string, title: string, color: string): Tab {
  const term = new Terminal({
    fontFamily: MONO, fontSize, lineHeight: 1.1, cursorBlink: true,
    allowProposedApi: true, scrollback: 1000,
    theme: xtermTheme(document.documentElement.classList.contains('light')),
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  const uni = new Unicode11Addon(); term.loadAddon(uni); term.unicode.activeVersion = '11';

  const wrap = document.createElement('div');
  wrap.className = 'term-wrap';
  $terminals.append(wrap);

  const tabEl = document.createElement('div');
  tabEl.className = 'tab';
  tabEl.innerHTML = `<span class="tdot" style="background:${color}"></span>` +
    `<span class="tname"></span><span class="tclose">✕</span>`;
  (tabEl.querySelector('.tname') as HTMLElement).textContent = title;
  tabEl.onclick = (e) => {
    if ((e.target as HTMLElement).classList.contains('tclose')) { closeTab(id); return; }
    activateTab(id);
  };
  $tabbar.append(tabEl);
  keepNewBtnLast();

  const tab: Tab = { id, title, color, term, fit, wrap, tabEl };
  tabs.push(tab);

  term.onData((d) => SendInput(id, d));
  EventsOn('pty:data:' + id, (d: string) => term.write(d));
  EventsOn('pty:exit:' + id, () => {
    term.write('\r\n\x1b[90m[session ended — ⌘W or click ✕ to close the tab]\x1b[0m\r\n');
  });

  (async () => {
    try { await (document as any).fonts.load('13px HopmuxMono'); await (document as any).fonts.ready; } catch {}
    term.open(wrap);
    fitTab(tab);
  })();
  return tab;
}

function fitTab(tab: Tab) {
  try { tab.fit.fit(); Resize(tab.id, tab.term.cols, tab.term.rows); } catch {}
}

function activateTab(id: string) {
  activeTab = id;
  $picker.classList.add('hidden');
  $terminals.classList.remove('hidden');
  for (const t of tabs) {
    const on = t.id === id;
    t.wrap.classList.toggle('hidden', !on);
    t.tabEl.classList.toggle('active', on);
    if (on) { setTimeout(() => { fitTab(t); t.term.focus(); }, 0); }
  }
  paintSidebar();
}

function closeTab(id: string) {
  const i = tabs.findIndex((t) => t.id === id);
  if (i < 0) return;
  const t = tabs[i];
  CloseSession(id);
  t.term.dispose(); t.wrap.remove(); t.tabEl.remove();
  tabs.splice(i, 1);
  if (activeTab === id) {
    const next = tabs[i] || tabs[i - 1];
    if (next) activateTab(next.id);
    else showPicker();
  }
}

function showPicker() {
  activeTab = null;
  $terminals.classList.add('hidden');
  $picker.classList.remove('hidden');
  for (const t of tabs) t.tabEl.classList.remove('active');
  renderPicker();
}

window.addEventListener('resize', () => { const t = tabs.find((x) => x.id === activeTab); if (t) fitTab(t); });

// ---------- helpers ----------
function rel(mtime: number): string {
  const now = Math.floor(Date.now() / 1000);
  let d = Math.max(0, now - mtime);
  if (d < 60) return `${d}s`; if (d < 3600) return `${(d / 60) | 0}m`;
  if (d < 86400) return `${(d / 3600) | 0}h`; if (d < 86400 * 30) return `${(d / 86400) | 0}d`;
  return `${(d / (86400 * 30)) | 0}mo`;
}
function project(cwd: string): string { const p = (cwd || '').replace(/\/+$/, ''); return p ? p.split('/').pop() || p : '?'; }
function sessionCount(h: Host): number { return (h.Tmux?.length || 0) + (h.Agents?.length || 0); }
function el(tag: string, cls?: string, text?: string): HTMLElement {
  const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e;
}
function dotColor(h?: Host): string {
  if (!h?.Scanned) return 'var(--muted)';
  if (h.AuthRequired) return 'var(--warning)';
  if (!h.Reachable) return 'var(--danger)';
  return 'var(--tmux)';
}
function subtitle(h?: Host): string {
  if (!h) return '';
  if (h.AuthRequired) return 'needs login';
  if (h.Scanned && !h.Reachable) return h.Err ? 'unreachable' : 'offline';
  if (h.Hostname) return h.Hostname;
  return '';
}
function gpuColor(pct: number): string { return pct >= 80 ? 'var(--danger)' : pct >= 40 ? 'var(--claude)' : 'var(--tmux)'; }
function miniBar(pct: number, w = 6): string { const f = Math.max(0, Math.min(w, Math.round((pct / 100) * w))); return '▓'.repeat(f) + '░'.repeat(w - f); }

// ---------- sidebar ----------
function paintSidebar() {
  $serverList.innerHTML = '';
  const recent = el('div', 'server recent' + (selected === null ? ' active' : ''));
  recent.append(el('span', 'dot'), (() => { const m = el('div', 'meta'); m.append(el('div', 'nm', '★ Recent sessions')); return m; })());
  (recent.querySelector('.dot') as HTMLElement).style.background = 'var(--claude)';
  recent.onclick = () => selectServer(null);
  $serverList.append(recent);

  for (const name of order) {
    const h = hosts.get(name);
    const row = el('div', 'server' + (selected === name ? ' active' : ''));
    const dot = el('span', 'dot'); dot.style.background = dotColor(h);
    const meta = el('div', 'meta');
    meta.append(el('div', 'nm', name));
    const sub = subtitle(h); if (sub) meta.append(el('div', 'sub', sub));
    row.append(dot, meta);
    if (showGPU && h?.Reachable && h.GPUs?.length) {
      const mx = Math.max(...h.GPUs.map((g) => g.Util));
      const used = h.GPUs.filter((g) => g.Util >= 20).length;
      const b = el('span', 'gpu-badge', `${used}/${h.GPUs.length} ${mx}%`); b.style.color = gpuColor(mx);
      row.append(b);
    } else if (h?.Reachable && sessionCount(h) > 0) {
      row.append(el('span', 'cnt', String(sessionCount(h))));
    }
    row.onclick = () => selectServer(name);
    $serverList.append(row);
  }
  // connection status
  const scanned = order.filter((n) => hosts.get(n)?.Scanned).length;
  const reachable = order.filter((n) => hosts.get(n)?.Reachable).length;
  if (scanned < order.length) { $connText.textContent = `Scanning… ${scanned}/${order.length}`; $connDot.style.color = 'var(--warning)'; }
  else { $connText.textContent = `${reachable}/${order.length} connected`; $connDot.style.color = reachable ? 'var(--tmux)' : 'var(--danger)'; }
}

// ---------- picker (session list) ----------
function selectServer(name: string | null) { selected = name; showPicker(); paintSidebar(); }

function recentAgents(): Agent[] {
  const all: Agent[] = [];
  for (const n of order) { const h = hosts.get(n); if (h?.Agents) all.push(...h.Agents); }
  return all.sort((a, b) => b.MTime - a.MTime).slice(0, 60);
}

function gpuBarHTML(h: Host): string {
  if (!showGPU || !h.GPUs?.length) return '';
  const segs = h.GPUs.map((g) => {
    const load = Math.max(g.Util, g.MemTotal ? Math.round((g.MemUsed * 100) / g.MemTotal) : 0);
    const mem = `${(g.MemUsed / 1024).toFixed(0)}/${(g.MemTotal / 1024).toFixed(0)}G`;
    return `<span style="color:${gpuColor(load)}">GPU${g.Index} ${miniBar(load)} ${g.Util}%</span> <span class="sub">${mem}</span>`;
  });
  return `<div class="gpu-bar">${segs.join('&nbsp;&nbsp;·&nbsp;&nbsp;')}</div>`;
}

function renderPicker() {
  if (!$picker.classList.contains('hidden')) { /* only when visible */ }
  $sessionList.innerHTML = '';
  if (selected === null) {
    const scanned = order.filter((n) => hosts.get(n)?.Scanned).length;
    $pickerHead.innerHTML = `★ Recent sessions <span class="sub">· ${scanned}/${order.length} scanned</span>`;
    const rec = recentAgents();
    if (!rec.length) { $sessionList.append(el('div', 'empty', 'scanning…')); return; }
    for (const a of rec) $sessionList.append(agentRow(a, true));
    return;
  }
  const h = hosts.get(selected); if (!h) return;
  if (h.AuthRequired) {
    $pickerHead.innerHTML = `${selected} <span class="sub">${h.Hostname || ''}</span>`;
    const r = el('div', 'sess login'); r.append(el('div', 'info', '⚿ needs interactive login — click to log in'));
    r.onclick = () => openTerminal({ host: selected!, kind: 'login', name: '', agent: '', sid: '', cwd: '' }, 'login', 'var(--warning)');
    $sessionList.append(r); return;
  }
  if (!h.Reachable) {
    $pickerHead.innerHTML = `${selected} <span class="sub">unreachable</span>`;
    const r = el('div', 'sess login'); r.append(el('div', 'info', '✗ ' + (h.Err || 'unreachable') + ' — click to try ssh'));
    r.onclick = () => openTerminal({ host: selected!, kind: 'login', name: '', agent: '', sid: '', cwd: '' }, 'ssh', 'var(--danger)');
    $sessionList.append(r); return;
  }
  const nc = h.Agents?.filter((a) => a.Agent === 'claude').length || 0;
  const nx = h.Agents?.filter((a) => a.Agent === 'codex').length || 0;
  $pickerHead.innerHTML = `${selected} <span class="sub">${h.Hostname || ''} · ${h.Tmux?.length || 0} tmux · ${nc} claude · ${nx} codex</span>` + gpuBarHTML(h);
  for (const t of (h.Tmux || [])) $sessionList.append(tmuxRow(t));
  for (const a of (h.Agents || []).slice().sort((x, y) => y.MTime - x.MTime)) $sessionList.append(agentRow(a, false));
  if (!sessionCount(h)) $sessionList.append(el('div', 'empty', 'no sessions here yet'));
}

function tmuxRow(t: Tmux): HTMLElement {
  const r = el('div', 'sess');
  const l1 = el('div', 'l1'); l1.append(el('span', 'tag tmux', '▣ tmux'), el('span', 'title', t.Name));
  r.append(l1, el('div', 'l2', `${t.Windows} windows · ${t.Attached ? 'attached' : 'detached'}`));
  r.onclick = () => openTerminal({ host: t.Host, kind: 'tmux', name: t.Name, agent: '', sid: '', cwd: '' }, t.Name, 'var(--tmux)');
  return r;
}
function agentRow(a: Agent, showHost: boolean): HTMLElement {
  const r = el('div', 'sess');
  const l1 = el('div', 'l1');
  l1.append(el('span', `tag ${a.Agent}`, a.Agent === 'claude' ? '◉ claude' : '◉ codex'),
    el('span', 'title', (showHost ? `[${a.Host}] ` : '') + (a.Title || '(no prompt yet)')));
  r.append(l1, el('div', 'l2', `${a.CWD || '~'} · ${rel(a.MTime)}`));
  const color = a.Agent === 'claude' ? 'var(--claude)' : 'var(--codex)';
  r.onclick = () => openTerminal({ host: a.Host, kind: 'agent', name: '', agent: a.Agent, sid: a.SID, cwd: a.CWD }, `${project(a.CWD)}`, color);
  return r;
}

// ---------- open a session as a new tab ----------
async function openTerminal(req: any, title: string, color: string) {
  const id = await OpenSession(req);
  const tab = newTab(id, `${req.host} · ${title}`, color);
  activateTab(tab.id);
  if (req.kind === 'login') startLoginPoll(req.host);
}

let loginPoll: number | undefined;
function startLoginPoll(host: string) {
  clearInterval(loginPoll); let tries = 0;
  loginPoll = window.setInterval(async () => {
    tries++;
    const h: Host = await RescanHost(host);
    if (h.Reachable && !h.AuthRequired) { clearInterval(loginPoll); if (selected === host) renderPicker(); }
    else if (tries > 40) clearInterval(loginPoll);
  }, 3000);
}

// ---------- modals ----------
const $overlay = document.getElementById('modal-overlay')!;
const $modalTitle = document.getElementById('modal-title')!;
const $modalBody = document.getElementById('modal-body')!;
function openModal(title: string, bodyHTML: string) {
  $modalTitle.textContent = title;
  $modalBody.innerHTML = bodyHTML;
  $overlay.classList.remove('hidden');
}
function closeModal() { $overlay.classList.add('hidden'); $modalBody.innerHTML = ''; }
document.getElementById('modal-close')!.onclick = closeModal;
$overlay.onclick = (e) => { if (e.target === $overlay) closeModal(); };

// ---- Add server ----
document.getElementById('add-server')!.onclick = () => {
  openModal('Add server', `
    <div class="field"><label>Alias (Host)</label><input id="f-alias" placeholder="ml-train-02" autofocus></div>
    <div class="row2">
      <div class="field"><label>HostName (IP / domain)</label><input id="f-hostname" placeholder="10.0.0.5"></div>
      <div class="field"><label>Port</label><input id="f-port" placeholder="22"></div>
    </div>
    <div class="field"><label>User</label><input id="f-user" placeholder="ubuntu"></div>
    <div class="hint">Appends a <b>Host</b> block to ~/.ssh/config, then rescans.</div>
    <div class="modal-err" id="f-err"></div>
    <div class="modal-actions">
      <div class="btn" id="f-cancel">Cancel</div>
      <div class="btn primary" id="f-save">Add</div>
    </div>`);
  (document.getElementById('f-alias') as HTMLInputElement)?.focus();
  document.getElementById('f-cancel')!.onclick = closeModal;
  document.getElementById('f-save')!.onclick = async () => {
    const g = (id: string) => (document.getElementById(id) as HTMLInputElement).value;
    const err = await AddServer(g('f-alias'), g('f-hostname'), g('f-port'), g('f-user'));
    if (err) { document.getElementById('f-err')!.textContent = err; return; }
    closeModal();
  };
};

// ---- Settings ----
document.getElementById('open-settings')!.onclick = async () => {
  const s = await GetSettings();
  const cfg = await ReadSSHConfig();
  openModal('Settings', `
    <div class="settings-section">Appearance</div>
    <div class="field"><label>Theme</label>
      <select id="s-theme"><option value="dark">Dark</option><option value="light">Light</option></select></div>
    <div class="field"><label>Terminal font size</label><input id="s-font" type="number" min="9" max="24" value="${s.fontSize}"></div>
    <div class="settings-section">Scanning</div>
    <div class="row2">
      <div class="field"><label>Auto-refresh (seconds, 0 = off)</label><input id="s-refresh" type="number" min="0" value="${s.autoRefreshSec}"></div>
      <div class="field"><label>Per-host timeout (s)</label><input id="s-timeout" type="number" min="2" max="30" value="${s.scanTimeoutSec}"></div>
    </div>
    <div class="settings-section">~/.ssh/config</div>
    <div class="field"><textarea id="s-config" spellcheck="false"></textarea>
      <div class="hint">Edited config is backed up to ~/.ssh/config.hopmux.bak before saving.</div></div>
    <div class="modal-err" id="s-err"></div>
    <div class="modal-actions">
      <div class="btn" id="s-cancel">Cancel</div>
      <div class="btn primary" id="s-save">Save</div>
    </div>`);
  (document.getElementById('s-theme') as HTMLSelectElement).value = s.theme;
  (document.getElementById('s-config') as HTMLTextAreaElement).value = cfg;
  document.getElementById('s-cancel')!.onclick = closeModal;
  document.getElementById('s-save')!.onclick = async () => {
    const num = (id: string) => parseInt((document.getElementById(id) as HTMLInputElement).value || '0', 10);
    const theme = (document.getElementById('s-theme') as HTMLSelectElement).value;
    const newSettings = { theme, autoRefreshSec: num('s-refresh'), scanTimeoutSec: num('s-timeout'), fontSize: num('s-font') };
    await SaveSettings(newSettings as any);
    applySettings(newSettings);
    const cfgErr = await WriteSSHConfig((document.getElementById('s-config') as HTMLTextAreaElement).value);
    if (cfgErr) { document.getElementById('s-err')!.textContent = cfgErr; return; }
    closeModal();
  };
};

function applySettings(s: { theme: string; autoRefreshSec: number; fontSize: number }) {
  const light = s.theme === 'light';
  document.documentElement.classList.toggle('light', light);
  for (const t of tabs) t.term.options.theme = xtermTheme(light);
  if (s.fontSize) setFontSize(s.fontSize);
  refreshMs = (s.autoRefreshSec || 0) * 1000;
}

// new-tab "+" button, kept last in the tab bar
const newTabBtn = el('div', 'tab-new', '＋');
newTabBtn.onclick = () => showPicker();
function keepNewBtnLast() { $tabbar.append(newTabBtn); } // append moves it to the end

// ---------- keyboard ----------
function terminalActive(): boolean { return activeTab !== null; }
function toggleSidebar() { document.getElementById('sidebar')!.classList.toggle('collapsed'); }
function toggleTheme() {
  const light = document.documentElement.classList.toggle('light');
  for (const t of tabs) t.term.options.theme = xtermTheme(light);
}
document.addEventListener('keydown', (ev) => {
  // Esc closes an open modal (Settings / Add server) before anything else.
  if (ev.key === 'Escape' && !$overlay.classList.contains('hidden')) {
    closeModal(); ev.preventDefault(); return;
  }
  // Zoom (font size), like a terminal: Cmd/Ctrl +/- and 0 to reset.
  if (ev.metaKey || ev.ctrlKey) {
    switch (ev.code) {
      case 'Equal': case 'NumpadAdd': setFontSize(fontSize + 1); ev.preventDefault(); return;
      case 'Minus': case 'NumpadSubtract': setFontSize(fontSize - 1); ev.preventDefault(); return;
      case 'Digit0': case 'Numpad0': setFontSize(13); ev.preventDefault(); return;
    }
  }
  const mod = ev.metaKey || ev.altKey;
  if (mod && !ev.ctrlKey) {
    switch (ev.code) {
      case 'KeyB': toggleSidebar(); ev.preventDefault(); return;
      case 'KeyG': showGPU = !showGPU; paintSidebar(); renderPicker(); ev.preventDefault(); return;
      case 'KeyR': Scan(); ev.preventDefault(); return;
      case 'KeyD': toggleTheme(); ev.preventDefault(); return;
      case 'KeyW': if (activeTab) closeTab(activeTab); ev.preventDefault(); return;
    }
  }
  if (terminalActive()) return; // keys go to the xterm
  // picker navigation
  const rows = Array.from($sessionList.querySelectorAll('.sess')) as HTMLElement[];
  if (!rows.length) return;
  let i = rows.findIndex((r) => r.classList.contains('kbsel'));
  if (ev.key === 'ArrowDown' || ev.key === 'j') { i = Math.min(i + 1, rows.length - 1); if (i < 0) i = 0; rows.forEach((r, k) => r.classList.toggle('kbsel', k === i)); rows[i].scrollIntoView({ block: 'nearest' }); ev.preventDefault(); }
  else if (ev.key === 'ArrowUp' || ev.key === 'k') { i = i <= 0 ? 0 : i - 1; rows.forEach((r, k) => r.classList.toggle('kbsel', k === i)); rows[i].scrollIntoView({ block: 'nearest' }); ev.preventDefault(); }
  else if (ev.key === 'Enter' && i >= 0) { rows[i].click(); ev.preventDefault(); }
});

// ---------- auto-refresh (safe: only already-reachable hosts) ----------
let refreshMs = 20000;
let refreshing = false;
setInterval(async () => {
  if (!refreshMs || refreshing || activeTab) return; // skip while a terminal tab is focused
  const live = order.filter((n) => { const h = hosts.get(n); return h?.Reachable && !h.AuthRequired; });
  if (!live.length) return;
  refreshing = true;
  try { for (const name of live) await RescanHost(name); } finally { refreshing = false; }
}, 5000); // check every 5s; only acts when refreshMs has elapsed conceptually (simple gate)

// ---------- boot ----------
EventsOn('host:update', (h: Host) => {
  hosts.set(h.Name, h); paintSidebar();
  if (!activeTab && (selected === null || selected === h.Name)) renderPicker();
});
EventsOn('scan:done', () => { paintSidebar(); if (!activeTab) renderPicker(); });
EventsOn('hosts:reloaded', (names: string[]) => { order = names; hosts.clear(); paintSidebar(); if (!activeTab) renderPicker(); });

GetSettings().then((s: any) => {
  refreshMs = (s.autoRefreshSec || 0) * 1000;
  if (s.theme === 'light') document.documentElement.classList.add('light');
});
HostNames().then((names: string[]) => { order = names; paintSidebar(); showPicker(); });
