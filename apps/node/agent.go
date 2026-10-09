package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Item is one video on its way to the library, or in it.
type Item struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`           // what it was called when it arrived
	File     string    `json:"file,omitempty"` // its name in the library, once ready
	Source   string    `json:"source"`         // "upload" or "link"
	State    string    `json:"state"`          // see the constants below
	Progress float64   `json:"progress"`       // 0..1 within the current state
	Size     int64     `json:"size"`           // bytes expected, 0 if a link didn't say
	Received int64     `json:"received"`       // bytes on disk so far
	Error    string    `json:"error,omitempty"`
	Added    time.Time `json:"added"`

	writing bool // an upload chunk is being written right now
}

const (
	stateUploading   = "uploading"
	stateDownloading = "downloading"
	stateQueued      = "queued"
	statePreparing   = "preparing"
	stateReady       = "ready"
	stateFailed      = "failed"
)

type Agent struct {
	cfg     Config
	run     Runner
	jf      *Jellyfin
	client  *http.Client // for fetching links
	version string
	logf    func(format string, args ...any)
	// lookPath finds a program on this machine; tests pretend.
	lookPath func(string) (string, error)

	mu      sync.Mutex
	phase   string // installing, ready, failed
	step    string
	instErr string
	fqdn    string
	logTail []string
	items   []*Item
	cancels map[string]context.CancelFunc
	share   shareState
	queue   chan string

	installRunning bool
}

type shareState struct {
	Enabled bool   `json:"enabled"`
	Busy    bool   `json:"busy"`
	Error   string `json:"error,omitempty"`
}

func newAgent(cfg Config, run Runner, version string) *Agent {
	a := &Agent{
		cfg:      cfg,
		run:      run,
		version:  version,
		client:   &http.Client{},
		phase:    "installing",
		step:     "Starting",
		cancels:  map[string]context.CancelFunc{},
		queue:    make(chan string, 256),
		lookPath: exec.LookPath,
		jf: &Jellyfin{
			base: strings.TrimRight(cfg.JellyfinURL, "/"), http: &http.Client{Timeout: 60 * time.Second},
			user: cfg.AdminUser, pass: cfg.AdminPassword, version: version,
		},
	}
	a.logf = func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		log.Print(msg)
		a.mu.Lock()
		a.logTail = append(a.logTail, time.Now().UTC().Format("15:04:05")+" "+msg)
		if len(a.logTail) > 40 {
			a.logTail = a.logTail[len(a.logTail)-40:]
		}
		a.mu.Unlock()
	}
	return a
}

func (a *Agent) workDir() string { return filepath.Join(a.cfg.DataDir, "work") }
func (a *Agent) srcPath(id string) string {
	return filepath.Join(a.workDir(), id+".src")
}
func (a *Agent) outPath(id string) string {
	return filepath.Join(a.workDir(), id+".mp4")
}
func (a *Agent) statePath() string { return filepath.Join(a.cfg.DataDir, "media.json") }

/* ------------------------------ persistence ------------------------------ */

// The item list is saved on every change of state (not of progress), so a
// restart keeps the library's history and any upload that was half done.
func (a *Agent) saveLocked() {
	raw, err := json.MarshalIndent(a.items, "", "  ")
	if err != nil {
		return
	}
	tmp := a.statePath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err == nil {
		os.Rename(tmp, a.statePath())
	}
}

// load restores the item list after a restart. Uploads resume where they
// stopped, from whatever reached the disk; a file that finished arriving is
// prepared again; a link that was mid-download has to be asked for again.
func (a *Agent) load() {
	raw, err := os.ReadFile(a.statePath())
	if err != nil {
		return
	}
	var items []*Item
	if json.Unmarshal(raw, &items) != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, it := range items {
		switch it.State {
		case stateUploading:
			if fi, err := os.Stat(a.srcPath(it.ID)); err == nil {
				it.Received = fi.Size()
			} else {
				it.Received = 0
			}
		case stateQueued, statePreparing:
			if _, err := os.Stat(a.srcPath(it.ID)); err == nil {
				it.State, it.Progress = stateQueued, 0
				a.queue <- it.ID
			} else {
				it.State, it.Error = stateFailed, "lost when the node restarted"
			}
		case stateDownloading:
			os.Remove(a.srcPath(it.ID))
			it.State, it.Error = stateFailed, "interrupted when the node restarted; fetch the link again"
		}
	}
	a.items = items
	a.saveLocked()
}

func (a *Agent) findLocked(id string) *Item {
	for _, it := range a.items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

func (a *Agent) update(id string, fn func(it *Item), persist bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if it := a.findLocked(id); it != nil {
		fn(it)
		if persist {
			a.saveLocked()
		}
	}
}

func (a *Agent) fail(id string, err error) {
	a.logf("item %s failed: %v", id, err)
	a.update(id, func(it *Item) { it.State, it.Error, it.Progress = stateFailed, err.Error(), 0 }, true)
	os.Remove(a.srcPath(id))
	os.Remove(a.outPath(id))
}

func newID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

/* ------------------------------- file names ------------------------------ */

// cleanName makes a file name safe to put on disk and show in Jellyfin, from
// whatever a browser or a URL supplied: no directories, no control or reserved
// characters, nothing hidden, and not absurdly long.
func cleanName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsControl(r), strings.ContainsRune(`/:*?"<>|`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	name = strings.Trim(strings.TrimSpace(b.String()), ".")
	if r := []rune(name); len(r) > 120 {
		name = string(r[:120])
	}
	if name == "" || name == "/" {
		name = "video"
	}
	return name
}

