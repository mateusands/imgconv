// The contract of the command line: what an invocation is allowed to mean, and
// what it must be told it cannot mean.
//
// WHY THIS EXISTS: every rule here is a case where guessing would be worse than
// refusing. `imgconv photo.jpg` with no target could plausibly mean four things,
// and picking one writes a file the operator did not ask for. `-o out.png --to
// jpeg` is a contradiction, not a precedence puzzle. `--quality 90` on a PNG
// target is a request the encoder cannot honour, and honouring it silently is how
// an operator ships a batch believing it was compressed.
//
// WHAT IS A RULE AND WHAT IS AN ACCIDENT: the exit codes are a rule and they are a
// published contract (.crew/operations.md) — 2 means the invocation was wrong, 1
// means a conversion failed, and the difference is what a shell script branches
// on. That an unknown or decode-only target exits 1 rather than 2 is that same
// contract read literally: it lists "unsupported target" as a conversion failure.
// That --outdir is refused on a single-file run is a rule for the same reason
// --quality is: it would otherwise silently redirect the output somewhere the
// operator did not name. That --jobs is accepted and ignored on a single-file run
// is deliberate and NOT the same case — it changes nothing about what is written.
//
// The exact wording of any message here is an accident; that it names the flag or
// the file is not.
package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mateusands/imgconv/internal/convert"
)

func requireUsageError(t *testing.T, err error, when string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a usage error when %s, got nil", when)
	}
	var u usageError
	if !errors.As(err, &u) {
		t.Fatalf("expected a usage error (exit 2) when %s, got %T: %v", when, err, err)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected the invocation to be accepted, got: %v", err)
	}
}

// inputFile puts a real file on disk. parse has to stat its input to tell a
// single-file run from a directory run, so these cases need a real path.
func inputFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("not an image; parse never reads the bytes"), 0o644); err != nil {
		t.Fatalf("writing the input file: %v", err)
	}
	return path
}

func testImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 128, A: 255})
		}
	}
	return img
}

func writePNG(t *testing.T, path string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, testImage()); err != nil {
		t.Fatalf("encoding the test PNG: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("writing the test PNG: %v", err)
	}
	return path
}

// writeAnimatedGIF puts a two-frame GIF on disk — the input A9 is about.
func writeAnimatedGIF(t *testing.T, path string) string {
	t.Helper()
	frame := func() *image.Paletted {
		p := image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White})
		p.SetColorIndex(1, 1, 1)
		return p
	}
	var buf bytes.Buffer
	err := gif.EncodeAll(&buf, &gif.GIF{
		Image: []*image.Paletted{frame(), frame()},
		Delay: []int{0, 0},
	})
	if err != nil {
		t.Fatalf("encoding the animated GIF: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("writing the animated GIF: %v", err)
	}
	return path
}

// --- one of -o / --to is required, and they may not disagree -----------------

func TestParse_ShouldRejectWhenNeitherOutputNorTargetIsGiven(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	_, err := parse([]string{in})

	requireUsageError(t, err, "neither -o nor --to was given")
}

func TestParse_ShouldRejectWhenOutputExtensionAndTargetDisagree(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	_, err := parse([]string{in, "-o", "out.png", "--to", "jpeg"})

	requireUsageError(t, err, "-o names a PNG and --to asks for JPEG")
}

func TestParse_ShouldAcceptWhenOutputExtensionAndTargetAgree(t *testing.T) {
	in := inputFile(t, "photo.png")

	cfg, err := parse([]string{in, "-o", "out.jpg", "--to", "jpeg"})

	requireNoError(t, err)
	if cfg.target.Name != "jpeg" {
		t.Errorf("target = %q, want jpeg", cfg.target.Name)
	}
}

