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
  - sudo tailscale up --authkey <YOUR_TAILSCALE_AUTH_KEY> --advertise-exit-node

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
		Name:   "example.com",
		Region: "nyc3",
		Size:   "s-1vcpu-1gb",
		Image: godo.DropletCreateImage{
			Slug: "ubuntu-20-04-x64",
		},
		SSHKeys: []godo.DropletCreateSSHKey{
			godo.DropletCreateSSHKey{ID: 289794},
			godo.DropletCreateSSHKey{Fingerprint: "3b:16:e4:bf:8b:00:8b:b8:59:8c:a9:d3:f0:19:fa:45"},
		},
		Backups:    true,
		IPv6:       true,
		Monitoring: true,
		Tags:       []string{"env:prod", "web"},
		UserData:   cloudInit, // Cloud-init script for Tailscale exit node
		VPCUUID:    "760e09ef-dc84-11e8-981e-3cfdfeaae000",
	}

	droplet, _, err := client.Droplets.Create(ctx, createRequest)
	if err != nil {
    return 0, err
	}
  return droplet.ID, nil
}

func Delete(digitalOceantoken, id string) error {
	return nil
}
