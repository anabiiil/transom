/* Transom control panel.
 *
 * Loaded (not deferred) from <head>: the first block applies the saved
 * theme before the first paint; everything that touches the DOM waits for
 * DOMContentLoaded (see init at the bottom).
 *
 * Talks to the Go server over the contract in docs/CONTRACT.md: every call
 * is a POST with a JSON body and the X-Transom-Token header; responses are
 * {ok, data} / {ok:false, error}.
 */
'use strict';

const token = new URLSearchParams(location.search).get('t') || '';

/* ── plumbing ──────────────────────────────────────────────── */
async function api(path, body) {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'X-Transom-Token': token, 'Content-Type': 'application/json' },
    body: JSON.stringify(body || {}),
  });
  let out;
  try { out = await res.json(); } catch { throw new Error(`${res.status} ${res.statusText || 'bad response'}`); }
  if (!out || !out.ok) throw new Error((out && out.error) || 'request failed');
  return out.data;
}
function lsGet(k) { try { return localStorage.getItem(k); } catch { return null; } }
function lsSet(k, v) { try { localStorage.setItem(k, v); } catch {} }

/* ── preferences ───────────────────────────────────────────── */
// TransomPrefs keeps panel preferences on the server (/api/prefs/get,
// /api/prefs/set) — the panel is served from a new port on every launch,
// so localStorage (per-origin) is effectively wiped each time. localStorage
// stays as a cache: get() answers from it until the server copy arrives,
// then from memory. set() updates memory at once and saves (debounced per key).
const TransomPrefs = (() => {
  const LS = 'transom-pref:';
  const mem = {}, timers = {}, pending = {};
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k && k.startsWith(LS)) mem[k.slice(LS.length)] = localStorage.getItem(k);
    }
  } catch {}
  const cache = (k, v) => {
    try { if (v === '') localStorage.removeItem(LS + k); else localStorage.setItem(LS + k, v); } catch {}
  };
  const ready = api('/api/prefs/get', {}).then(server => {
    server = server || {};
    // The server is authoritative, except for keys set since loading began.
    for (const k of Object.keys(mem)) {
      if (!(k in server) && !pending[k]) { delete mem[k]; cache(k, ''); }
    }
    for (const [k, v] of Object.entries(server)) {
      if (pending[k]) continue;
      mem[k] = String(v);
      cache(k, String(v));
    }
  }).catch(() => {});
  function get(key, def) {
    return Object.prototype.hasOwnProperty.call(mem, key) ? mem[key] : def;
  }
  function set(key, value) {
    const v = value == null ? '' : String(value);
    if (v === '') delete mem[key]; else mem[key] = v;
    cache(key, v);
    pending[key] = true;
    clearTimeout(timers[key]);
    timers[key] = setTimeout(() => {
      api('/api/prefs/set', { key, value: v })
        .catch(() => {}).finally(() => { delete pending[key]; });
    }, 300);
  }
  return { ready, get, set };
})();

/* ── theme ─────────────────────────────────────────────────── */
// The preference is "light", "dark" or "auto" (follow the system). It is
// applied from the localStorage cache first — no flash on the first paint —
// then corrected from the server copy once /api/prefs/get resolves.
const darkMQ = window.matchMedia('(prefers-color-scheme: dark)');
let themePref = 'auto';
function resolveTheme(p) { return p === 'light' || p === 'dark' ? p : (darkMQ.matches ? 'dark' : 'light'); }
function applyTheme(pref, save) {
  themePref = pref === 'light' || pref === 'dark' ? pref : 'auto';
  document.documentElement.dataset.theme = resolveTheme(themePref);
  document.querySelectorAll('[data-theme-set]').forEach(b => {
    const on = b.dataset.themeSet === themePref;
    b.classList.toggle('active', on);
    b.setAttribute('aria-pressed', on ? 'true' : 'false');
  });
  lsSet('transom-theme', themePref);
  if (save) TransomPrefs.set('theme', themePref);
}
applyTheme(TransomPrefs.get('theme') || lsGet('transom-theme') || 'auto', false);
darkMQ.addEventListener('change', () => { if (themePref === 'auto') applyTheme('auto', false); });

