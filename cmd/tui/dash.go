package main

import (
	"fmt"
	"strings"
	"yvpn/pkg/digital_ocean"

	tea "github.com/charmbracelet/bubbletea"
)

type Dash struct {
	tokens struct {
		digitalOcean string
		tailscale    string
	}
	Datacenters []string
	endpoints   map[string]string // name to digital ocean id
	cursor      int
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
		case "l", "h", "tab", "shift+tab":
			if m.cursor == 1 {
				m.cursor = 0
			} else {
				m.cursor = 1
			}
    case "enter":
      switch m.cursor {
      case 0:
        return NewAdd(m), tea.EnterAltScreen
      case 1:
    }
		}
	}
	return m, nil
}

func (m Dash) View() string {
	var sb strings.Builder
	sb.WriteString("|---[ yVPN dashboard]--------------------------------------\n")
	sb.WriteString("|                                                          \n")
	sb.WriteString("| Available Datacenters:                                   \n")
	sb.WriteString("|   ")
	remaining := 56
	for i, dc := range m.Datacenters {
		if remaining-(len(dc)+2) < 0 { // length of dc name plus space and comma
			sb.WriteString("\n|   ")
			remaining = 56
		}

		if i != len(m.Datacenters)-1 {
			sb.WriteString(fmt.Sprintf("%s, ", dc))
			remaining -= len(dc) + 2 // length of dc name plus space and comma
		} else {
			sb.WriteString(fmt.Sprintf("%s\n", dc))
		}
	}
	sb.WriteString("|                                                          \n")
	sb.WriteString("| Active Exit Nodes:                                       \n")
	sb.WriteString("|   <---- TODO, Not Implemented ---->                      \n")
	sb.WriteString("|                                                          \n")
	sb.WriteString("| Actions:                                                 \n")
	switch m.cursor {
	case 0:
		sb.WriteString("|                   > Add <     Delete                   \n")
	case 1:
		sb.WriteString("|                     Add     > Delete <                 \n")
	}
	sb.WriteString("|----------------------------------------------------------\n")

	return sb.String()
}

func NewDash(tokenDO, tokenTS string) Dash {
	datacenters, err := digital_ocean.FetchDatacenters(tokenDO)
	if err != nil {
		panic(err)
	}

	return Dash{
		Datacenters: datacenters,
		tokens: struct {
			digitalOcean string
			tailscale    string
		}{
			digitalOcean: tokenDO,
			tailscale:    tokenTS,
		},
	}
}
