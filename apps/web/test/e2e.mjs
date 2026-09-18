/**
 * End-to-end test for the yVPN web app.
 *
 * Drives the real apps/web/index.html in Chromium with the DigitalOcean and
 * Tailscale APIs mocked at the network layer, so the full create -> list ->
 * delete flow is exercised without touching a real cloud account or spending
 * a cent.
 *
 * The app itself has no dependencies; this test is the only thing that needs one.
 *
 *   npm install playwright && npx playwright install chromium
 *   node apps/web/test/e2e.mjs
 *
 * Exits non-zero if any check fails.
 */
import { chromium } from 'playwright';
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const APP = path.join(HERE, '..', 'index.html');
const SHOTS = process.env.SP || HERE;
const ORIGIN = 'http://127.0.0.1:8899';
const CORS = {
  'access-control-allow-origin': '*',
  'access-control-allow-headers': 'Authorization, Content-Type',
  'access-control-allow-methods': 'GET, POST, DELETE, OPTIONS',
  'content-type': 'application/json',
};

// ---- serve the real index.html at a normal http origin ----
const srv = http.createServer((req, res) => {
  res.writeHead(200, { 'content-type': 'text/html' });
  res.end(fs.readFileSync(APP));
}).listen(8899);

// ---- fake cloud state ----
let nextId = 1000;
const state = {
  droplets: [],
  keys: {},
  devices: [],
  routes: {},
  pollCount: 0,
};
const REGIONS = [
  { slug: 'nyc1', name: 'New York 1', available: true },
  { slug: 'sfo3', name: 'San Francisco 3', available: true },
  { slug: 'fra1', name: 'Frankfurt 1', available: true },
  { slug: 'lon1', name: 'London 1', available: false },
];

const log = [];
const results = [];
function check(name, cond, extra = '') {
  results.push({ name, ok: !!cond, extra });
  console.log(`${cond ? 'PASS' : 'FAIL'}  ${name}${extra ? '  — ' + extra : ''}`);
}

const browser = await chromium.launch();
const page = await browser.newPage();
page.on('console', (m) => log.push(`[console.${m.type()}] ${m.text()}`));
page.on('pageerror', (e) => { log.push(`[pageerror] ${e.message}`); check('no page errors', false, e.message); });

// ---------- DigitalOcean mock ----------
await page.route('https://api.digitalocean.com/**', async (route) => {
  const req = route.request();
  const url = new URL(req.url());
  const m = req.method();
  if (m === 'OPTIONS') return route.fulfill({ status: 204, headers: CORS, body: '' });

  const auth = req.headers()['authorization'];
  if (auth !== 'Bearer dop_v1_testtoken')
    return route.fulfill({ status: 401, headers: CORS, body: JSON.stringify({ id: 'unauthorized', message: 'Unable to authenticate you' }) });

  const p = url.pathname;
  const json = (o, status = 200) => route.fulfill({ status, headers: CORS, body: JSON.stringify(o) });

  if (p === '/v2/account') return json({ account: { email: 'ben@example.com', status: 'active' } });
  if (p === '/v2/customers/my/balance') {
    state.balanceCalls = (state.balanceCalls || 0) + 1;
    return json({ month_to_date_balance: '12.34', account_balance: '-5.00', month_to_date_usage: '12.34' });
  }
  if (p === '/v2/regions') return json({ regions: REGIONS });

  if (p === '/v2/droplets' && m === 'GET') {
    if (url.searchParams.get('tag_name') !== 'yVPN')
      return json({ message: 'test expected tag_name=yVPN' }, 500);
    return json({ droplets: state.droplets });
  }

  if (p === '/v2/droplets' && m === 'POST') {
    const b = JSON.parse(req.postData());
    check('create sends yVPN tag', b.tags?.includes('yVPN'));
    check('create sends ubuntu-24-04-x64', b.image === 'ubuntu-24-04-x64');
    check('create sends s-1vcpu-1gb', b.size === 's-1vcpu-1gb');
    check('cloud-init carries the auth key', b.user_data?.includes('tskey-auth-FAKE'));
    check('cloud-init advertises exit node', b.user_data?.includes('--advertise-exit-node'));
    // The slow parts, removed deliberately: an apt upgrade on first boot cost ~145 s
    // and DigitalOcean's vendor script another ~60 s.
    check('cloud-init does not apt-upgrade on boot', !/^\s*package_upgrade:/m.test(b.user_data || ''));
    check('cloud-init skips DO vendor data', /vendor_data:\s*\n\s*enabled: false/.test(b.user_data || ''));
    check('cloud-init installs the static tailscale build', b.user_data?.includes('pkgs.tailscale.com'));
    const d = {
      id: ++nextId, name: b.name, status: 'active',
      region: { slug: b.region, name: b.region },
      size: { slug: 's-1vcpu-1gb', price_monthly: 6, price_hourly: 0.00893 },
      created_at: new Date().toISOString(),
      networks: { v4: [{ type: 'public', ip_address: '203.0.113.' + (nextId % 250) }] },
    };
    state.droplets.push(d);
    state._pending = d.name;
    return json({ droplet: d }, 202);
  }

  if (/^\/v2\/droplets\/\d+$/.test(p) && m === 'DELETE') {
    const id = +p.split('/').pop();
    state.droplets = state.droplets.filter((d) => d.id !== id);
    return route.fulfill({ status: 204, headers: CORS, body: '' });
  }
  return json({ message: 'unmocked ' + m + ' ' + p }, 404);
});

