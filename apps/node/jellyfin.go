package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Jellyfin drives the server's own HTTP API: the first-run wizard, the library
// and the two accounts. Every call here was checked against Jellyfin 12.2.
type Jellyfin struct {
	base    string
	http    *http.Client
	user    string
	pass    string
	version string
}

// libraryName is what the dashboard and the guide call the one library.
const libraryName = "Watch party"

// libraryPath is MediaDir as the Jellyfin container sees it.
const libraryPath = "/media"

func (j *Jellyfin) authHeader(token string) string {
	h := fmt.Sprintf(`MediaBrowser Client="yvpn-node", Device="yvpn-node", DeviceId="yvpn-node", Version="%s"`, j.version)
	if token != "" {
		h += fmt.Sprintf(`, Token="%s"`, token)
	}
	return h
}

// do sends one request. in is JSON-encoded when non-nil; out, when non-nil,
// receives the decoded response.
func (j *Jellyfin) do(ctx context.Context, method, path, token string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, j.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", j.authHeader(token))
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := j.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("jellyfin %s %s: %s: %s", method, path, res.Status, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		// Jellyfin prefixes some responses with a UTF-8 BOM.
		return json.Unmarshal(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")), out)
	}
	return nil
}

// WaitHealthy waits for /health to say "Healthy". It answers "Degraded" with a
// 200 while the server is still starting, so the status code is not enough.
func (j *Jellyfin) WaitHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		req, _ := http.NewRequestWithContext(ctx, "GET", j.base+"/health", nil)
		if res, err := j.http.Do(req); err == nil {
			b, _ := io.ReadAll(io.LimitReader(res.Body, 64))
			res.Body.Close()
			if strings.TrimSpace(string(b)) == "Healthy" {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("Jellyfin did not become healthy within " + timeout.String())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// Setup takes a fresh server through its first-run wizard and creates the
// library and the guest account. Every step checks before it acts, so running
// it again after an interruption finishes the job instead of failing.
func (j *Jellyfin) Setup(ctx context.Context, serverName, guestUser, guestPass string) error {
	var info struct{ StartupWizardCompleted bool }
	if err := j.do(ctx, "GET", "/System/Info/Public", "", nil, &info); err != nil {
		return err
	}
	if !info.StartupWizardCompleted {
		steps := []struct {
			method, path string
			body         any
		}{
			{"POST", "/Startup/Configuration", map[string]string{
				"ServerName": serverName, "UICulture": "en-US",
				"MetadataCountryCode": "US", "PreferredMetadataLanguage": "en",
			}},
			// The GET is part of the protocol: it creates the first user, which
			// the POST then names.
			{"GET", "/Startup/User", nil},
			{"POST", "/Startup/User", map[string]string{"Name": j.user, "Password": j.pass}},
			// Jellyfin only ever sees connections from 127.0.0.1 (Tailscale
			// proxies to it), but guests arriving through Funnel are still remote
			// in spirit; leave remote access on so nothing second-guesses them.
			{"POST", "/Startup/RemoteAccess", map[string]bool{"EnableRemoteAccess": true}},
			{"POST", "/Startup/Complete", nil},
		}
		for _, s := range steps {
			if err := j.do(ctx, s.method, s.path, "", s.body, nil); err != nil {
				return err
			}
		}
	}

	token, err := j.Login(ctx)
	if err != nil {
		return err
	}

	var folders []struct{ Locations []string }
	if err := j.do(ctx, "GET", "/Library/VirtualFolders", token, nil, &folders); err != nil {
		return err
	}
	have := false
	for _, f := range folders {
		for _, l := range f.Locations {
			have = have || l == libraryPath
		}
	}
	if !have {
		// Home videos rather than movies: files are shown under their own
		// names, with no metadata lookup to mislabel a family video as a film.
		q := url.Values{"name": {libraryName}, "collectionType": {"homevideos"}, "refreshLibrary": {"false"}}
		body := map[string]any{"LibraryOptions": map[string]any{
			"PathInfos": []map[string]string{{"Path": libraryPath}},
		}}
		if err := j.do(ctx, "POST", "/Library/VirtualFolders?"+q.Encode(), token, body, nil); err != nil {
			return err
		}
	}

	if _, err := j.userID(ctx, token, guestUser); err != nil {
		body := map[string]string{"Name": guestUser, "Password": guestPass}
		if err := j.do(ctx, "POST", "/Users/New", token, body, nil); err != nil {
			return err
		}
	}
	// Neither account is listed on the sign-in page, which Funnel makes public;
	// the guest stays disabled until sharing is turned on.
	if err := j.setPolicy(ctx, token, j.user, map[string]any{"IsHidden": true}); err != nil {
		return err
	}
	return j.setPolicy(ctx, token, guestUser, map[string]any{"IsHidden": true, "IsDisabled": true})
}

func (j *Jellyfin) Login(ctx context.Context) (string, error) {
	var res struct{ AccessToken string }
	body := map[string]string{"Username": j.user, "Pw": j.pass}
	if err := j.do(ctx, "POST", "/Users/AuthenticateByName", "", body, &res); err != nil {
		return "", err
	}
	if res.AccessToken == "" {
		return "", errors.New("jellyfin: sign-in returned no token")
	}
	return res.AccessToken, nil
}

func (j *Jellyfin) userID(ctx context.Context, token, name string) (string, error) {
	var users []struct{ Id, Name string }
	if err := j.do(ctx, "GET", "/Users", token, nil, &users); err != nil {
		return "", err
	}
	for _, u := range users {
		if strings.EqualFold(u.Name, name) {
			return u.Id, nil
		}
	}
	return "", fmt.Errorf("jellyfin: no user %q", name)
}

// setPolicy changes some fields of a user's policy. Jellyfin replaces the whole
// policy on every write and fills anything missing with defaults that lock the
// user out (no authentication provider), so it is read, edited and written back.
func (j *Jellyfin) setPolicy(ctx context.Context, token, user string, change map[string]any) error {
	id, err := j.userID(ctx, token, user)
	if err != nil {
		return err
	}
	var u struct{ Policy map[string]any }
	if err := j.do(ctx, "GET", "/Users/"+id, token, nil, &u); err != nil {
		return err
	}
	if u.Policy == nil {
		return fmt.Errorf("jellyfin: user %q has no policy", user)
	}
	for k, v := range change {
		u.Policy[k] = v
	}
	return j.do(ctx, "POST", "/Users/"+id+"/Policy", token, u.Policy, nil)
}

// SetGuestEnabled lets the guest account sign in, or stops it.
func (j *Jellyfin) SetGuestEnabled(ctx context.Context, guestUser string, enabled bool) error {
	token, err := j.Login(ctx)
	if err != nil {
		return err
	}
	return j.setPolicy(ctx, token, guestUser, map[string]any{"IsDisabled": !enabled})
}

// Refresh rescans the library so a newly prepared video shows up.
func (j *Jellyfin) Refresh(ctx context.Context) error {
	token, err := j.Login(ctx)
	if err != nil {
		return err
	}
	return j.do(ctx, "POST", "/Library/Refresh", token, nil, nil)
}
