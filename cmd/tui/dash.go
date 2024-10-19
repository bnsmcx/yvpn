package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type Dash struct {
	tokens struct {
		digitalOcean string
		tailscale    string
	}
}

func (m Dash) Init() tea.Cmd {
	return nil
}

func (m Dash) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Dash) View() string {
	return fmt.Sprintf("Hello %s, %s", m.tokens.digitalOcean, m.tokens.tailscale)
}

func NewDash(tokenDO, tokenTS string) Dash {
	return Dash{
		tokens: struct {
			digitalOcean string
			tailscale    string
		}{
			digitalOcean: tokenDO,
			tailscale:    tokenTS,
		},
	}
}
