// The contract: one file is converted in exactly one place.
//
// WHY THIS PACKAGE EXISTS: decode -> transform -> encode was written three times
// — the CLI, the TUI and the directory run — and the copies had already drifted.
// Two of them warned that an animated GIF was being flattened and one did not, so
// converting a folder dropped every frame after the first in silence while
// converting the same file alone said so. AGENTS.md calls a behaviour that
// differs between the front ends a bug. These tests hold the seam shut.
//
// The output-naming tests were moved here from internal/tui when the same
// decision was found in three packages; they test one implementation now.
package go_test

import (
	. "github.com/mateusands/imgconv/internal/pipeline"
	"os"
	"path/filepath"
	"testing"

	"github.com/mateusands/imgconv/internal/imageio"
)

func pngFormat(t *testing.T) imageio.Format {
	t.Helper()
	f, ok := imageio.Lookup("png")
	if !ok {
		t.Fatal("png is not in the registry")
	}
	return f
}

func TestOutputPath_ShouldKeepTheBasenameAndTakeTheTargetExtension(t *testing.T) {
	png := pngFormat(t)

	if got := OutputPath("/photos/holiday.jpg", "", png); got != "/photos/holiday.png" {
		t.Errorf("got %q, want /photos/holiday.png", got)
	}
	if got := OutputPath("/photos/holiday", "", png); got != "/photos/holiday.png" {
		t.Errorf("an input with no extension should still get one: got %q", got)
	}
}

func TestOutputPath_ShouldLandBesideTheInputWhenNoDirectoryIsGiven(t *testing.T) {
	if got := filepath.Dir(OutputPath("/photos/holiday.jpg", "", pngFormat(t))); got != "/photos" {
		t.Errorf("output landed in %q, want /photos — not the working directory", got)
	}
}

func TestOutputPath_ShouldUseTheGivenDirectoryWhenThereIsOne(t *testing.T) {
	if got := OutputPath("/photos/holiday.jpg", "/out", pngFormat(t)); got != "/out/holiday.png" {
		t.Errorf("got %q, want /out/holiday.png", got)
	}
}

// The seam this package was extracted to close: a caller must be TOLD that
// frames were dropped, and every caller gets the same answer because there is
// only one place that can answer.
func TestConvert_ShouldReportThatFramesWereDroppedWhenTheInputIsAnimated(t *testing.T) {
	dir := t.TempDir()
	animated := filepath.Join(dir, "moving.gif")
	if err := os.WriteFile(animated, animatedGIFBytes(t, 3), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := Convert(Request{
		Input:  animated,
		Output: OutputPath(animated, "", pngFormat(t)),
		Target: pngFormat(t),
	})
	if err != nil {
		t.Fatalf("converting: %v", err)
	}
	if out.Warning == "" {
		t.Error("flattening an animated GIF must say so; silence here loses frames without a word")
	}
	if out.Format != "gif" {
		t.Errorf("format = %q, want gif", out.Format)
	}
}

func TestConvert_ShouldStaySilentWhenNothingWasLost(t *testing.T) {
	dir := t.TempDir()
	still := filepath.Join(dir, "still.gif")
	if err := os.WriteFile(still, animatedGIFBytes(t, 1), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := Convert(Request{
		Input:  still,
		Output: OutputPath(still, "", pngFormat(t)),
		Target: pngFormat(t),
	})
	if err != nil {
		t.Fatalf("converting: %v", err)
	}
	if out.Warning != "" {
		t.Errorf("a single-frame GIF lost nothing, so warning %q is noise", out.Warning)
	}
}

func TestConvert_ShouldRefuseToWriteOverTheInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.gif")
	if err := os.WriteFile(input, animatedGIFBytes(t, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	gifFormat, _ := imageio.Lookup("gif")

	// Target the input itself, forced. The refusal belongs to imageio.Write and
	// this test is here to prove the pipeline does not route around it.
	if _, err := Convert(Request{Input: input, Output: input, Target: gifFormat, Force: true}); err == nil {
		t.Fatal("converting a file onto itself must be refused")
	}
	after, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the input changed: had %d bytes, now has %d", len(before), len(after))
	}
}
