// The contract for a directory run: one broken file must not cost the others, and
// no file may be silently lost.
//
// WHY THIS EXISTS: a batch is the only place in this program where a failure can
// hide. A single conversion either works or prints its error; a run over a folder
// can convert nine files, quietly skip the tenth, and exit 0 — and the operator
// finds out months later. So every input is accounted for in the summary, and the
// run exits non-zero if any of them failed.
//
// WHAT IS A RULE AND WHAT IS AN ACCIDENT: converting the rest of the folder when
// one file is broken is a rule. Refusing the WHOLE run when two inputs would land
// on one output name is also a rule, and a stricter one — it happens before
// anything is written, and --force does not lift it, because there is no
// "intended" winner between two files the operator named equally. That the
// results come back sorted by input path is a rule too: a run whose output order
// changes between identical invocations cannot be diffed.
//
// The concurrency bound is tested by observation, not by reading the code: a
// test-only format blocks inside Encode and records how many encoders were ever
// running at once. A pool that ignores its limit passes every other test here.
package go_test

import (
	"bytes"
	"fmt"
	. "github.com/mateusands/imgconv/internal/batch"
	"image"
	"image/color"
	"image/gif"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mateusands/imgconv/internal/imageio"
)

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

func pngFormat(t *testing.T) imageio.Format {
	t.Helper()
	f, ok := imageio.Lookup("png")
	if !ok {
		t.Fatal("png is not in the registry")
	}
	return f
}

