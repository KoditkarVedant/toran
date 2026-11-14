// Tests of the account screens in index.html against a fake Go server: first-run setup, login,
// a login that ends while the page is open, per-user browser keys, and the admin's user list.
const { JSDOM, VirtualConsole } = require('jsdom');
const fs = require('fs');
const html = fs.readFileSync(require('path').join(__dirname, '..', 'index.html'), 'utf8');

let fails = 0;
const ok = (c, m) => { if (!c) { fails++; console.log('FAIL:', m); } else console.log('ok  :', m); };
const sleep = ms => new Promise(r => setTimeout(r, ms));

const CONFIG = { groups: [{ name: 'Media', items: [{ name: 'Jellyfin', url: ':8096' }] }], settings: {} };
const ADMIN = { id: 'a1', name: 'vedant', role: 'admin' };
const KID = { id: 'k1', name: 'kid', role: 'user' };

/* A fake server. `s` is its mutable state; every request is recorded in `calls` as "METHOD url". */
function makeDom(s) {
  const errors = [], calls = [], bodies = [];
  const vc = new VirtualConsole();
  vc.on('jsdomError', e => errors.push('jsdomError: ' + (e.detail?.stack || e.message)));
  const dom = new JSDOM(html, {
    url: 'http://pi.local:8080/', runScripts: 'dangerously', pretendToBeVisual: true, virtualConsole: vc,
    beforeParse(w) {
      w.structuredClone = x => JSON.parse(JSON.stringify(x));
      w.matchMedia = q => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} });
      w.HTMLDialogElement.prototype.showModal = function () { this.setAttribute('open', ''); };
      w.HTMLDialogElement.prototype.close = function () { this.removeAttribute('open'); };
      w.Element.prototype.scrollIntoView = () => {};
      w.confirm = () => s.confirm !== false;
      w.fetch = async (u, o = {}) => {
        u = String(u); const m = o.method || 'GET'; calls.push(m + ' ' + u);
        const body = o.body && typeof o.body === 'string' ? JSON.parse(o.body) : null; bodies.push(body);
        const res = (status, v) => ({ ok: status < 300, status, json: async () => { if (v === undefined) throw new Error('no body'); return v; } });
        if (u === 'api/health') return res(200, { ok: true, version: 't' });
        if (u === 'api/session') {
          if (s.noAccounts) return res(404, undefined);
          return res(200, s.me ? { state: 'ok', user: s.me } : { state: s.state });
        }
        if (u === 'api/setup') {
          if (body.code.toUpperCase() !== s.code) return res(403, { error: 'wrong setup code' });   // the real server ignores case, spaces and dashes
          s.me = { id: 'a1', name: body.name, role: 'admin' }; return res(200, { user: s.me });
        }
        if (u === 'api/login') {
          const who = s.accounts?.[body.name];
          if (!who || who.password !== body.password) return res(401, { error: 'wrong name or password' });
          s.me = who.user; s.expired = false; return res(200, { user: who.user });
        }
        if (u === 'api/logout') { s.me = null; return res(204); }
        if (u === 'api/password') return body.current === 'right' ? res(204) : res(403, { error: 'the current password is wrong' });
        if (u === 'api/users' && m === 'GET') return res(200, s.users);
        if (u === 'api/users' && m === 'POST') { s.users.push({ id: 'n' + s.users.length, name: body.name, role: body.role, disabled: false }); return res(201, {}); }
        if (u.startsWith('api/users/')) {
          const t = s.users.find(x => x.id === u.slice(10));
          if (m === 'PATCH') Object.assign(t, body.disabled !== undefined ? { disabled: body.disabled } : {}, body.role ? { role: body.role } : {});
          if (m === 'DELETE') s.users = s.users.filter(x => x !== t);
          return res(m === 'DELETE' ? 204 : 200, {});
        }
        if (u === 'api/status') return res(200, {});
        if (u === 'api/backups') return res(200, []);
        if (u === 'services.json' && s.expired) return res(401, { error: 'login required' });
        if (u === 'services.json' && m === 'PUT') { s.store = o.body; return res(204); }
        if (u === 'services.json') return s.store ? res(200, JSON.parse(s.store)) : res(404, undefined);
        throw new Error('unreachable: ' + u);   // weather and the like: offline
      };
    },
  });
  const w = dom.window, d = w.document;
  w.eval('reloadPage = () => { window.__reloaded = true; }');
  return { w, d, calls, bodies, errors, $: q => d.querySelector(q), $$: q => [...d.querySelectorAll(q)] };
}
const type = (input, v) => { input.value = v; };
const submit = (w, form) => form.dispatchEvent(new w.Event('submit', { bubbles: true, cancelable: true }));
const click = (w, e) => e.dispatchEvent(new w.MouseEvent('click', { bubbles: true, cancelable: true }));
const button = (page, text) => page.$$('button').find(b => b.textContent.trim() === text);

