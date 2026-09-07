package web

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ErrNoPicker says the machine has no native file dialog installed. It is a
// missing convenience, never a broken tool: the operator can still browse.
var ErrNoPicker = errors.New("no native file dialog found: install kdialog or zenity")

// pickTimeout bounds how long a dialog may stay open. It is generous because a
// human is choosing a file, and bounded because a dialog nobody closes must not
// hold a server goroutine for the life of the process.
const pickTimeout = 5 * time.Minute

// Picker opens the operating system's own file dialog and returns the files
// chosen. An empty result with a nil error means the operator pressed cancel,
// which is the common case and is not a failure.
//
// There is no folder mode. Choosing a folder was a second route to the same
// place, and the operator has to name files either way.
//
// It is a field on Server rather than a package function so a test can supply one:
// the real thing needs a human and a desktop, and neither belongs in a suite.
type Picker func() ([]string, error)

// SetPicker replaces the dialog. Tests use it; nothing else should.
func (s *Server) SetPicker(p Picker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.picker = p
}

// nativePicker shells out to whichever dialog this desktop has.
//
// 🔴 The arguments are fixed strings and nothing from the request reaches them.
// A dialog is launched with exec.Command and no shell, so there is no string for
// a caller to inject into — the only thing the browser controls is WHICH of the
// two fixed modes runs.
func nativePicker() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pickTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch {
	case have("kdialog"):
		cmd = exec.CommandContext(ctx, "kdialog", "--multiple", "--separate-output", "--getopenfilename", ".")
	case have("zenity"):
		cmd = exec.CommandContext(ctx, "zenity", "--file-selection", "--multiple", "--separator", "\n")
	default:
		return nil, ErrNoPicker
	}

	out, err := cmd.Output()
	if err != nil {
		// Both dialogs exit non-zero when the operator cancels. That is an answer,
		// not a failure, and treating it as one would put an error on screen every
		// time somebody changes their mind.
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("the file dialog was left open too long")
		}
		return nil, err
	}

	var picked []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			picked = append(picked, line)
		}
	}
	return picked, nil
}

func have(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}
