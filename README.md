# yVPN

Manage VPN exit nodes on a Tailscale network using DigitalOcean droplets — from a
terminal or a browser.

## Overview

yVPN simplifies the creation and management of distributed VPN exit nodes. It provisions DigitalOcean droplets preconfigured as Tailscale exit nodes, giving you on-demand VPN endpoints in various geographic locations.

Two front ends share one workflow:

| App | Path | What it is |
|---|---|---|
| CLI / TUI | [`apps/cli`](apps/cli) | Go binary — interactive TUI, scriptable CLI, SSH server mode |
| Web | [`apps/web`](apps/web) | Single static HTML file — no build, no npm, no framework |

The web app is documented in [apps/web/README.md](apps/web/README.md). It is
deployed as one Cloudflare Worker (or run locally as one Go binary) that serves
the page and relays its Tailscale API calls from the same origin, so users only
enter their two API credentials. DigitalOcean is called straight from the page.

The rest of this document covers the CLI.

## Features

- **Interactive TUI** - Clean terminal interface with keyboard navigation
- **Scriptable CLI** - JSON output for automation and scripting
- **One-click provisioning** - Create exit nodes in any DigitalOcean datacenter
- **Automated setup** - Droplets are fully configured via cloud-init (Tailscale installation, IP forwarding) and reach your tailnet in about a minute
- **SSH access** - Run as an SSH server for remote management without local installation
- **Exit node management** - View, create, and delete exit nodes from a unified dashboard
- **Full node detail** (web) - Open any row for everything DigitalOcean and Tailscale report about that node
- **Add-ons** - A node can also run something for your tailnet. The first is a **Jellyfin watch party**: drop a video on the node's row in the web app, then watch it in sync with family, on your tailnet or through a guest link. The droplet size follows what the node runs. See [Add-ons](#add-ons)

## Prerequisites

- DigitalOcean API token (with read/write access)
- Tailscale API key

## Installation

### From GitHub Releases

