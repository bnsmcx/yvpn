package main

import (
	"fmt"
	"os"
	"strconv"
	"yvpn/digital_ocean"
	"yvpn/tailscale"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "yvpn",
	Short: "A simple CLI app to create and delete tailscale exit nodes",
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new tailscale exit node",
	Run: func(cmd *cobra.Command, args []string) {
		digitalOceanToken := os.Getenv("DIGITAL_OCEAN_TOKEN")
		tailscaleAuth := os.Getenv("TAILSCALE_AUTH")
    tailscaleAPI := os.Getenv("TAILSCALE_API")
		if digitalOceanToken == "" || tailscaleAuth == "" {
      fmt.Println("Error: ensure all env variables are set: DIGITAL_OCEAN_TOKEN, TAILSCALE_AUTH, TAILSCALE_API, and TAILNET")
			os.Exit(1)
		}
		handleCreate(digitalOceanToken, tailscaleAuth, tailscaleAPI)
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete [node_id]",
	Short: "Delete a node with the given node_id using environment variables",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		digitalOceanToken := os.Getenv("DIGITAL_OCEAN_TOKEN")
		if digitalOceanToken == "" {
			fmt.Println("Error: DIGITAL_OCEAN_TOKEN environment variable is required")
			os.Exit(1)
		}
		nodeID, err := strconv.Atoi(args[0])
    if err != nil {
			fmt.Println("Error: node_id should be an integer")
			os.Exit(1)
    }
  
		handleDelete(digitalOceanToken, nodeID)
	},
}

func init() {
	// Registering the subcommands to the root command
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(deleteCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func handleCreate(digitalOceanToken, tailscaleAuth, tailscaleAPI string) {
	name, id, err := digital_ocean.Create(digitalOceanToken, tailscaleAuth)
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
