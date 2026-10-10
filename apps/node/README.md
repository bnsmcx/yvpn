# yvpn-node

The agent that runs on a yVPN node with an add-on. Today there is one add-on,
**Jellyfin watch party**: a private media server you drop videos into from the
yVPN dashboard, then watch in sync with family, on your tailnet or through a
guest link.

It is one static Go binary with no dependencies outside the standard library.

## How it gets onto a node

Picking an add-on in **New node** (web) or `--addon jellyfin` (CLI) changes the
droplet's cloud-init in three ways:

1. It writes `/etc/yvpn/node.json` (mode 0600): the add-on, the agent's bearer
   token, and the Jellyfin admin and guest passwords.
2. It downloads `yvpn-node-linux-amd64` from the GitHub release **matching the
   front end's own version**, so the release workflow attaches it to every tag.
3. It runs `yvpn-node bootstrap`, which installs and starts the
   `yvpn-node.service` systemd unit (`yvpn-node serve`).

None of the secrets are stored anywhere else. Each is an HMAC of the droplet's
name keyed by the DigitalOcean token (`apps/cli/pkg/addons`, mirrored in the
web app), so any front end holding that token can work them out again. The
token itself never reaches the node.

## What `serve` does

It installs the add-on in the background and reports each step:

| Step | What happens |
|---|---|
| Opening the control port | `tailscale serve --https=8443` → the agent on 127.0.0.1:8090. Retried every 15 s until it works, so turning on HTTPS certificates later fixes a node in place |
| Installing Docker | `apt-get install docker.io`, unless Docker is already there (about 15 s on Ubuntu 24.04) |
| Downloading Jellyfin | `docker pull jellyfin/jellyfin:12.2` |
| Starting Jellyfin | The container, listening on 127.0.0.1 only; containers are also blocked from the droplet metadata service, which holds this cloud-init |
| Setting up Jellyfin | The first-run wizard through Jellyfin's API: admin account, a *Watch party* library at `/media`, and a guest account that is hidden and disabled |
| Publishing Jellyfin | `tailscale serve --https=443` → Jellyfin |

Every step checks before it acts, so after a reboot or a failure it just runs
again. Nothing listens on the droplet's public address: Tailscale is the only way in.

## The control API

At `https://<node>.<tailnet>.ts.net:8443`, with `Authorization: Bearer <token>`.
CORS is open to any origin (the dashboard is served from elsewhere, and there are
no cookies to steal) and answers Chrome's private-network preflight.

| | |
|---|---|
| `GET /v1/status` | Setup phase and step, Jellyfin's URL, free disk, every video and where it stands, sharing, recent log lines |
| `POST /v1/uploads` `{name, size}` | Starts an upload, or finds the one this file already started, and returns `{id, offset}` |
| `PATCH /v1/uploads/{id}` | Appends one chunk. `Upload-Offset` must equal the bytes already stored; otherwise 409 with the real offset |
| `POST /v1/fetch` `{url}` | The node downloads a video from a link itself (Dropbox share links are turned into downloads) |
| `DELETE /v1/media/{id}` | Removes a video wherever it is: cancels a download or conversion, deletes its files, rescans the library |
| `POST /v1/share` `{enabled}` | Turns Tailscale Funnel on or off for port 443 and enables or disables the guest account. The control port is never funnelled |
| `POST /v1/retry` | Runs setup again after a failure |

Uploads are resumable. The file on disk is the record of what has arrived, so a
dropped connection or a closed tab costs only the chunk in flight, and dropping
the same file again carries on from there.

## Converting videos

Each video is made into one file every device plays directly: H.264 (8-bit, no
taller than 1080p), AAC audio, in an MP4 with its index at the front. That way
Jellyfin never transcodes live, which is what lets a 2-vCPU droplet host a
family at once.

Streams that are already right are copied rather than re-encoded, which takes
seconds. ffmpeg comes from the Jellyfin image, run in a throwaway container per
video, so there is nothing more to install. Videos are converted one at a time.

## Running it locally

```bash
go test ./...                                   # unit tests, with fakes for docker and tailscale
docker run -d --name jf-test -p 127.0.0.1:8097:8096 jellyfin/jellyfin:12.2
YVPN_JELLYFIN_URL=http://127.0.0.1:8097 go test -run Live -v   # the setup against a real Jellyfin
docker rm -f jf-test
```

`apps/web/test/realstack.mjs` runs the whole thing: this agent, a real Jellyfin
and real ffmpeg, driven through the web app.

## What isn't covered by the tests

`tailscale serve` and `tailscale funnel` are stubbed in every test; there is no
tailnet in CI. The commands follow the Tailscale 1.104 CLI. After a share
change the agent reads `tailscale funnel status --json` back rather than trusting
what it asked for. Every `tailscale` call is limited to 45 s: a tailnet that
hasn't allowed serve or Funnel makes the CLI print a `login.tailscale.com` link
and wait, so the agent stops waiting and passes that link to the dashboard, where
it is shown as a link to click. Real boot timing on a droplet is not measured
either.
