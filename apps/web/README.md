# yVPN web

A single-page GUI with the same capabilities as the `yvpn` CLI: create, list,
inspect, and delete Tailscale exit nodes running on DigitalOcean droplets, and
nodes that run an add-on (today: a Jellyfin watch party) instead of, or as well
as, being an exit node.

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
| `yvpn list` | The node table; open a row for everything both APIs report about that node |
| `yvpn datacenters [--addon x]` | The datacenter picker in the create dialog, with each region's monthly price for what the node will run |
| `yvpn create <dc> [--addon x] [--no-exit]` | **New node** — same cloud-init, same droplet spec, built in the table |
| `yvpn access <id>` | An add-on node's open row: its links and passwords, and much more (uploads, sharing) |
| `yvpn delete <id>` | **Delete**, with a confirmation dialog |
| TUI keymap | `n` new · `d` delete · `r` refresh · `j`/`k` or `↓`/`↑` select · `Enter`/`o` open · `?` help · `Esc` close or deselect |

Create runs the identical sequence to `cmd/tui/cli.go`: request an ephemeral
single-use auth key → provision the droplet with the same cloud-init → wait for
it to join the tailnet → approve its advertised routes → revoke the key. If any
step fails the droplet and key are rolled back, exactly as the CLI does.

Three deliberate differences:

- The tailnet poll runs every **2s** rather than 1s, and gives up after **15
  minutes** rather than 60. Browser tabs are a worse place to hold an hour-long
  loop, and 1s polling from a browser invites Tailscale's rate limiter.
- The wait is **cancellable**. Cancel aborts in flight and rolls back.
- It runs **in the table**, not in a dialog (below).

## Add-ons: a watch party

**New node** asks two things: what the node runs, then where. *Exit node* is the
default and works as before. *Jellyfin watch party* runs a private media server
as well. Untick *Also an exit node* to have just the server.

### Sizing follows the add-on

Each catalog entry says the smallest droplet it can run on. Jellyfin needs
2 vCPUs, 4 GB and 50 GB of disk. The page fetches DigitalOcean's regions and
sizes once, then works out the cheapest size that fits in every region each time
the add-on changes. Regions with nothing big enough drop out, and each card shows
the size it will get. One core with lots of memory does not count; the converter
needs both cores.

### The node's row

An add-on node's row opens on a card about using it. Everything the APIs say
about the droplet is still there, folded under *Node details*. The card shows:

- **Setup progress** while the add-on installs, then how to open it, with the
  admin password (hidden until you ask, and copyable).
- **Videos**: a drop zone, a file picker, and a field for a link to a video file
  (fetched by the node over its datacenter connection). Uploads go in 16 MiB
  chunks with real progress, and resume: a lost connection retries from where the
  node says it stands, and dropping the same file again carries on after a closed
  tab. Each video is then converted once so every device plays it directly, and
  the list shows *Converting… 63%* and then *Ready to watch*.
- **Share with guests**: off or on. On puts Jellyfin on the internet through
  Tailscale Funnel, behind a guest login, and shows the guest link and password
  with *Copy invite*, a message ready for the family chat. Uploads and the
  controls stay on the tailnet either way. If the tailnet refuses Funnel, the
  card says why and how to fix it.
- **How the watch party works**, including SyncPlay, Jellyfin's synced playback.

The card talks to the node's agent ([`apps/node`](../node)) **directly from the
browser** at `https://<node>.<tailnet>.ts.net:8443`, not through this site's
relay. That needs this computer on the tailnet, with MagicDNS and HTTPS
certificates turned on. If it can't connect, the card says exactly that.

A card is a live thing: an upload in progress, a link half typed, focus in a
field. The table repaints itself every few seconds while anything builds, so
each card is one DOM element per node, built once and moved into each fresh
table rather than rebuilt with it.

### Secrets without storage

The agent's token and the Jellyfin passwords are HMACs of the droplet's name,
keyed by your DigitalOcean token. No account, server or browser storage keeps
them: any browser (or the CLI) signed in with that token shows the same ones,
and the token never reaches the node. The admin password is
`xxxxx-xxxxx-xxxxx-xxxxx`; the guest's is made of syllables (`jaza-ruwa-wifa-20`),
since somebody will read it aloud and type it on a TV remote.

### Parity with the CLI

The add-on catalog, the secrets and the cloud-init exist twice: in JavaScript
here and in Go in `apps/cli/pkg/addons`. Both are held to the golden files in
[`testdata/parity`](../../testdata/parity), by the Go tests and by `e2e.mjs`, so
either front end builds exactly the same node. A plain exit node's cloud-init is
byte-for-byte what it was before add-ons existed.

## Creating a node happens in the table

