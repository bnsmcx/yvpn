package main

import (
	"fmt"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
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
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, contain(msg.Height, 30)
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
	height := m.height - (lipgloss.Height(top) + lipgloss.Height(bottom))
	content := lipgloss.Place(m.width, height,
		lipgloss.Top, lipgloss.Top, m.content(height))
	return fmt.Sprint(lipgloss.JoinVertical(lipgloss.Center, top, content, bottom))
}

func (m Dash) content(height int) string {
	// These widths are set via manual tinkering
	width := (m.width - 8) / 2
	columns := []table.Column{
		{Title: "Exit node", Width: width},
		{Title: "ID", Width: width},
	}

	var rows []table.Row
	for id, name := range []string{"foo", "bar", "spam", "eggs", "rock", "the", "casbah", "my", "dude"} {
		rows = append(rows, table.Row{name, fmt.Sprint(id)})
	}
	//if len(m.endpoints) > 0 {
	//	for name, id := range m.endpoints {
	//		rows = append(rows, table.Row{name, fmt.Sprint(id)})
	//	}
	//} else {
	//	rows = append(rows, table.Row{"None", ""})
	//}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(height),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		Renderer(m.renderer).
		BorderStyle(lipgloss.NormalBorder()).
		Foreground(lipgloss.Color(ACCENT_COLOR)).
		BorderForeground(lipgloss.Color(ACCENT_COLOR)).
		BorderBottom(true).
		Bold(false)
	s.Cell = s.Cell.
		Renderer(m.renderer).
		Foreground(lipgloss.Color(ACCENT_COLOR)).
		Bold(false)
	s.Selected = s.Selected.
		Renderer(m.renderer).
		BorderStyle(lipgloss.InnerHalfBlockBorder()).
		Foreground(lipgloss.Color(ACCENT_COLOR)).
		BorderForeground(lipgloss.Color(ACCENT_COLOR)).
		BorderLeft(true).
		Bold(false)
	t.SetStyles(s)

	//switch m.cursor {
	//case 0:
	//	sb.WriteString("|                   > Add <     Delete                   \n")
	//case 1:
	//	sb.WriteString("|                     Add     > Delete <                 \n")
	//}

	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, t.View())
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
