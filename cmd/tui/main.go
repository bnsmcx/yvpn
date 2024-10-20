package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	cLog "github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
)

func main() {
	if _, err := tea.LogToFile("debug.log", "simple"); err != nil {
		log.Fatal(err)
	}

	if len(os.Args) > 1 && os.Args[1] == "ssh" {
		serveOverSSH("0.0.0.0", "1337")
	} else {
		do, good1 := os.LookupEnv("DIGITAL_OCEAN_TOKEN")
		ts, good2 := os.LookupEnv("TAILSCALE_API")
		if good1 && good2 {
			p := tea.NewProgram(NewDash(do, ts), tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				log.Fatal(err)
			}
		} else {
			p := tea.NewProgram(NewOnboarding(), tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				log.Fatal(err)
			}
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
		cLog.Error("Could not start server", "error", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	cLog.Info("Starting SSH server", "host", host, "port", port)
	go func() {
		if err = s.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			cLog.Error("Could not start server", "error", err)
			done <- nil
		}
	}()

	<-done
	cLog.Info("Stopping SSH server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() { cancel() }()
	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		cLog.Error("Could not stop server", "error", err)
	}
}

func teaHandler(s ssh.Session) (tea.Model, []tea.ProgramOption) {
	return NewOnboarding(), []tea.ProgramOption{tea.WithAltScreen()}
}
