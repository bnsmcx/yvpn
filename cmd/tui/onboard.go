package main

import (
	"errors"
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
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
		dash, err := NewDash(
			m.form.GetString("digital_ocean"),
			m.form.GetString("tailscale"))
		if err != nil {
			m = NewOnboarding(0, 0, nil)
			return m, m.Init()
		}
		return dash, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

func contain(height int, max int) int {
	if height > max {
		return max
	}

	return height
}

func (m Onboard) View() string {
	top := m.getTopBar()
	bottom := m.getBottomBar()
	content := lipgloss.Place(m.width,
		m.height-(lipgloss.Height(top)+lipgloss.Height(bottom)),
		lipgloss.Center, lipgloss.Center, m.getStyledForm())
	return fmt.Sprint(lipgloss.JoinVertical(lipgloss.Center, top, content, bottom))
}

func (m Onboard) getTopBar() string {
	style := m.renderer.NewStyle().
		Background(lipgloss.Color("E59500")).
		Foreground(lipgloss.Color("0")).
		MarginBottom(1)
	left := m.renderer.NewStyle().Align(lipgloss.Left).PaddingLeft(1).
		Render("Onboarding")
	right := m.renderer.NewStyle().Align(lipgloss.Right).PaddingRight(1).
		Render(fmt.Sprintf("yVPN %s", VERSION))
	padding := strings.Repeat(" ",
		m.width-(lipgloss.Width(left)+lipgloss.Width(right)))
	bar := lipgloss.JoinHorizontal(lipgloss.Center, left, padding, right)
	return style.Render(bar)
}

func (m Onboard) getStyledForm() string {
	m.form.WithTheme(theme()).WithWidth(m.width - (m.width / 4))
	return m.renderer.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("E59500")).
		PaddingTop(2).
		PaddingLeft(2).
		PaddingRight(2).
		Render(m.form.View())
}

func (m Onboard) getBottomBar() string {
	style := m.renderer.NewStyle().
		Background(lipgloss.Color("E59500")).
		Foreground(lipgloss.Color("0")).
		MarginBottom(1)
	left := m.renderer.NewStyle().Align(lipgloss.Left).PaddingLeft(1).
		Render("")
	right := m.renderer.NewStyle().Align(lipgloss.Right).PaddingRight(1).
		Render("")
	padding := strings.Repeat(" ",
		m.width-(lipgloss.Width(left)+lipgloss.Width(right)))
	bar := lipgloss.JoinHorizontal(lipgloss.Center, left, padding, right)
	return style.Render(bar)
}

func theme() *huh.Theme {
	t := huh.ThemeBase()
	var (
		background = lipgloss.AdaptiveColor{Dark: "#282a36"}
		selection  = lipgloss.AdaptiveColor{Dark: "#44475a"}
		foreground = lipgloss.AdaptiveColor{Dark: "#f8f8f2"}
		comment    = lipgloss.AdaptiveColor{Dark: "#6272a4"}
		green      = lipgloss.AdaptiveColor{Dark: "#50fa7b"}
		prompt     = lipgloss.AdaptiveColor{Dark: "#E59500"}
		red        = lipgloss.AdaptiveColor{Dark: "#ff5555"}
		yellow     = lipgloss.AdaptiveColor{Dark: "#f1fa8c"}
	)

	t.Focused.Base = t.Focused.Base.BorderForeground(selection)
	t.Focused.Title = t.Focused.Title.Foreground(prompt)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(prompt)
	t.Focused.Description = t.Focused.Description.Foreground(comment)
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(red)
	t.Focused.Directory = t.Focused.Directory.Foreground(prompt)
	t.Focused.File = t.Focused.File.Foreground(foreground)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(red)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(yellow)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(yellow)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(yellow)
	t.Focused.Option = t.Focused.Option.Foreground(foreground)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(yellow)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(green)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(green)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(foreground)
	t.Focused.UnselectedPrefix = t.Focused.UnselectedPrefix.Foreground(comment)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(yellow).Background(prompt).Bold(true)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(foreground).Background(background)

	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(yellow)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(comment)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(yellow)

	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	return t
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