/* ── helpers ───────────────────────────────────────────────── */
const $ = id => document.getElementById(id);
const esc = s => String(s == null ? '' : s).replace(/[&<>"']/g, c =>
  ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const plural = (n, one, many) => `${fmtInt(n)} ${n === 1 ? one : (many || one + 's')}`;
function fmtInt(n) { return Number(n || 0).toLocaleString(); }
// Sizes are base 1000, like Finder: 1 KB = 1000 bytes.
function fmtSize(bytes) {
  const b = Math.max(0, Number(bytes) || 0);
  if (b < 1000) return b === 1 ? '1 byte' : `${fmtInt(b)} bytes`;
  const units = ['KB', 'MB', 'GB', 'TB', 'PB'];
  let v = b / 1000, i = 0;
  while (v >= 1000 && i < units.length - 1) { v /= 1000; i++; }
  const digits = v >= 100 || i === 0 ? 0 : 1;
  return `${v.toLocaleString(undefined, { maximumFractionDigits: digits })} ${units[i]}`;
}
const rtf = typeof Intl !== 'undefined' && Intl.RelativeTimeFormat
  ? new Intl.RelativeTimeFormat(undefined, { numeric: 'always' }) : null;
// relTime: "just now", "5 minutes ago", "2 days ago", "3 months ago", …
function relTime(iso) {
  const t = Date.parse(iso);
  if (!t) return '—';
  const s = (t - Date.now()) / 1000;
  const a = Math.abs(s);
  if (a < 45) return 'just now';
  const steps = [[3600, 60, 'minute'], [86400, 3600, 'hour'], [86400 * 7, 86400, 'day'],
    [86400 * 30.44, 86400 * 7, 'week'], [86400 * 365.25, 86400 * 30.44, 'month'], [Infinity, 86400 * 365.25, 'year']];
  for (const [lim, div, unit] of steps) {
    if (a < lim) {
      const n = Math.round(s / div) || (s < 0 ? -1 : 1);
      return rtf ? rtf.format(n, unit) : `${Math.abs(n)} ${unit}${Math.abs(n) === 1 ? '' : 's'} ago`;
    }
  }
  return '—';
}
function absTime(iso) {
  const t = Date.parse(iso);
  return t ? new Date(t).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }) : '';
}
function fmtElapsed(ms) {
  const s = Math.floor((Number(ms) || 0) / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, '0')}s`;
}
// splitPath: the head (directories) and tail (last component) for the
// middle-elided path display.
function splitPath(p) {
  p = String(p || '');
  const trimmed = p.replace(/\/+$/, '');
  const i = trimmed.lastIndexOf('/');
  if (i <= 0) return ['', p];
  return [p.slice(0, i), p.slice(i)];
}

/* ── icons ─────────────────────────────────────────────────── */
const svg = inner => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${inner}</svg>`;
const ICONS = {
  // stacked layers — caches
  cache: svg('<path d="M12 3l9 4.5-9 4.5-9-4.5z"/><path d="M3 12l9 4.5 9-4.5"/><path d="M3 16.5L12 21l9-4.5"/>'),
  // document with lines — logs
  log: svg('<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><polyline points="14 3 14 8 19 8"/><line x1="8.5" y1="12.5" x2="15.5" y2="12.5"/><line x1="8.5" y1="16" x2="15.5" y2="16"/>'),
  trash: svg('<polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6M14 11v6"/><path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/>'),
  // hammer — Xcode / build products
  xcode: svg('<path d="M15 12l-8.5 8.5a2.12 2.12 0 0 1-3-3L12 9"/><path d="M17.64 15L22 10.64"/><path d="M20.91 11.7l-1.25-1.25a2.4 2.4 0 0 1-.7-1.7V7.86L16.7 5.6a5.56 5.56 0 0 0-3.94-1.64H9l.92.82A6.18 6.18 0 0 1 12 9.4v1.56l2 2h2.47l2.26 1.91"/>'),
  // hexagon — node_modules & friends
  node: svg('<path d="M12 2l8.66 5v10L12 22l-8.66-5V7z"/><path d="M12 22V12"/><path d="M20.66 7L12 12 3.34 7"/>'),
  // stacked containers — Docker
  docker: svg('<rect x="3" y="11" width="18" height="9" rx="2"/><rect x="5.5" y="6.5" width="5" height="4.5" rx="1"/><rect x="11.5" y="6.5" width="5" height="4.5" rx="1"/><rect x="8.5" y="2.5" width="5" height="4" rx="1"/>'),
  // package — package caches / Homebrew
  box: svg('<path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/>'),
  file: svg('<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><polyline points="14 3 14 8 19 8"/>'),
  copy: svg('<rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>'),
  // app window with a missing tile — leftovers of removed apps
  app: svg('<rect x="3" y="3" width="18" height="18" rx="4.5"/><rect x="7" y="7" width="4" height="4" rx="1"/><rect x="13" y="7" width="4" height="4" rx="1"/><rect x="7" y="13" width="4" height="4" rx="1"/><path d="M13 15h4" stroke-dasharray="1.5 1.5"/>'),
};
const iconFor = k => ICONS[k] || ICONS.file;
const GLYPH = {
  scan: svg('<circle cx="11" cy="11" r="7"/><line x1="21" y1="21" x2="16" y2="16"/>'),
  sparkle: '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M11 3c.6 5.2 2.8 7.4 8 8-5.2.6-7.4 2.8-8 8-.6-5.2-2.8-7.4-8-8 5.2-.6 7.4-2.8 8-8z"/><path d="M18.5 2.5c.2 1.6.9 2.3 2.5 2.5-1.6.2-2.3.9-2.5 2.5-.2-1.6-.9-2.3-2.5-2.5 1.6-.2 2.3-.9 2.5-2.5z" opacity=".6"/></svg>',
  disk: svg('<rect x="2" y="13" width="20" height="8" rx="2"/><path d="M5.5 13L8 4h8l2.5 9"/><line x1="6" y1="17" x2="6.01" y2="17"/><line x1="10" y1="17" x2="10.01" y2="17"/>'),
  chev: '<svg class="chev" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="6 9 12 15 18 9"/></svg>',
  arrow: svg('<line x1="5" y1="12" x2="19" y2="12"/><polyline points="13 6 19 12 13 18"/>'),
  warn: svg('<path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>'),
  clean: svg('<path d="M3 21l7.5-7.5"/><path d="M14.5 3.5l6 6-7 7-6-6z"/><path d="M9.5 8.5l6 6"/>'),
  clock: svg('<circle cx="12" cy="12" r="9"/><polyline points="12 7 12 12 15.5 14"/>'),
};
// Risk is shown as a word + a distinct glyph + a colour, never colour alone.
const RISK = {
  safe:    { label: 'Safe',    title: 'Safe — regenerated automatically',
             icon: svg('<circle cx="12" cy="12" r="9"/><polyline points="8 12.5 11 15.5 16.5 9.5"/>') },
  review:  { label: 'Review',  title: 'Review — probably unwanted, but check first',
             icon: svg('<path d="M1.5 12s4-7 10.5-7 10.5 7 10.5 7-4 7-10.5 7S1.5 12 1.5 12z"/><circle cx="12" cy="12" r="3"/>') },
  caution: { label: 'Caution', title: 'Caution — these are your own files',
             icon: svg('<path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9.5" x2="12" y2="13.5"/><line x1="12" y1="17" x2="12.01" y2="17"/>') },
};
function riskBadge(r) {
  const k = RISK[r] ? r : 'review';
  return `<span class="badge risk ${k}" title="${esc(RISK[k].title)}">${RISK[k].icon}${RISK[k].label}</span>`;
}

/* ── state ─────────────────────────────────────────────────── */
const GROUPS = [
  { id: 'system',    name: 'System Junk', sub: 'Caches, logs, temporary files, Mail downloads and the Trash' },
  { id: 'developer', name: 'Developer',   sub: 'Xcode, simulators, package-manager caches, Homebrew and Docker' },
  { id: 'projects',  name: 'Projects',    sub: 'Dependency folders in projects you haven\'t touched lately' },
  { id: 'files',     name: 'Files',       sub: 'Large and old files, duplicates, and leftovers of removed apps' },
];
const GROUP_BY_ID = Object.fromEntries(GROUPS.map(g => [g.id, g]));
const ITEM_LIMIT = 200;

let categories = [];          // [Category] from /api/categories
let categoriesReady = Promise.resolve();
let scan = null;              // the latest ScanResult (items sorted by size)
const itemIndex = new Map();  // item id -> { item, cat }
const selected = new Set();   // ONE global selection of item ids, shared by every page
const expanded = new Set();   // category ids whose item list is open
const showAll = new Set();    // category ids showing past ITEM_LIMIT
let job = null;               // running scan: { id, ids, status, timer }
let disk = null;              // /api/disk
let currentPage = 'overview';

function catMeta(id) { return categories.find(c => c.id === id); }
function groupCats(g) {
  if (scan) return scan.categories.filter(c => c.group === g);
  return [];
}
function setScan(result, preselect) {
  const cats = (result && result.categories) || [];
  scan = {
    scannedAt: result && result.scannedAt,
    totalSize: 0,
    categories: cats.map(c => ({
      ...c,
      items: (c.items || []).slice().sort((a, b) => (b.size || 0) - (a.size || 0)),
    })),
  };
  itemIndex.clear();
  for (const c of scan.categories) for (const it of c.items) itemIndex.set(it.id, { item: it, cat: c });
  recomputeTotals();
  if (preselect) {
    // Only safe items start selected (CONTRACT.md, safety principle 5).
    selected.clear();
    for (const [id, { item }] of itemIndex) if (item.risk === 'safe') selected.add(id);
    showAll.clear();
  } else {
    for (const id of [...selected]) if (!itemIndex.has(id)) selected.delete(id);
  }
}
function recomputeTotals() {
  if (!scan) return;
  let total = 0;
  for (const c of scan.categories) {
    c.totalSize = c.items.reduce((s, it) => s + (it.size || 0), 0);
    c.count = c.items.length;
    total += c.totalSize;
  }
  scan.totalSize = total;
}
function selectionTotals(filter) {
  let n = 0, size = 0;
  for (const id of selected) {
    const e = itemIndex.get(id);
    if (!e || (filter && !filter(e))) continue;
    n++; size += e.item.size || 0;
  }
  return { n, size };
}
function groupTotals(g) {
  const cats = groupCats(g);
  return {
    scanned: cats.length > 0,
    size: cats.reduce((s, c) => s + c.totalSize, 0),
    count: cats.reduce((s, c) => s + c.count, 0),
    sel: selectionTotals(e => e.cat.group === g),
  };
}

