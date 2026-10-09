package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// install brings a fresh droplet up to a working Jellyfin. The steps are all
// idempotent, so after a reboot or a failure it simply runs again from the top
// and skips what is already in place.
func (a *Agent) install(ctx context.Context) error {
	for _, d := range []string{a.cfg.DataDir, a.workDir(), a.cfg.MediaDir,
		filepath.Join(a.cfg.DataDir, "jellyfin", "config"), filepath.Join(a.cfg.DataDir, "jellyfin", "cache")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	// First, so the dashboard can watch the rest. If the tailnet has HTTPS
	// certificates turned off this fails until someone turns them on; keep
	// trying rather than giving up on a node that will work in a minute.
	a.setStep("Opening the control port on your tailnet")
	for {
		err := a.openControlPort(ctx)
		if err == nil {
			break
		}
		a.logf("control port: %v (retrying in 15s)", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}

	if _, err := a.lookPath("docker"); err != nil {
		a.setStep("Installing Docker")
		if err := a.installDocker(ctx); err != nil {
			return err
		}
	}

	a.setStep("Downloading Jellyfin")
	if err := retry(ctx, 3, func() error {
		_, err := a.run.Run(ctx, "docker", "pull", "--quiet", a.cfg.Image)
		return err
	}); err != nil {
		return err
	}
	a.setStep("Starting Jellyfin")
	if err := a.startJellyfin(ctx); err != nil {
		return err
	}
	a.blockMetadata(ctx)

	a.setStep("Setting up Jellyfin")
	if err := a.jf.WaitHealthy(ctx, 10*time.Minute); err != nil {
		return err
	}
	if err := a.jf.Setup(ctx, a.serverName(), a.cfg.GuestUser, a.cfg.GuestPassword); err != nil {
		return err
	}

	a.setStep("Publishing Jellyfin on your tailnet")
	on, err := a.funnelled(ctx, "443")
	if err != nil {
		a.logf("funnel status: %v", err)
	}
	if !on {
		if err := a.serve(ctx, "443", a.jellyfinTarget()); err != nil {
			return err
		}
	} else if err := a.jf.SetGuestEnabled(ctx, a.cfg.GuestUser, true); err != nil {
		// Shared before a restart: keep it shared, and the guest able to sign in.
		return err
	}
	a.mu.Lock()
	a.share.Enabled = on
	a.mu.Unlock()
	return nil
}

func (a *Agent) openControlPort(ctx context.Context) error {
	st, err := a.tailscaleStatus(ctx)
	if err != nil {
		return err
	}
	if st.BackendState != "Running" {
		return fmt.Errorf("tailscale is %q, not running", st.BackendState)
	}
	a.mu.Lock()
	a.fqdn = strings.TrimSuffix(st.Self.DNSName, ".")
	a.mu.Unlock()
	return a.serve(ctx, controlPort, serveTarget(a.cfg.Listen))
}

// Ubuntu's own docker.io: one apt transaction from DigitalOcean's mirror.
// First boot races unattended-upgrades for the dpkg lock, hence the timeout.
func (a *Agent) installDocker(ctx context.Context) error {
	apt := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "apt-get", append([]string{"-o", "DPkg::Lock::Timeout=600", "-q", "-y"}, args...)...)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return cmdError("apt-get", args, err, out)
		}
		return nil
	}
	if err := retry(ctx, 3, func() error { return apt("update") }); err != nil {
		return err
	}
	if err := retry(ctx, 3, func() error { return apt("install", "--no-install-recommends", "docker.io") }); err != nil {
		return err
	}
	_, err := a.run.Run(ctx, "systemctl", "enable", "--now", "docker")
	return err
}

func (a *Agent) startJellyfin(ctx context.Context) error {
	if out, err := a.run.Run(ctx, "docker", "inspect", "-f", "{{.State.Running}}", a.cfg.Container); err == nil {
		if strings.TrimSpace(out) == "true" {
			return nil
		}
		_, err := a.run.Run(ctx, "docker", "start", a.cfg.Container)
		return err
	}
	port := strings.TrimPrefix(strings.TrimPrefix(a.jellyfinTarget(), "http://"), "https://")
	a.mu.Lock()
	published := "https://" + a.fqdn
	a.mu.Unlock()
	_, err := a.run.Run(ctx, "docker", "run", "-d",
		"--name", a.cfg.Container,
		"--restart", "unless-stopped",
		// Loopback only: Tailscale is the one way in.
		"-p", port+":8096",
		"-e", "JELLYFIN_PublishedServerUrl="+published,
		"-v", filepath.Join(a.cfg.DataDir, "jellyfin", "config")+":/config",
		"-v", filepath.Join(a.cfg.DataDir, "jellyfin", "cache")+":/cache",
		"-v", a.cfg.MediaDir+":"+libraryPath+":ro",
		a.cfg.Image)
	return err
}

// blockMetadata stops containers reading the droplet's metadata service, which
// would hand anything that compromised Jellyfin (reachable from the internet
// while shared) this node's cloud-init, passwords included. Best effort: on a
// machine without iptables there is no droplet metadata to protect either.
func (a *Agent) blockMetadata(ctx context.Context) {
	rule := []string{"DOCKER-USER", "-d", "169.254.169.254", "-j", "DROP"}
	if _, err := a.run.Run(ctx, "iptables", append([]string{"-C"}, rule...)...); err == nil {
		return
	}
	if _, err := a.run.Run(ctx, "iptables", append([]string{"-I"}, rule...)...); err != nil {
		a.logf("could not block the metadata service from containers: %v", err)
	}
}

func (a *Agent) serverName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if h, _, ok := strings.Cut(a.fqdn, "."); ok && h != "" {
		return h
	}
	return "yvpn"
}

func (a *Agent) setStep(s string) {
	a.mu.Lock()
	a.step = s
	a.mu.Unlock()
	a.logf("%s…", s)
}

// runInstall runs install in the background and records how it ended. A
// failure can be retried from the dashboard.
func (a *Agent) runInstall(ctx context.Context) {
	a.mu.Lock()
	if a.installRunning {
		a.mu.Unlock()
		return
	}
	a.installRunning = true
	a.phase, a.instErr, a.step = "installing", "", "Starting"
	a.mu.Unlock()

	start := time.Now()
	err := a.install(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.installRunning = false
	if err != nil {
		a.phase, a.instErr = "failed", err.Error()
		a.logTail = append(a.logTail, "install failed: "+err.Error())
		return
	}
	a.phase, a.step = "ready", "Ready"
	a.logTail = append(a.logTail, fmt.Sprintf("ready (%s)", time.Since(start).Round(time.Second)))
}

func retry(ctx context.Context, tries int, fn func() error) error {
	var err error
	for i := 0; i < tries; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i < tries-1 {
			select {
			case <-ctx.Done():
				return errors.Join(err, ctx.Err())
			case <-time.After(time.Duration(5*(i+1)) * time.Second):
			}
		}
	}
	return err
}
