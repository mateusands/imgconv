package go_test

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"testing"
)

// writePNG puts a real, non-uniform PNG on disk. Non-uniform on purpose: a solid
// colour encodes to nearly the same bytes at any size, which would let a test
// comparing file contents pass when it should not.
func writePNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 32), G: uint8(y * 32), B: 128, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// animatedGIFFixture encodes n real frames with the standard library, so a file
// this test calls animated is one the stdlib itself agrees is animated.
func animatedGIFFixture(t *testing.T, n int) []byte {
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
	return buf.Bytes()
}