func TestParse_ShouldTakeTheTargetFromTheOutputExtensionWhenOnlyOutputIsGiven(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	cfg, err := parse([]string{in, "-o", "out.tiff"})

	requireNoError(t, err)
	if cfg.target.Name != "tiff" {
		t.Errorf("target = %q, want tiff", cfg.target.Name)
	}
	if cfg.output != "out.tiff" {
		t.Errorf("output = %q, want out.tiff", cfg.output)
	}
}

func TestParse_ShouldPutTheOutputBesideTheInputWhenOnlyTargetIsGiven(t *testing.T) {
	in := inputFile(t, "photo.jpg")
	want := filepath.Join(filepath.Dir(in), "photo.png")

	cfg, err := parse([]string{in, "--to", "png"})

	requireNoError(t, err)
	if cfg.output != want {
		t.Errorf("output = %q, want %q", cfg.output, want)
	}
}

func TestParse_ShouldFailWithoutUsageWhenTheTargetFormatIsDecodeOnly(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	_, err := parse([]string{in, "--to", "webp"})

	if err == nil {
		t.Fatal("expected an error for a decode-only target, got nil")
	}
	var u usageError
	if errors.As(err, &u) {
		t.Errorf("an unsupported target is a conversion failure (exit 1), not a usage error: %v", err)
	}
}

func TestParse_ShouldFailWhenTheOutputExtensionNamesNoFormat(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	_, err := parse([]string{in, "-o", "out.txt"})

	if err == nil {
		t.Fatal("expected an error when the output extension names no format, got nil")
	}
}

// --- the shape of a directory run --------------------------------------------

func TestParse_ShouldRejectOutputFlagWhenTheInputIsADirectory(t *testing.T) {
	dir := t.TempDir()

	_, err := parse([]string{dir, "-o", "out.png"})

	requireUsageError(t, err, "-o names a file and the input is a directory")
}

func TestParse_ShouldRejectADirectoryInputWithoutOutdir(t *testing.T) {
	dir := t.TempDir()

	_, err := parse([]string{dir, "--to", "png"})

	requireUsageError(t, err, "a directory input was given without --outdir")
}

func TestParse_ShouldAcceptADirectoryRunWithTargetAndOutdir(t *testing.T) {
	dir := t.TempDir()
	out := t.TempDir()

	cfg, err := parse([]string{dir, "--to", "png", "--outdir", out})

	requireNoError(t, err)
	if !cfg.dir {
		t.Error("expected a directory run")
	}
	if cfg.outputDir != out {
		t.Errorf("outputDir = %q, want %q", cfg.outputDir, out)
	}
}

func TestParse_ShouldRejectOutdirOnASingleFileRun(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	_, err := parse([]string{in, "--to", "png", "--outdir", t.TempDir()})

	requireUsageError(t, err, "--outdir was given for a single file")
}

// --- --quality is a JPEG setting and nothing else ----------------------------

func TestParse_ShouldRejectQualityOnANonJpegTarget(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	_, err := parse([]string{in, "--to", "png", "--quality", "80"})

	requireUsageError(t, err, "--quality was given for a PNG target")
}

func TestParse_ShouldAcceptQualityOnAJpegTarget(t *testing.T) {
	in := inputFile(t, "photo.png")

	cfg, err := parse([]string{in, "--to", "jpeg", "--quality", "80"})

	requireNoError(t, err)
	if cfg.codec.Quality != 80 {
		t.Errorf("codec quality = %d, want 80", cfg.codec.Quality)
	}
}

func TestParse_ShouldRejectQualityOutsideOneToHundred(t *testing.T) {
	in := inputFile(t, "photo.png")

	for _, q := range []string{"0", "101", "-5"} {
		if _, err := parse([]string{in, "--to", "jpeg", "--quality", q}); err == nil {
			t.Errorf("--quality %s was accepted; 1-100 is the range", q)
		} else {
			requireUsageError(t, err, "--quality "+q+" is outside 1-100")
		}
	}
}

// --- --resize ----------------------------------------------------------------

