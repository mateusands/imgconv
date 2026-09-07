// Package convert's contract: a transform is pure. It takes an image.Image and
// returns one; it never learns which format the image came from, never opens a
// file, and never mutates what it was given.
//
// WHY THESE TESTS EXIST: resize is the only place in this program where a
// dimension is computed instead of read, and every arithmetic mistake there is
// silent — an off-by-one ratio still produces a perfectly valid image, just the
// wrong one. So every test below asserts the resulting BOUNDS, never merely that
// no error came back.
//
// WHAT IS A RULE AND WHAT IS AN ACCIDENT: preserving the aspect ratio when only
// one side is given is a rule. Both sides given winning over the ratio is also a
// rule — the operator asked for that distortion. Both sides zero returning the
// IDENTICAL value rather than a copy is a rule too: it is what lets a caller
// resize unconditionally and pay nothing when no resize was asked for. That the
// result is an *image.RGBA is an accident of the scaler, and nothing may depend
// on it.
package go_test

import (
	. "github.com/mateusands/imgconv/internal/convert"
	"image"
	"image/color"
	"strings"
	"testing"
)

// gradient returns a non-uniform image of the given size. Non-uniform on purpose:
// a solid colour scales to something plausible whatever the arithmetic did.
func gradient(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 128, A: 255})
		}
	}
	return img
}

func assertBounds(t *testing.T, got image.Image, wantW, wantH int) {
	t.Helper()
	if got.Bounds().Dx() != wantW || got.Bounds().Dy() != wantH {
		t.Errorf("bounds = %v (%dx%d), want %dx%d",
			got.Bounds(), got.Bounds().Dx(), got.Bounds().Dy(), wantW, wantH)
	}
}

func TestResize_ShouldPreserveAspectRatioWhenOnlyWidthIsGiven(t *testing.T) {
	got, err := Resize(gradient(40, 20), Options{Width: 20})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	assertBounds(t, got, 20, 10)
}

func TestResize_ShouldPreserveAspectRatioWhenOnlyHeightIsGiven(t *testing.T) {
	got, err := Resize(gradient(40, 20), Options{Height: 5})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	assertBounds(t, got, 10, 5)
}

func TestResize_ShouldUseBothDimensionsWhenBothAreGiven(t *testing.T) {
	// 40x20 is 2:1; asking for 8x8 is a distortion, and getting it is the point.
	got, err := Resize(gradient(40, 20), Options{Width: 8, Height: 8})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	assertBounds(t, got, 8, 8)
}

func TestResize_ShouldReturnTheSameImageWhenNoDimensionIsGiven(t *testing.T) {
	src := gradient(40, 20)

	got, err := Resize(src, Options{})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	// Identity, not equality: a copy with the same pixels would pass an equality
	// check and still break the "no resize costs nothing" promise.
	if got != src {
		t.Errorf("got a different value %p, want the input %p back untouched", got, src)
	}
}

func TestResize_ShouldClampToOnePixelWhenTheDerivedDimensionRoundsToZero(t *testing.T) {
	// 100x2 scaled to width 10 derives a height of 0.2. A zero-sided image is not
	// a valid result, so the floor is one pixel.
	got, err := Resize(gradient(100, 2), Options{Width: 10})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	assertBounds(t, got, 10, 1)
}

func TestResize_ShouldReturnTheValidationErrorWhenADimensionIsNegative(t *testing.T) {
	got, err := Resize(gradient(40, 20), Options{Width: -5})
	if err == nil {
		t.Fatal("expected the validation error to surface, got nil")
	}
	if got != nil {
		t.Errorf("got an image %v alongside the error; a rejected resize returns none", got.Bounds())
	}
	if !strings.Contains(err.Error(), "-5") {
		t.Errorf("error %q does not name the offending value", err)
	}
}

