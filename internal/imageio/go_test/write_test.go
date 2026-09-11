// Package imageio's write path is the red zone of this program.
//
// THE CONTRACT: converting a file must never damage the file it read. Every other
// failure here is recoverable by running the command again; this one is not.
//
// WHY THESE TESTS EXIST: the first revision of the plan said "open the destination
// with O_TRUNC when --force, and also refuse when the destination is the input".
// In that order the refusal arrives too late — O_TRUNC has already emptied the
// operator's file. Cross-review caught it before any code existed. The tests below
// are that finding turned into something that fails out loud if the order is ever
// reversed again, including through a symlink or a hard link, which are the two
// paths a string comparison cannot see.
//
// WHAT IS A RULE AND WHAT IS AN ACCIDENT: the refusal to overwrite is a rule.
// The refusal to write over the input holds EVEN WITH --force, which is also a
// rule — --force means "replace this output", never "damage my source". That the
// unforced path deletes a partial file while the forced path writes through a
// temporary file is a rule too, and the reason is asymmetric: without --force the
// file it removes is one nobody had; with --force there is a real file at the
// destination that must survive a failed encode.
package go_test

import (
	"bytes"
	"errors"
	. "github.com/mateusands/imgconv/internal/imageio"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// testImage returns a small non-uniform image. Non-uniform on purpose: a solid
// colour compresses to nearly the same bytes whatever the dimensions, which would
// let a test comparing file contents pass when it should not.
func testImage(t *testing.T) image.Image {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 128, A: 255})
		}
	}
	return img
}

// writeSourceFile puts a real encoded PNG on disk and opens it as a Source, the
// way a conversion would. Returns the source and the bytes it started with.
func writeSourceFile(t *testing.T, path string) (*Source, []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating the source file: %v", err)
	}
	png, ok := Lookup("png")
	if !ok {
		t.Fatal("png is not in the registry")
	}
	if err := png.Encode(f, testImage(t), Options{}); err != nil {
		t.Fatalf("encoding the source file: %v", err)
	}
	f.Close()

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the source back: %v", err)
	}
	src, err := Open(path)
	if err != nil {
		t.Fatalf("opening the source: %v", err)
	}
	t.Cleanup(func() { src.Close() })
	return src, before
}

func assertUnchanged(t *testing.T, path string, before []byte) {
	t.Helper()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s back: %v", path, err)
	}
	if string(after) != string(before) {
		t.Errorf("%s changed: had %d bytes, now has %d", path, len(before), len(after))
	}
}

func pngFormat(t *testing.T) Format {
	t.Helper()
	f, ok := Lookup("png")
	if !ok {
		t.Fatal("png is not in the registry")
	}
	return f
}

func TestWrite_ShouldRefuseWhenOutputExistsAndNotForced(t *testing.T) {
	dir := t.TempDir()
	src, _ := writeSourceFile(t, filepath.Join(dir, "in.png"))

	target := filepath.Join(dir, "out.png")
	existing := []byte("a file the operator cares about")
	if err := os.WriteFile(target, existing, 0o644); err != nil {
		t.Fatal(err)
	}

	err := Write(target, src, testImage(t), pngFormat(t), Options{}, false)
	if err == nil {
		t.Fatal("expected a refusal, got nil — the destination was overwritten")
	}
	assertUnchanged(t, target, existing)
}

