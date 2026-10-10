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
const PARITY = path.join(HERE, '..', '..', '..', 'testdata', 'parity');
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
  // Doesn't sell the small sizes -- like the real mem1. Gets a pricier one.
  { slug: 'mem1', name: 'Memphis 1', available: true },
  // Sells only GPU droplets.
  { slug: 'atl1', name: 'Atlanta 1', available: true },
  // Sells small sizes but doesn't carry the Ubuntu image.
  { slug: 'syd9', name: 'Sydney 9', available: true },
];
const SIZES = [
  { slug: 's-1vcpu-512mb-10gb', memory: 512, vcpus: 1, disk: 10, price_hourly: 0.00595, available: true,
    price_monthly: 4, transfer: 0.5, regions: ['nyc1', 'sfo3', 'fra1', 'lon1', 'syd9'] },
  { slug: 's-1vcpu-1gb', memory: 1024, vcpus: 1, disk: 25, price_hourly: 0.00893, available: true,
    price_monthly: 6, transfer: 1, regions: ['nyc1', 'sfo3', 'fra1', 'lon1', 'syd9'] },
  { slug: 's-2vcpu-4gb', memory: 4096, vcpus: 2, disk: 80, price_hourly: 0.03571, available: true,
    price_monthly: 24, transfer: 4, regions: ['nyc1', 'fra1', 'mem1'] },
  { slug: 's-1vcpu-2gb', memory: 2048, vcpus: 1, disk: 50, price_hourly: 0.01786, available: true,
    price_monthly: 12, transfer: 2, regions: ['nyc1', 'fra1', 'mem1'] },
  // Memory enough for Jellyfin, one core, and cheaper than s-2vcpu-4gb: not enough.
  { slug: 'm-1vcpu-8gb', memory: 8192, vcpus: 1, disk: 25, price_hourly: 0.03, available: true,
    price_monthly: 20, transfer: 4, regions: ['nyc1', 'fra1', 'sfo3'] },
  // Cheaper than everything, but too little disk for the image.
  { slug: 'tiny', price_monthly: 0.5, memory: 512, disk: 5, price_hourly: 0.001, available: true, regions: ['fra1', 'mem1'] },
  // Cheaper than everything, but not for sale.
  { slug: 'gone', price_monthly: 1.5, memory: 1024, disk: 25, price_hourly: 0.002, available: false, regions: ['fra1', 'mem1'] },
  { slug: 'gpu-h100x1-80gb', price_monthly: 2277.6, memory: 245760, disk: 720, price_hourly: 3.39, available: true,
    description: 'GPU', regions: ['atl1'] },
];
const IMAGE = { slug: 'ubuntu-24-04-x64', min_disk_size: 7, regions: ['nyc1', 'sfo3', 'fra1', 'lon1', 'mem1', 'atl1'] };

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
  if (p === '/v2/sizes') return json({ sizes: SIZES });
  if (p === '/v2/images/ubuntu-24-04-x64') return json({ image: IMAGE });

  if (p === '/v2/droplets' && m === 'GET') {
    if (url.searchParams.get('tag_name') !== 'yVPN')
      return json({ message: 'test expected tag_name=yVPN' }, 500);
    return json({ droplets: state.droplets });
  }

  if (p === '/v2/droplets' && m === 'POST') {
    const b = JSON.parse(req.postData());
    state.lastCreate = b;
    check('create sends yVPN tag', b.tags?.includes('yVPN'));
    check('create sends ubuntu-24-04-x64', b.image === 'ubuntu-24-04-x64');
    if (!b.tags.some((t) => t.startsWith('yvpn-addon:'))) {
      check('create sends the cheapest usable size in the region', b.size === 's-1vcpu-512mb-10gb', b.size);
      check('cloud-init advertises exit node', b.user_data?.includes('--advertise-exit-node'));
    }
    check('cloud-init carries the auth key', b.user_data?.includes('tskey-auth-FAKE'));
    // The slow parts, removed deliberately: an apt upgrade on first boot cost ~145 s
    // and DigitalOcean's vendor script another ~60 s.
    check('cloud-init does not apt-upgrade on boot', !/^\s*package_upgrade:/m.test(b.user_data || ''));
    check('cloud-init skips DO vendor data', /vendor_data:\s*\n\s*enabled: false/.test(b.user_data || ''));
    check('cloud-init installs the static tailscale build', b.user_data?.includes('pkgs.tailscale.com'));
    const d = {
      id: ++nextId, name: b.name, status: 'active',
      memory: SIZES.find((z) => z.slug === b.size).memory,
      disk: SIZES.find((z) => z.slug === b.size).disk,
      vcpus: SIZES.find((z) => z.slug === b.size).vcpus, locked: false,
      kernel: null, tags: b.tags, features: ['ipv6', 'droplet_agent'],
      vpc_uuid: 'vpc-1234', volume_ids: [], backup_ids: [], snapshot_ids: [],
      image: { slug: 'ubuntu-24-04-x64', name: '24.04 (LTS) x64', distribution: 'Ubuntu' },
      region: { slug: b.region, name: b.region, available: true, features: ['private_networking', 'ipv6'] },
      size: SIZES.find((z) => z.slug === b.size),
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
    state._pendingExit = b.user_data.includes('--advertise-exit-node');
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
        state.routes[dev.id] = state._pendingExit
          ? { advertisedRoutes: ['0.0.0.0/0', '::/0'], enabledRoutes: [] }
          : { advertisedRoutes: [], enabledRoutes: [] };
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
    state.routesPosted = (state.routesPosted || 0) + 1;
    state.routes[id].enabledRoutes = b.routes;
    check('exit routes approved with advertised routes', b.routes.includes('0.0.0.0/0'));
    return json(state.routes[id]);
  }
  return json({ message: 'unmocked ' + m + ' ' + p }, 404);
});

