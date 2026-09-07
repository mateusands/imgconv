package tui

import "github.com/charmbracelet/lipgloss"

// Every colour, width and marker this package renders lives here and nowhere
// else. A style literal inside a View() is unchangeable in practice: nobody finds
// the other five renders that also hardcoded that colour.
//
// The colours are 256-colour ANSI indexes rather than hex, so they follow the
// terminal's own palette instead of fighting the operator's theme.
var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	headingStyle  = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	itemStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	pathStyle     = lipgloss.NewStyle().Underline(true)
	successStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	helpStyle     = lipgloss.NewStyle().Faint(true)
)

// The markers are the same width on purpose: an unselected row must not shift
// sideways when the cursor leaves it.
const (
	cursorMarker = "> "
	blankMarker  = "  "
)
