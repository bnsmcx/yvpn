package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// The node is reached only through Tailscale. `tailscale serve` terminates
// HTTPS with the tailnet's own certificate and proxies to services that listen
// on 127.0.0.1 alone, so nothing here is ever on the droplet's public address.
// `tailscale funnel` is the same thing, opened to the internet.

type tsStatus struct {
	BackendState string
	Self         struct{ DNSName string }
}

func (a *Agent) tailscaleStatus(ctx context.Context) (tsStatus, error) {
	var s tsStatus
	out, err := a.run.Run(ctx, "tailscale", "status", "--json")
	if err != nil {
		return s, err
	}
	err = json.Unmarshal([]byte(out), &s)
	return s, err
}

// serveTarget is the local URL a tailnet HTTPS port forwards to.
func serveTarget(listen string) string { return "http://" + listen }

func (a *Agent) jellyfinTarget() string { return strings.TrimRight(a.cfg.JellyfinURL, "/") }

// serve publishes a local service on a tailnet HTTPS port, to the tailnet
// only. Over a port that was funnelled, it takes it back off the internet.
func (a *Agent) serve(ctx context.Context, port, target string) error {
	_, err := a.run.Run(ctx, "tailscale", "serve", "--bg", "--yes", "--https="+port, target)
	return err
}

func (a *Agent) funnel(ctx context.Context, port, target string) error {
	_, err := a.run.Run(ctx, "tailscale", "funnel", "--bg", "--yes", "--https="+port, target)
	return err
}

// funnelled reports whether a tailnet HTTPS port is open to the internet,
// from Tailscale's own config rather than from what the agent last asked for.
func (a *Agent) funnelled(ctx context.Context, port string) (bool, error) {
	out, err := a.run.Run(ctx, "tailscale", "funnel", "status", "--json")
	if err != nil {
		return false, err
	}
	var cfg struct{ AllowFunnel map[string]bool }
	if strings.TrimSpace(out) == "" || strings.TrimSpace(out) == "{}" {
		return false, nil
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		return false, err
	}
	for hostPort, on := range cfg.AllowFunnel {
		if on && strings.HasSuffix(hostPort, ":"+port) {
			return true, nil
		}
	}
	return false, nil
}

// funnelHint adds the fix to Tailscale's refusal, when it's the usual one.
func funnelHint(err error) error {
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "funnel") && (strings.Contains(low, "attribute") || strings.Contains(low, "not available") || strings.Contains(low, "not enabled")):
		return errors.New("Tailscale won't open Funnel for this node. In the Tailscale admin console, " +
			"Access controls must give it the \"funnel\" node attribute (new tailnets have it for every member). " +
			"Tailscale said: " + msg)
	case strings.Contains(low, "https") && strings.Contains(low, "cert"):
		return errors.New("HTTPS certificates are off for this tailnet. Turn them on in the Tailscale admin console " +
			"under DNS. Tailscale said: " + msg)
	}
	return err
}
