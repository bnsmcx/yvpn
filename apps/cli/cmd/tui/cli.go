package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"yvpn/pkg/addons"
	"yvpn/pkg/digital_ocean"
	"yvpn/pkg/tailscale"
)

// cliArgs is a command's arguments split into positionals and flags. Flags may
// come anywhere: `create fra1 --addon jellyfin` or `create --addon=jellyfin fra1`.
type cliArgs struct {
	pos   []string
	flags map[string]string // "--json" -> "", "--addon" -> "jellyfin"
}

// valueFlags take a value; every other flag is a switch.
var valueFlags = map[string]bool{"--addon": true}

func parseArgs(args []string) cliArgs {
	a := cliArgs{flags: map[string]string{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			a.pos = append(a.pos, arg)
			continue
		}
		name, val, hasVal := strings.Cut(arg, "=")
		if valueFlags[name] && !hasVal && i+1 < len(args) {
			i++
			val = args[i]
		}
		a.flags[name] = val
	}
	return a
}

func (a cliArgs) has(flag string) bool { _, ok := a.flags[flag]; return ok }

func runCLI(args []string) {
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	cmd := args[0]
	a := parseArgs(args[1:])
	jsonOutput := a.has("--json")
	addon := a.flags["--addon"]
	if _, ok := addons.Lookup(addon); !ok {
		fmt.Fprintf(os.Stderr, "Error: no add-on called %q (there is: %s)\n", addon, strings.Join(addons.IDs(), ", "))
		os.Exit(1)
	}

	switch cmd {
	case "list":
		doToken := requireEnv("DIGITAL_OCEAN_TOKEN")
		cmdList(doToken, jsonOutput)
	case "datacenters":
		doToken := requireEnv("DIGITAL_OCEAN_TOKEN")
		cmdDatacenters(doToken, addon, jsonOutput)
	case "create":
		if len(a.pos) < 1 {
			fmt.Fprintln(os.Stderr, "Error: datacenter argument required")
			fmt.Fprintln(os.Stderr, "Usage: yvpn create <datacenter> [--addon jellyfin] [--no-exit]")
			os.Exit(1)
		}
		exit := !a.has("--no-exit")
		if !exit && addon == "" {
			fmt.Fprintln(os.Stderr, "Error: --no-exit needs an --addon; a node with neither would do nothing")
			os.Exit(1)
		}
		doToken := requireEnv("DIGITAL_OCEAN_TOKEN")
		tsToken := requireEnv("TAILSCALE_API")
		cmdCreate(doToken, tsToken, a.pos[0], digital_ocean.Options{Addon: addon, Exit: exit, Version: VERSION}, jsonOutput)
	case "delete", "access":
		if len(a.pos) < 1 {
			fmt.Fprintln(os.Stderr, "Error: id argument required")
			fmt.Fprintf(os.Stderr, "Usage: yvpn %s <id>\n", cmd)
			os.Exit(1)
		}
		doToken := requireEnv("DIGITAL_OCEAN_TOKEN")
		id, err := strconv.Atoi(a.pos[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid id '%s' (must be a number)\n", a.pos[0])
			os.Exit(1)
		}
		if cmd == "delete" {
			cmdDelete(doToken, id, jsonOutput)
		} else {
			cmdAccess(doToken, os.Getenv("TAILSCALE_API"), id, jsonOutput)
		}
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

func printUsage() {
	fmt.Printf(`yvpn version %s

Usage:
  yvpn tui                    Launch interactive TUI
  yvpn list [--json]          List existing nodes
  yvpn datacenters [--json]   List available datacenters
        [--addon <name>]      ...priced for a node running that add-on
  yvpn create <datacenter>    Create a new exit node in datacenter
        [--addon <name>]      ...also running an add-on (jellyfin)
        [--no-exit]           ...not as an exit node (needs --addon)
  yvpn access <id> [--json]   Show how to reach a node's add-on, and its passwords
  yvpn delete <id>            Delete a node by ID
  yvpn ssh                    Start SSH server mode
  yvpn --help                 Show this help
  yvpn --version              Show version

Add-ons:
  jellyfin                    A watch-party media server (2 vCPU, 4 GB). Upload
                              and share from the yVPN web dashboard.

Environment variables:
  DIGITAL_OCEAN_TOKEN         DigitalOcean API token (required)
  TAILSCALE_API               Tailscale API key (required for create and access)
`, VERSION)
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func cmdList(token string, jsonOutput bool) {
	nodes, err := digital_ocean.FetchExitNodes(token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		printJSON(nodes)
		return
	}

	if len(nodes) == 0 {
		fmt.Println("No nodes found")
		return
	}

	fmt.Printf("%-40s %-12s %s\n", "NAME", "ADDON", "ID")
	for _, node := range nodes {
		addon := node.Addon
		if addon == "" {
			addon = "-"
		}
		fmt.Printf("%-40s %-12s %d\n", node.Name, addon, node.ID)
	}
}

func cmdDatacenters(token, addon string, jsonOutput bool) {
	datacenters, err := digital_ocean.FetchDatacenters(token, addon)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		printJSON(datacenters)
		return
	}

	if len(datacenters) == 0 {
		fmt.Println("No datacenters available")
		return
	}

	fmt.Printf("%-12s %-12s %-22s %s\n", "DATACENTER", "PRICE/HR", "SIZE", "NAME")
	for _, dc := range datacenters {
		fmt.Printf("%-12s %-12s %-22s %s\n", dc.Slug, fmt.Sprintf("$%.4f", dc.PriceHourly), dc.Size, dc.Name)
	}
}

func cmdCreate(doToken, tsToken, datacenter string, opts digital_ocean.Options, jsonOutput bool) {
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

	name, id, err := digital_ocean.Create(doToken, authKey, datacenter, opts)
	if err != nil {
		// Clean up auth key on failure
		tailscale.DeleteAuthKey(tsToken, authKeyID)
		fmt.Fprintf(os.Stderr, "Error creating droplet: %v\n", err)
		os.Exit(1)
	}

	if !jsonOutput {
		fmt.Println("Waiting for the node to register with Tailscale...")
	}

	elapsed, err := tailscale.WaitForNode(name, tsToken, opts.Exit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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
		if opts.Addon != "" {
			result["addon"] = opts.Addon
			result["access"] = accessFor(doToken, name, deviceName(tsToken, name))
		}
		printJSON(result)
		return
	}

	fmt.Printf("Node created: %s (ID: %d)\n", name, id)
	fmt.Printf("Ready in %d seconds (Tailscale registration: %ds)\n", int(totalTime), elapsed)
	if opts.Addon != "" {
		fmt.Printf("\n%s is installing on the node; it takes a few minutes.\n\n", opts.Addon)
		printAccess(accessFor(doToken, name, deviceName(tsToken, name)))
	}
}

// access is how to reach a node's add-on.
type access struct {
	URL           string `json:"url,omitempty"`
	ControlURL    string `json:"controlUrl,omitempty"`
	AdminUser     string `json:"adminUser"`
	AdminPassword string `json:"adminPassword"`
	GuestUser     string `json:"guestUser"`
	GuestPassword string `json:"guestPassword"`
	AgentToken    string `json:"agentToken"`
}

// deviceName is the node's MagicDNS name, or "" if it can't be found.
func deviceName(tsToken, name string) string {
	if tsToken == "" {
		return ""
	}
	dev, err := tailscale.FindDevice(name, tsToken)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(dev.Name, ".")
}

func accessFor(doToken, name, fqdn string) access {
	s := addons.Derive(doToken, name)
	a := access{AdminUser: s.AdminUser, AdminPassword: s.AdminPassword,
		GuestUser: s.GuestUser, GuestPassword: s.GuestPassword, AgentToken: s.AgentToken}
	if fqdn != "" {
		a.URL = "https://" + fqdn + "/"
		a.ControlURL = "https://" + fqdn + ":8443/v1/status"
	}
	return a
}

func printAccess(a access) {
	if a.URL != "" {
		fmt.Printf("  Jellyfin        %s   (on your tailnet)\n", a.URL)
	} else {
		fmt.Println("  Jellyfin        https://<node>.<tailnet>.ts.net/  (the node isn't on the tailnet yet)")
	}
	fmt.Printf("  Admin           %s / %s\n", a.AdminUser, a.AdminPassword)
	fmt.Printf("  Guest           %s / %s   (signs in only while sharing is on)\n", a.GuestUser, a.GuestPassword)
	fmt.Println("\n  Upload videos and turn on guest sharing from the yVPN web dashboard: open the node's row.")
	if a.ControlURL != "" {
		fmt.Printf("  The agent's API: curl -H 'Authorization: Bearer %s' %s\n", a.AgentToken, a.ControlURL)
	}
}

func cmdAccess(doToken, tsToken string, id int, jsonOutput bool) {
	nodes, err := digital_ocean.FetchExitNodes(doToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	var node *digital_ocean.ExitNode
	for i := range nodes {
		if nodes[i].ID == id {
			node = &nodes[i]
		}
	}
	switch {
	case node == nil:
		fmt.Fprintf(os.Stderr, "Error: no yVPN node with id %d\n", id)
		os.Exit(1)
	case node.Addon == "":
		fmt.Fprintf(os.Stderr, "%s is a plain exit node: there is nothing to sign in to.\n", node.Name)
		os.Exit(1)
	}
	a := accessFor(doToken, node.Name, deviceName(tsToken, node.Name))
	if jsonOutput {
		printJSON(a)
		return
	}
	fmt.Printf("%s (%s)\n\n", node.Name, node.Addon)
	printAccess(a)
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
		printJSON(result)
		return
	}

	fmt.Printf("Node %d deleted\n", id)
}