// writeImage puts a real encoded image on disk under the given name and returns
// the bytes written, so a test can prove the input survived the run.
func writeImage(t *testing.T, dir, name, format string) []byte {
	t.Helper()
	f, ok := imageio.Lookup(format)
	if !ok {
		t.Fatalf("%s is not in the registry", format)
	}
	var buf bytes.Buffer
	if err := f.Encode(&buf, testImage(t), imageio.Options{}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func inputNames(s Summary) []string {
	names := make([]string, 0, len(s.Results))
	for _, r := range s.Results {
		names = append(names, filepath.Base(r.Input))
	}
	sort.Strings(names)
	return names
}

func TestRun_ShouldConvertEverySupportedFileInTheDirectory(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	writeImage(t, in, "a.png", "png")
	writeImage(t, in, "b.gif", "gif")
	writeImage(t, in, "c.tif", "tiff")

	got, err := Run(Request{InputDir: in, OutputDir: out, Target: pngFormat(t), Jobs: 2})
	if err != nil {
		t.Fatalf("the run should not have been refused: %v", err)
	}
	if n := len(got.Failures()); n != 0 {
		t.Fatalf("expected no failures, got %d: %v", n, got.Failures())
	}
	if got.Converted() != 3 {
		t.Errorf("converted = %d, want 3", got.Converted())
	}
	for _, name := range []string{"a.png", "b.png", "c.png"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("%s was not produced: %v", name, err)
		}
	}
}

func TestRun_ShouldConvertTheRemainingFilesWhenOneIsBroken(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	writeImage(t, in, "good1.png", "png")
	writeImage(t, in, "good2.png", "png")
	// A .jpg holding text: the extension claims an image, the bytes are not one.
	if err := os.WriteFile(filepath.Join(in, "broken.jpg"), []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Run(Request{InputDir: in, OutputDir: out, Target: pngFormat(t), Jobs: 2})
	if err != nil {
		t.Fatalf("one bad file must not refuse the run: %v", err)
	}
	if got.Converted() != 2 {
		t.Errorf("converted = %d, want the two good files", got.Converted())
	}
	failures := got.Failures()
	if len(failures) != 1 {
		t.Fatalf("expected exactly one failure, got %d: %v", len(failures), failures)
	}
	if filepath.Base(failures[0].Input) != "broken.jpg" {
		t.Errorf("the failure names %q, want broken.jpg", failures[0].Input)
	}
	if failures[0].Err == nil {
		t.Error("a failure with a nil error tells the operator nothing")
	}
}

func TestRun_ShouldRefuseTheWholeRunWhenTwoInputsCollideOnOneOutputName(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	writeImage(t, in, "photo.png", "png")
	writeImage(t, in, "photo.gif", "gif")

	_, err := Run(Request{InputDir: in, OutputDir: out, Target: pngFormat(t), Jobs: 2})
	if err == nil {
		t.Fatal("two inputs landing on photo.png must refuse the run")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("photo.png")) {
		t.Errorf("the refusal must name the colliding output, got %q", err)
	}

	// Refused BEFORE converting anything: the output directory stays empty.
	entries, readErr := os.ReadDir(out)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Errorf("the run was refused but wrote %d file(s) anyway", len(entries))
	}
}

func TestRun_ShouldRefuseACollisionEvenWhenForced(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	writeImage(t, in, "photo.png", "png")
	writeImage(t, in, "photo.gif", "gif")

	if _, err := Run(Request{InputDir: in, OutputDir: out, Target: pngFormat(t), Force: true, Jobs: 2}); err == nil {
		t.Fatal("--force replaces an output; it does not pick a winner between two inputs")
	}
}

func TestRun_ShouldIncludeAFileWhoseExtensionLiesButWhoseBytesDecode(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	writeImage(t, in, "secretly-a-png.txt", "png")

	got, err := Run(Request{InputDir: in, OutputDir: out, Target: pngFormat(t), Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Converted() != 1 {
		t.Fatalf("the bytes decode, so it is an image: converted = %d, want 1", got.Converted())
	}
	if _, err := os.Stat(filepath.Join(out, "secretly-a-png.png")); err != nil {
		t.Errorf("expected secretly-a-png.png: %v", err)
	}
}

func TestRun_ShouldSkipAFileThatNeitherDecodesNorClaimsToBeAnImage(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	writeImage(t, in, "real.png", "png")
	if err := os.WriteFile(filepath.Join(in, "README.md"), []byte("# notes"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Run(Request{InputDir: in, OutputDir: out, Target: pngFormat(t), Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 1 {
		t.Errorf("a README is not a failed conversion, it is not a conversion: results = %v", inputNames(got))
	}
	if len(got.Failures()) != 0 {
		t.Errorf("skipping a plainly non-image file must not be reported as a failure: %v", got.Failures())
	}
}

func TestRun_ShouldNotRecurseIntoSubdirectories(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	writeImage(t, in, "top.png", "png")
	sub := filepath.Join(in, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeImage(t, sub, "deep.png", "png")

	got, err := Run(Request{InputDir: in, OutputDir: out, Target: pngFormat(t), Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if names := inputNames(got); len(names) != 1 || names[0] != "top.png" {
		t.Errorf("one level only: got %v, want [top.png]", names)
	}
}

func TestRun_ShouldLeaveEveryInputByteIdentical(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	before := map[string][]byte{
		"a.png": writeImage(t, in, "a.png", "png"),
		"b.gif": writeImage(t, in, "b.gif", "gif"),
	}

	if _, err := Run(Request{InputDir: in, OutputDir: out, Target: pngFormat(t), Force: true, Jobs: 2}); err != nil {
		t.Fatal(err)
	}
	for name, want := range before {
		got, err := os.ReadFile(filepath.Join(in, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s changed: had %d bytes, now has %d", name, len(want), len(got))
		}
	}
}

func TestRun_ShouldReturnResultsSortedByInputPath(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	for _, n := range []string{"c.png", "a.png", "b.png"} {
		writeImage(t, in, n, "png")
	}

	got, err := Run(Request{InputDir: in, OutputDir: out, Target: pngFormat(t), Jobs: 3})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range got.Results {
		names = append(names, filepath.Base(r.Input))
	}
	want := []string{"a.png", "b.png", "c.png"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Errorf("results order = %v, want %v — an unstable order cannot be diffed", names, want)
	}
}

func TestRun_ShouldRejectAJobsCountBelowOne(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	writeImage(t, in, "a.png", "png")

	if _, err := Run(Request{InputDir: in, OutputDir: out, Target: pngFormat(t), Jobs: -1}); err == nil {
		t.Error("a negative --jobs is a usage error, not a silent clamp")
	}
}

// countingFormat blocks every encoder until all of them that will ever run at
// once have arrived, and records the high-water mark. It is the only way to
// observe the pool's limit: a pool that ignores it passes every other test here.
func countingFormat(concurrent *int32, peak *int32, gate chan struct{}) imageio.Format {
	return imageio.Format{
		Name:       "counting",
		Extensions: []string{".png"},
		Encode: func(w io.Writer, img image.Image, o imageio.Options) error {
			now := atomic.AddInt32(concurrent, 1)
			for {
				old := atomic.LoadInt32(peak)
				if now <= old || atomic.CompareAndSwapInt32(peak, old, now) {
					break
				}
			}
			<-gate
			atomic.AddInt32(concurrent, -1)
			png, _ := imageio.Lookup("png")
			return png.Encode(w, img, o)
		},
	}
}

func TestRun_ShouldNeverRunMoreEncodersAtOnceThanJobsAllows(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	const files, jobs = 8, 2
	for i := 0; i < files; i++ {
		writeImage(t, in, fmt.Sprintf("f%d.png", i), "png")
	}

	var concurrent, peak int32
	gate := make(chan struct{})

	done := make(chan error, 1)
	go func() {
		_, err := Run(Request{
			InputDir: in, OutputDir: out,
			Target: countingFormat(&concurrent, &peak, gate),
			Jobs:   jobs,
		})
		done <- err
	}()

	// Wait for the pool to saturate before releasing anyone. Sending one token at
	// a time instead would let a worker finish and re-enter before its sibling had
	// even arrived, so the peak would read 1 on a perfectly correct pool — the
	// test would be measuring the scheduler, not the limit.
	deadline := time.Now().Add(10 * time.Second)
	for atomic.LoadInt32(&concurrent) < jobs {
		if time.Now().After(deadline) {
			close(gate)
			t.Fatalf("only %d encoder(s) ever started with --jobs %d", atomic.LoadInt32(&concurrent), jobs)
		}
		time.Sleep(time.Millisecond)
	}
	close(gate)

	if err := <-done; err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if got := atomic.LoadInt32(&peak); got > jobs {
		t.Errorf("%d encoders ran at once with --jobs %d — the limit is not being honoured", got, jobs)
	}
}

// animatedGIF encodes n real frames, so the fixture is a file the standard
// library itself agrees is animated.
func animatedGIF(t *testing.T, dir, name string, n int) {
	t.Helper()
	g := &gif.GIF{}
	for i := 0; i < n; i++ {
		p := image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{
			color.RGBA{A: 255}, color.RGBA{R: uint8(i * 40), A: 255},
		})
		p.SetColorIndex(i%4, 0, 1)
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 10)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A directory run flattens an animated GIF exactly as the single-file path does,
// so it has to SAY so exactly as the single-file path does. This test exists
// because it did not: the TUI and the CLI warned and a batch run did not, which
// is a behaviour differing between front ends over the same operation — a bug by
// AGENTS.md, and the kind that loses frames for a whole folder in silence.
func TestRun_ShouldReportThatFramesWereDroppedWhenFlatteningAnAnimatedGif(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	animatedGIF(t, in, "moving.gif", 3)
	writeImage(t, in, "still.png", "png")

	got, err := Run(Request{InputDir: in, OutputDir: out, Target: pngFormat(t), Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Failures()) != 0 {
		t.Fatalf("flattening is not a failure, it is a warning: %v", got.Failures())
	}

	var warned, quiet int
	for _, r := range got.Results {
		if r.Warning == "" {
			quiet++
			continue
		}
		warned++
		if filepath.Base(r.Input) != "moving.gif" {
			t.Errorf("%s was warned about; only the animated file should be", r.Input)
		}
	}
	if warned != 1 {
		t.Errorf("%d file(s) carried a warning, want exactly the animated one", warned)
	}
	if quiet != 1 {
		t.Errorf("%d file(s) were silent, want the still PNG to be", quiet)
	}
}
