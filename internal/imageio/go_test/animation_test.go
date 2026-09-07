// The contract: converting an animated GIF to a still format keeps only the first
// frame, and the operator must be TOLD so rather than silently losing the rest.
//
// WHY THIS LIVES IN imageio AND NOT IN convert: only this package knows what
// format the bytes were. convert takes an image.Image and is deliberately blind to
// where it came from, so it cannot detect animation without breaking the layer
// arrow. The approved plan's BDD table assigned this row to convert; that is
// recorded as a deviation.
//
// WHY IT DOES NOT USE gif.DecodeAll: that decompresses every frame of a file this
// program does not trust, which is the decode bomb the read path exists to refuse.
// Frame counting walks the block structure instead and decompresses nothing, so a
// hostile file costs its own size in reads and no allocation per frame.
package go_test

import (
	"bytes"
	. "github.com/mateusands/imgconv/internal/imageio"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"testing"
)

// animatedGIF encodes n real frames. Built with the standard library rather than
// by hand so the fixture is a file the stdlib itself agrees is animated.
func animatedGIF(t testing.TB, n int) []byte {
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
		t.Fatalf("building the %d-frame fixture: %v", n, err)
	}
	return buf.Bytes()
}

func sourceFrom(t *testing.T, name string, b []byte) *Source {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close() })
	return src
}

func TestIsAnimated_ShouldReportTrueWhenTheGifHasMoreThanOneFrame(t *testing.T) {
	src := sourceFrom(t, "moving.gif", animatedGIF(t, 3))

	animated, err := src.IsAnimated("gif")
	if err != nil {
		t.Fatalf("counting frames: %v", err)
	}
	if !animated {
		t.Error("a 3-frame GIF should be reported as animated")
	}
}

func TestIsAnimated_ShouldReportFalseWhenTheGifHasOneFrame(t *testing.T) {
	src := sourceFrom(t, "still.gif", animatedGIF(t, 1))

	animated, err := src.IsAnimated("gif")
	if err != nil {
		t.Fatalf("counting frames: %v", err)
	}
	if animated {
		t.Error("a 1-frame GIF is not animated, and warning about it would be noise")
	}
}

func TestIsAnimated_ShouldReportFalseForAFormatThatCannotAnimate(t *testing.T) {
	var buf bytes.Buffer
	png, _ := Lookup("png")
	if err := png.Encode(&buf, testImage(t), Options{}); err != nil {
		t.Fatal(err)
	}
	src := sourceFrom(t, "still.png", buf.Bytes())

	animated, err := src.IsAnimated("png")
	if err != nil {
		t.Fatalf("a non-GIF should answer without error: %v", err)
	}
	if animated {
		t.Error("PNG cannot animate here, so it must never be reported as animated")
	}
}

// The bomb case. This fixture declares 400 frames of 4x4 in a few kilobytes; the
// point is that answering the question must not cost 400 decompressed frames.
// Asserting the ANSWER only proves correctness, so this test also asserts the read
// path is still usable afterwards — a DecodeAll implementation would have already
// allocated everything by the time it returned.
func TestIsAnimated_ShouldAnswerWithoutDecompressingEveryFrame(t *testing.T) {
	fixture := animatedGIF(t, 400)
	if len(fixture) > 200_000 {
		t.Fatalf("the fixture should stay small to make the point, got %d bytes", len(fixture))
	}
	src := sourceFrom(t, "many.gif", fixture)

	animated, err := src.IsAnimated("gif")
	if err != nil {
		t.Fatalf("counting frames: %v", err)
	}
	if !animated {
		t.Error("400 frames should be reported as animated")
	}

	// The file must still be positioned so a normal decode works after the check.
	if _, format, err := src.Decode(Limits{}); err != nil || format != "gif" {
		t.Errorf("decode after the animation check failed: format=%q err=%v", format, err)
	}
}

func TestIsAnimated_ShouldNotConsumeThePositionNeededByDecode(t *testing.T) {
	src := sourceFrom(t, "order.gif", animatedGIF(t, 2))

	if _, _, err := src.Decode(Limits{}); err != nil {
		t.Fatalf("decode before the check: %v", err)
	}
	if _, err := src.IsAnimated("gif"); err != nil {
		t.Fatalf("the check must work after a decode too: %v", err)
	}
}

func TestSniff_ShouldReportTheFormatFromTheBytesNotTheExtension(t *testing.T) {
	var buf bytes.Buffer
	png, _ := Lookup("png")
	if err := png.Encode(&buf, testImage(t), Options{}); err != nil {
		t.Fatal(err)
	}
	// Named .jpg on purpose: Sniff is the function a batch run trusts to decide
	// what a file is, so it must not be fooled by the name either.
	src := sourceFrom(t, "lying.jpg", buf.Bytes())

	format, err := src.Sniff()
	if err != nil {
		t.Fatalf("sniffing: %v", err)
	}
	if format != "png" {
		t.Errorf("format = %q, want \"png\" — the extension was trusted", format)
	}
}

func TestSniff_ShouldFailForBytesThatAreNotAnImage(t *testing.T) {
	src := sourceFrom(t, "notes.txt", []byte("this is not an image"))

	if _, err := src.Sniff(); err == nil {
		t.Error("plain text is not an image and sniffing it must say so")
	}
}

// animatedGIFBytes is the fuzz seed builder; it takes testing.TB so both a *T
// and an *F can call it.
func animatedGIFBytes(tb testing.TB, n int) []byte { return animatedGIF(tb, n) }