/* ── modal system ──────────────────────────────────────────── */
// modal() renders an in-page dialog instead of native confirm()/alert() —
// WKWebView shows those as NSAlerts that can't be themed. Dimmed+blurred
// backdrop, centered card, focus trap, Esc cancels, Enter confirms (unless
// the confirm button is disabled). Resolves null when cancelled.
//   opts.body     extra HTML under the message
//   opts.onMount(ctx)   wire up custom controls; ctx = { card, confirmBtn, cancelBtn, close }
//   opts.onConfirm(ctx) async; return false to keep the dialog open (multi-step),
//                       anything else closes it and resolves with that value
let modalDepth = 0;
function modal(opts) {
  return new Promise((resolve) => {
    const id = ++modalDepth;
    const titleId = 'modal-title-' + id;
    const backdrop = document.createElement('div');
    backdrop.className = 'modal-backdrop';
    const card = document.createElement('div');
    card.className = 'modal-card';
    card.setAttribute('role', 'dialog');
    card.setAttribute('aria-modal', 'true');
    card.setAttribute('aria-labelledby', titleId);

    let html = '';
    if (opts.icon !== false) {
      html += `<div class="modal-icon${opts.danger ? ' danger' : ''}">${opts.danger ? GLYPH.warn : (opts.iconSvg || GLYPH.sparkle)}</div>`;
    }
    html += `<h3 class="modal-title" id="${titleId}">${opts.title || ''}</h3>`;
    if (opts.message) html += `<p class="modal-message">${opts.message}</p>`;
    if (opts.detail) html += `<div class="modal-detail">${opts.detail}</div>`;
    if (opts.body) html += `<div class="modal-body">${opts.body}</div>`;
    html += '<div class="modal-actions">';
    if (opts.cancelText !== null) html += `<button type="button" id="modal-cancel">${opts.cancelText || 'Cancel'}</button>`;
    html += `<button type="button" id="modal-confirm" class="${opts.danger ? 'danger' : 'primary'}">${opts.confirmText || 'OK'}</button>`;
    html += '</div>';
    card.innerHTML = html;
    backdrop.appendChild(card);
    document.body.appendChild(backdrop);

    const prevFocus = document.activeElement;
    const cancelBtn = card.querySelector('#modal-cancel');
    const confirmBtn = card.querySelector('#modal-confirm');
    let closed = false, working = false;
    const ctx = { card, confirmBtn, cancelBtn, close };

    function close(result) {
      if (closed) return;
      closed = true;
      document.removeEventListener('keydown', onKey, true);
      backdrop.classList.remove('show');
      setTimeout(() => backdrop.remove(), 180);
      if (prevFocus && prevFocus.focus && document.contains(prevFocus)) prevFocus.focus();
      resolve(result);
    }
    async function confirm() {
      if (working || confirmBtn.disabled) return;
      if (!opts.onConfirm) { close(true); return; }
      working = true;
      try {
        const r = await opts.onConfirm(ctx);
        if (r !== false) close(r === undefined ? true : r);
      } finally { working = false; }
    }
    function onKey(e) {
      if (e.key === 'Escape') {
        e.preventDefault();
        if (!working) close(null);
      } else if (e.key === 'Enter') {
        const active = document.activeElement;
        if (active && active.tagName === 'INPUT' && active.type === 'checkbox') return;
        if (active === cancelBtn || (active && active.tagName === 'SUMMARY')) return;
        e.preventDefault();
        confirm();
      } else if (e.key === 'Tab') {
        const focusables = Array.from(card.querySelectorAll('button, input, select, summary, [tabindex]:not([tabindex="-1"])'))
          .filter(el => !el.disabled && el.offsetParent !== null && !(el.type === 'radio' && !el.checked));
        if (!focusables.length) { e.preventDefault(); return; }
        const first = focusables[0], last = focusables[focusables.length - 1];
        if (!card.contains(document.activeElement)) { e.preventDefault(); first.focus(); }
        else if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
        else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
      }
    }
    if (cancelBtn) cancelBtn.onclick = () => { if (!working) close(null); };
    confirmBtn.onclick = confirm;
    backdrop.onclick = (e) => { if (e.target === backdrop && !working) close(null); };
    document.addEventListener('keydown', onKey, true);
    if (opts.onMount) opts.onMount(ctx);

    requestAnimationFrame(() => {
      backdrop.classList.add('show');
      const target = opts.danger ? (cancelBtn || confirmBtn) : (confirmBtn.disabled ? (card.querySelector('input') || cancelBtn) : confirmBtn);
      if (target) target.focus();
    });
  });
}
function alertModal(opts) {
  if (typeof opts === 'string') opts = { message: opts };
  return modal({ title: 'Notice', confirmText: 'OK', cancelText: null, ...opts });
}

/* ── toast & busy ──────────────────────────────────────────── */
let toastTimer = null;
// toast(msg, { bad, failed:[{id,path,error}] }) — with failures the toast
// stays until dismissed and carries an expandable list.
function toast(msg, opts) {
  opts = opts || {};
  const t = $('toast');
  const failed = opts.failed || [];
  let html = `<div class="tm">${esc(msg)}`;
  if (failed.length) {
    html += `<details><summary>${plural(failed.length, 'item')} could not be cleaned</summary><ul>` +
      failed.map(f => `<li>${esc(f.error || 'failed')}<br><code>${esc(f.path || f.id)}</code></li>`).join('') +
      '</ul></details>';
  }
  html += '</div>';
  if (failed.length) html += '<button type="button" class="tx" aria-label="Dismiss">Close</button>';
  t.innerHTML = html;
  t.classList.toggle('bad', !!opts.bad || failed.length > 0);
  t.classList.toggle('sticky', failed.length > 0);
  t.classList.add('show');
  clearTimeout(toastTimer);
  const hide = () => { t.classList.remove('show', 'sticky'); };
  const x = t.querySelector('.tx');
  if (x) x.onclick = hide;
  if (!failed.length) toastTimer = setTimeout(hide, opts.bad ? 5500 : 3800);
}
function busy(label) {
  if (label) { $('busy-label').textContent = label; $('busy').classList.add('show'); }
  else $('busy').classList.remove('show');
}

/* ── router ────────────────────────────────────────────────── */
const TITLES = {
  overview: ['Overview', 'Scan your Mac and see what can safely go'],
  system: [GROUP_BY_ID.system.name, GROUP_BY_ID.system.sub],
  developer: [GROUP_BY_ID.developer.name, GROUP_BY_ID.developer.sub],
  projects: [GROUP_BY_ID.projects.name, GROUP_BY_ID.projects.sub],
  files: [GROUP_BY_ID.files.name, GROUP_BY_ID.files.sub],
  history: ['History', 'What Transom has removed, newest first'],
  settings: ['Settings', 'Project folders and scan options'],
};
function showPage(route) {
  const name = TITLES[route] ? route : 'overview';
  const changed = name !== currentPage;
  currentPage = name;
  const pageId = GROUP_BY_ID[name] ? 'page-group' : 'page-' + name;
  document.querySelectorAll('.page').forEach(p => {
    const on = p.id === pageId;
    // Re-trigger the rise animation when switching between group pages.
    if (on && changed && p.classList.contains('active')) { p.classList.remove('active'); void p.offsetWidth; }
    p.classList.toggle('active', on);
  });
  document.querySelectorAll('.nav a').forEach(a => {
    const on = a.dataset.page === name;
    a.classList.toggle('active', on);
    if (on) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
  });
  $('page-title').textContent = TITLES[name][0];
  $('page-sub').textContent = TITLES[name][1];
  document.title = name === 'overview' ? 'Transom' : `${TITLES[name][0]} — Transom`;
  if (GROUP_BY_ID[name]) renderGroup();
  if (name === 'overview') renderOverview();
  if (name === 'history') loadHistory();
  if (name === 'settings') renderSettings();
}

