package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

/* ------------------------------- fakes ------------------------------- */

// fakeRunner answers for tailscale and docker. It keeps the funnel flag the
// way tailscaled would, and "converts" a video by writing the output file.
type fakeRunner struct {
	mu       sync.Mutex
	calls    []string
	funnel   bool
	funnelOK bool   // false: tailscale refuses funnel, like a tailnet without the attribute
	approval bool   // true: tailscale prints a link to allow funnel and waits, like a tailnet that hasn't yet
	probe    string // ffprobe's JSON
	workDir  string
	block    chan struct{} // when set, ffmpeg waits on it
}

func (f *fakeRunner) record(name string, args []string) string {
	c := name + " " + strings.Join(args, " ")
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	return c
}

func (f *fakeRunner) called(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	c := f.record(name, args)
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.HasPrefix(c, "tailscale status"):
		return `{"BackendState":"Running","Self":{"DNSName":"fra1-yvpn-1.tail1234.ts.net."}}`, nil
	case strings.HasPrefix(c, "tailscale funnel status"):
		return fmt.Sprintf(`{"AllowFunnel":{"fra1-yvpn-1.tail1234.ts.net:443":%v}}`, f.funnel), nil
	case strings.HasPrefix(c, "tailscale funnel --bg") && f.approval:
		f.mu.Unlock()
		<-ctx.Done()
		f.mu.Lock()
		out := "Funnel is not enabled on your tailnet.\nTo enable, visit:\n\n         https://login.tailscale.com/f/funnel?node=nABC123\n"
		return out, cmdError(name, args, ctx.Err(), []byte(out))
	case strings.HasPrefix(c, "tailscale funnel --bg"):
		if !f.funnelOK {
			return "", errors.New(`tailscale funnel: exit status 1: Funnel not available; "funnel" node attribute not set.`)
		}
		f.funnel = true
	case strings.HasPrefix(c, "tailscale serve --bg --yes --https=443"):
		f.funnel = false
	case strings.Contains(c, ffprobePath):
		return f.probe, nil
	}
	return "", nil
}

func (f *fakeRunner) Stream(ctx context.Context, onLine func(string), name string, args ...string) error {
	f.record(name, args)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	out := args[len(args)-1] // /work/<id>.mp4
	for _, l := range []string{"frame=1", "out_time_us=5000000", "progress=continue", "progress=end"} {
		onLine(l)
	}
	return os.WriteFile(filepath.Join(f.workDir, filepath.Base(out)), []byte("prepared"), 0o600)
}

// fakeJellyfin is just enough of Jellyfin's API for the agent's day-to-day
// calls: sign-in, users and their policies, and library refreshes.
type fakeJellyfin struct {
	mu       sync.Mutex
	policies map[string]map[string]any // user name -> policy
	refresh  int
}

func newFakeJellyfin() *fakeJellyfin {
	return &fakeJellyfin{policies: map[string]map[string]any{
		"admin": {"IsDisabled": false, "AuthenticationProviderId": "default"},
		"guest": {"IsDisabled": true, "AuthenticationProviderId": "default"},
	}}
}

func (j *fakeJellyfin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	j.mu.Lock()
	defer j.mu.Unlock()
	switch {
	case r.URL.Path == "/Users/AuthenticateByName":
		writeJSON(w, 200, map[string]string{"AccessToken": "tok"})
	case r.URL.Path == "/Users":
		writeJSON(w, 200, []map[string]string{{"Id": "admin", "Name": "admin"}, {"Id": "guest", "Name": "guest"}})
	case strings.HasSuffix(r.URL.Path, "/Policy"):
		id := strings.Split(r.URL.Path, "/")[2]
		var p map[string]any
		json.NewDecoder(r.Body).Decode(&p)
		j.policies[id] = p
		w.WriteHeader(204)
	case strings.HasPrefix(r.URL.Path, "/Users/"):
		id := strings.Split(r.URL.Path, "/")[2]
		writeJSON(w, 200, map[string]any{"Policy": j.policies[id]})
	case r.URL.Path == "/Library/Refresh":
		j.refresh++
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}

func (j *fakeJellyfin) guestDisabled() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.policies["guest"]["IsDisabled"] == true
}

