package do

import (
	"context"
	"fmt"
	"github.com/digitalocean/godo"
	"github.com/google/uuid"
	"log"
	"time"
	"yvpn_server/db"
	"yvpn_server/wg"
)

type NewEndpoint struct {
	Token      string
	AccountID  uuid.UUID
	Datacenter string
}

func (e *NewEndpoint) Create() error {
	client := godo.NewFromToken(e.Token)
	ctx := context.TODO()

	priv, pub, err := wg.GenerateKeys()
	if err != nil {
		return err
	}
	serverKeys := wg.Keys{
		Public:  pub,
		Private: priv,
	}

	var clientKeys = make(map[string]wg.Keys)

	for i := 2; i <= 255; i++ {
		priv, pub, err := wg.GenerateKeys()
		if err != nil {
			return err
		}
		keys := wg.Keys{
			Public:  pub,
			Private: priv,
		}
		clientKeys[fmt.Sprintf("10.0.0.%d", i)] = keys
	}

	serverConfig, err := wg.GenerateServerConfig(serverKeys, clientKeys)
	if err != nil {
		return err
	}

	cloudInit, err := wg.GenerateCloudInit(serverConfig)
	if err != nil {
		return err
	}

	createRequest := &godo.DropletCreateRequest{
		Name:     "yvpn-" + e.Datacenter,
		Region:   e.Datacenter,
		Size:     "s-1vcpu-1gb",
		UserData: cloudInit,
		Image: godo.DropletCreateImage{
			Slug: "ubuntu-22-04-x64",
		},
	}
	droplet, _, err := client.Droplets.Create(ctx, createRequest)
	if err != nil {
		return err
	}

	go awaitIPandUpdateEndpoint(e.Token, droplet.ID, clientKeys)

	endpoint := db.Endpoint{
		ID:         droplet.ID,
		Datacenter: droplet.Region.Slug,
		AccountID:  e.AccountID,
		PublicKey:  serverKeys.Public.String(),
		PrivateKey: serverKeys.Private.String(),
	}

	err = endpoint.Save()
	if err != nil {
		return err
	}

	return nil
}

func DeleteEndpoint(id int, token string) error {
	client := godo.NewFromToken(token)
	ctx := context.TODO()

	_, err := client.Droplets.Delete(ctx, id)
	return err
}

func awaitIPandUpdateEndpoint(token string, id int, clients map[string]wg.Keys) {
	client := godo.NewFromToken(token)
	for i := 0; i < 360; i++ {
		time.Sleep(time.Second)
		droplet, _, err := client.Droplets.Get(context.TODO(), id)
		if err != nil {
			log.Println(err)
			return
		} else if droplet.Status != "active" {
			continue
		}

		ip, err := droplet.PublicIPv4()
		if err != nil {
			log.Println(err)
			return
		}
		err = db.UpdateEndpointIPandClients(id, ip, clients)
		if err != nil {
			log.Println(err)
			return
		}
		return
	}
}