/* ── disk ──────────────────────────────────────────────────── */
async function loadDisk() {
  try { disk = await api('/api/disk', {}); } catch (e) { disk = { error: e.message }; }
  renderDisk();
}
function renderDisk() {
  const el = $('disk-card');
  if (!disk) { el.innerHTML = `<span class="k">${GLYPH.disk} Disk</span><span class="d">Loading…</span>`; return; }
  if (disk.error) { el.innerHTML = `<span class="k">${GLYPH.disk} Disk</span><span class="d">Couldn't read disk usage: ${esc(disk.error)}</span>`; return; }
  const total = disk.total || 0, free = disk.free || 0;
  const used = disk.used || Math.max(0, total - free);
  const recl = scan ? Math.min(scan.totalSize, used) : 0;
  const pct = v => total ? Math.max(0, Math.min(100, v / total * 100)) : 0;
  el.innerHTML = `
    <span class="k">${GLYPH.disk} ${esc(disk.volume || 'Disk')}</span>
    <span class="v">${fmtSize(free)}</span>
    <span class="d">available of ${fmtSize(total)}</span>
    <div class="dbar" role="img" aria-label="${esc(`${fmtSize(used)} used, ${fmtSize(free)} free${recl ? `, ${fmtSize(recl)} reclaimable` : ''}`)}">
      <i class="used" style="width:${pct(used - recl)}%"></i><i class="recl" style="width:${pct(recl)}%"></i>
    </div>
    <div class="legend">
      <span style="--c:#aeb6c8">Used ${fmtSize(used - recl)}</span>
      ${recl ? `<span style="--c:var(--accent)">Reclaimable ${fmtSize(recl)}</span>` : ''}
      <span style="--c:rgba(255,255,255,.2)">Free ${fmtSize(free)}</span>
    </div>`;
}

/* ── scanning ──────────────────────────────────────────────── */
function scanOptions() {
  const n = (k, d) => { const v = parseInt(TransomPrefs.get(k, ''), 10); return v > 0 ? v : d; };
  // roots: [] lets the server use its default (~ excluding ~/Library).
  return { staleDays: n('staleDays', 60), largeMinMB: n('largeMinMB', 500), roots: [] };
}
async function startScan(ids) {
  if (job) return;
  ids = ids || [];
  try {
    const d = await api('/api/scan/start', { categories: ids, options: scanOptions() });
    job = { id: d.jobId, ids, status: { state: 'running', progress: { category: '', scanned: 0, found: 0, elapsedMs: 0 } } };
    TransomPrefs.set('lastScanJob', d.jobId);
    renderAll();
    pollScan();
  } catch (e) {
    toast(`Couldn't start the scan: ${e.message}`, { bad: true });
  }
}
async function pollScan() {
  if (!job) return;
  const id = job.id;
  let st;
  try {
    st = await api('/api/scan/status', { jobId: id });
  } catch (e) {
    if (!job || job.id !== id) return;
    job = null;
    toast(`Scan failed: ${e.message}`, { bad: true });
    renderAll();
    return;
  }
  if (!job || job.id !== id) return;
  job.status = st;
  if (st.state === 'running') {
    renderScanProgress();
    job.timer = setTimeout(pollScan, 400);
    return;
  }
  job = null;
  if (st.state === 'done' && st.result) {
    setScan(st.result, true);
    const sel = selectionTotals();
    toast(sel.n ? `Scan complete — ${fmtSize(scan.totalSize)} found; ${fmtSize(sel.size)} of safe items selected`
      : `Scan complete — ${fmtSize(scan.totalSize)} found; review the items to choose what to clean`);
  } else if (st.state === 'cancelled') {
    toast('Scan cancelled');
  } else {
    toast(`Scan failed: ${st.error || 'unknown error'}`, { bad: true });
  }
  renderAll();
  renderDisk();
}
async function cancelScan(btn) {
  if (!job) return;
  if (btn) btn.disabled = true;
  try { await api('/api/scan/cancel', { jobId: job.id }); }
  catch (e) { toast(`Couldn't cancel: ${e.message}`, { bad: true }); if (btn) btn.disabled = false; }
  // The next poll picks up state "cancelled".
}
// On load, pick the previous scan back up (the server keeps it until the
// next scan) so a reload doesn't lose results.
async function resumeLastScan() {
  await TransomPrefs.ready;
  const id = TransomPrefs.get('lastScanJob');
  if (!id || job || scan) return;
  try {
    const st = await api('/api/scan/status', { jobId: id });
    if (job || scan) return;
    if (st.state === 'running') {
      job = { id, ids: [], status: st };
      renderAll();
      pollScan();
    } else if (st.state === 'done' && st.result) {
      setScan(st.result, true);
      renderAll();
      renderDisk();
    }
  } catch { /* the server restarted: that job is gone */ }
}
function scanList() {
  const ids = job && job.ids.length ? job.ids : categories.map(c => c.id);
  return ids;
}
function progressInfo() {
  const p = (job && job.status && job.status.progress) || {};
  const ids = scanList();
  const cat = categories.find(c => c.id === p.category || c.name === p.category);
  const idx = cat ? ids.indexOf(cat.id) : -1;
  const pct = idx >= 0 && ids.length ? Math.round((idx + 0.5) / ids.length * 100) : null;
  return {
    name: cat ? cat.name : (p.category || 'Starting…'),
    step: idx >= 0 ? `${idx + 1} of ${ids.length}` : '',
    pct,
    scanned: fmtInt(p.scanned),
    found: fmtInt(p.found),   // an item count (scan.Progress.AddFound counts items)
    elapsed: fmtElapsed(p.elapsedMs),
  };
}
function progressHTML() {
  const i = progressInfo();
  return `
    <div class="prog-cat"><span data-prog="name">${esc(i.name)}</span> <span class="muted" data-prog="step">${esc(i.step)}</span></div>
    <div class="prog${i.pct == null ? ' indet' : ''}" data-prog="bar" role="progressbar" aria-label="Scan progress"
         aria-valuemin="0" aria-valuemax="100"${i.pct == null ? '' : ` aria-valuenow="${i.pct}"`}><i style="width:${i.pct || 0}%"></i></div>
    <div class="prog-stats">
      <div><span>Scanned</span><b data-prog="scanned">${i.scanned}</b></div>
      <div><span>Items found</span><b data-prog="found">${i.found}</b></div>
      <div><span>Elapsed</span><b data-prog="elapsed">${i.elapsed}</b></div>
    </div>`;
}
// renderScanProgress updates the progress blocks in place (no re-render,
// so focus on the Cancel button survives each 400 ms tick).
function renderScanProgress() {
  const i = progressInfo();
  document.querySelectorAll('[data-prog]').forEach(el => {
    const k = el.dataset.prog;
    if (k === 'bar') {
      el.classList.toggle('indet', i.pct == null);
      el.firstElementChild.style.width = (i.pct || 0) + '%';
      if (i.pct == null) el.removeAttribute('aria-valuenow'); else el.setAttribute('aria-valuenow', i.pct);
    } else if (k in i) el.textContent = i[k];
  });
}

