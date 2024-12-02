package main

import (
	"errors"
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type Onboard struct {
	form   *huh.Form
	width  int
	height int
}

func (m Onboard) Init() tea.Cmd {
	return m.form.Init()
}

func (m Onboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
	top := m.getTopBar()
	content := lipgloss.Place(m.width, m.height-lipgloss.Height(top),
		lipgloss.Center, lipgloss.Center, m.form.View())
	return fmt.Sprint(lipgloss.JoinVertical(lipgloss.Center, top, content))
}

func (m Onboard) getTopBar() string {
	style := lipgloss.NewStyle().
		Background(lipgloss.Color("9")).
		Foreground(lipgloss.Color("15")).
		MarginBottom(1)
	left := lipgloss.NewStyle().Align(lipgloss.Left).PaddingLeft(1).
		Render("Onboarding")
	right := lipgloss.NewStyle().Align(lipgloss.Right).PaddingRight(1).
		Render(fmt.Sprintf("yVPN %s", VERSION))
	padding := strings.Repeat(" ",
		m.width-(lipgloss.Width(left)+lipgloss.Width(right)))
	bar := lipgloss.JoinHorizontal(lipgloss.Center, left, padding, right)
	return style.Render(bar)
}

func NewOnboarding() Onboard {
	w, h, err := term.GetSize(0)
	if err != nil {
		panic(err)
	}

	m := Onboard{
		width:  w,
		height: h,
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
