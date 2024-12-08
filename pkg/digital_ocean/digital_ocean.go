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
package_update: true
package_upgrade: true
packages:
  - curl

runcmd:
  # Install Tailscale
  - curl -fsSL https://tailscale.com/install.sh | sh
  
  # Authenticate and join the Tailscale network using your auth key
  - sudo tailscale up --authkey %s --advertise-exit-node

  # Optional: Enable IP forwarding for proper routing
  - echo "net.ipv4.ip_forward=1" | sudo tee -a /etc/sysctl.conf
  - echo "net.ipv6.conf.all.forwarding=1" | sudo tee -a /etc/sysctl.conf
  - sudo sysctl -p

  # Optional: Set up firewall rules to allow traffic forwarding
  - sudo iptables -A FORWARD -i tailscale0 -j ACCEPT
  - sudo iptables -A FORWARD -o tailscale0 -j ACCEPT
  - sudo iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE

final_message: "Tailscale exit node setup complete."
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
