package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"yvpn/pkg/digital_ocean"
	"yvpn/pkg/tailscale"
)

func runCLI(args []string) {
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	cmd := args[0]
	jsonOutput := hasFlag(args, "--json")

	switch cmd {
	case "list":
		doToken := requireEnv("DIGITAL_OCEAN_TOKEN")
		cmdList(doToken, jsonOutput)
	case "datacenters":
		doToken := requireEnv("DIGITAL_OCEAN_TOKEN")
		cmdDatacenters(doToken, jsonOutput)
	case "create":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Error: datacenter argument required")
			fmt.Fprintln(os.Stderr, "Usage: yvpn create <datacenter>")
			os.Exit(1)
		}
		doToken := requireEnv("DIGITAL_OCEAN_TOKEN")
		tsToken := requireEnv("TAILSCALE_API")
		cmdCreate(doToken, tsToken, args[1], jsonOutput)
	case "delete":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Error: id argument required")
			fmt.Fprintln(os.Stderr, "Usage: yvpn delete <id>")
			os.Exit(1)
		}
		doToken := requireEnv("DIGITAL_OCEAN_TOKEN")
		id, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid id '%s' (must be a number)\n", args[1])
			os.Exit(1)
		}
		cmdDelete(doToken, id, jsonOutput)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command '%s'\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func requireEnv(name string) string {
	val, ok := os.LookupEnv(name)
	if !ok || val == "" {
		fmt.Fprintf(os.Stderr, "Error: %s environment variable is required\n", name)
		os.Exit(1)
	}
	return val
}

func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func printUsage() {
	fmt.Printf(`yvpn version %s

Usage:
  yvpn tui                  Launch interactive TUI
  yvpn list [--json]        List existing exit nodes
  yvpn datacenters [--json] List available datacenters
  yvpn create <datacenter>  Create new exit node in datacenter
  yvpn delete <id>          Delete exit node by ID
  yvpn ssh                  Start SSH server mode
  yvpn --help               Show this help
  yvpn --version            Show version

Environment variables:
  DIGITAL_OCEAN_TOKEN       DigitalOcean API token (required)
  TAILSCALE_API             Tailscale API key (required for create)
`, VERSION)
}

func cmdList(token string, jsonOutput bool) {
	nodes, err := digital_ocean.FetchExitNodes(token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(nodes)
		return
	}

	if len(nodes) == 0 {
		fmt.Println("No exit nodes found")
		return
	}

	fmt.Printf("%-40s %s\n", "NAME", "ID")
	for _, node := range nodes {
		fmt.Printf("%-40s %d\n", node.Name, node.ID)
	}
}

func cmdDatacenters(token string, jsonOutput bool) {
	datacenters, err := digital_ocean.FetchDatacenters(token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(datacenters)
		return
	}

	if len(datacenters) == 0 {
		fmt.Println("No datacenters available")
		return
	}

	fmt.Println("DATACENTER")
	for _, dc := range datacenters {
		fmt.Println(dc)
	}
}

func cmdCreate(doToken, tsToken, datacenter string, jsonOutput bool) {
	startTime := time.Now()

	if !jsonOutput {
		fmt.Println("Getting auth key from Tailscale...")
	}

	authKey, authKeyID, err := tailscale.GetAuthKey(tsToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting Tailscale auth key: %v\n", err)
		os.Exit(1)
	}

	if !jsonOutput {
		fmt.Printf("Provisioning droplet in %s...\n", datacenter)
	}

	name, id, err := digital_ocean.Create(doToken, authKey, datacenter)
	if err != nil {
		// Clean up auth key on failure
		tailscale.DeleteAuthKey(tsToken, authKeyID)
		fmt.Fprintf(os.Stderr, "Error creating droplet: %v\n", err)
		os.Exit(1)
	}

	if !jsonOutput {
		fmt.Println("Waiting for exit node to register with Tailscale...")
	}

	elapsed, err := tailscale.EnableExit(name, tsToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error enabling exit node: %v\n", err)
		fmt.Fprintf(os.Stderr, "Droplet was created (ID: %d) but may need manual cleanup\n", id)
		os.Exit(1)
	}

	// Clean up the auth key after successful registration
	tailscale.DeleteAuthKey(tsToken, authKeyID)

	totalTime := time.Since(startTime).Seconds()

	if jsonOutput {
		result := map[string]interface{}{
			"name":    name,
			"id":      id,
			"elapsed": int(totalTime),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
		return
	}

	fmt.Printf("Exit node created: %s (ID: %d)\n", name, id)
	fmt.Printf("Ready in %d seconds (Tailscale registration: %ds)\n", int(totalTime), elapsed)
}

func cmdDelete(token string, id int, jsonOutput bool) {
	err := digital_ocean.Delete(token, id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting droplet: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		result := map[string]interface{}{
			"deleted": true,
			"id":      id,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
		return
	}

	fmt.Printf("Exit node %d deleted\n", id)
}
