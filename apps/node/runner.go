package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs the machine's own tools: tailscale, docker, apt-get, systemctl.
// Tests swap in a fake that records commands and answers for them.
type Runner interface {
	// Run returns combined output. A failure's error carries that output, since
	// it is usually the only explanation the tool gives.
	Run(ctx context.Context, name string, args ...string) (string, error)
	// Stream calls onLine for each line of stdout as it arrives.
	Stream(ctx context.Context, onLine func(string), name string, args ...string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(out), cmdError(name, args, err, out)
	}
	return string(out), nil
}

func (execRunner) Stream(ctx context.Context, onLine func(string), name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		onLine(sc.Text())
	}
	if err := cmd.Wait(); err != nil {
		return cmdError(name, args, err, stderr.Bytes())
	}
	return nil
}

// cmdError keeps the tail of a tool's output: the last lines are where tools
// put the reason, and the whole of an apt or docker log is too much to show.
func cmdError(name string, args []string, err error, out []byte) error {
	msg := strings.TrimSpace(string(out))
	if len(msg) > 600 {
		msg = "…" + msg[len(msg)-600:]
	}
	sub := ""
	if len(args) > 0 {
		sub = " " + args[0]
	}
	if msg == "" {
		return fmt.Errorf("%s%s: %v", name, sub, err)
	}
	return fmt.Errorf("%s%s: %v: %s", name, sub, err, msg)
}