// ---------- Tailscale relay mock (same origin as the page) ----------
await page.route(ORIGIN + '/api/**', async (route) => {
  const req = route.request();
  const url = new URL(req.url());
  const m = req.method();
  if (m === 'OPTIONS') return route.fulfill({ status: 204, headers: CORS, body: '' });

  if (req.headers()['authorization'] !== 'Bearer tskey-api-testtoken')
    return route.fulfill({ status: 401, headers: CORS, body: JSON.stringify({ message: 'bad ts token' }) });

  const p = url.pathname;
  const json = (o, status = 200) => route.fulfill({ status, headers: CORS, body: JSON.stringify(o) });

  if (p === '/api/v2/tailnet/-/keys' && m === 'POST') {
    const b = JSON.parse(req.postData());
    check('auth key is ephemeral', b.capabilities?.devices?.create?.ephemeral === true);
    check('auth key is preauthorized', b.capabilities?.devices?.create?.preauthorized === true);
    check('auth key is single-use', b.capabilities?.devices?.create?.reusable === false);
    const id = 'k' + Date.now();
    state.keys[id] = true;
    return json({ id, key: 'tskey-auth-FAKE' });
  }

  if (p.startsWith('/api/v2/tailnet/-/keys/') && m === 'DELETE') {
    const id = p.split('/').pop();
    delete state.keys[id];
    check('auth key revoked after create', true);
    return json({});
  }

  if (p === '/api/v2/tailnet/-/devices') {
    // Make the node show up only on the 2nd poll, exercising the wait loop.
    if (state._pending) {
      state.pollCount++;
      if (state.pollCount >= 2) {
        const dev = {
          id: 'dev' + state.devices.length, name: state._pending + '.tailnet.ts.net',
          hostname: state._pending, addresses: ['100.64.0.' + (10 + state.devices.length)],
          lastSeen: new Date().toISOString(), os: 'linux', clientVersion: '1.80.0',
        };
        state.devices.push(dev);
        state.routes[dev.id] = { advertisedRoutes: ['0.0.0.0/0', '::/0'], enabledRoutes: [] };
        state._pending = null; state.pollCount = 0;
      }
    }
    return json({ devices: state.devices });
  }

  const rm = p.match(/^\/api\/v2\/device\/([^/]+)\/routes$/);
  if (rm) {
    const id = rm[1];
    if (m === 'GET') return json(state.routes[id] || { advertisedRoutes: [], enabledRoutes: [] });
    const b = JSON.parse(req.postData());
    state.routes[id].enabledRoutes = b.routes;
    check('exit routes approved with advertised routes', b.routes.includes('0.0.0.0/0'));
    return json(state.routes[id]);
  }
  return json({ message: 'unmocked ' + m + ' ' + p }, 404);
});

