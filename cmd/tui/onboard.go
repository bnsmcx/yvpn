package main

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type Onboard struct {
	tokens struct {
		digitalOcean string
		tailscale    string
	}
	form *huh.Form
}

func (m Onboard) Init() tea.Cmd {
	return m.form.Init()
}

func (m Onboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	m.tokens.digitalOcean = m.form.GetString("digital_ocean")
	m.tokens.tailscale = m.form.GetString("tailscale")

	if m.form.State == huh.StateCompleted {
		return NewDash(m.tokens.digitalOcean, m.tokens.tailscale), tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

func (m Onboard) View() string {
	return m.form.View()
}

func NewOnboarding() Onboard {
	m := Onboard{
		tokens: struct {
			digitalOcean string
			tailscale    string
		}{},
	}

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
