const { JSDOM, VirtualConsole } = require('jsdom');
const fs = require('fs');
const html = fs.readFileSync(require('path').join(__dirname, '..', 'index.html'), 'utf8');
const services = JSON.stringify({ groups: [
  { name: 'Media', items: [{ name: 'Jellyfin', url: ':8096', desc: 'TV', icon: 'ðŸŽ¬' }, { name: 'Sonarr', url: ':8989' }, { name: 'Radarr', url: ':7878' }] },
  { name: 'Tools', items: [{ name: 'Portainer', url: ':9000', icon: 'ðŸ³' }, { name: 'Pi-hole', url: 'http://pi:81' }] }],
  settings: { design: 'vision', featured: 'Sonarr', name: 'Vedant' } });   // old-format settings -> must migrate

const errors = [], puts = [];
const vc = new VirtualConsole();
vc.on('jsdomError', e => errors.push('jsdomError: ' + (e.detail?.stack || e.message)));
vc.on('error', e => errors.push('console.error: ' + e));

const dom = new JSDOM(html, {
  url: 'http://pi.local:8080/', runScripts: 'dangerously', pretendToBeVisual: true, virtualConsole: vc,
  beforeParse(w) {
    w.structuredClone = x => JSON.parse(JSON.stringify(x));
    w.matchMedia = q =>({ matches: false, media: q, addEventListener() {}, removeEventListener() {} });
    w.HTMLDialogElement.prototype.showModal = function () { this.setAttribute('open', ''); };
    w.HTMLDialogElement.prototype.close = function () { this.removeAttribute('open'); this.dispatchEvent(new w.Event('close')); };
    w.Element.prototype.scrollIntoView = () => {};
    w.createImageBitmap = async () => ({ width: 100, height: 60 });
    w.confirm = () => true; w.alert = () => {};
    w.URL.createObjectURL = () => 'blob:x'; w.URL.revokeObjectURL = () => {};
    w.HTMLCanvasElement.prototype.getContext = () => ({ drawImage() {}, fillRect() {} });
    w.HTMLCanvasElement.prototype.toBlob = cb => cb(new w.Blob(['x']));
    w.HTMLAnchorElement.prototype.click = function () {};
    let store = services;
    w.fetch = async (u, o = {}) => {
      u = String(u);
      if (o.method === 'PUT' && u.includes('services.json')) { store = o.body; puts.push(o.body); return { ok: true }; }
      if (u.includes('services.json')) return { ok: true, json: async () => JSON.parse(store) };
      if (u.includes('open-meteo')) {
        if (u.includes('geocoding')) return { ok: true, json: async () => ({ results: [{ latitude: 1, longitude: 2 }] }) };
        return { ok: true, json: async () => ({ current: { temperature_2m: 21.4, weather_code: 2 } }) };
      }
      if (o.method) return { ok: true };
      throw new Error('unreachable: ' + u);       // status pings -> "down"
    };
  },
});
const w = dom.window, d = w.document, $ = s => d.querySelector(s), $$ = s => [...d.querySelectorAll(s)];
const clickEl = e => e.dispatchEvent(new w.MouseEvent('click', { bubbles: true, cancelable: true }));
const sleep = ms => new Promise(r => setTimeout(r, ms));
const fire = (e, type, init) => e.dispatchEvent(new w.Event(type, { bubbles: true, ...init }));
let fails = 0;
const ok = (cond, msg) => { if (!cond) { fails++; console.log('FAIL:', msg); } else console.log('ok  :', msg); };