// ================= drive the app =================
await page.goto(ORIGIN + '/');

check('no proxy URL field', (await page.$('#proxy')) === null);

check('login view shown first', await page.isVisible('#view-login'));
check('version stamped on the console', (await page.textContent('#ver-header')).trim() === 'v' + (await page.evaluate(() => VERSION)));
check('version stamped on the footer rail too', (await page.textContent('#ver-footer')).trim() === (await page.textContent('#ver-header')).trim());

// --- getting-started guide, reachable before you have any credentials ---
await page.click('#btn-guide-login');
await page.waitForSelector('#dlg-guide[open]');
const guide = await page.textContent('#dlg-guide');
check('guide opens from the sign-in card', /two API tokens/.test(guide));
check('guide covers both tokens', guide.includes('dop_v1_') && guide.includes('tskey-api-'));
check('guide covers using a node on devices', /Exit Node/.test(guide));
check('guide covers cost and cleanup', /per hour/.test(guide));
await page.keyboard.press('Escape');
await page.waitForTimeout(200);
check('dashboard hidden initially', await page.isHidden('#view-dash'));

// --- one combined credential field for password managers ---
check('single password input on login form', (await page.$$('#login-form input[type=password]')).length === 1);

const lastErr = async () => { const t = await page.$$('.toast.err'); return t.length ? t[t.length - 1].textContent() : ''; };

// --- malformed credentials are caught before any network call ---
await page.fill('#credentials', 'dop_v1_testtoken');
await page.click('#btn-signin');
await page.waitForSelector('.toast.err', { timeout: 5000 });
check('missing Tailscale key is reported', (await lastErr()).includes('No Tailscale API key'));

// --- bad token path ---
await page.fill('#credentials', 'wrong tskey-api-testtoken');
await page.click('#btn-signin');
await page.waitForFunction(() => [...document.querySelectorAll('.toast.err')].some((t) => t.textContent.includes('401')), null, { timeout: 5000 });
check('bad DO token surfaces a 401 error', true);
check('stays on login after bad token', await page.isVisible('#view-login'));

// --- setup helper composes the combined value ---
await page.fill('#credentials', '');
await page.click('#setup summary');
await page.fill('#ts-token', 'tskey-api-testtoken');
await page.fill('#do-token', 'dop_v1_testtoken');
check('setup helper fills Credentials', (await page.inputValue('#credentials')) === 'dop_v1_testtoken tskey-api-testtoken');

// --- good login (order doesn't matter) ---
await page.fill('#profile', 'personal');
await page.fill('#credentials', 'tskey-api-testtoken dop_v1_testtoken');
await page.click('#btn-signin');
await page.waitForSelector('#view-dash:not(.hidden)', { timeout: 5000 });
check('signs in with valid credentials', true);
await page.waitForTimeout(600);

check('empty state shown with no nodes', await page.isVisible('#nodes-empty'));
check('stats rendered', (await page.$$('#stats .stat')).length === 4);
check('no account-wide billing figures', !/balance|month to date/i.test(await page.textContent('#stats')));
check('billing endpoint never called', !state.balanceCalls);