Download the latest binary for your platform from the [Releases page](https://github.com/bnsmcx/yvpn/releases).

```bash
# Linux
curl -LO https://github.com/bnsmcx/yvpn/releases/latest/download/yvpn-linux-amd64
chmod +x yvpn-linux-amd64
sudo mv yvpn-linux-amd64 /usr/local/bin/yvpn

# macOS (Apple Silicon)
curl -LO https://github.com/bnsmcx/yvpn/releases/latest/download/yvpn-darwin-arm64
chmod +x yvpn-darwin-arm64
sudo mv yvpn-darwin-arm64 /usr/local/bin/yvpn

# macOS (Intel)
curl -LO https://github.com/bnsmcx/yvpn/releases/latest/download/yvpn-darwin-amd64
chmod +x yvpn-darwin-amd64
sudo mv yvpn-darwin-amd64 /usr/local/bin/yvpn
```

### From Source

```bash
git clone https://github.com/bnsmcx/yvpn.git
cd yvpn/apps/cli
go build -o yvpn ./cmd/tui
```

Requires Go 1.21+.

## Configuration

Set your credentials as environment variables:

```bash
export DIGITAL_OCEAN_TOKEN=<your_digitalocean_token>
export TAILSCALE_API=<your_tailscale_api_key>
```

## Usage

### Interactive TUI

```bash
yvpn tui
```

If credentials aren't set, you'll be prompted to enter them on startup.

### CLI Commands

```bash
# List existing exit nodes
yvpn list

# List available datacenters
yvpn datacenters

# Create a new exit node
yvpn create nyc1

# Create a node that runs Jellyfin for a watch party, and isn't an exit node
yvpn create nyc1 --addon jellyfin --no-exit

# Datacenters priced for a node running Jellyfin
yvpn datacenters --addon jellyfin

# How to reach an add-on node, with its passwords
yvpn access 12345

# Delete an exit node by ID
yvpn delete 12345

# JSON output for scripting
yvpn list --json
yvpn datacenters --json
yvpn create nyc1 --json

# Show version
yvpn --version
```

### SSH Server Mode

Run yVPN as an SSH server for remote access:

```bash
yvpn ssh
```

This starts an SSH server on port 1337. Connect with:

```bash
ssh -p 1337 user@hostname
```

Pass credentials via SSH environment variables.

### TUI Keyboard Controls

| Key | Action |
|-----|--------|
| `n` | Create new exit node |
| `d` | Delete selected exit node |
| `↑/↓` | Navigate list |
| `Enter` | Confirm selection |
| `Esc` | Go back / Cancel |
| `q` | Quit |

## How It Works

1. **Create**: Select a DigitalOcean datacenter. yVPN generates a temporary Tailscale auth key, provisions a droplet with cloud-init configuration, and waits for the node to appear on your tailnet.

2. **Configure**: The droplet installs Tailscale from the static tarball, enables IP forwarding, and advertises itself as an exit node. It is ready in about a minute; see [Boot time](#boot-time) for why.

3. **Enable**: Once the droplet joins your tailnet, yVPN enables it as an exit node via the Tailscale API.

4. **Use**: Connect to your new exit node from any device on your tailnet.

## Add-ons

Pick an add-on when creating a node (**New node** in the web app, or `--addon`)
and the node runs it as well as, or instead of, being an exit node. The droplet
is the cheapest in the region that is big enough for the add-on.

| Add-on | Needs | What it is |
|---|---|---|
| `jellyfin` | 2 vCPU · 4 GB · 50 GB | A private media server for a watch party. Drop videos on the node's row in the web app (or give it a link), and they are converted so every device plays them. Share with guests opens it to the internet through Tailscale Funnel, behind a guest login, and writes an invite to send. Jellyfin's SyncPlay keeps everyone's playback in step. |

An add-on node runs a small agent, [`apps/node`](apps/node), that installs the
add-on and serves the web app's controls on the tailnet only. Its passwords
derive from your DigitalOcean token, so they are never stored and any front end
holding the token shows the same ones (`yvpn access <id>`). Everything on the
node goes with it when it is deleted.

**It needs** MagicDNS and HTTPS certificates turned on in the tailnet (Tailscale
admin console → DNS), and the browser's computer on the tailnet. Sharing needs
Tailscale Funnel, which new tailnets allow for every member.

## Project Structure

```
yvpn/
├── apps/
│   ├── cli/               # Go TUI + CLI
│   │   ├── cmd/tui/
│   │   │   ├── main.go    # Entry point, SSH server
│   │   │   ├── cli.go     # CLI commands
│   │   │   ├── dash.go    # Dashboard view
│   │   │   ├── add.go     # Create exit node screen
│   │   │   ├── delete.go  # Delete exit node screen
│   │   │   ├── onboard.go # Credential input
│   │   │   └── style.go   # UI styling
│   │   ├── pkg/
│   │   │   ├── addons/    # Add-on catalog, node secrets, cloud-init
│   │   │   ├── digital_ocean/
│   │   │   └── tailscale/
│   │   ├── Dockerfile
│   │   ├── go.mod
│   │   └── shell.nix      # Nix development environment
│   ├── node/              # yvpn-node: the agent on add-on nodes
│   └── web/               # Single-page web app
│       ├── index.html     # The entire application
│       ├── proxy/         # Serves the page + Tailscale relay (Worker + Go)
│       └── test/          # Playwright end-to-end and real-stack tests
├── testdata/parity/       # Golden files both front ends are held to
└── README.md
```

## Development

### Using Nix

```bash
cd apps/cli && nix-shell
```

This provides Go, Git, gopls, and development tools.

## Droplet Specifications

Exit nodes are created with:
- **OS**: Ubuntu 24.04 x64
- **Size**: the cheapest droplet the region sells with at least 512 MB of RAM,
  enough disk for the image, and no GPU. Usually `s-1vcpu-512mb-10gb`; regions
  that don't sell it get the next cheapest instead of failing. Regions with no
  such size, or without the Ubuntu image, aren't offered. `yvpn datacenters`
  and the web picker show each region's size and hourly price.
- **Tag**: `yVPN`, plus `yvpn-addon:<name>` on a node with an add-on, whose
  size is instead the cheapest that meets the add-on's needs

### Boot time

A node used to take about 4.5 minutes to reach the tailnet. It now takes roughly
a minute. Measured on identical nyc1 droplets, seconds from kernel boot to the
node being ready:

| cloud-init | ready at |
|---|---|
| `package_update` + `package_upgrade` + `install.sh` (old) | 266 s |
| without the apt upgrade | 99 s |
| plus the static Tailscale tarball instead of `install.sh` | 89 s |
| plus skipping DigitalOcean's vendor data (current) | **31 s** |

Three changes, in order of what they saved:

- **No `package_upgrade` on first boot** (~145 s). Upgrading every package on a
  machine that lives for hours buys nothing.
- **`vendor_data: enabled: false`** (~57 s). DigitalOcean's vendor script installs
  its monitoring agent before user scripts run. Two consequences worth knowing:
  the image's default login is `ubuntu` rather than `root`, and no DO agent is
  installed, so the droplet does not report metrics.
- **The static tarball instead of `install.sh`** (~22 s). No apt repository to
  add, no `apt-get update`, and no waiting on unattended-upgrades for the dpkg
  lock. The URL pins `amd64`, which matches every non-GPU droplet size.

Tailscale installs its own netfilter rules for an exit node, so the manual
iptables rules the cloud-init used to write are gone.

## Dependencies

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Styling
- [Wish](https://github.com/charmbracelet/wish) - SSH server
- [godo](https://github.com/digitalocean/godo) - DigitalOcean API client

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