func TestResize_ShouldFailWhenTheSourceHasNoPixels(t *testing.T) {
	// Deriving a side from an empty source divides by zero. Refusing is the only
	// answer that is not a panic.
	if _, err := Resize(gradient(0, 0), Options{Width: 10}); err == nil {
		t.Fatal("expected a refusal to scale a 0x0 source, got nil")
	}
}

func TestOptions_ShouldRejectWhenADimensionIsNegative(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want string // the field the message must name
	}{
		{"negative width", Options{Width: -1}, "width"},
		{"negative height", Options{Height: -720}, "height"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.opts.Validate()
			if err == nil {
				t.Fatalf("%+v was accepted", c.opts)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not name %q", err, c.want)
			}
		})
	}
}

func TestOptions_ShouldAcceptWhenBothDimensionsAreZero(t *testing.T) {
	// Zero/zero is how a caller says "no resize"; rejecting it would force every
	// caller to branch before calling.
	if err := (Options{}).Validate(); err != nil {
		t.Errorf("zero dimensions must be legal: %v", err)
	}
}

// The bound that was missing, and the audit that found it.
//
// The read path refuses a file whose HEADER declares more pixels than the limit.
// Nothing refused an OUTPUT that big, and the output's missing side is derived
// from the input's aspect ratio — which the file, not the operator, controls.
// A 76-byte PNG of 1x100 passes the decode guard with four orders of magnitude to
// spare and, asked for --resize 100x, produced 100x10000: a 10,000x amplification.
// The arithmetic is quadratic, so 1x10000 with --resize 10000x asks for about 4 TB.
//
// The limit therefore lives where the ALLOCATION is, not only where the decode is.
// It is an int64 here rather than an imageio.Limits because convert may not import
// imageio; the layer arrow is what keeps this package pure.
func TestResize_ShouldRefuseWhenTheDerivedOutputWouldBeLargerThanTheLimit(t *testing.T) {
	// One pixel wide, 100 tall: 100 pixels, a legitimate tiny file.
	sliver := image.NewRGBA(image.Rect(0, 0, 1, 100))

	// Asking for width 100 derives height 100*100/1 = 10000, so 1,000,000 pixels.
	_, err := Resize(sliver, Options{Width: 100, MaxPixels: 1000})
	if err == nil {
		t.Fatal("a derived output above the limit must be refused before it is allocated")
	}
	if !strings.Contains(err.Error(), "1000000") && !strings.Contains(err.Error(), "1,000,000") {
		t.Errorf("the refusal should say how big it would have been, got %q", err)
	}
}

func TestResize_ShouldAllowAnOutputExactlyAtTheLimit(t *testing.T) {
	sliver := image.NewRGBA(image.Rect(0, 0, 1, 100))

	// 100 x 10000 = 1,000,000 exactly. A bound that rejects everything passes the
	// far-over case just like a correct one; only the exact case tells them apart.
	if _, err := Resize(sliver, Options{Width: 100, MaxPixels: 1_000_000}); err != nil {
		t.Errorf("exactly at the limit must be allowed: %v", err)
	}
	if _, err := Resize(sliver, Options{Width: 100, MaxPixels: 999_999}); err == nil {
		t.Error("one pixel over the limit must be refused")
	}
}

// Fail closed: a caller that never sets MaxPixels still gets a ceiling. A zero
// meaning "unlimited" would put the hole back for every caller that forgets.
func TestResize_ShouldApplyADefaultCeilingWhenTheCallerSetsNone(t *testing.T) {
	sliver := image.NewRGBA(image.Rect(0, 0, 1, 100_000))

	// Derives 100000 * 100000 / 1 = 10^10 pixels. Nothing should allocate that.
	if _, err := Resize(sliver, Options{Width: 100_000}); err == nil {
		t.Fatal("an unset MaxPixels must still refuse an absurd output, not allocate 10^10 pixels")
	}
}

func TestOptions_ShouldRejectANegativeMaxPixels(t *testing.T) {
	if err := (Options{MaxPixels: -1}).Validate(); err == nil {
		t.Error("a negative ceiling is not a ceiling")
	}
}