// libraryFile picks the name a prepared video takes in the library: the
// original's, as an .mp4, numbered if that name is taken.
func (a *Agent) libraryFile(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if base == "" {
		base = "video"
	}
	for n := 1; ; n++ {
		f := base + ".mp4"
		if n > 1 {
			f = fmt.Sprintf("%s (%d).mp4", base, n)
		}
		if _, err := os.Stat(filepath.Join(a.cfg.MediaDir, f)); errors.Is(err, os.ErrNotExist) {
			return f
		}
	}
}

/* ------------------------------- disk space ------------------------------ */

// Space for the original and its prepared copy, side by side, with a margin
// for Jellyfin's own images and metadata.
func needFor(size int64) int64 { return 2*size + 1<<30 }

func (a *Agent) checkSpace(size int64) error {
	free, _ := diskSpace(a.cfg.DataDir)
	if free > 0 && free < needFor(size) {
		return fmt.Errorf("not enough disk space on the node: this needs about %s free and there is %s",
			humanBytes(needFor(size)), humanBytes(free))
	}
	return nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

/* -------------------------------- uploads -------------------------------- */

var errBusy = errors.New("another chunk of this upload is still being written")

// startUpload registers an upload, or finds the one this file already started:
// dropping the same file again after a lost connection, or a closed tab,
// carries on from the last byte that arrived.
func (a *Agent) startUpload(name string, size int64) (*Item, error) {
	name = cleanName(name)
	a.mu.Lock()
	for _, it := range a.items {
		if it.Source == "upload" && it.State == stateUploading && it.Name == name && it.Size == size {
			c := *it
			a.mu.Unlock()
			return &c, nil
		}
	}
	a.mu.Unlock()

	if err := a.checkSpace(size); err != nil {
		return nil, err
	}
	it := &Item{ID: newID(), Name: name, Source: "upload", State: stateUploading, Size: size, Added: time.Now().UTC()}
	f, err := os.OpenFile(a.srcPath(it.ID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	a.mu.Lock()
	a.items = append(a.items, it)
	a.saveLocked()
	c := *it
	a.mu.Unlock()
	a.logf("upload %s started: %q, %s", it.ID, name, humanBytes(size))
	return &c, nil
}

// offsetError says where an upload really stands when a chunk claims otherwise.
type offsetError struct{ offset int64 }

func (e offsetError) Error() string { return fmt.Sprintf("upload is at byte %d", e.offset) }

// writeChunk appends one chunk at offset. The file on disk is the record of how
// much has arrived: a chunk cut off halfway still counts for what it wrote.
func (a *Agent) writeChunk(id string, offset int64, body io.Reader) (int64, error) {
	a.mu.Lock()
	it := a.findLocked(id)
	switch {
	case it == nil:
		a.mu.Unlock()
		return 0, os.ErrNotExist
	case it.State != stateUploading:
		a.mu.Unlock()
		return 0, fmt.Errorf("this upload is already %s", it.State)
	case it.writing:
		a.mu.Unlock()
		return 0, errBusy
	}
	it.writing = true
	size := it.Size
	a.mu.Unlock()
	defer a.update(id, func(it *Item) { it.writing = false }, false)

	f, err := os.OpenFile(a.srcPath(id), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if fi.Size() != offset {
		return fi.Size(), offsetError{fi.Size()}
	}
	remaining := size - offset
	n, copyErr := io.Copy(f, io.LimitReader(body, remaining+1))
	if n > remaining {
		// More than the file was announced as: keep the announced part only.
		f.Truncate(size)
		n = remaining
		copyErr = fmt.Errorf("chunk runs past the %d bytes this upload was started with", size)
	}
	got := offset + n
	done := got == size
	a.update(id, func(it *Item) {
		it.Received = got
		it.Progress = float64(got) / float64(size)
		if done {
			it.State, it.Progress = stateQueued, 0
		}
	}, done)
	if done {
		a.logf("upload %s complete", id)
		a.queue <- id
	}
	return got, copyErr
}

/* --------------------------------- links --------------------------------- */

// directLink turns a share page into the file behind it, for the hosts where
// that is a matter of one query parameter.
func directLink(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("that is not an http(s) link")
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host == "dropbox.com" || strings.HasSuffix(host, ".dropbox.com") {
		q := u.Query()
		q.Del("dl")
		q.Set("dl", "1")
		u.RawQuery = q.Encode()
	}
	return u, nil
}

// fetchLink downloads a video from a link into the work directory, from the
// node's own (datacenter) connection rather than the uploader's.
func (a *Agent) fetchLink(raw string) (*Item, error) {
	u, err := directLink(raw)
	if err != nil {
		return nil, err
	}
	name, _ := url.PathUnescape(path.Base(u.Path))
	it := &Item{ID: newID(), Name: cleanName(name), Source: "link", State: stateDownloading, Added: time.Now().UTC()}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.items = append(a.items, it)
	a.cancels[it.ID] = cancel
	a.saveLocked()
	c := *it
	a.mu.Unlock()
	a.logf("download %s started: %s", it.ID, u.Redacted())

	go func() {
		defer a.dropCancel(it.ID)
		if err := a.download(ctx, it.ID, u); err != nil {
			if ctx.Err() == nil {
				a.fail(it.ID, err)
			}
			return
		}
		a.update(it.ID, func(it *Item) { it.State, it.Progress = stateQueued, 0 }, true)
		a.queue <- it.ID
	}()
	return &c, nil
}

func (a *Agent) download(ctx context.Context, id string, u *url.URL) error {
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}
	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the link answered %s", res.Status)
	}
	if ct := res.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
		return errors.New("the link leads to a web page, not a video file; use the file's direct download link")
	}
	size := res.ContentLength
	if size > 0 {
		if err := a.checkSpace(size); err != nil {
			return err
		}
	}
	a.update(id, func(it *Item) {
		if size > 0 {
			it.Size = size
		}
		if _, p, err := mime.ParseMediaType(res.Header.Get("Content-Disposition")); err == nil && p["filename"] != "" {
			it.Name = cleanName(p["filename"])
		}
	}, true)

	f, err := os.OpenFile(a.srcPath(id), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 1<<20)
	var got int64
	last := time.Now()
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			got += int64(n)
			if time.Since(last) > 500*time.Millisecond {
				last = time.Now()
				a.update(id, func(it *Item) {
					it.Received = got
					if size > 0 {
						it.Progress = float64(got) / float64(size)
					}
				}, false)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if size > 0 && got != size {
		return fmt.Errorf("the download stopped at %s of %s", humanBytes(got), humanBytes(size))
	}
	a.update(id, func(it *Item) { it.Received, it.Size, it.Progress = got, got, 1 }, false)
	a.logf("download %s complete: %s", id, humanBytes(got))
	return nil
}

func (a *Agent) dropCancel(id string) {
	a.mu.Lock()
	delete(a.cancels, id)
	a.mu.Unlock()
}

/* ------------------------------ preparation ------------------------------ */

// prepWorker prepares one video at a time: the encoder already uses every core.
func (a *Agent) prepWorker() {
	for id := range a.queue {
		a.mu.Lock()
		it := a.findLocked(id)
		ok := it != nil && it.State == stateQueued
		ctx, cancel := context.WithCancel(context.Background())
		if ok {
			it.State, it.Progress = statePreparing, 0
			a.cancels[id] = cancel
			a.saveLocked()
		}
		a.mu.Unlock()
		if !ok {
			cancel()
			continue
		}
		err := a.prepare(ctx, id)
		cancel()
		a.dropCancel(id)
		if ctx.Err() != nil {
			continue // removed while it was being prepared
		}
		if err != nil {
			a.fail(id, err)
		}
	}
}

func (a *Agent) prepContainer(id string) string { return "yvpn-prep-" + id }

// ffRun runs ffmpeg or ffprobe from the Jellyfin image in a throwaway
// container named after the item, so removing the item can stop it.
func (a *Agent) ffArgs(id, tool string, args ...string) []string {
	return append([]string{"run", "--rm", "--name", a.prepContainer(id),
		"-v", a.workDir() + ":/work", "--entrypoint", tool, a.cfg.Image}, args...)
}

func (a *Agent) prepare(ctx context.Context, id string) error {
	start := time.Now()
	in, out := "/work/"+id+".src", "/work/"+id+".mp4"
	raw, err := a.run.Run(ctx, "docker", a.ffArgs(id, ffprobePath,
		"-v", "error", "-print_format", "json", "-show_format", "-show_streams", in)...)
	if err != nil {
		if ctx.Err() == nil {
			return errors.New("this doesn't look like a video file ffmpeg can read")
		}
		return err
	}
	probe, err := parseProbe(raw)
	if err != nil {
		return err
	}
	plan, err := planPrep(probe, in, out)
	if err != nil {
		return err
	}
	a.logf("preparing %s: copy video=%v copy audio=%v downscale=%v", id, plan.CopyVideo, plan.CopyAudio, plan.Downscaled)

	dur := probe.Duration()
	err = a.run.Stream(ctx, func(line string) {
		if f, ok := progressFrom(line, dur); ok {
			a.update(id, func(it *Item) { it.Progress = f }, false)
		}
	}, "docker", a.ffArgs(id, ffmpegPath, plan.Args...)...)
	if err != nil {
		return fmt.Errorf("converting the video failed: %v", err)
	}

	a.mu.Lock()
	it := a.findLocked(id)
	if it == nil {
		a.mu.Unlock()
		return nil
	}
	file := a.libraryFile(it.Name)
	a.mu.Unlock()
	if err := os.Rename(a.outPath(id), filepath.Join(a.cfg.MediaDir, file)); err != nil {
		return err
	}
	os.Remove(a.srcPath(id))
	a.update(id, func(it *Item) { it.State, it.File, it.Progress, it.Error = stateReady, file, 1, "" }, true)
	a.logf("prepared %s as %q in %s", id, file, time.Since(start).Round(time.Second))

	go func() {
		if err := a.jf.Refresh(context.Background()); err != nil {
			a.logf("library refresh failed: %v", err)
		}
	}()
	return nil
}

/* -------------------------------- removal -------------------------------- */

// remove deletes an item wherever it is: cancels its download or conversion,
// and takes its files off the disk and out of the library.
func (a *Agent) remove(id string) error {
	a.mu.Lock()
	it := a.findLocked(id)
	if it == nil {
		a.mu.Unlock()
		return os.ErrNotExist
	}
	if c := a.cancels[id]; c != nil {
		c()
	}
	preparing := it.State == statePreparing
	file := it.File
	kept := a.items[:0]
	for _, x := range a.items {
		if x.ID != id {
			kept = append(kept, x)
		}
	}
	a.items = kept
	a.saveLocked()
	a.mu.Unlock()

	if preparing {
		// Cancelling the docker client leaves its container running.
		a.run.Run(context.Background(), "docker", "rm", "-f", a.prepContainer(id))
	}
	os.Remove(a.srcPath(id))
	os.Remove(a.outPath(id))
	if file != "" {
		os.Remove(filepath.Join(a.cfg.MediaDir, file))
		go a.jf.Refresh(context.Background())
	}
	a.logf("removed %s", id)
	return nil
}