// ---------- the node agent (apps/node), on a node's tailnet name ----------
// Reached straight from the browser at https://<node>.tailnet.ts.net:8443.
const agent = {
  token: null, mode: 'ok', phase: 'installing', statusPolls: 0,
  media: [], share: { enabled: false, busy: false, error: '' },
  chunks: [], uploadStarts: 0, failPatchOnce: true, failShareOnce: true, fetched: null,
};
const agentStatus = (host) => ({
  addon: 'jellyfin', version: 'test', phase: agent.phase,
  step: agent.phase === 'ready' ? 'Ready' : 'Downloading Jellyfin', url: `https://${host}/`,
  disk: { free: 70e9, total: 80e9 }, media: agent.media, share: agent.share, log: ['pulling image'],
});
await page.route((u) => u.port === '8443' && u.hostname.endsWith('.tailnet.ts.net'), async (route) => {
  const req = route.request();
  const url = new URL(req.url());
  const m = req.method();
  const H = {
    'access-control-allow-origin': '*',
    'access-control-allow-headers': 'Authorization, Content-Type, Upload-Offset',
    'access-control-allow-methods': 'GET, POST, PATCH, DELETE, OPTIONS',
    'access-control-expose-headers': 'Upload-Offset',
    'content-type': 'application/json',
  };
  if (m === 'OPTIONS') return route.fulfill({ status: 204, headers: H, body: '' });
  if (agent.mode === 'down') return route.abort('connectionrefused');
  const json = (o, status = 200) => route.fulfill({ status, headers: H, body: JSON.stringify(o) });
  if (agent.mode === 'auth' || req.headers()['authorization'] !== 'Bearer ' + agent.token)
    return json({ message: 'wrong or missing token' }, 401);
  const p = url.pathname;

  if (p === '/v1/status') {
    if (agent.phase === 'installing' && ++agent.statusPolls >= 3) agent.phase = 'ready';
    // Each poll moves every video one step along: downloaded, queued, converting, ready.
    for (const it of agent.media) {
      if (it.state === 'downloading') { it.state = 'queued'; }
      else if (it.state === 'queued') { it.state = 'preparing'; it.progress = 0.5; }
      else if (it.state === 'preparing') {
        it.state = 'ready'; it.progress = 1; it.file = it.name.replace(/\.[^.]+$/, '') + '.mp4';
      }
    }
    return json(agentStatus(url.hostname));
  }
  if (p === '/v1/uploads' && m === 'POST') {
    agent.uploadStarts++;
    const b = JSON.parse(req.postData());
    let it = agent.media.find((x) => x.state === 'uploading' && x.name === b.name && x.size === b.size);
    if (!it) {
      it = { id: 'u' + agent.media.length, name: b.name, size: b.size, received: 0, state: 'uploading', source: 'upload', progress: 0 };
      agent.media.push(it);
    }
    return json({ id: it.id, offset: it.received, name: it.name });
  }
  const up = p.match(/^\/v1\/uploads\/(\w+)$/);
  if (up && m === 'PATCH') {
    const it = agent.media.find((x) => x.id === up[1]);
    const offset = Number(req.headers()['upload-offset']);
    // The second chunk's connection drops once: the page has to ask where the
    // upload stands and carry on from there.
    if (agent.failPatchOnce && offset > 0) { agent.failPatchOnce = false; return route.abort('connectionreset'); }
    if (offset !== it.received) return json({ offset: it.received, message: 'out of step' }, 409);
    const body = req.postDataBuffer();
    agent.chunks.push([offset, body.length]);
    it.received += body.length;
    if (it.received === it.size) it.state = 'queued';
    return json({ offset: it.received });
  }
  if (p === '/v1/fetch' && m === 'POST') {
    agent.fetched = JSON.parse(req.postData()).url;
    const it = { id: 'l' + agent.media.length, name: path.basename(new URL(agent.fetched).pathname), size: 0,
                 received: 0, state: 'downloading', source: 'link', progress: 0 };
    agent.media.push(it);
    return json(it);
  }
  const del = p.match(/^\/v1\/media\/(\w+)$/);
  if (del && m === 'DELETE') {
    agent.media = agent.media.filter((x) => x.id !== del[1]);
    return route.fulfill({ status: 204, headers: H, body: '' });
  }
  if (p === '/v1/share' && m === 'POST') {
    const { enabled } = JSON.parse(req.postData());
    if (enabled && agent.failShareOnce) {
      agent.failShareOnce = false;
      agent.share.error = "Tailscale won't open Funnel for this node. Give it the \"funnel\" node attribute, " +
        "or open https://login.tailscale.com/f/funnel?node=nABC123 as a tailnet admin.";
      return json({ message: agent.share.error, status: agentStatus(url.hostname) }, 502);
    }
    agent.share = { enabled, busy: false, error: '' };
    return json(agentStatus(url.hostname));
  }
  return json({ message: 'unmocked ' + m + ' ' + p }, 404);
});

