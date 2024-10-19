package main

import (
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"
)

type model string

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	return fmt.Sprintf("Hello %s", m)
}

func main() {
	if _, err := tea.LogToFile("debug.log", "simple"); err != nil {
		log.Fatal(err)
	}

	// Initialize our program
	p := tea.NewProgram(model("world"))
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
