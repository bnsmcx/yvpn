package tailscale

import (
	"sort"
	"testing"
)

func TestParsePaths(t *testing.T) {
	raw := []byte(`{"Peer":{
	  "a":{"HostName":"nyc1-yvpn-1","ExitNode":true,"Active":true,"CurAddr":"203.0.113.5:41641","Relay":"nyc"},
	  "b":{"HostName":"fra1-yvpn-2","Active":true,"CurAddr":"","Relay":"fra"},
	  "c":{"HostName":"lon1-yvpn-3","Active":true,"PeerRelay":"198.51.100.7:40000:vni:9","Relay":"lhr"},
	  "d":{"HostName":"phone","Active":false,"Relay":"sea"}}}`)
	got, err := ParsePaths(raw)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Name < got[j].Name })
	want := map[string]string{"fra1-yvpn-2": PathDERP, "lon1-yvpn-3": PathPeerRelay, "nyc1-yvpn-1": PathDirect, "phone": PathIdle}
	for _, p := range got {
		if want[p.Name] != p.Path {
			t.Errorf("%s: got %q, want %q", p.Name, p.Path, want[p.Name])
		}
	}
	if !got[2].ExitNode || got[2].Via != "203.0.113.5:41641" {
		t.Errorf("exit node row wrong: %+v", got[2])
	}
}
