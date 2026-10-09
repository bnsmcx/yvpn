// Package addons is what a yVPN node can run besides being an exit node: the
// catalog, the droplet each add-on needs, the secrets a node is given, and the
// cloud-init that builds it.
//
// The web app (apps/web/index.html) has its own copy of all of this, in
// JavaScript. Both are held to the same golden files in testdata/parity at the
// repository root, by this package's tests and by the web app's e2e test, so a
// node built from either front end is byte-for-byte the same node.
package addons

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// Needs is the smallest droplet that can run something.
type Needs struct {
	VCPUs    int `json:"vcpus"`
	MemoryMB int `json:"memoryMB"`
	DiskGB   int `json:"diskGB"`
}

// Addon is one entry in the catalog. The plain exit node is the entry with an
// empty ID, so the size rules have one place to look.
type Addon struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Needs   Needs  `json:"needs"`
}

// Catalog lists what a node can be created as. Jellyfin wants two cores and
// 4 GB to convert a film in reasonable time and serve a family at once, and
// the disk to hold a few films twice over while they are converted.
var Catalog = []Addon{
	{
		ID:      "",
		Name:    "Exit node",
		Summary: "Just the VPN: route your traffic out through this datacenter.",
		// Tailscale needs far less; 512 MB is the smallest droplet sold.
		Needs: Needs{VCPUs: 1, MemoryMB: 512, DiskGB: 0},
	},
	{
		ID:      "jellyfin",
		Name:    "Jellyfin watch party",
		Summary: "A private media server: drop in a video, then watch it in sync with family, on your tailnet or through a guest link.",
		Needs:   Needs{VCPUs: 2, MemoryMB: 4096, DiskGB: 50},
	},
}

// Lookup finds a catalog entry by ID; "" is the plain exit node.
func Lookup(id string) (Addon, bool) {
	for _, a := range Catalog {
		if a.ID == id {
			return a, true
		}
	}
	return Addon{}, false
}

// IDs lists the add-ons, without the plain exit node.
func IDs() []string {
	var ids []string
	for _, a := range Catalog {
		if a.ID != "" {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

// TagPrefix marks a droplet's add-on: "yvpn-addon:jellyfin". DigitalOcean tags
// allow colons, and the tag is how any front end tells what a node runs.
const TagPrefix = "yvpn-addon:"

// FromTags returns the add-on a droplet's tags name, or "".
func FromTags(tags []string) string {
	for _, t := range tags {
		if strings.HasPrefix(t, TagPrefix) {
			return strings.TrimPrefix(t, TagPrefix)
		}
	}
	return ""
}

/* -------------------------------- secrets -------------------------------- */

// Secrets is what a node with an add-on is set up with. None of it is stored
// anywhere but the node: each value is an HMAC of the droplet's name keyed by
// the DigitalOcean token, so whoever holds the token can work them out again,
// in any browser or terminal, and the token itself never leaves for the node.
type Secrets struct {
	AgentToken    string `json:"agentToken"`
	AdminUser     string `json:"adminUser"`
	AdminPassword string `json:"adminPassword"`
	GuestUser     string `json:"guestUser"`
	GuestPassword string `json:"guestPassword"`
}

func mac(doToken, purpose, name string) []byte {
	h := hmac.New(sha256.New, []byte(doToken))
	h.Write([]byte("yvpn/" + purpose + "/" + name))
	return h.Sum(nil)
}

// The guest password is read aloud and typed on a TV remote, so it is built of
// syllables: no l (it reads as 1 or I), no q, x or y, no capitals.
const (
	consonants = "bdfghjkmnprstvwz"
	vowels     = "aeiou"
)

func Derive(doToken, name string) Secrets {
	admin := base32.StdEncoding.EncodeToString(mac(doToken, "admin", name))
	admin = strings.ToLower(admin[:20])

	g := mac(doToken, "guest", name)
	var parts []string
	for i := 0; i < 3; i++ {
		b := g[4*i : 4*i+4]
		parts = append(parts, string([]byte{
			consonants[int(b[0])%len(consonants)], vowels[int(b[1])%len(vowels)],
			consonants[int(b[2])%len(consonants)], vowels[int(b[3])%len(vowels)],
		}))
	}
	n := (int(g[12])<<8|int(g[13]))%90 + 10

	return Secrets{
		AgentToken:    base64.RawURLEncoding.EncodeToString(mac(doToken, "agent", name)),
		AdminUser:     "admin",
		AdminPassword: admin[0:5] + "-" + admin[5:10] + "-" + admin[10:15] + "-" + admin[15:20],
		GuestUser:     "guest",
		GuestPassword: strings.Join(parts, "-") + "-" + itoa2(n),
	}
}

func itoa2(n int) string { return string([]byte{byte('0' + n/10), byte('0' + n%10)}) }

// nodeConfig is /etc/yvpn/node.json, field for field what the agent reads.
type nodeConfig struct {
	Addon         string `json:"addon"`
	Token         string `json:"token"`
	AdminUser     string `json:"adminUser"`
	AdminPassword string `json:"adminPassword"`
	GuestUser     string `json:"guestUser"`
	GuestPassword string `json:"guestPassword"`
}

// NodeConfig is the agent's config file, on one line.
func NodeConfig(addon string, s Secrets) string {
	b, err := json.Marshal(nodeConfig{addon, s.AgentToken, s.AdminUser, s.AdminPassword, s.GuestUser, s.GuestPassword})
	if err != nil {
		panic(err)
	}
	return string(b)
}
