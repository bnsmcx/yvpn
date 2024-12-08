package main

import (
	"fmt"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"yvpn/pkg/digital_ocean"
	"yvpn/pkg/tailscale"

	tea "github.com/charmbracelet/bubbletea"
)

var messages []string
var mu sync.Mutex

type tickMsg struct{}

type addedMsg struct {
	name string
	id   int
}

type Add struct {
	width      int
	height     int
	table      table.Model
	renderer   *lipgloss.Renderer
	dash       Dash
	started    bool
	done       bool
	start      time.Time
	datacenter string
}

func (m Add) Init() tea.Cmd {
	return nil
}

func (m Add) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.started && !m.done {
			return m, tick()
		}
		return m, nil
	case addedMsg:
		m.done = true
		m.dash.endpoints[msg.name] = msg.id
		m.dash.table = m.dash.buildTable()
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m.dash, tea.EnterAltScreen
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			if m.done {
				return m.dash, tea.EnterAltScreen
			} else {
				m.datacenter = m.table.SelectedRow()[0]
				m.start = time.Now()
				m.started = true
				return m, tea.Batch(tick(), m.addExit())
			}
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

func (m Add) addExit() tea.Cmd {
	return func() tea.Msg {

		mu.Lock()
		messages = append(messages, " Getting an auth key from Tailscale...")
		mu.Unlock()
		tailscaleAuth, tsKeyID, err := tailscale.GetAuthKey(m.dash.tokens.tailscale)
		if err != nil {
			log.Println("getting tailscale key:", err)
			os.Exit(1)
		}

		mu.Lock()
		messages = append(messages,
			fmt.Sprintf(" Provisioning a new droplet in the %s datacenter...",
				m.datacenter))
		mu.Unlock()
		name, id, err := digital_ocean.Create(m.dash.tokens.digitalOcean, tailscaleAuth, m.datacenter)
		if err != nil {
			log.Println("creating droplet:", err)
			os.Exit(1)
		}

		mu.Lock()
		messages = append(messages, " Waiting for the new exit node to phone home to Tailscale...")
		mu.Unlock()
		_, err = tailscale.EnableExit(name, m.dash.tokens.tailscale)
		if err != nil {
			log.Printf("\tenabling tailscale exit: %s\n", err.Error())
			digital_ocean.Delete(m.dash.tokens.digitalOcean, id)
			tailscale.DeleteAuthKey(m.dash.tokens.tailscale, tsKeyID)
			os.Exit(1)
		}

		mu.Lock()
		messages = append(messages, " Deleting the Tailscale auth key...")
		mu.Unlock()
		err = tailscale.DeleteAuthKey(m.dash.tokens.tailscale, tsKeyID)
		if err != nil {
			fmt.Println("deleting tailscale key:", err)
			os.Exit(1)
		}

		mu.Lock()
		messages = append(messages, fmt.Sprintf(" Done in %s", time.Since(m.start)))
		mu.Unlock()
		return addedMsg{name: name, id: id}
	}
}

func (m Add) View() string {
	top := getTopBar("Create exit node", m.renderer, m.width)
	bottom := getBottomBar(m.renderer, m.width, "esc [return to dash]")
	height := m.height - (lipgloss.Height(top) + lipgloss.Height(bottom))
	var content string
	if m.started {
		mu.Lock()
		var sb strings.Builder
		var ending, padChar string
		for i, msg := range messages {
			sb.WriteString(msg)
			if i != len(messages)-1 {
				ending = "[Completed] \n"
				padChar = "."
			} else {
				ending = "\n"
				padChar = " "
			}
			padding := m.width - lipgloss.Width(msg+ending)
			sb.WriteString(strings.Repeat(padChar, padding))
			sb.WriteString(ending)
		}
		mu.Unlock()
		summary := m.renderer.NewStyle().
			Foreground(lipgloss.Color(ACCENT_COLOR)).Render(sb.String())
		content = lipgloss.Place(m.width, height,
			lipgloss.Top, lipgloss.Left,
			lipgloss.PlaceHorizontal(m.width, lipgloss.Center, summary))
	} else {
		content = lipgloss.Place(m.width, height,
			lipgloss.Top, lipgloss.Top,
			lipgloss.PlaceHorizontal(m.width, lipgloss.Center, m.table.View()))
	}
	return fmt.Sprint(lipgloss.JoinVertical(lipgloss.Center, top, content, bottom))
}

func (m Add) buildTable() table.Model {
	// These widths are set via manual tinkering
	width := (m.width - 8) / 2
	columns := []table.Column{
		{Title: "Datacenter", Width: width},
		{Title: "Provider", Width: width},
	}

	var rows []table.Row

	// test data
	//for id, name := range []string{"foo", "bar", "spam", "eggs", "rock", "the", "casbah", "my", "dude",
	//	"foo", "bar", "spam", "eggs", "rock", "the", "casbah", "my", "dude",
	//	"foo", "bar", "spam", "eggs", "rock", "the", "casbah", "my", "dude"} {
	//	rows = append(rows, table.Row{name, fmt.Sprint(id)})
	//}

	if len(m.dash.Datacenters) > 0 {
		for _, dc := range m.dash.Datacenters {
			rows = append(rows, table.Row{dc, "Digital Ocean"})
		}
	} else {
		rows = append(rows, table.Row{"None", ""})
	}

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

func tick() tea.Cmd {
	return tea.Tick(time.Duration(time.Second)/60, func(_ time.Time) tea.Msg {
		return tickMsg{}
	})
}

func NewAdd(dash Dash) Add {
	m := Add{
		dash:     dash,
		renderer: dash.renderer,
		width:    dash.width,
		height:   dash.height,
	}

	m.table = m.buildTable()

	return m
}