const testToken = "0123456789abcdef0123456789abcdef0123456789a"

type harness struct {
	t   *testing.T
	a   *Agent
	run *fakeRunner
	jf  *fakeJellyfin
	srv *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	jf := newFakeJellyfin()
	jsrv := httptest.NewServer(jf)
	t.Cleanup(jsrv.Close)
	cfg := Config{Addon: "jellyfin", Token: testToken, AdminPassword: "a", GuestPassword: "g",
		DataDir: filepath.Join(dir, "data"), MediaDir: filepath.Join(dir, "media"), JellyfinURL: jsrv.URL}
	cfg.applyDefaults()
	for _, d := range []string{cfg.DataDir, filepath.Join(cfg.DataDir, "work"), cfg.MediaDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	run := &fakeRunner{funnelOK: true, workDir: filepath.Join(cfg.DataDir, "work"),
		probe: `{"streams":[{"index":0,"codec_type":"video","codec_name":"hevc","pix_fmt":"yuv420p10le","height":2160},
		                    {"index":1,"codec_type":"audio","codec_name":"opus"}],"format":{"duration":"10.0"}}`}
	a := newAgent(cfg, run, "test")
	a.logf = func(string, ...any) {}
	a.fqdn = "fra1-yvpn-1.tail1234.ts.net"
	go a.prepWorker()
	srv := httptest.NewServer(a.handler())
	t.Cleanup(srv.Close)
	return &harness{t: t, a: a, run: run, jf: jf, srv: srv}
}

// call makes an authenticated request and decodes the JSON answer into out.
func (h *harness) call(method, path string, body io.Reader, hdr map[string]string, out any) int {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func (h *harness) jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

// waitFor polls the agent's status until cond holds.
func (h *harness) waitFor(what string, cond func(statusResponse) bool) statusResponse {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := h.a.status()
		if cond(st) {
			return st
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s; status %+v", what, st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

/* -------------------------------- tests -------------------------------- */

func TestAuthAndCORS(t *testing.T) {
	h := newHarness(t)

	res, err := http.Get(h.srv.URL + "/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("no token: got %d, want 401", res.StatusCode)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("a 401 must still carry CORS headers, or the browser hides the reason")
	}

	req, _ := http.NewRequest("OPTIONS", h.srv.URL+"/v1/uploads/x", nil)
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 204 || res.Header.Get("Access-Control-Allow-Private-Network") != "true" {
		t.Fatalf("preflight: %d %v", res.StatusCode, res.Header)
	}
	if !strings.Contains(res.Header.Get("Access-Control-Allow-Headers"), "Upload-Offset") ||
		!strings.Contains(res.Header.Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Fatal("preflight must allow the upload's header and method")
	}

	res, _ = http.Get(h.srv.URL + "/healthz")
	if res.StatusCode != 200 {
		t.Fatal("healthz should not need the token")
	}

	var st statusResponse
	if code := h.call("GET", "/v1/status", nil, nil, &st); code != 200 || st.Addon != "jellyfin" {
		t.Fatalf("status: %d %+v", code, st)
	}
	if st.URL != "https://fra1-yvpn-1.tail1234.ts.net/" {
		t.Fatalf("status url: %q", st.URL)
	}
}

func TestUploadResumeAndPrepare(t *testing.T) {
	h := newHarness(t)
	data := bytes.Repeat([]byte("0123456789"), 1000) // 10 kB

	var up struct {
		ID     string
		Offset int64
		Name   string
	}
	if code := h.call("POST", "/v1/uploads", h.jsonBody(map[string]any{"name": `../../etc/Holiday "2024".mkv`, "size": len(data)}), nil, &up); code != 200 {
		t.Fatalf("start upload: %d", code)
	}
	if up.Offset != 0 || up.Name != `Holiday _2024_.mkv` {
		t.Fatalf("start upload: %+v", up)
	}

	patch := func(offset int, chunk []byte) (int, int64) {
		var r struct{ Offset int64 }
		code := h.call("PATCH", "/v1/uploads/"+up.ID, bytes.NewReader(chunk),
			map[string]string{"Upload-Offset": fmt.Sprint(offset), "Content-Type": "application/octet-stream"}, &r)
		return code, r.Offset
	}

	if code, off := patch(0, data[:4000]); code != 200 || off != 4000 {
		t.Fatalf("first chunk: %d %d", code, off)
	}
	// A chunk sent again after a lost response is refused with where the
	// upload really stands.
	if code, off := patch(0, data[:4000]); code != 409 || off != 4000 {
		t.Fatalf("stale chunk: %d %d", code, off)
	}

	// Starting the same file again (a reopened tab) finds the same upload.
	var again struct {
		ID     string
		Offset int64
	}
	h.call("POST", "/v1/uploads", h.jsonBody(map[string]any{"name": `../../etc/Holiday "2024".mkv`, "size": len(data)}), nil, &again)
	if again.ID != up.ID || again.Offset != 4000 {
		t.Fatalf("resume: got %+v, want id %s at 4000", again, up.ID)
	}

	// More than was announced is cut back to the announced size.
	big := append(append([]byte{}, data[4000:]...), []byte("extra")...)
	if code, off := patch(4000, big); code != 400 || off != int64(len(data)) {
		t.Fatalf("overlong chunk: %d %d", code, off)
	}

	st := h.waitFor("the video to be prepared", func(s statusResponse) bool {
		return len(s.Media) == 1 && s.Media[0].State == stateReady
	})
	it := st.Media[0]
	if it.File != "Holiday _2024_.mp4" || it.Progress != 1 {
		t.Fatalf("ready item: %+v", it)
	}
	if b, err := os.ReadFile(filepath.Join(h.a.cfg.MediaDir, it.File)); err != nil || string(b) != "prepared" {
		t.Fatalf("library file: %q %v", b, err)
	}
	if _, err := os.Stat(h.a.srcPath(it.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the original should be gone once the prepared copy is in the library")
	}
	if !h.run.called("docker run --rm --name yvpn-prep-" + it.ID) {
		t.Fatalf("ffmpeg should run in a named throwaway container; calls: %v", h.run.calls)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.jf.mu.Lock()
		n := h.jf.refresh
		h.jf.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the library was never refreshed")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A second video with the same name gets a numbered file.
	var up2 struct{ ID string }
	h.call("POST", "/v1/uploads", h.jsonBody(map[string]any{"name": "Holiday _2024_.webm", "size": 3}), nil, &up2)
	h.call("PATCH", "/v1/uploads/"+up2.ID, strings.NewReader("abc"), map[string]string{"Upload-Offset": "0"}, nil)
	st = h.waitFor("the second video", func(s statusResponse) bool {
		return len(s.Media) == 2 && s.Media[1].State == stateReady
	})
	if st.Media[1].File != "Holiday _2024_ (2).mp4" {
		t.Fatalf("second file: %q", st.Media[1].File)
	}
}

func TestUploadRefusedWithoutSpace(t *testing.T) {
	h := newHarness(t)
	var r struct{ Message string }
	code := h.call("POST", "/v1/uploads", h.jsonBody(map[string]any{"name": "huge.mkv", "size": int64(1) << 50}), nil, &r)
	if code != http.StatusInsufficientStorage || !strings.Contains(r.Message, "not enough disk space") {
		t.Fatalf("got %d %q", code, r.Message)
	}
}

func TestFetchLink(t *testing.T) {
	h := newHarness(t)
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>"))
		default:
			w.Header().Set("Content-Disposition", `attachment; filename="Family Film.mkv"`)
			w.Write(bytes.Repeat([]byte("v"), 5000))
		}
	}))
	defer files.Close()

	var it Item
	if code := h.call("POST", "/v1/fetch", h.jsonBody(map[string]string{"url": files.URL + "/dl/abc"}), nil, &it); code != 200 {
		t.Fatalf("fetch: %d", code)
	}
	st := h.waitFor("the link's video", func(s statusResponse) bool {
		return len(s.Media) == 1 && s.Media[0].State == stateReady
	})
	if st.Media[0].Name != "Family Film.mkv" || st.Media[0].File != "Family Film.mp4" || st.Media[0].Size != 5000 {
		t.Fatalf("fetched item: %+v", st.Media[0])
	}

	h.call("POST", "/v1/fetch", h.jsonBody(map[string]string{"url": files.URL + "/page"}), nil, &it)
	st = h.waitFor("the web page to be refused", func(s statusResponse) bool {
		return len(s.Media) == 2 && s.Media[1].State == stateFailed
	})
	if !strings.Contains(st.Media[1].Error, "web page") {
		t.Fatalf("html link error: %q", st.Media[1].Error)
	}

	var e struct{ Message string }
	if code := h.call("POST", "/v1/fetch", h.jsonBody(map[string]string{"url": "file:///etc/passwd"}), nil, &e); code != 400 {
		t.Fatalf("non-http link: %d", code)
	}
}

func TestRemoveStopsPreparation(t *testing.T) {
	h := newHarness(t)
	h.run.block = make(chan struct{})
	var up struct{ ID string }
	h.call("POST", "/v1/uploads", h.jsonBody(map[string]any{"name": "a.mkv", "size": 3}), nil, &up)
	h.call("PATCH", "/v1/uploads/"+up.ID, strings.NewReader("abc"), map[string]string{"Upload-Offset": "0"}, nil)
	h.waitFor("preparation to start", func(s statusResponse) bool {
		return len(s.Media) == 1 && s.Media[0].State == statePreparing
	})
	if code := h.call("DELETE", "/v1/media/"+up.ID, nil, nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if !h.run.called("docker rm -f yvpn-prep-" + up.ID) {
		t.Fatal("removing a video mid-conversion must stop its container")
	}
	if st := h.a.status(); len(st.Media) != 0 {
		t.Fatalf("still listed: %+v", st.Media)
	}
	if code := h.call("DELETE", "/v1/media/"+up.ID, nil, nil, nil); code != 404 {
		t.Fatalf("second delete: %d", code)
	}
}

func TestShare(t *testing.T) {
	h := newHarness(t)
	h.a.phase = "ready"

	var st statusResponse
	if code := h.call("POST", "/v1/share", h.jsonBody(map[string]bool{"enabled": true}), nil, &st); code != 200 {
		t.Fatalf("share on: %d", code)
	}
	if !st.Share.Enabled || h.jf.guestDisabled() {
		t.Fatalf("sharing on should open the funnel and enable the guest: %+v", st.Share)
	}
	h.jf.mu.Lock()
	provider := h.jf.policies["guest"]["AuthenticationProviderId"]
	h.jf.mu.Unlock()
	if provider != "default" {
		t.Fatal("the guest's policy must be written back whole, or Jellyfin locks the guest out")
	}
	if !h.run.called("tailscale funnel --bg --yes --https=443 http://127.0.0.1:8096") &&
		!h.run.called("tailscale funnel --bg --yes --https=443 "+h.a.jellyfinTarget()) {
		t.Fatalf("funnel call: %v", h.run.calls)
	}

	if code := h.call("POST", "/v1/share", h.jsonBody(map[string]bool{"enabled": false}), nil, &st); code != 200 {
		t.Fatalf("share off: %d", code)
	}
	if st.Share.Enabled || !h.jf.guestDisabled() {
		t.Fatalf("sharing off should close the funnel and disable the guest: %+v", st.Share)
	}
	for _, c := range h.run.calls {
		if strings.Contains(c, "--https="+controlPort) && strings.Contains(c, "funnel") {
			t.Fatal("the control port must never be funnelled")
		}
	}
}

func TestShareRefusedByTailnet(t *testing.T) {
	h := newHarness(t)
	h.a.phase = "ready"
	h.run.funnelOK = false
	var r struct {
		Message string
		Status  statusResponse
	}
	if code := h.call("POST", "/v1/share", h.jsonBody(map[string]bool{"enabled": true}), nil, &r); code != http.StatusBadGateway {
		t.Fatalf("got %d", code)
	}
	if !strings.Contains(r.Message, `"funnel" node attribute`) || r.Status.Share.Enabled || r.Status.Share.Error == "" {
		t.Fatalf("refusal should explain the fix: %+v", r)
	}
	if !h.jf.guestDisabled() {
		t.Fatal("a failed share must leave the guest locked out")
	}
}

// A tailnet that hasn't allowed Funnel makes tailscale print a link and wait.
// The wait must end, and the link must reach the dashboard.
func TestShareWaitingForApproval(t *testing.T) {
	defer func(d time.Duration) { tsTimeout = d }(tsTimeout)
	tsTimeout = 200 * time.Millisecond
	h := newHarness(t)
	h.a.phase = "ready"
	h.run.approval = true
	var r struct {
		Message string
		Status  statusResponse
	}
	start := time.Now()
	if code := h.call("POST", "/v1/share", h.jsonBody(map[string]bool{"enabled": true}), nil, &r); code != http.StatusBadGateway {
		t.Fatalf("got %d", code)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("the wait for approval was not cut short")
	}
	if !strings.Contains(r.Message, "https://login.tailscale.com/f/funnel?node=nABC123 as a tailnet admin") ||
		!strings.Contains(r.Status.Share.Error, "https://login.tailscale.com/f/funnel?node=nABC123") {
		t.Fatalf("the approval link should reach the dashboard: %+v", r)
	}
	if r.Status.Share.Enabled || !h.jf.guestDisabled() {
		t.Fatal("an unapproved share must stay off, with the guest locked out")
	}
}

func TestShareWaitsForSetup(t *testing.T) {
	h := newHarness(t)
	if code := h.call("POST", "/v1/share", h.jsonBody(map[string]bool{"enabled": true}), nil, nil); code != http.StatusBadGateway {
		t.Fatalf("got %d", code)
	}
	if h.run.called("tailscale funnel --bg") {
		t.Fatal("nothing should be published before Jellyfin is set up")
	}
}

func TestLoadAfterRestart(t *testing.T) {
	h := newHarness(t)
	a := h.a
	os.WriteFile(a.srcPath("up1"), []byte("12345"), 0o600)
	os.WriteFile(a.srcPath("q1"), []byte("abc"), 0o600)
	a.items = []*Item{
		{ID: "up1", Name: "a.mkv", Source: "upload", State: stateUploading, Size: 10},
		{ID: "q1", Name: "b.mkv", Source: "upload", State: statePreparing, Size: 3},
		{ID: "dl1", Name: "c.mkv", Source: "link", State: stateDownloading},
		{ID: "r1", Name: "d.mkv", File: "d.mp4", Source: "upload", State: stateReady},
	}
	a.mu.Lock()
	a.saveLocked()
	a.mu.Unlock()

	b := newAgent(a.cfg, h.run, "test")
	b.logf = func(string, ...any) {}
	b.load()
	got := map[string]string{}
	for _, it := range b.items {
		got[it.ID] = it.State
	}
	want := map[string]string{"up1": stateUploading, "q1": stateQueued, "dl1": stateFailed, "r1": stateReady}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after restart: %v, want %v", got, want)
	}
	if b.items[0].Received != 5 {
		t.Fatalf("an upload resumes from what reached the disk: %d", b.items[0].Received)
	}
	if len(b.queue) != 1 {
		t.Fatal("a fully arrived video is prepared again")
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"Holiday 2024.mkv":       "Holiday 2024.mkv",
		"../../etc/passwd":       "passwd",
		`C:\Users\me\a.mp4`:      "a.mp4",
		"..":                     "video",
		"":                       "video",
		".hidden.mkv":            "hidden.mkv",
		"a\x00b<c>d|e?.mkv":      "a_b_c_d_e_.mkv",
		"  spaced  .mp4  ":       "spaced  .mp4",
		strings.Repeat("é", 200): strings.Repeat("é", 120),
	} {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDirectLink(t *testing.T) {
	u, err := directLink("https://www.dropbox.com/scl/fi/abc/film.mkv?rlkey=x&dl=0")
	if err != nil || u.Query().Get("dl") != "1" || u.Query().Get("rlkey") != "x" {
		t.Fatalf("dropbox: %v %v", u, err)
	}
	if _, err := directLink("ftp://example.com/a"); err == nil {
		t.Fatal("ftp should be refused")
	}
}

func TestPlanPrep(t *testing.T) {
	probe := func(s string) probeResult {
		p, err := parseProbe(s)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	has := func(args []string, seq ...string) bool {
		return strings.Contains(" "+strings.Join(args, " ")+" ", " "+strings.Join(seq, " ")+" ")
	}

	// Already right: copied, not re-encoded.
	p, err := planPrep(probe(`{"streams":[{"index":0,"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p","height":1080},
		{"index":1,"codec_type":"audio","codec_name":"aac"}]}`), "/work/a.src", "/work/a.mp4")
	if err != nil || !p.CopyVideo || !p.CopyAudio || !has(p.Args, "-c:v", "copy") || !has(p.Args, "-c:a", "copy") {
		t.Fatalf("h264+aac: %+v %v", p, err)
	}
	if !has(p.Args, "-movflags", "+faststart") || p.Args[len(p.Args)-1] != "/work/a.mp4" {
		t.Fatalf("output: %v", p.Args)
	}

	// 4K HEVC, Opus, cover art first: re-encoded, scaled to 1080p, art skipped.
	p, err = planPrep(probe(`{"streams":[
		{"index":0,"codec_type":"video","codec_name":"mjpeg","disposition":{"attached_pic":1}},
		{"index":1,"codec_type":"video","codec_name":"hevc","pix_fmt":"yuv420p10le","height":2160},
		{"index":2,"codec_type":"audio","codec_name":"opus"}]}`), "in", "out")
	if err != nil || p.CopyVideo || p.CopyAudio || !p.Downscaled {
		t.Fatalf("hevc: %+v %v", p, err)
	}
	for _, seq := range [][]string{{"-map", "0:1"}, {"-map", "0:2"}, {"-c:v", "libx264"}, {"-vf", "scale=-2:1080"},
		{"-pix_fmt", "yuv420p"}, {"-c:a", "aac"}} {
		if !has(p.Args, seq...) {
			t.Errorf("hevc plan lacks %v: %v", seq, p.Args)
		}
	}

	// 10-bit H.264 won't play in most browsers: re-encoded.
	p, _ = planPrep(probe(`{"streams":[{"index":0,"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p10le","height":720}]}`), "in", "out")
	if p.CopyVideo || has(p.Args, "-c:a") {
		t.Fatalf("10-bit h264 without audio: %v", p.Args)
	}

	if _, err := planPrep(probe(`{"streams":[{"index":0,"codec_type":"audio","codec_name":"mp3"}]}`), "in", "out"); err == nil {
		t.Fatal("audio alone should be refused")
	}
}

func TestProgressFrom(t *testing.T) {
	for _, c := range []struct {
		line string
		dur  float64
		want float64
		ok   bool
	}{
		{"out_time_us=5000000", 10, 0.5, true},
		{"out_time_ms=2500000", 10, 0.25, true},
		{"out_time_us=10000000", 10, 0.99, true},
		{"progress=end", 10, 1, true},
		{"progress=continue", 10, 0, false},
		{"out_time_us=N/A", 10, 0, false},
		{"out_time_us=5000000", 0, 0, false},
		{"frame=12", 10, 0, false},
	} {
		got, ok := progressFrom(c.line, c.dur)
		if ok != c.ok || got != c.want {
			t.Errorf("progressFrom(%q, %v) = %v, %v; want %v, %v", c.line, c.dur, got, ok, c.want, c.ok)
		}
	}
}

func TestConfigValidation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "node.json")
	os.WriteFile(p, []byte(`{"addon":"jellyfin","token":"short","adminPassword":"a","guestPassword":"b"}`), 0o600)
	if _, err := loadConfig(p); err == nil {
		t.Fatal("a short token should be refused")
	}
	os.WriteFile(p, []byte(`{"addon":"jellyfin","token":"`+testToken+`","adminUser":"admin","adminPassword":"a","guestUser":"guest","guestPassword":"b"}`), 0o600)
	c, err := loadConfig(p)
	if err != nil || c.Image != defaultImage || c.Listen != "127.0.0.1:8090" {
		t.Fatalf("defaults: %+v %v", c, err)
	}
}