// --- cost so far: per-second at price_hourly, $0.01 minimum, 672 h cap per calendar month ---
const cost = (d) => page.evaluate((d) => costSoFar(d, Date.UTC(2026, 8, 17, 12)), d);
const size = { price_hourly: 0.00893, price_monthly: 6 };
check('brand-new node bills the $0.01 minimum', (await cost({ size, created_at: '2026-09-17T11:59:30Z' })) === 0.01);
check('10 h node costs 10 x hourly', Math.abs((await cost({ size, created_at: '2026-09-17T02:00:00Z' })) - 0.0893) < 1e-9);
// Aug (744 h) caps at 672 h; Sep 1 -> Sep 17 12:00 is 396 h, under the cap.
check('each calendar month capped at 672 h', Math.abs((await cost({ size, created_at: '2026-08-01T00:00:00Z' })) - 0.00893 * (672 + 396)) < 1e-9);
check('falls back to monthly/672 without price_hourly', Math.abs((await cost({ size: { price_monthly: 6.72 }, created_at: '2026-09-17T10:00:00Z' })) - 0.02) < 1e-9);

// --- create a node ---
await page.click('#btn-new');
await page.waitForSelector('#dlg-create[open]');
await page.waitForTimeout(300);
const regionCount = (await page.$$('#regions .region')).length;
check('only available regions offered', regionCount === 3, `got ${regionCount}`);
check('regions sorted by slug', (await page.textContent('#regions')).indexOf('fra1') < (await page.textContent('#regions')).indexOf('nyc1'));

await page.click('#regions .region:has-text("fra1") span');
await page.click('#btn-create-go');
await page.waitForSelector('#create-log .row.ok', { timeout: 30000 });
await page.waitForFunction(() => document.querySelector('#create-log').textContent.includes('ready in'), null, { timeout: 30000 });
check('create flow completes', true);
const logText = await page.textContent('#create-log');
check('log shows auth key step', logText.includes('auth key from Tailscale'));
check('log shows provisioning step', logText.includes('fra1 datacenter'));
check('log shows tailnet wait', logText.includes('phone home'));
check('progress bar completed', await page.getAttribute('#create-bar', 'value') === '5');
await page.screenshot({ path: SHOTS + '/shot-create.png' });
check('Create disabled after a completed run', await page.isDisabled('#btn-create-go'));

await page.click('#btn-create-cancel');
await page.waitForTimeout(800);

// --- verify listing + enrichment ---
const rows = await page.$$('#nodes-body tr');
check('node appears in table', rows.length === 1, `${rows.length} rows`);
const rowText = await page.textContent('#nodes-body');
check('shows region', rowText.includes('fra1'));
check('shows public IP', rowText.includes('203.0.113.'));
check('shows tailnet IP', rowText.includes('100.64.0.'));
check('shows cost so far, not list price', rowText.includes('$0.01') && !rowText.includes('$6.00'));
check('table note lines up with the first column', await page.evaluate(() => {
  const textLeft = (el) => el.getBoundingClientRect().left + parseFloat(getComputedStyle(el).paddingLeft);
  return Math.abs(textLeft(document.querySelector('.tablenote')) - textLeft(document.querySelector('#nodes thead th'))) < 0.6;
}));
check('cost column is right-aligned', await page.evaluate(() => getComputedStyle(document.querySelector('#nodes-body td.num')).textAlign) === 'right');
check('status pill says exit node', rowText.includes('exit node'));
check('running cost stat is hourly', (await page.textContent('#stats')).includes('$0.009/hr'));

// --- reopening the dialog re-arms Create ---
await page.click('#btn-new');
await page.waitForSelector('#dlg-create[open]');
check('Create re-enabled when dialog reopens', !(await page.isDisabled('#btn-create-go')));
check('region picker visible again', await page.isVisible('#regions'));
await page.click('#btn-create-cancel');
await page.waitForTimeout(300);

// --- keyboard: j/k and arrows / d ---
const selected = async () => (await page.getAttribute('#nodes-body tr', 'aria-selected')) === 'true';
await page.keyboard.press('j');
check('j selects a row', await selected());
await page.click('#nodes-body tr td.name');
check('clicking the selected row deselects it', !(await selected()));
await page.keyboard.press('Control+k');
check('Ctrl+K is left to the browser', !(await selected()));
await page.keyboard.press('k');
check('k selects a row', await selected());
await page.click('#nodes-body tr td.name');
await page.keyboard.press('ArrowDown');
check('arrow key selects a row', await selected());
check('delete button enabled on selection', !(await page.isDisabled('#btn-delete')));

