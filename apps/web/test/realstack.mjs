/**
 * Real-stack test for the watch party: the real web app, the real node agent
 * (apps/node), a real Jellyfin in Docker and real ffmpeg. A real video is
 * uploaded through the page, converted, and checked in Jellyfin's own library;
 * sharing is turned on and off and checked against Jellyfin's sign-in.
 *
 * What it can't have here is mocked: DigitalOcean's and Tailscale's APIs (as in
 * e2e.mjs), and the `tailscale` and `iptables` commands the agent runs, which
 * are replaced by stubs on its PATH. The node's tailnet address is routed to
 * the agent on 127.0.0.1:8090.
 *
 * Needs Docker, Go and ffmpeg, plus Playwright as for e2e.mjs; uses ports 8096
 * (Jellyfin), 8090 (the agent) and 8898 (the page), and leaves nothing behind.
 *
 *   node apps/web/test/realstack.mjs
 */
import { chromium } from 'playwright';
import { execFileSync, spawn } from 'node:child_process';
import http from 'node:http';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const APP = path.join(HERE, '..', 'index.html');
const NODE_SRC = path.join(HERE, '..', '..', 'node');
const RS = fs.mkdtempSync(path.join(os.tmpdir(), 'yvpn-realstack-'));
const NAME = 'fra1-yvpn-1700000000', HOST = NAME + '.tail-rs.ts.net';
const DO_TOKEN = 'dop_v1_testtoken';
const AGENT = 'http://127.0.0.1:8090', JF = 'http://127.0.0.1:8096';
const ORIGIN = 'http://127.0.0.1:8898';
const CONTAINER = 'yvpn-jellyfin';

const results = [];
const check = (n, c, x = '') => { results.push(!!c); console.log(`${c ? 'PASS' : 'FAIL'}  ${n}${x ? '  — ' + x : ''}`); };
const run = (cmd, args, opts = {}) => execFileSync(cmd, args, { stdio: ['ignore', 'pipe', 'inherit'], ...opts });

/* ------------------------------ the stage ------------------------------ */

for (const d of ['bin', 'ts', 'files', 'data', 'media']) fs.mkdirSync(path.join(RS, d));
// `tailscale`: answers status, and keeps the funnel flag the way tailscaled would.
fs.writeFileSync(path.join(RS, 'bin', 'tailscale'), `#!/bin/bash
D=${RS}/ts
echo "$*" >> $D/calls.log
case "$1 $2" in
  "status --json") echo '{"BackendState":"Running","Self":{"DNSName":"${HOST}."}}' ;;
  "funnel status") if [ -f $D/funnel ]; then echo '{"AllowFunnel":{"${HOST}:443":true}}'; else echo '{}'; fi ;;
  *)
    if [ "$1" = funnel ]; then
      if [[ "$*" == *" off"* ]]; then rm -f $D/funnel; else touch $D/funnel; fi
    elif [ "$1" = serve ] && [[ "$*" == *--https=443* ]]; then rm -f $D/funnel; fi ;;
esac
`, { mode: 0o755 });
// `iptables`: this machine's firewall is not the test's to change.
fs.writeFileSync(path.join(RS, 'bin', 'iptables'), '#!/bin/bash\n[ "$1" = "-C" ] && exit 1\nexit 0\n', { mode: 0o755 });

// A 34 MB MPEG-4 Part 2 / MP3 file (three upload chunks, and a full conversion),
// and a small H.264/AAC clip that should be copied rather than converted.
const ff = (...a) => run('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-y', ...a]);
ff('-f', 'lavfi', '-i', 'testsrc2=size=1280x720:rate=30', '-f', 'lavfi', '-i', 'sine=frequency=330', '-t', '20',
   '-c:v', 'mpeg4', '-q:v', '1', '-c:a', 'libmp3lame', path.join(RS, 'files', "Grandma's 80th.mkv"));
ff('-f', 'lavfi', '-i', 'testsrc=size=640x360:rate=25', '-f', 'lavfi', '-i', 'sine=frequency=550', '-t', '6',
   '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-c:a', 'aac', path.join(RS, 'files', 'clip.mp4'));
run('go', ['build', '-o', path.join(RS, 'yvpn-node'), '.'], { cwd: NODE_SRC });
try { run('docker', ['rm', '-f', CONTAINER], { stdio: 'ignore' }); } catch {}

const srv = http.createServer((req, res) => {
  if (req.url.startsWith('/files/')) {
    const f = path.join(RS, 'files', path.basename(req.url));
    res.writeHead(200, { 'content-type': 'video/mp4', 'content-length': fs.statSync(f).size });
    return fs.createReadStream(f).pipe(res);
  }
  res.writeHead(200, { 'content-type': 'text/html' });
  res.end(fs.readFileSync(APP));
}).listen(8898);

const browser = await chromium.launch();
const page = await browser.newPage();
await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: ORIGIN });
page.on('pageerror', (e) => check('no page errors', false, e.message));
await page.goto(ORIGIN + '/');

