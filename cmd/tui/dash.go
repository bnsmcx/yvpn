package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type dash string

func (m dash) Init() tea.Cmd {
	return nil
}

func (m dash) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m dash) View() string {
	return fmt.Sprintf("Hello %s", m)
}