// ================= drive the app =================
await page.goto(ORIGIN + '/');

// --- parity with the CLI: the same golden files apps/cli/pkg/addons checks ---
{
  const golden = (f) => fs.readFileSync(path.join(PARITY, f), 'utf8');
  const tok = 'dop_v1_parity', name = 'fra1-yvpn-1700000000';
  check('add-on catalog matches the CLI',
    JSON.stringify(await page.evaluate(() => ADDONS)) === JSON.stringify(JSON.parse(golden('addons.json'))));
  check('derived secrets match the CLI',
    JSON.stringify(await page.evaluate(([t, n]) => deriveSecrets(t, n), [tok, name])) ===
    JSON.stringify(JSON.parse(golden('secrets.json'))));
  const render = (p) => page.evaluate(async ([t, n, p]) => {
    const s = await deriveSecrets(t, n);
    return cloudInit({ ...p, nodeConfig: p.addon ? nodeConfig(p.addon, s) : undefined });
  }, [tok, name, p]);
  const base = { authKey: 'tskey-auth-PARITY', version: '9.9.9' };
  check('plain exit node cloud-init matches the CLI', (await render({ ...base, exit: true, addon: '' })) === golden('cloudinit-exit.yaml'));
  check('jellyfin + exit cloud-init matches the CLI', (await render({ ...base, exit: true, addon: 'jellyfin' })) === golden('cloudinit-jellyfin-exit.yaml'));
  check('jellyfin-only cloud-init matches the CLI', (await render({ ...base, exit: false, addon: 'jellyfin' })) === golden('cloudinit-jellyfin.yaml'));
}

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
check('guide covers cost and cleanup', /a month/.test(guide));
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
check('only regions that can run a node offered', regionCount === 4, `got ${regionCount}`);
const regionText = await page.textContent('#regions');
check('no GPU-only, image-less or unavailable regions', !/atl1|syd9|lon1/.test(regionText));
check('region shows its cheapest monthly price', (await page.textContent('#regions .region:has-text("fra1")')).includes('$4/mo'));
check('region without small sizes falls back to the next cheapest', (await page.textContent('#regions .region:has-text("mem1")')).includes('$12/mo'));
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
      (await page.textContent('#nodes-body tr.detail')).includes('$4.00'));