func TestWrite_ShouldOverwriteWhenForced(t *testing.T) {
	dir := t.TempDir()
	src, _ := writeSourceFile(t, filepath.Join(dir, "in.png"))

	target := filepath.Join(dir, "out.png")
	if err := os.WriteFile(target, []byte("replace me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(target, src, testImage(t), pngFormat(t), Options{}, true); err != nil {
		t.Fatalf("forced write should succeed: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "replace me" {
		t.Error("the destination was not replaced")
	}
	if _, _, err := image.Decode(bytes.NewReader(got)); err != nil {
		t.Errorf("the forced write did not produce a decodable image: %v", err)
	}
}

// The three aliasing tests below all pass --force, because that is the flag under
// which the bug existed. Unforced, O_EXCL refuses them for a different reason and
// would hide the defect.

func TestWrite_ShouldRefuseWhenTargetIsTheInputPath(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	src, before := writeSourceFile(t, in)

	err := Write(in, src, testImage(t), pngFormat(t), Options{}, true)
	if err == nil {
		t.Fatal("expected a refusal to write over the input, got nil")
	}
	assertUnchanged(t, in, before)
}

func TestWrite_ShouldRefuseWhenTargetIsASymlinkToTheInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	src, before := writeSourceFile(t, in)

	link := filepath.Join(dir, "alias.png")
	if err := os.Symlink(in, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	err := Write(link, src, testImage(t), pngFormat(t), Options{}, true)
	if err == nil {
		t.Fatal("expected a refusal: the symlink resolves to the input")
	}
	assertUnchanged(t, in, before)
}

func TestWrite_ShouldRefuseWhenTargetIsAHardLinkToTheInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	src, before := writeSourceFile(t, in)

	link := filepath.Join(dir, "hard.png")
	if err := os.Link(in, link); err != nil {
		t.Skipf("hard links unavailable here: %v", err)
	}

	err := Write(link, src, testImage(t), pngFormat(t), Options{}, true)
	if err == nil {
		t.Fatal("expected a refusal: the hard link is the same inode as the input")
	}
	assertUnchanged(t, in, before)
}

// failingFormat writes some bytes and then fails, which is the only way to reach
// the partial-output branch deliberately. A malformed input cannot do it: that
// fails before the encoder is ever called.
func failingFormat() Format {
	return Format{
		Name:       "failing",
		Extensions: []string{".fail"},
		Encode: func(w io.Writer, _ image.Image, _ Options) error {
			w.Write([]byte("partial output that must not survive"))
			return errors.New("encoder failed on purpose")
		},
	}
}

func TestWrite_ShouldLeaveNoFileWhenEncodeFailsAndNotForced(t *testing.T) {
	dir := t.TempDir()
	src, _ := writeSourceFile(t, filepath.Join(dir, "in.png"))

	target := filepath.Join(dir, "out.fail")
	if err := Write(target, src, testImage(t), failingFormat(), Options{}, false); err == nil {
		t.Fatal("expected the encode error to surface")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("a partial file was left at %s (stat err: %v)", target, err)
	}
}

func TestWrite_ShouldLeaveDestinationUntouchedWhenForcedEncodeFails(t *testing.T) {
	dir := t.TempDir()
	src, _ := writeSourceFile(t, filepath.Join(dir, "in.png"))

	target := filepath.Join(dir, "out.fail")
	existing := []byte("the previous output, still wanted if the new one fails")
	if err := os.WriteFile(target, existing, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(target, src, testImage(t), failingFormat(), Options{}, true); err == nil {
		t.Fatal("expected the encode error to surface")
	}
	assertUnchanged(t, target, existing)
}

// Three gaps an independent review found in this file, each one a case the
// original tests never reached.

// A dangling symlink is a destination that EXISTS as a directory entry and has
// nothing behind it. O_EXCL refuses it with EEXIST, so --force sends it to
// replace() — which opened it O_RDONLY, followed the link to nothing, and failed
// with ENOENT. The operator passed --force and got "no such file or directory"
// about a file they were trying to create.
func TestWrite_ShouldReplaceADanglingSymlinkWhenForced(t *testing.T) {
	dir := t.TempDir()
	src, before := writeSourceFile(t, filepath.Join(dir, "in.png"))

	link := filepath.Join(dir, "out.png")
	if err := os.Symlink(filepath.Join(dir, "nothing-here"), link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	if err := Write(link, src, testImage(t), pngFormat(t), Options{}, true); err != nil {
		t.Fatalf("--force must replace a dangling symlink: %v", err)
	}
	// Rename replaces the LINK, not whatever it pointed at, so nothing was
	// created at the dangling target.
	if _, err := os.Stat(filepath.Join(dir, "nothing-here")); err == nil {
		t.Error("the write followed the symlink and created its target")
	}
	assertUnchanged(t, filepath.Join(dir, "in.png"), before)
}

// A destination the operator had kept private must not come back world-readable
// because this program replaced it. CreateTemp makes 0600 and the old code
// forced 0644 unconditionally.
func TestWrite_ShouldKeepTheDestinationsPermissionsWhenForced(t *testing.T) {
	dir := t.TempDir()
	src, _ := writeSourceFile(t, filepath.Join(dir, "in.png"))

	target := filepath.Join(dir, "private.png")
	if err := os.WriteFile(target, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Write(target, src, testImage(t), pngFormat(t), Options{}, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("permissions became %04o, want 0600 — replacing a file must not widen who can read it", got)
	}
}

// NOT TESTED HERE, deliberately: the case where f.Encode succeeds and out.Close
// then fails — a full disk, where the bytes are buffered and the error only
// surfaces on the flush. Reaching it from this package would mean exporting
// machinery that exists for no other reason, so the fix (removing the file when
// the close fails) is verified by reading write.go rather than by this file.
