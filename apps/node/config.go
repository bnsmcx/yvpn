package main

import (
	"encoding/json"
	"errors"
	"os"
)

// Config is /etc/yvpn/node.json, which cloud-init writes before the agent
// starts. The first six fields come from whoever created the node (the web app
// or the CLI); they derive every secret from their DigitalOcean token and the
// droplet's name, so they can show them again later without storing anything.
// The rest are defaults that only tests and local runs override.
type Config struct {
	Addon         string `json:"addon"`
	Token         string `json:"token"`
	AdminUser     string `json:"adminUser"`
	AdminPassword string `json:"adminPassword"`
	GuestUser     string `json:"guestUser"`
	GuestPassword string `json:"guestPassword"`

	Listen      string `json:"listen,omitempty"`      // the agent's own HTTP server
	DataDir     string `json:"dataDir,omitempty"`     // state, uploads in progress, Jellyfin's config
	MediaDir    string `json:"mediaDir,omitempty"`    // finished videos, which Jellyfin serves
	Container   string `json:"container,omitempty"`   // the Jellyfin container's name
	Image       string `json:"image,omitempty"`       // the Jellyfin image, which also supplies ffmpeg
	JellyfinURL string `json:"jellyfinURL,omitempty"` // where Jellyfin listens on this machine
}

// The Jellyfin version is pinned by minor release: patch releases still arrive,
// a new minor (and its API) only when the agent is released against it.
const defaultImage = "jellyfin/jellyfin:12.2"

// controlPort is the tailnet HTTPS port the dashboard reaches the agent on.
// Tailscale serves HTTPS only on 443, 8443 and 10000, and 443 is Jellyfin's.
const controlPort = "8443"

func loadConfig(path string) (Config, error) {
	var c Config
	raw, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	c.applyDefaults()
	return c, c.validate()
}

func (c *Config) applyDefaults() {
	def := func(p *string, v string) {
		if *p == "" {
			*p = v
		}
	}
	def(&c.Listen, "127.0.0.1:8090")
	def(&c.DataDir, "/srv/yvpn")
	def(&c.MediaDir, "/srv/media")
	def(&c.Container, "yvpn-jellyfin")
	def(&c.Image, defaultImage)
	def(&c.JellyfinURL, "http://127.0.0.1:8096")
	def(&c.AdminUser, "admin")
	def(&c.GuestUser, "guest")
}

func (c Config) validate() error {
	switch {
	case c.Addon != "jellyfin":
		return errors.New("config: addon must be \"jellyfin\", got " + `"` + c.Addon + `"`)
	case len(c.Token) < 32:
		return errors.New("config: token is missing or too short")
	case c.AdminPassword == "" || c.GuestPassword == "":
		return errors.New("config: adminPassword and guestPassword are required")
	}
	return nil
}
