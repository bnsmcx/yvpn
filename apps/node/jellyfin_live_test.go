package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestJellyfinLive runs the setup against a real, fresh Jellyfin. It is skipped
// unless YVPN_JELLYFIN_URL points at one that has never been set up:
//
//	docker run -d --name jf-test -p 127.0.0.1:8097:8096 jellyfin/jellyfin:12.2
//	YVPN_JELLYFIN_URL=http://127.0.0.1:8097 go test -run Live -v
func TestJellyfinLive(t *testing.T) {
	base := os.Getenv("YVPN_JELLYFIN_URL")
	if base == "" {
		t.Skip("YVPN_JELLYFIN_URL not set")
	}
	ctx := context.Background()
	j := &Jellyfin{base: base, http: &http.Client{Timeout: time.Minute}, user: "admin", pass: "admin-pass-1234", version: "test"}
	if err := j.WaitHealthy(ctx, 3*time.Minute); err != nil {
		t.Fatal(err)
	}
	// Twice: the second run must find everything in place and change nothing.
	for i := 0; i < 2; i++ {
		if err := j.Setup(ctx, "yvpn-live", "guest", "guest-pass-42"); err != nil {
			t.Fatalf("setup run %d: %v", i+1, err)
		}
	}

	guestLogin := func() int {
		body := bytes.NewBufferString(`{"Username":"guest","Pw":"guest-pass-42"}`)
		req, _ := http.NewRequest("POST", base+"/Users/AuthenticateByName", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", `MediaBrowser Client="t", Device="t", DeviceId="live-test", Version="1"`)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := guestLogin(); code == 200 {
		t.Fatal("the guest must not sign in before sharing is on")
	}
	if err := j.SetGuestEnabled(ctx, "guest", true); err != nil {
		t.Fatal(err)
	}
	if code := guestLogin(); code != 200 {
		t.Fatalf("enabled guest sign-in: %d", code)
	}
	if err := j.SetGuestEnabled(ctx, "guest", false); err != nil {
		t.Fatal(err)
	}
	if code := guestLogin(); code == 200 {
		t.Fatal("the guest must be locked out again")
	}

	token, err := j.Login(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var folders []struct {
		Name           string
		CollectionType string
		Locations      []string
	}
	if err := j.do(ctx, "GET", "/Library/VirtualFolders", token, nil, &folders); err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].Name != libraryName || folders[0].CollectionType != "homevideos" {
		t.Fatalf("library: %+v", folders)
	}
	// The sign-in page Funnel makes public must not list who can sign in.
	var public []any
	if err := j.do(ctx, "GET", "/Users/Public", "", nil, &public); err != nil {
		t.Fatal(err)
	}
	if len(public) != 0 {
		t.Fatalf("users listed on the public sign-in page: %v", public)
	}
	if err := j.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
}