/* ── overview ──────────────────────────────────────────────── */
function renderOverview() {
  renderScanCard();
  renderResults();
}
function renderScanCard() {
  const el = $('scan-card');
  if (job) {
    const scope = job.ids.length ? `${plural(job.ids.length, 'category', 'categories')}` : 'everything';
    el.innerHTML = `
      <span class="k">${GLYPH.scan} Scanning ${esc(scope)}</span>
      ${progressHTML()}
      <div class="actions"><button type="button" class="on-mint" data-act="cancel-scan">Cancel scan</button></div>`;
    return;
  }
  if (!scan) {
    el.innerHTML = `
      <span class="k">${GLYPH.sparkle} Smart Scan</span>
      <span class="v">Find what you can clean</span>
      <span class="d">Checks all ${categories.length || 15} categories — caches, logs, developer junk, stale dependencies, large and duplicate files.
        Scanning is read-only: nothing is removed until you choose to clean.</span>
      <div class="actions"><button type="button" class="ink lg" data-act="scan">${GLYPH.scan}Scan</button></div>`;
    return;
  }
  const sel = selectionTotals();
  el.innerHTML = `
    <span class="k">${GLYPH.sparkle} Reclaimable</span>
    <span class="v">${fmtSize(scan.totalSize)}</span>
    <span class="d">${plural(itemIndex.size, 'item')} found · scanned ${esc(relTime(scan.scannedAt))}</span>
    <div class="actions">
      ${sel.n ? `<button type="button" class="ink" data-act="clean">${GLYPH.clean}Clean selected (${plural(sel.n, 'item')}, ${fmtSize(sel.size)})</button>` :
        '<span class="d">Nothing selected — open a group below to choose items.</span>'}
      <button type="button" class="on-mint" data-act="scan">Scan again</button>
    </div>`;
}
function renderResults() {
  const el = $('ov-results');
  if (!scan) { el.innerHTML = ''; return; }
  const cards = GROUPS.map(g => {
    const t = groupTotals(g.id);
    const icon = { system: 'cache', developer: 'xcode', projects: 'node', files: 'copy' }[g.id];
    if (!t.scanned || job) {
      return `<a class="stat dim" href="#${g.id}">
        <span class="k">${iconFor(icon)} ${esc(g.name)} ${GLYPH.arrow.replace('<svg', '<svg class="go"')}</span>
        <span class="v">${job ? 'Scanning…' : 'Not scanned'}</span>
        <span class="d">${esc(g.sub)}</span></a>`;
    }
    return `<a class="stat" href="#${g.id}">
      <span class="k">${iconFor(icon)} ${esc(g.name)} ${GLYPH.arrow.replace('<svg', '<svg class="go"')}</span>
      <span class="v">${fmtSize(t.size)}</span>
      <span class="d">${plural(t.count, 'item')}${t.sel.n ? ` · ${fmtInt(t.sel.n)} selected (${fmtSize(t.sel.size)})` : ''}</span></a>`;
  }).join('');
  el.innerHTML = `
    <div class="sec-head"><h2>By group</h2><span class="sub">${scan && !job ? 'Open a group to review items before cleaning' : ''}</span></div>
    <div class="stats" style="margin-top:14px">${cards}</div>`;
}

