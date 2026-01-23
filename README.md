# yVPN

A terminal user interface (TUI) application for managing VPN exit nodes on a Tailscale network using DigitalOcean droplets.

## Overview

yVPN simplifies the creation and management of distributed VPN exit nodes. It provisions DigitalOcean droplets preconfigured as Tailscale exit nodes, giving you on-demand VPN endpoints in various geographic locations.

## Features

- **Interactive TUI** - Clean terminal interface with keyboard navigation
- **One-click provisioning** - Create exit nodes in any DigitalOcean datacenter
- **Automated setup** - Droplets are fully configured via cloud-init (Tailscale installation, IP forwarding, NAT rules)
- **SSH access** - Run as an SSH server for remote management without local installation
- **Exit node management** - View, create, and delete exit nodes from a unified dashboard

## Prerequisites

- Go 1.21+
- DigitalOcean API token (with read/write access)
- Tailscale API key
- SSH host key (for SSH server mode)

## Installation

### From source

```bash
git clone https://github.com/your-username/yvpn.git
cd yvpn
go build -o yvpn ./cmd/tui
```

### Docker

```bash
docker build -t yvpn:latest .
```

## Usage

### Local TUI Mode

Set your credentials as environment variables:

```bash
export DIGITAL_OCEAN_TOKEN=<your_digitalocean_token>
export TAILSCALE_API=<your_tailscale_api_key>
```

Run the application:

```bash
./yvpn
```

If credentials aren't set, you'll be prompted to enter them on startup.

### SSH Server Mode

Run yVPN as an SSH server for remote access:

```bash
./yvpn ssh
```

This starts an SSH server on port 1337. Connect with:

```bash
ssh -p 1337 user@hostname
```

Pass credentials via SSH environment variables.

### Docker

```bash
docker run -d -p 22:1337 --name yvpn yvpn:latest
```

### Keyboard Controls

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

2. **Configure**: The droplet automatically installs Tailscale, enables IP forwarding, configures iptables rules, and advertises itself as an exit node.

3. **Enable**: Once the droplet joins your tailnet, yVPN enables it as an exit node via the Tailscale API.

4. **Use**: Connect to your new exit node from any device on your tailnet.

## Project Structure

```
yvpn/
├── cmd/
│   ├── tui/           # Main TUI application
│   │   ├── main.go    # Entry point, SSH server
│   │   ├── dash.go    # Dashboard view
│   │   ├── add.go     # Create exit node screen
│   │   ├── delete.go  # Delete exit node screen
│   │   ├── onboard.go # Credential input
│   │   └── style.go   # UI styling
│   └── test/          # CLI testing tool
├── pkg/
│   ├── digital_ocean/ # DigitalOcean API wrapper
│   └── tailscale/     # Tailscale API wrapper
├── Dockerfile
├── go.mod
└── shell.nix          # Nix development environment
```

## Development

### Using Nix

```bash
nix-shell
```

This provides Go, Git, gopls, and development tools.

### Testing CLI

A CLI tool is available for testing individual operations:

```bash
# List available datacenters
go run cmd/test/main.go datacenters

# Create exit node in a datacenter
go run cmd/test/main.go create <datacenter>

# Delete exit node by droplet ID
go run cmd/test/main.go delete <droplet_id>

# Generate new Tailscale auth key
go run cmd/test/main.go newkey

# Delete Tailscale auth key
go run cmd/test/main.go killkey <key_id>
```

## Droplet Specifications

Exit nodes are created with:
- **OS**: Ubuntu 24.04 x64
- **Size**: s-1vcpu-1gb (1 vCPU, 1GB RAM)
- **Tag**: `yVPN`

## Dependencies

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Styling
- [Wish](https://github.com/charmbracelet/wish) - SSH server
- [godo](https://github.com/digitalocean/godo) - DigitalOcean API client