(async () => {
  /* ---- 1. first run: setup ---- */
  {
    const s = { state: 'setup', code: 'ABCD-EFGH-JKLM', store: JSON.stringify(CONFIG) };
    const p = makeDom(s); await sleep(150);
    ok(p.d.documentElement.dataset.gate === 'setup', 'first run shows the setup screen');
    const inputs = p.$$('#auth input');
    ok(inputs.length === 3 && inputs[2].type === 'password' && inputs[2].autocomplete === 'new-password', 'setup asks for code, name and a new password');
    ok(p.$$('.tile').length === 0, 'the dashboard is not drawn before anyone is logged in');
    ok(!p.calls.includes('GET services.json'), 'no data is requested before login');

    type(inputs[0], 'WRONG'); type(inputs[1], 'vedant'); type(inputs[2], 'correct horse battery');
    submit(p.w, p.$('#auth form')); await sleep(80);
    ok(/wrong setup code/.test(p.$('.auth-msg').textContent) && p.$('.auth-msg').classList.contains('bad'), 'a wrong code is explained');
    ok(p.d.documentElement.dataset.gate === 'setup' && p.$$('#auth input')[2].value === '', 'the screen stays and the password is cleared');

    type(p.$$('#auth input')[0], 'abcd-efgh-jklm');
    type(p.$$('#auth input')[2], 'correct horse battery');
    submit(p.w, p.$('#auth form')); await sleep(250);
    ok(JSON.stringify(p.bodies[p.calls.lastIndexOf('POST api/setup')]) === JSON.stringify({ code: 'abcd-efgh-jklm', name: 'vedant', password: 'correct horse battery' }), 'setup sends code, name and password');
    ok(!p.d.documentElement.hasAttribute('data-gate') && p.$('#auth').children.length === 0, 'after setup the gate opens and the form is gone');
    ok(p.$$('.tile').length === 1, 'the dashboard appears');
    ok(p.$('#btn-account').style.display === '' && p.$('#btn-account').textContent === 'V', 'the account button shows the initial');
    ok(p.w.eval('KEY') === 'toran.config.v2.a1' && p.w.eval('RKEY') === 'toran.recent.a1', 'browser storage is keyed by user: ' + p.w.eval('KEY'));
    ok(p.errors.length === 0, 'no page errors: ' + p.errors.join('; '));
  }

  /* ---- 2. login ---- */
  {
    const s = { state: 'login', accounts: { vedant: { password: 'correct horse battery', user: ADMIN } }, store: JSON.stringify(CONFIG) };
    const p = makeDom(s); await sleep(150);
    ok(p.d.documentElement.dataset.gate === 'login' && p.$$('#auth input').length === 2, 'login asks for name and password only');
    ok(p.$$('#auth input')[0].autocomplete === 'username' && p.$$('#auth input')[1].autocomplete === 'current-password', 'password managers can fill it');
    type(p.$$('#auth input')[0], 'vedant'); type(p.$$('#auth input')[1], 'nope');
    submit(p.w, p.$('#auth form')); await sleep(80);
    ok(/wrong name or password/.test(p.$('.auth-msg').textContent) && p.$$('.tile').length === 0, 'a wrong password is refused and shows nothing');
    type(p.$$('#auth input')[1], 'correct horse battery');
    submit(p.w, p.$('#auth form')); await sleep(250);
    ok(!p.d.documentElement.hasAttribute('data-gate') && p.$$('.tile').length === 1, 'the right password opens the dashboard');
    ok(p.calls.indexOf('GET services.json') > p.calls.indexOf('POST api/login'), 'data is only requested after logging in');
  }

  /* ---- 3. already logged in; a brand-new user has no saved config yet ---- */
  {
    const s = { me: KID, users: [] };           // no services.json on the server: the starter dashboard shows
    const p = makeDom(s); await sleep(200);
    ok(!p.d.documentElement.hasAttribute('data-gate') && p.$$('.tile').length > 0, 'a new user gets the starter dashboard');
    ok(p.w.eval('KEY') === 'toran.config.v2.k1', 'KEY is per user');
    p.w.eval('cfg.groups[0].name = "Mine"; queueSave()'); await sleep(900);
    ok(JSON.parse(s.store).groups[0].name === 'Mine', 'their first edit is saved to the server');
  }

  /* ---- 4. the login ends while the page is open ---- */
  {
    const s = { me: ADMIN, users: [], store: JSON.stringify(CONFIG), accounts: { vedant: { password: 'correct horse battery', user: ADMIN } } };
    const p = makeDom(s); await sleep(200);
    s.expired = true; s.me = null;
    p.w.eval('cfg.groups[0].name = "Edited while logged out"; queueSave()'); await sleep(900);
    ok(p.d.documentElement.dataset.gate === 'login', 'an ended login brings up the login screen');
    ok(/changes are kept/.test(p.$('.auth-msg').textContent), 'it says nothing is lost: ' + p.$('.auth-msg').textContent);
    ok(/Edited while logged out/.test(p.w.eval('JSON.stringify(cfg)')), 'the unsaved edit is still in memory');
    ok(!/Could not save/.test(p.$('#toast').textContent), 'it is not reported as a failed save');
    type(p.$$('#auth input')[0], 'vedant'); type(p.$$('#auth input')[1], 'correct horse battery');
    submit(p.w, p.$('#auth form')); await sleep(400);
    ok(!p.d.documentElement.hasAttribute('data-gate'), 'after logging in the page continues');
    ok(JSON.parse(s.store).groups[0].name === 'Edited while logged out', 'the interrupted save is repeated and reaches the server');
    ok(!p.w.__reloaded, 'the same person logging in does not reload the page');
    ok(p.errors.length === 0, 'no page errors: ' + p.errors.join('; '));
  }
  {
    const other = { id: 'o1', name: 'other', role: 'user' };
    const s = { me: ADMIN, users: [], store: JSON.stringify(CONFIG), accounts: { other: { password: 'another password', user: other } } };
    const p = makeDom(s); await sleep(200);
    s.expired = true; s.me = null;
    p.w.eval('cfg.groups[0].name = "private"; queueSave()'); await sleep(900);
    type(p.$$('#auth input')[0], 'other'); type(p.$$('#auth input')[1], 'another password');
    submit(p.w, p.$('#auth form')); await sleep(400);
    ok(p.w.__reloaded === true, 'a different person logging in reloads the page, so they never get the first one\'s data');
    ok(JSON.parse(s.store).groups[0].name !== 'private', 'the first person\'s unsaved edit is not saved into the second person\'s dashboard');
  }

  /* ---- 5. no account system: the page works as before ---- */
  {
    const p = makeDom({ noAccounts: true, store: JSON.stringify(CONFIG) }); await sleep(200);
    ok(!p.d.documentElement.hasAttribute('data-gate') && p.$$('.tile').length === 1, 'without the Go server there is no login');
    ok(p.$('#btn-account').style.display === 'none', 'and no account button');
    ok(p.w.eval('KEY') === 'toran.config.v2', 'and the browser keys stay as they were');
  }

  /* ---- 6. account drawer: a plain user ---- */
  {
    const s = { me: KID, users: [], store: JSON.stringify(CONFIG) };
    const p = makeDom(s); await sleep(200);
    click(p.w, p.$('#btn-account')); await sleep(50);
    ok(p.$('#drawer').classList.contains('open') && /Account/.test(p.$('#drawer h3').textContent), 'the account button opens the account drawer');
    ok(!/Users/.test(p.$('#drbody').textContent.replace('kid', '')) && !p.calls.includes('GET api/users'), 'a plain user sees no user management and asks for none');

    const pw = p.$$('#drbody input');
    type(pw[0], 'wrong'); type(pw[1], 'a new password');
    click(p.w, button(p, 'Change password')); await sleep(100);
    ok(/current password is wrong/.test(p.$('#drbody .auth-msg').textContent), 'a wrong current password is explained');
    ok(p.d.documentElement.dataset.gate === undefined, '...and does not look like an ended login (it is a 403, not a 401)');
    type(pw[0], 'right');
    click(p.w, button(p, 'Change password')); await sleep(100);
    ok(/Password changed/.test(p.$('#drbody .auth-msg').textContent) && pw[0].value === '' && pw[1].value === '', 'a changed password is confirmed and the fields are cleared');

    click(p.w, button(p, 'Log out')); await sleep(80);
    ok(p.calls.includes('POST api/logout') && p.w.__reloaded === true, 'logging out tells the server and reloads');
  }

  /* ---- 7. account drawer: an admin manages users ---- */
  {
    const s = { me: ADMIN, store: JSON.stringify(CONFIG), users: [
      { id: 'a1', name: 'vedant', role: 'admin', disabled: false }, { id: 'k1', name: 'kid', role: 'user', disabled: false }] };
    const p = makeDom(s); await sleep(200);
    click(p.w, p.$('#btn-account')); await sleep(150);
    const rows = p.$$('.urow');
    ok(rows.length === 2 && /vedant \(you\)/.test(rows[0].textContent) && /kid/.test(rows[1].textContent), 'the admin sees every user');
    ok(rows[0].querySelectorAll('button').length === 0, 'there are no actions on your own row');
    ok(['Reset password', 'Disable', 'Make admin', 'Remove'].every(t => [...rows[1].querySelectorAll('button')].some(b => b.textContent === t)), 'the other user has all four actions');

    click(p.w, [...rows[1].querySelectorAll('button')].find(b => b.textContent === 'Disable')); await sleep(100);
    ok(p.calls.includes('PATCH api/users/k1') && p.bodies[p.calls.lastIndexOf('PATCH api/users/k1')].disabled === true, 'Disable asks the server');
    ok(p.$$('.urow')[1].classList.contains('off') && /Enable/.test(p.$$('.urow')[1].textContent), 'the list refreshes: disabled, now offers Enable');

    click(p.w, [...p.$$('.urow')[1].querySelectorAll('button')].find(b => b.textContent === 'Make admin')); await sleep(100);
    ok(p.bodies[p.calls.lastIndexOf('PATCH api/users/k1')].role === 'admin', 'Make admin sends the new role');

    click(p.w, [...p.$$('.urow')[1].querySelectorAll('button')].find(b => b.textContent === 'Reset password')); await sleep(50);
    const resetPw = p.$$('.urow')[1].querySelector('input');
    ok(resetPw && resetPw.type === 'password', 'Reset password opens a password field in the row');
    type(resetPw, 'a brand new password');
    click(p.w, [...p.$$('.urow')[1].querySelectorAll('button')].find(b => b.textContent === 'Set password')); await sleep(100);
    ok(p.bodies[p.calls.lastIndexOf('PATCH api/users/k1')].password === 'a brand new password', 'the new password is sent');

    // adding
    const addInputs = p.$$('#drbody input'), sel = p.$('#drbody select');
    type(addInputs.find(i => i.getAttribute('aria-label') === 'User name'), 'asha');
    type(addInputs.find(i => /Password \(at least/.test(i.getAttribute('aria-label'))), 'a password for asha');
    sel.value = 'user';
    click(p.w, button(p, 'Add user')); await sleep(120);
    const post = p.bodies[p.calls.lastIndexOf('POST api/users')];
    ok(post && post.name === 'asha' && post.role === 'user' && post.password === 'a password for asha', 'Add user sends name, role and password');
    ok(p.$$('.urow').length === 3 && /asha/.test(p.$$('.urow')[2].textContent), 'the new user appears in the list');

    // removing asks first
    s.confirm = false;
    click(p.w, [...p.$$('.urow')[2].querySelectorAll('button')].find(b => b.textContent === 'Remove')); await sleep(80);
    ok(!p.calls.includes('DELETE api/users/n2'), 'declining the confirmation removes nobody');
    s.confirm = true;
    click(p.w, [...p.$$('.urow')[2].querySelectorAll('button')].find(b => b.textContent === 'Remove')); await sleep(100);
    ok(p.calls.includes('DELETE api/users/n2') && p.$$('.urow').length === 2, 'confirming removes the user and refreshes the list');
    ok(p.errors.length === 0, 'no page errors: ' + p.errors.join('; '));
  }

  console.log(fails ? `\n${fails} FAILED` : '\nALL PASSED');
  process.exit(fails ? 1 : 0);
})().catch(e => { console.log('TEST CRASH', e); process.exit(2); });
