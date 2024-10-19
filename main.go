package main

import (
	"fmt"
	"os"
	"strconv"
	"yvpn/digital_ocean"
	"yvpn/tailscale"
)

func main() {
	switch os.Args[1] {
	case "create":
    do := os.Getenv("DIGITAL_OCEAN_TOKEN")
    tsAuth := os.Getenv("TAILSCALE_AUTH")
    tsAPI := os.Getenv("TAILSCALE_API")
    datacenter := os.Args[2]
		handleCreate(do, tsAuth, tsAPI, datacenter)
	case "delete":
    do := os.Getenv("DIGITAL_OCEAN_TOKEN")
    id, _ := strconv.Atoi(os.Args[2])
		handleDelete(do, id)
	}
}

func handleCreate(digitalOceanToken, tailscaleAuth, tailscaleAPI, datacenter string) {
	name, id, err := digital_ocean.Create(digitalOceanToken, tailscaleAuth, datacenter)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("Created new VPS at Digital Ocean: %s [id=%d]\n", name, id)

	fmt.Println("\tEnabling exit node...")
	elapsed, err := tailscale.EnableExit(name, tailscaleAPI)
	if err != nil {
		fmt.Printf("\t%s\n", err.Error())
		handleDelete(digitalOceanToken, id)
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
