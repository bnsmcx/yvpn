package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"yvpn/pkg/digital_ocean"
	"yvpn/pkg/tailscale"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type tickMsg struct{}

type doneMsg struct {
	name string
	id   int
}

type Add struct {
	dash       Dash
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
    m.dash.endpoints[msg.id] = msg.name
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
		m.datacenter = m.form.GetString("datacenter")
		m.start = time.Now()
		m.started = true
		cmds = append(cmds, tick(), m.addExit())
	}

	return m, tea.Batch(cmds...)
}

func (m Add) addExit() tea.Cmd {
	return func() tea.Msg {
		tailscaleAuth, tsKeyID, err := tailscale.GetAuthKey(m.dash.tokens.tailscale)
		if err != nil {
			log.Println("getting tailscale key:", err)
			os.Exit(1)
		}

		name, id, err := digital_ocean.Create(m.dash.tokens.digitalOcean, tailscaleAuth, m.datacenter)
		if err != nil {
			log.Println("creating droplet:", err)
			os.Exit(1)
		}

		_, err = tailscale.EnableExit(name, m.dash.tokens.tailscale)
		if err != nil {
			log.Printf("\tenabling tailscale exit: %s\n", err.Error())
			digital_ocean.Delete(m.dash.tokens.digitalOcean, id)
			tailscale.DeleteAuthKey(m.dash.tokens.tailscale, tsKeyID)
			os.Exit(1)
		}

		err = tailscale.DeleteAuthKey(m.dash.tokens.tailscale, tsKeyID)
		if err != nil {
			fmt.Println("deleting tailscale key:", err)
			os.Exit(1)
		}

		return doneMsg{name: name, id: id}
	}
}

func (m Add) View() string {
	var content string
	if m.form.State == huh.StateCompleted {
		if m.done {
			content = fmt.Sprintf("Done in %s (press enter to return to dash)", time.Since(m.start))
		} else {
			var sb strings.Builder
			sb.WriteString("|---[ yVPN add exit node ]---------------------------------\n")
			sb.WriteString("|                                                          \n")
			sb.WriteString(fmt.Sprintf("|  Creating new exit node: %s\n", m.datacenter))
			sb.WriteString(fmt.Sprintf("|    Elapsed time: %s\n", time.Since(m.start).String()))
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
