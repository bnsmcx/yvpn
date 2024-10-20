package main

import (
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if _, err := tea.LogToFile("debug.log", "simple"); err != nil {
		log.Fatal(err)
	}

	do, good1 := os.LookupEnv("DIGITAL_OCEAN_TOKEN")
	ts, good2 := os.LookupEnv("TAILSCALE_API")
	if good1 && good2 {
		p := tea.NewProgram(NewDash(do, ts), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			log.Fatal(err)
		}
	} else {
		p := tea.NewProgram(NewOnboarding(), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			log.Fatal(err)
		}
	}
}