func TestParse_ShouldReadEveryAcceptedResizeShape(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	cases := []struct {
		arg  string
		want convert.Options
	}{
		{"800x600", convert.Options{Width: 800, Height: 600}},
		{"800x", convert.Options{Width: 800}},
		{"x600", convert.Options{Height: 600}},
	}
	for _, c := range cases {
		cfg, err := parse([]string{in, "--to", "png", "--resize", c.arg})
		requireNoError(t, err)
		if cfg.transform != c.want {
			t.Errorf("--resize %s gave %+v, want %+v", c.arg, cfg.transform, c.want)
		}
	}
}

func TestParse_ShouldRejectAResizeDimensionThatIsZeroOrNegative(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	for _, arg := range []string{"0x600", "800x0", "-1x600", "800x-1", "0x", "x0"} {
		_, err := parse([]string{in, "--to", "png", "--resize", arg})
		requireUsageError(t, err, "--resize "+arg+" has a dimension that is zero or negative")
	}
}

func TestParse_ShouldRejectAResizeThatIsNotWidthByHeight(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	for _, arg := range []string{"800", "x", "axb", "800x600x400", ""} {
		_, err := parse([]string{in, "--to", "png", "--resize", arg})
		requireUsageError(t, err, "--resize "+arg+" is not WxH")
	}
}

func TestParse_ShouldLeaveTheTransformEmptyWhenResizeIsNotGiven(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	cfg, err := parse([]string{in, "--to", "png"})

	requireNoError(t, err)
	if (cfg.transform != convert.Options{}) {
		t.Errorf("transform = %+v, want the zero value, which is what asks for no resize", cfg.transform)
	}
}

// --- the remaining knobs ------------------------------------------------------

func TestParse_ShouldRejectAJobsCountBelowOne(t *testing.T) {
	dir := t.TempDir()

	for _, jobs := range []string{"0", "-2"} {
		_, err := parse([]string{dir, "--to", "png", "--outdir", dir, "--jobs", jobs})
		requireUsageError(t, err, "--jobs "+jobs+" is below one")
	}
}

func TestParse_ShouldRejectAMaxPixelsAboveTheCeiling(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	_, err := parse([]string{in, "--to", "png", "--max-pixels", "999999999999"})

	requireUsageError(t, err, "--max-pixels is above the ceiling imageio allows")
}

// --- the shapes with no input path -------------------------------------------

func TestParse_ShouldStartTheInterfaceWhenThereAreNoArguments(t *testing.T) {
	cfg, err := parse(nil)

	requireNoError(t, err)
	if !cfg.tui {
		t.Error("no arguments must start the interactive front end")
	}
}

func TestParse_ShouldRejectFlagsWithNoInputPath(t *testing.T) {
	_, err := parse([]string{"--to", "png"})

	requireUsageError(t, err, "a target was given with no input path")
}

func TestParse_ShouldRejectMoreThanOneInputPath(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	_, err := parse([]string{in, in, "--to", "png"})

	requireUsageError(t, err, "two input paths were given")
}

func TestParse_ShouldRejectAnUnknownFlag(t *testing.T) {
	in := inputFile(t, "photo.jpg")

	_, err := parse([]string{in, "--to", "png", "--sharpen"})

	requireUsageError(t, err, "an unknown flag was given")
}

// --- the exit-code contract (.crew/operations.md) ----------------------------

func TestExitCode_ShouldMapEachOutcomeToItsPublishedCode(t *testing.T) {
	if got := exitCode(nil); got != 0 {
		t.Errorf("success exited %d, want 0", got)
	}
	if got := exitCode(errors.New("photo.jpg: unexpected EOF")); got != 1 {
		t.Errorf("a conversion failure exited %d, want 1", got)
	}
	if got := exitCode(usagef("one of -o or --to is required")); got != 2 {
		t.Errorf("a usage error exited %d, want 2", got)
	}
}

func TestRun_ShouldExitTwoWhenTheInvocationIsWrong(t *testing.T) {
	in := inputFile(t, "photo.jpg")
	var stdout, stderr bytes.Buffer

	code := run([]string{in}, &stdout, &stderr)

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stderr.Len() == 0 {
		t.Error("a refused invocation must say why on stderr")
	}
}