// The node's config, made by the page's own code: what a create would write.
const cfg = await page.evaluate(async ([tok, name]) =>
  JSON.parse(nodeConfig('jellyfin', await deriveSecrets(tok, name))), [DO_TOKEN, NAME]);
fs.writeFileSync(path.join(RS, 'node.json'),
  JSON.stringify({ ...cfg, dataDir: path.join(RS, 'data'), mediaDir: path.join(RS, 'media') }));
const agentLog = fs.openSync(path.join(RS, 'agent.log'), 'w');
const agentProc = spawn(path.join(RS, 'yvpn-node'), ['serve', '-config', path.join(RS, 'node.json')], {
  stdio: ['ignore', agentLog, agentLog],
  env: { ...process.env, PATH: path.join(RS, 'bin') + ':' + process.env.PATH },
});

/* ------------------------------- mocks -------------------------------- */

const CORS = { 'access-control-allow-origin': '*', 'access-control-allow-headers': 'Authorization, Content-Type',
  'access-control-allow-methods': 'GET, POST, DELETE, OPTIONS', 'content-type': 'application/json' };
const droplet = {
  id: 4242, name: NAME, status: 'active', memory: 4096, vcpus: 2, disk: 80,
  created_at: new Date(Date.now() - 60e3).toISOString(),
  tags: ['yVPN', 'yvpn-addon:jellyfin'], region: { slug: 'fra1', name: 'Frankfurt 1' },
  size: { slug: 's-2vcpu-4gb', price_hourly: 0.03571, price_monthly: 24 },
  image: { slug: 'ubuntu-24-04-x64' }, networks: { v4: [{ type: 'public', ip_address: '203.0.113.9' }], v6: [] },
};
await page.route('https://api.digitalocean.com/**', (route) => {
  const p = new URL(route.request().url()).pathname;
  if (route.request().method() === 'OPTIONS') return route.fulfill({ status: 204, headers: CORS, body: '' });
  const json = (o) => route.fulfill({ status: 200, headers: CORS, body: JSON.stringify(o) });
  if (p === '/v2/account') return json({ account: {} });
  if (p === '/v2/regions') return json({ regions: [] });
  if (p === '/v2/sizes') return json({ sizes: [] });
  if (p.startsWith('/v2/images')) return json({ image: { regions: [] } });
  if (p === '/v2/droplets') return json({ droplets: [droplet] });
  return route.fulfill({ status: 404, headers: CORS, body: '{}' });
});
await page.route(ORIGIN + '/api/**', (route) => {
  const p = new URL(route.request().url()).pathname;
  const json = (o) => route.fulfill({ status: 200, headers: CORS, body: JSON.stringify(o) });
  if (p.endsWith('/devices'))
    return json({ devices: [{ id: 'd1', name: HOST, hostname: NAME, addresses: ['100.64.0.9'], lastSeen: new Date().toISOString() }] });
  if (p.endsWith('/routes')) return json({ advertisedRoutes: [], enabledRoutes: [] });
  return route.fulfill({ status: 404, headers: CORS, body: '{}' });
});
// The node, as the browser reaches it over the tailnet: everything, CORS
// preflights included, goes to the real agent.
await page.route((u) => u.hostname === HOST && u.port === '8443', async (route) => {
  const u = new URL(route.request().url());
  await route.fulfill({ response: await route.fetch({ url: AGENT + u.pathname + u.search }) });
});

const jf = async (method, p, token, body) => {
  const auth = `MediaBrowser Client="rs", Device="rs", DeviceId="rs-${token ? 'a' : 'x'}", Version="1"` + (token ? `, Token="${token}"` : '');
  const r = await fetch(JF + p, { method, headers: { 'Content-Type': 'application/json', Authorization: auth }, body: body && JSON.stringify(body) });
  const t = await r.text();
  let out = null;
  try { out = t ? JSON.parse(t.replace(/^﻿/, '')) : null; } catch {}   // refusals are plain text
  return { status: r.status, body: out };
};
const login = (u, p) => jf('POST', '/Users/AuthenticateByName', null, { Username: u, Pw: p });

/* -------------------------------- run --------------------------------- */

const card = '#nodes-body .card-addon';
const text = async () => (await page.textContent(card)).replace(/\s+/g, ' ');
const waitText = (re, timeout) => page.waitForFunction((src) =>
  new RegExp(src).test((document.querySelector('#nodes-body .card-addon')?.textContent || '').replace(/\s+/g, ' ')), re.source, { timeout });

