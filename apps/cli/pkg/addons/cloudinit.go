package addons

import "strings"

// Params is everything that varies between nodes' cloud-init.
type Params struct {
	AuthKey    string // single-use Tailscale auth key
	Exit       bool   // advertise as an exit node
	Addon      string // catalog ID, or ""
	NodeConfig string // the agent's config, from NodeConfig; ignored without an add-on
	Version    string // the release the agent is downloaded from
}

// The script is assembled from these fragments in the same order in both front
// ends; the web app's copies are character-for-character these. Placeholders
// are {{NAME}}, filled by plain replacement.
const (
	ciHead = `#cloud-config

# DigitalOcean's vendor data runs a ~60 s agent install before any of this, and
# these nodes are disposable, so it is skipped. Consequence: the image's default
# user stays "ubuntu" rather than root, and no DO monitoring agent is installed.
vendor_data:
  enabled: false
`
	ciFilesExit = `  - path: /etc/sysctl.d/99-tailscale.conf
    content: |
      net.ipv4.ip_forward = 1
      net.ipv6.conf.all.forwarding = 1
`
	ciFilesNode = `  - path: /etc/yvpn/node.json
    permissions: '0600'
    content: |
      {{NODE_CONFIG}}
`
	ciRunExit = `  - sysctl --system
`
	ciRunTailscale = `  # The static tarball, rather than install.sh: no apt repo, no apt-get update,
  # and no waiting on unattended-upgrades for the dpkg lock. amd64 matches every
  # non-GPU droplet size. No package_update/package_upgrade either -- an apt
  # upgrade on first boot cost ~145 s and these nodes live for hours.
  - mkdir -p /tmp/tailscale
  - curl -fsSL https://pkgs.tailscale.com/stable/tailscale_latest_amd64.tgz | tar xz -C /tmp/tailscale --strip-components=1
  - install -m 0755 /tmp/tailscale/tailscale /usr/bin/tailscale
  - install -m 0755 /tmp/tailscale/tailscaled /usr/sbin/tailscaled
  - install -m 0644 /tmp/tailscale/systemd/tailscaled.service /etc/systemd/system/tailscaled.service
  - install -m 0644 /tmp/tailscale/systemd/tailscaled.defaults /etc/default/tailscaled
  - systemctl enable --now tailscaled
`
	ciUpExit = `  # Tailscale installs its own netfilter rules for an exit node, so the manual
  # iptables FORWARD/MASQUERADE rules that used to be here are not needed.
  - tailscale up --authkey {{AUTH_KEY}} --advertise-exit-node
`
	ciUpPlain = `  - tailscale up --authkey {{AUTH_KEY}}
`
	ciRunNode = `  # The add-on's agent, from the yVPN release this front end belongs to. It
  # installs the add-on and serves the dashboard's controls on the tailnet only.
  - curl -fsSL -o /usr/local/bin/yvpn-node https://github.com/bnsmcx/yvpn/releases/download/v{{VERSION}}/yvpn-node-linux-amd64
  - chmod 0755 /usr/local/bin/yvpn-node
  - /usr/local/bin/yvpn-node bootstrap
`
	ciTailExit = `final_message: "yVPN exit node ready."
`
	ciTailNode = `final_message: "yVPN node ready; its add-on is installing."
`
)

// CloudInit builds a node's user data.
func CloudInit(p Params) string {
	files := ""
	if p.Exit {
		files += ciFilesExit
	}
	if p.Addon != "" {
		files += strings.ReplaceAll(ciFilesNode, "{{NODE_CONFIG}}", p.NodeConfig)
	}

	out := ciHead
	if files != "" {
		out += "\nwrite_files:\n" + files
	}
	out += "\nruncmd:\n"
	if p.Exit {
		out += ciRunExit
	}
	out += ciRunTailscale
	if p.Exit {
		out += strings.ReplaceAll(ciUpExit, "{{AUTH_KEY}}", p.AuthKey)
	} else {
		out += strings.ReplaceAll(ciUpPlain, "{{AUTH_KEY}}", p.AuthKey)
	}
	if p.Addon != "" {
		out += strings.ReplaceAll(ciRunNode, "{{VERSION}}", p.Version)
		out += "\n" + ciTailNode
	} else {
		out += "\n" + ciTailExit
	}
	return out
}
