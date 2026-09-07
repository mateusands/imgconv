// The contract of the pure part of the TUI: which files it offers, which formats
// it lets the operator aim at, where a conversion lands, and how the cursor moves.
//
// Why it exists: the Bubble Tea loop itself gets no automated test in v1 (the plan
// says so, and teatest is its own piece of work), so everything that can be
// decided WITHOUT a terminal is pulled out into plain functions and tested here.
// What is left in the loop is dispatch and rendering, which a human has to look at.
//
// Two of these are rules rather than accidents:
//   - the file list is decided by the BYTES, never by the extension. A picker that
//     trusted the name would offer a .jpg holding text and hide a PNG called
//     notes.txt;
//   - the target list comes from the registry in imageio and is filtered by
//     CanEncode. A decode-only format must never be offered as a target, and a
//     list of format names written out in this package would be a second source
//     of truth next to the registry.
//
// The cursor clamping is a decision, not a rule: wrapping would be just as valid,
// and the test states the choice so that changing it is deliberate.
package go_test

import (
	"bytes"
	. "github.com/mateusands/imgconv/internal/tui"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/mateusands/imgconv/internal/imageio"
)

func TestFileListing(t *testing.T) {
	dir := t.TempDir()
	writePNGBytes(t, filepath.Join(dir, "photo.png"))
	writePNGBytes(t, filepath.Join(dir, "notes.txt"))
	writeText(t, filepath.Join(dir, "broken.jpg"))
	writeText(t, filepath.Join(dir, "readme.md"))
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("could not create the subdirectory: %v", err)
	}

	t.Run("should_list_only_the_files_whose_bytes_are_an_image", func(t *testing.T) {
		got, err := ListImages(dir)
		if err != nil {
			t.Fatalf("ListImages returned an error: %v", err)
		}
		want := []string{"notes.txt", "photo.png"}
		if len(got) != len(want) {
			t.Fatalf("listed %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("listed %v, want %v", got, want)
			}
		}
	})

	t.Run("should_report_the_error_when_the_directory_cannot_be_read", func(t *testing.T) {
		if _, err := ListImages(filepath.Join(dir, "there-is-no-such-directory")); err == nil {
			t.Fatal("ListImages accepted a directory that does not exist")
		}
	})
}

func TestTargetFormats(t *testing.T) {
	t.Run("should_offer_only_formats_that_can_be_encoded", func(t *testing.T) {
		for _, f := range TargetFormats() {
			if !f.CanEncode() {
				t.Fatalf("%s is decode-only and was offered as a target", f.Name)
			}
		}
	})

	t.Run("should_offer_every_encodable_row_of_the_registry", func(t *testing.T) {
		offered := map[string]bool{}
		for _, f := range TargetFormats() {
			offered[f.Name] = true
		}
		for _, f := range imageio.Formats() {
			if f.CanEncode() && !offered[f.Name] {
				t.Fatalf("%s is in the registry and can encode, but was not offered", f.Name)
			}
			if !f.CanEncode() && offered[f.Name] {
				t.Fatalf("%s is decode-only in the registry, but was offered", f.Name)
			}
		}
	})
}

func TestCursorMovement(t *testing.T) {
	cases := []struct {
		name   string
		cursor int
		delta  int
		length int
		want   int
	}{
		{"should_move_down_one_item", 0, 1, 3, 1},
		{"should_move_up_one_item", 2, -1, 3, 1},
		{"should_stop_at_the_last_item_rather_than_wrap", 2, 1, 3, 2},
		{"should_stop_at_the_first_item_rather_than_wrap", 0, -1, 3, 0},
		{"should_stay_at_zero_when_the_list_is_empty", 0, 1, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MoveCursor(c.cursor, c.delta, c.length); got != c.want {
				t.Fatalf("MoveCursor(%d, %d, %d) = %d, want %d", c.cursor, c.delta, c.length, got, c.want)
			}
		})
	}
}

// writePNGBytes writes a real 2x2 PNG. Real bytes rather than a fake: the thing
// under test is whether the sniffer accepts the file, so a stand-in would test
// nothing.
func writePNGBytes(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("could not encode the test PNG: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("could not write %s: %v", path, err)
	}
}

func writeText(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("this is not an image\n"), 0o644); err != nil {
		t.Fatalf("could not write %s: %v", path, err)
	}
}