check('table note lines up with the first column', await page.evaluate(() => {
  const textLeft = (el) => el.getBoundingClientRect().left + parseFloat(getComputedStyle(el).paddingLeft);
  return Math.abs(textLeft(document.querySelector('.tablenote')) - textLeft(document.querySelector('#nodes thead th'))) < 0.6;
}));
check('cost column is right-aligned', await page.evaluate(() => getComputedStyle(document.querySelector('#nodes-body td.num')).textAlign) === 'right');
check('status pill says exit node', rowText.includes('exit node'));
const statsText = async () => (await page.textContent('#stats')).replace(/\s+/g, ' ');
check('rate stat is monthly by default', (await statsText()).includes('$4/mo'), await statsText());
check('stats use the clearer labels', /Current rate/i.test(await statsText()) && /Cost so far/i.test(await statsText()) && !/Running cost|Spent so far/i.test(await statsText()));
check('rate says what it assumes', (await statsText()).includes('if this node keeps running'));
await page.click('#rate-unit');
check('clicking the unit switches to hourly', (await statsText()).includes('$0.006/hr'), await statsText());
check('hourly choice is remembered', (await page.evaluate(() => localStorage.getItem('yvpn.rate'))) === 'hr');
check('unit toggle keeps focus', await page.evaluate(() => document.activeElement?.id === 'rate-unit'));
await page.keyboard.press('Enter');
check('toggles back to monthly from the keyboard', (await statsText()).includes('$4/mo'));
// Enter belongs to the focused button, so let go of it before the row-keyboard checks.
await page.evaluate(() => document.activeElement?.blur());

// --- the open row: everything both APIs will tell us ---
check('tailnet devices requested with fields=all', !!state.allFieldsAsked);
const detail = (await page.textContent('#nodes-body tr.detail')).replace(/\s+/g, ' ');
for (const [what, needle] of [
  ['droplet image', 'ubuntu-24-04-x64'],
  ['memory', '512 MB'],
  ['disk', '10 GB'],
  ['monthly transfer', '0.5 TB/mo'],
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
  ['the hourly rate', '$0.00595'],
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

// ================= add-on: a Jellyfin watch party =================
await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: ORIGIN });
const clip = () => page.evaluate(() => navigator.clipboard.readText());
const card = '#nodes-body .card-addon';
const cardText = async () => (await page.textContent(card)).replace(/\s+/g, ' ');
const waitCard = (re, timeout = 20000) =>
  page.waitForFunction((src) => new RegExp(src).test(document.querySelector('#nodes-body .card-addon')?.textContent.replace(/\s+/g, ' ') || ''),
    re.source, { timeout });