func TestRun_ShouldExitOneAndNameTheFileWhenTheInputCannotBeRead(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.png")
	var stdout, stderr bytes.Buffer

	code := run([]string{missing, "--to", "jpeg"}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "absent.png") {
		t.Errorf("the error must name the file; stderr was %q", stderr.String())
	}
}

func TestRun_ShouldConvertOneFileAndExitZero(t *testing.T) {
	dir := t.TempDir()
	in := writePNG(t, filepath.Join(dir, "photo.png"))
	out := filepath.Join(dir, "photo.jpg")
	var stdout, stderr bytes.Buffer

	code := run([]string{in, "-o", out}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("the output file was not written: %v", err)
	}
}

func TestRun_ShouldRefuseToReplaceAnExistingOutputWithoutForce(t *testing.T) {
	dir := t.TempDir()
	in := writePNG(t, filepath.Join(dir, "photo.png"))
	out := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(out, []byte("previous output"), 0o644); err != nil {
		t.Fatalf("seeding the destination: %v", err)
	}
	var stdout, stderr bytes.Buffer

	code := run([]string{in, "-o", out}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	after, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading the destination back: %v", err)
	}
	if string(after) != "previous output" {
		t.Errorf("the existing destination was modified without --force; it now holds %d bytes", len(after))
	}
}

// A9: frames after the first are dropped, and dropping them silently is the
// failure this asserts against.
func TestRun_ShouldWarnOnStderrWhenAnAnimatedGifIsFlattened(t *testing.T) {
	dir := t.TempDir()
	in := writeAnimatedGIF(t, filepath.Join(dir, "loop.gif"))
	var stdout, stderr bytes.Buffer

	code := run([]string{in, "--to", "png"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	warning := stderr.String()
	if !strings.Contains(warning, "loop.gif") || !strings.Contains(warning, "first frame") {
		t.Errorf("expected a warning naming the file and the dropped frames, got %q", warning)
	}
}

func TestRun_ShouldNotWarnWhenTheGifHasOneFrame(t *testing.T) {
	dir := t.TempDir()
	still := filepath.Join(dir, "still.gif")
	var buf bytes.Buffer
	p := image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White})
	if err := gif.Encode(&buf, p, nil); err != nil {
		t.Fatalf("encoding the still GIF: %v", err)
	}
	if err := os.WriteFile(still, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("writing the still GIF: %v", err)
	}
	var stdout, stderr bytes.Buffer

	code := run([]string{still, "--to", "png"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "first frame") {
		t.Errorf("a one-frame GIF must not warn, got %q", stderr.String())
	}
}

// A6: a directory run names every failure and exits non-zero when any failed.
func TestRun_ShouldNameEveryFailureAndExitOneWhenADirectoryRunPartlyFails(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	writePNG(t, filepath.Join(in, "good.png"))
	if err := os.WriteFile(filepath.Join(in, "broken.jpg"), []byte("this is not a JPEG"), 0o644); err != nil {
		t.Fatalf("writing the broken file: %v", err)
	}
	var stdout, stderr bytes.Buffer

	// png and not tiff: batch names an output with Extensions[0], and tiff's list
	// begins ".tif", which would make this assert a registry ordering accident.
	code := run([]string{in, "--to", "png", "--outdir", out}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	broken := filepath.Join(in, "broken.jpg")
	if !strings.Contains(stderr.String(), broken) {
		t.Errorf("every failure must be named on stderr, got %q", stderr.String())
	}
	// Named once, not twice: imageio prefixes its own errors with the same path.
	if n := strings.Count(stderr.String(), broken); n != 1 {
		t.Errorf("the failed input is named %d times in one line: %q", n, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "good.png")); err != nil {
		t.Errorf("the healthy file must still have converted: %v", err)
	}
}
