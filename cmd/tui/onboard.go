package main

import (
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

type Onboard struct {
	renderer *lipgloss.Renderer
	form     *huh.Form
	width    int
	height   int
}

func (m Onboard) Init() tea.Cmd {
	return m.form.Init()
}

func (m Onboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, contain(msg.Height, 30)
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
		dash, err := NewDash(m.renderer, m.height, m.width,
			m.form.GetString("digital_ocean"),
			m.form.GetString("tailscale"))
		if err != nil {
			resetModel := NewOnboarding(m.height, m.width, m.renderer)
			return resetModel, resetModel.Init()
		}
		return dash, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

func (m Onboard) View() string {
	top := getTopBar("Onboarding", m.renderer, m.width)
	bottom := getBottomBar(m.renderer, m.width, "")
	content := lipgloss.Place(m.width,
		m.height-(lipgloss.Height(top)+lipgloss.Height(bottom)),
		lipgloss.Center, lipgloss.Center, m.getStyledForm())
	return fmt.Sprint(lipgloss.JoinVertical(lipgloss.Center, top, content, bottom))
}

func (m Onboard) getStyledForm() string {
	m.form.WithTheme(m.formTheme()).WithWidth(m.width - (m.width / 4))
	return m.renderer.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(ACCENT_COLOR)).
		PaddingTop(2).
		PaddingLeft(2).
		PaddingRight(2).
		Render(m.form.View())
}

func (m Onboard) formTheme() *huh.Theme {
	custom := huh.ThemeBase()
	custom.Blurred.Title = m.renderer.NewStyle().
		Foreground(lipgloss.Color("8"))
	custom.Blurred.TextInput.Prompt = m.renderer.NewStyle().
		Foreground(lipgloss.Color("8"))
	custom.Blurred.TextInput.Text = m.renderer.NewStyle().
		Foreground(lipgloss.Color("8"))
	custom.Focused.Title = m.renderer.NewStyle().
		Foreground(lipgloss.Color(ACCENT_COLOR))
	custom.Focused.TextInput.Prompt = m.renderer.NewStyle().
		Foreground(lipgloss.Color(ACCENT_COLOR))
	custom.Focused.TextInput.Cursor = m.renderer.NewStyle().
		Foreground(lipgloss.Color(ACCENT_COLOR))
	custom.Focused.Base = m.renderer.NewStyle().
		Padding(0, 1).
		Border(lipgloss.ThickBorder(), false).
		BorderLeft(true).
		BorderForeground(lipgloss.Color(ACCENT_COLOR))
	return custom
}

func NewOnboarding(height, width int, renderer *lipgloss.Renderer) Onboard {
	m := Onboard{
		width:  width,
		height: contain(height, 30),
	}

	if renderer != nil {
		m.renderer = renderer
	} else {
		m.renderer = lipgloss.DefaultRenderer()
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
