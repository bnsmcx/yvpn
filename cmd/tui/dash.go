package main

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"strings"
	"yvpn/pkg/digital_ocean"

	tea "github.com/charmbracelet/bubbletea"
)

type Dash struct {
	renderer *lipgloss.Renderer
	height   int
	width    int
	tokens   struct {
		digitalOcean string
		tailscale    string
	}
	Datacenters []string
	endpoints   map[string]int //  name to digital ocean id
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
		case "l", "h", "tab", "shift+tab", "left", "right":
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
				return NewDelete(m), tea.EnterAltScreen
			}
		}
	}
	return m, nil
}

func (m Dash) View() string {
	top := getTopBar("Dashboard", m.renderer, m.width)
	bottom := getBottomBar(m.renderer, m.width)
	content := lipgloss.Place(m.width,
		m.height-(lipgloss.Height(top)+lipgloss.Height(bottom)),
		lipgloss.Center, lipgloss.Center, m.content())
	return fmt.Sprint(lipgloss.JoinVertical(lipgloss.Center, top, content, bottom))
}

func (m Dash) content() string {
	var sb strings.Builder
	sb.WriteString("|---[ yVPN dashboard ]-------------------------------------\n")
	sb.WriteString("|                                                          \n")
	sb.WriteString("|                                                          \n")
	sb.WriteString("| Active Exit Nodes:                                       \n")
	sb.WriteString("|                                                          \n")
	if len(m.endpoints) > 0 {
		for name, id := range m.endpoints {
			sb.WriteString(fmt.Sprintf("|   [%d] %s\n", id, name))
		}
	} else {
		sb.WriteString("|   [ none ]                                               \n")
	}
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

func NewDash(renderer *lipgloss.Renderer, h, w int, tokenDO, tokenTS string) (Dash, error) {
	datacenters, err := digital_ocean.FetchDatacenters(tokenDO)
	if err != nil {
		return Dash{}, fmt.Errorf("fetching available datacenters %s", err.Error())
	}

	return Dash{
		renderer:    renderer,
		height:      contain(h, 30),
		width:       w,
		endpoints:   make(map[string]int),
		Datacenters: datacenters,
		tokens: struct {
			digitalOcean string
			tailscale    string
		}{
			digitalOcean: tokenDO,
			tailscale:    tokenTS,
		},
	}, nil
}