await page.keyboard.press('d');
await page.waitForSelector('#dlg-delete[open]');
check('d opens delete confirm', (await page.textContent('#delete-sub')).includes('fra1'));
await page.click('#btn-delete-go');
await page.waitForTimeout(900);
check('node deleted from table', (await page.$$('#nodes-body tr')).length === 0);
check('empty state returns', await page.isVisible('#nodes-empty'));
await page.screenshot({ path: SHOTS + '/shot-dash.png' });

// --- dark mode ---
await page.emulateMedia({ colorScheme: 'dark' });
await page.evaluate(() => { document.querySelectorAll('.toast').forEach(t => t.remove()); });
await page.click('#btn-new');
await page.waitForSelector('#dlg-create[open]');
await page.waitForTimeout(300);
await page.screenshot({ path: SHOTS + '/shot-dark.png' });
await page.click('#btn-create-cancel');
await page.waitForTimeout(300);

// --- session persistence + logout ---
await page.reload();
await page.waitForTimeout(500);
check('stays signed in across reload', await page.isVisible('#view-dash'));

// --- DISPLAY selector and theme persistence ---
const scheme = () => page.evaluate(() => document.documentElement.style.colorScheme || '');
const pressed = () => page.evaluate(() =>
  [...document.querySelectorAll('#display-sel .seg')]
    .find((b) => b.getAttribute('aria-pressed') === 'true')?.dataset.theme || null);
const reload = async () => { await page.reload(); await page.waitForTimeout(400); };
const setTheme = (t) => page.click(`#display-sel .seg[data-theme="${t}"]`);

check('DISPLAY starts on auto', (await scheme()) === '' && (await pressed()) === 'auto',
      `${await pressed()} / "${await scheme()}"`);

await setTheme('dark');
check('dark position applies dark', (await scheme()) === 'dark' && (await pressed()) === 'dark');
await reload();
check('dark survives a reload', (await scheme()) === 'dark' && (await pressed()) === 'dark',
      `${await pressed()} / "${await scheme()}"`);

await setTheme('light');
check('light position applies light', (await scheme()) === 'light' && (await pressed()) === 'light');
await reload();
check('light survives a reload', (await scheme()) === 'light' && (await pressed()) === 'light');

await setTheme('auto');
check('auto clears the inline override', (await scheme()) === '' && (await pressed()) === 'auto');
await reload();
check('auto survives a reload', (await scheme()) === '' && (await pressed()) === 'auto');

// The switch is addressable, not a cycle: re-picking the live position is a no-op.
await setTheme('dark');
await setTheme('dark');
check('re-picking a position is idempotent', (await scheme()) === 'dark' && (await pressed()) === 'dark');

// Restoring in <head> is what stops a stored choice flashing the wrong palette
// on load; assert it structurally, since a post-load read cannot prove ordering.
{
  const html = fs.readFileSync(APP, 'utf8');
  const headEnd = html.indexOf('</head>');
  const restore = html.indexOf('yvpn.theme');
  check('theme is restored inside <head>, before any paint',
        restore > 0 && headEnd > 0 && restore < headEnd);
}

await page.click('#btn-logout');
await page.waitForSelector('#view-login:not(.hidden)', { timeout: 3000 });
check('logout returns to login', await page.isVisible('#view-login'));
await page.reload();
await page.waitForTimeout(400);
check('logout clears stored credentials', await page.isVisible('#view-login'));
check('theme preference outlives sign-out', (await scheme()) === 'dark', await scheme());
check('key legends are hidden before sign-in', await page.isHidden('.keys'));

await browser.close();
srv.close();

const failed = results.filter((r) => !r.ok);
console.log(`\n${results.length - failed.length}/${results.length} checks passed`);
if (log.length) console.log('\nbrowser log:\n' + log.join('\n'));
process.exit(failed.length ? 1 : 0);