await page.click('#btn-new');
await page.waitForSelector('#dlg-create[open]');
await page.waitForTimeout(200);
check('create dialog offers the add-on catalog', (await page.$$('#addons label')).length === 2);
check('a plain exit node is the default', await page.isChecked('#addons input[value=""]'));
check('a plain node is held to being an exit node', (await page.isChecked('#exit')) && (await page.isDisabled('#exit')));
await page.click('#addons label:has-text("Jellyfin") span');
const jfRegions = await page.$$eval('#regions .region b', (els) => els.map((e) => e.textContent));
check('regions narrow to those selling a size big enough for Jellyfin',
      JSON.stringify(jfRegions) === JSON.stringify(['fra1', 'mem1', 'nyc1']), jfRegions.join(','));
check('one core is not enough, however much memory', !jfRegions.includes('sfo3'));
check('prices follow the add-on', (await page.textContent('#regions .region:has-text("fra1")')).includes('$24/mo'));
check('each region names the size it will get', (await page.textContent('#regions .region:has-text("fra1")')).includes('s-2vcpu-4gb'));
check('the dialog says what it is priced for', /Jellyfin/.test(await page.textContent('#size-hint')));
check('an add-on node may skip being an exit node', !(await page.isDisabled('#exit')));
await page.click('#addons label:has-text("Exit node") span');
check('back to a plain node, prices drop again', (await page.textContent('#regions .region:has-text("fra1")')).includes('$4/mo'));
check('and it is held to being an exit node again', (await page.isChecked('#exit')) && (await page.isDisabled('#exit')));
await page.click('#addons label:has-text("Jellyfin") span');
await page.uncheck('#exit');
await page.click('#regions .region:has-text("fra1") span');
await page.screenshot({ path: SHOTS + '/shot-create-addon.png' });
const routesBefore = state.routesPosted || 0;
await page.click('#btn-create-go');
await page.waitForSelector('#nodes-body tr.pending', { timeout: 5000 });
check('the building row says what it will run', (await page.textContent('#nodes-body tr.pending .badge')) === 'jellyfin');
await page.waitForFunction(() => document.querySelectorAll('#nodes-body tr.pending').length === 0, null, { timeout: 30000 });

