package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorMuted  = lipgloss.Color("245")
	colorBlue   = lipgloss.Color("39")
	colorYellow = lipgloss.Color("221")
	colorRed    = lipgloss.Color("203")

	titleBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().Foreground(colorMuted)

	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62"))

	columnHeaderStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1)

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(0, 1)

	cardSelectedStyle = cardStyle.BorderForeground(lipgloss.Color("62"))

	modalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(1, 2)

	errorStyle = lipgloss.NewStyle().Foreground(colorRed)
)

func statusGlyph(s string) string {
	switch s {
	case "in-progress":
		return "◐"
	case "done":
		return "●"
	default:
		return "○"
	}
}

func priorityStyle(p string) lipgloss.Style {
	switch p {
	case "low":
		return lipgloss.NewStyle().Foreground(colorBlue)
	case "medium":
		return lipgloss.NewStyle().Foreground(colorYellow)
	case "high":
		return lipgloss.NewStyle().Foreground(colorRed)
	default:
		return lipgloss.NewStyle().Foreground(colorMuted)
	}
}
