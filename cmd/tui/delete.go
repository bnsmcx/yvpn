package main

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"log"
	"strings"
	"time"
	"yvpn/pkg/digital_ocean"

	tea "github.com/charmbracelet/bubbletea"
)

type Delete struct {
	dash     Dash
	started  bool
	done     bool
	start    time.Time
	endpoint string
	height   int
	width    int
	renderer *lipgloss.Renderer
}

type deletedMsg struct {
	name string
	id   int
}

func (m Delete) Init() tea.Cmd {
	return nil
}

func (m Delete) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.started && !m.done {
			return m, tick()
		}
		return m, nil
	case deletedMsg:
		m.done = true
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
			}
		}
	}

	var cmds []tea.Cmd

	if !m.started {
		m.start = time.Now()
		m.started = true
		cmds = append(cmds, tick(), m.deleteExit())
	}

	return m, tea.Batch(cmds...)
}

func (m Delete) deleteExit() tea.Cmd {
	return func() tea.Msg {
		id, _ := m.dash.endpoints[m.endpoint]
		if err := digital_ocean.Delete(m.dash.tokens.digitalOcean, id); err != nil {
			log.Println(err)
		}
		delete(m.dash.endpoints, m.endpoint)
		return deletedMsg{name: m.endpoint, id: id}
	}
}

func (m Delete) View() string {
	top := getTopBar("Deleting exit node", m.renderer, m.width)
	bottom := getBottomBar(m.renderer, m.width, "esc [return to dash]")
	height := m.height - (lipgloss.Height(top) + lipgloss.Height(bottom))
	var content string
	if m.done {
		msg := fmt.Sprintf(" Done in %s (press enter to return to dash)", time.Since(m.start))
		msg = fmt.Sprintf("%s%s\n", msg, strings.Repeat(" ", m.width-lipgloss.Width(msg)))
		msg = m.renderer.NewStyle().
			Foreground(lipgloss.Color(ACCENT_COLOR)).Render(msg)
		content = lipgloss.Place(m.width, height,
			lipgloss.Top, lipgloss.Left,
			lipgloss.PlaceHorizontal(m.width, lipgloss.Center, msg))
	} else {
		var sb strings.Builder
		msg := fmt.Sprintf(" Deleting exit node: %s", m.endpoint)
		msg = fmt.Sprintf("%s%s\n", msg, strings.Repeat(" ", m.width-lipgloss.Width(msg)-4))
		sb.WriteString(msg)
		msg = fmt.Sprintf(" \tElapsed time: %s", time.Since(m.start).String())
		msg = fmt.Sprintf("%s%s\n", msg, strings.Repeat(" ", m.width-lipgloss.Width(msg)-4))
		sb.WriteString(msg)
		content = m.renderer.NewStyle().
			Foreground(lipgloss.Color(ACCENT_COLOR)).Render(sb.String())
		content = lipgloss.Place(m.width, height,
			lipgloss.Top, lipgloss.Left,
			lipgloss.PlaceHorizontal(m.width, lipgloss.Center, content))
	}
	return fmt.Sprint(lipgloss.JoinVertical(lipgloss.Center, top, content, bottom))
}

func NewDelete(dash Dash) Delete {
	m := Delete{
		renderer: dash.renderer,
		dash:     dash,
		width:    dash.width,
		height:   dash.height,
		endpoint: dash.table.SelectedRow()[0],
	}

	return m
}
