package digital_ocean

import (
	"context"
	"fmt"
	"slices"
	"strings"
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

// image is what every node boots. Not every region carries it, and it sets a
// floor on disk size.
const image = "ubuntu-24-04-x64"

// minMemoryMB is the least memory a node may have. Tailscale needs far less
// than this; 512 MB is the smallest droplet DigitalOcean sells.
const minMemoryMB = 512

// Datacenter is a region a node can be created in, with the cheapest size
// there that can run one.
type Datacenter struct {
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Size        string  `json:"size"`
	PriceHourly float64 `json:"price_hourly"`
}

// usable reports whether a size can run a node: big enough, and a plain CPU
// droplet. GPU droplets need their own images and cost dollars an hour.
func usable(s godo.Size, minDisk int) bool {
	return s.Available &&
		s.Memory >= minMemoryMB &&
		s.Disk >= minDisk &&
		!strings.HasPrefix(s.Slug, "gpu-") &&
		!strings.Contains(strings.ToUpper(s.Description), "GPU")
}

// FetchDatacenters lists every region a node can be created in, sorted by
// slug, each with the cheapest usable size it offers. Regions without one
// (no usable size, or no Ubuntu image) are left out rather than failing later
// at create time.
func FetchDatacenters(token string) ([]Datacenter, error) {
	client := godo.NewFromToken(token)
	ctx := context.TODO()
	opts := &godo.ListOptions{Page: 1, PerPage: 200}

	regions, _, err := client.Regions.List(ctx, opts)
	if err != nil {
		return nil, err
	}
	sizes, _, err := client.Sizes.List(ctx, opts)
	if err != nil {
		return nil, err
	}
	img, _, err := client.Images.GetBySlug(ctx, image)
	if err != nil {
		return nil, err
	}

	return pickDatacenters(regions, sizes, img), nil
}

// pickDatacenters is FetchDatacenters without the API calls.
func pickDatacenters(regions []godo.Region, sizes []godo.Size, img *godo.Image) []Datacenter {
	var datacenters []Datacenter
	for _, r := range regions {
		if !r.Available || !slices.Contains(img.Regions, r.Slug) {
			continue
		}
		var best *godo.Size
		for i, s := range sizes {
			if !usable(s, img.MinDiskSize) || !slices.Contains(s.Regions, r.Slug) {
				continue
			}
			if best == nil || s.PriceHourly < best.PriceHourly {
				best = &sizes[i]
			}
		}
		if best == nil {
			continue
		}
		datacenters = append(datacenters, Datacenter{
			Slug:        r.Slug,
			Name:        r.Name,
			Size:        best.Slug,
			PriceHourly: best.PriceHourly,
		})
	}

	slices.SortFunc(datacenters, func(a, b Datacenter) int {
		return strings.Compare(a.Slug, b.Slug)
	})

	return datacenters
}

func Create(token, tailscaleAuth, datacenter string) (string, int, error) {
	client := godo.NewFromToken(token)
	ctx := context.TODO()

	datacenters, err := FetchDatacenters(token)
	if err != nil {
		return "", 0, err
	}
	i := slices.IndexFunc(datacenters, func(d Datacenter) bool { return d.Slug == datacenter })
	if i < 0 {
		return "", 0, fmt.Errorf("can't create a node in %q: no such region, or it has no droplet size that can run one", datacenter)
	}
	size := datacenters[i].Size

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
  # Tailscale installs its own netfilter rules for an exit node, so the manual
  # iptables FORWARD/MASQUERADE rules that used to be here are not needed.
  - tailscale up --authkey %s --advertise-exit-node

final_message: "yVPN exit node ready."
`, tailscaleAuth)

	createRequest := &godo.DropletCreateRequest{
		Tags:   []string{"yVPN"},
		Name:   fmt.Sprintf("%s-yvpn-%d", datacenter, time.Now().Unix()),
		Region: datacenter,
		Size:   size,
		Image: godo.DropletCreateImage{
			Slug: image,
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
