package main

import (
	"fmt"
	"os"
	"strconv"
	"yvpn/pkg/digital_ocean"
	"yvpn/pkg/tailscale"
)

func main() {
	switch os.Args[1] {
	case "create":
		do := os.Getenv("DIGITAL_OCEAN_TOKEN")
		tsAuth := os.Getenv("TAILSCALE_API")
		datacenter := os.Args[2]
		handleCreate(do, tsAuth, datacenter)
	case "delete":
		do := os.Getenv("DIGITAL_OCEAN_TOKEN")
		id, _ := strconv.Atoi(os.Args[2])
		handleDelete(do, id)
	case "datacenters":
		do := os.Getenv("DIGITAL_OCEAN_TOKEN")
		handleFetchDatacenters(do)
	case "newkey":
		tsAPI := os.Getenv("TAILSCALE_API")
    handleFetchTSKey(tsAPI)
  case "killkey":
		tsAPI := os.Getenv("TAILSCALE_API")
    handleKillTSKey(tsAPI)
	}
}

func handleKillTSKey(tsAPI string) {
  err := tailscale.DeleteAuthKey(tsAPI, os.Args[2])
  if err != nil {
		fmt.Println(err)
		os.Exit(1)
  }
  fmt.Println("Deleted key:", os.Args[2])
}

func handleFetchTSKey(tailscaleAPI string) {
	key, id, err := tailscale.GetAuthKey(tailscaleAPI)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
  fmt.Println("New Key:", key, id)
}

func handleFetchDatacenters(digitalOceanToken string) {
	datacenters, err := digital_ocean.FetchDatacenters(digitalOceanToken)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println("Available datacenters:")
	for _, dc := range datacenters {
		fmt.Printf("\t%s\n", dc)
	}
}

func handleCreate(digitalOceanToken, tailscaleAPI, datacenter string) {
  tailscaleAuth, tsKeyID, err := tailscale.GetAuthKey(tailscaleAPI)
  if err != nil {
    fmt.Println("getting tailscale key:", err)
		os.Exit(1)
  }

	name, id, err := digital_ocean.Create(digitalOceanToken, tailscaleAuth, datacenter)
	if err != nil {
    fmt.Println("creating droplet:", err)
		os.Exit(1)
	}
	fmt.Printf("Created new VPS at Digital Ocean: %s [id=%d]\n", name, id)

	fmt.Println("\tEnabling exit node...")
	elapsed, err := tailscale.EnableExit(name, tailscaleAPI)
	if err != nil {
    fmt.Printf("\tenabling tailscale exit: %s\n", err.Error())
		handleDelete(digitalOceanToken, id)
    tailscale.DeleteAuthKey(tailscaleAPI, tsKeyID)
		os.Exit(1)
	}
  
  err = tailscale.DeleteAuthKey(tailscaleAPI, tsKeyID)
	if err != nil {
    fmt.Println("deleting tailscale key:", err)
		os.Exit(1)
	}

	fmt.Printf("\tExit node activated after %d seconds\n", elapsed)
	fmt.Println("\tActivation complete")
}

func handleDelete(token string, id int) {
	if err := digital_ocean.Delete(token, id); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("Deleted tailscale exit node: %d\n", id)
}
