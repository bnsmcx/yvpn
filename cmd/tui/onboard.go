package main

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type Onboard struct {
	form *huh.Form
}

func (m Onboard) Init() tea.Cmd {
	return m.form.Init()
}

func (m Onboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
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

	// Check if form is completed
	if m.form.State == huh.StateCompleted {
		dash, err := NewDash(
      m.form.GetString("digital_ocean"), 
      m.form.GetString("tailscale"))
		if err != nil {
      m = NewOnboarding()
			return m, m.Init()
		}
		return dash, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

func (m Onboard) View() string {
	return m.form.View()
}

func NewOnboarding() Onboard {
	m := Onboard{}

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("tailscale").
				Title("Tailscale API Token").
				Validate(requiredField),
			huh.NewInput().
				Key("digital_ocean").
				Title("Digital Ocean API Token").
				Validate(requiredField),
		),
	).WithWidth(45).WithShowHelp(false).WithShowErrors(false)

	return m
}

func requiredField(str string) error {
	if str == "" {
		return errors.New("This field is required.")
	}
	return nil
}
