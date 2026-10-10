package main

import (
	"context"
	"errors"
	"time"
)

// setShare opens Jellyfin to the internet through Tailscale Funnel and lets the
// guest account sign in, or closes both again. Only Jellyfin's port is ever
// funnelled: uploads and this control API stay on the tailnet.
//
// The order matters both ways. Opening, the guest is enabled first so the link
// works the moment it is public; closing, the port is taken back first so no
// one is left on a public page that has stopped accepting them.
func (a *Agent) setShare(ctx context.Context, enabled bool) error {
	a.mu.Lock()
	switch {
	case a.phase != "ready":
		a.mu.Unlock()
		return errors.New("Jellyfin is still being set up")
	case a.share.Busy:
		a.mu.Unlock()
		return errors.New("sharing is already being changed")
	}
	a.share.Busy, a.share.Error = true, ""
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	err := a.applyShare(ctx, enabled)

	// Whatever happened, report what Tailscale is actually doing now.
	on, serr := a.funnelled(ctx, "443")
	a.mu.Lock()
	defer a.mu.Unlock()
	a.share.Busy = false
	if serr == nil {
		a.share.Enabled = on
	} else if err == nil {
		a.share.Enabled = enabled
	}
	if err != nil {
		a.share.Error = err.Error()
		a.logTail = append(a.logTail, "sharing: "+err.Error())
	}
	return err
}

func (a *Agent) applyShare(ctx context.Context, enabled bool) error {
	target := a.jellyfinTarget()
	if enabled {
		if err := a.jf.SetGuestEnabled(ctx, a.cfg.GuestUser, true); err != nil {
			return err
		}
		if err := a.funnel(ctx, "443", target); err != nil {
			// Leave the guest locked out again if the port never opened.
			a.jf.SetGuestEnabled(ctx, a.cfg.GuestUser, false)
			return funnelHint(err)
		}
		a.logf("sharing on: Jellyfin is on the internet through Funnel")
		return nil
	}

	// Serving the port again replaces its funnel with a tailnet-only handler.
	// Should Tailscale ever keep the funnel flag across that, turn it off
	// explicitly and serve once more.
	if err := a.serve(ctx, "443", target); err != nil {
		return err
	}
	if on, err := a.funnelled(ctx, "443"); err == nil && on {
		a.ts(ctx, "funnel", "--https=443", "off")
		if err := a.serve(ctx, "443", target); err != nil {
			return err
		}
	}
	if err := a.jf.SetGuestEnabled(ctx, a.cfg.GuestUser, false); err != nil {
		return err
	}
	a.logf("sharing off: Jellyfin is on the tailnet only")
	return nil
}