{
  const b = state.lastCreate;
  const sec = await page.evaluate((n) => deriveSecrets('dop_v1_testtoken', n), b.name);
  agent.token = sec.agentToken;
  check('add-on node is tagged with it', b.tags.includes('yvpn-addon:jellyfin'));
  check('add-on node gets the size it needs', b.size === 's-2vcpu-4gb', b.size);
  check('a non-exit node neither forwards nor advertises', !b.user_data.includes('--advertise-exit-node') && !b.user_data.includes('ip_forward'));
  check('the agent comes from this release', b.user_data.includes(`releases/download/v${await page.evaluate(() => VERSION)}/yvpn-node-linux-amd64`));
  check("the agent's config carries the derived token", b.user_data.includes(`"token":"${sec.agentToken}"`));
  check("the agent's config is a private file", b.user_data.includes("permissions: '0600'"));
  check('the DigitalOcean token never goes to the node', !b.user_data.includes('dop_v1_testtoken'));
  check('no exit routes approved for a node that is not an exit node', (state.routesPosted || 0) === routesBefore);
  check('a non-exit add-on node reads as online', (await page.textContent('#nodes-body tr[data-key] .pill')).includes('online'));
  check('the row carries the add-on badge', (await page.textContent('#nodes-body tr[data-key] .badge')) === 'jellyfin');

  // Open it: the card first, the API detail folded underneath.
  await page.click('#nodes-body tr[data-key] td.name');
  await page.waitForSelector(card, { timeout: 5000 });
  check('an add-on row opens on its card', true);
  check('node details are folded away', !(await page.getAttribute('#nodes-body details.more', 'open')));
  check('the folded details still hold the droplet', (await page.textContent('#nodes-body details.more')).includes('ubuntu-24-04-x64'));
  await waitCard(/Downloading Jellyfin/);
  check('setup progress is shown while it installs', /Installing/.test(await cardText()));
  check('videos wait for Jellyfin', await page.getAttribute(card + ' .drop', 'aria-disabled') === 'true');
  await waitCard(/Open Jellyfin/);
  const host = b.name + '.tailnet.ts.net';
  check('card links to Jellyfin on the tailnet', await page.getAttribute(card + ' a[href^="https://"]', 'href') === `https://${host}/`);
  check('admin password starts hidden', !(await cardText()).includes(sec.adminPassword));
  await page.click(card + ' [data-act=reveal]');
  check('admin password can be shown', (await cardText()).includes(sec.adminPassword));
  await page.click(card + ' [data-copy=admin]');
  check('admin password can be copied', (await clip()) === sec.adminPassword);
  check('free disk is reported', /GB free on the node/.test(await cardText()));
  check('the card warns videos go with the node', /deleted with the node/.test(await cardText()));
  await page.screenshot({ path: SHOTS + '/shot-card-ready.png', fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(200);
  check('the open card fits a phone without scrolling sideways', await page.evaluate(() => {
    const wrap = document.querySelector('.tablewrap').getBoundingClientRect();
    const box = document.querySelector('.addon-detail').getBoundingClientRect();
    return box.left >= wrap.left - 1 && box.right <= wrap.right + 1 &&
      document.querySelector('.screen-scroll').scrollWidth <= document.querySelector('.screen-scroll').clientWidth + 1;
  }));
  await page.screenshot({ path: SHOTS + '/shot-card-phone.png' });
  await page.setViewportSize({ width: 1280, height: 720 });

  // --- a 36 MiB video: three chunks, and the second one's connection drops ---
  const SIZE = 36 * 1024 * 1024;
  await page.setInputFiles(card + ' input[type=file]', { name: 'Family Holiday.mkv', mimeType: 'video/x-matroska', buffer: Buffer.alloc(SIZE, 7) });
  await waitCard(/Ready to watch/, 40000);
  const sent = agent.chunks.reduce((n, [, len]) => n + len, 0);
  check('the whole file arrives', sent === SIZE, `${sent} of ${SIZE}`);
  check('it goes in 16 MiB chunks', agent.chunks.every(([, len]) => len <= 16 * 1024 * 1024) && agent.chunks.length === 3,
        JSON.stringify(agent.chunks));
  check('a dropped connection resumes where the node says it stands', agent.uploadStarts === 2 && agent.chunks[1][0] === 16 * 1024 * 1024);
  check('the converted name is shown', (await cardText()).includes('Family Holiday.mp4'));

  // --- a link instead ---
  await page.fill(card + ' .ca-link input', 'https://example.com/films/Birthday.mp4');
  await page.click(card + ' .ca-link button');
  await page.waitForFunction(() => !document.querySelector('#nodes-body .card-addon .ca-link input').value, null, { timeout: 5000 });
  check('a pasted link is sent to the node', agent.fetched === 'https://example.com/films/Birthday.mp4');
  await waitCard(/Birthday\.mp4.*(Downloading|Waiting|Converting|Ready)/);
  check('the link shows up in the list', true);

  // --- the table repaints under the card without losing what is typed in it ---
  await page.fill(card + ' .ca-link input', 'https://half-typ');
  await page.focus(card + ' .ca-link input');
  await page.evaluate(() => refresh());
  check('a repaint keeps a half-typed link', (await page.inputValue(card + ' .ca-link input')) === 'https://half-typ');
  check('a repaint keeps focus in the card', await page.evaluate(() => document.activeElement?.matches('.card-addon .ca-link input')));
  await page.fill(card + ' .ca-link input', '');
  await page.evaluate(() => document.activeElement?.blur());

  // --- sharing: refused by the tailnet once, then on ---
  check('sharing starts off', /Only your tailnet can open Jellyfin/.test(await cardText()));
  await page.click(card + ' [data-act=share][data-on="1"]');
  await waitCard(/funnel" node attribute/);
  check("the tailnet's refusal is explained in the card", true);
  check("Tailscale's link to allow it is clickable",
        (await page.getAttribute(card + ' p.err a', 'href')) === 'https://login.tailscale.com/f/funnel?node=nABC123');
  check('a refused share stays off', await page.getAttribute(card + ' [data-act=share][data-on="0"]', 'aria-pressed') === 'true');
  await page.click(card + ' [data-act=share][data-on="1"]');
  await waitCard(/Guest link/);
  check('sharing on shows the guest login', (await cardText()).includes(sec.guestPassword));
  check('the state says it is shared', /shared with guests/.test(await page.textContent(card + ' .ca-head')));
  await page.click(card + ' [data-act=invite]');
  const invite = await clip();
  check('the invite has the link, the login and SyncPlay',
        invite.includes(`https://${host}/`) && invite.includes(sec.guestPassword) && invite.includes('"guest"') && /SyncPlay/.test(invite));
  await page.screenshot({ path: SHOTS + '/shot-card-shared.png', fullPage: true });
  await page.click(card + ' [data-act=share][data-on="0"]');
  await waitCard(/Only your tailnet can open Jellyfin/);
  check('sharing can be turned off again', agent.share.enabled === false);

  // --- removing a finished video takes two clicks ---
  const holiday = agent.media.find((x) => x.name === 'Family Holiday.mkv');
  await page.click(`${card} [data-act=remove][data-id="${holiday.id}"]`);
  check('the first click only asks', agent.media.some((x) => x.id === holiday.id) &&
        (await page.textContent(`${card} [data-act=remove][data-id="${holiday.id}"]`)) === 'remove it?');
  await page.click(`${card} [data-act=remove][data-id="${holiday.id}"]`);
  await page.waitForFunction(() => !/Family Holiday/.test(document.querySelector('#nodes-body .card-addon').textContent), null, { timeout: 5000 });
  check('the second click removes it', !agent.media.some((x) => x.id === holiday.id));

  // --- the ways it can't be reached ---
  agent.mode = 'auth';
  await page.evaluate(() => agents.forEach((a) => { a.next = 0; }));
  await waitCard(/different DigitalOcean token/);
  check('a node made with another token says so', true);
  agent.mode = 'down';
  state.droplets.find((x) => x.name === b.name).created_at = new Date(Date.now() - 10 * 60e3).toISOString();
  await page.evaluate(() => refresh());
  await page.evaluate(() => agents.forEach((a) => { a.next = 0; }));
  await waitCard(/MagicDNS/);
  check('an unreachable node explains what the browser needs', /HTTPS certificates/.test(await cardText()));
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.waitForTimeout(300);   // let the palette's transitions finish
  await page.screenshot({ path: SHOTS + '/shot-card-unreachable-dark.png', fullPage: true });
  await page.emulateMedia({ colorScheme: 'light' });
  agent.mode = 'ok';

  // --- deleting it says the videos go too ---
  await page.click('#btn-delete');
  await page.waitForSelector('#dlg-delete[open]');
  check('deleting an add-on node warns about its videos', /videos/.test(await page.textContent('#dlg-delete')));
  await page.click('#btn-delete-go');
  await page.waitForFunction(() => !document.querySelector('#nodes-body .card-addon'), null, { timeout: 5000 });
  check('the node and its card are gone', state.droplets.length === 0);
}

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
