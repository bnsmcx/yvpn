package main

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if _, err := tea.LogToFile("debug.log", "simple"); err != nil {
		log.Fatal(err)
	}

	p := tea.NewProgram(NewOnboarding(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
