// Package tui is the interactive front end. It renders and dispatches, and it
// decides nothing about images: every conversion it starts goes through the same
// batch/convert/imageio path the CLI uses, because a behaviour that differs
// between the two front ends is a bug and not a feature of the TUI.
package tui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the interactive front end and blocks until the operator quits. It
// offers the image files in the process's working directory and writes each
// conversion beside its input.
//
// This signature is the boundary cmd depends on: cmd starts the TUI when it was
// given no arguments and does nothing else with this package.
func Run() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(NewModel(dir)).Run()
	return err
}
