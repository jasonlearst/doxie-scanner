import { api, ApiError, pageImage, pdfURL } from './api.js';

/* ---------- DOM helpers ---------- */

function h(tag, props, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'text') el.textContent = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, '');
    else el.setAttribute(k, v);
  }
  for (const c of children.flat(Infinity)) {
    if (c == null || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

function icon(name, cls = '') {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('class', `icon ${cls}`.trim());
  svg.setAttribute('aria-hidden', 'true');
  const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
  use.setAttribute('href', `#i-${name}`);
  svg.append(use);
  return svg;
}

let toastTimer;
function toast(message) {
  const el = document.getElementById('toast');
  el.textContent = message;
  el.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.remove('show'), 2400);
}

function failed(err) {
  toast(err instanceof ApiError ? err.message : 'Something went wrong.');
}

/* ---------- Labels ---------- */

const SIDES = { auto: 'Both sides, skip blanks', double: 'Both sides', single: 'One side' };
const SIDES_SHORT = { auto: 'Auto', double: 'Both', single: 'One' };
const COLORS = { auto: 'Auto color', color: 'Color', gray: 'Gray', bw: 'Black & white' };
const COLORS_SHORT = { auto: 'Auto', color: 'Color', gray: 'Gray', bw: 'B&W' };

function summary(s) {
  return `${s.resolution} dpi · ${SIDES[s.sides]} · ${COLORS[s.color]}`;
}

function plural(n, one, many = `${one}s`) {
  return `${n} ${n === 1 ? one : many}`;
}

function dayLabel(iso) {
  const d = new Date(iso);
  const today = new Date();
  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  if (d.toDateString() === today.toDateString()) return 'Today';
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday';
  return d.toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' });
}

function timeLabel(iso) {
  return new Date(iso).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
}

/* ---------- State ---------- */

const SETTINGS_KEY = 'scanSettings';

const state = {
  route: { name: 'home' },
  status: null,
  statusError: null,
  options: null,
  scanSettings: null,
  scans: null,
  scan: null,
  scanError: null,
  paperless: null,
  splitDraft: null,
  hiddenOpen: false,
  starting: false,
  notice: null,
  seenPages: new Set(),
};

function loadScanSettings(defaults) {
  try {
    const saved = JSON.parse(localStorage.getItem(SETTINGS_KEY) || 'null');
    if (saved) return { ...defaults, ...saved };
  } catch { /* private mode or corrupt value: fall back to defaults */ }
  return { ...defaults };
}

function saveScanSettings(s) {
  state.scanSettings = s;
  try { localStorage.setItem(SETTINGS_KEY, JSON.stringify(s)); } catch { /* not persisted */ }
}

/* ---------- Scanner status ---------- */

function scannerState() {
  const s = state.status;
  if (state.statusError) return { dot: 'red', title: "Can't reach the scanner app", text: state.statusError.message };
  if (!s || s.checking) return { spinner: true, title: 'Checking the scanner…', text: '' };
  if (s.busy) return { spinner: true, title: 'Scanning…', text: 'Pages show up as each sheet comes out.', busy: true };
  if (!s.connected) {
    const code = s.error && s.error.code;
    if (code === 'device_not_found') return { dot: 'orange', title: 'Off', text: 'Press the power button on the scanner to turn it on.' };
    if (code === 'claim_interface_failed') return { dot: 'red', title: 'Scanner is in use', text: 'Another app has the scanner. Close it, then wait a moment.' };
    return { dot: 'red', title: 'Scanner problem', text: (s.error && s.error.message) || 'The scanner is not responding.' };
  }
  return s.paper
    ? { dot: 'green', title: 'Ready', text: 'Paper is loaded.', ready: true }
    : { dot: 'green', title: 'Ready', text: 'Put paper in the scanner, then tap Scan.', ready: true };
}

function statusCell(st) {
  return h('div', { class: 'cell status-cell', role: 'status', 'aria-live': 'polite' },
    st.spinner ? h('span', { class: 'spinner', 'aria-hidden': 'true' }) : h('span', { class: `dot ${st.dot || ''}`, 'aria-hidden': 'true' }),
    h('div', { class: 'main' },
      h('div', { class: 'title' }, st.title),
      st.text && h('div', { class: 'subtitle' }, st.text)));
}

/* ---------- Scan list helpers ---------- */

function needsReview(s) {
  if (s.status === 'running') return true;
  if (s.sent.failed > 0) return true;
  return s.sent.total === 0 && (s.pageCount > 0 || s.status === 'failed');
}

// Scanner errors read "what happened: what to do"; lists show only the first part.
function shortError(err) {
  return (err || 'scanner problem').split(':')[0];
}

// "paper jam: open the scanner" → "Paper jam. Open the scanner."
function sentence(err) {
  if (!err) return '';
  const [what, ...todo] = err.split(': ');
  const cap = (t) => t.charAt(0).toUpperCase() + t.slice(1);
  return [what, todo.join(': ')].filter(Boolean).map((t) => cap(t.trim()).replace(/[^.!?]$/, '$&.')).join(' ');
}

function scanStatus(s) {
  if (s.status === 'running') return { text: 'Scanning…', tone: 'blue' };
  if (s.status === 'failed' && s.pageCount === 0) return { text: `Stopped: ${shortError(s.error)}`, tone: 'red' };
  if (s.sent.failed > 0) return { text: 'Couldn’t send', tone: 'red' };
  if (s.sent.total > 0 && s.sent.done === s.sent.total) return { text: 'Filed in Paperless', tone: 'green' };
  if (s.sent.sending > 0) return { text: 'Sending to Paperless…', tone: '' };
  if (s.sent.total > 0) return { text: 'Sent, waiting for Paperless', tone: '' };
  if (s.status === 'failed') return { text: 'Stopped early · To review', tone: 'orange' };
  return { text: 'To review', tone: 'orange' };
}

function scanRow(s, selected) {
  const status = scanStatus(s);
  const cover = s.coverPage
    ? h('img', { class: 'thumb', alt: '', loading: 'lazy', src: `/api/scans/${s.id}/pages/${s.coverPage}/image?size=thumb&v=${s.coverKey}` })
    : h('span', { class: `thumb ${status.tone ? `text-${status.tone}` : ''}`, 'aria-hidden': 'true' }, s.status === 'running' ? h('span', { class: 'spinner' }) : icon(s.status === 'failed' ? 'alert' : 'scan'));
  return h('a', { class: 'cell indent', href: `#/scans/${s.id}`, 'aria-current': s.id === selected ? 'page' : null },
    cover,
    h('div', { class: 'main' },
      h('div', { class: 'title' }, s.name),
      h('div', { class: 'subtitle' }, s.pageCount ? `${timeLabel(s.createdAt)} · ${plural(s.pageCount, 'page')}` : timeLabel(s.createdAt)),
      h('div', { class: `subtitle ${status.tone ? `text-${status.tone}` : ''}` }, status.text)),
    icon('chevron-right', 'chev'));
}

/* ---------- Sheets ---------- */

let openSheets = 0;

function openSheet({ title, lede, body, endAction, onClose }) {
  const layer = document.getElementById('layer');
  const previousFocus = document.activeElement;
  const scrim = h('div', { class: 'scrim' });
  const closeBtn = h('button', { class: 'btn plain', type: 'button', onclick: () => close() }, endAction ? 'Cancel' : 'Done');
  const head = h('div', { class: 'sheet-head' },
    endAction ? closeBtn : h('span'),
    h('h2', { id: `sheet-title-${++openSheets}` }, title),
    h('div', { class: 'end' }, endAction || closeBtn));
  const panel = h('div', { class: 'sheet-panel', role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': `sheet-title-${openSheets}` },
    h('div', { class: 'grabber', 'aria-hidden': 'true' }), head, lede && h('p', { class: 'sheet-lede' }, lede), body);
  let closed = false;
  function close() {
    if (closed) return;
    closed = true;
    document.removeEventListener('keydown', onKey);
    panel.classList.add('closing');
    scrim.style.opacity = '0';
    scrim.style.transition = 'opacity .2s';
    setTimeout(() => { panel.remove(); scrim.remove(); }, 220);
    if (previousFocus && previousFocus.focus) previousFocus.focus();
    if (onClose) onClose();
  }
  function onKey(e) { if (e.key === 'Escape') close(); }
  scrim.addEventListener('click', close);
  document.addEventListener('keydown', onKey);
  layer.append(scrim, panel);
  const first = panel.querySelector('input, select, .segmented input:checked, button:not(.plain)');
  (first || panel).focus({ preventScroll: true });
  return { close, panel };
}

function confirmSheet({ title, text, confirm, destructive }) {
  return new Promise((resolve) => {
    let decided = false;
    const yes = h('button', { class: `btn big ${destructive ? 'destructive' : 'primary'}`, type: 'button' }, confirm);
    const sheet = openSheet({
      title,
      body: h('div', {}, h('p', { class: 'sheet-lede' }, text), yes),
      onClose: () => { if (!decided) resolve(false); },
    });
    yes.addEventListener('click', () => { decided = true; resolve(true); sheet.close(); });
  });
}

function segmented(name, options, value, onChange) {
  return h('div', { class: 'segmented', role: 'radiogroup' },
    options.map(([v, label]) => {
      const id = `${name}-${v}`;
      const input = h('input', { type: 'radio', name, id, value: v, checked: String(v) === String(value) });
      input.addEventListener('change', () => onChange(v));
      return [input, h('label', { for: id }, label)];
    }));
}

function settingsFields(s, onChange, idPrefix) {
  const set = (k) => (v) => onChange({ ...s, [k]: k === 'resolution' ? Number(v) : v });
  return h('div', { class: 'list' },
    h('div', { class: 'field' },
      h('div', { class: 'field-label' }, h('span', {}, 'Resolution'), h('span', { class: 'num' }, `${s.resolution} dpi`)),
      segmented(`${idPrefix}-res`, (state.options ? state.options.resolutions : [200, 300, 600]).map((r) => [r, String(r)]), s.resolution, set('resolution')),
      h('div', { class: 'field-hint' }, '300 suits most paper. 600 is for photos and fine print, and is slower.')),
    h('div', { class: 'field' },
      h('div', { class: 'field-label' }, h('span', {}, 'Sides'), h('span', {}, SIDES[s.sides])),
      segmented(`${idPrefix}-sides`, ['auto', 'double', 'single'].map((v) => [v, SIDES_SHORT[v]]), s.sides, set('sides')),
      h('div', { class: 'field-hint' }, 'Auto scans both sides and hides backs that look blank. Hidden backs are kept, so you can bring them back.')),
    h('div', { class: 'field' },
      h('div', { class: 'field-label' }, h('span', {}, 'Color'), h('span', {}, COLORS[s.color])),
      segmented(`${idPrefix}-color`, ['auto', 'color', 'gray', 'bw'].map((v) => [v, COLORS_SHORT[v]]), s.color, set('color')),
      h('div', { class: 'field-hint' }, 'Auto picks color, gray or black & white for each page. You can change any page later.')));
}

function openScanSettings() {
  const body = h('div', {});
  const draw = (s) => body.replaceChildren(settingsFields(s, (next) => { saveScanSettings(next); draw(next); render(); }, 'scan'));
  draw(state.scanSettings);
  openSheet({ title: 'Scan settings', lede: 'For scans you start here. The scanner’s own button has its settings under Settings.', body });
}

/* ---------- Views ---------- */

function viewHome() {
  const st = scannerState();
  const review = (state.scans || []).filter(needsReview);
  return h('div', { class: 'page' },
    h('h1', { class: 'large-title' }, 'Scanner'),
    h('section', { class: 'scan-area' },
      h('div', { class: 'list' }, statusCell(st)),
      h('button', {
        class: 'btn primary big', type: 'button', onclick: startScan,
        disabled: !st.ready || state.starting,
      }, icon('scan'), state.starting ? 'Starting…' : 'Scan'),
      state.notice && h('div', { class: `notice ${state.notice.tone}`, role: 'alert', style: 'margin-top:12px' },
        icon(state.notice.icon || 'alert'), h('p', {}, h('strong', {}, state.notice.title), state.notice.text))),
    h('section', { class: 'group' },
      h('div', { class: 'list' },
        h('button', { class: 'cell', type: 'button', onclick: openScanSettings },
          h('div', { class: 'main' }, h('div', { class: 'title' }, 'Scan settings'), h('div', { class: 'subtitle' }, summary(state.scanSettings))),
          icon('chevron-right', 'chev'))),
      h('p', { class: 'group-footer' }, 'You can also press the button on the scanner.')),
    h('section', { class: 'group' },
      h('h2', { class: 'group-header' }, 'To review'),
      h('div', { class: 'list' },
        state.scans === null ? h('div', { class: 'empty' }, 'Loading…')
          : review.length ? review.map(scanRow)
            : h('div', { class: 'empty' }, h('strong', {}, 'Nothing to review'), 'Scans wait here until you send them to Paperless.'))));
}

// Tablets and laptops show the scan list beside the open scan, like Mail.
const sideBySide = matchMedia('(min-width: 900px)');

function viewScans(selected) {
  const scans = state.scans;
  const page = h('div', { class: 'page' }, h('h1', { class: 'large-title' }, 'Scans'));
  if (scans === null) return page.appendChild(h('div', { class: 'empty' }, 'Loading…')) && page;
  if (!scans.length) {
    page.append(h('div', { class: 'list' }, h('div', { class: 'empty' }, h('strong', {}, 'No scans yet'), 'Put paper in the scanner and tap Scan, or press the scanner’s button.')));
    return page;
  }
  const groups = new Map();
  for (const s of scans) {
    const label = dayLabel(s.createdAt);
    if (!groups.has(label)) groups.set(label, []);
    groups.get(label).push(s);
  }
  for (const [label, list] of groups) {
    page.append(h('section', { class: 'group' }, h('h2', { class: 'group-header' }, label), h('div', { class: 'list' }, list.map((s) => scanRow(s, selected)))));
  }
  return page;
}

function documentsOf(scan) {
  const docs = [];
  for (const p of scan.pages) {
    if (p.hidden) continue;
    if (!docs.length || (scan.splits || []).includes(p.id)) docs.push([]);
    docs[docs.length - 1].push(p);
  }
  return docs;
}

function tile(scan, page, label) {
  const isNew = !state.seenPages.has(`${scan.id}/${page.id}`);
  state.seenPages.add(`${scan.id}/${page.id}`);
  return h('button', {
    class: `tile${page.hidden ? ' hidden-page' : ''}${isNew && scan.status === 'running' ? ' arrived' : ''}`,
    type: 'button', 'aria-label': `${label}${page.hidden ? ', hidden' : ''}. Open`,
    onclick: () => openViewer(page.id),
  }, h('span', { class: 'sheet' }, h('img', { alt: '', loading: 'lazy', src: pageImage(scan.id, page, 'thumb') })), label);
}

function sentRow(scan, sent) {
  const done = sent.status === 'done';
  const bad = sent.status === 'failed';
  const waiting = { sending: 'Sending to Paperless…', processing: 'Paperless is reading it…' }[sent.status] || 'Waiting for Paperless…';
  const text = done ? 'Filed in Paperless' : bad ? (sent.error || 'Paperless couldn’t take it') : waiting;
  return h('div', { class: 'cell' },
    h('span', { class: `sent-icon ${done ? 'done' : bad ? 'failed' : ''}` }, done ? icon('check', 'sm') : bad ? icon('alert', 'sm') : h('span', { class: 'spinner' })),
    h('div', { class: 'main' },
      h('div', { class: 'title' }, sent.title),
      h('div', { class: `subtitle ${bad ? 'text-red' : ''}` }, `${plural(sent.pages.length, 'page')} · ${text}`)),
    done && state.paperless && state.paperless.url && sent.documentId
      ? h('a', { class: 'btn plain', href: `${state.paperless.url}/documents/${sent.documentId}/details`, target: '_blank', rel: 'noopener' }, 'Open', icon('external', 'sm'))
      : null);
}

function viewMasterDetail() {
  const r = state.route;
  const detail = r.name === 'scan' ? viewScan() : h('div', { class: 'detail-empty' },
    h('div', {}, icon('layers'), h('strong', {}, 'Choose a scan'), h('p', {}, 'Pick one from the list to check its pages and send it to Paperless.')));
  return h('div', { class: 'master-detail' }, h('nav', { class: 'master', 'aria-label': 'Scans' }, viewScans(r.id)), h('div', { class: 'detail' }, detail));
}

function viewScan() {
  const scan = state.scan;
  const back = sideBySide.matches ? h('span') : h('button', { class: 'back', type: 'button', onclick: goBack }, icon('chevron-left'), 'Back');
  if (!scan) {
    return h('div', { class: 'page wide' }, h('div', { class: 'navbar' }, back),
      state.scanError ? h('div', { class: 'list' }, h('div', { class: 'empty' }, h('strong', {}, 'Scan not found'), 'It may have been deleted.'))
        : h('div', { class: 'empty' }, 'Loading…'));
  }
  const running = scan.status === 'running';
  const visible = scan.pages.filter((p) => !p.hidden);
  const hidden = scan.pages.filter((p) => p.hidden);
  const docs = documentsOf(scan);

  const page = h('div', { class: 'page wide' },
    h('div', { class: 'navbar' }, back, h('h1', { class: 'navbar-title' }, scan.name),
      h('div', { class: 'end' }, h('button', { class: 'btn icon-only', type: 'button', 'aria-label': 'More', onclick: () => openScanMenu(scan) }, icon('more')))),
    h('div', { class: 'navbar-title-row' },
      h('div', {},
        h('h2', {}, scan.name),
        h('p', { class: 'num' }, `${dayLabel(scan.createdAt)}, ${timeLabel(scan.createdAt)} · ${plural(visible.length, 'page')} · ${scan.settings.resolution} dpi`))));

  if (running) {
    page.append(h('div', { class: 'notice blue', role: 'status' }, h('span', { class: 'spinner' }),
      h('p', {}, h('strong', {}, `Scanning… ${plural(scan.pagesScanned ?? visible.length, 'page')} so far`), 'Keep paper in the scanner, or wait for it to finish.')));
  } else if (scan.status === 'failed') {
    page.append(h('div', { class: 'notice red', role: 'alert' }, icon('alert'),
      h('p', {}, h('strong', {}, 'Scanning stopped'), `${sentence(scan.error) || 'The scanner reported a problem.'}${scan.pages.length ? ' The pages scanned before it stopped are kept.' : ''}`)));
    if (!scan.pages.length) {
      page.append(h('div', { class: 'stack' },
        h('button', { class: 'btn primary big', type: 'button', onclick: () => scanAgain(scan) }, icon('scan'), 'Scan again'),
        h('button', { class: 'btn plain destructive', type: 'button', onclick: () => deleteScan(scan) }, 'Delete this scan')));
      return page;
    }
  }

  if (scan.sent && scan.sent.length) {
    const removeAt = scan.removeAt && new Date(scan.removeAt);
    page.append(h('section', { class: 'group' }, h('h2', { class: 'group-header' }, 'Sent to Paperless'),
      h('div', { class: 'list' }, scan.sent.map((s) => sentRow(scan, s))),
      removeAt && h('p', { class: 'group-footer' }, `Every page is filed, so this scan will be deleted from the scanner on ${removeAt.toLocaleDateString(undefined, { month: 'long', day: 'numeric' })}. Change this under Settings.`)));
  }

  if (state.splitDraft) {
    page.append(splitEditor(scan, visible));
  } else if (!visible.length && !running) {
    page.append(h('div', { class: 'list' }, h('div', { class: 'empty' }, h('strong', {}, 'No pages'), hidden.length ? 'Every page is hidden. Show the hidden pages below to bring one back.' : 'Nothing was scanned.')));
  } else {
    let n = 0;
    docs.forEach((pages, i) => {
      page.append(h('section', { class: 'doc' },
        docs.length > 1 && h('div', { class: 'doc-header' }, h('h3', {}, `Document ${i + 1}`),
          h('span', { class: 'num' }, plural(pages.length, 'page'), ' · ',
            h('a', { href: pdfURL(scan.id, pages.map((p) => p.id)), download: '', 'aria-label': `Download document ${i + 1} as PDF` }, 'PDF'))),
        h('div', { class: 'pages' }, pages.map((p) => tile(scan, p, `Page ${++n}`)))));
    });
    if (running) page.append(h('div', { class: 'pages', 'aria-hidden': 'true' }, h('div', { class: 'tile placeholder' }, h('span', { class: 'sheet' }), `Page ${n + 1}`)));
  }

  if (hidden.length && !state.splitDraft) {
    const toggle = h('button', { class: 'cell', type: 'button', 'aria-expanded': String(state.hiddenOpen), onclick: () => { state.hiddenOpen = !state.hiddenOpen; render(); } },
      icon('eye-off'), h('div', { class: 'main' },
        h('div', { class: 'title' }, `${plural(hidden.length, 'hidden page')}`),
        h('div', { class: 'subtitle' }, 'Backs that looked blank. They won’t be sent unless you show them.')),
      h('span', { class: 'value action' }, state.hiddenOpen ? 'Hide' : 'Show'));
    page.append(h('section', { class: 'group' }, h('div', { class: 'list' }, toggle),
      state.hiddenOpen && h('div', { class: 'pages', style: 'margin-top:16px' }, hidden.map((p) => tile(scan, p, 'Hidden')))));
  }

  if (running || scan.pages.length) page.append(actionBar(scan, visible, running));
  return page;
}

function actionBar(scan, visible, running) {
  if (state.splitDraft) {
    return h('div', { class: 'actionbar' },
      h('button', { class: 'btn', type: 'button', onclick: () => { state.splitDraft = null; render(); } }, 'Cancel'),
      h('button', { class: 'btn primary', type: 'button', onclick: () => saveSplits(scan) }, 'Done'));
  }
  const none = !visible.length || running;
  const pdf = none
    ? h('button', { class: 'btn', type: 'button', disabled: true }, icon('download'), 'PDF')
    : h('a', { class: 'btn', href: pdfURL(scan.id), download: '', 'aria-label': 'Download as PDF' }, icon('download'), 'PDF');
  const filed = new Set((scan.sent || []).filter((s) => s.status === 'done').flatMap((s) => s.pages));
  const allFiled = visible.length > 0 && visible.every((p) => filed.has(p.id));
  return h('div', { class: 'actionbar' },
    h('button', { class: 'btn', type: 'button', disabled: none || visible.length < 2, onclick: () => { state.splitDraft = new Set(scan.splits || []); render(); } }, icon('scissors'), 'Split'),
    pdf,
    h('button', { class: `btn send${allFiled ? '' : ' primary'}`, type: 'button', disabled: none, onclick: () => openSend(scan) }, icon('send'), allFiled ? 'Send again' : 'Send'));
}

function splitEditor(scan, visible) {
  const draft = state.splitDraft;
  const list = h('div', { class: 'split-list' });
  let doc = 1;
  visible.forEach((p, i) => {
    if (i > 0) {
      const on = draft.has(p.id);
      if (on) doc++;
      list.append(h('button', {
        class: `divider${on ? ' on' : ''}`, type: 'button', 'aria-pressed': String(on),
        onclick: () => { on ? draft.delete(p.id) : draft.add(p.id); render(); },
      }, on ? [icon('scissors', 'sm'), `Document ${doc} starts here`] : 'Tap to split here'));
    }
    list.append(h('div', { class: 'split-row' }, h('img', { alt: '', loading: 'lazy', src: pageImage(scan.id, p, 'thumb') }),
      h('div', {}, h('div', {}, `Page ${i + 1}`), h('div', { class: 'subtitle', style: 'color:var(--label-2);font-size:15px' }, `Document ${doc}`))));
  });
  return h('section', { class: 'group' },
    h('p', { class: 'group-footer', style: 'margin:0 4px 12px;font-size:15px' }, 'Tap between two pages to start a new document there. Each document goes to Paperless on its own.'),
    list);
}

async function saveSplits(scan) {
  const visibleIds = scan.pages.filter((p) => !p.hidden).map((p) => p.id);
  const keepHidden = (scan.splits || []).filter((id) => !visibleIds.includes(id));
  try {
    state.scan = await api.updateScan(scan.id, { splits: [...keepHidden, ...visibleIds.filter((id) => state.splitDraft.has(id))] });
    state.splitDraft = null;
    const n = documentsOf(state.scan).length;
    toast(n > 1 ? `Split into ${n} documents` : 'One document');
    render();
  } catch (e) { failed(e); }
}

function openScanMenu(scan) {
  const rename = h('button', { class: 'cell', type: 'button' }, icon('pencil'), h('div', { class: 'main' }, h('div', { class: 'title' }, 'Rename')));
  const pdf = h('a', { class: 'cell', href: pdfURL(scan.id), download: '' }, icon('download'), h('div', { class: 'main' }, h('div', { class: 'title' }, 'Download as PDF')));
  const del = h('button', { class: 'cell destructive', type: 'button' }, icon('trash'), h('div', { class: 'main' }, h('div', { class: 'title' }, 'Delete scan')));
  const sheet = openSheet({ title: scan.name, body: h('div', { class: 'list' }, rename, pdf, del) });
  rename.addEventListener('click', () => { sheet.close(); setTimeout(() => openRename(scan), 230); });
  pdf.addEventListener('click', () => sheet.close());
  del.addEventListener('click', () => { sheet.close(); deleteScan(scan); });
}

async function deleteScan(scan) {
  if (scan.pages.length) {
    const sent = scan.sent && scan.sent.length ? ' Anything already sent to Paperless stays there.' : '';
    if (!(await confirmSheet({ title: 'Delete this scan?', text: `Its ${plural(scan.pages.length, 'page')} will be deleted from the scanner.${sent}`, confirm: 'Delete scan', destructive: true }))) return;
  }
  try {
    await api.deleteScan(scan.id);
    toast('Scan deleted');
    location.hash = '#/scans';
  } catch (e) { failed(e); }
}

// Retries a scan that stopped before any page came out, with the same
// settings, and drops the empty one.
async function scanAgain(scan) {
  try {
    const { jobId } = await api.startScan(scan.settings);
    api.deleteScan(scan.id).catch(() => {});
    location.hash = `#/scans/${jobId}`;
  } catch (e) {
    toast(e.code === 'no_paper' ? 'Put paper in the scanner first' : e.code === 'scanner_unavailable' ? 'The scanner is off. Press its power button.' : e.message);
  }
}

function openRename(scan) {
  const input = h('input', { type: 'text', value: scan.name, 'aria-label': 'Name', enterkeyhint: 'done' });
  const save = h('button', { class: 'btn plain', type: 'button', style: 'font-weight:600' }, 'Save');
  const sheet = openSheet({ title: 'Rename', body: input, endAction: save });
  const submit = async () => {
    const name = input.value.trim();
    if (!name) return;
    try { state.scan = await api.updateScan(scan.id, { name }); sheet.close(); render(); } catch (e) { failed(e); }
  };
  save.addEventListener('click', submit);
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') submit(); });
  input.select();
}

/* ---------- Send to Paperless ---------- */

async function openSend(scan) {
  const docs = documentsOf(scan);
  if (!state.paperless) { try { state.paperless = await api.paperless(); } catch (e) { failed(e); return; } }
  if (!state.paperless.configured) {
    openSheet({
      title: 'Paperless isn’t connected',
      body: h('div', {},
        h('p', { class: 'sheet-lede' }, 'Ask whoever set up the scanner to add the Paperless address and API token. Until then you can download the scan as a PDF.'),
        h('a', { class: 'btn big', href: pdfURL(scan.id), download: '' }, icon('download'), 'Download PDF')),
    });
    return;
  }

  const body = h('div', {}, h('div', { class: 'empty' }, h('span', { class: 'spinner', style: 'display:inline-block' }), h('div', {}, 'Getting tags and correspondents from Paperless…')));
  const send = h('button', { class: 'btn plain', type: 'button', style: 'font-weight:600', disabled: true }, 'Send');
  const sheet = openSheet({
    title: docs.length > 1 ? `Send ${docs.length} documents` : 'Send to Paperless',
    lede: 'Anything you leave blank, Paperless fills in with its own rules.',
    body, endAction: send,
  });

  let meta;
  try { meta = await api.paperlessMetadata(); } catch (e) {
    body.replaceChildren(h('div', { class: 'notice red' }, icon('alert'), h('p', {}, h('strong', {}, 'Can’t reach Paperless'), e.message)),
      h('a', { class: 'btn big', href: pdfURL(scan.id), download: '' }, icon('download'), 'Download PDF instead'));
    return;
  }

  const forms = docs.map((pages, i) => documentForm(scan, pages, i, docs.length, meta));
  const label = docs.length > 1 ? `Send ${docs.length} documents` : 'Send';
  const big = h('button', { class: 'btn primary big', type: 'button', onclick: submit }, icon('send'), label);
  body.replaceChildren(...forms.map((f) => f.el), big);
  send.disabled = false;
  send.addEventListener('click', submit);

  // The scanner answers as soon as it has the documents; building the PDFs
  // and uploading them carries on there in the background.
  let sending = false;
  const pending = (on) => {
    sending = on;
    send.disabled = big.disabled = on;
    big.replaceChildren(icon('send'), on ? 'Sending…' : label);
  };
  async function submit() {
    if (sending) return;
    pending(true);
    try {
      state.scan = await api.send(scan.id, forms.map((f) => f.value()));
      state.scans = await api.scans().catch(() => state.scans);
      sheet.close();
      toast(docs.length > 1 ? `Sending ${docs.length} documents to Paperless` : 'Sending to Paperless');
      render();
    } catch (e) {
      failed(e);
      pending(false);
    }
  }
}

function documentForm(scan, pages, i, total, meta) {
  const placeholder = total > 1 ? `${scan.name} (${i + 1} of ${total})` : scan.name;
  const title = h('input', { type: 'text', placeholder, 'aria-label': 'Title' });
  const date = h('input', { type: 'date', 'aria-label': 'Date on the document' });
  const choose = (label, items) => h('select', { 'aria-label': label },
    h('option', { value: '' }, 'Let Paperless decide'), items.map((it) => h('option', { value: it.id }, it.name)));
  const correspondent = choose('Correspondent', meta.correspondents);
  const docType = choose('Document type', meta.documentTypes);
  const picked = new Set();
  const filter = h('input', { type: 'text', placeholder: 'Find a tag', 'aria-label': 'Find a tag', style: 'margin-bottom:10px' });
  const chips = h('div', { class: 'chips' });
  const drawChips = () => {
    const q = filter.value.trim().toLowerCase();
    chips.replaceChildren(...meta.tags.filter((t) => picked.has(t.id) || !q || t.name.toLowerCase().includes(q)).slice(0, 40).map((t) =>
      h('button', { class: 'chip', type: 'button', 'aria-pressed': String(picked.has(t.id)), onclick: () => { picked.has(t.id) ? picked.delete(t.id) : picked.add(t.id); drawChips(); } },
        picked.has(t.id) && icon('check', 'sm'), t.name)));
  };
  filter.addEventListener('input', drawChips);
  drawChips();
  const field = (label, control, hint) => h('div', { class: 'field' }, h('div', { class: 'field-label' }, label), control, hint && h('div', { class: 'field-hint' }, hint));

  const el = h('div', { class: 'doc-form' },
    h('div', { class: 'doc-form-head' }, h('img', { alt: '', src: pageImage(scan.id, pages[0], 'thumb') }),
      h('div', {}, h('strong', {}, total > 1 ? `Document ${i + 1}` : 'Document'), h('span', { class: 'num' }, plural(pages.length, 'page')))),
    field('Title', title),
    field('Date', date, 'Leave blank to use the date Paperless finds on the page.'),
    field('Correspondent', correspondent),
    field('Document type', docType),
    meta.tags.length ? field('Tags', h('div', {}, meta.tags.length > 12 ? filter : null, chips)) : null);

  return {
    el,
    value: () => ({
      pages: pages.map((p) => p.id),
      title: title.value.trim() || placeholder,
      created: date.value,
      correspondent: Number(correspondent.value) || 0,
      documentType: Number(docType.value) || 0,
      tags: [...picked],
    }),
  };
}

/* ---------- Page viewer ---------- */

let viewer = null;

// The viewer pages through visible pages, numbered as in the grid, or
// through the hidden ones when it was opened on a hidden page.
function viewerPages() {
  if (!state.scan) return [];
  const current = state.scan.pages.find((p) => p.id === viewer.pageId);
  const hidden = Boolean(current && current.hidden);
  return state.scan.pages.filter((p) => Boolean(p.hidden) === hidden);
}

function openViewer(pageId) {
  viewer = { pageId, mode: 'view', loading: false };
  document.addEventListener('keydown', viewerKeys);
  drawViewer();
}

function closeViewer() {
  if (viewer && viewer.cropper) viewer.cropper.destroy();
  viewer = null;
  document.removeEventListener('keydown', viewerKeys);
  const el = document.querySelector('.viewer');
  if (el) el.remove();
}

function viewerKeys(e) {
  if (!viewer || document.querySelector('.sheet-panel')) return;
  if (e.key === 'Escape') { if (viewer.mode === 'adjust') leaveAdjust(); else closeViewer(); }
  if (viewer.mode !== 'view') return;
  if (e.key === 'ArrowLeft') step(-1);
  if (e.key === 'ArrowRight') step(1);
}

function step(delta) {
  if (viewer.mode !== 'view') return;
  const pages = viewerPages();
  const i = pages.findIndex((p) => p.id === viewer.pageId);
  const next = pages[i + delta];
  if (!next) return;
  viewer.pageId = next.id;
  drawViewer();
}

function currentPage() {
  return viewerPages().find((p) => p.id === viewer.pageId);
}

function drawViewer() {
  const scan = state.scan;
  const p = scan && currentPage();
  if (!p) { closeViewer(); return; }
  const pages = viewerPages();
  const index = pages.indexOf(p);
  const existing = document.querySelector('.viewer');
  const adjusting = viewer.mode === 'adjust';

  const img = h('img', { class: 'page-img', alt: `Page ${index + 1}`, src: viewerImage(p) });
  const spin = h('span', { class: 'spinner loading', hidden: true });
  if (!img.complete) {
    img.style.opacity = '0.4';
    spin.hidden = false;
    img.addEventListener('load', () => { spin.hidden = true; img.style.opacity = '1'; }, { once: true });
  }
  const stage = h('div', { class: 'viewer-stage' },
    img, spin,
    p.hidden && h('span', { class: 'viewer-badge' }, 'Hidden: won’t be sent'),
    !adjusting && h('button', { class: 'nav-arrow prev', type: 'button', 'aria-label': 'Previous page', disabled: index === 0, onclick: () => step(-1) }, icon('chevron-left')),
    !adjusting && h('button', { class: 'nav-arrow next', type: 'button', 'aria-label': 'Next page', disabled: index === pages.length - 1, onclick: () => step(1) }, icon('chevron-right')));
  let startX = null;
  stage.addEventListener('pointerdown', (e) => { startX = e.clientX; });
  stage.addEventListener('pointerup', (e) => {
    if (startX === null) return;
    const dx = e.clientX - startX;
    startX = null;
    if (Math.abs(dx) > 50) step(dx < 0 ? 1 : -1);
  });

  const tool = (name, label, onclick, cls = '') => h('button', { type: 'button', class: cls, onclick }, icon(name), label);
  const toolbar = h('div', { class: 'toolbar' },
    tool('rotate', 'Rotate', () => editPage({ rotation: ((p.edits.rotation || 0) + 90) % 360 })),
    tool('crop', 'Crop', startCrop),
    tool('sliders', 'Adjust', openAdjust),
    tool(p.hidden ? 'eye' : 'eye-off', p.hidden ? 'Show' : 'Hide', () => toggleHidden(p, pages, index)),
    tool('more', 'More', openPageMenu));

  const top = h('div', { class: 'viewer-top' },
    h('button', { class: 'btn plain start', type: 'button', onclick: closeViewer }, 'Done'),
    h('div', { class: 'count num' }, p.hidden ? (pages.length > 1 ? `Hidden page ${index + 1} of ${pages.length}` : 'Hidden page') : `Page ${index + 1} of ${pages.length}`),
    h('span'));
  const adjust = adjusting && adjustControls(p, img);

  // Only the first draw fades in; later draws replace an opaque viewer.
  const el = h('div', { class: `viewer${adjusting ? ' adjusting' : ''}${existing ? ' steady' : ''}`, role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Page viewer' },
    adjusting ? adjust.top : top, stage, adjusting ? adjust.panel : toolbar);
  if (existing) existing.replaceWith(el); else document.getElementById('layer').append(el);
}

function viewerImage(p) {
  const untoned = viewer.mode === 'adjust' && (p.edits.color || 'color') !== 'bw';
  return pageImage(state.scan.id, p, 'preview', { untoned });
}

function preload(url) {
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = img.onerror = resolve;
    img.src = url;
  });
}

// Hiding or showing moves the page to the other set, so the viewer
// moves on to its neighbor in the set being browsed.
async function toggleHidden(p, pages, index) {
  const neighbor = pages[index + 1] || pages[index - 1];
  const updated = await editPage({ hidden: !p.hidden }, { redraw: false });
  if (!updated) return;
  toast(updated.hidden ? 'Page hidden' : 'Page will be sent');
  if (neighbor) viewer.pageId = neighbor.id;
  drawViewer();
}

async function editPage(patch, { redraw = true } = {}) {
  const scan = state.scan;
  const p = currentPage();
  try {
    const updated = await api.editPage(scan.id, p.id, patch);
    const i = scan.pages.findIndex((x) => x.id === p.id);
    scan.pages[i] = updated;
    if (redraw) drawViewer();
    render();
    return updated;
  } catch (e) { failed(e); return null; }
}

// Adjust keeps the page in view. Each release saves; while dragging, an
// SVG filter previews the tone on the untoned page.
async function openAdjust() {
  const e = currentPage().edits;
  viewer.tone = { brightness: e.brightness || 0, contrast: e.contrast || 0 };
  viewer.saving = Promise.resolve();
  viewer.mode = 'adjust';
  await preload(viewerImage(currentPage()));
  if (!viewer || viewer.mode !== 'adjust') return;
  drawViewer();
  document.querySelector('.adjust-panel input:checked').focus({ preventScroll: true });
}

async function leaveAdjust() {
  if (!viewer || viewer.mode !== 'adjust') return;
  await viewer.saving;
  viewer.mode = 'view';
  await preload(viewerImage(currentPage()));
  if (!viewer || viewer.mode !== 'view') return;
  drawViewer();
  const back = [...document.querySelectorAll('.toolbar button')].find((b) => b.textContent === 'Adjust');
  if (back) back.focus({ preventScroll: true });
}

const TONE_FILTER = '<svg class="tone-filter" aria-hidden="true" focusable="false"><filter id="tone" color-interpolation-filters="sRGB">'
  + '<feComponentTransfer><feFuncR type="linear"/><feFuncG type="linear"/><feFuncB type="linear"/></feComponentTransfer>'
  + '<feComponentTransfer><feFuncR type="linear"/><feFuncG type="linear"/><feFuncB type="linear"/></feComponentTransfer>'
  + '</filter></svg>';

// Mirrors the server: imaging.AdjustBrightness, then AdjustContrast,
// each clamped, which SVG does after every primitive.
function applyTone(svg, img, { brightness, contrast }) {
  const v = (100 + contrast) / 100;
  const k = contrast >= 0 ? v * v : v;
  const [shift, scale] = svg.querySelectorAll('feComponentTransfer');
  for (const f of shift.children) { f.setAttribute('slope', '1'); f.setAttribute('intercept', String(brightness / 100)); }
  for (const f of scale.children) { f.setAttribute('slope', String(k)); f.setAttribute('intercept', String(0.5 - 0.5 * k)); }
  img.style.filter = brightness || contrast ? 'url(#tone)' : '';
}

function adjustControls(p, img) {
  const tone = viewer.tone;
  const signed = (v) => (v > 0 ? `+${v}` : String(v));
  const holder = h('div');
  holder.innerHTML = TONE_FILTER;
  const svg = holder.firstChild;
  const isBW = () => (currentPage().edits.color || 'color') === 'bw';
  if (!isBW()) applyTone(svg, img, tone);

  const save = (patch, after) => {
    viewer.saving = viewer.saving.then(async () => {
      const updated = await editPage(patch, { redraw: false });
      if (updated && after) await after(updated);
    });
  };
  const showServerRender = async (updated) => {
    const url = viewerImage(updated);
    await preload(url);
    img.src = url;
    if (isBW()) img.style.filter = ''; else applyTone(svg, img, tone);
  };

  const reset = h('button', { class: 'btn plain start regular', type: 'button', disabled: !tone.brightness && !tone.contrast }, 'Reset');
  const sliders = {};
  const range = (label, key) => {
    const out = h('span', { class: 'num' }, signed(tone[key]));
    const input = h('input', { type: 'range', min: -100, max: 100, step: 5, value: tone[key], 'aria-label': label });
    input.addEventListener('input', () => {
      tone[key] = Number(input.value);
      out.textContent = signed(tone[key]);
      reset.disabled = !tone.brightness && !tone.contrast;
      if (!isBW()) applyTone(svg, img, tone);
    });
    input.addEventListener('change', () => save({ [key]: tone[key] }, isBW() ? showServerRender : null));
    sliders[key] = { input, out };
    return h('div', { class: 'field' }, h('div', { class: 'field-label' }, h('span', {}, label), out), input);
  };
  reset.addEventListener('click', () => {
    tone.brightness = 0;
    tone.contrast = 0;
    for (const { input, out } of Object.values(sliders)) { input.value = 0; out.textContent = '0'; }
    reset.disabled = true;
    if (!isBW()) applyTone(svg, img, tone);
    save({ brightness: 0, contrast: 0 }, isBW() ? showServerRender : null);
  });

  const color = segmented('adj-color', ['color', 'gray', 'bw'].map((v) => [v, COLORS_SHORT[v]]), p.edits.color || 'color',
    (v) => save({ color: v }, showServerRender));
  color.setAttribute('aria-label', 'Color');

  const top = h('div', { class: 'viewer-top' },
    reset,
    h('div', { class: 'count' }, 'Adjust'),
    h('button', { class: 'btn plain end', type: 'button', onclick: leaveAdjust }, 'Done'));
  const panel = h('div', { class: 'adjust-panel' },
    svg,
    h('div', { class: 'field' }, color),
    range('Brightness', 'brightness'),
    range('Contrast', 'contrast'),
    h('p', { class: 'field-hint' }, 'Changes never touch the original scan, so you can always undo them.'));
  return { top, panel };
}

function openPageMenu() {
  const scan = state.scan;
  const p = currentPage();
  const visible = scan.pages.filter((x) => !x.hidden);
  const isSplit = (scan.splits || []).includes(p.id);
  const pos = scan.pages.indexOf(p);
  const row = (name, label, sub, onclick, cls = '', disabled = false) =>
    h('button', { class: `cell ${cls}`, type: 'button', onclick, disabled }, icon(name), h('div', { class: 'main' }, h('div', { class: 'title' }, label), sub && h('div', { class: 'subtitle' }, sub)));
  const sheet = openSheet({
    title: `Page ${pos + 1}`,
    body: h('div', {},
      h('div', { class: 'list' },
        row('arrow-left', 'Move earlier', null, () => move(-1), '', pos === 0),
        row('arrow-right', 'Move later', null, () => move(1), '', pos === scan.pages.length - 1),
        !p.hidden && visible[0] !== p && row('scissors', isSplit ? 'Join with the document before' : 'Start a new document here', 'Splits this scan into separate Paperless documents.', toggleSplit)),
      h('div', { class: 'list', style: 'margin-top:16px' },
        h('a', { class: 'cell', href: pdfURL(scan.id, [p.id]), download: '' }, icon('download'),
          h('div', { class: 'main' }, h('div', { class: 'title' }, 'Download this page'), h('div', { class: 'subtitle' }, 'As a PDF, with your edits.')))),
      h('div', { class: 'list', style: 'margin-top:16px' },
        row('trash', 'Delete page', 'Removes this page for good.', del, 'destructive'))),
  });
  async function move(d) {
    const order = scan.pages.map((x) => x.id);
    const [moved] = order.splice(pos, 1);
    order.splice(pos + d, 0, moved);
    try { state.scan = await api.reorder(scan.id, order); sheet.close(); drawViewer(); render(); } catch (e) { failed(e); }
  }
  async function toggleSplit() {
    const splits = new Set(scan.splits || []);
    isSplit ? splits.delete(p.id) : splits.add(p.id);
    try { state.scan = await api.updateScan(scan.id, { splits: [...splits] }); sheet.close(); render(); toast(isSplit ? 'Joined' : 'New document starts here'); } catch (e) { failed(e); }
  }
  async function del() {
    sheet.close();
    if (!(await confirmSheet({ title: 'Delete this page?', text: 'It will be removed from this scan. This can’t be undone.', confirm: 'Delete page', destructive: true }))) return;
    try {
      const nextId = (scan.pages[pos + 1] || scan.pages[pos - 1] || {}).id;
      await api.deletePage(scan.id, p.id);
      state.scan = await api.scan(scan.id);
      if (nextId) { viewer.pageId = nextId; drawViewer(); } else closeViewer();
      render();
      toast('Page deleted');
    } catch (e) { failed(e); }
  }
}

function startCrop() {
  const scan = state.scan;
  const p = currentPage();
  const rot = p.edits.rotation || 0;
  const fullW = rot === 90 || rot === 270 ? p.heightPx : p.widthPx;
  const fullH = rot === 90 || rot === 270 ? p.widthPx : p.heightPx;
  const img = h('img', { alt: 'Page to crop', src: pageImage(scan.id, p, 'preview', { uncropped: true }) });
  const stage = h('div', { class: 'viewer-stage' }, h('div', { class: 'crop-stage' }, img));
  const done = h('button', { type: 'button' }, icon('check'), 'Done');
  const reset = h('button', { type: 'button' }, icon('x'), 'No crop');
  const auto = h('button', { type: 'button' }, icon('fit'), 'Auto');
  const cancel = h('button', { type: 'button' }, icon('chevron-left'), 'Cancel');
  const el = document.querySelector('.viewer');
  el.querySelector('.viewer-stage').replaceWith(stage);
  el.querySelector('.toolbar').replaceChildren(cancel, reset, auto, done);
  viewer.mode = 'crop';

  img.addEventListener('load', () => {
    const scale = img.naturalWidth / fullW;
    viewer.cropper = new window.Cropper(img, {
      viewMode: 1, autoCropArea: 1, background: false, zoomable: false, movable: false, rotatable: false, scalable: false,
      ready() {
        const c = p.edits.crop;
        if (c) this.cropper.setData({ x: c.x * scale, y: c.y * scale, width: c.w * scale, height: c.h * scale });
      },
    });
    viewer.cropScale = scale;
  }, { once: true });

  const leave = () => { viewer.cropper && viewer.cropper.destroy(); viewer.cropper = null; viewer.mode = 'view'; drawViewer(); };
  cancel.addEventListener('click', leave);
  const apply = async (patch) => { viewer.mode = 'view'; viewer.cropper && viewer.cropper.destroy(); viewer.cropper = null; if (!await editPage(patch)) drawViewer(); };
  // No crop shows the page exactly as scanned; Auto goes back to the sheet's edges.
  reset.addEventListener('click', () => apply({ crop: null, straighten: 0 }));
  auto.addEventListener('click', () => apply({ autoCrop: true }));
  done.addEventListener('click', async () => {
    if (!viewer.cropper) return;
    const d = viewer.cropper.getData(true);
    const s = viewer.cropScale;
    const x = Math.max(0, Math.round(d.x / s));
    const y = Math.max(0, Math.round(d.y / s));
    const crop = { x, y, w: Math.min(fullW - x, Math.round(d.width / s)), h: Math.min(fullH - y, Math.round(d.height / s)) };
    viewer.cropper.destroy();
    viewer.cropper = null;
    viewer.mode = 'view';
    const whole = crop.x === 0 && crop.y === 0 && crop.w >= fullW - 2 && crop.h >= fullH - 2;
    await editPage({ crop: whole ? null : crop });
  });
}

/* ---------- Settings ---------- */

function viewSettings() {
  const page = h('div', { class: 'page' }, h('h1', { class: 'large-title' }, 'Settings'));
  const app = state.appSettings;
  const save = async (next) => {
    state.appSettings = next;
    render();
    try { state.appSettings = await api.saveSettings(next); toast('Saved'); } catch (e) { failed(e); }
  };
  const buttonGroup = h('section', { class: 'group' }, h('h2', { class: 'group-header' }, 'The scanner’s button'));
  if (app) {
    buttonGroup.append(settingsFields(app.buttonScan, (next) => save({ ...app, buttonScan: next }), 'button'),
      h('p', { class: 'group-footer' }, 'Used when someone presses the button on the scanner. Those scans wait under To review until someone sends them.'));
  } else buttonGroup.append(h('div', { class: 'list' }, h('div', { class: 'empty' }, 'Loading…')));
  page.append(buttonGroup);

  if (app) {
    const keep = [[0, 'Forever'], [1, '1 day'], [7, '7 days'], [30, '30 days']];
    page.append(h('section', { class: 'group' }, h('h2', { class: 'group-header' }, 'Keep filed scans'),
      h('div', { class: 'list' }, h('div', { class: 'field' },
        segmented('keep', keep, app.retentionDays, (v) => save({ ...app, retentionDays: Number(v) })))),
      h('p', { class: 'group-footer' }, 'Once Paperless has filed every page of a scan, the scanner deletes its copy after this long. Scans you haven’t sent are never deleted.')));
  }

  const pl = state.paperless;
  let plCell;
  if (!pl) plCell = h('div', { class: 'cell' }, h('span', { class: 'spinner' }), h('div', { class: 'main' }, h('div', { class: 'title' }, 'Checking Paperless…')));
  else if (!pl.configured) plCell = h('div', { class: 'cell' }, h('span', { class: 'dot' }), h('div', { class: 'main' }, h('div', { class: 'title' }, 'Not set up'), h('div', { class: 'subtitle' }, 'Scans can be downloaded as PDFs until Paperless is connected.')));
  else if (pl.connected) plCell = h('a', { class: 'cell', href: pl.url, target: '_blank', rel: 'noopener' }, h('span', { class: 'dot green' }), h('div', { class: 'main' }, h('div', { class: 'title' }, 'Connected'), h('div', { class: 'subtitle' }, pl.url)), icon('external', 'chev'));
  else plCell = h('div', { class: 'cell' }, h('span', { class: 'dot red' }), h('div', { class: 'main' }, h('div', { class: 'title' }, 'Can’t connect'), h('div', { class: 'subtitle' }, pl.error || pl.url)));
  page.append(h('section', { class: 'group' }, h('h2', { class: 'group-header' }, 'Paperless'), h('div', { class: 'list' }, plCell),
    pl && !pl.configured && h('p', { class: 'group-footer' }, 'To connect, whoever runs the scanner sets DOXIE_PAPERLESS_URL and DOXIE_PAPERLESS_TOKEN and restarts it.')));

  const st = scannerState();
  page.append(h('section', { class: 'group' }, h('h2', { class: 'group-header' }, 'Scanner'),
    h('div', { class: 'list' },
      statusCell(st),
      h('div', { class: 'cell' }, h('div', { class: 'main' }, h('div', { class: 'title' }, 'Model')), h('span', { class: 'value' }, 'Doxie Pro DX400')),
      h('div', { class: 'cell' }, h('div', { class: 'main' }, h('div', { class: 'title' }, 'App version')), h('span', { class: 'value num' }, state.version || '…')))));
  return page;
}

/* ---------- Actions ---------- */

async function startScan() {
  state.starting = true;
  state.notice = null;
  render();
  try {
    const { jobId } = await api.startScan(state.scanSettings);
    state.starting = false;
    location.hash = `#/scans/${jobId}`;
  } catch (e) {
    state.starting = false;
    if (e.code === 'no_paper') state.notice = { tone: 'orange', title: 'No paper', text: 'Put paper in the scanner, then tap Scan.' };
    else if (e.code === 'scanner_unavailable') state.notice = { tone: 'orange', icon: 'power', title: 'The scanner is off', text: 'Press its power button, wait a few seconds, then tap Scan.' };
    else if (e.status === 409 && e.code !== 'no_paper') state.notice = { tone: 'blue', title: 'Already scanning', text: 'Wait for the current scan to finish.' };
    else state.notice = { tone: 'red', title: 'Couldn’t start the scan', text: e.message };
    render();
  }
}

function goBack() {
  if (history.length > 1 && document.referrer !== '' ) history.back();
  else location.hash = '#/scans';
}

/* ---------- Routing and rendering ---------- */

function parseRoute() {
  const hash = location.hash || '#/';
  if (hash === '#/' || hash === '#') return { name: 'home' };
  if (hash === '#/scans') return { name: 'scans' };
  if (hash === '#/settings') return { name: 'settings' };
  const m = hash.match(/^#\/scans\/([\w-]+)$/);
  if (m) return { name: 'scan', id: m[1] };
  return null;
}

let lastSignature = '';

function render(force = true) {
  const view = document.getElementById('view');
  const r = state.route;
  const split = sideBySide.matches && (r.name === 'scans' || r.name === 'scan');
  const node = split ? viewMasterDetail() : r.name === 'home' ? viewHome() : r.name === 'scans' ? viewScans() : r.name === 'settings' ? viewSettings() : viewScan();
  view.classList.toggle('flush', split);
  const sig = node.outerHTML;
  if (!force && sig === lastSignature) return;
  lastSignature = sig;
  view.replaceChildren(node);
  watchTitle(node);
}

// The bar title appears once the large title has scrolled out of view.
let titleObserver;
function watchTitle(node) {
  if (titleObserver) titleObserver.disconnect();
  const bar = node.querySelector('.navbar');
  const big = node.querySelector('.navbar-title-row h2');
  if (!bar || !big) return;
  titleObserver = new IntersectionObserver(([entry]) => bar.classList.toggle('scrolled', !entry.isIntersecting), { rootMargin: '-52px 0px 0px 0px' });
  titleObserver.observe(big);
}

async function onRoute() {
  const r = parseRoute();
  if (!r) { location.replace('#/'); return; }
  const changed = r.name !== state.route.name || r.id !== state.route.id;
  state.route = r;
  if (changed) {
    closeViewer();
    state.splitDraft = null;
    state.hiddenOpen = false;
    state.notice = null;
    if (r.name === 'scan') { state.scan = null; state.scanError = null; }
    window.scrollTo(0, 0);
  }
  const tab = r.name === 'scan' ? 'scans' : r.name;
  document.querySelectorAll('.tabbar a').forEach((a) => {
    if (a.dataset.tab === tab) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
  });
  render();
  await refresh();
  if (changed) document.getElementById('view').focus({ preventScroll: true });
}

async function refresh() {
  const r = state.route;
  try {
    if (r.name === 'home' || r.name === 'scans' || (r.name === 'scan' && sideBySide.matches)) state.scans = await api.scans();
    if (r.name === 'scan') {
      try { state.scan = await api.scan(r.id); } catch (e) { if (e.status === 404) state.scanError = e; else throw e; }
    }
    if (r.name === 'settings') {
      const [settings, version] = await Promise.all([api.settings(), api.version()]);
      state.appSettings = settings;
      state.version = version.version;
      api.paperless().then((p) => { state.paperless = p; render(false); }).catch(() => {});
    }
  } catch (e) { failed(e); }
  render(false);
}

/* ---------- Polling ---------- */

let ticks = 0;

async function tick() {
  if (document.hidden) return;
  ticks++;
  const r = state.route;
  const wasBusy = state.status && state.status.busy;
  if (ticks % 2 === 0) {
    try { state.status = await api.status(); state.statusError = null; } catch (e) { state.statusError = e; }
  }
  const busy = state.status && state.status.busy;
  try {
    if (r.name === 'scan' && state.scan && !state.splitDraft) {
      if (state.scan.status === 'running') {
        state.scan = await api.scan(r.id);
        if (viewer && viewer.mode === 'view') drawViewer();
      } else if (ticks % 2 === 0 && (state.scan.sent || []).some((s) => ['sending', 'queued', 'processing'].includes(s.status))) {
        state.scan = await api.scan(r.id); // the scanner follows Paperless itself
      }
    }
    const listed = r.name === 'home' || r.name === 'scans' || (r.name === 'scan' && sideBySide.matches);
    if (listed && (busy || wasBusy !== busy || ticks % 5 === 0)) state.scans = await api.scans();
  } catch { /* the status cell already reports reachability */ }
  if (!viewer && !document.querySelector('.sheet-panel')) render(false);
}

/* ---------- Start ---------- */

async function start() {
  sideBySide.addEventListener('change', () => refresh());
  let options = { defaults: { resolution: 300, sides: 'auto', color: 'auto' }, resolutions: [200, 300, 600] };
  try { options = await api.options(); } catch { /* keep defaults */ }
  state.options = options;
  state.scanSettings = loadScanSettings(options.defaults);
  api.paperless().then((p) => { state.paperless = p; }).catch(() => {});
  window.addEventListener('hashchange', onRoute);
  document.addEventListener('visibilitychange', () => { if (!document.hidden) { ticks = 1; tick(); } });
  try { state.status = await api.status(); } catch (e) { state.statusError = e; }
  await onRoute();
  setInterval(tick, 1000);
}

start();
