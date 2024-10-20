package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type tickMsg struct{}

type doneMsg struct{}

type Add struct {
	dash       tea.Model
	form       *huh.Form
	started    bool
	done       bool
	start      time.Time
	datacenter string
}

func (m Add) Init() tea.Cmd {
	return m.form.Init()
}

func (m Add) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.started && !m.done {
			return m, tick()
		}
		return m, nil
	case doneMsg:
		m.done = true
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
	}

	var cmds []tea.Cmd

	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
		cmds = append(cmds, cmd)
	}

	if m.form.State == huh.StateCompleted && !m.started {
		m.datacenter = m.form.GetString("datacenter")
		m.start = time.Now()
		m.started = true
		cmds = append(cmds, tick(), m.addExit())
	}

	return m, tea.Batch(cmds...)
}

func (m Add) addExit() tea.Cmd {
	return func() tea.Msg {
		// Simulate a long task (e.g., 5 seconds)
		time.Sleep(5 * time.Second)
		return doneMsg{}
	}
}

func (m Add) View() string {
	var content string
	if m.form.State == huh.StateCompleted {
		if m.done {
			content = fmt.Sprintf("Done in %s", time.Since(m.start))
		} else {
			var sb strings.Builder
			sb.WriteString("|---[ yVPN add exit node ]---------------------------------\n")
			sb.WriteString("|                                                          \n")
			sb.WriteString(fmt.Sprintf("|  Creating new exit node: %s\n", m.datacenter))
			sb.WriteString(fmt.Sprintf("|    Elapsed time: %s", time.Since(m.start).String()))
			sb.WriteString("|                                                          \n")
			sb.WriteString("|    Average time: ~180 seconds (placeholder guess)        \n")
			sb.WriteString("|                                                          \n")
			sb.WriteString("|----------------------------------------------------------\n")

			content = sb.String()
		}
	} else {
		content = m.form.View()
	}
	return content
}

func tick() tea.Cmd {
	return tea.Tick(time.Duration(time.Second)/60, func(_ time.Time) tea.Msg {
		return tickMsg{}
	})
}

func NewAdd(dash Dash) Add {
	m := Add{
		dash: dash,
	}

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("datacenter").
				Options(huh.NewOptions(dash.Datacenters...)...).
				Title("Choose your level").
				Description("This will determine your benefits package"),

			huh.NewConfirm().
				Key("done").
				Title("All done?").
				Validate(func(v bool) error {
					if !v {
						return fmt.Errorf("Welp, finish up then")
					}
					return nil
				}).
				Affirmative("Yep").
				Negative("Wait, no")),
	).WithWidth(60).WithShowHelp(true).WithShowErrors(false)

	return m
}
