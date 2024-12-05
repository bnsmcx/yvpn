package main

import (
	"fmt"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
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
	table       table.Model
	endpoints   map[string]int //  name to digital ocean id
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
		case "enter":
		case "n":
			return NewAdd(m), tea.EnterAltScreen
		case "ctrl+c", "q":
			return m, tea.Quit
		}
	}

	// This is ugly but it works, "I'll refactor it later"
	m.table.SetHeight(m.height - (lipgloss.Height(
		getTopBar("", m.renderer, m.width)) +
		lipgloss.Height(getBottomBar(m.renderer, m.width, ""))) - 1)

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)

	return m, cmd
}

func (m Dash) View() string {
	top := getTopBar("Dashboard", m.renderer, m.width)
	bottom := getBottomBar(m.renderer, m.width, m.makeHelpMenu())
	height := m.height - (lipgloss.Height(top) + lipgloss.Height(bottom))
	content := lipgloss.Place(m.width, height,
		lipgloss.Top, lipgloss.Top,
		lipgloss.PlaceHorizontal(m.width, lipgloss.Center, m.table.View()))
	return fmt.Sprint(lipgloss.JoinVertical(lipgloss.Center, top, content, bottom))
}

func (m Dash) makeHelpMenu() string {
	processed := assembleHelpEntries(m.table.KeyMap.ShortHelp())
	processed = append(processed, "enter [interact]", "n [create new]")
	var spacer = " 𔗘 "
	var sb strings.Builder

	for i, entry := range processed {
		if sb.Len()+len(entry)+len(spacer) > m.width {
			break
		}

		sb.WriteString(entry)

		if i != len(processed)-1 {
			sb.WriteString(spacer)
		}
	}

	return sb.String()
}

func assembleHelpEntries(help []key.Binding) []string {
	var assembled []string
	for _, item := range help {
		assembled = append(assembled, makeHelpMsg(item))
	}
	return assembled
}

func makeHelpMsg(item key.Binding) string {
	return fmt.Sprintf("%s [%s]", item.Help().Key, item.Help().Desc)
}

func (m Dash) buildTable() table.Model {
	// These widths are set via manual tinkering
	width := (m.width - 8) / 2
	columns := []table.Column{
		{Title: "Exit node", Width: width},
		{Title: "ID", Width: width},
	}

	var rows []table.Row
	for id, name := range []string{"foo", "bar", "spam", "eggs", "rock", "the", "casbah", "my", "dude",
		"foo", "bar", "spam", "eggs", "rock", "the", "casbah", "my", "dude",
		"foo", "bar", "spam", "eggs", "rock", "the", "casbah", "my", "dude"} {
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

	return t
}

func NewDash(renderer *lipgloss.Renderer, h, w int, tokenDO, tokenTS string) (Dash, error) {
	datacenters, err := digital_ocean.FetchDatacenters(tokenDO)
	if err != nil {
		return Dash{}, fmt.Errorf("fetching available datacenters %s", err.Error())
	}

	dash := Dash{
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
	}

	dash.table = dash.buildTable()

	return dash, nil
}