try {
  await page.fill('#credentials', DO_TOKEN + ' tskey-api-testtoken');
  await page.click('#btn-signin');
  await page.waitForSelector('#nodes-body tr[data-key]');
  await page.click('#nodes-body tr[data-key] td.name');
  await page.waitForSelector(card);
  const t0 = Date.now();
  await page.waitForFunction(() => /ready/.test(document.querySelector('#nodes-body .card-addon .ca-head')?.textContent || ''),
    null, { timeout: 420000 });
  check('the agent installs and sets up a real Jellyfin', true, `${Math.round((Date.now() - t0) / 1000)}s`);
  await page.click(card + ' [data-act=reveal]');
  check('the card shows the derived admin password', (await text()).includes(cfg.adminPassword));
  const adm = await login('admin', cfg.adminPassword);
  check('that password signs in to Jellyfin', adm.status === 200);
  const token = adm.body?.AccessToken;

  await page.setInputFiles(card + ' input[type=file]', path.join(RS, 'files', "Grandma's 80th.mkv"));
  await waitText(/Ready to watch as “Grandma's 80th\.mp4”/, 300000);
  check('an uploaded video is converted and ready', true);

  await page.fill(card + ' .ca-link input', ORIGIN + '/files/clip.mp4');
  await page.click(card + ' .ca-link button');
  await page.waitForFunction(() => (document.querySelector('#nodes-body .card-addon').textContent.match(/Ready to watch/g) || []).length === 2,
    null, { timeout: 120000 });
  check('a linked video is fetched by the node and ready', true);
  const log = fs.readFileSync(path.join(RS, 'agent.log'), 'utf8');
  check('the MPEG-4/MP3 upload was re-encoded', /copy video=false copy audio=false/.test(log));
  check('the H.264/AAC link was copied, not re-encoded', /copy video=true copy audio=true/.test(log));

  let items = [];
  for (let i = 0; i < 30 && items.length < 2; i++) {
    items = (await jf('GET', '/Items?Recursive=true&IncludeItemTypes=Video,Movie&Fields=MediaSources', token)).body?.Items || [];
    if (items.length < 2) await new Promise((r) => setTimeout(r, 1000));
  }
  check("both videos are in Jellyfin's library", items.length === 2, items.map((i) => i.Name).join(', '));
  for (const it of items) {
    const ms = it.MediaSources?.[0] || {};
    const v = ms.MediaStreams?.find((s) => s.Type === 'Video'), a = ms.MediaStreams?.find((s) => s.Type === 'Audio');
    check(`"${it.Name}" is H.264/AAC in MP4`, ms.Container?.includes('mp4') && v?.Codec === 'h264' && a?.Codec === 'aac',
      `${ms.Container} ${v?.Codec} ${a?.Codec}`);
  }
  const big = items.find((i) => /Grandma/.test(i.Name));
  const st = await fetch(`${JF}/Videos/${big?.Id}/stream?static=true`,
    { headers: { Authorization: `MediaBrowser Token="${token}"`, Range: 'bytes=0-1023' } });
  check('Jellyfin streams it directly', st.status === 206 || st.status === 200, String(st.status));

  check('the guest cannot sign in before sharing', (await login('guest', cfg.guestPassword)).status !== 200);
  await page.click(card + ' [data-act=share][data-on="1"]');
  await waitText(/Guest link/, 60000);
  check('sharing on funnels port 443 and never the control port', fs.existsSync(path.join(RS, 'ts', 'funnel')) &&
    !/funnel .*8443/.test(fs.readFileSync(path.join(RS, 'ts', 'calls.log'), 'utf8')));
  check('the guest can sign in while shared', (await login('guest', cfg.guestPassword)).status === 200);
  await page.click(card + ' [data-act=share][data-on="0"]');
  await waitText(/Only your tailnet can open Jellyfin/, 60000);
  check('sharing off closes the funnel', !fs.existsSync(path.join(RS, 'ts', 'funnel')));
  check('and locks the guest out again', (await login('guest', cfg.guestPassword)).status !== 200);

  const clipBtn = `${card} li:has-text("clip.mp4") [data-act=remove]`;
  await page.click(clipBtn);
  await page.click(clipBtn);
  await page.waitForFunction(() => !/clip\.mp4/.test(document.querySelector('#nodes-body .card-addon').textContent), null, { timeout: 15000 });
  check('a removed video leaves the disk', !fs.existsSync(path.join(RS, 'media', 'clip.mp4')));
  let left = 2;
  for (let i = 0; i < 20 && left !== 1; i++) {
    await new Promise((r) => setTimeout(r, 1000));
    left = ((await jf('GET', '/Items?Recursive=true&IncludeItemTypes=Video,Movie', token)).body?.Items || []).length;
  }
  check("and Jellyfin's library", left === 1, String(left));
} catch (e) {
  check('the run finished', false, e.message);
  console.log('\nagent log:\n' + fs.readFileSync(path.join(RS, 'agent.log'), 'utf8').split('\n').slice(-25).join('\n'));
} finally {
  await browser.close();
  srv.close();
  agentProc.kill();
  try { run('docker', ['rm', '-f', CONTAINER], { stdio: 'ignore' }); } catch {}
  // Jellyfin's files belong to the container's user; take them back to delete them.
  try { run('docker', ['run', '--rm', '-v', RS + ':/rs', '--entrypoint', 'rm', 'jellyfin/jellyfin:12.2', '-rf', '/rs/data', '/rs/media'], { stdio: 'ignore' }); } catch {}
  fs.rmSync(RS, { recursive: true, force: true });
}

const bad = results.filter((r) => !r).length;
console.log(`\n${results.length - bad}/${results.length} real-stack checks passed`);
process.exit(bad ? 1 : 0);
