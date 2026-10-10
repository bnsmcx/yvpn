package addons

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The golden files are shared with the web app's e2e test, which renders the
// same inputs in the browser and compares against the same bytes.
//
//	go test ./pkg/addons -update    # after a deliberate change
var update = flag.Bool("update", false, "rewrite the parity golden files")

const parityDir = "../../../../testdata/parity"

// The inputs both front ends render the goldens from.
const (
	parityToken   = "dop_v1_parity"
	parityName    = "fra1-yvpn-1700000000"
	parityAuthKey = "tskey-auth-PARITY"
	parityVersion = "9.9.9"
)

func golden(t *testing.T, file, got string) {
	t.Helper()
	path := filepath.Join(parityDir, file)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file.\n--- got ---\n%s\n--- want ---\n%s", file, got, want)
	}
}

func jsonString(t *testing.T, v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func TestCatalogParity(t *testing.T) {
	golden(t, "addons.json", jsonString(t, Catalog))
}

func TestSecretsParity(t *testing.T) {
	s := Derive(parityToken, parityName)
	golden(t, "secrets.json", jsonString(t, s))

	if len(s.AgentToken) != 43 {
		t.Errorf("agent token is %d chars, want 43", len(s.AgentToken))
	}
	if !regexp.MustCompile(`^[a-z2-7]{5}(-[a-z2-7]{5}){3}$`).MatchString(s.AdminPassword) {
		t.Errorf("admin password %q", s.AdminPassword)
	}
	if !regexp.MustCompile(`^([bdfghjkmnprstvwz][aeiou]){2}(-([bdfghjkmnprstvwz][aeiou]){2}){2}-[1-9][0-9]$`).MatchString(s.GuestPassword) {
		t.Errorf("guest password %q", s.GuestPassword)
	}
	if other := Derive(parityToken, "fra1-yvpn-1700000001"); other.AgentToken == s.AgentToken || other.GuestPassword == s.GuestPassword {
		t.Error("every node must get its own secrets")
	}
	if other := Derive("dop_v1_other", parityName); other.AdminPassword == s.AdminPassword {
		t.Error("secrets must depend on the token")
	}
}

func TestCloudInitParity(t *testing.T) {
	s := Derive(parityToken, parityName)
	cases := map[string]Params{
		"cloudinit-exit.yaml":          {AuthKey: parityAuthKey, Exit: true},
		"cloudinit-jellyfin-exit.yaml": {AuthKey: parityAuthKey, Exit: true, Addon: "jellyfin", NodeConfig: NodeConfig("jellyfin", s), Version: parityVersion},
		"cloudinit-jellyfin.yaml":      {AuthKey: parityAuthKey, Exit: false, Addon: "jellyfin", NodeConfig: NodeConfig("jellyfin", s), Version: parityVersion},
	}
	for file, p := range cases {
		golden(t, file, CloudInit(p))
	}
}

// A plain exit node must come out exactly as it did before add-ons existed.
func TestPlainExitNodeUnchanged(t *testing.T) {
	got := CloudInit(Params{AuthKey: parityAuthKey, Exit: true})
	for _, want := range []string{
		"vendor_data:\n  enabled: false\n",
		"\nwrite_files:\n  - path: /etc/sysctl.d/99-tailscale.conf\n",
		"\nruncmd:\n  - sysctl --system\n",
		"  - tailscale up --authkey tskey-auth-PARITY --advertise-exit-node\n\nfinal_message: \"yVPN exit node ready.\"\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plain exit node lacks %q", want)
		}
	}
	for _, unwanted := range []string{"yvpn-node", "node.json", "{{"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("plain exit node has %q", unwanted)
		}
	}
}

func TestAddonNodeWithoutExit(t *testing.T) {
	s := Derive(parityToken, parityName)
	got := CloudInit(Params{AuthKey: parityAuthKey, Addon: "jellyfin", NodeConfig: NodeConfig("jellyfin", s), Version: parityVersion})
	if strings.Contains(got, "advertise-exit-node") || strings.Contains(got, "ip_forward") {
		t.Error("a node that is not an exit node must not forward or advertise routes")
	}
	if !strings.Contains(got, "releases/download/v9.9.9/yvpn-node-linux-amd64") {
		t.Error("the agent must come from the front end's own release")
	}
	if !strings.Contains(got, "    permissions: '0600'\n    content: |\n      {\"addon\":\"jellyfin\",\"token\":\""+s.AgentToken+"\"") {
		t.Errorf("node config not written as a private file:\n%s", got)
	}
}

func TestFromTags(t *testing.T) {
	if FromTags([]string{"yVPN", "yvpn-addon:jellyfin"}) != "jellyfin" || FromTags([]string{"yVPN"}) != "" {
		t.Fatal("FromTags")
	}
	if _, ok := Lookup("jellyfin"); !ok {
		t.Fatal("jellyfin missing from the catalog")
	}
	if ids := IDs(); len(ids) != 1 || ids[0] != "jellyfin" {
		t.Fatalf("IDs: %v", ids)
	}
}
