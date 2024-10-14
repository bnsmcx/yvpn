package digital_ocean

import (
	"context"
	"fmt"

	"github.com/digitalocean/godo"
)

func Create(digitalOceanToken, tailscaleToken string) (int, error) {
	client := godo.NewFromToken(digitalOceanToken)
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
`, tailscaleToken)

	createRequest := &godo.DropletCreateRequest{
		Name:   "nyc3-yvpn-digital-ocean",
		Region: "nyc3",
		Size:   "s-1vcpu-1gb",
		Image: godo.DropletCreateImage{
			Slug: "ubuntu-20-04-x64",
		},
		UserData: cloudInit, // Cloud-init script for Tailscale exit node
	}

	droplet, _, err := client.Droplets.Create(ctx, createRequest)
	if err != nil {
		return 0, err
	}
	return droplet.ID, nil
}

func Delete(digitalOceantoken string, id int) error {
	client := godo.NewFromToken(digitalOceantoken)
	ctx := context.TODO()

	_, err := client.Droplets.Delete(ctx, id)
	return err
}
