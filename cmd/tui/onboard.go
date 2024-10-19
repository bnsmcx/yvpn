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

	if m.form.State == huh.StateCompleted {
		return dash("dash"), tea.Batch(cmds...)
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
				Title("Tailscale API Token").
				Value(&m.tokens.tailscale).
				Validate(requiredField),
			huh.NewInput().
				Title("Digital Ocean API Token").
				Value(&m.tokens.digitalOcean).
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
