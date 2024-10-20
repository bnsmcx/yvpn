package main

import (
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type Add struct {
	dash tea.Model
	form *huh.Form
}

func (m Add) Init() tea.Cmd {
	return m.form.Init()
}

func (m Add) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
	}

	var cmds []tea.Cmd

	// Process the form
	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
		cmds = append(cmds, cmd)
	}

  log.Println(m.form.GetString("datacenter"))

	if m.form.State == huh.StateCompleted {
		return m.dash, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

func (m Add) View() string {
	return m.form.View()
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
