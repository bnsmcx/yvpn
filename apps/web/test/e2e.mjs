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
      memory: 1024, vcpus: 1, disk: 25, locked: false,
      kernel: null, tags: ['yVPN'], features: ['ipv6', 'droplet_agent'],
      vpc_uuid: 'vpc-1234', volume_ids: [], backup_ids: [], snapshot_ids: [],
      image: { slug: 'ubuntu-24-04-x64', name: '24.04 (LTS) x64', distribution: 'Ubuntu' },
      region: { slug: b.region, name: b.region, available: true, features: ['private_networking', 'ipv6'] },
      size: { slug: 's-1vcpu-1gb', price_monthly: 6, price_hourly: 0.00893, transfer: 1 },
      created_at: new Date().toISOString(),
      networks: {
        v4: [
          { type: 'public', ip_address: '203.0.113.' + (nextId % 250), netmask: '255.255.240.0', gateway: '203.0.112.1' },
          { type: 'private', ip_address: '10.114.0.5', netmask: '255.255.240.0', gateway: '10.114.0.1' },
        ],
        v6: [{ type: 'public', ip_address: '2604:a880::1', netmask: 64, gateway: '2604:a880::1' }],
      },
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
    if (url.searchParams.get('fields') === 'all') state.allFieldsAsked = true;
    // holdDevice keeps a node stuck on "joining tailnet", so a test can take its
    // time cancelling one mid-build.
    if (state.holdDevice) return json({ devices: state.devices });
    // Make the node show up only on the 2nd poll, exercising the wait loop.
    if (state._pending) {
      state.pollCount++;
      if (state.pollCount >= 2) {
        const dev = {
          id: 'dev' + state.devices.length, nodeId: 'nodeabc' + state.devices.length,
          name: state._pending + '.tailnet.ts.net',
          hostname: state._pending, addresses: ['100.64.0.' + (10 + state.devices.length), 'fd7a::1'],
          lastSeen: new Date().toISOString(), os: 'linux', clientVersion: '1.80.0',
          user: 'ben@example.com', created: new Date().toISOString(),
          keyExpiryDisabled: false, expires: '2026-12-01T00:00:00Z',
          authorized: true, isExternal: false, blocksIncomingConnections: false,
          updateAvailable: false, tags: [],
          // fields=all only
          clientConnectivity: {
            endpoints: ['203.0.113.7:41641'],
            derp: '',
            mappingVariesByDestIP: false,
            latency: { 'Frankfurt': { preferred: true, latencyMs: 12.5 }, 'London': { latencyMs: 24.9 } },
          },
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
const guide = (await page.textContent('#dlg-guide')).replace(/\s+/g, ' ');  // copy wraps mid-link
check('guide opens from the sign-in card', /You need two tokens/.test(guide));
check('guide points at signups for both services', /Sign up for DigitalOcean/.test(guide) && /Sign up for Tailscale/.test(guide));
check('the DigitalOcean signup is the referral link', await page.getAttribute('#dlg-guide a[href*="m.do.co"]', 'href') === 'https://m.do.co/c/2606e10dfcb8');
check('the referral link is disclosed as one', /\(referral link\)/.test(guide));
check('guide says how to check it worked', /ifconfig\.me/.test(guide));
check('guide ends with troubleshooting', /When something looks wrong/.test(guide) &&
  guide.indexOf('When something looks wrong') > guide.indexOf('Connecting a device to it'));
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
await page.screenshot({ path: SHOTS + '/shot-create.png' });
// Hold the node on the tailnet poll so the building row can be inspected; the
// mock's own two-poll delay still runs once it is released.
state.holdDevice = true;
await page.click('#btn-create-go');

// The dialog gets out of the way immediately; the node builds in the table.
await page.waitForFunction(() => !document.querySelector('#dlg-create').open, null, { timeout: 5000 });
check('create dialog closes on Create', true);
check('no progress modal left behind', (await page.$('#create-log')) === null && (await page.$('#create-bar')) === null);
await page.waitForSelector('#nodes-body tr.pending', { timeout: 5000 });
check('a pending row appears at once', true);
check('pending row names the datacenter', (await page.textContent('#nodes-body tr.pending')).includes('fra1'));
check('pending row reports a live phase', /requesting key|provisioning|booting|joining tailnet/.test(
  await page.textContent('#nodes-body tr.pending .pill.busy')));
check('pending row counts seconds', /·\s*\d+s/.test(await page.textContent('#nodes-body tr.pending .elapsed')));

// Opening the row shows the build log the modal used to own.
await page.click('#nodes-body tr.pending td.name');
await page.waitForSelector('#nodes-body tr.detail .log', { timeout: 5000 });
const buildLog = await page.textContent('#nodes-body tr.detail .log');
check('open row carries the build log', buildLog.includes('auth key from Tailscale'));
check('log shows provisioning step', buildLog.includes('fra1 datacenter'));
check('a build in flight offers to roll back', await page.isVisible('#nodes-body [data-cancel]'));
check('the building row reports it is still waiting', /joining tailnet/.test(
  await page.textContent('#nodes-body tr.pending .pill.busy')));
await page.screenshot({ path: SHOTS + '/shot-building.png' });

// …and the row graduates into an ordinary node when the build is done.
state.holdDevice = false;
await page.waitForFunction(
  () => document.querySelectorAll('#nodes-body tr.pending').length === 0,
  null, { timeout: 30000 });
check('create flow completes', true);
check('the row graduates in place', (await page.$$('#nodes-body tr[data-key]')).length === 1);
check('finished row shows a normal status pill', (await page.textContent('#nodes-body tr[data-key] .pill')).includes('exit node'));
check('no build log left on a finished node', (await page.$('#nodes-body tr.detail .log')) === null);
check('the open row stayed open', await page.getAttribute('#nodes-body tr[data-key]', 'aria-expanded') === 'true');

// --- verify listing + enrichment ---
const rows = await page.$$('#nodes-body tr[data-key]');
check('node appears in table', rows.length === 1, `${rows.length} rows`);
// The open panel is a row of its own, so read the node's own row, not the tbody.
const rowText = await page.textContent('#nodes-body tr[data-key]');
check('shows region', rowText.includes('fra1'));
check('shows public IP', rowText.includes('203.0.113.'));
check('shows tailnet IP', rowText.includes('100.64.0.'));
check('shows cost so far, not list price', rowText.includes('$0.01') && !rowText.includes('$6.00'));
check('the list price is in the open panel instead',
      (await page.textContent('#nodes-body tr.detail')).includes('$6.00'));
check('table note lines up with the first column', await page.evaluate(() => {
  const textLeft = (el) => el.getBoundingClientRect().left + parseFloat(getComputedStyle(el).paddingLeft);
  return Math.abs(textLeft(document.querySelector('.tablenote')) - textLeft(document.querySelector('#nodes thead th'))) < 0.6;
}));
check('cost column is right-aligned', await page.evaluate(() => getComputedStyle(document.querySelector('#nodes-body td.num')).textAlign) === 'right');
check('status pill says exit node', rowText.includes('exit node'));
check('running cost stat is hourly', (await page.textContent('#stats')).includes('$0.009/hr'));

// --- the open row: everything both APIs will tell us ---
check('tailnet devices requested with fields=all', !!state.allFieldsAsked);
const detail = (await page.textContent('#nodes-body tr.detail')).replace(/\s+/g, ' ');
for (const [what, needle] of [
  ['droplet image', 'ubuntu-24-04-x64'],
  ['vCPUs and memory', '1 GB'],
  ['disk', '25 GB'],
  ['monthly transfer', '1 TB/mo'],
  ['droplet features', 'droplet_agent'],
  ['the VPC', 'vpc-1234'],
  ['uptime', 'Uptime'],
  ['region features', 'private_networking'],
  ['the public netmask', '255.255.240.0'],
  ['the gateway', '203.0.112.1'],
  ['the private address', '10.114.0.5'],
  ['IPv6', '2604:a880::1'],
  ['the tailnet address', '100.64.0.'],
  ['the tailscale client version', '1.80.0'],
  ['who owns the machine', 'ben@example.com'],
  ['the node id', 'nodeabc'],
  ['key expiry', 'Key expiry'],
  ['advertised routes', '0.0.0.0/0'],
  ['the preferred relay', 'Frankfurt'],
  ['relay latency', '13ms'],
  ['the tailnet endpoint', '203.0.113.7:41641'],
  ['the hourly rate', '$0.00893'],
  ['cost so far', 'Cost so far'],
]) check('open row shows ' + what, detail.includes(needle), needle);
check('open row explains the missing metrics', /monitoring agent/.test(detail));
check('detail panel spans the table', await page.getAttribute('#nodes-body tr.detail td', 'colspan') === '9');
await page.screenshot({ path: SHOTS + '/shot-detail.png' });

// It closes again, from the keyboard as well as the pointer.
await page.keyboard.press('Enter');
check('Enter closes the open row', (await page.$('#nodes-body tr.detail')) === null);
await page.keyboard.press('Enter');
check('Enter opens it again', (await page.$('#nodes-body tr.detail')) !== null);
check('only the open row is in the DOM', (await page.$$('#nodes-body tr')).length === 2);
await page.keyboard.press('Enter');

// --- reopening the dialog still offers the picker ---
await page.click('#btn-new');
await page.waitForSelector('#dlg-create[open]');
check('Create is armed when the dialog reopens', !(await page.isDisabled('#btn-create-go')));
check('region picker visible again', await page.isVisible('#regions'));
await page.click('#btn-create-cancel');
await page.waitForTimeout(300);

// --- keyboard: j/k and arrows / Esc / d ---
const selected = async () => (await page.getAttribute('#nodes-body tr', 'aria-selected')) === 'true';
await page.keyboard.press('j');
check('j selects a row', await selected());
check('j alone does not open the row', (await page.$('#nodes-body tr.detail')) === null);
await page.keyboard.press('Escape');
check('Escape clears the selection', !(await selected()));
await page.keyboard.press('Control+k');
check('Ctrl+K is left to the browser', !(await selected()));
await page.keyboard.press('k');
check('k selects a row', await selected());
await page.keyboard.press('Escape');
await page.keyboard.press('ArrowDown');
check('arrow key selects a row', await selected());
check('delete button enabled on selection', !(await page.isDisabled('#btn-delete')));

await page.keyboard.press('d');
await page.waitForSelector('#dlg-delete[open]');
check('d opens delete confirm on a finished node', (await page.textContent('#delete-sub')).includes('fra1'));
await page.click('#btn-delete-go');
await page.waitForTimeout(900);
check('node deleted from table', (await page.$$('#nodes-body tr')).length === 0);
check('empty state returns', await page.isVisible('#nodes-empty'));
await page.screenshot({ path: SHOTS + '/shot-dash.png' });

// --- cancelling a build rolls it back, and the row says so until dismissed ---
state.holdDevice = true;
await page.click('#btn-new');
await page.waitForSelector('#dlg-create[open]');
await page.waitForTimeout(200);
await page.click('#regions .region:has-text("nyc1") span');
await page.click('#btn-create-go');
await page.waitForSelector('#nodes-body tr.pending', { timeout: 5000 });
check('a second node can be started from the table', true);
await page.click('#nodes-body tr.pending td.name');
// Wait until the droplet exists — the build only reaches the tailnet poll once
// it does — so the rollback has something to roll back.
await page.waitForFunction(
  () => /joining tailnet/.test(document.querySelector('#nodes-body tr.pending')?.textContent || ''),
  null, { timeout: 15000 });
check('the building row fills in its droplet columns',
      (await page.textContent('#nodes-body tr.pending')).includes(String(state.droplets[0].id)));
check('the building row keeps its panel open as it adopts the droplet',
      (await page.$('#nodes-body tr.detail .log')) !== null);
await page.click('#nodes-body [data-cancel]');
await page.waitForSelector('#nodes-body tr.pending.failed', { timeout: 15000 });
check('a cancelled build says so in its row', (await page.textContent('#nodes-body tr.pending.failed')).includes('cancelled'));
check('cancelling rolls the droplet back', state.droplets.length === 0);
const failedLog = await page.textContent('#nodes-body tr.detail .log');
check('the failed row keeps its log', failedLog.includes('Cancelled.'));
check('a failed row can be dismissed', await page.isVisible('#nodes-body [data-dismiss]'));
await page.click('#nodes-body [data-dismiss]');
await page.waitForTimeout(300);
check('dismissing clears the row', (await page.$$('#nodes-body tr')).length === 0);
check('empty state returns after a dismissed failure', await page.isVisible('#nodes-empty'));
state.holdDevice = false;
state._pending = null;
state.pollCount = 0;

// --- dark mode ---
await page.emulateMedia({ colorScheme: 'dark' });
await page.evaluate(() => { document.querySelectorAll('.toast').forEach(t => t.remove()); });
await page.click('#btn-new');
await page.waitForSelector('#dlg-create[open]');
await page.waitForTimeout(300);
await page.screenshot({ path: SHOTS + '/shot-dark.png' });
await page.click('#btn-create-cancel');
await page.waitForTimeout(300);
check('no stray toasts block the table', true);

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
