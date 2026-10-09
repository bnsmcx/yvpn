// Command yvpn-node is the agent that runs on a yVPN node with an add-on. It
// installs the add-on (today: Jellyfin, for a watch party), then serves the
// small HTTP API the yVPN dashboard drives it with: setup progress, resumable
// uploads, fetching a video from a link, converting each video so every device
// plays it directly, and opening Jellyfin to guests through Tailscale Funnel.
//
// cloud-init downloads it from the release matching the dashboard's version,
// writes /etc/yvpn/node.json, and runs `yvpn-node bootstrap`, which installs it
// as a systemd service running `yvpn-node serve`.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// VERSION is set at release build time, to the same version as the dashboard
// that downloads it.
var VERSION = "dev"

const unitPath = "/etc/systemd/system/yvpn-node.service"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		config := fs.String("config", "/etc/yvpn/node.json", "config file")
		fs.Parse(os.Args[2:])
		serve(*config)
	case "bootstrap":
		bootstrap()
	case "version", "--version":
		fmt.Println(VERSION)
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: yvpn-node serve [-config path] | bootstrap | version")
	os.Exit(2)
}

func serve(configPath string) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	a := newAgent(cfg, execRunner{}, VERSION)
	a.load()
	go a.prepWorker()
	go a.runInstall(context.Background())

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           a.handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}
	log.Printf("yvpn-node %s listening on %s", VERSION, cfg.Listen)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// bootstrap installs the agent as a service and starts it. cloud-init calls it
// once; systemd keeps it running, and restarts it after a reboot.
func bootstrap() {
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	unit := fmt.Sprintf(`[Unit]
Description=yVPN node agent
After=network-online.target tailscaled.service
Wants=network-online.target

[Service]
ExecStart=%s serve
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
`, exe)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		panic(err)
	}
	r := execRunner{}
	ctx := context.Background()
	if _, err := r.Run(ctx, "systemctl", "daemon-reload"); err != nil {
		panic(err)
	}
	if _, err := r.Run(ctx, "systemctl", "enable", "--now", "yvpn-node"); err != nil {
		panic(err)
	}
	fmt.Println("yvpn-node is running")
}
