package main

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

const ACCENT_COLOR = "130"

func getTopBar(title string, renderer *lipgloss.Renderer, width int) string {
	style := renderer.NewStyle().
		Background(lipgloss.Color(ACCENT_COLOR)).
		Foreground(lipgloss.Color("0")).
		MarginBottom(1)
	left := renderer.NewStyle().Align(lipgloss.Left).PaddingLeft(1).
		Render(title)
	right := renderer.NewStyle().Align(lipgloss.Right).PaddingRight(1).
		Render(fmt.Sprintf("yVPN %s", VERSION))
	padding := strings.Repeat(" ",
		width-(lipgloss.Width(left)+lipgloss.Width(right)))
	bar := lipgloss.JoinHorizontal(lipgloss.Center, left, padding, right)
	return style.Render(bar)
}

func getBottomBar(renderer *lipgloss.Renderer, width int, help string) string {
	style := renderer.NewStyle().
		Background(lipgloss.Color(ACCENT_COLOR)).
		Foreground(lipgloss.Color("0")).
		MarginBottom(1)
	bar := lipgloss.Place(width, 1, lipgloss.Center, lipgloss.Center, help)
	return style.Render(bar)
}

func contain(height int, max int) int {
	if height > max {
		return max
	}

	return height
}
