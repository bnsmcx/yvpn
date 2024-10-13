package main

import (
	"fmt"
	"os"

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
		token := os.Getenv("DIGITAL_OCEAN_TOKEN")
		key := os.Getenv("TAILSCALE_TOKEN")
		if token == "" || key == "" {
			fmt.Println("Error: both DIGITAL_OCEAN_TOKEN and TAILSCALE_TOKEN environment variables are required")
			os.Exit(1)
		}
		handleCreate(token, key)
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete [node_id]",
	Short: "Delete a node with the given node_id using environment variables",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		token := os.Getenv("DIGITAL_OCEAN_TOKEN")
		if token == "" {
			fmt.Println("Error: DIGITAL_OCEAN_TOKEN environment variable is required")
			os.Exit(1)
		}
		nodeID := args[0]
		handleDelete(token, nodeID)
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

func handleCreate(token string, key string) {
	// Logic for creating with token and key
	fmt.Printf("Creating key: %s with token: %s\n", key, token)
}

func handleDelete(token string, id string) {
	// Logic for deleting with token and id
	fmt.Printf("Deleting id: %s with token: %s\n", id, token)
}