(async () => {
  await sleep(300);
  ok(w.eval('cfg.settings.layout') === 'launcher' && w.eval('cfg.settings.nav') === 'tabs' && w.eval('cfg.settings.surface') === 'glass', 'old "vision" design migrated to layout/nav/surface');
  ok(w.eval('cfg.settings.design') === undefined, 'old design key removed');
  ok($('#hero .hero h2')?.textContent === 'Sonarr', 'featured banner uses migrated featuredItem');
  ok($$('.tile').length === 5, 'tiles rendered: ' + $$('.tile').length);
  ok($$('#tabs button').length === 3, 'tabs: All + 2 groups');
  ok(d.documentElement.dataset.layout === 'launcher', 'layout attribute applied');

  // tabs filter
  $$('#tabs button')[2].click();
  ok($$('.tile').length === 2, 'tab filters to one group');
  $$('#tabs button')[0].click();

  // search
  const q = $('#q'); q.value = 'pi'; fire(q, 'input');
  ok($$('.tile').length === 1 && $('.tile.first'), 'search narrows + marks first');
  q.value = ''; fire(q, 'input');

  // open settings drawer, walk every tab, click every control
  $('#btn-settings').click();
  ok($('#drawer').classList.contains('open') && !$('#drawer').inert, 'drawer opens');
  for (const [k] of [['look'], ['bg'], ['parts'], ['general']]) {
    const tab = $$('.dr-tabs button').find(b => b.className !== undefined && b.textContent.toLowerCase().startsWith(k === 'parts' ? 'comp' : k === 'bg' ? 'back' : k));
    tab.click();
    const btns = $$('#drbody .pick button'), seg = $$('#drbody .seg button'), tg = $$('#drbody .tg input'), rg = $$('#drbody input[type=range]');
    for (const b of btns) { b.click(); }
    for (const b of seg) { b.click(); }
    for (const t of tg) { t.checked = !t.checked; fire(t, 'change'); }
    for (const r of rg) { r.value = r.max; fire(r, 'input'); }
    ok(true, `tab "${k}": clicked ${btns.length} picks, ${seg.length} seg, ${tg.length} toggles, ${rg.length} sliders`);
  }
  // look + layout + theme combos
  $$('.dr-tabs button')[0].click();
  const clickLabel = (grp, label) => $$(`#drbody .pick button`).find(b => b.title === label)?.click();
  clickLabel(0, 'Cubes'); ok(d.documentElement.dataset.layout === 'cubes' && S('theme') === 'amoled', 'Look "Cubes" sets layout + theme');
  clickLabel(0, 'Dracula'); ok(S('theme') === 'dracula' && d.documentElement.dataset.layout === 'cubes', 'theme changes independently of layout');
  $$('#drbody .pick button').filter(b => b.title === 'Bento').pop().click(); ok(d.documentElement.dataset.layout === 'bento' && S('theme') === 'dracula', 'layout changes independently of theme');
  function S(k) { return w.eval(`cfg.settings['${k}']`); }
  ok(getComputedStyle0('--bg') === '#282a36', 'dracula colours applied to :root');
  function getComputedStyle0(v) { return d.documentElement.style.getPropertyValue(v); }

  // surface/accent
  w.eval('setKey("accent","#ff0000")'); ok(getComputedStyle0('--accent') === '#ff0000', 'accent override applied');

  // weather (the loop above flipped every toggle, so put the unit/clock toggles back first)
  w.eval('setKey("tempF",undefined); setKey("clock24",undefined)');
  w.eval('setKey("city","Pune")'); w.eval('loadWeather()'); await sleep(300);
  ok(/21°C/.test($('#wx-t').textContent), 'weather chip filled: ' + $('#wx-t').textContent);

  // autosave
  await sleep(900);
  ok(puts.length >= 1, 'autosave PUT sent (' + puts.length + ')');
  const saved = JSON.parse(puts[puts.length - 1]);
  ok(saved.settings.theme === 'dracula' && saved.settings.layout === 'bento' && !('design' in saved.settings), 'saved JSON has new schema');

  // undo
  $$('.dr-foot button')[0].click();
  ok(S('layout') === 'launcher' && S('theme') === 'dark', 'Undo restores the settings from when the drawer opened');

  // close
  $$('.dr-foot button')[1].click(); await sleep(100);
  ok(!$('#drawer').classList.contains('open') && $('#drawer').inert, 'drawer closes');

  // edit mode: add / edit / delete / drag
  $('#btn-edit').click(); ok(d.body.classList.contains('editing'), 'edit mode on');
  $('#btn-add').click();
  const form = $$('#drawer .in'); form[0].value = 'Grafana'; form[1].value = '3001'; form[3].value = 'Tools';
  $$('#drawer .dr-foot button').find(b => b.textContent === 'Save').click(); await sleep(50);
  ok(w.eval('cfg.groups[1].items.length') === 3, 'service added to Tools');
  // edit existing
  const tile = $$('.tile').find(t => t.querySelector('.name').textContent === 'Pi-hole');
  clickEl(tile);
  ok($('#drawer h3').textContent === 'Edit service', 'clicking a tile in edit mode opens the editor (h3 is "' + $('#drawer h3').textContent + '", open=' + $('#drawer').classList.contains('open') + ')');
  $$('#drawer .in')[0].value = 'Pi-hole DNS';
  $$('#drawer .dr-foot button').find(b => b.textContent === 'Save').click(); await sleep(50);
  ok(w.eval('cfg.groups[1].items.some(i=>i.name==="Pi-hole DNS")'), 'service renamed in place');
  // drag Grafana before Portainer
  const tiles = $$('.tile'); const gf = tiles.find(t => t.querySelector('.name').textContent === 'Grafana'), pt = tiles.find(t => t.querySelector('.name').textContent === 'Portainer');
  const ds = new w.Event('dragstart', { bubbles: true }); ds.dataTransfer = { setData() {}, effectAllowed: '' }; gf.dispatchEvent(ds); const ev = new w.Event('drop', { bubbles: true, cancelable: true }); pt.dispatchEvent(ev);
  ok(w.eval('cfg.groups[1].items[0].name') === 'Grafana', 'drag & drop reordered services');
  // delete
  clickEl($$('.tile').find(t => t.querySelector('.name').textContent === 'Grafana'));
  $$('#drawer .dr-foot button').find(b => b.textContent === 'Delete').click(); await sleep(50);
  ok(!w.eval('cfg.groups.flatMap(g=>g.items).some(i=>i.name==="Grafana")'), 'service deleted');
  $('#btn-done').click(); ok(!d.body.classList.contains('editing'), 'edit mode off');

  // ---- new in this round ----
  w.eval('setKey("newTab", true)'); await sleep(20);
  ok($$('.tile').every(t => t.getAttribute('target') === '_blank' && /noopener/.test(t.getAttribute('rel'))), 'open-in-new-tab adds target=_blank rel=noopener to tiles');
  w.eval('setKey("newTab", undefined)'); await sleep(20);
  ok($$('.tile').every(t => !t.hasAttribute('target')), 'new-tab option can be turned off');

  w.eval('setKey("nav","tabs"); setKey("featured", false); setKey("recent", true)');
  const names = () => $$('#recent .rcard .name').map(n => n.textContent);
  w.eval('window.__by = n => cfg.groups.flatMap(g=>g.items).find(i=>i.name===n)');
  for (const n of ['Sonarr', 'Sonarr', 'Sonarr', 'Radarr']) { w.eval(`bump(window.__by("${n}"))`); await sleep(5); }
  w.eval('render()');
  ok(names()[0] === 'Radarr', 'recent row (latest) puts the last opened first: ' + names().join(','));
  w.eval('setKey("recentBy","used")'); await sleep(20);
  ok(names()[0] === 'Sonarr' && /Most used/.test($('#recent h2').textContent), 'most-used sorting by open count: ' + names().join(','));
  const order = () => [...$('#main').children].map(c => c.id).join(',');
  ok(order().endsWith('recent'), 'recent row defaults to the bottom: ' + order());
  w.eval('setKey("recentPos","top")'); await sleep(20);
  ok(order().indexOf('recent') < order().indexOf('tabs') && order().indexOf('recent') < order().indexOf('groups'), 'recent row can sit at the top: ' + order());
  w.eval('localStorage.setItem("toran.recent", JSON.stringify({Sonarr: 1700000000000}))');
  ok(w.eval('recentMap().Sonarr.n') === 1, 'old timestamp-only recent data is upgraded');

  w.eval('setKey("topbar","minimal")'); ok(d.documentElement.dataset.topbar === 'minimal', 'top bar style applied');
  w.eval('setKey("scale",125)'); ok(d.documentElement.style.getPropertyValue('--scale') === '1.25', 'UI scale applied');
  w.eval('setKey("tempF",true); tick()'); ok(/70°F/.test($('#wx-t').textContent), 'temperature in F: ' + $('#wx-t').textContent);
  w.eval('setKey("clock24",true); tick()'); ok(!/am|pm/i.test($('#time').textContent), '24h clock: ' + $('#time').textContent);

  ok(w.eval('slugOf("jellyfin")') === 'jellyfin' && w.eval('slugOf("home-assistant.svg")') === 'home-assistant', 'app-name slugs recognised');
  ok(w.eval('slugOf("TV")') === null && w.eval('slugOf("🎬")') === null && w.eval('slugOf("https://x/y.png")') === null, 'emoji, uppercase text and URLs are not treated as app names');
  ok(w.eval('autoSlug("Home Assistant")') === 'home-assistant' && w.eval('autoSlug("Pi-hole")') === 'pi-hole', 'auto name-to-slug');
  const box = d.createElement('div'); w.eval('window.__b=null'); w.__b = box;
  w.eval('fillIcon(window.__b, "🎬", "Jellyfin")'); ok(box.textContent === '🎬', 'emoji icon shown as text');
  w.eval('fillIcon(window.__b, "", "Zed")'); ok(box.textContent === 'Z', 'no logo found -> first letter shown');
  w.eval('setKey("topbar",undefined); setKey("scale",undefined)');

  // every layout x every theme renders without throwing
  let combos = 0;
  for (const l of ['grid', 'list', 'compact', 'launcher', 'rows', 'bento', 'cubes']) for (const t of w.eval('THEMES.map(t=>t.id)')) { w.eval(`setKey("layout","${l}");setKey("theme","${t}")`); combos++; }
  ok(true, `${combos} layoutÃ—theme combinations applied`);
  for (const n of ['sections', 'tabs', 'sidebar']) { w.eval(`setKey("nav","${n}")`); ok(d.documentElement.dataset.nav === n, 'nav ' + n); }

  // screensaver
  w.eval('startSS("stars")'); ok($('#ss').hasAttribute('open'), 'screensaver starts'); w.eval('ssEl.close()');

  console.log('\nerrors:', errors.length ? errors : 'none');
  console.log(fails ? `\n${fails} FAILED` : '\nALL PASSED');
  process.exit(fails || errors.length ? 1 : 0);
})().catch(e => { console.log('TEST CRASH', e, '\npage errors:', errors); process.exit(2); });