/* ── group pages ───────────────────────────────────────────── */
function renderGroup() {
  const g = currentPage;
  const root = $('page-group');
  if (!GROUP_BY_ID[g]) return;
  if (job) {
    root.innerHTML = `
      <section class="card"><div class="card-head">${GLYPH.scan}<h2>Scanning…</h2>
        <span class="hint"><button type="button" class="sm" data-act="cancel-scan">Cancel</button></span></div>
        <div class="card-body">${progressHTML()}</div></section>`;
    return;
  }
  const cats = groupCats(g);
  if (!cats.length) { root.innerHTML = emptyGroupHTML(g); return; }
  const t = groupTotals(g);
  root.innerHTML = `
    <div class="toolbar">
      <span class="sum" id="group-sum">${groupSumHTML(t)}</span>
      <button type="button" class="sm" data-act="sel-safe">Select safe</button>
      <button type="button" class="sm" data-act="sel-all">Select all</button>
      <button type="button" class="sm" data-act="sel-none">Select none</button>
      <button type="button" class="sm" data-act="scan-group">Rescan group</button>
    </div>
    <div class="cats">${cats.map(catHTML).join('')}</div>`;
  syncSelectionUI();
}
function groupSumHTML(t) {
  return `<b>${fmtSize(t.size)}</b> in ${plural(t.count, 'item')} · <b>${fmtInt(t.sel.n)}</b> selected (${fmtSize(t.sel.size)})`;
}
function emptyGroupHTML(g) {
  const meta = categories.filter(c => c.group === g);
  const scannedOther = !!scan;
  return `
    <section class="card">
      <div class="empty-state">
        <div class="ic">${GLYPH.scan}</div>
        <h3>${scannedOther ? 'Not part of the last scan' : 'Not scanned yet'}</h3>
        <p>Scan ${esc(GROUP_BY_ID[g].name)} to see how much space ${meta.length === 1 ? 'this category takes' : `these ${meta.length || ''} categories take`}.
          Scanning is read-only — nothing is removed until you choose to clean.</p>
        <button type="button" class="primary" data-act="scan-group">${GLYPH.scan}Scan this group</button>
      </div>
      ${meta.length ? `<ul class="covers">${meta.map(c => `
        <li><span class="cat-icon">${iconFor(c.icon)}</span>
          <span class="t"><b>${esc(c.name)}</b><span>${esc(c.description)}</span></span>
          ${riskBadge(c.risk)}</li>`).join('')}</ul>` : ''}
    </section>`;
}
function catHTML(c) {
  const open = expanded.has(c.id) && c.items.length > 0;
  const bodyId = 'cat-body-' + c.id;
  const empty = c.items.length === 0;
  return `
    <section class="card cat${empty ? ' empty-cat' : ''}" data-cat="${esc(c.id)}">
      <div class="cat-head">
        <input type="checkbox" class="cbx" data-cat-check="${esc(c.id)}" aria-label="Select all items in ${esc(c.name)}"${empty ? ' disabled' : ''}>
        <span class="cat-icon">${iconFor(c.icon)}</span>
        <button type="button" class="cat-toggle" data-toggle="${esc(c.id)}" aria-expanded="${open}" aria-controls="${bodyId}"${empty ? ' disabled' : ''}>
          <span class="cat-title">
            <span class="nm">${esc(c.name)} ${riskBadge(c.risk)}</span>
            <span class="ds">${esc(c.description)}</span>
          </span>
          <span class="cat-meta">
            <b>${empty ? 'Nothing found' : fmtSize(c.totalSize)}</b>
            <span data-cat-sel="${esc(c.id)}">${empty ? '' : plural(c.count, 'item')}</span>
          </span>
          ${empty ? '' : GLYPH.chev}
        </button>
      </div>
      <div class="cat-body" id="${bodyId}"${open ? '' : ' hidden'}>${open ? catBodyHTML(c) : ''}</div>
    </section>`;
}
function catBodyHTML(c) {
  const all = showAll.has(c.id);
  const limit = all ? Infinity : ITEM_LIMIT;
  let html = '<div class="items">';
  if (c.id === 'duplicates' || c.items.some(it => it.group)) {
    // Group duplicate copies by their set id, biggest sets first.
    const sets = new Map();
    for (const it of c.items) {
      const k = it.group || '';
      if (!sets.has(k)) sets.set(k, []);
      sets.get(k).push(it);
    }
    const ordered = [...sets.entries()].sort((a, b) =>
      b[1].reduce((s, x) => s + x.size, 0) - a[1].reduce((s, x) => s + x.size, 0));
    let shown = 0, setNo = 0;
    for (const [, items] of ordered) {
      if (shown >= limit) break;
      setNo++;
      const name = splitPath(items[0].path)[1].replace(/^\//, '') || items[0].label;
      const size = items.reduce((s, x) => s + x.size, 0);
      html += `<div class="dup-head"><span>Set ${setNo}</span><span class="nm" title="${esc(name)}">${esc(name)}</span>
        <span class="r">${plural(items.length, 'extra copy', 'extra copies')} · ${fmtSize(size)}</span></div>`;
      for (const it of items) {
        if (shown >= limit) break;
        html += itemHTML(it, c);
        shown++;
      }
    }
  } else {
    c.items.slice(0, limit).forEach(it => { html += itemHTML(it, c); });
  }
  html += '</div>';
  if (c.items.length > ITEM_LIMIT) {
    html += all
      ? `<div class="more"><span class="muted">Showing all ${fmtInt(c.items.length)} items</span>
           <button type="button" class="sm" data-showall="${esc(c.id)}">Show fewer</button></div>`
      : `<div class="more"><span class="muted">Showing the ${fmtInt(ITEM_LIMIT)} largest of ${fmtInt(c.items.length)} items</span>
           <button type="button" class="sm" data-showall="${esc(c.id)}">Show all ${fmtInt(c.items.length)}</button></div>`;
  }
  return html;
}
let cbxSeq = 0;
function itemHTML(it, c) {
  const cid = 'ck-' + (++cbxSeq);
  const [h, t] = splitPath(it.path);
  const isCmd = it.kind === 'command';
  const riskDiff = it.risk && it.risk !== c.risk;
  return `
    <div class="item" data-id="${esc(it.id)}">
      <input type="checkbox" class="cbx" id="${cid}" data-item="${esc(it.id)}">
      <label for="${cid}" class="item-main">
        <span class="item-label"><span class="lt">${esc(it.label || t.replace(/^\//, ''))}</span>
          ${isCmd ? '<span class="kind-cmd">command</span>' : ''}${riskDiff ? riskBadge(it.risk) : ''}</span>
        <span class="path" title="${esc(it.path)}"><span class="h">${esc(h)}</span><span class="t">${esc(t)}</span></span>
        ${it.note ? `<span class="item-note">${esc(it.note)}</span>` : ''}
      </label>
      <span class="item-size">${fmtSize(it.size)}</span>
      <span class="item-time" title="${esc(absTime(it.modTime))}">${esc(relTime(it.modTime))}</span>
      ${isCmd || !it.path ? '<span class="reveal-ph"></span>'
        : `<button type="button" class="sm reveal" data-reveal="${esc(it.id)}" title="Show in Finder" aria-label="Reveal ${esc(it.label || it.path)} in Finder">Reveal</button>`}
    </div>`;
}
function toggleCat(id) {
  const c = scan && scan.categories.find(x => x.id === id);
  if (!c || !c.items.length) return;
  const sec = document.querySelector(`.cat[data-cat="${CSS.escape(id)}"]`);
  if (!sec) return;
  const body = sec.querySelector('.cat-body');
  const btn = sec.querySelector('.cat-toggle');
  if (expanded.has(id)) {
    expanded.delete(id);
    body.hidden = true;
    body.innerHTML = '';
    btn.setAttribute('aria-expanded', 'false');
  } else {
    expanded.add(id);
    body.innerHTML = catBodyHTML(c);
    body.hidden = false;
    btn.setAttribute('aria-expanded', 'true');
  }
  syncSelectionUI();
}
function rerenderCatBody(id) {
  const c = scan && scan.categories.find(x => x.id === id);
  const body = document.getElementById('cat-body-' + id);
  if (!c || !body || body.hidden) return;
  body.innerHTML = catBodyHTML(c);
  syncSelectionUI();
}

/* ── selection ─────────────────────────────────────────────── */
// syncSelectionUI reflects the global `selected` Set onto whatever is
// rendered (item/category checkboxes, counts, the sticky bar, overview)
// without re-rendering rows, so focus and scroll position are kept.
function syncSelectionUI() {
  document.querySelectorAll('input[data-item]').forEach(cb => { cb.checked = selected.has(cb.dataset.item); });
  if (scan) {
    for (const c of scan.categories) {
      const cb = document.querySelector(`input[data-cat-check="${CSS.escape(c.id)}"]`);
      if (!cb) continue;
      let n = 0, size = 0;
      for (const it of c.items) if (selected.has(it.id)) { n++; size += it.size || 0; }
      cb.checked = n > 0 && n === c.items.length;
      cb.indeterminate = n > 0 && n < c.items.length;
      const lbl = document.querySelector(`[data-cat-sel="${CSS.escape(c.id)}"]`);
      if (lbl && c.items.length) lbl.textContent = n ? `${fmtInt(n)} of ${plural(c.count, 'item')} · ${fmtSize(size)} selected` : plural(c.count, 'item');
    }
  }
  const sum = $('group-sum');
  if (sum && GROUP_BY_ID[currentPage]) sum.innerHTML = groupSumHTML(groupTotals(currentPage));
  renderSelbar();
  if (currentPage === 'overview') renderOverview();
  renderNavCounts();
}
function renderSelbar() {
  const t = selectionTotals();
  const bar = $('selbar');
  const show = t.n > 0 && !job;
  bar.hidden = !show;
  document.body.classList.toggle('has-sel', show);
  $('sel-count').textContent = `${plural(t.n, 'item')} selected`;
  $('sel-size').textContent = fmtSize(t.size);
  $('sel-clean').textContent = `Clean ${fmtSize(t.size)}…`;
}
function selectWhere(pred, on) {
  for (const [id, e] of itemIndex) if (pred(e)) { if (on) selected.add(id); else selected.delete(id); }
  syncSelectionUI();
}
function renderNavCounts() {
  for (const g of GROUPS) {
    const el = $('nav-' + g.id);
    const t = groupTotals(g.id);
    el.hidden = !t.scanned || !t.size || !!job;
    el.textContent = fmtSize(t.size);
  }
}
function renderAll() {
  const running = !!job;
  $('scan-pill').hidden = !running;
  renderNavCounts();
  renderSelbar();
  if (currentPage === 'overview') renderOverview();
  if (GROUP_BY_ID[currentPage]) renderGroup();
  renderDisk();
}

/* ── clean flow ────────────────────────────────────────────── */
async function openClean() {
  if (job) return;
  const ids = [...selected].filter(id => itemIndex.has(id));
  if (!ids.length) return;
  const entries = ids.map(id => itemIndex.get(id));
  const total = entries.reduce((s, e) => s + (e.item.size || 0), 0);
  const caution = entries.filter(e => e.item.risk === 'caution').length;
  const inTrash = entries.filter(e => e.cat.id === 'trash').length;
  const cmds = entries.filter(e => e.item.kind === 'command').length;
  const catNames = [...new Set(entries.map(e => e.cat.name))];
  const catText = catNames.length <= 3 ? catNames.map(esc).join(', ') : `${plural(catNames.length, 'category', 'categories')}`;

  let body = `
    <fieldset class="modes">
      <legend>How</legend>
      <label class="mode-opt"><input type="radio" name="clean-mode" value="trash" checked>
        <span><span class="ct">Move to Trash (recoverable)</span><span class="ch">Put back from the Trash in Finder if you change your mind.</span></span></label>
      <label class="mode-opt del"><input type="radio" name="clean-mode" value="delete">
        <span><span class="ct">Delete permanently</span><span class="ch">Frees the space right away. This can't be undone.</span></span></label>
    </fieldset>`;
  if (caution) {
    body += `<div class="warn">${GLYPH.warn}<span><b>${plural(caution, 'item is', 'items are')} marked Caution</b> — these are your own files
      (large files or duplicates), not regenerable junk. Check them before continuing.</span></div>`;
  }
  if (inTrash) {
    body += `<div class="warn">${GLYPH.warn}<span>${plural(inTrash, 'item')} already in the Trash will be <b>deleted permanently</b> whichever option you pick.</span></div>`;
  }
  if (cmds) {
    body += `<div class="modal-detail">${plural(cmds, 'item runs a cleanup command', 'items run cleanup commands')} (like <span class="mono">brew cleanup</span>); what they remove can't be moved to the Trash.</div>`;
  }
  body += `
    <div class="modal-checks" id="ack-wrap" hidden>
      <label class="modal-check"><input type="checkbox" id="clean-ack">
        <span><span class="ct">I understand</span><span class="ch" id="ack-hint"></span></span></label>
    </div>
    <div class="modal-detail est" id="clean-est" hidden></div>`;

  let dry = null;       // the dry-run result for the current mode
  let dryMode = null;
  let mode = 'trash';

  const result = await modal({
    title: `Clean ${plural(ids.length, 'item')}?`,
    iconSvg: GLYPH.clean,
    message: `<b>${fmtSize(total)}</b> selected in ${catText}. Transom will first check exactly what can be removed.`,
    body,
    confirmText: 'Check & continue',
    onMount(ctx) {
      const { card, confirmBtn } = ctx;
      const ack = card.querySelector('#clean-ack');
      const wrap = card.querySelector('#ack-wrap');
      const est = card.querySelector('#clean-est');
      const icon = card.querySelector('.modal-icon');
      const needAck = () => caution > 0 || inTrash > 0 || mode === 'delete';
      ctx.update = () => {
        const need = needAck();
        wrap.hidden = !need;
        if (!need) ack.checked = false;
        const reasons = [];
        if (mode === 'delete') reasons.push('deleted items can\'t be recovered');
        if (caution) reasons.push('Caution items are my own files');
        if (inTrash) reasons.push('Trash contents are deleted permanently');
        const hint = reasons.join('; ');
        card.querySelector('#ack-hint').textContent = hint ? hint[0].toUpperCase() + hint.slice(1) + '.' : '';
        const del = mode === 'delete';
        icon.classList.toggle('danger', del);
        icon.innerHTML = del ? GLYPH.warn : GLYPH.clean;
        if (dry && dryMode === mode) {
          confirmBtn.textContent = del ? `Delete ${plural(dry.removed, 'item')} permanently` : `Move ${plural(dry.removed, 'item')} to Trash`;
          confirmBtn.className = del ? 'danger' : 'primary';
          est.hidden = false;
        } else {
          confirmBtn.textContent = 'Check & continue';
          confirmBtn.className = 'primary';
          est.hidden = true;
        }
        confirmBtn.disabled = need && !ack.checked;
      };
      card.querySelectorAll('input[name=clean-mode]').forEach(r => r.addEventListener('change', () => {
        mode = r.value;
        ctx.update();
      }));
      ack.addEventListener('change', ctx.update);
      ctx.update();
    },
    async onConfirm(ctx) {
      const { card, confirmBtn } = ctx;
      if (dry && dryMode === mode) return { mode, ids };   // step 2: go
      // Step 1: dry run for the exact estimate, then ask again.
      const label = confirmBtn.textContent;
      confirmBtn.disabled = true;
      confirmBtn.innerHTML = '<span class="spin sm" aria-hidden="true"></span>Checking…';
      try {
        dry = await api('/api/clean', { items: ids, mode, dryRun: true });
        dryMode = mode;
      } catch (e) {
        confirmBtn.textContent = label;
        confirmBtn.disabled = false;
        toast(`Dry run failed: ${e.message}`, { bad: true });
        return false;
      }
      const failed = dry.failed || [];
      const est = card.querySelector('#clean-est');
      est.innerHTML = `Cleaning will free <b>${fmtSize(dry.freed)}</b> from ${plural(dry.removed, 'item')}` +
        (failed.length ? `<br>${plural(failed.length, 'item')} would be skipped:<ul>${failed.slice(0, 20).map(f =>
          `<li>${esc(f.error || 'failed')} — <span class="mono">${esc(f.path || f.id)}</span></li>`).join('')}</ul>` : '.');
      ctx.update();
      confirmBtn.focus();
      return false;
    },
  });
  if (!result) return;
  await runClean(result.ids, result.mode);
}
async function runClean(ids, mode) {
  busy(mode === 'delete' ? `Deleting ${plural(ids.length, 'item')}…` : `Moving ${plural(ids.length, 'item')} to the Trash…`);
  let r;
  try {
    r = await api('/api/clean', { items: ids, mode, dryRun: false });
  } catch (e) {
    busy(false);
    toast(`Clean failed: ${e.message}`, { bad: true });
    return;
  }
  busy(false);
  const failed = r.failed || [];
  const failedIds = new Set(failed.map(f => f.id));
  const done = new Set(ids.filter(id => !failedIds.has(id)));
  if (scan) {
    for (const c of scan.categories) c.items = c.items.filter(it => !done.has(it.id));
    for (const id of done) { itemIndex.delete(id); selected.delete(id); }
    recomputeTotals();
  }
  const verb = mode === 'delete' ? 'deleted' : 'moved to the Trash';
  toast(`Freed ${fmtSize(r.freed)} — ${plural(r.removed, 'item')} ${verb}`, { failed });
  renderAll();
  loadDisk();
  if (currentPage === 'history') loadHistory();
}

/* ── reveal ────────────────────────────────────────────────── */
async function reveal(id) {
  const e = itemIndex.get(id);
  if (!e || !e.item.path) return;
  try { await api('/api/reveal', { path: e.item.path }); }
  catch (err) { toast(`Couldn't reveal: ${err.message}`, { bad: true }); }
}

/* ── history ───────────────────────────────────────────────── */
async function loadHistory() {
  const body = $('history-body');
  let rows;
  try { [rows] = await Promise.all([api('/api/history', {}), categoriesReady]); rows = rows || []; }
  catch (e) { body.innerHTML = `<div class="empty">Couldn't load history: ${esc(e.message)}</div>`; return; }
  const freed = rows.reduce((s, h) => s + (h.freed || 0), 0);
  $('history-hint').textContent = rows.length ? `${fmtSize(freed)} freed in ${plural(rows.length, 'cleanup')}` : '';
  if (!rows.length) {
    body.innerHTML = '<div class="empty">Nothing cleaned yet. Run a scan from the Overview to get started.</div>';
    return;
  }
  const catName = id => (catMeta(id) || {}).name || id;
  body.innerHTML = `<table>
    <thead><tr><th>When</th><th class="num">Freed</th><th class="num">Items</th><th>Mode</th><th>Categories</th></tr></thead>
    <tbody>${rows.map(h => `<tr>
      <td class="nw" title="${esc(absTime(h.at))}">${esc(relTime(h.at))}<span class="abs">${esc(absTime(h.at))}</span></td>
      <td class="num nw"><b>${fmtSize(h.freed)}</b></td>
      <td class="num">${fmtInt(h.removed)}</td>
      <td>${h.mode === 'delete' ? '<span class="badge mode-delete">Deleted</span>' : '<span class="badge mode-trash">Trash</span>'}</td>
      <td>${(h.categories || []).map(c => `<span class="chip">${esc(catName(c))}</span>`).join('')}</td>
    </tr>`).join('')}</tbody></table>`;
}

/* ── settings ──────────────────────────────────────────────── */
// projectRoots is a JSON-encoded array of absolute paths (or "~", "~/...")
// stored as a single server preference; empty/missing means the default
// (["~"]). settingsRoots holds the working copy while the page is open.
function parseRootsList(raw) {
  if (raw) {
    try {
      const arr = JSON.parse(raw);
      if (Array.isArray(arr) && arr.length) return arr.map(String);
    } catch { /* fall through to the default */ }
  }
  return ['~'];
}
function isValidRootInput(v) {
  v = String(v || '').trim();
  return v === '~' || v.startsWith('~/') || v.startsWith('/');
}
let settingsRoots = null; // lazily loaded from TransomPrefs, kept across page switches
async function renderSettings() {
  const root = $('page-settings');
  root.innerHTML = '<section class="card"><div class="card-body"><div class="empty">Loading…</div></div></section>';
  await TransomPrefs.ready;
  if (currentPage !== 'settings') return;
  if (settingsRoots == null) settingsRoots = parseRootsList(TransomPrefs.get('projectRoots'));
  const staleDays = parseInt(TransomPrefs.get('staleDays', ''), 10) || 60;
  const largeMinMB = parseInt(TransomPrefs.get('largeMinMB', ''), 10) || 500;
  root.innerHTML = `
    <section class="card" id="settings-roots">
      <div class="card-head">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M3 7.5A2.5 2.5 0 0 1 5.5 5H10l2 2.5h6.5A2.5 2.5 0 0 1 21 10v7.5a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 17.5z"/><line x1="3" y1="11" x2="21" y2="11"/></svg>
        <h2>Project folders</h2>
      </div>
      <div class="card-body">
        <p class="muted">Where Transom looks for old node_modules, vendor, .venv and target folders.</p>
        <div class="roots-list" id="roots-list"></div>
        <div class="row roots-add">
          <input type="text" id="root-input" class="field" placeholder="~/Projects or /absolute/path" aria-label="Add a project folder">
          <button type="button" class="sm" id="root-add">Add</button>
        </div>
        <div class="warn" id="roots-error" hidden></div>
      </div>
    </section>
    <section class="card">
      <div class="card-head">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="9"/><polyline points="12 7 12 12 15.5 14"/></svg>
        <h2>Scan options</h2>
      </div>
      <div class="card-body">
        <div class="settings-grid">
          <label class="field-label" for="opt-staledays">Stale after (days)
            <span class="field-hint">Projects untouched this long count as stale.</span>
            <input type="number" min="1" step="1" id="opt-staledays" class="field" value="${staleDays}">
          </label>
          <label class="field-label" for="opt-largemin">Large file threshold (MB)
            <span class="field-hint">Files at or above this size show up as Large.</span>
            <input type="number" min="1" step="1" id="opt-largemin" class="field" value="${largeMinMB}">
          </label>
        </div>
      </div>
    </section>`;
  renderRootsList();
  $('root-add').addEventListener('click', addRootFromInput);
  $('root-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { e.preventDefault(); addRootFromInput(); }
  });
  $('opt-staledays').addEventListener('change', (e) => {
    const v = Math.max(1, parseInt(e.target.value, 10) || 60);
    e.target.value = v;
    TransomPrefs.set('staleDays', v);
  });
  $('opt-largemin').addEventListener('change', (e) => {
    const v = Math.max(1, parseInt(e.target.value, 10) || 500);
    e.target.value = v;
    TransomPrefs.set('largeMinMB', v);
  });
}
function renderRootsList() {
  const el = $('roots-list');
  if (!el) return;
  el.innerHTML = settingsRoots.map((r, i) => `
    <div class="root-row">
      <span class="mono root-path">${esc(r)}</span>
      <button type="button" class="sm ghost" data-remove-root="${i}"${settingsRoots.length <= 1 ? ' disabled title="At least one folder is required"' : ''}>Remove</button>
    </div>`).join('');
  el.querySelectorAll('[data-remove-root]').forEach(b =>
    b.addEventListener('click', () => removeRoot(parseInt(b.dataset.removeRoot, 10))));
}
async function addRootFromInput() {
  const input = $('root-input');
  const err = $('roots-error');
  const v = input.value.trim();
  err.hidden = true;
  if (!v) return;
  if (!isValidRootInput(v)) {
    err.textContent = 'Enter an absolute path (starting with /) or a ~/ path.';
    err.hidden = false;
    return;
  }
  if (settingsRoots.includes(v)) { input.value = ''; return; }
  const prev = settingsRoots.slice();
  settingsRoots.push(v);
  if (await saveRoots()) input.value = '';
  else settingsRoots = prev;
}
async function removeRoot(i) {
  const prev = settingsRoots.slice();
  settingsRoots.splice(i, 1);
  if (!settingsRoots.length) settingsRoots = ['~'];
  if (!(await saveRoots())) settingsRoots = prev;
}
async function saveRoots() {
  const err = $('roots-error');
  try {
    const value = JSON.stringify(settingsRoots);
    await api('/api/prefs/set', { key: 'projectRoots', value });
    TransomPrefs.set('projectRoots', value); // keeps the local cache in sync
    err.hidden = true;
    renderRootsList();
    return true;
  } catch (e) {
    err.textContent = e.message || 'Could not save project folders.';
    err.hidden = false;
    renderRootsList();
    return false;
  }
}

