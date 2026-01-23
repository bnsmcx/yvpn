package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	"golang.org/x/term"
)

const (
	VERSION = "0.1.0"
)

func main() {
	if _, err := tea.LogToFile("debug.log", "simple"); err != nil {
		log.Fatal(err)
	}

	if len(os.Args) < 2 {
		printUsage()
		return
	}

	switch os.Args[1] {
	case "tui":
		runTUI()
	case "ssh":
		serveOverSSH("0.0.0.0", "1337")
	case "list", "datacenters", "create", "delete":
		runCLI(os.Args[1:])
	case "--help", "-h", "help":
		printUsage()
	case "--version", "-v", "version":
		fmt.Printf("yvpn version %s\n", VERSION)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command '%s'\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runTUI() {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		log.Fatal(err)
	}
	do, good1 := os.LookupEnv("DIGITAL_OCEAN_TOKEN")
	ts, good2 := os.LookupEnv("TAILSCALE_API")
	if good1 && good2 {
		dash, err := NewDash(nil, h, w, do, ts)
		if err != nil {
			log.Fatal(err)
		}
		p := tea.NewProgram(dash, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			log.Fatal(err)
		}
	} else {
		p := tea.NewProgram(NewOnboarding(h, w, nil), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			log.Fatal(err)
		}
	}
}

func serveOverSSH(host, port string) {
	s, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
		wish.WithHostKeyPath(".ssh/id_ed25519"),
		wish.WithMiddleware(
			bubbletea.Middleware(teaHandler),
			activeterm.Middleware(), // Bubble Tea apps usually require a PTY.
			logging.Middleware(),
		),
	)
	if err != nil {
		log.Error("Could not start server", "error", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	log.Info("Starting SSH server", "host", host, "port", port)
	go func() {
		if err = s.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("Could not start server", "error", err)
			done <- nil
		}
	}()

	<-done
	log.Info("Stopping SSH server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() { cancel() }()
	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("Could not stop server", "error", err)
	}
}

func teaHandler(s ssh.Session) (tea.Model, []tea.ProgramOption) {
	pty, _, active := s.Pty()
	if !active {
		wish.Fatalln(s, "no active terminal, skipping")
		return nil, nil
	}

	renderer := bubbletea.MakeRenderer(s)

	var do, ts string
	for _, val := range s.Environ() {
		keyValPair := strings.Split(val, "=")
		key := keyValPair[0]
		val = keyValPair[1]
		if key == "DIGITAL_OCEAN_TOKEN" {
			do = val
		}
		if key == "TAILSCALE_API" {
			ts = val
		}
	}

	if do != "" && ts != "" {
		dash, err := NewDash(renderer, pty.Window.Height, pty.Window.Width, do, ts)
		// Only go straight to dash if the creds seem valid, go to onboarding
		if err == nil {
			return dash, []tea.ProgramOption{tea.WithAltScreen()}
		}
	}

	return NewOnboarding(pty.Window.Height, pty.Window.Width, renderer),
		[]tea.ProgramOption{tea.WithAltScreen()}
}
