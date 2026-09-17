# yVPN web

A single-page GUI with the same capabilities as the `yvpn` CLI: create, list,
inspect, and delete Tailscale exit nodes running on DigitalOcean droplets.

`index.html` is the entire application — no build step, no npm install, no
framework, no CDN. It is served, together with a tiny Tailscale relay, by a
single Cloudflare Worker (or a single Go binary locally), so users never see or
configure a proxy.

## What "signing in" means

There is no account and no auth layer. You supply the two API credentials the app
acts with, and it acts with them:

| Field | What it is |
|---|---|
| Profile | A label so password managers store this as a normal login item |
| Credentials | Both tokens in one string, separated by whitespace, either order |

The two tokens are:

- a DigitalOcean personal access token, **read + write** (`dop_v1_…`)
- a Tailscale API access token (`tskey-api-…`)

So the Credentials value looks like `dop_v1_… tskey-api-…`. The app splits it on
whitespace and tells the tokens apart by prefix, and reports a clear error if
either is missing. First time through, open *Build this from your two tokens* on
the sign-in form and paste each token separately; it fills Credentials for you.

Credentials live in `sessionStorage` and are wiped when the tab closes. Tick
*Keep me signed in* to move them to `localStorage` instead. **Sign out** clears
both. Nothing is ever sent anywhere except DigitalOcean and the relay on the site serving the page.

Rotating a credential is a password-manager operation: change it there, sign out,
sign back in. The app has no state to migrate.

### Password manager setup (Bitwarden and friends)

Both tokens travel as the single password, so a password manager's ordinary
"save login" prompt captures everything — no custom fields:

- **Username** → the `Profile` field (`autocomplete="username"`)
- **Password** → the `Credentials` field (`autocomplete="current-password"`)

The helper inputs in *Build this from your two tokens* are plain text and marked
`data-bwignore` / `data-1p-ignore` / `data-lpignore`, so managers save the
combined field rather than either token alone. When the helper fills Credentials
it fires `input` and `change` events, which is what makes Bitwarden notice the
value and offer to save it.

To rotate one token, edit the password in the manager and replace that half.

## Deploying, and why there's a relay

DigitalOcean's API sends `access-control-allow-origin: *` and permits the
`Authorization` header, so the page calls it **directly** from your browser.

Tailscale's API sends **no CORS headers at all**, so every browser refuses
cross-origin requests to it. The fix is to never make one: whatever serves
`index.html` also answers `/api/…` on the same origin and relays those calls to
`api.tailscale.com`. The page just calls `/api/v2/…` on its own host, so there
is nothing for users to configure.

The relay is deliberately tiny:

- it forwards **only** the four endpoints yVPN uses, so it can't be repurposed as
  an open relay;
- it holds **no secrets** — the browser sends its own Tailscale key in the
  `Authorization` header, which is passed straight through;
- it sends **no CORS headers**, so other sites can't drive it from a browser;
- your **DigitalOcean token never reaches it**.

### Cloudflare Workers

```bash
cd apps/web
npx wrangler deploy
```

`wrangler.toml` bundles `index.html` into `proxy/worker.js`, so one Worker serves
the page at `/` and the relay at `/api/`. Open the Worker's URL and sign in.

### Locally (one binary, no cloud account)

```bash
cd apps/web/proxy
go run .
# open http://localhost:8777
```

Opening `index.html` directly as a file still loads the page, but Tailscale
calls will fail — it needs to be served by one of the above.

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

## Look

The app is styled as a TVA-style workstation: a beige console housing with the
working area set into it as an amber-phosphor screen. Colours come from the
boards in [`design/`](design) — amber `#FFB000` as the interactive colour on
a warm near-black screen, cream text, tan hairlines, with olive and brick-rust
for healthy and failed states so the four node states stay distinguishable
instead of collapsing into one hue.

Both themes are real, and they light the same console differently rather than
recolouring one thing. In **dark** the room lights are off: the housing recedes
to near-black and only the screen is lit, its stamped labels inverting to tan --
no pale frame around a dark screen. In **light** the cabinet sits under full
light, beige, and the screen becomes a pale readout with amber darkened to
`#8A5200` so it holds contrast on cream. The whole palette is `light-dark()`
pairs keyed off `color-scheme`, so one property repaints everything.

The **DISPLAY** switch on the lower rail selects between them — three positions,
`auto` / `dark` / `light`, addressed directly rather than cycled. `auto` follows
the system and is the default; a choice is kept in `localStorage` under
`yvpn.theme` and restored from a script in `<head>` so it cannot flash the wrong
palette on load. It is stored apart from your credentials deliberately: a display
preference should outlive signing out.

## Browser support

Uses `<dialog>`, the popover API, view transitions, CSS nesting, `light-dark()`,
`:has()` and `color-mix()`. Current Chrome, Edge, Safari, and Firefox all handle these;
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
