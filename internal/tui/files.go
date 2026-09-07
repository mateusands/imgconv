package tui

// ListImages, TargetFormats and MoveCursor are exported for one reason only: the
// tests for this package live in go_test/, which is a separate Go package and can
// therefore reach nothing unexported. Nothing outside internal/tui calls them.

import (
	"os"
	"path/filepath"

	"github.com/mateusands/imgconv/internal/imageio"
)

// ListImages returns the names of the files in dir whose BYTES decode as an
// image, in the order os.ReadDir gives them, which is sorted by name.
//
// The extension is not consulted. A picker that trusted the name would offer a
// .jpg holding text and hide a PNG called notes.txt.
//
// A file that cannot be opened or sniffed is left out silently: this is a
// directory listing, not a conversion, and the operator gets a message about the
// file they picked, never about every file they did not.
func ListImages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if isImage(filepath.Join(dir, e.Name())) {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// isImage reports whether path holds an image, by reading its header only.
func isImage(path string) bool {
	src, err := imageio.Open(path)
	if err != nil {
		return false
	}
	defer src.Close()
	_, err = src.Sniff()
	return err == nil
}

// TargetFormats returns the formats a conversion may aim at, in registry order.
//
// Decode-only rows are dropped here so the interface can never offer one. The
// registry in imageio is the single source: a list of format names written out in
// this package would be a second one, and it would go stale the first time a row
// is added there.
func TargetFormats() []imageio.Format {
	var out []imageio.Format
	for _, f := range imageio.Formats() {
		if f.CanEncode() {
			out = append(out, f)
		}
	}
	return out
}

// MoveCursor returns the cursor moved by delta inside a list of length items.
//
// It clamps rather than wraps: at the end of a list, pressing down again does
// nothing. An empty list keeps the cursor at zero, which is what stops the two
// selection screens from indexing a slice they have no entry in.
func MoveCursor(cursor, delta, length int) int {
	if length == 0 {
		return 0
	}
	next := cursor + delta
	if next < 0 {
		return 0
	}
	if next > length-1 {
		return length - 1
	}
	return next
}
