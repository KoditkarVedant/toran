const { JSDOM, VirtualConsole } = require('jsdom');
const fs = require('fs');
const html = fs.readFileSync(require('path').join(__dirname, '..', 'index.html'), 'utf8');
let store = JSON.stringify({ groups: [
  { name: 'Media', items: [{ name: 'Jellyfin', url: ':8096' }, { name: 'Sonarr', url: ':8989' }] },
  { name: 'Tools', items: [{ name: 'Portainer', url: 'http://pi:9000' }] }], settings: {} });
const errors = [], calls = [];
const vc = new VirtualConsole();
vc.on('jsdomError', e => errors.push('jsdomError: ' + (e.detail?.stack || e.message)));

const dom = new JSDOM(html, {
  url: 'http://pi.local:8080/', runScripts: 'dangerously', pretendToBeVisual: true, virtualConsole: vc,
  beforeParse(w) {
    w.structuredClone = x => JSON.parse(JSON.stringify(x));
    w.matchMedia = q => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} });
    w.HTMLDialogElement.prototype.showModal = function () { this.setAttribute('open', ''); };
    w.HTMLDialogElement.prototype.close = function () { this.removeAttribute('open'); this.dispatchEvent(new w.Event('close')); };
    w.Element.prototype.scrollIntoView = () => {};
    w.confirm = () => true;
    w.fetch = async (u, o = {}) => {
      u = String(u); const m = o.method || 'GET'; calls.push(m + ' ' + u);
      const json = (v, ok = true) => ({ ok, status: ok ? 200 : 503, json: async () => v });
      if (u === 'api/health') return json({ ok: true, version: '1.0' });
      if (u === 'api/status') return json({ ':8096': { state: 'up', code: 200, ms: 12 }, ':8989': { state: 'warn', code: 502, ms: 40 }, 'http://pi:9000': { state: 'down', ms: 2500 } });
      if (u === 'api/backups') return json([{ name: 'services-20250101-120000-000.json', time: '2025-01-01T12:00:00Z', size: 100 }]);
      if (u === 'api/backups/restore') { store = JSON.stringify({ groups: [{ name: 'Restored', items: [{ name: 'Old', url: ':1' }] }] }); return { ok: true }; }
      if (m === 'PUT' && u.includes('services.json')) { store = o.body; return { ok: true }; }
      if (u.includes('services.json')) return { ok: true, json: async () => JSON.parse(store) };
      if (u.includes('open-meteo')) throw new Error('offline');
      throw new Error('unreachable: ' + u);
    };
  },
});
const w = dom.window, d = w.document, $ = s => d.querySelector(s), $$ = s => [...d.querySelectorAll(s)];
const sleep = ms => new Promise(r => setTimeout(r, ms));
const clickEl = e => e.dispatchEvent(new w.MouseEvent('click', { bubbles: true, cancelable: true }));
let fails = 0;
const ok = (c, m) => { if (!c) { fails++; console.log('FAIL:', m); } else console.log('ok  :', m); };

(async () => {
  await sleep(400);
  ok(w.eval('!!api') === true, 'page detects the Go server (api/health)');
  const dot = u => $(`.dot[data-u="${w.eval(`resolve("${u}")`)}"]`);
  ok(dot(':8096').classList.contains('up') && /HTTP 200/.test(dot(':8096').title), 'server status: up, tooltip has HTTP code: "' + dot(':8096').title + '"');
  ok(dot(':8989').classList.contains('warn') && /502/.test(dot(':8989').title), 'server status: 5xx shows as warn');
  ok(dot('http://pi:9000').classList.contains('down'), 'server status: down');
  ok(calls.every(c => !/^GET http:\/\/pi/.test(c)), 'browser does not ping services itself when the server does it');

  // backups in the General tab
  $('#btn-settings').click(); $$('.dr-tabs button').find(b => b.textContent === 'General').click(); await sleep(50);
  ok($$('#drbody .bkrow').length === 1, 'backup list shown in General tab');
  clickEl($('#drbody .bkrow button')); await sleep(80);
  ok(w.eval('cfg.groups[0].name') === 'Restored', 'restoring a backup reloads the dashboard from the server');
  ok(calls.includes('POST api/backups/restore'), 'restore request sent');

  console.log('\npage errors:', errors.length ? errors : 'none');
  console.log(fails ? `\n${fails} FAILED` : '\nALL PASSED');
  process.exit(fails || errors.length ? 1 : 0);
})().catch(e => { console.log('TEST CRASH', e, errors); process.exit(2); });
