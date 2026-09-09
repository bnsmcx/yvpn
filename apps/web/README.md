# yVPN web

A single-page GUI with the same capabilities as the `yvpn` CLI: create, list,
inspect, and delete Tailscale exit nodes running on DigitalOcean droplets.

`index.html` is the entire application — no build step, no npm install, no
framework, no CDN. Open the file and it runs.

## What "signing in" means

There is no account and no auth layer. You supply the two API credentials the app
acts with, and it acts with them:

| Field | What it is |
|---|---|
| Profile | A label so password managers store this as a normal login item |
| DigitalOcean token | Personal access token, **read + write** |
| Tailscale API key | An API access token (`tskey-api-…`) |
| Tailscale proxy URL | Your deployed proxy — see below |

Credentials live in `sessionStorage` and are wiped when the tab closes. Tick
*Keep me signed in* to move them to `localStorage` instead. **Sign out** clears
both. Nothing is ever sent anywhere except DigitalOcean and your own proxy.

Rotating a credential is a password-manager operation: change it there, sign out,
sign back in. The app has no state to migrate.

### Password manager setup (Bitwarden and friends)

The login form is a real `<form>` with stable field names, so a manager can fill
it as one item:

- **Username** → the `Profile` field (`autocomplete="username"`)
- **Password** → the DigitalOcean token (`autocomplete="current-password"`)
- **Custom field** named `tailscale-token` → your Tailscale API key
- **Custom field** named `tailscale-proxy-url` → your proxy URL

Bitwarden matches custom fields by the input's `name`/`id`, both of which are set
to exactly those strings. Save the item once and it autofills every field after.

## The proxy, and why it has to exist

DigitalOcean's API sends `access-control-allow-origin: *` and permits the
`Authorization` header, so this page calls it **directly** from your browser.

Tailscale's API sends **no CORS headers at all** — verified against
`api.tailscale.com` for `https://`, `http://localhost`, and `null` (what a
`file://` page sends). Every browser therefore refuses those requests. This is
enforced by the browser, so no amount of client-side code can work around it; a
web app that talks to Tailscale needs something server-side in the path.

`proxy/` is that something, and it is deliberately tiny:

- it forwards **only** the four endpoints yVPN uses, so it can't be repurposed as
  an open relay;
- it holds **no secrets** — your browser sends its own Tailscale key in the
  `Authorization` header, which is passed straight through, so someone who finds
  the URL without a key can do nothing with it;
- your **DigitalOcean token never reaches it**. That half of the app stays
  browser-direct.

> Do not substitute a public CORS proxy for this. Those relay your `Authorization`
> header through a stranger's server, which hands them control of your tailnet.

### Run it locally (one binary, no cloud account)

```bash
go run ./apps/web/proxy      # serves the app and the proxy on one origin
# open http://localhost:8777
```

Because the page and the API come from the same origin here, CORS never enters
the picture. Leave the proxy URL blank, or set it to `http://localhost:8777`.

### Deploy it (Cloudflare Workers)

```bash
npx wrangler deploy apps/web/proxy/worker.js --name yvpn-proxy \
  --compatibility-date 2026-01-01
```

Or paste `worker.js` into the dashboard under *Workers & Pages → Create → Worker*.
Then host `index.html` anywhere static — GitHub Pages, S3, a CDN, a USB stick —
and paste the worker URL into the login form.

Optionally set `ALLOWED_ORIGINS` on the worker to restrict callers, e.g.
`https://bnsmcx.github.io,null` (`null` is what `file://` pages send). Left unset,
any origin may call it, which is safe here only because there are no cookies and
no ambient credentials — every request must carry its own bearer token.

## Parity with the CLI

| CLI | Web |
|---|---|
| `yvpn list` | The node table, plus tailnet status and cost columns |
| `yvpn datacenters` | The datacenter picker in the create dialog |
| `yvpn create <dc>` | **New exit node** — same cloud-init, same droplet spec, live progress log |
| `yvpn delete <id>` | **Delete**, with a confirmation dialog |
| TUI keymap | `n` new · `d` delete · `r` refresh · `↑`/`↓` select · `?` help · `Esc` close |

Create runs the identical sequence to `cmd/tui/cli.go`: request an ephemeral
single-use auth key → provision the droplet with the same cloud-init → wait for
it to join the tailnet → approve its advertised routes → revoke the key. If any
step fails the droplet and key are rolled back, exactly as the CLI does.

Two deliberate differences:

- The tailnet poll runs every **3s** rather than 1s, and gives up after **15
  minutes** rather than 60. Browser tabs are a worse place to hold an hour-long
  loop, and 1s polling from a browser invites Tailscale's rate limiter.
- The wait is **cancellable**. Cancel aborts in flight and rolls back.

## Stats

Per node: region, public IP, tailnet IP, droplet status, tailnet reachability,
whether exit routes are actually approved, size, monthly cost, and age.
Across the fleet: node count, how many are live on the tailnet, total burn rate,
and month-to-date usage plus account balance pulled from DigitalOcean billing.

Tailnet columns are best-effort — if the proxy is unreachable the droplet data
still renders and a warning appears, rather than the page going blank.

## Browser support

Uses `<dialog>`, the popover API, view transitions, CSS nesting, `light-dark()`,
and `color-mix()`. Current Chrome, Edge, Safari, and Firefox all handle these;
view transitions degrade to an instant swap where unsupported.

## Tests

```bash
npm install playwright && npx playwright install chromium
node apps/web/test/e2e.mjs
```

Drives the real `index.html` in Chromium with both APIs mocked at the network
layer — the whole create → list → delete flow, credential handling, keyboard
shortcuts, and rollback — without touching a real account. The app has no
dependencies; this test is the only thing that needs one.