/* ── wiring ────────────────────────────────────────────────── */
function onAction(act, btn) {
  switch (act) {
    case 'scan': startScan([]); break;
    case 'cancel-scan': cancelScan(btn); break;
    case 'clean': openClean(); break;
    case 'scan-group': {
      const g = currentPage;
      const ids = categories.filter(c => c.group === g).map(c => c.id);
      if (ids.length) startScan(ids);
      else toast('No categories in this group', { bad: true });
      break;
    }
    case 'sel-safe': selectWhere(e => e.cat.group === currentPage, false);
      selectWhere(e => e.cat.group === currentPage && e.item.risk === 'safe', true); break;
    case 'sel-all': selectWhere(e => e.cat.group === currentPage, true); break;
    case 'sel-none': selectWhere(e => e.cat.group === currentPage, false); break;
  }
}
function init() {
  applyTheme(themePref, false);
  document.querySelectorAll('[data-theme-set]').forEach(b =>
    b.addEventListener('click', () => applyTheme(b.dataset.themeSet, true)));
  TransomPrefs.ready.then(() => {
    const saved = TransomPrefs.get('theme');
    if (saved && saved !== themePref) applyTheme(saved, false);
  });

  document.addEventListener('click', (e) => {
    const t = e.target.closest('[data-act], [data-toggle], [data-reveal], [data-showall]');
    if (!t || t.disabled) return;
    if (t.dataset.act) onAction(t.dataset.act, t);
    else if (t.dataset.toggle) toggleCat(t.dataset.toggle);
    else if (t.dataset.reveal) reveal(t.dataset.reveal);
    else if (t.dataset.showall) {
      const id = t.dataset.showall;
      if (showAll.has(id)) showAll.delete(id); else showAll.add(id);
      rerenderCatBody(id);
    }
  });
  document.addEventListener('change', (e) => {
    const cb = e.target;
    if (cb.dataset.item) {
      if (cb.checked) selected.add(cb.dataset.item); else selected.delete(cb.dataset.item);
      syncSelectionUI();
    } else if (cb.dataset.catCheck) {
      const c = scan && scan.categories.find(x => x.id === cb.dataset.catCheck);
      if (!c) return;
      for (const it of c.items) { if (cb.checked) selected.add(it.id); else selected.delete(it.id); }
      syncSelectionUI();
    }
  });
  $('sel-clear').addEventListener('click', () => { selected.clear(); syncSelectionUI(); });
  $('sel-clean').addEventListener('click', openClean);
  window.addEventListener('hashchange', () => showPage(location.hash.slice(1)));

  renderDisk();
  loadDisk();
  categoriesReady = api('/api/categories', {}).then(list => {
    categories = list || [];
    renderAll();
  }).catch(e => toast(`Couldn't load categories: ${e.message}`, { bad: true }));
  categoriesReady.then(resumeLastScan);
  showPage(location.hash.slice(1));
  renderAll();
  // Keep relative times ("scanned 2 minutes ago") fresh.
  setInterval(() => { if (currentPage === 'overview' && scan && !job) renderScanCard(); }, 60000);
  // Keep-alive: in detached mode the server exits after an hour without any
  // request (see detachedIdleExit in cmd/ui.go). A periodic /api/disk call
  // keeps a long-open tab from going stale, and doubles as a disk refresh.
  setInterval(loadDisk, 5 * 60 * 1000);
}
document.addEventListener('DOMContentLoaded', init);
