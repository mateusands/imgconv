// The contract under fuzzing: a hostile file may make this program REFUSE, but it
// may never make it hang, panic, or allocate without bound.
//
// WHY THIS EXISTS: countGIFFrames is a hand-written parser of untrusted binary
// input — a block walk over lengths that the file itself supplies. That is the
// shape that produces infinite loops and unbounded reads, and AGENTS.md says
// every input file is hostile until decoded. The unit tests only ever feed it
// GIFs the standard library produced, which is exactly the input a bug like that
// survives.
package go_test

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/mateusands/imgconv/internal/imageio"
)

func FuzzIsAnimated(f *testing.F) {
	f.Add(animatedGIFBytes(f, 1))
	f.Add(animatedGIFBytes(f, 3))
	f.Add([]byte("GIF89a"))
	f.Add([]byte("GIF89a\x00\x00\x00\x00\x80\x00\x00"))
	// A global colour table flag set with no table behind it, then an image
	// descriptor: the shape that tempts a parser into reading past the end.
	f.Add([]byte("GIF89a\x01\x00\x01\x00\xF7\x00\x00\x2C"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "fuzz.gif")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Skip()
		}
		src, err := Open(path)
		if err != nil {
			t.Skip()
		}
		defer src.Close()

		// Only the answer is under test; an error is a legitimate answer. What is
		// NOT legitimate is not returning at all, or panicking on the way.
		_, _ = src.IsAnimated("gif")
	})
}