Picking a datacenter is the only thing the create dialog does. Press **Create**
and it closes; the node appears as a row immediately, reporting each step in its
status column — *requesting key*, *provisioning*, *booting*, *joining tailnet*,
*revoking key* — with the seconds ticking beside it. When the build finishes the
row simply stops being special: it is an ordinary node, in the place it has held
all along, because the pending create adopts the droplet's own row as soon as the
droplet exists rather than being swapped out for it.

That leaves the dashboard usable while a node boots. You can start a second one,
delete a third, or open the building row to watch its log — the same log the old
progress dialog held, now attached to the thing it describes — and **cancel**,
which aborts in flight and rolls the half-built node back.

A build that fails or is cancelled leaves its row behind, opened on its log and
marked *failed* or *cancelled*, until you dismiss it. A failure that vanished on
the next poll would be a failure nobody got to read.

While anything is building, the table reloads itself every 10s, so a booting
node's IP, droplet status and cost fill themselves in.

## Getting started, in the app

**Guide** in the header rail (and a link under the sign-in button) opens a
getting-started dialog written widest-first: the opening paragraph is the whole
thing for anyone who already has API tokens, and each section below it assumes
less — where the two tokens come from, what creating a node actually does, how to
select an exit node on a phone or laptop, what it costs, and what the error
messages mean.

## Version

The version is stamped on both console rails and lives in one constant,
`VERSION`, in `index.html`. **Bumping it is how a release is made:** every push
to `main` reads it, and if there is no release for that version yet, the release
workflow builds the CLI binaries and the node agent and publishes them as
`v<version>`, creating the tag itself. There is nothing to tag by hand. The
workflow fails if the constant, `apps/cli/cmd/tui/main.go` and the stamps in the
markup disagree.

Bump the version with any change to what a node is built from (the cloud-init,
or the agent in `apps/node`): nodes download the agent from the release that
matches the page's version.

## Stats

Per node, in the nine columns: region, public IP, tailnet IP, droplet status,
tailnet reachability, whether exit routes are actually approved, size, cost so
far, and age. Across the fleet: node count (and how many are still building),
how many are live on the tailnet, the current rate (what those nodes would cost
over a month left running; click the unit to switch between `/mo` and `/hr`,
kept in `localStorage` under `yvpn.rate`), and the cost so far of the nodes that
exist.

### Opening a row

Nine columns is what fits, not what the two APIs know. Click a node — or select
it and press `Enter` — and a panel drops out of the row with the rest of it:

| Group | What it holds |
|---|---|
| Droplet | DO id, status, image, kernel, size, vCPUs, memory, disk, monthly transfer, features, tags, VPC, volumes, backups, snapshots, lock state, creation time, uptime |
| Region | Datacenter slug and name, whether it is still accepting droplets, region features |
| Addresses | Public IPv4 with its netmask and gateway, private IPv4, IPv6, and both tailnet addresses |
| Tailscale | Machine and host name, node id, owner, OS, client version, update available, authorized, reachable, join time, last seen, key expiry, inbound blocking, external, tags, tailnet-lock error |
| Exit routing | Advertised and approved routes, whether exit routing is actually approved, the DERP relay in use, round-trip latency to the nearest relays, endpoints, whether NAT mapping varies by destination |
| Billing | Hourly rate, monthly cap, cost so far, billing start |

A `<details>` element cannot live between two `<tr>`s, so the panel is a second
row that is only in the DOM while it is open. Selection and open panels survive
a refresh, so a node's detail does not collapse under you every poll.

Everything in it comes from calls the app already makes, bar one addition: the
tailnet device list is now fetched with `fields=all`, which is what supplies the
relay, endpoints and per-relay latency. That is a query parameter on an endpoint
the relay already forwards, so the allowlist is unchanged.

There are no CPU or bandwidth graphs, and the panel says so where you would go
looking for them: these nodes skip DigitalOcean's monitoring agent to cut about
a minute off boot, so its metrics API has nothing to report for them.

Costs are worked out from each droplet's own `price_hourly` and `created_at`,
the way DigitalOcean bills Droplets: per second, with a $0.01 minimum, capped at
672 hours per calendar month. No billing API is called, so the token doesn't
need billing access and nothing account-wide (other droplets, volumes, credits)
is mixed in. Deleted nodes drop out of the total, because their billing record
goes with the droplet.

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
shortcuts, and rollback — without touching a real account. It also covers the
watch party against a mocked node agent: sizing per add-on, parity with the CLI's
golden files, chunked uploads that survive a dropped connection, link fetching,
sharing (refused by the tailnet, then allowed), copy, removal, the card surviving
a repaint, the error states, and phone width. The app has no dependencies; the
tests are the only things that need one.

```bash
node apps/web/test/realstack.mjs     # needs Docker, Go and ffmpeg as well
```

Runs the watch party for real: the real agent installs a real Jellyfin in Docker,
and the page uploads real videos, which ffmpeg converts and Jellyfin then serves.
Only DigitalOcean, Tailscale's API and the `tailscale` command are stand-ins.
