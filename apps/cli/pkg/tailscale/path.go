package tailscale

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// How this device's traffic reaches a peer, as the local tailscaled sees it.
// Only "direct" gets an exit node's full speed; the other two go through a
// relay, and a speed run over one measures the relay as much as the node.
const (
	PathDirect    = "direct"
	PathPeerRelay = "peer relay"
	PathDERP      = "DERP"
	PathIdle      = "idle" // no traffic lately, so no path yet
)

// PeerPath is one peer's path, from `tailscale status --json` on this device.
type PeerPath struct {
	Name     string `json:"name"`      // the peer's hostname
	ExitNode bool   `json:"exit_node"` // this device is using it as its exit node
	Path     string `json:"path"`      // one of the Path* constants
	Via      string `json:"via"`       // the address, relay, or DERP region
}

type localStatus struct {
	Peer map[string]struct {
		HostName  string
		DNSName   string
		ExitNode  bool
		Active    bool
		CurAddr   string
		Relay     string
		PeerRelay string
	}
}

// ParsePaths reads `tailscale status --json` and returns every peer's path.
func ParsePaths(statusJSON []byte) ([]PeerPath, error) {
	var st localStatus
	if err := json.Unmarshal(statusJSON, &st); err != nil {
		return nil, err
	}
	var out []PeerPath
	for _, p := range st.Peer {
		pp := PeerPath{Name: p.HostName, ExitNode: p.ExitNode}
		switch {
		case p.CurAddr != "":
			pp.Path, pp.Via = PathDirect, p.CurAddr
		case p.PeerRelay != "":
			pp.Path, pp.Via = PathPeerRelay, p.PeerRelay
		case p.Active && p.Relay != "":
			pp.Path, pp.Via = PathDERP, p.Relay
		default:
			pp.Path, pp.Via = PathIdle, p.Relay
		}
		out = append(out, pp)
	}
	return out, nil
}

// LocalPath asks this device's tailscaled for the path to name (a hostname
// or a prefix of one), or to the exit node in use when name is "".
func LocalPath(name string) (PeerPath, error) {
	raw, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		return PeerPath{}, fmt.Errorf("tailscale status: %w (is Tailscale running on this device?)", err)
	}
	paths, err := ParsePaths(raw)
	if err != nil {
		return PeerPath{}, err
	}
	for _, p := range paths {
		if (name == "" && p.ExitNode) || (name != "" && strings.HasPrefix(p.Name, name)) {
			return p, nil
		}
	}
	if name == "" {
		return PeerPath{}, fmt.Errorf("this device isn't using an exit node")
	}
	return PeerPath{}, fmt.Errorf("no peer called %q on this tailnet", name)
}
