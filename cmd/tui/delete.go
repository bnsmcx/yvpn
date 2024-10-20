package main

import (
	"fmt"
	"log"
	"strings"
	"time"
	"yvpn/pkg/digital_ocean"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type Delete struct {
	dash     Dash
	form     *huh.Form
	started  bool
	done     bool
	start    time.Time
	endpoint string
}

type deletedMsg struct {
	name string
	id   int
}

func (m Delete) Init() tea.Cmd {
	return m.form.Init()
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
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			if m.done {
				return m.dash, tea.EnterAltScreen
			}
		}
	}

	var cmds []tea.Cmd

	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
		cmds = append(cmds, cmd)
	}

	if m.form.State == huh.StateCompleted && !m.started {
		m.endpoint = m.form.GetString("endpoint")
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
	var content string
	if m.form.State == huh.StateCompleted {
		if m.done {
			content = fmt.Sprintf("Done in %s (press enter to return to dash)", time.Since(m.start))
		} else {
			var sb strings.Builder
			sb.WriteString("|---[ yVPN delete exit node ]------------------------------\n")
			sb.WriteString("|                                                          \n")
			sb.WriteString(fmt.Sprintf("|  Deleting exit node: %s\n", m.endpoint))
			sb.WriteString(fmt.Sprintf("|    Elapsed time: %s\n", time.Since(m.start).String()))
			sb.WriteString("|                                                          \n")
			sb.WriteString("|                                                          \n")
			sb.WriteString("|----------------------------------------------------------\n")
			content = sb.String()
		}
	} else {
		content = m.form.View()
	}
	return content
}

func NewDelete(dash Dash) Delete {
	m := Delete{
		dash: dash,
	}

	var endpoints []string
	for name, _ := range m.dash.endpoints {
		endpoints = append(endpoints, name)
	}

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("endpoint").
				Options(huh.NewOptions(endpoints...)...).
				Title("Choose datacenter"),

			huh.NewConfirm().
				Key("done").
				Title("All done?").
				Validate(func(v bool) error {
					if !v {
						return fmt.Errorf("Welp, finish up then")
					}
					return nil
				}).
				Affirmative("Yes").
				Negative("No")),
	).WithWidth(60).WithShowHelp(true).WithShowErrors(false)

	return m
}
