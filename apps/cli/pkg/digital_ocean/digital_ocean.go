package digital_ocean

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/digitalocean/godo"
)

type ExitNode struct {
	Name string
	ID   int
}

func FetchExitNodes(token string) (nodes []ExitNode, err error) {
	client := godo.NewFromToken(token)
	ctx := context.TODO()

	opt := &godo.ListOptions{
		Page:    1,
		PerPage: 200,
	}

	droplets, _, err := client.Droplets.ListByTag(ctx, "yVPN", opt)
	if err != nil {
		return nil, err
	}

	for _, d := range droplets {
		node := ExitNode{
			Name: d.Name,
			ID:   d.ID,
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

func FetchDatacenters(token string) ([]string, error) {
	var datacenters []string
	client := godo.NewFromToken(token)
	ctx := context.TODO()

	opts := &godo.ListOptions{
		Page:    1,
		PerPage: 200,
	}

	regions, _, err := client.Regions.List(ctx, opts)
	if err != nil {
		return datacenters, err
	}

	for _, r := range regions {
		if r.Available {
			datacenters = append(datacenters, r.Slug)
		}
	}

	slices.Sort(datacenters)

	return datacenters, nil
}

func Create(token, tailscaleAuth, datacenter string) (string, int, error) {
	client := godo.NewFromToken(token)
	ctx := context.TODO()

	// Cloud-init script for setting up Tailscale as an exit node
	cloudInit := fmt.Sprintf(`#cloud-config

# DigitalOcean's vendor data runs a ~60 s agent install before any of this, and
# these nodes are disposable, so it is skipped. Consequence: the image's default
# user stays "ubuntu" rather than root, and no DO monitoring agent is installed.
vendor_data:
  enabled: false

write_files:
  - path: /etc/sysctl.d/99-tailscale.conf
    content: |
      net.ipv4.ip_forward = 1
      net.ipv6.conf.all.forwarding = 1

runcmd:
  - sysctl --system
  # The static tarball, rather than install.sh: no apt repo, no apt-get update,
  # and no waiting on unattended-upgrades for the dpkg lock. amd64 matches the
  # droplet size below. No package_update/package_upgrade either -- an apt
  # upgrade on first boot cost ~145 s and these nodes live for hours.
  - mkdir -p /tmp/tailscale
  - curl -fsSL https://pkgs.tailscale.com/stable/tailscale_latest_amd64.tgz | tar xz -C /tmp/tailscale --strip-components=1
  - install -m 0755 /tmp/tailscale/tailscale /usr/bin/tailscale
  - install -m 0755 /tmp/tailscale/tailscaled /usr/sbin/tailscaled
  - install -m 0644 /tmp/tailscale/systemd/tailscaled.service /etc/systemd/system/tailscaled.service
  - install -m 0644 /tmp/tailscale/systemd/tailscaled.defaults /etc/default/tailscaled
  - systemctl enable --now tailscaled
  # Tailscale installs its own netfilter rules for an exit node, so the manual
  # iptables FORWARD/MASQUERADE rules that used to be here are not needed.
  - tailscale up --authkey %s --advertise-exit-node

final_message: "yVPN exit node ready."
`, tailscaleAuth)

	createRequest := &godo.DropletCreateRequest{
		Tags:   []string{"yVPN"},
		Name:   fmt.Sprintf("%s-yvpn-%d", datacenter, time.Now().Unix()),
		Region: datacenter,
		Size:   "s-1vcpu-1gb",
		Image: godo.DropletCreateImage{
			Slug: "ubuntu-24-04-x64",
		},
		UserData: cloudInit, // Cloud-init script for Tailscale exit node
	}

	droplet, _, err := client.Droplets.Create(ctx, createRequest)
	if err != nil {
		return "", 0, err
	}
	return createRequest.Name, droplet.ID, nil
}

func Delete(token string, id int) error {
	client := godo.NewFromToken(token)
	ctx := context.TODO()

	_, err := client.Droplets.Delete(ctx, id)
	return err
}
